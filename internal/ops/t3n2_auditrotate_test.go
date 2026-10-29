package ops

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Zweite Nachbesserung T3, Punkt 2: Export und Rotation gegeneinander
// gesperrt (Wegwerf-Test A3 der Prüfsitzung, angepasst).

// t3n2HookRepo ruft hook einmal auf, wenn der Export die Nachweise liest,
// also nachdem er die Archivliste geladen hat (openAuditSource) und bevor
// er abgeschlossene Tage liest.
type t3n2HookRepo struct {
	*store.SQLite
	hook func()
}

func (r *t3n2HookRepo) AuditExports(ctx context.Context) ([]store.AuditExport, error) {
	if h := r.hook; h != nil {
		r.hook = nil
		h()
	}
	return r.SQLite.AuditExports(ctx)
}

// t3n2NoLockRepo ist ein Store ohne Sperrdateien (wie eine ältere
// Programmversion ohne Sperre oder ein Lauf, der sie umgeht).
type t3n2NoLockRepo struct{ *store.SQLite }

func (t3n2NoLockRepo) Path() string { return "" }

func TestT3N2RotateDuringExportToNewFolder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		noLock bool // die Rotation umgeht die Sperre: die zweite Absicherung greift
	}{{"locked", false}, {"without-lock", true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			base := t.TempDir()
			dirA := filepath.Join(base, "export-a")
			dirB := filepath.Join(base, "export-b") // z. B. anderer Laufwerksbuchstabe
			now := time.Now().UTC()
			st := t3nOpen(t, filepath.Join(base, "d", "postkauz.db"))
			t3nEvent(t, st, now.Add(-72*time.Hour), "sync.run")
			t3nEvent(t, st, now.Add(-48*time.Hour), "sync.run")
			t3nEvent(t, st, now.Add(-time.Minute), "sync.run")
			if _, err := t3nExport(st, dirA); err != nil {
				t.Fatal(err)
			}
			if v := t3nVerify(t, st); v.Status != "ok" {
				t.Fatalf("verify before = %+v", v)
			}
			before, err := st.AuditExports(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var rotRepo AuditRepo = st
			if tc.noLock {
				rotRepo = t3n2NoLockRepo{st}
			}
			var rotErr error
			rotated := false
			repo := &t3n2HookRepo{SQLite: st}
			repo.hook = func() {
				res, err := AuditRotate(rotRepo, 36*time.Hour).Handler(ctx, Call{Input: json.RawMessage(`{}`)})
				rotErr = err
				rotated = err == nil && res.(AuditRotateOutput).Status == "rotated"
			}
			_, err = t3nExport(repo, dirB)
			if tc.noLock {
				if !rotated {
					t.Fatalf("rotation without lock did not run: %v", rotErr)
				}
				if !errors.Is(err, ErrAuditExportSourceChanged) {
					t.Errorf("export after a rotation inside its window = %v, want ErrAuditExportSourceChanged", err)
				}
			} else {
				if !errors.Is(rotErr, ErrAuditExportRunning) || rotated {
					t.Errorf("rotation while the export runs = %v (rotated %v), want ErrAuditExportRunning", rotErr, rotated)
				}
				if err != nil {
					t.Errorf("export = %v", err)
				}
			}
			after, err := st.AuditExports(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) {
				t.Fatalf("records %d -> %d", len(before), len(after))
			}
			for i := range before {
				if after[i].FirstEventID != before[i].FirstEventID || after[i].LastEventID != before[i].LastEventID {
					t.Errorf("record %s overwritten: ids %d..%d -> %d..%d", before[i].Day, before[i].FirstEventID, before[i].LastEventID,
						after[i].FirstEventID, after[i].LastEventID)
				}
			}
			if v := t3nVerify(t, st); v.Status != "ok" {
				t.Errorf("verify after = %+v", v)
			}
			// Danach geht beides wieder: Export in den neuen Ordner, Prüfung.
			if _, err := t3nExport(st, dirB); err != nil {
				t.Errorf("export to the new folder afterwards = %v", err)
			}
			if v := t3nVerify(t, st); v.Status != "ok" {
				t.Errorf("verify after the second export = %+v", v)
			}
		})
	}
}

// Punkt 2: Die Rotation holt audit-export.lock (nicht im Trockenlauf);
// läuft ein Export, verschiebt sie nichts.
func TestT3N2RotateRefusedWhileExportRuns(t *testing.T) {
	e := newRotEnv(t)
	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockBesideDB(e.st, AuditExportLockName, ErrAuditExportRunning)
	if err != nil {
		t.Fatal(err)
	}
	before := e.rows(t)
	if _, err := e.run(t, "audit.rotate", `{}`, true); err != nil {
		t.Errorf("dry run while an export runs = %v", err)
	}
	if _, err := e.run(t, "audit.rotate", `{}`, false); !errors.Is(err, ErrAuditExportRunning) {
		t.Fatalf("rotate while an export runs = %v, want ErrAuditExportRunning", err)
	}
	if got := e.rows(t); got != before+4 { // je Aufruf Start und Abschluss
		t.Errorf("audit rows %d -> %d: the refused rotation moved rows", before, got)
	}
	unlock()
	out, err := e.run(t, "audit.rotate", `{}`, false)
	if err != nil || out.(AuditRotateOutput).Status != "rotated" {
		t.Fatalf("rotate after the export ended = %+v, %v", out, err)
	}
}
