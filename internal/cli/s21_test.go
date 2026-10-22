package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.1: doctor kennt Schema 10. Ein Bestand mit Schema 9 (Stand S2.0) ist
// needs_migration mit Hinweis; das nächste Kommando migriert, danach ok.
func TestDoctorKnowsSchema10(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateToForTest(t.Context(), 9); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	rep := doctorJSON(t)
	if rep.SchemaKnown != 10 || rep.SchemaVersion != 9 || rep.Database != "needs_migration" ||
		!hasWarning(rep, "database schema 9 is older than this program (10)") {
		t.Fatalf("doctor on schema 9: %+v", rep)
	}
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %d %s", code, errs)
	}
	if rep := doctorJSON(t); rep.SchemaVersion != 10 || rep.Database != "ok" {
		t.Fatalf("doctor after migration: %+v", rep)
	}
}
