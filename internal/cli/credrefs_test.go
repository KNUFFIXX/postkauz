package cli

import (
	"encoding/json"
	"errors"
	"os"
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

// Ohne erreichbaren Schlüsselbund zieht nichts um, das Kommando läuft, und
// doctor sagt, dass der Umzug wartet und die Umgebungsvariable gilt.
func TestLegacyKeyringEntryPostponedWithoutKeyring(t *testing.T) {
	dataDir, _ := setupEnv(t)
	addDemo(t, canary)
	st := openDB(t, dataDir)
	if err := st.ExecForTest(`UPDATE accounts SET credential_ref = 'account/demo'`); err != nil {
		t.Fatal(err)
	}
	orig := openSecrets
	openSecrets = func(string, string) (secrets.Store, error) {
		return secrets.WithEnv(secrets.Unavailable{Reason: "test"}), nil
	}
	t.Cleanup(func() { openSecrets = orig })
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK || errs != "" {
		t.Fatalf("accounts list: %d %q", code, errs)
	}
	if a, _ := st.AccountByName(t.Context(), "demo"); a.CredentialRef != "account/demo" {
		t.Fatalf("ref switched without keyring: %s", a.CredentialRef)
	}
	if rep := doctorJSON(t); !hasWarning(rep, "once the OS keyring is reachable (POSTKAUZ_PASSWORD_DEMO is not affected)") {
		t.Fatalf("doctor: %v", rep.Warnings)
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

// Gegenprüfung 21.10.2026, Befund 1: doctor auf einem Bestand mit Schema 7
// (0.1.0-rc1, noch nicht migriert) meldete „accounts: … no such column:
// aliases". Jetzt liest doctor die Konten ohne die neuen Spalten, meldet
// needs_migration mit Hinweis und warnt für den alten Verweis; das nächste
// Kommando migriert und zieht um.
func TestDoctorOnSchema7Database(t *testing.T) {
	dataDir, kr := setupEnv(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateToForTest(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO accounts(id,name,email,host,port,tls,username,credential_ref,insecure_test_server,created_at)
		VALUES (1,'demo','demo@example.invalid','127.0.0.1',11143,'none','tester@example.invalid','account/demo',1,1)`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	_ = kr.Set("account/demo", canary)

	code, out, errs := run(t, "doctor", "--json")
	var rep DoctorReport
	if code != ExitOK || json.Unmarshal([]byte(out), &rep) != nil {
		t.Fatalf("doctor: %d %s %s", code, out, errs)
	}
	if rep.Database != "needs_migration" || rep.SchemaVersion != 7 {
		t.Fatalf("database = %s, schema %d", rep.Database, rep.SchemaVersion)
	}
	if hasWarning(rep, "no such column") || hasWarning(rep, "accounts:") {
		t.Fatalf("accounts not readable: %v", rep.Warnings)
	}
	if len(rep.Accounts) != 1 || rep.Accounts[0].Name != "demo" || rep.Accounts[0].Password != "stored" {
		t.Fatalf("accounts = %+v", rep.Accounts)
	}
	if !hasWarning(rep, "database schema 7 is older than this program") ||
		!hasWarning(rep, "account demo still uses the old keyring entry account/demo; the next command (e.g. accounts list) migrates the database") {
		t.Fatalf("warnings = %v", rep.Warnings)
	}
	if strings.Contains(out, canary) {
		t.Fatal("password in the doctor output")
	}
	// Text-Ausgabe: dieselbe Lage ohne Fehlerzeile.
	if code, out, _ := run(t, "doctor"); code != ExitOK || strings.Contains(out, "no such column") || !strings.Contains(out, "demo: password stored") {
		t.Fatalf("doctor (text): %d %s", code, out)
	}

	if code, _, errs := run(t, "accounts", "list"); code != ExitOK || errs != "" {
		t.Fatalf("accounts list: %d %q", code, errs)
	}
	rep = doctorJSON(t)
	if rep.Database != "ok" || hasWarning(rep, "keyring entry") || hasWarning(rep, "older than this program") {
		t.Fatalf("doctor after migration: %s %v", rep.Database, rep.Warnings)
	}
	if refs := keyringRefs(kr); len(refs) != 1 || refs["account/demo"] != "" {
		t.Fatalf("keyring after move = %v", refs)
	}
}

// Gegenprüfung 21.10.2026, Befund 3 (A02 E1): Der MCP-Server fasst keine
// Passwörter an und zieht deshalb auch keine Schlüsselbund-Einträge um.
// Startet `mcp` mit einem Konto mit altem Verweis, bleiben Verweis und
// Schlüsselbund unverändert; erst ein CLI-Kommando zieht um.
func TestMCPDoesNotMoveKeyringEntries(t *testing.T) {
	dataDir, kr := setupEnv(t)
	addDemo(t, canary)
	key := newKey(t, "--name", "agent", "--role", "reader")
	st := openDB(t, dataDir)
	a, _ := st.AccountByName(t.Context(), "demo")
	newRef := a.CredentialRef
	if err := st.ExecForTest(`UPDATE accounts SET credential_ref = 'account/demo'`); err != nil {
		t.Fatal(err)
	}
	_ = kr.Delete(newRef)
	_ = kr.Set("account/demo", canary)

	cs, stderr := mcpSession(t, key)
	if v, res := callTool(t, cs, "mail_list_accounts", map[string]any{}); res.IsError {
		t.Fatalf("mail_list_accounts: %v", v)
	}
	if a, _ := st.AccountByName(t.Context(), "demo"); a.CredentialRef != "account/demo" {
		t.Fatalf("mcp switched the reference to %s", a.CredentialRef)
	}
	if refs := keyringRefs(kr); len(refs) != 1 || refs["account/demo"] != canary {
		t.Fatalf("mcp changed the keyring: %v", refs)
	}
	if strings.Contains(stderr.String(), "keyring") {
		t.Fatalf("stderr: %s", stderr.String())
	}
	// Gegenprobe: das nächste CLI-Kommando zieht um.
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK || errs != "" {
		t.Fatalf("accounts list: %d %q", code, errs)
	}
	if refs := keyringRefs(kr); len(refs) != 1 || refs[newRef] != canary {
		t.Fatalf("keyring after accounts list = %v", refs)
	}
}

// Befund 5, Sicht des Menschen: Ohne lesbaren alten Eintrag und ohne
// Umgebungsvariable bleibt der alte Verweis, und doctor sagt, worauf der
// Umzug wartet (statt „the next command moves it").
func TestLegacyKeyringEntryMissingKeepsRef(t *testing.T) {
	dataDir, kr := setupEnv(t)
	addDemo(t, canary)
	st := openDB(t, dataDir)
	a, _ := st.AccountByName(t.Context(), "demo")
	if err := st.ExecForTest(`UPDATE accounts SET credential_ref = 'account/demo'`); err != nil {
		t.Fatal(err)
	}
	_ = kr.Delete(a.CredentialRef) // weder alt noch neu lesbar
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK || errs != "" {
		t.Fatalf("accounts list: %d %q", code, errs)
	}
	if a, _ := st.AccountByName(t.Context(), "demo"); a.CredentialRef != "account/demo" {
		t.Fatalf("reference switched to %s", a.CredentialRef)
	}
	rep := doctorJSON(t)
	if !hasWarning(rep, "still uses the old keyring entry account/demo, which holds no readable password") || hasWarning(rep, "the next command") {
		t.Fatalf("doctor: %v", rep.Warnings)
	}
}
