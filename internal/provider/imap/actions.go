package imap

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// Schreibende Kommandos für Postfach-Aktionen (T2, A03 S3.1/S3.2; Vertrag
// in provider.MailProvider): STORE und MOVE. Beide laufen über
// Session.DoOnce: Nach einem Verbindungsfehler wiederholt die Session sie
// nicht blind (T1a Punkt 5); der Job prüft beim nächsten Lauf. UIDs gelten
// nur unter uidValidity (Ensure prüft, imapx.uidSet weist 0 und
// 4294967295 ab). Es gibt kein EXPUNGE und kein \Deleted (ADR-002).

var _ provider.MailProvider = (*Provider)(nil)

// rw wählt den Ordner zum Schreiben (SELECT statt EXAMINE).
func (p *Provider) rw() imapx.SelectOptions {
	o := p.selectOpts()
	o.ReadOnly = false
	return o
}

// SetFlags setzt bzw. entfernt Flags und Keywords. Keywords verlangen, dass
// der Ordner freie Keywords dauerhaft speichert (PERMANENTFLAGS \*, E5);
// sonst ErrUnsupported, bevor ein Kommando den Server erreicht.
func (p *Provider) SetFlags(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID, patch provider.FlagPatch) error {
	if len(uids) == 0 {
		return nil
	}
	var add, del []string
	for _, f := range []struct {
		v    *bool
		flag string
	}{{patch.Seen, `\Seen`}, {patch.Flagged, `\Flagged`}, {patch.Answered, `\Answered`}, {patch.Draft, `\Draft`}} {
		switch {
		case f.v == nil:
		case *f.v:
			add = append(add, f.flag)
		default:
			del = append(del, f.flag)
		}
	}
	for _, k := range patch.AddKeywords {
		if !validKeyword(k) {
			return fmt.Errorf("imap: invalid keyword")
		}
		add = append(add, k)
	}
	for _, k := range patch.RemoveKeywords {
		if !validKeyword(k) {
			return fmt.Errorf("imap: invalid keyword")
		}
		del = append(del, k)
	}
	keywords := len(patch.AddKeywords)+len(patch.RemoveKeywords) > 0
	return p.s.DoOnce(ctx, func(c *imapx.Client) error {
		info, err := c.Ensure(mailbox, uidValidity, p.rw())
		if err != nil {
			return err
		}
		if keywords && !slices.Contains(info.PermanentFlags, `\*`) {
			return fmt.Errorf("%w: the mailbox does not store keywords (no PERMANENTFLAGS \\*)", provider.ErrUnsupported)
		}
		if len(add) > 0 {
			if err := c.StoreFlags(fromUIDs(uids), add, true); err != nil {
				return err
			}
		}
		if len(del) > 0 {
			return c.StoreFlags(fromUIDs(uids), del, false)
		}
		return nil
	})
}

// validKeyword: ein IMAP-Atom ohne führendes '\' (Systemflags setzt nur
// FlagPatch) und ohne Sonderzeichen.
func validKeyword(k string) bool {
	if k == "" || strings.HasPrefix(k, `\`) || len(k) > 64 {
		return false
	}
	for _, r := range k {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune(`(){%*"]\`, r) {
			return false
		}
	}
	return true
}

// Move verschiebt Nachrichten, nur mit IMAP MOVE (imapx.Client.Move; ohne
// MOVE ErrUnsupported, kein Ersatz über COPY und Löschen, A03 E2).
func (p *Provider) Move(ctx context.Context, from string, uidValidity uint32, uids []provider.UID, to string) (provider.MoveResult, error) {
	var res provider.MoveResult
	if len(uids) == 0 {
		return res, nil
	}
	if !p.caps.Move {
		return res, fmt.Errorf("%w: MOVE (RFC 6851); messages are not moved without it", provider.ErrUnsupported)
	}
	err := p.s.DoOnce(ctx, func(c *imapx.Client) error {
		if _, err := c.Ensure(from, uidValidity, p.rw()); err != nil {
			return err
		}
		r, err := c.Move(fromUIDs(uids), to)
		if err != nil {
			return err
		}
		res = provider.MoveResult{UIDValidity: r.UIDValidity, UIDs: map[provider.UID]provider.UID{}}
		for a, b := range r.UIDs {
			res.UIDs[provider.UID(a)] = provider.UID(b)
		}
		return nil
	})
	return res, err
}

// Trash verschiebt Nachrichten in den Ordner mit Rolle trash (bei Gmail
// [Gmail]/Papierkorb: Gmail nimmt dabei alle Labels weg). Nie endgültig.
func (p *Provider) Trash(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID) error {
	boxes, err := p.ListMailboxes(ctx)
	if err != nil {
		return err
	}
	for _, b := range boxes {
		if b.Role == provider.RoleTrash {
			if b.Path == mailbox {
				return nil // schon im Papierkorb
			}
			_, err := p.Move(ctx, mailbox, uidValidity, uids, b.Path)
			return err
		}
	}
	return fmt.Errorf("%w: no mailbox with role trash", provider.ErrNotFound)
}
