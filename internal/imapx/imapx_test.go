package imapx

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

const (
	testUser = "tester@example.invalid"
	testPass = "CANARY-imap-pw-0c4e"
	testTok  = "CANARY-oauth-token-9b2d"
)

// fakeServer startet einen IMAP-Server im Prozess (go-imap imapmemserver)
// auf 127.0.0.1; ohne Docker, für Unit-Tests.
func fakeServer(t *testing.T, newSession func(u *imapmemserver.User) imapserver.Session) string {
	t.Helper()
	u := imapmemserver.NewUser(testUser, testPass)
	for _, mb := range []string{"INBOX", "Sent", "Archive", "Papierkorb", "Projekte/Alt"} {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	if newSession == nil {
		newSession = func(u *imapmemserver.User) imapserver.Session { return imapmemserver.NewUserSession(u) }
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return newSession(u), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}, imap.CapSASLIR: {}},
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

func cfgFor(t *testing.T, addr string) Config {
	host, port, _ := net.SplitHostPort(addr)
	p := 0
	for _, r := range port {
		p = p*10 + int(r-'0')
	}
	return Config{Host: host, Port: p, TLS: TLSNone, InsecureTestServer: true, CommandTimeout: 2 * time.Second}
}

func TestDialLoginCapsList(t *testing.T) {
	addr := fakeServer(t, nil)
	c, err := Dial(context.Background(), cfgFor(t, addr))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	caps, err := c.Caps()
	if err != nil || !contains(caps, "IMAP4rev2") {
		t.Fatalf("caps = %v %v", caps, err)
	}
	boxes, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, b := range boxes {
		paths = append(paths, b.Path)
	}
	if strings.Join(paths, ",") != "Archive,INBOX,Papierkorb,Projekte/Alt,Sent" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestLoginRejectedIsErrAuth(t *testing.T) {
	addr := fakeServer(t, nil)
	c, err := Dial(context.Background(), cfgFor(t, addr))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Login(testUser, "wrong")
	if !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatal("error contains the password")
	}
}

// Watchdog: Ein Server, der die Verbindung annimmt und schweigt (Blackhole),
// wird nach der Deadline erkannt, statt ewig zu hängen (Spike-Befund).
func TestWatchdogDetectsSilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close() // schweigen
		}
	}()
	cfg := cfgFor(t, ln.Addr().String())
	cfg.CommandTimeout = 300 * time.Millisecond
	start := time.Now()
	_, err = Dial(context.Background(), cfg)
	if !errors.Is(err, provider.ErrConnection) || !strings.Contains(err.Error(), "watchdog") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("watchdog took %s", d)
	}
}

