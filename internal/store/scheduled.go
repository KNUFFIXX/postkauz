package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// Geplantes Senden (A10 Teil 2; docs/versanderkennung.md, Abschnitt
// „Geplantes Senden“). Befund B1: Plant der Mensch einen Entwurf in Gmail,
// verschwindet die Kopie aus den Entwürfen, und Gmail legt eine neue
// Nachricht an, die nur in „Alle Nachrichten“ liegt, kein \Draft trägt und
// den künftigen Termin als Date und INTERNALDATE hat.
//
// Eine Nachricht ist geplant, solange sie
//
//	(a) in einem Ordner mit Rolle all liegt und in keinem mit Rolle inbox,
//	    sent, drafts, trash oder junk und in keinem Entwürfe-Ordner im Sinn
//	    von sentOrAllCond (virtuelle Sichten zählen nie),
//	(b) ihr INTERNALDATE mehr als 2 Minuten nach der aktuellen Zeit liegt,
//	(c) ihr Absender eine eigene Adresse ist (prüft internal/drafts mit
//	    OwnAddresses, wie im Rückfall) und
//	(d) sie nirgends \Draft trägt.
//
// Abgeleitet nur aus dem Spiegel (kein Zugriff auf den Server, keine
// Gmail-Erweiterung, kein Schema-Upgrade). Der Termin ist INTERNALDATE.

// scheduledPlaceCond ist (a) und (d) für die Nachricht m.
const scheduledPlaceCond = `EXISTS (SELECT 1 FROM message_mailboxes al JOIN mailboxes ab ON ab.id = al.mailbox_id
		WHERE al.message_id = m.id AND ab.role = 'all' AND ab.virtual_view = 0)
	AND NOT EXISTS (SELECT 1 FROM message_mailboxes ol JOIN mailboxes ob ON ob.id = ol.mailbox_id
		WHERE ol.message_id = m.id AND ob.virtual_view = 0 AND ob.role IN ('inbox','sent','drafts','trash','junk'))
	AND NOT EXISTS (SELECT 1 FROM message_mailboxes dl JOIN mailboxes db ON db.id = dl.mailbox_id
		WHERE dl.message_id = m.id AND (dl.flag_draft = 1 OR db.role = 'drafts'
			OR db.id IN (SELECT remote_mailbox_id FROM drafts WHERE account_id = m.account_id AND remote_mailbox_id IS NOT NULL)))`

// ScheduledMessage ist eine geplante Nachricht (Bedingungen (a), (b), (d);
// (c) prüft der Aufrufer).
type ScheduledMessage struct {
	SentMessage
	// MessageIDHeader: Message-ID ohne <> ("" = keine).
	MessageIDHeader string
	// CreatedAt: Zeitpunkt der Spiegelung (messages.created_at).
	CreatedAt time.Time
}

