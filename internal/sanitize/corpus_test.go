package sanitize

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
)

// Akzeptanz S1.6 (Teil Bereinigung): Gegen den Standardkorpus liefern die
// Mails mit verstecktem Text hidden=true und verlieren die Anweisung, alle
// anderen HTML-Mails melden nichts (keine Fehlalarme); Links, entfernte
// Ziele, Tracking-Pixel und Zitate wie im Manifest.
func TestCorpusCleanup(t *testing.T) {
	c, err := corpus.Generate(corpus.Options{ZipBombBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	hidden, quotes, links := 0, 0, 0
	for i, raw := range c.Raw {
		e := c.Manifest.Messages[i]
		internal, _ := time.Parse(time.RFC3339, e.InternalDate)
		m, err := mimeparse.Parse(bytes.NewReader(raw), mimeparse.Options{InternalDate: internal})
		if err != nil {
			continue // kaputte Nachrichten prüft der Parser-Test
		}
		var r Result
		if m.HTML != "" {
			r = HTML(m.HTML)
		}
		if r.Hidden != e.HiddenText {
			t.Errorf("%s (%s): hidden = %v (%v), want %v", e.File, e.Kind, r.Hidden, r.HiddenReasons, e.HiddenText)
		}
		if e.HiddenText {
			hidden++
			if strings.Contains(r.Text, "Ignoriere") || strings.Contains(r.Text, e.HiddenContains) {
				t.Errorf("%s: hidden instruction left in the text", e.File)
			}
		}
		if e.HTMLContains != "" && !strings.Contains(r.Text, e.HTMLContains) {
			t.Errorf("%s: html text lacks %q:\n%s", e.File, e.HTMLContains, r.Text)
		}
		if len(e.Links) > 0 {
			links++
			if len(r.Links) != len(e.Links) {
				t.Fatalf("%s: links %+v, want %+v", e.File, r.Links, e.Links)
			}
			for k, want := range e.Links {
				got := r.Links[k]
				w := slices.Clone(got.Warnings)
				slices.Sort(w)
				ww := slices.Clone(want.Warnings)
				slices.Sort(ww)
				if got.Host != want.Host || !slices.Equal(w, ww) {
					t.Errorf("%s: link %d = %s %v, want %s %v", e.File, k, got.Host, got.Warnings, want.Host, want.Warnings)
				}
			}
		}
		if r.RemovedLinks != e.RemovedLinks || r.TrackingPixels != e.TrackingPixels {
			if e.Kind != "newsletter" && e.Kind != "single" && e.Kind != "thread" || e.RemovedLinks+e.TrackingPixels > 0 {
				t.Errorf("%s: removed %d, pixels %d; want %d, %d", e.File, r.RemovedLinks, r.TrackingPixels, e.RemovedLinks, e.TrackingPixels)
			}
		}
		if e.UnquotedContains != "" {
			quotes++
			text := m.Text
			if text == "" {
				text = r.Text
			}
			un := StripQuotes(text)
			if !strings.Contains(un, e.UnquotedContains) || strings.Contains(un, e.QuotedContains) {
				t.Errorf("%s: unquoted text = %q", e.File, un)
			}
			if !strings.Contains(text, e.QuotedContains) {
				t.Errorf("%s: full text lacks the quote", e.File)
			}
		}
	}
	if hidden != 3 || quotes != 2 || links != 1 {
		t.Fatalf("corpus cases: hidden %d, quotes %d, links %d", hidden, quotes, links)
	}
}
