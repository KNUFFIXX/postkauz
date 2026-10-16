package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/search"
)

// Suche über beide FTS-Tabellen (docs/datenmodell.md, „Volltextsuche").
//
// Jeder positive Textterm liefert Treffer aus dem Wortindex (Wortstamm,
// Präfix oder Phrase; BM25 mit Spaltengewichten) und aus dem
// Trigram-Index (Teilwort; halbes Gewicht). Eine Nachricht passt, wenn
// jeder Term in einem der beiden Indizes trifft (UND); ihr Wert ist die
// Summe der besten Werte je Term (BM25 ist selbst eine Summe über Terme).
// Dazu kommt das Datum: Relevanz × (1 + 1 / (1 + Alter / 90 Tage)), eine
// neue Nachricht zählt also bis zu doppelt, eine 90 Tage alte 1,5-fach.
// Ohne Textterme wird nach Datum sortiert. Ausschluss (-wort) und
// Operatoren sind SQL-Bedingungen mit Parametern.

// bm25Weights: Betreff vor Absender vor Empfängern/Text vor Anhängen.
const bm25Weights = "3.0, 2.0, 1.0, 1.0, 0.5"

// trigramFactor gewichtet reine Teilwort-Treffer geringer.
const trigramFactor = 0.5

// recencyMillis ist die Zeitkonstante des Datumsanteils (90 Tage).
const recencyMillis = 90 * 24 * 3600 * 1000

// SearchParams beschreibt eine Suche.
type SearchParams struct {
	Query     search.Query
	AccountID int64 // 0 = alle Konten
	Limit     int
	After     *search.Cursor // nil = erste Seite
	Now       time.Time      // Bezugszeit des Datumsanteils (bei After dessen Now)
}

// SearchHit ist ein Treffer.
type SearchHit struct {
	ID             int64
	AccountID      int64
	Account        string
	ThreadID       int64
	Date           time.Time
	From           string
	Subject        string
	Snippet        string
	Seen           bool
	Flagged        bool
	Answered       bool
	Draft          bool
	HasAttachments bool
	DeletedRemote  bool
	// Score ist der Sortierwert: Relevanz mit Datumsanteil, bei reiner
	// Datumssortierung das Datum in Unix-Millisekunden.
	Score float64
}

// SearchResult ist eine Seite der Treffer.
type SearchResult struct {
	Hits []SearchHit
	// Total zählt alle Treffer der Anfrage (0, wenn die Seite leer ist).
	Total int
}

const dateExpr = `COALESCE(m.date_header, m.internal_date, 0)`

// Search führt eine übersetzte Anfrage aus.
func (s *SQLite) Search(ctx context.Context, p SearchParams) (SearchResult, error) {
	var res SearchResult
	sqlText, args := buildSearch(p)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return res, fmt.Errorf("store: search: %w", err)
	}
	defer rows.Close()
	res.Hits = []SearchHit{}
	for rows.Next() {
		var h SearchHit
		var date int64
		var seen, flagged, answered, draft, att, deleted int
		if err := rows.Scan(&h.ID, &h.Score, &res.Total, &h.AccountID, &h.Account, &h.ThreadID, &date, &h.From, &h.Subject,
			&h.Snippet, &seen, &flagged, &answered, &draft, &att, &deleted); err != nil {
			return res, err
		}
		if date > 0 {
			h.Date = time.UnixMilli(date).UTC()
		}
		h.Seen, h.Flagged, h.Answered, h.Draft = seen == 1, flagged == 1, answered == 1, draft == 1
		h.HasAttachments, h.DeletedRemote = att == 1, deleted == 1
		res.Hits = append(res.Hits, h)
	}
	return res, rows.Err()
}

