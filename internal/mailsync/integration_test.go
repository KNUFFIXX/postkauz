package mailsync

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/corpus/imapload"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Integrationstests des Syncs gegen die Docker-Testserver (deploy/testmail;
// `make testmail-up testmail-setup integration`). Sie laufen nur mit
// POSTKAUZ_IT=1 bzw. POSTKAUZ_IT=dovecot,stalwart,greenmail. Jeder Lauf legt
// den Korpus unter einem eigenen Ordner ab (Stalwart und GreenMail haben nur
// ein gemeinsames Testkonto) und löscht ihn am Ende wieder.

type itServer struct {
	name     string
	host     string
	port     int
	tls      string
	user     func() string
	strategy provider.Strategy
}

var itServers = []itServer{
	{name: "dovecot", host: "127.0.0.1", port: 11143, tls: "none", strategy: provider.StrategyCondStore,
		user: func() string { return fmt.Sprintf("it-sync-%d@example.org", time.Now().UnixNano()) }},
	{name: "stalwart", host: "127.0.0.1", port: 12993, tls: "tls", strategy: provider.StrategyCondStore,
		user: func() string { return "test" }},
	{name: "greenmail", host: "127.0.0.1", port: 13143, tls: "none", strategy: provider.StrategyUIDScan,
		user: func() string { return "test" }},
}

const itPass = secrets.Secret("pass")

func itRun(t *testing.T, f func(t *testing.T, srv itServer, user string)) {
	if os.Getenv("POSTKAUZ_IT") == "" {
		t.Skip("integration tests need Docker test servers: make testmail-up testmail-setup integration")
	}
	sel := os.Getenv("POSTKAUZ_IT")
	for _, srv := range itServers {
		t.Run(srv.name, func(t *testing.T) {
			if sel != "1" && sel != "all" && !slices.Contains(strings.Split(sel, ","), srv.name) {
				t.Skip("not selected in POSTKAUZ_IT")
			}
			f(t, srv, srv.user())
		})
	}
}

func itClient(t *testing.T, srv itServer, user string) *imapclient.Client {
	t.Helper()
	addr := fmt.Sprintf("%s:%d", srv.host, srv.port)
	var c *imapclient.Client
	var err error
	if srv.tls == "tls" {
		c, err = imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: &tls.Config{InsecureSkipVerify: true}}) //nolint:gosec // lokaler Testserver
	} else {
		c, err = imapclient.DialInsecure(addr, nil)
	}
	if err != nil {
		t.Fatalf("server not reachable (make testmail-up testmail-setup): %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Login(user, string(itPass)).Wait(); err != nil {
		t.Fatal(err)
	}
	return c
}

