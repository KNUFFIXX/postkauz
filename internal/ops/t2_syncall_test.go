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
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// allRunner spielt je Konto ein Verhalten: "watch" läuft bis ctx endet,
// "busy" meldet zuerst sync_running, dann watch, "auth" scheitert an der
// Anmeldung, "authonce" nur beim ersten Mal (dann watch), "fail" endet mit
// einem anderen Fehler.
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
	case b == "fail":
		return SyncOutput{Account: a.Name}, errors.New("unexpected failure")
	case b == "authonce" && n == 1:
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
// abgelehnter Anmeldung nach 30 min (V10); keines beendet die anderen. Das Audit hat
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
		!strings.Contains(strings.Join(accountLines, "\n"), "; retrying in 30m (") {
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

// T2/V10: Auch wenn alle Konten an der Anmeldung scheitern, endet --all
// nicht von selbst (jedes versucht es nach 30 min erneut); nach dem Ende
// nennt der Bericht je Konto den Fehler, ohne Passwort. --all gilt nur mit
// watch, nicht mit account oder mailboxes.
func TestSyncWatchAllInputAndEnd(t *testing.T) {
	e, _ := newAllEnv(t, map[string]string{"alpha": "auth", "beta": "auth"})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.run", Input: json.RawMessage(`{"watch":true,"all":true}`)})
	if err != nil {
		t.Fatalf("all accounts failed: %v", err)
	}
	for _, a := range res.Output.(SyncOutput).Accounts {
		if !strings.Contains(a.Error, "server said no") || strings.Contains(a.Error, canary) {
			t.Fatalf("account %s: %q", a.Account, a.Error)
		}
	}
	for _, in := range []string{`{"all":true}`, `{"watch":true,"all":true,"account":"alpha"}`, `{"watch":true,"all":true,"mailboxes":["INBOX"]}`} {
		var ie *InputError
		if _, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "sync.run", Input: json.RawMessage(in)}); !errors.As(err, &ie) {
			t.Errorf("%s: want input error, got %v", in, err)
		}
	}
}

// V10 der Nachbesserung T2: Unter --all versucht ein Konto nach einem
// Anmeldefehler (auch NO [UNAVAILABLE]) oder ohne Zugangsdaten nach
// allRetryMax erneut, statt bis zum Neustart zu enden. Solange steht es mit
// Status error am Konto (doctor zeigt es); die erste Runde danach löscht
// den Vermerk.
func TestSyncWatchAllRetriesAfterAuthFailure(t *testing.T) {
	e, r := newAllEnv(t, map[string]string{"alpha": "authonce", "beta": "watch"})
	defer func(f, m time.Duration) { allRetryFirst, allRetryMax = f, m }(allRetryFirst, allRetryMax)
	allRetryFirst, allRetryMax = 10*time.Millisecond, 200*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, err := e.st.AccountByName(ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.sec.Delete(b.CredentialRef); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines []string
	done := make(chan error, 1)
	go func() {
		_, err := e.x.Execute(ctx, Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "sync.run",
			Input: json.RawMessage(`{"watch":true,"all":true}`), Progress: func(p Progress) {
				if p.Event == "account" {
					mu.Lock()
					lines = append(lines, p.Account+": "+p.Message)
					mu.Unlock()
				}
			}})
		done <- err
	}()
	status := func(name string) store.AccountRef {
		refs, err := e.st.AccountRefs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range refs {
			if a.Name == name {
				return a
			}
		}
		t.Fatalf("no account %s", name)
		return store.AccountRef{}
	}
	// beta ohne Passwort: Vermerk, kein Ende.
	deadline := time.Now().Add(5 * time.Second)
	for status("beta").Status != "error" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := status("beta"); st.Status != "error" || !strings.HasPrefix(st.LastError, "sync --watch --all: no credentials (no password stored)") {
		t.Fatalf("beta = %+v", st)
	}
	// alpha: erst abgelehnt, nach allRetryMax angemeldet; danach Status ok.
	select {
	case n := <-r.started:
		if n != "alpha" {
			t.Fatalf("started %s", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("alpha did not retry")
	}
	if st := status("alpha"); st.Status != "ok" || st.LastError != "" {
		t.Fatalf("alpha after retry = %+v", st)
	}
	// beta bekommt sein Passwort zurück und läuft beim nächsten Versuch.
	if err := e.sec.Set(b.CredentialRef, secrets.Secret(canary)); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-r.started:
		if n != "beta" {
			t.Fatalf("started %s", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("beta did not retry")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, "alpha: ") || !strings.Contains(all, "retrying in 200ms (login errors are not retried sooner") ||
		!strings.Contains(all, "beta: no password stored") || strings.Contains(all, canary) || strings.Contains(all, "stopped:") {
		t.Fatalf("lines = %s", all)
	}
	if r.calls["alpha"] != 2 {
		t.Fatalf("calls = %v", r.calls)
	}
}

// Nachprüfung zu fd70f19, Punkte 3 und 4: Ein Fehlervermerk aus einem
// früheren Prozess (z. B. Anmeldefehler vor der Korrektur des Passworts)
// verschwindet mit der ersten erfolgreichen Runde nach dem Neustart; ein
// Konto, das an einem anderen Fehler endet, steht mit „stopped“ am Konto.
// Status anderer Art (hier „new“) bleibt unberührt.
func TestSyncWatchAllStatusAfterRestartAndStop(t *testing.T) {
	e, r := newAllEnv(t, map[string]string{"alpha": "watch", "beta": "fail", "gamma": "watch"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refs := func() map[string]store.AccountRef {
		list, err := e.st.AccountRefs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]store.AccountRef{}
		for _, a := range list {
			out[a.Name] = a
		}
		return out
	}
	alpha, _ := e.st.AccountByName(ctx, "alpha")
	if err := e.st.SetAccountStatus(ctx, alpha.ID, "error", "sync --watch --all: login rejected by the server at 2026-10-28 14:03 UTC; retrying every 30m"); err != nil {
		t.Fatal(err)
	}
	gammaBefore := refs()["gamma"].Status
	done := make(chan error, 1)
	go func() {
		_, err := e.x.Execute(ctx, Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "sync.run", Input: json.RawMessage(`{"watch":true,"all":true}`)})
		done <- err
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
	deadline := time.Now().Add(5 * time.Second)
	for refs()["beta"].Status != "error" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	now := refs()
	if now["alpha"].Status != "ok" || now["alpha"].LastError != "" {
		t.Fatalf("alpha after restart = %+v", now["alpha"])
	}
	if now["beta"].Status != "error" || !strings.HasPrefix(now["beta"].LastError, "sync --watch --all: stopped (unexpected error, see the log) at ") {
		t.Fatalf("beta = %+v", now["beta"])
	}
	if now["gamma"].Status != gammaBefore || now["gamma"].LastError != "" {
		t.Fatalf("gamma = %+v (before %s)", now["gamma"], gammaBefore)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
