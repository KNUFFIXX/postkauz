package mailsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
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
	// refuse: Statt zu kappen, macht der Proxy aus der nächsten
	// Bestätigung eine Ablehnung (NO [OVERQUOTA]), wie ein Server mit
	// vollem Kontingent (W3).
	refuse atomic.Bool
}

var appendOK = regexp.MustCompile(`(?m)^(\S+) OK \[APPENDUID[^\r\n]*`)

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

// viaProxy lässt den Runner über einen appendDropProxy verbinden, mit
// höchstens einem Versuch je Kommando (kein Wiederaufbau im selben Aufruf).
func (e *pushEnv) viaProxy(t *testing.T) *appendDropProxy {
	t.Helper()
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
	return px
}

func TestReviewW1NoDuplicateAfterInterruptedRun(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	px := e.viaProxy(t)
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

// W3 (Server im Prozess): Lehnt der Server das APPEND eines Entwurfs ab,
// legt der Sync die übrigen trotzdem ab, der Job ist erledigt. Runden ohne
// RetryDrafts (Dauerbetrieb zwischen den vollen Abgleichen) versuchen ihn
// nicht erneut; postkauz sync schon.
func TestReviewW3RejectedDraftSyncGoesOn(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	px := e.viaProxy(t)
	bad := e.newDraft(t, "Zu groß")
	good := e.newDraft(t, "Passt")
	px.refuse.Store(true)
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil || out.Drafts == nil || out.Drafts.Rejected != 1 || out.Drafts.Pushed != 1 || out.Drafts.Error != "" {
		t.Fatalf("sync = %+v, %v", out.Drafts, err)
	}
	if d := e.draft(t, bad.ID); d.RemoteState != drafts.RemotePending || d.PushNote != drafts.PushNotePushRejected {
		t.Fatalf("rejected draft = %+v", d)
	}
	if d := e.draft(t, good.ID); d.RemoteState != drafts.RemotePushed {
		t.Fatalf("valid draft = %s", d.RemoteState)
	}
	if st, n := e.jobState(t); st != "done" || n != 0 {
		t.Fatalf("job = %s, %d attempts", st, n)
	}
	// Eine Runde ohne RetryDrafts (Dauerbetrieb zwischen den vollen
	// Abgleichen): kein neuer Versuch.
	remote, err := e.runner.Connect(context.Background(), e.account(t), testPass, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	eng := e.engine(t, remote)
	eng.Drafts = &Drafts{Pusher: e.runner.Pusher, Jobs: &jobs.Runner{Store: e.st, TakeOver: true}, Account: e.account(t)}
	if rep, err := eng.SyncAll(context.Background(), Options{}); err != nil || (rep.Drafts != nil && rep.Drafts.Pushed+rep.Drafts.Rejected != 0) {
		t.Fatalf("round without retry = %+v, %v", rep.Drafts, err)
	}
	if d := e.draft(t, bad.ID); d.RemoteState != drafts.RemotePending {
		t.Fatalf("retried in a round without RetryDrafts: %s", d.RemoteState)
	}
	// postkauz sync versucht es erneut; jetzt nimmt der Server an.
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.Drafts == nil || out.Drafts.Pushed != 1 {
		t.Fatalf("sync = %+v, %v", out.Drafts, err)
	}
	if d := e.draft(t, bad.ID); d.RemoteState != drafts.RemotePushed || d.PushNote != "" {
		t.Fatalf("after retry = %+v", d)
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
