package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regressionstests zur Gegenprüfung von S2.3 (Befunde B1, K1, K2, K8, W1,
// W2, W4).

// B1: Eine zu lange oder ungültige Eingabe nennt in der Fehlermeldung nur
// Feld und Regel, nie den Wert; Längen-Grenzen der Entwurfsfelder sind
// denied too_large (A02, „Vorgaben"), nicht invalid_input mit dem Wert.
func TestReviewB1NoValuesInErrors(t *testing.T) {
	e := newDraftEnv(t, false)
	const canary = "KANARIEB1WERT"
	long := func(n int) string { return canary + strings.Repeat("x", n) }
	tooLarge := []struct {
		op string
		in map[string]any
	}{
		{"drafts.create", map[string]any{"account": "demo", "kind": "new", "subject": long(600)}},
		{"drafts.create", map[string]any{"account": "demo", "kind": "new", "body_markdown": long(200001)}},
		{"drafts.create", map[string]any{"account": "demo", "kind": "new", "body_html": "<p>" + long(200001) + "</p>"}},
		{"drafts.update", map[string]any{"draft_id": 1, "subject": long(600)}},
		{"drafts.update", map[string]any{"draft_id": 1, "body_markdown": long(200001)}},
		{"drafts.reject", map[string]any{"approval_id": 1, "reason": long(2001)}},
		{"identities.add", map[string]any{"account": "demo", "email": "neu@example.org", "name": long(201)}},
	}
	for _, c := range tooLarge {
		_, err := e.do(t, humanCLI, c.op, c.in, false)
		wantDenied(t, err, "too_large")
		if strings.Contains(err.Error(), canary) {
			t.Errorf("%s: error contains the value: %.200s", c.op, err)
		}
	}
	invalid := []struct {
		op string
		in map[string]any
	}{
		{"drafts.list", map[string]any{"account": "Konto " + canary}},
		{"drafts.create", map[string]any{"account": "demo", "kind": canary}},
		{"drafts.create", map[string]any{"account": "demo", "kind": "new", "to": canary}},
		{"drafts.create", map[string]any{"account": "demo", "kind": "new", canary: 1}},
		{"accounts.set", map[string]any{"name": "Konto " + canary, "aliases": []string{}}},
		{"accounts.set", map[string]any{"name": "demo", "aliases": []string{canary + " <a@example.org>"}}},
	}
	for _, c := range invalid {
		_, err := e.do(t, humanCLI, c.op, c.in, false)
		var ie *InputError
		if !errors.As(err, &ie) {
			t.Fatalf("%s %v: error %v, want invalid input", c.op, c.in, err)
		}
		if strings.Contains(err.Error(), canary) {
			t.Errorf("%s: error contains the value: %s", c.op, err)
		}
	}
	// Die Meldung nennt Feld und Regel.
	_, err := e.do(t, humanCLI, "drafts.list", map[string]any{"account": "Konto " + canary}, false)
	if !strings.Contains(err.Error(), "account") || !strings.Contains(err.Error(), "expected form") {
		t.Errorf("message without field and rule: %s", err)
	}
	_, err = e.do(t, humanCLI, "drafts.create", map[string]any{"account": "demo", "kind": canary}, false)
	if !strings.Contains(err.Error(), "kind") || !strings.Contains(err.Error(), "one of: new, reply") {
		t.Errorf("enum message: %s", err)
	}
	// Audit: kein Wert, auch nicht als Feldname.
	_, all := e.auditJSON(t)
	if strings.Contains(all, canary) {
		t.Fatalf("audit contains the canary: %s", all)
	}
}

// schemaError für jede Regelart: nie der Wert, immer ein Feldpfad.
func TestSchemaErrorWithoutValue(t *testing.T) {
	rs, err := resolveSchema(json.RawMessage(`{"type":"object","properties":{
		"s":{"type":"string","maxLength":3,"pattern":"^a"},
		"l":{"type":"array","maxItems":1,"items":{"type":"string","minLength":2}},
		"o":{"type":"array","items":{"type":"object","properties":{"f":{"type":"string","enum":["x","y"]}},"additionalProperties":false}},
		"n":{"type":"integer","minimum":1}},
		"required":["s"],"additionalProperties":false}`))
	if err != nil {
		t.Fatal(err)
	}
	const v = "WERTKANARIE"
	for in, want := range map[string]string{
		`{"s":"` + v + `"}`:                    "s: too long (at most 3 characters)",
		`{"s":"b"}`:                            "s: does not have the expected form",
		`{"s":"a","l":["` + v + `","bb"]}`:     "l: too many entries (at most 1)",
		`{"s":"a","l":["b"]}`:                  "l[]: too short (at least 2 characters)",
		`{"s":"a","o":[{"f":"` + v + `"}]}`:    "o[].f: not an allowed value (one of: x, y)",
		`{"s":"a","o":[{"` + v + `":1}]}`:      "o[]: unknown field",
		`{"s":"a","` + v + `":1}`:              "invalid input: unknown field",
		`{"s":"a","n":"` + v + `"}`:            "n: wrong type (want integer)",
		`{"s":"a","n":0}`:                      "n: too small (minimum 1)",
		`{"l":[]}`:                             "required field missing (required: s)",
		`{"s":"` + v + "\u202e" + v + `xxxx"}`: "s: too long",
	} {
		err := validateAgainst(rs, json.RawMessage(in))
		if err == nil {
			t.Fatalf("%s: no error", in)
		}
		if strings.Contains(err.Error(), v) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %q, want %q without the value", in, err, want)
		}
	}
	if err := validateAgainst(rs, json.RawMessage(`{"s":"`+v)); err == nil || strings.Contains(err.Error(), v) {
		t.Errorf("broken JSON: %v", err)
	}
}

