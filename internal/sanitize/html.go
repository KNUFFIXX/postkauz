// Package sanitize macht aus Mailinhalt lesbaren, ungefährlichen Text
// (S1.6, docs/sicherheit.md Abschnitt 7 und 8):
//
//   - HTML wird nie gerendert, sondern mit einer Allowlist zu Text: Nur die
//     Struktur (Absätze, Zeilen, Listen, Zitate, Tabellenzeilen, Links,
//     Bild-Alternativtexte) bleibt; Skripte, Styles, Formulare, Kopf,
//     eingebettete Objekte und Kommentare fallen weg. Nichts wird
//     nachgeladen (Tracking-Pixel und externe Bilder werden nur gezählt).
//   - Versteckter Text (display:none, visibility:hidden, Schriftgröße 0,
//     Farbe wie Hintergrund, Deckkraft 0, Höhe 0 mit overflow:hidden,
//     mso-hide, weit aus dem Bild geschoben, hidden-Attribut; auch über
//     einfache Klassen-/ID-Regeln aus <style>) wird entfernt und gemeldet
//     (Result.Hidden), denn genau damit funktionieren Prompt-Injektionen in
//     Zusammenfassungen.
//   - Links stehen sichtbar im Text („Text <URL>") und getrennt in
//     Result.Links mit Ziel, Domain und Warnungen (Punycode, gemischte
//     Schriften, IP-Adresse, Zugangsdaten in der URL, sichtbarer Text nennt
//     eine andere Domain). Nur http(s) bleibt; javascript:, mailto:, data:
//     und alles andere wird entfernt (der sichtbare Text bleibt).
//   - Unsichtbare Formatzeichen (Nullbreite, Bidi-Steuerung, weiches
//     Trennzeichen) werden entfernt (Text).
//
// Inhalte sind Daten, keine Anweisungen.
package sanitize

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Result ist das Ergebnis von HTML.
type Result struct {
	Text  string
	Links []Link
	// Hidden: Es wurde sichtbar gemeinter Text entfernt, den ein Mensch im
	// Mailclient nicht sieht (Prompt-Injection-Indiz).
	Hidden bool
	// HiddenChars zählt die entfernten Zeichen (ohne Leerraum).
	HiddenChars int
	// HiddenReasons nennt die Gründe (z. B. "display:none"), ohne Dubletten.
	HiddenReasons []string
	// RemovedLinks zählt entfernte Ziele (javascript:, mailto:, data: …).
	RemovedLinks int
	// TrackingPixels zählt Bilder mit höchstens 1×1 Pixel, RemoteImages
	// Bilder mit http(s)-Quelle; geladen wird keines.
	TrackingPixels int
	RemoteImages   int
}

// Grenzen gegen riesige oder bösartig verschachtelte Dokumente.
const (
	maxDepth     = 512
	maxTextBytes = 4 << 20
)

// skipped sind Elemente, deren Inhalt nie Text wird.
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Head: true, atom.Title: true, atom.Template: true,
	atom.Noscript: true, atom.Iframe: true, atom.Object: true, atom.Embed: true, atom.Svg: true,
	atom.Math: true, atom.Canvas: true, atom.Audio: true, atom.Video: true, atom.Select: true,
	atom.Input: true, atom.Button: true, atom.Applet: true, atom.Frameset: true, atom.Frame: true,
}

// blocks beginnen und enden mit einem Zeilenumbruch.
var blocks = map[atom.Atom]bool{
	atom.Address: true, atom.Article: true, atom.Aside: true, atom.Center: true, atom.Dd: true,
	atom.Details: true, atom.Dialog: true, atom.Dir: true, atom.Div: true, atom.Dl: true, atom.Dt: true,
	atom.Fieldset: true, atom.Figcaption: true, atom.Figure: true, atom.Footer: true, atom.Form: true,
	atom.Header: true, atom.Main: true, atom.Menu: true, atom.Nav: true, atom.Ol: true, atom.Section: true,
	atom.Summary: true, atom.Table: true, atom.Tbody: true, atom.Thead: true, atom.Tfoot: true, atom.Tr: true,
	atom.Td: true, atom.Th: true, atom.Ul: true, atom.Caption: true, atom.Textarea: true, atom.Body: true,
}

