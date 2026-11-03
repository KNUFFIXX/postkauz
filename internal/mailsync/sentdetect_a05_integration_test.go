package mailsync

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A05: Versanderkennung bei Antworten (POSTKAUZ_IT; `make integration`).
// Die Ausgangsnachricht trägt einen Verlauf im Outlook-Stil; sie liegt in
// Gesendet, damit der Lauf nur Entwürfe-Ordner und Gesendet spiegeln muss
// (gemeinsame Testkonten). Das Mailprogramm „sendet" wie Gmail im Browser:
// neue Message-ID, Zitat ohne „>".

// a05Original baut die Ausgangsnachricht mit Outlook-Verlauf.
func a05Original(mid, subject, to string) []byte {
	text := "Guten Tag,\n\nanbei die Unterlagen zur Wohnung im zweiten Stock. Bitte prüfen Sie die Abrechnung bis Ende der Woche.\n\n" +
		"Freundliche Grüße\nFamilie Muster\n\n-----Ursprüngliche Nachricht-----\nVon: Hausverwaltung <info@example.org>\n" +
		"Gesendet: Montag, 2. November 2026 10:00\nAn: Familie Muster <muster@example.com>\nBetreff: Heizung\n\n" +
		"Die Heizungsfirma kommt laut Terminplan erst im Dezember; das Thermostat im Badezimmer tauschen wir dann mit."
	return []byte(strings.ReplaceAll(fmt.Sprintf("From: Familie Muster <muster@example.com>\nTo: %s\nSubject: %s\nDate: %s\n"+
		"Message-ID: <%s>\nMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n%s\n", to, subject,
		time.Now().UTC().Format(time.RFC1123Z), mid, text), "\n", "\r\n"))
}

// a05Reply baut die gesendete Antwort; inReplyTo "" = ohne In-Reply-To.
func a05Reply(mid, inReplyTo, subject, text string) []byte {
	irt := ""
	if inReplyTo != "" {
		irt = "In-Reply-To: <" + inReplyTo + ">\nReferences: <" + inReplyTo + ">\n"
	}
	return []byte(strings.ReplaceAll(fmt.Sprintf("From: demo@example.org\nTo: muster@example.com\nSubject: %s\nDate: %s\n"+
		"Message-ID: <%s>\n%sMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n%s\n", subject,
		time.Now().UTC().Format(time.RFC1123Z), mid, irt, text), "\n", "\r\n"))
}

var quotePrefixRe = regexp.MustCompile(`(?m)^> ?`)

// gmailText ist der Klartext eines Entwurfs, wie Gmail im Browser ihn
// sendet: das Zitat ohne „>", added vor der Einleitung eingefügt.
func gmailText(d store.Draft, added string) string {
	text := quotePrefixRe.ReplaceAllString(d.BodyText, "")
	if added != "" {
		i := strings.Index(text, "\nOn ")
		if i < 0 {
			i = strings.Index(text, "\nAm ")
		}
		text = text[:i] + "\n" + added + "\n" + text[i:]
	}
	return text
}

