package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// K2 (Nachbesserung T1a): doctor zeigt die unvollständigen Serverantworten
// je Konto (Gesamtzahl, letzter Lauf) und warnt, wenn der letzte Lauf damit
// höchstens sieben Tage zurückliegt.
func TestDoctorShowsIncompleteAnswers(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateAccount(t.Context(), store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 1,
		TLS: "none", Username: "tester@example.invalid", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	for _, n := range []int{1, 2} {
		if err := st.RecordIncompleteAnswers(t.Context(), id, n, at); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()
	rep := doctorJSON(t)
	if len(rep.Accounts) != 1 || rep.Accounts[0].IncompleteAnswers != 3 || rep.Accounts[0].IncompleteAnswersLast != 2 ||
		rep.Accounts[0].IncompleteAnswersAt == nil || !hasWarning(rep, "account demo: 2 incomplete server answers") {
		t.Fatalf("doctor: %+v", rep)
	}
	code, out, errs := run(t, "doctor")
	if code != ExitOK || !strings.Contains(out, "incomplete server answers: 3 (last: 2 on ") {
		t.Fatalf("doctor text: %d %s %s", code, out, errs)
	}
}
