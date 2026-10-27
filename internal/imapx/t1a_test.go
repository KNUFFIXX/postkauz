package imapx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// T1a: UID-Sicherheit (A6, UID 4294967295) und Session.DoOnce.

// strictServer ist ein Skript-Server: Er protokolliert jedes Kommando,
// lehnt jeden „*“ in UID FETCH und UID STORE ab (A6, Vertragstest „kein *
// in UID-Sets“) und beantwortet SEARCH mit der Zeile aus search.
type strictServer struct {
	addr   string
	exists int
	search string // Antwortzeile auf UID SEARCH (ohne Tag), "" = leer

	// fetch beantwortet UID FETCH selbst (Tests mit großen Köpfen).
	fetch func(w io.Writer, tag string)

	mu   sync.Mutex
	cmds []string
	star int
}

func (s *strictServer) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.cmds)
}

func newStrictServer(t *testing.T, exists int, search string) *strictServer {
	t.Helper()
	s := &strictServer{exists: exists, search: search}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s.addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *strictServer) serve(c net.Conn) {
	defer c.Close()
	_, _ = io.WriteString(c, "* OK [CAPABILITY IMAP4rev1 ESEARCH UIDPLUS] ready\r\n")
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		tag, rest, _ := strings.Cut(line, " ")
		s.mu.Lock()
		if !strings.HasPrefix(rest, "LOGIN") {
			s.cmds = append(s.cmds, rest)
		}
		s.mu.Unlock()
		reply := tag + " OK done\r\n"
		switch {
		case strings.HasPrefix(rest, "LOGIN"):
			reply = tag + " OK logged in\r\n"
		case strings.HasPrefix(rest, "EXAMINE"), strings.HasPrefix(rest, "SELECT"):
			reply = fmt.Sprintf("* %d EXISTS\r\n* OK [UIDVALIDITY 7] x\r\n* OK [UIDNEXT 100] x\r\n%s OK done\r\n", s.exists, tag)
		case strings.HasPrefix(rest, "UID FETCH"), strings.HasPrefix(rest, "UID STORE"):
			if set := strings.Fields(rest)[2]; strings.Contains(set, "*") {
				s.mu.Lock()
				s.star++
				s.mu.Unlock()
				reply = tag + " NO [CANNOT] star in UID set\r\n"
			} else if s.fetch != nil && strings.HasPrefix(rest, "UID FETCH") {
				s.fetch(c, tag)
				continue
			}
		case strings.HasPrefix(rest, "UID SEARCH"):
			reply = ""
			if s.search != "" {
				reply = strings.ReplaceAll(s.search, "TAGX", tag) + "\r\n"
			}
			reply += tag + " OK done\r\n"
		}
		_, _ = io.WriteString(c, reply)
	}
}

func dialStrict(t *testing.T, s *strictServer) *Client {
	t.Helper()
	c := dialLogin(t, s.addr)
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: false}); err != nil {
		t.Fatal(err)
	}
	return c
}

