package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// Sicherung und Wiederherstellung (S3.4, E7, A10): der Schnappschuss für
// backup.create und die Schritte, die eine wiederhergestellte Datenbank zu
// einer eigenen Instanz machen. Die Abläufe stehen in internal/backup.

// SnapshotTo schreibt eine konsistente Kopie der Datenbank nach dest
// (VACUUM INTO: ein Lesevorgang über den ganzen Bestand, neben laufenden
// Schreibern wie sync --watch; WAL-Inhalte sind enthalten). dest darf noch
// nicht existieren. Die Datei hat von Anfang an die Rechte 0600
// (Nachbesserung T3, wie der Migrations-Snapshot in sqlite.go): erst leer
// mit O_EXCL angelegt (VACUUM INTO nimmt ein leeres Ziel an), dann
// gefüllt; vorher lag sie bis zum chmod nach umask lesbar (meist 0644).
// Das chmod danach ist nur eine zweite Absicherung (etwa wenn eine
// Vorgabe-ACL die Rechte beim Anlegen erweitert) und nicht fatal.
func (s *SQLite) SnapshotTo(ctx context.Context, dest string) error {
	if s.path == "" {
		return errors.New("store: snapshot of an in-memory database")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("store: snapshot: %w", err)
	}
	_ = f.Close()
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("store: snapshot: %w", err)
	}
	_ = os.Chmod(dest, 0o600)
	return nil
}

// RestoreReason ist der Grund, den die Wiederherstellung an geschlossene
// Jobs, Aktionen, Anträge, Entwürfe und Audit-Starts schreibt.
const RestoreReason = "restored"

// RestorePrep steuert PrepareRestored.
type RestorePrep struct {
	// DataDirID ist die neue Kennung des Datenverzeichnisses (32 Hex);
	// leer = die Kennung des Originals bleibt (--keep-identity).
	DataDirID string
	// CredentialRef bildet den Schlüsselbund-Verweis eines Kontos zur neuen
	// Kennung (die ersten 8 Hex) und dem Kontonamen (secrets.AccountRef);
	// nur mit DataDirID.
	CredentialRef func(dirID8, account string) string
	// Event baut das Systemereignis der Wiederherstellung mit den Zählern;
	// es wird als Letztes geschrieben, in derselben Transaktion wie die
	// Änderungen außer den Aktionen (siehe PrepareRestored).
	Event func(RestoreFixes) audit.Event
	Now   time.Time
}

// RestoreFixes zählt, was PrepareRestored geändert hat (für Ausgabe und
// Audit; nie Inhalte).
type RestoreFixes struct {
	NewIdentity bool `json:"new_identity"`
	// CredentialRefs: Konten, deren Schlüsselbund-Verweis auf die neue
	// Kennung zeigt (die Einträge des Originals werden nie gelesen).
	CredentialRefs int `json:"credential_refs"`
	// CredentialRefsLeft: gelöschte Merkliste alter Einträge des Originals
	// (meta.credential_refs_left), 0 oder 1.
	CredentialRefsLeft int `json:"credential_refs_left"`
	// AuditExports: Exportnachweise, deren Ort geleert wurde.
	AuditExports int `json:"audit_exports"`
	// Jobs: offene Jobs, die auf den Server schreiben würden (sync,
	// draft_push, mailbox_action), jetzt failed.
	Jobs int `json:"jobs"`
	// Actions: eingereihte oder laufende Postfach-Aktionen, jetzt
	// abgeschlossen (failed bzw. partial, Fehler restored).
	Actions int `json:"actions"`
	// Requests: offene Anträge (requested), jetzt abgelehnt.
	Requests int `json:"requests"`
	// DraftsHeld: wartende Entwürfe, die der Push zurückhält
	// (push_hold = restored), bis der Mensch sie ändert oder vorlegt.
	DraftsHeld int `json:"drafts_held"`
	// PushAttempts: offene Ablageversuche, gelöscht.
	PushAttempts int `json:"push_attempts"`
	// AuditStarts: Start-Einträge ohne Abschluss (Aufrufe, die beim
	// Schnappschuss liefen, z. B. backup.create selbst), jetzt mit
	// Abschluss error/restored.
	AuditStarts int `json:"audit_starts"`
}

var restoreDataDirIDRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// restoreJobTypes sind die Job-Arten, die auf den Server schreiben bzw.
// einen Lauf mit Zugangsdaten auslösen (ADR-009). index, extract, import
// und export arbeiten nur lokal und bleiben.
var restoreJobTypes = []any{JobDraftPush, JobMailboxAction, JobSync}

// PrepareRestored macht eine eben wiederhergestellte Datenbank zu einer
// eigenen Instanz (A10). Nichts darf danach still auf einen Server
// schreiben, und nichts zeigt mehr auf Schlüsselbund-Einträge oder den
// Exportort des Originals:
//
//  1. Eingereihte und laufende Postfach-Aktionen werden abgeschlossen
//     (FinishAction mit Fehler restored: offene Ziele failed, lokal
//     geänderte Flags gelten wieder wie vorher); offene Anträge werden
//     abgelehnt (Freigabe rejected, Grund restored).
//  2. In einer Transaktion: neue meta.datadir_id und Verweise der Konten
//     auf die neue Kennung (ohne DataDirID unverändert), Merkliste alter
//     Einträge gelöscht; Exportorte geleert; offene Jobs sync, draft_push
//     und mailbox_action failed; offene Ablageversuche gelöscht und
//     wartende Entwürfe zurückgehalten; offene Audit-Starts abgeschlossen;
//     zuletzt das Systemereignis (p.Event).
//
// Schritt 1 läuft in eigenen Transaktionen (FinishAction); scheitert
// danach etwas, verwirft der Aufrufer das ganze Verzeichnis (restore legt
// es neu an und entfernt es bei jedem Fehler).
func (s *SQLite) PrepareRestored(ctx context.Context, p RestorePrep) (RestoreFixes, error) {
	var fx RestoreFixes
	if p.DataDirID != "" && (!restoreDataDirIDRE.MatchString(p.DataDirID) || p.CredentialRef == nil) {
		return fx, errors.New("store: restore: invalid data directory id")
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	ms := now.UnixMilli()

	// 1. Aktionen.
	open, err := s.restoreOpenActions(ctx)
	if err != nil {
		return fx, err
	}
	for _, id := range open {
		if _, err := s.FinishAction(ctx, id, nil, RestoreReason, now); err != nil {
			return fx, fmt.Errorf("store: restore: action %d: %w", id, err)
		}
		fx.Actions++
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fx, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	exec := func(q string, args ...any) (int, error) {
		res, err := tx.ExecContext(ctx, q, args...)
		if err != nil {
			return 0, fmt.Errorf("store: restore: %w", err)
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}

	// Offene Anträge: Freigabe und Aktion abgelehnt.
	if _, err = exec(`UPDATE approvals SET decision = 'rejected', decided_by = 'system', decided_at = ?, reason = ?
		WHERE decision = 'pending' AND id IN (SELECT approval_id FROM actions WHERE state = 'requested' AND approval_id IS NOT NULL)`, ms, RestoreReason); err != nil {
		return fx, err
	}
	if fx.Requests, err = exec(`UPDATE actions SET state = 'rejected', error = ?, updated_at = ?, done_at = ?
		WHERE state = 'requested'`, RestoreReason, ms, ms); err != nil {
		return fx, err
	}

	// Merker „wiederhergestellt“ (dritte Nachbesserung T3, Punkt 5), auch
	// bei --keep-identity: Für den Audit-Export gilt die Altbestand-Regel
	// (Manifest ohne Kennung) dann nicht mehr (ops.checkManifestOwner).
	if _, err = exec(`INSERT INTO meta(key, value) VALUES ('restored_at', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.FormatInt(ms, 10)); err != nil {
		return fx, err
	}

	// 2. Kennung und Schlüsselbund-Verweise.
	if p.DataDirID != "" {
		fx.NewIdentity = true
		if _, err = exec(`INSERT INTO meta(key, value) VALUES ('datadir_id', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, p.DataDirID); err != nil {
			return fx, err
		}
		type acc struct {
			id        int64
			name, ref string
		}
		var accs []acc
		rows, qerr := tx.QueryContext(ctx, `SELECT id, name, credential_ref FROM accounts ORDER BY id`)
		if qerr != nil {
			err = qerr
			return fx, err
		}
		for rows.Next() {
			var a acc
			if err = rows.Scan(&a.id, &a.name, &a.ref); err != nil {
				_ = rows.Close()
				return fx, err
			}
			accs = append(accs, a)
		}
		if err = rows.Close(); err != nil {
			return fx, err
		}
		for _, a := range accs {
			want := p.CredentialRef(p.DataDirID[:8], a.name)
			if want == a.ref {
				continue
			}
			if _, err = exec(`UPDATE accounts SET credential_ref = ? WHERE id = ?`, want, a.id); err != nil {
				return fx, err
			}
			fx.CredentialRefs++
		}
		if fx.CredentialRefsLeft, err = exec(`DELETE FROM meta WHERE key = ?`, metaCredentialRefsLeft); err != nil {
			return fx, err
		}
	}

	// Exportorte gehören zum Original.
	if fx.AuditExports, err = exec(`UPDATE audit_exports SET location = '' WHERE location != ''`); err != nil {
		return fx, err
	}
	// Ein Merker eines abgebrochenen Exports (Nachbesserung T3, Punkt 1)
	// zeigt auf einen Ort des Originals.
	if _, err = exec(`DELETE FROM meta WHERE key = ?`, metaAuditExportPending); err != nil {
		return fx, err
	}

	// Jobs, die auf den Server schreiben würden.
	if fx.Jobs, err = exec(`UPDATE jobs SET state = 'failed', last_error = ?, locked_by = NULL, locked_at = NULL
		WHERE state IN ('queued','running') AND type IN (?,?,?)`, append([]any{RestoreReason}, restoreJobTypes...)...); err != nil {
		return fx, err
	}

	// Entwürfe: offene Ablageversuche weg, wartende Kopien zurückhalten.
	if fx.PushAttempts, err = exec(`UPDATE drafts SET push_attempt = NULL WHERE push_attempt IS NOT NULL`); err != nil {
		return fx, err
	}
	if fx.DraftsHeld, err = exec(`UPDATE drafts SET push_hold = ? WHERE `+draftPushPending, RestoreReason); err != nil {
		return fx, err
	}

	// Offene Audit-Starts: Ihr Prozess läuft in diesem Datenverzeichnis nie.
	if fx.AuditStarts, err = exec(`INSERT INTO audit_events(ts, actor_type, api_key_id, scope, operation, params_redacted,
		targets, result, reason, dry_run, duration_ms, policy_version, ref_id)
		SELECT ?, a.actor_type, a.api_key_id, a.scope, a.operation, a.params_redacted, a.targets, 'error', ?,
		a.dry_run, NULL, a.policy_version, a.id FROM audit_events a
		WHERE a.result = 'started' AND NOT EXISTS (SELECT 1 FROM audit_events c WHERE c.ref_id = a.id)
		ORDER BY a.id`, ms, RestoreReason); err != nil {
		return fx, err
	}

	if p.Event != nil {
		x := &DraftTx{ctx: ctx, tx: tx}
		if _, err = x.AppendAudit(p.Event(fx)); err != nil {
			return fx, err
		}
	}
	err = tx.Commit()
	return fx, err
}

// restoreOpenActions liefert die eingereihten und laufenden Aktionen.
func (s *SQLite) restoreOpenActions(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM actions WHERE state IN ('queued','running') ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: restore: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DataDirIDFull liefert die volle Kennung des Datenverzeichnisses (32 Hex;
// für Tests und restore).
func (s *SQLite) DataDirIDFull(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'datadir_id'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoDataDirID
	}
	return v, err
}

// Restored meldet, ob diese Datenbank aus einer Sicherung wiederhergestellt
// wurde (meta.restored_at, seit der dritten Nachbesserung T3).
func (s *SQLite) Restored(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM meta WHERE key = 'restored_at'`).Scan(&n)
	return n > 0, err
}
