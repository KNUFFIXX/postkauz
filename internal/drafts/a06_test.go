package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A06 Teil 3: verwaiste Fortsetzungszeilen. Nachbau des Befunds aus dem
// Dauerbetrieb (03.–05.11.2026, die echten Texte bleiben außerhalb des
// Repos): Gmail im Browser sendet einen unveränderten PostKauz-Entwurf;
// die erste Zitatebene steht ohne „>“, innere Ebenen mit „>“, aber nach
// dem Umbruch (nach Bytes, hier bei 76) stehen ihre Fortsetzungszeilen
// ohne „>“. text_unquoted behielt diese Zeilen, der Rest ab der Einleitung
// passte nicht mehr zum Zitat des Entwurfs, und der Entwurf blieb offen.

const (
	a06OrigID  = "a06-orig@example.org"
	a06OwnText = "Hallo Anna,\n\nvielen Dank für den Hinweis. Den Beleg vom Oktober schicke ich Ihnen morgen früh.\n\nViele Grüße"
	// a06Original: Mail von Anna, unten ein Zitat (innere Ebene) mit langen,
	// ungebrochenen Zeilen (aus dem HTML einer früheren Antwort).
	a06Original = "Hallo Demo,\n\nich habe mir die Unterlagen angesehen. Die Abrechnung für die Wohnung im zweiten Stock passt, " +
		"aber bei den Heizkosten fehlt noch der Beleg vom Oktober. Können Sie den bitte nachreichen?\n\nViele Grüße\nAnna\n\n" +
		"Am 01.11.2026 um 10:00 schrieb Demo <demo@example.org>:\n" +
		"> Hallo Anna, anbei die Abrechnung für das Haus in der Gartenstraße mit allen Belegen, die wir bis Ende Oktober bekommen haben, sortiert nach Monat und Wohnung.\n" +
		"> Die Rechnung des Schornsteinfegers kommt erst nächste Woche, ich schicke sie Ihnen dann sofort weiter.\n" +
		">\n> Viele Grüße\n> Demo"
	// a06Umlaut: innere Ebene mit vielen Umlauten, so dass an einer
	// Umbruchstelle Zeichen und Bytes verschieden entscheiden.
	a06Umlaut = "Hallo Demo,\n\nbitte prüfen.\n\nAm 01.11.2026 um 10:00 schrieb Demo <demo@example.org>:\n" +
		"> Größere Änderungen über Bürgschaften für Müßiggänger übernimmt künftig Jürgen Schüßler aus Köln und Düsseldorf, nicht mehr Björn.\n" +
		"> Gruß"
)

var a06Anna = []store.Addr{{Name: "Anna", Address: "anna@example.org"}}

// a06Wrap bricht s wie Gmail nach Bytes um (höchstens width Bytes je
// Zeile, an Leerzeichen); prefix steht nur vor der ersten Zeile.
func a06Wrap(s, prefix string, width int) []string {
	var out []string
	cur := prefix
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "" || cur == prefix:
			cur += w
		case len(cur)+1+len(w) <= width:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = w
		}
	}
	return append(out, cur)
}

