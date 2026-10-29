package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errBusyFake steht für ERROR_SHARING_VIOLATION unter Windows (ein
// Virenscanner hält eine Datei kurz offen).
var errBusyFake = errors.New("The process cannot access the file because it is being used by another process (fake)")

// fakeFS ersetzt Rename, Remove und die Einstufung vorübergehender Fehler
// für einen Test: Die ersten fail Aufrufe der gewählten Funktion scheitern
// mit errBusyFake, danach läuft die echte.
type fakeFS struct {
	removeCalls, renameCalls int
}

func useFakeFS(t *testing.T, failRemove, failRename int) *fakeFS {
	t.Helper()
	f := &fakeFS{}
	origRename, origRemove, origTransient, origPauses := fsRename, fsRemove, transientFSErr, fsRetryPauses
	t.Cleanup(func() {
		fsRename, fsRemove, transientFSErr, fsRetryPauses = origRename, origRemove, origTransient, origPauses
	})
	fsRemove = func(p string) error {
		f.removeCalls++
		if f.removeCalls <= failRemove {
			return &os.PathError{Op: "remove", Path: p, Err: errBusyFake}
		}
		return os.Remove(p)
	}
	fsRename = func(a, b string) error {
		f.renameCalls++
		if f.renameCalls <= failRename {
			return &os.LinkError{Op: "rename", Old: a, New: b, Err: errBusyFake}
		}
		return os.Rename(a, b)
	}
	transientFSErr = func(err error) bool { return errors.Is(err, errBusyFake) }
	fsRetryPauses = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	return f
}

// workAndTarget legt einen Arbeitsordner mit einer Datei und ein leeres
// Ziel an.
func workAndTarget(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	work, to := filepath.Join(base, "neu.restoring-0011aabb"), filepath.Join(base, "neu")
	for _, d := range []string{work, to} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "postkauz.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	return work, to
}

// T3-Nachbesserung 2, Punkt 13: Unter Windows scheitern Remove und Rename
// im letzten Schritt von restore oft kurz (Virenscanner). Ein
// vorübergehender Fehler wird wiederholt; bleibt er, nennt die Meldung den
// echten Fehler statt „no longer empty“. Nicht vorübergehende Fehler und ein
// wirklich nicht mehr leeres Ziel werden nicht wiederholt.
func TestT3N2MoveIntoPlaceRetriesTransientErrors(t *testing.T) {
	t.Run("retried until it works", func(t *testing.T) {
		f := useFakeFS(t, 2, 3)
		work, to := workAndTarget(t)
		if err := moveIntoPlace(work, to); err != nil {
			t.Fatalf("moveIntoPlace with transient errors: %v", err)
		}
		if f.removeCalls != 3 || f.renameCalls != 4 {
			t.Fatalf("calls: remove %d, rename %d; want 3 and 4", f.removeCalls, f.renameCalls)
		}
		if _, err := os.Stat(filepath.Join(to, "postkauz.db")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("remove keeps failing", func(t *testing.T) {
		f := useFakeFS(t, 100, 0)
		work, to := workAndTarget(t)
		err := moveIntoPlace(work, to)
		if err == nil || !errors.Is(err, errBusyFake) || strings.Contains(err.Error(), "no longer empty") || errors.Is(err, ErrTarget) {
			t.Fatalf("remove keeps failing: err = %v; want the real error", err)
		}
		if f.removeCalls != 1+len(fsRetryPauses) || f.renameCalls != 0 {
			t.Fatalf("calls: remove %d, rename %d", f.removeCalls, f.renameCalls)
		}
	})
	t.Run("rename keeps failing", func(t *testing.T) {
		f := useFakeFS(t, 0, 100)
		work, to := workAndTarget(t)
		err := moveIntoPlace(work, to)
		if err == nil || !errors.Is(err, errBusyFake) {
			t.Fatalf("rename keeps failing: err = %v", err)
		}
		if f.renameCalls != 1+len(fsRetryPauses) {
			t.Fatalf("rename calls %d", f.renameCalls)
		}
	})
	t.Run("not transient", func(t *testing.T) {
		f := useFakeFS(t, 100, 0)
		transientFSErr = func(error) bool { return false }
		work, to := workAndTarget(t)
		if err := moveIntoPlace(work, to); !errors.Is(err, errBusyFake) || f.removeCalls != 1 {
			t.Fatalf("non-transient error: err = %v after %d calls", err, f.removeCalls)
		}
	})
	t.Run("really not empty", func(t *testing.T) {
		f := useFakeFS(t, 0, 0)
		work, to := workAndTarget(t)
		if err := os.WriteFile(filepath.Join(to, "fremd.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := moveIntoPlace(work, to)
		if !errors.Is(err, ErrTarget) || !strings.Contains(err.Error(), "no longer empty") || f.removeCalls != 1 || f.renameCalls != 0 {
			t.Fatalf("not empty: err = %v, calls %d/%d", err, f.removeCalls, f.renameCalls)
		}
	})
	t.Run("default classifier", func(t *testing.T) {
		if transientFSErr(errBusyFake) || transientFSErr(os.ErrPermission) {
			t.Fatal("the default classifier retries an unknown error")
		}
	})
}

// Punkt 13 Ende-zu-Ende: restore in ein vorhandenes leeres Ziel, dessen
// Entfernen und das Umbenennen erst beim dritten Versuch gehen.
func TestT3N2RestoreRetriesTransientMoveErrors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	f := useFakeFS(t, 2, 2)
	to := filepath.Join(t.TempDir(), "neu")
	if err := os.Mkdir(to, 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := Restore(ctx, RestoreOptions{From: from, DataDir: to})
	if err != nil || res.Blobs != 3 {
		t.Fatalf("restore with transient errors: %+v %v", res, err)
	}
	if f.removeCalls != 3 || f.renameCalls != 3 {
		t.Fatalf("calls: remove %d, rename %d", f.removeCalls, f.renameCalls)
	}
	if left := RestoreLeftovers(to); len(left) != 0 {
		t.Fatalf("work folder left: %v", left)
	}
}
