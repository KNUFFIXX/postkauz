package imap

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// Append legt eine Rohmail in mailbox ab (Entwürfe, S2.5; A02 E12) und
// liefert UID und UIDVALIDITY der Kopie.
//
// APPEND ist nicht wiederholbar: Bricht die Verbindung ab, nachdem der
// Server die Nachricht angenommen hat, aber bevor seine Antwort ankam, läge
// sie nach einem blinden Wiederholen doppelt im Ordner. Deshalb merkt sich
// Append vor dem ersten Versuch, welche UIDs mit o.MessageID schon im Ordner
// liegen (frühere Kopien desselben Entwurfs bleiben dort, kein Expunge), und
// sucht nach einem Verbindungsabbruch zuerst nach einer neuen Kopie mit
// dieser Message-ID; nur wenn keine da ist, legt es erneut ab. Nennt der
// Server keine UID (kein UIDPLUS), wird sie auf demselben Weg ermittelt
// (UID SEARCH HEADER Message-ID). Ohne o.MessageID entfällt beides (UID 0;
// nach einem Abbruch kann eine Dublette entstehen).
//
// Fehler: provider.ErrNotFound, wenn der Ordner fehlt; provider.ErrRejected
// (zusammen mit imapx.ErrRejected), wenn der Server das APPEND selbst
// ablehnt (NO/BAD, z. B. zu groß, Kontingent); Verbindungs- und
// Anmeldefehler wie bei allen Kommandos.
//
// Die Liste known gilt nur innerhalb eines Aufrufs. Bricht der Lauf ab,
// bevor der Aufrufer die Kopie vermerkt hat (Wiederaufbau gescheitert,
// Strg+C, Prozessende), findet der nächste Lauf sie über den vorab
// vermerkten Versuch im Spiegel wieder (internal/drafts, push_attempt;
// Gegenprüfung S2.4/S2.5, W1).
func (p *Provider) Append(ctx context.Context, mailbox string, raw []byte, o provider.AppendOptions) (provider.AppendResult, error) {
	var res provider.AppendResult
	var known []uint32 // UIDs mit dieser Message-ID vor dem ersten Versuch
	var uv uint32      // UIDVALIDITY vor dem ersten Versuch
	tried := false
	err := p.s.Do(ctx, func(c *imapx.Client) error {
		res = provider.AppendResult{}
		if !tried {
			st, err := c.Select(mailbox, imapx.SelectOptions{ReadOnly: true})
			if err != nil {
				return err
			}
			uv = st.UIDValidity
			if o.MessageID != "" {
				if known, err = c.SearchHeader("Message-ID", o.MessageID, 0); err != nil {
					return err
				}
			}
		} else if o.MessageID != "" {
			// Der vorige Versuch endete mit einem Verbindungsfehler: Liegt
			// die Kopie schon da?
			found, err := findCopy(c, mailbox, o.MessageID, uv, known)
			if err != nil {
				return err
			}
			if found.UID != 0 {
				res = found
				return nil
			}
		}
		tried = true
		r, err := c.AppendInfo(mailbox, raw, flagList(o.Flags), o.Date)
		if errors.Is(err, imapx.ErrRejected) {
			return fmt.Errorf("%w: %w", provider.ErrRejected, err)
		}
		if err != nil {
			return err
		}
		res = provider.AppendResult{UID: provider.UID(r.UID), UIDValidity: r.UIDValidity}
		if (res.UID == 0 || res.UIDValidity == 0) && o.MessageID != "" {
			found, err := findCopy(c, mailbox, o.MessageID, uv, known)
			if err != nil {
				return err
			}
			if res.UID == 0 {
				res.UID = found.UID
			}
			if res.UIDValidity == 0 {
				res.UIDValidity = found.UIDValidity
			}
		}
		return nil
	})
	p.mu.Lock()
	delete(p.cache, mailbox) // der Ordner hat sich geändert
	p.mu.Unlock()
	return res, err
}

// findCopy sucht die neue Kopie mit Message-ID mid: die höchste UID mit
// dieser Message-ID, die vor dem ersten Versuch nicht da war (bei
// unveränderter UIDVALIDITY uv). UID 0 = keine gefunden.
func findCopy(c *imapx.Client, mailbox, mid string, uv uint32, known []uint32) (provider.AppendResult, error) {
	st, err := c.Select(mailbox, imapx.SelectOptions{ReadOnly: true}) // frisch: die neue Nachricht soll sichtbar sein
	if err != nil {
		return provider.AppendResult{}, err
	}
	res := provider.AppendResult{UIDValidity: st.UIDValidity}
	uids, err := c.SearchHeader("Message-ID", mid, 0)
	if err != nil {
		return res, err
	}
	for i := len(uids) - 1; i >= 0; i-- {
		if st.UIDValidity != uv || !slices.Contains(known, uids[i]) {
			res.UID = provider.UID(uids[i])
			break
		}
	}
	return res, nil
}

// flagList übersetzt Flags in IMAP-Systemflags und Keywords.
func flagList(f provider.Flags) []string {
	var out []string
	for _, x := range []struct {
		on   bool
		name string
	}{{f.Seen, `\Seen`}, {f.Flagged, `\Flagged`}, {f.Answered, `\Answered`}, {f.Draft, `\Draft`}} {
		if x.on {
			out = append(out, x.name)
		}
	}
	return append(out, f.Keywords...)
}
