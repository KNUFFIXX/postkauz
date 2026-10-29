package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/export"
	"github.com/KNUFFIXX/postkauz/internal/magic"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Anhänge sichern und Export (S3.3, A03 E6): attachments.save legt einen
// Anhang unter [paths] export_dir ab (R1, mail:export, auch für Agenten);
// messages.export schreibt das Archiv als mboxrd oder EML-Verzeichnis (nur
// für Menschen). Die Fachlogik liegt in internal/export.

// ExportRepo liefert Ordner und Nachrichten für messages.export.
type ExportRepo interface {
	ExportFolders(ctx context.Context, accountID int64, f store.ExportFilter) ([]store.ExportFolder, error)
	ExportMessages(ctx context.Context, accountID int64, folder store.ExportFolder, f store.ExportFilter) ([]store.ExportItem, error)
}

// Ordner im Export für Nachrichten ohne Serverkopie (Festlegung S3.3):
// importierte unter "_imported/<Import-Ordner>", solche ganz ohne Ordner
// (auf dem Server gelöscht, nie importiert) in "_no_folder".
const (
	ExportImportedFolder = "_imported"
	ExportNoFolderName   = "_no_folder"
)

// AttachmentSaveOutput ist die Ausgabe von attachments.save. Path und
// Filename stammen (bereinigt) aus der Mail: Untrusted-Hülle.
type AttachmentSaveOutput struct {
	Envelope
	ID        int64 `json:"id"`
	MessageID int64 `json:"message_id"`
	// Path ist der Pfad relativ zu export_dir ("/"-getrennt).
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	// Blocked: ausführbarer Typ, abgelegt mit der Endung ".blocked".
	Blocked bool `json:"blocked"`
	// Truncated: Der Anhang war schon beim Spiegeln unvollständig.
	Truncated bool `json:"truncated"`
	DryRun    bool `json:"dry_run"`
}

// extForType: Endung für Anhänge ohne Namen (nur eindeutige Typen; sonst
// ".bin").
var extForType = map[string]string{
	magic.PDF: ".pdf", "image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", magic.ZIP: ".zip",
	magic.DOCX: ".docx", magic.XLSX: ".xlsx", magic.PPTX: ".pptx", magic.TextPlain: ".txt", magic.TextHTML: ".html",
	magic.RFC822: ".eml", "text/calendar": ".ics",
}

