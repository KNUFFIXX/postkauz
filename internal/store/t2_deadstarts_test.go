package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// T2 Punkt 7 (i): CloseDeadStarts schließt nur Starts, die mindestens
// DeadStartAge alt sind (ein langsamer Lauf vor seiner Kontosperre bleibt
// offen und schreibt seinen Abschluss selbst), und einen Start mit mehreren
// Konten (sync --watch --all) nur, wenn keines der anderen Konten belegt ist.
func TestCloseDeadStartsAgeAndOtherAccounts(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	add := func(age time.Duration, targets string) int64 {
		t.Helper()
		var id int64
		if err := s.db.QueryRow(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result)
			VALUES (?, 'human', 'cli', 'sync.run', '{}', ?, 'started') RETURNING id`, now.Add(-age).UnixMilli(), targets).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	young := add(DeadStartAge-time.Minute, `["account:a"]`)     // langsamer Lauf, noch vor der Sperre
	old := add(DeadStartAge+time.Minute, `["account:a"]`)       // toter Lauf
	all := add(2*DeadStartAge, `["account:a","account:b"]`)     // --all, Konto b noch belegt
	allFree := add(2*DeadStartAge, `["account:a","account:c"]`) // --all, Konto c frei
	busy := func(name string) bool { return name == "b" }

	n, err := s.CloseDeadStarts(ctx, "a", 0, now, busy)
	if err != nil || n != 2 {
		t.Fatalf("closed = %d %v, want 2", n, err)
	}
	ends := map[int64]bool{}
	rows, err := s.AuditQuery(ctx, AuditFilter{Result: "error"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Reason != "process_ended" {
			t.Fatalf("end = %+v", r)
		}
		ends[r.RefID] = true
	}
	if ends[young] || !ends[old] || ends[all] || !ends[allFree] {
		t.Fatalf("closed starts = %v (young %d old %d all %d free %d)", ends, young, old, all, allFree)
	}
	// Der langsame Lauf kann seinen Abschluss danach selbst schreiben.
	if _, err := s.db.Exec(`INSERT INTO audit_events(ts, actor_type, operation, result, ref_id) VALUES (?, 'human', 'sync.run', 'ok', ?)`,
		now.UnixMilli(), young); err != nil {
		t.Fatalf("own end entry of the slow run: %v", err)
	}
	// Später (Konto b frei, alles älter): nichts doppelt, nur der --all-Start.
	n, err = s.CloseDeadStarts(ctx, "a", 0, now.Add(time.Hour), nil)
	if err != nil || n != 1 {
		t.Fatalf("second close = %d %v, want 1", n, err)
	}
}
