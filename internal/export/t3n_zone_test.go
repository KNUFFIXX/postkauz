package export

// Nachbesserung T3, Punkt 11c: Jede abgelegte Datei bekommt nach dem
// Schreiben die Herkunft „aus dem Internet“ (unter Windows der Datenstrom
// Zone.Identifier, siehe t3n_zone_windows_test.go); scheitert das, bleibt
// die Datei, es gibt nur eine Warnung (ohne Pfad).

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// markRecorder ersetzt markFile für einen Test und zeichnet die Pfade auf;
// fail != nil lässt jedes Setzen scheitern.
type markRecorder struct {
	mu    sync.Mutex
	paths []string
	fail  error
}

func recordMarks(t *testing.T, fail error) *markRecorder {
	t.Helper()
	r := &markRecorder{fail: fail}
	old := markFile
	markFile = func(path string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.paths = append(r.paths, path)
		return r.fail
	}
	t.Cleanup(func() { markFile = old })
	return r
}

// files liefert alle regulären Dateien unter dir (absolut, sortiert).
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func TestT3NEveryWrittenFileIsMarked(t *testing.T) {
	r := recordMarks(t, nil)
	dir := filepath.Join(t.TempDir(), "export")
	save(t, dir, SaveRequest{Filename: "Rechnung.pdf", Subdir: "belege"})
	save(t, dir, SaveRequest{Filename: "start.cmd"})
	a := newArchive()
	a.add("INBOX", 1, []byte("Subject: eins\r\n\r\nx\r\n"), Item{From: "a@example.org", Received: when})
	a.add("Archiv/2026", 2, []byte("Subject: zwei\r\n\r\ny\r\n"), Item{From: "b@example.org", Received: when})
	a.add("Archiv/2026", 3, nil, Item{})
	for _, format := range []string{FormatMbox, FormatEML} {
		if _, err := Run(context.Background(), a.options(format, filepath.Join(dir, format))); err != nil {
			t.Fatal(err)
		}
	}
	got := slices.Clone(r.paths)
	slices.Sort(got)
	if want := files(t, dir); !slices.Equal(got, want) {
		t.Errorf("marked files:\n%q\nwant every written file:\n%q", got, want)
	}
}

func TestT3NMarkFailureOnlyWarns(t *testing.T) {
	recordMarks(t, &os.PathError{Op: "open", Path: "/secret/place:Zone.Identifier", Err: errors.New("not supported")})
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, nil))
	dir := filepath.Join(t.TempDir(), "export")
	res := save(t, dir, SaveRequest{Filename: "Rechnung.pdf", Log: log})
	if !res.Unmarked {
		t.Errorf("attachment: Unmarked not reported")
	}
	if _, err := os.Stat(filepath.Join(dir, res.Path)); err != nil {
		t.Errorf("attachment removed after a failed mark: %v", err)
	}
	a := newArchive()
	a.add("INBOX", 1, []byte("Subject: eins\r\n\r\nx\r\n"), Item{From: "a@example.org", Received: when})
	a.add("INBOX", 2, []byte("Subject: zwei\r\n\r\ny\r\n"), Item{From: "a@example.org", Received: when})
	for format, want := range map[string]int{FormatMbox: 1, FormatEML: 3} { // EML: zwei Dateien und index.csv
		o := a.options(format, filepath.Join(dir, format))
		o.Log = log
		rep, err := Run(context.Background(), o)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if rep.Unmarked != want || rep.Messages != 2 {
			t.Errorf("%s: unmarked %d, messages %d; want %d, 2", format, rep.Unmarked, rep.Messages, want)
		}
	}
	out := logBuf.String()
	if n := strings.Count(out, "level=WARN"); n != 3 || !strings.Contains(out, "Zone.Identifier") || !strings.Contains(out, "not supported") {
		t.Errorf("want one warning per call (3), got:\n%s", out)
	}
	if strings.Contains(out, "/secret/place") || strings.Contains(out, "Rechnung") || strings.Contains(out, dir) {
		t.Errorf("warning names a path:\n%s", out)
	}
}
