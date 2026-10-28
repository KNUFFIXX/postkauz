package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// --- sync.request (T2, MCP mail_sync_now) --------------------------------------
//
// ADR-009 E1, Entscheid 23: Der MCP-Prozess kennt kein Passwort und synct
// nicht selbst. sync.request reiht nur den Job sync des Kontos ein; der
// Prozess mit Zugangsdaten und Kontosperre führt ihn aus: sync --watch
// prüft je --jobs-interval (10 s), ein einmaliger postkauz sync des Kontos
// erledigt ihn mit. Vor jeder Ausführung prüft der Ausführende die Herkunft
// des Jobs (SyncJobs.Denied), wie DraftPusher.originDenied bei Entwürfen.

// SyncRequestNote steht in jedem Ergebnis von sync.request: wann der Job
// läuft.
const SyncRequestNote = "the sync is queued: a running sync --watch of this account runs it within its jobs interval (10 s by default); " +
	"without one, the next postkauz sync of the account does it"

// SyncRequestInput ist die Eingabe von sync.request.
type SyncRequestInput struct {
	Account string `json:"account,omitempty"`
	Mailbox string `json:"mailbox,omitempty"`
}

// SyncRequestOutput ist die Ausgabe von sync.request.
type SyncRequestOutput struct {
	Account string `json:"account"`
	Mailbox string `json:"mailbox,omitempty"`
	// Status: queued (eingereiht oder einem offenen Job hinzugefügt) bzw.
	// im Trockenlauf would_queue.
	Status string `json:"status"`
	// JobID: der offene Job (fehlt im Trockenlauf).
	JobID int64 `json:"job_id,omitempty"`
	// Merged: Ein Job war schon offen; diese Anforderung ist ihm
	// hinzugefügt (Ordner vereinigt, ohne Ordner = alle).
	Merged bool   `json:"merged,omitempty"`
	Note   string `json:"note"`
	DryRun bool   `json:"dry_run"`
}

// syncJobStore ist der Teil des Stores, den Jobs sync brauchen.
type syncJobStore interface {
	EnqueueSyncJob(ctx context.Context, accountID int64, mailboxes []string, now time.Time, origin store.JobOrigin) (int64, bool, error)
	ClaimSyncJob(ctx context.Context, accountID int64, owner string, lease time.Duration, takeOver bool) (store.SyncJob, bool, error)
	CompleteSyncJob(ctx context.Context, j store.SyncJob, owner string) (bool, error)
	ReleaseJob(ctx context.Context, id int64, owner string, runAt time.Time) error
	FailJob(ctx context.Context, id int64, owner, errText string, maxAttempts int, retryAt time.Time) error
	SettleSyncJobs(ctx context.Context, accountID int64, mailboxes []string, since time.Time) (int, error)
	APIKeyByName(ctx context.Context, name string) (store.APIKey, error)
	AuditWriter() audit.Writer
}

// syncJobStoreOf: Die Jobs liegen in derselben Datenbank wie Entwürfe und
// Schlüssel (Deps.Drafts.Store; wie DraftPusher). nil ohne Datenbank
// (Register für Hilfe und Bindungsprüfung, Handler laufen dort nie).
func syncJobStoreOf(d Deps) syncJobStore {
	if d.Drafts == nil || d.Drafts.Store == nil {
		return nil
	}
	return d.Drafts.Store
}

