package importer

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

const (
	memUser = "tester@example.invalid"
	memPass = secrets.Secret("pass-for-the-in-process-server")
)

type rawLit struct{ *bytes.Reader }

func (l rawLit) Size() int64 { return int64(l.Reader.Len()) }

// memServer: IMAP-Server im Prozess mit den Korpus-Nachrichten, für die
// keep gilt. Der go-imap-Testserver liefert für Kopfzeilen ohne Doppelpunkt
// einen leeren BODY[]; diese Nachrichten lädt er nicht (wie in mailsync).
func memServer(t testing.TB, c *corpus.Corpus, keep func(i int) bool) string {
	t.Helper()
	u := imapmemserver.NewUser(memUser, string(memPass))
	_ = u.Create("INBOX", nil)
	for i, e := range c.Manifest.Messages {
		if !keep(i) {
			continue
		}
		_ = u.Create(e.Folder, nil)
		date, _ := time.Parse(time.RFC3339, e.InternalDate)
		var flags []imap.Flag
		for _, f := range e.Flags {
			flags = append(flags, imap.Flag(f))
		}
		if _, err := u.Append(e.Folder, rawLit{bytes.NewReader(c.Raw[i])}, &imap.AppendOptions{Flags: flags, Time: date}); err != nil {
			t.Fatal(err)
		}
	}
	return serveMem(t, u, nil)
}

// countingSession zählt FETCH-Kommandos, die den ganzen Inhalt anfordern
// (BODY[] bzw. BINARY[]), nicht nur Kopf oder Größe.
type countingSession struct {
	*imapmemserver.UserSession
	bodies *atomic.Int64
}

func (s countingSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	for _, sec := range options.BodySection {
		if sec.Specifier == imap.PartSpecifierNone && len(sec.Part) == 0 {
			s.bodies.Add(1)
		}
	}
	if len(options.BinarySection) > 0 {
		s.bodies.Add(1)
	}
	return s.UserSession.Fetch(w, numSet, options)
}

// serveMem startet einen IMAP-Server im Prozess für den Benutzer u
// (nur localhost) und liefert seine Adresse. bodies (optional) zählt die
// Inhaltsabrufe.
func serveMem(t testing.TB, u *imapmemserver.User, bodies *atomic.Int64) string {
	t.Helper()
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			if bodies != nil {
				return countingSession{UserSession: imapmemserver.NewUserSession(u), bodies: bodies}, nil, nil
			}
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
	return ln.Addr().String()
}

