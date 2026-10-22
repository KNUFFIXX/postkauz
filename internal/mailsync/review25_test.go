package mailsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Gegenprüfung S2.4/S2.5, W1 (Server im Prozess): Der Server nimmt APPEND
// an, die Verbindung bricht ab, bevor die Bestätigung ankommt, und der
// Wiederaufbau im selben Aufruf gelingt nicht (MaxAttempts 1). Der Lauf
// endet mit einem Verbindungsfehler. Der nächste Lauf darf keine zweite
// Kopie ablegen: Er spiegelt den Entwürfe-Ordner und übernimmt die Kopie
// des abgebrochenen Versuchs.

// appendDropProxy leitet zum Server weiter und kappt die Verbindung einmal,
// sobald der Server ein APPEND bestätigt (APPENDUID), bevor der Client die
// Bestätigung liest.
type appendDropProxy struct {
	target string
	armed  atomic.Bool
	drops  atomic.Int32
}

func (p *appendDropProxy) serve(ln net.Listener) {
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
					if _, werr := c.Write(buf[:n]); werr != nil {
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

func TestReviewW1NoDuplicateAfterInterruptedRun(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	px := &appendDropProxy{target: e.addr}
	go px.serve(ln)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(port)
	e.runner.Connect = func(ctx context.Context, a store.Account, pw secrets.Secret, idle time.Duration) (RemoteCloser, error) {
		prov, err := imapprov.Connect(ctx, imapprov.Config{Host: "127.0.0.1", Port: pn, TLS: "none", Username: testUser,
			InsecureTestServer: true, CommandTimeout: 5 * time.Second,
			Session: imapx.SessionOptions{MaxAttempts: 1, Backoff: imapx.Backoff{Initial: 10 * time.Millisecond}}}, testPass)
		if err != nil {
			return nil, err
		}
		return imapRemote{prov}, nil
	}
	d := e.newDraft(t, "Abbruch")

	px.armed.Store(true)
	if _, err := e.sync(t, ops.SyncOptions{}); !errors.Is(err, provider.ErrConnection) || px.drops.Load() != 1 {
		t.Fatalf("interrupted sync = %v (drops %d)", err, px.drops.Load())
	}
	client := imapSession(t, e.addr)
	if n := len(serverCopies(t, client, "Drafts", d.MessageIDHeader)); n != 1 {
		t.Fatalf("server copies after the interrupted run = %d (the server took the APPEND)", n)
	}
	if got := e.draft(t, d.ID); got.RemoteState != drafts.RemotePending || got.PushAttempt == nil {
		t.Fatalf("draft after the interrupted run = %+v", got)
	}

	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Drafts == nil || out.Drafts.Adopted != 1 || out.Drafts.Pushed != 0 {
		t.Fatalf("drafts = %+v", out.Drafts)
	}
	copies := serverCopies(t, client, "Drafts", d.MessageIDHeader)
	if len(copies) != 1 {
		t.Fatalf("server copies = %v (a second copy was appended)", copies)
	}
	got := e.draft(t, d.ID)
	for uid := range copies {
		if got.RemoteUID != int64(uid) {
			t.Fatalf("recorded uid %d, server uid %d", got.RemoteUID, uid)
		}
	}
	if got.RemoteState != drafts.RemotePushed || got.PushAttempt != nil || got.PushedMessageHash == "" {
		t.Fatalf("draft = %+v", got)
	}
	if st, _ := e.jobState(t); st != "done" {
		t.Fatalf("job = %s", st)
	}
}

// W3: Eine Ablehnung des APPEND (provider.ErrRejected) kommt beim Pusher
// als ops.ErrAppendRejected an, ohne Text des Servers; andere Fehler
// unverändert.
func TestReviewW3RejectionMapsToAppendRejected(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{fmt.Errorf("%w: %w", provider.ErrRejected, fmt.Errorf("%w (NO, code OVERQUOTA)", imapx.ErrRejected)), ops.ErrAppendRejected},
		{fmt.Errorf("%w: test", provider.ErrConnection), provider.ErrConnection},
		{fmt.Errorf("%w: mailbox", provider.ErrNotFound), provider.ErrNotFound},
	}
	for _, c := range cases {
		a := draftAppender{r: fakeAppend{err: c.err}, path: "Drafts"}
		_, err := a.AppendDraft(context.Background(), []byte("x"), "pk.1@example.org", time.Now())
		if !errors.Is(err, c.want) {
			t.Errorf("%v -> %v, want %v", c.err, err, c.want)
		}
		if errors.Is(c.want, ops.ErrAppendRejected) && err.Error() != ops.ErrAppendRejected.Error() {
			t.Errorf("rejection carries server text: %v", err)
		}
	}
	if h := (draftAppender{}).ContentHash([]byte("a\r\nb\r\n")); h != (draftAppender{}).ContentHash([]byte("a\nb\n")) {
		t.Fatal("content hash depends on CR")
	}
}

type fakeAppend struct{ err error }

func (f fakeAppend) Append(context.Context, string, []byte, provider.AppendOptions) (provider.AppendResult, error) {
	return provider.AppendResult{}, f.err
}
