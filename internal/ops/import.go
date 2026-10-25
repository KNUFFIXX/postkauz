package ops

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// --- import.run (S1.10) -------------------------------------------------------

// ImportOptions steuert einen Import.
type ImportOptions struct {
	Format     string   // mbox | maildir
	Path       string   // Datei oder Ordner (wie angegeben)
	Mailbox    string   // Zielordner bzw. Präfix ("" = aus den Pfaden)
	LabelsFrom []string // Ordner, deren Nachrichten nur zugeordnet werden
	Rescan     bool
	DryRun     bool
	// NoExtract: Textextraktion der Anhänge nicht bis zum Ende abarbeiten,
	// nur mit dem Zeitbudget wie der Sync (rc6).
	NoExtract bool
}

// ImportSource ist das Ergebnis einer Quelle (Datei bzw. Maildir-Ordner).
type ImportSource struct {
	Path      string   `json:"path"`
	Folder    string   `json:"folder"`
	Mode      string   `json:"mode"` // content | labels
	Variant   string   `json:"variant,omitempty"`
	New       int      `json:"new"`
	Existing  int      `json:"existing"`
	Skipped   int      `json:"skipped"`
	Errors    int      `json:"errors"`
	Linked    int      `json:"linked"`
	Bytes     int64    `json:"bytes"`
	ResumedAt int64    `json:"resumed_at,omitempty"`
	Unchanged bool     `json:"unchanged,omitempty"`
	Problems  []string `json:"problems,omitempty"`
}

