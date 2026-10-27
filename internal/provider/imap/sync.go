package imap

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// DefaultWindow ist die Fenstergröße des Erstsyncs (Nachrichten je
// Changes-Aufruf; Spike: 200).
const DefaultWindow = 200

// searchCache hält das Ergebnis von UID SEARCH ALL je Ordner, solange sich
// UIDVALIDITY, UIDNEXT und EXISTS nicht ändern. Ein Erstsync in Fenstern
// fragt so einmal statt einmal je Fenster.
type searchCache struct {
	uidValidity, uidNext, exists uint32
	uids                         []uint32
}

func (p *Provider) esearch() bool { return p.caps.IMAP4rev2 || p.has("ESEARCH") }

func (p *Provider) selectOpts() imapx.SelectOptions {
	return imapx.SelectOptions{ReadOnly: true, CondStore: p.caps.CondStore}
}

func (p *Provider) cached(mailbox string, st imapx.SelectInfo) []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sc, ok := p.cache[mailbox]; ok && sc.uidValidity == st.UIDValidity && sc.uidNext == st.UIDNext && sc.exists == st.Messages {
		return sc.uids
	}
	return nil
}

// searchAll liefert alle UIDs des Ordners; fresh fragt den Server auch dann,
// wenn der Zwischenspeicher passt (voller Abgleich). Liefert der Server
// eine andere Zahl von UIDs als EXISTS aus demselben SELECT, ist das
// provider.ErrIncomplete (T1a Punkt 3): Aus einer leeren oder gekürzten
// Liste folgten sonst Löschungen, die es nicht gab.
func (p *Provider) searchAll(c *imapx.Client, mailbox string, st imapx.SelectInfo, fresh bool) ([]uint32, error) {
	if uids := p.cached(mailbox, st); uids != nil && !fresh {
		return uids, nil
	}
	uids, err := c.UIDSearch(0, imapx.SearchOptions{ESearch: p.esearch()})
	if err != nil {
		return nil, err
	}
	if len(uids) != int(st.Messages) {
		return nil, fmt.Errorf("%w: UID SEARCH ALL returned %d UIDs, EXISTS is %d", provider.ErrIncomplete, len(uids), st.Messages)
	}
	p.mu.Lock()
	if p.cache == nil {
		p.cache = map[string]searchCache{}
	}
	p.cache[mailbox] = searchCache{uidValidity: st.UIDValidity, uidNext: st.UIDNext, exists: st.Messages, uids: uids}
	p.mu.Unlock()
	return uids, nil
}

func (p *Provider) searchFrom(c *imapx.Client, mailbox string, st imapx.SelectInfo, from uint32) ([]uint32, error) {
	if uids := p.cached(mailbox, st); uids != nil {
		return fromUID(uids, from), nil
	}
	return c.UIDSearch(from, imapx.SearchOptions{ESearch: p.esearch()})
}

func fromUID(sorted []uint32, from uint32) []uint32 {
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i] >= from })
	return sorted[i:]
}

func toUIDs(in []uint32) []provider.UID {
	out := make([]provider.UID, len(in))
	for i, u := range in {
		out[i] = provider.UID(u)
	}
	return out
}

func fromUIDs(in []provider.UID) []uint32 {
	out := make([]uint32, len(in))
	for i, u := range in {
		out[i] = uint32(u)
	}
	return out
}

