package drafts

import (
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T3, Punkt 15 (Befund D7a B1): Gmail im Browser setzt als
// Absender die Schreibweise mit Punkten, das Konto steht ohne Punkte in
// PostKauz. Eine Mail nur in „All Mail" von dieser Schreibweise ist eine
// eigene (Versanderkennung, Rückfall). Erfundene Adressen; die Domain
// steht getrennt (PII-Gate).
func TestT3NDetectGmailDotsAndPlusAsOwn(t *testing.T) {
	const gm = "@gmail.com"
	e := newEnv(t)
	m := newMirror(e)
	if err := e.st.ExecForTest(`UPDATE accounts SET email = ? WHERE id = ?`, "pktestowner"+gm, e.acc); err != nil {
		t.Fatal(err)
	}
	a := e.fallbackDraft("Re: Gmail-Schreibweise")
	b := e.fallbackDraft("Re: Gmail-Schreibweise")
	to := []store.Addr{{Address: "anna@example.org"}, {Address: "bob@example.org"}}
	at := e.now.Add(time.Minute)
	m.put("all", sentMsg{messageID: "punkte@example.org", subject: "Re: Gmail-Schreibweise", from: "Demo <PK.Test.Owner" + gm + ">", to: to, date: at, text: fbBody})
	rep := e.check()
	if rep.SentByFallback != 1 || rep.SentDraftIDs[0] != a.ID {
		t.Fatalf("dotted spelling not recognized as own: report = %+v", rep)
	}
	m.put("all", sentMsg{messageID: "plus@example.org", subject: "Re: Gmail-Schreibweise", from: "pk.test.owner+lists" + "@googlemail.com", to: to, date: at, text: fbBody})
	if rep := e.check(); rep.SentByFallback != 1 || rep.SentDraftIDs[0] != b.ID {
		t.Fatalf("+tag/googlemail spelling not recognized as own: report = %+v", rep)
	}
}

// Andere Anbieter unverändert: Punkte zählen dort.
func TestT3NDetectOtherProvidersKeepDots(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	e.fallbackDraft("Re: Andere Domain")
	m.put("all", sentMsg{messageID: "fremd@example.org", subject: "Re: Andere Domain", from: "De.mo@example.org",
		to: []store.Addr{{Address: "anna@example.org"}, {Address: "bob@example.org"}}, date: e.now.Add(time.Minute), text: fbBody})
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("De.mo@example.org counted as the account demo@example.org: %+v", rep)
	}
}
