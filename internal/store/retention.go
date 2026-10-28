package store

import (
	"context"
	"fmt"
	"time"
)

// Löschkonzept (S3.4, E8; docs/datenmodell.md, „Löschkonzept“): Zeilen für
// retention.purge und blobs.gc. Gelöscht wird nur über diese ausdrücklichen
// Operationen (internal/retention), nie nebenbei (ADR-003).

// purgeRule ist die Regel, nach der retention.purge eine Nachricht m löschen
// darf (A13.2, A14), mit dem Stichtag als ersten Parameter:
//
//   - der Server führt sie nicht mehr (deleted_remote_at vor dem Stichtag
//     und keine Zuordnung in message_mailboxes),
//   - sie gehört nicht zum importierten Archiv (keine Zeile in
//     message_import_folders): Eine nur importierte Nachricht trägt
//     deleted_remote_at = Importzeit, lag aber nie auf dem Server; eine
//     importierte, die auch auf dem Server lag, bleibt ebenso,
//   - kein Entwurf verweist auf sie (drafts.in_reply_to_msg,
//     drafts.sent_message_id; offen oder geschlossen). So bricht purge
//     keinen Entwurf (Zitatquelle, Antwortbezug), und der Verweis bleibt als
//     Zahl stehen (E8), ohne dass ON DELETE SET NULL ihn löscht.
const purgeRule = `m.deleted_remote_at IS NOT NULL AND m.deleted_remote_at < ?
	AND NOT EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = m.id)
	AND NOT EXISTS (SELECT 1 FROM message_import_folders mi WHERE mi.message_id = m.id)
	AND NOT EXISTS (SELECT 1 FROM drafts d WHERE d.in_reply_to_msg = m.id OR d.sent_message_id = m.id)`

// PurgeCounts zählt, was retention.purge sieht (Stichtag und Konto wie
// angegeben). Gone sind alle Nachrichten ohne Serverkopie mit
// deleted_remote_at vor dem Stichtag; davon bleiben KeptImported (Archiv)
// und KeptDrafts (Verweis eines Entwurfs); der Rest steht in IDs.
type PurgeCounts struct {
	Gone         int
	KeptImported int
	KeptDrafts   int
}

