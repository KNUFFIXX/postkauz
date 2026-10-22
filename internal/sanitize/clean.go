package sanitize

import (
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// CleanResult ist das Ergebnis von CleanHTML.
type CleanResult struct {
	// HTML ist das bereinigte Fragment (Inhalt von <body>, ohne Hülle).
	HTML string
	// Zähler dessen, was entfernt wurde (für Hinweise an den Aufrufer).
	RemovedElements   int // Elemente samt Inhalt (Skripte, Formularfelder …) oder ausgepackt
	RemovedLinks      int // Linkziele außer https: und mailto: (der Text bleibt)
	RemovedImages     int // Bilder außer cid:
	RemovedAttributes int // Attribute außerhalb der Allowlist, Stile mit Remote-Inhalt
}

// Removed meldet, ob überhaupt etwas entfernt wurde.
func (r CleanResult) Removed() bool {
	return r.RemovedElements+r.RemovedLinks+r.RemovedImages+r.RemovedAttributes > 0
}

// cleanDropped sind Elemente, die samt Inhalt wegfallen: Skripte, Stile,
// eingebettete Objekte, Formularfelder, Kopfelemente.
var cleanDropped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Head: true, atom.Title: true, atom.Template: true,
	atom.Noscript: true, atom.Iframe: true, atom.Object: true, atom.Embed: true, atom.Svg: true,
	atom.Math: true, atom.Canvas: true, atom.Audio: true, atom.Video: true, atom.Select: true,
	atom.Option: true, atom.Optgroup: true, atom.Input: true, atom.Button: true, atom.Textarea: true,
	atom.Applet: true, atom.Frameset: true, atom.Frame: true, atom.Link: true, atom.Meta: true,
	atom.Base: true, atom.Param: true, atom.Source: true, atom.Track: true, atom.Map: true,
	atom.Area: true, atom.Datalist: true, atom.Output: true, atom.Progress: true, atom.Meter: true,
	atom.Dialog: true,
}

// cleanAllowed sind Elemente, die mit gefilterten Attributen bleiben. Alle
// anderen (form, label, unbekannte Tags …) werden ausgepackt: Der Inhalt
// bleibt, das Element fällt weg.
var cleanAllowed = map[atom.Atom]bool{
	atom.A: true, atom.Abbr: true, atom.Address: true, atom.Article: true, atom.Aside: true,
	atom.B: true, atom.Big: true, atom.Blockquote: true, atom.Br: true, atom.Caption: true,
	atom.Center: true, atom.Cite: true, atom.Code: true, atom.Col: true, atom.Colgroup: true,
	atom.Dd: true, atom.Del: true, atom.Dfn: true, atom.Div: true, atom.Dl: true, atom.Dt: true,
	atom.Em: true, atom.Figcaption: true, atom.Figure: true, atom.Font: true, atom.Footer: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Header: true, atom.Hr: true, atom.I: true, atom.Img: true, atom.Ins: true, atom.Kbd: true,
	atom.Li: true, atom.Mark: true, atom.Ol: true, atom.P: true, atom.Pre: true, atom.Q: true,
	atom.S: true, atom.Samp: true, atom.Section: true, atom.Small: true, atom.Span: true,
	atom.Strike: true, atom.Strong: true, atom.Sub: true, atom.Sup: true, atom.Table: true,
	atom.Tbody: true, atom.Td: true, atom.Tfoot: true, atom.Th: true, atom.Thead: true, atom.Tr: true,
	atom.Tt: true, atom.U: true, atom.Ul: true, atom.Var: true, atom.Wbr: true,
}

// cleanVoid sind Elemente ohne End-Tag.
var cleanVoid = map[atom.Atom]bool{atom.Br: true, atom.Hr: true, atom.Img: true, atom.Col: true, atom.Wbr: true}

// cleanAttrs sind die erlaubten Attribute (für alle erlaubten Elemente);
// href und src prüft cleanURL, style prüft cleanStyle.
var cleanAttrs = map[string]bool{
	"title": true, "dir": true, "lang": true, "align": true, "valign": true, "width": true,
	"height": true, "colspan": true, "rowspan": true, "border": true, "cellpadding": true,
	"cellspacing": true, "bgcolor": true, "color": true, "face": true, "size": true, "start": true,
	"type": true, "alt": true, "style": true, "href": true, "src": true,
}

// CleanHTML bereinigt HTML für eine ausgehende Nachricht mit einer
// Allowlist (A02 E3): keine Skripte, Stile, Formulare, eingebetteten
// Objekte oder Kommentare; Links nur https: und mailto: (sonst bleibt nur
// der Text), Bilder nur cid: (Teil derselben Nachricht), Stilangaben ohne
// Remote-Inhalt (url(), @import, expression …), keine Ereignis-Attribute,
// keine Klassen und IDs (Regeln aus <style> fallen ohnehin weg).
// Unsichtbare Formatzeichen werden aus dem Text entfernt (Text).
//
// CleanHTML prüft nicht auf versteckten Text; das tut HTML (Result.Hidden),
// und der Aufrufer entscheidet (compose lehnt ab). Ein ganzes Dokument wird
// auf den Inhalt von <body> reduziert.
func CleanHTML(src string) CleanResult {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return CleanResult{}
	}
	c := &cleaner{}
	body := findBody(doc, 0)
	if body == nil {
		body = doc
	}
	for ch := body.FirstChild; ch != nil; ch = ch.NextSibling {
		c.node(ch, 0)
	}
	c.res.HTML = c.out.String()
	return c.res
}

