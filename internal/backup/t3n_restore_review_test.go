package backup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// T3-Nachbesserung (Wenn Zeit): restore liefert je Konto Server und
// Benutzer und die Pfade der Stildateien zur Sichtprüfung, ohne
// Geheimnisse (kein Schlüsselbund-Verweis, kein Passwort).
func TestT3NRestoreListsAccountsAndStyleFiles(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	style := filepath.Join(t.TempDir(), "stil.md")
	if _, err := e.st.AddIdentity(ctx, store.Identity{AccountID: 1, Name: "Büro", Email: "demo@example.org", StyleFile: style}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddIdentity(ctx, store.Identity{AccountID: 1, Name: "Ohne", Email: "demo@example.org"}); err != nil {
		t.Fatal(err)
	}
	from := backupOf(t, e)
	res, err := Restore(ctx, RestoreOptions{From: from, DataDir: filepath.Join(t.TempDir(), "neu")})
	if err != nil {
		t.Fatal(err)
	}
	want := RestoredAccount{Name: "demo", Host: "127.0.0.1", Port: 143, TLS: "none", Username: "u", AuthType: "password"}
	if len(res.Accounts) != 1 || res.Accounts[0] != want {
		t.Fatalf("accounts = %+v, want [%+v]", res.Accounts, want)
	}
	if len(res.StyleFiles) != 1 || res.StyleFiles[0] != (RestoredStyle{Account: "demo", Identity: "Büro", Path: style}) {
		t.Fatalf("style files = %+v", res.StyleFiles)
	}
	// Trockenlauf: nichts geöffnet, nichts gelistet.
	dry, err := Restore(ctx, RestoreOptions{From: from, DataDir: filepath.Join(t.TempDir(), "dry"), DryRun: true})
	if err != nil || dry.Accounts != nil || dry.StyleFiles != nil {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
}
