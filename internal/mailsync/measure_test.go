package mailsync

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// TestS17BuildMeasurementDB baut die Datenbank für die Messung von S1.7
// (docs/suche_messung.md): POSTKAUZ_S17_BUILD=<Datenbankdatei> (neu, der
// Blob-Store liegt daneben), POSTKAUZ_S17_COUNT=100000 (Standard). Der
// synthetische Korpus wird in den IMAP-Server im Prozess geladen und mit
// dem normalen Sync gespiegelt; danach baut der index-Job den Suchindex.
// Gemessen: Sync ohne Jobs und Index getrennt. Die Suchlatenz misst
// danach internal/jobs (TestS17SearchMeasurement).
func TestS17BuildMeasurementDB(t *testing.T) {
	path := os.Getenv("POSTKAUZ_S17_BUILD")
	if path == "" {
		t.Skip("set POSTKAUZ_S17_BUILD to build the measurement database (see docs/suche_messung.md)")
	}
	count := 100000
	if v, err := strconv.Atoi(os.Getenv("POSTKAUZ_S17_COUNT")); err == nil && v > 0 {
		count = v
	}
	ctx := context.Background()
	start := time.Now()
	c, err := corpus.Generate(corpus.Options{Count: count})
	if err != nil {
		t.Fatal(err)
	}
	u := imapmemserver.NewUser(testUser, string(testPass))
	_ = u.Create("INBOX", nil)
	var raw int64
	for i, e := range c.Manifest.Messages {
		if memSkip(e) {
			continue
		}
		_ = u.Create(e.Folder, nil)
		date, _ := time.Parse(time.RFC3339, e.InternalDate)
		var flags []imap.Flag
		for _, f := range e.Flags {
			flags = append(flags, imap.Flag(f))
		}
		if _, err := u.Append(e.Folder, lit(c.Raw[i]), &imap.AppendOptions{Flags: flags, Time: date}); err != nil {
			t.Fatal(err)
		}
		raw += int64(len(c.Raw[i]))
	}
	c = nil
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}}, InsecureAuth: true, Logger: log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	t.Logf("corpus: %d messages, %.0f MB raw, generated and loaded in %s", count, float64(raw)/1e6, time.Since(start).Round(time.Second))

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := blob.Open(path+".blobs", blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	acct, err := st.CreateAccount(ctx, store.Account{Name: "messung", Email: "messung@example.org", Host: "127.0.0.1", Port: 1,
		TLS: "none", Username: testUser, AuthType: "password", CredentialRef: "account/messung", InsecureTestServer: true})
	if err != nil {
		t.Fatal(err)
	}
	eng := &Engine{Store: st, Blobs: b, Remote: connect(t, ln.Addr().String(), 0), AccountID: acct}
	start = time.Now()
	rep, err := eng.SyncAll(ctx, Options{Window: 500})
	if err != nil {
		t.Fatal(err)
	}
	d := time.Since(start)
	t.Logf("sync without jobs: %d messages in %s (%.0f/s), %d errors", rep.Mirrored, d.Round(time.Second), float64(rep.Mirrored)/d.Seconds(), rep.Errors)

	start = time.Now()
	jr, err := (&jobs.Runner{Store: st, Blobs: b, TakeOver: true}).RunAccount(ctx, acct)
	if err != nil {
		t.Fatal(err)
	}
	d = time.Since(start)
	t.Logf("index job: %d messages in %s (%.0f/s), %d pending", jr.Indexed, d.Round(100*time.Millisecond), float64(jr.Indexed)/d.Seconds(), jr.IndexPending)
	if fi, err := os.Stat(path); err == nil {
		t.Logf("database file: %.0f MB (plus WAL)", float64(fi.Size())/1e6)
	}
}
