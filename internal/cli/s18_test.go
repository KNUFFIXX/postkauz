package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S1.8: Schlüssel. Der Klartext erscheint genau einmal (key create), in der
// Datenbank steht nur der Hash, key list zeigt ihn nie; widerrufene
// Schlüssel werden abgewiesen.
func TestKeyCreateListRevoke(t *testing.T) {
	dataDir, _ := setupEnv(t)
	code, out, errs := run(t, "key", "create", "--name", "test-agent", "--role", "reader")
	if code != ExitOK {
		t.Fatalf("key create: %d %s", code, errs)
	}
	secret := regexp.MustCompile(`pk_key_[A-Za-z0-9_-]{43}`).FindString(out)
	if secret == "" || !strings.Contains(out, "role reader: mail:read, mail:search, audit:read") ||
		!strings.Contains(out, "only time the key is shown") {
		t.Fatalf("key create output:\n%s", out)
	}
	// JSON: Klartext im Feld secret, Schlüssel sonst nirgends.
	code, out, _ = run(t, "key", "create", "--name", "suche", "--role", "assistant", "--scopes", "mail:search", "--expires", "never", "--json")
	var created ops.KeysCreateOutput
	if code != ExitOK || json.Unmarshal([]byte(out), &created) != nil || !strings.HasPrefix(created.Secret, ops.KeyPrefix) ||
		strings.Join(created.Key.Scopes, ",") != "mail:search" || created.Key.ExpiresAt != "" || !created.Key.DryRunDefault {
		t.Fatalf("key create --json: %d %s", code, out)
	}
	// Erweitern ist verboten, doppelte Namen auch.
	if code, _, errs := run(t, "key", "create", "--name", "x", "--role", "reader", "--scopes", "mail:draft", "--json"); code != ExitUsage ||
		!strings.Contains(errs, "only narrow") {
		t.Fatalf("widening scopes: %d %s", code, errs)
	}
	if code, _, errs := run(t, "key", "create", "--name", "test-agent", "--json"); code != ExitError || !strings.Contains(errs, `"exists"`) {
		t.Fatalf("duplicate: %d %s", code, errs)
	}
	if code, _, _ := run(t, "key", "create", "--name", "x", "--expires", "soon"); code != ExitUsage {
		t.Fatalf("bad --expires: %d", code)
	}

	code, out, _ = run(t, "key", "list")
	if code != ExitOK || !strings.Contains(out, "test-agent  reader") || !strings.Contains(out, "active") ||
		strings.Contains(out, secret) || strings.Contains(out, ops.HashKey(secret)) {
		t.Fatalf("key list:\n%s", out)
	}
	_, out, _ = run(t, "key", "list", "--json")
	if strings.Contains(out, "pk_key_") || strings.Contains(out, ops.HashKey(secret)) || strings.Contains(out, "hash") {
		t.Fatalf("key list --json leaks: %s", out)
	}
	// Datenbank: nur der Hash, nie der Klartext (auch nicht im WAL oder Log).
	assertNotInDir(t, dataDir, secret)
	assertNotInDir(t, dataDir, created.Secret)

	if code, out, _ := run(t, "key", "revoke", "test-agent"); code != ExitOK || !strings.Contains(out, "key test-agent revoked") {
		t.Fatalf("revoke: %d %s", code, out)
	}
	if code, out, _ := run(t, "key", "revoke", "test-agent"); code != ExitOK || !strings.Contains(out, "already revoked") {
		t.Fatalf("revoke twice: %d %s", code, out)
	}
	if _, out, _ := run(t, "key", "list"); !strings.Contains(out, "revoked") {
		t.Fatalf("key list after revoke:\n%s", out)
	}
	// --key mit einem widerrufenen Schlüssel: verweigert (Exit 3).
	if code, _, errs := run(t, "accounts", "list", "--key", "test-agent", "--json"); code != ExitDenied || !strings.Contains(errs, "revoked") {
		t.Fatalf("revoked key: %d %s", code, errs)
	}
	// Der Klartext ist nie ein Argument von --key.
	if code, _, _ := run(t, "accounts", "list", "--key", created.Secret); code != ExitUsage {
		t.Fatalf("--key with a secret: %d", code)
	}
	if code, _, _ := run(t, "key", "revoke", "unbekannt"); code != ExitError {
		t.Fatalf("revoke unknown: %d", code)
	}
}

func assertNotInDir(t *testing.T, dir, needle string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), needle) {
			t.Errorf("%s contains the key in clear text", p)
		}
		return nil
	})
}

