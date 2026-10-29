package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Zweite Nachbesserung T3 (auftraege/A03_T3_nachbesserung2.md):
// Regressionstests zum Audit-Export (Wegwerf-Tests der Prüfsitzung, an die
// Lösung angepasst).

// t3n2CancelAfterSave bricht den Kontext nach dem ersten gespeicherten
// Nachweis ab (Strg+C kommt während oder nach dem ersten Nachweis an).
type t3n2CancelAfterSave struct {
	*store.SQLite
	cancel context.CancelFunc
	n      int
}

func (r *t3n2CancelAfterSave) SaveAuditExport(ctx context.Context, e store.AuditExport) error {
	err := r.SQLite.SaveAuditExport(ctx, e)
	r.n++
	if r.cancel != nil && r.n == 1 {
		r.cancel()
	}
	return err
}

// Punkt 1: Ein Abbruch nach dem ersten gespeicherten Nachweis lässt ein
// Manifest zurück, das zu den Nachweisen passt; audit verify bleibt ok
// (docs/betrieb.md 1.8), und der nächste Export schließt ab.
func TestT3N2AbortAfterFirstRecordKeepsManifest(t *testing.T) {
	for _, tc := range []string{"two-days", "one-day-recorded", "first-export-ever", "missing-file-repaired"} {
		t.Run(tc, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "audit-export")
			now := time.Now().UTC()
			today := now.Truncate(24 * time.Hour)
			st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
			switch tc {
			case "two-days":
				// Gestern exportiert, vor Mitternacht: Der Tag bleibt offen.
				t3nEvent(t, st, today.Add(-2*time.Hour), "sync.run")
				old := auditClock
				auditClock = func() time.Time { return today.Add(-time.Hour) }
				_, err := t3nExport(st, dir)
				auditClock = old
				if err != nil {
					t.Fatal(err)
				}
				// Später Eintrag gestern und einer heute: zwei Tage zu schreiben.
				t3nEvent(t, st, today.Add(-30*time.Minute), "sync.run")
				t3nEvent(t, st, now, "keys.list")
			case "one-day-recorded":
				t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
				if _, err := t3nExport(st, dir); err != nil {
					t.Fatal(err)
				}
				t3nEvent(t, st, now, "keys.list")
			case "first-export-ever":
				t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
				t3nEvent(t, st, now, "keys.list")
			case "missing-file-repaired":
				// Drei Tage am Ort; die Datei des ersten fehlt und wird neu
				// geschrieben, der letzte hat einen neuen Eintrag. Nach dem
				// Abbruch stehen der zweite (unverändert) und der dritte (noch
				// nicht geschrieben) mit ihren bisherigen Werten im Manifest.
				t3nEvent(t, st, now.Add(-72*time.Hour), "sync.run")
				t3nEvent(t, st, now.Add(-48*time.Hour), "sync.run")
				t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
				if _, err := t3nExport(st, dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(dir, auditFileName(now.Add(-72*time.Hour).Format("2006-01-02")))); err != nil {
					t.Fatal(err)
				}
				t3nEvent(t, st, now, "keys.list")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := &t3n2CancelAfterSave{SQLite: st, cancel: cancel}
			if _, err := AuditExport(repo, dir).Handler(ctx, Call{Input: json.RawMessage(`{}`)}); err == nil {
				t.Fatal("the export was not aborted")
			}
			if repo.n != 1 {
				t.Fatalf("records saved before the abort = %d, want 1", repo.n)
			}
			if v := t3nVerify(t, st); v.Status != "ok" {
				t.Errorf("verify after an abort following the first record = %s %+v", v.Status, v.Problems)
			}
			if _, err := t3nExport(st, dir); err != nil {
				t.Fatalf("next export = %v", err)
			}
			if v := t3nVerify(t, st); v.Status != "ok" || len(v.NotExported) != 0 {
				t.Errorf("verify after the next export = %+v", v)
			}
		})
	}
}
