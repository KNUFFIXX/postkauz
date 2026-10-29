package retention_test

// Nachbesserung T3 Punkt 6 (A14 „GC bricht offene Entwürfe nicht“):
// blobs.gc darf keine Datei löschen, die nach der Altersprüfung von
// blob.Put erneut abgelegt wurde (Existed=true, frisches Datum), etwa für
// einen Dateianhang eines neuen Entwurfs (drafts.create im MCP-Prozess,
// ohne Kontosperre). Entwurfsanhänge haben keine Zeile in blobs, also
// prüft keine SQL-Anweisung vor dem Löschen nach.
//
// Das Zeitfenster zwischen Altersprüfung und Löschen öffnet der Test
// deterministisch über eine SQL-Funktion in einem Trigger auf DELETE FROM
// blobs (DeleteUnreferencedBlobRows läuft genau dazwischen).

import (
	"bytes"
	"context"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modernc.org/sqlite"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/retention"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// t3nHook läuft einmal, wenn der Trigger die SQL-Funktion aufruft.
var t3nHook func()

func init() {
	sqlite.MustRegisterScalarFunction("pk_t3n_hook", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		if h := t3nHook; h != nil {
			t3nHook = nil
			h()
		}
		return int64(0), nil
	})
}

// t3nEnv: Datenbank, Blob-Store mit einem alten Blob ohne Verweis und eine
// alte, unverwiesene Zeile ohne Datei (damit DeleteUnreferencedBlobRows
// läuft).
func t3nEnv(t *testing.T, content []byte) (*store.SQLite, *blob.Store, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	bs, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := bs.Put(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := bs.Path(info.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-90 * 24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO blobs(sha256, size, compressed, created_at) VALUES (?, 1, 0, ?)`,
		"aa"+strings.Repeat("0", 62), old.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return st, bs, info.SHA256
}

// Put mitten im Lauf (nach der Altersprüfung): Die Datei bleibt, GC zählt
// sie als skipped.
func TestT3NGCKeepsBlobRePutDuringRun(t *testing.T) {
	content := []byte("rechnung.pdf, früher schon einmal angehängt")
	st, bs, sum := t3nEnv(t, content)
	if err := st.ExecForTest(`CREATE TRIGGER t3n_hook BEFORE DELETE ON blobs BEGIN SELECT pk_t3n_hook(); END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { t3nHook = nil })
	var reput blob.Info
	var putErr error
	t3nHook = func() {
		// drafts.create: putFiles → Put (Existed, frisches Datum); danach entstünde der Entwurf.
		reput, putErr = bs.Put(bytes.NewReader(content))
	}
	svc := &retention.Service{Store: st, Blobs: bs}
	rep, err := svc.GC(context.Background(), 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if putErr != nil || !reput.Existed {
		t.Fatalf("hook did not re-put: existed=%v err=%v", reput.Existed, putErr)
	}
	if !bs.Has(sum) {
		t.Fatalf("blobs.gc removed a blob that blob.Put reported as stored (Existed=true) during the run; report %+v", rep)
	}
	if rep.Skipped != 1 || rep.RemovedFiles != 0 || rep.RemovedRows != 1 {
		t.Errorf("report = %+v, want skipped 1, removed files 0, removed rows 1", rep)
	}
}

// Entwurf mitten im Lauf, dessen Anhang die alte Datei nennt, ohne dass ein
// Put ihr Datum erneuert (etwa weil Chtimes scheiterte): GC lädt die
// Entwurfs-Hashes nach dem Löschen der Zeilen neu, die Datei bleibt.
func TestT3NGCKeepsBlobOfDraftCreatedDuringRun(t *testing.T) {
	ctx := context.Background()
	st, bs, sum := t3nEnv(t, []byte("anhang eines neuen Entwurfs"))
	acct, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "demo@example.org", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	// Der Entwurf entsteht im selben Augenblick wie das Löschen der Zeilen
	// (Trigger in derselben Anweisung, sonst sperrte SQLite die zweite
	// Verbindung aus); Hash und Konto sind feste Werte des Tests.
	if err := st.ExecForTest(fmt.Sprintf(`CREATE TRIGGER t3n_draft BEFORE DELETE ON blobs
		WHEN NOT EXISTS (SELECT 1 FROM drafts) BEGIN
		INSERT INTO drafts(account_id, kind, message_id_header, status, attachment_blobs, created_by, created_at, updated_at)
		VALUES (%d, 'new', '<t3n@example.org>', 'draft', json_array(json_object('sha256', '%s')), 'cli', 1, 1); END`, acct, sum)); err != nil {
		t.Fatal(err)
	}
	svc := &retention.Service{Store: st, Blobs: bs}
	rep, err := svc.GC(ctx, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := st.QueryIntForTest(`SELECT count(*) FROM drafts`); n != 1 {
		t.Fatalf("test setup: draft not created during the run (%d)", n)
	}
	if !bs.Has(sum) {
		t.Fatalf("blobs.gc removed the attachment of a draft created during the run; report %+v", rep)
	}
	if rep.Skipped != 1 || rep.RemovedFiles != 0 {
		t.Errorf("report = %+v, want skipped 1, removed files 0", rep)
	}
}