// S1.8: --key handelt mit den Rechten eines Schlüssels (reader darf lesen,
// aber keine Schlüssel verwalten), --explain zeigt Entscheidung und Audit-ID
// auf stderr, ohne Inhalte; das Audit nennt den Schlüssel.
func TestKeyFlagExplainAndAudit(t *testing.T) {
	setupEnv(t)
	seedS16(t)
	run(t, "key", "create", "--name", "leser", "--role", "reader")
	run(t, "key", "create", "--name", "triage", "--role", "reader", "--scopes", "mail:search")

	code, out, errs := run(t, "search", "Rechnung", "--key", "leser", "--explain")
	if code != ExitOK || !strings.Contains(out, "Rechnung [attachment]") || !strings.Contains(out, "sql:") {
		t.Fatalf("search as reader: %d %s %s", code, out, errs)
	}
	m := regexp.MustCompile(`explain: messages.search \(scope mail:search, risk R0\) as key key:leser: allow \(ok\), dry run no; audit event (\d+)`).
		FindStringSubmatch(errs)
	if m == nil {
		t.Fatalf("explain line missing:\n%s", errs)
	}
	// triage (nur mail:search) darf suchen, aber keine Nachricht lesen.
	if code, _, _ := run(t, "search", "Heizung", "--key", "triage"); code != ExitOK {
		t.Fatalf("triage search: %d", code)
	}
	code, _, errs = run(t, "show", "1", "--key", "triage", "--explain", "--json")
	if code != ExitDenied || !strings.Contains(errs, `"decision":"deny"`) || !strings.Contains(errs, `"reason":"missing_scope"`) ||
		!strings.Contains(errs, `"audit_id":`) || !strings.Contains(errs, `"code":"denied"`) {
		t.Fatalf("triage show: %d %s", code, errs)
	}
	if code, out, _ := run(t, "policy", "show", "--key", "triage"); code != ExitOK || !strings.Contains(out, "scopes:          mail:search") ||
		strings.Contains(out, "messages.get") || !strings.Contains(out, "messages.search") {
		t.Fatalf("policy show triage: %d\n%s", code, out)
	}
	if code, _, _ := run(t, "key", "create", "--name", "boese", "--key", "leser"); code != ExitDenied {
		t.Fatalf("reader creates key: %d", code)
	}

	// Audit: Schlüsselname, Ergebnis, keine Suchbegriffe, kein Mailtext.
	code, out, _ = run(t, "audit", "--since", "10m", "--key-id", "key:leser")
	if code != ExitOK || !strings.Contains(out, "key:leser") || !strings.Contains(out, "messages.search") ||
		!regexp.MustCompile(`keys\.create\s+denied \(missing_scope\)`).MatchString(out) || strings.Contains(out, "key:triage") {
		t.Fatalf("audit:\n%s", out)
	}
	_, all, _ := run(t, "audit", "--since", "1h", "--json", "--limit", "1000")
	for _, leak := range []string{"Rechnung", "Heizung", "Monteur", "Ignoriere", "portal.example.net"} {
		if strings.Contains(all, leak) {
			t.Fatalf("audit contains %q:\n%s", leak, all)
		}
	}
	lines := strings.Split(strings.TrimSpace(all), "\n")
	var ev ops.AuditEventOutput
	if json.Unmarshal([]byte(lines[0]), &ev) != nil || ev.ID == 0 || ev.Operation == "" {
		t.Fatalf("audit ndjson: %s", lines[0])
	}
	if !strings.Contains(all, `"id":`+m[1]+`,`) {
		t.Fatalf("audit id %s from --explain not in the log", m[1])
	}
}