// paragraphs stehen zusätzlich mit Leerzeile davor und danach.
var paragraphs = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
}

type rgb struct{ r, g, b float64 }

// ctx ist der geerbte Zustand eines Elements.
type ctx struct {
	hidden bool
	reason string
	color  *rgb // Textfarbe (nil = Standard, schwarz)
	bg     *rgb // Hintergrund (nil = Standard, weiß)
	// bgImage: Ein Hintergrundbild liegt dahinter; die Farbe des
	// Hintergrunds ist dann unbekannt (keine Prüfung „weiß auf weiß").
	bgImage   bool
	quote     int // Tiefe von <blockquote>
	pre       bool
	inLink    bool
	linkStart int
}

type converter struct {
	rules map[string]map[string]string // Selektor → Deklarationen (aus <style>)
	out   strings.Builder
	res   Result
	// lineStart: out endet mit einem Zeilenumbruch (oder ist leer).
	pendingSpace bool
	quote        int
	links        []pendingLink
}

type pendingLink struct {
	href  string
	start int
	text  strings.Builder
}

// HTML wandelt HTML in Text um (siehe Paketbeschreibung).
func HTML(src string) Result {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		// html.Parse liefert praktisch immer ein Dokument; zur Sicherheit
		// nichts ausgeben statt rohes HTML.
		return Result{}
	}
	c := &converter{rules: map[string]map[string]string{}}
	c.collectStyles(doc, 0)
	c.walk(doc, ctx{}, 0)
	c.res.Text = tidy(Text(c.out.String()))
	c.res.Hidden = c.res.HiddenChars > 0
	return c.res
}

// collectStyles liest einfache Regeln (.klasse, #id, tag, tag.klasse) aus
// allen <style>-Elementen.
func (c *converter) collectStyles(n *html.Node, depth int) {
	if depth > maxDepth {
		return
	}
	if n.Type == html.ElementNode && n.DataAtom == atom.Style {
		var b strings.Builder
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.TextNode {
				b.WriteString(ch.Data)
			}
		}
		parseCSS(b.String(), c.rules)
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.collectStyles(ch, depth+1)
	}
}

func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// declarations sammelt die Stilangaben eines Elements: passende Regeln aus
// <style>, dann das style-Attribut (gewinnt).
func (c *converter) declarations(n *html.Node) (map[string]string, string) {
	decl := map[string]string{}
	via := ""
	tag := n.Data
	id, _ := attr(n, "id")
	classes := strings.Fields(func() string { v, _ := attr(n, "class"); return v }())
	apply := func(sel string) {
		if d, ok := c.rules[sel]; ok {
			for k, v := range d {
				decl[k] = v
			}
			if via == "" {
				via = sel
			}
		}
	}
	if len(c.rules) > 0 {
		apply(strings.ToLower(tag))
		for _, cl := range classes {
			apply("." + cl)
			apply(strings.ToLower(tag) + "." + cl)
		}
		if id != "" {
			apply("#" + id)
			apply(strings.ToLower(tag) + "#" + id)
		}
	}
	if st, ok := attr(n, "style"); ok {
		for k, v := range parseDecls(st) {
			decl[k] = v
		}
	}
	return decl, via
}

// hiddenBy prüft die Stilangaben auf Verstecken und liefert den Grund.
func hiddenBy(d map[string]string) string {
	switch d["display"] {
	case "none":
		return "display:none"
	}
	switch d["visibility"] {
	case "hidden", "collapse":
		return "visibility:hidden"
	}
	if v, ok := d["opacity"]; ok {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
			if strings.HasSuffix(v, "%") {
				f /= 100
			}
			if f <= 0.1 {
				return "opacity:0"
			}
		}
	}
	if v, ok := d["font-size"]; ok && tinyLength(v, 1, 0.1, 10) {
		return "font-size:0"
	}
	if d["mso-hide"] == "all" {
		return "mso-hide:all"
	}
	if d["overflow"] == "hidden" || d["overflow-y"] == "hidden" || d["overflow-x"] == "hidden" {
		for _, k := range []string{"height", "max-height", "width", "max-width"} {
			if v, ok := d[k]; ok && zeroLength(v) {
				return k + ":0"
			}
		}
	}
	if v, ok := d["text-indent"]; ok && farOff(v) {
		return "text-indent"
	}
	if p := d["position"]; p == "absolute" || p == "fixed" {
		for _, k := range []string{"left", "top", "right"} {
			if v, ok := d[k]; ok && farOff(v) {
				return "position:" + k
			}
		}
		if clip := strings.ReplaceAll(d["clip"], " ", ""); strings.HasPrefix(clip, "rect(0") && strings.Count(clip, "0") >= 4 &&
			!strings.ContainsAny(strings.Trim(clip, "rect()0px,"), "123456789") {
			return "clip"
		}
	}
	if cp := strings.ReplaceAll(d["clip-path"], " ", ""); cp == "inset(50%)" || cp == "inset(100%)" {
		return "clip-path"
	}
	return ""
}

