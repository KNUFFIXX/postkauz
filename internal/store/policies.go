package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Standardrichtlinie in der Tabelle policies (A02 E8, Migration 0010).
// Jeder Prozess trägt beim Start die geladene Policy-Datei ein: Gleicht sie
// der zuletzt eingetragenen (gleicher SHA-256 der Datei, gleiches
// Dokument), bleibt deren Version; sonst entsteht eine neue Zeile mit der
// nächsten Version. So zeigt policy_version im Audit, unter welcher Fassung
// eine Entscheidung fiel, auch wenn die Datei zwischendurch zurückgeändert
// wurde (A → B → A ergibt die Versionen 1, 2, 3).

// PolicyRecord ist die zuletzt eingetragene Standardrichtlinie.
type PolicyRecord struct {
	Version   int64
	SHA256    string // "" = keine Datei (eingebaute Standards)
	Document  string
	CreatedAt time.Time
}

// RecordPolicy trägt die Standardrichtlinie ein (siehe oben) und liefert
// ihre Version. sha256 "" heißt: keine Datei, eingebaute Standards.
func (s *SQLite) RecordPolicy(ctx context.Context, sha256, document string, now time.Time) (int64, error) {
	var version int64
	err := s.DraftTx(ctx, func(x *DraftTx) error {
		last, ok, err := latestPolicy(ctx, x.tx)
		if err != nil {
			return err
		}
		if ok && last.SHA256 == sha256 && last.Document == document {
			version = last.Version
			return nil
		}
		return x.tx.QueryRowContext(ctx, `INSERT INTO policies(api_key_id, document, version, sha256, created_at)
			VALUES (NULL, ?, (SELECT COALESCE(MAX(version), 0) + 1 FROM policies WHERE api_key_id IS NULL), ?, ?)
			RETURNING version`, document, nullStr(sha256), now.UnixMilli()).Scan(&version)
	})
	return version, err
}

// LatestPolicy liefert die zuletzt eingetragene Standardrichtlinie (ok =
// false: noch keine). Nur lesend (doctor).
func (s *SQLite) LatestPolicy(ctx context.Context) (PolicyRecord, bool, error) {
	return latestPolicy(ctx, s.db)
}

func latestPolicy(ctx context.Context, q querier) (PolicyRecord, bool, error) {
	var r PolicyRecord
	var created int64
	err := q.QueryRowContext(ctx, `SELECT version, COALESCE(sha256, ''), document, created_at FROM policies
		WHERE api_key_id IS NULL ORDER BY id DESC LIMIT 1`).Scan(&r.Version, &r.SHA256, &r.Document, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.CreatedAt = unixMilliUTC(created)
	return r, true, nil
}
