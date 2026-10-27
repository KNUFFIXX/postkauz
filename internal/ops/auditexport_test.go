package ops

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S1.11: Export mit Hash-Kette über alle Tage und Manifest; verify erkennt
// eine veränderte Zeile (mit Zeilennummer), eine gebrochene Kette, ein
// fehlendes oder verändertes Manifest und eine veränderte Datenbankzeile,
// ohne Folgefehler in späteren Tagen.
func TestAuditExportChainAndManifest(t *testing.T) {
	e := newAdminEnv(t)
	// Zwei Tage: Einträge von gestern per Hand (Einfügen ist erlaubt, nur
	// UPDATE und DELETE sind gesperrt), dazu die Aufrufe von heute.
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	for i := 0; i < 3; i++ {
		if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'system', 'sync.run', 'ok')`,
			yesterday.Add(time.Duration(i)*time.Minute).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	e.run(t, adminKey, "keys.list", map[string]any{})
	res, err := e.run(t, adminKey, "audit.export", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ex := res.Output.(AuditExportOutput)
	if len(ex.Files) != 2 || ex.Format != 2 || ex.Manifest != filepath.Join(e.dir, "postkauz-audit-manifest.json") {
		t.Fatalf("export = %+v", ex)
	}
	day1, _ := os.ReadFile(ex.Files[0].File)
	day2, _ := os.ReadFile(ex.Files[1].File)

	// Kette: jede Zeile hängt an der vorigen, über die Tagesgrenze hinweg.
	head1, bad := checkChain(day1, auditChainGenesis)
	if bad != 0 || head1 != ex.Files[0].ChainHead {
		t.Fatalf("day 1 chain: bad line %d, head %s, want %s", bad, head1, ex.Files[0].ChainHead)
	}
	head2, bad := checkChain(day2, head1)
	if bad != 0 || head2 != ex.ChainHead || head2 != ex.Files[1].ChainHead {
		t.Fatalf("day 2 chain: bad line %d", bad)
	}
	if _, bad := checkChain(day2, auditChainGenesis); bad != 1 {
		t.Fatalf("day 2 must continue day 1 (bad line %d)", bad)
	}
	var first auditChainLine
	if err := json.Unmarshal(bytes.SplitN(day1, []byte{'\n'}, 2)[0], &first); err != nil || first.Prev != auditChainGenesis {
		t.Fatalf("first line: %+v %v", first, err)
	}
	var ev AuditEventOutput
	if err := json.Unmarshal(first.Event, &ev); err != nil || ev.Operation != "sync.run" {
		t.Fatalf("event in line: %+v %v", ev, err)
	}
	// Manifest: Prüfsumme und Kettenende je Datei, ohne Zeitangabe.
	var m auditManifest
	mb, _ := os.ReadFile(ex.Manifest)
	if err := json.Unmarshal(mb, &m); err != nil || len(m.Days) != 2 || m.Head != head2 || m.Genesis != auditChainGenesis ||
		m.Days[0].SHA256 != sha256Hex(day1) || m.Days[1].File != filepath.Base(ex.Files[1].File) || m.Days[0].Events != 3 {
		t.Fatalf("manifest = %s (%v)", mb, err)
	}

	verify := func() AuditVerifyOutput {
		t.Helper()
		res, err := e.run(t, adminKey, "audit.verify", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		return res.Output.(AuditVerifyOutput)
	}
	if v := verify(); v.Status != "ok" || v.DaysChecked != 2 || v.ChainHead != head2 || v.LegacyDays != 0 {
		t.Fatalf("verify = %+v", v)
	}

	// Eine Zeile verändert (Ergebnis ok → denied in Zeile 2 von Tag 1).
	lines := bytes.Split(day1, []byte{'\n'})
	lines[1] = bytes.Replace(lines[1], []byte(`"result":"ok"`), []byte(`"result":"denied"`), 1)
	if err := os.WriteFile(ex.Files[0].File, bytes.Join(lines, []byte{'\n'}), 0o600); err != nil {
		t.Fatal(err)
	}
	v := verify()
	want := map[string]int{AuditFileChanged: 2, AuditChainBroken: 2}
	if v.Status != "mismatch" || len(v.Problems) != 2 {
		t.Fatalf("verify after change = %+v", v)
	}
	for _, p := range v.Problems {
		if want[p.Problem] != p.Line || p.Day != ex.Files[0].Day {
			t.Fatalf("problem %+v, want lines %v", p, want)
		}
	}
	// Export überschreibt die veränderte Datei nicht.
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("export over a changed file: %v", err)
	}
	_ = os.WriteFile(ex.Files[0].File, day1, 0o600)

	// Wer Zeile und Kette passend fälscht, fällt an Prüfsumme und Datenbank auf.
	forged := forgeLine(t, day1, 2, auditChainGenesis)
	_ = os.WriteFile(ex.Files[0].File, forged, 0o600)
	v = verify()
	if v.Status != "mismatch" || len(v.Problems) != 1 || v.Problems[0].Problem != AuditFileChanged || v.Problems[0].Line != 2 {
		t.Fatalf("verify after a consistent forgery = %+v", v)
	}
	_ = os.WriteFile(ex.Files[0].File, day1, 0o600)

	// Manifest fehlt bzw. ist verändert.
	_ = os.Remove(ex.Manifest)
	if v := verify(); len(v.Problems) != 1 || v.Problems[0].Problem != AuditManifestMissing || v.Problems[0].Day != "" {
		t.Fatalf("verify without manifest = %+v", v)
	}
	_ = os.WriteFile(ex.Manifest, bytes.Replace(mb, []byte(`"events": 3`), []byte(`"events": 2`), 1), 0o600)
	if v := verify(); len(v.Problems) != 1 || v.Problems[0].Problem != AuditManifestChanged || v.Problems[0].Line == 0 {
		t.Fatalf("verify with changed manifest = %+v", v)
	}
	_ = os.WriteFile(ex.Manifest, mb, 0o600)
	if v := verify(); v.Status != "ok" {
		t.Fatalf("verify after restore = %+v", v)
	}

	// Datenbankzeile von gestern verändert (Sperre per Hand entfernt): nur
	// Tag 1 ist betroffen, Tag 2 und Manifest nicht (keine Folgefehler).
	if err := e.st.ExecForTest(`DROP TRIGGER audit_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE audit_events SET operation = 'accounts.list' WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	v = verify()
	if v.Status != "mismatch" || len(v.Problems) != 1 || v.Problems[0].Problem != AuditDatabaseChanged || v.Problems[0].Line != 3 ||
		v.Problems[0].Day != ex.Files[0].Day {
		t.Fatalf("verify after db change = %+v", v)
	}
}