// ScheduledMessages liefert die Nachrichten des Kontos mit den Bedingungen
// (a), (b) und (d) oben, INTERNALDATE nach notBefore (Zeit des Laufs + 2
// Minuten), nach Termin sortiert. Vorgefiltert über den Index
// messages_account_date: Date-Kopfzeile nach notBefore oder keine
// Date-Kopfzeile (Gmail setzt beide auf den Termin, B1). Eine Nachricht
// mit vergangenem Date und künftigem INTERNALDATE findet diese Abfrage
// nicht (bekannte Grenze; sonst läse jeder Lauf mit einem Entwurf ohne
// Kopie alle Nachrichten des Kontos, BenchmarkCheckAfterSync22k).
func (s *SQLite) ScheduledMessages(ctx context.Context, accountID int64, notBefore time.Time) ([]ScheduledMessage, error) {
	nb := notBefore.UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT `+sentCandidateCols+`, COALESCE(m.message_id_header, ''), m.created_at
		FROM messages m WHERE m.id IN (
			SELECT id FROM messages WHERE account_id = ? AND date_header > ?
			UNION ALL
			SELECT id FROM messages WHERE account_id = ? AND date_header IS NULL)
		AND COALESCE(m.internal_date, 0) > ?
		AND `+scheduledPlaceCond+` ORDER BY COALESCE(m.internal_date, 0), m.id`, accountID, nb, accountID, nb)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledMessage
	for rows.Next() {
		var m ScheduledMessage
		var to, cc string
		var date, internal, created int64
		if err := rows.Scan(&m.ID, &m.Subject, &m.From, &to, &cc, &date, &m.InSent, &m.ContentHash, &m.InReplyTo, &internal,
			&m.MessageIDHeader, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(to), &m.To)
		_ = json.Unmarshal([]byte(cc), &m.Cc)
		m.Date = time.UnixMilli(date).UTC()
		if internal != 0 {
			m.InternalDate = time.UnixMilli(internal).UTC()
		}
		m.CreatedAt = time.UnixMilli(created).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// MessagePlace beschreibt, wo eine Nachricht laut Spiegel liegt (A10
// Teil 2, Übergänge eines Vermerks). Virtuelle Sichten zählen nicht.
type MessagePlace struct {
	// Found: Die Nachricht gibt es im Spiegel (auch ohne Serverkopie).
	Found bool
	// Roles: Rollen der Ordner mit einer Kopie (ohne virtuelle Sichten).
	Roles map[string]bool
	// InDrafts: in einem Entwürfe-Ordner im Sinn von sentOrAllCond (Rolle
	// drafts oder Ablageordner eines Entwurfs) oder irgendwo mit \Draft.
	InDrafts bool
	// From: Absender wie messages.from_addr.
	From string
	// InternalDate: INTERNALDATE (Nullwert = unbekannt).
	InternalDate time.Time
}

// Gone meldet, ob die Nachricht weg ist: nicht im Spiegel, ohne
// Serverkopie oder nur noch im Papierkorb bzw. Spam.
func (p MessagePlace) Gone() bool {
	if !p.Found {
		return true
	}
	for r := range p.Roles {
		if r != "trash" && r != "junk" {
			return false
		}
	}
	return true
}

// OnlyAll meldet, ob die Nachricht nur in „Alle Nachrichten“ liegt (und
// höchstens in eigenen Ordnern): nicht in Posteingang, Gesendet, Entwürfe,
// Papierkorb oder Spam.
func (p MessagePlace) OnlyAll() bool {
	if !p.Roles["all"] || p.InDrafts {
		return false
	}
	for _, r := range []string{"inbox", "sent", "drafts", "trash", "junk"} {
		if p.Roles[r] {
			return false
		}
	}
	return true
}

// Place liefert, wo die Nachricht id des Kontos laut Spiegel liegt.
func (s *SQLite) Place(ctx context.Context, accountID, id int64) (MessagePlace, error) {
	return place(ctx, s.db, accountID, id)
}

// Place liefert in der Transaktion, wo die Nachricht id liegt.
func (x *DraftTx) Place(accountID, id int64) (MessagePlace, error) {
	return place(x.ctx, x.tx, accountID, id)
}

func place(ctx context.Context, q querier, accountID, id int64) (MessagePlace, error) {
	p := MessagePlace{Roles: map[string]bool{}}
	var internal int64
	err := q.QueryRowContext(ctx, `SELECT m.from_addr, COALESCE(m.internal_date, 0),
		EXISTS (SELECT 1 FROM message_mailboxes dl JOIN mailboxes db ON db.id = dl.mailbox_id
			WHERE dl.message_id = m.id AND (dl.flag_draft = 1 OR db.role = 'drafts'
				OR db.id IN (SELECT remote_mailbox_id FROM drafts WHERE account_id = m.account_id AND remote_mailbox_id IS NOT NULL)))
		FROM messages m WHERE m.id = ? AND m.account_id = ?`, id, accountID).Scan(&p.From, &internal, &p.InDrafts)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT b.role FROM message_mailboxes l JOIN mailboxes b ON b.id = l.mailbox_id
		WHERE l.message_id = ? AND b.virtual_view = 0`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return p, err
		}
		p.Roles[r] = true
	}
	p.Found = true
	if internal != 0 {
		p.InternalDate = time.UnixMilli(internal).UTC()
	}
	return p, rows.Err()
}