// a06GmailSent baut den Klartext, den Gmail im Browser aus dem HTML eines
// PostKauz-Entwurfs (rc3-Form) macht: eigener Text, Einleitungszeile,
// Leerzeile, Zitattext der ersten Ebene ohne „>“, innere Zeilen mit „> “,
// alles neu umbrochen (Fortsetzungen innerer Zeilen ohne „>“).
func a06GmailSent(own, intro, original string) string {
	lines := []string{own, "", intro, ""}
	for _, l := range strings.Split(original, "\n") {
		switch {
		case strings.TrimSpace(l) == "" || strings.TrimSpace(l) == ">":
			lines = append(lines, strings.TrimSpace(l))
		case strings.HasPrefix(l, "> "):
			lines = append(lines, a06Wrap(strings.TrimPrefix(l, "> "), "> ", 76)...)
		default:
			lines = append(lines, a06Wrap(l, "", 76)...)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

type a06Env struct {
	*env
	m    *mirror
	orig int64
}

func newA06Env(t *testing.T, original string) *a06Env {
	e := newEnv(t)
	m := newMirror(e)
	orig := m.put("inbox", sentMsg{messageID: a06OrigID, subject: "Abrechnung Gartenstraße", from: "Anna <anna@example.org>",
		to: []store.Addr{{Address: "demo@example.org"}}, date: e.now.Add(-time.Hour), text: original})
	return &a06Env{env: e, m: m, orig: orig}
}

// draft baut den Antwort-Entwurf wie drafts.create (compose.Build, also
// body_text wie im Betrieb) und legt ihn im Entwürfe-Ordner ab.
func (e *a06Env) draft(subject, original string) (store.Draft, string) {
	e.t.Helper()
	in := compose.NewInput(compose.KindReply)
	in.AccountAddress, in.Signature = "demo@example.org", false
	in.To, in.ExplicitRecipients, in.Subject, in.BodyMarkdown = []string{"Anna <anna@example.org>"}, true, subject, a06OwnText
	in.Original = &compose.Original{MessageID: a06OrigID, From: []compose.Address{{Name: "Anna", Address: "anna@example.org"}},
		Date: time.Date(2026, 11, 3, 9, 15, 0, 0, time.UTC), Subject: "Abrechnung Gartenstraße", Text: original}
	in.Now = func() time.Time { return e.now }
	msg, err := compose.Build(in)
	if err != nil {
		e.t.Fatal(err)
	}
	r, err := e.svc.Create(context.Background(), NewDraft{AccountID: e.acc, Kind: KindReply, CreatedBy: "key:agent", InReplyToMsg: e.orig,
		Content: Content{To: a06Anna, Subject: subject, BodyMarkdown: a06OwnText, BodyText: msg.BodyText}})
	if err != nil {
		e.t.Fatal(err)
	}
	q := QuoteOf(msg.BodyText)
	if q.Line == nil {
		e.t.Fatalf("draft without quote:\n%s", msg.BodyText)
	}
	intro := ""
	for _, l := range strings.Split(msg.BodyText, "\n") {
		if strings.HasPrefix(l, "On ") && strings.HasSuffix(l, "wrote:") {
			intro = l
		}
	}
	return e.push(r.Draft), intro
}

// push legt die Kopie im Entwürfe-Ordner ab (wie thEnv.draftOf).
func (e *a06Env) push(d store.Draft) store.Draft {
	e.t.Helper()
	ctx := context.Background()
	id := e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now, text: "kopie " + d.BodyText})
	var uid int64
	var hash string
	if err := e.st.DBForTest().QueryRow(`SELECT mm.uid, m.content_hash FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
		WHERE m.id = ?`, id).Scan(&uid, &hash); err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.svc.Pushed(ctx, d.ID, Fingerprint(e.env.draft(d.ID)), PushedCopy{MailboxID: e.m.boxes["drafts"], UIDValidity: 1,
		UID: uid, ContentHash: hash}); err != nil {
		e.t.Fatal(err)
	}
	return e.env.draft(d.ID)
}

func (e *a06Env) dropCopy(d store.Draft) {
	e.t.Helper()
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], d.RemoteUID); err != nil {
		e.t.Fatal(err)
	}
}

// send spiegelt die gesendete Nachricht wie der Sync: text_plain roh,
// text_unquoted = StripQuotes (mailsync.fillClean). inReplyTo (ohne <>,
// wie im Spiegel) "" = ohne
// In-Reply-To und References (Gmail mit eigenem Betreff).
func (e *a06Env) send(subject, inReplyTo, plain string) int64 {
	e.t.Helper()
	box := e.m.boxes["sent"]
	e.m.uid[box]++
	at := e.now.Add(10 * time.Minute)
	nm := store.NewMessage{Link: store.LinkFlags{UID: e.m.uid[box], Seen: true}, MessageID: "gmail-a06-" + subject + "@example.org",
		Subject: subject, From: "demo@example.org", To: addrs(a06Anna), Cc: addrs(nil), DateHeader: at, InternalDate: at,
		Size: int64(300 + len(plain)), TextPlain: plain, TextUnquoted: sanitize.StripQuotes(plain), ContentHash: "a06" + subject + plain,
		CleanVersion: 4, Links: json.RawMessage("[]")}
	if inReplyTo != "" {
		nm.InReplyTo, nm.References = inReplyTo, []string{inReplyTo}
	}
	if _, err := e.st.ApplyBatch(context.Background(), store.MirrorBatch{AccountID: e.acc, MailboxID: box, New: []store.NewMessage{nm},
		State: &store.MailboxState{UIDValidity: 1, UIDNext: e.m.uid[box] + 1, Phase: store.PhaseReady}}); err != nil {
		e.t.Fatal(err)
	}
	n, err := e.st.QueryIntForTest(`SELECT max(id) FROM messages`)
	if err != nil {
		e.t.Fatal(err)
	}
	return int64(n)
}

// scores liefert die Ähnlichkeit mit dem Text wie rc3 (text_unquoted) und
// wie jetzt (sentText aus dem Klartext).
func a06Scores(d store.Draft, plain string) (rc3, now float64) {
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, d.BodyHTML), "")
	return fd.ScoreSent(NewSentWords(sanitize.StripQuotes(plain))), fd.ScoreSent(NewSentWords(sentText(store.Body{TextPlain: plain})))
}

