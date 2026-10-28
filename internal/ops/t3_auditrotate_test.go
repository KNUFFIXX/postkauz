package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// audit rotate (T3, A03 E8, A14): Akzeptanz 3. Ohne bestätigten Export
// denied (not_exported); mit Export und grünem verify entsteht die
// Archivdatei, audit verify bleibt grün über Archiv und Datenbank, die
// Kette ist ungebrochen, und der nächste audit export läuft (auch an einen
// neuen Ort, dann aus dem Archiv).

type rotEnv struct {
	st  *store.SQLite
	x   *Executor
	dir string
}

func newRotEnv(t *testing.T) *rotEnv {
	t.Helper()
	st := openTestStore(t)
	e := &rotEnv{st: st, dir: t.TempDir()}
	reg := NewRegistry()
	for _, op := range []Operation{AuditExport(st, e.dir), AuditVerify(st), AuditRotate(st, 365*24*time.Hour)} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	// Das Audit der Aufrufe geht in dieselbe Datenbank (wie in der CLI).
	e.x = &Executor{Registry: reg, Audit: st.AuditWriter()}
	w := st.AuditWriter()
	ctx := context.Background()
	old := time.Now().UTC().AddDate(-2, 0, 0).Truncate(24 * time.Hour)
	for _, ts := range []time.Time{old.Add(time.Hour), old.Add(2 * time.Hour), old.Add(3 * time.Hour),
		old.Add(25 * time.Hour), old.Add(26 * time.Hour), time.Now().Add(-time.Minute)} {
		if err := w.Append(ctx, audit.Event{TS: ts, ActorType: audit.ActorAgent, KeyID: "key:k", Operation: "messages.search",
			ParamsRedacted: json.RawMessage(`{"fields":["query"]}`), Result: audit.ResultOK}); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *rotEnv) run(t *testing.T, op, input string, dry bool) (any, error) {
	t.Helper()
	res, err := e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorHuman, Operation: op,
		Input: json.RawMessage(input), DryRun: &dry})
	return res.Output, err
}

