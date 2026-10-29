package ops

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Dritte Nachbesserung T3, Punkt 5: Wiederhergestellte Kopie (neue Kennung) exportiert erst in einen
// eigenen Ordner X, dann in den Ordner F des Originals, dessen Manifest
// noch ohne Kennung ist (Altbestand). Variante "quiet-day": Der letzte
// Export des Originals lief an einem Tag ohne vorherige Einträge (alle
// Manifest-Tage abgeschlossen). Variante "open-day": Am Exporttag gab es
// vorher schon Einträge (offener Tag im Manifest; Gegenargument). Vorher
// übernahm die Kopie im Fall quiet-day den Ordner (Altbestand-Regel).
func TestT3N3RestoredCopyLegacyManifest(t *testing.T) {
	for _, tc := range []string{"quiet-day", "open-day"} {
		t.Run(tc, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "audit-export")
			now := time.Now().UTC()
			orig := t3nOpen(t, filepath.Join(base, "orig", "postkauz.db"))
			t3nEvent(t, orig, now.Add(-48*time.Hour), "sync.run")
			t3nEvent(t, orig, now.Add(-24*time.Hour), "sync.run")
			if tc == "open-day" {
				t3nEvent(t, orig, now.Add(-time.Minute), "sync.run")
			}
			if _, err := t3nExport(orig, dir); err != nil { // Export des Originals vor dem Update
				t.Fatal(err)
			}
			t3n2LegacyManifest(t, dir)
			t3nEvent(t, orig, now, "audit.export") // Abschluss-Eintrag dieses Exports
			cp := t3nRestoredCopy(t, orig, base, now)
			if _, err := t3nExport(cp, filepath.Join(base, "copy-export")); err != nil {
				t.Fatal(err)
			}
			t3nEvent(t, cp, now, "audit.export")
			_, err := t3nExport(cp, dir)
			if !errors.Is(err, ErrAuditExportOtherInstance) {
				t.Errorf("copy export into the original's legacy folder = %v, want ErrAuditExportOtherInstance", err)
			}
			t3nEvent(t, orig, now, "keys.list")
			if _, err := t3nExport(orig, dir); err != nil {
				t.Errorf("original export afterwards = %v", err)
			}
		})
	}
}