// K1: Ein beliebiger Schlüssel der Eingabe (vom Aufrufer gewählt) kommt
// nie ins Audit; nur bekannte Feldnamen, unbekannte gezählt. Jede
// Operation des Katalogs kennt alle eigenen Felder (sonst stünde ein
// bekanntes Feld als unbekannt im Audit).
func TestReviewK1RedactOnlyKnownFields(t *testing.T) {
	const canary = "SchluesselKanarie"
	for _, r := range []func(json.RawMessage) (json.RawMessage, []string){redactDraftInput, redactAccountInput, redactAccountRef, redactAccountSet} {
		params, _ := r(json.RawMessage(`{"name":"demo","account":"demo","` + canary + `":"y"}`))
		if strings.Contains(string(params), canary) || !strings.Contains(string(params), `"unknown_fields":1`) {
			t.Errorf("params = %s", params)
		}
	}
	for _, op := range Catalog(Deps{}) {
		if op.Redact == nil {
			continue
		}
		var s struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(op.InputSchema, &s); err != nil {
			t.Fatal(err)
		}
		in := map[string]any{}
		for k := range s.Properties {
			in[k] = nil
		}
		b, _ := json.Marshal(in)
		params, _ := op.Redact(b)
		if strings.Contains(string(params), "unknown_fields") {
			t.Errorf("%s: own fields counted as unknown: %s", op.Name, params)
		}
	}
}

