package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Dritte Nachbesserung T3, Punkt 3: Ein Audit-Ziel aus der Eingabe eines
// abgelehnten bzw. gescheiterten Aufrufs (Ergebnis error/denied, Thread
// bzw. Anhang gibt es nicht) setzte den Startwert in 0022 auf INT64_MAX;
// danach scheiterte jedes neue Thread- und Anhang-INSERT (nextIDExpr lief
// über). Jetzt zählen nur Ziele erfolgreicher Aufrufe zwischen 1 und 2^53-1.
func TestT3N3AuditTargetOverflowsFloor(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 19); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO threads(id,account_id,subject_norm) VALUES (1,1,'a')`,
		`INSERT INTO messages(id,account_id,thread_id,subject,created_at) VALUES (1,1,1,'a',1)`,
		// Agent (Rolle assistant) ruft threads.get / attachments.text mit
		// erfundener ID: nicht gefunden, Ergebnis error; Ziel steht trotzdem im Audit.
		`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'threads.get', 'error', '["thread:9223372036854775807"]')`,
		`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'attachments.text', 'error', '["attachment:99999999999999999999"]')`,
		`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'threads.get', 'denied', '["thread:500"]')`,
		// Erfolgreich, aber über 2^53-1 (älteres Programm ohne Obergrenze
		// im Schema; der alte Start-Eintrag hieß auch ok).
		`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'threads.get', 'ok', '["thread:9007199254740992"]')`,
		`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'attachments.text', 'ok', '["attachment:9223372036854775807"]')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ft, _ := s.QueryIntForTest(`SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'id_floor_threads'`)
	fa, _ := s.QueryIntForTest(`SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'id_floor_attachments'`)
	if ft != 1 || fa != 0 {
		t.Errorf("floors from failed or oversized audit targets: threads %d (want 1), attachments %d (want 0)", ft, fa)
	}
	if _, err := s.db.Exec(`INSERT INTO threads(id, account_id, subject_norm) VALUES (` + nextThreadID + `, 1, 'neu')`); err != nil {
		t.Errorf("new thread after migration: %v", err)
	}
	if err := upsertAttachments(ctx, s.db, 1, []NewAttachment{{PartID: "2", ContentType: "text/plain", Size: 1}}, 1, false); err != nil {
		t.Errorf("new attachment after migration: %v", err)
	}
}
