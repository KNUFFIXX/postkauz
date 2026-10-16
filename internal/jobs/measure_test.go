package jobs

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// TestS17SearchMeasurement misst Indexdauer und Suchlatenz an einer großen,
// schon gespiegelten Datenbank (Akzeptanz S1.7: 100.000 Nachrichten, Ziel
// unter 300 ms je Anfrage). Nur ausdrücklich: POSTKAUZ_S17_DB (Datenbank
// nach `sync --full`); POSTKAUZ_S17_REINDEX=1 baut den Index vorher neu auf
// und misst dabei die Indexdauer. Ergebnisse stehen in
// docs/suche_messung.md.
func TestS17SearchMeasurement(t *testing.T) {
	dbPath := os.Getenv("POSTKAUZ_S17_DB")
	if dbPath == "" {
		t.Skip("set POSTKAUZ_S17_DB to measure (see docs/suche_messung.md)")
	}
	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DBForTest()
	var msgs int
	_ = db.QueryRow(`SELECT count(*) FROM messages`).Scan(&msgs)
	size := func() string {
		var pages, pageSize int64
		_ = db.QueryRow(`PRAGMA page_count`).Scan(&pages)
		_ = db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
		return fmt.Sprintf("%.0f MB", float64(pages*pageSize)/1e6)
	}
	t.Logf("messages: %d, database: %s", msgs, size())

	if os.Getenv("POSTKAUZ_S17_REINDEX") == "1" {
		for _, q := range []string{`INSERT INTO fts_messages(fts_messages) VALUES ('delete-all')`,
			`INSERT INTO fts_trigram(fts_trigram) VALUES ('delete-all')`, `DELETE FROM index_queue`,
			`INSERT INTO index_queue(message_id, reindex, queued_at) SELECT id, 0, 0 FROM messages`} {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = db.Exec(`VACUUM`)
		before := size()
		accts, _ := st.ListAccounts(ctx)
		start := time.Now()
		total := 0
		for _, a := range accts {
			r := &Runner{Store: st, TakeOver: true}
			rep, err := r.RunAccount(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			total += rep.Indexed
		}
		d := time.Since(start)
		t.Logf("index: %d messages in %s (%.0f messages/s); database %s before, %s after",
			total, d.Round(100*time.Millisecond), float64(total)/d.Seconds(), before, size())
		for _, tbl := range []string{"fts_messages", "fts_trigram"} {
			var n int64
			if err := db.QueryRow(`SELECT sum(pgsize) FROM dbstat WHERE name LIKE ?`, tbl+"%").Scan(&n); err == nil {
				t.Logf("  %s: %.0f MB", tbl, float64(n)/1e6)
			}
		}
	}

	queries := []struct{ kind, q string }{
		{"Wort, häufig (Stamm + Teilwort)", "Rechnung"},
		{"Wort mit Umlaut", "prüfen"},
		{"Umlaut umschrieben", "pruefen"},
		{"Teilwort im Kompositum", "Vollstreck"},
		{"Teilwort, selten", "Wärmemengenzähler"},
		{"ß/ss", "Erdgeschoß"},
		{"zwei Wörter", "Rechnung Erdgeschoss"},
		{"drei Wörter", "Heizung Keller Lüftung"},
		{"Phrase", `"Erdgeschoss links"`},
		{"kurz (Präfix)", "ab"},
		{"Ausschluss", "steuer -grundsteuer"},
		{"Absender + Wort", "from:fiktiv steuer"},
		{"Betreff-Operator", "subject:Zwangsvollstreckung"},
		{"Ordner + Wort", "in:INBOX Heizung"},
		{"nur Operator (Datum sortiert)", "has:attachment"},
		{"nur Datum", "after:2025-01-01 before:2025-02-01"},
		{"nur Ordner", "in:Archiv/2024"},
		{"kein Treffer", "Xylophon"},
	}
	const runs = 15
	t.Logf("| Anfrage-Typ | Anfrage | Treffer | erste (kalt) | Median | p95 | max | Seite 2 (Cursor) |")
	t.Logf("|---|---|---:|---:|---:|---:|---:|---:|")
	var worst time.Duration
	for _, qq := range queries {
		parsed, err := search.Parse(qq.q, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		var lat []time.Duration
		var res store.SearchResult
		for i := 0; i < runs; i++ {
			s := time.Now()
			res, err = st.Search(ctx, store.SearchParams{Query: parsed, Limit: 20, Now: now})
			lat = append(lat, time.Since(s))
			if err != nil {
				t.Fatal(err)
			}
		}
		page2 := "–"
		if len(res.Hits) == 20 {
			last := res.Hits[19]
			s := time.Now()
			if _, err := st.Search(ctx, store.SearchParams{Query: parsed, Limit: 20, Now: now,
				After: &search.Cursor{Score: last.Score, ID: last.ID}}); err != nil {
				t.Fatal(err)
			}
			page2 = ms(time.Since(s))
		}
		first := lat[0]
		warm := slices.Clone(lat[1:])
		slices.Sort(warm)
		med, p95, mx := warm[len(warm)/2], warm[(len(warm)*95+99)/100-1], warm[len(warm)-1]
		worst = max(worst, mx, first)
		t.Logf("| %s | `%s` | %d | %s | %s | %s | %s | %s |", qq.kind, strings.ReplaceAll(qq.q, "|", `\|`), res.Total,
			ms(first), ms(med), ms(p95), ms(mx), page2)
	}
	t.Logf("slowest single query: %s (target < 300 ms)", ms(worst))
}

func ms(d time.Duration) string { return fmt.Sprintf("%d ms", d.Milliseconds()) }
