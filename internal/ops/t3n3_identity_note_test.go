package ops

import (
	"fmt"
	"strings"
	"testing"
)

// Dritte Nachbesserung T3, Punkt 9d: Wird die Identität nur über den
// Gmail-Schlüssel gefunden (andere Schreibweise), nennt drafts.create in
// notes, welche Identität mit welcher Adresse genommen wurde; bei der
// exakten Schreibweise und per ID gibt es diesen Hinweis nicht.
func TestT3N3IdentityByKeyNote(t *testing.T) {
	const gm = "@gmail.com"
	e := newDraftEnv(t, false)
	added := e.must(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "pktestowner" + gm}).(IdentityResultOutput)
	want := fmt.Sprintf("identity %d (pktestowner%s) was used", added.Identity.ID, gm)
	for _, c := range []struct {
		sel  string
		note bool
	}{{"pk.test.owner+x" + gm, true}, {"PKTestOwner" + gm, false}, {fmt.Sprint(added.Identity.ID), false}} {
		out, err := e.do(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "to": []string{"a@example.org"},
			"identity": c.sel}, false)
		if err != nil {
			t.Fatalf("drafts.create identity %q: %v", c.sel, err)
		}
		notes := out.(DraftsWriteOutput).Notes
		found := false
		for _, n := range notes {
			if strings.HasPrefix(n, want) && strings.Contains(n, c.sel) {
				found = true
			}
		}
		if found != c.note {
			t.Errorf("identity %q: notes = %q, want the identity note: %v", c.sel, notes, c.note)
		}
	}
}
