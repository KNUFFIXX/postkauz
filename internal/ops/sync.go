package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// MaxLinkSizeExtra: So viele Bytes darf die lokale Kopie größer sein als
// die des Servers, damit sync.run sie per Message-ID ohne Inhaltsabruf
// verknüpft (Restfall von C2, 25.10.2026; Umsetzung in internal/mailsync).
const MaxLinkSizeExtra = 6

// SyncOptions steuert einen Sync (sync.run).
type SyncOptions struct {
	Full        bool
	Watch       bool
	Window      int
	Interval    time.Duration // nur Watch
	IdleRestart time.Duration // nur Watch
	// FullCheckInterval: Abstand der vollen UID-Abgleiche (nur Watch; 0 = 1 h).
	FullCheckInterval time.Duration
	// JobsInterval: Abstand, in dem der Dauerläufer die Jobs des Kontos
	// prüft (draft_push; nur Watch; 0 = 10 s; A02 E1).
	JobsInterval time.Duration
	Mailboxes    []string
	DryRun       bool
	// AuditID: Start-Eintrag dieses Aufrufs (A-F9). Mit der Kontosperre
	// schließt der Lauf die offenen Starts früherer, hart beendeter Läufe
	// des Kontos ab, nie diesen.
	AuditID int64
}

// SyncMailbox ist das Ergebnis eines Ordners.
type SyncMailbox struct {
	Path     string   `json:"path"`
	Role     string   `json:"role"`
	New      int      `json:"new"`
	Updated  int      `json:"updated"`
	Removed  int      `json:"removed"`
	Errors   int      `json:"errors"`
	Reset    bool     `json:"reset,omitempty"`
	Total    int      `json:"total"`
	Mirrored int      `json:"mirrored"`
	Problems []string `json:"problems,omitempty"`
}

// SyncOutput ist die Ausgabe von sync.run.
type SyncOutput struct {
	Account          string        `json:"account"`
	Mode             string        `json:"mode"` // incremental | full | watch
	Strategy         string        `json:"strategy,omitempty"`
	Mailboxes        []SyncMailbox `json:"mailboxes"`
	New              int           `json:"new"`
	Updated          int           `json:"updated"`
	Removed          int           `json:"removed"`
	Errors           int           `json:"errors"`
	MessagesMirrored int           `json:"messages_mirrored"`
	// AlreadyKnownDownloaded zählt Nachrichten, deren Inhalt geladen wurde,
	// obwohl sie danach per content_hash als schon vorhanden zugeordnet
	// wurden (Größe wich von der lokalen Kopie ab oder Message-ID fehlte);
	// ...Bytes ihre Größe. Fehlt bei 0.
	AlreadyKnownDownloaded      int   `json:"already_known_downloaded,omitempty"`
	AlreadyKnownDownloadedBytes int64 `json:"already_known_downloaded_bytes,omitempty"`
	// LinkedDespiteSizeDifference zählt verschiedene Nachrichten, an die
	// eine Ordnerkopie per Message-ID ohne Inhaltsabruf gehängt wurde,
	// obwohl die lokale Kopie bis zu MaxLinkSizeExtra Bytes größer ist
	// (Thunderbird-Import, nie Entwürfe). Fehlt bei 0.
	LinkedDespiteSizeDifference int `json:"linked_despite_size_difference,omitempty"`
	// IncompleteAnswers zählt leere oder unvollständige Serverantworten
	// (T1a Punkt 3: LIST ohne Ordner bzw. INBOX, UID SEARCH ALL ungleich
	// EXISTS); dabei wurde nichts gelöst oder entfernt. Fehlt bei 0.
	IncompleteAnswers int `json:"incomplete_answers,omitempty"`
	// SkippedTooLarge zählt die Nachrichten, die dieser Lauf nicht geladen
	// hat, weil sie größer als [imap] max_message_bytes sind (MaxMessageBytes,
	// nur mit SkippedTooLarge > 0); sie zählen auch unter Errors (A5.3).
	SkippedTooLarge int   `json:"skipped_too_large,omitempty"`
	MaxMessageBytes int64 `json:"max_message_bytes,omitempty"`
	// AttachmentsExtracted zählt Anhänge, deren Text dieser Lauf
	// extrahiert hat (S1.6).
	AttachmentsExtracted int `json:"attachments_extracted,omitempty"`
	// MessagesIndexed zählt die in den Suchindex aufgenommenen Nachrichten,
	// IndexPending die danach noch wartenden (S1.7).
	MessagesIndexed int   `json:"messages_indexed,omitempty"`
	IndexPending    int   `json:"index_pending,omitempty"`
	Rounds          int   `json:"rounds,omitempty"`
	DurationMS      int64 `json:"duration_ms"`
	DryRun          bool  `json:"dry_run"`
	// Drafts: Ablage der Entwürfe im Entwürfe-Ordner (Job draft_push, S2.5);
	// nil = nichts zu tun.
	Drafts *SyncDrafts `json:"drafts,omitempty"`
	// DraftCheck: Prüfung der Entwürfe nach dem Spiegeln (S2.6): als
	// gesendet erkannt (E5), Freigabe abgelaufen (E7), Kopie im
	// Entwürfe-Ordner verschwunden oder bearbeitet (E6); nil = nichts
	// geändert.
	DraftCheck *drafts.CheckReport `json:"draft_check,omitempty"`
}

