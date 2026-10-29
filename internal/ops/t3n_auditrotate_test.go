package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T3, Punkt 3a: Zwei Rotationen planen denselben Bereich.
// Committet Lauf A, nachdem Lauf B seinen Plan gelesen hat, darf B das
// Archiv von A nicht als Abbruchrest löschen.
type t3nRotateHookRepo struct {
	*store.SQLite
	hook func()
}

func (r *t3nRotateHookRepo) AuditArchivePath(file string) string {
	if r.hook != nil {
		h := r.hook
		r.hook = nil
		h() // Lauf A läuft genau jetzt vollständig durch
	}
	return r.SQLite.AuditArchivePath(file)
}

func TestT3NConcurrentRotateKeepsCommittedArchive(t *testing.T) {
	e := newRotEnv(t)
	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	if v := e.verify(t); v.Status != "ok" {
		t.Fatalf("verify = %+v", v)
	}
	ctx := context.Background()
	var archiveA string
	repoB := &t3nRotateHookRepo{SQLite: e.st}
	repoB.hook = func() {
		out, err := AuditRotate(e.st, 365*24*time.Hour).Handler(ctx, Call{Input: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatalf("rotation A: %v", err)
		}
		archiveA = out.(AuditRotateOutput).Archive
	}
	_, errB := AuditRotate(repoB, 365*24*time.Hour).Handler(ctx, Call{Input: json.RawMessage(`{}`)})
	if !errors.Is(errB, ErrAuditRotateRunning) {
		t.Errorf("rotation B = %v, want ErrAuditRotateRunning", errB)
	}
	if archiveA == "" {
		t.Fatal("rotation A did not run")
	}
	if _, err := os.Stat(archiveA); err != nil {
		t.Errorf("archive of the committed rotation A is gone: %v", err)
	}
	if v := e.verify(t); v.Status != "ok" {
		var ps []string
		for _, p := range v.Problems {
			ps = append(ps, p.Day+":"+p.Problem)
		}
		t.Errorf("verify after both rotations = %s (%s)", v.Status, strings.Join(ps, ", "))
	}
}

// Punkt 3a: Hält ein anderer Lauf die Sperre, bricht die Rotation ab, ohne
// etwas zu schreiben oder zu löschen.
func TestT3NRotateLock(t *testing.T) {
	e := newRotEnv(t)
	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	l, err := flock.TryLock(filepath.Join(filepath.Dir(e.st.Path()), "audit-rotate.lock"))
	if err != nil {
		t.Fatal(err)
	}
	before := e.rows(t)
	if _, err := e.run(t, "audit.rotate", `{}`, false); !errors.Is(err, ErrAuditRotateRunning) {
		t.Fatalf("rotate while another runs = %v, want ErrAuditRotateRunning", err)
	}
	if got := e.rows(t); got != before+2 { // Start und Abschluss des Aufrufs
		t.Errorf("audit rows %d -> %d: the refused rotation moved rows", before, got)
	}
	l.Release()
	out, err := e.run(t, "audit.rotate", `{}`, false)
	if err != nil || out.(AuditRotateOutput).Status != "rotated" {
		t.Fatalf("rotate after the other one ended = %+v, %v", out, err)
	}
}