// ImportOutput ist die Ausgabe von import.run.
type ImportOutput struct {
	Account string         `json:"account"`
	Format  string         `json:"format"`
	Sources []ImportSource `json:"sources"`
	// New: neu gespeicherte Nachrichten; Existing: schon vorhanden (nur der
	// Ordner kommt dazu); Skipped: gelöscht markiert, tmp/, leer oder (bei
	// Labels) ohne vorhandene Nachricht; Errors: nicht lesbar/zu groß.
	New      int `json:"new"`
	Existing int `json:"existing"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
	// Linked zählt neue Ordnerzuordnungen vorhandener Nachrichten.
	Linked int `json:"linked"`
	// SourcesUnchanged: Quellen, die seit dem letzten Import unverändert
	// sind und nicht gelesen wurden (--rescan liest sie).
	SourcesUnchanged int `json:"sources_unchanged"`
	// FilesIgnored: Dateien, die keine mbox sind (.msf, Filterregeln …).
	FilesIgnored         int   `json:"files_ignored"`
	Bytes                int64 `json:"bytes"`
	AttachmentsExtracted int   `json:"attachments_extracted,omitempty"`
	MessagesIndexed      int   `json:"messages_indexed,omitempty"`
	IndexPending         int   `json:"index_pending,omitempty"`
	// ExtractPending: Anhänge, deren Text danach noch aussteht (Abbruch oder
	// --no-extract; der nächste Sync macht weiter). Bei einem Abbruch kommt
	// der Bericht mit dem Fehler zurück (Executor, Result.Output).
	ExtractPending int   `json:"extract_pending,omitempty"`
	DurationMS     int64 `json:"duration_ms"`
	DryRun         bool  `json:"dry_run"`
}

// ImportRunner führt den Import aus (internal/importer). Im Trockenlauf
// liest und zählt er nur.
type ImportRunner interface {
	Import(ctx context.Context, a store.Account, o ImportOptions, progress func(Progress)) (ImportOutput, error)
}

// ImportRunInput ist die Eingabe von import.run.
type ImportRunInput struct {
	Account    string   `json:"account,omitempty"`
	Format     string   `json:"format"`
	Path       string   `json:"path"`
	Mailbox    string   `json:"mailbox,omitempty"`
	LabelsFrom []string `json:"labels_from,omitempty"`
	Rescan     bool     `json:"rescan,omitempty"`
	NoExtract  bool     `json:"no_extract,omitempty"`
}

// ImportRun liest mbox-Dateien bzw. Maildir-Ordner in ein Konto ein, mit
// Abgleich per Message-ID und content_hash (ADR-003). Lokal schreibend, ohne
// Serverkontakt (R1). Scope admin: Die Operation liest beliebige lokale
// Dateien; sie ist für den Menschen an der CLI, nicht für Agenten (kein
// MCP-Tool).
func ImportRun(repo AccountRepo, runner ImportRunner) Operation {
	return Operation{
		Name:       "import.run",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R1,
		Idempotent: true,
		Summary:    "Import mbox files or maildir folders into an account; messages that exist already only get the folder.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "format": {"type": "string", "enum": ["mbox", "maildir"]},
    "path": {"type": "string", "minLength": 1, "description": "mbox file, folder with mbox files (Thunderbird profile folder) or maildir"},
    "mailbox": {"type": "string", "minLength": 1, "maxLength": 512, "description": "target folder (a file or the maildir root) or prefix of the folders below"},
    "labels_from": {"type": "array", "items": {"type": "string", "minLength": 1},
      "description": "folders whose messages are only linked to existing messages with the same content (Gmail labels); nothing is stored from them; copies with other content are skipped with a note, partial Thunderbird copies are linked"},
    "rescan": {"type": "boolean", "description": "read sources again that did not change since the last import"},
    "no_extract": {"type": "boolean", "description": "do not extract all attachment text now; only the time budget of a sync, the next syncs continue"}
  },
  "required": ["format", "path"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "format": {"type": "string", "enum": ["mbox", "maildir"]},
    "sources": {"type": "array", "items": {"type": "object", "properties": {
      "path": {"type": "string"}, "folder": {"type": "string"}, "mode": {"type": "string", "enum": ["content", "labels"]},
      "variant": {"type": "string"}, "new": {"type": "integer"}, "existing": {"type": "integer"}, "skipped": {"type": "integer"},
      "errors": {"type": "integer"}, "linked": {"type": "integer"}, "bytes": {"type": "integer"},
      "resumed_at": {"type": "integer"}, "unchanged": {"type": "boolean"},
      "problems": {"type": "array", "items": {"type": "string"}}},
      "required": ["path", "folder", "mode", "new", "existing", "skipped", "errors", "linked", "bytes"]}},
    "new": {"type": "integer"}, "existing": {"type": "integer"}, "skipped": {"type": "integer"}, "errors": {"type": "integer"},
    "linked": {"type": "integer"}, "sources_unchanged": {"type": "integer"}, "files_ignored": {"type": "integer"},
    "bytes": {"type": "integer"}, "attachments_extracted": {"type": "integer"}, "messages_indexed": {"type": "integer"},
    "index_pending": {"type": "integer"}, "extract_pending": {"type": "integer"}, "duration_ms": {"type": "integer"}, "dry_run": {"type": "boolean"}
  },
  "required": ["account", "format", "sources", "new", "existing", "skipped", "errors", "linked", "sources_unchanged",
    "files_ignored", "bytes", "duration_ms", "dry_run"]
}`),
		// Im Audit nur Feldnamen und das Konto: Pfade können Namen enthalten.
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in ImportRunInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.Format != "mbox" && in.Format != "maildir" {
				return nil, inputErr("format must be mbox or maildir")
			}
			if strings.TrimSpace(in.Path) == "" {
				return nil, inputErr("path is missing")
			}
			if strings.Trim(in.Mailbox, "/ ") == "" && in.Mailbox != "" {
				return nil, inputErr("mailbox must not be empty")
			}
			a, err := ResolveAccount(ctx, repo, in.Account)
			if err != nil {
				return nil, err
			}
			out, err := runner.Import(ctx, a, ImportOptions{Format: in.Format, Path: in.Path, Mailbox: in.Mailbox,
				LabelsFrom: in.LabelsFrom, Rescan: in.Rescan, DryRun: call.DryRun, NoExtract: in.NoExtract}, call.Progress)
			if out.Sources == nil {
				out.Sources = []ImportSource{}
			}
			out.Account, out.Format, out.DryRun = a.Name, in.Format, call.DryRun
			if err != nil {
				// Abbruch (Strg+C): Der Bericht bis dahin geht mit dem Fehler
				// zurück (die CLI zeigt ihn, Exit bleibt interrupted). Sonst
				// nur der Fehler.
				if ctx.Err() != nil {
					return out, err
				}
				return nil, err
			}
			return out, nil
		},
	}
}
