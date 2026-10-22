package sanitize

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Positivliste der Stilangaben in ausgehendem HTML (Gegenprüfung S2.3,
// Befund W3). Vorher ließ cleanStyle jede Eigenschaft ohne Remote-Inhalt
// durch; damit ließ sich Text verstecken, den die Erkennung aus S1.6 nicht
// als versteckt meldet (font-size:3px, color:rgba(0,0,0,0.02),
// filter:opacity(0), -webkit-text-fill-color:transparent,
// height:1px;width:1px;overflow:hidden, font-size:calc(0px) …). Ein Agent
// darf keine unsichtbaren Anweisungen verschicken (A02 E3). Erlaubt sind
// deshalb nur Eigenschaften, mit denen sich Text nicht verstecken lässt,
// und nur Werte in fester Form (keine Funktionen wie calc(), var(),
// keine Transparenz):
//
//	color                    #rgb, #rrggbb, rgb(r,g,b) (ohne Alpha) oder ein
//	                         bekannter Farbname; Kontrast zu Weiß mindestens
//	                         1,5:1 (Hintergrundfarben gibt es nicht)
//	font-weight              normal, bold, bolder, lighter, 100 … 900
//	font-style               normal, italic, oblique
//	font-family              Namen aus Buchstaben, Ziffern, Leerzeichen,
//	                         Komma, Bindestrich, Anführungszeichen
//	font-size                ≥ 10px, ≥ 8pt, ≥ 0.75rem, ≥ 1em, ≥ 100 % oder
//	                         x-small … xxx-large, larger (em und % nur ≥ 1,
//	                         weil sie sich verschachtelt multiplizieren)
//	text-decoration(-line)   none, underline, overline, line-through
//	text-align               left, right, center, justify, start, end
//	vertical-align           baseline, top, middle, bottom, text-top, text-bottom
//	margin(-*), padding(-*)  0, auto (nur margin) oder 1–4 Längen ≥ 0 und
//	                         höchstens 100px/75pt/10em/50 %
//
// Alles andere fällt weg (die Angabe, nicht das Element; der Text bleibt
// sichtbar). Dazu: Das Attribut title fällt weg (Tooltip-Text, den niemand
// liest), bgcolor ebenso (Hintergrund), color wird wie die Eigenschaft
// geprüft, alt eines Bildes wird gekürzt als sichtbarer Text hinter das
// Bild gesetzt ([alt]), und small/sub/sup wirken nur eine Ebene tief.

var (
	fontFamilyRE = regexp.MustCompile(`^[a-z0-9 ,'"-]{1,100}$`)
	lengthRE     = regexp.MustCompile(`^(\d{1,4}(?:\.\d{1,4})?)(px|pt|em|rem|%)$`)
	hex3or6RE    = regexp.MustCompile(`^#(?:[0-9a-f]{3}|[0-9a-f]{6})$`)
	rgb3RE       = regexp.MustCompile(`^rgb\(\s*\d{1,3}%?\s*,\s*\d{1,3}%?\s*,\s*\d{1,3}%?\s*\)$|^rgb\(\s*\d{1,3}%?\s+\d{1,3}%?\s+\d{1,3}%?\s*\)$`)
)

// MinTextContrast ist der kleinste Kontrast einer Textfarbe zu Weiß (WCAG-
// Formel). 1,5:1 lässt übliche Farben zu (Orange 2:1, Grau #aaa 2,3:1) und
// verbietet Text, der auf Weiß kaum zu sehen ist (#ddd 1,4:1, Gelb 1,1:1).
const MinTextContrast = 1.5

// MaxAltChars: so viel Alternativtext eines Bildes bleibt (sichtbar).
const MaxAltChars = 100

var styleWords = map[string]map[string]bool{
	"font-weight":    set("normal", "bold", "bolder", "lighter", "100", "200", "300", "400", "500", "600", "700", "800", "900"),
	"font-style":     set("normal", "italic", "oblique"),
	"text-align":     set("left", "right", "center", "justify", "start", "end"),
	"vertical-align": set("baseline", "top", "middle", "bottom", "text-top", "text-bottom"),
}

var fontSizeWords = set("x-small", "small", "medium", "large", "x-large", "xx-large", "xxx-large", "larger")

var decorationWords = set("none", "underline", "overline", "line-through")

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// cleanStyle prüft ein style-Attribut. Angaben mit url(), image-set(),
// @import, expression(), behavior, -moz-binding, javascript:, Kommentaren
// oder CSS-Escapes (mit denen sich all das verschleiern ließe) verwerfen
// das ganze Attribut (ok=false). Sonst bleiben nur die Deklarationen der
// Positivliste (wörtlich, ohne !important); dropped meldet, ob eine
// wegfiel. Bleibt keine, ist ok=false.
func cleanStyle(v string) (out string, ok bool) {
	out, ok, _ = cleanStyleDecls(v)
	return out, ok
}

func cleanStyleDecls(v string) (out string, ok, dropped bool) {
	v = Text(v)
	low := strings.ToLower(v)
	for _, bad := range []string{"url(", "image-set(", "image(", "@import", "expression", "behavior",
		"-moz-binding", "javascript:", "/*", `\`, "<", ">"} {
		if strings.Contains(low, bad) {
			return "", false, true
		}
	}
	if strings.ContainsFunc(v, unicode.IsControl) {
		return "", false, true
	}
	var keep []string
	for _, d := range strings.Split(v, ";") {
		if strings.TrimSpace(d) == "" {
			continue
		}
		k, val, found := strings.Cut(d, ":")
		k = strings.ToLower(strings.TrimSpace(k))
		val = strings.ToLower(strings.TrimSpace(val))
		if imp := strings.TrimSuffix(val, "!important"); imp != val {
			val = strings.TrimSpace(imp)
			d = k + ": " + val
		}
		if !found || !allowedDecl(k, val) {
			dropped = true
			continue
		}
		keep = append(keep, strings.TrimSpace(d))
	}
	if len(keep) == 0 {
		return "", false, dropped
	}
	return strings.Join(keep, "; "), true, dropped
}

// allowedDecl prüft eine Deklaration gegen die Positivliste.
func allowedDecl(k, v string) bool {
	if words, ok := styleWords[k]; ok {
		return words[v]
	}
	switch k {
	case "color":
		return readableColor(v)
	case "font-family":
		return fontFamilyRE.MatchString(v)
	case "font-size":
		return fontSizeOK(v)
	case "text-decoration", "text-decoration-line":
		for _, w := range strings.Fields(v) {
			if !decorationWords[w] {
				return false
			}
		}
		return v != ""
	case "margin", "margin-top", "margin-right", "margin-bottom", "margin-left":
		return boxLengths(v, true)
	case "padding", "padding-top", "padding-right", "padding-bottom", "padding-left":
		return boxLengths(v, false)
	}
	return false
}

// readableColor: deckende Farbe in fester Form mit Kontrast zu Weiß.
func readableColor(v string) bool {
	if !hex3or6RE.MatchString(v) && !rgb3RE.MatchString(v) {
		if _, ok := named[v]; !ok {
			return false
		}
	}
	c, ok := parseColor(v)
	if !ok || c.transparent {
		return false
	}
	return contrastToWhite(c.rgb) >= MinTextContrast
}

// contrastToWhite ist der Kontrast nach WCAG 2 (1 bis 21).
func contrastToWhite(c rgb) float64 {
	lin := func(v float64) float64 {
		v = math.Min(math.Max(v, 0), 255) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
	return 1.05 / (l + 0.05)
}

// fontSizeOK: lesbare Schriftgröße, die sich verschachtelt nicht verkleinert.
func fontSizeOK(v string) bool {
	if fontSizeWords[v] {
		return true
	}
	m := lengthRE.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return false
	}
	switch m[2] {
	case "px":
		return n >= 10
	case "pt":
		return n >= 8
	case "rem":
		return n >= 0.75
	case "em":
		return n >= 1
	case "%":
		return n >= 100
	}
	return false
}

// boxLengths: 1 bis 4 nicht negative, begrenzte Längen (oder auto bei margin).
func boxLengths(v string, autoOK bool) bool {
	parts := strings.Fields(v)
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "0" || (autoOK && p == "auto") {
			continue
		}
		m := lengthRE.FindStringSubmatch(p)
		if m == nil {
			return false
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return false
		}
		limit := map[string]float64{"px": 100, "pt": 75, "em": 10, "rem": 10, "%": 50}[m[2]]
		if n > limit {
			return false
		}
	}
	return true
}

// cleanAlt kürzt den Alternativtext eines Bildes für die sichtbare
// Wiedergabe hinter dem Bild ("" = keiner).
func cleanAlt(v string) string {
	v = strings.Join(strings.Fields(Text(v)), " ")
	if r := []rune(v); len(r) > MaxAltChars {
		v = string(r[:MaxAltChars]) + "…"
	}
	return v
}
