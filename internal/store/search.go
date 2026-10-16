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
// Trigram-Index (Teilwort). Eine Nachricht passt, wenn jeder Term in einem
// der beiden Indizes trifft (UND); ihr Wert ist die Summe der Werte je
// Term (BM25 ist selbst eine Summe über Terme; Ausführung in
// search_text.go). Ein reiner
// Teilwort-Treffer zählt mit einem sehr kleinen festen Wert: Er liegt
// immer hinter Wort-Treffern, untereinander entscheidet das Datum (BM25
// über den Trigram-Index wäre teuer und sagt über Teilwörter wenig).
// Dazu kommt das Datum: Relevanz × (1 + 1 / (1 + Alter / 90 Tage)), eine
// neue Nachricht zählt also bis zu doppelt, eine 90 Tage alte 1,5-fach.
// Ohne Textterme wird nach Datum sortiert (Lauf über den Index
// messages_sortdate). Ausschluss (-wort) und Operatoren sind
// SQL-Bedingungen mit Parametern.

// bm25Weights: Betreff vor Absender vor Empfängern/Text vor Anhängen.
const bm25Weights = "3.0, 2.0, 1.0, 1.0, 0.5"

// substringScore ist der Wert eines reinen Teilwort-Treffers (BM25-Werte
// sind negativ, kleiner ist besser; FTS5 setzt die IDF sehr häufiger Wörter
// auf 1e-6, dieser Wert liegt darunter).
const substringScore = -1e-9

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
	// Total zählt alle Treffer der Anfrage (bei Relevanzsortierung 0, wenn
	// die Seite leer ist).
	Total int
}

const dateExpr = `COALESCE(m.date_header, m.internal_date, 0)`

// Search führt eine übersetzte Anfrage aus.
func (s *SQLite) Search(ctx context.Context, p SearchParams) (SearchResult, error) {
	if len(p.Query.Positive()) > 0 {
		return s.searchText(ctx, p)
	}
	res := SearchResult{Hits: []SearchHit{}}
	sqlText, args := buildSearch(p)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return res, fmt.Errorf("store: search: %w", err)
	}
	defer rows.Close()
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
	if err := rows.Err(); err != nil {
		return res, err
	}
	if len(p.Query.Positive()) == 0 {
		// Datumssortierung: Die Seite kommt aus dem Indexlauf; gezählt
		// wird getrennt (über die schmalen Indizes).
		where, wargs := searchWhere(p)
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages m`+where, wargs...).Scan(&res.Total); err != nil {
			return res, fmt.Errorf("store: search count: %w", err)
		}
	}
	return res, nil
}

// searchWhere baut die Bedingungen aus Konto, Operatoren und Ausschluss
// (über messages m).
func searchWhere(p SearchParams) (string, []any) {
	return searchWhereOpt(p, true)
}

func searchWhereOpt(p SearchParams, negatives bool) (string, []any) {
	var where []string
	var args []any
	if p.AccountID > 0 {
		where = append(where, `m.account_id = ?`)
		args = append(args, p.AccountID)
	}
	for _, f := range p.Query.Filters {
		cond, a := filterSQL(f)
		if f.Negated {
			cond = `NOT (` + cond + `)`
		}
		where = append(where, cond)
		args = append(args, a...)
	}
	for _, t := range p.Query.Negative() {
		if !negatives {
			break
		}
		var parts []string
		if t.Word != "" {
			parts = append(parts, `SELECT rowid FROM fts_messages WHERE fts_messages MATCH ?`)
			args = append(args, t.Word)
		}
		if t.Tri != "" {
			parts = append(parts, `SELECT rowid FROM fts_trigram WHERE fts_trigram MATCH ?`)
			args = append(args, t.Tri)
		}
		where = append(where, `m.id NOT IN (`+strings.Join(parts, ` UNION ALL `)+`)`)
	}
	if len(where) == 0 {
		return "", nil
	}
	return ` WHERE ` + strings.Join(where, ` AND `), args
}

// pageSQL holt die Zeilen einer Seite (ranked: id, score, total).
const pageSQL = ` SELECT r.id, r.score, r.total, m.account_id, a.name, COALESCE(m.thread_id, 0), ` + dateExpr + `,
	m.from_addr, m.subject, m.snippet, m.flag_seen, m.flag_flagged, m.flag_answered, m.flag_draft, m.has_attachments,
	m.deleted_remote_at IS NOT NULL
	FROM ranked r JOIN messages m ON m.id = r.id JOIN accounts a ON a.id = m.account_id
	ORDER BY r.score DESC, r.id DESC`

// buildSearch setzt die Anweisung für Anfragen nur mit Operatoren
// zusammen (neueste zuerst; Lauf über messages_sortdate bis zur
// Seitengröße). Anfragen mit Texttermen: search_text.go.
func buildSearch(p SearchParams) (string, []any) {
	cond, args := searchWhere(p)
	if p.After != nil {
		c := `(` + dateExpr + `, m.id) < (?, ?)`
		if cond == "" {
			cond = ` WHERE ` + c
		} else {
			cond += ` AND ` + c
		}
		args = append(args, int64(p.After.Score), p.After.ID)
	}
	args = append(args, p.Limit)
	return `WITH ranked AS (SELECT m.id AS id, CAST(` + dateExpr + ` AS REAL) AS score, 0 AS total
		FROM messages m INDEXED BY messages_sortdate` + cond + `
		ORDER BY ` + dateExpr + ` DESC, m.id DESC LIMIT ?)` + pageSQL, args
}

// filterSQL übersetzt einen Operator in eine Bedingung über messages m.
// Ordner und Tags sind Mengen (IN mit Unterabfrage): SQLite baut sie
// einmal auf, statt je Nachricht nachzusehen.
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
		return `m.id IN (SELECT mm.message_id FROM mailboxes b JOIN message_mailboxes mm ON mm.mailbox_id = b.id
			WHERE b.path = ? COLLATE NOCASE OR b.path LIKE ? ESCAPE '\' OR b.path LIKE ? ESCAPE '\' OR b.role = ?)`,
			[]any{f.Value, esc + "/%", esc + ".%", strings.ToLower(f.Value)}
	case "thread":
		return `m.thread_id = ?`, []any{f.ID}
	case "tag":
		return `m.id IN (SELECT mt.message_id FROM tags g JOIN message_tags mt ON mt.tag_id = g.id
			WHERE g.name = ? COLLATE NOCASE)`, []any{f.Value}
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

// SearchSQL liefert die Anweisungen einer Suche mit Platzhaltern statt
// Werten (--explain); Nutzertext steht nur in den Parametern.
func SearchSQL(p SearchParams) string {
	if len(p.Query.Positive()) > 0 {
		return textSQL(p)
	}
	q, _ := buildSearch(p)
	return q
}
