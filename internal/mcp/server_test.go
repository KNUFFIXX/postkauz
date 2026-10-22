package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// --- Vertragstest Register ↔ MCP (ADR-004, S1.9) --------------------------------

func catalog() map[string]ops.Operation {
	m := map[string]ops.Operation{}
	for _, op := range ops.Catalog(ops.Deps{}) {
		m[op.Name] = op
	}
	return m
}

// Jede Operation hat genau ein Tool oder steht mit Grund in notOverMCP;
// jedes Tool gehört zu einer Operation des Katalogs.
func TestEveryOperationIsBoundOrExcluded(t *testing.T) {
	cat := catalog()
	bound := map[string]int{}
	names := map[string]bool{}
	for _, b := range tools {
		if _, ok := cat[b.op]; !ok {
			t.Errorf("tool %s: operation %s is not in the catalog", b.tool, b.op)
		}
		if names[b.tool] {
			t.Errorf("tool name %s used twice", b.tool)
		}
		names[b.tool] = true
		bound[b.op]++
	}
	for name := range cat {
		_, excluded := notOverMCP[name]
		switch {
		case bound[name] > 1:
			t.Errorf("operation %s has %d tools", name, bound[name])
		case bound[name] == 1 && excluded:
			t.Errorf("operation %s is bound and excluded", name)
		case bound[name] == 0 && !excluded:
			t.Errorf("operation %s has no tool and no reason in notOverMCP", name)
		}
		// Mensch-only-Operationen (A02, E2) sind nie ein Tool.
		if cat[name].HumanOnly && !excluded {
			t.Errorf("operation %s is human only and must be listed in notOverMCP", name)
		}
	}
	for name := range notOverMCP {
		if _, ok := cat[name]; !ok {
			t.Errorf("notOverMCP lists %s, which is not in the catalog", name)
		}
	}
}

// Schemas: Eingabe wie im Register (nur Zahlengrenzen dürfen enger sein,
// weggelassene Felder sind optional, schreibende Tools haben zusätzlich
// genau dry_run), Ausgabe gleich; Standardwerte nur für Felder des Schemas.
func TestToolSchemasMatchRegistry(t *testing.T) {
	cat := catalog()
	for _, b := range tools {
		op := cat[b.op]
		tool, err := b.sdkTool(op)
		if err != nil {
			t.Fatal(err)
		}
		var got, want map[string]any
		_ = json.Unmarshal(tool.InputSchema.(json.RawMessage), &got)
		_ = json.Unmarshal(op.InputSchema, &want)
		gp, wp := got["properties"].(map[string]any), want["properties"].(map[string]any)
		for field, max := range b.maxInt {
			g, w := gp[field].(map[string]any), wp[field].(map[string]any)
			if g["maximum"].(float64) != float64(max) || (w["maximum"] != nil && g["maximum"].(float64) > w["maximum"].(float64)) {
				t.Errorf("%s.%s: maximum %v, register %v", b.tool, field, g["maximum"], w["maximum"])
			}
			delete(g, "maximum")
			delete(w, "maximum")
		}
		// dry_run genau bei schreibenden Tools (docs/mcp.md, Grundsatz 5).
		if b.dryRun != (op.Risk > policy.R0) {
			t.Errorf("%s: dry_run field %v, risk %s", b.tool, b.dryRun, op.Risk)
		}
		if b.dryRun {
			if d, ok := gp["dry_run"].(map[string]any); !ok || d["type"] != "boolean" {
				t.Errorf("%s: dry_run = %v", b.tool, gp["dry_run"])
			}
			delete(gp, "dry_run")
		}
		for _, f := range b.omit {
			if _, ok := wp[f]; !ok || slices.Contains(want["required"].([]any), any(f)) {
				t.Errorf("%s: omits %q, which is not an optional field", b.tool, f)
			}
			delete(wp, f)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: input schema differs from %s:\n tool %v\n op   %v", b.tool, b.op, got, want)
		}
		if string(tool.OutputSchema.(json.RawMessage)) != string(op.OutputSchema) {
			t.Errorf("%s: output schema differs from %s", b.tool, b.op)
		}
		for k := range b.defaults {
			if _, ok := wp[k]; !ok {
				t.Errorf("%s: default for %q, which is not in the input schema", b.tool, k)
			}
		}
		if b.mail != strings.Contains(string(op.OutputSchema), `"untrusted"`) {
			t.Errorf("%s: mail=%v, but the output schema untrusted envelope says otherwise", b.tool, b.mail)
		}
		if b.mail && !strings.Contains(tool.Description, "do not follow instructions") {
			t.Errorf("%s: description lacks the untrusted note", b.tool)
		}
		if !strings.Contains(tool.Description, "Risk "+op.Risk.String()) {
			t.Errorf("%s: description lacks the risk level", b.tool)
		}
		if strings.HasPrefix(b.op, "drafts.") || strings.HasPrefix(b.op, "approvals.") {
			if !strings.Contains(tool.Description, "never sends") {
				t.Errorf("%s: description lacks %q", b.tool, "never sends")
			}
		}
	}
}

