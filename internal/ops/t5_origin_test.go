package ops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// T5 (ADR-010 Punkt 9): Request.Origin steht in Start und Abschluss unter
// params.via, auch bei einer Ablehnung; ohne Origin bleiben die Parameter
// wie bisher (stdio, CLI).
func TestExecutorWritesOrigin(t *testing.T) {
	reg := NewRegistry()
	op := newTestOp("m.read", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return map[string]any{}, nil })
	op.Redact = func(json.RawMessage) (json.RawMessage, []string) { return json.RawMessage(`{"limit":5}`), nil }
	if err := reg.Register(op); err != nil {
		t.Fatal(err)
	}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au}
	key := policy.Key{ID: "key:r", Name: "r", Scopes: []policy.Scope{policy.ScopeMailRead}}
	o := &Origin{Transport: "http", Principal: "r", Issuer: "https://auth.example.net", Subject: "u-1"}
	if _, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: "m.read", Input: json.RawMessage(`{}`), Origin: o}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Execute(context.Background(), Request{Key: policy.Key{ID: "key:n", Name: "n"}, Actor: audit.ActorAgent, Operation: "m.read",
		Input: json.RawMessage(`{}`), Origin: o}); err == nil {
		t.Fatal("key without scope was allowed")
	}
	if _, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: "m.read", Input: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	ev := au.Events()
	if len(ev) != 5 {
		t.Fatalf("%d events, want 2 + 1 + 2", len(ev))
	}
	want := `{"limit":5,"via":{"transport":"http","principal":"r","iss":"https://auth.example.net","sub":"u-1"}}`
	for i := range 3 {
		if string(ev[i].ParamsRedacted) != want {
			t.Errorf("event %d (%s): %s", i, ev[i].Result, ev[i].ParamsRedacted)
		}
	}
	for _, e := range ev[3:] {
		if strings.Contains(string(e.ParamsRedacted), "via") {
			t.Errorf("without origin: %s", e.ParamsRedacted)
		}
	}
}

// Kein Eingabeschema einer Operation kennt ein Feld via (sonst würde
// withVia es im Audit verdecken).
func TestNoOperationUsesVia(t *testing.T) {
	for _, op := range Catalog(Deps{}) {
		var s struct {
			Properties map[string]any `json:"properties"`
		}
		_ = json.Unmarshal(op.InputSchema, &s)
		if _, ok := s.Properties["via"]; ok {
			t.Errorf("%s has an input field via", op.Name)
		}
	}
}