// orphans zählt die Zeilen ohne „>“ direkt unter einer „>“-Zeile.
func a06Orphans(plain string) int {
	n, prevQuote := 0, false
	for _, l := range strings.Split(plain, "\n") {
		q := strings.HasPrefix(l, ">")
		if !q && prevQuote && strings.TrimSpace(l) != "" {
			n++
		}
		prevQuote = q || (prevQuote && strings.TrimSpace(l) != "")
	}
	return n
}

// (1) Fall A: eigener Betreff (Entwurf und gesendete Nachricht gleich),
// Gmail sendet ohne In-Reply-To → nur der Rückfall; Fortsetzungen auch
// als Kette über mehrere Zeilen.
func TestA06CaseAOwnSubjectFallback(t *testing.T) {
	e := newA06Env(t, a06Original)
	d, intro := e.draft("Beleg Oktober Gartenstraße", a06Original)
	plain := a06GmailSent(a06OwnText, intro, a06Original)
	if n := a06Orphans(plain); n < 3 {
		t.Fatalf("test text has %d orphaned continuation lines, want a chain:\n%s", n, plain)
	}
	rc3, now := a06Scores(d, plain)
	if rc3 >= FallbackMinJaccard || now < 1 {
		t.Fatalf("scores rc3 %.3f (want < 0.9), now %.3f (want 1.0)", rc3, now)
	}
	sent := e.send("Beleg Oktober Gartenstraße", "", plain)
	rep := e.check()
	if rep.SentByFallback != 1 || rep.SentByThread != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.env.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	if r, ok := e.lastAudit(OpSentDetected); !ok || r.Reason != DetectFallback {
		t.Fatalf("audit = %+v", r)
	}
}

// (2) Fall B: Betreff „Re: …“, In-Reply-To erhalten, Kopie aus den
// Entwürfen verschwunden. Rückfall und Verlauf sind möglich; der Rückfall
// kommt zuerst und schließt mit Grund fallback. Ist der Rückfall für diese
// Nachricht schon gelaufen (früherer Lauf, Marke darüber), schließt der
// Weg über den Verlauf (Grund thread); vor A06 scheiterten beide.
func TestA06CaseBReplySubject(t *testing.T) {
	for _, way := range []string{DetectFallback, DetectThread} {
		t.Run(way, func(t *testing.T) {
			e := newA06Env(t, a06Original)
			d, intro := e.draft("Re: Abrechnung Gartenstraße", a06Original)
			e.dropCopy(d)
			plain := a06GmailSent(a06OwnText, intro, a06Original)
			rc3, now := a06Scores(d, plain)
			if rc3 >= ThreadMinJaccard || now < 1 {
				t.Fatalf("scores rc3 %.3f (want < 0.5), now %.3f", rc3, now)
			}
			sent := e.send("Re: Abrechnung Gartenstraße", a06OrigID, plain)
			if way == DetectThread {
				// Wie nach einem früheren Lauf eines älteren Builds: Marke
				// über der Nachricht, Entwurf geprüft, Regelstand aktuell.
				ctx := context.Background()
				if err := e.st.SetSentCheckMark(ctx, e.acc, sent); err != nil {
					t.Fatal(err)
				}
				if err := e.st.SetFallbackChecked(ctx, d.ID, e.env.draft(d.ID).ContentChangedAt); err != nil {
					t.Fatal(err)
				}
				if err := e.st.SetSentCheckRev(ctx, e.acc, SentCheckRev); err != nil {
					t.Fatal(err)
				}
			}
			rep := e.check()
			if len(rep.SentDraftIDs) != 1 || (way == DetectFallback && rep.SentByFallback != 1) || (way == DetectThread && rep.SentByThread != 1) {
				t.Fatalf("report = %+v", rep)
			}
			if got := e.env.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
				t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
			}
			if r, ok := e.lastAudit(OpSentDetected); !ok || r.Reason != way {
				t.Fatalf("audit reason = %q, want %q", r.Reason, way)
			}
		})
	}
}