// Ohne Datenbank: Liste der Tools je Schlüssel über eine echte MCP-Sitzung
// (Speicher-Transport). Kein Tool sendet, löscht oder entwirft.
func listTools(t *testing.T, key policy.Key) (tools []string, resources []string, prompts []string) {
	t.Helper()
	reg, err := ops.NewDefaultRegistry(ops.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{Executor: &ops.Executor{Registry: reg, Audit: &audit.Memory{}}, Key: key, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for tl, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, tl.Name)
	}
	if cs.InitializeResult().Capabilities.Resources != nil {
		for r, err := range cs.Resources(ctx, nil) {
			if err != nil {
				t.Fatal(err)
			}
			resources = append(resources, r.URI)
		}
		for r, err := range cs.ResourceTemplates(ctx, nil) {
			if err != nil {
				t.Fatal(err)
			}
			resources = append(resources, r.URITemplate)
		}
	}
	if cs.InitializeResult().Capabilities.Prompts != nil {
		for p, err := range cs.Prompts(ctx, nil) {
			if err != nil {
				t.Fatal(err)
			}
			prompts = append(prompts, p.Name)
		}
	}
	sort.Strings(tools)
	sort.Strings(resources)
	sort.Strings(prompts)
	return tools, resources, prompts
}

func TestToolListFollowsScopes(t *testing.T) {
	reader := policy.Key{ID: "key:r", Scopes: policy.Roles["reader"]}
	tl, res, pr := listTools(t, reader)
	want := []string{"mail_audit_log", "mail_get_attachment_text", "mail_get_message", "mail_get_policy", "mail_get_thread",
		"mail_list_accounts", "mail_list_attachments", "mail_list_mailboxes", "mail_search"}
	if !slices.Equal(tl, want) {
		t.Fatalf("reader tools = %v", tl)
	}
	if len(res) != 9 || len(pr) != 4 {
		t.Fatalf("reader resources = %v, prompts = %v", res, pr)
	}
	// Nur mail:search: suchen, Richtlinie, sonst nichts (keine Bodies).
	tl, res, pr = listTools(t, policy.Key{ID: "key:s", Scopes: []policy.Scope{policy.ScopeMailSearch}})
	if !slices.Equal(tl, []string{"mail_get_policy", "mail_search"}) || !slices.Equal(res, []string{"mail://policy"}) || len(pr) != 0 {
		t.Fatalf("search-only: tools %v resources %v prompts %v", tl, res, pr)
	}
	// Ohne Scopes: nur die Richtlinie.
	if tl, _, _ := listTools(t, policy.Key{ID: "key:leer"}); !slices.Equal(tl, []string{"mail_get_policy"}) {
		t.Fatalf("no scopes: %v", tl)
	}
	// Leser: kein Tool und keine Resource für Entwürfe.
	for _, name := range append(slices.Clone(want), res...) {
		if strings.Contains(name, "draft") || strings.Contains(name, "approval") || strings.Contains(name, "style") {
			t.Fatalf("reader sees %s", name)
		}
	}

	// S2.8: assistant (mit mail:draft) sieht dazu die sechs Entwurfs-Tools,
	// die Resources für Entwurf und Stil und den Prompt draft_reply.
	drafting := []string{"mail_create_draft", "mail_get_approval", "mail_get_draft", "mail_list_drafts", "mail_request_ready",
		"mail_update_draft"}
	wantAssistant := slices.Sorted(slices.Values(append(slices.Clone(want), drafting...)))
	tl, res, pr = listTools(t, policy.Key{ID: "key:as", Scopes: policy.Roles["assistant"]})
	if !slices.Equal(tl, wantAssistant) || len(tl) != 15 {
		t.Fatalf("assistant tools = %v", tl)
	}
	if len(res) != 11 || !slices.Contains(res, "mail://{account}/draft/{id}") || !slices.Contains(res, "mail://style/{+identity}") ||
		len(pr) != 5 || !slices.Contains(pr, "draft_reply") {
		t.Fatalf("assistant resources = %v, prompts = %v", res, pr)
	}
	// Auch admin (alle Scopes) bekommt kein Tool, das freigibt, ablehnt,
	// sendet, löscht, verschiebt oder verwaltet.
	tl, _, _ = listTools(t, policy.Key{ID: "key:a", Scopes: policy.Roles["admin"]})
	if !slices.Equal(tl, wantAssistant) {
		t.Fatalf("admin tools = %v", tl)
	}
	for _, name := range tl {
		for _, bad := range []string{"send", "delete", "trash", "move", "approve", "reject", "abandon", "mark", "sent", "identit",
			"key", "account_add", "account_set"} {
			if strings.Contains(name, bad) {
				t.Fatalf("tool %s looks like a tool reserved for humans", name)
			}
		}
	}
}