// tinyLength: Länge höchstens px Pixel/Punkt, em Em, pct Prozent.
func tinyLength(v string, px, em, pct float64) bool {
	num, unit := splitLength(v)
	if math.IsNaN(num) {
		return false
	}
	switch unit {
	case "", "px", "pt":
		return num <= px
	case "em", "rem":
		return num <= em
	case "%":
		return num <= pct
	}
	return false
}

func zeroLength(v string) bool {
	num, _ := splitLength(v)
	return !math.IsNaN(num) && num == 0
}

// farOff: weit aus dem Bild geschoben (≤ -999 Pixel oder ≤ -50 em).
func farOff(v string) bool {
	num, unit := splitLength(v)
	if math.IsNaN(num) {
		return false
	}
	switch unit {
	case "em", "rem":
		return num <= -50
	case "%":
		return num <= -100
	}
	return num <= -999
}

func splitLength(v string) (float64, string) {
	v = strings.TrimSpace(strings.ToLower(v))
	i := 0
	for i < len(v) && (v[i] == '-' || v[i] == '+' || v[i] == '.' || (v[i] >= '0' && v[i] <= '9')) {
		i++
	}
	num, err := strconv.ParseFloat(v[:i], 64)
	if err != nil {
		return math.NaN(), ""
	}
	return num, strings.TrimSpace(v[i:])
}

// sameColor: Text hebt sich vom Hintergrund kaum ab (weiß auf weiß).
func sameColor(fg, bg rgb) bool {
	d := math.Abs(fg.r-bg.r) + math.Abs(fg.g-bg.g) + math.Abs(fg.b-bg.b)
	return d < 24
}

var white = rgb{255, 255, 255}

// element berechnet den Zustand für die Kinder von n.
func (c *converter) element(n *html.Node, p ctx) ctx {
	cur := p
	cur.inLink = false
	if cur.hidden {
		return cur
	}
	if _, ok := attr(n, "hidden"); ok {
		cur.hidden, cur.reason = true, "hidden attribute"
		return cur
	}
	decl, via := c.declarations(n)
	if r := hiddenBy(decl); r != "" {
		if via != "" {
			if _, inline := attr(n, "style"); !inline || hiddenBy(parseDecls(func() string { v, _ := attr(n, "style"); return v }())) == "" {
				r += " (" + via + ")"
			}
		}
		cur.hidden, cur.reason = true, r
		return cur
	}
	// Farben: Attribute (font color, bgcolor), dann CSS.
	if n.DataAtom == atom.Font {
		if v, ok := attr(n, "color"); ok {
			if col, ok := parseColor(v); ok {
				cur.color = &col.rgb
			} else if col.transparent {
				cur.hidden, cur.reason = true, "color:transparent"
				return cur
			}
		}
	}
	if v, ok := attr(n, "bgcolor"); ok {
		if col, ok := parseColor(v); ok {
			cur.bg, cur.bgImage = &col.rgb, false
		}
	}
	if _, ok := attr(n, "background"); ok {
		cur.bgImage = true
	}
	if v := decl["background-image"]; v != "" && v != "none" {
		cur.bgImage = true
	}
	if strings.Contains(decl["background"], "url(") {
		cur.bgImage = true
	}
	if v, ok := decl["color"]; ok {
		if col, ok := parseColor(v); ok {
			cur.color = &col.rgb
		} else if col.transparent {
			cur.hidden, cur.reason = true, "color:transparent"
			return cur
		}
	}
	for _, k := range []string{"background-color", "background"} {
		if v, ok := decl[k]; ok {
			if col, ok := parseColor(firstColorWord(v)); ok {
				cur.bg = &col.rgb
				if !strings.Contains(v, "url(") {
					cur.bgImage = false
				}
				break
			}
		}
	}
	return cur
}

