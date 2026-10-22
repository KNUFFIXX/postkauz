package ops

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Regressionstests zur Gegenprüfung von S2.3 (Befunde B1, K1).

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
		`{"s":"` + v + `"}`:                 "s: too long (at most 3 characters)",
		`{"s":"b"}`:                         "s: does not have the expected form",
		`{"s":"a","l":["` + v + `","bb"]}`:  "l: too many entries (at most 1)",
		`{"s":"a","l":["b"]}`:               "l[]: too short (at least 2 characters)",
		`{"s":"a","o":[{"f":"` + v + `"}]}`: "o[].f: not an allowed value (one of: x, y)",
		`{"s":"a","o":[{"` + v + `":1}]}`:   "o[]: unknown field",
		`{"s":"a","` + v + `":1}`:           "invalid input: unknown field",
		`{"s":"a","n":"` + v + `"}`:         "n: wrong type (want integer)",
		`{"s":"a","n":0}`:                   "n: too small (minimum 1)",
		`{"l":[]}`:                          "required field missing (required: s)",
		`{"s":"` + v + `‮` + v + `xxxx"}`:   "s: too long",
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