func (e *rotEnv) verify(t *testing.T) AuditVerifyOutput {
	t.Helper()
	out, err := e.run(t, "audit.verify", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	return out.(AuditVerifyOutput)
}

func (e *rotEnv) rows(t *testing.T) int {
	t.Helper()
	n, err := e.st.QueryIntForTest(`SELECT count(*) FROM audit_events`)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAuditRotate(t *testing.T) {
	ctx := context.Background()
	e := newRotEnv(t)

	// Ohne Export: denied (not_exported), nichts geändert.
	before := e.rows(t)
	_, err := e.run(t, "audit.rotate", `{}`, false)
	var de *DeniedError
	if !errors.As(err, &de) || de.Reason != "not_exported" {
		t.Fatalf("rotate without export: %v", err)
	}
	if n := e.rows(t); n != before+2 {
		t.Fatalf("rows %d -> %d; only the two audit entries of the call may be new", before, n)
	}

	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	v := e.verify(t)
	if v.Status != "ok" || v.DaysChecked != 3 {
		t.Fatalf("verify before rotate = %+v", v)
	}
	head := v.ChainHead
	old1 := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")

	// Trockenlauf zählt, ändert nichts.
	before = e.rows(t)
	out, err := e.run(t, "audit.rotate", `{}`, true)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(AuditRotateOutput)
	if o.Status != "would_rotate" || o.Days != 2 || o.Events != 5 || o.FromDay != old1 || o.Retain != "365d" {
		t.Fatalf("dry run = %+v", o)
	}
	if _, err := os.Stat(o.Archive); !errors.Is(err, os.ErrNotExist) || e.rows(t) != before+2 {
		t.Fatalf("dry run wrote something: %v", err)
	}

	// Eine veränderte Exportdatei: verify meldet sie, rotate lehnt ab.
	f := filepath.Join(e.dir, auditFileName(old1))
	orig, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, append([]byte("x"), orig...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, "audit.rotate", `{}`, false); !errors.As(err, &de) || de.Reason != "not_verified" {
		t.Fatalf("rotate with a changed export file: %v", err)
	}
	if err := os.WriteFile(f, orig, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err = e.run(t, "audit.rotate", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	o = out.(AuditRotateOutput)
	if o.Status != "rotated" || o.Days != 2 || o.Events != 5 || o.ChainHead == "" || len(o.SHA256) != 64 {
		t.Fatalf("rotate = %+v", o)
	}
	if filepath.Base(o.Archive) != store.AuditArchiveName(o.FromDay, o.ToDay) || filepath.Dir(o.Archive) != filepath.Dir(e.st.Path()) {
		t.Fatalf("archive at %s", o.Archive)
	}
	if fi, err := os.Stat(o.Archive); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("archive file: %v %v", fi, err)
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM audit_events WHERE ts < ?`, time.Now().AddDate(-1, 0, 0).UnixMilli()); n != 0 {
		t.Fatalf("%d old rows left in the database", n)
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM audit_archives WHERE events = 5 AND chain_head = ?`, o.ChainHead); n != 1 {
		t.Fatal("archive not recorded")
	}
	// Append-only gilt wieder.
	if err := e.st.ExecForTest(`DELETE FROM audit_events`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete after rotation: %v", err)
	}
	evs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "audit.rotate"})
	if last := evs[len(evs)-1]; !strings.Contains(string(last.ParamsRedacted), `"days":2`) || !strings.Contains(string(last.ParamsRedacted), `"events":5`) {
		t.Fatalf("audit end entry = %s", last.ParamsRedacted)
	}

	// verify über Archiv und Datenbank: grün, gleiche Kette.
	v = e.verify(t)
	if v.Status != "ok" || v.DaysChecked != 3 || v.ChainHead != head {
		t.Fatalf("verify after rotate = %+v (head before %s)", v, head)
	}
	// Der nächste Export: grün, alte Tage unverändert.
	out, err = e.run(t, "audit.export", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.(AuditExportOutput).Files {
		if f.Day <= o.ToDay && f.State != "unchanged" {
			t.Fatalf("archived day %s: %s", f.Day, f.State)
		}
	}
	if v = e.verify(t); v.Status != "ok" {
		t.Fatalf("verify after the next export = %+v", v)
	}
	// Export an einen neuen Ort: archivierte Tage kommen aus dem Archiv,
	// Byte für Byte wie zuvor.
	other := t.TempDir()
	if _, err := e.run(t, "audit.export", `{"to":"`+filepath.ToSlash(other)+`"}`, false); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{o.FromDay, o.ToDay} {
		a, _ := os.ReadFile(filepath.Join(e.dir, auditFileName(d)))
		b, err := os.ReadFile(filepath.Join(other, auditFileName(d)))
		if err != nil || string(a) != string(b) {
			t.Fatalf("day %s rewritten differently from the archive: %v", d, err)
		}
	}
	if v = e.verify(t); v.Status != "ok" {
		t.Fatalf("verify after export to a new place = %+v", v)
	}
	// Nichts mehr zu tun.
	if out, err = e.run(t, "audit.rotate", `{}`, false); err != nil || out.(AuditRotateOutput).Status != "nothing_to_rotate" {
		t.Fatalf("second rotate: %+v %v", out, err)
	}
}

// Fehlt das Archiv oder ist es verändert, meldet verify genau das (nicht
// zusätzlich jeden späteren Tag) und prüft den Rest ab dem Anker; ein
// Export, der archivierte Tage neu schreiben müsste, bricht ab.
func TestAuditRotateArchiveProblems(t *testing.T) {
	e := newRotEnv(t)
	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "audit.rotate", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	arc := out.(AuditRotateOutput).Archive
	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(arc)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		v := e.verify(t)
		if v.Status != "mismatch" || len(v.Problems) != 1 || v.Problems[0].Problem != want || v.Problems[0].File != arc {
			t.Fatalf("verify = %+v, want only %s", v, want)
		}
		if _, err := e.run(t, "audit.export", `{"to":"`+filepath.ToSlash(t.TempDir())+`"}`, false); !errors.Is(err, ErrAuditArchive) {
			t.Fatalf("export to a new place with a broken archive: %v", err)
		}
		if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
			t.Fatalf("export at the same place: %v", err)
		}
	}
	if err := os.Remove(arc); err != nil {
		t.Fatal(err)
	}
	check(AuditArchiveMissing)
	if err := os.WriteFile(arc, append(saved, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	check(AuditArchiveChanged)
	if err := os.WriteFile(arc, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if v := e.verify(t); v.Status != "ok" {
		t.Fatalf("verify with the archive back = %+v", v)
	}
}

// Ein Archiv ohne Eintrag (Abbruch vor der Transaktion) wird ersetzt; die
// Zeilen standen ja noch in der Datenbank.
func TestAuditRotateReplacesLeftoverArchive(t *testing.T) {
	e := newRotEnv(t)
	if _, err := e.run(t, "audit.export", `{}`, false); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "audit.rotate", `{}`, true)
	if err != nil {
		t.Fatal(err)
	}
	arc := out.(AuditRotateOutput).Archive
	if err := os.WriteFile(arc, []byte("Rest eines Abbruchs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err = e.run(t, "audit.rotate", `{}`, false); err != nil || out.(AuditRotateOutput).Status != "rotated" {
		t.Fatalf("rotate over a leftover: %+v %v", out, err)
	}
	if v := e.verify(t); v.Status != "ok" {
		t.Fatalf("verify = %+v", v)
	}
}
