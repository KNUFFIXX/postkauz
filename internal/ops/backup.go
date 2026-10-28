package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// Sicherung (S3.4, E7): backup.create ist HumanOnly (admin, R1). Ein Agent
// kopiert nie das ganze Archiv; die Wiederherstellung ist ein
// CLI-Kommando vor dem Öffnen einer Datenbank (restore, wie doctor) und
// keine Operation des Registers.

// BackupRunner sichert das Datenverzeichnis (internal/backup.Runner; in
// Tests ersetzbar).
type BackupRunner interface {
	Create(ctx context.Context, o backup.Options) (backup.Result, error)
}

// BackupCreateInput ist die Eingabe von backup.create: genau eines von to
// (Ordner) und tar ("-" = Tar-Strom auf die Standardausgabe).
type BackupCreateInput struct {
	To  string `json:"to,omitempty"`
	Tar string `json:"tar,omitempty"`
}

// BackupCreate sichert Datenbank (Schnappschuss), Blobs und
// Audit-Archive in einen Ordner oder als Tar-Strom (HumanOnly, admin, R1).
// stdout ist das Ziel des Tar-Stroms (CLI: die Standardausgabe).
func BackupCreate(r BackupRunner, stdout io.Writer) Operation {
	return Operation{
		Name:      "backup.create",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary: "Back up the data directory (database snapshot, blobs, audit archives) with a SHA-256 manifest " +
			"into a folder (only missing blobs are copied) or as a tar stream; the backup is not encrypted.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "to": {"type": "string", "minLength": 1, "description": "backup folder: new, empty or holding an earlier backup of this program; never inside the data directory"},
    "tar": {"type": "string", "enum": ["-"], "description": "\"-\": write a tar stream to standard output instead of a folder (for age or restic)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "mode": {"type": "string", "enum": ["folder", "tar"]},
    "target": {"type": "string"},
    "manifest": {"type": "string"},
    "manifest_sha256": {"type": "string"},
    "database": {"type": "string"},
    "schema_version": {"type": "integer"},
    "program_version": {"type": "string"},
    "created_at": {"type": "string"},
    "files": {"type": "integer"},
    "bytes": {"type": "integer"},
    "blobs": {"type": "integer"},
    "blobs_copied": {"type": "integer"},
    "blobs_existing": {"type": "integer"},
    "bytes_copied": {"type": "integer"},
    "audit_archives": {"type": "integer"},
    "snapshots_removed": {"type": "integer"},
    "encrypt_note": {"type": "string"},
    "warning": {"type": "string"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["mode", "target", "manifest", "database", "schema_version", "files", "bytes", "blobs", "blobs_copied",
    "blobs_existing", "bytes_copied", "audit_archives", "encrypt_note", "warning", "dry_run"]
}`),
		// Audit: Art und Ausweis des Ziels (SHA-256 des Pfads, gekürzt),
		// nie der Pfad selbst; die Zähler schreibt backup.done.
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in BackupCreateInput
			if json.Unmarshal(raw, &in) != nil {
				return json.RawMessage(`{"invalid_input":true}`), nil
			}
			p := map[string]string{"mode": "folder", "target_hash": backup.PathHash(in.To)}
			if in.Tar != "" {
				p = map[string]string{"mode": "tar", "target_hash": "stdout"}
			}
			b, _ := json.Marshal(p)
			return b, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in BackupCreateInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if (in.To == "") == (in.Tar == "") {
				return nil, inputErr("give either a backup folder (to) or tar \"-\" for a tar stream on standard output")
			}
			if r == nil {
				return nil, errors.New("ops: backup is not available in this process")
			}
			o := backup.Options{To: in.To, DryRun: call.DryRun, CallAuditID: call.AuditID}
			if in.Tar != "" {
				if stdout == nil {
					return nil, inputErr("no standard output for the tar stream here; give a backup folder (to)")
				}
				o.Tar = stdout
			}
			return r.Create(ctx, o)
		},
	}
}
