package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// T3-Nachbesserung 3, Punkt 7: Scheitert im letzten Schritt von restore das
// Umbenennen (oder das Entfernen des leeren Ziels) endgültig, ist die Kopie
// schon fertig. Früher löschte restore sie (bei einem großen Archiv mehrere
// GB); jetzt bleibt der Arbeitsordner stehen, und die Meldung nennt ihn und
// den Befehl, der ihn an seinen Platz bringt. Hat sich das Ziel inzwischen
// gefüllt (backup_target), wird wie bisher aufgeräumt.
func TestT3N3RestoreKeepsWorkDirWhenRenameFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	origID, err := e.st.DataDirIDFull(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name                   string
		failRemove, failRename int
		targetExists           bool
	}{
		{"rename keeps failing", 0, 100, false},
		{"removing the empty target keeps failing", 100, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			useFakeFS(t, c.failRemove, c.failRename)
			// Leerzeichen und Apostroph prüfen das Quoting des Befehls.
			to := filepath.Join(t.TempDir(), "neu dir's")
			if c.targetExists {
				if err := os.Mkdir(to, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Restore(ctx, RestoreOptions{From: from, DataDir: to})
			if err == nil || !errors.Is(err, errBusyFake) || errors.Is(err, ErrTarget) {
				t.Fatalf("restore with a failing move: err = %v", err)
			}
			left := RestoreLeftovers(to)
			if len(left) != 1 {
				t.Fatalf("work folder after the failed move: %v (err %v)", left, err)
			}
			work := left[0]
			msg := err.Error()
			if !strings.Contains(msg, `kept in "`+work+`"`) {
				t.Fatalf("the message does not name the work folder %s: %s", work, msg)
			}
			if _, serr := os.Stat(filepath.Join(work, brand.Name+".db")); serr != nil {
				t.Fatalf("database in the kept work folder: %v", serr)
			}
			_, cmd, ok := strings.Cut(msg, "put it in place with: ")
			if !ok {
				t.Fatalf("no command in the message: %s", msg)
			}
			want := "mv " + shQuote(work) + " " + shQuote(to)
			if c.targetExists {
				want = "rmdir " + shQuote(to) + " && " + want
			}
			if runtime.GOOS == "windows" {
				want = "Rename-Item -LiteralPath " + psQuote(work) + " -NewName " + psQuote(filepath.Base(to))
				if c.targetExists {
					want = "[System.IO.Directory]::Delete(" + psQuote(to) + "); " + want
				}
			}
			if cmd != want {
				t.Fatalf("command = %s, want %s", cmd, want)
			}
			if runtime.GOOS == "windows" || testing.Short() {
				return
			}
			// Der genannte Befehl bringt die Kopie an ihren Platz; sie ist
			// eine eigene Instanz (neue Kennung).
			if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
				t.Fatalf("%s: %v %s", cmd, err, out)
			}
			if left := RestoreLeftovers(to); len(left) != 0 {
				t.Fatalf("work folder after the command: %v", left)
			}
			st, err := store.Open(filepath.Join(to, brand.Name+".db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if id, err := st.DataDirIDFull(ctx); err != nil || id == origID {
				t.Fatalf("data directory id of the moved copy = %q (%v); the original has %q", id, err, origID)
			}
		})
	}
	t.Run("target no longer empty", func(t *testing.T) {
		useFakeFS(t, 0, 0)
		to := filepath.Join(t.TempDir(), "neu")
		if err := os.Mkdir(to, 0o700); err != nil {
			t.Fatal(err)
		}
		fsRemove = func(p string) error {
			if err := os.WriteFile(filepath.Join(to, "fremd.txt"), []byte("x"), 0o600); err != nil {
				return err
			}
			return os.Remove(p)
		}
		_, err := Restore(ctx, RestoreOptions{From: from, DataDir: to})
		if !errors.Is(err, ErrTarget) || strings.Contains(err.Error(), "kept in") {
			t.Fatalf("target filled during restore: err = %v", err)
		}
		if left := RestoreLeftovers(to); len(left) != 0 {
			t.Fatalf("work folder left although the target changed: %v", left)
		}
	})
}

// Punkt 7: der Befehl je Betriebssystem, unabhängig vom laufenden System.
func TestT3N3KeptCopyCommand(t *testing.T) {
	if got := psQuote("a'b\u2019c"); got != "'a''b\u2019\u2019c'" {
		t.Errorf("psQuote = %s", got)
	}
	base := t.TempDir()
	work, to := filepath.Join(base, "it's.restoring-0011aabb"), filepath.Join(base, "it's")
	errX := errors.New("rename failed")
	for _, c := range []struct {
		goos   string
		exists bool
		want   string
	}{
		{"windows", false, "Rename-Item -LiteralPath '" + strings.ReplaceAll(work, "'", "''") + "' -NewName 'it''s'"},
		{"linux", false, "mv '" + strings.ReplaceAll(work, "'", `'\''`) + "' '" + strings.ReplaceAll(to, "'", `'\''`) + "'"},
		{"windows", true, "[System.IO.Directory]::Delete('" + strings.ReplaceAll(to, "'", "''") + "'); Rename-Item"},
		{"darwin", true, "rmdir '" + strings.ReplaceAll(to, "'", `'\''`) + "' && mv "},
	} {
		if c.exists {
			if err := os.MkdirAll(to, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		err := keptCopyErr(errX, work, to, c.goos)
		if !errors.Is(err, errX) || !strings.Contains(err.Error(), `kept in "`+work+`"`) {
			t.Fatalf("%s: %v", c.goos, err)
		}
		_, cmd, _ := strings.Cut(err.Error(), "put it in place with: ")
		if !strings.HasPrefix(cmd, c.want) {
			t.Errorf("%s (target exists %v): command = %s, want prefix %s", c.goos, c.exists, cmd, c.want)
		}
	}
}
