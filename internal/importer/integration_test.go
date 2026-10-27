package importer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/corpus/imapload"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Integrationstest des Imports gegen Dovecot (deploy/testmail; `make
// testmail-up testmail-setup integration`, nur mit POSTKAUZ_IT=1 bzw.
// POSTKAUZ_IT=dovecot). Ein frisches Dovecot-Konto bekommt den Korpus ab
// Index 300 (echter Server: auch die Nachrichten, die der Testserver im
// Prozess nicht liefern kann), der Sync spiegelt es; danach wird der Korpus
// unter Index 430 als Thunderbird-Profil importiert (rund 30 % Überlappung).

func TestIntegrationImportOverlapDovecot(t *testing.T) {
	sel := os.Getenv("POSTKAUZ_IT")
	if sel == "" {
		t.Skip("integration tests need Docker test servers: make testmail-up testmail-setup integration")
	}
	if sel != "1" && sel != "all" && !slices.Contains(strings.Split(sel, ","), "dovecot") {
		t.Skip("dovecot not selected in POSTKAUZ_IT")
	}
	const host, port = "127.0.0.1", 11143
	pass := secrets.Secret("pass")
	user := fmt.Sprintf("it-import-%d@example.org", time.Now().UnixNano())
	c := standardCorpus(t)
	onServer := func(i int) bool { return i >= 300 }
	imported := func(i int) bool { return i < 430 }

	sub := &corpus.Corpus{Manifest: c.Manifest}
	sub.Manifest.Messages = nil
	for i, e := range c.Manifest.Messages {
		if onServer(i) {
			sub.Manifest.Messages = append(sub.Manifest.Messages, e)
			sub.Raw = append(sub.Raw, c.Raw[i])
		}
	}
	cl, err := imapx.Dial(context.Background(), imapx.Config{Host: host, Port: port, TLS: imapx.TLSNone, InsecureTestServer: true,
		CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("dovecot not reachable (make testmail-up): %v", err)
	}
	if err := cl.Login(user, pass); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := imapload.Load(context.Background(), cl, sub, imapload.Options{}); err != nil {
		t.Fatal(err)
	}
	_ = cl.Close()
	t.Logf("loaded %d messages in %s", len(sub.Raw), time.Since(start).Round(time.Millisecond))

	e := newEnv(t)
	syncOnce := func() mailsync.Report {
		prov, err := imapprov.Connect(context.Background(), imapprov.Config{Host: host, Port: port, TLS: "none", Username: user,
			InsecureTestServer: true, CommandTimeout: 30 * time.Second}, pass)
		if err != nil {
			t.Fatal(err)
		}
		defer prov.Close()
		rep, err := (&mailsync.Engine{Store: e.st, Blobs: e.blobs, Remote: prov, AccountID: e.acct}).SyncAll(context.Background(), mailsync.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	srep := syncOnce()
	synced := e.count(t, `SELECT count(*) FROM messages`)
	if srep.Errors != 0 || synced != len(sub.Raw) {
		t.Fatalf("sync = errors %d, messages %d, want %d", srep.Errors, synced, len(sub.Raw))
	}

	dir := filepath.Join(t.TempDir(), "imap.example.org")
	exp, err := c.WriteThunderbird(dir, corpus.ImportOptions{Only: imported})
	if err != nil {
		t.Fatal(err)
	}
	// Dovecot ersetzt NUL-Bytes beim Speichern durch 0x80 (gleiche Größe,
	// anderer Inhalt). Der Thunderbird-Export entsteht hier aus dem
	// Korpus, nicht vom Server, und enthält die NUL-Bytes noch: Diese
	// Nachrichten sind für den Import seit rc6 eine zweite Fassung mit
	// derselben Message-ID (Gmail-Regel, knownVersion) und kommen als neu
	// hinzu. Bei einem echten Thunderbird-Profil stammen die Bytes vom
	// Server, dann sind beide gleich.
	overlap, altered := 0, 0
	for _, i := range exp.Included {
		if onServer(i) {
			overlap++
			if bytes.IndexByte(c.Raw[i], 0) >= 0 {
				altered++
			}
		}
	}
	if altered == 0 {
		t.Fatal("corpus without NUL bytes in the overlap: adjust the test")
	}
	start = time.Now()
	rep := e.run(t, dir, FormatMbox, "", Options{})
	t.Logf("import: new %d, existing %d, skipped %d, errors %d (%.0f %% of the import already synced) in %s",
		rep.New, rep.Existing, rep.Skipped, rep.Errors, 100*float64(overlap)/float64(exp.New), time.Since(start).Round(time.Millisecond))
	checkCounts(t, rep, exp, overlap-altered)
	if n := e.count(t, `SELECT count(*) FROM messages`); n != synced+exp.New-overlap+altered {
		t.Fatalf("messages = %d, want %d (no duplicates)", n, synced+exp.New-overlap+altered)
	}
	// Genau die umkodierten Nachrichten gibt es zweimal: Serverfassung und
	// importierte Fassung.
	if n := e.count(t, `SELECT count(*) FROM (SELECT message_id_header FROM messages WHERE message_id_header IS NOT NULL
		GROUP BY message_id_header HAVING count(*) = 2 AND sum(deleted_remote_at IS NULL) = 1)`); n != altered {
		t.Fatalf("message IDs with a server and an import version = %d, want %d", n, altered)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE deleted_remote_at IS NULL`); n != synced {
		t.Fatalf("present on the server = %d, want %d", n, synced)
	}
	if rep := e.run(t, dir, FormatMbox, "", Options{Rescan: true}); rep.New != 0 {
		t.Fatalf("second run new = %d", rep.New)
	}
	if srep := syncOnce(); srep.New != 0 || srep.Removed != 0 || srep.Errors != 0 {
		t.Fatalf("sync after import = new %d removed %d errors %d", srep.New, srep.Removed, srep.Errors)
	}
}
