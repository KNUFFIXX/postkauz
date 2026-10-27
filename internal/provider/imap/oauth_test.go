package imap

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"

	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/oauth/oauthtest"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// xoauthServer ist ein IMAP-Server, der nur XOAUTH2 anbietet (wie
// outlook.office365.com) und das zuletzt ausgegebene Access-Token des
// Fake-AS verlangt.
type xoauthServer struct {
	mu     sync.Mutex
	want   func() string
	logins []string
}

type xoauthSession struct {
	*imapmemserver.UserSession
	srv *xoauthServer
}

func (s *xoauthSession) Login(string, string) error       { return imapserver.ErrAuthFailed }
func (s *xoauthSession) AuthenticateMechanisms() []string { return []string{"XOAUTH2"} }
func (s *xoauthSession) Authenticate(mech string) (sasl.Server, error) {
	if mech != "XOAUTH2" {
		return nil, errors.New("unsupported")
	}
	return &xoauthSASL{srv: s.srv}, nil
}

type xoauthSASL struct {
	srv    *xoauthServer
	failed bool
}

func (x *xoauthSASL) Next(resp []byte) ([]byte, bool, error) {
	if x.failed {
		return nil, true, imapserver.ErrAuthFailed
	}
	x.srv.mu.Lock()
	defer x.srv.mu.Unlock()
	want := "user=tester@example.invalid\x01auth=Bearer " + x.srv.want() + "\x01\x01"
	if string(resp) == want {
		x.srv.logins = append(x.srv.logins, "ok")
		return nil, true, nil
	}
	x.srv.logins = append(x.srv.logins, "rejected")
	x.failed = true
	return []byte(`{"status":"401","schemes":"bearer"}`), false, nil
}

func startXOAuthServer(t *testing.T, want func() string) (*xoauthServer, store.Account) {
	t.Helper()
	u := imapmemserver.NewUser("tester@example.invalid", "unused")
	for _, mb := range []string{"INBOX", "Sent Items", "Deleted Items"} {
		_ = u.Create(mb, nil)
	}
	xs := &xoauthServer{want: want}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &xoauthSession{UserSession: imapmemserver.NewUserSession(u), srv: xs}, nil, nil
		},
		Caps:         goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapSASLIR: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return xs, store.Account{Name: "outlook", Host: host, Port: p, TLS: "none", Username: "tester@example.invalid",
		InsecureTestServer: true, AuthType: "oauth2"}
}

// account test mit XOAUTH2: Die Tokenquelle erneuert über den Fake-AS, der
// IMAP-Server nimmt genau das neue Access-Token (S3.7 Akzeptanz 2).
func TestProbeWithXOAuth2TokenSource(t *testing.T) {
	as := oauthtest.New(t)
	m := oauth.Microsoft{Endpoint: as.Endpoint, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second}
	client := oauth.Client{ClientID: oauthtest.ClientID, Tenant: "consumers"}
	first, err := m.LoginLoopback(context.Background(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	xs, acct := startXOAuthServer(t, as.LastAccess)
	// Gespeichert ist nur das Refresh-Token: Die Quelle erneuert vor der Anmeldung.
	src := oauth.NewSource(oauth.Token{RefreshToken: first.RefreshToken}, m.Refresher(client))
	res, err := Prober{CommandTimeout: 2 * time.Second}.Probe(context.Background(), acct, provider.Credential{Token: src})
	if err != nil {
		t.Fatal(err)
	}
	if res.AuthMechanism != "XOAUTH2" || len(res.Mailboxes) != 3 || as.Count("refresh_token") != 1 {
		t.Fatalf("result = %+v, refreshes %d", res, as.Count("refresh_token"))
	}

	// Refresh abgelehnt: Anmeldefehler (ErrAuth, kein Wiederholen), kein
	// Anbietertext, keine weitere Anmeldung am IMAP-Server.
	as.FailRefresh = "invalid_grant"
	bad := oauth.NewSource(oauth.Token{RefreshToken: "CANARY-revoked"}, m.Refresher(client))
	_, err = ConnectWith(context.Background(), Config{Host: acct.Host, Port: acct.Port, TLS: "none", Username: acct.Username,
		InsecureTestServer: true, CommandTimeout: 2 * time.Second}, provider.Credential{Token: bad})
	if !errors.Is(err, provider.ErrAuth) || !errors.Is(err, oauth.ErrRefresh) || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err = %v", err)
	}
	xs.mu.Lock()
	logins := append([]string(nil), xs.logins...)
	xs.mu.Unlock()
	if as.Count("refresh_token") != 2 || len(logins) != 1 {
		t.Fatalf("refreshes %d, imap logins %v", as.Count("refresh_token"), logins)
	}
}
