package ops

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// fixture17 legt die Datenbank mit Schema 17 und ihren Audit-Export aus
// testdata/t1b_schema17 (erzeugt mit dem Stand T1a, siehe gen/main.go) in
// ein frisches Verzeichnis und biegt den Exportort auf die Kopie um.
func fixture17(t *testing.T) (dbPath, exportDir string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", "t1b_schema17")
	f, err := os.Open(filepath.Join(src, "postkauz.db.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	dbPath = filepath.Join(dir, "postkauz.db")
	if err := os.WriteFile(dbPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	exportDir = filepath.Join(dir, "export")
	if err := os.MkdirAll(exportDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(src, "export", "*"))
	if len(files) != 5 {
		t.Fatalf("fixture export files = %v", files)
	}
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(exportDir, filepath.Base(p)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db := rawDB(t, dbPath)
	if _, err := db.Exec(`UPDATE audit_exports SET location = ?`, exportDir); err != nil {
		t.Fatal(err)
	}
	return dbPath, exportDir
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// auditRows liest die Spalten, die es schon vor 0018 gab, Wert für Wert.
func auditRows(t *testing.T, db *sql.DB) (rows []string, seq int64) {
	t.Helper()
	r, err := db.Query(`SELECT id, ts, actor_type, quote(api_key_id), quote(scope), operation, params_redacted, targets, result,
		quote(reason), dry_run, quote(duration_ms), quote(policy_version) FROM audit_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for r.Next() {
		v := make([]any, 13)
		p := make([]any, 13)
		for i := range v {
			p[i] = &v[i]
		}
		if err := r.Scan(p...); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(v)
		rows = append(rows, string(b))
	}
	if err := db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'audit_events'`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return rows, seq
}

// Pflichttest A1 (A03, Variante A; A03_teile.md T1b): Ein Export, den der
// Stand vor der Migration geschrieben hat, besteht nach Migration 0018
// audit verify; die alten Zeilen bleiben Wert für Wert gleich; ein neuer
// Export lässt die alten Tage Byte für Byte stehen, und verify bleibt ok.
func TestMigration0018AuditVerify(t *testing.T) {
	ctx := context.Background()
	dbPath, exportDir := fixture17(t)
	before, seqBefore := auditRows(t, rawDB(t, dbPath))
	if len(before) != 32 { // 30 + Export + Prüfung des alten Stands
		t.Fatalf("fixture has %d audit rows, want 32", len(before))
	}
	old := map[string][]byte{}
	for _, d := range []string{"2026-10-01", "2026-10-02", "2026-10-03"} {
		b, err := os.ReadFile(filepath.Join(exportDir, auditFileName(d)))
		if err != nil {
			t.Fatal(err)
		}
		old[d] = b
	}

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(ctx); v != 17 {
		t.Fatalf("fixture schema = %d, want 17", v)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.SchemaVersion(ctx); v != store.KnownSchemaVersion() || st.LastSnapshot == "" {
		t.Fatalf("schema = %d, snapshot %q", v, st.LastSnapshot)
	}
	after, seqAfter := auditRows(t, rawDB(t, dbPath))
	if strings.Join(after, "\n") != strings.Join(before, "\n") || seqAfter < seqBefore {
		t.Fatalf("audit rows changed by the migration (seq %d -> %d)", seqBefore, seqAfter)
	}
	var refs int
	if err := rawDB(t, dbPath).QueryRow(`SELECT count(*) FROM audit_events WHERE ref_id IS NOT NULL`).Scan(&refs); err != nil || refs != 0 {
		t.Fatalf("ref_id on old rows: %d %v", refs, err)
	}
	// Append-only gilt weiter (Trigger neu angelegt).
	if _, err := rawDB(t, dbPath).Exec(`UPDATE audit_events SET result = 'ok' WHERE id = 1`); err == nil {
		t.Fatal("update on audit_events allowed after 0018")
	}
	if _, err := rawDB(t, dbPath).Exec(`DELETE FROM audit_events WHERE id = 1`); err == nil {
		t.Fatal("delete on audit_events allowed after 0018")
	}

	reg := NewRegistry()
	for _, op := range []Operation{AuditExport(st, exportDir), AuditVerify(st), AuditList(st)} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	x := &Executor{Registry: reg, Audit: st.AuditWriter()}
	human := policy.Key{ID: "cli", Human: true, Scopes: policy.Roles["admin"]}
	run := func(op string) Result {
		t.Helper()
		res, err := x.Execute(ctx, Request{Key: human, Actor: audit.ActorHuman, Operation: op, Input: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return res
	}
	if v := run("audit.verify").Output.(AuditVerifyOutput); v.Status != "ok" || len(v.Problems) != 0 || v.DaysChecked != 4 {
		t.Fatalf("verify after migration = %+v", v)
	}
	ex := run("audit.export").Output.(AuditExportOutput)
	states := map[string]string{}
	for _, f := range ex.Files {
		states[f.Day] = f.State
	}
	for d, b := range old {
		got, _ := os.ReadFile(filepath.Join(exportDir, auditFileName(d)))
		if states[d] != "unchanged" || !bytes.Equal(got, b) {
			t.Fatalf("day %s after the new export: state %q, bytes equal %v", d, states[d], bytes.Equal(got, b))
		}
	}
	if v := run("audit.verify").Output.(AuditVerifyOutput); v.Status != "ok" || len(v.Problems) != 0 {
		t.Fatalf("verify after a new export = %+v", v)
	}
	// Die neuen Aufrufe haben je Start und Abschluss; audit.list fasst sie
	// zusammen, raw zeigt beide.
	res, err := x.Execute(ctx, Request{Key: human, Actor: audit.ActorHuman, Operation: "audit.list",
		Input: json.RawMessage(`{"since":"2026-10-01","operation":"audit.export"}`)})
	if err != nil {
		t.Fatal(err)
	}
	evs := res.Output.(AuditListOutput).Events
	if len(evs) != 2 || evs[0].Result != "ok" || evs[0].EndID != 0 || evs[1].Result != "ok" || evs[1].EndID == 0 {
		// evs[0]: Export des alten Stands (ein Eintrag), evs[1]: neuer Export.
		t.Fatalf("merged list = %+v", evs)
	}
}

// A1: audit.list zeigt je Aufruf eine Zeile mit Endergebnis, raw jede
// Zeile; ein Start ohne Abschluss bleibt started; Filter result gilt für das
// Endergebnis; doctor zählt alte Starts ohne Abschluss.
func TestAuditListMergesStartAndEnd(t *testing.T) {
	e := newAdminEnv(t)
	ctx := t.Context()
	e.run(t, adminKey, "keys.list", map[string]any{})
	e.run(t, adminKey, "keys.revoke", map[string]any{"name": "fehlt"}) // fachlicher Fehler: Abschluss error
	agent := policy.Key{ID: "key:leser", Scopes: policy.Roles["reader"]}
	e.run(t, agent, "keys.list", map[string]any{}) // Policy-Ablehnung: ein Eintrag

	list := func(in map[string]any) []AuditEventOutput {
		t.Helper()
		res, err := e.run(t, adminKey, "audit.list", in)
		if err != nil {
			t.Fatal(err)
		}
		return res.Output.(AuditListOutput).Events
	}
	merged := list(map[string]any{"since": "1h"})
	// keys.list, keys.revoke, abgelehnt, dazu der laufende audit.list
	// (noch ohne Abschluss: started).
	if len(merged) != 4 {
		t.Fatalf("merged = %+v", merged)
	}
	if m := merged[0]; m.Operation != "keys.list" || m.Result != "ok" || m.EndID == 0 || m.RefID != 0 {
		t.Fatalf("merged[0] = %+v", m)
	}
	if m := merged[1]; m.Operation != "keys.revoke" || m.Result == "started" || m.EndID == 0 {
		t.Fatalf("merged[1] = %+v", m)
	}
	if m := merged[2]; m.Result != "denied" || m.EndID != 0 || m.Key != "key:leser" {
		t.Fatalf("merged[2] = %+v", m)
	}
	if m := merged[3]; m.Operation != "audit.list" || m.Result != "started" {
		t.Fatalf("merged[3] = %+v", m)
	}
	raw := list(map[string]any{"since": "1h", "raw": true})
	// 2 + 2 + 1 + 2 (erster audit.list) + 1 (dieser, noch ohne Abschluss)
	if len(raw) != 8 || raw[1].RefID != raw[0].ID || raw[1].Result != "ok" || raw[0].Result != "started" {
		t.Fatalf("raw = %+v", raw)
	}
	if ok := list(map[string]any{"since": "1h", "result": "denied"}); len(ok) != 1 || ok[0].Key != "key:leser" {
		t.Fatalf("result filter = %+v", ok)
	}
	// Alle Aufrufe sind abgeschlossen; ein alter Start ohne Abschluss
	// (Absturz) zählt.
	if n, err := e.st.AuditOpenStarts(ctx, time.Now().Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("open starts = %d %v", n, err)
	}
	if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'human', 'sync.run', 'started')`,
		time.Now().Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if n, err := e.st.AuditOpenStarts(ctx, time.Now().Add(-10*time.Minute)); err != nil || n != 1 {
		t.Fatalf("open starts after a crash = %d %v", n, err)
	}
	// Während ein Sync läuft (doctor sieht die Kontosperre), zählt sein
	// offener Start nicht.
	if n, err := e.st.AuditOpenStarts(ctx, time.Now().Add(-10*time.Minute), "sync.run", "import.run"); err != nil || n != 0 {
		t.Fatalf("open starts while a sync runs = %d %v", n, err)
	}
}

// ErrorKinds nennt Typen und Codes, nie den Text eines Fehlers.
func TestErrorKindsWithoutText(t *testing.T) {
	err := fmt.Errorf("store: x: %w", errors.Join(errors.New("KANARIE /home/geheim"), &DeniedError{Reason: "draft_closed", Msg: "KANARIE"}))
	k := ErrorKinds(err)
	if strings.Contains(k, "KANARIE") || !strings.Contains(k, "*fmt.wrapError") || !strings.Contains(k, "*ops.DeniedError") ||
		!strings.Contains(k, "reason=draft_closed") {
		t.Fatalf("kinds = %q", k)
	}
}

// countingRepo zählt die Einträge, die Export und Prüfung aus der
// Datenbank lesen.
type countingRepo struct {
	AuditRepo
	rows int
}

func (c *countingRepo) AuditQuery(ctx context.Context, f store.AuditFilter) ([]store.AuditRecord, error) {
	l, err := c.AuditRepo.AuditQuery(ctx, f)
	c.rows += len(l)
	return l, err
}

// Nachprüfung T1a: Der Plan von Export, Prüfung und doctor lädt nur die
// Kandidaten der offenen Tage; abgeschlossene Tage liest der Export gar
// nicht, die Prüfung tageweise. Ein verspäteter Eintrag (Zeitstempel eines
// abgeschlossenen Tages, geschrieben nach dessen Export) kommt in den
// nächsten offenen Tag; ein Export an einen neuen Ort schreibt alle Tage
// aus der Datenbank neu, und verify bleibt ok.
func TestAuditPlanLoadsOnlyOpenDays(t *testing.T) {
	e := newAdminEnv(t)
	ctx := t.Context()
	day := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	clock := day
	e.x.Now = func() time.Time { return clock }
	auditClock = func() time.Time { return clock }
	t.Cleanup(func() { auditClock = time.Now })
	for d := 0; d < 3; d++ {
		clock = day.Add(time.Duration(d) * 24 * time.Hour)
		for i := 0; i < 50; i++ {
			e.run(t, adminKey, "keys.list", map[string]any{})
		}
	}
	clock = day.Add(3*24*time.Hour + time.Hour) // 4. März: Tage 1.–3. abgeschlossen
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// Verspäteter Eintrag mit dem Zeitstempel des 2. März.
	if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'system', 'sync.run', 'ok')`,
		day.Add(24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	cr := &countingRepo{AuditRepo: e.st}
	exp, ver := AuditExport(cr, e.dir), AuditVerify(cr)
	call := func(op Operation) any {
		t.Helper()
		out, err := op.Handler(ctx, Call{Input: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ex := call(exp).(AuditExportOutput)
	if cr.rows > 10 {
		t.Fatalf("export read %d audit rows; only the open days should be read", cr.rows)
	}
	states := map[string]string{}
	for _, f := range ex.Files {
		states[f.Day] = f.State
	}
	if states["2026-03-01"] != "unchanged" || states["2026-03-02"] != "unchanged" || states["2026-03-03"] != "unchanged" ||
		states["2026-03-04"] != "updated" {
		t.Fatalf("states = %v", states)
	}
	b, _ := os.ReadFile(filepath.Join(e.dir, auditFileName("2026-03-04")))
	if !bytes.Contains(b, []byte(`"sync.run"`)) {
		t.Fatal("late entry not in the next open day")
	}
	cr.rows = 0
	if v := call(ver).(AuditVerifyOutput); v.Status != "ok" || v.DaysChecked != 4 {
		t.Fatalf("verify = %+v", v)
	}
	if cr.rows > 320 { // vier Tage je einmal, dazu die offenen Kandidaten
		t.Fatalf("verify read %d rows", cr.rows)
	}
	cr.rows = 0
	if days, err := AuditUnexportedDays(ctx, cr); err != nil || len(days) != 0 || cr.rows > 10 {
		t.Fatalf("unexported = %v %v (rows %d)", days, err, cr.rows)
	}
	// Neuer Ort: alle Tage neu aus der Datenbank, Kette wie vorher.
	other := filepath.Join(t.TempDir(), "neu")
	out, err := AuditExport(cr, other).Handler(ctx, Call{Input: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(AuditExportOutput); o.ChainHead != ex.ChainHead || len(o.Files) != 4 {
		t.Fatalf("export to a new place: head %s vs %s, %d files", o.ChainHead, ex.ChainHead, len(o.Files))
	}
	for _, d := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		a, _ := os.ReadFile(filepath.Join(e.dir, auditFileName(d)))
		b, _ := os.ReadFile(filepath.Join(other, auditFileName(d)))
		if !bytes.Equal(a, b) {
			t.Fatalf("day %s differs at the new place", d)
		}
	}
	if v := call(ver).(AuditVerifyOutput); v.Status != "ok" {
		t.Fatalf("verify after moving = %+v", v)
	}
}