func TestDialRefusedIsErrConnection(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if _, err := Dial(context.Background(), cfgFor(t, addr)); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlainTextOnlyForLoopback(t *testing.T) {
	for _, cfg := range []Config{
		{Host: "imap.example.org", Port: 143, TLS: TLSNone},
		{Host: "192.0.2.1", Port: 993, TLS: TLSImplicit, InsecureTestServer: true},
	} {
		if _, err := Dial(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "localhost") {
			t.Errorf("%+v: err = %v", cfg, err)
		}
	}
}

func TestWordDecoderHandlesCharsets(t *testing.T) {
	d := WordDecoder()
	for in, want := range map[string]string{
		"=?windows-1252?Q?Stra=DFe_f=FCr_Caf=E9?=": "Straße für Café",
		"=?iso-8859-1?B?R3L832U=?=":                "Grüße",
		"=?utf-8?Q?Erdgescho=C3=9F?=":              "Erdgeschoß",
	} {
		got, err := d.DecodeHeader(in)
		if err != nil || got != want {
			t.Errorf("%s = %q (%v), want %q", in, got, err, want)
		}
	}
}

// --- P3: OAuth2 über SASL gegen einen Fake-Server ---------------------------

type saslSession struct {
	*imapmemserver.UserSession
	user *imapmemserver.User
}

func (s *saslSession) Login(string, string) error { return imapserver.ErrAuthFailed }
func (s *saslSession) AuthenticateMechanisms() []string {
	return []string{MechOAuthBearer, MechXOAuth2}
}
func (s *saslSession) Authenticate(mech string) (sasl.Server, error) {
	switch mech {
	case MechOAuthBearer:
		return sasl.NewOAuthBearerServer(func(o sasl.OAuthBearerOptions) *sasl.OAuthBearerError {
			if o.Username != testUser || o.Token != testTok {
				return &sasl.OAuthBearerError{Status: "invalid_token", Schemes: "bearer"}
			}
			return nil
		}), nil
	case MechXOAuth2:
		return &fakeXOAuth2Server{}, nil
	}
	return nil, errors.New("unsupported")
}

// fakeXOAuth2Server prüft die Anfangsantwort wie Gmail: bei Fehler eine
// JSON-Challenge, nach der leeren Client-Antwort NO.
type fakeXOAuth2Server struct{ failed bool }

func (s *fakeXOAuth2Server) Next(resp []byte) ([]byte, bool, error) {
	if s.failed {
		return nil, true, imapserver.ErrAuthFailed
	}
	if string(resp) == "user="+testUser+"\x01auth=Bearer "+testTok+"\x01\x01" {
		return nil, true, nil
	}
	s.failed = true
	return []byte(`{"status":"401","schemes":"bearer"}`), false, nil
}

func TestOAuthSASLAgainstFakeServer(t *testing.T) {
	addr := fakeServer(t, func(u *imapmemserver.User) imapserver.Session {
		return &saslSession{UserSession: imapmemserver.NewUserSession(u), user: u}
	})
	cfg := cfgFor(t, addr)
	for _, mech := range []string{MechOAuthBearer, MechXOAuth2} {
		for _, tc := range []struct {
			token secrets.Secret
			ok    bool
		}{{testTok, true}, {"CANARY-expired-token", false}} {
			t.Run(mech+"/"+map[bool]string{true: "valid", false: "invalid"}[tc.ok], func(t *testing.T) {
				c, err := Dial(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				var sc sasl.Client
				if mech == MechXOAuth2 {
					sc = NewXOAuth2Client(testUser, tc.token)
				} else {
					sc = NewOAuthBearerClient(testUser, cfg.Host, cfg.Port, tc.token)
				}
				err = c.Authenticate(sc)
				if tc.ok && err != nil {
					t.Fatalf("auth: %v", err)
				}
				if !tc.ok && !errors.Is(err, provider.ErrAuth) {
					t.Fatalf("want ErrAuth, got %v", err)
				}
				if err != nil && strings.Contains(err.Error(), string(tc.token)) {
					t.Fatal("error leaks the token")
				}
				if tc.ok {
					if _, err := c.List(); err != nil {
						t.Fatalf("list after auth: %v", err)
					}
				}
			})
		}
	}
}

func TestOAuthClientChoosesMechanism(t *testing.T) {
	c, err := OAuthClient([]string{"IMAP4rev1", "AUTH=XOAUTH2", "AUTH=OAUTHBEARER"}, testUser, "h", 993, testTok)
	if err != nil {
		t.Fatal(err)
	}
	if mech, _, _ := c.Start(); mech != MechOAuthBearer {
		t.Errorf("mech = %s", mech)
	}
	c, _ = OAuthClient([]string{"AUTH=XOAUTH2"}, testUser, "h", 993, testTok)
	if mech, _, _ := c.Start(); mech != MechXOAuth2 {
		t.Errorf("mech = %s", mech)
	}
	if _, err := OAuthClient([]string{"AUTH=PLAIN"}, testUser, "h", 993, testTok); err == nil {
		t.Error("no OAuth mechanism must fail")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestListWithStatusAndCreate(t *testing.T) {
	addr := fakeServer(t, nil)
	c, err := Dial(context.Background(), cfgFor(t, addr))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	if err := c.Create("Entwürfe"); err != nil {
		t.Fatal(err)
	}
	if err := c.Create("Entwürfe"); err != nil {
		t.Fatalf("second create must be a no-op: %v", err)
	}
	boxes, err := c.ListWith(ListOptions{Status: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, b := range boxes {
		if b.Path == "Entwürfe" {
			seen = true
		}
		if b.Status == nil || b.Status.UIDValidity == 0 || b.Status.UIDNext == 0 {
			t.Errorf("%s: status = %+v", b.Path, b.Status)
		}
	}
	if !seen {
		t.Fatalf("created mailbox missing: %+v", boxes)
	}
	if err := c.Noop(); err != nil {
		t.Fatal(err)
	}
}
