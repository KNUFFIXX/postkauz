package mailsync

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

const (
	testUser = "tester@example.invalid"
	testPass = secrets.Secret("pass-for-the-in-process-server")
)

var (
	corpusOnce sync.Once
	theCorpus  *corpus.Corpus
)

func standardCorpus(t testing.TB) *corpus.Corpus {
	t.Helper()
	corpusOnce.Do(func() {
		c, err := corpus.Generate(corpus.Options{})
		if err != nil {
			panic(err)
		}
		theCorpus = c
	})
	return theCorpus
}

type rawLit struct{ *bytes.Reader }

func (l rawLit) Size() int64 { return int64(l.Reader.Len()) }

func lit(b []byte) imap.LiteralReader { return rawLit{bytes.NewReader(b)} }

// memServer startet einen IMAP-Server im Prozess, befüllt mit dem
// Standardkorpus (Ordner, Flags, INTERNALDATE wie im Manifest).
func memServer(t testing.TB, load bool) (string, *imapmemserver.User) {
	t.Helper()
	u := imapmemserver.NewUser(testUser, string(testPass))
	_ = u.Create("INBOX", nil)
	if load {
		loadCorpus(t, u)
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String(), u
}

// memSkip: Der go-imap-Testserver liefert für Nachrichten mit einer
// Kopfzeile ohne Doppelpunkt einen leeren BODY[] (sein Parser scheitert);
// echte Server liefern die Rohbytes. Diese zwei Korpus-Nachrichten fehlen
// deshalb im In-Prozess-Server (der Docker-Integrationstest prüft alle 1.000).
func memSkip(e corpus.Entry) bool { return e.Broken == "header_no_colon" }

// memCount ist die Zahl der Korpus-Nachrichten im In-Prozess-Server.
const memCount = 998

func loadCorpus(t testing.TB, u *imapmemserver.User) {
	c := standardCorpus(t)
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
	}
}

type env struct {
	st     *store.SQLite
	blobs  *blob.Store
	acct   int64
	addr   string
	dbPath string
	dir    string
}

func newEnv(t testing.TB, addr string) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{addr: addr, dir: dir, dbPath: filepath.Join(dir, "pk.db")}
	e.open(t)
	id, err := e.st.CreateAccount(context.Background(), store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1",
		Port: 1, TLS: "none", Username: testUser, AuthType: "password", CredentialRef: "account/demo", InsecureTestServer: true})
	if err != nil {
		t.Fatal(err)
	}
	e.acct = id
	return e
}

