package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// --- sync --watch --all (T2) ----------------------------------------------------
//
// Ein Prozess beobachtet alle Konten: je Konto eine Goroutine (crash.Go)
// mit eigener Kontosperre und eigenen Zugangsdaten (wie ein einzelner
// sync --watch). Ein Konto, dessen Sperre belegt ist (ein anderer Sync
// läuft) oder dessen Server nicht erreichbar ist, wartet und versucht es
// erneut (1 min, verdoppelt bis 30 min); ein Konto, dessen Anmeldung
// scheitert oder dessen Zugangsdaten fehlen, versucht es nach 30 min
// erneut (V10 der Nachbesserung T2; vorher endete es bis zum nächsten
// Neustart, auch bei einem kurzen NO [UNAVAILABLE] des Servers) und steht
// solange mit Status error in der Datenbank (doctor zeigt es). Keines
// beendet die anderen. Im Audit steht ein einziger Aufruf sync.run mit
// allen Konten als Ziel (redactSyncRun); store.CloseDeadStarts schließt ihn
// deshalb nur ab, wenn keines seiner anderen Konten von einem Prozess
// gesperrt ist.

// Wartezeiten zwischen zwei Versuchen eines Kontos (Sperre belegt,
// Verbindung). In Tests ersetzbar.
var (
	allRetryFirst = time.Minute
	allRetryMax   = 30 * time.Minute
)