func itProvider(t *testing.T, srv itServer, user string, idleRestart time.Duration) *imapprov.Provider {
	t.Helper()
	p, err := imapprov.Connect(context.Background(), imapprov.Config{Host: srv.host, Port: srv.port, TLS: srv.tls, Username: user,
		InsecureTestServer: true, CommandTimeout: 10 * time.Second, IdleRestart: idleRestart}, itPass)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// itLoad legt den Korpus unter einem neuen Ordner ab; liefert Präfix,
// Trennzeichen und die Ordner mit ihrer Nachrichtenzahl.
func itLoad(t *testing.T, srv itServer, user string) (prefix, delim string, folders map[string]int) {
	t.Helper()
	c, err := imapx.Dial(context.Background(), imapx.Config{Host: srv.host, Port: srv.port, TLS: imapx.TLSMode(srv.tls),
		InsecureTestServer: true, CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login(user, itPass); err != nil {
		t.Fatal(err)
	}
	prefix = fmt.Sprintf("PKIT%d", time.Now().UnixNano())
	start := time.Now()
	folders, err = imapload.Load(context.Background(), c, standardCorpus(t), imapload.Options{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: corpus loaded in %s", srv.name, time.Since(start).Round(time.Millisecond))
	delim, _ = imapload.Delimiter(c)
	t.Cleanup(func() {
		cc := itClient(t, srv, user)
		for f := range folders {
			_ = cc.Delete(f).Wait()
		}
		for f := range folders {
			if i := strings.LastIndex(f, delim); i > 0 {
				_ = cc.Delete(f[:i]).Wait()
			}
		}
		_ = cc.Delete(prefix).Wait()
	})
	return prefix, delim, folders
}

func keys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Akzeptanz S1.4 und S1.5 je Server: 1.000 Korpus-Nachrichten in Fenstern,
// Abbruch und Wiederaufnahme ohne Doppelte; danach Änderungen von außen
// (Flag, Löschung, neue Nachricht) mit der Strategie des Servers
// (CONDSTORE bzw. UID-Scan) und ein UIDVALIDITY-Wechsel (P5).
func TestIntegrationSync(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		prefix, delim, folders := itLoad(t, srv, user)
		e := newEnv(t, "")
		prov := itProvider(t, srv, user, 0)
		if s := prov.Strategy(); s != srv.strategy {
			t.Fatalf("strategy = %s, want %s", s, srv.strategy)
		}
		opts := Options{Window: 100, Mailboxes: keys(folders)}
		eng := e.engine(t, prov)
		windows := 0
		eng.AfterBatch = func(string, int) error {
			if windows++; windows == 4 {
				return errCrash
			}
			return nil
		}
		if _, err := eng.SyncAll(context.Background(), opts); err == nil {
			t.Fatal("expected the simulated crash")
		}
		partial := e.count(t, `SELECT count(*) FROM messages`)
		eng.AfterBatch = nil
		start := time.Now()
		rep, err := eng.SyncAll(context.Background(), opts)
		if err != nil || rep.Mirrored != 1000 || rep.Errors != 0 || rep.New != 1000-partial {
			t.Fatalf("resume after %d = %+v %v", partial, rep, err)
		}
		t.Logf("%s: crashed after %d, resumed %d in %s", srv.name, partial, rep.New, time.Since(start).Round(time.Millisecond))
		e.noDuplicates(t, 1000)
		// S1.7: Index-Job über die vom Server gelieferten Nachrichten, zwei
		// Fälle des Suchtestsatzes (Teilwort, ß/ss gleich).
		start = time.Now()
		jr, err := (&jobs.Runner{Store: e.st, Blobs: e.blobs, TakeOver: true}).RunAccount(context.Background(), e.acct)
		if err != nil || jr.Indexed != 1000 || jr.IndexPending != 0 {
			t.Fatalf("index job = %+v %v", jr, err)
		}
		t.Logf("%s: indexed %d messages in %s", srv.name, jr.Indexed, time.Since(start).Round(time.Millisecond))
		hits := func(q string) int {
			parsed, err := search.Parse(q, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			res, err := e.st.Search(context.Background(), store.SearchParams{Query: parsed, AccountID: e.acct, Limit: 1, Now: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			return res.Total
		}
		if a, b := hits("Erdgeschoss"), hits("Erdgeschoß"); a == 0 || a != b {
			t.Errorf("Erdgeschoss %d, Erdgeschoß %d", a, b)
		}
		if n := hits("Vollstreck"); n == 0 {
			t.Error("Vollstreck: no hits")
		}
		for _, m := range rep.Mailboxes {
			if m.Mirrored != folders[m.Path] {
				t.Errorf("%s: mirrored %d, want %d", m.Path, m.Mirrored, folders[m.Path])
			}
		}

		start = time.Now()
		rep, err = eng.SyncAll(context.Background(), Options{Mailboxes: opts.Mailboxes})
		if err != nil || rep.New+rep.Updated+rep.Removed != 0 {
			t.Fatalf("idle incremental = %+v %v", rep, err)
		}
		idle := time.Since(start)
		t.Logf("%s: idle incremental sync in %s", srv.name, idle.Round(time.Millisecond))
		if idle > 5*time.Second {
			t.Errorf("idle incremental took %s (criterion: under 5 s)", idle)
		}

		// Änderungen von außen.
		inbox := imapload.Folder("INBOX", prefix, delim)
		arch := imapload.Folder("Archiv/2024", prefix, delim)
		out := itClient(t, srv, user)
		if _, err := out.Select(inbox, nil).Wait(); err != nil {
			t.Fatal(err)
		}
		unseen, err := out.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
		if err != nil || len(unseen.AllUIDs()) == 0 {
			t.Fatalf("unseen search: %v", err)
		}
		seenUID := unseen.AllUIDs()[0]
		if err := out.Store(imap.UIDSetNum(seenUID), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}, Silent: true}, nil).Close(); err != nil {
			t.Fatal(err)
		}
		app := out.Append(inbox, int64(len(mail(9002))), nil)
		_, _ = app.Write(mail(9002))
		_ = app.Close()
		if _, err := app.Wait(); err != nil {
			t.Fatal(err)
		}
		if _, err := out.Select(arch, nil).Wait(); err != nil {
			t.Fatal(err)
		}
		all, _ := out.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
		delUID := all.AllUIDs()[0]
		if err := out.Store(imap.UIDSetNum(delUID), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
			t.Fatal(err)
		}
		if err := out.UIDExpunge(imap.UIDSetNum(delUID)).Close(); err != nil {
			t.Fatal(err)
		}
		rep, err = eng.SyncAll(context.Background(), Options{Mailboxes: opts.Mailboxes})
		if err != nil || rep.Updated != 1 || rep.Removed != 1 || rep.New != 1 || rep.Mirrored != 1000 {
			t.Fatalf("after external changes = %+v %v", rep, err)
		}
		if n := e.count(t, `SELECT count(*) FROM messages WHERE deleted_remote_at IS NOT NULL`); n != 1 {
			t.Errorf("deleted_remote = %d, want 1", n)
		}

		// UIDVALIDITY-Wechsel: Drafts löschen, neu anlegen, gleiche Nachrichten.
		drafts := imapload.Folder("Drafts", prefix, delim)
		before, _ := out.Status(drafts, &imap.StatusOptions{UIDValidity: true}).Wait()
		c := standardCorpus(t)
		var recreated int
		for attempt := 0; attempt < 3; attempt++ {
			if err := out.Delete(drafts).Wait(); err != nil {
				t.Fatal(err)
			}
			if err := out.Create(drafts, nil).Wait(); err != nil {
				t.Fatal(err)
			}
			after, _ := out.Status(drafts, &imap.StatusOptions{UIDValidity: true}).Wait()
			if after.UIDValidity != before.UIDValidity {
				break
			}
			time.Sleep(1100 * time.Millisecond) // Dovecot vergibt UIDVALIDITY sekundengenau
		}
		for i, m := range c.Manifest.Messages {
			if m.Folder != "Drafts" {
				continue
			}
			cmd := out.Append(drafts, int64(len(c.Raw[i])), nil)
			_, _ = cmd.Write(c.Raw[i])
			_ = cmd.Close()
			if _, err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			recreated++
		}
		cr := &countingRemote{Remote: prov}
		rep, err = e.engine(t, cr).SyncAll(context.Background(), Options{Mailboxes: opts.Mailboxes})
		if err != nil {
			t.Fatal(err)
		}
		var dr MailboxReport
		for _, m := range rep.Mailboxes {
			if m.Path == drafts {
				dr = m
			}
		}
		if !dr.Reset || dr.Mirrored != recreated || rep.Mirrored != 1000 {
			t.Fatalf("UIDVALIDITY reset = %+v (total %d)", dr, rep.Mirrored)
		}
		if n := e.count(t, `SELECT count(*) FROM messages`); n != 1001 {
			t.Errorf("messages after reset = %d, want 1001 (1000 + 1 new, no duplicates)", n)
		}
		t.Logf("%s: UIDVALIDITY reset re-matched %d messages, %d bodies fetched again", srv.name, dr.Mirrored, cr.raw.Load())
	})
}

// Akzeptanz S1.5: Eine neue Nachricht im per IDLE beobachteten Ordner ist
// nach wenigen Sekunden lokal sichtbar.
func TestIntegrationIdle(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		prefix, delim, _ := itLoad(t, srv, user)
		inbox := imapload.Folder("INBOX", prefix, delim)
		e := newEnv(t, "")
		eng := e.engine(t, itProvider(t, srv, user, 0))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- eng.Watch(ctx, WatchOptions{Options: Options{Mailboxes: []string{inbox}}, Interval: time.Hour, IdleMailbox: inbox})
		}()
		// Nachrichten der INBOX laut Manifest (echte Server liefern alle aus).
		want := 0
		for _, m := range standardCorpus(t).Manifest.Messages {
			if m.Folder == "INBOX" {
				want++
			}
		}
		waitFor(t, 60*time.Second, func() bool { return e.count(t, `SELECT count(*) FROM messages`) == want })
		time.Sleep(500 * time.Millisecond)
		out := itClient(t, srv, user)
		start := time.Now()
		cmd := out.Append(inbox, int64(len(mail(9100))), nil)
		_, _ = cmd.Write(mail(9100))
		_ = cmd.Close()
		if _, err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 15*time.Second, func() bool { return e.count(t, `SELECT count(*) FROM messages`) == want+1 })
		t.Logf("%s: new message visible after %s via IDLE", srv.name, time.Since(start).Round(time.Millisecond))
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