func (e *env) sync(t testing.TB, addr string) mailsync.Report {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	prov, err := imapprov.Connect(context.Background(), imapprov.Config{Host: host, Port: p, TLS: "none", Username: memUser,
		InsecureTestServer: true, CommandTimeout: 10 * time.Second}, memPass)
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

// overlapCase beschreibt Server- und Importbestand: Der Server hat den
// Korpus ab Index 300, der Import die Indizes unter 430 (rund 30 % des
// Imports liegen also schon auf dem Server).
func overlapCase(c *corpus.Corpus) (server func(i int) bool, imported func(i int) bool) {
	server = func(i int) bool { return i >= 300 && c.Manifest.Messages[i].Broken != "header_no_colon" }
	imported = func(i int) bool { return i < 430 }
	return
}

// Import mit Überlappung zu einem gesyncten Konto (A01 S1.10): keine
// Doppelten, vorhandene Nachrichten bekommen nur den Ordner (Flags bleiben
// die des Servers), Zählung stimmt, zweiter Lauf new: 0, ein späterer Sync
// sieht nichts Neues und der Import stört ihn nicht.
func TestImportOverlapWithSyncedAccount(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	server, imported := overlapCase(c)
	addr := memServer(t, c, server)
	e := newEnv(t)
	srep := e.sync(t, addr)
	synced := e.count(t, `SELECT count(*) FROM messages`)
	if srep.Errors != 0 || synced == 0 {
		t.Fatalf("sync = %+v", srep)
	}

	dir := t.TempDir()
	exp, err := c.WriteThunderbird(dir, corpus.ImportOptions{Only: imported})
	if err != nil {
		t.Fatal(err)
	}
	var overlap []int
	noID := 0
	for _, i := range exp.Included {
		if server(i) {
			overlap = append(overlap, i)
			if c.Manifest.Messages[i].MessageID == "" {
				noID++
			}
		}
	}
	share := float64(len(overlap)) / float64(exp.New)
	t.Logf("import %d messages, %d (%.0f %%) already synced, %d of them without Message-ID (matched by content_hash)",
		exp.New, len(overlap), share*100, noID)
	if share < 0.28 || share > 0.32 || noID == 0 {
		t.Fatalf("test data: overlap %.2f, without Message-ID %d", share, noID)
	}
	rep := e.run(t, dir, FormatMbox, "", Options{})
	checkCounts(t, rep, exp, len(overlap))
	if n := e.count(t, `SELECT count(*) FROM messages`); n != synced+exp.New-len(overlap) {
		t.Fatalf("messages = %d, want %d", n, synced+exp.New-len(overlap))
	}
	if n := e.count(t, `SELECT count(*) FROM (SELECT message_id_header FROM messages WHERE message_id_header IS NOT NULL
		GROUP BY message_id_header HAVING count(*) > 1)`); n != 0 {
		t.Fatalf("%d duplicate Message-IDs", n)
	}
	// Server-Stand unverändert: gleich viele vorhandene Nachrichten, gleiche
	// Flags; jede überlappende hat ihren Server-Ordner und den Import-Ordner.
	if n := e.count(t, `SELECT count(*) FROM messages WHERE deleted_remote_at IS NULL`); n != synced {
		t.Fatalf("present on the server = %d, want %d", n, synced)
	}
	for _, i := range overlap {
		m := c.Manifest.Messages[i]
		if m.MessageID == "" {
			continue
		}
		seen := 0
		if slices.Contains(m.Flags, `\Seen`) {
			seen = 1
		}
		if n := e.count(t, `SELECT count(*) FROM messages msg WHERE msg.message_id_header = ? AND msg.flag_seen = ?
			AND EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = msg.id)
			AND EXISTS (SELECT 1 FROM message_import_folders mf WHERE mf.message_id = msg.id)`, m.MessageID, seen); n != 1 {
			t.Errorf("%s: overlapping message lost its server state or got no import folder", m.File)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM messages msg WHERE msg.message_id_header IS NULL
		AND EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = msg.id)
		AND EXISTS (SELECT 1 FROM message_import_folders mf WHERE mf.message_id = msg.id)`); n != noID {
		t.Errorf("messages without Message-ID matched by content_hash = %d, want %d", n, noID)
	}

	// Zweiter Lauf: new: 0 (unverändert bzw. mit --rescan alles vorhanden).
	if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 0 {
		t.Fatalf("second run new = %d", rep.New)
	}
	if rep := e.run(t, dir, FormatMbox, "", Options{Rescan: true}); rep.New != 0 || rep.Existing != exp.New+exp.Copies {
		t.Fatalf("rescan = new %d existing %d", rep.New, rep.Existing)
	}
	// Ein weiterer Sync: nichts Neues, nichts entfernt, Import bleibt.
	srep = e.sync(t, addr)
	if srep.New != 0 || srep.Removed != 0 || srep.Mirrored != synced {
		t.Fatalf("sync after import = new %d removed %d mirrored %d", srep.New, srep.Removed, srep.Mirrored)
	}
	if n := e.count(t, `SELECT count(*) FROM messages`); n != synced+exp.New-len(overlap) {
		t.Fatalf("messages after the second sync = %d", n)
	}
}

// Import zuerst, Sync danach (Erstabgleich, ADR-003): Der Sync erkennt die
// importierten Nachrichten (Message-ID und Größe bzw. content_hash) und lädt
// sie nicht doppelt; sie verlieren deleted_remote_at.
func TestSyncAfterImport(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	server, imported := overlapCase(c)
	addr := memServer(t, c, server)
	e := newEnv(t)
	dir := filepath.Join(t.TempDir(), "tb")
	exp, err := c.WriteThunderbird(dir, corpus.ImportOptions{Only: imported})
	if err != nil {
		t.Fatal(err)
	}
	rep := e.run(t, dir, FormatMbox, "", Options{})
	checkCounts(t, rep, exp, 0)
	serverCount := 0
	for i := range c.Manifest.Messages {
		if server(i) {
			serverCount++
		}
	}
	overlap := 0
	for _, i := range exp.Included {
		if server(i) {
			overlap++
		}
	}
	srep := e.sync(t, addr)
	if srep.Errors != 0 || srep.Mirrored != serverCount {
		t.Fatalf("sync = errors %d mirrored %d, want %d", srep.Errors, srep.Mirrored, serverCount)
	}
	if n := e.count(t, `SELECT count(*) FROM messages`); n != exp.New+serverCount-overlap {
		t.Fatalf("messages = %d, want %d (no duplicates)", n, exp.New+serverCount-overlap)
	}
}