// redactSyncRun: wie redactAccountRef; mit all zusätzlich alle Konten als
// Ziele (ein Aufruf, mehrere Konten). Die Konten liest es aus der
// Datenbank; scheitert das, bleibt es bei den Feldnamen.
func redactSyncRun(repo AccountRepo) RedactFunc {
	return func(raw json.RawMessage) (json.RawMessage, []string) {
		params, targets := redactAccountRef(raw)
		var in struct {
			All bool `json:"all"`
		}
		if json.Unmarshal(raw, &in) != nil || !in.All || repo == nil {
			return params, targets
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, err := repo.ListAccounts(ctx)
		if err != nil {
			return params, targets
		}
		for _, a := range list {
			targets = append(targets, "account:"+a.Name)
		}
		return params, targets
	}
}

// watchAllAccounts führt sync.run für alle Konten nebeneinander aus und
// fasst die Ergebnisse zusammen. Endet ctx, kommt das Ergebnis ohne Fehler
// (wie ein einzelner Dauerläufer nach Strg+C). Enden alle Konten von
// selbst, kommt der Fehler zurück, der einen Menschen braucht
// (Anmeldefehler zuerst), damit der Aufrufer (Aufgabenplanung) ihn am
// Exit-Code erkennt.
func watchAllAccounts(ctx context.Context, repo AccountRepo, creds Credentials, runner SyncRunner, call Call, o SyncOptions) (SyncOutput, error) {
	list, err := repo.ListAccounts(ctx)
	if err != nil {
		return SyncOutput{}, err
	}
	if len(list) == 0 {
		return SyncOutput{}, inputErr("no account configured yet; add one with: account add")
	}
	names := make([]string, len(list))
	results := make([]SyncOutput, len(list))
	errs := make([]error, len(list))
	done := make([]<-chan error, len(list))
	for i, la := range list {
		a := la.Account
		names[i] = a.Name
		progress := func(p Progress) {
			p.Account = a.Name
			call.progress(p)
		}
		status := func(msg string) {
			st, le := "ok", ""
			if msg != "" {
				st, le = "error", msg
			}
			if err := repo.SetAccountStatus(context.WithoutCancel(ctx), a.ID, st, le); err != nil {
				progress(Progress{Event: "account", Message: "could not record the account status: " + ErrorKinds(err)})
			}
		}
		done[i] = crash.Go("sync --watch "+a.Name, func() error {
			results[i], errs[i] = watchAccount(ctx, a.Name, func() (provider.Credential, error) { return creds.For(a, o.DryRun) },
				func(cred provider.Credential, p func(Progress)) (SyncOutput, error) {
					return runner.Sync(ctx, a, cred, o, p)
				},
				o.DryRun, progress, status)
			return nil
		})
	}
	for i, d := range done {
		if perr := <-d; perr != nil {
			errs[i] = perr // Panic in der Goroutine eines Kontos
		}
	}
	mode := "watch"
	if !o.Watch {
		mode = "incremental"
	}
	out := SyncOutput{Account: strings.Join(names, ","), Mode: mode, Mailboxes: []SyncMailbox{}, DryRun: o.DryRun}
	var firstErr, authErr error
	for i := range list {
		r := results[i]
		r.Account = names[i]
		if r.Mailboxes == nil {
			r.Mailboxes = []SyncMailbox{}
		}
		if errs[i] != nil {
			r.Error = errs[i].Error()
			if firstErr == nil {
				firstErr = errs[i]
			}
			if authErr == nil && errors.Is(errs[i], provider.ErrAuth) {
				authErr = errs[i]
			}
		}
		out.Rounds += r.Rounds
		out.New += r.New
		out.Updated += r.Updated
		out.Removed += r.Removed
		out.Errors += r.Errors
		out.MessagesMirrored += r.MessagesMirrored
		out.DurationMS = max(out.DurationMS, r.DurationMS)
		out.Accounts = append(out.Accounts, r)
	}
	sort.SliceStable(out.Accounts, func(i, j int) bool { return out.Accounts[i].Account < out.Accounts[j].Account })
	if ctx.Err() != nil || firstErr == nil {
		return out, nil
	}
	// Alle Konten sind von selbst geendet.
	if authErr != nil {
		return out, authErr
	}
	return out, firstErr
}

// watchAccount ist der Dauerläufer eines Kontos unter --all: Er wiederholt
// bei belegter Sperre und Verbindungsfehlern mit wachsender Wartezeit, nach
// einem Anmeldefehler oder ohne Zugangsdaten nach allRetryMax (V10; status
// vermerkt es am Konto, die erste Runde danach löscht den Vermerk), und
// endet bei jedem anderen Fehler und mit ctx. Fehlertexte sind vom Passwort
// bereinigt (scrub); der Vermerk nennt nur die Art des Fehlers.
func watchAccount(ctx context.Context, name string, credFor func() (provider.Credential, error),
	run func(provider.Credential, func(Progress)) (SyncOutput, error), dryRun bool, progress func(Progress), status func(string)) (SyncOutput, error) {
	var total SyncOutput
	wait := allRetryFirst
	var marked atomic.Bool
	runProgress := func(p Progress) {
		if p.Event == "round" && marked.CompareAndSwap(true, false) {
			status("")
		}
		progress(p)
	}
	for {
		var thisWait time.Duration
		var authErr error // Grund der langen Wartezeit (Bericht bei Ende während ihr)
		cred, err := credFor()
		if err != nil {
			if dryRun {
				progress(Progress{Event: "account", Message: "stopped: " + err.Error() + "; the other accounts go on"})
				return total, err
			}
			thisWait, authErr = allRetryMax, err
			marked.Store(true)
			status(fmt.Sprintf("sync --watch --all: no credentials (%s) at %s; retrying every %s", credKind(err),
				time.Now().UTC().Format("2006-01-02 15:04 UTC"), shortDur(allRetryMax)))
			progress(Progress{Event: "account", Message: fmt.Sprintf("%s; retrying in %s (the other accounts go on)", err.Error(), shortDur(thisWait))})
		} else {
			var out SyncOutput
			out, err = run(cred, runProgress)
			addAccountRun(&total, out)
			if ctx.Err() != nil {
				return total, nil
			}
			if err == nil {
				return total, nil
			}
			err = scrub(err, cred.Password)
			switch {
			case dryRun:
				progress(Progress{Event: "account", Message: "stopped: " + err.Error() + "; the other accounts go on"})
				return total, err
			case errors.Is(err, ErrSyncRunning) || errors.Is(err, provider.ErrConnection):
				thisWait = wait
				wait = min(2*wait, allRetryMax)
				progress(Progress{Event: "account", Message: fmt.Sprintf("%s; trying again in %s", err.Error(), shortDur(thisWait))})
			case authFailure(err):
				thisWait, authErr = allRetryMax, err
				marked.Store(true)
				status(fmt.Sprintf("sync --watch --all: %s at %s; retrying every %s", credKind(err),
					time.Now().UTC().Format("2006-01-02 15:04 UTC"), shortDur(allRetryMax)))
				progress(Progress{Event: "account", Message: fmt.Sprintf("%s; retrying in %s (login errors are not retried sooner, to avoid locking the account; the other accounts go on)",
					err.Error(), shortDur(thisWait))})
			default:
				progress(Progress{Event: "account", Message: "stopped: " + err.Error() + "; the other accounts go on"})
				return total, err
			}
		}
		t := time.NewTimer(thisWait)
		select {
		case <-ctx.Done():
			t.Stop()
			return total, authErr // nil, außer nach einem Anmeldefehler: der Bericht nennt ihn
		case <-t.C:
		}
	}
}

// authFailure: Die Anmeldung wurde abgelehnt (auch NO [UNAVAILABLE] auf
// LOGIN, imapx macht jedes NO/BAD daraus) oder ein OAuth2-Token ließ sich
// nicht erneuern.
func authFailure(err error) bool {
	return errors.Is(err, provider.ErrAuth) || errors.Is(err, oauth.ErrRefresh) || errors.Is(err, oauth.ErrTokenCorrupt)
}

// credKind ist die Art eines Anmelde- oder Zugangsdatenfehlers für den
// Vermerk am Konto (nie Server- oder Fehlertext).
func credKind(err error) string {
	switch {
	case errors.Is(err, provider.ErrAuth):
		return "login rejected by the server"
	case errors.Is(err, oauth.ErrRefresh), errors.Is(err, oauth.ErrTokenCorrupt):
		return "OAuth2 token could not be renewed"
	case errors.Is(err, ErrNoToken):
		return "no OAuth2 token stored"
	case errors.Is(err, ErrNoSecret):
		return "no password stored"
	}
	return "credentials not readable"
}

// shortDur schreibt 30m statt 30m0s.
func shortDur(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	return s
}

// addAccountRun zählt einen Versuch eines Kontos zum Ergebnis des Kontos:
// Zähler summiert, sonst gilt der letzte Versuch (ein Versuch, der an der
// Sperre scheiterte, löscht den Stand der gespiegelten Nachrichten nicht).
func addAccountRun(total *SyncOutput, out SyncOutput) {
	prev := *total
	*total = out
	total.Rounds += prev.Rounds
	total.New += prev.New
	total.Updated += prev.Updated
	total.Removed += prev.Removed
	total.Errors += prev.Errors
	if total.MessagesMirrored == 0 {
		total.MessagesMirrored = prev.MessagesMirrored
	}
	if total.Strategy == "" {
		total.Strategy = prev.Strategy
	}
}