func TestIntegrationSentDetectionReplies(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		e := newITDraftEnv(t, srv, user)
		sent := e.sentPath()
		var sentMids []string
		t.Cleanup(func() { e.cleanupMailbox(sent, sentMids) })
		tag := fmt.Sprintf("%s %d", srv.name, time.Now().UnixNano())
		ctx := context.Background()
		sync := func() ops.SyncOutput {
			t.Helper()
			out, err := e.runner.Sync(ctx, e.acc, provider.PasswordCredential(itPass), ops.SyncOptions{Mailboxes: []string{e.path, sent}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		origs := map[int64]string{} // Entwurf → Message-ID des Originals
		reply := func(name string) store.Draft {
			t.Helper()
			mid := fmt.Sprintf("orig-%s-%d@example.com", name, time.Now().UnixNano())
			e.appendSent(sent, a05Original(mid, "Abrechnung "+name+" "+tag, e.acc.Email), mid, &sentMids)
			sync()
			orig, err := e.st.QueryIntForTest(`SELECT id FROM messages WHERE message_id_header = ?`, mid)
			if err != nil || orig == 0 {
				t.Fatalf("original %s: %d, %v", name, orig, err)
			}
			out, err := e.do(itAssistant, "drafts.create", map[string]any{"kind": "reply", "account": "it", "in_reply_to": orig,
				"body_markdown": "Vielen Dank für die Unterlagen, wir prüfen die Abrechnung und melden uns bis Freitag bei Ihnen."})
			if err != nil {
				t.Fatal(err)
			}
			o := out.(ops.DraftsWriteOutput)
			e.mids = append(e.mids, o.MessageID)
			if out := sync(); out.Drafts == nil || out.Drafts.Pushed != 1 {
				t.Fatalf("push %s: %+v", name, out.Drafts)
			}
			d := e.draft(o.DraftID)
			origs[d.ID] = mid
			if !strings.Contains(d.BodyText, "Ursprüngliche Nachricht") {
				t.Fatalf("%s: quote without the history: %q", name, d.BodyText)
			}
			return d
		}
		// (F, Teil 1) unverändert gesendet, Zitat ohne „>", ohne In-Reply-To
		// (sonst könnte auch der Weg über den Verlauf treffen).
		f := reply("F")
		midF := fmt.Sprintf("webmail-f-%d@example.org", time.Now().UnixNano())
		e.appendSent(sent, a05Reply(midF, "", f.Subject, gmailText(f, "")), midF, &sentMids)
		e.removeDraftCopy(f)
		out := sync()
		if out.DraftCheck == nil || out.DraftCheck.SentByFallback != 1 {
			t.Fatalf("F: check = %+v", out.DraftCheck)
		}
		if d := e.draft(f.ID); d.Status != string(drafts.StatusSentByHuman) {
			t.Fatalf("F = %s/%s", d.Status, d.RemoteState)
		}

		// (D, Teil 2) neue Message-ID, ein Satz ergänzt, In-Reply-To gleich,
		// Kopie entfernt → sent_by_human über den Verlauf. (E) wie D ohne
		// In-Reply-To → bleibt offen (Rückfall unter 0,9).
		added := "Der Hausmeister kommt am Montag vorbei und bringt die Schlüssel."
		dd, de := reply("D"), reply("E")
		midD := fmt.Sprintf("webmail-d-%d@example.org", time.Now().UnixNano())
		e.appendSent(sent, a05Reply(midD, origs[dd.ID], dd.Subject, gmailText(dd, added)), midD, &sentMids)
		e.removeDraftCopy(dd)
		midE := fmt.Sprintf("webmail-e-%d@example.org", time.Now().UnixNano())
		e.appendSent(sent, a05Reply(midE, "", de.Subject, gmailText(de, added)), midE, &sentMids)
		e.removeDraftCopy(de)
		out = sync()
		if out.DraftCheck == nil || out.DraftCheck.SentByThread != 1 || out.DraftCheck.SentByFallback != 0 || out.DraftCheck.Missing != 1 {
			t.Fatalf("D/E: check = %+v", out.DraftCheck)
		}
		if d := e.draft(dd.ID); d.Status != string(drafts.StatusSentByHuman) || d.SentMessageID == 0 {
			t.Fatalf("D = %s", d.Status)
		}
		if d := e.draft(de.ID); d.Status != string(drafts.StatusDraft) || d.RemoteState != drafts.RemoteMissing {
			t.Fatalf("E = %s/%s", d.Status, d.RemoteState)
		}
		n, err := e.st.QueryIntForTest(`SELECT count(*) FROM audit_events WHERE operation = ? AND reason = ? AND actor_type = 'system'`,
			drafts.OpSentDetected, drafts.DetectThread)
		if err != nil || n != 1 {
			t.Fatalf("audit thread: %d, %v", n, err)
		}
	})
}
