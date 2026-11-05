package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Versanderkennung (S2.6, A02 E5): Abfragen über den Spiegel. Die Regeln
// (Message-ID, Rückfall-Abgleich) stehen in internal/drafts.
//
// Als gesendet zählt eine Nachricht des Kontos, die
//   - in einem Ordner mit Rolle sent oder all liegt,
//   - in keinem Ordner das Flag \Draft trägt und in keinem Entwürfe-Ordner
//     liegt (Rolle drafts oder ein Ordner, in den PostKauz Entwürfe abgelegt
//     hat; Befund K8: Gmail führt jede Entwurfskopie auch in „All Mail"),
//   - und noch keinem Entwurf als gesendete Nachricht zugeordnet ist
//     (drafts.sent_message_id).
//
// Dazu schließt die Versanderkennung Nachrichten aus, deren Inhalts-Hash
// der einer von PostKauz abgelegten Entwurfskopie ist (DraftCopyHashes,
// K3).

// sentOrAllCond ist die Bedingung oben ohne die letzte (Zuordnung zu einem
// Entwurf): in Gesendet bzw. „Alle Nachrichten“, nirgends \Draft, in
// keinem Entwürfe-Ordner. Für ersetzte Entwürfe (A06 Teil 4) zählt auch
// eine Nachricht, die schon einem anderen Entwurf als gesendet zugeordnet
// ist.
const sentOrAllCond = `EXISTS (SELECT 1 FROM message_mailboxes sl JOIN mailboxes sb ON sb.id = sl.mailbox_id
		WHERE sl.message_id = m.id AND sb.role IN ('sent','all'))
	AND NOT EXISTS (SELECT 1 FROM message_mailboxes dl JOIN mailboxes db ON db.id = dl.mailbox_id
		WHERE dl.message_id = m.id AND (dl.flag_draft = 1 OR db.role = 'drafts'
			OR db.id IN (SELECT remote_mailbox_id FROM drafts WHERE account_id = m.account_id AND remote_mailbox_id IS NOT NULL)))`

// sentMessageCond ist die Bedingung oben für die Nachricht m.
const sentMessageCond = sentOrAllCond + `
	AND NOT EXISTS (SELECT 1 FROM drafts cd WHERE cd.sent_message_id = m.id)`

