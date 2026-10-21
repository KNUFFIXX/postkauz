package store

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/search"
)

// Suche mit Texttermen (Relevanz). Gemessen bei 100.000 Nachrichten
// (docs/suche_messung.md): Zwischentabellen und GROUP BY in SQLite kosten
// mehr als die FTS-Abfragen selbst. Deshalb:
//
//  1. Je Term zwei Abfragen (Wortindex mit BM25, Trigram-Index nur rowid),
//     beide nach rowid sortiert; FTS5 liefert sie ohne Sortieren. Die
//     Abfragen aller Terme laufen nebeneinander (eigene Verbindungen).
//  2. Vereinigung je Term und Schnitt über die Terme in Go (Merge sortierter
//     Listen, linear); Ausschluss-Terme werden ebenso abgezogen.
//  3. Konto, Operatoren und Datum der Kandidaten in einer Abfrage über den
//     schmalen Index messages_search; die Kandidaten kommen als JSON-Array
//     (json_each) hinein.
//  4. Wert (Relevanz × Datumsanteil), Cursor und Sortierung in Go; die
//     Zeilen der Seite holt eine letzte Abfrage.
//
// Nutzertext steht weiterhin nur in Parametern (FTS5-Ausdrücke aus
// internal/search, Filterwerte).

const (
	sqlWordHits = `SELECT rowid, bm25(fts_messages, ` + bm25Weights + `) FROM fts_messages WHERE fts_messages MATCH ? ORDER BY rowid`
	sqlWordIDs  = `SELECT rowid FROM fts_messages WHERE fts_messages MATCH ? ORDER BY rowid`
	sqlTriIDs   = `SELECT rowid FROM fts_trigram WHERE fts_trigram MATCH ? ORDER BY rowid`
)

// hitList sind Nachrichten-IDs aufsteigend, mit Wert (negativ, kleiner ist
// besser; nur bei Wort-Treffern aus BM25).
type hitList struct {
	ids []int64
	r   []float64
}

// ftsJob ist eine FTS-Abfrage, die nebenläufig laufen kann.
type ftsJob struct {
	sql   string
	expr  string
	score bool
	out   hitList
	err   error
}

