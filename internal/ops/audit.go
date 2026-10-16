package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Audit lesen, exportieren, prüfen (S1.8; docs/sicherheit.md, Abschnitt 6).
// Das Audit enthält nie Mailinhalte; deshalb braucht seine Ausgabe keine
// Untrusted-Hülle.

// AuditRepo ist der Teil des Stores, den die Audit-Operationen brauchen.
type AuditRepo interface {
	AuditQuery(ctx context.Context, f store.AuditFilter) ([]store.AuditRecord, error)
	AuditDays(ctx context.Context) ([]string, error)
	SaveAuditExport(ctx context.Context, e store.AuditExport) error
	AuditExports(ctx context.Context) ([]store.AuditExport, error)
}

// AuditEventOutput ist ein Audit-Eintrag in audit.list und im Export.
type AuditEventOutput struct {
	ID         int64           `json:"id"`
	TS         string          `json:"ts"`
	Actor      string          `json:"actor"`
	Key        string          `json:"key,omitempty"`
	Scope      string          `json:"scope,omitempty"`
	Operation  string          `json:"operation"`
	Params     json.RawMessage `json:"params"`
	Targets    []string        `json:"targets"`
	Result     string          `json:"result"`
	Reason     string          `json:"reason,omitempty"`
	DryRun     bool            `json:"dry_run"`
	DurationMS int64           `json:"duration_ms,omitempty"`
}

func auditEventOut(r store.AuditRecord) AuditEventOutput {
	o := AuditEventOutput{ID: r.ID, TS: r.TS.UTC().Format("2006-01-02T15:04:05.000Z07:00"), Actor: string(r.ActorType), Key: r.KeyID,
		Scope: r.Scope, Operation: r.Operation, Params: json.RawMessage(r.ParamsRedacted), Targets: r.Targets,
		Result: string(r.Result), Reason: r.Reason, DryRun: r.DryRun, DurationMS: r.Duration.Milliseconds()}
	if len(o.Params) == 0 || !json.Valid(o.Params) {
		o.Params = json.RawMessage(`{}`)
	}
	if o.Targets == nil {
		o.Targets = []string{}
	}
	return o
}

const auditEventSchema = `{"type": "object", "properties": {
      "id": {"type": "integer"}, "ts": {"type": "string", "format": "date-time"},
      "actor": {"type": "string", "enum": ["human", "agent", "system"]}, "key": {"type": "string"},
      "scope": {"type": "string"}, "operation": {"type": "string"}, "params": {"type": "object"},
      "targets": {"type": "array", "items": {"type": "string"}},
      "result": {"type": "string", "enum": ["ok", "denied", "needs_approval", "error"]}, "reason": {"type": "string"},
      "dry_run": {"type": "boolean"}, "duration_ms": {"type": "integer"}},
      "required": ["id", "ts", "actor", "operation", "params", "targets", "result", "dry_run"]}`

// ParseSince liest eine Zeitangabe: Dauer rückwärts ab now ("10m", "24h",
// "7d"), Datum ("2026-10-16", Mitternacht Ortszeit) oder RFC 3339.
func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n >= 0 {
			return now.Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, inputErr("invalid time %q: use a duration like 10m, 24h or 7d, a date like 2026-10-16, or RFC 3339", s)
}

// AuditListOutput ist die Ausgabe von audit.list.
type AuditListOutput struct {
	Events []AuditEventOutput `json:"events"`
	// NextAfterID setzt mit after_id fort, wenn die Seite voll war.
	NextAfterID int64 `json:"next_after_id,omitempty"`
}

// Grenzen von audit.list.
const (
	DefaultAuditLimit = 100
	MaxAuditLimit     = 1000
)