type cleaner struct {
	out strings.Builder
	res CleanResult
}

func findBody(n *html.Node, depth int) *html.Node {
	if depth > maxDepth {
		return nil
	}
	if n.Type == html.ElementNode && n.DataAtom == atom.Body {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if b := findBody(ch, depth+1); b != nil {
			return b
		}
	}
	return nil
}

func (c *cleaner) node(n *html.Node, depth int) {
	if depth > maxDepth {
		c.res.RemovedElements++
		return
	}
	switch n.Type {
	case html.TextNode:
		c.out.WriteString(html.EscapeString(Text(n.Data)))
		return
	case html.ElementNode:
	default: // Kommentare, Doctype
		return
	}
	if cleanDropped[n.DataAtom] {
		c.res.RemovedElements++
		return
	}
	if !cleanAllowed[n.DataAtom] {
		c.res.RemovedElements++
		c.children(n, depth)
		return
	}
	attrs, ok := c.attributes(n)
	if !ok {
		// Link ohne erlaubtes Ziel: Text behalten; Bild ohne cid: weglassen.
		if n.DataAtom == atom.Img {
			return
		}
		c.children(n, depth)
		return
	}
	c.out.WriteString("<" + n.DataAtom.String())
	for _, a := range attrs {
		c.out.WriteString(" " + a.Key + `="` + html.EscapeString(a.Val) + `"`)
	}
	c.out.WriteString(">")
	if cleanVoid[n.DataAtom] {
		return
	}
	c.children(n, depth)
	c.out.WriteString("</" + n.DataAtom.String() + ">")
}

func (c *cleaner) children(n *html.Node, depth int) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.node(ch, depth+1)
	}
}

// attributes filtert die Attribute; ok=false heißt: a ohne erlaubtes Ziel
// bzw. img ohne cid:-Quelle.
func (c *cleaner) attributes(n *html.Node) ([]html.Attribute, bool) {
	var out []html.Attribute
	hasHref, hasSrc := false, false
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" || !cleanAttrs[key] {
			c.res.RemovedAttributes++
			continue
		}
		switch key {
		case "href":
			if n.DataAtom != atom.A {
				c.res.RemovedAttributes++
				continue
			}
			u, ok := cleanURL(a.Val, "https", "mailto")
			if !ok {
				continue
			}
			hasHref = true
			out = append(out, html.Attribute{Key: key, Val: u})
		case "src":
			if n.DataAtom != atom.Img {
				c.res.RemovedAttributes++
				continue
			}
			u, ok := cleanURL(a.Val, "cid")
			if !ok {
				continue
			}
			hasSrc = true
			out = append(out, html.Attribute{Key: key, Val: u})
		case "style":
			v, ok := cleanStyle(a.Val)
			if !ok {
				c.res.RemovedAttributes++
				continue
			}
			out = append(out, html.Attribute{Key: key, Val: v})
		default:
			out = append(out, html.Attribute{Key: key, Val: Text(a.Val)})
		}
	}
	switch {
	case n.DataAtom == atom.A && !hasHref:
		if _, had := attr(n, "href"); had {
			c.res.RemovedLinks++
		}
		return nil, false
	case n.DataAtom == atom.Img && !hasSrc:
		c.res.RemovedImages++
		return nil, false
	}
	return out, true
}

// cleanURL prüft ein Ziel gegen die erlaubten Schemata und liefert es
// normalisiert. Leerraum und Steuerzeichen im Ziel werden entfernt (wie es
// Browser tun), damit „java script:" nicht durchrutscht.
func cleanURL(v string, schemes ...string) (string, bool) {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || Invisible(r) || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, v)
	u, err := url.Parse(v)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	for _, s := range schemes {
		if scheme != s {
			continue
		}
		u.Scheme = scheme
		if scheme == "https" && u.Host == "" {
			return "", false
		}
		if scheme != "https" && u.Opaque == "" {
			return "", false
		}
		return u.String(), true
	}
	return "", false
}

// cleanStyle lässt Stilangaben ohne Remote-Inhalt und ohne Tricks durch.
// Angaben mit url(), image-set(), @import, expression(), behavior,
// -moz-binding, javascript:, Kommentaren oder CSS-Escapes (mit denen sich
// all das verschleiern ließe) fallen als Ganzes weg.
func cleanStyle(v string) (string, bool) {
	v = Text(v)
	low := strings.ToLower(v)
	for _, bad := range []string{"url(", "image-set(", "image(", "@import", "expression", "behavior",
		"-moz-binding", "javascript:", "/*", `\`, "<", ">"} {
		if strings.Contains(low, bad) {
			return "", false
		}
	}
	if strings.ContainsFunc(v, unicode.IsControl) {
		return "", false
	}
	return strings.TrimSpace(v), true
}
