package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// t3n2IDErr lässt das Lesen der Kennung scheitern (z. B. gesperrte
// Datenbank).
type t3n2IDErr struct{ *store.SQLite }

func (t3n2IDErr) DataDirIDFull(context.Context) (string, error) {
	return "", errors.New("database is locked")
}

type t3n2NoID struct{ *store.SQLite }

func (t3n2NoID) DataDirIDFull(context.Context) (string, error) { return "", store.ErrNoDataDirID }

// Punkt 7a: Ein Fehler beim Lesen der Kennung schaltet den Schutz von
// Punkt 2 der ersten Nachbesserung nicht still ab: Der Export bricht ab,
// bevor er etwas schreibt, verify meldet den Fehler.
func TestT3N2DataDirIDErrorStopsExport(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "audit-export")
	now := time.Now().UTC()
	orig := t3nOpen(t, filepath.Join(base, "orig", "postkauz.db"))
	t3nEvent(t, orig, now.Add(-time.Minute), "sync.run")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	cp := t3nRestoredCopy(t, orig, base, now)
	t3nEvent(t, orig, now, "keys.list")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	today := filepath.Join(dir, auditFileName(now.Format("2006-01-02")))
	before, _ := os.ReadFile(today)
	manifestBefore, _ := os.ReadFile(filepath.Join(dir, auditManifestName()))
	t3nEvent(t, cp, now, "accounts.list")
	if _, err := t3nExport(t3n2IDErr{cp}, dir); err == nil {
		t.Error("export with a failing id lookup ran")
	}
	after, _ := os.ReadFile(today)
	manifestAfter, _ := os.ReadFile(filepath.Join(dir, auditManifestName()))
	if !bytes.Equal(before, after) || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Error("the copy wrote into the original's folder")
	}
	if _, err := AuditVerify(t3n2IDErr{orig}).Handler(context.Background(), Call{Input: json.RawMessage(`{}`)}); err == nil {
		t.Error("verify with a failing id lookup ran")
	}
	// Ohne Kennung (Schema vor 8, store.ErrNoDataDirID) exportiert er wie
	// bisher ohne Vergleich.
	if _, err := t3nExport(t3n2NoID{orig}, dir); err != nil {
		t.Errorf("export without an id = %v", err)
	}
}

// t3n2FailPending lässt den Export vor der Datei scheitern: Der Merker
// steht schon, die Datei wird nicht mehr geschrieben (wie Strg+C genau
// dazwischen). Auf Wunsch scheitert er nach der Datei (t3nFailSaveRepo).
type t3n2FailPending struct {
	*t3nFailSaveRepo
	failPending bool
}

func (r *t3n2FailPending) AddAuditExportPending(ctx context.Context, p store.AuditExportPending) error {
	if err := r.SQLite.AddAuditExportPending(ctx, p); err != nil {
		return err
	}
	if r.failPending {
		return errors.New("simulated abort after the marker, before the file")
	}
	return nil
}

// Punkt 7d (ersetzt TestT3NAuditExportTwoAborts, Festlegung 2 der ersten
// Nachbesserung): zwei Abbrüche hintereinander, der erste zwischen Datei und
// Nachweis, der zweite nach seinem Merker, aber vor dem Schreiben der Datei.
// Der neue Merker ersetzt den alten nicht: Die Datei des ersten bleibt eine
// eigene, der nächste Export schließt ab.
func TestT3N2TwoAbortsSecondBeforeFile(t *testing.T) {
	e := newAdminEnv(t)
	repo := &t3n2FailPending{t3nFailSaveRepo: &t3nFailSaveRepo{SQLite: e.st}}
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := t3nExport(repo, e.dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(e.dir, auditFileName(time.Now().UTC().Format("2006-01-02")))
	repo.fail = true // 1. Abbruch nach der Datei
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := t3nExport(repo, e.dir); err == nil {
		t.Fatal("abort 1 did not happen")
	}
	first, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	repo.fail, repo.failPending = false, true // 2. Abbruch vor der Datei
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := t3nExport(repo, e.dir); err == nil {
		t.Fatal("abort 2 did not happen")
	}
	if second, _ := os.ReadFile(file); !bytes.Equal(first, second) {
		t.Fatal("abort 2 changed the file")
	}
	if pend, _ := e.st.AuditExportPending(context.Background()); len(pend) != 2 {
		t.Errorf("markers after two aborts = %+v, want both", pend)
	}
	if v := t3nVerify(t, repo); v.Status != "ok" || len(v.Incomplete) != 1 {
		t.Errorf("verify after two aborts = %+v (want ok with the file of abort 1 as incomplete)", v)
	}
	repo.failPending = false
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := t3nExport(repo, e.dir); err != nil {
		t.Fatalf("export after two aborts = %v", err)
	}
	if v := t3nVerify(t, repo); v.Status != "ok" || len(v.Incomplete) != 0 {
		t.Fatalf("verify = %+v", v)
	}
}
