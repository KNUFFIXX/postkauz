package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.1: doctor kennt Schema 10 (seit S2.3: 11). Ein Bestand mit Schema 9
// (Stand S2.0) ist needs_migration mit Hinweis; das nächste Kommando
// migriert, danach ok.
func TestDoctorKnowsSchema10(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateToForTest(t.Context(), 9); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	rep := doctorJSON(t)
	known := store.KnownSchemaVersion()
	if known < 11 || rep.SchemaKnown != known || rep.SchemaVersion != 9 || rep.Database != "needs_migration" ||
		!hasWarning(rep, fmt.Sprintf("database schema 9 is older than this program (%d)", known)) {
		t.Fatalf("doctor on schema 9: %+v", rep)
	}
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %d %s", code, errs)
	}
	if rep := doctorJSON(t); rep.SchemaVersion != known || rep.Database != "ok" {
		t.Fatalf("doctor after migration: %+v", rep)
	}
}

// Entscheid 36 (S2.1): `account set <name> --password-stdin` ersetzt das
// gespeicherte Passwort (Fake-Schlüsselbund). Vorher scheitert die
// Anmeldung mit dem falschen Passwort, danach gelingt sie. Das Passwort
// steht in keiner Ausgabe, keinem Audit-Eintrag und keiner Fehlermeldung;
// mit --key (auch admin) Exit 3 human_only ohne Wirkung; ohne Eingabe
// Exit 2; nie als Argument.
func TestAccountSetPasswordStdin(t *testing.T) {
	dataDir, kr := setupEnv(t)
	const wrong, right = "FalschesKanarienPasswort-36", "RichtigesKanarienPasswort-36"
	host, port := fakeIMAP(t, right)
	if code, _, errs := runIn(t, wrong+"\n", "account", "add", "demo", "--imap", host+":"+port, "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %s", errs)
	}
	var all strings.Builder
	step := func(stdin string, want int, args ...string) (string, string) {
		t.Helper()
		code, out, errs := runIn(t, stdin, args...)
		all.WriteString(out + errs)
		if code != want {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		return out, errs
	}
	step("", ExitAuth, "account", "test", "demo")

	// Ein Schlüssel darf es nicht, auch admin nicht.
	run(t, "key", "create", "--name", "adm", "--role", "admin")
	_, errs := step(right+"\n", ExitDenied, "account", "set", "demo", "--password-stdin", "--key", "adm")
	if !strings.Contains(errs, "human_only") {
		t.Fatalf("--key: %s", errs)
	}
	// Trockenlauf ändert nichts.
	out, _ := step(right+"\n", ExitOK, "account", "set", "demo", "--password-stdin", "--dry-run")
	if !strings.Contains(out, "dry run: would replace the password of account demo in the OS keyring as account/") {
		t.Fatalf("dry run: %s", out)
	}
	step("", ExitAuth, "account", "test", "demo")

	out, _ = step(right+"\n", ExitOK, "account", "set", "demo", "--password-stdin", "--json")
	var o ops.AccountsSetOutput
	if json.Unmarshal([]byte(out), &o) != nil || o.Password != "replaced" || o.SecretStore != "keyring" || o.AliasesChanged {
		t.Fatalf("account set --json: %s", out)
	}
	if refs := keyringRefs(kr); len(refs) != 1 || refs[o.CredentialRef] != right {
		t.Fatalf("keyring = %v", refs)
	}
	step("", ExitOK, "account", "test", "demo")

	// Zusammen mit --aliases; Textausgabe nennt beides.
	out, _ = step(right+"\n", ExitOK, "account", "set", "demo", "--aliases", "alias@example.org", "--password-stdin")
	if !strings.Contains(out, "account demo: aliases alias@example.org") || !strings.Contains(out, "account demo: password replaced") {
		t.Fatalf("aliases + password: %s", out)
	}
	// Eingabefehler.
	step("", ExitUsage, "account", "set", "demo", "--password-stdin")
	step("", ExitUsage, "account", "set", "demo")
	step("", ExitUsage, "account", "set", "demo", "--password-stdin", right)
	step("", ExitUsage, "account", "set", "demo", "--password", right)

	if strings.Contains(all.String(), right) || strings.Contains(all.String(), wrong) {
		t.Fatalf("password in output:\n%s", all.String())
	}
	code, aud, _ := run(t, "audit", "--since", "1h", "--limit", "1000", "--json")
	logb, _ := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
	if code != ExitOK || !strings.Contains(aud, `"replace_password"`) || strings.Contains(aud+string(logb), right) || strings.Contains(aud+string(logb), wrong) {
		t.Fatalf("audit/log: %s", aud)
	}
}

