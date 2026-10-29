package imap

// Nachbesserung T3, Punkt 10: Meldet der Server beim SELECT UIDVALIDITY 0
// (oder gar keins; go-imap übernimmt dann 0), ist das eine unvollständige
// Antwort (provider.ErrIncomplete), nie ein Reset und nie ein Wechsel der
// UIDVALIDITY. Ein Reset löste alle Zuordnungen des Ordners und setzte
// deleted_remote_at (retention.purge löschte nach der Frist); ein Wechsel
// erklärte die Ziele einer Aktion für veraltet; ein APPEND vermerkte die
// Kopie unter UIDVALIDITY 0.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

var uidValidityCode = regexp.MustCompile(`\[UIDVALIDITY [0-9]+\]`)

// zeroUVProxy leitet zum Server weiter und ersetzt, solange armed gilt,
// jedes „[UIDVALIDITY n]“ der Antworten durch „[UIDVALIDITY 0]“.
type zeroUVProxy struct {
	target string
	armed  atomic.Bool
}

func (p *zeroUVProxy) serve(ln net.Listener) {
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
					out := buf[:n]
					if p.armed.Load() && bytes.Contains(out, []byte("[UIDVALIDITY ")) {
						out = uidValidityCode.ReplaceAll(out, []byte("[UIDVALIDITY 0]"))
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

// zeroUVSetup startet einen Server mit einer Nachricht in INBOX und einen
// Proxy davor; connect meldet einen Provider über den Proxy an.
func zeroUVSetup(t *testing.T) (px *zeroUVProxy, srvAddr string, connect func() *Provider) {
	t.Helper()
	srvAddr, u := memServerFor(t, "INBOX", "Drafts")
	if _, err := u.Append("INBOX", rawLit{bytes.NewReader(draftMail("a@example.org", "x"))}, &goimap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	px = &zeroUVProxy{target: srvAddr}
	go px.serve(ln)
	connect = func() *Provider {
		t.Helper()
		p, err := Connect(context.Background(), Config{Host: "127.0.0.1", Port: portOf(ln.Addr().String()), TLS: "none",
			Username: "tester@example.invalid", InsecureTestServer: true, CommandTimeout: 2 * time.Second,
			Session: imapx.SessionOptions{MaxAttempts: 1, Backoff: imapx.Backoff{Initial: 10 * time.Millisecond}}}, "pw-test")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	return px, srvAddr, connect
}

func TestT3NChangesUIDValidityZeroIsIncompleteNotReset(t *testing.T) {
	px, _, connect := zeroUVSetup(t)
	p := connect()
	ctx := context.Background()
	first, err := p.Changes(ctx, "INBOX", provider.SyncState{}, provider.ChangesOptions{})
	if err != nil || first.NewState.UIDValidity == 0 {
		t.Fatalf("first sync: %+v %v", first.NewState, err)
	}
	px.armed.Store(true)
	cs, err := p.Changes(ctx, "INBOX", first.NewState, provider.ChangesOptions{KnownCount: 1})
	if cs.Reset {
		t.Errorf("UIDVALIDITY 0 from the server reported as a reset (would unlink every copy of the mailbox)")
	}
	if !errors.Is(err, provider.ErrIncomplete) {
		t.Errorf("want provider.ErrIncomplete, got %v", err)
	}
	// Auf der gewählten Verbindung (Ordner noch mit UIDVALIDITY 0 gewählt)
	// ist ein UID-Kommando ebenfalls unvollständig, kein Wechsel.
	_, err = p.FetchHeaders(ctx, "INBOX", first.NewState.UIDValidity, []provider.UID{1})
	if !errors.Is(err, provider.ErrIncomplete) || errors.Is(err, provider.ErrUIDValidityChanged) {
		t.Errorf("FetchHeaders on the selected mailbox: want provider.ErrIncomplete, got %v", err)
	}
	// Erstabgleich: Auch ohne gespeicherten Stand nie UIDVALIDITY 0 übernehmen.
	cs, err = connect().Changes(ctx, "INBOX", provider.SyncState{}, provider.ChangesOptions{})
	if !errors.Is(err, provider.ErrIncomplete) || cs.NewState.UIDValidity != 0 || len(cs.Created) > 0 {
		t.Errorf("first sync with UIDVALIDITY 0: %+v %v", cs, err)
	}
}

func TestT3NUIDCommandsUIDValidityZeroIsIncomplete(t *testing.T) {
	px, _, connect := zeroUVSetup(t)
	ctx := context.Background()
	first, err := connect().Changes(ctx, "INBOX", provider.SyncState{}, provider.ChangesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	uv := first.NewState.UIDValidity
	px.armed.Store(true)
	// Frische Verbindung: Ensure wählt neu und sieht UIDVALIDITY 0.
	_, err = connect().FetchHeaders(ctx, "INBOX", uv, []provider.UID{1})
	if !errors.Is(err, provider.ErrIncomplete) || errors.Is(err, provider.ErrUIDValidityChanged) {
		t.Errorf("FetchHeaders: want provider.ErrIncomplete (not a UIDVALIDITY change), got %v", err)
	}
	err = connect().SetFlags(ctx, "INBOX", uv, []provider.UID{1}, provider.FlagPatch{Seen: new(true)})
	if !errors.Is(err, provider.ErrIncomplete) || errors.Is(err, provider.ErrUIDValidityChanged) {
		t.Errorf("SetFlags: want provider.ErrIncomplete (not a UIDVALIDITY change), got %v", err)
	}
}

func TestT3NAppendUIDValidityZeroIsIncomplete(t *testing.T) {
	px, srvAddr, connect := zeroUVSetup(t)
	p := connect()
	px.armed.Store(true)
	for _, mid := range []string{"pk.uv0@example.org", ""} {
		raw := draftMail("pk.uv0@example.org", "y")
		res, err := p.Append(context.Background(), "Drafts", raw, provider.AppendOptions{MessageID: mid})
		if !errors.Is(err, provider.ErrIncomplete) || res.UIDValidity != 0 || res.UID != 0 {
			t.Errorf("append (message id %q) with UIDVALIDITY 0: %+v %v", mid, res, err)
		}
	}
	if got := countMID(t, srvAddr, "Drafts", "pk.uv0@example.org"); len(got) != 0 {
		t.Errorf("append with UIDVALIDITY 0 stored %d copies, want none", len(got))
	}
}
