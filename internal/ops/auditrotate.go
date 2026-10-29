package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Rotation des Audits (T3, A03 E8, A14; docs/sicherheit.md, Abschnitt 6):
// Tage, die exportiert und per audit verify bestätigt und älter als
// [audit] retain sind, wandern in eine Archivdatenbank
// audit-archive-<von>-<bis>.db im Datenverzeichnis. Die Hash-Kette läuft
// danach weiter: Export und Prüfung lesen die Einträge archivierter Tage
// aus dem Archiv (archivedRepo), als stünden sie noch in der Datenbank;
// audit_archives hält Prüfsumme der Archivdatei und den Kettenwert nach dem
// letzten archivierten Tag (Anker). Fehlt ein Archiv oder ist es
// verändert, meldet audit verify das und prüft den Rest ab dem Anker.

// AuditArchiver ist der Teil des Stores für Archive (store.SQLite). Ein
// AuditRepo ohne ihn (Testattrappen) kennt keine Archive.
type AuditArchiver interface {
	AuditArchives(ctx context.Context) ([]store.AuditArchive, error)
	AuditArchivePath(file string) string
	WriteAuditArchive(ctx context.Context, path string, info map[string]string, ids []int64) (string, error)
	CommitAuditRotation(ctx context.Context, a store.AuditArchive, ids []int64) error
}

// Befunde von audit.verify zu Archiven.
const (
	AuditArchiveMissing = "archive_missing" // Archivdatei fehlt
	AuditArchiveChanged = "archive_changed" // Archivdatei passt nicht zu ihrer Prüfsumme
)

// ErrAuditArchive: Ein Archiv fehlt oder ist verändert; der Export kann
// dessen Tage nicht neu schreiben.
var ErrAuditArchive = errors.New("audit archive is missing or changed")

// ErrAuditRotateRunning: Eine andere Rotation dieses Datenverzeichnisses
// läuft oder hat seit der Planung ein Archiv eingetragen (Nachbesserung
// T3, Punkt 3a).
var ErrAuditRotateRunning = errors.New("another audit rotate of this data directory is running")

// auditArchiveState ist ein Archiv mit dem Ergebnis seiner Prüfung.
type auditArchiveState struct {
	store.AuditArchive
	path    string
	problem string // "" = verwendbar
	db      *store.AuditArchiveDB
}

// auditSource ist das Audit, wie Export und Prüfung es sehen: Datenbank
// und verwendbare Archive zusammen (repo), dazu alle Archive mit Befund.
type auditSource struct {
	repo     AuditRepo
	archives []*auditArchiveState
	// base ist der Store selbst (für Merker und Kennung; repo kann eine
	// Hülle mit Archiven sein).
	base AuditRepo
}

// archiveOf liefert das Archiv, das einen Exporttag enthält.
func (s auditSource) archiveOf(day string) *auditArchiveState {
	for _, a := range s.archives {
		if day >= a.FromDay && day <= a.ToDay {
			return a
		}
	}
	return nil
}

// lastArchivedDay ist der letzte archivierte Tag ("" = keiner).
func (s auditSource) lastArchivedDay() string {
	last := ""
	for _, a := range s.archives {
		last = max(last, a.ToDay)
	}
	return last
}

// openAuditSource prüft jedes Archiv (Datei vorhanden, Prüfsumme, lesbar)
// und baut die gemeinsame Sicht. close schließt die Archive.
func openAuditSource(ctx context.Context, repo AuditRepo) (src auditSource, closeFn func(), err error) {
	src.repo, src.base = repo, repo
	closeFn = func() {
		for _, a := range src.archives {
			if a.db != nil {
				_ = a.db.Close()
			}
		}
	}
	ar, ok := repo.(AuditArchiver)
	if !ok {
		return src, closeFn, nil
	}
	list, err := ar.AuditArchives(ctx)
	if err != nil {
		return src, closeFn, err
	}
	var dbs []*store.AuditArchiveDB
	for _, a := range list {
		st := &auditArchiveState{AuditArchive: a, path: ar.AuditArchivePath(a.File)}
		src.archives = append(src.archives, st)
		sum, err := store.FileSHA256(st.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			st.problem = AuditArchiveMissing
			continue
		case err != nil:
			return src, closeFn, err
		case sum != a.SHA256:
			st.problem = AuditArchiveChanged
			continue
		}
		if st.db, err = store.OpenAuditArchive(st.path); err != nil {
			st.problem = AuditArchiveChanged
			continue
		}
		dbs = append(dbs, st.db)
	}
	if len(dbs) > 0 {
		src.repo = archivedRepo{AuditRepo: repo, archives: dbs}
	}
	return src, closeFn, nil
}