// buildSearch setzt die SQL-Anweisung zusammen. Nutzertext steht nur in
// den Parametern (FTS5-Ausdrücke aus internal/search, Filterwerte).
func buildSearch(p SearchParams) (string, []any) {
	var with strings.Builder
	var withArgs, whereArgs []any
	where := []string{}
	if p.AccountID > 0 {
		where = append(where, `m.account_id = ?`)
		whereArgs = append(whereArgs, p.AccountID)
	}
	for _, f := range p.Query.Filters {
		cond, a := filterSQL(f)
		if f.Negated {
			cond = `NOT (` + cond + `)`
		}
		where = append(where, cond)
		whereArgs = append(whereArgs, a...)
	}
	for _, t := range p.Query.Negative() {
		var parts []string
		if t.Word != "" {
			parts = append(parts, `SELECT rowid FROM fts_messages WHERE fts_messages MATCH ?`)
			whereArgs = append(whereArgs, t.Word)
		}
		if t.Tri != "" {
			parts = append(parts, `SELECT rowid FROM fts_trigram WHERE fts_trigram MATCH ?`)
			whereArgs = append(whereArgs, t.Tri)
		}
		where = append(where, `m.id NOT IN (`+strings.Join(parts, ` UNION ALL `)+`)`)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = ` WHERE ` + strings.Join(where, ` AND `)
	}

	pos := p.Query.Positive()
	var rankedSQL string
	var rankedArgs []any
	if len(pos) > 0 {
		with.WriteString(`WITH hits(id, t, r) AS MATERIALIZED (`)
		first := true
		for i, t := range pos {
			if t.Word != "" {
				if !first {
					with.WriteString(` UNION ALL `)
				}
				fmt.Fprintf(&with, `SELECT rowid, %d, bm25(fts_messages, %s) FROM fts_messages WHERE fts_messages MATCH ?`, i, bm25Weights)
				withArgs = append(withArgs, t.Word)
				first = false
			}
			if t.Tri != "" {
				if !first {
					with.WriteString(` UNION ALL `)
				}
				fmt.Fprintf(&with, `SELECT rowid, %d, %g * bm25(fts_trigram, %s) FROM fts_trigram WHERE fts_trigram MATCH ?`,
					i, trigramFactor, bm25Weights)
				withArgs = append(withArgs, t.Tri)
				first = false
			}
		}
		fmt.Fprintf(&with, `), per(id, t, r) AS (SELECT id, t, min(r) FROM hits GROUP BY id, t),
			cand(id, r) AS (SELECT id, sum(r) FROM per GROUP BY id HAVING count(*) = %d), `, len(pos))
		rankedSQL = `ranked AS (SELECT m.id AS id,
			(-c.r) * (1.0 + 1.0 / (1.0 + max(0, ? - ` + dateExpr + `) / ` + fmt.Sprint(recencyMillis) + `.0)) AS score,
			count(*) OVER () AS total
			FROM cand c JOIN messages m ON m.id = c.id` + whereSQL + `)`
		rankedArgs = append([]any{p.Now.UnixMilli()}, whereArgs...)
	} else {
		with.WriteString(`WITH `)
		rankedSQL = `ranked AS (SELECT m.id AS id, CAST(` + dateExpr + ` AS REAL) AS score, count(*) OVER () AS total
			FROM messages m` + whereSQL + `)`
		rankedArgs = whereArgs
	}
	outer := ` SELECT r.id, r.score, r.total, m.account_id, a.name, COALESCE(m.thread_id, 0), ` + dateExpr + `,
		m.from_addr, m.subject, m.snippet, m.flag_seen, m.flag_flagged, m.flag_answered, m.flag_draft, m.has_attachments,
		m.deleted_remote_at IS NOT NULL
		FROM ranked r JOIN messages m ON m.id = r.id JOIN accounts a ON a.id = m.account_id`
	var outerArgs []any
	if p.After != nil {
		outer += ` WHERE (r.score < ? OR (r.score = ? AND r.id < ?))`
		outerArgs = append(outerArgs, p.After.Score, p.After.Score, p.After.ID)
	}
	outer += ` ORDER BY r.score DESC, r.id DESC LIMIT ?`
	outerArgs = append(outerArgs, p.Limit)

	args := append(append(withArgs, rankedArgs...), outerArgs...)
	return with.String() + rankedSQL + outer, args
}

// filterSQL übersetzt einen Operator in eine Bedingung über messages m.
func filterSQL(f search.Filter) (string, []any) {
	switch f.Kind {
	case "has_attachment":
		return `m.has_attachments = 1`, nil
	case "is":
		return map[string]string{"unread": `m.flag_seen = 0`, "read": `m.flag_seen = 1`, "flagged": `m.flag_flagged = 1`,
			"answered": `m.flag_answered = 1`, "draft": `m.flag_draft = 1`}[f.Value], nil
	case "mailbox":
		// Pfad (ohne Groß-/Kleinschreibung), Unterordner (Trenner / oder .)
		// oder Rolle (inbox, sent, archive …).
		esc := likeEscape(f.Value)
		return `EXISTS (SELECT 1 FROM message_mailboxes mm JOIN mailboxes b ON b.id = mm.mailbox_id
			WHERE mm.message_id = m.id AND (b.path = ? COLLATE NOCASE OR b.path LIKE ? ESCAPE '\'
			OR b.path LIKE ? ESCAPE '\' OR b.role = ?))`, []any{f.Value, esc + "/%", esc + ".%", strings.ToLower(f.Value)}
	case "thread":
		return `m.thread_id = ?`, []any{f.ID}
	case "tag":
		return `EXISTS (SELECT 1 FROM message_tags mt JOIN tags g ON g.id = mt.tag_id
			WHERE mt.message_id = m.id AND g.name = ? COLLATE NOCASE)`, []any{f.Value}
	case "after":
		return dateExpr + ` >= ?`, []any{f.Time.UnixMilli()}
	case "before":
		return dateExpr + ` < ?`, []any{f.Time.UnixMilli()}
	}
	return `0`, nil // unbekannt: nichts (der Parser lässt das nicht zu)
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchSQLForTest liefert die Anweisung einer Suche (Tests, --explain).
func SearchSQLForTest(p SearchParams) string {
	q, _ := buildSearch(p)
	return q
}