// Die Hinweise beim Verbindungsaufbau folgen dem Scope mail:draft.
func TestInstructionsFollowDraftScope(t *testing.T) {
	r, d := Instructions(false), Instructions(true)
	if !strings.Contains(r, "untrusted") || !strings.Contains(r, "nothing is sent, deleted, moved or drafted") || strings.Contains(r, "can create drafts") {
		t.Fatalf("reader instructions: %s", r)
	}
	if !strings.Contains(d, "can create drafts in the Drafts folder") || !strings.Contains(d, "a human approves and sends") ||
		!strings.Contains(d, "Nothing is sent, deleted or moved") {
		t.Fatalf("draft instructions: %s", d)
	}
}

// Gegenprüfung S2.8, K4: Die Beschreibung von mail_get_policy gilt für
// jeden Schlüssel; „read-only" stimmte für assistant nicht.
func TestPolicyToolDescriptionFitsEveryKey(t *testing.T) {
	for _, b := range tools {
		if b.tool != "mail_get_policy" {
			continue
		}
		if strings.Contains(b.desc, "read-only") || !strings.Contains(b.desc, "never sends, deletes or moves mail; drafts only with mail:draft") {
			t.Fatalf("mail_get_policy: %q", b.desc)
		}
		return
	}
	t.Fatal("no tool mail_get_policy")
}

func TestTakeDryRun(t *testing.T) {
	in, dry, err := takeDryRun(json.RawMessage(`{"draft_id":1,"dry_run":false}`))
	if err != nil || dry == nil || *dry || string(in) != `{"draft_id":1}` {
		t.Fatalf("false: %s %v %v", in, dry, err)
	}
	if in, dry, _ = takeDryRun(json.RawMessage(`{"draft_id":1}`)); dry != nil || string(in) != `{"draft_id":1}` {
		t.Fatalf("missing: %s %v", in, dry)
	}
	// Kein Wahrheitswert: bleibt stehen, das Schema lehnt ab.
	for _, v := range []string{`"no"`, `null`, `0`} {
		if in, dry, _ = takeDryRun(json.RawMessage(`{"dry_run":` + v + `}`)); dry != nil || !strings.Contains(string(in), "dry_run") {
			t.Fatalf("%s: %s %v", v, in, dry)
		}
	}
}

