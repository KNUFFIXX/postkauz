package imap

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Integrationstests gegen die Docker-Testserver (deploy/testmail). Sie laufen
// nur mit POSTKAUZ_IT=1 (alle) oder POSTKAUZ_IT=dovecot,stalwart,greenmail
// (Auswahl), z. B. über `make integration`; `make check` und die CI
// überspringen sie. Ist die Variable gesetzt und ein Server nicht
// erreichbar, schlägt der Test fehl (nicht still übersprungen).

type itServer struct {
	name      string
	cfg       Config
	pass      secrets.Secret
	strategy  provider.Strategy
	caps      func(provider.Capabilities) bool
	create    []string // vorher anlegen (Server ohne Standardordner)
	wantRoles map[string]provider.Role
}

var itServers = []itServer{
	{
		name: "dovecot",
		cfg:  Config{Host: "127.0.0.1", Port: 11143, TLS: "none", Username: "it-user@example.org", InsecureTestServer: true},
		pass: "pass", strategy: provider.StrategyCondStore,
		caps: func(c provider.Capabilities) bool {
			return c.CondStore && c.QResync && c.SpecialUse && c.Idle && c.Move && c.UIDPlus
		},
		wantRoles: map[string]provider.Role{"INBOX": provider.RoleInbox, "Sent": provider.RoleSent,
			"Drafts": provider.RoleDrafts, "Trash": provider.RoleTrash, "Junk": provider.RoleJunk},
	},
	{
		name: "stalwart",
		cfg:  Config{Host: "127.0.0.1", Port: 12993, TLS: "tls", Username: "test", InsecureTestServer: true},
		pass: "pass", strategy: provider.StrategyCondStore,
		caps: func(c provider.Capabilities) bool {
			return c.IMAP4rev2 && c.CondStore && c.QResync && c.SpecialUse && c.Idle && c.Move
		},
		// Stalwart nennt die Attribute nur mit RETURN (SPECIAL-USE).
		wantRoles: map[string]provider.Role{"INBOX": provider.RoleInbox, "Sent Items": provider.RoleSent,
			"Drafts": provider.RoleDrafts, "Deleted Items": provider.RoleTrash, "Junk Mail": provider.RoleJunk},
	},
	{
		name: "greenmail",
		cfg:  Config{Host: "127.0.0.1", Port: 13143, TLS: "none", Username: "test", InsecureTestServer: true},
		pass: "pass", strategy: provider.StrategyUIDScan,
		caps: func(c provider.Capabilities) bool {
			return !c.CondStore && !c.SpecialUse && c.Idle && c.UIDPlus
		},
		// Kein SPECIAL-USE: Rollen kommen aus den Namen (Rückfall).
		create: []string{"Gesendet", "Entwürfe", "Papierkorb", "Spam", "Kunden"},
		wantRoles: map[string]provider.Role{"INBOX": provider.RoleInbox, "Gesendet": provider.RoleSent,
			"Entwürfe": provider.RoleDrafts, "Papierkorb": provider.RoleTrash, "Spam": provider.RoleJunk,
			"Kunden": provider.RoleCustom},
	},
}

func itEnabled(name string) bool {
	v := os.Getenv("POSTKAUZ_IT")
	if v == "" {
		return false
	}
	if v == "1" || v == "all" {
		return true
	}
	return slices.Contains(strings.Split(v, ","), name)
}

func TestIntegrationCapabilitiesAndRoles(t *testing.T) {
	if os.Getenv("POSTKAUZ_IT") == "" {
		t.Skip("integration tests need Docker test servers: make testmail-up testmail-setup integration")
	}
	for _, srv := range itServers {
		t.Run(srv.name, func(t *testing.T) {
			if !itEnabled(srv.name) {
				t.Skip("not selected in POSTKAUZ_IT")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cfg := srv.cfg
			cfg.CommandTimeout = 5 * time.Second
			cfg.Session = imapx.SessionOptions{MaxAttempts: 1}
			if len(srv.create) > 0 {
				c, err := imapx.Dial(ctx, imapx.Config{Host: cfg.Host, Port: cfg.Port, TLS: imapx.TLSMode(cfg.TLS),
					InsecureTestServer: true, CommandTimeout: cfg.CommandTimeout})
				if err != nil {
					t.Fatalf("server not reachable (make testmail-up): %v", err)
				}
				if err := c.Login(cfg.Username, srv.pass); err != nil {
					t.Fatal(err)
				}
				for _, mb := range srv.create {
					if err := c.Create(mb); err != nil {
						t.Fatalf("create %s: %v", mb, err)
					}
				}
				_ = c.Close()
			}
			prov, err := Connect(ctx, cfg, srv.pass)
			if err != nil {
				t.Fatalf("connect (make testmail-up testmail-setup): %v", err)
			}
			defer prov.Close()
			caps := prov.Capabilities()
			t.Logf("%s capabilities: %s", srv.name, strings.Join(prov.RawCapabilities(), " "))
			if !srv.caps(caps) {
				t.Errorf("capabilities = %+v", caps)
			}
			if s := prov.Strategy(); s != srv.strategy {
				t.Errorf("strategy = %s, want %s", s, srv.strategy)
			}
			boxes, err := prov.ListMailboxes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]provider.Role{}
			for _, b := range boxes {
				got[b.Path] = b.Role
				t.Logf("  %-16s %-8s %+v", b.Path, b.Role, b.State)
			}
			for path, role := range srv.wantRoles {
				if got[path] != role {
					t.Errorf("role of %q = %q, want %q", path, got[path], role)
				}
			}
			// Mit LIST-STATUS kommt der Zustand gleich mit.
			if caps.IMAP4rev2 || slices.Contains(prov.RawCapabilities(), "LIST-STATUS") {
				for _, b := range boxes {
					if b.State.UIDValidity == 0 {
						t.Errorf("%s: no UIDVALIDITY from LIST-STATUS", b.Path)
					}
				}
			}
		})
	}
}

// Falsches Passwort: ErrAuth, kein Wiederholen (Anbieter sperren sonst).
func TestIntegrationWrongPassword(t *testing.T) {
	if os.Getenv("POSTKAUZ_IT") == "" {
		t.Skip("integration tests need Docker test servers")
	}
	for _, srv := range itServers {
		t.Run(srv.name, func(t *testing.T) {
			if !itEnabled(srv.name) {
				t.Skip("not selected in POSTKAUZ_IT")
			}
			cfg := srv.cfg
			cfg.CommandTimeout = 5 * time.Second
			_, err := Connect(context.Background(), cfg, "CANARY-wrong-pass")
			if !errors.Is(err, provider.ErrAuth) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "CANARY") {
				t.Fatal("error leaks the password")
			}
		})
	}
}
