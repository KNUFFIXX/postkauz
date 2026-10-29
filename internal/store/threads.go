package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/thread"
)

// Threads (S1.6): Die Zuordnung entscheidet internal/thread (Assign); hier
// steht der Index in SQLite (threads, thread_refs) und die Pflege der
// Kennzahlen. Alles läuft in der Transaktion, die die Nachricht schreibt.

// ErrThreadNotFound: Thread unbekannt.
var ErrThreadNotFound = errors.New("store: thread not found")

type querier interface {
	execQuerier
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txIndex ist thread.Index für ein Konto innerhalb einer Transaktion.
type txIndex struct {
	ctx     context.Context
	q       querier
	account int64
	msgID   int64    // Nachricht, die Add gerade zuordnet
	self    []string // eigene Adressen des Kontos (E-Mail, Benutzername)
}

var _ thread.Index = (*txIndex)(nil)

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (x *txIndex) Lookup(ids []string) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{x.account}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := x.q.QueryContext(x.ctx, `SELECT DISTINCT thread_id FROM thread_refs WHERE account_id = ? AND ref_id IN (`+
		placeholders(len(ids))+`) ORDER BY thread_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanInfo(sc interface{ Scan(...any) error }) (thread.Info, error) {
	var i thread.Info
	var first, last sql.NullInt64
	var orphan int
	if err := sc.Scan(&i.ID, &first, &last, &orphan); err != nil {
		return i, err
	}
	if first.Valid {
		i.First = time.UnixMilli(first.Int64).UTC()
	}
	if last.Valid {
		i.Last = time.UnixMilli(last.Int64).UTC()
	}
	i.OrphanRoot = orphan == 1
	return i, nil
}

func (x *txIndex) Candidates(norm string) ([]thread.Info, error) {
	rows, err := x.q.QueryContext(x.ctx, `SELECT id, first_date, last_date, root_orphan FROM threads
		WHERE account_id = ? AND subject_norm = ? ORDER BY id`, x.account, norm)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []thread.Info
	for rows.Next() {
		i, err := scanInfo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (x *txIndex) Info(id int64) (thread.Info, error) {
	return scanInfo(x.q.QueryRowContext(x.ctx, `SELECT id, first_date, last_date, root_orphan FROM threads WHERE id = ?`, id))
}

// Participants liefert die Adressen aus From, To und Cc aller Nachrichten
// des Threads ohne die eigenen des Kontos.
func (x *txIndex) Participants(id int64) ([]string, error) {
	rows, err := x.q.QueryContext(x.ctx, `SELECT from_addr, to_addrs, cc_addrs FROM messages WHERE thread_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var from, to, cc string
		if err := rows.Scan(&from, &to, &cc); err != nil {
			return nil, err
		}
		for _, p := range participants(from, json.RawMessage(to), json.RawMessage(cc), x.self) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out, rows.Err()
}

// participants: Adressen aus From, To und Cc (thread.NormalizeAddress)
// ohne Dubletten und ohne self.
func participants(from string, to, cc json.RawMessage, self []string) []string {
	var out []string
	add := func(s string) {
		if a := thread.NormalizeAddress(s); a != "" && !isSelf(self, a) && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	add(from)
	for _, raw := range []json.RawMessage{to, cc} {
		var as []struct {
			Address string `json:"address"`
		}
		_ = json.Unmarshal(raw, &as)
		for _, a := range as {
			add(a.Address)
		}
	}
	return out
}

// isSelf meldet, ob a eine der eigenen Adressen self ist, nach
// mailaddr.Key (Gmail: Punkte im lokalen Teil, Zusatz ab „+“,
// googlemail.com; Nachbesserung T3, Punkt 15).
func isSelf(self []string, a string) bool {
	k := mailaddr.Key(a)
	return slices.ContainsFunc(self, func(s string) bool { return mailaddr.Key(s) == k })
}

// selfAddresses liefert die eigenen Adressen des Kontos: E-Mail,
// Benutzername (wenn er eine Adresse ist), die Aliasse (seit S2.0/A6) und
// seit S2.1 die Adressen und Reply-To der Identitäten (A02, E9).
func selfAddresses(ctx context.Context, q querier, account int64) ([]string, error) {
	var email, user, aliases, idents string
	err := q.QueryRowContext(ctx, `SELECT email, username, aliases,
		(SELECT json_group_array(x) FROM (SELECT email AS x FROM identities WHERE account_id = a.id
		 UNION ALL SELECT reply_to FROM identities WHERE account_id = a.id AND reply_to IS NOT NULL))
		FROM accounts a WHERE id = ?`, account).Scan(&email, &user, &aliases, &idents)
	if err != nil {
		return nil, fmt.Errorf("store: account %d: %w", account, err)
	}
	var more, fromIdents []string
	if err := json.Unmarshal([]byte(aliases), &more); err != nil {
		return nil, fmt.Errorf("store: account %d: aliases are corrupt", account)
	}
	if err := json.Unmarshal([]byte(idents), &fromIdents); err != nil {
		return nil, fmt.Errorf("store: account %d: identities: %w", account, err)
	}
	var out []string
	for _, s := range append(append([]string{email, user}, more...), fromIdents...) {
		if a := thread.NormalizeAddress(s); a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (x *txIndex) Create(norm string) (int64, error) {
	var id int64
	// ID oberhalb des Höchststands (A14, store/ids.go): Threads werden beim
	// Zusammenführen und von purge gelöscht.
	err := x.q.QueryRowContext(x.ctx, `INSERT INTO threads(id, account_id, subject_norm) VALUES (`+nextThreadID+`, ?, ?) RETURNING id`,
		x.account, norm).Scan(&id)
	return id, err
}

func (x *txIndex) Merge(into int64, from []int64) error {
	for _, f := range from {
		if f == into {
			continue
		}
		for _, q := range []string{
			`UPDATE thread_refs SET thread_id = ? WHERE thread_id = ?`,
			`UPDATE messages SET thread_id = ? WHERE thread_id = ?`,
		} {
			if _, err := x.q.ExecContext(x.ctx, q, into, f); err != nil {
				return fmt.Errorf("store: merge thread %d into %d: %w", f, into, err)
			}
		}
		if _, err := x.q.ExecContext(x.ctx, `DELETE FROM threads WHERE id = ?`, f); err != nil {
			return err
		}
	}
	return refreshThread(x.ctx, x.q, into)
}

func (x *txIndex) Add(t int64, ids []string, _ thread.Message) error {
	for _, id := range ids {
		if _, err := x.q.ExecContext(x.ctx, `INSERT INTO thread_refs(account_id, ref_id, thread_id) VALUES (?,?,?)
			ON CONFLICT(account_id, ref_id) DO NOTHING`, x.account, id, t); err != nil {
			return fmt.Errorf("store: thread ref: %w", err)
		}
	}
	if _, err := x.q.ExecContext(x.ctx, `UPDATE messages SET thread_id = ? WHERE id = ?`, t, x.msgID); err != nil {
		return err
	}
	return refreshThread(x.ctx, x.q, t)
}

// msgDate ist das Datum einer Nachricht in SQL (Kopfzeile, sonst Server).
const msgDate = `COALESCE(date_header, internal_date)`

// refreshThread berechnet Anzahl, Zeitspanne, Teilnehmer, Betreff und
// Waisen-Wurzel eines Threads aus seinen Nachrichten.
func refreshThread(ctx context.Context, q querier, id int64) error {
	if _, err := q.ExecContext(ctx, `UPDATE threads SET
		message_count = (SELECT count(*) FROM messages WHERE thread_id = ?1),
		first_date = (SELECT min(`+msgDate+`) FROM messages WHERE thread_id = ?1),
		last_date  = (SELECT max(`+msgDate+`) FROM messages WHERE thread_id = ?1),
		participants = COALESCE((SELECT json_group_array(from_addr) FROM (SELECT DISTINCT from_addr FROM messages
			WHERE thread_id = ?1 AND from_addr != '' ORDER BY from_addr LIMIT 20)), '[]')
		WHERE id = ?1`, id); err != nil {
		return fmt.Errorf("store: refresh thread %d: %w", id, err)
	}
	var subject, refs string
	var irt sql.NullString
	err := q.QueryRowContext(ctx, `SELECT subject, refs, in_reply_to FROM messages
		WHERE thread_id = ? AND `+msgDate+` IS NOT NULL ORDER BY `+msgDate+`, id LIMIT 1`, id).Scan(&subject, &refs, &irt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // keine Nachricht mit Datum: Betreff bleibt
	}
	if err != nil {
		return err
	}
	root := thread.Message{Subject: subject}
	_ = json.Unmarshal([]byte(refs), &root.References)
	if irt.Valid {
		root.InReplyTo = []string{irt.String}
	}
	norm, _ := thread.NormalizeSubject(subject)
	_, err = q.ExecContext(ctx, `UPDATE threads SET subject_norm = ?, root_orphan = ? WHERE id = ?`, norm, b2i(root.Orphan()), id)
	return err
}

// threadMessage baut die Sicht des Threadings auf eine neue Nachricht.
func threadMessage(n NewMessage) thread.Message {
	m := thread.Message{MessageID: n.MessageID, References: n.References, Subject: n.Subject, Date: n.DateHeader,
		Participants: participants(n.From, n.To, n.Cc, nil)}
	if n.InReplyTo != "" {
		m.InReplyTo = []string{n.InReplyTo}
	}
	if m.Date.IsZero() {
		m.Date = n.InternalDate
	}
	return m
}

// assignThread ordnet die Nachricht mid einem Thread zu.
// Die eigenen Adressen des Kontos fallen aus den Beteiligten heraus (sie
// stehen in fast jeder Nachricht und verbänden sonst alles).
func assignThread(ctx context.Context, q querier, account, mid int64, m thread.Message) error {
	self, err := selfAddresses(ctx, q, account)
	if err != nil {
		return err
	}
	m.Participants = slices.DeleteFunc(slices.Clone(m.Participants), func(a string) bool { return isSelf(self, a) })
	ix := &txIndex{ctx: ctx, q: q, account: account, msgID: mid, self: self}
	_, err = thread.Assign(ix, m)
	return err
}

// ThreadUnassigned ordnet bis zu limit Nachrichten des Kontos ohne Thread
// zu (Bestand von vor S1.6), nach Datum, in einer Transaktion. Liefert die
// Zahl der zugeordneten Nachrichten (0 = nichts mehr zu tun).
func (s *SQLite) ThreadUnassigned(ctx context.Context, accountID int64, limit int) (n int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(message_id_header, ''), subject, refs, COALESCE(in_reply_to, ''),
		date_header, internal_date, from_addr, to_addrs, cc_addrs FROM messages WHERE account_id = ? AND thread_id IS NULL
		ORDER BY `+msgDate+`, id LIMIT ?`, accountID, limit)
	if err != nil {
		return 0, err
	}
	type item struct {
		id int64
		m  thread.Message
	}
	var items []item
	for rows.Next() {
		var it item
		var refs, irt, from, to, cc string
		var dh, idate sql.NullInt64
		if err := rows.Scan(&it.id, &it.m.MessageID, &it.m.Subject, &refs, &irt, &dh, &idate, &from, &to, &cc); err != nil {
			_ = rows.Close()
			return 0, err
		}
		_ = json.Unmarshal([]byte(refs), &it.m.References)
		it.m.Participants = participants(from, json.RawMessage(to), json.RawMessage(cc), nil)
		if irt != "" {
			it.m.InReplyTo = []string{irt}
		}
		switch {
		case dh.Valid:
			it.m.Date = time.UnixMilli(dh.Int64).UTC()
		case idate.Valid:
			it.m.Date = time.UnixMilli(idate.Int64).UTC()
		}
		items = append(items, it)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, it := range items {
		if err := assignThread(ctx, tx, accountID, it.id, it.m); err != nil {
			return 0, err
		}
	}
	return len(items), tx.Commit()
}

// Thread ist ein Thread mit seinen Kennzahlen.
type Thread struct {
	ID           int64
	AccountID    int64
	Account      string
	SubjectNorm  string
	FirstDate    time.Time
	LastDate     time.Time
	MessageCount int
	Participants []string
}

// ThreadMessage ist eine Nachricht in der Sicht eines Threads.
type ThreadMessage struct {
	ID             int64
	Date           time.Time
	From           string
	Subject        string
	Seen           bool
	Flagged        bool
	HasAttachments bool
	Snippet        string
	DeletedRemote  bool
	RemoteStatus   string // present | imported_only | deleted_remote (A4)
}

// ThreadByID liefert einen Thread und seine Nachrichten nach Datum.
func (s *SQLite) ThreadByID(ctx context.Context, id int64) (Thread, []ThreadMessage, error) {
	var t Thread
	var first, last sql.NullInt64
	var parts string
	err := s.db.QueryRowContext(ctx, `SELECT t.id, t.account_id, a.name, t.subject_norm, t.first_date, t.last_date,
		t.message_count, t.participants FROM threads t JOIN accounts a ON a.id = t.account_id WHERE t.id = ?`, id).Scan(
		&t.ID, &t.AccountID, &t.Account, &t.SubjectNorm, &first, &last, &t.MessageCount, &parts)
	if errors.Is(err, sql.ErrNoRows) {
		return t, nil, fmt.Errorf("%w: id %d", ErrThreadNotFound, id)
	}
	if err != nil {
		return t, nil, err
	}
	if first.Valid {
		t.FirstDate = time.UnixMilli(first.Int64).UTC()
	}
	if last.Valid {
		t.LastDate = time.UnixMilli(last.Int64).UTC()
	}
	_ = json.Unmarshal([]byte(parts), &t.Participants)
	rows, err := s.db.QueryContext(ctx, `SELECT id, `+msgDate+`, from_addr, subject, flag_seen, flag_flagged, has_attachments,
		snippet, deleted_remote_at, `+remoteStatusExpr+` FROM messages m WHERE thread_id = ? ORDER BY `+msgDate+`, id`, id)
	if err != nil {
		return t, nil, err
	}
	defer rows.Close()
	var out []ThreadMessage
	for rows.Next() {
		var m ThreadMessage
		var d, del sql.NullInt64
		var seen, flagged, att int
		if err := rows.Scan(&m.ID, &d, &m.From, &m.Subject, &seen, &flagged, &att, &m.Snippet, &del, &m.RemoteStatus); err != nil {
			return t, nil, err
		}
		if d.Valid {
			m.Date = time.UnixMilli(d.Int64).UTC()
		}
		m.Seen, m.Flagged, m.HasAttachments, m.DeletedRemote = seen == 1, flagged == 1, att == 1, del.Valid
		out = append(out, m)
	}
	return t, out, rows.Err()
}