// forgeLine ändert Zeile n (ab 1) und rechnet die Kette ab dort passend neu.
func forgeLine(t *testing.T, content []byte, n int, genesis string) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix(content, []byte{'\n'}), []byte{'\n'})
	head := genesis
	var out bytes.Buffer
	for i, raw := range lines {
		var l auditChainLine
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatal(err)
		}
		if i+1 == n {
			l.Event = bytes.Replace(l.Event, []byte(`"result":"ok"`), []byte(`"result":"denied"`), 1)
		}
		l.Prev = head
		l.Chain = chainNext(head, l.Event)
		head = l.Chain
		b, _ := json.Marshal(l)
		out.Write(b)
		out.WriteByte('\n')
	}
	if _, bad := checkChain(out.Bytes(), genesis); bad != 0 {
		t.Fatalf("forgery has a broken chain at line %d", bad)
	}
	return out.Bytes()
}

// Exporte im Format 1 (S1.8 bis S1.10) prüft verify weiter; der nächste
// Export schreibt sie mit Kette neu.
func TestAuditVerifyLegacyFormat(t *testing.T) {
	e := newAdminEnv(t)
	ctx := t.Context()
	e.run(t, adminKey, "keys.list", map[string]any{})
	days, _ := e.st.AuditDays(ctx)
	list, _ := e.st.AuditQuery(ctx, store.AuditFilter{})
	d, err := auditDayContent(list, 0, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(e.dir, 0o700)
	file := filepath.Join(e.dir, auditFileName(days[0]))
	_ = os.WriteFile(file, d.content, 0o600)
	saveLegacy(t, e, days[0], d)
	res, _ := e.run(t, adminKey, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "ok" || v.LegacyDays != 1 || v.ChainHead != "" {
		t.Fatalf("verify legacy = %+v", v)
	}
	// Eine veränderte Zeile im alten Format fällt ebenfalls auf.
	_ = os.WriteFile(file, bytes.Replace(d.content, []byte(`"result":"ok"`), []byte(`"result":"error"`), 1), 0o600)
	res, _ = e.run(t, adminKey, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "mismatch" || len(v.Problems) != 1 || v.Problems[0].Problem != AuditFileChanged ||
		v.Problems[0].Line != 1 {
		t.Fatalf("verify legacy change = %+v", v)
	}
	_ = os.WriteFile(file, d.content, 0o600)
	res, err = e.run(t, adminKey, "audit.export", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if ex := res.Output.(AuditExportOutput); ex.Files[0].State != "updated" {
		t.Fatalf("export over legacy = %+v", ex)
	}
	res, _ = e.run(t, adminKey, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "ok" || v.LegacyDays != 0 || v.ChainHead == "" {
		t.Fatalf("verify after rewrite = %+v", v)
	}
}

func saveLegacy(t *testing.T, e *adminEnv, day string, d auditDay) {
	t.Helper()
	if err := e.st.ExecForTest(`INSERT INTO audit_exports(day, first_event_id, last_event_id, sha256, location, exported_at) VALUES (?,?,?,?,?,?)`,
		day, d.first, d.last, sha256Hex(d.content), e.dir, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}
