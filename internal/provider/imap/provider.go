package imap

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Config beschreibt die Verbindung eines Kontos.
type Config struct {
	Host               string
	Port               int
	TLS                string // tls | starttls | none
	Username           string
	InsecureTestServer bool
	// CommandTimeout: so lange darf der Server während eines Kommandos
	// schweigen (0 = 30 s).
	CommandTimeout time.Duration
	// CommandMaxDuration: Obergrenze je Kommando (0 = 10 min; ADR-007,
	// Nachtrag 16.10.2026).
	CommandMaxDuration time.Duration
	// MaxMessageBytes begrenzt eine Rohmail (0 = 256 MiB; [imap]
	// max_message_bytes, A02 A4). Größere sind ein Fehler je Nachricht.
	MaxMessageBytes int64
	// Session steuert Reconnect und Backoff (Nullwert = Standard).
	Session imapx.SessionOptions
	// IdleRestart ist der Abstand, in dem IDLE neu gestartet wird
	// (0 = imapx.DefaultIdleRestart, 8 min; ADR-007: 5 bis 10 min).
	IdleRestart time.Duration
}

// Provider ist der IMAP-Provider. Stand S2.5: Verbinden, Fähigkeiten,
// Ordner mit Rollen, Changes, FetchHeaders, FetchRaw und Idle (sync.go),
// Append für Entwürfe (append.go). SetFlags, Move und Trash folgen mit S3;
// erst dann erfüllt Provider provider.MailProvider vollständig.
type Provider struct {
	s          *imapx.Session
	newSession func() *imapx.Session // eigene Verbindung für IDLE
	cfg        Config
	caps       provider.Capabilities
	raw        []string
	// mech ist das Anmeldeverfahren der letzten Anmeldung (LOGIN,
	// OAUTHBEARER oder XOAUTH2).
	mech atomic.Value

	mu    sync.Mutex
	cache map[string]searchCache
}

// Connect meldet sich mit Passwort an (mit Watchdog, Reconnect und Backoff)
// und liest die Fähigkeiten. Anmeldefehler sind provider.ErrAuth und werden
// nicht wiederholt.
func Connect(ctx context.Context, cfg Config, password secrets.Secret) (*Provider, error) {
	return ConnectWith(ctx, cfg, provider.PasswordCredential(password))
}

// ConnectWith meldet sich mit Passwort oder OAuth2-Token an (S3.7). Bei
// OAuth2 holt jede Anmeldung, auch nach einem Reconnect, ein gültiges
// Access-Token aus cred.Token und meldet sich per SASL an (OAUTHBEARER vor
// XOAUTH2, je nach CAPABILITY vor der Anmeldung). Scheitert die Tokenquelle
// (Refresh abgelehnt, Token nicht speicherbar), ist das ein Anmeldefehler
// (provider.ErrAuth): kein Wiederholen mit Backoff, `sync --watch` endet.
func ConnectWith(ctx context.Context, cfg Config, cred provider.Credential) (*Provider, error) {
	p := &Provider{cfg: cfg}
	login := func(c *imapx.Client) error {
		if !cred.IsOAuth() {
			p.mech.Store("LOGIN")
			return c.Login(cfg.Username, cred.Password)
		}
		tok, err := cred.Token.Token(ctx)
		if err != nil {
			return fmt.Errorf("%w: %w", provider.ErrAuth, err)
		}
		pre, err := c.Caps()
		if err != nil {
			return err
		}
		sc, err := imapx.OAuthClient(pre, cfg.Username, cfg.Host, cfg.Port, tok)
		if err != nil {
			return fmt.Errorf("%w: %w", provider.ErrAuth, err)
		}
		mech, _, _ := sc.Start()
		p.mech.Store(mech)
		return c.Authenticate(sc)
	}
	newSession := func() *imapx.Session {
		return imapx.NewSession(imapx.Config{
			Host: cfg.Host, Port: cfg.Port, TLS: imapx.TLSMode(cfg.TLS),
			InsecureTestServer: cfg.InsecureTestServer, CommandTimeout: cfg.CommandTimeout,
			CommandMaxDuration: cfg.CommandMaxDuration, MaxMessageBytes: cfg.MaxMessageBytes,
		}, login, cfg.Session)
	}
	s := newSession()
	var raw []string
	err := s.Do(ctx, func(c *imapx.Client) error {
		var err error
		raw, err = c.Caps()
		return err
	})
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	p.s, p.newSession, p.caps, p.raw = s, newSession, Capabilities(raw), raw
	return p, nil
}

