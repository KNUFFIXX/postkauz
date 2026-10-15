package thread

import "testing"

func TestGroup(t *testing.T) {
	nodes := []Node{
		{MessageID: "a@example.invalid"},
		{MessageID: "b@example.invalid", InReplyTo: []string{"a@example.invalid"}},
		// fehlendes Zwischenglied x: verweist nur auf x und a
		{MessageID: "c@example.invalid", References: []string{"a@example.invalid", "x@example.invalid"}},
		// verweist nur auf das fehlende x: hängt trotzdem am Thread
		{MessageID: "d@example.invalid", InReplyTo: []string{"x@example.invalid"}},
		{MessageID: "e@example.invalid"},
		{MessageID: ""},
		{MessageID: ""},
		// Selbstverweis darf nichts verbinden
		{MessageID: "f@example.invalid", References: []string{"f@example.invalid"}},
	}
	g := Group(nodes)
	want := []int{0, 0, 0, 0, 1, 2, 3, 4}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("groups = %v, want %v", g, want)
		}
	}
}