func (s *SQLite) runFTS(ctx context.Context, j *ftsJob) {
	rows, err := s.db.QueryContext(ctx, j.sql, j.expr)
	if err != nil {
		j.err = fmt.Errorf("store: search: %w", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var r float64
		if j.score {
			err = rows.Scan(&id, &r)
		} else {
			err = rows.Scan(&id)
		}
		if err != nil {
			j.err = err
			return
		}
		j.out.ids = append(j.out.ids, id)
		if j.score {
			j.out.r = append(j.out.r, r)
		}
	}
	j.err = rows.Err()
}

// runAll führt die Abfragen mit höchstens so vielen gleichzeitig aus, wie
// Prozessoren da sind (höchstens 4). Die Arbeiter laufen unter crash.Go
// (A02 A5): Ein Panic beendet die Suche mit einem *crash.PanicError statt
// den Prozess (z. B. den MCP-Server). Sie holen sich die nächste Abfrage
// über einen Zähler, damit ein abgestürzter Arbeiter niemanden blockiert.
func (s *SQLite) runAll(ctx context.Context, jobs []*ftsJob) error {
	workers := min(runtime.GOMAXPROCS(0), 4, len(jobs))
	var next atomic.Int64
	done := make([]<-chan error, 0, workers)
	for range workers {
		done = append(done, crash.Go("search query", func() error {
			for i := next.Add(1) - 1; i < int64(len(jobs)); i = next.Add(1) - 1 {
				s.runFTS(ctx, jobs[i])
			}
			return nil
		}))
	}
	var perr error
	for _, d := range done {
		if err := <-d; err != nil && perr == nil {
			perr = err
		}
	}
	if perr != nil {
		return perr
	}
	for _, j := range jobs {
		if j.err != nil {
			return j.err
		}
	}
	return ctx.Err()
}

// unionHits vereinigt Wort- und Teilwort-Treffer eines Terms; ein reiner
// Teilwort-Treffer bekommt substringScore.
func unionHits(word hitList, tri []int64) hitList {
	out := hitList{ids: make([]int64, 0, max(len(word.ids), len(tri))), r: make([]float64, 0, max(len(word.ids), len(tri)))}
	i, k := 0, 0
	for i < len(word.ids) || k < len(tri) {
		switch {
		case k >= len(tri) || (i < len(word.ids) && word.ids[i] < tri[k]):
			out.ids, out.r = append(out.ids, word.ids[i]), append(out.r, word.r[i])
			i++
		case i >= len(word.ids) || tri[k] < word.ids[i]:
			out.ids, out.r = append(out.ids, tri[k]), append(out.r, substringScore)
			k++
		default: // in beiden
			out.ids, out.r = append(out.ids, word.ids[i]), append(out.r, word.r[i])
			i, k = i+1, k+1
		}
	}
	return out
}

// intersectHits behält Nachrichten, die in beiden Listen stehen; die Werte
// addieren sich.
func intersectHits(a, b hitList) hitList {
	out := hitList{}
	i, k := 0, 0
	for i < len(a.ids) && k < len(b.ids) {
		switch {
		case a.ids[i] < b.ids[k]:
			i++
		case b.ids[k] < a.ids[i]:
			k++
		default:
			out.ids, out.r = append(out.ids, a.ids[i]), append(out.r, a.r[i]+b.r[k])
			i, k = i+1, k+1
		}
	}
	return out
}

// subtractIDs entfernt die (sortierten) IDs aus der Liste.
func subtractIDs(a hitList, drop []int64) hitList {
	out := hitList{}
	k := 0
	for i, id := range a.ids {
		for k < len(drop) && drop[k] < id {
			k++
		}
		if k < len(drop) && drop[k] == id {
			continue
		}
		out.ids, out.r = append(out.ids, id), append(out.r, a.r[i])
	}
	return out
}

// mergeIDs vereinigt sortierte ID-Listen.
func mergeIDs(a, b []int64) []int64 {
	out := make([]int64, 0, len(a)+len(b))
	i, k := 0, 0
	for i < len(a) || k < len(b) {
		switch {
		case k >= len(b) || (i < len(a) && a[i] < b[k]):
			out = append(out, a[i])
			i++
		case i >= len(a) || b[k] < a[i]:
			out = append(out, b[k])
			k++
		default:
			out = append(out, a[i])
			i, k = i+1, k+1
		}
	}
	return out
}

// textJobs legt die FTS-Abfragen einer Anfrage an: je positivem Term Wort
// (mit BM25) und Teilwort, je Ausschluss-Term Wort und Teilwort (nur IDs).
func textJobs(q search.Query) (pos [][2]*ftsJob, neg [][2]*ftsJob, all []*ftsJob) {
	add := func(t search.Term, score bool) [2]*ftsJob {
		var pair [2]*ftsJob
		if t.Word != "" {
			sqlText := sqlWordIDs
			if score {
				sqlText = sqlWordHits
			}
			pair[0] = &ftsJob{sql: sqlText, expr: t.Word, score: score}
			all = append(all, pair[0])
		}
		if t.Tri != "" {
			pair[1] = &ftsJob{sql: sqlTriIDs, expr: t.Tri}
			all = append(all, pair[1])
		}
		return pair
	}
	for _, t := range q.Positive() {
		pos = append(pos, add(t, true))
	}
	for _, t := range q.Negative() {
		neg = append(neg, add(t, false))
	}
	return pos, neg, all
}

func jobOut(j *ftsJob) hitList {
	if j == nil {
		return hitList{}
	}
	return j.out
}

// sqlCandidates: Datum der Kandidaten mit Konto- und Operator-Filtern.
const sqlCandidates = `SELECT m.id, ` + dateExpr + ` FROM json_each(?) j CROSS JOIN messages m INDEXED BY messages_search ON m.id = j.value`

// searchText führt eine Anfrage mit Texttermen aus (siehe oben).
func (s *SQLite) searchText(ctx context.Context, p SearchParams) (SearchResult, error) {
	res := SearchResult{Hits: []SearchHit{}}
	pos, neg, all := textJobs(p.Query)
	if err := s.runAll(ctx, all); err != nil {
		return res, err
	}
	var cand hitList
	for i, pair := range pos {
		h := unionHits(jobOut(pair[0]), jobOut(pair[1]).ids)
		if i == 0 {
			cand = h
		} else {
			cand = intersectHits(cand, h)
		}
		if len(cand.ids) == 0 {
			return res, nil
		}
	}
	for _, pair := range neg {
		cand = subtractIDs(cand, mergeIDs(jobOut(pair[0]).ids, jobOut(pair[1]).ids))
	}
	if len(cand.ids) == 0 {
		return res, nil
	}

	// Konto, Operatoren, Datum.
	var js strings.Builder
	js.Grow(len(cand.ids) * 7)
	js.WriteByte('[')
	for i, id := range cand.ids {
		if i > 0 {
			js.WriteByte(',')
		}
		js.WriteString(strconv.FormatInt(id, 10))
	}
	js.WriteByte(']')
	where, wargs := searchWhereOpt(p, false)
	rows, err := s.db.QueryContext(ctx, sqlCandidates+where, append([]any{js.String()}, wargs...)...)
	if err != nil {
		return res, fmt.Errorf("store: search filter: %w", err)
	}
	type scored struct {
		id    int64
		score float64
	}
	now := float64(p.Now.UnixMilli())
	var page []scored
	for rows.Next() {
		var id, date int64
		if err := rows.Scan(&id, &date); err != nil {
			_ = rows.Close()
			return res, err
		}
		k, ok := slices.BinarySearch(cand.ids, id)
		if !ok {
			continue
		}
		res.Total++
		sc := -cand.r[k] * (1.0 + 1.0/(1.0+max(0, now-float64(date))/float64(recencyMillis)))
		if a := p.After; a != nil && !(sc < a.Score || (sc == a.Score && id < a.ID)) {
			continue
		}
		page = append(page, scored{id, sc})
	}
	if err := rows.Close(); err != nil {
		return res, err
	}
	slices.SortFunc(page, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		case a.id > b.id:
			return -1
		case a.id < b.id:
			return 1
		}
		return 0
	})
	if len(page) > p.Limit {
		page = page[:p.Limit]
	}
	if len(page) == 0 {
		return res, nil
	}

	// Zeilen der Seite.
	ids := make([]any, len(page))
	for i, sc := range page {
		ids[i] = sc.id
	}
	byID, err := s.hitRows(ctx, ids)
	if err != nil {
		return res, err
	}
	for _, sc := range page {
		if h, ok := byID[sc.id]; ok {
			h.Score = sc.score
			res.Hits = append(res.Hits, h)
		}
	}
	return res, nil
}