// AuthMechanism nennt das Anmeldeverfahren der letzten Anmeldung (LOGIN,
// OAUTHBEARER oder XOAUTH2).
func (p *Provider) AuthMechanism() string {
	m, _ := p.mech.Load().(string)
	return m
}

// Capabilities liefert die erkannten Fähigkeiten.
func (p *Provider) Capabilities() provider.Capabilities { return p.caps }

// RawCapabilities liefert die CAPABILITY-Liste des Servers (sortiert).
func (p *Provider) RawCapabilities() []string { return slices.Clone(p.raw) }

// Strategy ist die Sync-Strategie, die PostKauz fährt (ADR-007: ohne QRESYNC).
func (p *Provider) Strategy() provider.Strategy { return EffectiveStrategy(p.caps) }

func (p *Provider) has(capability string) bool {
	for _, c := range p.raw {
		if strings.EqualFold(c, capability) {
			return true
		}
	}
	return false
}

// ListMailboxes liefert alle auswählbaren Ordner mit Rolle und, wo der
// Server LIST-STATUS kann, mit Zustand (UIDVALIDITY, UIDNEXT, HIGHESTMODSEQ).
func (p *Provider) ListMailboxes(ctx context.Context) ([]provider.Mailbox, error) {
	opts := imapx.ListOptions{
		SpecialUse:    p.caps.SpecialUse,
		Status:        p.caps.IMAP4rev2 || p.has("LIST-STATUS"),
		HighestModSeq: p.caps.CondStore,
	}
	var boxes []imapx.MailboxInfo
	err := p.s.Do(ctx, func(c *imapx.Client) error {
		var err error
		boxes, err = c.ListWith(opts)
		return err
	})
	if err != nil {
		return nil, err
	}
	var sel []imapx.MailboxInfo
	for _, b := range boxes {
		if hasAttr(b.Attrs, `\Noselect`) || hasAttr(b.Attrs, `\NonExistent`) {
			continue
		}
		sel = append(sel, b)
	}
	roles := AssignRoles(sel)
	out := make([]provider.Mailbox, 0, len(sel))
	for i, b := range sel {
		m := provider.Mailbox{Path: b.Path, Name: leafName(b), Role: roles[i]}
		if st := b.Status; st != nil {
			m.State = provider.SyncState{UIDValidity: st.UIDValidity, UIDNext: st.UIDNext, HighestModSeq: st.HighestModSeq}
		}
		out = append(out, m)
	}
	return out, nil
}

// Close meldet sich ab.
func (p *Provider) Close() error { return p.s.Close() }

func leafName(m imapx.MailboxInfo) string {
	if m.Delim != "" {
		parts := strings.Split(m.Path, m.Delim)
		return parts[len(parts)-1]
	}
	return path.Base(m.Path)
}

// AssignRoles vergibt Rollen für eine Ordnerliste: INBOX, dann SPECIAL-USE
// (Vorrang), dann Namen. Jede Rolle außer custom wird höchstens einmal
// vergeben; per Name nur, wenn kein Ordner die Rolle per SPECIAL-USE trägt.
// Bei mehreren Namenstreffern gewinnt der flachste Pfad, dann der
// alphabetisch erste.
func AssignRoles(boxes []imapx.MailboxInfo) []provider.Role {
	roles := make([]provider.Role, len(boxes))
	taken := map[provider.Role]bool{}
	for i, b := range boxes {
		r := Role(imapx.MailboxInfo{Path: b.Path, Delim: b.Delim, Attrs: b.Attrs})
		if r == provider.RoleInbox || specialUseRole(b.Attrs) != "" {
			if r != provider.RoleCustom && taken[r] {
				r = provider.RoleCustom
			}
			roles[i] = r
			taken[r] = true
		}
	}
	order := make([]int, 0, len(boxes))
	for i := range boxes {
		if roles[i] == "" {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		da, db := depth(boxes[a]), depth(boxes[b])
		if da != db {
			return da - db
		}
		return strings.Compare(boxes[a].Path, boxes[b].Path)
	})
	for _, i := range order {
		r := nameRole(boxes[i])
		if r != provider.RoleCustom && taken[r] {
			r = provider.RoleCustom
		}
		roles[i] = r
		if r != provider.RoleCustom {
			taken[r] = true
		}
	}
	return roles
}

func depth(m imapx.MailboxInfo) int {
	if m.Delim == "" {
		return 0
	}
	return strings.Count(m.Path, m.Delim)
}