// textHidden: Text in diesem Zustand ist unsichtbar (Farbe wie Hintergrund).
func textHidden(s ctx) (bool, string) {
	if s.hidden {
		return true, s.reason
	}
	if s.color != nil && !s.bgImage {
		bg := white
		if s.bg != nil {
			bg = *s.bg
		}
		if sameColor(*s.color, bg) {
			return true, "color like background"
		}
	}
	return false, ""
}

func (c *converter) hide(text, reason string) {
	n := 0
	for _, r := range text {
		if !unicode.IsSpace(r) && !invisible(r) {
			n++
		}
	}
	if n == 0 {
		return
	}
	c.res.HiddenChars += n
	for _, have := range c.res.HiddenReasons {
		if have == reason {
			return
		}
	}
	c.res.HiddenReasons = append(c.res.HiddenReasons, reason)
}

// hideSubtree zählt den Text eines versteckten Teilbaums.
func (c *converter) hideSubtree(n *html.Node, reason string, depth int) {
	if depth > maxDepth {
		return
	}
	switch n.Type {
	case html.TextNode:
		c.hide(n.Data, reason)
		return
	case html.ElementNode:
		if skipped[n.DataAtom] {
			return
		}
		if n.DataAtom == atom.Img {
			if alt, ok := attr(n, "alt"); ok {
				c.hide(alt, reason)
			}
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.hideSubtree(ch, reason, depth+1)
	}
}

func (c *converter) walk(n *html.Node, s ctx, depth int) {
	if depth > maxDepth || c.out.Len() > maxTextBytes {
		return
	}
	switch n.Type {
	case html.TextNode:
		if h, why := textHidden(s); h {
			c.hide(n.Data, why)
			return
		}
		c.text(n.Data, s)
		return
	case html.CommentNode, html.DoctypeNode:
		return
	case html.DocumentNode:
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.walk(ch, s, depth+1)
		}
		return
	case html.ElementNode:
	default:
		return
	}
	if skipped[n.DataAtom] {
		return
	}
	cs := c.element(n, s)
	if cs.hidden && !s.hidden {
		c.hideSubtree(n, cs.reason, depth)
		return
	}
	switch n.DataAtom {
	case atom.Br:
		c.newline()
		return
	case atom.Hr:
		c.block()
		c.write("---")
		c.block()
		return
	case atom.Img:
		c.image(n, cs)
		return
	case atom.Li:
		c.block()
		c.write("- ")
	case atom.Blockquote:
		c.block()
		cs.quote = s.quote + 1
		c.quote = cs.quote
	case atom.Pre:
		c.block()
		cs.pre = true
	case atom.A:
		if href, ok := attr(n, "href"); ok {
			c.links = append(c.links, pendingLink{href: href, start: c.out.Len()})
			cs.inLink = true
		}
	}
	if paragraphs[n.DataAtom] {
		c.paragraph()
	} else if blocks[n.DataAtom] {
		c.block()
	}
	if n.DataAtom == atom.Td || n.DataAtom == atom.Th {
		c.pendingSpace = false
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.walk(ch, cs, depth+1)
	}
	switch n.DataAtom {
	case atom.A:
		if cs.inLink && len(c.links) > 0 {
			c.closeLink()
		}
	case atom.Blockquote:
		c.block()
		c.quote = s.quote
	}
	if paragraphs[n.DataAtom] {
		c.paragraph()
	} else if blocks[n.DataAtom] || n.DataAtom == atom.Pre || n.DataAtom == atom.Li {
		c.block()
	}
}

