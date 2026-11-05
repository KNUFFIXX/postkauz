package compose

import (
	"html"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

// rc3QuoteHTML ist der Zitatblock von 0.3.0-rc3 (body.go bis A05):
// Einleitung als <div>, Zitat als ein <blockquote> mit wörtlichem „>“ für
// innere Ebenen.
func rc3QuoteHTML(line, text string) string {
	s := "<div>" + html.EscapeString(line) + "</div>\n"
	if strings.TrimSpace(text) != "" {
		s += "<blockquote>" + textToHTML(text) + "</blockquote>\n"
	}
	return s
}

// A06 Teil 2, Pflicht: Der Klartext eines Antwort-Entwurfs (body_text,
// daraus QuoteOf und preview_hash) bleibt Byte für Byte wie in rc3, für 0,
// 1, 2 und 3 innere Ebenen, „>foo“ ohne Leerzeichen, „>>“, Leerzeilen im
// Zitat, leere Zitatzeilen („>“, „> >“, „> “), Ebenenwechsel nach oben
// und unten, Zitat am Anfang und am Ende.
func TestA06QuoteHTMLKeepsBodyText(t *testing.T) {
	originals := map[string]string{
		"level0":        "Guten Tag,\n\nder Monteur kommt am Freitag.\n\nGruß\nAnna",
		"level1":        "Passt.\n\nAm 01.11.2026 schrieb Bert:\n> Kommt der Monteur?\n> Bitte melden.\n\nGruß",
		"level2":        "Ja.\n\n> Frage zwei\n> > Frage eins, eine lange Zeile mit sehr vielen Wörtern, die über achtzig Zeichen hinausgeht und weiter.\n> > noch eins\n> zurück\nEnde",
		"level3":        "A\n> B\n> > C\n> > > D\n> > > E\n> > F\n> G\nH",
		"nospace":       "Text\n>foo\n>>bar\n> >baz\n>> > qux\n> >>quux\nEnde",
		"emptyquoted":   "Text\n> erste\n>\n> zweite\n> >\n> > dritte\n> \n> > \nEnde",
		"blanklines":    "Text\n\n> eins\n\n> zwei\n\n\n> > drei\n\nEnde",
		"down":          "> > > tief\n> > mittel\n> flach\noben",
		"startsquoted":  "> Anfang zitiert\n> > tiefer\nText",
		"endsquoted":    "Text\n> > Ende tief",
		"endsemptyq":    "Text\n> > tief\n> >",
		"endsemptyq1":   "Text\n> eins\n>",
		"endsspaces":    "Text\n> eins\n>    ",
		"spaces":        "Text\n>  zwei Leerzeichen\n>\tTab\n> >   drei\n  eingerückt\n\t> tab davor",
		"entities":      "Text\n> a < b & c > d\n> > \"q\" 'x'",
		"onlyquote":     "> nur Zitat",
		"onlyemptyq":    ">\n> >",
		"truncatedmark": "Text\n> zitiert\n" + QuoteTruncatedMarker,
		"gtwords":       "> > > >\n>>>>\n> > > > x",
	}
	for name, txt := range originals {
		for _, kind := range []Kind{KindReply, KindReplyAll} {
			in := baseInput(kind)
			in.Original.HTML = ""
			in.Original.Text = txt
			in.BodyMarkdown = "Danke, passt."
			m := mustBuild(t, in)
			b := &builder{in: in}
			line, quoted := b.quoteLine(in.Original), b.quotedText(in.Original)
			newBlock := quoteHTML(line, quoted)
			if !strings.Contains(m.BodyHTML, newBlock) {
				t.Fatalf("%s: quote block not in the HTML:\n%s", name, m.BodyHTML)
			}
			old := strings.Replace(m.BodyHTML, newBlock, rc3QuoteHTML(line, quoted), 1)
			want := sanitize.HTML(old).Text
			if m.BodyText != want {
				t.Fatalf("%s (%s): body_text differs from rc3\nrc3: %q\nnew: %q\nhtml: %s", name, kind, want, m.BodyText, m.BodyHTML)
			}
			if sanitize.HTML(m.BodyHTML).Hidden {
				t.Fatalf("%s: quote block counts as hidden text", name)
			}
		}
	}
}

// Die Form ist Gmails eigene: gmail_quote, gmail_attr mit der
// Einleitungszeile, blockquote mit type="cite" und dem Stil; innere Ebenen
// verschachtelt, nicht als wörtliches „&gt;“.
func TestA06QuoteHTMLForm(t *testing.T) {
	in := baseInput(KindReply)
	in.Original.HTML = ""
	in.Original.Text = "Antwort\n> Frage\n> > Vorfrage\n>foo"
	m := mustBuild(t, in)
	for _, want := range []string{
		`<div class="gmail_quote"><div dir="ltr" class="gmail_attr">On `,
		` wrote:<br></div><blockquote class="gmail_quote" type="cite" style="margin:0px 0px 0px 0.8ex;border-left:1px solid rgb(204,204,204);padding-left:1ex">Antwort<br>`,
		`<blockquote class="gmail_quote" type="cite" style="margin:0px 0px 0px 0.8ex;border-left:1px solid rgb(204,204,204);padding-left:1ex">Frage<br>`,
		`<blockquote class="gmail_quote" type="cite" style="margin:0px 0px 0px 0.8ex;border-left:1px solid rgb(204,204,204);padding-left:1ex">Vorfrage<br>`,
		`</blockquote></blockquote>&gt;foo</blockquote></div>`,
	} {
		if !strings.Contains(m.BodyHTML, want) {
			t.Fatalf("html lacks %q:\n%s", want, m.BodyHTML)
		}
	}
	if strings.Contains(m.BodyHTML, "&gt; Frage") || strings.Contains(m.BodyHTML, "<blockquote>") {
		t.Fatalf("literal > or plain blockquote left:\n%s", m.BodyHTML)
	}
	// Ohne Zitattext: nur die Einleitung im gmail_quote.
	in.Original.Text = "   "
	if m := mustBuild(t, in); strings.Contains(m.BodyHTML, "<blockquote") || !strings.Contains(m.BodyHTML, `class="gmail_attr"`) {
		t.Fatalf("empty quote:\n%s", m.BodyHTML)
	}
	// Weiterleitung bleibt wie bisher (kein gmail_quote).
	if m := mustBuild(t, baseInput(KindForward)); strings.Contains(m.BodyHTML, "gmail_quote") {
		t.Fatalf("forward:\n%s", m.BodyHTML)
	}
}