// Zustände eines Vermerks zum geplanten Senden.
const (
	// ScheduledActive: Die vermerkte Nachricht ist geplant (oder ihr
	// Termin ist eben verstrichen, A10 Teil 2 (iv)).
	ScheduledActive = "scheduled"
	// ScheduledCancelled: Die vermerkte Nachricht liegt wieder im
	// Entwürfe-Ordner (Plan abgebrochen, B2); sie gilt als übernommene
	// Fassung des Entwurfs.
	ScheduledCancelled = "cancelled"
	// ScheduledGone: Die vermerkte Nachricht ist weg; der Vermerk hält nur
	// noch den Termin für das Zeitfenster der übrigen Wege.
	ScheduledGone = "gone"
)

// Scheduled ist der Vermerk zum geplanten Senden eines Entwurfs (A10
// Teil 2), in meta unter scheduled/<draft_id> (ohne Migration; das Audit
// rotiert, der Vermerk nicht).
type Scheduled struct {
	DraftID int64 `json:"-"`
	// MessageID: lokale ID der geplanten Nachricht.
	MessageID int64 `json:"message_id"`
	// At: der Termin (INTERNALDATE).
	At time.Time `json:"scheduled_for"`
	// State: ScheduledActive, ScheduledCancelled oder ScheduledGone.
	State string `json:"state"`
}

// scheduledKeyPrefix ist der Anfang der Schlüssel in meta.
const scheduledKeyPrefix = "scheduled/"

func scheduledKey(draftID int64) string { return scheduledKeyPrefix + strconv.FormatInt(draftID, 10) }

// closedStatuses sind die abgeschlossenen Zustände eines Entwurfs
// (drafts.Status.Closed; hier ohne Abhängigkeit vom Paket drafts).
const closedStatuses = `('sent_by_human','abandoned')`

// ScheduledOf liefert den Vermerk eines offenen Entwurfs; ok false: keiner
// oder der Entwurf ist abgeschlossen (Vermerke geschlossener Entwürfe
// gelten als nicht vorhanden).
func (s *SQLite) ScheduledOf(ctx context.Context, draftID int64) (Scheduled, bool, error) {
	return scheduledOf(ctx, s.db, draftID)
}

// ScheduledOf liefert den Vermerk in der Transaktion (siehe oben).
func (x *DraftTx) ScheduledOf(draftID int64) (Scheduled, bool, error) {
	return scheduledOf(x.ctx, x.tx, draftID)
}

func scheduledOf(ctx context.Context, q querier, draftID int64) (Scheduled, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT mt.value FROM meta mt JOIN drafts d ON d.id = ?
		WHERE mt.key = ? AND d.status NOT IN `+closedStatuses, draftID, scheduledKey(draftID)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return Scheduled{}, false, nil
	}
	if err != nil {
		return Scheduled{}, false, err
	}
	sc, ok := parseScheduled(draftID, v)
	return sc, ok, nil
}

func parseScheduled(draftID int64, v string) (Scheduled, bool) {
	var sc Scheduled
	if err := json.Unmarshal([]byte(v), &sc); err != nil || sc.MessageID <= 0 {
		return Scheduled{}, false // unlesbar: wie keiner
	}
	switch sc.State {
	case ScheduledActive, ScheduledCancelled, ScheduledGone:
	default:
		return Scheduled{}, false
	}
	sc.DraftID = draftID
	return sc, true
}

// ScheduledOfAccount liefert die Vermerke der offenen Entwürfe eines
// Kontos (accountID 0 = aller Konten), nach Entwurf.
func (s *SQLite) ScheduledOfAccount(ctx context.Context, accountID int64) (map[int64]Scheduled, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, mt.value FROM meta mt
		JOIN drafts d ON mt.key = '`+scheduledKeyPrefix+`' || d.id
		WHERE mt.key LIKE '`+scheduledKeyPrefix+`%' AND d.status NOT IN `+closedStatuses+` AND (? = 0 OR d.account_id = ?)`,
		accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Scheduled{}
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		if sc, ok := parseScheduled(id, v); ok {
			out[id] = sc
		}
	}
	return out, rows.Err()
}