// SyncDrafts ist das Ergebnis des Jobs draft_push in einem Sync.
type SyncDrafts struct {
	DraftPushReport
	// Mailbox: der Entwürfe-Ordner (Pfad).
	Mailbox string `json:"mailbox,omitempty"`
	// Error: Grund, aus dem (nicht alle) Entwürfe abgelegt wurden, ohne
	// Mailinhalt; no_drafts_mailbox, wenn es keinen Entwürfe-Ordner gibt.
	Error string `json:"error,omitempty"`
}

// ReasonNoDraftsMailbox: Das Konto hat keinen Ordner mit Rolle drafts und
// [draft] mailbox ist nicht gesetzt bzw. fehlt auf dem Server (A02 E12).
const ReasonNoDraftsMailbox = "no_drafts_mailbox"

// SyncRunner führt den Sync eines Kontos aus (internal/mailsync). Im
// Trockenlauf meldet er nur Ordner und Stände, ohne zu schreiben.
type SyncRunner interface {
	Sync(ctx context.Context, a store.Account, cred provider.Credential, o SyncOptions, progress func(Progress)) (SyncOutput, error)
}

// ErrSyncRunning: Für das Konto läuft schon ein schreibender Sync (anderer
// Prozess, z. B. `sync --watch`). Der Runner prüft das vor dem Verbinden.
var ErrSyncRunning = errors.New("another sync is running")

// ResolveAccount wählt das Konto: den Namen, sonst das einzige Konto.
func ResolveAccount(ctx context.Context, repo AccountRepo, name string) (store.Account, error) {
	if name != "" {
		if err := validAccountName(name); err != nil {
			return store.Account{}, err
		}
		return repo.AccountByName(ctx, name)
	}
	list, err := repo.ListAccounts(ctx)
	if err != nil {
		return store.Account{}, err
	}
	switch len(list) {
	case 0:
		return store.Account{}, inputErr("no account configured yet; add one with: account add")
	case 1:
		return list[0].Account, nil
	}
	return store.Account{}, inputErr("%d accounts configured; choose one with --account", len(list))
}

// redactAccountRef: nur Feldnamen und das Konto als Ziel.
func redactAccountRef(raw json.RawMessage) (json.RawMessage, []string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	params, _ := json.Marshal(redactFields(m))
	var name string
	if v, ok := m["account"]; ok && json.Unmarshal(v, &name) == nil && AccountNameRE.MatchString(name) {
		return params, []string{"account:" + name}
	}
	return params, nil
}

// SyncRunInput ist die Eingabe von sync.run.
type SyncRunInput struct {
	Account            string `json:"account,omitempty"`
	Full               bool   `json:"full,omitempty"`
	Watch              bool   `json:"watch,omitempty"`
	Window             int    `json:"window,omitempty"`
	IntervalSeconds    int    `json:"interval_seconds,omitempty"`
	IdleRestartSeconds int    `json:"idle_restart_seconds,omitempty"`
	// FullCheckIntervalSeconds: Abstand der vollen UID-Abgleiche im
	// Dauerbetrieb (0 = 3600; A02 A1).
	FullCheckIntervalSeconds int `json:"full_check_interval_seconds,omitempty"`
	// JobsIntervalSeconds: Abstand der Prüfung auf Jobs (draft_push) im
	// Dauerbetrieb (0 = 10; A02 E1).
	JobsIntervalSeconds int      `json:"jobs_interval_seconds,omitempty"`
	Mailboxes           []string `json:"mailboxes,omitempty"`
}

