package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// allRunner spielt je Konto ein Verhalten: "watch" läuft bis ctx endet,
// "busy" meldet zuerst sync_running, dann watch, "auth" scheitert an der
// Anmeldung.
type allRunner struct {
	mu       sync.Mutex
	behavior map[string]string
	calls    map[string]int
	opts     map[string]SyncOptions
	started  chan string
}

func (r *allRunner) Sync(ctx context.Context, a store.Account, cred provider.Credential, o SyncOptions, progress func(Progress)) (SyncOutput, error) {
	r.mu.Lock()
	r.calls[a.Name]++
	n := r.calls[a.Name]
	r.opts[a.Name] = o
	b := r.behavior[a.Name]
	r.mu.Unlock()
	if cred.Password != canary {
		return SyncOutput{}, errors.New("wrong credential")
	}
	switch {
	case b == "auth":
		return SyncOutput{Account: a.Name}, fmt.Errorf("%w: server said no to %s", provider.ErrAuth, cred.Password)
	case b == "busy" && n == 1:
		return SyncOutput{Account: a.Name}, fmt.Errorf("%w: account %s", ErrSyncRunning, a.Name)
	}
	progress(Progress{Event: "round", Round: &SyncOutput{Account: a.Name, New: 1}})
	r.started <- a.Name
	<-ctx.Done()
	return SyncOutput{Account: a.Name, Mode: "watch", Rounds: 1, New: 1, MessagesMirrored: 3, Mailboxes: []SyncMailbox{}}, nil
}

func newAllEnv(t *testing.T, behavior map[string]string) (*accountsEnv, *allRunner) {
	t.Helper()
	e := newAccountsEnv(t)
	r := &allRunner{behavior: behavior, calls: map[string]int{}, opts: map[string]SyncOptions{}, started: make(chan string, 8)}
	if err := e.x.Registry.Register(SyncRun(e.st, Credentials{Secrets: e.sec}, r)); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(behavior))
	for n := range behavior {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		in := demoInput()
		in["name"] = n
		if _, err := e.run("accounts.add", in, canary, false); err != nil {
			t.Fatal(err)
		}
	}
	return e, r
}

// T2: sync --watch --all beobachtet jedes Konto in einer eigenen Goroutine.
// Ein Konto mit belegter Sperre versucht es später erneut, eines mit
// abgelehnter Anmeldung endet; keines beendet die anderen. Das Audit hat
// einen Aufruf mit allen Konten als Ziel, Meldungen tragen das Konto, und
// kein Fehlertext enthält das Passwort.
func TestSyncWatchAllAccounts(t *testing.T) {
	e, r := newAllEnv(t, map[string]string{"alpha": "watch", "beta": "busy", "gamma": "auth"})
	defer func(f time.Duration) { allRetryFirst = f }(allRetryFirst)
	allRetryFirst = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var events []Progress
	type result struct {
		res Result
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := e.x.Execute(ctx, Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "sync.run",
			Input: json.RawMessage(`{"watch":true,"all":true}`), Progress: func(p Progress) {
				mu.Lock()
				events = append(events, p)
				mu.Unlock()
			}})
		done <- result{res, err}
	}()
	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case n := <-r.started:
			got[n] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("watchers started: %v", got)
		}
	}
	cancel()
	res := <-done
	if res.err != nil {
		t.Fatalf("watch --all: %v", res.err)
	}
	out := res.res.Output.(SyncOutput)
	if out.Account != "alpha,beta,gamma" || len(out.Accounts) != 3 || out.Rounds != 2 || out.New != 2 {
		t.Fatalf("out = %+v", out)
	}
	byName := map[string]SyncOutput{}
	for _, x := range out.Accounts {
		byName[x.Account] = x
	}
	if byName["alpha"].Error != "" || byName["beta"].Error != "" || !strings.Contains(byName["gamma"].Error, "server said no") ||
		strings.Contains(byName["gamma"].Error, canary) {
		t.Fatalf("errors per account: %+v", out.Accounts)
	}
	if r.calls["beta"] != 2 || r.calls["gamma"] != 1 || r.opts["alpha"].AuditID != res.res.AuditID || !r.opts["alpha"].Watch {
		t.Fatalf("calls = %v, opts = %+v", r.calls, r.opts)
	}
	mu.Lock()
	var accountLines []string
	for _, p := range events {
		if p.Account == "" {
			t.Fatalf("progress without account: %+v", p)
		}
		if p.Event == "account" {
			accountLines = append(accountLines, p.Account+": "+p.Message)
		}
	}
	mu.Unlock()
	if len(accountLines) != 2 || !strings.Contains(strings.Join(accountLines, "\n"), "beta: another sync is running") ||
		!strings.Contains(strings.Join(accountLines, "\n"), "gamma: stopped:") {
		t.Fatalf("account lines = %q", accountLines)
	}
	var start audit.Event
	for _, ev := range e.au.Events() {
		if ev.Operation == "sync.run" && ev.Result == audit.ResultStarted {
			start = ev
		}
	}
	if !slices.Equal(start.Targets, []string{"account:alpha", "account:beta", "account:gamma"}) {
		t.Fatalf("audit targets = %v", start.Targets)
	}
}

// T2: Enden alle Konten von selbst, kommt der Fehler, der einen Menschen
// braucht (Anmeldung zuerst), damit die Aufgabenplanung ihn am Exit-Code
// erkennt. --all gilt nur mit watch, nicht mit account oder mailboxes.
func TestSyncWatchAllInputAndEnd(t *testing.T) {
	e, _ := newAllEnv(t, map[string]string{"alpha": "auth", "beta": "auth"})
	_, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "sync.run", Input: json.RawMessage(`{"watch":true,"all":true}`)})
	if !errors.Is(err, provider.ErrAuth) || strings.Contains(err.Error(), canary) {
		t.Fatalf("all accounts failed: %v", err)
	}
	for _, in := range []string{`{"all":true}`, `{"watch":true,"all":true,"account":"alpha"}`, `{"watch":true,"all":true,"mailboxes":["INBOX"]}`} {
		var ie *InputError
		if _, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "sync.run", Input: json.RawMessage(in)}); !errors.As(err, &ie) {
			t.Errorf("%s: want input error, got %v", in, err)
		}
	}
}