// S1.8: show zeigt alles Nötige: Kopf, Absenderprüfung, Text, Warnzeile bei
// verstecktem Text, Links und die Anhangsliste; --max-chars kürzt.
func TestShowFull(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	code, out, _ := run(t, "show", ph["html"])
	for _, want := range []string{"subject:", "Rechnung", "sender checks:", "spf none, dkim none, dmarc none",
		"warning:", "hidden text removed", "--- text (from HTML) ---", "--- links ---", "--- attachments ---", "Rechnung.pdf",
		"TYPE MISMATCH", "EXECUTABLE"} {
		if code != ExitOK || !strings.Contains(out, want) {
			t.Fatalf("show lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Ignoriere") {
		t.Fatal("hidden text shown")
	}
	_, out, _ = run(t, "show", ph["answer"], "--max-chars", "5")
	if !strings.Contains(out, "[cut after --max-chars]") {
		t.Fatalf("show --max-chars:\n%s", out)
	}
	var js map[string]any
	_, out, _ = run(t, "show", ph["html"], "--json")
	if json.Unmarshal([]byte(out), &js) != nil || js["untrusted"] != true || js["source"] != "email" || js["notice"] != ops.UntrustedNotice ||
		js["hidden_text_removed"] != true {
		t.Fatalf("show --json envelope: %s", out)
	}
	_, out, _ = run(t, "thread", ph["thread"], "--max-messages", "1")
	if !strings.Contains(out, "(1 older messages not shown)") || !strings.Contains(out, "2 messages") {
		t.Fatalf("thread --max-messages:\n%s", out)
	}
	_, out, _ = run(t, "search", "Heizung", "--json", "--fields", "id,subject")
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var h map[string]any
		// Die gewählten Felder und immer die Kennzeichnung der Hülle (S1.11).
		if json.Unmarshal([]byte(l), &h) != nil || len(h) != 4 || h["id"] == nil || h["subject"] == nil || h["untrusted"] != true || h["source"] != "email" {
			t.Fatalf("--fields: %s", l)
		}
	}
	if code, _, _ := run(t, "search", "Heizung", "--json", "--fields", "body"); code != ExitUsage {
		t.Fatalf("--fields unknown: %d", code)
	}
	_, out, _ = run(t, "search", "Heizung", "--mailbox", "Archiv")
	if !strings.Contains(out, "No messages found.") {
		t.Fatalf("search --mailbox:\n%s", out)
	}
}

// S1.8: audit export schreibt Tagesdateien mit Prüfsumme, audit verify
// meldet ok; eine veränderte Datei ergibt mismatch und Exit 1, und der
// nächste Export überschreibt sie nicht.
func TestAuditExportVerify(t *testing.T) {
	setupEnv(t)
	run(t, "accounts", "list")
	if code, out, _ := run(t, "audit", "verify"); code != ExitOK || !strings.Contains(out, "nothing exported yet") {
		t.Fatalf("verify before export: %d %s", code, out)
	}
	dir := filepath.Join(t.TempDir(), "export")
	if code, _, errs := run(t, "audit", "export"); code != ExitUsage || !strings.Contains(errs, "no export directory") {
		t.Fatalf("export without --to: %d %s", code, errs)
	}
	code, out, _ := run(t, "audit", "export", "--to", dir)
	if code != ExitOK || !strings.Contains(out, "written") {
		t.Fatalf("export: %d %s", code, out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "postkauz-audit-*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files: %v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "postkauz-audit-manifest.json")); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if code, out, _ := run(t, "audit", "verify"); code != ExitOK || !strings.HasPrefix(out, "ok: 1 exported day checked") ||
		!strings.Contains(out, "chain head: ") {
		t.Fatalf("verify: %d %s", code, out)
	}
	if code, out, _ := run(t, "audit", "export", "--to", dir); code != ExitOK || !strings.Contains(out, "updated") {
		t.Fatalf("export again: %d %s", code, out)
	}
	f, _ := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"id":999}` + "\n")
	_ = f.Close()
	code, out, errs := run(t, "audit", "verify")
	if code != ExitError || !strings.Contains(out, "mismatch") || !strings.Contains(out, "export file was changed (line ") ||
		!strings.Contains(out, "hash chain is broken (line ") || !strings.Contains(errs, "audit export does not match") {
		t.Fatalf("verify after change: %d %s %s", code, out, errs)
	}
	if code, _, errs := run(t, "audit", "export", "--to", dir, "--json"); code != ExitError || !strings.Contains(errs, `"mismatch"`) {
		t.Fatalf("export over a changed file: %d %s", code, errs)
	}
	if code, _, _ := run(t, "audit", "--since", "gestern"); code != ExitUsage {
		t.Fatalf("bad --since: %d", code)
	}
}

// S1.8: --profile trennt Instanzen, --quiet unterdrückt den Fortschritt.
func TestProfileAndQuiet(t *testing.T) {
	dataDir, _ := setupEnv(t)
	run(t, "key", "create", "--name", "haupt")
	run(t, "key", "create", "--name", "probe", "--profile", "test")
	_, main, _ := run(t, "key", "list")
	_, prof, _ := run(t, "key", "list", "--profile", "test")
	if !strings.Contains(main, "haupt") || strings.Contains(main, "probe") || !strings.Contains(prof, "probe") || strings.Contains(prof, "haupt") {
		t.Fatalf("profiles not separate:\nmain:\n%s\nprofile:\n%s", main, prof)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "profiles", "test", "postkauz.db")); err != nil {
		t.Fatalf("profile database: %v", err)
	}
	if _, out, _ := run(t, "doctor", "--profile", "test"); !strings.Contains(out, filepath.Join("profiles", "test")) {
		t.Fatalf("doctor --profile:\n%s", out)
	}
	if code, _, _ := run(t, "key", "list", "--profile", "Böse"); code != ExitUsage {
		t.Fatalf("bad profile name: %d", code)
	}

	addr := s16Server(t)
	runIn(t, canary+"\n", "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid", "--tls", "none",
		"--insecure-test-server", "--password-stdin")
	t.Cleanup(func() { observeRequest = nil })
	var got []bool
	observeRequest = func(r ops.Request) error { got = append(got, r.Progress != nil); return nil }
	code, out, errs := run(t, "sync", "-a", "demo", "--window", "1", "--quiet")
	if code != ExitOK || errs != "" || !strings.Contains(out, "3 messages mirrored") {
		t.Fatalf("sync --quiet: %d out=%s err=%q", code, out, errs)
	}
	run(t, "sync", "-a", "demo")
	if len(got) != 2 || got[0] || !got[1] {
		t.Fatalf("progress receiver with --quiet / without: %v", got)
	}
}