// SentByMessageID liefert die kleinste ID einer gesendeten Nachricht (siehe
// oben) des Kontos mit dieser Message-ID (ohne <>), deren Inhalts-Hash
// nicht in exclude steht (Kopien, die PostKauz abgelegt hat; K3); ok false
// = keine.
func (s *SQLite) SentByMessageID(ctx context.Context, accountID int64, messageID string, exclude []string) (id int64, ok bool, err error) {
	if exclude == nil {
		exclude = []string{}
	}
	ex, err := json.Marshal(exclude)
	if err != nil {
		return 0, false, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT m.id FROM messages m WHERE m.account_id = ? AND m.message_id_header = ?
		AND COALESCE(m.content_hash, '') NOT IN (SELECT value FROM json_each(?))
		AND `+sentMessageCond+` ORDER BY m.id LIMIT 1`, accountID, messageID, string(ex)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// DraftCopyHashes liefert die Inhalts-Hashes aller Kopien, die PostKauz für
// Entwürfe des Kontos abgelegt hat oder gerade ablegt: die zuletzt
// abgelegte (pushed_content_hash), frühere (remote_previous, seit K3 mit
// Hash) und die eines Ablageversuchs mit offenem Ausgang (push_attempt).
// Eine Nachricht mit einem dieser Hashes ist eine Entwurfskopie, keine
// gesendete Nachricht, auch wenn der Spiegel sie (noch) nicht im
// Entwürfe-Ordner sieht, etwa nur in Gmail „Alle Nachrichten"
// (Gegenprüfung S2.6–S2.8, K3).
func (s *SQLite) DraftCopyHashes(ctx context.Context, accountID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pushed_content_hash FROM drafts
			WHERE account_id = ? AND COALESCE(pushed_content_hash, '') != ''
		UNION SELECT json_extract(push_attempt, '$.content_hash') FROM drafts
			WHERE account_id = ? AND push_attempt IS NOT NULL AND COALESCE(json_extract(push_attempt, '$.content_hash'), '') != ''
		UNION SELECT json_extract(p.value, '$.content_hash') FROM drafts d, json_each(d.remote_previous) p
			WHERE d.account_id = ? AND COALESCE(json_extract(p.value, '$.content_hash'), '') != ''`, accountID, accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SentMessage ist eine gesendete Nachricht als Kandidat des
// Rückfall-Abgleichs: Kopffelder ohne Text (den liest MessageBody nur für
// Kandidaten, deren Empfänger und Betreff passen).
type SentMessage struct {
	ID      int64
	Subject string
	// From wie messages.from_addr („Name <adresse>" oder nur die Adresse).
	From   string
	To, Cc []Addr
	// Date: Date-Kopfzeile, sonst INTERNALDATE.
	Date time.Time
	// InSent: Die Nachricht liegt in einem Ordner mit Rolle sent (sonst nur
	// in einem mit Rolle all).
	InSent bool
	// ContentHash wie messages.content_hash ("" = unbekannt).
	ContentHash string
}

// sentCandidateCols sind die Spalten eines Kandidaten (scanSentMessages).
const sentCandidateCols = `m.id, m.subject, m.from_addr, m.to_addrs, m.cc_addrs,
	COALESCE(m.date_header, m.internal_date, 0),
	EXISTS (SELECT 1 FROM message_mailboxes il JOIN mailboxes ib ON ib.id = il.mailbox_id
		WHERE il.message_id = m.id AND ib.role = 'sent'),
	COALESCE(m.content_hash, '')`

// SentCandidates liefert die gesendeten Nachrichten (siehe oben) des
// Kontos mit Datum (Date-Kopfzeile, sonst INTERNALDATE) zwischen from und
// to einschließlich, älteste zuerst. Die Auswahl nach Datum läuft über den
// Index messages_account_date (zwei Teilabfragen statt OR, sonst liest
// SQLite alle Nachrichten des Kontos). Seit der Gegenprüfung S2.6–S2.8
// (W2) nur noch für das Zeitfenster eines neuen oder geänderten Entwurfs;
// sonst prüft der Sync nur neue Nachrichten (NewSentCandidates).
func (s *SQLite) SentCandidates(ctx context.Context, accountID int64, from, to time.Time) ([]SentMessage, error) {
	f, t := from.UnixMilli(), to.UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT `+sentCandidateCols+`
		FROM messages m WHERE m.id IN (
			SELECT id FROM messages WHERE account_id = ? AND date_header BETWEEN ? AND ?
			UNION ALL
			SELECT id FROM messages WHERE account_id = ? AND date_header IS NULL AND internal_date BETWEEN ? AND ?)
		AND `+sentMessageCond+` ORDER BY COALESCE(m.date_header, m.internal_date, 0), m.id`, accountID, f, t, accountID, f, t)
	if err != nil {
		return nil, err
	}
	return scanSentMessages(rows)
}

// NewSentCandidates liefert die gesendeten Nachrichten (siehe oben) des
// Kontos mit after < messages.id <= upto, älteste zuerst (W2: nur, was seit
// der letzten Prüfung neu gespiegelt wurde; Auswahl über den Primärschlüssel).
func (s *SQLite) NewSentCandidates(ctx context.Context, accountID, after, upto int64) ([]SentMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sentCandidateCols+`
		FROM messages m WHERE m.id > ? AND m.id <= ? AND m.account_id = ?
		AND `+sentMessageCond+` ORDER BY COALESCE(m.date_header, m.internal_date, 0), m.id`, after, upto, accountID)
	if err != nil {
		return nil, err
	}
	return scanSentMessages(rows)
}

func scanSentMessages(rows *sql.Rows) ([]SentMessage, error) {
	defer rows.Close()
	var out []SentMessage
	for rows.Next() {
		var m SentMessage
		var to, cc string
		var date int64
		if err := rows.Scan(&m.ID, &m.Subject, &m.From, &to, &cc, &date, &m.InSent, &m.ContentHash); err != nil {
			return nil, err
		}
		// Unlesbare Adresslisten zählen als leer (der Abgleich passt dann nicht).
		_ = json.Unmarshal([]byte(to), &m.To)
		_ = json.Unmarshal([]byte(cc), &m.Cc)
		m.Date = time.UnixMilli(date).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// trimIDChars sind die Zeichen, die beim Vergleich einer Message-ID mit
// In-Reply-To wegfallen: Spitzklammern und Leerraum (A05 Teil 2).
const trimIDChars = `' <>' || char(9,10,13)`

// SentReplies liefert die gesendeten Nachrichten (siehe oben) des Kontos,
// deren In-Reply-To die Message-ID der Nachricht original ist (ohne <> und
// Leerraum verglichen), älteste zuerst (A05 Teil 2, Weg über den Verlauf).
// Gesucht wird ohne eigenen Index über thread_refs (Konto, Message-ID des
// Originals) → Thread → Index messages_thread, gefiltert auf In-Reply-To;
// gelesen werden also nur Nachrichten aus dem Verlauf des Originals. Hat
// das Original keine Message-ID oder fehlt es, ist die Liste leer.
func (s *SQLite) SentReplies(ctx context.Context, accountID, original int64) ([]SentMessage, error) {
	return s.replies(ctx, accountID, original, sentMessageCond)
}

// OwnReplies liefert wie SentReplies die eigenen Antworten auf original,
// aber auch solche, die schon einem Entwurf als gesendet zugeordnet sind
// (A06 Teil 4: Eine gesendete Antwort ersetzt auch die übrigen Fassungen
// desselben Entwurfs).
func (s *SQLite) OwnReplies(ctx context.Context, accountID, original int64) ([]SentMessage, error) {
	return s.replies(ctx, accountID, original, sentOrAllCond)
}

func (s *SQLite) replies(ctx context.Context, accountID, original int64, cond string) ([]SentMessage, error) {
	// Zwei Schritte mit gebundenem Wert: Eine Verknüpfung mit der
	// Message-ID des Originals in derselben Abfrage ließ SQLite alle
	// Nachrichten des Kontos lesen (BenchmarkCheckAfterSync22k,
	// steady_replies: 1,8 s je Lauf).
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT trim(COALESCE(message_id_header, ''), `+trimIDChars+`) FROM messages
		WHERE id = ? AND account_id = ?`, original, accountID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && h == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// CROSS JOIN legt die Reihenfolge fest: erst der Thread über
	// thread_refs, dann seine Nachrichten über messages_thread (sonst nahm
	// SQLite den Index über das Konto und las alle seine Nachrichten).
	rows, err := s.db.QueryContext(ctx, `SELECT `+sentCandidateCols+` FROM thread_refs r CROSS JOIN messages m ON m.thread_id = r.thread_id
		WHERE r.account_id = ? AND r.ref_id = ? AND m.account_id = r.account_id
		AND trim(COALESCE(m.in_reply_to, ''), `+trimIDChars+`) = ?
		AND `+cond+` ORDER BY COALESCE(m.date_header, m.internal_date, 0), m.id`, accountID, h, h)
	if err != nil {
		return nil, err
	}
	return scanSentMessages(rows)
}

// MaxMessageID liefert die höchste messages.id (aller Konten; 0 = keine).
// Nachrichten werden nie gelöscht, die IDs steigen: Alles bis zu dieser ID
// ist gespiegelt (W2, Hochwassermarke).
func (s *SQLite) MaxMessageID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(max(id), 0) FROM messages`).Scan(&id)
	return id, err
}

// SentCheckMark liefert die Hochwassermarke der Versanderkennung eines
// Kontos (sent_check_marks; Migration 0016, W2): die höchste messages.id,
// die eine vollständige Prüfung gesehen hat. ok false = noch keine.
func (s *SQLite) SentCheckMark(ctx context.Context, accountID int64) (last int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT last_message_id FROM sent_check_marks WHERE account_id = ?`, accountID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return last, err == nil, err
}

// SetSentCheckMark setzt die Hochwassermarke eines Kontos; sie sinkt nie.
func (s *SQLite) SetSentCheckMark(ctx context.Context, accountID, last int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sent_check_marks(account_id, last_message_id) VALUES (?, ?)
		ON CONFLICT(account_id) DO UPDATE SET last_message_id = max(last_message_id, excluded.last_message_id)`, accountID, last)
	return err
}

// SetFallbackChecked vermerkt, dass das ganze Zeitfenster eines Entwurfs
// im Stand contentChangedAt geprüft ist (drafts.fallback_checked_at; W2).
// Hat sich der Inhalt inzwischen geändert (content_changed_at weicht ab),
// bleibt der Vermerk aus: Die nächste Prüfung prüft das neue Fenster.
func (s *SQLite) SetFallbackChecked(ctx context.Context, draftID int64, contentChangedAt time.Time) error {
	ms := contentChangedAt.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE drafts SET fallback_checked_at = ? WHERE id = ? AND content_changed_at = ?`, ms, draftID, ms)
	return err
}