func (e *env) open(t testing.TB) {
	t.Helper()
	st, err := store.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	b, err := blob.Open(filepath.Join(e.dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.st, e.blobs = st, b
}

func connect(t testing.TB, addr string, idleRestart time.Duration) *imapprov.Provider {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	prov, err := imapprov.Connect(context.Background(), imapprov.Config{Host: host, Port: p, TLS: "none", Username: testUser,
		InsecureTestServer: true, CommandTimeout: 5 * time.Second, IdleRestart: idleRestart,
		Session: imapx.SessionOptions{MaxAttempts: 2, Backoff: imapx.Backoff{Initial: 10 * time.Millisecond}}}, testPass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prov.Close() })
	return prov
}

func (e *env) engine(t testing.TB, r Remote) *Engine {
	return &Engine{Store: e.st, Blobs: e.blobs, Remote: r, AccountID: e.acct}
}

func (e *env) count(t testing.TB, q string, args ...any) int {
	t.Helper()
	db, err := store.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := db.QueryIntForTest(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// noDuplicates: jede Nachricht genau einmal, jede (Ordner, UID) genau einmal.
func (e *env) noDuplicates(t testing.TB, want int) {
	t.Helper()
	if n := e.count(t, `SELECT count(*) FROM messages`); n != want {
		t.Errorf("messages = %d, want %d", n, want)
	}
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes`); n != want {
		t.Errorf("message_mailboxes = %d, want %d", n, want)
	}
	if n := e.count(t, `SELECT count(*) FROM (SELECT message_id_header FROM messages WHERE message_id_header IS NOT NULL
		GROUP BY message_id_header HAVING count(*) > 1)`); n != 0 {
		t.Errorf("%d Message-IDs occur more than once", n)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE id NOT IN (SELECT message_id FROM bodies)`); n != 0 {
		t.Errorf("%d messages without body row", n)
	}
}

func folderCounts(c *corpus.Corpus) map[string]int {
	m := map[string]int{}
	for _, e := range c.Manifest.Messages {
		if memSkip(e) {
			continue
		}
		m[e.Folder]++
	}
	return m
}

// Akzeptanz S1.4: 1.000 Korpus-Nachrichten in Fenstern gespiegelt, mit
// geparsten Feldern; ein zweiter Lauf ändert nichts.
func TestInitialSyncMirrorsCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("full corpus (1,000 messages); skipped with -short")
	}
	c := standardCorpus(t)
	addr, _ := memServer(t, true)
	e := newEnv(t, addr)
	var windows atomic.Int32
	eng := e.engine(t, connect(t, addr, 0))
	eng.Progress = func(ev Event) {
		if ev.Kind == "window" {
			windows.Add(1)
		}
	}
	rep, err := eng.SyncAll(context.Background(), Options{Window: 100})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Mirrored != memCount || rep.Errors != 0 || rep.New != memCount {
		t.Fatalf("report = %+v", rep)
	}
	if windows.Load() < 10 {
		t.Fatalf("windows = %d, expected at least 10 with window 100", windows.Load())
	}
	want := folderCounts(c)
	for _, m := range rep.Mailboxes {
		if m.Mirrored != want[m.Path] || m.Total != want[m.Path] {
			t.Errorf("%s: mirrored %d, total %d, want %d", m.Path, m.Mirrored, m.Total, want[m.Path])
		}
	}
	e.noDuplicates(t, memCount)
	// Geparste Felder gegen das Manifest (Stichprobe über alle gültigen Nachrichten).
	for i, m := range c.Manifest.Messages {
		if m.MessageID == "" || m.Broken != "" || memSkip(m) {
			continue
		}
		n := e.count(t, `SELECT count(*) FROM messages WHERE message_id_header = ? AND subject = ? AND blob_sha256 IS NOT NULL`,
			m.MessageID, m.Subject)
		if n != 1 {
			t.Errorf("%s: no row with subject %q", m.File, m.Subject)
		}
		if i > 300 {
			break
		}
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE flag_seen = 1`); n != countFlag(c, `\Seen`) {
		t.Errorf("seen = %d, want %d", n, countFlag(c, `\Seen`))
	}
	if n := e.count(t, `SELECT count(*) FROM mailboxes WHERE sync_phase != 'ready'`); n != 0 {
		t.Errorf("%d mailboxes not ready", n)
	}
	if n := e.count(t, `SELECT count(*) FROM blobs WHERE sha256 IN (SELECT blob_sha256 FROM messages)`); n != memCount {
		t.Errorf("message blobs = %d", n)
	}
	// Akzeptanz S1.6: Threads des Korpus stimmen mit dem Manifest.
	e.checkThreads(t, c)
	// Bestand ohne Threads (gespiegelt vor S1.6) wird beim nächsten Lauf
	// nachgezogen, mit demselben Ergebnis.
	for _, q := range []string{`UPDATE messages SET thread_id = NULL`, `DELETE FROM thread_refs`, `DELETE FROM threads`} {
		if _, err := e.st.DBForTest().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rep2, err := eng.SyncAll(context.Background(), Options{})
	if err != nil || rep2.New != 0 || rep2.Updated != 0 || rep2.Removed != 0 || rep2.Mirrored != memCount {
		t.Fatalf("second run = %+v %v", rep2, err)
	}
	e.checkThreads(t, c)
	rep3, err := eng.SyncAll(context.Background(), Options{Full: true})
	if err != nil || rep3.New != 0 || rep3.Updated != 0 || rep3.Removed != 0 || rep3.Mirrored != memCount {
		t.Fatalf("full run = %+v %v", rep3, err)
	}
}

// checkThreads: Jeder Thread des Manifests ist genau ein Thread in der
// Datenbank, verschiedene Threads bleiben getrennt, Einzelnachrichten
// stehen allein (Nachrichten ohne Message-ID werden über ihre ID nicht
// gefunden und hier übersprungen).
func (e *env) checkThreads(t *testing.T, c *corpus.Corpus) {
	t.Helper()
	rows, err := e.st.DBForTest().Query(`SELECT m.message_id_header, m.thread_id, t.message_count FROM messages m
		LEFT JOIN threads t ON t.id = m.thread_id WHERE m.message_id_header IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	type tinfo struct {
		id, count int64
	}
	byMsg := map[string]tinfo{}
	for rows.Next() {
		var mid string
		var tid, cnt sql.NullInt64
		if err := rows.Scan(&mid, &tid, &cnt); err != nil {
			t.Fatal(err)
		}
		if !tid.Valid {
			t.Errorf("message %s has no thread", mid)
		}
		byMsg[mid] = tinfo{tid.Int64, cnt.Int64}
	}
	_ = rows.Close()
	manifestThread := map[string]int64{}
	dbThread := map[int64]string{}
	checked := 0
	for _, m := range c.Manifest.Messages {
		if m.MessageID == "" || memSkip(m) {
			continue
		}
		got, ok := byMsg[m.MessageID]
		if !ok {
			continue // z. B. Message-ID ohne Klammern anders gelesen
		}
		checked++
		if m.Thread == "" {
			if got.count != 1 {
				t.Errorf("%s: standalone message in a thread of %d", m.File, got.count)
			}
			continue
		}
		if prev, ok := manifestThread[m.Thread]; ok && prev != got.id {
			t.Errorf("%s: thread %s split (%d and %d)", m.File, m.Thread, prev, got.id)
		}
		manifestThread[m.Thread] = got.id
		if other, ok := dbThread[got.id]; ok && other != m.Thread {
			t.Errorf("%s: threads %s and %s merged", m.File, m.Thread, other)
		}
		dbThread[got.id] = m.Thread
	}
	if len(manifestThread) != 60 || checked < 900 {
		t.Fatalf("checked %d messages in %d threads", checked, len(manifestThread))
	}
}

func countFlag(c *corpus.Corpus, flag string) int {
	n := 0
	for _, e := range c.Manifest.Messages {
		if !memSkip(e) && slices.Contains(e.Flags, flag) {
			n++
		}
	}
	return n
}

// flakyRemote lässt FetchRaw nach n Nachrichten scheitern (Abbruch mitten
// im Fenster: Blobs sind geschrieben, die Transaktion nicht).
type flakyRemote struct {
	Remote
	left    atomic.Int32
	fetched atomic.Int32
}

var errCrash = fmt.Errorf("%w: simulated crash", ErrAborted)

func (f *flakyRemote) FetchRaw(ctx context.Context, mb string, uv uint32, uids []provider.UID, fn func(provider.UID, io.Reader) error) error {
	return f.Remote.FetchRaw(ctx, mb, uv, uids, func(u provider.UID, r io.Reader) error {
		f.fetched.Add(1)
		if f.left.Add(-1) < 0 {
			return errCrash
		}
		return fn(u, r)
	})
}

// Akzeptanz S1.4: Abbruch mitten im Fenster und nach einem Fenster; der
// Neustart setzt fort und ergibt dieselbe Anzahl ohne Doppelte.
func TestAbortedSyncResumesWithoutDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("full corpus (1,000 messages); skipped with -short")
	}
	addr, _ := memServer(t, true)
	e := newEnv(t, addr)
	prov := connect(t, addr, 0)
	fr := &flakyRemote{Remote: prov}
	fr.left.Store(150)
	if _, err := e.engine(t, fr).SyncAll(context.Background(), Options{Window: 100}); !errors.Is(err, ErrAborted) {
		t.Fatalf("first run: %v", err)
	}
	partial := e.count(t, `SELECT count(*) FROM messages`)
	if partial != 100 {
		t.Fatalf("after crash in window 3: %d messages, want the first committed window (100)", partial)
	}
	// Abbruch nach dem dritten gespeicherten Fenster.
	eng := e.engine(t, prov)
	windows := 0
	eng.AfterBatch = func(string, int) error {
		windows++
		if windows == 3 {
			return errCrash
		}
		return nil
	}
	if _, err := eng.SyncAll(context.Background(), Options{Window: 100}); !errors.Is(err, ErrAborted) {
		t.Fatalf("second run: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM messages`); n <= partial || n >= memCount {
		t.Fatalf("after abort: %d", n)
	}
	rep, err := e.engine(t, prov).SyncAll(context.Background(), Options{Window: 100})
	if err != nil || rep.Mirrored != memCount || rep.Errors != 0 {
		t.Fatalf("resume = %+v %v", rep, err)
	}
	e.noDuplicates(t, memCount)
}

