package drafts

import (
	"errors"
	"testing"
)

// Die erlaubten Übergänge, unabhängig von der Tabelle in state.go
// aufgeschrieben (docs/sicherheit.md, Abschnitt 5; A02 E5, E7). Jedes
// andere Paar aus Zustand und Ereignis ist verboten.
var wantTransitions = map[[2]string][2]string{
	{"draft", "update"}:                   {"draft", ""},
	{"pending_approval", "update"}:        {"draft", "supersede"},
	{"ready", "update"}:                   {"draft", "supersede"},
	{"draft", "request_ready"}:            {"pending_approval", "create"},
	{"pending_approval", "approve"}:       {"ready", "approve"},
	{"pending_approval", "reject"}:        {"draft", "reject"},
	{"draft", "abandon"}:                  {"abandoned", ""},
	{"pending_approval", "abandon"}:       {"abandoned", "supersede"},
	{"ready", "abandon"}:                  {"abandoned", "supersede"},
	{"draft", "mark_sent"}:                {"sent_by_human", ""},
	{"pending_approval", "mark_sent"}:     {"sent_by_human", "close_open"},
	{"ready", "mark_sent"}:                {"sent_by_human", "close_open"},
	{"draft", "sent_detected"}:            {"sent_by_human", ""},
	{"pending_approval", "sent_detected"}: {"sent_by_human", "close_open"},
	{"ready", "sent_detected"}:            {"sent_by_human", "close_open"},
	{"pending_approval", "expire"}:        {"draft", "expire"},
	{"ready", "expire"}:                   {"draft", "expire"},
}

// Zustandstabelle: jeder erlaubte Übergang genau einmal und mit dem
// richtigen Ziel; jedes andere Paar denied, nach Abschluss mit
// draft_closed, sonst invalid_transition.
func TestTransitionsTable(t *testing.T) {
	seen := map[[2]string]int{}
	for _, tr := range AllowedTransitions() {
		k := [2]string{string(tr.From), string(tr.Event)}
		seen[k]++
		w, ok := wantTransitions[k]
		if !ok {
			t.Errorf("unexpected allowed transition %s --%s--> %s", tr.From, tr.Event, tr.To)
			continue
		}
		if string(tr.To) != w[0] || string(tr.Approval) != w[1] {
			t.Errorf("%s --%s--> %s (approval %q), want %s (approval %q)", tr.From, tr.Event, tr.To, tr.Approval, w[0], w[1])
		}
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("transition %v listed %d times", k, n)
		}
	}
	if len(seen) != len(wantTransitions) {
		t.Errorf("%d allowed transitions, want %d", len(seen), len(wantTransitions))
	}
	for _, s := range Statuses {
		for _, ev := range Events {
			tr, err := Next(s, ev)
			w, allowed := wantTransitions[[2]string{string(s), string(ev)}]
			switch {
			case allowed:
				if err != nil || string(tr.To) != w[0] {
					t.Errorf("Next(%s, %s) = %+v, %v; want %s", s, ev, tr, err, w[0])
				}
			case s.Closed():
				if !errors.Is(err, ErrDenied) || ReasonOf(err) != ReasonDraftClosed {
					t.Errorf("Next(%s, %s): err = %v, want denied draft_closed", s, ev, err)
				}
			default:
				if !errors.Is(err, ErrDenied) || ReasonOf(err) != ReasonInvalidTransition {
					t.Errorf("Next(%s, %s): err = %v, want denied invalid_transition", s, ev, err)
				}
			}
		}
	}
}