// sentCheckRevKey ist der Schlüssel in meta, unter dem steht, nach welchem
// Stand der Regeln die offenen Entwürfe eines Kontos zuletzt vollständig
// im Rückfall geprüft wurden (A05 Teil 1, ohne Migration).
func sentCheckRevKey(accountID int64) string { return fmt.Sprintf("sent_check_rev/%d", accountID) }

// SentCheckRev liefert den Stand der Regeln, nach dem der Rückfall die
// offenen Entwürfe des Kontos zuletzt vollständig geprüft hat (0 = nie,
// also vor A05).
func (s *SQLite) SentCheckRev(ctx context.Context, accountID int64) (int, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, sentCheckRevKey(accountID)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(v) // unlesbar: 0, die nächste Prüfung prüft neu
	return n, nil
}

// SetSentCheckRev vermerkt den Stand der Regeln nach einer vollständigen
// Prüfung.
func (s *SQLite) SetSentCheckRev(ctx context.Context, accountID int64, rev int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, sentCheckRevKey(accountID), strconv.Itoa(rev))
	return err
}

// DraftReplacedBy liefert die lokale ID der eigenen Antwort, durch die die
// Versanderkennung den Entwurf draftID als ersetzt geschlossen hat (A06
// Teil 4), aus dem Audit (drafts.replaced_detected), ohne Migration. Das
// Audit-Ereignis trägt denselben Zeitpunkt wie closed_at; gesucht wird über
// den Index audit_events_ts in ±1 s. ok false: keins (anders geschlossen
// oder schon aus dem Audit rotiert).
func (s *SQLite) DraftReplacedBy(ctx context.Context, draftID int64, closedAt time.Time) (replyID int64, ok bool, err error) {
	if closedAt.IsZero() {
		return 0, false, nil
	}
	var params string
	err = s.db.QueryRowContext(ctx, `SELECT params_redacted FROM audit_events WHERE ts BETWEEN ? AND ?
		AND operation = 'drafts.replaced_detected' AND result = 'ok'
		AND EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(audit_events.targets) THEN audit_events.targets ELSE '[]' END)
		WHERE value = ?) ORDER BY id DESC LIMIT 1`,
		closedAt.UnixMilli()-1000, closedAt.UnixMilli()+1000, "draft:"+strconv.FormatInt(draftID, 10)).Scan(&params)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var p struct {
		Reply int64 `json:"reply_message_id"`
	}
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return 0, false, fmt.Errorf("store: audit params of a replaced draft: %w", err)
	}
	return p.Reply, true, nil
}