// SyncRun spiegelt ein Konto: Erstsync in Fenstern (wiederaufnehmbar),
// danach inkrementell; mit watch als Dauerläufer (IDLE auf INBOX plus
// Intervall). Lokal schreibend (R1, mail:sync). Am Server schreibt er nur
// eines: Zu Beginn jedes Laufs (im Dauerbetrieb zusätzlich alle
// jobs_interval) legt er wartende Entwürfe per APPEND im Entwürfe-Ordner ab
// (Job draft_push, A02 E1; die Entwürfe hat ein Mensch oder Agent über
// drafts.create/drafts.update angelegt, die Freigabe-Regeln greifen dort).
// Er sendet nie, löscht nie, verschiebt nie.
func SyncRun(repo AccountRepo, creds Credentials, runner SyncRunner) Operation {
	return Operation{
		Name:  "sync.run",
		Scope: policy.ScopeMailSync,
		// R2 (T1b, A03 Nachtrag 24.10.): Der Sync legt wartende Entwürfe per
		// APPEND im Entwürfe-Ordner ab, ändert also den Server (reversibel).
		Risk:       policy.R2,
		Idempotent: true,
		Summary:    "Mirror an account: resumable first sync in windows, then incremental; on the server it only puts queued drafts into the Drafts folder, it never sends, deletes or moves.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "full": {"type": "boolean", "description": "also re-check all flags and deletions and fetch anything missing"},
    "watch": {"type": "boolean", "description": "keep running: IDLE on INBOX plus a sync every interval"},
    "window": {"type": "integer", "minimum": 1, "maximum": 5000},
    "interval_seconds": {"type": "integer", "minimum": 1},
    "idle_restart_seconds": {"type": "integer", "minimum": 300, "maximum": 600},
    "full_check_interval_seconds": {"type": "integer", "minimum": 1, "description": "with watch: time between full UID checks of all mailboxes (deletions); default 3600"},
    "jobs_interval_seconds": {"type": "integer", "minimum": 1, "description": "with watch: time between checks for queued jobs (drafts to put into the Drafts folder); default 10"},
    "mailboxes": {"type": "array", "items": {"type": "string"}}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "mode": {"type": "string", "enum": ["incremental", "full", "watch"]},
    "strategy": {"type": "string"},
    "mailboxes": {"type": "array", "items": {"type": "object", "properties": {
      "path": {"type": "string"}, "role": {"type": "string"}, "new": {"type": "integer"},
      "updated": {"type": "integer"}, "removed": {"type": "integer"}, "errors": {"type": "integer"},
      "reset": {"type": "boolean"}, "total": {"type": "integer"}, "mirrored": {"type": "integer"},
      "problems": {"type": "array", "items": {"type": "string"}}},
      "required": ["path", "role", "new", "updated", "removed", "errors", "total", "mirrored"]}},
    "new": {"type": "integer"}, "updated": {"type": "integer"}, "removed": {"type": "integer"},
    "errors": {"type": "integer"}, "messages_mirrored": {"type": "integer"},
    "already_known_downloaded": {"type": "integer", "description": "messages downloaded although they turned out to be known locally (matched by content hash)"},
    "already_known_downloaded_bytes": {"type": "integer"},
    "linked_despite_size_difference": {"type": "integer", "description": "messages linked by Message-ID without download although the local copy is up to ` + strconv.Itoa(MaxLinkSizeExtra) + ` bytes larger"},
    "incomplete_answers": {"type": "integer", "description": "empty or incomplete server answers (mailbox list without INBOX, UID list shorter or longer than EXISTS); nothing was removed"},
    "attachments_extracted": {"type": "integer"},
    "rounds": {"type": "integer"}, "duration_ms": {"type": "integer"}, "dry_run": {"type": "boolean"},
    "drafts": {"type": "object", "properties": {
      "pushed": {"type": "integer"}, "taken_over": {"type": "integer"}, "missing": {"type": "integer"},
      "approval_stale": {"type": "integer"}, "failed": {"type": "integer"}, "rejected": {"type": "integer"}, "adopted": {"type": "integer"}, "key_revoked": {"type": "integer"},
      "draft_ids": {"type": "array", "items": {"type": "integer"}}, "mailbox": {"type": "string"}, "error": {"type": "string"}},
      "required": ["pushed"]}
  },
  "required": ["account", "mode", "mailboxes", "new", "updated", "removed", "errors", "messages_mirrored", "duration_ms", "dry_run"]
}`),
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in SyncRunInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.Window < 0 || in.Window > 5000 {
				return nil, inputErr("window must be between 1 and 5000")
			}
			if in.IdleRestartSeconds != 0 && (in.IdleRestartSeconds < 300 || in.IdleRestartSeconds > 600) {
				return nil, inputErr("idle restart must be between 300 and 600 seconds (5 to 10 minutes)")
			}
			if in.IntervalSeconds < 0 || in.FullCheckIntervalSeconds < 0 || in.JobsIntervalSeconds < 0 {
				return nil, inputErr("interval must be positive")
			}
			if (in.IntervalSeconds != 0 || in.IdleRestartSeconds != 0 || in.FullCheckIntervalSeconds != 0 || in.JobsIntervalSeconds != 0) && !in.Watch {
				return nil, inputErr("interval, idle restart, full check interval and jobs interval only apply with watch")
			}
			a, err := ResolveAccount(ctx, repo, in.Account)
			if err != nil {
				return nil, err
			}
			cred, err := creds.For(a, call.DryRun)
			if err != nil {
				return nil, err
			}
			o := SyncOptions{Full: in.Full, Watch: in.Watch, Window: in.Window, Mailboxes: in.Mailboxes, DryRun: call.DryRun,
				Interval: time.Duration(in.IntervalSeconds) * time.Second, IdleRestart: time.Duration(in.IdleRestartSeconds) * time.Second,
				FullCheckInterval: time.Duration(in.FullCheckIntervalSeconds) * time.Second,
				JobsInterval:      time.Duration(in.JobsIntervalSeconds) * time.Second, AuditID: call.AuditID}
			out, err := runner.Sync(ctx, a, cred, o, call.Progress)
			if err != nil {
				return nil, scrub(err, cred.Password)
			}
			if out.Mailboxes == nil {
				out.Mailboxes = []SyncMailbox{}
			}
			out.DryRun = call.DryRun
			return out, nil
		},
	}
}

// --- mailboxes.list ----------------------------------------------------------

// MailboxRepo liefert die gespiegelten Ordner.
type MailboxRepo interface {
	Mailboxes(ctx context.Context, accountID int64) ([]store.Mailbox, error)
}

// MailboxInfo ist ein Ordner in mailboxes.list.
type MailboxInfo struct {
	ID          int64  `json:"id"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Total       int    `json:"total"`    // laut Server beim letzten Sync
	Mirrored    int    `json:"mirrored"` // lokal gespiegelt
	Unread      int    `json:"unread"`
	SyncPhase   string `json:"sync_phase"`
	UIDValidity uint32 `json:"uidvalidity,omitempty"`
	LastSyncAt  string `json:"last_sync_at,omitempty"`
}

