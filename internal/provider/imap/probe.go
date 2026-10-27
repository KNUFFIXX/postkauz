// Package imap ist der IMAP-Provider über internal/imapx (ADR-007).
//
// Stand S1.3: Verbinden (Watchdog, Reconnect mit Backoff), Fähigkeiten,
// Ordner mit Rollen (SPECIAL-USE, sonst Namen auf Deutsch/Englisch, Gmail-
// Ordner unter „[Gmail]"/„[Google Mail]") und die Verbindungsprüfung für
// `account test`. Changes, Fetch, Append und Idle folgen in S1.4/S1.5.
package imap

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Prober implementiert ops.AccountProber.
type Prober struct {
	// CommandTimeout und CommandMaxDuration steuern den Watchdog
	// (0 = Standard: 30 s Schweigen, 10 min je Kommando).
	CommandTimeout     time.Duration
	CommandMaxDuration time.Duration
}

var _ ops.AccountProber = Prober{}

// Probe meldet sich an, liest Fähigkeiten und Ordner und meldet sich ab.
// Am Server wird nichts verändert (kein SELECT, kein FETCH). Für eine
// Prüfung genügt ein Versuch; Reconnect ist Sache des Sync.
func (p Prober) Probe(ctx context.Context, a store.Account, cred provider.Credential) (ops.ProbeResult, error) {
	prov, err := ConnectWith(ctx, Config{Host: a.Host, Port: a.Port, TLS: a.TLS, Username: a.Username,
		InsecureTestServer: a.InsecureTestServer, CommandTimeout: p.CommandTimeout,
		CommandMaxDuration: p.CommandMaxDuration, Session: imapx.SessionOptions{MaxAttempts: 1}}, cred)
	if err != nil {
		return ops.ProbeResult{}, err
	}
	defer prov.Close()
	boxes, err := prov.ListMailboxes(ctx)
	if err != nil {
		return ops.ProbeResult{}, err
	}
	caps := prov.Capabilities()
	cj, _ := json.Marshal(caps)
	res := ops.ProbeResult{Capabilities: prov.RawCapabilities(), SyncStrategy: string(EffectiveStrategy(caps)), CapabilitiesJSON: string(cj),
		AuthMechanism: prov.AuthMechanism()}
	for _, b := range boxes {
		res.Mailboxes = append(res.Mailboxes, ops.ProbeMailbox{Path: b.Path, Role: string(b.Role)})
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

// Rollen aus SPECIAL-USE (RFC 6154). \Flagged und \Important sind
// virtuelle Sichten (Gmail „Markiert"/„Wichtig") und bleiben custom.
var specialUse = map[string]provider.Role{
	`\sent`: provider.RoleSent, `\drafts`: provider.RoleDrafts, `\trash`: provider.RoleTrash,
	`\junk`: provider.RoleJunk, `\archive`: provider.RoleArchive, `\all`: provider.RoleAll,
}

// Namensheuristik, wenn der Server kein SPECIAL-USE meldet (verbreitete
// englische und deutsche Ordnernamen).
var nameRoles = map[string]provider.Role{
	"sent": provider.RoleSent, "sent items": provider.RoleSent, "sent messages": provider.RoleSent,
	"sent mail": provider.RoleSent, "gesendet": provider.RoleSent, "gesendete objekte": provider.RoleSent,
	"gesendete elemente": provider.RoleSent, "gesendete nachrichten": provider.RoleSent,
	"drafts": provider.RoleDrafts, "draft": provider.RoleDrafts, "entwürfe": provider.RoleDrafts,
	"entwurf": provider.RoleDrafts,
	"trash":   provider.RoleTrash, "deleted items": provider.RoleTrash, "deleted messages": provider.RoleTrash,
	"bin": provider.RoleTrash, "papierkorb": provider.RoleTrash, "gelöschte elemente": provider.RoleTrash,
	"gelöschte objekte": provider.RoleTrash,
	"junk":              provider.RoleJunk, "spam": provider.RoleJunk, "junk e-mail": provider.RoleJunk,
	"junk-e-mail": provider.RoleJunk, "junk mail": provider.RoleJunk, "werbung": provider.RoleJunk,
	"archive": provider.RoleArchive, "archives": provider.RoleArchive, "archiv": provider.RoleArchive,
}

// Gmail-Ordner unter „[Gmail]" bzw. „[Google Mail]" (ältere deutsche
// Konten), englisch und deutsch. Rückfall, falls SPECIAL-USE fehlt.
var gmailRoles = map[string]provider.Role{
	"sent mail": provider.RoleSent, "gesendet": provider.RoleSent,
	"drafts": provider.RoleDrafts, "entwürfe": provider.RoleDrafts,
	"trash": provider.RoleTrash, "bin": provider.RoleTrash, "papierkorb": provider.RoleTrash,
	"spam":     provider.RoleJunk,
	"all mail": provider.RoleAll, "alle nachrichten": provider.RoleAll,
	"starred": provider.RoleCustom, "markiert": provider.RoleCustom,
	"important": provider.RoleCustom, "wichtig": provider.RoleCustom,
}

func specialUseRole(attrs []string) provider.Role {
	for _, a := range attrs {
		if r, ok := specialUse[strings.ToLower(a)]; ok {
			return r
		}
	}
	return ""
}

func nameRole(m imapx.MailboxInfo) provider.Role {
	if strings.EqualFold(m.Path, "INBOX") {
		return provider.RoleInbox
	}
	leaf := strings.ToLower(strings.TrimSpace(leafName(m)))
	if m.Delim != "" {
		if first, _, ok := strings.Cut(m.Path, m.Delim); ok && (strings.EqualFold(first, "[Gmail]") || strings.EqualFold(first, "[Google Mail]")) {
			if r, ok := gmailRoles[leaf]; ok {
				return r
			}
		}
	}
	if r, ok := nameRoles[leaf]; ok {
		return r
	}
	return provider.RoleCustom
}

// Role bestimmt die Rolle eines einzelnen Ordners: INBOX, dann SPECIAL-USE,
// dann Name. Für eine ganze Liste (eindeutige Rollen) gilt AssignRoles.
func Role(m imapx.MailboxInfo) provider.Role {
	if strings.EqualFold(m.Path, "INBOX") {
		return provider.RoleInbox
	}
	if r := specialUseRole(m.Attrs); r != "" {
		return r
	}
	return nameRole(m)
}