// AuditList zeigt Audit-Einträge (ohne Inhalte). Lesend (R0, audit:read).
func AuditList(repo AuditRepo) Operation {
	return Operation{
		Name:       "audit.list",
		Scope:      policy.ScopeAuditRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show audit log entries: who called which operation with which result (never message content).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "since": {"type": "string", "description": "duration back from now (10m, 24h, 7d), a date or RFC 3339; default 24h"},
    "until": {"type": "string", "description": "end (exclusive), same forms as since"},
    "actor": {"type": "string", "enum": ["human", "agent", "system"]},
    "key": {"type": "string", "description": "key id as shown in the log, e.g. key:assistant-1"},
    "operation": {"type": "string", "description": "operation name, e.g. messages.search"},
    "result": {"type": "string", "enum": ["ok", "denied", "needs_approval", "error"]},
    "limit": {"type": "integer", "minimum": 1, "maximum": 1000, "description": "default 100"},
    "after_id": {"type": "integer", "minimum": 0, "description": "continue after this entry (next_after_id)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "events": {"type": "array", "items": ` + auditEventSchema + `},
    "next_after_id": {"type": "integer"}
  },
  "required": ["events"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			params, _ := redactAccountInput(raw)
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Since     string `json:"since,omitempty"`
				Until     string `json:"until,omitempty"`
				Actor     string `json:"actor,omitempty"`
				Key       string `json:"key,omitempty"`
				Operation string `json:"operation,omitempty"`
				Result    string `json:"result,omitempty"`
				Limit     int    `json:"limit,omitempty"`
				AfterID   int64  `json:"after_id,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			now := time.Now()
			if in.Since == "" {
				in.Since = "24h"
			}
			since, err := ParseSince(in.Since, now)
			if err != nil {
				return nil, err
			}
			until, err := ParseSince(in.Until, now)
			if err != nil {
				return nil, err
			}
			if in.Limit == 0 {
				in.Limit = DefaultAuditLimit
			}
			list, err := repo.AuditQuery(ctx, store.AuditFilter{Since: since, Until: until, Actor: in.Actor, Key: in.Key,
				Operation: in.Operation, Result: in.Result, AfterID: in.AfterID, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			out := AuditListOutput{Events: []AuditEventOutput{}}
			for _, r := range list {
				out.Events = append(out.Events, auditEventOut(r))
			}
			if len(list) == in.Limit {
				out.NextAfterID = list[len(list)-1].ID
			}
			return out, nil
		},
	}
}

// --- Export und Prüfung -------------------------------------------------------

// auditFileName ist der Name der Exportdatei eines Tages.
func auditFileName(day string) string { return brand.Name + "-audit-" + day + ".jsonl" }

// auditDayContent erzeugt den Inhalt der Exportdatei eines Tages aus der
// Datenbank: eine JSON-Zeile je Eintrag, aufsteigend, höchstens bis lastID
// (0 = alle). Dieselbe Funktion dient Export und Prüfung.
func auditDayContent(ctx context.Context, repo AuditRepo, day string, lastID int64) ([]byte, int64, int64, int, error) {
	start, err := time.Parse("2006-01-02", day)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	list, err := repo.AuditQuery(ctx, store.AuditFilter{Since: start, Until: start.Add(24 * time.Hour)})
	if err != nil {
		return nil, 0, 0, 0, err
	}
	var buf bytes.Buffer
	var first, last int64
	n := 0
	for _, r := range list {
		if lastID > 0 && r.ID > lastID {
			break
		}
		b, err := json.Marshal(auditEventOut(r))
		if err != nil {
			return nil, 0, 0, 0, err
		}
		buf.Write(b)
		buf.WriteByte('\n')
		if first == 0 {
			first = r.ID
		}
		last = r.ID
		n++
	}
	return buf.Bytes(), first, last, n, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

// AuditExportFile beschreibt eine geschriebene (oder geprüfte) Exportdatei.
type AuditExportFile struct {
	Day          string `json:"day"`
	File         string `json:"file"`
	Events       int    `json:"events"`
	FirstEventID int64  `json:"first_event_id"`
	LastEventID  int64  `json:"last_event_id"`
	SHA256       string `json:"sha256"`
	State        string `json:"state"` // written | updated | unchanged
}

// AuditExportOutput ist die Ausgabe von audit.export.
type AuditExportOutput struct {
	Location string            `json:"location"`
	Files    []AuditExportFile `json:"files"`
	DryRun   bool              `json:"dry_run"`
}

// ErrAuditExportChanged: Eine früher exportierte Datei passt nicht mehr zu
// ihrer Prüfsumme; der Export überschreibt sie nicht (sonst verschwände der
// Beleg einer Manipulation).
var ErrAuditExportChanged = errors.New("audit export file does not match its recorded checksum")

// AuditExport schreibt das Audit tageweise als JSONL an einen zweiten Ort
// und hält Prüfsumme und Ort in audit_exports fest (R1, admin). Ein noch
// laufender Tag wird bei jedem Export neu geschrieben.
func AuditExport(repo AuditRepo, defaultDir string) Operation {
	return Operation{
		Name:    "audit.export",
		Scope:   policy.ScopeAdmin,
		Risk:    policy.R1,
		Summary: "Export the audit log day by day as JSONL files with checksums to a second location.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "to": {"type": "string", "description": "export directory (default: [audit] export_dir in the configuration)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "location": {"type": "string"},
    "files": {"type": "array", "items": {"type": "object", "properties": {
      "day": {"type": "string"}, "file": {"type": "string"}, "events": {"type": "integer"},
      "first_event_id": {"type": "integer"}, "last_event_id": {"type": "integer"}, "sha256": {"type": "string"},
      "state": {"type": "string", "enum": ["written", "updated", "unchanged"]}},
      "required": ["day", "file", "events", "first_event_id", "last_event_id", "sha256", "state"]}},
    "dry_run": {"type": "boolean"}
  },
  "required": ["location", "files", "dry_run"]
}`),
		// Der Pfad kann den Nutzernamen enthalten: ins Audit nur Feldnamen.
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			params, _ := redactAccountInput(raw)
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				To string `json:"to,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			dir := in.To
			if dir == "" {
				dir = defaultDir
			}
			if dir == "" {
				return nil, inputErr("no export directory: give one (audit export --to <dir>) or set [audit] export_dir in the configuration")
			}
			dir, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			days, err := repo.AuditDays(ctx)
			if err != nil {
				return nil, err
			}
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			prev := map[string]store.AuditExport{}
			for _, e := range done {
				prev[e.Day] = e
			}
			out := AuditExportOutput{Location: dir, Files: []AuditExportFile{}, DryRun: call.DryRun}
			if !call.DryRun {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return nil, err
				}
			}
			for _, day := range days {
				content, first, last, n, err := auditDayContent(ctx, repo, day, 0)
				if err != nil {
					return nil, err
				}
				f := AuditExportFile{Day: day, File: filepath.Join(dir, auditFileName(day)), Events: n, FirstEventID: first,
					LastEventID: last, SHA256: sha256Hex(content), State: "written"}
				if p, ok := prev[day]; ok {
					// Die frühere Datei muss unverändert sein, bevor sie ersetzt wird.
					old := filepath.Join(p.Location, auditFileName(day))
					got, err := fileSHA256(old)
					switch {
					case err == nil && got != p.SHA256:
						return nil, fmt.Errorf("%w: %s (run audit verify)", ErrAuditExportChanged, old)
					case err == nil && got == f.SHA256 && p.Location == dir:
						f.State = "unchanged"
					case err != nil && !errors.Is(err, fs.ErrNotExist):
						return nil, err
					default:
						f.State = "updated"
					}
				}
				out.Files = append(out.Files, f)
				if call.DryRun || f.State == "unchanged" {
					continue
				}
				if err := writeFileAtomic(f.File, content); err != nil {
					return nil, err
				}
				if err := repo.SaveAuditExport(ctx, store.AuditExport{Day: day, FirstEventID: first, LastEventID: last,
					SHA256: f.SHA256, Location: dir, ExportedAt: time.Now()}); err != nil {
					return nil, err
				}
			}
			return out, nil
		},
	}
}

// writeFileAtomic schreibt über eine Temporärdatei im selben Verzeichnis.
func writeFileAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// AuditProblem ist ein Befund von audit.verify.
type AuditProblem struct {
	Day     string `json:"day"`
	File    string `json:"file"`
	Problem string `json:"problem"` // file_missing | file_changed | database_changed
}

// AuditVerifyOutput ist die Ausgabe von audit.verify.
type AuditVerifyOutput struct {
	Status      string         `json:"status"` // ok | mismatch
	DaysChecked int            `json:"days_checked"`
	Problems    []AuditProblem `json:"problems"`
	// NotExported nennt Tage mit Einträgen, die noch nicht (vollständig)
	// exportiert sind; das ist kein Fehler, nur ein Hinweis.
	NotExported []string `json:"not_exported"`
}

// AuditVerify vergleicht die exportierten Dateien mit ihren Prüfsummen und
// mit den Zeilen der Datenbank (R0, audit:read).
func AuditVerify(repo AuditRepo) Operation {
	return Operation{
		Name:       "audit.verify",
		Scope:      policy.ScopeAuditRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Check exported audit files against their checksums and against the database.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "enum": ["ok", "mismatch"]},
    "days_checked": {"type": "integer"},
    "problems": {"type": "array", "items": {"type": "object", "properties": {
      "day": {"type": "string"}, "file": {"type": "string"},
      "problem": {"type": "string", "enum": ["file_missing", "file_changed", "database_changed"]}},
      "required": ["day", "file", "problem"]}},
    "not_exported": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["status", "days_checked", "problems", "not_exported"]
}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			out := AuditVerifyOutput{Status: "ok", Problems: []AuditProblem{}, NotExported: []string{}}
			exported := map[string]store.AuditExport{}
			for _, e := range done {
				exported[e.Day] = e
				out.DaysChecked++
				file := filepath.Join(e.Location, auditFileName(e.Day))
				got, err := fileSHA256(file)
				switch {
				case errors.Is(err, fs.ErrNotExist):
					out.Problems = append(out.Problems, AuditProblem{Day: e.Day, File: file, Problem: "file_missing"})
				case err != nil:
					return nil, err
				case got != e.SHA256:
					out.Problems = append(out.Problems, AuditProblem{Day: e.Day, File: file, Problem: "file_changed"})
				}
				content, _, _, _, err := auditDayContent(ctx, repo, e.Day, e.LastEventID)
				if err != nil {
					return nil, err
				}
				if sha256Hex(content) != e.SHA256 {
					out.Problems = append(out.Problems, AuditProblem{Day: e.Day, File: file, Problem: "database_changed"})
				}
			}
			days, err := repo.AuditDays(ctx)
			if err != nil {
				return nil, err
			}
			for _, d := range days {
				e, ok := exported[d]
				if !ok {
					out.NotExported = append(out.NotExported, d)
					continue
				}
				_, _, last, _, err := auditDayContent(ctx, repo, d, 0)
				if err != nil {
					return nil, err
				}
				if last > e.LastEventID {
					out.NotExported = append(out.NotExported, d)
				}
			}
			if len(out.Problems) > 0 {
				out.Status = "mismatch"
			}
			return out, nil
		},
	}
}