// helperEnv startet den Kindprozess für den kill-9-Test.
const helperEnv = "POSTKAUZ_SYNC_KILL_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runKillHelper())
	}
	os.Exit(m.Run())
}

// runKillHelper synct langsam (kleine Fenster, Pause je Fenster), bis der
// Elternprozess ihn mit SIGKILL beendet.
func runKillHelper() int {
	ctx := context.Background()
	st, err := store.Open(os.Getenv("PK_DB"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	b, err := blob.Open(os.Getenv("PK_BLOBS"), blob.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	host, port, _ := net.SplitHostPort(os.Getenv("PK_ADDR"))
	p, _ := strconv.Atoi(port)
	prov, err := imapprov.Connect(ctx, imapprov.Config{Host: host, Port: p, TLS: "none", Username: testUser, InsecureTestServer: true}, testPass)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	acct, _ := strconv.ParseInt(os.Getenv("PK_ACCOUNT"), 10, 64)
	eng := &Engine{Store: st, Blobs: b, Remote: prov, AccountID: acct,
		AfterBatch: func(string, int) error { time.Sleep(40 * time.Millisecond); return nil }}
	if _, err := eng.SyncAll(ctx, Options{Window: 25}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	return 0
}

// Akzeptanz S1.4: `kill -9` mitten im Erstsync (echter Prozess, hart
// beendet: keine Aufräumarbeiten, offene Transaktion), Neustart ergibt
// dieselbe Anzahl ohne Doppelte.
func TestKill9DuringInitialSync(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process")
	}
	addr, _ := memServer(t, true)
	e := newEnv(t, addr)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "PK_DB="+e.dbPath, "PK_BLOBS="+filepath.Join(e.dir, "blobs"),
		"PK_ADDR="+addr, "PK_ACCOUNT="+strconv.FormatInt(e.acct, 10))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var seen int
	for time.Now().Before(deadline) {
		seen = e.count(t, `SELECT count(*) FROM message_mailboxes`)
		if seen >= 150 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil { // SIGKILL (Windows: TerminateProcess)
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if seen < 150 || seen >= memCount {
		t.Fatalf("child mirrored %d before the kill (stderr: %s)", seen, stderr.String())
	}
	before := e.count(t, `SELECT count(*) FROM messages`)
	t.Logf("killed after %d mirrored copies (%d messages committed)", seen, before)

	rep, err := e.engine(t, connect(t, addr, 0)).SyncAll(context.Background(), Options{Window: 200})
	if err != nil || rep.Mirrored != memCount || rep.Errors != 0 {
		t.Fatalf("restart = %+v %v", rep, err)
	}
	if rep.New != memCount-before {
		t.Errorf("restart mirrored %d new, want %d", rep.New, memCount-before)
	}
	e.noDuplicates(t, memCount)
}

// imapSession öffnet eine zweite Sitzung „von außen" (anderer Client).
func imapSession(t testing.TB, addr string) *imapclient.Client {
	t.Helper()
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Login(testUser, string(testPass)).Wait(); err != nil {
		t.Fatal(err)
	}
	return c
}

func uidOf(t testing.TB, c *imapclient.Client, mailbox, subject string) imap.UID {
	t.Helper()
	if _, err := c.Select(mailbox, nil).Wait(); err != nil {
		t.Fatal(err)
	}
	d, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: subject}}}, nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	uids := d.AllUIDs()
	if len(uids) == 0 {
		t.Fatalf("%s: no message with subject %q", mailbox, subject)
	}
	return uids[0]
}

// Akzeptanz S1.5 (Strategie uid-scan, In-Prozess-Server ohne CONDSTORE):
// Flag-Änderung, Löschung, neue Nachricht und Verschieben von außen.
func TestIncrementalSeesExternalChanges(t *testing.T) {
	addr, u := memServer(t, false)
	for i := 1; i <= 30; i++ {
		_, _ = u.Append("INBOX", lit(mail(i)), &imap.AppendOptions{})
	}
	_ = u.Create("Archiv", nil)
	e := newEnv(t, addr)
	prov := connect(t, addr, 0)
	if prov.Strategy() != provider.StrategyUIDScan {
		t.Fatalf("strategy = %s", prov.Strategy())
	}
	eng := e.engine(t, prov)
	if rep, err := eng.SyncAll(context.Background(), Options{Window: 7}); err != nil || rep.Mirrored != 30 {
		t.Fatalf("initial = %+v %v", rep, err)
	}
	out := imapSession(t, addr)
	seenUID := uidOf(t, out, "INBOX", "Nachricht 3")
	if err := out.Store(imap.UIDSetNum(seenUID), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	delUID := uidOf(t, out, "INBOX", "Nachricht 5")
	if err := out.Store(imap.UIDSetNum(delUID), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.UIDExpunge(imap.UIDSetNum(delUID)).Close(); err != nil {
		t.Fatal(err)
	}
	moveUID := uidOf(t, out, "INBOX", "Nachricht 7")
	if _, err := out.Move(imap.UIDSetNum(moveUID), "Archiv").Wait(); err != nil {
		t.Fatal(err)
	}
	_, _ = u.Append("INBOX", lit(mail(31)), &imap.AppendOptions{})

	rep, err := eng.SyncAll(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// INBOX: 1 neu, 1 Flag, 2 entfernt (gelöscht, verschoben); Archiv: 1 zugeordnet.
	if rep.Updated != 1 || rep.Removed != 2 || rep.New != 2 || rep.Mirrored != 30 {
		t.Fatalf("report = %+v", rep)
	}
	if n := e.count(t, `SELECT count(*) FROM messages`); n != 31 {
		t.Fatalf("messages = %d (moving must not duplicate)", n)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE subject = 'Nachricht 3' AND flag_seen = 1`); n != 1 {
		t.Fatal("seen flag not mirrored")
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE subject = 'Nachricht 5' AND deleted_remote_at IS NOT NULL`); n != 1 {
		t.Fatal("deletion not mirrored (archive must keep the message)")
	}
	if n := e.count(t, `SELECT count(*) FROM messages m JOIN message_mailboxes mm ON mm.message_id = m.id
		JOIN mailboxes b ON b.id = mm.mailbox_id WHERE m.subject = 'Nachricht 7' AND b.path = 'Archiv' AND m.deleted_remote_at IS NULL`); n != 1 {
		t.Fatal("moved message not linked in Archiv")
	}
}

func mail(i int) []byte {
	return []byte(fmt.Sprintf("Message-ID: <n%d@example.org>\r\nFrom: Absender <a@example.org>\r\nTo: b@example.org\r\n"+
		"Subject: Nachricht %d\r\nDate: Mon, 06 Jan 2025 10:%02d:00 +0100\r\n\r\nText %d\r\n", i, i, i%60, i))
}

// countingRemote zählt geladene Rohmails.
type countingRemote struct {
	Remote
	raw atomic.Int32
}

func (c *countingRemote) FetchRaw(ctx context.Context, mb string, uv uint32, uids []provider.UID, fn func(provider.UID, io.Reader) error) error {
	c.raw.Add(int32(len(uids)))
	return c.Remote.FetchRaw(ctx, mb, uv, uids, fn)
}

// Akzeptanz S1.5 / P5: Ordner gelöscht und neu angelegt (neues
// UIDVALIDITY), Inhalt gleich: Reset und Abgleich per Message-ID, ohne
// Doppelte und ohne die Inhalte erneut zu laden.
func TestUIDValidityChangeMatchesByMessageID(t *testing.T) {
	if testing.Short() {
		t.Skip("full corpus (1,000 messages); skipped with -short")
	}
	c := standardCorpus(t)
	addr, u := memServer(t, true)
	e := newEnv(t, addr)
	cr := &countingRemote{Remote: connect(t, addr, 0)}
	eng := e.engine(t, cr)
	if rep, err := eng.SyncAll(context.Background(), Options{}); err != nil || rep.Mirrored != memCount {
		t.Fatalf("initial = %+v %v", rep, err)
	}
	const folder = "Archiv/2025"
	if err := u.Delete(folder); err != nil {
		t.Fatal(err)
	}
	if err := u.Create(folder, nil); err != nil {
		t.Fatal(err)
	}
	n := 0
	for i, m := range c.Manifest.Messages {
		if m.Folder != folder || memSkip(m) {
			continue
		}
		date, _ := time.Parse(time.RFC3339, m.InternalDate)
		_, _ = u.Append(folder, lit(c.Raw[i]), &imap.AppendOptions{Time: date})
		n++
	}
	cr.raw.Store(0)
	rep, err := eng.SyncAll(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var mr MailboxReport
	for _, m := range rep.Mailboxes {
		if m.Path == folder {
			mr = m
		}
	}
	if !mr.Reset || mr.New != n || mr.Mirrored != n || rep.Mirrored != memCount {
		t.Fatalf("reset report = %+v (total %+v)", mr, rep)
	}
	// Nachrichten ohne Message-ID werden per Inhalt (content_hash) erkannt
	// und dafür geladen; alle anderen nur zugeordnet.
	if got := cr.raw.Load(); got > 4 {
		t.Errorf("re-fetched %d bodies after the reset, want only those without Message-ID", got)
	}
	e.noDuplicates(t, memCount)
	if n := e.count(t, `SELECT count(*) FROM messages WHERE deleted_remote_at IS NOT NULL`); n != 0 {
		t.Errorf("%d messages still marked deleted after re-matching", n)
	}
	// Die Flags kommen vom Server (neu angelegt ohne Flags): ungelesen.
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes mm JOIN mailboxes b ON b.id = mm.mailbox_id
		WHERE b.path = ? AND mm.flag_seen = 1`, folder); n != 0 {
		t.Errorf("flags after reset must come from the server, %d still seen", n)
	}
}

// Akzeptanz S1.5: Watch sieht eine neue Nachricht in der INBOX per IDLE
// nach wenigen Sekunden und eine Flag-Änderung in einem anderen Ordner nach
// einem Intervall.
func TestWatchIdleAndInterval(t *testing.T) {
	addr, u := memServer(t, false)
	_ = u.Create("Archiv", nil)
	_, _ = u.Append("Archiv", lit(mail(1)), &imap.AppendOptions{})
	e := newEnv(t, addr)
	prov := connect(t, addr, 300*time.Millisecond)
	eng := e.engine(t, prov)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rounds atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- eng.Watch(ctx, WatchOptions{Interval: 700 * time.Millisecond, IdleRetry: 50 * time.Millisecond,
			OnRound: func(Report, error) { rounds.Add(1) }})
	}()
	waitFor(t, 5*time.Second, func() bool { return e.count(t, `SELECT count(*) FROM messages`) == 1 })
	time.Sleep(100 * time.Millisecond) // IDLE läuft
	_, _ = u.Append("INBOX", lit(mail(2)), &imap.AppendOptions{})
	start := time.Now()
	waitFor(t, 5*time.Second, func() bool { return e.count(t, `SELECT count(*) FROM messages WHERE subject = 'Nachricht 2'`) == 1 })
	t.Logf("INBOX change visible after %s", time.Since(start))

	out := imapSession(t, addr)
	uid := uidOf(t, out, "Archiv", "Nachricht 1")
	if err := out.Store(imap.UIDSetNum(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagFlagged}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM messages WHERE subject = 'Nachricht 1' AND flag_flagged = 1`) == 1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rounds.Load() < 2 {
		t.Fatalf("rounds = %d", rounds.Load())
	}
}

func waitFor(t testing.TB, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", d)
}

func TestDiff(t *testing.T) {
	removed, missing := diff([]uint32{1, 2, 4, 7}, []provider.UID{2, 3, 4, 8, 9}, 9)
	if !slices.Equal(removed, []uint32{1, 7}) || !slices.Equal(missing, []provider.UID{3, 8}) {
		t.Fatalf("diff = %v %v", removed, missing)
	}
}

func TestContentHashIgnoresLineEndings(t *testing.T) {
	a, _ := ContentHash(strings.NewReader("A: b\r\n\r\nText\r\n"))
	b, _ := ContentHash(strings.NewReader("A: b\n\nText\n"))
	c, _ := ContentHash(strings.NewReader("A: b\n\nText!\n"))
	if a != b || a == c {
		t.Fatalf("hashes: %s %s %s", a, b, c)
	}
}

// Regression: Bei unlesbarem Kopf (> 1 MiB) bricht der Parser früh ab; der
// content_hash muss trotzdem die ganze Rohmail abdecken, sonst ordnet
// FindByContentHash eine andere Mail mit gleichem Anfang falsch zu.
func TestMirrorRawHashesWholeUnreadableMessage(t *testing.T) {
	b, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Blobs: b}
	hdr := "Message-ID: <x@example.org>\r\nX-Big: " + strings.Repeat("a", 2<<20) + "\r\n\r\n"
	m1 := hdr + "body one " + strings.Repeat("1", 200000)
	m2 := hdr + "body two " + strings.Repeat("2", 200000)
	n1, v1, err := e.mirrorRaw(strings.NewReader(m1), provider.Header{UID: 1, Size: int64(len(m1))})
	if err != nil {
		t.Fatal(err)
	}
	n2, _, err := e.mirrorRaw(strings.NewReader(m2), provider.Header{UID: 2, Size: int64(len(m2))})
	if err != nil {
		t.Fatal(err)
	}
	if n1.Subject != "" || n1.ContentHash == "" {
		t.Fatalf("expected unreadable header with hash, got subject %q hash %q", n1.Subject, n1.ContentHash)
	}
	if n1.ContentHash == n2.ContentHash {
		t.Fatalf("different messages share content_hash %s", n1.ContentHash)
	}
	want, _ := ContentHash(strings.NewReader(m1))
	if n1.ContentHash != want || len(v1) != 3 || v1[0] != want {
		t.Fatalf("content_hash = %s (variants %v), want hash of whole message %s first", n1.ContentHash, v1, want)
	}
}

// Varianten am Zeilenende (S1.10): exakter Wert zuerst, gleich ContentHash;
// dazu 0, 1, 2 Zeilenenden am Schluss.
func TestContentHasherVariants(t *testing.T) {
	sum := func(s string) string {
		h, _ := ContentHash(strings.NewReader(s))
		return h
	}
	h := NewContentHasher()
	for _, part := range []string{"Subject: x\r\n\r\nText\r", "\n", "\r\n"} {
		_, _ = h.Write([]byte(part))
	}
	exact, variants := h.Sums()
	if exact != sum("Subject: x\n\nText\n\n") || len(variants) != 3 || variants[0] != exact ||
		!slices.Contains(variants, sum("Subject: x\n\nText")) || !slices.Contains(variants, sum("Subject: x\n\nText\n")) {
		t.Fatalf("variants wrong: %v", variants)
	}
	h = NewContentHasher()
	_, _ = h.Write([]byte("\n\n"))
	if e, v := h.Sums(); e != sum("\n\n") || len(v) != 3 {
		t.Fatalf("only line breaks: %s %v", e, v)
	}
}

func TestSnippet(t *testing.T) {
	if s := snippet("  Hallo\r\n\r\n  Welt  "); s != "Hallo Welt" {
		t.Fatalf("snippet = %q", s)
	}
	if s := snippet(strings.Repeat("ä", 300)); len([]rune(s)) != snippetLen {
		t.Fatalf("snippet length = %d", len([]rune(s)))
	}
}

func TestClampIdleRestart(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{0: 8 * time.Minute, time.Second: 5 * time.Minute,
		7 * time.Minute: 7 * time.Minute, time.Hour: 10 * time.Minute} {
		if got := ClampIdleRestart(in); got != want {
			t.Errorf("ClampIdleRestart(%s) = %s", in, got)
		}
	}
}

// brokenRemote liefert für eine UID eine unlesbare Antwort (ErrProtocol),
// sobald sie im angefragten Fenster steht.
type brokenRemote struct {
	Remote
	bad   provider.UID
	calls atomic.Int32
}

func (b *brokenRemote) has(uids []provider.UID) bool { return slices.Contains(uids, b.bad) }

func (b *brokenRemote) FetchHeaders(ctx context.Context, mb string, uv uint32, uids []provider.UID) ([]provider.Header, error) {
	b.calls.Add(1)
	if b.has(uids) && len(uids) > 0 && mb == "INBOX" {
		return nil, fmt.Errorf("%w: imapwire: expected SP", provider.ErrProtocol)
	}
	return b.Remote.FetchHeaders(ctx, mb, uv, uids)
}

// Eine Nachricht, die der Server nicht lesbar ausliefert, wird durch
// Halbieren des Fensters eingegrenzt und übersprungen; alle anderen werden
// gespiegelt, der Fehler wird gezählt.
func TestProtocolErrorIsIsolatedByBisection(t *testing.T) {
	addr, u := memServer(t, false)
	for i := 1; i <= 40; i++ {
		_, _ = u.Append("INBOX", lit(mail(i)), &imap.AppendOptions{})
	}
	e := newEnv(t, addr)
	br := &brokenRemote{Remote: connect(t, addr, 0), bad: 17}
	rep, err := e.engine(t, br).SyncAll(context.Background(), Options{Window: 40})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Mirrored != 39 || rep.Errors != 1 || len(rep.Mailboxes[0].Problems) != 1 || !strings.Contains(rep.Mailboxes[0].Problems[0], "uid 17") {
		t.Fatalf("report = %+v", rep)
	}
	if c := br.calls.Load(); c > 15 {
		t.Fatalf("%d header fetches; bisection should need about 2*log2(40)", c)
	}
	// Liefert der Server die Nachricht später lesbar, holt der nächste Lauf sie nach.
	rep, err = e.engine(t, br.Remote).SyncAll(context.Background(), Options{})
	if err != nil || rep.Mirrored != 40 || rep.New != 1 || rep.Errors != 0 {
		t.Fatalf("second run = %+v %v", rep, err)
	}
}

// Zwei schreibende Syncs desselben Kontos (`sync --watch` und ein manueller
// `sync`, hier zwei Runner auf eigenen Store-Instanzen wie zwei Prozesse):
// Der zweite bricht sofort mit ErrSyncRunning ab, ohne sich am Server
// anzumelden. Ein Trockenlauf und ein späterer Lauf sind nicht betroffen.
func TestRunnerRefusesSecondSyncOfSameAccount(t *testing.T) {
	e := newEnv(t, "")
	a, err := e.st.AccountByName(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	blocking := func(ctx context.Context, _ store.Account, _ secrets.Secret, _ time.Duration) (RemoteCloser, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil, errors.New("test: no server")
	}
	first := &Runner{Store: e.st, Blobs: e.blobs, Connect: blocking}
	done := make(chan error, 1)
	go func() {
		_, err := first.Sync(context.Background(), a, testPass, ops.SyncOptions{}, nil)
		done <- err
	}()
	<-entered

	st2, err := store.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	second := &Runner{Store: st2, Blobs: e.blobs, Connect: blocking}
	_, err = second.Sync(context.Background(), a, testPass, ops.SyncOptions{Full: true}, nil)
	if !errors.Is(err, ops.ErrSyncRunning) || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("second sync: err = %v, want ErrSyncRunning", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("second sync connected to the server (%d connects)", n)
	}
	// Trockenlauf schreibt nichts und braucht keine Sperre.
	if _, err := second.Sync(context.Background(), a, testPass, ops.SyncOptions{DryRun: true}, nil); errors.Is(err, ops.ErrSyncRunning) {
		t.Fatalf("dry run was blocked by the lock: %v", err)
	}

	close(release)
	if err := <-done; err == nil || errors.Is(err, ops.ErrSyncRunning) {
		t.Fatalf("first sync: %v", err)
	}
	// Nach dem Ende des ersten Laufs ist das Konto wieder frei.
	if _, err := second.Sync(context.Background(), a, testPass, ops.SyncOptions{}, nil); errors.Is(err, ops.ErrSyncRunning) {
		t.Fatalf("lock not released: %v", err)
	}
}

func TestSizeGroups(t *testing.T) {
	hs := []provider.Header{{UID: 1, Size: 10}, {UID: 2, Size: 10}, {UID: 3, Size: 50}, {UID: 4, Size: 1}, {UID: 5, Size: 19}}
	got := sizeGroups(hs, 20)
	want := [][]provider.UID{{1, 2}, {3}, {4, 5}}
	if len(got) != len(want) {
		t.Fatalf("groups = %v", got)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Fatalf("groups = %v, want %v", got, want)
		}
	}
}
