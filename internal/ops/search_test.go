package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

func TestMessagesSearch(t *testing.T) {
	e := newMirrorEnv(t)
	id := e.seed(t)
	ctx := context.Background()
	search1 := func(in map[string]any) SearchOutput {
		t.Helper()
		out, err := e.run("messages.search", in, "", false)
		if err != nil {
			t.Fatalf("%v: %v", in, err)
		}
		return out.(SearchOutput)
	}
	// Vor dem Index-Job: keine Treffer, aber der Hinweis auf Wartende.
	if o := search1(map[string]any{"query": "Rückruf"}); len(o.Hits) != 0 || o.IndexPending != 1 || o.Order != "relevance" {
		t.Fatalf("before indexing = %+v", o)
	}
	for {
		n, err := e.st.IndexBatch(ctx, 0, 100, search.NewStemmer().Build)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	// Text des extrahierten Anhangs, Umlaut-Variante, nur dieses Konto.
	o := search1(map[string]any{"query": "rueckruf", "account": "demo", "explain": true})
	if len(o.Hits) != 1 || o.Hits[0].ID != id || o.Hits[0].Account != "demo" || o.Hits[0].Subject != "Hallo" ||
		len(o.Hits[0].Flags) != 1 || o.Hits[0].Flags[0] != "seen" || o.Total != 1 || o.IndexPending != 0 || o.Hits[0].Cursor == "" {
		t.Fatalf("hits = %+v", o)
	}
	x := o.Explain
	if x == nil || x.Scope != "account demo" || len(x.Terms) != 1 || x.Terms[0].Mode != "stem" ||
		!strings.Contains(x.SQL, "fts_trigram") || strings.Contains(strings.ToLower(x.SQL), "ruckruf") {
		t.Fatalf("explain = %+v", x)
	}
	// Seite voll: next_cursor; Folgeseite leer.
	o = search1(map[string]any{"query": "Hallo", "limit": 1})
	if o.NextCursor == "" || len(o.Hits) != 1 {
		t.Fatalf("page 1 = %+v", o)
	}
	if o2 := search1(map[string]any{"query": "Hallo", "limit": 1, "cursor": o.NextCursor}); len(o2.Hits) != 0 || o2.NextCursor != "" {
		t.Fatalf("page 2 = %+v", o2)
	}
	// Nur Operator: Datumssortierung.
	if o := search1(map[string]any{"query": "is:read"}); o.Order != "date" || len(o.Hits) != 1 {
		t.Fatalf("filter only = %+v", o)
	}
	// Eingabefehler: Abfragesprache, fremder Cursor, Grenze; unbekanntes Konto.
	var ie *InputError
	for _, in := range []map[string]any{{"query": "has:pdf"}, {"query": "Text", "cursor": o.NextCursor},
		{"query": "Hallo", "limit": 501}, {"query": "Hallo", "cursor": "kaputt"}} {
		if _, err := e.run("messages.search", in, "", false); !errors.As(err, &ie) {
			t.Errorf("%v: err = %v, want input error", in, err)
		}
	}
	if _, err := e.run("messages.search", map[string]any{"query": "Hallo", "account": "nope"}, "", false); !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("unknown account: %v", err)
	}
	// Audit ohne Suchtext.
	if _, err := e.run("messages.search", map[string]any{"query": "Geheimprojekt from:anna", "account": "demo"}, "", false); err != nil {
		t.Fatal(err)
	}
	ev := e.au.Events()
	last := ev[len(ev)-1]
	p := string(last.ParamsRedacted)
	if last.Operation != "messages.search" || strings.Contains(p, "Geheimprojekt") || strings.Contains(p, "anna") ||
		!strings.Contains(p, `"terms":2`) || !strings.Contains(p, `"from"`) || len(last.Targets) != 1 || last.Targets[0] != "account:demo" {
		t.Fatalf("audit = %s %v", p, last.Targets)
	}
}
