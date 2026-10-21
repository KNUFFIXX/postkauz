package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// keyringRefs liefert die Verweise im Test-Schlüsselbund.
func keyringRefs(kr *memKeyring) map[string]string {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	out := map[string]string{}
	for k, v := range kr.m {
		out[k] = v.Reveal()
	}
	return out
}

func addDemo(t *testing.T, password string) {
	t.Helper()
	code, out, errs := runIn(t, password+"\n", "account", "add", "demo", "--imap", "127.0.0.1:11143", "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin", "--json")
	if code != ExitOK {
		t.Fatalf("account add: code=%d out=%s err=%s", code, out, errs)
	}
}

func openDB(t *testing.T, dataDir string) *store.SQLite {
	t.Helper()
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// S2.0/A2: Zwei Datenverzeichnisse mit gleichem Kontonamen haben getrennte
// Einträge im (gemeinsamen) Schlüsselbund; jedes liest sein eigenes Passwort.
func TestTwoDataDirsSameAccountNameSeparateKeyringEntries(t *testing.T) {
	dataA, kr := setupEnv(t)
	addDemo(t, "pw-of-a")
	dataB := filepath.Join(filepath.Dir(dataA), "data-b")
	t.Setenv("POSTKAUZ_DATA_DIR", dataB)
	addDemo(t, "pw-of-b")

	refs := keyringRefs(kr)
	if len(refs) != 2 {
		t.Fatalf("keyring = %v, want two entries", refs)
	}
	for dir, pw := range map[string]string{dataA: "pw-of-a", dataB: "pw-of-b"} {
		a, err := openDB(t, dir).AccountByName(t.Context(), "demo")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(a.CredentialRef, "account/") || strings.Count(a.CredentialRef, "/") != 2 || refs[a.CredentialRef] != pw {
			t.Fatalf("%s: ref %s holds %q, want %q (keyring %v)", dir, a.CredentialRef, refs[a.CredentialRef], pw, refs)
		}
	}
	// Konto in B entfernen: Der Eintrag von A bleibt.
	if code, out, errs := run(t, "account", "remove", "demo"); code != ExitOK {
		t.Fatalf("remove: %d %s %s", code, out, errs)
	}
	if refs := keyringRefs(kr); len(refs) != 1 {
		t.Fatalf("after remove in B: %v", refs)
	}
	t.Setenv("POSTKAUZ_DATA_DIR", dataA)
	if rep := doctorJSON(t); len(rep.Accounts) != 1 || rep.Accounts[0].Password != "stored" {
		t.Fatalf("doctor in A: %+v", rep.Accounts)
	}
}

// Umzug beim ersten Öffnen: Ein Konto mit altem Verweis (account/demo, wie
// bis S1) wird beim nächsten Kommando umgezogen; doctor meldet vorher den
// alten Verweis und danach nichts mehr.
func TestLegacyKeyringEntryMovedOnOpen(t *testing.T) {
	dataDir, kr := setupEnv(t)
	addDemo(t, canary)
	st := openDB(t, dataDir)
	newRef := ""
	if a, err := st.AccountByName(t.Context(), "demo"); err == nil {
		newRef = a.CredentialRef
	}
	// Zustand einer Datenbank aus S1 nach Migration 0008: alter Verweis,
	// Eintrag unter dem alten Namen.
	if err := st.ExecForTest(`UPDATE accounts SET credential_ref = 'account/demo'`); err != nil {
		t.Fatal(err)
	}
	_ = kr.Delete(newRef)
	_ = kr.Set("account/demo", canary)

	rep := doctorJSON(t)
	if !hasWarning(rep, "still uses the old keyring entry account/demo") {
		t.Fatalf("doctor before move: %v", rep.Warnings)
	}
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK || errs != "" {
		t.Fatalf("accounts list: %d %q", code, errs)
	}
	if refs := keyringRefs(kr); len(refs) != 1 || refs[newRef] != canary {
		t.Fatalf("keyring after move = %v", refs)
	}
	if a, _ := st.AccountByName(t.Context(), "demo"); a.CredentialRef != newRef {
		t.Fatalf("ref = %s, want %s", a.CredentialRef, newRef)
	}
	rep = doctorJSON(t)
	if hasWarning(rep, "keyring entry") || rep.Accounts[0].Password != "stored" {
		t.Fatalf("doctor after move: %+v %v", rep.Accounts, rep.Warnings)
	}
}

// Scheitert beim Umzug das Löschen des alten Eintrags, bleibt er stehen,
// das Kommando läuft trotzdem, und doctor warnt, solange er besteht.
func TestLegacyKeyringEntryLeftDoctorWarns(t *testing.T) {
	dataDir, kr := setupEnv(t)
	addDemo(t, canary)
	st := openDB(t, dataDir)
	a, _ := st.AccountByName(t.Context(), "demo")
	newRef := a.CredentialRef
	if err := st.ExecForTest(`UPDATE accounts SET credential_ref = 'account/demo'`); err != nil {
		t.Fatal(err)
	}
	_ = kr.Delete(newRef)
	_ = kr.Set("account/demo", canary)
	orig := openSecrets
	openSecrets = func(string, string) (secrets.Store, error) { return secrets.WithEnv(stuckKeyring{kr}), nil }
	t.Cleanup(func() { openSecrets = orig })

	code, _, errs := run(t, "accounts", "list")
	if code != ExitOK || !strings.Contains(errs, "old entry account/demo could not be deleted") {
		t.Fatalf("accounts list: %d %q", code, errs)
	}
	if strings.Contains(errs, canary) {
		t.Fatal("password in the warning")
	}
	if refs := keyringRefs(kr); len(refs) != 2 || refs["account/demo"] != canary || refs[newRef] != canary {
		t.Fatalf("keyring = %v", refs)
	}
	rep := doctorJSON(t)
	if !hasWarning(rep, "old keyring entry account/demo is still there") || rep.Accounts[0].Password != "stored" {
		t.Fatalf("doctor: %+v %v", rep.Accounts, rep.Warnings)
	}
	// Löscht der Mensch den alten Eintrag, verschwindet die Warnung.
	_ = kr.Delete("account/demo")
	if rep := doctorJSON(t); hasWarning(rep, "keyring entry") {
		t.Fatalf("doctor after manual delete: %v", rep.Warnings)
	}
}

// stuckKeyring: Löschen scheitert (z. B. fehlende Rechte am Eintrag).
type stuckKeyring struct{ *memKeyring }

func (stuckKeyring) Delete(string) error { return errors.New("access denied (test)") }

func hasWarning(rep DoctorReport, part string) bool {
	for _, w := range rep.Warnings {
		if strings.Contains(w, part) {
			return true
		}
	}
	return false
}
