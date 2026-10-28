package importer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// T2 Punkt 7 (i): Der Import holt die Kontosperre vor dem Finden der
// Quellen (Discover kann bei großen Quellen dauern; solange sähe ein
// anderer Lauf seinen offenen Start als tot an). Belegt: sync_running,
// bevor der Pfad überhaupt gelesen wird; der Trockenlauf sperrt nicht.
func TestImportLocksBeforeDiscover(t *testing.T) {
	e := newEnv(t)
	r := e.importRunner(t)
	l, err := flock.TryLock(filepath.Join(r.LockDir, flock.AccountLockName(e.acct)))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	a, err := e.st.AccountByName(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "gibt-es-nicht")
	_, err = r.Import(context.Background(), a, ops.ImportOptions{Format: "mbox", Path: missing}, nil)
	if !errors.Is(err, ops.ErrSyncRunning) {
		t.Fatalf("err = %v, want sync_running before the path is read", err)
	}
	var ie *ops.InputError
	if _, err := r.Import(context.Background(), a, ops.ImportOptions{Format: "mbox", Path: missing, DryRun: true}, nil); !errors.As(err, &ie) {
		t.Fatalf("dry run: err = %v, want input error", err)
	}
}
