package compose

import (
	"strings"
	"testing"
	"time"
)

// Zusatz zu S2.4 (Betreiber, 22.10.2026): {date_numeric} für eine deutsche
// Zitatzeile („Am 21.10.2026 09:15 schrieb …:"), Zeitzone wie {date}. Der
// Standard bleibt englisch (Golden-Test unverändert, TestGoldenReplyAll).
func TestQuoteLineDateNumeric(t *testing.T) {
	in := baseInput(KindReply)
	in.QuoteLine = "Am {date_numeric} schrieb {from}:"
	m := mustBuild(t, in)
	if !strings.Contains(m.BodyText, "Am 21.10.2026 09:15 schrieb Erika Beispiel <erika@example.org>:") {
		t.Fatalf("quote line:\n%s", m.BodyText)
	}
	// Beide Platzhalter in derselben Zeitzone (der Uhr des Baus), hier UTC.
	in = baseInput(KindReply)
	in.Now = clock(fixedNow.UTC())
	in.QuoteLine = "{date} = {date_numeric} {from}"
	m = mustBuild(t, in)
	if !strings.Contains(m.BodyText, "Wed, 21 Oct 2026 07:15 +0000 = 21.10.2026 07:15 Erika Beispiel") {
		t.Fatalf("time zone:\n%s", m.BodyText)
	}
	// Ohne Datum der Ausgangsnachricht.
	in = baseInput(KindReply)
	in.Original.Date = time.Time{}
	in.QuoteLine = "Am {date_numeric} schrieb {from}:"
	m = mustBuild(t, in)
	if !strings.Contains(m.BodyText, "Am ??.??.???? ??:?? schrieb Erika Beispiel") {
		t.Fatalf("unknown date:\n%s", m.BodyText)
	}
	// Standard unverändert.
	m = mustBuild(t, baseInput(KindReply))
	if !strings.Contains(m.BodyText, "On Wed, 21 Oct 2026 09:15 +0200, Erika Beispiel <erika@example.org> wrote:") {
		t.Fatalf("default quote line:\n%s", m.BodyText)
	}
}