// SyncRequest reiht einen Sync des Kontos ein (R1, mail:sync): lokal
// schreibend (eine Zeile in jobs), ohne Serverkontakt. Trockenlauf reiht
// nichts ein.
func SyncRequest(d Deps) Operation {
	return Operation{
		Name:       "sync.request",
		Scope:      policy.ScopeMailSync,
		Risk:       policy.R1,
		Idempotent: true,
		Summary:    "Ask for a sync of an account now: queues a job that a running sync --watch runs within 10 s (otherwise the next sync); this process never connects to the server.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "account name; without it the only configured account"},
    "mailbox": {"type": "string", "minLength": 1, "maxLength": 512, "description": "only this mailbox (path as in mailboxes.list); without it all mailboxes"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "mailbox": {"type": "string"},
    "status": {"type": "string", "enum": ["queued", "would_queue"]},
    "job_id": {"type": "integer"},
    "merged": {"type": "boolean", "description": "a sync of this account was already queued; this request was added to it"},
    "note": {"type": "string"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "status", "note", "dry_run"]
}`),
		// Im Audit nur Feldnamen und das Konto: Ordnernamen können Namen
		// enthalten.
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in SyncRequestInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.Mailbox != "" && strings.TrimSpace(in.Mailbox) == "" {
				return nil, inputErr("mailbox must not be empty")
			}
			a, err := ResolveAccount(ctx, d.Accounts, in.Account)
			if err != nil {
				return nil, err
			}
			out := SyncRequestOutput{Account: a.Name, Mailbox: in.Mailbox, Status: "would_queue", Note: SyncRequestNote, DryRun: call.DryRun}
			if call.DryRun {
				return out, nil
			}
			st := syncJobStoreOf(d)
			if st == nil {
				return nil, errors.New("sync.request: no job store")
			}
			var boxes []string
			if in.Mailbox != "" {
				boxes = []string{in.Mailbox}
			}
			id, created, err := st.EnqueueSyncJob(ctx, a.ID, boxes, time.Now(), store.JobOrigin{KeyID: call.Key.ID, AuditID: call.AuditID})
			if err != nil {
				return nil, err
			}
			out.Status, out.JobID, out.Merged = "queued", id, !created
			return out, nil
		},
	}
}

// --- Ausführung (Prozess mit Zugangsdaten, internal/mailsync) -------------------

// SyncJobs ist der fachliche Teil des Jobs sync für den Ausführenden:
// übernehmen, Herkunft prüfen, abschließen. Der Lauf selbst ist Sache von
// internal/mailsync.
type SyncJobs struct {
	st    syncJobStore
	owner string
	// Now ist die Uhr (nil = time.Now; Tests).
	Now func() time.Time
}

// NewSyncJobs baut den Ausführungsteil aus den Abhängigkeiten (nil ohne
// Datenbank).
func NewSyncJobs(d Deps) *SyncJobs {
	st := syncJobStoreOf(d)
	if st == nil {
		return nil
	}
	return &SyncJobs{st: st, owner: fmt.Sprintf("sync pid %d at %d", os.Getpid(), time.Now().UnixNano())}
}

func (s *SyncJobs) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// syncJobLease: so lange gilt die Sperre eines laufenden Jobs sync ohne
// Kontosperre des Übernehmenden (mit Sperre übernimmt er sofort).
const syncJobLease = 30 * time.Minute

// Claim übernimmt den fälligen Job sync des Kontos; takeOver: Der Aufrufer
// hält die Kontosperre. ok = false: keiner fällig.
func (s *SyncJobs) Claim(ctx context.Context, accountID int64, takeOver bool) (store.SyncJob, bool, error) {
	return s.st.ClaimSyncJob(ctx, accountID, s.owner, syncJobLease, takeOver)
}

// Denied prüft die Herkunft des Jobs frisch aus api_keys (A8, wie
// DraftPusher.originDenied): Ein Job eines Schlüssels, der inzwischen
// widerrufen oder abgelaufen ist, gelöscht wurde oder den Scope mail:sync
// nicht mehr hat, läuft nicht. Liefert den Grund (key_revoked,
// key_expired, key_invalid, missing_scope …) oder "". Jobs des Menschen
// (cli) und des Systems laufen immer.
func (s *SyncJobs) Denied(ctx context.Context, j store.SyncJob) (string, error) {
	name, ok := strings.CutPrefix(j.Origin.KeyID, "key:")
	if !ok {
		return "", nil
	}
	k, err := s.st.APIKeyByName(ctx, name)
	if errors.Is(err, store.ErrKeyNotFound) {
		return "key_invalid", nil
	}
	if err != nil {
		return "", err
	}
	switch KeyStatus(k, s.now()) {
	case "revoked":
		return "key_revoked", nil
	case "expired":
		return "key_expired", nil
	}
	if dec := policy.Check(PolicyKey(k), policy.Requirement{Name: "sync.request", Scope: policy.ScopeMailSync, Risk: policy.R1}, false); !dec.Allowed() {
		return dec.Reason, nil
	}
	return "", nil
}

// Refuse beendet einen Job, dessen Herkunft nicht mehr gilt, endgültig
// (failed mit Grund, kein Lauf) und hält das als Systemereignis sync.job
// im Audit fest (Konto, Job, Herkunft, Grund; nie Ordnernamen).
func (s *SyncJobs) Refuse(ctx context.Context, a store.Account, j store.SyncJob, reason string) error {
	err := s.st.FailJob(ctx, j.ID, s.owner, "origin_denied: "+reason, 1, s.now())
	params, _ := json.Marshal(map[string]any{"job_id": j.ID, "requested_by": j.Origin.KeyID, "mailboxes": len(j.Mailboxes)})
	aerr := s.st.AuditWriter().Append(ctx, audit.Event{TS: s.now().UTC(), ActorType: audit.ActorSystem, KeyID: "system",
		Scope: string(policy.ScopeMailSync), Operation: "sync.job", ParamsRedacted: params, Targets: []string{"account:" + a.Name},
		Result: audit.ResultDenied, Reason: reason})
	return errors.Join(err, aerr)
}

// Complete schließt einen ausgeführten Job ab (bleibt fällig, wenn
// inzwischen eine neue Anforderung kam).
func (s *SyncJobs) Complete(ctx context.Context, j store.SyncJob) error {
	_, err := s.st.CompleteSyncJob(ctx, j, s.owner)
	if errors.Is(err, store.ErrJobLost) {
		return nil
	}
	return err
}

// Release gibt den Job unerledigt zurück, fällig ab runAt (Verbindung weg,
// Abbruch, Anmeldefehler).
func (s *SyncJobs) Release(ctx context.Context, j store.SyncJob, runAt time.Time) error {
	return s.st.ReleaseJob(context.WithoutCancel(ctx), j.ID, s.owner, runAt)
}

// Fail verbucht einen Fehler des Laufs: Wiederholung nach 1, 2, 4 …
// Minuten (höchstens eine Stunde), nach fünf Fehlern failed.
func (s *SyncJobs) Fail(ctx context.Context, j store.SyncJob, errText string) error {
	backoff := min(time.Minute<<min(j.Attempts, 6), time.Hour)
	return s.st.FailJob(context.WithoutCancel(ctx), j.ID, s.owner, errText, 5, s.now().Add(backoff))
}

// Settle erledigt offene Jobs, die ein fehlerfreier Lauf ab since über
// mailboxes (leer = alle) mit abgedeckt hat. Nur unter der Kontosperre.
func (s *SyncJobs) Settle(ctx context.Context, accountID int64, mailboxes []string, since time.Time) (int, error) {
	return s.st.SettleSyncJobs(ctx, accountID, mailboxes, since)
}
