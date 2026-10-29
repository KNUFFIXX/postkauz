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

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T3, Punkte 1 und 2 (Regressionstests aus der Prüfsitzung,
// auftraege/A03_T3_nachbesserung.md).

// t3nFailSaveRepo lässt den Nachweis scheitern, nachdem die Tagesdatei
// geschrieben ist (wie Strg+C oder SQLITE_BUSY zwischen Datei und Nachweis).
type t3nFailSaveRepo struct {
	*store.SQLite
	fail bool
}

func (r *t3nFailSaveRepo) SaveAuditExport(ctx context.Context, e store.AuditExport) error {
	if r.fail {
		return errors.New("simulated abort between file and record")
	}
	return r.SQLite.SaveAuditExport(ctx, e)
}

func t3nExport(repo AuditRepo, dir string) (AuditExportOutput, error) {
	res, err := AuditExport(repo, dir).Handler(context.Background(), Call{Input: json.RawMessage(`{}`)})
	if err != nil {
		return AuditExportOutput{}, err
	}
	return res.(AuditExportOutput), nil
}

func t3nVerify(t *testing.T, repo AuditRepo) AuditVerifyOutput {
	t.Helper()
	res, err := AuditVerify(repo).Handler(context.Background(), Call{Input: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	return res.(AuditVerifyOutput)
}

// Punkt 1: Ein Export, der zwischen Tagesdatei und Nachweis abbricht, sperrt
// folgende Exporte nicht und hinterlässt kein file_changed in verify.
func TestT3NAuditExportAbortBetweenFileAndRecord(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firstExport bool // Tag hat schon einen Nachweis (Export lief heute schon)
	}{{"recorded-day", true}, {"new-day", false}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newAdminEnv(t)
			repo := &t3nFailSaveRepo{SQLite: e.st}
			e.run(t, adminKey, "keys.list", map[string]any{})
			if tc.firstExport {
				if _, err := t3nExport(repo, e.dir); err != nil {
					t.Fatal(err)
				}
			}
			e.run(t, adminKey, "keys.list", map[string]any{}) // neuer Eintrag heute
			repo.fail = true
			if _, err := t3nExport(repo, e.dir); err == nil {
				t.Fatal("simulated abort did not happen")
			}
			repo.fail = false
			pend, err := e.st.AuditExportPending(context.Background())
			if err != nil || len(pend) != 1 {
				t.Fatalf("pending marker after the abort = %+v, %v", pend, err)
			}
			v := t3nVerify(t, repo)
			if v.Status != "ok" {
				t.Errorf("verify right after the aborted export = %+v", v)
			}
			if tc.firstExport && (len(v.Incomplete) != 1 || v.Incomplete[0].Problem != AuditExportIncomplete) {
				t.Errorf("verify should name the incomplete export: %+v", v.Incomplete)
			}
			// Wie im Betrieb: Der abgebrochene Aufruf schreibt seinen
			// Abschluss-Eintrag (error) in den offenen Tag, der Inhalt ändert
			// sich also vor dem nächsten Export.
			e.run(t, adminKey, "keys.list", map[string]any{})
			if _, err := t3nExport(repo, e.dir); err != nil {
				t.Fatalf("export after an aborted export = %v", err)
			}
			if v := t3nVerify(t, repo); v.Status != "ok" || len(v.Incomplete) != 0 {
				t.Fatalf("verify after recovery = %+v", v)
			}
			if pend, _ := e.st.AuditExportPending(context.Background()); len(pend) != 0 {
				t.Fatalf("marker left after a complete export: %+v", pend)
			}
		})
	}
}

// Punkt 1: Zwei Abbrüche hintereinander (der zweite vor dem Schreiben der
// Datei): Die Datei des ersten bleibt über ihren Merker eine eigene.
func TestT3NAuditExportTwoAborts(t *testing.T) {
	e := newAdminEnv(t)
	repo := &t3nFailSaveRepo{SQLite: e.st}
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := t3nExport(repo, e.dir); err != nil {
		t.Fatal(err)
	}
	repo.fail = true
	for range 2 {
		e.run(t, adminKey, "keys.list", map[string]any{})
		if _, err := t3nExport(repo, e.dir); err == nil {
			t.Fatal("simulated abort did not happen")
		}
	}
	repo.fail = false
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := t3nExport(repo, e.dir); err != nil {
		t.Fatalf("export after two aborts = %v", err)
	}
	if v := t3nVerify(t, repo); v.Status != "ok" {
		t.Fatalf("verify = %+v", v)
	}
}

