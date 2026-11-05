package ops

import (
	"context"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A06 Teil 5 (Offene Frage 3): drafts.create (reply, reply_all) und
// drafts.update bei Antworten warnen, wenn der Betreff nach Entfernen der
// Präfixe vom Original abweicht; nichts wird verhindert. Weiterleitung und
// Antwort ohne Original im Spiegel: keine Warnung.
func TestA06SubjectDiffersWarning(t *testing.T) {
	e := newDraftEnv(t, true)
	raw := "From: Anna Beispiel <anna@example.org>\r\nTo: Demo <demo@example.org>\r\nMessage-ID: <a06-betreff@example.org>\r\nSubject: Heizung Haus 3\r\n\r\nText"
	orig := e.addOriginalAt(t, 41, "a06-betreff@example.org", "Anna Beispiel <anna@example.org>", "Heizung Haus 3", raw)
	warned := func(out any) bool { return slices.Contains(out.(DraftsWriteOutput).Warnings, SubjectDiffersWarning) }

	for _, kind := range []string{"reply", "reply_all"} {
		// Gleicher Betreff: abgeleitet, mit eigenem Präfix, ohne Präfix.
		for _, subj := range []string{"", "Re: Heizung Haus 3", "AW: AW: Heizung Haus 3", "Heizung Haus 3", "WG: Heizung  Haus 3"} {
			in := map[string]any{"kind": kind, "in_reply_to": orig, "body_markdown": "Danke."}
			if subj != "" {
				in["subject"] = subj
			}
			for _, dry := range []bool{true, false} {
				out, err := e.do(t, assistant, "drafts.create", in, dry)
				if err != nil {
					t.Fatal(err)
				}
				if warned(out) {
					t.Fatalf("%s %q (dry %v): warning for the same subject", kind, subj, dry)
				}
			}
		}
		// Anderer Betreff: Warnung, der Entwurf entsteht trotzdem.
		out, err := e.do(t, assistant, "drafts.create", map[string]any{"kind": kind, "in_reply_to": orig, "body_markdown": "Danke.",
			"subject": "Termin Heizung"}, false)
		if err != nil || !warned(out) || out.(DraftsWriteOutput).DraftID == 0 {
			t.Fatalf("%s other subject: %v, %+v", kind, err, out)
		}
		id := out.(DraftsWriteOutput).DraftID
		// drafts.update: andere Änderung → Warnung bleibt; Betreff zurück → weg.
		if out := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "body_markdown": "Danke!"}); !warned(out) {
			t.Fatalf("%s update keeps the other subject: no warning", kind)
		}
		if out := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": "Re: Heizung Haus 3"}); warned(out) {
			t.Fatalf("%s update back to the original subject: warning", kind)
		}
		if out := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": "Heizung Haus 4"}); !warned(out) {
			t.Fatalf("%s update to another subject: no warning", kind)
		}
	}
	// Weiterleitung mit eigenem Betreff: keine Warnung.
	if out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "forward", "in_reply_to": orig, "to": []string{"b@example.net"},
		"subject": "Zur Info"}); warned(out) {
		t.Fatal("forward: warning")
	}
	// Original fehlt im Spiegel (purge): keine Warnung. Über die
	// Operationen nicht erreichbar (eine Antwort ohne Original lässt sich
	// weder anlegen noch ändern, compose verlangt es); deshalb direkt.
	o := newDraftOps(e.deps)
	for _, d := range []store.Draft{
		{Kind: "reply", InReplyToMsg: 999999, Subject: "Termin Heizung"}, // gelöscht
		{Kind: "reply", Subject: "Termin Heizung"},                       // in_reply_to_msg NULL nach purge
	} {
		if differs, err := o.subjectDiffers(context.Background(), d); err != nil || differs {
			t.Fatalf("original missing (%d): %v, %v", d.InReplyToMsg, differs, err)
		}
	}
	if differs, err := o.subjectDiffers(context.Background(), store.Draft{Kind: "reply", InReplyToMsg: orig, Subject: "Termin"}); err != nil || !differs {
		t.Fatalf("direct check with original: %v, %v", differs, err)
	}
}

// Nachbesserung 1: Ein Originalbetreff über 500 Zeichen wird für die
// Antwort gekürzt; das ist keine Abweichung (vorher falsche Warnung).
func TestA06SubjectWarningLongOriginal(t *testing.T) {
	e := newDraftEnv(t, true)
	long := strings.Repeat("Heizungsabrechnung ", 40) // 760 Zeichen
	long = strings.TrimSpace(long)
	raw := "From: Anna Beispiel <anna@example.org>\r\nTo: Demo <demo@example.org>\r\nMessage-ID: <a06-lang@example.org>\r\nSubject: " + long + "\r\n\r\nText"
	orig := e.addOriginalAt(t, 51, "a06-lang@example.org", "Anna Beispiel <anna@example.org>", long, raw)
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "body_markdown": "Danke."}).(DraftsWriteOutput)
	if n := utf8.RuneCountInString(out.Preview.Subject); n > 500 || n < 400 {
		t.Fatalf("subject %d characters, want shortened to at most 500", n)
	}
	if slices.Contains(out.Warnings, SubjectDiffersWarning) {
		t.Fatalf("warning for the shortened derived subject: %q", out.Warnings)
	}
	up := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "body_markdown": "Danke!"}).(DraftsWriteOutput)
	if slices.Contains(up.Warnings, SubjectDiffersWarning) {
		t.Fatalf("update: warning for the shortened derived subject")
	}
}