// PurgeCandidates liefert die IDs der Nachrichten, die retention.purge
// löschen darf (purgeRule), aufsteigend, und die Zähler. accountID 0 =
// alle Konten.
func (s *SQLite) PurgeCandidates(ctx context.Context, cutoff time.Time, accountID int64) ([]int64, PurgeCounts, error) {
	var c PurgeCounts
	acct, args := "", []any{cutoff.UnixMilli()}
	if accountID > 0 {
		acct, args = " AND m.account_id = ?", append(args, accountID)
	}
	err := s.db.QueryRowContext(ctx, `SELECT count(*),
		coalesce(sum(EXISTS (SELECT 1 FROM message_import_folders mi WHERE mi.message_id = m.id)), 0),
		coalesce(sum(NOT EXISTS (SELECT 1 FROM message_import_folders mi WHERE mi.message_id = m.id)
			AND EXISTS (SELECT 1 FROM drafts d WHERE d.in_reply_to_msg = m.id OR d.sent_message_id = m.id)), 0)
		FROM messages m WHERE m.deleted_remote_at IS NOT NULL AND m.deleted_remote_at < ?
		AND NOT EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = m.id)`+acct, args...).Scan(&c.Gone, &c.KeptImported, &c.KeptDrafts)
	if err != nil {
		return nil, c, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id FROM messages m WHERE `+purgeRule+acct+` ORDER BY m.id`, args...)
	if err != nil {
		return nil, c, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, c, err
		}
		ids = append(ids, id)
	}
	return ids, c, rows.Err()
}

// PurgeResult ist das Ergebnis von PurgeMessages.
type PurgeResult struct {
	Messages       int // gelöschte Nachrichten
	Attachments    int // ihre Anhangszeilen (die Blobs bleiben, blobs.gc)
	ThreadsRemoved int // Threads ohne Nachricht danach
}

// purgeBatch: so viele Nachrichten je Transaktion (kurze Schreibsperren
// für andere Prozesse, z. B. den MCP-Server).
const purgeBatch = 500

// PurgeMessages löscht die Nachrichten ids, soweit sie zum Stichtag noch
// purgeRule erfüllen (jede Transaktion prüft die Regel neu). Mit ihnen
// gehen Text (bodies), Anhangszeilen, Tags, Indexzeilen (Trigger
// messages_index_delete, index_queue) per Fremdschlüssel bzw. Trigger;
// betroffene Threads werden neu berechnet, Threads ohne Nachricht gelöscht.
// IDs werden danach nicht neu vergeben (Höchststand in meta, 0022). Blobs
// bleiben (blobs.gc).
func (s *SQLite) PurgeMessages(ctx context.Context, cutoff time.Time, ids []int64) (PurgeResult, error) {
	var res PurgeResult
	for len(ids) > 0 {
		n := min(len(ids), purgeBatch)
		r, err := s.purgeBatch(ctx, cutoff, ids[:n])
		if err != nil {
			return res, err
		}
		res.Messages += r.Messages
		res.Attachments += r.Attachments
		res.ThreadsRemoved += r.ThreadsRemoved
		ids = ids[n:]
	}
	return res, nil
}

func (s *SQLite) purgeBatch(ctx context.Context, cutoff time.Time, ids []int64) (res PurgeResult, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	args := []any{cutoff.UnixMilli()}
	for _, id := range ids {
		args = append(args, id)
	}
	in := `(` + placeholders(len(ids)) + `)`
	countArgs := append(append([]any{}, args[1:]...), args[0])
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE message_id IN `+in+` AND message_id IN
		(SELECT m.id FROM messages m WHERE `+purgeRule+`)`, countArgs...).Scan(&res.Attachments); err != nil {
		return res, err
	}
	rows, err := tx.QueryContext(ctx, `DELETE FROM messages WHERE id IN (SELECT m.id FROM messages m WHERE `+purgeRule+`
		AND m.id IN `+in+`) RETURNING thread_id`, args...)
	if err != nil {
		return res, fmt.Errorf("store: purge: %w", err)
	}
	threads := map[int64]bool{}
	for rows.Next() {
		var tid *int64
		if err := rows.Scan(&tid); err != nil {
			_ = rows.Close()
			return res, err
		}
		res.Messages++
		if tid != nil {
			threads[*tid] = true
		}
	}
	if err := rows.Close(); err != nil {
		return res, err
	}
	for tid := range threads {
		var left int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE thread_id = ?`, tid).Scan(&left); err != nil {
			return res, err
		}
		if left == 0 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, tid); err != nil {
				return res, err
			}
			res.ThreadsRemoved++
			continue
		}
		if err := refreshThread(ctx, tx, tid); err != nil {
			return res, err
		}
	}
	return res, tx.Commit()
}

// BlobReferences liefert alle Hashes, auf die etwas verweist (Mark für
// blobs.gc, A13.3): Rohmails (messages.blob_sha256), Anhänge
// (attachments.sha256) und Anhänge aller Entwürfe in jedem Status
// (drafts.attachment_blobs; Dateianhänge haben keine Zeile in blobs).
// Weitere Spalten mit Blob-Hashes gibt es nicht (geprüft an allen
// Migrationen bis 0022: identities.style_file ist ein Pfad,
// content_hash/pushed_content_hash/remote_previous sind Prüfsummen von
// Nachrichten, keine Blobs).
func (s *SQLite) BlobReferences(ctx context.Context) (map[string]bool, error) {
	refs := map[string]bool{}
	for _, q := range []string{
		`SELECT DISTINCT blob_sha256 FROM messages WHERE blob_sha256 IS NOT NULL`,
		`SELECT DISTINCT sha256 FROM attachments WHERE sha256 IS NOT NULL`,
	} {
		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				_ = rows.Close()
				return nil, err
			}
			refs[h] = true
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	drafts, err := s.DraftAttachmentHashes(ctx)
	if err != nil {
		return nil, err
	}
	for _, h := range drafts {
		refs[h] = true
	}
	return refs, nil
}

// BlobRowAge ist eine Zeile der Tabelle blobs mit ihrem Anlagezeitpunkt.
type BlobRowAge struct {
	SHA256    string
	CreatedAt time.Time
}

// BlobRowAges liefert alle Zeilen der Tabelle blobs mit created_at.
func (s *SQLite) BlobRowAges(ctx context.Context) ([]BlobRowAge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sha256, created_at FROM blobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlobRowAge
	for rows.Next() {
		var b BlobRowAge
		var at int64
		if err := rows.Scan(&b.SHA256, &at); err != nil {
			return nil, err
		}
		b.CreatedAt = unixMilliUTC(at)
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteUnreferencedBlobRows löscht die Zeilen sums aus blobs, soweit beim
// Löschen nichts auf sie verweist (gleiche Quellen wie BlobReferences, in
// derselben Anweisung geprüft; dazu der Fremdschlüssel von messages und
// attachments). Liefert die gelöschten Hashes.
func (s *SQLite) DeleteUnreferencedBlobRows(ctx context.Context, sums []string) ([]string, error) {
	var out []string
	for len(sums) > 0 {
		n := min(len(sums), purgeBatch)
		args := make([]any, 0, n)
		for _, h := range sums[:n] {
			args = append(args, h)
		}
		rows, err := s.db.QueryContext(ctx, `DELETE FROM blobs WHERE sha256 IN (`+placeholders(n)+`)
			AND sha256 NOT IN (SELECT blob_sha256 FROM messages WHERE blob_sha256 IS NOT NULL)
			AND sha256 NOT IN (SELECT sha256 FROM attachments WHERE sha256 IS NOT NULL)
			AND sha256 NOT IN (SELECT json_extract(j.value, '$.sha256') FROM drafts d, json_each(d.attachment_blobs) j
			                   WHERE json_extract(j.value, '$.sha256') IS NOT NULL)
			RETURNING sha256`, args...)
		if err != nil {
			return out, fmt.Errorf("store: delete blob rows: %w", err)
		}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				_ = rows.Close()
				return out, err
			}
			out = append(out, h)
		}
		if err := rows.Close(); err != nil {
			return out, err
		}
		sums = sums[n:]
	}
	return out, nil
}

// AccountIDs liefert die IDs aller Konten (Kontosperren für purge und
// blob gc).
func (s *SQLite) AccountIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM accounts ORDER BY id`)
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
