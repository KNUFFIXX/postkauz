package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// archive legt ein Archiv von audit.rotate im Datenverzeichnis an und trägt
// es mit seiner Prüfsumme in audit_archives ein (wie CommitAuditRotation).
func (e *env) archive(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	if err := e.st.ExecForTest(`INSERT INTO audit_archives(file, from_day, to_day, first_event_id, last_event_id, events, sha256,
		chain_prev, chain_head, created_at) VALUES (?, '2025-01-01', '2025-01-31', 1, 2, 2, ?, 'p', 'h', 1)`,
		name, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
}

// T3-Nachbesserung 3(b) (G1-3b): Eine Archivdatei ohne Eintrag in
// audit_archives (Rest eines hart abgebrochenen audit rotate mit anderem
// Tagesbereich) gehört nicht in die Sicherung.
func TestT3NBackupSkipsUnregisteredAuditArchive(t *testing.T) {
	e := newEnv(t)
	orphan := "audit-archive-2024-01-01-2024-03-31.db"
	if err := os.WriteFile(filepath.Join(e.dir, orphan), []byte("half written"), 0o600); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "backup")
	if _, err := e.r.Create(context.Background(), Options{To: to}); err != nil {
		t.Fatal(err)
	}
	for _, f := range readManifest(t, to).Files {
		if f.Path == orphan {
			t.Fatalf("backup holds the unregistered archive %s (kind %s)", orphan, f.Kind)
		}
	}
}

// T3-Nachbesserung 3(b): Nur eingetragene Archive mit passender Prüfsumme
// kommen in die Sicherung (Ordner, Tar, Trockenlauf); ein verändertes und
// ein fehlendes stehen in audit_archives_skipped, die Sicherung läuft
// weiter. Ein Archiv, das erst nach dem Schnappschuss eingetragen wurde,
// gehört nicht zu diesem Stand.
func TestT3NBackupTakesOnlyRegisteredMatchingArchives(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	good, changed, missing := "audit-archive-2025-01-01-2025-01-31.db", "audit-archive-2025-02-01-2025-02-28.db",
		"audit-archive-2025-03-01-2025-03-31.db"
	e.archive(t, good, "gut")
	e.archive(t, changed, "vorher")
	e.archive(t, missing, "weg")
	if err := os.WriteFile(filepath.Join(e.dir, changed), []byte("nachher"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.dir, missing)); err != nil {
		t.Fatal(err)
	}
	wantSkipped := []string{changed + " (archive_changed)", missing + " (archive_missing)"}
	late := "audit-archive-2025-04-01-2025-04-30.db"
	e.r.hook = func(stage string) {
		if stage == "snapshot" {
			e.archive(t, late, "spaet")
		}
	}

	to := filepath.Join(t.TempDir(), "backup")
	res, err := e.r.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatal(err)
	}
	if res.AuditArchives != 1 || !slices.Equal(res.AuditArchivesSkipped, wantSkipped) {
		t.Fatalf("folder: archives %d, skipped %v", res.AuditArchives, res.AuditArchivesSkipped)
	}
	var paths []string
	for _, f := range readManifest(t, to).Files {
		if f.Kind == KindAuditArchive {
			paths = append(paths, f.Path)
		}
	}
	if !slices.Equal(paths, []string{good}) {
		t.Fatalf("folder: archives in the manifest = %v, want only %s", paths, good)
	}
	if !strings.Contains(lastAudit(t, e.st, OpDone), `"audit_archives_skipped":2`) {
		t.Fatalf("backup.done without the skipped count: %s", lastAudit(t, e.st, OpDone))
	}

	// Tar: das späte Archiv ist jetzt eingetragen und kommt mit.
	e.r.hook = nil
	var buf bytes.Buffer
	res, err = e.r.Create(ctx, Options{Tar: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if res.AuditArchives != 2 || !slices.Equal(res.AuditArchivesSkipped, wantSkipped) {
		t.Fatalf("tar: archives %d, skipped %v", res.AuditArchives, res.AuditArchivesSkipped)
	}
	tr := tar.NewReader(&buf)
	var inTar []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if archiveRE.MatchString(h.Name) {
			inTar = append(inTar, h.Name)
		}
	}
	if !slices.Equal(inTar, []string{good, late}) {
		t.Fatalf("tar: archives = %v", inTar)
	}

	dry, err := e.r.Create(ctx, Options{To: filepath.Join(t.TempDir(), "dry"), DryRun: true})
	if err != nil || dry.AuditArchives != 2 || !slices.Equal(dry.AuditArchivesSkipped, wantSkipped) {
		t.Fatalf("dry run: archives %d, skipped %v, err %v", dry.AuditArchives, dry.AuditArchivesSkipped, err)
	}
}
