package ops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Zweite Nachbesserung T3: audit verify und die Merker abgebrochener
// Exporte (Punkte 6 und 7b; Wegwerf-Tests der Prüfsitzung, angepasst).

// Punkt 6: Ein Merker steht, die Datei wurde danach verändert. verify meldet
// file_changed, auch für einen Tag ohne Nachweis (new-day); der nächste
// Export überschreibt die veränderte Datei nicht.
func TestT3N2MarkerThenFileChanged(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded bool
	}{{"recorded-day", true}, {"new-day", false}} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "audit-export")
			now := time.Now().UTC()
			st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
			repo := &t3nFailSaveRepo{SQLite: st}
			t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
			if tc.recorded {
				if _, err := t3nExport(repo, dir); err != nil {
					t.Fatal(err)
				}
			}
			t3nEvent(t, st, now, "keys.list")
			repo.fail = true
			if _, err := t3nExport(repo, dir); err == nil {
				t.Fatal("simulated abort did not happen")
			}
			repo.fail = false
			if pend, _ := st.AuditExportPending(context.Background()); len(pend) != 1 {
				t.Fatalf("markers after the abort = %+v", pend)
			}
			file := filepath.Join(dir, auditFileName(now.Format("2006-01-02")))
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			// Die erste Zeile entfernen.
			_, rest, _ := bytes.Cut(b, []byte{'\n'})
			if err := os.WriteFile(file, rest, 0o600); err != nil {
				t.Fatal(err)
			}
			v := t3nVerify(t, st)
			changed := 0
			for _, p := range v.Problems {
				if p.Problem == AuditFileChanged && p.File == file {
					changed++
				}
			}
			if v.Status != "mismatch" || changed != 1 || len(v.Incomplete) != 0 {
				t.Errorf("verify after the file of an aborted export was changed = %+v (want exactly one file_changed)", v)
			}
			// Der Beleg bleibt: Der Export ersetzt die Datei nicht.
			_, xerr := t3nExport(st, dir)
			if !errors.Is(xerr, ErrAuditExportForeign) && !errors.Is(xerr, ErrAuditExportChanged) {
				t.Errorf("next export = %v, want a refusal", xerr)
			}
			if after, _ := os.ReadFile(file); !bytes.Equal(after, rest) {
				t.Error("the export replaced the changed file")
			}
		})
	}
}

// Bestehender Schutz (Prüfsitzung A2b): Wer nur die Datenbank ändern kann,
// ändert eine Zeile, passt den Nachweis an und legt einen Merker mit der
// Prüfsumme der (lesbaren) Exportdatei an. verify meldet das trotzdem.
func TestT3N2DBOnlyAttackerWithMarker(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dir := filepath.Join(base, "audit-export")
	now := time.Now().UTC()
	st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
	t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
	t3nEvent(t, st, now, "keys.list")
	if _, err := t3nExport(st, dir); err != nil {
		t.Fatal(err)
	}
	day := now.Format("2006-01-02")
	fileSum, err := fileSHA256(filepath.Join(dir, auditFileName(day)))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TRIGGER audit_events_no_update`, `UPDATE audit_events SET operation = 'harmless' WHERE id = 2`} {
		if err := st.ExecForTest(q); err != nil {
			t.Fatal(err)
		}
	}
	evs, err := st.AuditQuery(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := auditDayContent(evs, 0, 2, auditChainGenesis)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`UPDATE audit_exports SET sha256 = ? WHERE day = ?`, sha256Hex(d.content), day); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAuditExportPending(ctx, store.AuditExportPending{Day: day, Location: dir, SHA256: fileSum}); err != nil {
		t.Fatal(err)
	}
	if v := t3nVerify(t, st); v.Status == "ok" {
		t.Errorf("a database-only change with a forged marker stays unnoticed: %+v", v)
	}
}

// Punkt 7b: Der Merker eines aufgegebenen Ordners bleibt nicht für immer
// stehen. Der Nachweis desselben Tages an einem anderen Ort löscht ihn, und
// verify übergeht einen solchen Merker ohnehin (Altbestand).
func TestT3N2MarkerOfAbandonedFolder(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dirA := filepath.Join(base, "export-a")
	dirB := filepath.Join(base, "export-b")
	now := time.Now().UTC()
	st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
	repo := &t3nFailSaveRepo{SQLite: st}
	t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
	if _, err := t3nExport(repo, dirA); err != nil {
		t.Fatal(err)
	}
	t3nEvent(t, st, now, "keys.list")
	repo.fail = true
	if _, err := t3nExport(repo, dirA); err == nil {
		t.Fatal("simulated abort did not happen")
	}
	repo.fail = false
	marker, err := st.AuditExportPending(ctx)
	if err != nil || len(marker) != 1 {
		t.Fatalf("marker after the abort = %+v, %v", marker, err)
	}
	// Der Betreiber wechselt den Ordner (z. B. neues Laufwerk).
	t3nEvent(t, st, now, "keys.list")
	if _, err := t3nExport(st, dirB); err != nil {
		t.Fatal(err)
	}
	if pend, _ := st.AuditExportPending(ctx); len(pend) != 0 {
		t.Errorf("markers after the record of the same day in another folder = %+v", pend)
	}
	if v := t3nVerify(t, st); v.Status != "ok" || len(v.Incomplete) != 0 {
		t.Errorf("verify after the change of folder = %+v", v)
	}
	// Altbestand: ein Merker im aufgegebenen Ordner, den eine frühere
	// Version stehen ließ. verify übergeht ihn.
	if err := st.AddAuditExportPending(ctx, marker[0]); err != nil {
		t.Fatal(err)
	}
	if v := t3nVerify(t, st); v.Status != "ok" || len(v.Incomplete) != 0 {
		t.Errorf("verify with a marker of the abandoned folder = %+v", v)
	}
}