// EditedReplyInDrafts meldet, ob im Entwürfe-Ordner eine Nachricht des
// Kontos liegt, die nach after datiert ist, deren In-Reply-To die
// Message-ID des Originals original ist und die keine von PostKauz
// abgelegte Kopie ist (Message-ID keines Entwurfs des Kontos): Der Mensch
// bearbeitet die Antwort dann in Gmail unter neuer Message-ID weiter (A06
// Nachbesserung 1, Sicherung c für ersetzte Entwürfe). „Im Entwürfe-Ordner“
// heißt hier vorsichtig: in irgendeinem Ordner mit \Draft, mit Rolle
// drafts oder im Ablageordner eines Entwurfs. Gesucht wird wie bei
// SentReplies nur im Verlauf des Originals (thread_refs → messages_thread);
// Gmail führt eine weiterbearbeitete Antwort dort, weil sie References auf
// das Original trägt.
func (x *DraftTx) EditedReplyInDrafts(accountID, original int64, after time.Time) (bool, error) {
	var h string
	err := x.tx.QueryRowContext(x.ctx, `SELECT trim(COALESCE(message_id_header, ''), `+trimIDChars+`) FROM messages
		WHERE id = ? AND account_id = ?`, original, accountID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && h == "") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Wie replies(): erst der Thread des Originals über thread_refs, dann
	// seine Nachrichten über messages_thread (Nachbesserung 2; vorher alle
	// Nachrichten des Kontos).
	var found bool
	err = x.tx.QueryRowContext(x.ctx, `SELECT EXISTS (SELECT 1 FROM thread_refs r CROSS JOIN messages m ON m.thread_id = r.thread_id
		WHERE r.account_id = ? AND r.ref_id = ? AND m.account_id = r.account_id
		AND trim(COALESCE(m.in_reply_to, ''), `+trimIDChars+`) = ?
		AND COALESCE(m.date_header, m.internal_date, 0) > ?
		AND EXISTS (SELECT 1 FROM message_mailboxes dl JOIN mailboxes db ON db.id = dl.mailbox_id
			WHERE dl.message_id = m.id AND (dl.flag_draft = 1 OR db.role = 'drafts'
				OR db.id IN (SELECT remote_mailbox_id FROM drafts WHERE account_id = m.account_id AND remote_mailbox_id IS NOT NULL)))
		AND NOT EXISTS (SELECT 1 FROM drafts od WHERE od.account_id = m.account_id AND od.message_id_header = m.message_id_header))`,
		accountID, h, h, after.UnixMilli()).Scan(&found)
	return found, err
}
