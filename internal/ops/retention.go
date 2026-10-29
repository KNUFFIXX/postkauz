package ops

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/retention"
)

// Löschkonzept (S3.4, E8): retention.purge und blobs.gc. Beide nur für den
// Menschen (HumanOnly, Scope admin, R2), mit Trockenlauf und Zählern im
// Audit (AuditCounts, nach einem Abbruch der Stand bis dahin über
// OutputOnError); Agenten sehen keine davon (kein MCP-Tool). Die
// Fachlogik steht in internal/retention und store/retention.go.

// RetentionConfig sind die Fristen aus der Konfiguration ([retention],
// [audit] retain; config.checkRetention setzt die Standards).
type RetentionConfig struct {
	// DeletedRemoteAfter: Standardfrist von purge (0 = keine; dann braucht
	// purge older_than).
	DeletedRemoteAfter time.Duration
	// BlobGCMinAge: Mindestalter einer Datei ohne Verweis für blob gc.
	BlobGCMinAge time.Duration
	// AuditRetain: audit rotate verschiebt nur ältere Tage.
	AuditRetain time.Duration
}

// RetentionPurgeOutput ist die Ausgabe von retention.purge.
type RetentionPurgeOutput struct {
	Account string `json:"account,omitempty"`
	// OlderThan ist die wirksame Frist (Eingabe oder Konfiguration), Cutoff
	// der Stichtag daraus (UTC, RFC 3339).
	OlderThan string `json:"older_than"`
	Cutoff    string `json:"cutoff"`
	// GoneFromServer: Nachrichten ohne Serverkopie, deren Löschung vor dem
	// Stichtag bemerkt wurde; davon bleiben KeptImported (importiertes
	// Archiv) und KeptDrafts (Verweis eines Entwurfs); Eligible = der Rest.
	GoneFromServer int  `json:"gone_from_server"`
	KeptImported   int  `json:"kept_imported"`
	KeptDrafts     int  `json:"kept_drafts"`
	Eligible       int  `json:"eligible"`
	Purged         int  `json:"purged"`
	Attachments    int  `json:"attachments"`
	ThreadsRemoved int  `json:"threads_removed"`
	DryRun         bool `json:"dry_run"`
}

// retentionArg: Fristen und Datumsangaben, die ins Audit dürfen (keine
// Freitexte).
var retentionArg = regexp.MustCompile(`^([0-9]{1,6}(d|h|m|s|ms)|[0-9]{4}-[0-9]{2}-[0-9]{2})$`)

// retentionErr übersetzt einen Fehler des Dienstes: Ein laufender Sync
// oder Import ist ErrSyncRunning (CLI: sync_running).
func retentionErr(err error) error {
	if errors.Is(err, retention.ErrBusy) {
		return errors.Join(ErrSyncRunning, err)
	}
	return err
}

