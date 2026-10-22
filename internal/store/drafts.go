package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// Entwürfe, Freigaben und Identitäten (S2.1, Migration 0010). Hier steht
// nur die Ablage; Zustandsmaschine, preview_hash, Grenzen und Message-ID
// liegen in internal/drafts, das jede Änderung in einer kurzen
// Transaktion (BEGIN IMMEDIATE, siehe Open: _txlock) über DraftTx ausführt:
// lesen, Übergang prüfen, schreiben, bei Systemereignissen den
// Audit-Eintrag in derselben Transaktion.

// Fehler der Entwurfsablage.
var (
	ErrDraftNotFound    = errors.New("store: draft not found")
	ErrApprovalNotFound = errors.New("store: approval not found")
	ErrIdentityNotFound = errors.New("store: identity not found")
)

// Addr ist eine Empfängeradresse eines Entwurfs (JSON wie messages.to_addrs).
type Addr struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// DraftAttachment ist ein Anhang eines Entwurfs: Blob im Blob-Store, Name
// und Typ (E10; die Auswahl und Prüfung kommt mit S2.2/S2.3).
type DraftAttachment struct {
	SHA256      string `json:"sha256"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
	// Declared ist der angegebene Typ (Anhang einer Nachricht), Mismatch und
	// Executable die Befunde der Typprüfung beim Nachrichtenbau
	// (Entscheid 39: erlaubt, mit Warnung in Ergebnis und draft show; S2.3).
	Declared   string `json:"declared,omitempty"`
	Mismatch   bool   `json:"mismatch,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

// Draft ist eine Zeile der Tabelle drafts.
type Draft struct {
	ID                int64
	AccountID         int64
	IdentityID        int64 // 0 = keine Identität (Kontoadresse)
	Kind              string
	InReplyToMsg      int64 // 0 = keine Ausgangsnachricht
	MessageIDHeader   string
	FromName          string
	FromEmail         string
	To, Cc, Bcc       []Addr
	Subject           string
	BodyMarkdown      string
	BodyText          string
	BodyHTML          string
	HeadersJSON       string // JSON-Objekt
	Attachments       []DraftAttachment
	Status            string
	PreviewHash       string
	HookResult        string // "" | pass | fail | timeout | error
	RemoteMailboxID   int64
	RemoteUID         int64
	RemoteState       string // pending | pushed | taken_over | missing
	PushedContentHash string
	CreatedBy         string // "cli" | "key:<name>"
	ApprovalID        int64
	SentMessageID     int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ClosedAt          time.Time // Nullzeit = offen
	// ComposeOptions: Schalter des Nachrichtenbaus als JSON-Objekt
	// (quote, signature; Migration 0011, S2.3). "" = {}.
	ComposeOptions string
}

// Approval ist eine Zeile der Tabelle approvals.
type Approval struct {
	ID          int64
	SubjectType string // draft | action
	SubjectID   int64
	Description string
	RequestedBy string
	RequestedAt time.Time
	PreviewHash string
	ExpiresAt   time.Time
	Decision    string // pending | approved | rejected | superseded | expired
	DecidedBy   string
	DecidedAt   time.Time
	Reason      string
}

// Identity ist eine Absenderidentität eines Kontos (E9).
type Identity struct {
	ID            int64
	AccountID     int64
	Name          string
	Email         string
	ReplyTo       string
	SignatureText string
	SignatureHTML string
	StyleFile     string
	ReplyPrefix   string
	ForwardPrefix string
	IsDefault     bool
	CreatedAt     time.Time
}

// DraftFilter wählt Entwürfe für ListDrafts (Felder optional).
type DraftFilter struct {
	AccountID int64
	Status    []string
	Limit     int
}

// DraftTx ist eine laufende Schreibtransaktion für Entwürfe.
type DraftTx struct {
	ctx context.Context
	tx  *sql.Tx
}

// DraftTx führt fn in einer Schreibtransaktion aus (BEGIN IMMEDIATE: die
// Schreibsperre sofort, sonst warten bis busy_timeout). Ein Fehler von fn
// rollt alles zurück. fn soll kurz bleiben: kein Netz, kein Hook, keine
// Dateien.
func (s *SQLite) DraftTx(ctx context.Context, fn func(*DraftTx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(&DraftTx{ctx: ctx, tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

const draftCols = `id, account_id, COALESCE(identity_id, 0), kind, COALESCE(in_reply_to_msg, 0), message_id_header,
	from_name, from_email, to_addrs, cc_addrs, bcc_addrs, subject, COALESCE(body_markdown, ''), body_text,
	COALESCE(body_html, ''), headers_json, attachment_blobs, status, COALESCE(preview_hash, ''), COALESCE(hook_result, ''),
	COALESCE(remote_mailbox_id, 0), COALESCE(remote_uid, 0), remote_state, COALESCE(pushed_content_hash, ''),
	COALESCE(created_by, ''), COALESCE(approval_id, 0), COALESCE(sent_message_id, 0), created_at, updated_at,
	COALESCE(closed_at, 0), compose_options`

func scanDraft(sc interface{ Scan(...any) error }) (Draft, error) {
	var d Draft
	var to, cc, bcc, atts string
	var created, updated, closed int64
	if err := sc.Scan(&d.ID, &d.AccountID, &d.IdentityID, &d.Kind, &d.InReplyToMsg, &d.MessageIDHeader,
		&d.FromName, &d.FromEmail, &to, &cc, &bcc, &d.Subject, &d.BodyMarkdown, &d.BodyText,
		&d.BodyHTML, &d.HeadersJSON, &atts, &d.Status, &d.PreviewHash, &d.HookResult,
		&d.RemoteMailboxID, &d.RemoteUID, &d.RemoteState, &d.PushedContentHash,
		&d.CreatedBy, &d.ApprovalID, &d.SentMessageID, &created, &updated, &closed, &d.ComposeOptions); err != nil {
		return d, err
	}
	for _, x := range []struct {
		raw string
		dst any
	}{{to, &d.To}, {cc, &d.Cc}, {bcc, &d.Bcc}, {atts, &d.Attachments}} {
		if err := json.Unmarshal([]byte(x.raw), x.dst); err != nil {
			return d, fmt.Errorf("store: draft %d: corrupt JSON column: %w", d.ID, err)
		}
	}
	d.CreatedAt, d.UpdatedAt = unixMilliUTC(created), unixMilliUTC(updated)
	if closed != 0 {
		d.ClosedAt = unixMilliUTC(closed)
	}
	return d, nil
}

// draftArgs liefert die veränderlichen Spalten in der Reihenfolge von
// draftWriteCols.
func draftArgs(d Draft) ([]any, error) {
	enc := func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	}
	var js []string
	for _, l := range [][]Addr{d.To, d.Cc, d.Bcc} {
		if l == nil {
			l = []Addr{}
		}
		s, err := enc(l)
		if err != nil {
			return nil, err
		}
		js = append(js, s)
	}
	atts := d.Attachments
	if atts == nil {
		atts = []DraftAttachment{}
	}
	as, err := enc(atts)
	if err != nil {
		return nil, err
	}
	headers := d.HeadersJSON
	if headers == "" {
		headers = "{}"
	}
	remote := d.RemoteState
	if remote == "" {
		remote = "pending"
	}
	var closed any
	if !d.ClosedAt.IsZero() {
		closed = d.ClosedAt.UnixMilli()
	}
	opts := d.ComposeOptions
	if opts == "" {
		opts = "{}"
	}
	return []any{nullID(d.IdentityID), d.Kind, nullID(d.InReplyToMsg), d.MessageIDHeader, d.FromName, d.FromEmail,
		js[0], js[1], js[2], d.Subject, nullStr(d.BodyMarkdown), d.BodyText, nullStr(d.BodyHTML), headers, as,
		d.Status, nullStr(d.PreviewHash), nullStr(d.HookResult), nullID(d.RemoteMailboxID), nullID(d.RemoteUID), remote,
		nullStr(d.PushedContentHash), nullStr(d.CreatedBy), nullID(d.ApprovalID), nullID(d.SentMessageID),
		d.UpdatedAt.UnixMilli(), closed, opts}, nil
}

const draftWriteCols = `identity_id, kind, in_reply_to_msg, message_id_header, from_name, from_email,
	to_addrs, cc_addrs, bcc_addrs, subject, body_markdown, body_text, body_html, headers_json, attachment_blobs,
	status, preview_hash, hook_result, remote_mailbox_id, remote_uid, remote_state,
	pushed_content_hash, created_by, approval_id, sent_message_id, updated_at, closed_at, compose_options`

func nullID(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

// InsertDraft legt einen Entwurf an und liefert seine ID.
func (x *DraftTx) InsertDraft(d Draft) (int64, error) {
	args, err := draftArgs(d)
	if err != nil {
		return 0, err
	}
	n := strings.Count(draftWriteCols, ",") + 1
	var id int64
	err = x.tx.QueryRowContext(x.ctx, `INSERT INTO drafts(account_id, created_at, `+draftWriteCols+`)
		VALUES (?, ?`+strings.Repeat(", ?", n)+`) RETURNING id`,
		append([]any{d.AccountID, d.CreatedAt.UnixMilli()}, args...)...).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: insert draft: %w", err)
	}
	return id, nil
}

// UpdateDraft schreibt alle veränderlichen Spalten eines Entwurfs.
func (x *DraftTx) UpdateDraft(d Draft) error {
	args, err := draftArgs(d)
	if err != nil {
		return err
	}
	cols := strings.Split(draftWriteCols, ",")
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i]) + " = ?"
	}
	res, err := x.tx.ExecContext(x.ctx, `UPDATE drafts SET `+strings.Join(cols, ", ")+` WHERE id = ?`, append(args, d.ID)...)
	if err != nil {
		return fmt.Errorf("store: update draft %d: %w", d.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %d", ErrDraftNotFound, d.ID)
	}
	return nil
}

// Draft liest einen Entwurf innerhalb der Transaktion.
func (x *DraftTx) Draft(id int64) (Draft, error) { return getDraft(x.ctx, x.tx, id) }

// Draft liest einen Entwurf.
func (s *SQLite) Draft(ctx context.Context, id int64) (Draft, error) { return getDraft(ctx, s.db, id) }

func getDraft(ctx context.Context, q querier, id int64) (Draft, error) {
	d, err := scanDraft(q.QueryRowContext(ctx, `SELECT `+draftCols+` FROM drafts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("%w: %d", ErrDraftNotFound, id)
	}
	return d, err
}

// ListDrafts liefert Entwürfe, neueste zuerst.
func (s *SQLite) ListDrafts(ctx context.Context, f DraftFilter) ([]Draft, error) {
	var where []string
	var args []any
	if f.AccountID > 0 {
		where, args = append(where, "account_id = ?"), append(args, f.AccountID)
	}
	if len(f.Status) > 0 {
		where = append(where, "status IN (?"+strings.Repeat(",?", len(f.Status)-1)+")")
		for _, st := range f.Status {
			args = append(args, st)
		}
	}
	q := `SELECT ` + draftCols + ` FROM drafts`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY updated_at DESC, id DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Draft
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ExpiredDraftIDs liefert die Entwürfe in pending_approval oder ready,
// deren offene bzw. erteilte Freigabe bis now abgelaufen ist (E7;
// accountID 0 = alle Konten).
func (x *DraftTx) ExpiredDraftIDs(now time.Time, accountID int64) ([]int64, error) {
	rows, err := x.tx.QueryContext(x.ctx, `SELECT d.id FROM drafts d JOIN approvals a ON a.id = d.approval_id
		WHERE d.status IN ('pending_approval','ready') AND a.decision IN ('pending','approved')
		  AND a.expires_at IS NOT NULL AND a.expires_at <= ? AND (? = 0 OR d.account_id = ?) ORDER BY d.id`,
		now.UnixMilli(), accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

const approvalCols = `id, subject_type, COALESCE(subject_id, 0), description, COALESCE(requested_by, ''), requested_at,
	preview_hash, COALESCE(expires_at, 0), decision, COALESCE(decided_by, ''), COALESCE(decided_at, 0), COALESCE(reason, '')`

func scanApproval(sc interface{ Scan(...any) error }) (Approval, error) {
	var a Approval
	var req, exp, dec int64
	if err := sc.Scan(&a.ID, &a.SubjectType, &a.SubjectID, &a.Description, &a.RequestedBy, &req, &a.PreviewHash, &exp,
		&a.Decision, &a.DecidedBy, &dec, &a.Reason); err != nil {
		return a, err
	}
	a.RequestedAt = unixMilliUTC(req)
	if exp != 0 {
		a.ExpiresAt = unixMilliUTC(exp)
	}
	if dec != 0 {
		a.DecidedAt = unixMilliUTC(dec)
	}
	return a, nil
}

// Approval liest eine Freigabe innerhalb der Transaktion.
func (x *DraftTx) Approval(id int64) (Approval, error) { return getApproval(x.ctx, x.tx, id) }

// Approval liest eine Freigabe.
func (s *SQLite) Approval(ctx context.Context, id int64) (Approval, error) {
	return getApproval(ctx, s.db, id)
}

func getApproval(ctx context.Context, q querier, id int64) (Approval, error) {
	a, err := scanApproval(q.QueryRowContext(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("%w: %d", ErrApprovalNotFound, id)
	}
	return a, err
}

// InsertApproval legt eine Freigabe an (Entscheidung pending).
func (x *DraftTx) InsertApproval(a Approval) (int64, error) {
	if a.Decision == "" {
		a.Decision = "pending"
	}
	var id int64
	err := x.tx.QueryRowContext(x.ctx, `INSERT INTO approvals(subject_type, subject_id, description, requested_by,
		requested_at, preview_hash, expires_at, decision) VALUES (?,?,?,?,?,?,?,?) RETURNING id`,
		a.SubjectType, nullID(a.SubjectID), a.Description, nullStr(a.RequestedBy), a.RequestedAt.UnixMilli(),
		a.PreviewHash, nullTime(a.ExpiresAt), a.Decision).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: insert approval: %w", err)
	}
	return id, nil
}

// UpdateApproval schreibt Entscheidung, Entscheider, Zeitpunkte und Grund.
func (x *DraftTx) UpdateApproval(a Approval) error {
	res, err := x.tx.ExecContext(x.ctx, `UPDATE approvals SET decision = ?, decided_by = ?, decided_at = ?, reason = ?,
		expires_at = ? WHERE id = ?`, a.Decision, nullStr(a.DecidedBy), nullTime(a.DecidedAt), nullStr(a.Reason),
		nullTime(a.ExpiresAt), a.ID)
	if err != nil {
		return fmt.Errorf("store: update approval %d: %w", a.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %d", ErrApprovalNotFound, a.ID)
	}
	return nil
}

// AppendAudit schreibt einen Audit-Eintrag in derselben Transaktion (für
// Systemereignisse wie den Ablauf einer Freigabe: Übergang und Eintrag
// gelingen nur zusammen).
func (x *DraftTx) AppendAudit(e audit.Event) (int64, error) {
	params := e.ParamsRedacted
	if len(params) == 0 {
		params = []byte(`{}`)
	}
	if !json.Valid(params) {
		return 0, fmt.Errorf("store: audit: params_redacted is not valid JSON")
	}
	targets := e.Targets
	if targets == nil {
		targets = []string{}
	}
	tj, err := json.Marshal(targets)
	if err != nil {
		return 0, err
	}
	return appendAudit(x.ctx, x.tx, e, params, tj)
}

// EnqueueDraftPush reiht den Job draft_push für ein Konto ein (A02 E1), in
// derselben Transaktion wie die Änderung des Entwurfs. Ist schon einer
// offen (queued oder running; höchstens einer je Art und Konto, Index
// jobs_open), bleibt es bei diesem; er arbeitet alle Entwürfe des Kontos
// mit remote_state pending ab (S2.5). Liefert die ID des offenen Jobs.
func (x *DraftTx) EnqueueDraftPush(accountID int64, now time.Time) (int64, error) {
	payload := fmt.Sprintf(`{"account_id":%d}`, accountID)
	if _, err := x.tx.ExecContext(x.ctx, `INSERT OR IGNORE INTO jobs(type, payload, run_at) VALUES (?, ?, ?)`,
		JobDraftPush, payload, now.UnixMilli()); err != nil {
		return 0, fmt.Errorf("store: enqueue draft_push: %w", err)
	}
	var id int64
	err := x.tx.QueryRowContext(x.ctx, `SELECT id FROM jobs WHERE type = ? AND state IN ('queued','running')
		AND json_extract(payload, '$.account_id') = ?`, JobDraftPush, accountID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: enqueue draft_push: %w", err)
	}
	return id, nil
}

// Account liest ein Konto innerhalb der Transaktion.
func (x *DraftTx) Account(id int64) (Account, error) {
	var a Account
	err := scanAccount(x.tx.QueryRowContext(x.ctx, `SELECT `+accountCols+` FROM accounts WHERE id = ?`, id), &a)
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("%w: id %d", ErrAccountNotFound, id)
	}
	return a, err
}

// --- Identitäten -------------------------------------------------------------

const identityCols = `id, account_id, name, email, COALESCE(reply_to, ''), signature_text, signature_html,
	COALESCE(style_file, ''), reply_prefix, forward_prefix, is_default, created_at`

func scanIdentity(sc interface{ Scan(...any) error }) (Identity, error) {
	var i Identity
	var def int
	var created int64
	err := sc.Scan(&i.ID, &i.AccountID, &i.Name, &i.Email, &i.ReplyTo, &i.SignatureText, &i.SignatureHTML,
		&i.StyleFile, &i.ReplyPrefix, &i.ForwardPrefix, &def, &created)
	i.IsDefault = def == 1
	if created != 0 {
		i.CreatedAt = unixMilliUTC(created)
	}
	return i, err
}

// Identity liest eine Identität innerhalb der Transaktion.
func (x *DraftTx) Identity(id int64) (Identity, error) { return getIdentity(x.ctx, x.tx, id) }

// Identity liest eine Identität.
func (s *SQLite) Identity(ctx context.Context, id int64) (Identity, error) {
	return getIdentity(ctx, s.db, id)
}

func getIdentity(ctx context.Context, q querier, id int64) (Identity, error) {
	i, err := scanIdentity(q.QueryRowContext(ctx, `SELECT `+identityCols+` FROM identities WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, fmt.Errorf("%w: %d", ErrIdentityNotFound, id)
	}
	return i, err
}

// DefaultIdentity liefert die Standardidentität eines Kontos; ok = false,
// wenn das Konto keine hat.
func (x *DraftTx) DefaultIdentity(accountID int64) (Identity, bool, error) {
	i, err := scanIdentity(x.tx.QueryRowContext(x.ctx, `SELECT `+identityCols+` FROM identities
		WHERE account_id = ? AND is_default = 1`, accountID))
	if errors.Is(err, sql.ErrNoRows) {
		return i, false, nil
	}
	return i, err == nil, err
}

// Identities liefert die Identitäten eines Kontos (0 = aller Konten), nach ID.
func (s *SQLite) Identities(ctx context.Context, accountID int64) ([]Identity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+identityCols+` FROM identities WHERE (? = 0 OR account_id = ?) ORDER BY id`,
		accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		i, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// AddIdentity legt eine Identität an. Ist sie die erste des Kontos oder
// ausdrücklich Standard, wird sie Standard (die bisherige verliert das).
func (s *SQLite) AddIdentity(ctx context.Context, i Identity) (int64, error) {
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now()
	}
	if i.ReplyPrefix == "" {
		i.ReplyPrefix = "Re: "
	}
	if i.ForwardPrefix == "" {
		i.ForwardPrefix = "Fwd: "
	}
	var id int64
	err := s.DraftTx(ctx, func(x *DraftTx) error {
		var n int
		if err := x.tx.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE account_id = ?`, i.AccountID).Scan(&n); err != nil {
			return err
		}
		def := i.IsDefault || n == 0
		if def {
			if _, err := x.tx.ExecContext(ctx, `UPDATE identities SET is_default = 0 WHERE account_id = ?`, i.AccountID); err != nil {
				return err
			}
		}
		return x.tx.QueryRowContext(ctx, `INSERT INTO identities(account_id, name, email, reply_to, signature_text,
			signature_html, style_file, reply_prefix, forward_prefix, is_default, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?) RETURNING id`, i.AccountID, i.Name, i.Email, nullStr(i.ReplyTo), i.SignatureText,
			i.SignatureHTML, nullStr(i.StyleFile), i.ReplyPrefix, i.ForwardPrefix, boolInt(def), i.CreatedAt.UnixMilli()).Scan(&id)
	})
	if err != nil {
		return 0, fmt.Errorf("store: add identity: %w", err)
	}
	return id, nil
}

// SetDefaultIdentity macht eine Identität zur Standardidentität ihres Kontos.
func (s *SQLite) SetDefaultIdentity(ctx context.Context, id int64) error {
	return s.DraftTx(ctx, func(x *DraftTx) error {
		i, err := x.Identity(id)
		if err != nil {
			return err
		}
		if _, err := x.tx.ExecContext(ctx, `UPDATE identities SET is_default = 0 WHERE account_id = ?`, i.AccountID); err != nil {
			return err
		}
		_, err = x.tx.ExecContext(ctx, `UPDATE identities SET is_default = 1 WHERE id = ?`, id)
		return err
	})
}

// RemoveIdentity entfernt eine Identität. Entwürfe behalten ihren Absender
// (from_name/from_email sind eine Kopie), identity_id wird NULL. War sie
// Standard, wird die älteste verbleibende des Kontos Standard.
func (s *SQLite) RemoveIdentity(ctx context.Context, id int64) error {
	return s.DraftTx(ctx, func(x *DraftTx) error {
		i, err := x.Identity(id)
		if err != nil {
			return err
		}
		if _, err := x.tx.ExecContext(ctx, `DELETE FROM identities WHERE id = ?`, id); err != nil {
			return err
		}
		if !i.IsDefault {
			return nil
		}
		_, err = x.tx.ExecContext(ctx, `UPDATE identities SET is_default = 1
			WHERE id = (SELECT MIN(id) FROM identities WHERE account_id = ?)`, i.AccountID)
		return err
	})
}

// OwnAddresses liefert die eigenen Adressen eines Kontos (E9):
// Kontoadresse, Anmeldename (wenn Adresse), die Adressen und Reply-To der
// Identitäten und die Aliasse; klein, ohne Dubletten.
func (s *SQLite) OwnAddresses(ctx context.Context, accountID int64) ([]string, error) {
	return selfAddresses(ctx, s.db, accountID)
}