// Punkt 1d: Ein zweiter Export desselben Datenverzeichnisses bricht mit
// ErrAuditExportRunning ab, solange der erste läuft.
func TestT3NAuditExportLock(t *testing.T) {
	st := openTestStore(t)
	dir := filepath.Join(t.TempDir(), "export")
	l, err := flock.TryLock(filepath.Join(filepath.Dir(st.Path()), "audit-export.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := t3nExport(st, dir); !errors.Is(err, ErrAuditExportRunning) {
		t.Fatalf("export while another runs = %v, want ErrAuditExportRunning", err)
	}
	// Der Trockenlauf schreibt nichts und sperrt nicht.
	if _, err := AuditExport(st, dir).Handler(context.Background(), Call{Input: json.RawMessage(`{}`), DryRun: true}); err != nil {
		t.Fatalf("dry run while another export runs = %v", err)
	}
	l.Release()
	if _, err := t3nExport(st, dir); err != nil {
		t.Fatalf("export after the other one ended = %v", err)
	}
}

// t3nOpen öffnet eine Datenbankdatei unter p.
func t3nOpen(t *testing.T, p string) *store.SQLite {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	st, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func t3nEvent(t *testing.T, st *store.SQLite, ts time.Time, op string) {
	t.Helper()
	if err := st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'system', ?, 'ok')`, ts.UnixMilli(), op); err != nil {
		t.Fatal(err)
	}
}

// t3nRestoredCopy legt eine wiederhergestellte Kopie von orig an (wie
// backup/restore: Schnappschuss, PrepareRestored mit neuer Kennung).
func t3nRestoredCopy(t *testing.T, orig *store.SQLite, base string, now time.Time) *store.SQLite {
	t.Helper()
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(base, "copy"), 0o700); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(base, "copy", "postkauz.db")
	if err := orig.SnapshotTo(ctx, snap); err != nil {
		t.Fatal(err)
	}
	cp := t3nOpen(t, snap)
	if _, err := cp.PrepareRestored(ctx, store.RestorePrep{DataDirID: "0123456789abcdef0123456789abcdef",
		CredentialRef: func(id8, a string) string { return "account/" + id8 + "/" + a },
		Event: func(store.RestoreFixes) audit.Event {
			return audit.Event{TS: now, ActorType: audit.ActorSystem, Operation: "backup.restore", Result: audit.ResultOK, ParamsRedacted: []byte("{}")}
		},
		Now: now}); err != nil {
		t.Fatal(err)
	}
	return cp
}

// Punkt 2: Eine wiederhergestellte Kopie, deren Export in den Ordner des
// Originals zeigt, ersetzt dort keine Tagesdatei und kein Manifest; das
// Original exportiert danach weiter.
func TestT3NRestoredCopyDoesNotOverwriteOriginalExport(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "audit-export")
	now := time.Now().UTC()
	orig := t3nOpen(t, filepath.Join(base, "orig", "postkauz.db"))
	t3nEvent(t, orig, now.Add(-24*time.Hour), "sync.run")
	t3nEvent(t, orig, now.Add(-time.Minute), "sync.run")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	cp := t3nRestoredCopy(t, orig, base, now)
	// Das Original arbeitet weiter und exportiert.
	t3nEvent(t, orig, now, "keys.list")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	today := filepath.Join(dir, auditFileName(now.Format("2006-01-02")))
	before, _ := os.ReadFile(today)
	manifestBefore, _ := os.ReadFile(filepath.Join(dir, auditManifestName()))
	// Die Kopie exportiert in denselben Ordner.
	t3nEvent(t, cp, now, "accounts.list")
	if _, err := t3nExport(cp, dir); !errors.Is(err, ErrAuditExportOtherInstance) {
		t.Errorf("copy export into the original's folder = %v, want ErrAuditExportOtherInstance", err)
	}
	after, _ := os.ReadFile(today)
	manifestAfter, _ := os.ReadFile(filepath.Join(dir, auditManifestName()))
	if !bytes.Equal(before, after) || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Error("the restored copy changed the original's export")
	}
	if _, err := t3nExport(orig, dir); err != nil {
		t.Errorf("original export after the copy's attempt = %v", err)
	}
	if v := t3nVerify(t, orig); v.Status != "ok" {
		t.Errorf("verify of the original = %+v", v)
	}
	// In einen eigenen Ordner exportiert die Kopie wie gewohnt.
	if _, err := t3nExport(cp, filepath.Join(base, "copy-export")); err != nil {
		t.Errorf("copy export into its own folder = %v", err)
	}
	if v := t3nVerify(t, cp); v.Status != "ok" {
		t.Errorf("verify of the copy = %+v", v)
	}
}

// Punkt 2: Ein Manifest ohne Kennung (vor 0.3.0-alpha.7) gilt nur für eine
// Datenbank mit Nachweis an diesem Ort; verify nimmt es bis zum nächsten
// Export hin, der es mit Kennung neu schreibt.
func TestT3NLegacyManifestWithoutDataDirID(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "audit-export")
	now := time.Now().UTC()
	orig := t3nOpen(t, filepath.Join(base, "orig", "postkauz.db"))
	t3nEvent(t, orig, now.Add(-24*time.Hour), "sync.run")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	// Manifest wie vor alpha.7: ohne datadir_id.
	mpath := filepath.Join(dir, auditManifestName())
	b, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var m auditManifest
	if err := json.Unmarshal(b, &m); err != nil || m.DataDirID == "" {
		t.Fatalf("manifest without datadir_id after export: %v %q", err, m.DataDirID)
	}
	if err := os.WriteFile(mpath, buildManifest(m.Days, ""), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := t3nVerify(t, orig); v.Status != "ok" {
		t.Fatalf("verify with a legacy manifest = %+v", v)
	}
	cp := t3nRestoredCopy(t, orig, base, now)
	if _, err := t3nExport(cp, dir); !errors.Is(err, ErrAuditExportOtherInstance) {
		t.Errorf("copy export with a legacy manifest = %v, want ErrAuditExportOtherInstance", err)
	}
	t3nEvent(t, orig, now, "keys.list")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatalf("original export with a legacy manifest = %v", err)
	}
	b, _ = os.ReadFile(mpath)
	if err := json.Unmarshal(b, &m); err != nil || m.DataDirID == "" {
		t.Errorf("the export did not add datadir_id: %v %q", err, m.DataDirID)
	}
}