// W1: Die Freigabe bindet die fertige Nachricht. Ändert sich nach der
// Vorlage etwas, das nur beim Bau hinzukommt (Signatur oder Reply-To der
// Identität), ist approve approval_stale; der Entwurf fällt auf draft
// zurück, die Freigabe ist superseded.
func TestReviewW1ApprovalBindsBuiltMessage(t *testing.T) {
	for name, change := range map[string]string{
		"signature": `UPDATE identities SET signature_text = 'Andere Signatur' WHERE id = 1`,
		"reply-to":  `UPDATE identities SET reply_to = 'umleitung@example.net' WHERE id = 1`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newDraftEnv(t, false)
			// Die Ausgangsnachricht hat kein From: Empfänger ausdrücklich
			// (ohne Empfänger keine Vorlage, Gegenprüfung S2.9, B1).
			out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": 1, "body_markdown": "Antwort",
				"to": []string{"erika@example.org"}}).(DraftsWriteOutput)
			req := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": out.DraftID}).(DraftsRequestOutput)
			if len(req.MessageHash) != 64 {
				t.Fatalf("request = %+v", req)
			}
			if ap := e.must(t, assistant, "approvals.get", map[string]any{"approval_id": req.ApprovalID}).(ApprovalOutput); ap.MessageHash != req.MessageHash {
				t.Fatalf("approval = %+v", ap)
			}
			if err := e.st.ExecForTest(change); err != nil {
				t.Fatal(err)
			}
			_, err := e.do(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID}, false)
			wantDenied(t, err, "approval_stale")
			got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
			ap := e.must(t, assistant, "approvals.get", map[string]any{"approval_id": req.ApprovalID}).(ApprovalOutput)
			if got.Status != "draft" || ap.Status != "superseded" {
				t.Fatalf("after stale approve: draft %s, approval %s", got.Status, ap.Status)
			}
			// Neu vorgelegt, bindet sie die neue Nachricht und lässt sich freigeben.
			req2 := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": out.DraftID}).(DraftsRequestOutput)
			if req2.MessageHash == req.MessageHash {
				t.Fatal("message hash ignores the change")
			}
			if dec := e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req2.ApprovalID}).(DraftsDecideOutput); dec.Status != "ready" {
				t.Fatalf("approve = %+v", dec)
			}
		})
	}
}

// W2: Ein zweiter Schreiber ändert den Text, während der Hook den alten
// Stand prüft. Vorher wurde trotzdem vorgelegt (hook=pass für einen Text,
// den der Hook nie sah); jetzt draft_changed, keine Freigabe.
func TestReviewW2HookRaceRefused(t *testing.T) {
	e := newDraftEnv(t, true)
	const changed = "Geaenderter Text des zweiten Schreibers"
	e.check.during = func() {
		e.check.during = nil
		e.must(t, assistant, "drafts.update", map[string]any{"draft_id": 1, "body_markdown": changed})
	}
	_, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}, false)
	wantDenied(t, err, "draft_changed")
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": 1}).(DraftDetail)
	if got.Status != "draft" || got.ApprovalID != 0 || !strings.Contains(got.BodyText, changed) {
		t.Fatalf("draft = %+v", got)
	}
	// Erneut vorgelegt: Der Hook sieht den neuen Text.
	r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}).(DraftsRequestOutput)
	if r.Status != "pending_approval" || !strings.Contains(string(e.check.eml), "Geaenderter") {
		t.Fatalf("request = %+v", r)
	}
}

// W4: B ändert den Text, während A (nur Betreff) liest und baut; A darf
// B's Text nicht mit dem alten Stand überschreiben.
func TestReviewW4NoLostUpdate(t *testing.T) {
	e := newDraftEnv(t, false)
	const bodyB = "Text von Schreiber B"
	calls := 0
	testHookBeforeUpdateWrite = func() {
		calls++
		if calls == 1 {
			// B schreibt dazwischen (sein eigener Aufruf ist der zweite Haken-Aufruf).
			e.must(t, assistant, "drafts.update", map[string]any{"draft_id": 1, "body_markdown": bodyB})
		}
	}
	t.Cleanup(func() { testHookBeforeUpdateWrite = nil })
	out := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": 1, "subject": "Betreff von A"}).(DraftsWriteOutput)
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": 1}).(DraftDetail)
	if got.Subject != "Betreff von A" || got.BodyMarkdown != bodyB || !strings.Contains(got.BodyText, bodyB) || out.Preview.Subject != "Betreff von A" {
		t.Fatalf("draft = subject %q, markdown %q", got.Subject, got.BodyMarkdown)
	}
	// Ändert ein zweiter Schreiber vor jedem Schreibversuch, gibt A nach
	// updateAttempts Versuchen mit draft_changed auf.
	round := 0
	testHookBeforeUpdateWrite = func() {
		round++
		if round%2 == 1 { // A's Versuch; der Aufruf von B selbst ist gerade
			e.must(t, humanCLI, "drafts.update", map[string]any{"draft_id": 1, "body_markdown": fmt.Sprintf("Runde %d", round)})
		}
	}
	_, err := e.do(t, assistant, "drafts.update", map[string]any{"draft_id": 1, "subject": "nie"}, false)
	wantDenied(t, err, "draft_changed")
	if round != 2*updateAttempts {
		t.Fatalf("rounds = %d", round)
	}
}