// AttachmentsSave legt einen Anhang als Datei unter [paths] export_dir ab
// (optional in einem Unterordner): Name bereinigt, ausführbare Typen mit
// ".blocked", nie überschreibend (Zähler), Datei 0600. Adressierung wie
// attachments.text (id, optional message und account). R1, mail:export.
func AttachmentsSave(d Deps) Operation {
	return Operation{
		Name:    "attachments.save",
		Scope:   policy.ScopeMailExport,
		Risk:    policy.R1,
		Summary: "Save an attachment as a file below export_dir (name cleaned, executables get .blocked, never overwrites).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "integer", "minimum": 1, "description": "attachment id from attachments.list"},
    "message": {"type": "integer", "minimum": 1, "description": "only if the attachment belongs to this message (else not found)"},
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "only if the attachment belongs to this account (else not found)"},
    "subdir": {"type": "string", "maxLength": 512, "description": "folder below export_dir, e.g. invoices/2026 (no .., no absolute path)"}
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + envelopeProps + `,
    "id": {"type": "integer"}, "message_id": {"type": "integer"},
    "path": {"type": "string", "description": "relative to export_dir"},
    "filename": {"type": "string"}, "size": {"type": "integer"}, "sha256": {"type": "string"},
    "blocked": {"type": "boolean"}, "truncated": {"type": "boolean"}, "dry_run": {"type": "boolean"}
  },
  "required": ["untrusted", "source", "notice", "id", "message_id", "path", "filename", "size", "sha256", "blocked", "truncated", "dry_run"]
}`),
		// Ins Audit nur Feldnamen und die Anhang-ID (der Unterordner ist
		// freier Text des Aufrufers).
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				ID int64 `json:"id"`
			}
			params, _ := redactAccountRef(raw)
			if json.Unmarshal(raw, &in) == nil && in.ID > 0 {
				return params, []string{"attachment:" + itoa(in.ID)}
			}
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ID      int64  `json:"id"`
				Message int64  `json:"message,omitempty"`
				Account string `json:"account,omitempty"`
				Subdir  string `json:"subdir,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if d.ExportDir == "" {
				return nil, fmt.Errorf("ops: attachments.save: export_dir is not configured")
			}
			a, _, err := d.Messages.AttachmentByID(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if in.Message > 0 && a.MessageID != in.Message {
				return nil, fmt.Errorf("%w: id %d in message %d", store.ErrAttachmentNotFound, in.ID, in.Message)
			}
			if in.Account != "" {
				acc, err := ResolveAccount(ctx, d.Accounts, in.Account)
				if err != nil {
					return nil, err
				}
				if a.AccountID != acc.ID {
					return nil, fmt.Errorf("%w: id %d in account %s", store.ErrAttachmentNotFound, in.ID, acc.Name)
				}
			}
			if a.SHA256 == "" {
				return nil, deniedErr("attachment_not_stored", "attachment %d has no stored content (skipped when the message was mirrored)", in.ID)
			}
			ext := extForType[a.ContentType]
			if ext == "" {
				ext = ".bin"
			}
			res, err := export.SaveAttachment(export.SaveRequest{ExportDir: d.ExportDir, Subdir: in.Subdir, Filename: a.Filename,
				Fallback: "attachment-" + itoa(a.ID) + ext, DeclaredType: a.DeclaredType, Executable: a.Executable, SHA256: a.SHA256,
				Open: func() (io.ReadCloser, error) { return d.Blobs.Get(a.SHA256) }, DryRun: call.DryRun, Log: d.Log})
			switch {
			case errors.Is(err, export.ErrOutside):
				return nil, deniedErr("outside_export_dir", "subdir must stay below export_dir: no .., no absolute path, no symbolic link")
			case errors.Is(err, export.ErrInvalidSubdir):
				return nil, deniedErr("invalid_subdir", "each part of subdir must be a plain file name (no reserved names, no : or control characters, at most 8 levels)")
			case err != nil:
				return nil, err
			}
			return AttachmentSaveOutput{Envelope: emailEnvelope(), ID: a.ID, MessageID: a.MessageID, Path: res.Path, Filename: res.Name,
				Size: res.Size, SHA256: res.SHA256, Blocked: res.Blocked, Truncated: a.Truncated, DryRun: call.DryRun}, nil
		},
	}
}

// MessagesExportInput ist die Eingabe von messages.export.
type MessagesExportInput struct {
	Format  string `json:"format"`
	To      string `json:"to"`
	Account string `json:"account,omitempty"`
	Mailbox string `json:"mailbox,omitempty"`
	Since   string `json:"since,omitempty"`
}

// ExportFolderOutput ist ein Ordner in messages.export.
type ExportFolderOutput struct {
	Folder   string `json:"folder"`
	Path     string `json:"path"`
	Messages int    `json:"messages"`
	Skipped  int    `json:"skipped"`
	Bytes    int64  `json:"bytes"`
}

// ExportSkipOutput ist eine nicht exportierte Nachricht.
type ExportSkipOutput struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// MessagesExportOutput ist die Ausgabe von messages.export (ohne Inhalt:
// Ordnernamen, Pfade, Zähler, IDs).
type MessagesExportOutput struct {
	Format     string               `json:"format"`
	Location   string               `json:"location"`
	Account    string               `json:"account"`
	Folders    []ExportFolderOutput `json:"folders"`
	Messages   int                  `json:"messages"`
	Skipped    int                  `json:"skipped"`
	SkippedBy  map[string]int       `json:"skipped_by"`
	SkippedIDs []ExportSkipOutput   `json:"skipped_ids"`
	Bytes      int64                `json:"bytes"`
	// NewlineAdded: Nachrichten ohne Zeilenende am Schluss, die in der mbox
	// eines bekommen haben (ihr content_hash weicht danach ab).
	NewlineAdded int  `json:"newline_added"`
	DryRun       bool `json:"dry_run"`
}

// MessagesExport schreibt das Archiv eines Kontos in einen neuen oder
// leeren Ordner: mboxrd (eine Datei je Ordner) oder EML-Verzeichnis (je
// Ordner ein Unterordner, Dateien Byte für Byte wie gespeichert, dazu
// index.csv ohne Betreff). Nur für Menschen (HumanOnly, admin, R1): Ein
// Agent darf einzelne Anhänge ablegen, nie das Archiv kopieren (E6).
func MessagesExport(d Deps) Operation {
	return Operation{
		Name:      "messages.export",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary:   "Export the archive of an account as mboxrd (one file per folder) or as a folder of .eml files with index.csv.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "format": {"type": "string", "enum": ["mbox", "eml"]},
    "to": {"type": "string", "minLength": 1, "description": "new or empty target folder (relative: below export_dir)"},
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "account (default: the only one)"},
    "mailbox": {"type": "string", "minLength": 1, "description": "only this server mailbox (path)"},
    "since": {"type": "string", "description": "only messages since (30d, 2026-01-01, RFC 3339)"}
  },
  "required": ["format", "to"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "format": {"type": "string"}, "location": {"type": "string"}, "account": {"type": "string"},
    "folders": {"type": "array", "items": {"type": "object", "properties": {
      "folder": {"type": "string"}, "path": {"type": "string"}, "messages": {"type": "integer"},
      "skipped": {"type": "integer"}, "bytes": {"type": "integer"}},
      "required": ["folder", "path", "messages", "skipped", "bytes"]}},
    "messages": {"type": "integer"}, "skipped": {"type": "integer"},
    "skipped_by": {"type": "object", "additionalProperties": {"type": "integer"}},
    "skipped_ids": {"type": "array", "items": {"type": "object", "properties": {
      "id": {"type": "integer"}, "reason": {"type": "string", "enum": ["no_blob", "blob_missing", "blob_damaged"]}},
      "required": ["id", "reason"]}},
    "bytes": {"type": "integer"}, "newline_added": {"type": "integer"}, "dry_run": {"type": "boolean"}
  },
  "required": ["format", "location", "account", "folders", "messages", "skipped", "skipped_by", "skipped_ids", "bytes", "newline_added", "dry_run"]
}`),
		// Ins Audit nur Feldnamen und das Konto (Ziel und Ordner können
		// den Nutzernamen bzw. Ordnernamen enthalten).
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in MessagesExportInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			acc, err := ResolveAccount(ctx, d.Accounts, in.Account)
			if err != nil {
				return nil, err
			}
			since, err := ParseSince(in.Since, time.Now())
			if err != nil {
				return nil, err
			}
			dir := in.To
			if !filepath.IsAbs(dir) {
				// Relativ heißt unter export_dir (die CLI macht Pfade vorher
				// absolut, wie der Nutzer sie meint).
				if d.ExportDir == "" {
					return nil, inputErr("give an absolute target folder (export_dir is not configured)")
				}
				dir = filepath.Join(d.ExportDir, dir)
			}
			dir = filepath.Clean(dir)
			filter := store.ExportFilter{Mailbox: in.Mailbox, Since: since}
			sf, err := d.Export.ExportFolders(ctx, acc.ID, filter)
			if err != nil {
				return nil, err
			}
			folders := make([]export.Folder, 0, len(sf))
			for _, f := range sf {
				ef := export.Folder{Name: f.Path, Segments: f.Segments, Count: f.Messages, NoBlob: f.NoBlob, Key: f}
				switch f.Kind {
				case store.ExportImported:
					ef.Name = ExportImportedFolder + "/" + f.Path
					ef.Segments = append([]string{ExportImportedFolder}, f.Segments...)
				case store.ExportNoFolder:
					ef.Name, ef.Segments = ExportNoFolderName, []string{ExportNoFolderName}
				}
				folders = append(folders, ef)
			}
			rep, err := export.Run(ctx, export.Options{Format: in.Format, Dir: dir, DryRun: call.DryRun, Folders: folders,
				Items: func(ctx context.Context, f export.Folder) ([]export.Item, error) {
					list, err := d.Export.ExportMessages(ctx, acc.ID, f.Key.(store.ExportFolder), filter)
					if err != nil {
						return nil, err
					}
					out := make([]export.Item, len(list))
					for i, m := range list {
						out[i] = export.Item{ID: m.ID, MessageID: m.MessageID, From: m.From, BlobSHA256: m.BlobSHA256,
							Date: firstTime(m.DateHeader, m.InternalDate), Received: firstTime(m.InternalDate, m.DateHeader)}
					}
					return out, nil
				},
				Open:       d.Blobs.Get,
				IsNotFound: func(err error) bool { return errors.Is(err, blob.ErrNotFound) },
				Log:        d.Log,
				Progress: func(e export.Event) {
					call.progress(Progress{Event: "export_folder", Mailbox: e.Folder, Done: e.Done, Total: e.Total})
				}})
			if errors.Is(err, export.ErrTargetNotFresh) {
				return nil, deniedErr("target_not_fresh", "%s", strings.TrimPrefix(err.Error(), export.ErrTargetNotFresh.Error()+": "))
			}
			out := MessagesExportOutput{Format: in.Format, Location: dir, Account: acc.Name, Folders: []ExportFolderOutput{},
				Messages: rep.Messages, Skipped: rep.Skipped, SkippedBy: rep.SkippedBy, SkippedIDs: []ExportSkipOutput{},
				Bytes: rep.Bytes, NewlineAdded: rep.NewlineAdded, DryRun: call.DryRun}
			if out.SkippedBy == nil {
				out.SkippedBy = map[string]int{}
			}
			for _, f := range rep.Folders {
				out.Folders = append(out.Folders, ExportFolderOutput{Folder: f.Folder, Path: f.Path, Messages: f.Messages, Skipped: f.Skipped, Bytes: f.Bytes})
			}
			for _, s := range rep.SkippedList {
				out.SkippedIDs = append(out.SkippedIDs, ExportSkipOutput{ID: s.ID, Reason: s.Reason})
			}
			return out, err
		},
		OutputOnError: true,
	}
}

// firstTime liefert die erste Zeit, die nicht null ist.
func firstTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}
