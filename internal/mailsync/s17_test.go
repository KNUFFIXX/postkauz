package mailsync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Akzeptanz S1.7: Der Korpus wird gespiegelt, der Sync arbeitet danach die
// Jobs ab (Extraktion, Index), und der Suchtestsatz aus dem Manifest
// (Flexion, Umlaute, ß/ss, Komposita, Phrasen, Ausschluss, Operatoren,
// Anhangstext, versteckter Text) liefert die erwarteten Treffer und
// Nichttreffer.
func TestCorpusSearchSet(t *testing.T) {
	c := standardCorpus(t)
	if len(c.Manifest.Search) < 20 {
		t.Fatalf("manifest has %d search cases", len(c.Manifest.Search))
	}
	ctx := context.Background()
	addr, _ := memServer(t, true)
	e := newEnv(t, addr)
	eng := e.engine(t, connect(t, addr, 0))
	eng.Jobs = &jobs.Runner{Store: e.st, Blobs: e.blobs, Extractor: extract.New(extract.Options{Timeout: 20 * time.Second}),
		ExtractBudget: 5 * time.Minute}
	rep, err := eng.SyncAll(ctx, Options{})
	if err != nil || rep.Mirrored != memCount || rep.Indexed != memCount || rep.IndexPending != 0 {
		t.Fatalf("sync = %+v %v", rep, err)
	}
	fileOf := map[int64]string{}
	for _, m := range c.Manifest.Messages {
		if m.MessageID == "" || memSkip(m) {
			continue
		}
		ids, err := e.st.MessageIDsByHeader(ctx, e.acct, m.MessageID)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			fileOf[id] = m.File
		}
	}
	find := func(q string) map[string]store.SearchHit {
		t.Helper()
		parsed, err := search.Parse(q, time.UTC)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		out := map[string]store.SearchHit{}
		var after *search.Cursor
		now := time.Now()
		for {
			res, err := e.st.Search(ctx, store.SearchParams{Query: parsed, AccountID: e.acct, Limit: 200, After: after, Now: now})
			if err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			for _, h := range res.Hits {
				name := fileOf[h.ID]
				if name == "" {
					name = fmt.Sprint("id:", h.ID) // Nachricht ohne Message-ID
				}
				out[name] = h
			}
			if len(res.Hits) < 200 {
				return out
			}
			last := res.Hits[len(res.Hits)-1]
			after = &search.Cursor{Score: last.Score, ID: last.ID}
		}
	}
	for _, sc := range c.Manifest.Search {
		got := find(sc.Query)
		for _, w := range sc.Want {
			if _, ok := got[w]; !ok {
				t.Errorf("%q (%s): %s missing (%d hits)", sc.Query, sc.Note, w, len(got))
			}
		}
		for _, n := range sc.Not {
			if _, ok := got[n]; ok {
				t.Errorf("%q (%s): %s must not be a hit", sc.Query, sc.Note, n)
			}
		}
		if sc.SameAs != "" {
			other := find(sc.SameAs)
			a, b := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(other))
			if !slices.Equal(a, b) {
				t.Errorf("%q and %q differ: %d vs %d hits", sc.Query, sc.SameAs, len(a), len(b))
			}
		}
		if sc.AllHaveAttachments {
			for f, h := range got {
				if !h.HasAttachments {
					t.Errorf("%q: %s has no attachment", sc.Query, f)
				}
			}
		}
		t.Logf("%-40q %4d hits  (%s)", sc.Query, len(got), sc.Note)
	}
}
