package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T1b, A-F9 (b): doctor blendet offene Starts von Sync und
// Import nur für das Konto aus, dessen Sperre gehalten wird. Der tote Start
// von Konto B zählt, während Konto A synct.
func TestAF9DoctorCountsDeadStartPerAccount(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if code, _, errs := run(t, "key", "create", "--name", "probe"); code != ExitOK {
		t.Fatalf("key create: %d %s", code, errs)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ids := map[string]int64{}
	for _, name := range []string{"konto-a", "konto-b"} {
		id, err := st.CreateAccount(ctx, store.Account{Name: name, Email: name + "@example.org", Host: "127.0.0.1", Port: 1, TLS: "none",
			Username: name, AuthType: "password", CredentialRef: "account/" + name, InsecureTestServer: true})
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		if err := st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, targets, result)
			VALUES (?, 'human', 'cli', 'sync.run', ?, 'started')`, time.Now().Add(-time.Hour).UnixMilli(), `["account:`+name+`"]`); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()
	// Konto A synct (Sperre gehalten), Konto B hat ebenfalls eine Sperrdatei.
	l, err := flock.TryLock(filepath.Join(dataDir, flock.AccountLockName(ids["konto-a"])))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if lb, err := flock.TryLock(filepath.Join(dataDir, flock.AccountLockName(ids["konto-b"]))); err != nil {
		t.Fatal(err)
	} else {
		lb.Release()
	}
	_, out, _ := run(t, "doctor", "--json")
	var r DoctorReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if r.AuditUnfinished != 1 {
		t.Fatalf("audit_unfinished_calls = %d, want 1 (only account B)", r.AuditUnfinished)
	}
}
