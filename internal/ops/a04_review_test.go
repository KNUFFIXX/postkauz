package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// Nachbesserung A04 nach der Gegenprüfung: K1 (derive_recipients=false nur
// bei reply/reply_all), K4 (Wahrheitswert im Audit), K5 (drafts.update ohne
// to behält die leere Empfängerliste).
func TestA04ReviewDeriveRecipients(t *testing.T) {
	e := newDraftEnv(t, false)
	orig := e.addOriginal(t, "From: Anna Beispiel <anna@example.org>\r\nTo: Demo <demo@example.org>\r\n"+
		"Message-ID: <a04k@example.org>\r\nSubject: Hallo\r\n\r\nText")

	// K1: false bei new und forward ist ein Eingabefehler, ohne Wert in der
	// Meldung; true bleibt erlaubt.
	for _, in := range []map[string]any{
		{"kind": "new", "account": "demo", "derive_recipients": false, "to": []string{"kanarie-k1@example.org"}},
		{"kind": "forward", "in_reply_to": orig, "derive_recipients": false, "to": []string{"kanarie-k1@example.org"}},
	} {
		_, err := e.do(t, assistant, "drafts.create", in, false)
		var ie *InputError
		if !errors.As(err, &ie) || !strings.Contains(err.Error(), "derive_recipients") || strings.Contains(err.Error(), "kanarie-k1") {
			t.Fatalf("%v: error %v, want invalid input naming the field only", in["kind"], err)
		}
	}
	e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "derive_recipients": true})
	e.must(t, assistant, "drafts.create", map[string]any{"kind": "forward", "in_reply_to": orig, "derive_recipients": true})

	// K5: Ohne to bleibt der Entwurf nach einer Änderung ohne Empfänger.
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "body_markdown": "Noch offen",
		"derive_recipients": false}).(DraftsWriteOutput)
	up := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "body_markdown": "Noch immer offen"}).(DraftsWriteOutput)
	if len(up.Preview.To)+len(up.Preview.Cc)+len(up.Preview.Bcc) != 0 || !slices.Contains(up.Warnings, NoRecipientsWarning) ||
		up.Preview.InReplyTo != "<a04k@example.org>" {
		t.Fatalf("update without to: %+v warnings %q", up.Preview, up.Warnings)
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if len(got.To)+len(got.Cc)+len(got.Bcc) != 0 {
		t.Fatalf("get after update: to %v cc %v bcc %v", got.To, got.Cc, got.Bcc)
	}

	// K4: Das Audit nennt den Wahrheitswert, nie Adressen.
	evs, _ := e.auditJSON(t)
	var withFalse, withTrue int
	for _, ev := range evs {
		if ev.Operation != "drafts.create" {
			continue
		}
		p := string(ev.ParamsRedacted)
		if strings.Contains(p, "kanarie-k1") {
			t.Fatalf("audit contains an address: %s", p)
		}
		switch {
		case strings.Contains(p, `"derive_recipients":false`):
			withFalse++
		case strings.Contains(p, `"derive_recipients":true`):
			withTrue++
		}
	}
	if withFalse == 0 || withTrue == 0 {
		t.Fatalf("audit entries with derive_recipients false %d, true %d", withFalse, withTrue)
	}
}
