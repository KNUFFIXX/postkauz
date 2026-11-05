package sanitize

import (
	"strings"
	"testing"
	"time"
)

// A06 Teil 3: Regel von JoinWrappedQuotes an den Kanten.
func TestJoinWrappedQuotes(t *testing.T) {
	// q(n) ist eine „>“-Zeile mit genau n Bytes.
	q := func(n int) string { return "> " + strings.Repeat("x", n-2) }
	cases := []struct {
		name, in, want string
	}{
		{"wrapped once", q(76) + "\nweiter so", q(76) + " weiter so"},
		{"chain over three lines", q(76) + "\n" + strings.Repeat("y", 74) + "\nund Ende\n\nEigener Text",
			q(76) + " " + strings.Repeat("y", 74) + " und Ende\n\nEigener Text"},
		{"sum exactly 72 is no wrap", q(68) + "\nabc", q(68) + "\nabc"},
		{"sum 73 is a wrap", q(68) + "\nabcd", q(68) + " abcd"},
		{"previous line 80 bytes", q(80) + "\nx", q(80) + " x"},
		{"previous line 81 bytes is never wrapped", q(81) + "\nNein, das stimmt nicht.", q(81) + "\nNein, das stimmt nicht."},
		{"blank line ends the quote", q(76) + "\n\nweiter", q(76) + "\n\nweiter"},
		{"short quote line, inline answer", "> Kommt er am Freitag?\nJa, passt.", "> Kommt er am Freitag?\nJa, passt."},
		{"next quote line stays", q(76) + "\n> > tiefer", q(76) + "\n> > tiefer"},
		{"no quote before", strings.Repeat("z", 76) + "\nweiter", strings.Repeat("z", 76) + "\nweiter"},
		{"continuation measured by its own length", q(76) + "\nkurz\nnoch", q(76) + " kurz\nnoch"},
		{"crlf", q(76) + "\r\nweiter\r\n", q(76) + " weiter\n"},
		{"indented quote mark", "  " + q(74) + "\nweiter", "  " + q(74) + " weiter"},
		{"indented continuation", q(76) + "\n   weiter", q(76) + " weiter"},
		// Bytes, nicht Zeichen: 66 Zeichen, 72 Bytes (sechs Umlaute); mit
		// „und“ 76 Bytes (aber nur 70 Zeichen) → umbrochen.
		{"bytes not characters", "> " + strings.Repeat("ü", 6) + strings.Repeat("a", 58) + "\nund weiter",
			"> " + strings.Repeat("ü", 6) + strings.Repeat("a", 58) + " und weiter"},
	}
	for _, c := range cases {
		if got := JoinWrappedQuotes(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	// Bekannte Grenze (Nachbesserung 1, hingenommen): Eine eigene
	// Inline-Zeile ohne Leerzeile direkt unter einer 73 bis 80 Byte
	// langen „>“-Zeile ist von einer Fortsetzung nicht zu unterscheiden
	// und wird angehängt (danach mit dem Zitat entfernt).
	for _, n := range []int{73, 76, 80} {
		in := q(n) + "\nJa, das passt so."
		if got := JoinWrappedQuotes(in); got != q(n)+" Ja, das passt so." {
			t.Errorf("inline line under a %d byte quote line: %q", n, got)
		}
		if got := StripQuotes(JoinWrappedQuotes("Danke.\n\n" + in)); got != "Danke." {
			t.Errorf("inline line under a %d byte quote line, stripped: %q", n, got)
		}
	}
	// Die Grenze gilt für jede „>“-Zeile bis 80 Bytes, nicht nur für lange
	// (Nachbesserung 2): 60 Bytes + 1 + „Entschuldigung,“ (15) = 76 > 72.
	in := q(60) + "\nEntschuldigung, das habe ich übersehen."
	if got := JoinWrappedQuotes(in); got != q(60)+" Entschuldigung, das habe ich übersehen." {
		t.Errorf("own line under a 60 byte quote line: %q", got)
	}
	// Mit kurzem erstem Wort bleibt sie stehen: 60 + 1 + 2 = 63.
	if got := JoinWrappedQuotes(q(60) + "\nJa, das habe ich übersehen."); got != q(60)+"\nJa, das habe ich übersehen." {
		t.Errorf("short first word: %q", got)
	}
	// Zeichen statt Bytes hätten den letzten Fall nicht verbunden.
	if n := len([]rune("> "+strings.Repeat("ü", 6)+strings.Repeat("a", 58))) + 1 + 3; n > WrapMinWidth {
		t.Fatalf("test case does not separate bytes from characters: %d", n)
	}
}

// Nach dem Verbinden entfernt StripQuotes die Fortsetzungen mit ihrer
// „>“-Zeile; eine Inline-Antwort unter einer kurzen Zitatzeile bleibt
// eigener Text.
func TestJoinWrappedQuotesThenStrip(t *testing.T) {
	wrapped := "Danke, passt.\n\nAm 01.11.2026 schrieb Anna <anna@example.org>:\n" +
		"> Bitte schauen Sie sich die Abrechnung für das Haus in der Gartenstraße\nan und melden Sie sich bis Freitag.\n"
	if got := StripQuotes(wrapped); !strings.Contains(got, "melden Sie sich") {
		t.Fatalf("precondition: StripQuotes alone keeps the orphan: %q", got)
	}
	if got := StripQuotes(JoinWrappedQuotes(wrapped)); got != "Danke, passt." {
		t.Fatalf("joined and stripped = %q", got)
	}
	inline := "Am 01.11.2026 schrieb Anna <anna@example.org>:\n> Kommt der Monteur?\nJa, am Freitag.\n> Und die Rechnung?\nKommt mit der Post."
	if got := StripQuotes(JoinWrappedQuotes(inline)); got != "Ja, am Freitag.\nKommt mit der Post." {
		t.Fatalf("inline answers = %q", got)
	}
}

// Nachbesserung 2: linear auch bei einer sehr langen Kette (etwa 10 MiB
// Fortsetzungen unter einer „>“-Zeile); vorher quadratisch.
func TestJoinWrappedQuotesLongChainIsLinear(t *testing.T) {
	line := strings.Repeat("x", 74) // 74 + 1 + 74 > 72, höchstens 80
	n := 10 << 20 / (len(line) + 1)
	var b strings.Builder
	b.WriteString("> " + strings.Repeat("q", 74) + "\n")
	for i := 0; i < n; i++ {
		b.WriteString(line + "\n")
	}
	in := b.String()
	start := time.Now()
	got := JoinWrappedQuotes(in)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("10 MiB chain took %v", d)
	}
	if strings.Count(got, "\n") != 1 || len(got) != len(in) {
		t.Fatalf("chain not joined into one line: %d newlines, %d of %d bytes", strings.Count(got, "\n"), len(got), len(in))
	}
}
