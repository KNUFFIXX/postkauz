package ops

import (
	"path/filepath"
	"testing"
	"time"
)

// A10 (T3): Nach einer Wiederherstellung sind die Exportorte geleert. verify
// prüft diese Tage nur gegen die Prüfsumme in der Datenbank (origin_days,
// eine veränderte Zeile bleibt database_changed); export schreibt sie an
// den eigenen Ort neu, danach prüft verify wieder die Dateien.
func TestAuditVerifyAndExportAfterRestore(t *testing.T) {
	e := newAdminEnv(t)
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	for i := 0; i < 3; i++ {
		if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'system', 'sync.run', 'ok')`,
			yesterday.Add(time.Duration(i)*time.Minute).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.run(t, adminKey, "audit.export", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE audit_exports SET location = ''`); err != nil {
		t.Fatal(err)
	}
	verify := func() AuditVerifyOutput {
		t.Helper()
		res, err := e.run(t, adminKey, "audit.verify", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		return res.Output.(AuditVerifyOutput)
	}
	if v := verify(); v.Status != "ok" || v.OriginDays != 2 || v.DaysChecked != 2 || v.ChainHead == "" {
		t.Fatalf("verify after restore = %+v", v)
	}
	// Eigener Exportort: die Tage werden dort neu geschrieben.
	dir := t.TempDir()
	res, err := e.run(t, adminKey, "audit.export", map[string]any{"to": dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Output.(AuditExportOutput).Files {
		if f.State == "unchanged" || filepath.Dir(f.File) != dir {
			t.Fatalf("export after restore: %+v", f)
		}
	}
	if v := verify(); v.Status != "ok" || v.OriginDays != 0 {
		t.Fatalf("verify after the own export = %+v", v)
	}
	// Eine veränderte Zeile eines Tages des Originals fällt auch ohne Datei auf.
	if err := e.st.ExecForTest(`UPDATE audit_exports SET location = ''`); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`DROP TRIGGER audit_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE audit_events SET operation = 'accounts.list' WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	// Der Folgetag hängt an der Kette des veränderten Tages: chain_broken.
	if v := verify(); v.Status != "mismatch" || len(v.Problems) != 2 || v.Problems[0].Problem != AuditDatabaseChanged || v.Problems[0].File != "" ||
		v.Problems[1].Problem != AuditChainBroken {
		t.Fatalf("verify after a changed row = %+v", v)
	}
}
