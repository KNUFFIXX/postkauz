package compose

import (
	"bytes"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	ghtml "github.com/yuin/goldmark/renderer/html"

	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

// markdown ist der Umsetzer nach E3: CommonMark ohne Erweiterungen, einzige
// Option „Zeilenumbruch = <br>". Rohes HTML im Markdown fällt weg
// (goldmark-Standard ohne WithUnsafe).
var markdown = goldmark.New(goldmark.WithRendererOptions(ghtml.WithHardWraps()))

// quoteDateLayout ist das Datum in Einleitungszeile und Weiterleitungskopf.
const quoteDateLayout = "Mon, 2 Jan 2006 15:04 -0700"

func (b *builder) checkBody() error {
	in := b.in
	if in.BodyMarkdown != "" && in.BodyHTML != "" {
		return deny(ReasonInvalidInput, "body", "body_markdown and body_html are mutually exclusive")
	}
	for _, f := range []struct{ name, v string }{{"body_markdown", in.BodyMarkdown}, {"body_html", in.BodyHTML}} {
		if n := utf8.RuneCountInString(f.v); n > b.lim.MaxBodyChars {
			return deny(ReasonTooLarge, f.name, "%d characters, at most %d", n, b.lim.MaxBodyChars)
		}
	}
	return nil
}

// clean bereinigt ein HTML-Fragment mit der Allowlist und lehnt
// versteckten Text ab (geprüft am Fragment vor der Bereinigung, denn die
// Allowlist entfernt <style>-Regeln und Klassen, die Text verstecken).
func (b *builder) clean(field, src string) (string, error) {
	if sanitize.HTML(src).Hidden {
		return "", deny(ReasonHiddenText, field, "html contains text a human would not see")
	}
	r := sanitize.CleanHTML(src)
	if r.Removed() {
		b.note(NoteContentRemoved)
	}
	return r.HTML, nil
}

// textToHTML macht aus Klartext ein HTML-Fragment (Zeilen mit <br>).
func textToHTML(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = html.EscapeString(sanitize.Text(l))
	}
	return strings.Join(lines, "<br>\n")
}

// originalText ist der bereinigte Text der Ausgangsnachricht: aus ihrem
// HTML ohne versteckten Text, sonst ihr Klartext.
func originalText(o *Original) string {
	if strings.TrimSpace(o.HTML) != "" {
		if t := sanitize.HTML(o.HTML).Text; strings.TrimSpace(t) != "" {
			return t
		}
	}
	return sanitize.Text(o.Text)
}

// quotedText ist originalText, gekürzt auf MaxQuoteChars (an einem
// Zeilenende, wenn eines in der zweiten Hälfte liegt) mit Markierung und
// Hinweis NoteQuoteTruncated (K3).
func (b *builder) quotedText(o *Original) string {
	t := originalText(o)
	r := []rune(t)
	if len(r) <= MaxQuoteChars {
		return t
	}
	cut := string(r[:MaxQuoteChars])
	if i := strings.LastIndex(cut, "\n"); i > len(cut)/2 {
		cut = cut[:i]
	}
	b.note(NoteQuoteTruncated)
	return cut + "\n\n" + QuoteTruncatedMarker
}

func formatAddr(a Address) string {
	if a.Name == "" {
		return a.Address
	}
	return a.Name + " <" + a.Address + ">"
}

func formatAddrs(l []Address) string {
	s := make([]string, len(l))
	for i, a := range l {
		s[i] = formatAddr(a)
	}
	return strings.Join(s, ", ")
}

func (b *builder) quoteDate(t time.Time) string {
	if t.IsZero() {
		return "an unknown date"
	}
	return t.In(b.msg.Date.Location()).Format(quoteDateLayout)
}

// quoteLine füllt die Vorlage der Einleitungszeile.
func (b *builder) quoteLine(o *Original) string {
	tpl := b.in.QuoteLine
	if strings.TrimSpace(tpl) == "" {
		tpl = DefaultQuoteLine
	}
	from := "unknown sender"
	if len(o.From) > 0 {
		from = formatAddrs(o.From)
	}
	r := strings.NewReplacer("{date}", b.quoteDate(o.Date), "{from}", headerText(from))
	return headerText(r.Replace(tpl))
}

func (b *builder) body() error {
	in := b.in
	var own string
	switch {
	case in.BodyHTML != "":
		h, err := b.clean("body_html", in.BodyHTML)
		if err != nil {
			return err
		}
		own = h
	case in.BodyMarkdown != "":
		var buf bytes.Buffer
		if err := markdown.Convert([]byte(in.BodyMarkdown), &buf); err != nil {
			return deny(ReasonInvalidInput, "body_markdown", "markdown conversion failed")
		}
		h, err := b.clean("body_markdown", buf.String())
		if err != nil {
			return err
		}
		own = h
	}

	var doc strings.Builder
	doc.WriteString("<!DOCTYPE html>\n<html><head><meta http-equiv=\"Content-Type\" content=\"text/html; charset=utf-8\"></head><body>\n")
	if strings.TrimSpace(own) != "" {
		doc.WriteString("<div>" + strings.TrimSpace(own) + "</div>\n")
	}
	if in.Signature {
		sig := ""
		switch {
		case strings.TrimSpace(in.Identity.SignatureHTML) != "":
			h, err := b.clean("signature_html", in.Identity.SignatureHTML)
			if err != nil {
				return err
			}
			sig = strings.TrimSpace(h)
		case strings.TrimSpace(in.Identity.SignatureText) != "":
			sig = textToHTML(strings.TrimSpace(in.Identity.SignatureText))
		}
		if sig != "" {
			doc.WriteString("<div><br></div>\n<div>" + sig + "</div>\n")
		}
	}
	switch o := in.Original; {
	case (in.Kind == KindReply || in.Kind == KindReplyAll) && in.Quote:
		doc.WriteString("<div><br></div>\n<div>" + html.EscapeString(b.quoteLine(o)) + "</div>\n")
		if t := b.quotedText(o); strings.TrimSpace(t) != "" {
			doc.WriteString("<blockquote>" + textToHTML(t) + "</blockquote>\n")
		}
	case in.Kind == KindForward:
		lines := []string{"---------- Forwarded message ----------"}
		add := func(k, v string) {
			if v = headerText(v); v != "" {
				lines = append(lines, k+": "+v)
			}
		}
		add("From", formatAddrs(o.From))
		add("Date", b.quoteDate(o.Date))
		add("Subject", o.Subject)
		add("To", formatAddrs(o.To))
		add("Cc", formatAddrs(o.Cc))
		doc.WriteString("<div><br></div>\n<div>" + textToHTML(strings.Join(lines, "\n")) + "</div>\n")
		if t := b.quotedText(o); strings.TrimSpace(t) != "" {
			doc.WriteString("<div><br></div>\n<div>" + textToHTML(t) + "</div>\n")
		}
	}
	doc.WriteString("</body></html>\n")

	final := doc.String()
	res := sanitize.HTML(final)
	if res.Hidden {
		// Zweite Sicherung: Das fertige Dokument darf nichts verstecken.
		return deny(ReasonHiddenText, "html", "html contains text a human would not see")
	}
	b.msg.BodyHTML = final
	b.msg.BodyText = res.Text
	return nil
}