// K2: Anhänge werden nur bis zum Restbudget gelesen; vorher las jeder
// Anhang einzeln bis zur vollen Grenze, die Summe prüfte erst der
// Nachrichtenbau.
func TestReviewK2AttachmentBudgetWhileLoading(t *testing.T) {
	e := newDraftEnv(t, false)
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(e.attach, f), []byte(strings.Repeat(f[:1], 600)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := e.must(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new",
		"attachments": []map[string]any{{"file": "a.txt"}, {"file": "b.txt"}}}).(DraftsWriteOutput)
	// Dieselbe Datenbank mit kleinerer Grenze: Die gespeicherten Anhänge
	// (zusammen 1.200 Bytes) passen nicht mehr.
	deps := e.deps
	deps.Draft.MaxAttachmentBytes = 1000
	reg, err := NewDefaultRegistry(deps)
	if err != nil {
		t.Fatal(err)
	}
	x := &Executor{Registry: reg, Audit: e.st.AuditWriter()}
	e.blobs.read = 0
	in, _ := json.Marshal(map[string]any{"draft_id": out.DraftID, "subject": "neu"})
	_, err = x.Execute(context.Background(), Request{Key: humanCLI, Actor: "human", Operation: "drafts.update", Input: in})
	wantDenied(t, err, "too_large")
	if e.blobs.read > 1001 {
		t.Fatalf("read %d bytes for a limit of 1000", e.blobs.read)
	}
	// Dateien unter attach_dir ebenso: ein Fehler, bevor die zweite ganz gelesen ist.
	in, _ = json.Marshal(map[string]any{"account": "demo", "kind": "new", "attachments": []map[string]any{{"file": "a.txt"}, {"file": "b.txt"}}})
	_, err = x.Execute(context.Background(), Request{Key: humanCLI, Actor: "human", Operation: "drafts.create", Input: in})
	wantDenied(t, err, "too_large")
}

// K8: request_ready und update im Trockenlauf speichern keinen Rückfall
// einer abgelaufenen Freigabe (Datenbank unverändert, kein Audit
// drafts.expire), zeigen aber den Stand danach.
func TestReviewK8DryRunStoresNoExpiry(t *testing.T) {
	e := newDraftEnv(t, false)
	if err := e.st.ExecForTest(`UPDATE approvals SET expires_at = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	before := e.state(t)
	out, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	if r := out.(DraftsRequestOutput); r.PreviousStatus != "draft" || r.Status != "pending_approval" || !r.DryRun {
		t.Fatalf("dry run = %+v", r)
	}
	if _, err := e.do(t, assistant, "drafts.update", map[string]any{"draft_id": 2, "subject": "trocken"}, true); err != nil {
		t.Fatal(err)
	}
	if e.state(t) != before {
		t.Fatal("dry run stored the expiry")
	}
	if _, all := e.auditJSON(t); strings.Contains(all, "drafts.expire") {
		t.Fatalf("dry run audited an expiry: %s", all)
	}
}
