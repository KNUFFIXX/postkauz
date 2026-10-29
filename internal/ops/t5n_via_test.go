package ops

import (
	"encoding/json"
	"strings"
	"testing"
)

// Nachbesserung T5, Punkt 9 (e): withVia und withCounts mit den Parametern
// null legen ein leeres Objekt an (vorher Panic); ein Nicht-Objekt bleibt
// unverändert, leere Parameter werden zum Objekt.
func TestT5NWithViaEdge(t *testing.T) {
	o := Origin{Transport: "http", Principal: "reader"}
	cases := []struct{ in, want string }{
		{`null`, `{"via":{"transport":"http","principal":"reader"}}`},
		{``, `{"via":{"transport":"http","principal":"reader"}}`},
		{`{"limit":5}`, `{"limit":5,"via":{"transport":"http","principal":"reader"}}`},
		{`[1]`, `[1]`},
	}
	for _, c := range cases {
		var got json.RawMessage
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("withVia(%q) panics: %v", c.in, r)
				}
			}()
			got = withVia(json.RawMessage(c.in), o)
		}()
		if string(got) != c.want {
			t.Errorf("withVia(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestT5NWithCountsEdge(t *testing.T) {
	counts := map[string]int64{"n": 2}
	cases := []struct{ in, want string }{
		{`null`, `{"counts":{"n":2}}`},
		{``, `{"counts":{"n":2}}`},
		{`{"a":1}`, `{"a":1,"counts":{"n":2}}`},
		{`"x"`, `"x"`},
	}
	for _, c := range cases {
		var got json.RawMessage
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("withCounts(%q) panics: %v", c.in, r)
				}
			}()
			got = withCounts(json.RawMessage(c.in), counts)
		}()
		if string(got) != c.want {
			t.Errorf("withCounts(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// Erreichbarkeit heute (Prüfung F7): Keine Operation lässt die Eingabe null
// durch die Prüfung, und jedes Redact liefert ein Objekt.
func TestT5NRedactAlwaysObject(t *testing.T) {
	for _, op := range Catalog(Deps{}) {
		if err := op.validateInput(json.RawMessage(`null`)); err == nil {
			t.Errorf("%s: input null passes validation", op.Name)
		}
		if op.Redact == nil {
			continue
		}
		inputs := []string{`{}`}
		if in, ok := contractInputs[op.Name]; ok {
			inputs = append(inputs, in.input)
		}
		for _, raw := range inputs {
			if op.validateInput(json.RawMessage(raw)) != nil {
				continue
			}
			p, _ := op.Redact(json.RawMessage(raw))
			if s := strings.TrimSpace(string(p)); len(s) > 0 && s[0] != '{' {
				t.Errorf("%s: Redact(%s) = %s (no object)", op.Name, raw, s)
			}
		}
	}
}
