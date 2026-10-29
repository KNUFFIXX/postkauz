package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// T3-Nachbesserung 2, Punkt 4: Ein Blob, der schon vor der Sicherung
// fehlte, blockiert backup create nicht mehr: Exit 0, Zeile missing: und
// eine Warnung mit dem Hash auf stderr, missing_blobs in --json. restore
// übernimmt den Zustand: Zeile missing:, doctor --blobs nennt die Zeile
// ohne Datei, die Fehlermeldung erklärt, dass er schon beim Sichern fehlte.
func TestT3N2BackupAndRestoreWithBlobMissingBeforehand(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %s", errs)
	}
	h := sha256.Sum256([]byte("verlorene Mail"))
	sum := hex.EncodeToString(h[:])
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(context.Background(), store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "u", AuthType: "password", CredentialRef: "account/demo"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO blobs(sha256, size, created_at) VALUES (?, 14, 1)`, sum); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO messages(account_id, subject, blob_sha256, created_at) VALUES (1, 'x', ?, 1)`, sum); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	backupDir := filepath.Join(t.TempDir(), "b")
	code, out, errs := run(t, "backup", "create", "--to", backupDir)
	if code != ExitOK || !strings.Contains(out, "  missing:   1 blob already missing before the backup") ||
		!strings.Contains(errs, "warning: 1 blob the database refers to was already missing from the blob store before this backup") ||
		!strings.Contains(errs, sum) {
		t.Fatalf("backup with a blob missing beforehand: %d\n%s\n%s", code, out, errs)
	}
	code, out, errs = run(t, "backup", "create", "--to", backupDir, "--json")
	var res struct {
		MissingBlobs []string `json:"missing_blobs"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &res) != nil || !slices.Equal(res.MissingBlobs, []string{sum}) {
		t.Fatalf("second backup --json: %d\n%s\n%s", code, out, errs)
	}

	target := filepath.Join(t.TempDir(), "neu")
	code, out, errs = run(t, "restore", "--from", backupDir, "--data-dir", target, "--dry-run")
	if code != ExitOK || !strings.Contains(out, "missing:   1 blob was already missing when the backup was made") {
		t.Fatalf("restore --dry-run: %d\n%s\n%s", code, out, errs)
	}
	code, out, errs = run(t, "restore", "--from", backupDir, "--data-dir", target)
	if code != ExitError || !strings.Contains(out, "missing:   1 blob was already missing when the backup was made") ||
		!strings.Contains(out, "rows without file: "+sum) || !strings.Contains(errs, "1 blob was already missing when the backup was made") {
		t.Fatalf("restore: %d\n%s\n%s", code, out, errs)
	}
	if b, _ := doctorBlobsOf(t, target); b.RowsWithoutFile != 1 || !slices.Equal(b.Examples["rows_without_file"], []string{sum}) {
		t.Fatalf("doctor --blobs of the copy: %+v", b)
	}
}
