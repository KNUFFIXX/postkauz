package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.5: Job draft_push. Erledigt, wenn nichts mehr wartet; bei
// Verbindungsfehlern sofort wieder fällig ohne Fehlerzählung (der nächste
// Sync holt nach); andere Fehler mit Wartezeit.
func TestRunDraftPush(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()
	db := f.st.DBForTest()
	if _, err := db.Exec(`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_state,created_at,updated_at)
		VALUES (?,'new','pk.1@example.org','draft','pending',1,1)`, f.acct); err != nil {
		t.Fatal(err)
	}
	job := func() (state string, attempts int, runAt int64) {
		t.Helper()
		if err := db.QueryRow(`SELECT state, attempts, run_at FROM jobs WHERE type = 'draft_push' ORDER BY id DESC LIMIT 1`).
			Scan(&state, &attempts, &runAt); err != nil {
			t.Fatal(err)
		}
		return
	}
	r := &Runner{Store: f.st, TakeOver: true}
	calls := 0
	push := func(err error, done bool) func(context.Context) error {
		return func(context.Context) error {
			calls++
			if done {
				if _, e := db.Exec(`UPDATE drafts SET remote_state = 'pushed'`); e != nil {
					t.Fatal(e)
				}
			}
			return err
		}
	}
	// Kein Job: push läuft nicht.
	if ran, err := r.RunDraftPush(ctx, f.acct, push(nil, true)); ran || err != nil || calls != 0 {
		t.Fatalf("without job: ran %v, %v, calls %d", ran, err, calls)
	}
	if _, err := f.st.EnsureDraftPushJob(ctx, f.acct, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Verbindungsfehler: zurück, sofort fällig, keine Fehlerzählung.
	connErr := fmt.Errorf("%w: test", provider.ErrConnection)
	if ran, err := r.RunDraftPush(ctx, f.acct, push(connErr, false)); !ran || !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("connection error: ran %v, %v", ran, err)
	}
	if st, n, runAt := job(); st != "queued" || n != 0 || runAt > time.Now().UnixMilli() {
		t.Fatalf("after connection error: %s, %d attempts, due %d", st, n, runAt)
	}
	// Anderer Fehler: Fehler des Jobs mit Wartezeit.
	if ran, err := r.RunDraftPush(ctx, f.acct, push(errors.New("no_drafts_mailbox: test"), false)); !ran || err == nil {
		t.Fatalf("other error: ran %v, %v", ran, err)
	}
	if st, n, runAt := job(); st != "queued" || n != 1 || runAt <= time.Now().UnixMilli() {
		t.Fatalf("after error: %s, %d attempts, due %d", st, n, runAt)
	}
	// Noch nicht fällig: push läuft nicht.
	calls = 0
	if ran, _ := r.RunDraftPush(ctx, f.acct, push(nil, true)); ran || calls != 0 {
		t.Fatal("job ran before it was due")
	}
	if _, err := db.Exec(`UPDATE jobs SET run_at = 0`); err != nil {
		t.Fatal(err)
	}
	// Erfolg, aber der Entwurf wartet noch (z. B. inzwischen geändert):
	// der Job bleibt fällig.
	if ran, err := r.RunDraftPush(ctx, f.acct, push(nil, false)); !ran || err != nil {
		t.Fatalf("success with pending drafts: %v, %v", ran, err)
	}
	if st, n, _ := job(); st != "queued" || n != 0 {
		t.Fatalf("after success with pending drafts: %s, %d", st, n)
	}
	if ran, err := r.RunDraftPush(ctx, f.acct, push(nil, true)); !ran || err != nil {
		t.Fatalf("success: %v, %v", ran, err)
	}
	if st, _, _ := job(); st != "done" {
		t.Fatalf("after success: %s", st)
	}
	var open int
	if err := db.QueryRow(`SELECT count(*) FROM jobs WHERE type = ? AND state IN ('queued','running')`, store.JobDraftPush).Scan(&open); err != nil || open != 0 {
		t.Fatalf("open jobs = %d, %v", open, err)
	}
}
