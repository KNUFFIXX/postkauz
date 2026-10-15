package imapx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// memServer startet einen In-Prozess-Server und liefert Adresse und Nutzer
// (zum Befüllen und Verändern von außen).
func memServer(t *testing.T) (string, *imapmemserver.User) {
	t.Helper()
	u := imapmemserver.NewUser(testUser, testPass)
	for _, mb := range []string{"INBOX", "Archiv"} {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
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

func testMail(i int) []byte {
	return []byte(fmt.Sprintf("Message-ID: <m%d@example.org>\r\nFrom: a@example.org\r\nTo: b@example.org\r\nSubject: Test %d\r\nDate: Mon, 06 Jan 2025 10:00:00 +0100\r\n\r\nInhalt %d\r\n", i, i, i))
}

func dialLogin(t *testing.T, addr string) *Client {
	t.Helper()
	c, err := Dial(context.Background(), cfgFor(t, addr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAppendSelectSearchFetch(t *testing.T) {
	addr, _ := memServer(t)
	c := dialLogin(t, addr)
	date := time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		var flags []string
		if i%2 == 0 {
			flags = []string{`\Seen`}
		}
		uid, err := c.Append("INBOX", testMail(i), flags, date)
		if err != nil {
			t.Fatal(err)
		}
		if uid != uint32(i) {
			t.Fatalf("APPENDUID = %d, want %d", uid, i)
		}
	}
	st, err := c.Status("INBOX")
	if err != nil || st.Messages != 5 || st.UIDNext != 6 || st.Unseen != 3 {
		t.Fatalf("status = %+v %v", st, err)
	}
	info, err := c.Select("INBOX", SelectOptions{ReadOnly: true})
	if err != nil || info.Messages != 5 || info.UIDNext != 6 || info.UIDValidity == 0 {
		t.Fatalf("select = %+v %v", info, err)
	}
	all, err := c.UIDSearch(0, SearchOptions{ESearch: true})
	if err != nil || !slices.Equal(all, []uint32{1, 2, 3, 4, 5}) {
		t.Fatalf("search all = %v %v", all, err)
	}
	// „UID 9:*" liefert laut RFC die höchste UID mit; sie wird gefiltert.
	if from, err := c.UIDSearch(9, SearchOptions{}); err != nil || len(from) != 0 {
		t.Fatalf("search 9:* = %v %v", from, err)
	}
	if from, err := c.UIDSearch(4, SearchOptions{}); err != nil || !slices.Equal(from, []uint32{4, 5}) {
		t.Fatalf("search 4:* = %v %v", from, err)
	}
	meta, err := c.FetchMeta([]uint32{2, 3, 99}, MetaOptions{Header: true})
	if err != nil || len(meta) != 2 {
		t.Fatalf("meta = %+v %v", meta, err)
	}
	if meta[0].UID != 2 || !slices.Contains(meta[0].Flags, `\Seen`) || meta[0].Size != int64(len(testMail(2))) ||
		!meta[0].InternalDate.Equal(date) || !strings.Contains(string(meta[0].HeaderFields), "<m2@example.org>") {
		t.Fatalf("meta[0] = %+v (%q)", meta[0], meta[0].HeaderFields)
	}
	var flagged []uint32
	if err := c.FetchFlags(2, 4, FlagsOptions{}, func(f FlagsData) { flagged = append(flagged, f.UID) }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(flagged, []uint32{2, 3, 4}) {
		t.Fatalf("flags range = %v", flagged)
	}
	got := map[uint32]string{}
	if err := c.FetchRaw([]uint32{1, 5}, func(uid uint32, r io.Reader) error {
		b, err := io.ReadAll(r)
		got[uid] = string(b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got[1] != string(testMail(1)) || got[5] != string(testMail(5)) {
		t.Fatalf("raw = %v", got)
	}
	// Der Sync liest nur (BODY.PEEK): \Seen bleibt unverändert.
	if st, _ := c.Status("INBOX"); st.Unseen != 3 {
		t.Fatalf("FetchRaw changed \\Seen: unseen = %d", st.Unseen)
	}
	// Ein Fehler des Aufrufers bricht ab und kommt unverändert zurück.
	stop := errors.New("stop")
	if err := c.FetchRaw([]uint32{1, 2}, func(uint32, io.Reader) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("callback error = %v", err)
	}
}

func TestStoreFlagsNeedsReadWriteAndNeverDeleted(t *testing.T) {
	addr, u := memServer(t)
	_, _ = u.Append("INBOX", literal(testMail(1)), &imap.AppendOptions{})
	c := dialLogin(t, addr)
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.StoreFlags([]uint32{1}, []string{`\Seen`}, true); err == nil {
		t.Fatal("StoreFlags on a read-only mailbox must fail")
	}
	if _, err := c.Select("INBOX", SelectOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.StoreFlags([]uint32{1}, []string{`\Deleted`}, true); err == nil {
		t.Fatal(`\Deleted must be refused`)
	}
	if err := c.StoreFlags([]uint32{1}, []string{`\Seen`, `\Flagged`}, true); err != nil {
		t.Fatal(err)
	}
	var fl []string
	_ = c.FetchFlags(1, 0, FlagsOptions{}, func(f FlagsData) { fl = f.Flags })
	if !slices.Contains(fl, `\Seen`) || !slices.Contains(fl, `\Flagged`) {
		t.Fatalf("flags = %v", fl)
	}
}

func TestEnsureDetectsUIDValidityChange(t *testing.T) {
	addr, u := memServer(t)
	c := dialLogin(t, addr)
	info, err := c.Ensure("Archiv", 0, SelectOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ensure("Archiv", info.UIDValidity, SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	_ = u.Delete("Archiv")
	_ = u.Create("Archiv", nil)
	if _, err := c.Ensure("Archiv", info.UIDValidity, SelectOptions{ReadOnly: true}); !errors.Is(err, provider.ErrUIDValidityChanged) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Select("Gibtsnicht", SelectOptions{ReadOnly: true}); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("missing mailbox: %v", err)
	}
}

func literal(b []byte) imap.LiteralReader { return &lit{bytes.NewReader(b)} }

type lit struct{ *bytes.Reader }

func (l *lit) Size() int64 { return int64(l.Reader.Len()) }

// IDLE meldet eine neue Nachricht von außen; ohne Änderung kehrt es nach
// maxWait zurück (Neustart).
func TestIdleReportsChangesAndRestarts(t *testing.T) {
	addr, u := memServer(t)
	c := dialLogin(t, addr)
	start := time.Now()
	changed, err := c.Idle(context.Background(), "INBOX", 150*time.Millisecond)
	if err != nil || changed || time.Since(start) < 150*time.Millisecond {
		t.Fatalf("idle without change: changed=%v err=%v after %s", changed, err, time.Since(start))
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = u.Append("INBOX", literal(testMail(7)), &imap.AppendOptions{})
	}()
	start = time.Now()
	changed, err = c.Idle(context.Background(), "INBOX", 5*time.Second)
	if err != nil || !changed || time.Since(start) > 3*time.Second {
		t.Fatalf("idle with append: changed=%v err=%v after %s", changed, err, time.Since(start))
	}
	// Nach IDLE ist die Verbindung normal benutzbar.
	if all, err := c.UIDSearch(0, SearchOptions{}); err != nil || len(all) != 1 {
		t.Fatalf("after idle: %v %v", all, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if _, err := c.Idle(ctx, "INBOX", 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

// Akzeptanz S1.5 (Blackhole): Verstummt der Server während IDLE, fällt das
// beim fälligen Neustart auf (DONE bleibt unbeantwortet, Watchdog), nicht
// erst nach 28 Minuten wie in go-imap.
func TestIdleDetectsBlackhole(t *testing.T) {
	addr, _ := memServer(t)
	p := newBlackholeProxy(t, addr)
	cfg := cfgFor(t, p.ln.Addr().String())
	cfg.CommandTimeout = 300 * time.Millisecond
	c, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeNow()
	if err := c.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	// Der Server verstummt, nachdem IDLE läuft: Erst DONE beim Neustart
	// bleibt unbeantwortet.
	go func() { time.Sleep(100 * time.Millisecond); p.hole.Store(true) }()
	start := time.Now()
	_, err = c.Idle(context.Background(), "INBOX", 200*time.Millisecond)
	d := time.Since(start)
	if !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("err = %v", err)
	}
	if d < 200*time.Millisecond || d > 3*time.Second {
		t.Fatalf("blackhole detected after %s (want restart 200ms + deadline 300ms)", d)
	}
	// Verstummt er schon vor IDLE, schlägt der Watchdog beim Start zu.
	p2 := newBlackholeProxy(t, addr)
	cfg2 := cfgFor(t, p2.ln.Addr().String())
	cfg2.CommandTimeout = 300 * time.Millisecond
	c3, err := Dial(context.Background(), cfg2)
	if err != nil {
		t.Fatal(err)
	}
	defer c3.closeNow()
	if err := c3.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := c3.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	p2.hole.Store(true)
	if _, err := c3.Idle(context.Background(), "INBOX", time.Minute); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("silent before IDLE: %v", err)
	}
}

// Der Watchdog misst Schweigen, nicht Gesamtdauer: Ein langsamer, aber
// stetiger Datenstrom überschreitet die Deadline insgesamt, ohne abzubrechen.
func TestWatchdogAllowsSlowSteadyResponses(t *testing.T) {
	addr, u := memServer(t)
	big := bytes.Repeat([]byte("x"), 64<<10)
	raw := append([]byte("Message-ID: <big@example.org>\r\nSubject: gross\r\n\r\n"), big...)
	_, _ = u.Append("INBOX", literal(raw), &imap.AppendOptions{})
	// Proxy, der Daten in kleinen Häppchen mit Pausen weiterreicht.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var slow atomic.Bool
	go func() {
		for {
			cc, err := ln.Accept()
			if err != nil {
				return
			}
			b, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(b, cc) }()
			go func() {
				buf := make([]byte, 8<<10)
				for {
					n, err := b.Read(buf)
					if n > 0 {
						if slow.Load() {
							time.Sleep(60 * time.Millisecond)
						}
						if _, werr := cc.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	cfg := cfgFor(t, ln.Addr().String())
	cfg.CommandTimeout = 250 * time.Millisecond
	c, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeNow()
	if err := c.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	slow.Store(true)
	start := time.Now()
	var n int
	err = c.FetchRaw([]uint32{1}, func(_ uint32, r io.Reader) error {
		b, err := io.ReadAll(r)
		n = len(b)
		return err
	})
	if err != nil || n != len(raw) {
		t.Fatalf("slow fetch: n=%d err=%v", n, err)
	}
	if time.Since(start) < cfg.CommandTimeout {
		t.Skip("proxy was not slow enough to exceed the deadline in total")
	}
}
