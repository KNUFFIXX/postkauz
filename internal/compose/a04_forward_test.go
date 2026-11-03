package compose

import (
	"slices"
	"strings"
	"testing"
)

// A04 Teil 1: Eine Weiterleitung trägt In-Reply-To und References wie eine
// Antwort (Funktion References, MaxReferences, Derived), damit Gmail sie im
// Verlauf des Originals lässt. Vorher fehlten beide bei forward.
func TestA04ForwardCarriesThreadHeaders(t *testing.T) {
	in := baseInput(KindForward)
	in.To = []string{"kollege@example.net"}
	in.BodyMarkdown = "Zur Info."
	m := mustBuild(t, in)
	reply := mustBuild(t, baseInput(KindReply))
	if m.InReplyTo != reply.InReplyTo || !slices.Equal(m.References, reply.References) || m.InReplyTo == "" {
		t.Fatalf("forward in-reply-to %q refs %v, reply %q %v", m.InReplyTo, m.References, reply.InReplyTo, reply.References)
	}
	for _, f := range []string{DerivedInReplyTo, DerivedReferences} {
		if !slices.Contains(m.Derived, f) {
			t.Errorf("derived %v lacks %s", m.Derived, f)
		}
	}
	// In den Bytes der Nachricht und für den eigenen Parser.
	head, _, _ := strings.Cut(strings.ReplaceAll(string(m.Raw), "\r\n", "\n"), "\n\n")
	if !strings.Contains(head, "In-Reply-To: <orig.42@example.org>") ||
		!strings.Contains(head, "References: <root@example.org> <parent@example.org>") {
		t.Errorf("header block:\n%s", head)
	}
	p := parse(t, m.Raw)
	if !slices.Equal(p.InReplyTo, []string{"orig.42@example.org"}) || !slices.Equal(p.References, m.References) {
		t.Errorf("parsed in-reply-to %v references %v", p.InReplyTo, p.References)
	}

	// Ohne References der Ausgangsnachricht: deren In-Reply-To + Message-ID.
	in = baseInput(KindForward)
	in.Original.References = nil
	in.Original.InReplyTo = []string{"parent@example.org"}
	if m := mustBuild(t, in); !slices.Equal(m.References, []string{"parent@example.org", "orig.42@example.org"}) {
		t.Errorf("references from in-reply-to: %v", m.References)
	}

	// Lange Kette: der erste und die letzten MaxReferences-1 Einträge.
	in = baseInput(KindForward)
	in.Original.References = nil
	for i := range 30 {
		in.Original.References = append(in.Original.References, "r"+strings.Repeat("x", i)+"@example.org")
	}
	m = mustBuild(t, in)
	if len(m.References) != MaxReferences || m.References[0] != "r@example.org" || m.References[MaxReferences-1] != "orig.42@example.org" {
		t.Errorf("long chain: %d entries %v", len(m.References), m.References)
	}

	// Ausgangsnachricht ohne Message-ID: kein In-Reply-To, nichts abgeleitet.
	in = baseInput(KindForward)
	in.Original.MessageID, in.Original.References, in.Original.InReplyTo = "", nil, nil
	m = mustBuild(t, in)
	if m.InReplyTo != "" || len(m.References) != 0 || slices.Contains(m.Derived, DerivedInReplyTo) {
		t.Errorf("without message-id: %q %v derived %v", m.InReplyTo, m.References, m.Derived)
	}

	// Ein neuer Entwurf trägt weiter keine.
	if m := mustBuild(t, baseInput(KindNew)); m.InReplyTo != "" || len(m.References) != 0 {
		t.Errorf("new: %q %v", m.InReplyTo, m.References)
	}
}

// Nachbesserung A04, K5: Ausgangsnachricht ohne Message-ID, aber mit
// References: References wie bei einer Antwort (ohne eigene ID), kein
// In-Reply-To.
func TestA04ForwardWithoutMessageIDKeepsReferences(t *testing.T) {
	for _, kind := range []Kind{KindForward, KindReply} {
		in := baseInput(kind)
		in.Original.MessageID = ""
		in.Original.InReplyTo = nil
		in.Original.References = []string{"root@example.org", "parent@example.org"}
		m := mustBuild(t, in)
		if m.InReplyTo != "" || !slices.Equal(m.References, []string{"root@example.org", "parent@example.org"}) {
			t.Errorf("%s: in-reply-to %q references %v", kind, m.InReplyTo, m.References)
		}
		if slices.Contains(m.Derived, DerivedInReplyTo) || !slices.Contains(m.Derived, DerivedReferences) {
			t.Errorf("%s: derived %v", kind, m.Derived)
		}
		head, _, _ := strings.Cut(strings.ReplaceAll(string(m.Raw), "\r\n", "\n"), "\n\n")
		if strings.Contains(head, "In-Reply-To:") || !strings.Contains(head, "References: <root@example.org> <parent@example.org>") {
			t.Errorf("%s: header block:\n%s", kind, head)
		}
	}
}
