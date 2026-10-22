package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
)

// Gegenprüfung S2.4/S2.5, K4: Die gebaute Nachricht (bis auf Date) hängt
// nicht von der Zeitzone des Prozesses ab. Sonst bauten CLI und
// Sync-Dienst mit verschiedenen Zeitzonen verschiedene Zitatzeilen, und
// approve bzw. der Push meldeten approval_stale.
func TestReviewK4MessageHashIndependentOfTimeZone(t *testing.T) {
	for _, kind := range []Kind{KindReply, KindForward} {
		var first string
		for _, loc := range []*time.Location{time.UTC, time.FixedZone("", 2*3600), time.FixedZone("", -7*3600), time.FixedZone("", 9*3600+1800)} {
			in := baseInput(kind)
			in.Now = clock(fixedNow.In(loc))
			in.QuoteLine = "{date} | {date_numeric} | {from}"
			m := mustBuild(t, in)
			h := MessageHash(m.Raw)
			if first == "" {
				first = h
				continue
			}
			if h != first {
				t.Fatalf("%s: message hash depends on the time zone %v:\n%s", kind, loc, m.BodyText)
			}
		}
	}
}

// OriginalFrom liefert das Datum in der Zone der Date-Kopfzeile (gleicher
// Zeitpunkt wie mimeparse); ohne Kopfzeile UTC.
func TestReviewK4OriginalDateKeepsHeaderZone(t *testing.T) {
	raw := "Message-ID: <o.1@example.org>\r\nFrom: Erika <erika@example.org>\r\nTo: b@example.org\r\nSubject: Hallo\r\n" +
		"Date: Wed, 21 Oct 2026 09:15:00 -0700\r\n\r\nText\r\n"
	pm, err := mimeparse.Parse(strings.NewReader(raw), mimeparse.Options{})
	if err != nil {
		t.Fatal(err)
	}
	og := OriginalFrom(pm)
	if !og.Date.Equal(pm.Date) || og.Date.Format("-0700 15:04") != "-0700 09:15" {
		t.Fatalf("date = %v (parsed %v)", og.Date, pm.Date)
	}
	in := baseInput(KindReply)
	in.Original.Date = og.Date
	m := mustBuild(t, in)
	if !strings.Contains(m.BodyText, "On Wed, 21 Oct 2026 09:15 -0700, Erika Beispiel") {
		t.Fatalf("quote line:\n%s", m.BodyText)
	}
	// Ohne Date-Kopfzeile: INTERNALDATE in UTC.
	raw = strings.Replace(raw, "Date: Wed, 21 Oct 2026 09:15:00 -0700\r\n", "", 1)
	pm, err = mimeparse.Parse(strings.NewReader(raw), mimeparse.Options{InternalDate: time.Date(2026, 10, 21, 16, 15, 0, 0, time.FixedZone("", 3600))})
	if err != nil {
		t.Fatal(err)
	}
	if og := OriginalFrom(pm); og.Date.Location() != time.UTC || og.Date.Hour() != 15 {
		t.Fatalf("internal date = %v", og.Date)
	}
}
