package thread

import (
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

func day(n int) time.Time { return t0.Add(time.Duration(n) * 24 * time.Hour) }

func TestGroupByHeaders(t *testing.T) {
	msgs := []Message{
		{MessageID: "a@example.invalid", Subject: "Heizung", Date: day(0)},
		{MessageID: "b@example.invalid", InReplyTo: []string{"a@example.invalid"}, Subject: "AW: Heizung", Date: day(1)},
		// fehlendes Zwischenglied x: verweist nur auf x und a
		{MessageID: "c@example.invalid", References: []string{"a@example.invalid", "x@example.invalid"}, Subject: "Re: Heizung", Date: day(2)},
		// verweist nur auf das fehlende x: hängt trotzdem am Thread
		{MessageID: "d@example.invalid", InReplyTo: []string{"x@example.invalid"}, Subject: "Neuer Betreff", Date: day(3)},
		{MessageID: "e@example.invalid", Subject: "Heizung", Date: day(4)}, // gleicher Betreff ohne Präfix: eigener Thread
		{MessageID: "", Subject: "ohne ID"},
		{MessageID: "", Subject: "ohne ID"},
		// Selbstverweis darf nichts verbinden
		{MessageID: "f@example.invalid", References: []string{"f@example.invalid"}, Subject: "f"},
	}
	g := Group(msgs)
	want := []int{0, 0, 0, 0, 1, 2, 3, 4}
	if !slices.Equal(g, want) {
		t.Fatalf("groups = %v, want %v", g, want)
	}
}

// Betreff-Rückfall: Eine Antwort ohne Verweise hängt am Thread gleichen
// Betreffs im Zeitfenster, unabhängig von der Reihenfolge der Ankunft.
func TestSubjectFallbackIsOrderIndependent(t *testing.T) {
	anna, bert := []string{"anna@example.org"}, []string{"bert@example.org"}
	msgs := []Message{
		{MessageID: "o@example.invalid", Subject: "Wasserschaden Bad", Date: day(0), Participants: anna},
		{MessageID: "r1@example.invalid", InReplyTo: []string{"o@example.invalid"}, Subject: "AW: Wasserschaden Bad", Date: day(2), Participants: anna},
		{MessageID: "r2@example.invalid", Subject: "Re: Wasserschaden Bad", Date: day(5), Participants: anna},                       // Waise
		{MessageID: "r3@example.invalid", Subject: "WG: AW: wasserschaden  bad", Date: day(6), Participants: append(anna, bert...)}, // Waise
		{MessageID: "late@example.invalid", Subject: "AW: Wasserschaden Bad", Date: day(80), Participants: anna},                    // außerhalb des Fensters
		{MessageID: "other@example.invalid", Subject: "Re: Anderes Thema", Date: day(1), Participants: anna},
	}
	base := msgs
	want := []bool{true, true, true, true, false, false} // im Thread des Originals?
	perms := [][]int{{0, 1, 2, 3, 4, 5}, {5, 4, 3, 2, 1, 0}, {2, 3, 0, 5, 1, 4}, {3, 1, 4, 2, 0, 5}}
	for _, p := range perms {
		in := make([]Message, len(p))
		for i, k := range p {
			in[i] = base[k]
		}
		g := Group(in)
		pos := map[int]int{}
		for i, k := range p {
			pos[k] = g[i]
		}
		for k, w := range want {
			if (pos[k] == pos[0]) != w {
				t.Errorf("order %v: message %d in thread of the original = %v, want %v (groups %v)", p, k, pos[k] == pos[0], w, g)
			}
		}
	}
}

// Gleichlautende Antworten fremder Absender laufen nicht zusammen: Der
// Betreff-Rückfall verlangt gemeinsame Beteiligte, in jeder Reihenfolge.
func TestSubjectFallbackNeedsSharedParticipants(t *testing.T) {
	msgs := []Message{
		{MessageID: "a1@example.invalid", Subject: "Rechnung", Date: day(0), Participants: []string{"anna@example.org"}},
		// Waise von Anna (Antwort ohne Verweise): gehört dazu
		{MessageID: "a2@example.invalid", Subject: "AW: Rechnung", Date: day(1), Participants: []string{"anna@example.org"}},
		// fremde Absender mit demselben Betreff: je eigener Thread
		{MessageID: "x1@example.invalid", Subject: "Re: Rechnung", Date: day(2), Participants: []string{"xaver@example.net"}},
		{MessageID: "y1@example.invalid", Subject: "AW: Rechnung", Date: day(3), Participants: []string{"yvonne@example.com"}},
		// Waise ohne Beteiligte (nur an das eigene Konto): kein Rückfall
		{MessageID: "n1@example.invalid", Subject: "AW: Rechnung", Date: day(4)},
		// Antwort an Anna in Kopie von Carl: gehört dazu
		{MessageID: "c1@example.invalid", Subject: "Re: Rechnung", Date: day(5), Participants: []string{"carl@example.org", "anna@example.org"}},
	}
	want := []bool{true, true, false, false, false, true}
	perms := [][]int{{0, 1, 2, 3, 4, 5}, {5, 4, 3, 2, 1, 0}, {2, 1, 3, 5, 0, 4}, {1, 2, 3, 4, 5, 0}}
	for _, p := range perms {
		in := make([]Message, len(p))
		for i, k := range p {
			in[i] = msgs[k]
		}
		g := Group(in)
		pos := map[int]int{}
		for i, k := range p {
			pos[k] = g[i]
		}
		for k, w := range want {
			if (pos[k] == pos[0]) != w {
				t.Errorf("order %v: message %d in thread of the original = %v, want %v (groups %v)", p, k, pos[k] == pos[0], w, g)
			}
		}
		if pos[2] == pos[3] {
			t.Errorf("order %v: the two foreign senders share a thread (groups %v)", p, g)
		}
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{"Anna B <Anna@Example.ORG>": "anna@example.org", " b@example.org ": "b@example.org",
		"ohne Adresse": "", `"X <y>" <z@example.net>`: "z@example.net"} {
		if got := NormalizeAddress(in); got != want {
			t.Errorf("NormalizeAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeSubject(t *testing.T) {
	for in, want := range map[string]struct {
		norm  string
		reply bool
	}{
		"Heizung":                      {"heizung", false},
		"AW: Heizung":                  {"heizung", true},
		"Re: AW: WG:  Heizung  defekt": {"heizung defekt", true},
		"RE[2]: Heizung":               {"heizung", true},
		"Fwd: Heizung (fwd)":           {"heizung", true},
		"[Mieterbund] Rundbrief":       {"rundbrief", false},
		"[Liste] Re: Rundbrief":        {"rundbrief", true},
		"Antw: Frage":                  {"frage", true},
		"Regal: Aufbau":                {"regal aufbau", false},
		"Fenster – kaputt 🔧 (Nr. 4)":   {"fenster kaputt nr 4", false},
		"AW: Fenster - kaputt (Nr. 4)": {"fenster kaputt nr 4", true},
		"":                             {"", false},
	} {
		n, r := NormalizeSubject(in)
		if n != want.norm || r != want.reply {
			t.Errorf("NormalizeSubject(%q) = %q, %v; want %q, %v", in, n, r, want.norm, want.reply)
		}
	}
}
