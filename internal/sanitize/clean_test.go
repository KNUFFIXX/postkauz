package sanitize

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestCleanHTMLAllowlist(t *testing.T) {
	src := `<!DOCTYPE html><html><head><title>T</title><style>.x{color:red}</style>
<script>alert(1)</script></head><body onload="evil()">
<p class="x" id="p1" style="color:#333" onclick="evil()">Hallo <b>Welt</b></p>
<script>alert(2)</script><!-- Kommentar -->
<form action="https://evil.example.com/"><label>Name</label><input name="n"><button>Go</button></form>
<a href="https://www.example.com/a?b=1">sicher</a>
<a href="http://www.example.com/">unsicher</a>
<a href="java&#x09;script:alert(1)">js</a>
<a href="mailto:info@example.com">Mail</a>
<img src="https://tracker.example.com/p.gif" width="1" height="1">
<img src="cid:logo@example.com" alt="Logo">
<div style="background:url(https://example.com/x.png)">bg</div>
<iframe src="https://example.com/"></iframe><object data="x"></object>
<custom-tag>innen</custom-tag>
<table bgcolor="#eeeeee" cellpadding="2"><tr><td colspan="2">Zelle</td></tr></table>
<p>Null&#x200B;breite</p>
</body></html>`
	r := CleanHTML(src)
	for _, bad := range []string{"<script", "alert", "onload", "onclick", "class=", "id=", "<form", "<input",
		"<button", "evil", "http://", "javascript", "tracker", "url(", "<iframe", "<object", "<style",
		"<title", "Kommentar", "<custom-tag", "\u200b"} {
		if strings.Contains(r.HTML, bad) {
			t.Errorf("cleaned HTML still contains %q:\n%s", bad, r.HTML)
		}
	}
	for _, good := range []string{`<p style="color:#333">Hallo <b>Welt</b></p>`, `<a href="https://www.example.com/a?b=1">sicher</a>`,
		"unsicher", "js", `<a href="mailto:info@example.com">Mail</a>`, `<img src="cid:logo@example.com"> [Logo]`,
		"<div>bg</div>", "innen", "Name", `<table cellpadding="2">`, `<td colspan="2">Zelle</td>`, "Nullbreite"} {
		if !strings.Contains(r.HTML, good) {
			t.Errorf("cleaned HTML lacks %q:\n%s", good, r.HTML)
		}
	}
	if r.RemovedLinks != 2 || r.RemovedImages != 1 || r.RemovedElements == 0 || r.RemovedAttributes == 0 || !r.Removed() {
		t.Errorf("counters = %+v", r)
	}
}

func TestCleanHTMLKeepsHarmlessInput(t *testing.T) {
	src := "<p>Guten Tag,<br>\nanbei die <strong>Unterlagen</strong>.</p>\n<ul>\n<li>eins</li>\n</ul>\n"
	r := CleanHTML(src)
	if r.HTML != src || r.Removed() {
		t.Fatalf("clean = %q (%+v)", r.HTML, r)
	}
	// Idempotent: zweimal bereinigt ist wie einmal.
	if again := CleanHTML(r.HTML); again.HTML != r.HTML {
		t.Fatalf("not idempotent: %q", again.HTML)
	}
}

// FuzzCleanHTML: Was CleanHTML ausgibt, enthält – erneut geparst – nur
// erlaubte Elemente und Attribute, Links nur https:/mailto:, Bilder nur cid:.
func FuzzCleanHTML(f *testing.F) {
	for _, s := range []string{"<p>a</p>", `<a href="https://e.example">e</a>`, `<img src="cid:x">`,
		`<a href="jav&#x61;script:x">j</a>`, "<svg><a href='javascript:x'>s</a></svg>", "<table><tr><td>x<form><input>",
		"<p style='color:red' onclick=x>p", "<noscript><p>x</p></noscript>", "<math><mi><a href=javascript:1>m",
		`<a href=" https://e.example">e</a>`, strings.Repeat("<div>", 600)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := CleanHTML(s).HTML
		doc, err := html.Parse(strings.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		var walk func(n *html.Node, depth int)
		walk = func(n *html.Node, depth int) {
			if n.Type == html.CommentNode {
				t.Fatalf("comment in output %q", out)
			}
			if n.Type == html.ElementNode && depth > 2 { // html, head, body des Parsers ausgenommen
				if !cleanAllowed[n.DataAtom] || n.Namespace != "" {
					t.Fatalf("element %q in output %q", n.Data, out)
				}
				for _, a := range n.Attr {
					v := strings.ToLower(a.Val)
					switch {
					case !cleanAttrs[a.Key]:
						t.Fatalf("attribute %q in output %q", a.Key, out)
					case a.Key == "href" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "mailto:"):
						t.Fatalf("href %q in output %q", a.Val, out)
					case a.Key == "src" && !strings.HasPrefix(v, "cid:"):
						t.Fatalf("src %q in output %q", a.Val, out)
					case a.Key == "style" && strings.Contains(v, "url("):
						t.Fatalf("style %q in output %q", a.Val, out)
					}
				}
			}
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch, depth+1)
			}
		}
		walk(doc, 0)
	})
}

func TestCleanStyle(t *testing.T) {
	for _, v := range []string{"background:url(x)", "background-image: image-set('a' 1x)", "x:expression(alert(1))",
		"@import 'x'", "behavior:url(x.htc)", "-moz-binding:url(x)", `color:\72 ed`, "color:red/**/"} {
		if _, ok := cleanStyle(v); ok {
			t.Errorf("style %q accepted", v)
		}
	}
	if v, ok := cleanStyle(" color: #333; font-weight: bold "); !ok || v != "color: #333; font-weight: bold" {
		t.Errorf("harmless style = %q, %v", v, ok)
	}
}
