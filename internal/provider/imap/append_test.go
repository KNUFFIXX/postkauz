package imap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// dropProxy leitet zum Server weiter und kappt die Verbindung einmal genau
// dann, wenn der Server ein APPEND bestätigt (APPENDUID), bevor der Client
// die Bestätigung liest: Die Nachricht liegt dann auf dem Server, der
// Client sieht nur den Verbindungsabbruch.
type dropProxy struct {
	target string
	armed  atomic.Bool
	drops  atomic.Int32
	// refuse: Statt zu kappen, macht der Proxy aus der Bestätigung einmal
	// eine Ablehnung (NO [OVERQUOTA]), wie ein Server mit vollem
	// Kontingent (Gegenprüfung S2.4/S2.5, W3).
	refuse atomic.Bool
}

var appendOK = regexp.MustCompile(`(?m)^(\S+) OK \[APPENDUID[^\r\n]*`)

func (p *dropProxy) serve(t *testing.T, ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = c.Close()
			continue
		}
		var once sync.Once
		closeBoth := func() { once.Do(func() { _ = c.Close(); _ = s.Close() }) }
		go func() { _, _ = io.Copy(s, c); closeBoth() }()
		go func() {
			defer closeBoth()
			buf := make([]byte, 32<<10)
			for {
				n, err := s.Read(buf)
				if n > 0 {
					if bytes.Contains(buf[:n], []byte("APPENDUID")) && p.armed.CompareAndSwap(true, false) {
						p.drops.Add(1)
						return
					}
					out := buf[:n]
					if bytes.Contains(out, []byte("APPENDUID")) && p.refuse.CompareAndSwap(true, false) {
						out = appendOK.ReplaceAll(out, []byte("$1 NO [OVERQUOTA] Quota exceeded (Mailbox full)"))
					}
					if _, werr := c.Write(out); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
	}
}

func memServerFor(t *testing.T, boxes ...string) (string, *imapmemserver.User) {
	t.Helper()
	u := imapmemserver.NewUser("tester@example.invalid", "pw-test")
	for _, mb := range boxes {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps:         goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapIMAP4rev2: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String(), u
}

func draftMail(mid, body string) []byte {
	return []byte(fmt.Sprintf("Message-Id: <%s>\r\nFrom: a@example.org\r\nTo: b@example.org\r\nSubject: Entwurf\r\n"+
		"Date: Thu, 22 Oct 2026 14:03:05 +0200\r\n\r\n%s\r\n", mid, body))
}

// countMID zählt die Nachrichten mit Message-ID mid im Ordner (eigener Client).
func countMID(t *testing.T, addr, mailbox, mid string) []uint32 {
	t.Helper()
	c, err := imapx.Dial(context.Background(), imapx.Config{Host: "127.0.0.1", Port: portOf(addr), TLS: imapx.TLSNone,
		InsecureTestServer: true, CommandTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("tester@example.invalid", "pw-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select(mailbox, imapx.SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	uids, err := c.SearchHeader("Message-ID", mid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return uids
}

func portOf(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}

// S2.5: Append legt ab und nennt UID und UIDVALIDITY; bricht die
// Verbindung ab, nachdem der Server angenommen hat, entsteht keine
// Dublette (die Kopie wird über die Message-ID wiedergefunden).
func TestAppendNoDuplicateAfterDroppedConnection(t *testing.T) {
	srvAddr, u := memServerFor(t, "INBOX", "Drafts")
	// Eine frühere Kopie desselben Entwurfs liegt schon da (kein Expunge).
	if _, err := u.Append("Drafts", rawLit{bytes.NewReader(draftMail("pk.1@example.org", "alt"))}, &goimap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	px := &dropProxy{target: srvAddr}
	go px.serve(t, ln)
	p, err := Connect(context.Background(), Config{Host: "127.0.0.1", Port: portOf(ln.Addr().String()), TLS: "none",
		Username: "tester@example.invalid", InsecureTestServer: true, CommandTimeout: 2 * time.Second,
		Session: imapx.SessionOptions{MaxAttempts: 3, Backoff: imapx.Backoff{Initial: 10 * time.Millisecond}}}, "pw-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	date := time.Date(2026, 10, 22, 14, 3, 5, 0, time.UTC)
	o := provider.AppendOptions{Flags: provider.Flags{Draft: true, Seen: true}, Date: date, MessageID: "pk.1@example.org"}

	px.armed.Store(true)
	res, err := p.Append(context.Background(), "Drafts", draftMail("pk.1@example.org", "neu"), o)
	if err != nil {
		t.Fatal(err)
	}
	if px.drops.Load() != 1 {
		t.Fatal("the proxy did not drop the connection")
	}
	uids := countMID(t, srvAddr, "Drafts", "pk.1@example.org")
	if len(uids) != 2 || res.UID != provider.UID(uids[1]) || res.UIDValidity == 0 {
		t.Fatalf("result %+v, copies %v: want the old copy and exactly one new one", res, uids)
	}
	// Ohne Abbruch: APPENDUID.
	res2, err := p.Append(context.Background(), "Drafts", draftMail("pk.1@example.org", "neuer"), o)
	if err != nil || res2.UID != res.UID+1 || res2.UIDValidity != res.UIDValidity {
		t.Fatalf("second append = %+v, %v", res2, err)
	}
	// Ordner fehlt: kein Verbindungsfehler, nicht wiederholt, keine
	// Ablehnung der Nachricht (betrifft alle Entwürfe).
	if _, err := p.Append(context.Background(), "Fehlt", draftMail("pk.2@example.org", "x"), o); err == nil ||
		errors.Is(err, provider.ErrConnection) || errors.Is(err, provider.ErrRejected) {
		t.Fatalf("missing mailbox = %v", err)
	}
	// W3: Lehnt der Server das APPEND selbst ab (NO), ist das
	// provider.ErrRejected, ohne den Freitext des Servers; die Verbindung
	// bleibt nutzbar.
	px.refuse.Store(true)
	_, err = p.Append(context.Background(), "Drafts", draftMail("pk.3@example.org", "x"), o)
	if !errors.Is(err, provider.ErrRejected) || !errors.Is(err, imapx.ErrRejected) || errors.Is(err, provider.ErrConnection) ||
		strings.Contains(err.Error(), "Mailbox full") {
		t.Fatalf("rejected append = %v", err)
	}
	if _, err := p.Append(context.Background(), "Drafts", draftMail("pk.4@example.org", "y"), o); err != nil {
		t.Fatalf("append after a rejection = %v", err)
	}
}

type rawLit struct{ *bytes.Reader }

func (l rawLit) Size() int64 { return int64(l.Reader.Len()) }

func TestFlagList(t *testing.T) {
	got := flagList(provider.Flags{Seen: true, Draft: true, Keywords: []string{"$Label"}})
	if fmt.Sprint(got) != `[\Seen \Draft $Label]` {
		t.Fatalf("flags = %v", got)
	}
}
