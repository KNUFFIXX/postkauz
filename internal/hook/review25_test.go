package hook

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Gegenprüfung S2.4/S2.5: K1 (liegen gebliebene Temp-Verzeichnisse, Hook
// nach SIGKILL der CLI, Löschfehler ins Log ohne Pfad), K2 (Stapeldatei
// mit Punkt oder Leerzeichen am Ende), K9 (übrig gebliebene Kinder nach
// einem normalen Ende).

// K2: Windows entfernt Punkte und Leerzeichen am Ende eines Dateinamens;
// „lint.bat." startet lint.bat über cmd.exe. Das zählt als Stapeldatei.
func TestReviewK2BatchWithTrailingDotOrSpace(t *testing.T) {
	for _, c := range []struct {
		name  string
		batch bool
	}{
		{"lint.bat", true}, {"lint.bat.", true}, {"lint.bat ", true}, {"lint.CMD. .", true}, {`C:\hooks\lint.cmd...`, true},
		{"lint.exe", false}, {"lint.exe.", false}, {"lint", false}, {"batch.ps1", false}, {"lint.bat.exe", false},
	} {
		if got := isBatch(c.name); got != c.batch {
			t.Errorf("isBatch(%q) = %v, want %v", c.name, got, c.batch)
		}
	}
	if runtime.GOOS == "windows" {
		return // Windows legt solche Namen gar nicht an
	}
	dir := t.TempDir()
	for _, name := range []string{"lint.bat.", "lint.cmd ", "lint.Bat. "} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveProgram(p); err == nil || !strings.Contains(err.Error(), "batch files") {
			t.Errorf("%q: err = %v", name, err)
		}
	}
}

// K1: Ein späterer Lauf löscht liegen gebliebene Temp-Verzeichnisse von
// Hooks (älter als eine Stunde, eigener Präfix, kein Symlink), sonst nichts.
func TestReviewK1CleanupStaleTempDirs(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	mk := func(name string, age time.Duration) string {
		t.Helper()
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(p, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, FileEML), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := mk(tempPrefix+"old1", 2*time.Hour)
	fresh := mk(tempPrefix+"fresh", 10*time.Minute)
	other := mk("other-old", 3*time.Hour)
	target := mk("ziel", 3*time.Hour)
	link := filepath.Join(base, tempPrefix+"link")
	if err := os.Symlink(target, link); err == nil {
		_ = os.Chtimes(link, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	}
	var buf bytes.Buffer
	cleanupStale(base, now, slog.New(slog.NewTextHandler(&buf, nil)))
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale directory still there: %v", err)
	}
	for _, p := range []string{fresh, other, target} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed: %v", filepath.Base(p), err)
		}
	}
	if !strings.Contains(buf.String(), "count=1") || strings.Contains(buf.String(), base) {
		t.Fatalf("log = %q", buf.String())
	}
}

// K1: Fehler beim Löschen stehen ohne Pfad im Log.
func TestReviewK1PathlessError(t *testing.T) {
	err := &fs.PathError{Op: "unlinkat", Path: "/home/nutzer/tmp/postkauz-hook-1/message.eml", Err: fs.ErrPermission}
	got := pathless(err)
	if strings.Contains(got, "nutzer") || strings.Contains(got, "postkauz-hook") || !strings.Contains(got, "unlinkat") {
		t.Fatalf("pathless = %q", got)
	}
}

// K9: Endet der Hook normal (Exit 0), sein Kind läuft aber weiter, wird das
// Kind beendet (es könnte sonst die Dateien weiter lesen), und das
// Temp-Verzeichnis ist weg.
func TestReviewK9NormalEndKillsLeftoverChildren(t *testing.T) {
	needChild(t)
	dir := t.TempDir()
	childPid := filepath.Join(dir, "child")
	res := run(t, Runner{Command: helperCmd(t, "spawn", childPid), Timeout: time.Minute}, sample)
	if res.Status != Pass {
		t.Fatalf("result %+v", res)
	}
	if runtime.GOOS == "windows" {
		return // Prozessprüfung nur mit Unix-Mitteln (alive_unix_test.go)
	}
	b, err := os.ReadFile(childPid)
	if err != nil {
		t.Fatalf("child did not start: %v", err)
	}
	p, _ := strconv.Atoi(string(b))
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(p) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still alive after the hook ended", p)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
