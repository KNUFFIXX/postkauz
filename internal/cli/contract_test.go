package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/oauth/oauthtest"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// --- Vertragstest Register ↔ CLI (ADR-004, S1.8) -------------------------------
//
// Jede Operation des Katalogs ist über mindestens ein Kommando erreichbar
// (seit S2.3 mehrere: drafts.create über draft new|reply|forward) und jedes
// Kommando gehört zu einer Operation; die Felder, die ein Kommando setzen
// kann (binding.covers), sind Eigenschaften des InputSchema, und alle
// Kommandos einer Operation zusammen erreichen jedes Feld; jede gebaute
// Eingabe entspricht dem InputSchema, jede --json-Ausgabe dem OutputSchema.

func resolved(t *testing.T, raw json.RawMessage) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// subSchema holt properties.<name>.items aus einem Schema (NDJSON-Zeilen).
func subSchema(t *testing.T, raw json.RawMessage, name string) json.RawMessage {
	t.Helper()
	var s struct {
		Properties map[string]struct {
			Items json.RawMessage `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s.Properties[name].Items
}

func schemaProps(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sorted(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}

func TestBindingsCoverCatalogAndSchemas(t *testing.T) {
	cat := map[string]ops.Operation{}
	for _, op := range ops.Catalog(ops.Deps{}) {
		cat[op.Name] = op
	}
	union := map[string]map[string]bool{}
	paths := map[string]bool{}
	for _, b := range bindings {
		op, ok := cat[b.op]
		if !ok {
			t.Errorf("binding %v: operation %s is not in the catalog", b.path, b.op)
			continue
		}
		if p := strings.Join(b.path, " "); paths[p] {
			t.Errorf("command %q bound twice", p)
		} else {
			paths[p] = true
		}
		props := schemaProps(t, op.InputSchema)
		if union[b.op] == nil {
			union[b.op] = map[string]bool{}
		}
		for _, f := range b.covers {
			if !slices.Contains(props, f) {
				t.Errorf("%s (%s): covers %q, which is not in the schema %v", b.op, strings.Join(b.path, " "), f, props)
			}
			union[b.op][f] = true
		}
	}
	for name, op := range cat {
		if union[name] == nil {
			t.Errorf("operation %s has no command", name)
			continue
		}
		var got []string
		for f := range union[name] {
			got = append(got, f)
		}
		if g, w := sorted(got), schemaProps(t, op.InputSchema); !slices.Equal(g, w) {
			t.Errorf("%s: commands set %v, schema has %v", name, g, w)
		}
	}
}

// cliSample ist ein Aufruf im Vertragstest; {name} wird ersetzt.
type cliSample struct {
	op   string
	args []string
	// captureOnly: nur die Eingabe prüfen, nicht ausführen (sync --watch liefe dauerhaft).
	captureOnly bool
}

// sampleStdin: Eingabe auf stdin je Beispiel (Argumente mit Leerzeichen
// verbunden), etwa das Passwort bei --password-stdin.
var sampleStdin = map[string]string{
	"account set demo --password-stdin":                                     canary + "\n",
	"draft new -a demo --to a@example.org --html-file - --attach notiz.txt": "<p>Hallo <b>Welt</b></p>",
}

var cliSamples = []cliSample{
	{"accounts.list", []string{"accounts", "list"}, false},
	{"accounts.add", []string{"account", "add", "zwei", "--imap", "127.0.0.1:1", "--user", "u@example.invalid", "--email", "z@example.invalid",
		"--tls", "none", "--auth", "app_password", "--insecure-test-server", "--password-from-env", "--dry-run"}, false},
	{"accounts.set", []string{"account", "set", "demo", "--aliases", "Alias@example.org, zweit@example.net"}, false},
	{"accounts.set", []string{"account", "set", "demo", "--password-stdin"}, false},
	{"accounts.set", []string{"account", "set", "demo", "--drafts-mailbox", ""}, false},
	{"accounts.test", []string{"account", "test", "demo"}, false},
	// S3.7: OAuth2-Konto anlegen und anmelden (Fake-AS, setupEnv).
	{"accounts.add", []string{"account", "add", "ms", "--imap", "outlook.office365.com:993", "--user", "ms@example.org",
		"--auth", "oauth2", "--client-id", oauthtest.ClientID, "--tenant", "consumers"}, false},
	{"accounts.login", []string{"account", "login", "ms", "--device-code"}, false},
	{"accounts.remove", []string{"account", "remove", "leer", "--dry-run"}, false},
	{"accounts.remove", []string{"account", "remove", "leer", "--force", "--dry-run"}, false},
	{"sync.run", []string{"sync", "-a", "demo", "--full", "--window", "50", "--mailbox", "INBOX"}, false},
	{"sync.run", []string{"sync", "-a", "demo", "--watch", "--interval", "1m", "--idle-restart", "6m", "--full-check-interval", "30m", "--jobs-interval", "5s"}, true},
	{"sync.run", []string{"sync", "--watch", "--all"}, true},
	{"sync.request", []string{"sync", "request", "-a", "demo", "--mailbox", "INBOX"}, false},
	{"import.run", []string{"import", "mbox", "{mbox}", "-a", "demo", "--mailbox", "Archiv", "--labels-from", "{labels}", "--rescan", "--no-extract"}, false},
	{"mailboxes.list", []string{"mailbox", "list", "-a", "demo"}, false},
	{"mailboxes.overview", []string{"mailbox", "overview", "{mailbox}", "-a", "demo", "--since", "3650d", "--limit", "5"}, false},
	{"messages.get", []string{"show", "{html}", "--strip-quotes", "--max-chars", "10"}, false},
	{"messages.get", []string{"show", "antwort@example.org", "-a", "demo", "--headers"}, false},
	{"messages.search", []string{"search", "Rechnung", "-a", "demo", "--mailbox", "INBOX", "--limit", "5", "--explain"}, false},
	{"messages.search", []string{"search", "Heizung", "--cursor", "{cursor}"}, false},
	{"threads.get", []string{"thread", "{thread}", "--strip-quotes", "--max-messages", "1", "--max-chars", "5"}, false},
	{"threads.get", []string{"thread", "{answer}", "--message", "-a", "demo"}, false},
	{"attachments.list", []string{"attach", "list", "{html}"}, false},
	{"attachments.text", []string{"attach", "text", "{att}", "--max-chars", "5", "--message", "{html}", "-a", "demo"}, false},
	// S3.3: Anhang unter export_dir, Export in ein frisches Ziel.
	{"attachments.save", []string{"attach", "save", "{att}", "--subdir", "belege", "--message", "{html}", "-a", "demo"}, false},
	{"messages.export", []string{"export", "eml", "--to", "{exporttarget}", "-a", "demo", "--mailbox", "INBOX", "--since", "3650d"}, false},
	{"keys.create", []string{"key", "create", "--name", "k1", "--role", "reader", "--scopes", "mail:search", "--expires", "30d",
		"--dry-run-default=false"}, false},
	{"keys.list", []string{"key", "list"}, false},
	{"keys.revoke", []string{"key", "revoke", "k1"}, false},
	{"audit.list", []string{"audit", "--since", "1h", "--until", "2099-01-01", "--actor", "human", "--key-id", "cli",
		"--operation", "keys.list", "--result", "ok", "--raw", "--limit", "5", "--after-id", "1"}, false},
	{"audit.export", []string{"audit", "export", "--to", "{exportdir}"}, false},
	{"audit.verify", []string{"audit", "verify"}, false},
	{"policy.get", []string{"policy", "show"}, false},
	// S2.3: Identität, drei Entwürfe (1 reply_all, 2 new, 3 forward),
	// ändern, vorlegen, freigeben (Freigabe 1), ablehnen (Freigabe 2),
	// abschließen; die Reihenfolge baut aufeinander auf.
	{"identities.add", []string{"identity", "add", "-a", "demo", "--email", "demo@example.org", "--name", "Demo", "--reply-to", "antwort@example.org",
		"--signature-file", "{sigtext}", "--signature-html-file", "{sightml}", "--style-file", "{sigtext}", "--reply-prefix", "AW: ",
		"--forward-prefix", "WG: ", "--default"}, false},
	{"identities.list", []string{"identity", "list", "-a", "demo"}, false},
	{"drafts.style", []string{"draft", "style", "demo@example.org", "-a", "demo", "--max-chars", "100"}, false},
	{"identities.set_default", []string{"identity", "set-default", "1"}, false},
	{"drafts.create", []string{"draft", "reply", "{html}", "--all", "--to", "Zweit <zweit@example.net>", "--cc", "c@example.net", "--bcc",
		"b@example.net", "--subject", "Re: Rechnung", "--body-file", "{body}", "--attach", "{att}", "--no-quote", "--no-signature",
		"--identity", "demo@example.org"}, false},
	{"drafts.create", []string{"draft", "new", "-a", "demo", "--to", "a@example.org", "--html-file", "-", "--attach", "notiz.txt"}, false},
	{"drafts.create", []string{"draft", "forward", "antwort@example.org", "-a", "demo", "--to", "a@example.org", "--no-attachments"}, false},
	{"drafts.update", []string{"draft", "edit", "1", "--to", "neu@example.org", "--cc", "", "--bcc", "", "--subject", "Neu", "--body-file", "{body}",
		"--attach", "notiz.txt", "--quote=false", "--signature=true"}, false},
	{"drafts.update", []string{"draft", "edit", "1", "--html-file", "{html-body}", "--no-attachments"}, false},
	{"drafts.get", []string{"draft", "show", "1"}, false},
	{"drafts.get", []string{"draft", "show", "1", "--eml"}, false},
	{"drafts.get", []string{"draft", "show", "1", "-a", "demo", "--max-chars", "100"}, false},
	{"drafts.list", []string{"draft", "list", "-a", "demo", "--status", "draft,pending_approval", "--limit", "5"}, false},
	{"drafts.request_ready", []string{"draft", "request", "1"}, false},
	{"approvals.get", []string{"approval", "show", "1"}, false},
	{"drafts.approve", []string{"approve", "1"}, false},
	{"drafts.request_ready", []string{"draft", "request", "2"}, false},
	{"drafts.reject", []string{"reject", "2", "--reason", "zu lang"}, false},
	{"drafts.abandon", []string{"draft", "abandon", "2"}, false},
	{"drafts.mark_sent", []string{"draft", "mark-sent", "1"}, false},
	{"identities.remove", []string{"identity", "remove", "1"}, false},
	// T2: Tags und Aktionen (Aktion 1 mark, 2 move, 3 und 4 Anträge auf
	// Papierkorb; 3 freigegeben, 4 abgelehnt). Ausgeführt wird nichts (kein
	// Sync im Test).
	{"messages.tag", []string{"tag", "add", "rechnung,wichtig", "{html}", "-a", "demo", "--confirm-count", "1"}, false},
	{"messages.tag", []string{"tag", "remove", "wichtig", "{html}", "-a", "demo"}, false},
	{"tags.list", []string{"tag", "list", "-a", "demo"}, false},
	{"messages.mark", []string{"mark", "{html}", "--unseen", "--flagged", "-a", "demo", "--confirm-count", "1"}, false},
	{"messages.move", []string{"move", "{html}", "--role", "archive", "--from", "INBOX", "-a", "demo"}, false},
	{"messages.move", []string{"move", "{html}", "--to", "Archive", "-a", "demo", "--confirm-count", "1", "--dry-run"}, false},
	{"messages.trash", []string{"trash", "{answer}", "--from", "INBOX", "-a", "demo", "--confirm-count", "1"}, false},
	{"actions.approve", []string{"action", "approve", "3"}, false},
	{"messages.trash", []string{"trash", "{html}", "-a", "demo"}, false},
	{"actions.reject", []string{"action", "reject", "4", "--reason", "nein"}, false},
	{"actions.get", []string{"action", "show", "3"}, false},
	{"actions.list", []string{"action", "list", "-a", "demo", "--state", "requested,rejected", "--limit", "5"}, false},
	{"drafts.cleanup_copies", []string{"draft", "cleanup", "1"}, false},
	// S3.4: Sicherung in einen Ordner; Tar-Strom nur als Trockenlauf (die
	// Ausgabe geht dann auf stderr, stdout gehört dem Strom).
	{"backup.create", []string{"backup", "create", "--to", "{backupdir}"}, false},
	{"backup.create", []string{"backup", "create", "--tar", "-", "--dry-run"}, false},
	// T3: Löschkonzept (E8); im Testbestand ist nichts alt genug, beide
	// zählen nur.
	{"retention.purge", []string{"purge", "--older-than", "30d", "-a", "demo"}, false},
	{"blobs.gc", []string{"blob", "gc", "--dry-run"}, false},
	{"audit.rotate", []string{"audit", "rotate", "--dry-run"}, false},
}

// sampleCode: erwarteter Exit-Code je Beispiel, wenn nicht 0 (Antrag auf
// Papierkorb: Exit 4, needs_approval; die Ausgabe nennt den Antrag).
var sampleCode = map[string]int{
	"trash {answer} --from INBOX -a demo --confirm-count 1": ExitNeedsApproval,
	"trash {html} -a demo": ExitNeedsApproval,
}

// seedS16: Konto demo mit den drei Nachrichten aus s16Server, gesynct;
// liefert die Platzhalter für cliSamples.
func seedS16(t *testing.T) map[string]string {
	t.Helper()
	addr := s16Server(t, "Archive", "Trash")
	if code, out, errs := runIn(t, canary+"\n", "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, "sync", "-a", "demo"); code != ExitOK {
		t.Fatalf("sync: %d %s %s", code, out, errs)
	}
	show := func(mid string) ops.MessageOutput {
		_, out, _ := run(t, "show", mid, "--json")
		var m ops.MessageOutput
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("show %s: %v %s", mid, err, out)
		}
		return m
	}
	html, answer := show("html@example.org"), show("antwort@example.org")
	var boxes ops.MailboxesListOutput
	if _, out, _ := run(t, "mailbox", "list", "-a", "demo", "--json"); json.Unmarshal([]byte(out), &boxes) != nil || len(boxes.Mailboxes) == 0 {
		t.Fatalf("seed: mailbox list %s", out)
	}
	_, out, _ := run(t, "search", "Heizung", "--json", "--limit", "1")
	var hit ops.SearchHitOutput
	_ = json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &hit)
	if len(html.Attachments) != 1 || answer.ThreadID == 0 || hit.Cursor == "" {
		t.Fatalf("seed: html=%+v answer=%+v hit=%+v", html.Attachments, answer.ThreadID, hit)
	}
	// mbox mit einer neuen Nachricht und einer zweiten Fassung von
	// html@example.org (gleiche Message-ID, anderer Inhalt; seit rc6 eine
	// eigene Nachricht, seit T1c ist die Message-ID damit mehrdeutig, und
	// show nimmt dafür antwort@example.org); Label-Ordner.
	dir := t.TempDir()
	mbox := filepath.Join(dir, "Archiv")
	labels := filepath.Join(dir, "labels")
	if err := os.WriteFile(mbox, []byte("From - Thu Oct 15 10:00:00 2026\nX-Mozilla-Status: 0001\nSubject: neu\nMessage-ID: <neu@example.org>\n\nneu\n\n"+
		"From - Thu Oct 15 10:00:00 2026\nSubject: x\nMessage-ID: <html@example.org>\n\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(labels, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(labels, "Wichtig"), []byte("From - Thu Oct 15 10:00:00 2026\nSubject: x\nMessage-ID: <neu@example.org>\n\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"mbox": mbox, "labels": labels, "html": fmt.Sprint(html.ID), "answer": fmt.Sprint(answer.ID), "thread": fmt.Sprint(answer.ThreadID),
		"att": fmt.Sprint(html.Attachments[0].ID), "cursor": hit.Cursor, "mailbox": fmt.Sprint(boxes.Mailboxes[0].ID), "exportdir": filepath.Join(t.TempDir(), "audit"),
		"backupdir":    filepath.Join(t.TempDir(), "backup"),
		"exporttarget": filepath.Join(t.TempDir(), "export")}
}

var errCaptureOnly = errors.New("captured")

func TestCLIContractRegistry(t *testing.T) {
	dataDir, _ := setupEnv(t)
	t.Setenv("POSTKAUZ_PASSWORD_ZWEI", "x")
	t.Setenv("POSTKAUZ_PASSWORD_LEER", "x")
	ph := seedS16(t)
	// Dateien für Entwürfe und Identität; Anhänge nur aus attach_dir.
	files := t.TempDir()
	for name, text := range map[string]string{"body": "**Hallo**", "html-body": "<p>Hallo</p>", "sigtext": "Gruß", "sightml": "<p>Gruß</p>"} {
		ph[name] = filepath.Join(files, name)
		if err := os.WriteFile(ph[name], []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "attach"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "attach", "notiz.txt"), []byte("Notiz"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(t, "account", "add", "leer", "--imap", "127.0.0.1:1", "--user", "u", "--tls", "none",
		"--insecure-test-server", "--password-from-env"); code != ExitOK {
		t.Fatalf("account add leer: %s", errs)
	}
	cat := map[string]ops.Operation{}
	for _, op := range ops.Catalog(ops.Deps{}) {
		cat[op.Name] = op
	}
	covers := map[string][]string{}
	for _, b := range bindings {
		for _, f := range b.covers {
			if !slices.Contains(covers[b.op], f) {
				covers[b.op] = append(covers[b.op], f)
			}
		}
	}
	set := map[string]map[string]bool{}
	ran := map[string]bool{}
	for _, s := range cliSamples {
		args := make([]string, 0, len(s.args)+1)
		for _, a := range s.args {
			if strings.HasPrefix(a, "{") {
				a = ph[strings.Trim(a, "{}")]
			}
			args = append(args, a)
		}
		args = append(args, "--json")
		var reqs []ops.Request
		observeRequest = func(r ops.Request) error {
			reqs = append(reqs, r)
			if s.captureOnly {
				return errCaptureOnly
			}
			return nil
		}
		code, out, errs := runIn(t, sampleStdin[strings.Join(s.args, " ")], args...)
		observeRequest = nil
		if len(reqs) != 1 || reqs[0].Operation != s.op {
			t.Fatalf("%v: requests %+v, want one %s", args, reqs, s.op)
		}
		op := cat[s.op]
		var inst any
		if err := json.Unmarshal(reqs[0].Input, &inst); err != nil {
			t.Fatal(err)
		}
		if err := resolved(t, op.InputSchema).Validate(inst); err != nil {
			t.Fatalf("%v: input %s does not match the schema of %s: %v", args, reqs[0].Input, s.op, err)
		}
		if set[s.op] == nil {
			set[s.op] = map[string]bool{}
		}
		for k := range inst.(map[string]any) {
			set[s.op][k] = true
			if !slices.Contains(covers[s.op], k) {
				t.Errorf("%v: sets %q, not listed in covers", args, k)
			}
		}
		if s.captureOnly {
			continue
		}
		if want := sampleCode[strings.Join(s.args, " ")]; code != want {
			t.Fatalf("%v: code=%d (want %d) out=%s err=%s", args, code, want, out, errs)
		}
		ran[s.op] = true
		// Ausgabe gegen das OutputSchema (NDJSON: je Zeile gegen items).
		lines := strings.Split(strings.TrimSpace(out), "\n")
		schema, ndjson := op.OutputSchema, false
		switch s.op {
		case "messages.search":
			schema, ndjson = subSchema(t, op.OutputSchema, "hits"), true
		case "audit.list":
			schema, ndjson = subSchema(t, op.OutputSchema, "events"), true
		}
		if !ndjson && len(lines) != 1 {
			t.Fatalf("%v: want one JSON object, got %d lines", args, len(lines))
		}
		rs := resolved(t, schema)
		for _, l := range lines {
			if l == "" {
				continue
			}
			var v map[string]any
			if err := json.Unmarshal([]byte(l), &v); err != nil {
				t.Fatalf("%v: not JSON: %s", args, l)
			}
			if _, head := v["explain"]; head && ndjson {
				continue // Kopfzeile von search --explain
			}
			if err := rs.Validate(v); err != nil {
				t.Fatalf("%v: output does not match the output schema of %s: %v\n%s", args, s.op, err, l)
			}
		}
	}
	for name := range cat {
		if !ran[name] {
			t.Errorf("no CLI sample ran %s", name)
		}
		var got []string
		for k := range set[name] {
			got = append(got, k)
		}
		if g, w := sorted(got), sorted(covers[name]); !slices.Equal(g, w) {
			t.Errorf("%s: samples set %v, binding covers %v (every flag needs a sample)", name, g, w)
		}
	}
}

// --- Exit-Codes wie docs/cli.md (Abschnitt 4) ----------------------------------

func TestExitCodesMatchDocs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	row := regexp.MustCompile("(?m)^\\| (\\d) \\| `(Exit[A-Za-z]+)` \\|")
	got := map[string]int{}
	for _, m := range row.FindAllStringSubmatch(doc, -1) {
		got[m[2]] = int(m[1][0] - '0')
	}
	want := map[string]int{"ExitOK": ExitOK, "ExitError": ExitError, "ExitUsage": ExitUsage, "ExitDenied": ExitDenied,
		"ExitNeedsApproval": ExitNeedsApproval, "ExitConnection": ExitConnection, "ExitAuth": ExitAuth}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("docs/cli.md exit code table = %v, code = %v", got, want)
	}

	// Jeder Fehlercode, den classify vergibt, steht in der Liste der Codes.
	codesLine := regexp.MustCompile("Codes: (`[a-z_]+`(?:, `[a-z_]+`)*)\\.").FindStringSubmatch(doc)
	if codesLine == nil {
		t.Fatal("docs/cli.md: list of error codes not found")
	}
	documented := map[string]bool{}
	for _, c := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(codesLine[1], -1) {
		documented[c[1]] = true
	}
	cases := []struct {
		err  error
		exit int
		code string
	}{
		{usageError{errors.New("x")}, ExitUsage, "usage"},
		{&ops.InputError{Msg: "x"}, ExitUsage, "invalid_input"},
		{&ops.PolicyError{Decision: policy.Decision{Outcome: policy.Deny}}, ExitDenied, "denied"},
		{&ops.PolicyError{Decision: policy.Decision{Outcome: policy.NeedsApproval}}, ExitNeedsApproval, "needs_approval"},
		{ops.ErrKeyRevoked, ExitDenied, "denied"},
		{ops.ErrKeyInvalid, ExitDenied, "denied"},
		{ops.ErrUnknownOperation, ExitError, "unknown_operation"},
		{&ops.PanicError{Operation: "x"}, ExitError, "internal"},
		{store.ErrAccountExists, ExitError, "exists"},
		{store.ErrMigrationBlocked, ExitError, "migration_blocked"},
		{store.ErrDatabaseBusy, ExitError, "database_busy"},
		{fmt.Errorf("%w: x", store.ErrMigrationMismatch), ExitError, "schema_mismatch"},
		{store.ErrKeyExists, ExitError, "exists"},
		{store.ErrMessageNotFound, ExitError, "not_found"},
		{store.ErrKeyNotFound, ExitError, "not_found"},
		{store.ErrDraftNotFound, ExitError, "not_found"},
		{store.ErrApprovalNotFound, ExitError, "not_found"},
		{store.ErrIdentityNotFound, ExitError, "not_found"},
		{fmt.Errorf("%w: 999", store.ErrActionNotFound), ExitError, "not_found"},
		{&ops.DeniedError{Reason: "approval_stale"}, ExitDenied, "denied"},
		{&ops.PolicyError{Operation: "drafts.approve", Decision: policy.Decision{Outcome: policy.Deny, Reason: "human_only"}}, ExitDenied, "denied"},
		{provider.ErrAuth, ExitAuth, "auth"},
		{provider.ErrConnection, ExitConnection, "connection"},
		{secrets.ErrUnavailable, ExitError, "secret_store_unavailable"},
		{ops.ErrNoSecret, ExitError, "no_secret"},
		{store.ErrSchemaTooNew, ExitError, "schema_too_new"},
		{fmt.Errorf("%w: x", policy.ErrInvalidFile), ExitError, "policy_invalid"},
		{ops.ErrSyncRunning, ExitError, "sync_running"},
		{errAuditMismatch, ExitError, "mismatch"},
		{fmt.Errorf("%w: x", backup.ErrTarget), ExitError, "backup_target"},
		{fmt.Errorf("%w: x", backup.ErrManifest), ExitError, "backup_invalid"},
		{fmt.Errorf("%w: x", backup.ErrCorruptSource), ExitError, "blob_corrupt"},
		{backup.ErrBusy, ExitError, "backup_running"},
		{errors.New("boom"), ExitError, "error"},
	}
	for _, c := range cases {
		exit, info := classify(c.err)
		if exit != c.exit || info.Code != c.code {
			t.Errorf("classify(%v) = %d %s, want %d %s", c.err, exit, info.Code, c.exit, c.code)
		}
		if !documented[info.Code] {
			t.Errorf("error code %q is not listed in docs/cli.md", info.Code)
		}
	}
	for _, c := range []string{"interrupted"} { // vergibt Run selbst
		if !documented[c] {
			t.Errorf("error code %q is not listed in docs/cli.md", c)
		}
	}
}

// Exit-Codes Ende zu Ende: je Code ein echter Aufruf (4 gibt es erst mit
// Freigaben in S2: kein Kommando erreicht heute eine R3-Operation).
func TestExitCodesEndToEnd(t *testing.T) {
	setupEnv(t)
	if code, _, _ := run(t, "key", "create", "--name", "leser", "--role", "reader"); code != ExitOK {
		t.Fatalf("key create: %d", code)
	}
	cases := []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"accounts", "list"}, ExitOK, ""},
		{[]string{"show", "999"}, ExitError, "not_found"},
		{[]string{"bogus"}, ExitUsage, "usage"},
		{[]string{"search", "has:pdf"}, ExitUsage, "invalid_input"},
		{[]string{"show", "1", "--max-chars", "-1"}, ExitUsage, "usage"},
		{[]string{"key", "list", "--key", "leser"}, ExitDenied, "denied"},
		{[]string{"accounts", "list", "--key", "fehlt"}, ExitError, "not_found"},
		{[]string{"account", "test", "demo"}, ExitError, "not_found"},
		{[]string{"audit", "verify"}, ExitOK, ""},
	}
	for _, c := range cases {
		code, _, errs := run(t, append(c.args, "--json")...)
		if code != c.exit {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, code, c.exit, errs)
			continue
		}
		if c.code != "" && !strings.Contains(errs, `"code":"`+c.code+`"`) {
			t.Errorf("%v: stderr %s, want code %s", c.args, errs, c.code)
		}
	}
	// Exit 5: Server nicht erreichbar.
	code, _, errs := runIn(t, "pw\n", "account", "add", "tot", "--imap", "127.0.0.1:1", "--user", "u", "--tls", "none",
		"--insecure-test-server", "--password-stdin")
	if code != ExitOK {
		t.Fatalf("account add: %d %s", code, errs)
	}
	if code, _, errs := run(t, "account", "test", "tot", "--json"); code != ExitConnection || !strings.Contains(errs, `"connection"`) {
		t.Errorf("account test unreachable: exit %d %s", code, errs)
	}
}