// Flags übersetzt IMAP-Flags. \Recent und \Deleted sind keine Keywords;
// freie Keywords werden sortiert.
func Flags(list []string) provider.Flags {
	var f provider.Flags
	for _, s := range list {
		switch strings.ToLower(s) {
		case `\seen`:
			f.Seen = true
		case `\flagged`:
			f.Flagged = true
		case `\answered`:
			f.Answered = true
		case `\draft`:
			f.Draft = true
		default:
			if !strings.HasPrefix(s, `\`) && s != "" {
				f.Keywords = append(f.Keywords, s)
			}
		}
	}
	sort.Strings(f.Keywords)
	f.Keywords = slices.Compact(f.Keywords)
	return f
}

// Changes ermittelt die Änderungen eines Ordners (docs/architektur.md,
// „Änderungserkennung"; ADR-007: ohne QRESYNC):
//
//   - SELECT (EXAMINE, mit CONDSTORE wo vorhanden). Anderes UIDVALIDITY als
//     since: Reset, der Ordner wird wie neu behandelt.
//   - Neue UIDs ab since.UIDNext (UID SEARCH). Stimmt EXISTS nicht mit
//     KnownCount plus Neuen überein (oder FullScan, CheckDeleted,
//     Erstabgleich, Reset), folgt UID SEARCH ALL: Present für den
//     Löschabgleich (bei FullScan und CheckDeleted frisch vom Server).
//   - Höchstens Limit neue UIDs je Aufruf; More meldet den Rest. Der Cursor
//     NewState.UIDNext steht dann hinter dem Fenster.
//   - Flags erst im letzten Fenster: CONDSTORE per CHANGEDSINCE ab dem
//     gespeicherten HIGHESTMODSEQ (im Erstsync der Stand vor dem ersten
//     Fenster), sonst alle bekannten UIDs (Strategie uid-scan).
func (p *Provider) Changes(ctx context.Context, mailbox string, since provider.SyncState, o provider.ChangesOptions) (provider.ChangeSet, error) {
	limit := o.Limit
	if limit <= 0 {
		limit = DefaultWindow
	}
	var cs provider.ChangeSet
	err := p.s.Do(ctx, func(c *imapx.Client) error {
		cs = provider.ChangeSet{}
		st, err := c.Select(mailbox, p.selectOpts())
		if err != nil {
			return err
		}
		s := since
		if s.UIDValidity != 0 && s.UIDValidity != st.UIDValidity {
			cs.Reset = true
			s = provider.SyncState{}
		}
		cs.Exists = st.Messages
		from := max(s.UIDNext, 1)

		var newUIDs []uint32
		fresh := o.FullScan || o.CheckDeleted
		needAll := s.UIDValidity == 0 || fresh
		if !needAll && (st.UIDNext == 0 || st.UIDNext > from) {
			if newUIDs, err = p.searchFrom(c, mailbox, st, from); err != nil {
				return err
			}
		}
		if !needAll && int(st.Messages) != o.KnownCount+len(newUIDs) {
			needAll = true
		}
		if needAll {
			all, err := p.searchAll(c, mailbox, st, fresh)
			if err != nil {
				return err
			}
			cs.Present = toUIDs(all)
			newUIDs = fromUID(all, from)
		}

		window := newUIDs
		if len(window) > limit {
			window = window[:limit]
			cs.More = true
		}
		cs.Created = toUIDs(window)
		cs.NewState = provider.SyncState{UIDValidity: st.UIDValidity, UIDNext: st.UIDNext}
		if cs.More {
			cs.NewState.UIDNext = window[len(window)-1] + 1
		} else if len(newUIDs) > 0 && cs.NewState.UIDNext <= newUIDs[len(newUIDs)-1] {
			cs.NewState.UIDNext = newUIDs[len(newUIDs)-1] + 1 // Server ohne UIDNEXT
		}
		if cs.NewState.UIDNext < from {
			cs.NewState.UIDNext = from
		}

		condstore := p.caps.CondStore && st.HighestModSeq > 0
		if condstore {
			cs.NewState.HighestModSeq = st.HighestModSeq
			if cs.More && s.HighestModSeq > 0 {
				cs.NewState.HighestModSeq = s.HighestModSeq // Untergrenze bis zum letzten Fenster halten
			}
		}
		if cs.More || from <= 1 {
			return nil // Flags erst im letzten Fenster; ohne bekannte UIDs nichts zu prüfen
		}
		fo := imapx.FlagsOptions{ModSeq: condstore}
		to := from - 1
		if condstore && s.HighestModSeq > 0 && !o.FullScan {
			fo.ChangedSince = s.HighestModSeq
			to = 0
		}
		return c.FetchFlags(1, to, fo, func(f imapx.FlagsData) {
			cs.Updated = append(cs.Updated, provider.FlagUpdate{UID: provider.UID(f.UID), ModSeq: f.ModSeq, Flags: Flags(f.Flags)})
		})
	})
	if err != nil {
		return provider.ChangeSet{}, err
	}
	return cs, nil
}

// FetchHeaders liefert Flags, Datum, Größe, MODSEQ, Kopf und Message-ID der UIDs.
//
// Ganzer Kopf statt HEADER.FIELDS (MESSAGE-ID): GreenMail liefert für die
// von go-imap gequoteten Feldnamen nichts; der Kopf kostet wenige KB je
// Nachricht und wird mit demselben Parser gelesen wie später die ganze
// Nachricht. Köpfe begrenzt (A12.4): Ein zu großer Kopf wird nicht gelesen
// und gekennzeichnet. Passt ein Kopf nicht mehr ins Budget eines FETCH,
// bricht imapx den FETCH samt Verbindung ab (K3, imapx.ErrHeaderBudget);
// die übrigen UIDs kommen mit dem nächsten FETCH auf neuer Verbindung.
func (p *Provider) FetchHeaders(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID) ([]provider.Header, error) {
	var out []provider.Header
	todo := fromUIDs(uids)
	for len(todo) > 0 {
		var meta []imapx.MessageMeta
		err := p.s.Do(ctx, func(c *imapx.Client) error {
			meta = nil
			if _, err := c.Ensure(mailbox, uidValidity, p.selectOpts()); err != nil {
				return err
			}
			var err error
			meta, err = c.FetchMeta(todo, imapx.MetaOptions{Header: true, ModSeq: p.caps.CondStore})
			return err
		})
		aborted := errors.Is(err, imapx.ErrHeaderBudget)
		if err != nil && !aborted {
			return nil, err
		}
		if aborted && len(meta) == 0 {
			return nil, fmt.Errorf("%w: no header fits the fetch budget", provider.ErrProtocol)
		}
		got := make(map[uint32]bool, len(meta))
		for _, m := range meta {
			got[m.UID] = true
			out = append(out, provider.Header{UID: provider.UID(m.UID), ModSeq: m.ModSeq, Flags: Flags(m.Flags),
				InternalDate: m.InternalDate, Size: m.Size, MessageID: mimeparse.HeaderMessageID(m.HeaderFields),
				RawHeader: m.HeaderFields, HeaderTooLarge: m.HeaderSkipped == imapx.HeaderTooLarge})
		}
		if !aborted {
			break
		}
		todo = slices.DeleteFunc(todo, func(u uint32) bool { return got[u] })
	}
	slices.SortFunc(out, func(a, b provider.Header) int { return cmp.Compare(a.UID, b.UID) })
	return out, nil
}

// FetchRaw streamt die Rohmails der UIDs (BODY.PEEK[]).
func (p *Provider) FetchRaw(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID, fn func(provider.UID, io.Reader) error) error {
	return p.s.Do(ctx, func(c *imapx.Client) error {
		if _, err := c.Ensure(mailbox, uidValidity, p.selectOpts()); err != nil {
			return err
		}
		return c.FetchRaw(fromUIDs(uids), func(uid uint32, r io.Reader) error { return fn(provider.UID(uid), r) })
	})
}

// Idle hält auf einer eigenen Verbindung IDLE auf mailbox und meldet jede
// Änderung auf notify (nicht blockierend). IDLE wird alle IdleRestart neu
// gestartet (ADR-007: 5 bis 10 Minuten). Verbindungsfehler werden mit
// Backoff wiederholt (imapx.Session); scheitert das, kommt der Fehler
// zurück, ErrAuth sofort. ctx-Ende: nil.
func (p *Provider) Idle(ctx context.Context, mailbox string, notify chan<- struct{}) error {
	if !p.caps.Idle {
		return provider.ErrUnsupported
	}
	s := p.newSession()
	defer s.Close()
	restart := p.cfg.IdleRestart
	if restart <= 0 {
		restart = imapx.DefaultIdleRestart
	}
	for {
		var changed bool
		err := s.Do(ctx, func(c *imapx.Client) error {
			var err error
			changed, err = c.Idle(ctx, mailbox, restart)
			return err
		})
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if changed {
			select {
			case notify <- struct{}{}:
			default:
			}
		}
	}
}