func (c *converter) image(n *html.Node, s ctx) {
	w, _ := attr(n, "width")
	h, _ := attr(n, "height")
	decl, _ := c.declarations(n)
	if (w != "" && tinyLength(w, 1, 0, 0)) || (h != "" && tinyLength(h, 1, 0, 0)) ||
		(decl["width"] != "" && tinyLength(decl["width"], 1, 0, 0)) || (decl["height"] != "" && tinyLength(decl["height"], 1, 0, 0)) {
		c.res.TrackingPixels++
		return
	}
	src, _ := attr(n, "src")
	if low := strings.ToLower(strings.TrimSpace(src)); strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		c.res.RemoteImages++
	}
	if alt, ok := attr(n, "alt"); ok && strings.TrimSpace(alt) != "" {
		c.text("[image: "+strings.TrimSpace(alt)+"]", s)
	}
}

// closeLink schließt den innersten offenen Link: Text und Ziel prüfen,
// Ziel sichtbar anhängen.
func (c *converter) closeLink() {
	l := c.links[len(c.links)-1]
	c.links = c.links[:len(c.links)-1]
	full := c.out.String()
	text := ""
	if l.start <= len(full) {
		text = strings.TrimSpace(strings.Join(strings.Fields(full[l.start:]), " "))
	}
	link, ok := CheckLink(l.href, text)
	if !ok {
		if strings.TrimSpace(l.href) != "" && !strings.HasPrefix(strings.TrimSpace(l.href), "#") {
			c.res.RemovedLinks++
		}
		return
	}
	c.res.Links = append(c.res.Links, link)
	if text == "" {
		c.write("[link] <" + link.URL + ">")
		return
	}
	if sameTarget(text, link.URL) {
		return
	}
	c.write(" <" + link.URL + ">")
}

// sameTarget: Der sichtbare Text ist schon die URL.
func sameTarget(text, u string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		return strings.TrimSuffix(strings.TrimPrefix(s, "www."), "/")
	}
	return norm(text) == norm(u)
}

// --- Ausgabe ----------------------------------------------------------------

func (c *converter) lastByte() byte {
	s := c.out.String()
	if s == "" {
		return '\n'
	}
	return s[len(s)-1]
}

func (c *converter) write(s string) {
	if s == "" {
		return
	}
	if c.lastByte() == '\n' && c.quote > 0 {
		c.out.WriteString(strings.Repeat("> ", c.quote))
	}
	c.out.WriteString(s)
	c.pendingSpace = false
}

func (c *converter) text(s string, st ctx) {
	if st.pre {
		for i, line := range strings.Split(s, "\n") {
			if i > 0 {
				c.newline()
			}
			c.write(line)
		}
		return
	}
	lead := len(s) > 0 && isSpaceByte(s[0])
	trail := len(s) > 0 && isSpaceByte(s[len(s)-1])
	words := strings.Fields(s)
	if len(words) == 0 {
		if lead && c.lastByte() != '\n' {
			c.pendingSpace = true
		}
		return
	}
	if (lead || c.pendingSpace) && c.lastByte() != '\n' && c.lastByte() != ' ' {
		c.out.WriteByte(' ')
	}
	c.write(strings.Join(words, " "))
	c.pendingSpace = trail
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

func (c *converter) newline() {
	if c.quote > 0 && c.lastByte() == '\n' {
		c.out.WriteString(strings.TrimSpace(strings.Repeat("> ", c.quote)))
	}
	c.out.WriteByte('\n')
	c.pendingSpace = false
}

// block beginnt eine neue Zeile, falls die aktuelle nicht leer ist.
func (c *converter) block() {
	if c.lastByte() != '\n' {
		c.out.WriteByte('\n')
	}
	c.pendingSpace = false
}

// paragraph sorgt für eine Leerzeile.
func (c *converter) paragraph() {
	c.block()
	s := c.out.String()
	if s != "" && !strings.HasSuffix(s, "\n\n") {
		c.newline()
	}
}

// tidy: Leerzeichen am Zeilenende weg, höchstens eine Leerzeile am Stück,
// keine Leerzeilen am Anfang und Ende.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(strings.Trim(l, ">")) == "" && strings.TrimSpace(l) != "" {
			l = strings.TrimSpace(l) // leere Zitatzeile: nur ">"
		}
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