// lastAuditPolicyVersion liest die policy_version des letzten Audit-Eintrags.
func lastAuditPolicyVersion(t *testing.T) int64 {
	t.Helper()
	code, out, errs := run(t, "audit", "--since", "1h", "--limit", "1000", "--json")
	if code != ExitOK {
		t.Fatalf("audit: %d %s", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var e ops.AuditEventOutput
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &e); err != nil {
		t.Fatalf("audit line: %v %s", err, out)
	}
	return e.PolicyVersion
}

// S2.1 (A02 E8): Die Policy-Datei neben der Konfiguration wird bei jedem
// Kommando geladen und geprüft, ihre Version eingetragen und im Audit
// vermerkt; max_chars wirkt in policy show; doctor zeigt Pfad, Zustand und
// Version (ohne selbst einzutragen); eine ungültige Datei hält Kommandos
// auf (Exit 1, Code policy_invalid).
func TestPolicyFileLoadedRecordedAudited(t *testing.T) {
	dataDir, _ := setupEnv(t)
	t.Setenv(policy.EnvPolicy, "")
	path := filepath.Join(filepath.Dir(dataDir), policy.FileName)

	// Ohne Datei: eingebaute Standards, Version 1.
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %d %s", code, errs)
	}
	rep := doctorJSON(t)
	if rep.PolicyPath != path || rep.Policy != "default" || rep.PolicyVersion != 1 || rep.AttachDir != filepath.Join(dataDir, "attach") {
		t.Fatalf("doctor without policy file: %+v", rep)
	}
	if v := lastAuditPolicyVersion(t); v != 1 {
		t.Fatalf("audit policy_version = %d, want 1", v)
	}

	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("[agent_defaults]\nmax_chars = 5000\n")
	if rep := doctorJSON(t); rep.Policy != "ok" || rep.PolicyVersion != 0 || len(rep.PolicySHA256) != 64 {
		t.Fatalf("doctor before the next command: %+v", rep)
	}
	code, out, errs := run(t, "policy", "show", "--json")
	var ps ops.PolicyOutput
	if code != ExitOK || json.Unmarshal([]byte(out), &ps) != nil || ps.MaxChars != 5000 {
		t.Fatalf("policy show: %d %s %s", code, out, errs)
	}
	if v := lastAuditPolicyVersion(t); v != 2 {
		t.Fatalf("audit policy_version = %d, want 2", v)
	}
	if code, out, _ := run(t, "doctor"); code != ExitOK || !strings.Contains(out, "policy:    "+path+" (found; version 2)") ||
		!strings.Contains(out, "attach:    "+filepath.Join(dataDir, "attach")) {
		t.Fatalf("doctor text: %s", out)
	}
	// Zurück auf den Stand ohne Datei: neue Version (A → B → A = 1, 2, 3).
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	run(t, "accounts", "list")
	if v := lastAuditPolicyVersion(t); v != 3 {
		t.Fatalf("audit policy_version = %d, want 3", v)
	}

	// Unbekannter Schlüssel: Kommando startet nicht, doctor meldet invalid.
	write("[agent_defaults]\nmax_chars = 5000\n[send]\nauto = true\n")
	code, _, errs = run(t, "accounts", "list", "--json")
	if code != ExitError || !strings.Contains(errs, `"code":"policy_invalid"`) || !strings.Contains(errs, "unknown keys: send, send.auto") {
		t.Fatalf("invalid policy: %d %s", code, errs)
	}
	code, out, _ = run(t, "doctor", "--json")
	var bad DoctorReport
	if code != ExitError || json.Unmarshal([]byte(out), &bad) != nil || bad.Policy != "invalid" || !strings.Contains(bad.PolicyError, "unknown keys") {
		t.Fatalf("doctor with invalid policy: %d %s", code, out)
	}

	// POSTKAUZ_POLICY: anderer Pfad; fehlt die Datei dort, ist das ein Fehler.
	other := filepath.Join(t.TempDir(), "richtlinie.toml")
	t.Setenv(policy.EnvPolicy, other)
	if code, _, errs := run(t, "accounts", "list"); code != ExitError || !strings.Contains(errs, "does not exist") {
		t.Fatalf("missing explicit policy: %d %s", code, errs)
	}
	if err := os.WriteFile(other, []byte("[draft]\ncheck_hook = [\"lint-mail\", \"{eml}\"]\ncheck_hook_timeout = \"20s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("explicit policy: %d %s", code, errs)
	}
	if rep := doctorJSON(t); rep.PolicyPath != other || rep.Policy != "ok" || rep.PolicyVersion != 4 {
		t.Fatalf("doctor with POSTKAUZ_POLICY: %+v", rep)
	}
}
