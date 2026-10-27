package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// T1a Punkt 4 (A14): Ein Lauf, der vor Mitternacht UTC beginnt und danach
// endet (wie `sync --watch`), darf einen schon abgeschlossen exportierten
// Tag nicht verändern. Bis T1a trug sein Abschluss den Zeitstempel des
// Beginns und wanderte deshalb in den nächsten offenen Tag; seit A1
// (Migration 0018) trägt der Abschluss seinen eigenen Zeitstempel und
// landet ohnehin dort. audit verify bleibt ok.
func TestAuditLateEntryKeepsExportedDay(t *testing.T) {
	e := newAdminEnv(t)
	var mu sync.Mutex
	clock := time.Date(2026, 3, 9, 23, 50, 0, 0, time.UTC)
	e.x.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	setClock := func(t time.Time) { mu.Lock(); clock = t; mu.Unlock() }
	auditClock = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	t.Cleanup(func() { auditClock = time.Now })

	release := make(chan struct{})
	started := make(chan struct{})
	if err := e.x.Registry.Register(Operation{Name: "test.watch", Scope: policy.ScopeAdmin, Risk: policy.R0,
		Summary:      "long-running test operation",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			close(started)
			<-release
			return nil, errors.New("watch ended")
		}}); err != nil {
		t.Fatal(err)
	}
	e.run(t, adminKey, "keys.list", map[string]any{}) // Tag D: ein gewöhnlicher Eintrag
	done := make(chan error, 1)
	go func() {
		_, err := e.run(t, adminKey, "test.watch", map[string]any{})
		done <- err
	}()
	<-started
	// Nach Mitternacht: Einträge des neuen Tages, dann der tägliche Export.
	setClock(time.Date(2026, 3, 10, 0, 5, 0, 0, time.UTC))
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	dayD := filepath.Join(e.dir, auditFileName("2026-03-09"))
	before, err := os.ReadFile(dayD)
	if err != nil {
		t.Fatal(err)
	}
	// Der Lauf endet: Abschluss-Eintrag mit eigenem Zeitstempel (06:00).
	setClock(time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC))
	close(release)
	if err := <-done; err == nil {
		t.Fatal("test.watch should end with an error")
	}
	recs, _ := e.st.AuditQuery(t.Context(), auditFilterOp("test.watch"))
	if len(recs) != 2 || recs[0].Result != audit.ResultStarted || recs[0].TS.Day() != 9 || recs[1].Result != audit.ResultError ||
		recs[1].TS.Day() != 10 || recs[1].RefID != recs[0].ID || recs[1].Duration != 6*time.Hour+10*time.Minute {
		t.Fatalf("late entry not as expected: %+v", recs)
	}
	res, err := e.run(t, adminKey, "audit.export", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ex := res.Output.(AuditExportOutput)
	states := map[string]string{}
	for _, f := range ex.Files {
		states[f.Day] = f.State
	}
	if states["2026-03-09"] != "unchanged" || states["2026-03-10"] != "updated" {
		t.Fatalf("export states = %v", states)
	}
	after, _ := os.ReadFile(dayD)
	if !bytes.Equal(before, after) {
		t.Fatal("the exported day was rewritten by a late entry")
	}
	next, _ := os.ReadFile(filepath.Join(e.dir, auditFileName("2026-03-10")))
	if bytes.Count(next, []byte(`"test.watch"`)) != 1 {
		t.Fatalf("late entry missing from the next day:\n%s", next)
	}
	res, _ = e.run(t, adminKey, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "ok" || len(v.NotExported) != 1 || v.NotExported[0] != "2026-03-10" {
		// Der Aufruf von audit.verify selbst ist ein neuer Eintrag am 10.
		t.Fatalf("verify = %+v", v)
	}
	unexp, err := AuditUnexportedDays(t.Context(), e.st)
	if err != nil || len(unexp) != 1 || unexp[0] != "2026-03-10" {
		t.Fatalf("unexported = %v %v", unexp, err)
	}
}

// A12.4: Der Export ersetzt am Ziel keine Datei, die er nicht selbst
// geschrieben hat (weder Tag-Datei noch Manifest).
func TestAuditExportKeepsForeignFiles(t *testing.T) {
	e := newAdminEnv(t)
	e.run(t, adminKey, "keys.list", map[string]any{})
	days, _ := e.st.AuditDays(t.Context())
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(e.dir, auditFileName(days[0]))
	if err := os.WriteFile(foreign, []byte("not mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); !errors.Is(err, ErrAuditExportForeign) {
		t.Fatalf("export over a foreign day file = %v", err)
	}
	if b, _ := os.ReadFile(foreign); string(b) != "not mine\n" {
		t.Fatal("foreign day file was replaced")
	}
	_ = os.Remove(foreign)
	manifest := filepath.Join(e.dir, auditManifestName())
	_ = os.WriteFile(manifest, []byte(`{"something":"else"}`), 0o600)
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); !errors.Is(err, ErrAuditExportForeign) {
		t.Fatalf("export over a foreign manifest = %v", err)
	}
	if _, err := os.Stat(foreign); err == nil {
		t.Fatal("export wrote a day file although the manifest is foreign")
	}
	_ = os.Remove(manifest)
	// Eigene Dateien ersetzt er weiter, auch an einem neuen Ort mit
	// eigenem Manifest.
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e.run(t, adminKey, "keys.list", map[string]any{})
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func auditFilterOp(op string) store.AuditFilter { return store.AuditFilter{Operation: op} }
