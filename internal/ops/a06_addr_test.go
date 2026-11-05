package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// addOriginalAt spiegelt eine Ausgangsnachricht wie addOriginal, mit
// eigener UID, Message-ID, Absenderzeile und eigenem Betreff (mehrere je
// Test).
func (e *draftEnv) addOriginalAt(t *testing.T, uid uint32, msgID, from, subject, raw string) int64 {
	t.Helper()
	ctx := context.Background()
	sha := fmt.Sprintf("sha-a06-%d", uid)
	e.blobs.m[sha] = raw
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	_ = e.st.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES (?,?,1)`, sha, len(raw))
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: e.acct, MailboxID: boxes[0].ID,
		New: []store.NewMessage{{Link: store.LinkFlags{UID: uid}, MessageID: msgID, Subject: subject,
			From: from, Size: int64(len(raw)), BlobSHA256: sha, BlobSize: int64(len(raw)),
			TextPlain: "Text", TextUnquoted: "Text", CleanVersion: 1}},
		State: &store.MailboxState{UIDValidity: 3, UIDNext: uid + 1, Phase: store.PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, msgID)
	if len(ids) != 1 {
		t.Fatalf("original %s: %v", msgID, ids)
	}
	return ids[0]
}

// A06 Teil 1 (Befund Dauerbetrieb 04.11.2026): Ein Antwort-Entwurf an einen
// Absender, dessen Name ein Komma enthält oder gleich seiner Adresse ist,
// ließ sich nicht mehr ändern und nicht vorlegen („to[0]: not an RFC 5322
// address“), weil der Adresstext ohne Anführungszeichen zurück in den
// Parser ging. Jetzt: drafts.update ohne Änderung der Empfänger und
// drafts.request_ready (Trockenlauf) gehen durch; Vorschau und drafts.get
// zeigen den Namen lesbar, mit Sonderzeichen in Anführungszeichen.
func TestA06ReplyToNamesWithSpecials(t *testing.T) {
	e := newDraftEnv(t, true)
	// header: Kopfzeile der Ausgangsnachricht (Outlook quotet den Namen);
	// from: messages.from_addr, wie mailsync ihn ungequotet speichert.
	cases := []struct {
		header, from, msgID, wantTo string
		uid                         uint32
	}{
		{`"Nachname, Vorname" <h@example.com>`, "Nachname, Vorname <h@example.com>", "a06-komma@example.org", `"Nachname, Vorname" <h@example.com>`, 21},
		{`"x@example.com" <x@example.com>`, "x@example.com <x@example.com>", "a06-gleich@example.org", "x@example.com", 22},
		{`=?utf-8?q?M=C3=BCller=2C_J=C3=BCrgen_=28B=C3=BCro=29?= <m@example.com>`, "Müller, Jürgen (Büro) <m@example.com>", "a06-umlaut@example.org",
			`"Müller, Jürgen (Büro)" <m@example.com>`, 23},
	}
	for _, c := range cases {
		raw := "From: " + c.header + "\r\nTo: Demo <demo@example.org>\r\nMessage-ID: <" + c.msgID + ">\r\nSubject: Heizung\r\n\r\nText"
		orig := e.addOriginalAt(t, c.uid, c.msgID, c.from, "Heizung", raw)
		cr := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "body_markdown": "Danke, wir kümmern uns."}).(DraftsWriteOutput)
		if !slices.Equal(cr.Preview.To, []string{c.wantTo}) {
			t.Fatalf("%s: create preview to = %q, want %q", c.from, cr.Preview.To, c.wantTo)
		}
		// Ändern ohne Empfänger: kein invalid_address.
		up, err := e.do(t, assistant, "drafts.update", map[string]any{"draft_id": cr.DraftID, "body_markdown": "Danke, wir kümmern uns heute."}, false)
		if err != nil {
			t.Fatalf("%s: update: %v", c.from, err)
		}
		if got := up.(DraftsWriteOutput).Preview.To; !slices.Equal(got, []string{c.wantTo}) {
			t.Fatalf("%s: update preview to = %q", c.from, got)
		}
		// Vorlegen im Trockenlauf (Neubau): kein invalid_address.
		if _, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": cr.DraftID}, true); err != nil {
			t.Fatalf("%s: request_ready (dry run): %v", c.from, err)
		}
		get := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": cr.DraftID}).(DraftDetail)
		if !slices.Equal(get.To, []string{c.wantTo}) || strings.Contains(strings.Join(get.To, ","), "=?") {
			t.Fatalf("%s: get to = %q", c.from, get.To)
		}
		// Was der Agent zurückgibt, geht wieder durch.
		if _, err := e.do(t, assistant, "drafts.update", map[string]any{"draft_id": cr.DraftID, "to": get.To}, false); err != nil {
			t.Fatalf("%s: update with the shown to: %v", c.from, err)
		}
	}
}

// Bestand: Ein vor A06 gespeicherter Entwurf hält Name und Adresse getrennt
// (store.Addr); er heilt ohne Datenkorrektur beim nächsten update bzw.
// request_ready. Nachgestellt, indem die Empfänger direkt in der Tabelle
// stehen wie nach drafts.create in rc3.
func TestA06StoredDraftHealsOnUpdate(t *testing.T) {
	e := newDraftEnv(t, true)
	raw := "From: \"Nachname, Vorname\" <h@example.com>\r\nTo: Demo <demo@example.org>\r\nMessage-ID: <a06-bestand@example.org>\r\nSubject: Heizung\r\n\r\nText"
	orig := e.addOriginalAt(t, 31, "a06-bestand@example.org", "Nachname, Vorname <h@example.com>", "Heizung", raw)
	id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": orig, "body_markdown": "Danke."}).(DraftsWriteOutput).DraftID
	var to string
	if err := e.st.DBForTest().QueryRow(`SELECT to_addrs FROM drafts WHERE id = ?`, id).Scan(&to); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(to, `"name":"Nachname, Vorname"`) || !strings.Contains(to, `"address":"h@example.com"`) {
		t.Fatalf("stored to_addrs = %s", to)
	}
	if r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput); r.Status != "pending_approval" {
		t.Fatalf("request_ready: %+v", r)
	}
}
