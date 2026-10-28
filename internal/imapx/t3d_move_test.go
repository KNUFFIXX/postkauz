package imapx

import (
	"io"
	"log"
	"net"
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// quietMoveSession antwortet auf MOVE ohne passende Nachricht wie Dovecot:
// OK ohne COPYUID und ohne EXPUNGE (gone sind die UIDs, die es im
// Quellordner nicht gibt).
type quietMoveSession struct {
	*imapmemserver.UserSession
	gone map[imap.UID]bool
}

func (s quietMoveSession) Move(w *imapserver.MoveWriter, n imap.NumSet, dest string) error {
	if set, ok := n.(imap.UIDSet); ok {
		all := true
		for _, r := range set {
			for u := r.Start; u <= r.Stop; u++ {
				all = all && s.gone[u]
			}
		}
		if all {
			return nil
		}
	}
	return s.UserSession.Move(w, n, dest)
}

// v11Server: drei Nachrichten in INBOX (UID 1 bis 3), Ordner Archiv.
func v11Server(t *testing.T) string {
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
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return quietMoveSession{UserSession: imapmemserver.NewUserSession(u), gone: map[imap.UID]bool{9: true, 10: true}}, nil, nil
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

// V11 der Nachbesserung T2: Eine UID, die es im Quellordner nicht mehr
// gibt, meldet Move als Missing: aus COPYUID (der Server nennt nur die
// verschobenen) und ohne COPYUID aus dem fehlenden EXPUNGE (Dovecot-Art,
// nichts verschoben).
func TestMoveReportsVanishedUIDs(t *testing.T) {
	c := dialLogin(t, v11Server(t))
	if _, err := c.Select("INBOX", SelectOptions{}); err != nil {
		t.Fatal(err)
	}
	res, err := c.Move([]uint32{1, 9}, "Archiv")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Missing, []uint32{9}) || res.UIDs[1] == 0 {
		t.Fatalf("mixed move: %+v", res)
	}
	res, err = c.Move([]uint32{9, 10}, "Archiv")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Missing, []uint32{9, 10}) || len(res.UIDs) != 0 {
		t.Fatalf("vanished only: %+v", res)
	}
	res, err = c.Move([]uint32{2, 3}, "Archiv")
	if err != nil || len(res.Missing) != 0 || len(res.UIDs) != 2 {
		t.Fatalf("all present: %+v %v", res, err)
	}
}