// archivedRepo liest Audit-Einträge aus Datenbank und Archiven zusammen,
// als stünden die archivierten noch in der Datenbank (IDs werden nie neu
// vergeben, AUTOINCREMENT).
type archivedRepo struct {
	AuditRepo
	archives []*store.AuditArchiveDB
}

func (r archivedRepo) AuditQuery(ctx context.Context, f store.AuditFilter) ([]store.AuditRecord, error) {
	out, err := r.AuditRepo.AuditQuery(ctx, f)
	if err != nil {
		return nil, err
	}
	for _, a := range r.archives {
		more, err := a.AuditQuery(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (r archivedRepo) AuditCountUpTo(ctx context.Context, id int64) (int, error) {
	n, err := r.AuditRepo.AuditCountUpTo(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, a := range r.archives {
		m, err := a.AuditCountUpTo(ctx, id)
		if err != nil {
			return 0, err
		}
		n += m
	}
	return n, nil
}

// AuditRotateOutput ist die Ausgabe von audit.rotate.
type AuditRotateOutput struct {
	// Status: rotated | nothing_to_rotate (im Trockenlauf: would_rotate).
	Status string `json:"status"`
	Retain string `json:"retain"`
	// Cutoff: nur Tage, die vor diesem Zeitpunkt endeten (UTC, RFC 3339).
	Cutoff    string `json:"cutoff"`
	Days      int    `json:"days"`
	FromDay   string `json:"from_day,omitempty"`
	ToDay     string `json:"to_day,omitempty"`
	Events    int    `json:"events"`
	Archive   string `json:"archive,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	ChainHead string `json:"chain_head,omitempty"`
	DryRun    bool   `json:"dry_run"`
}

// AuditRotate verschiebt alte, exportierte und bestätigte Tage in eine
// Archivdatenbank (HumanOnly, admin, R2; E8). Ohne Export eines alten Tages
// denied not_exported, bei Abweichungen von audit verify denied
// not_verified.
func AuditRotate(repo AuditRepo, retain time.Duration) Operation {
	return Operation{
		Name:      "audit.rotate",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R2,
		HumanOnly: true,
		Summary: "Move audit days older than [audit] retain that were exported and confirmed by audit verify into an archive " +
			"database next to the database; audit verify and audit export keep working across it.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "enum": ["rotated", "would_rotate", "nothing_to_rotate"]},
    "retain": {"type": "string"}, "cutoff": {"type": "string", "format": "date-time"},
    "days": {"type": "integer"}, "from_day": {"type": "string"}, "to_day": {"type": "string"}, "events": {"type": "integer"},
    "archive": {"type": "string"}, "sha256": {"type": "string"}, "chain_head": {"type": "string"}, "dry_run": {"type": "boolean"}
  },
  "required": ["status", "retain", "cutoff", "days", "events", "dry_run"]
}`),
		AuditCounts: func(out any) map[string]int64 {
			o, ok := out.(AuditRotateOutput)
			if !ok {
				return nil
			}
			return map[string]int64{"days": int64(o.Days), "events": int64(o.Events)}
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			ar, ok := repo.(AuditArchiver)
			if !ok {
				return nil, errors.New("audit rotate needs the database")
			}
			if retain <= 0 {
				retain = 365 * 24 * time.Hour
			}
			now := auditClock().UTC()
			cutoff := now.Add(-retain)
			out := AuditRotateOutput{Status: "nothing_to_rotate", Retain: formatDays(retain), Cutoff: cutoff.Format(time.RFC3339), DryRun: call.DryRun}

			src, closeArchives, err := openAuditSource(ctx, repo)
			defer closeArchives()
			if err != nil {
				return nil, err
			}
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			plan, err := buildAuditPlan(ctx, src.repo, done)
			if err != nil {
				return nil, err
			}
			old := func(day string) bool { return !auditDayEnd(day).After(cutoff) }
			// Ein alter Tag ohne abgeschlossenen Export (nie exportiert, oder
			// der Export lief vor seinem Ende): erst exportieren.
			for _, d := range plan.days {
				if old(d) {
					return nil, deniedErr("not_exported", "audit day %s is older than %s but not exported completely; run audit export, then audit verify, then try again", d, out.Retain)
				}
			}
			lastArchived := src.lastArchivedDay()
			var rotate []store.AuditExport
			for _, e := range done {
				if e.Day <= lastArchived || !old(e.Day) {
					continue
				}
				if _, closed := plan.asg.closed(e.Day); !closed {
					return nil, deniedErr("not_exported", "audit day %s was exported before it ended; run audit export again, then audit verify", e.Day)
				}
				rotate = append(rotate, e)
			}
			if len(rotate) == 0 {
				return out, nil
			}
			// Bestätigung: audit verify über alles (Datenbank, Archive,
			// Dateien, Kette, Manifest) ohne Befund.
			v, heads, err := verifyAudit(ctx, src)
			if err != nil {
				return nil, err
			}
			if v.Status != "ok" {
				return nil, deniedErr("not_verified", "audit verify reports %d problems; fix them before rotating (run: audit verify)", len(v.Problems))
			}
			var ids []int64
			var records []store.AuditRecord
			for _, e := range rotate {
				evs, err := loadAuditDay(ctx, src.repo, plan.asg, e)
				if err != nil {
					return nil, err
				}
				records = append(records, evs...)
			}
			if len(records) == 0 {
				return out, nil
			}
			sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
			for _, r := range records {
				ids = append(ids, r.ID)
			}
			prev := auditChainGenesis
			if i := slices.IndexFunc(done, func(e store.AuditExport) bool { return e.Day == rotate[0].Day }); i > 0 {
				prev = heads[done[i-1].Day]
			}
			first, last := rotate[0].Day, rotate[len(rotate)-1].Day
			arc := store.AuditArchive{File: store.AuditArchiveName(first, last), FromDay: first, ToDay: last,
				FirstEventID: ids[0], LastEventID: ids[len(ids)-1], Events: len(ids), ChainPrev: prev, ChainHead: heads[last], CreatedAt: now}
			out.Status, out.Days, out.FromDay, out.ToDay, out.Events = "would_rotate", len(rotate), first, last, len(ids)
			out.Archive, out.ChainHead = ar.AuditArchivePath(arc.File), arc.ChainHead
			if call.DryRun {
				return out, nil
			}
			// Nur eine Rotation je Datenverzeichnis zugleich (Punkt 3a).
			unlock, err := lockBesideDB(repo, "audit-rotate.lock", ErrAuditRotateRunning)
			if err != nil {
				return nil, err
			}
			defer unlock()
			// Hat seit der Planung ein anderer Lauf ein Archiv eingetragen
			// (etwa ohne Sperre, ältere Programmversion), ist der Plan
			// überholt; eine Datei mit eingetragenem Namen ist dessen
			// Archiv und wird nie gelöscht.
			recorded, err := ar.AuditArchives(ctx)
			if err != nil {
				return nil, err
			}
			for _, a := range recorded {
				if a.File == arc.File || !slices.ContainsFunc(src.archives, func(s *auditArchiveState) bool { return s.File == a.File }) {
					return nil, fmt.Errorf("%w: archive %s was recorded in the meantime; run audit verify, then audit rotate again", ErrAuditRotateRunning, a.File)
				}
			}
			// Eine Datei dieses Namens ohne Eintrag stammt von einem
			// abgebrochenen Lauf (vor der Transaktion): Ihre Zeilen stehen
			// noch in der Datenbank, sie wird ersetzt.
			if _, err := os.Lstat(out.Archive); err == nil {
				if err := os.Remove(out.Archive); err != nil {
					return nil, err
				}
			}
			info := map[string]string{"program": brand.Name, "format": "1", "from_day": first, "to_day": last,
				"first_event_id": fmt.Sprint(arc.FirstEventID), "last_event_id": fmt.Sprint(arc.LastEventID),
				"events": fmt.Sprint(arc.Events), "chain_prev": arc.ChainPrev, "chain_head": arc.ChainHead,
				"chain": auditChainRule, "created_at": now.Format(time.RFC3339)}
			if arc.SHA256, err = ar.WriteAuditArchive(ctx, out.Archive, info, ids); err != nil {
				return nil, err
			}
			if err := checkArchiveCopy(ctx, out.Archive, records); err != nil {
				_ = os.Remove(out.Archive)
				return nil, err
			}
			if err := ar.CommitAuditRotation(ctx, arc, ids); err != nil {
				_ = os.Remove(out.Archive)
				return nil, err
			}
			out.Status, out.SHA256 = "rotated", arc.SHA256
			return out, nil
		},
	}
}

// checkArchiveCopy liest das geschriebene Archiv und vergleicht jeden
// Eintrag in der Form des Exports mit dem Original.
func checkArchiveCopy(ctx context.Context, path string, want []store.AuditRecord) error {
	db, err := store.OpenAuditArchive(path)
	if err != nil {
		return err
	}
	defer db.Close()
	got, err := db.AuditQuery(ctx, store.AuditFilter{})
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("audit archive holds %d of %d entries; nothing was deleted", len(got), len(want))
	}
	for i := range got {
		a, _ := json.Marshal(auditEventOut(got[i]))
		b, _ := json.Marshal(auditEventOut(want[i]))
		if string(a) != string(b) {
			return fmt.Errorf("audit archive entry %d differs from the database; nothing was deleted", want[i].ID)
		}
	}
	return nil
}