// MailboxesListOutput ist die Ausgabe von mailboxes.list.
type MailboxesListOutput struct {
	Account   string        `json:"account"`
	Mailboxes []MailboxInfo `json:"mailboxes"`
}

// MailboxesList listet die Ordner eines Kontos aus dem lokalen Spiegel
// (ohne Serverkontakt; Stand des letzten Syncs).
func MailboxesList(repo AccountRepo, boxes MailboxRepo) Operation {
	return Operation{
		Name:       "mailboxes.list",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "List the mailboxes of an account with message counts from the last sync.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"}},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "mailboxes": {"type": "array", "items": {"type": "object", "properties": {
      "id": {"type": "integer"}, "path": {"type": "string"}, "name": {"type": "string"}, "role": {"type": "string"},
      "total": {"type": "integer"}, "mirrored": {"type": "integer"}, "unread": {"type": "integer"},
      "sync_phase": {"type": "string", "enum": ["new", "initial", "ready"]}, "uidvalidity": {"type": "integer"},
      "last_sync_at": {"type": "string", "format": "date-time"}},
      "required": ["id", "path", "name", "role", "total", "mirrored", "unread", "sync_phase"]}}
  },
  "required": ["account", "mailboxes"]
}`),
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Account string `json:"account,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			a, err := ResolveAccount(ctx, repo, in.Account)
			if err != nil {
				return nil, err
			}
			list, err := boxes.Mailboxes(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			out := MailboxesListOutput{Account: a.Name, Mailboxes: []MailboxInfo{}}
			for _, m := range list {
				mi := MailboxInfo{ID: m.ID, Path: m.Path, Name: m.Name, Role: m.Role, Total: m.Total, Mirrored: m.Mirrored,
					Unread: m.Unread, SyncPhase: m.Phase, UIDValidity: m.UIDValidity}
				if !m.LastSyncAt.IsZero() {
					mi.LastSyncAt = m.LastSyncAt.UTC().Format(time.RFC3339)
				}
				out.Mailboxes = append(out.Mailboxes, mi)
			}
			return out, nil
		},
	}
}
