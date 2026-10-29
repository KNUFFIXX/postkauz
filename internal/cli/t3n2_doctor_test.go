package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Zweite Nachbesserung T3, Punkt 14: Während audit rotate läuft (Sperre
// audit-rotate.lock gehalten), rät doctor nicht, die noch nicht
// eingetragene Archivdatei wegzuschieben, sondern später erneut zu prüfen.
// Ohne laufende Rotation bleibt der Rat.
func TestT3N2DoctorDuringRotate(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := store.AuditArchivePrefix + "2025-01-01-2025-01-31.db"
	if err := os.WriteFile(filepath.Join(dataDir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ohne Sperrdatei: Rest eines abgebrochenen Laufs, doctor legt keine
	// Sperrdatei an.
	var rep DoctorReport
	doctorArchiveFiles(context.Background(), st, &rep)
	if len(rep.AuditArchivesUnregistered) != 1 || rep.AuditRotateRunning || !strings.Contains(strings.Join(rep.Warnings, "\n"), "move them out") {
		t.Fatalf("doctor without a running rotation = %+v %v", rep.AuditArchivesUnregistered, rep.Warnings)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "audit-rotate.lock")); err == nil {
		t.Error("doctor created the lock file of audit rotate")
	}

	l, err := flock.TryLock(filepath.Join(dataDir, "audit-rotate.lock"))
	if err != nil {
		t.Fatal(err)
	}
	rep = DoctorReport{}
	doctorArchiveFiles(context.Background(), st, &rep)
	all := strings.Join(rep.Warnings, "\n")
	if strings.Contains(all, "move them out") {
		t.Errorf("doctor advises to move the archive file while audit rotate runs: %s", all)
	}
	if !rep.AuditRotateRunning || len(rep.AuditArchivesUnregistered) != 0 || !strings.Contains(all, "check again later") || !strings.Contains(all, name) {
		t.Errorf("doctor during audit rotate = running %v, unregistered %v, warnings %v", rep.AuditRotateRunning, rep.AuditArchivesUnregistered, rep.Warnings)
	}

	// Nach der Rotation (Sperre frei, Datei liegt noch ohne Eintrag): wieder
	// der Rat zum Verschieben.
	l.Release()
	rep = DoctorReport{}
	doctorArchiveFiles(context.Background(), st, &rep)
	if rep.AuditRotateRunning || len(rep.AuditArchivesUnregistered) != 1 {
		t.Errorf("doctor after audit rotate = running %v, unregistered %v", rep.AuditRotateRunning, rep.AuditArchivesUnregistered)
	}
}
