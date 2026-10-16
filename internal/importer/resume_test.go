package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// helperEnv startet den Kindprozess für den kill-9-Test.
const helperEnv = "POSTKAUZ_IMPORT_KILL_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runKillHelper())
	}
	os.Exit(m.Run())
}

// runKillHelper importiert langsam (kleine Stapel, Pause je Stapel), bis
// der Elternprozess ihn mit SIGKILL beendet.
func runKillHelper() int {
	st, err := store.Open(os.Getenv("PK_DB"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	b, err := blob.Open(os.Getenv("PK_BLOBS"), blob.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	acct, _ := strconv.ParseInt(os.Getenv("PK_ACCOUNT"), 10, 64)
	srcs, _, err := Discover(os.Getenv("PK_DIR"), FormatMbox, "", store.ImportContent)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	eng := &Engine{Store: st, Blobs: b, AccountID: acct, BatchMessages: 20,
		AfterBatch: func(Source, int64) error { time.Sleep(40 * time.Millisecond); return nil }}
	if _, err := eng.Run(context.Background(), Options{Sources: srcs}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// checkComplete: Nach Wiederaufnahme ist der Bestand vollständig und ohne
// Doppelte, jede Nachricht in ihren Ordnern.
func (e *env) checkComplete(t testing.TB, exp corpus.ImportExpect) {
	t.Helper()
	if n := e.count(t, `SELECT count(*) FROM messages`); n != exp.New {
		t.Fatalf("messages = %d, want %d", n, exp.New)
	}
	if n := e.count(t, `SELECT count(*) FROM (SELECT message_id_header FROM messages WHERE message_id_header IS NOT NULL
		GROUP BY message_id_header HAVING count(*) > 1)`); n != 0 {
		t.Fatalf("%d duplicate Message-IDs", n)
	}
	links := 0
	for _, mids := range exp.Folders {
		links += len(mids)
	}
	if n := e.count(t, `SELECT count(*) FROM message_import_folders mf JOIN messages m ON m.id = mf.message_id
		WHERE m.message_id_header IS NOT NULL`); n != links {
		t.Fatalf("folder links = %d, want %d", n, links)
	}
	if n := e.count(t, `SELECT count(*) FROM import_sources WHERE state != 'done'`); n != 0 {
		t.Fatalf("%d sources not done", n)
	}
}

// Harter Abbruch (kill -9) mitten im Import: Der nächste Lauf setzt an der
// gespeicherten Position fort, ohne Doppelte (A01 S1.10).
func TestImportResumesAfterKill9(t *testing.T) {
	skipShort(t) // voller Korpus und Kindprozess
	c := standardCorpus(t)
	dir := filepath.Join(t.TempDir(), "tb")
	exp, err := c.WriteThunderbird(dir, corpus.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "PK_DB="+e.dbPath, "PK_BLOBS="+filepath.Join(e.dir, "blobs"),
		"PK_DIR="+dir, "PK_ACCOUNT="+strconv.FormatInt(e.acct, 10))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	var seen int
	for time.Now().Before(deadline) {
		seen = e.count(t, `SELECT count(*) FROM messages`)
		if seen >= 150 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil { // SIGKILL (Windows: TerminateProcess)
		t.Fatal(err)
	}
	_ = cmd.Wait()
	before := e.count(t, `SELECT count(*) FROM messages`)
	if seen < 150 || before >= exp.New {
		t.Fatalf("child imported %d before the kill (stderr: %s)", before, stderr.String())
	}
	running := e.count(t, `SELECT count(*) FROM import_sources WHERE state = 'running' AND position > 0`)
	t.Logf("killed after %d messages (%d sources running)", before, running)
	if running != 1 {
		t.Fatalf("running sources with a position = %d, want 1", running)
	}

	rep := e.run(t, dir, FormatMbox, "", Options{})
	resumed := false
	for _, s := range rep.Sources {
		resumed = resumed || s.ResumedAt > 0
	}
	if !resumed || rep.Errors != 0 {
		t.Fatalf("restart did not resume: %+v", rep.Sources)
	}
	if rep.New != exp.New-before {
		t.Errorf("restart imported %d new, want %d", rep.New, exp.New-before)
	}
	e.checkComplete(t, exp)
}

// Abbruch im Prozess (wie Strg+C zwischen zwei Stapeln) und Maildir-Cursor.
func TestImportResumesMaildirAfterAbort(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	dir := filepath.Join(t.TempDir(), "Mail")
	exp, err := c.WriteMaildir(dir, corpus.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	srcs, _, err := Discover(dir, FormatMaildir, "", store.ImportContent)
	if err != nil {
		t.Fatal(err)
	}
	batches := 0
	eng := e.engine()
	eng.BatchMessages = 50
	eng.AfterBatch = func(Source, int64) error {
		batches++
		if batches == 3 {
			return errors.New("simulated crash")
		}
		return nil
	}
	if _, err := eng.Run(context.Background(), Options{Sources: srcs}); !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
	before := e.count(t, `SELECT count(*) FROM messages`)
	if before != 150 {
		t.Fatalf("committed before the abort = %d, want 150", before)
	}
	rep := e.run(t, dir, FormatMaildir, "", Options{})
	var resumed int64
	for _, s := range rep.Sources {
		resumed += s.ResumedAt
	}
	if resumed == 0 || rep.New != exp.New-before || rep.Errors != 0 {
		t.Fatalf("restart = new %d (want %d) resumed %d", rep.New, exp.New-before, resumed)
	}
	e.checkComplete(t, exp)
}
