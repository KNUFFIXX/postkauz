package compose

import (
	"slices"
	"testing"
)

// Gegenprüfung S2.9, B1: Eine Antwort auf eine eigene Mail an sich selbst
// hatte leere Empfänger (To von o ohne eigene Adressen). Jetzt geht sie wie
// in Gmail und Thunderbird an die ursprünglichen Empfänger; eigene Adressen
// fallen nur weg, solange einer bleibt.
func TestReviewB1ReplyToOwnMessage(t *testing.T) {
	own := []string{"konto@example.com", "buero@example.com", "alias@example.com"}
	self := Address{Name: "Büro", Address: "buero@example.com"}
	cases := []struct {
		name           string
		o              Original
		all            bool
		wantTo, wantCc []string
	}{
		{"own to self, reply", Original{From: []Address{self}, To: []Address{self}}, false,
			[]string{"buero@example.com"}, nil},
		{"own to self, reply all", Original{From: []Address{self}, To: []Address{self}, Cc: []Address{{Address: "Alias@Example.com"}}}, true,
			[]string{"buero@example.com"}, nil},
		{"own to self via alias, reply", Original{From: []Address{self}, To: []Address{{Address: "Alias@Example.com"}}}, false,
			[]string{"alias@example.com"}, nil},
		{"own without To, reply", Original{From: []Address{self}}, false,
			[]string{"buero@example.com"}, nil},
		{"own to third, reply", Original{From: []Address{self}, To: []Address{self, {Address: "dritte@example.net"}},
			Cc: []Address{{Address: "vierte@example.net"}}}, false,
			[]string{"dritte@example.net"}, nil},
		{"own to third, reply all", Original{From: []Address{self}, To: []Address{self, {Address: "dritte@example.net"}},
			Cc: []Address{{Address: "Alias@Example.com"}, {Address: "vierte@example.net"}}}, true,
			[]string{"dritte@example.net"}, []string{"vierte@example.net"}},
		{"own to self, third in cc, reply all", Original{From: []Address{self}, To: []Address{self}, Cc: []Address{{Address: "dritte@example.net"}}}, true,
			[]string{"dritte@example.net"}, nil},
		{"foreign, reply", *original(), false, []string{"erika@example.org"}, nil},
		{"foreign, reply all", *original(), true, []string{"erika@example.org"}, []string{"dritte@example.net", "vierte@example.net"}},
		{"foreign to self only, reply all", Original{From: []Address{{Address: "erika@example.org"}}, To: []Address{self}}, true,
			[]string{"erika@example.org"}, nil},
	}
	for _, c := range cases {
		to, cc := ReplyRecipients(c.o, own, c.all)
		if got := addrs(to); !slices.Equal(got, c.wantTo) {
			t.Errorf("%s: to %v, want %v", c.name, got, c.wantTo)
		}
		if got := addrs(cc); !slices.Equal(got, c.wantCc) {
			t.Errorf("%s: cc %v, want %v", c.name, got, c.wantCc)
		}
	}

	// Durch den ganzen Nachrichtenbau: eigene Mail an sich selbst.
	for _, kind := range []Kind{KindReply, KindReplyAll} {
		in := baseInput(kind)
		in.Original.From = []Address{self}
		in.Original.To = []Address{self}
		in.Original.Cc = nil
		m := mustBuild(t, in)
		if got := addrs(m.To); !slices.Equal(got, []string{"buero@example.com"}) || len(m.Cc) != 0 {
			t.Errorf("%s to self: to %v cc %v", kind, got, addrs(m.Cc))
		}
	}
}
