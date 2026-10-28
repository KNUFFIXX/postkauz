package ops

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Audit lesen, exportieren, prüfen (S1.8; docs/sicherheit.md, Abschnitt 6).
// Das Audit enthält nie Mailinhalte; deshalb braucht seine Ausgabe keine
// Untrusted-Hülle.

// AuditRepo ist der Teil des Stores, den die Audit-Operationen brauchen.
type AuditRepo interface {
	AuditQuery(ctx context.Context, f store.AuditFilter) ([]store.AuditRecord, error)
	AuditCalls(ctx context.Context, f store.AuditFilter) ([]store.AuditRecord, error)
	AuditDays(ctx context.Context) ([]string, error)
	SaveAuditExport(ctx context.Context, e store.AuditExport) error
	AuditExports(ctx context.Context) ([]store.AuditExport, error)
	// AuditCountUpTo zählt die Einträge mit ID bis einschließlich id
	// (audit verify, Zählabgleich; A-F4).
	AuditCountUpTo(ctx context.Context, id int64) (int, error)
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
	// PolicyVersion: Version der Richtlinie bei der Entscheidung (seit
	// S2.1; fehlt bei älteren Einträgen, damit frühere Exporte Byte für
	// Byte gleich bleiben).
	PolicyVersion int64 `json:"policy_version,omitempty"`
	// RefID: Im Abschluss-Eintrag die ID des Start-Eintrags (seit
	// Migration 0018); fehlt sonst, damit frühere Exporte gleich bleiben.
	RefID int64 `json:"ref_id,omitempty"`
	// EndID: nur in der zusammengefassten Ansicht von audit.list (ohne
	// raw): die ID des Abschluss-Eintrags; Result, Reason und DurationMS
	// stammen dann von ihm. Nie im Export.
	EndID int64 `json:"end_id,omitempty"`
}

func auditEventOut(r store.AuditRecord) AuditEventOutput {
	o := AuditEventOutput{ID: r.ID, TS: r.TS.UTC().Format("2006-01-02T15:04:05.000Z07:00"), Actor: string(r.ActorType), Key: r.KeyID,
		Scope: r.Scope, Operation: r.Operation, Params: json.RawMessage(r.ParamsRedacted), Targets: r.Targets,
		Result: string(r.Result), Reason: r.Reason, DryRun: r.DryRun, DurationMS: r.Duration.Milliseconds(),
		PolicyVersion: r.PolicyVersion, RefID: r.RefID}
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
      "result": {"type": "string", "enum": ["started", "ok", "denied", "needs_approval", "error"]}, "reason": {"type": "string"},
      "dry_run": {"type": "boolean"}, "duration_ms": {"type": "integer"}, "policy_version": {"type": "integer"},
      "ref_id": {"type": "integer"}, "end_id": {"type": "integer"}},
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
	return time.Time{}, inputErr("invalid time: use a duration like 10m, 24h or 7d, a date like 2026-10-16, or RFC 3339")
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
// Seit Migration 0018 (A1) fasst sie Start und Abschluss eines Aufrufs zu
// einer Zeile zusammen (ID und Zeit des Starts, Endergebnis, Dauer, end_id);
// raw zeigt alle Zeilen einzeln.
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
    "result": {"type": "string", "enum": ["started", "ok", "denied", "needs_approval", "error"], "description": "final result of the call (started = no end entry yet)"},
    "raw": {"type": "boolean", "description": "show every stored entry (start and end of a call separately) instead of one line per call"},
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
				Raw       bool   `json:"raw,omitempty"`
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
			// Standard: eine Zeile je Aufruf (Start mit Endergebnis, A1);
			// raw: jede gespeicherte Zeile.
			query := repo.AuditCalls
			if in.Raw {
				query = repo.AuditQuery
			}
			list, err := query(ctx, store.AuditFilter{Since: since, Until: until, Actor: in.Actor, Key: in.Key,
				Operation: in.Operation, Result: in.Result, AfterID: in.AfterID, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			out := AuditListOutput{Events: []AuditEventOutput{}}
			for _, r := range list {
				o := auditEventOut(r)
				o.EndID = r.EndID
				out.Events = append(out.Events, o)
			}
			if len(list) == in.Limit {
				out.NextAfterID = list[len(list)-1].ID
			}
			return out, nil
		},
	}
}