// SetScheduled schreibt den Vermerk eines Entwurfs (ersetzt einen früheren).
func (x *DraftTx) SetScheduled(sc Scheduled) error {
	b, err := json.Marshal(Scheduled{MessageID: sc.MessageID, At: sc.At.UTC(), State: sc.State})
	if err != nil {
		return err
	}
	_, err = x.tx.ExecContext(x.ctx, `INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, scheduledKey(sc.DraftID), string(b))
	return err
}

// deleteScheduled löscht den Vermerk eines Entwurfs (UpdateDraft, sobald
// der Entwurf abgeschlossen ist: in derselben Transaktion, die ihn schließt).
func (x *DraftTx) deleteScheduled(draftID int64) error {
	_, err := x.tx.ExecContext(x.ctx, `DELETE FROM meta WHERE key = ?`, scheduledKey(draftID))
	return err
}

// ScheduledMessageIDs liefert die lokalen IDs der Nachrichten, die ein
// Vermerk eines offenen Entwurfs des Kontos als geplant oder abgebrochen
// hält (für die Wege der Versanderkennung gesperrt wie zugeordnete).
func (s *SQLite) ScheduledMessageIDs(ctx context.Context, accountID int64) (map[int64]bool, error) {
	all, err := s.ScheduledOfAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, sc := range all {
		if sc.State != ScheduledGone {
			out[sc.MessageID] = true
		}
	}
	return out, nil
}

// IsScheduled meldet für die Nachrichten ids des Kontos, welche nach den
// Bedingungen (a), (b) und (d) geplant sind (INTERNALDATE nach notBefore),
// mit ihrem Termin. (c) eigener Absender: own (klein geschriebene eigene
// Adressen; nil = nicht prüfen). Für die Anzeige (A10 Teil 3).
func (s *SQLite) IsScheduled(ctx context.Context, ids []int64, notBefore time.Time, own func(from string) bool) (map[int64]time.Time, error) {
	out := map[int64]time.Time{}
	if len(ids) == 0 {
		return out, nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.from_addr, m.internal_date FROM messages m
		WHERE m.id IN (SELECT value FROM json_each(?)) AND COALESCE(m.internal_date, 0) > ?
		AND `+scheduledPlaceCond, string(b), notBefore.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, internal int64
		var from string
		if err := rows.Scan(&id, &from, &internal); err != nil {
			return nil, err
		}
		if own != nil && !own(from) {
			continue
		}
		out[id] = time.UnixMilli(internal).UTC()
	}
	return out, rows.Err()
}

// ScheduledSQL ist die Bedingung „geplant“ ((a), (b), (d)) für die
// Nachricht m mit einem Platzhalter für notBefore (Millisekunden), für die
// Suche (is:scheduled, A10 Teil 3); den eigenen Absender (c) prüft der
// Aufrufer.
const ScheduledSQL = `COALESCE(m.internal_date, 0) > ? AND ` + scheduledPlaceCond

// ScheduledKeysForTest liefert die Schlüssel aller Vermerke, auch die
// geschlossener Entwürfe (die beim Schließen gelöscht sein müssen).
func (s *SQLite) ScheduledKeysForTest(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM meta WHERE key LIKE '`+scheduledKeyPrefix+`%' ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// MessageIDHeaderOf liefert die Message-ID (ohne <> und Leerraum) der
// Nachricht id des Kontos; "" = keine oder unbekannt.
func (s *SQLite) MessageIDHeaderOf(ctx context.Context, accountID, id int64) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT trim(COALESCE(message_id_header, ''), `+trimIDChars+`) FROM messages
		WHERE id = ? AND account_id = ?`, id, accountID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return h, err
}