// hitRows liest Kopf und Flags der Treffer einer Seite.
func (s *SQLite) hitRows(ctx context.Context, ids []any) (map[int64]SearchHit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.account_id, a.name, COALESCE(m.thread_id, 0), `+dateExpr+`,
		m.from_addr, m.subject, m.snippet, m.flag_seen, m.flag_flagged, m.flag_answered, m.flag_draft, m.has_attachments,
		m.deleted_remote_at IS NOT NULL
		FROM messages m JOIN accounts a ON a.id = m.account_id WHERE m.id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]SearchHit, len(ids))
	for rows.Next() {
		var h SearchHit
		var date int64
		var seen, flagged, answered, draft, att, deleted int
		if err := rows.Scan(&h.ID, &h.AccountID, &h.Account, &h.ThreadID, &date, &h.From, &h.Subject, &h.Snippet,
			&seen, &flagged, &answered, &draft, &att, &deleted); err != nil {
			return nil, err
		}
		if date > 0 {
			h.Date = time.UnixMilli(date).UTC()
		}
		h.Seen, h.Flagged, h.Answered, h.Draft = seen == 1, flagged == 1, answered == 1, draft == 1
		h.HasAttachments, h.DeletedRemote = att == 1, deleted == 1
		out[h.ID] = h
	}
	return out, rows.Err()
}

// textSQL beschreibt die Anweisungen des Textpfads (für --explain).
func textSQL(p SearchParams) string {
	_, _, all := textJobs(p.Query)
	var parts []string
	for _, j := range all {
		parts = append(parts, j.sql)
	}
	where, _ := searchWhereOpt(p, false)
	parts = append(parts, sqlCandidates+where, `SELECT … FROM messages m JOIN accounts a … WHERE m.id IN (page)`)
	return strings.Join(parts, ";\n")
}