// RetentionPurge entfernt Nachrichten, die auf dem Server gelöscht wurden
// (E8, A13.2, A14; Regel in store/retention.go).
func RetentionPurge(d Deps) Operation {
	return Operation{
		Name:      "retention.purge",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R2,
		HumanOnly: true,
		// Bericht bis zum Abbruch, Zähler auch dann im Audit (Punkt 7).
		OutputOnError: true,
		Summary: "Delete messages from the local archive that were deleted on the server longer ago than a period " +
			"(never imported archive messages, never messages a draft refers to); blobs stay until blob gc.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "older_than": {"type": "string", "description": "period like 90d or 12h, or a date (YYYY-MM-DD); default: [retention] deleted_remote_after"},
    "account": {"type": "string", "description": "only this account (default: all)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"}, "older_than": {"type": "string"}, "cutoff": {"type": "string", "format": "date-time"},
    "gone_from_server": {"type": "integer"}, "kept_imported": {"type": "integer"}, "kept_drafts": {"type": "integer"},
    "eligible": {"type": "integer"}, "purged": {"type": "integer"}, "attachments": {"type": "integer"},
    "threads_removed": {"type": "integer"}, "dry_run": {"type": "boolean"}
  },
  "required": ["older_than", "cutoff", "gone_from_server", "kept_imported", "kept_drafts", "eligible", "purged", "attachments", "threads_removed", "dry_run"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				OlderThan string `json:"older_than"`
				Account   string `json:"account"`
			}
			_ = json.Unmarshal(raw, &in)
			p := map[string]any{}
			if retentionArg.MatchString(in.OlderThan) {
				p["older_than"] = in.OlderThan
			} else if in.OlderThan != "" {
				p["older_than"] = "other"
			}
			var targets []string
			if AccountNameRE.MatchString(in.Account) {
				targets = []string{"account:" + in.Account}
			}
			b, _ := json.Marshal(p)
			return b, targets
		},
		AuditCounts: func(out any) map[string]int64 {
			o, ok := out.(RetentionPurgeOutput)
			if !ok {
				return nil
			}
			return map[string]int64{"gone_from_server": int64(o.GoneFromServer), "kept_imported": int64(o.KeptImported),
				"kept_drafts": int64(o.KeptDrafts), "eligible": int64(o.Eligible), "purged": int64(o.Purged),
				"attachments": int64(o.Attachments), "threads_removed": int64(o.ThreadsRemoved)}
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				OlderThan string `json:"older_than"`
				Account   string `json:"account"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			svc := d.Retention
			if svc == nil {
				return nil, errors.New("retention: not configured")
			}
			now := svc.Now
			if now == nil {
				now = time.Now
			}
			at := now()
			older := in.OlderThan
			if older == "" {
				if d.RetentionConfig.DeletedRemoteAfter <= 0 {
					return nil, inputErr("no period: give one (purge --older-than 90d) or set [retention] deleted_remote_after in the configuration")
				}
				older = formatDays(d.RetentionConfig.DeletedRemoteAfter)
			}
			cutoff, err := ParseSince(older, at)
			if err != nil {
				return nil, err
			}
			cutoff = minTime(cutoff, at)
			o := retention.PurgeOptions{Cutoff: cutoff, DryRun: call.DryRun}
			out := RetentionPurgeOutput{OlderThan: older, Cutoff: cutoff.UTC().Format(time.RFC3339), DryRun: call.DryRun}
			if in.Account != "" {
				a, err := d.Accounts.AccountByName(ctx, in.Account)
				if err != nil {
					return nil, err
				}
				o.AccountID, out.Account = a.ID, a.Name
			}
			rep, err := svc.Purge(ctx, o)
			out.GoneFromServer, out.KeptImported, out.KeptDrafts, out.Eligible = rep.GoneFromServer, rep.KeptImported, rep.KeptDrafts, rep.Eligible
			out.Purged, out.Attachments, out.ThreadsRemoved = rep.Purged, rep.Attachments, rep.ThreadsRemoved
			if err != nil {
				// Teil-Ausgabe (OutputOnError): Was bis zum Abbruch gelöscht
				// wurde, steht so im Audit und in der Ausgabe (Punkt 7).
				return partialOutput(out, err), retentionErr(err)
			}
			return out, nil
		},
	}
}

// partialOutput liefert die Ausgabe bis zu einem Abbruch (OutputOnError);
// lehnte der Dienst vor jeder Arbeit ab (ein Sync läuft), keine.
func partialOutput(out any, err error) any {
	if errors.Is(err, retention.ErrBusy) {
		return nil
	}
	return out
}

// BlobsGCOutput ist die Ausgabe von blobs.gc.
type BlobsGCOutput struct {
	// MinAge ist das wirksame Mindestalter ([retention] blob_gc_min_age).
	MinAge       string `json:"min_age"`
	Files        int    `json:"files"`
	Rows         int    `json:"rows"`
	Referenced   int    `json:"referenced"`
	Unreferenced int    `json:"unreferenced"`
	KeptYoung    int    `json:"kept_young"`
	RemovedFiles int    `json:"removed_files"`
	RemovedRows  int    `json:"removed_rows"`
	Bytes        int64  `json:"bytes"`
	Skipped      int    `json:"skipped"`
	DryRun       bool   `json:"dry_run"`
}

// BlobsGC entfernt Dateien und Zeilen des Blob-Stores ohne Verweis, die
// älter als das Mindestalter sind (E8, A11, A13.3, A14).
func BlobsGC(d Deps) Operation {
	return Operation{
		Name:      "blobs.gc",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R2,
		HumanOnly: true,
		// Bericht bis zum Abbruch, Zähler auch dann im Audit (Punkt 7).
		OutputOnError: true,
		Summary: "Delete stored files (raw messages, attachments) nothing refers to any more and that are older than " +
			"[retention] blob_gc_min_age; draft attachments always stay.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "min_age": {"type": "string"}, "files": {"type": "integer"}, "rows": {"type": "integer"},
    "referenced": {"type": "integer"}, "unreferenced": {"type": "integer"}, "kept_young": {"type": "integer"},
    "removed_files": {"type": "integer"}, "removed_rows": {"type": "integer"}, "bytes": {"type": "integer"},
    "skipped": {"type": "integer"}, "dry_run": {"type": "boolean"}
  },
  "required": ["min_age", "files", "rows", "referenced", "unreferenced", "kept_young", "removed_files", "removed_rows", "bytes", "skipped", "dry_run"]
}`),
		AuditCounts: func(out any) map[string]int64 {
			o, ok := out.(BlobsGCOutput)
			if !ok {
				return nil
			}
			return map[string]int64{"files": int64(o.Files), "rows": int64(o.Rows), "referenced": int64(o.Referenced),
				"unreferenced": int64(o.Unreferenced), "kept_young": int64(o.KeptYoung), "removed_files": int64(o.RemovedFiles),
				"removed_rows": int64(o.RemovedRows), "bytes": o.Bytes, "skipped": int64(o.Skipped)}
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			svc := d.Retention
			if svc == nil {
				return nil, errors.New("retention: not configured")
			}
			minAge := d.RetentionConfig.BlobGCMinAge
			if minAge <= 0 {
				minAge = 24 * time.Hour
			}
			rep, err := svc.GC(ctx, minAge, call.DryRun)
			out := BlobsGCOutput{MinAge: formatDays(minAge), Files: rep.Files, Rows: rep.Rows, Referenced: rep.Referenced,
				Unreferenced: rep.Unreferenced, KeptYoung: rep.KeptYoung, RemovedFiles: rep.RemovedFiles, RemovedRows: rep.RemovedRows,
				Bytes: rep.Bytes, Skipped: rep.Skipped, DryRun: call.DryRun}
			if err != nil {
				return partialOutput(out, err), retentionErr(err)
			}
			return out, nil
		},
	}
}

// formatDays schreibt eine Dauer in ganzen Tagen ("365d"), sonst wie Go.
func formatDays(d time.Duration) string {
	if d > 0 && d%(24*time.Hour) == 0 {
		return itoa(int64(d/(24*time.Hour))) + "d"
	}
	return d.String()
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