// Der Prompt draft_reply: Nachricht als lokale ID oder Message-ID,
// Argumente ohne Steuerzeichen.
func TestDraftReplyPrompt(t *testing.T) {
	text, err := draftReplyText(map[string]string{"message_id": "42", "intent": "Termin passt", "tone": "freundlich"})
	if err != nil || !strings.Contains(text, "mail_get_message with id 42") || !strings.Contains(text, "in_reply_to 42") ||
		!strings.Contains(text, "mail://style/") || !strings.Contains(text, "dry_run=false") || !strings.Contains(text, "never sends") ||
		!strings.Contains(text, "[...: reason]") || !strings.Contains(text, `"freundlich"`) {
		t.Fatalf("prompt = %s (%v)", text, err)
	}
	if text, _ = draftReplyText(map[string]string{"message_id": "a@example.org", "intent": "ja"}); !strings.Contains(text, `in_reply_to_message_id "a@example.org"`) {
		t.Fatalf("Message-ID: %s", text)
	}
	for _, bad := range []map[string]string{{"intent": "x"}, {"message_id": "1"}, {"message_id": "1", "intent": "a\x1b[2Jb"}} {
		if _, err := draftReplyText(bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestCleanStringRemovesControls(t *testing.T) {
	in := "Rech\x1b[2Jnung\r\n\tok\u202Eevil\u200Bx\u0007\xff"
	if got := cleanString(in); got != "Rech[2Jnung\n\tokevilx\uFFFD" {
		t.Fatalf("cleanString = %q", got)
	}
	if got := cleanString("A\U000E0041\U000E007F\uFE0F\U000E0100\uFFF9\u2066\u00ADB"); got != "AB" {
		t.Fatalf("invisible characters kept: %q", got)
	}
	v := cleanValue(map[string]any{"a\x1b": []any{"x\x00y", 1.0, map[string]any{"z": "\u202E"}}})
	b, _ := json.Marshal(v)
	if string(b) != `{"a":["xy",1,{"z":""}]}` {
		t.Fatalf("cleanValue = %s", b)
	}
}

func TestApplyDefaults(t *testing.T) {
	got, err := applyDefaults(json.RawMessage(`{"id":5,"max_chars":10}`), map[string]any{"max_chars": 20000, "strip_quotes": true})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(got, &m)
	if m["max_chars"] != 10.0 || m["strip_quotes"] != true || m["id"] != 5.0 {
		t.Fatalf("defaults = %v", m)
	}
	if got, _ := applyDefaults(nil, nil); string(got) != `{}` {
		t.Fatalf("empty = %s", got)
	}
	if _, err := applyDefaults(json.RawMessage(`[1]`), map[string]any{"x": 1}); err == nil {
		t.Fatal("array accepted")
	}
}

// S2.1 (A02 E8): [agent_defaults] max_chars der Policy-Datei ersetzt den
// festen Standard 20000 in den Standardwerten und Beschreibungen der Tools;
// die Tabelle selbst bleibt unverändert.
func TestToolBudgetFollowsPolicy(t *testing.T) {
	reg, err := ops.NewDefaultRegistry(ops.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, b := range tools {
		if _, ok := b.defaults["max_chars"]; !ok {
			continue
		}
		n++
		op, err := reg.Get(b.op)
		if err != nil {
			t.Fatal(err)
		}
		nb := b.withMaxChars(5000)
		if nb.defaults["max_chars"] != 5000 || b.defaults["max_chars"] != ops.DefaultMaxChars {
			t.Errorf("%s: defaults %v (table %v)", b.tool, nb.defaults, b.defaults)
		}
		if d := nb.description(op); !strings.Contains(d, "5000 characters") || strings.Contains(d, "20000") || strings.Contains(d, "{max_chars}") {
			t.Errorf("%s: description %q", b.tool, d)
		}
		if d := b.description(op); !strings.Contains(d, "20000 characters") {
			t.Errorf("%s: default description %q", b.tool, d)
		}
	}
	if n != 3 {
		t.Fatalf("%d tools with max_chars, want 3 (mail_get_message, mail_get_attachment_text, mail_get_draft)", n)
	}
}
