package mailsync

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync/atomic"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// T1a Punkt 3: Leere oder unvollständige Antworten des Servers löschen
// nichts. Der Fake-Server kann LIST leeren (bzw. INBOX weglassen) und
// UID SEARCH ALL um eine UID kürzen.
type lossyServer struct {
	u          *imapmemserver.User
	emptyList  atomic.Bool
	noInbox    atomic.Bool
	shortUIDs  atomic.Bool
	searchSeen atomic.Int32
}

type lossySession struct {
	*imapmemserver.UserSession
	s *lossyServer
}

func (l *lossySession) List(w *imapserver.ListWriter, ref string, patterns []string, opts *imap.ListOptions) error {
	if l.s.emptyList.Load() {
		return nil
	}
	if l.s.noInbox.Load() {
		// Nur der zweite Ordner, INBOX fehlt.
		return w.WriteList(&imap.ListData{Mailbox: "Archiv", Delim: '/'})
	}
	return l.UserSession.List(w, ref, patterns, opts)
}

func (l *lossySession) Search(kind imapserver.NumKind, c *imap.SearchCriteria, o *imap.SearchOptions) (*imap.SearchData, error) {
	d, err := l.UserSession.Search(kind, c, o)
	if err != nil || !l.s.shortUIDs.Load() || kind != imapserver.NumKindUID || len(c.UID) > 0 {
		return d, err
	}
	l.s.searchSeen.Add(1)
	set, ok := d.All.(imap.UIDSet)
	if !ok {
		return d, nil
	}
	nums, _ := set.Nums()
	if len(nums) > 0 {
		d.All = imap.UIDSetNum(nums[1:]...)
		d.Count = uint32(len(nums) - 1)
	}
	return d, nil
}

func newLossyServer(t *testing.T) (string, *lossyServer) {
	t.Helper()
	ls := &lossyServer{u: imapmemserver.NewUser(testUser, string(testPass))}
	for _, mb := range []string{"INBOX", "Archiv"} {
		_ = ls.u.Create(mb, nil)
	}
	for i := 1; i <= 5; i++ {
		mb := "INBOX"
		if i > 3 {
			mb = "Archiv"
		}
		if _, err := ls.u.Append(mb, lit(mail(i)), &imap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &lossySession{UserSession: imapmemserver.NewUserSession(ls.u), s: ls}, nil, nil
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
	return ln.Addr().String(), ls
}

func TestIncompleteServerAnswersRemoveNothing(t *testing.T) {
	addr, ls := newLossyServer(t)
	e := newEnv(t, addr)
	eng := e.engine(t, connect(t, addr, 0))
	ctx := context.Background()
	if _, err := eng.SyncAll(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	intact := func(step string) {
		t.Helper()
		if n := e.count(t, `SELECT count(*) FROM message_mailboxes`); n != 5 {
			t.Fatalf("%s: links = %d, want 5", step, n)
		}
		if n := e.count(t, `SELECT count(*) FROM mailboxes`); n != 2 {
			t.Fatalf("%s: mailboxes = %d, want 2", step, n)
		}
		if n := e.count(t, `SELECT count(*) FROM messages WHERE deleted_remote_at IS NOT NULL`); n != 0 {
			t.Fatalf("%s: %d messages marked deleted on the server", step, n)
		}
	}
	intact("first sync")

	// UID SEARCH ALL liefert eine UID weniger als EXISTS: Ordner
	// überspringen, nichts lösen, zählen.
	ls.shortUIDs.Store(true)
	rep, err := eng.SyncAll(ctx, Options{CheckDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if ls.searchSeen.Load() == 0 || rep.Incomplete != 2 || rep.Removed != 0 || rep.Errors != 2 {
		t.Fatalf("short UID list: searches=%d report=%+v", ls.searchSeen.Load(), rep)
	}
	intact("short UID list")
	ls.shortUIDs.Store(false)

	// LIST ohne Ordner bzw. ohne INBOX: Lauf abbrechen, nichts entfernen.
	for _, flag := range []*atomic.Bool{&ls.emptyList, &ls.noInbox} {
		flag.Store(true)
		rep, err := eng.SyncAll(ctx, Options{CheckDeleted: true})
		if !errors.Is(err, provider.ErrIncomplete) || !errors.Is(err, ErrIncompleteList) || rep.Incomplete != 1 {
			t.Fatalf("incomplete LIST: err=%v report=%+v", err, rep)
		}
		intact("incomplete LIST")
		flag.Store(false)
	}

	rep, err = eng.SyncAll(ctx, Options{CheckDeleted: true})
	if err != nil || rep.Incomplete != 0 || rep.Errors != 0 {
		t.Fatalf("recovered: err=%v report=%+v", err, rep)
	}
	intact("recovered")

	// Eine echte Löschung wird weiter erkannt.
	if err := ls.u.Delete("Archiv"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.SyncAll(ctx, Options{CheckDeleted: true}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM mailboxes`); n != 1 {
		t.Fatalf("removed mailbox still known: %d", n)
	}
}
