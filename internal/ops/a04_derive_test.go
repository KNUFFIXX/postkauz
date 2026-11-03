package ops

import (
	"errors"
	"slices"
	"testing"
)

// A04 Teil 2: drafts.create mit derive_recipients=false legt eine Antwort im
// Thread an, deren Empfänger nur die genannten sind, auch gar keine. Die
// Kopfzeilen des Verlaufs, der Betreff und das Zitat bleiben; ohne Empfänger
// steht die Warnung wie bei kind=new im Ergebnis, und request_ready lehnt
// ab, bis der Mensch einen Empfänger einträgt. Ohne das Feld (Standard true)
// werden die Empfänger wie bisher abgeleitet.
func TestA04ReplyWithoutDerivedRecipients(t *testing.T) {
	e := newDraftEnv(t, true)
	orig := e.addOriginal(t, "From: Anna Beispiel <anna@example.org>\r\nTo: Demo <demo@example.org>, Dritte <dritte@example.org>\r\n"+
		"Cc: Vierte <vierte@example.org>\r\nMessage-ID: <a04@example.org>\r\nSubject: Hallo\r\n\r\nText")
	// Standard: abgeleitet (Absender, bei reply_all auch To/Cc).
	def := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply_all", "in_reply_to": orig, "body_markdown": "Text"}).(DraftsWriteOutput)
	if !slices.Equal(def.Preview.To, []string{"Anna Beispiel <anna@example.org>"}) || len(def.Preview.Cc) != 2 || slices.Contains(def.Warnings, NoRecipientsWarning) {
		t.Fatalf("default reply_all: to %v cc %v warnings %q", def.Preview.To, def.Preview.Cc, def.Warnings)
	}

	for _, kind := range []string{"reply", "reply_all"} {
		for _, dry := range []bool{true, false} {
			res, err := e.do(t, assistant, "drafts.create", map[string]any{"kind": kind, "in_reply_to": orig, "body_markdown": "Text",
				"to": []string{}, "derive_recipients": false}, dry)
			if err != nil {
				t.Fatalf("%s (dry %v): %v", kind, dry, err)
			}
			out := res.(DraftsWriteOutput)
			p := out.Preview
			if len(p.To)+len(p.Cc)+len(p.Bcc) != 0 || !slices.Contains(out.Warnings, NoRecipientsWarning) {
				t.Fatalf("%s (dry %v): to %v cc %v bcc %v warnings %q", kind, dry, p.To, p.Cc, p.Bcc, out.Warnings)
			}
			if p.InReplyTo != "<a04@example.org>" || p.Subject != "Re: Hallo" || out.Original == nil || out.Original.Part != "quote" {
				t.Fatalf("%s (dry %v): in-reply-to %q subject %q original %+v", kind, dry, p.InReplyTo, p.Subject, out.Original)
			}
			if out.DerivedFromEmail != nil && (slices.Contains(out.DerivedFromEmail.Fields, "to") || slices.Contains(out.DerivedFromEmail.Fields, "cc")) {
				t.Fatalf("%s (dry %v): derived %v", kind, dry, out.DerivedFromEmail.Fields)
			}
		}
	}

	// Nur die genannten Empfänger, keiner aus der Ausgangsnachricht.
	only := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply_all", "in_reply_to": orig, "body_markdown": "Text",
		"cc": []string{"zweit@example.net"}, "derive_recipients": false}).(DraftsWriteOutput)
	if len(only.Preview.To) != 0 || !slices.Equal(only.Preview.Cc, []string{"zweit@example.net"}) || slices.Contains(only.Warnings, NoRecipientsWarning) {
		t.Fatalf("given cc only: to %v cc %v warnings %q", only.Preview.To, only.Preview.Cc, only.Warnings)
	}
	// derive_recipients=true ist der Standard.
	explicitTrue := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "body_markdown": "Text",
		"derive_recipients": true}).(DraftsWriteOutput)
	if !slices.Equal(explicitTrue.Preview.To, def.Preview.To) || len(explicitTrue.Preview.Cc) != 0 {
		t.Fatalf("derive_recipients=true: to %v cc %v", explicitTrue.Preview.To, explicitTrue.Preview.Cc)
	}

	// Der Entwurf ohne Empfänger wird nicht vorgelegt, bis der Mensch die
	// Adresse einträgt; danach geht es wie bei jedem Entwurf.
	id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "body_markdown": "Noch offen",
		"derive_recipients": false}).(DraftsWriteOutput).DraftID
	_, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}, false)
	wantDenied(t, err, "no_recipients")
	up := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "to": []string{"erika@example.org"}}).(DraftsWriteOutput)
	if !slices.Equal(up.Preview.To, []string{"erika@example.org"}) || slices.Contains(up.Warnings, NoRecipientsWarning) || up.Preview.InReplyTo != "<a04@example.org>" {
		t.Fatalf("after adding a recipient: %+v warnings %q", up.Preview, up.Warnings)
	}
	if r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput); r.Status != "pending_approval" {
		t.Fatalf("request with recipient: %+v", r)
	}

	// Falscher Typ: Eingabefehler.
	var ie *InputError
	if _, err := e.do(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "derive_recipients": "no"}, false); !errors.As(err, &ie) {
		t.Fatalf("derive_recipients as string: %v", err)
	}
}
