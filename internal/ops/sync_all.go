package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// --- sync --watch --all (T2) ----------------------------------------------------
//
// Ein Prozess beobachtet alle Konten: je Konto eine Goroutine (crash.Go)
// mit eigener Kontosperre und eigenen Zugangsdaten (wie ein einzelner
// sync --watch). Ein Konto, dessen Sperre belegt ist (ein anderer Sync
// läuft) oder dessen Server nicht erreichbar ist, wartet und versucht es
// erneut (1 min, verdoppelt bis 30 min); ein Konto, dessen Anmeldung
// scheitert oder dessen Zugangsdaten fehlen, endet. Keines beendet die
// anderen. Im Audit steht ein einziger Aufruf sync.run mit allen Konten als
// Ziel (redactSyncRun); store.CloseDeadStarts schließt ihn deshalb nur ab,
// wenn keines seiner anderen Konten von einem Prozess gesperrt ist.

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
		done[i] = crash.Go("sync --watch "+a.Name, func() error {
			results[i], errs[i] = watchAccount(ctx, a.Name, func() (provider.Credential, error) { return creds.For(a, o.DryRun) },
				func(cred provider.Credential) (SyncOutput, error) { return runner.Sync(ctx, a, cred, o, progress) }, o.DryRun, progress)
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
// bei belegter Sperre und Verbindungsfehlern mit wachsender Wartezeit und
// endet bei jedem anderen Fehler (Anmeldung, Zugangsdaten) und mit ctx.
// Fehlertexte sind vom Passwort bereinigt (scrub).
func watchAccount(ctx context.Context, name string, credFor func() (provider.Credential, error),
	run func(provider.Credential) (SyncOutput, error), dryRun bool, progress func(Progress)) (SyncOutput, error) {
	var total SyncOutput
	wait := allRetryFirst
	for {
		cred, err := credFor()
		if err != nil {
			progress(Progress{Event: "account", Message: "stopped: " + err.Error() + "; the other accounts go on"})
			return total, err
		}
		out, err := run(cred)
		addAccountRun(&total, out)
		if ctx.Err() != nil {
			return total, nil
		}
		if err == nil {
			return total, nil
		}
		err = scrub(err, cred.Password)
		retry := !dryRun && (errors.Is(err, ErrSyncRunning) || errors.Is(err, provider.ErrConnection))
		if !retry {
			progress(Progress{Event: "account", Message: "stopped: " + err.Error() + "; the other accounts go on"})
			return total, err
		}
		progress(Progress{Event: "account", Message: fmt.Sprintf("%s; trying again in %s", err.Error(), wait)})
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return total, nil
		case <-t.C:
		}
		wait = min(2*wait, allRetryMax)
	}
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
