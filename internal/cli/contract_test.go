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

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// --- Vertragstest Register ↔ CLI (ADR-004, S1.8) -------------------------------
//
// Jede Operation des Katalogs ist über genau ein Kommando erreichbar und
// umgekehrt; die Felder, die ein Kommando setzen kann (binding.covers),
// sind genau die Eigenschaften des InputSchema; jede gebaute Eingabe
// entspricht dem InputSchema, jede --json-Ausgabe dem OutputSchema.

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
	bound := map[string]int{}
	for _, b := range bindings {
		bound[b.op]++
		op, ok := cat[b.op]
		if !ok {
			t.Errorf("binding %v: operation %s is not in the catalog", b.path, b.op)
			continue
		}
		if got, want := sorted(b.covers), schemaProps(t, op.InputSchema); !slices.Equal(got, want) {
			t.Errorf("%s (%s): command sets %v, schema has %v", b.op, strings.Join(b.path, " "), got, want)
		}
	}
	for name := range cat {
		if bound[name] != 1 {
			t.Errorf("operation %s is bound to %d commands, want exactly 1", name, bound[name])
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

var cliSamples = []cliSample{
	{"accounts.list", []string{"accounts", "list"}, false},
	{"accounts.add", []string{"account", "add", "zwei", "--imap", "127.0.0.1:1", "--user", "u@example.invalid", "--email", "z@example.invalid",
		"--tls", "none", "--auth", "app_password", "--insecure-test-server", "--password-from-env", "--dry-run"}, false},
	{"accounts.set", []string{"account", "set", "demo", "--aliases", "Alias@example.org, zweit@example.net"}, false},
	{"accounts.test", []string{"account", "test", "demo"}, false},
	{"accounts.remove", []string{"account", "remove", "leer", "--dry-run"}, false},
	{"sync.run", []string{"sync", "-a", "demo", "--full", "--window", "50", "--mailbox", "INBOX"}, false},
	{"sync.run", []string{"sync", "-a", "demo", "--watch", "--interval", "1m", "--idle-restart", "6m", "--full-check-interval", "30m"}, true},
	{"import.run", []string{"import", "mbox", "{mbox}", "-a", "demo", "--mailbox", "Archiv", "--labels-from", "{labels}", "--rescan"}, false},
	{"mailboxes.list", []string{"mailbox", "list", "-a", "demo"}, false},
	{"mailboxes.overview", []string{"mailbox", "overview", "{mailbox}", "-a", "demo", "--since", "3650d", "--limit", "5"}, false},
	{"messages.get", []string{"show", "{html}", "--strip-quotes", "--max-chars", "10"}, false},
	{"messages.get", []string{"show", "html@example.org", "-a", "demo", "--headers"}, false},
	{"messages.search", []string{"search", "Rechnung", "-a", "demo", "--mailbox", "INBOX", "--limit", "5", "--explain"}, false},
	{"messages.search", []string{"search", "Heizung", "--cursor", "{cursor}"}, false},
	{"threads.get", []string{"thread", "{thread}", "--strip-quotes", "--max-messages", "1", "--max-chars", "5"}, false},
	{"threads.get", []string{"thread", "{answer}", "--message", "-a", "demo"}, false},
	{"attachments.list", []string{"attach", "list", "{html}"}, false},
	{"attachments.text", []string{"attach", "text", "{att}", "--max-chars", "5", "--message", "{html}", "-a", "demo"}, false},
	{"keys.create", []string{"key", "create", "--name", "k1", "--role", "reader", "--scopes", "mail:search", "--expires", "30d",
		"--dry-run-default=false"}, false},
	{"keys.list", []string{"key", "list"}, false},
	{"keys.revoke", []string{"key", "revoke", "k1"}, false},
	{"audit.list", []string{"audit", "--since", "1h", "--until", "2099-01-01", "--actor", "human", "--key-id", "cli",
		"--operation", "keys.list", "--result", "ok", "--limit", "5", "--after-id", "1"}, false},
	{"audit.export", []string{"audit", "export", "--to", "{exportdir}"}, false},
	{"audit.verify", []string{"audit", "verify"}, false},
	{"policy.get", []string{"policy", "show"}, false},
}

// seedS16: Konto demo mit den drei Nachrichten aus s16Server, gesynct;
// liefert die Platzhalter für cliSamples.
func seedS16(t *testing.T) map[string]string {
	t.Helper()
	addr := s16Server(t)
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
	// mbox mit einer neuen und einer vorhandenen Nachricht; Label-Ordner.
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
		"att": fmt.Sprint(html.Attachments[0].ID), "cursor": hit.Cursor, "mailbox": fmt.Sprint(boxes.Mailboxes[0].ID), "exportdir": filepath.Join(t.TempDir(), "audit")}
}

var errCaptureOnly = errors.New("captured")

func TestCLIContractRegistry(t *testing.T) {
	setupEnv(t)
	t.Setenv("POSTKAUZ_PASSWORD_ZWEI", "x")
	t.Setenv("POSTKAUZ_PASSWORD_LEER", "x")
	ph := seedS16(t)
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
		covers[b.op] = b.covers
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
		code, out, errs := run(t, args...)
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
		if code != ExitOK {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, out, errs)
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
		"ExitNeedsApproval": ExitNeedsApproval, "ExitConnection": ExitConnection}
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
		{store.ErrKeyExists, ExitError, "exists"},
		{store.ErrMessageNotFound, ExitError, "not_found"},
		{store.ErrKeyNotFound, ExitError, "not_found"},
		{provider.ErrAuth, ExitError, "auth"},
		{provider.ErrConnection, ExitConnection, "connection"},
		{secrets.ErrUnavailable, ExitError, "secret_store_unavailable"},
		{ops.ErrNoSecret, ExitError, "no_secret"},
		{store.ErrSchemaTooNew, ExitError, "schema_too_new"},
		{fmt.Errorf("%w: x", policy.ErrInvalidFile), ExitError, "policy_invalid"},
		{ops.ErrSyncRunning, ExitError, "sync_running"},
		{errAuditMismatch, ExitError, "mismatch"},
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
