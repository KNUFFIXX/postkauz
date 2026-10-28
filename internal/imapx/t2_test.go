package imapx

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// recSession zählt schreibende Kommandos am Server.
type recSession struct {
	*imapmemserver.UserSession
	mu    *sync.Mutex
	calls *[]string
}

func (s recSession) rec(c string) {
	s.mu.Lock()
	*s.calls = append(*s.calls, c)
	s.mu.Unlock()
}

func (s recSession) Store(w *imapserver.FetchWriter, n imap.NumSet, f *imap.StoreFlags, o *imap.StoreOptions) error {
	s.rec("STORE")
	return s.UserSession.Store(w, n, f, o)
}

func (s recSession) Copy(n imap.NumSet, dest string) (*imap.CopyData, error) {
	s.rec("COPY")
	return s.UserSession.Copy(n, dest)
}

func (s recSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	s.rec("EXPUNGE")
	return s.UserSession.Expunge(w, uids)
}

func (s recSession) Move(w *imapserver.MoveWriter, n imap.NumSet, dest string) error {
	s.rec("MOVE")
	return s.UserSession.Move(w, n, dest)
}

// recServer startet einen Server mit den Fähigkeiten caps, der STORE,
// COPY, EXPUNGE und MOVE zählt; drei Nachrichten in INBOX.
func recServer(t *testing.T, caps imap.CapSet) (string, func() []string) {
	t.Helper()
	u := imapmemserver.NewUser(testUser, testPass)
	for _, mb := range []string{"INBOX", "Archiv"} {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		if _, err := u.Append("INBOX", literal(testMail(i)), &imap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var calls []string
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return recSession{UserSession: imapmemserver.NewUserSession(u), mu: &mu, calls: &calls}, nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), calls...)
	}
}

// A13.1: Ohne MOVE beim Server meldet Move ErrUnsupported, und kein
// Kommando, das kopiert, Flags setzt oder löscht, erreicht den Server
// (go-imap wiche sonst auf COPY + \Deleted + EXPUNGE aus).
func TestMoveWithoutMoveCapabilityTouchesNothing(t *testing.T) {
	addr, calls := recServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	c := dialLogin(t, addr)
	if _, err := c.Select("INBOX", SelectOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := c.Move([]uint32{1, 2}, "Archiv")
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Fatalf("move without MOVE: %v", err)
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("server saw %v", got)
	}
	st, err := c.Status("INBOX")
	if err != nil || st.Messages != 3 {
		t.Fatalf("INBOX after refused move: %+v %v", st, err)
	}
}

// Mit MOVE (IMAP4rev2): genau ein MOVE, die neuen UIDs kommen aus COPYUID;
// nur lesend gewählt, leere Liste und UID 0 erreichen den Server nicht.
func TestMoveMapsUIDs(t *testing.T) {
	addr, calls := recServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}})
	c := dialLogin(t, addr)
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Move([]uint32{1}, "Archiv"); err == nil {
		t.Fatal("move on a read-only selected mailbox accepted")
	}
	if _, err := c.Select("INBOX", SelectOptions{}); err != nil {
		t.Fatal(err)
	}
	if res, err := c.Move(nil, "Archiv"); err != nil || len(res.UIDs) != 0 {
		t.Fatalf("empty move: %+v %v", res, err)
	}
	if _, err := c.Move([]uint32{0}, "Archiv"); !errors.Is(err, ErrInvalidUID) {
		t.Fatalf("uid 0: %v", err)
	}
	res, err := c.Move([]uint32{1, 3}, "Archiv")
	if err != nil {
		t.Fatal(err)
	}
	if res.UIDValidity == 0 || len(res.UIDs) != 2 || res.UIDs[1] == 0 || res.UIDs[3] == 0 || res.UIDs[1] >= res.UIDs[3] {
		t.Fatalf("result = %+v", res)
	}
	if got := calls(); len(got) != 1 || got[0] != "MOVE" {
		t.Fatalf("server saw %v", got)
	}
	st, _ := c.Status("Archiv")
	if st.Messages != 2 {
		t.Fatalf("Archiv has %d", st.Messages)
	}
}