// (3) Wie (1) mit „ß“ und Umlauten an der Umbruchkante: An mindestens
// einer Stelle entscheiden Bytes anders als Zeichen.
func TestA06CaseAUmlautEdge(t *testing.T) {
	e := newA06Env(t, a06Umlaut)
	d, intro := e.draft("Bürgschaft Schüßler", a06Umlaut)
	plain := a06GmailSent(a06OwnText, intro, a06Umlaut)
	lines := strings.Split(plain, "\n")
	edge := false
	for i := 1; i < len(lines); i++ {
		prev, cur := lines[i-1], lines[i]
		if !strings.HasPrefix(prev, ">") || strings.HasPrefix(cur, ">") || strings.TrimSpace(cur) == "" {
			continue
		}
		w := strings.Fields(cur)[0]
		if utf8.RuneCountInString(prev)+1+utf8.RuneCountInString(w) <= sanitize.WrapMinWidth && len(prev)+1+len(w) > sanitize.WrapMinWidth {
			edge = true
		}
	}
	if !edge {
		t.Fatalf("no break where bytes and characters differ:\n%s", plain)
	}
	if _, now := a06Scores(d, plain); now < 1 {
		t.Fatalf("score = %.3f", now)
	}
	e.send("Bürgschaft Schüßler", "", plain)
	if rep := e.check(); rep.SentByFallback != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

// (4) Inline-Antwort: Eine kurze eigene Zeile direkt unter einer kurzen
// „>“-Zeile bleibt eigener Text; der Mensch hat den Entwurf geändert, er
// bleibt offen.
func TestA06InlineAnswerUnderShortQuote(t *testing.T) {
	const original = "Hallo Demo,\n\nzwei Fragen:\n\nAm 01.11.2026 schrieb Demo <demo@example.org>:\n> Kommt der Monteur?\n> Und die Rechnung?"
	e := newA06Env(t, original)
	d, intro := e.draft("Fragen Monteur", original)
	plain := a06GmailSent(a06OwnText, intro, original)
	plain = strings.Replace(plain, "> Kommt der Monteur?\n", "> Kommt der Monteur?\nJa, am Freitag gegen neun Uhr morgens mit zwei Kollegen.\n", 1)
	if got := sentText(store.Body{TextPlain: plain}); !strings.Contains(got, "Ja, am Freitag gegen neun Uhr") {
		t.Fatalf("inline answer lost:\n%s", got)
	}
	e.send("Fragen Monteur", "", plain)
	if rep := e.check(); !rep.Empty() && rep.SentByFallback != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.env.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("draft = %s", got.Status)
	}
}

// (5) Negativtest: Eine Inline-Antwort bzw. ein Nachsatz ohne Leerzeile
// direkt unter einer langen „>“-Zeile (über 80 Bytes, ungebrochenes
// Outlook- oder Apple-Zitat) ist keine Fortsetzung; sie bleibt eigener
// Text, und der geänderte Entwurf gilt nicht als gesendet.
func TestA06NoJoinUnderLongQuoteLine(t *testing.T) {
	long := "> Hallo Anna, anbei die Abrechnung für das Haus in der Gartenstraße mit allen Belegen bis Oktober."
	if len(long) <= sanitize.WrapMaxLine {
		t.Fatalf("quote line has %d bytes", len(long))
	}
	original := "Hallo Demo,\n\nsiehe unten.\n\nAm 01.11.2026 schrieb Demo <demo@example.org>:\n" + long
	e := newA06Env(t, original)
	d, intro := e.draft("Abrechnung Haus", original)
	// Gesendet: der Entwurf, darunter ohne Leerzeile das ungebrochene Zitat
	// (Outlook) und direkt darunter ein Nachsatz des Menschen.
	plain := a06OwnText + "\n\n" + intro + "\n\nHallo Demo,\n\nsiehe unten.\n\nAm 01.11.2026 schrieb Demo <demo@example.org>:\n" + long +
		"\nNein, der Beleg für Oktober fehlt noch, bitte schicken Sie ihn mir bis Montag zu.\n"
	got := sentText(store.Body{TextPlain: plain})
	if !strings.Contains(got, "bitte schicken Sie ihn mir bis Montag") {
		t.Fatalf("addition lost:\n%s", got)
	}
	e.send("Abrechnung Haus", "", plain)
	e.check()
	if got := e.env.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("draft = %s, want draft (changed before sending)", got.Status)
	}
}

// Spiegel, Suche und Snippet bleiben unverändert: text_unquoted der
// gesendeten Nachricht ist weiter StripQuotes des Klartexts, mit den
// verwaisten Zeilen.
func TestA06MirrorUnchanged(t *testing.T) {
	e := newA06Env(t, a06Original)
	_, intro := e.draft("Beleg Oktober Gartenstraße", a06Original)
	plain := a06GmailSent(a06OwnText, intro, a06Original)
	id := e.send("Beleg Oktober Gartenstraße", "", plain)
	e.check()
	b, err := e.st.MessageBody(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if b.TextUnquoted != sanitize.StripQuotes(plain) || b.TextPlain != plain {
		t.Fatal("mirror text changed by the check")
	}
}
