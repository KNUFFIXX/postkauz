package ops

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Vorlage der migrierten Datenbank für die Testumgebungen: Die Migrationen
// 1 bis 20 kosten je Umgebung rund 75 ms, die Kopie einer einmal migrierten
// Datei rund 3 ms. Der Vertragstest baut je Operation, Rolle und Variante
// eine frische Umgebung (über 2.000 Fälle); mit -race lag das Paket nach T2
// bei 27 Minuten, knapp unter der Zeitgrenze des CI-Jobs (30 min).
var (
	dbTemplateOnce sync.Once
	dbTemplateDir  string
	dbTemplate     []byte
	dbTemplateErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if dbTemplateDir != "" {
		_ = os.RemoveAll(dbTemplateDir)
	}
	os.Exit(code)
}

// openTestStore öffnet eine frische, migrierte Datenbank im TempDir des
// Tests (Kopie der Vorlage; Migrate prüft nur noch die Version).
func openTestStore(t *testing.T) *store.SQLite {
	t.Helper()
	dbTemplateOnce.Do(func() {
		if dbTemplateDir, dbTemplateErr = os.MkdirTemp("", "pk-ops-template-"); dbTemplateErr != nil {
			return
		}
		p := filepath.Join(dbTemplateDir, "pk.db")
		st, err := store.Open(p)
		if err != nil {
			dbTemplateErr = err
			return
		}
		if dbTemplateErr = st.Migrate(context.Background()); dbTemplateErr != nil {
			_ = st.Close()
			return
		}
		// Close schreibt das WAL in die Datei zurück; danach ist die Datei
		// allein vollständig.
		if dbTemplateErr = st.Close(); dbTemplateErr != nil {
			return
		}
		dbTemplate, dbTemplateErr = os.ReadFile(p)
	})
	if dbTemplateErr != nil {
		t.Fatalf("database template: %v", dbTemplateErr)
	}
	p := filepath.Join(t.TempDir(), "pk.db")
	if err := os.WriteFile(p, dbTemplate, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}
