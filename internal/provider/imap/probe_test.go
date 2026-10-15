package imap

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

func TestProbeAgainstInProcessServer(t *testing.T) {
	u := imapmemserver.NewUser("tester@example.invalid", "pw-test")
	for _, mb := range []string{"INBOX", "Gesendet", "Entwürfe", "Spam", "Kunden"} {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps:         goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapIMAP4rev2: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	acct := store.Account{Name: "demo", Host: host, Port: p, TLS: "none", Username: "tester@example.invalid", InsecureTestServer: true}

	res, err := Prober{CommandTimeout: 2 * time.Second}.Probe(context.Background(), acct, "pw-test")
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, m := range res.Mailboxes {
		roles[m.Path] = m.Role
	}
	want := map[string]string{"INBOX": "inbox", "Gesendet": "sent", "Entwürfe": "drafts", "Spam": "junk", "Kunden": "custom"}
	for p, r := range want {
		if roles[p] != r {
			t.Errorf("role %s = %q, want %q (all: %v)", p, roles[p], r, roles)
		}
	}
	if res.SyncStrategy != "uid-scan" || res.CapabilitiesJSON == "" {
		t.Errorf("result = %+v", res)
	}

	if _, err := (Prober{CommandTimeout: 2 * time.Second}).Probe(context.Background(), acct, "wrong"); !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestRoleAndCapabilities(t *testing.T) {
	cases := []struct {
		m    imapx.MailboxInfo
		want provider.Role
	}{
		{imapx.MailboxInfo{Path: "inbox"}, provider.RoleInbox},
		{imapx.MailboxInfo{Path: "[Gmail]/Gesendet", Delim: "/", Attrs: []string{`\HasNoChildren`, `\Sent`}}, provider.RoleSent},
		{imapx.MailboxInfo{Path: "INBOX.Trash", Delim: "."}, provider.RoleTrash},
		{imapx.MailboxInfo{Path: "Gelöschte Elemente", Delim: "/"}, provider.RoleTrash},
		{imapx.MailboxInfo{Path: "Archiv/2024", Delim: "/"}, provider.RoleCustom},
		{imapx.MailboxInfo{Path: "Notes", Attrs: []string{`\Archive`}}, provider.RoleArchive},
	}
	for _, c := range cases {
		if got := Role(c.m); got != c.want {
			t.Errorf("Role(%+v) = %s, want %s", c.m, got, c.want)
		}
	}
	caps := Capabilities([]string{"IMAP4rev1", "CONDSTORE", "QRESYNC", "IDLE", "SPECIAL-USE", "X-GM-EXT-1"})
	if !caps.CondStore || !caps.QResync || !caps.Idle || !caps.SpecialUse || !caps.GmailExtensions || caps.Move {
		t.Errorf("caps = %+v", caps)
	}
	// ADR-007: kein QRESYNC mit go-imap beta.8, also condstore.
	if s := EffectiveStrategy(caps); s != provider.StrategyCondStore {
		t.Errorf("strategy = %s", s)
	}
	if s := EffectiveStrategy(Capabilities([]string{"IMAP4rev1"})); s != provider.StrategyUIDScan {
		t.Errorf("strategy = %s", s)
	}
}
