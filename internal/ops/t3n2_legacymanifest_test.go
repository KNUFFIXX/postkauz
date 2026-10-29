package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Zweite Nachbesserung T3, Punkte 3 und 7c: Wem gehört der Exportordner?
// (Wegwerf-Tests A5 und A4 der Prüfsitzung, angepasst.)

// t3n2LegacyManifest schreibt das Manifest am Ort ohne datadir_id (wie vor
// 0.3.0-alpha.7), auf Wunsch mit einem zusätzlichen Tag.
func t3n2LegacyManifest(t *testing.T, dir string, extra ...auditManifestDay) {
	t.Helper()
	mpath := filepath.Join(dir, auditManifestName())
	b, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var m auditManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mpath, buildManifest(append(m.Days, extra...), ""), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Punkt 3: Der erste Export nach dem Update scheitert nicht an der
// Schreibweise des Ordners. Ein Manifest ohne Kennung gilt als eigenes,
// wenn jeder seiner Tage einem Nachweis dieser Datenbank mit gleicher
// Prüfsumme entspricht, gleich unter welcher Schreibweise der Ort steht.
func TestT3N2FirstExportAfterUpdateOtherSpelling(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spell func(t *testing.T, base, dir string) string
	}{
		{"same-string", func(_ *testing.T, _, dir string) string { return dir }},
		{"trailing-slash", func(_ *testing.T, _, dir string) string { return dir + string(os.PathSeparator) }},
		{"dot-segment", func(_ *testing.T, base, _ string) string { return filepath.Join(base, "x", "..") + "/audit-export" }},
		{"other-spelling-same-folder", func(t *testing.T, base, _ string) string {
			// Gleicher Ordner, andere Zeichenkette (unter Windows: Groß- und
			// Kleinschreibung oder neuer Laufwerksbuchstabe von Google Drive).
			l := filepath.Join(base, "H-drive")
			if err := os.Symlink(base, l); err != nil {
				t.Skipf("no symlink: %v", err)
			}
			return filepath.Join(l, "audit-export")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "audit-export")
			now := time.Now().UTC()
			st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
			t3nEvent(t, st, now.Add(-24*time.Hour), "sync.run")
			t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
			if _, err := t3nExport(st, dir); err != nil {
				t.Fatal(err)
			}
			t3n2LegacyManifest(t, dir)
			t3nEvent(t, st, now, "keys.list")
			if _, err := t3nExport(st, tc.spell(t, base, dir)); err != nil {
				t.Fatalf("first export after the update = %v", err)
			}
			if v := t3nVerify(t, st); v.Status != "ok" {
				t.Errorf("verify after the first export = %+v", v)
			}
		})
	}
}

// Punkt 3: Passt ein Manifest ohne Kennung nicht zu den Nachweisen (hier: ein
// Tag, den diese Datenbank nicht kennt) und steht der Ort in anderer
// Schreibweise, bleibt der Export abgewiesen; die Meldung nennt den bisher
// erfassten Ort und den Ausweg. Mit genau diesem Ort geht es.
func TestT3N2LegacyManifestMismatchNamesRecordedLocation(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "audit-export")
	now := time.Now().UTC()
	st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
	t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
	if _, err := t3nExport(st, dir); err != nil {
		t.Fatal(err)
	}
	t3n2LegacyManifest(t, dir, auditManifestDay{Day: "2001-01-01", File: auditFileName("2001-01-01"), Events: 1, FirstEventID: 1,
		LastEventID: 1, SHA256: strings.Repeat("ab", 32), ChainHead: strings.Repeat("cd", 32)})
	l := filepath.Join(base, "H-drive")
	if err := os.Symlink(base, l); err != nil {
		t.Skipf("no symlink: %v", err)
	}
	_, err := t3nExport(st, filepath.Join(l, "audit-export"))
	if !errors.Is(err, ErrAuditExportOtherInstance) {
		t.Fatalf("export with a mismatching legacy manifest under another spelling = %v, want ErrAuditExportOtherInstance", err)
	}
	if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "--to") {
		t.Errorf("the message should name the recorded location %s and the way out: %v", dir, err)
	}
	if _, err := t3nExport(st, dir); err != nil {
		t.Errorf("export to the recorded location = %v", err)
	}
}

// Punkt 7c: Eine Kopie mit --keep-identity (gleiche Kennung) ersetzt im
// Ordner des Originals nichts, solange sie Nachweise des Originals ohne Ort
// hat und an diesem Ort keinen eigenen.
func TestT3N2KeepIdentityCopyDoesNotOverwriteOriginal(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dir := filepath.Join(base, "audit-export")
	now := time.Now().UTC()
	orig := t3nOpen(t, filepath.Join(base, "orig", "postkauz.db"))
	t3nEvent(t, orig, now.Add(-24*time.Hour), "sync.run")
	t3nEvent(t, orig, now.Add(-time.Minute), "sync.run")
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "copy"), 0o700); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(base, "copy", "postkauz.db")
	if err := orig.SnapshotTo(ctx, snap); err != nil {
		t.Fatal(err)
	}
	cp := t3nOpen(t, snap)
	if _, err := cp.PrepareRestored(ctx, store.RestorePrep{DataDirID: "", // --keep-identity
		Event: func(store.RestoreFixes) audit.Event {
			return audit.Event{TS: now, ActorType: audit.ActorSystem, Operation: "backup.restore", Result: audit.ResultOK, ParamsRedacted: []byte("{}")}
		}, Now: now}); err != nil {
		t.Fatal(err)
	}
	t3nEvent(t, orig, now, "keys.list") // das Original arbeitet weiter
	if _, err := t3nExport(orig, dir); err != nil {
		t.Fatal(err)
	}
	today := filepath.Join(dir, auditFileName(now.Format("2006-01-02")))
	before, _ := os.ReadFile(today)
	manifestBefore, _ := os.ReadFile(filepath.Join(dir, auditManifestName()))
	t3nEvent(t, cp, now, "accounts.list")
	if _, err := t3nExport(cp, dir); !errors.Is(err, ErrAuditExportOtherInstance) {
		t.Errorf("keep-identity copy export into the original's folder = %v, want ErrAuditExportOtherInstance", err)
	}
	after, _ := os.ReadFile(today)
	manifestAfter, _ := os.ReadFile(filepath.Join(dir, auditManifestName()))
	if !bytes.Equal(before, after) || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Error("the keep-identity copy changed the original's export")
	}
	if _, err := t3nExport(orig, dir); err != nil {
		t.Errorf("original export afterwards = %v", err)
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
