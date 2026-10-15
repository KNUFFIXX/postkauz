// Package imap ist der IMAP-Provider über internal/imapx (ADR-007).
//
// Stand S1.1: nur die Verbindungsprüfung für `account test` (Anmelden,
// Fähigkeiten, Ordner mit Rollen). Der vollständige provider.MailProvider
// (Changes, Fetch, Append, Idle) folgt in S1.3–S1.5.
package imap

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Prober implementiert ops.AccountProber.
type Prober struct {
	// CommandTimeout überschreibt den Watchdog (Tests).
	CommandTimeout time.Duration
}

var _ ops.AccountProber = Prober{}

// Probe meldet sich an, liest Fähigkeiten und Ordner und meldet sich ab.
// Am Server wird nichts verändert (kein SELECT, kein FETCH).
func (p Prober) Probe(ctx context.Context, a store.Account, pw secrets.Secret) (ops.ProbeResult, error) {
	c, err := imapx.Dial(ctx, imapx.Config{Host: a.Host, Port: a.Port, TLS: imapx.TLSMode(a.TLS),
		InsecureTestServer: a.InsecureTestServer, CommandTimeout: p.CommandTimeout})
	if err != nil {
		return ops.ProbeResult{}, err
	}
	defer c.Close()
	if err := c.Login(a.Username, pw); err != nil {
		return ops.ProbeResult{}, err
	}
	capsList, err := c.Caps()
	if err != nil {
		return ops.ProbeResult{}, err
	}
	boxes, err := c.List()
	if err != nil {
		return ops.ProbeResult{}, err
	}
	caps := Capabilities(capsList)
	cj, _ := json.Marshal(caps)
	res := ops.ProbeResult{Capabilities: capsList, SyncStrategy: string(EffectiveStrategy(caps)), CapabilitiesJSON: string(cj)}
	for _, b := range boxes {
		if hasAttr(b.Attrs, `\Noselect`) || hasAttr(b.Attrs, `\NonExistent`) {
			continue
		}
		res.Mailboxes = append(res.Mailboxes, ops.ProbeMailbox{Path: b.Path, Role: string(Role(b))})
	}
	return res, nil
}

// Capabilities übersetzt die CAPABILITY-Liste in provider.Capabilities.
// KeywordsPermanent braucht ein SELECT und bleibt hier unbekannt (false).
func Capabilities(list []string) provider.Capabilities {
	has := map[string]bool{}
	for _, c := range list {
		has[strings.ToUpper(c)] = true
	}
	return provider.Capabilities{
		IMAP4rev2:       has["IMAP4REV2"],
		CondStore:       has["CONDSTORE"] || has["QRESYNC"], // QRESYNC impliziert CONDSTORE (RFC 7162)
		QResync:         has["QRESYNC"],
		UIDPlus:         has["UIDPLUS"] || has["IMAP4REV2"],
		Idle:            has["IDLE"] || has["IMAP4REV2"],
		Move:            has["MOVE"] || has["IMAP4REV2"],
		SpecialUse:      has["SPECIAL-USE"],
		Namespace:       has["NAMESPACE"] || has["IMAP4REV2"],
		GmailExtensions: has["X-GM-EXT-1"],
		UTF8Accept:      has["UTF8=ACCEPT"],
	}
}

// EffectiveStrategy ist die Strategie, die PostKauz tatsächlich fährt:
// go-imap v2 beta.8 kann kein QRESYNC (ADR-007), also höchstens condstore
// mit UID-SEARCH-Abgleich für Löschungen.
func EffectiveStrategy(c provider.Capabilities) provider.Strategy {
	c.QResync = false
	return c.SyncStrategy()
}

func hasAttr(attrs []string, want string) bool {
	for _, a := range attrs {
		if strings.EqualFold(a, want) {
			return true
		}
	}
	return false
}

// Rollen aus SPECIAL-USE (RFC 6154).
var specialUse = map[string]provider.Role{
	`\sent`: provider.RoleSent, `\drafts`: provider.RoleDrafts, `\trash`: provider.RoleTrash,
	`\junk`: provider.RoleJunk, `\archive`: provider.RoleArchive,
}

// Namensheuristik, wenn der Server kein SPECIAL-USE meldet (verbreitete
// englische und deutsche Ordnernamen).
var nameRoles = map[string]provider.Role{
	"sent": provider.RoleSent, "sent items": provider.RoleSent, "sent messages": provider.RoleSent,
	"sent mail": provider.RoleSent, "gesendet": provider.RoleSent, "gesendete objekte": provider.RoleSent,
	"gesendete elemente": provider.RoleSent,
	"drafts":             provider.RoleDrafts, "draft": provider.RoleDrafts, "entwürfe": provider.RoleDrafts,
	"trash": provider.RoleTrash, "deleted items": provider.RoleTrash, "deleted messages": provider.RoleTrash,
	"bin": provider.RoleTrash, "papierkorb": provider.RoleTrash, "gelöschte elemente": provider.RoleTrash,
	"gelöschte objekte": provider.RoleTrash,
	"junk":              provider.RoleJunk, "spam": provider.RoleJunk, "junk e-mail": provider.RoleJunk, "junk-e-mail": provider.RoleJunk,
	"archive": provider.RoleArchive, "archiv": provider.RoleArchive,
}

// Role bestimmt die Rolle eines Ordners: INBOX, dann SPECIAL-USE, dann Name.
func Role(m imapx.MailboxInfo) provider.Role {
	if strings.EqualFold(m.Path, "INBOX") {
		return provider.RoleInbox
	}
	for _, a := range m.Attrs {
		if r, ok := specialUse[strings.ToLower(a)]; ok {
			return r
		}
	}
	leaf := m.Path
	if m.Delim != "" {
		parts := strings.Split(m.Path, m.Delim)
		leaf = parts[len(parts)-1]
	} else {
		leaf = path.Base(leaf)
	}
	if r, ok := nameRoles[strings.ToLower(strings.TrimSpace(leaf))]; ok {
		return r
	}
	return provider.RoleCustom
}
