package sanitize

import (
	"strings"
	"testing"
)

// Gegenprüfung S2.3, W3: Mit diesen Angaben ließ sich Text verstecken, ohne
// dass die Erkennung aus S1.6 ihn als versteckt meldete; CleanHTML ließ sie
// durch. Jetzt fällt jede weg (Positivliste), der Text bleibt sichtbar.
func TestReviewW3HiddenTextProbes(t *testing.T) {
	const word = "Anweisung"
	styles := []string{
		"font-size:3px", "font-size: 3px", "font-size:calc(0px)", "font-size:0.5em", "font-size:50%", "font-size:xx-small",
		"font-size:smaller", "color:rgba(0,0,0,0.02)", "color:rgba(0, 0, 0, .02)", "color:#00000005", "color:#fff", "color:#eeeeee",
		"color:hsla(0,0%,0%,0.01)", "color:transparent", "color:yellow", "filter:opacity(0)", "-webkit-text-fill-color:transparent",
		"height:1px;width:1px;overflow:hidden", "max-height:0;overflow:hidden", "opacity:0.05", "display:none", "visibility:hidden",
		"position:absolute;left:-9999px", "text-indent:-9999px", "transform:scale(0)", "clip-path:inset(50%)",
		"line-height:0", "letter-spacing:-10px", "margin-left:-9999px", "margin:0 0 0 5000px", "padding-left:9999px",
		"background-color:#000", "background:#000", "font-size:var(--klein)", "color:var(--x)", "mix-blend-mode:screen",
	}
	for _, st := range styles {
		src := `<p>sichtbar</p><span style="` + st + `">` + word + `</span>`
		r := CleanHTML(src)
		if !strings.Contains(r.HTML, word) || HTML(r.HTML).Hidden {
			t.Errorf("%s: text missing or hidden: %s", st, r.HTML)
		}
		if strings.Contains(r.HTML, "style=") {
			t.Errorf("%s: style kept: %s", st, r.HTML)
		}
		if !r.Removed() {
			t.Errorf("%s: removal not reported", st)
		}
	}
	for name, src := range map[string]string{
		"title":           `<a href="https://example.org/" title="` + word + `">Link</a><span title="` + word + `">x</span>`,
		"font color":      `<font color="#fefefe">` + word + `</font>`,
		"bgcolor":         `<table bgcolor="#000000"><tr><td><font color="#eeeeee">` + word + `</font></td></tr></table>`,
		"nested small":    `<small><small><small><small>` + word + `</small></small></small></small>`,
		"nested sub, sup": `<sub><sup><sub>` + word + `</sub></sup></sub>`,
	} {
		r := CleanHTML(src)
		switch {
		case strings.Contains(r.HTML, "title="), strings.Contains(r.HTML, "bgcolor"), strings.Contains(r.HTML, `color="#`):
			t.Errorf("%s: attribute kept: %s", name, r.HTML)
		case strings.Count(r.HTML, "<small>")+strings.Count(r.HTML, "<sub>")+strings.Count(r.HTML, "<sup>") > 1:
			t.Errorf("%s: shrinking nested: %s", name, r.HTML)
		}
	}
	// alt: gekürzt und sichtbar hinter dem Bild.
	r := CleanHTML(`<img src="cid:logo@example.com" alt="` + word + strings.Repeat(" lang", 50) + `">`)
	if strings.Contains(r.HTML, "alt=") || !strings.Contains(r.HTML, "> ["+word) || !strings.Contains(r.HTML, "…]") ||
		!strings.Contains(HTML(r.HTML).Text, word) {
		t.Errorf("alt: %s", r.HTML)
	}
	// Erlaubtes bleibt wörtlich.
	for _, st := range []string{"color:#333", "color: rgb(0, 0, 128)", "color:orange", "font-weight: bold", "font-style:italic",
		"font-size:12px", "font-size: 1.2em", "font-size:small", "font-family: Arial, 'Helvetica Neue', sans-serif",
		"text-decoration: underline line-through", "text-align:center", "margin: 0 0 1em", "padding:4px 8px", "margin-left:auto",
		"vertical-align: middle"} {
		if v, ok := cleanStyle(st); !ok || v != st {
			t.Errorf("harmless %q = %q, %v", st, v, ok)
		}
	}
	if v, ok := cleanStyle("color:#333; font-size:2px; font-weight:bold !important"); !ok || v != "color:#333; font-weight: bold" {
		t.Errorf("mixed = %q, %v", v, ok)
	}
}
