package sanitize

import (
	"math"
	"strconv"
	"strings"
)

// parseDecls liest Deklarationen „a: b; c: d" (klein geschrieben, ohne
// !important). Werte mit Klammern (rgb(…)) bleiben ganz.
func parseDecls(s string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		v = strings.TrimSpace(strings.TrimSuffix(v, "!important"))
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// parseCSS liest einfache Regeln aus einem Stylesheet: Selektoren aus Tag,
// #id und .klasse (auch kombiniert wie p.klein), durch Komma getrennt.
// Alles andere (Nachfahren, Pseudoklassen, @media-Blöcke) wird übergangen.
// Kommentare werden entfernt.
func parseCSS(css string, rules map[string]map[string]string) {
	for {
		i := strings.Index(css, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(css[i+2:], "*/")
		if j < 0 {
			css = css[:i]
			break
		}
		css = css[:i] + css[i+2+j+2:]
	}
	depth := 0
	start := 0
	sel := ""
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			if depth == 0 {
				sel = strings.TrimSpace(css[start:i])
				start = i + 1
			}
			depth++
		case '}':
			depth--
			if depth == 0 {
				body := css[start:i]
				start = i + 1
				if strings.HasPrefix(sel, "@") || strings.Contains(body, "{") {
					continue // @media u. Ä.: übergehen
				}
				decls := parseDecls(body)
				for _, one := range strings.Split(sel, ",") {
					one = strings.TrimSpace(one)
					if !simpleSelector(one) {
						continue
					}
					key := normalizeSelector(one)
					if rules[key] == nil {
						rules[key] = map[string]string{}
					}
					for k, v := range decls {
						rules[key][k] = v
					}
				}
			}
			if depth < 0 {
				depth = 0
			}
		}
	}
}

func simpleSelector(s string) bool {
	if s == "" || strings.ContainsAny(s, " >+~:[*") {
		return false
	}
	// höchstens eine Klasse oder eine ID (Kombinationen wie .a.b übergehen)
	return strings.Count(s, ".")+strings.Count(s, "#") <= 1
}

// normalizeSelector: Tagnamen klein, Klassen und IDs wie geschrieben.
func normalizeSelector(s string) string {
	i := strings.IndexAny(s, ".#")
	if i < 0 {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:i]) + s[i:]
}

// color ist eine geparste Farbe; transparent ist gesetzt, wenn die Farbe
// ausdrücklich durchsichtig ist (transparent, Alpha 0).
type color struct {
	rgb
	transparent bool
}

var named = map[string]rgb{
	"white": {255, 255, 255}, "black": {0, 0, 0}, "red": {255, 0, 0}, "green": {0, 128, 0}, "blue": {0, 0, 255},
	"gray": {128, 128, 128}, "grey": {128, 128, 128}, "silver": {192, 192, 192}, "yellow": {255, 255, 0},
	"snow": {255, 250, 250}, "ivory": {255, 255, 240}, "whitesmoke": {245, 245, 245}, "ghostwhite": {248, 248, 255},
	"floralwhite": {255, 250, 240}, "navy": {0, 0, 128}, "maroon": {128, 0, 0}, "purple": {128, 0, 128},
	"orange": {255, 165, 0}, "darkgray": {169, 169, 169}, "lightgray": {211, 211, 211}, "lightgrey": {211, 211, 211},
}

// parseColor liest #rgb, #rrggbb, rgb(), rgba() und einige Farbnamen.
func parseColor(v string) (color, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case v == "transparent":
		return color{transparent: true}, false
	case strings.HasPrefix(v, "#"):
		h := v[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) == 8 { // #rrggbbaa
			if a, err := strconv.ParseUint(h[6:8], 16, 8); err == nil && a == 0 {
				return color{transparent: true}, false
			}
			h = h[:6]
		}
		if len(h) != 6 {
			return color{}, false
		}
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return color{}, false
		}
		return color{rgb: rgb{float64(n >> 16), float64(n >> 8 & 0xff), float64(n & 0xff)}}, true
	case strings.HasPrefix(v, "rgb"):
		open, close := strings.Index(v, "("), strings.LastIndex(v, ")")
		if open < 0 || close < open {
			return color{}, false
		}
		parts := strings.FieldsFunc(v[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return color{}, false
		}
		var c [3]float64
		for i := range 3 {
			p := parts[i]
			f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return color{}, false
			}
			if strings.HasSuffix(p, "%") {
				f *= 2.55
			}
			c[i] = f
		}
		if len(parts) >= 4 {
			a, err := strconv.ParseFloat(strings.TrimSuffix(parts[3], "%"), 64)
			if err == nil && a == 0 {
				return color{transparent: true}, false
			}
		}
		return color{rgb: rgb{c[0], c[1], c[2]}}, true
	case strings.HasPrefix(v, "hsl"):
		return parseHSL(v)
	}
	if c, ok := named[v]; ok {
		return color{rgb: c}, true
	}
	return color{}, false
}

// parseHSL liest hsl()/hsla() (Farbton in Grad, Sättigung und Helligkeit
// in Prozent, Alpha optional; mit Kommas oder Leerzeichen und „/").
func parseHSL(v string) (color, bool) {
	open, close := strings.Index(v, "("), strings.LastIndex(v, ")")
	if open < 0 || close < open {
		return color{}, false
	}
	parts := strings.FieldsFunc(v[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) < 3 {
		return color{}, false
	}
	h, err1 := strconv.ParseFloat(strings.TrimSuffix(parts[0], "deg"), 64)
	s, err2 := strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
	l, err3 := strconv.ParseFloat(strings.TrimSuffix(parts[2], "%"), 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return color{}, false
	}
	if len(parts) >= 4 {
		a, err := strconv.ParseFloat(strings.TrimSuffix(parts[3], "%"), 64)
		if err == nil && a == 0 {
			return color{transparent: true}, false
		}
	}
	s, l = min(max(s, 0), 100)/100, min(max(l, 0), 100)/100
	h = math.Mod(math.Mod(h, 360)+360, 360)
	// CSS Color 4: f(n) = l - a·max(-1, min(k-3, 9-k, 1)), k = (n + h/30) mod 12
	a := s * min(l, 1-l)
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		return 255 * (l - a*max(-1, min(k-3, 9-k, 1)))
	}
	return color{rgb: rgb{f(0), f(8), f(4)}}, true
}

// firstColorWord holt aus einer background-Kurzschreibweise die Farbe.
func firstColorWord(v string) string {
	for _, fn := range []string{"rgb", "hsl"} {
		if i := strings.Index(v, fn); i >= 0 {
			if j := strings.Index(v[i:], ")"); j >= 0 {
				return v[i : i+j+1]
			}
		}
	}
	for _, w := range strings.Fields(v) {
		if strings.HasPrefix(w, "#") || named[w] != (rgb{}) || w == "black" || w == "transparent" {
			return w
		}
	}
	return v
}