// A6: UID 0 hieße in go-imap „*“ (die neueste Nachricht). Jede Operation
// mit UID 0, UID 4294967295, leerer Liste oder UIDVALIDITY 0 scheitert,
// ohne dass ein Kommando den Server erreicht.
func TestInvalidUIDsNeverReachTheServer(t *testing.T) {
	s := newStrictServer(t, 5, "")
	c := dialStrict(t, s)
	before := len(s.log())
	for _, uids := range [][]uint32{{0}, {3, 0}, {maxUID}, {1, maxUID - 1, maxUID}} {
		if _, err := c.FetchMeta(uids, MetaOptions{Header: true}); !errors.Is(err, ErrInvalidUID) {
			t.Fatalf("FetchMeta(%v) = %v", uids, err)
		}
		if err := c.FetchRaw(uids, func(uint32, io.Reader) error { return nil }); !errors.Is(err, ErrInvalidUID) {
			t.Fatalf("FetchRaw(%v) = %v", uids, err)
		}
		if err := c.StoreFlags(uids, []string{`\Seen`}, true); !errors.Is(err, ErrInvalidUID) {
			t.Fatalf("StoreFlags(%v) = %v", uids, err)
		}
	}
	if _, err := c.Ensure("INBOX", 0, SelectOptions{ReadOnly: true}); !errors.Is(err, ErrInvalidUID) {
		t.Fatalf("Ensure with UIDVALIDITY 0 = %v", err)
	}
	// Leere Listen: nichts zu tun, kein Kommando, Verbindung bleibt.
	if m, err := c.FetchMeta(nil, MetaOptions{}); err != nil || m != nil {
		t.Fatalf("FetchMeta(empty) = %v %v", m, err)
	}
	if err := c.StoreFlags(nil, []string{`\Seen`}, true); err != nil {
		t.Fatalf("StoreFlags(empty) = %v", err)
	}
	if got := s.log()[before:]; len(got) != 0 {
		t.Fatalf("commands reached the server: %q", got)
	}
	if err := c.Noop(); err != nil {
		t.Fatalf("connection lost: %v", err)
	}
	// Gültige Listen gehen ohne „*“ hinaus.
	if _, err := c.FetchMeta([]uint32{1, 2, 3, 9}, MetaOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.StoreFlags([]uint32{4, maxUID - 1}, []string{`\Seen`}, true); err != nil {
		t.Fatal(err)
	}
	if s.star != 0 {
		t.Fatalf("%d commands with * in a UID set", s.star)
	}
	if err := uidSetOnly(); err != nil {
		t.Fatal(err)
	}
}

// uidSetOnly prüft die Grundannahme von A6 an der Bibliothek selbst: Ein
// Set, das uidSet baut, ist nie dynamisch.
func uidSetOnly() error {
	for _, uids := range [][]uint32{{1}, {1, 2, 3}, {7, 5, 6}, {maxUID - 1}} {
		set, err := uidSet(uids)
		if err != nil {
			return err
		}
		if set.Dynamic() || strings.Contains(set.String(), "*") {
			return fmt.Errorf("uidSet(%v) = %s", uids, set)
		}
	}
	return nil
}

// T1a Punkt 2: Ein empfangenes UID-Set wird nie über Nums() aufgelöst.
// „1:4294967295“ (go-imap liefe endlos) und mehr UIDs als EXISTS werden
// abgelehnt, schnell und ohne großen Speicher.
func TestReceivedUIDSetsAreBounded(t *testing.T) {
	cases := []struct {
		name, search string
		exists       int
		want         []uint32
		bad          bool
	}{
		{"esearch full range", `* ESEARCH (TAG "TAGX") UID ALL 1:4294967295`, 5, nil, true},
		{"plain max uid", `* SEARCH 4294967294 4294967295`, 5, nil, true},
		{"more than exists", `* ESEARCH (TAG "TAGX") UID ALL 1:6`, 5, nil, true},
		{"huge below max", `* ESEARCH (TAG "TAGX") UID ALL 1:4294967294`, 50000, nil, true},
		{"esearch ok", `* ESEARCH (TAG "TAGX") UID ALL 2:4,9`, 4, []uint32{2, 3, 4, 9}, false},
		{"plain ok", `* SEARCH 9 2 3`, 3, []uint32{2, 3, 9}, false},
		{"empty", ``, 0, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStrictServer(t, tc.exists, tc.search)
			c := dialStrict(t, s)
			done := make(chan struct{})
			var got []uint32
			var err error
			go func() {
				defer close(done)
				got, err = c.UIDSearch(0, SearchOptions{ESearch: strings.Contains(tc.search, "ESEARCH")})
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("UIDSearch did not return (unbounded UID set)")
			}
			if tc.bad {
				if !errors.Is(err, ErrRejected) {
					t.Fatalf("err = %v, got %d UIDs", err, len(got))
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("got %v %v, want %v", got, err, tc.want)
			}
		})
	}
	// Dasselbe für SEARCH HEADER (Entwürfe wiederfinden).
	s := newStrictServer(t, 3, `* ESEARCH (TAG "TAGX") UID ALL 1:4294967295`)
	c := dialStrict(t, s)
	s.search = `* SEARCH 1 4294967295`
	if _, err := c.SearchHeader("Message-ID", "x@example.org", 0); !errors.Is(err, ErrRejected) {
		t.Fatalf("SearchHeader = %v", err)
	}
}

// T1a Punkt 5: DoOnce führt einen Schreibbefehl nach einem
// Verbindungsfehler nicht erneut aus; Do (lesend) schon.
func TestSessionDoOnceDoesNotRepeat(t *testing.T) {
	addr := fakeServer(t, nil)
	opts := SessionOptions{Sleep: func(context.Context, time.Duration) error { return nil }}
	s := NewSession(cfgFor(t, addr), loginFunc, opts)
	defer s.Close()
	connErr := fmt.Errorf("%w: cut", provider.ErrConnection)
	calls := 0
	err := s.DoOnce(context.Background(), func(*Client) error { calls++; return connErr })
	if !errors.Is(err, provider.ErrConnection) || calls != 1 {
		t.Fatalf("DoOnce: err=%v calls=%d", err, calls)
	}
	calls = 0
	err = s.Do(context.Background(), func(*Client) error {
		calls++
		if calls < 3 {
			return connErr
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("Do: err=%v calls=%d", err, calls)
	}
	// Scheitert schon der Verbindungsaufbau, darf DoOnce ihn wiederholen
	// (f lief noch nicht).
	cfg := cfgFor(t, "127.0.0.1:1")
	s2 := NewSession(cfg, loginFunc, SessionOptions{MaxAttempts: 3, Sleep: opts.Sleep})
	defer s2.Close()
	calls = 0
	if err := s2.DoOnce(context.Background(), func(*Client) error { calls++; return nil }); !errors.Is(err, provider.ErrConnection) ||
		calls != 0 || s2.Dials() != 3 {
		t.Fatalf("DoOnce dial: err=%v calls=%d dials=%d", err, calls, s2.Dials())
	}
}

// fetchHeaders schreibt je Kopfgröße eine FETCH-Antwort mit BODY[HEADER]
// (UID 1, 2, …); die Bytes werden gestreamt, nicht im Speicher gebaut.
func fetchHeaders(sizes ...int64) func(io.Writer, string) {
	return func(w io.Writer, tag string) {
		for i, n := range sizes {
			fmt.Fprintf(w, "* %d FETCH (UID %d RFC822.SIZE %d FLAGS () BODY[HEADER] {%d}\r\n", i+1, i+1, n+10, n)
			head := []byte("Subject: x\r\n")
			if n < int64(len(head)) {
				head = head[:n]
			}
			_, _ = w.Write(head)
			_, _ = io.CopyN(w, zeros{}, n-int64(len(head)))
			_, _ = io.WriteString(w, ")\r\n")
		}
		_, _ = io.WriteString(w, tag+" OK done\r\n")
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// A12.4: Ein Kopf von 50 MB wird nicht gelesen (HeaderTooLarge), die
// übrigen kommen; Köpfe, die nicht mehr ins Budget passen, sind
// HeaderOverBudget.
func TestFetchMetaLimitsHeaders(t *testing.T) {
	s := newStrictServer(t, 3, "")
	s.fetch = fetchHeaders(100, 50<<20, 200)
	c := dialStrict(t, s)
	c.timeout = 30 * time.Second
	meta, err := c.FetchMeta([]uint32{1, 2, 3}, MetaOptions{Header: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 3 || meta[0].HeaderSkipped != "" || len(meta[0].HeaderFields) != 100 ||
		meta[1].HeaderSkipped != HeaderTooLarge || meta[1].HeaderFields != nil || meta[1].Size != 50<<20+10 ||
		meta[2].HeaderSkipped != "" || len(meta[2].HeaderFields) != 200 {
		t.Fatalf("meta = %+v", summary(meta))
	}
	// Budget: 100 Bytes je Kopf, 150 je Aufruf (auf die Kopfgrenze
	// angehoben, wenn kleiner).
	s.fetch = fetchHeaders(80, 80, 80)
	meta, err = c.FetchMeta([]uint32{1, 2, 3}, MetaOptions{Header: true, MaxHeaderBytes: 100, HeaderBudget: 150})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 3 || meta[0].HeaderSkipped != "" || meta[1].HeaderSkipped != HeaderOverBudget || meta[2].HeaderSkipped != HeaderOverBudget {
		t.Fatalf("budget = %+v", summary(meta))
	}
	if err := c.Noop(); err != nil {
		t.Fatalf("connection unusable after skipped literals: %v", err)
	}
}

func summary(ms []MessageMeta) []string {
	var out []string
	for _, m := range ms {
		out = append(out, fmt.Sprintf("uid=%d len=%d skipped=%q size=%d", m.UID, len(m.HeaderFields), m.HeaderSkipped, m.Size))
	}
	return out
}

// A12.4, Kanarientest: Der Server spiegelt nach erfolgreicher Anmeldung
// das Passwort in Fehlerantworten (Response-Code, Freitext, unlesbare
// Antwort). Kein Fehler dieser Schicht enthält es.
func TestServerTextNeverInErrors(t *testing.T) {
	s := newStrictServer(t, 3, "")
	c := dialStrict(t, s)
	answers := []func(io.Writer, string){
		func(w io.Writer, tag string) { fmt.Fprintf(w, "%s NO [%s] wrong %s\r\n", tag, testPass, testPass) },
		func(w io.Writer, tag string) { fmt.Fprintf(w, "%s BAD [ALERT] your password is %s\r\n", tag, testPass) },
		func(w io.Writer, tag string) { fmt.Fprintf(w, "* 1 FETCH (UID %s)\r\n%s OK done\r\n", testPass, tag) },
	}
	for i, a := range answers {
		s.fetch = a
		_, err := c.FetchMeta([]uint32{1}, MetaOptions{})
		if err == nil {
			t.Fatalf("answer %d: no error", i)
		}
		if strings.Contains(err.Error(), testPass) {
			t.Fatalf("answer %d: password in error: %v", i, err)
		}
		if i == 1 && !strings.Contains(err.Error(), "code ALERT") {
			t.Fatalf("known code lost: %v", err)
		}
		if i == 0 && !strings.Contains(err.Error(), "code other") {
			t.Fatalf("unknown code not replaced: %v", err)
		}
		if errors.Is(err, provider.ErrProtocol) || errors.Is(err, provider.ErrConnection) {
			c = dialStrict(t, s) // Verbindung nach unlesbarer Antwort neu
		}
	}
}
