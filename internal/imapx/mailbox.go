package imapx

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// SelectInfo ist der Zustand eines gewählten Ordners (SELECT/EXAMINE).
type SelectInfo struct {
	Mailbox        string
	ReadOnly       bool
	Messages       uint32 // EXISTS
	UIDNext        uint32
	UIDValidity    uint32
	HighestModSeq  uint64 // 0 = unbekannt (ohne CONDSTORE)
	PermanentFlags []string
}

// SelectOptions steuert Select.
type SelectOptions struct {
	// ReadOnly wählt per EXAMINE: Der Sync verändert nie etwas (auch nicht
	// \Recent); schreibende Kommandos brauchen ReadOnly=false.
	ReadOnly bool
	// CondStore hängt (CONDSTORE) an; nur setzen, wenn der Server es kann.
	CondStore bool
}

// Select wählt einen Ordner und liefert seinen Zustand.
func (c *Client) Select(mailbox string, o SelectOptions) (SelectInfo, error) {
	var d *imap.SelectData
	err := c.run(func() error {
		var err error
		d, err = c.c.Select(mailbox, &imap.SelectOptions{ReadOnly: o.ReadOnly, CondStore: o.CondStore}).Wait()
		return err
	})
	if err != nil {
		c.sel = nil
		return SelectInfo{}, selectErr(err)
	}
	info := SelectInfo{Mailbox: mailbox, ReadOnly: o.ReadOnly, Messages: d.NumMessages, UIDNext: uint32(d.UIDNext),
		UIDValidity: d.UIDValidity, HighestModSeq: d.HighestModSeq}
	for _, f := range d.PermanentFlags {
		info.PermanentFlags = append(info.PermanentFlags, string(f))
	}
	c.sel = &info
	c.exists.Store(info.Messages)
	return info, nil
}

// selectErr: Ein abgelehntes SELECT heißt meist „Ordner gibt es nicht".
func selectErr(err error) error {
	var ie *imap.Error
	if errors.As(err, &ie) && ie.Type == imap.StatusResponseTypeNo &&
		(ie.Code == imap.ResponseCodeNonExistent || ie.Code == "") {
		return fmt.Errorf("%w: mailbox (%v)", provider.ErrNotFound, cmdErr(err))
	}
	return cmdErr(err)
}

// Selected liefert den aktuell gewählten Ordner (nil = keiner).
func (c *Client) Selected() *SelectInfo { return c.sel }

// Ensure wählt mailbox, falls nicht schon gewählt, und prüft UIDVALIDITY.
// Ein Wechsel (z. B. nach Reconnect, Ordner neu angelegt) ist
// provider.ErrUIDValidityChanged: Gespeicherte UIDs gelten nicht mehr.
// UIDVALIDITY 0 ist ErrInvalidUID, ohne Serveraufruf (A6): Wer UIDs
// anspricht, muss wissen, unter welchem Stand sie gelten; 0 hieße „ohne
// Prüfung“.
func (c *Client) Ensure(mailbox string, uidValidity uint32, o SelectOptions) (SelectInfo, error) {
	if uidValidity == 0 {
		return SelectInfo{}, fmt.Errorf("%w: UIDVALIDITY 0", ErrInvalidUID)
	}
	return c.ensure(mailbox, uidValidity, o)
}

// ensure ist Ensure ohne Pflicht-UIDVALIDITY (0 = nicht prüfen); nur für
// Kommandos ohne UIDs (IDLE).
func (c *Client) ensure(mailbox string, uidValidity uint32, o SelectOptions) (SelectInfo, error) {
	if s := c.sel; s != nil && s.Mailbox == mailbox && (s.ReadOnly == o.ReadOnly || !s.ReadOnly) {
		if uidValidity != 0 && s.UIDValidity != uidValidity {
			return *s, provider.ErrUIDValidityChanged
		}
		return *s, nil
	}
	info, err := c.Select(mailbox, o)
	if err != nil {
		return info, err
	}
	if uidValidity != 0 && info.UIDValidity != uidValidity {
		return info, provider.ErrUIDValidityChanged
	}
	return info, nil
}

// SearchOptions steuert UIDSearch.
type SearchOptions struct {
	// ESearch nutzt RETURN (ALL): Der Server antwortet mit Bereichen
	// (1:50000) statt jeder einzelnen UID. Nur mit IMAP4rev2/ESEARCH.
	ESearch bool
}

// UIDSearch liefert alle UIDs ab from im gewählten Ordner, aufsteigend
// (from <= 1: UID SEARCH ALL). „UID n:*" liefert laut RFC immer auch die
// höchste UID, selbst wenn sie kleiner als n ist; sie wird herausgefiltert.
func (c *Client) UIDSearch(from uint32, o SearchOptions) ([]uint32, error) {
	crit := &imap.SearchCriteria{}
	if from > 1 {
		crit.UID = []imap.UIDSet{{imap.UIDRange{Start: imap.UID(from), Stop: 0}}}
	}
	var opts *imap.SearchOptions
	if o.ESearch {
		opts = &imap.SearchOptions{ReturnAll: true}
	}
	var data *imap.SearchData
	err := c.run(func() error {
		var err error
		data, err = c.c.UIDSearch(crit, opts).Wait()
		return err
	})
	if err != nil {
		return nil, cmdErr(err)
	}
	return c.receivedUIDs(data, from)
}

// receivedUIDs löst das UID-Set einer SEARCH-Antwort auf (UIDs ab from).
// Nie über die Methode Nums des UIDSet: go-imap (beta.8) zählt dort mit
// uint32 bis zum Ende des Bereichs; endet er bei 4294967295, läuft die
// Schleife über und endet nie, und ein Server, der „1:4294967294“ meldet, ließe vier
// Milliarden Zahlen anlegen (T1a Punkt 2). Deshalb bereichsweise mit
// Obergrenzen: kein „*“, kein Bereich bis 4294967295, und zusammen
// höchstens so viele UIDs, wie der Ordner Nachrichten hat (EXISTS), und
// nie mehr als MaxReceivedUIDs (EXISTS kommt auch vom Server; W2). Sonst
// ErrRejected („invalid UID set“), nichts wird geliefert.
func (c *Client) receivedUIDs(data *imap.SearchData, from uint32) ([]uint32, error) {
	if data.All == nil {
		return nil, nil
	}
	set, ok := data.All.(imap.UIDSet)
	if !ok {
		return nil, errBadUIDSet
	}
	return boundedUIDs(set, from, c.existsBound())
}

var errBadUIDSet = fmt.Errorf("%w: server returned an invalid UID set", ErrRejected)

// existsBound ist die größte Zahl von UIDs, die eine Antwort im gewählten
// Ordner haben kann: EXISTS aus SELECT oder, falls größer, aus einer
// späteren unaufgeforderten EXISTS-Meldung. Ohne gewählten Ordner 0.
func (c *Client) existsBound() uint32 {
	if c.sel == nil {
		return 0
	}
	return max(c.sel.Messages, c.exists.Load())
}

// MaxReceivedUIDs ist die vom Server unabhängige Obergrenze für die UIDs
// einer SEARCH-Antwort (W2): EXISTS stammt selbst vom Server; mit EXISTS
// 4294967294 und „1:4294967294“ stünden sonst rund 16 GiB an. 10 Millionen
// UIDs belegen höchstens 40 MB; kein echter Ordner hat so viele
// Nachrichten (Gmail „Alle Nachrichten“ beim Betreiber: rund 50.000).
const MaxReceivedUIDs = 10_000_000

// boundedUIDs liefert die UIDs ab from eines empfangenen Sets, aufsteigend
// und ohne Dubletten. Bereichsweise: Erst werden alle Bereiche geprüft und
// gezählt (ohne Speicher), dann nur die Teile ab from aufgelöst. Abgelehnt
// (errBadUIDSet): „*“ (Start oder Stop 0), ein Bereich bis 4294967295 und
// mehr UIDs insgesamt als limit (EXISTS) oder MaxReceivedUIDs.
func boundedUIDs(set imap.UIDSet, from, limit uint32) ([]uint32, error) {
	limit = min(limit, MaxReceivedUIDs)
	var total, wanted uint64
	for _, r := range set {
		start, stop := uint32(r.Start), uint32(r.Stop)
		if start == 0 || stop == 0 {
			return nil, errBadUIDSet
		}
		if start > stop {
			start, stop = stop, start
		}
		if stop >= maxUID {
			return nil, errBadUIDSet
		}
		total += uint64(stop-start) + 1
		if total > uint64(limit) {
			return nil, fmt.Errorf("%w (%d UIDs or more for at most %d messages)", errBadUIDSet, total, limit)
		}
		if stop >= from {
			wanted += uint64(stop-max(start, from)) + 1
		}
	}
	out := make([]uint32, 0, wanted) // wanted <= total <= MaxReceivedUIDs
	for _, r := range set {
		start, stop := uint32(r.Start), uint32(r.Stop)
		if start > stop {
			start, stop = stop, start
		}
		for u := max(start, from); u <= stop; u++ { // stop < maxUID: kein Überlauf
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// MessageMeta ist die Kopfsicht einer Nachricht ohne Inhalt.
type MessageMeta struct {
	UID          uint32
	Flags        []string
	ModSeq       uint64
	InternalDate time.Time
	Size         int64
	// HeaderFields sind der Kopf bzw. die angeforderten Kopfzeilen, roh.
	HeaderFields []byte
	// HeaderSkipped nennt, warum HeaderFields fehlt ("" = gelesen):
	// HeaderTooLarge.
	HeaderSkipped string
}

// MetaOptions steuert FetchMeta.
type MetaOptions struct {
	// Header holt den ganzen Kopf (BODY.PEEK[HEADER]).
	Header bool
	// HeaderFields holt nur diese Kopfzeilen (BODY.PEEK[HEADER.FIELDS]).
	// Vorsicht: GreenMail 2.1 liefert dafür leere Inhalte, weil go-imap die
	// Feldnamen in Anführungszeichen setzt; der Sync nutzt deshalb Header.
	HeaderFields []string
	ModSeq       bool // MODSEQ mitholen (CONDSTORE)
	// MaxHeaderBytes begrenzt einen Kopf (0 = DefaultMaxHeaderBytes),
	// HeaderBudget die Köpfe eines Aufrufs (0 = DefaultHeaderBudget).
	MaxHeaderBytes int64
	HeaderBudget   int64
}

// maxUID (4294967295) wird nie gesendet und nie als Ende eines
// empfangenen Bereichs angenommen: go-imap (beta.8) zählt in
// Range.append bis Stop mit uint32 und läuft dort endlos (T1a Punkt 2).
const maxUID = math.MaxUint32

// ErrInvalidUID: Eine UID (0 oder 4294967295), eine leere UID-Liste oder
// UIDVALIDITY 0 sollte an den Server gehen. Der Aufruf scheitert, bevor
// etwas gesendet wird (A6: UID 0 hieße in go-imap „*“, also die neueste
// Nachricht des Ordners). Kein Verbindungsfehler, nicht wiederholbar.
var ErrInvalidUID = errors.New("imapx: invalid_uid")

// uidSet baut das UID-Set für ein Kommando. Einzige Stelle, die UID-Sets
// aus Zahlen zusammensetzt (`make guard`: kein AddNum/AddRange außerhalb).
// UID 0 („*“ in go-imap), 4294967295 und eine leere Liste sind
// ErrInvalidUID; das Ergebnis enthält nie „*“.
func uidSet(uids []uint32) (imap.UIDSet, error) {
	if len(uids) == 0 {
		return nil, fmt.Errorf("%w: empty UID list", ErrInvalidUID)
	}
	var s imap.UIDSet
	for _, u := range uids {
		if u == 0 || u == maxUID {
			return nil, fmt.Errorf("%w: UID %d", ErrInvalidUID, u)
		}
		s.AddNum(imap.UID(u))
	}
	if s.Dynamic() {
		return nil, fmt.Errorf("%w: set contains *", ErrInvalidUID)
	}
	return s, nil
}

// Grenzen für Köpfe in FetchMeta (A12.4): go-imap läse ein Literal sonst
// unbegrenzt in den Speicher, und ein Absender bestimmt die Größe des
// Kopfes.
const (
	// DefaultMaxHeaderBytes: größter Kopf, der gelesen wird (1 MiB; echte
	// Köpfe haben wenige KB, lange Signaturketten einige 10 KB).
	DefaultMaxHeaderBytes int64 = 1 << 20
	// DefaultHeaderBudget: Summe der Köpfe je FETCH (16 MiB).
	DefaultHeaderBudget int64 = 16 << 20
)

// Gründe, aus denen FetchMeta einen Kopf nicht liefert.
const (
	// HeaderTooLarge: Der Kopf ist größer als MaxHeaderBytes; er wird nicht
	// gelesen, die übrigen Angaben (Größe, Flags) schon.
	HeaderTooLarge = "too_large"
)

// ErrHeaderBudget: FetchMeta hat beim ersten Kopf, der nicht mehr ins
// Budget passte, abgebrochen (K3). Ein IMAP-FETCH lässt sich nicht
// anhalten; ohne Abbruch überträgt der Server alle übrigen Köpfe, die
// verworfen und erneut angefordert würden (bei vielen großen Köpfen ein
// Vielfaches der Daten). Deshalb ist die Verbindung danach geschlossen.
// Die gelieferten Nachrichten sind vollständig; die übrigen UIDs holt der
// Aufrufer mit einem neuen FETCH auf einer neuen Verbindung
// (internal/provider/imap, FetchHeaders). Als ErrProtocol verwirft
// Session die Verbindung, ohne f zu wiederholen.
var ErrHeaderBudget = fmt.Errorf("%w: header budget of one fetch exhausted; connection closed, fetch the rest again", provider.ErrProtocol)

// FetchMeta holt UID, FLAGS, INTERNALDATE, RFC822.SIZE (und MODSEQ sowie
// gewählte Kopfzeilen per BODY.PEEK) der angegebenen UIDs im gewählten
// Ordner. Nicht mehr vorhandene UIDs fehlen im Ergebnis. Köpfe werden
// gestreamt und begrenzt gelesen (A12.4): höchstens MaxHeaderBytes je Kopf
// (größere tragen HeaderSkipped) und HeaderBudget je Aufruf; passt ein Kopf
// nicht mehr ins Budget, kommen die bis dahin gelesenen Nachrichten mit
// ErrHeaderBudget zurück, und die Verbindung ist geschlossen.
func (c *Client) FetchMeta(uids []uint32, o MetaOptions) ([]MessageMeta, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	set, err := uidSet(uids)
	if err != nil {
		return nil, err
	}
	maxHeader, budget := o.MaxHeaderBytes, o.HeaderBudget
	if maxHeader <= 0 {
		maxHeader = DefaultMaxHeaderBytes
	}
	if budget <= 0 {
		budget = DefaultHeaderBudget
	}
	budget = max(budget, maxHeader) // ein Kopf bis zur Grenze passt immer
	opts := &imap.FetchOptions{UID: true, Flags: true, InternalDate: true, RFC822Size: true, ModSeq: o.ModSeq}
	if o.Header || len(o.HeaderFields) > 0 {
		sec := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
		if !o.Header {
			sec.HeaderFields = o.HeaderFields
		}
		opts.BodySection = []*imap.FetchItemBodySection{sec}
	}
	var out []MessageMeta
	aborted := false
	err = c.run(func() error {
		out = nil
		left := budget
		cmd := c.c.Fetch(set, opts)
	messages:
		for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
			var m MessageMeta
			for item := msg.Next(); item != nil; item = msg.Next() {
				switch it := item.(type) {
				case imapclient.FetchItemDataUID:
					m.UID = uint32(it.UID)
				case imapclient.FetchItemDataFlags:
					m.Flags = flagStrings(it.Flags)
				case imapclient.FetchItemDataInternalDate:
					m.InternalDate = it.Time
				case imapclient.FetchItemDataRFC822Size:
					m.Size = it.Size
				case imapclient.FetchItemDataModSeq:
					m.ModSeq = it.ModSeq
				case imapclient.FetchItemDataBodySection:
					if it.Literal == nil {
						continue
					}
					var err error
					var over bool
					m.HeaderFields, m.HeaderSkipped, over, err = readHeader(it.Literal, maxHeader, &left)
					if err != nil {
						_ = cmd.Close()
						return err
					}
					if over {
						// Erst die Leitung schließen (der Server sendet
						// nicht weiter, go-imap liest nichts mehr), dann
						// das Kommando freigeben.
						aborted = true
						_ = c.act.Conn.Close()
						_ = cmd.Close()
						break messages
					}
				}
			}
			if m.UID != 0 {
				out = append(out, m)
			}
		}
		if aborted {
			return nil
		}
		return cmd.Close()
	})
	// cmp.Compare statt int-Differenz: Auf 32-Bit-Systemen (int = int32)
	// liefe int(a.UID) - int(b.UID) bei UIDs über 2^31 über (K5).
	slices.SortFunc(out, func(a, b MessageMeta) int { return cmp.Compare(a.UID, b.UID) })
	if aborted {
		c.closeNow()
		return out, ErrHeaderBudget
	}
	if err != nil {
		return nil, cmdErr(err)
	}
	return out, nil
}

// readHeader liest einen Kopf, höchstens maxHeader Bytes und höchstens den
// Rest *left des Budgets. Ein größerer Kopf (angekündigt oder tatsächlich)
// wird nicht weiter gelesen (HeaderTooLarge; go-imap verwirft den Rest des
// Literals); passt er nicht mehr ins Budget, ist over true.
func readHeader(lit imap.LiteralReader, maxHeader int64, left *int64) (b []byte, skipped string, over bool, err error) {
	size := lit.Size()
	switch {
	case size > maxHeader:
		return nil, HeaderTooLarge, false, nil
	case size > *left:
		return nil, "", true, nil
	}
	limit := min(maxHeader, *left)
	b, err = io.ReadAll(io.LimitReader(lit, limit+1))
	if err != nil {
		return nil, "", false, err
	}
	*left -= int64(len(b))
	switch {
	case int64(len(b)) > maxHeader:
		return nil, HeaderTooLarge, false, nil
	case int64(len(b)) > limit:
		return nil, "", true, nil
	}
	return b, "", false, nil
}

func flagStrings(fs []imap.Flag) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, string(f))
	}
	return out
}

// FlagsData sind die Flags einer Nachricht (FetchFlags).
type FlagsData struct {
	UID    uint32
	Flags  []string
	ModSeq uint64
}

// FlagsOptions steuert FetchFlags.
type FlagsOptions struct {
	// ChangedSince > 0: nur Nachrichten mit MODSEQ darüber (CONDSTORE).
	ChangedSince uint64
	// ModSeq holt MODSEQ mit (CONDSTORE).
	ModSeq bool
}

// FetchFlags ruft fn für die Flags aller Nachrichten mit UID in [from, to]
// auf (to = 0: bis zur höchsten UID). Gestreamt, damit 50.000 Nachrichten
// nicht auf einmal im Speicher liegen.
func (c *Client) FetchFlags(from, to uint32, o FlagsOptions, fn func(FlagsData)) error {
	if from == 0 {
		from = 1
	}
	if to != 0 && to < from {
		return nil
	}
	set := imap.UIDSet{imap.UIDRange{Start: imap.UID(from), Stop: imap.UID(to)}}
	opts := &imap.FetchOptions{UID: true, Flags: true, ModSeq: o.ModSeq || o.ChangedSince > 0, ChangedSince: o.ChangedSince}
	err := c.run(func() error {
		cmd := c.c.Fetch(set, opts)
		for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
			b, err := msg.Collect()
			if err != nil {
				_ = cmd.Close()
				return err
			}
			if b.UID == 0 || uint32(b.UID) < from || (to != 0 && uint32(b.UID) > to) {
				continue
			}
			fn(FlagsData{UID: uint32(b.UID), Flags: flagStrings(b.Flags), ModSeq: b.ModSeq})
		}
		return cmd.Close()
	})
	return cmdErr(err)
}

// ErrMessageTooLarge: Die Rohmail ist größer als MaxMessageBytes
// ([imap] max_message_bytes). FetchRaw liest sie nicht und reicht dem
// Rückruf einen Reader, der diesen Fehler meldet; der Sync zählt das als
// Fehler dieser Nachricht und macht mit den übrigen weiter.
var ErrMessageTooLarge = errors.New("imapx: message larger than imap.max_message_bytes")

func (c *Client) tooLarge(size int64) error {
	if size > 0 {
		return fmt.Errorf("%w (%d bytes, limit %d)", ErrMessageTooLarge, size, c.maxMsg)
	}
	return fmt.Errorf("%w (limit %d)", ErrMessageTooLarge, c.maxMsg)
}

// errReader meldet beim ersten Lesen err.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// cappedReader liefert höchstens left Bytes; kommt danach noch etwas, meldet
// er tooLarge (statt still abzuschneiden: eine gekürzte Mail wäre falsch
// gespiegelt). Ein Server kann eine kleinere Größe ankündigen, als er
// schickt; der Rest des Literals verwirft go-imap beim nächsten Element.
type cappedReader struct {
	r    io.Reader
	left int64
	err  func() error
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		var one [1]byte
		n, err := c.r.Read(one[:])
		if n > 0 {
			return 0, c.err()
		}
		return 0, err
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// limited begrenzt eine Rohmail (r, angekündigte Größe size) auf maxMsg
// Bytes (A02 A4). Ist schon die Ankündigung zu groß, wird nichts gelesen.
func (c *Client) limited(r io.Reader, size int64) io.Reader {
	if size > c.maxMsg {
		return errReader{c.tooLarge(size)}
	}
	return &cappedReader{r: r, left: c.maxMsg, err: func() error { return c.tooLarge(0) }}
}

// buffer liest eine Rohmail in den Speicher, höchstens maxMsg Bytes; eine
// größere ergibt einen Reader, der ErrMessageTooLarge meldet (kein Fehler
// des Abrufs). Andere Lesefehler kommen zurück.
func (c *Client) buffer(lit imap.LiteralReader) (io.Reader, error) {
	b, err := io.ReadAll(c.limited(lit, lit.Size()))
	switch {
	case errors.Is(err, ErrMessageTooLarge):
		return errReader{err}, nil
	case err != nil:
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// FetchRaw streamt die vollständigen Rohmails (BODY.PEEK[], \Seen bleibt
// unberührt) der angegebenen UIDs und ruft fn je Nachricht auf. Der Reader
// gilt nur während fn. Nennt der Server die UID erst nach dem Inhalt, wird
// der Inhalt gepuffert, höchstens MaxMessageBytes. Eine größere Nachricht
// wird nicht gelesen: fn bekommt einen Reader, der ErrMessageTooLarge
// meldet (A02 A4); gibt fn nil zurück, geht es mit der nächsten weiter.
func (c *Client) FetchRaw(uids []uint32, fn func(uid uint32, r io.Reader) error) error {
	if len(uids) == 0 {
		return nil
	}
	set, err := uidSet(uids)
	if err != nil {
		return err
	}
	sec := &imap.FetchItemBodySection{Peek: true}
	opts := &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}
	var fnErr error
	err = c.run(func() error {
		cmd := c.c.Fetch(set, opts)
		for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
			var uid uint32
			var pending io.Reader // gepufferter Inhalt (UID kam erst danach)
			delivered := false
			for item := msg.Next(); item != nil; item = msg.Next() {
				switch it := item.(type) {
				case imapclient.FetchItemDataUID:
					uid = uint32(it.UID)
				case imapclient.FetchItemDataBodySection:
					if it.Literal == nil || fnErr != nil {
						// Nach einem Fehler des Rückrufs wird nichts mehr
						// gelesen (vorher: jede weitere Nachricht ganz in den
						// Speicher); go-imap verwirft den Rest des Literals.
						continue
					}
					if uid != 0 {
						u, size := uid, it.Literal.Size()
						fnErr = c.callback(it.Literal, func(r io.Reader) error { return fn(u, c.limited(r, size)) })
						delivered = true
					} else {
						// UID erst nach dem Inhalt: puffern, höchstens bis zur
						// Grenze. go-imap beta.8 ordnet solche Antworten
						// allerdings gar nicht dem Kommando zu (es erwartet die
						// UID vor jedem Literal); der Zweig bleibt für einen
						// Fork bzw. eine neuere Bibliothek.
						var err error
						if pending, err = c.buffer(it.Literal); err != nil {
							_ = cmd.Close()
							return err
						}
					}
				}
			}
			if !delivered && pending != nil && uid != 0 && fnErr == nil {
				u := uid
				fnErr = c.callback(pending, func(r io.Reader) error { return fn(u, r) })
			}
		}
		return cmd.Close()
	})
	if err != nil {
		return cmdErr(err)
	}
	return fnErr
}

// StatusInfo ist das Ergebnis von STATUS.
type StatusInfo struct {
	Messages    uint32
	Unseen      uint32
	UIDNext     uint32
	UIDValidity uint32
}

// Status fragt den Zustand eines Ordners ab, ohne ihn zu wählen.
func (c *Client) Status(mailbox string) (StatusInfo, error) {
	var d *imap.StatusData
	err := c.run(func() error {
		var err error
		d, err = c.c.Status(mailbox, &imap.StatusOptions{NumMessages: true, NumUnseen: true, UIDNext: true, UIDValidity: true}).Wait()
		return err
	})
	if err != nil {
		return StatusInfo{}, selectErr(err)
	}
	st := StatusInfo{UIDNext: uint32(d.UIDNext), UIDValidity: d.UIDValidity}
	if d.NumMessages != nil {
		st.Messages = *d.NumMessages
	}
	if d.NumUnseen != nil {
		st.Unseen = *d.NumUnseen
	}
	return st, nil
}

// Append legt eine Rohmail in mailbox ab und liefert die neue UID
// (APPENDUID, sonst 0). Gebraucht für Entwürfe (S2) und zum Befüllen der
// Testserver mit dem Korpus.
func (c *Client) Append(mailbox string, raw []byte, flags []string, date time.Time) (uint32, error) {
	info, err := c.AppendInfo(mailbox, raw, flags, date)
	return info.UID, err
}

// AppendResult ist die Antwort auf APPEND: UID und UIDVALIDITY der neuen
// Nachricht aus APPENDUID (UIDPLUS bzw. IMAP4rev2); ohne diese Angabe 0.
type AppendResult struct {
	UID         uint32
	UIDValidity uint32
}

// AppendInfo legt eine Rohmail in mailbox ab wie Append und liefert dazu
// die UIDVALIDITY aus APPENDUID (Entwürfe, S2.5). Ein abgelehntes APPEND
// (NO/BAD, z. B. Ordner fehlt, Kontingent voll) ist ErrRejected. APPEND ist
// nicht wiederholbar: Endet es mit einem Verbindungsfehler, kann die
// Nachricht trotzdem abgelegt sein; der Aufrufer prüft das, bevor er es
// erneut versucht (internal/provider/imap, Append).
func (c *Client) AppendInfo(mailbox string, raw []byte, flags []string, date time.Time) (AppendResult, error) {
	opts := &imap.AppendOptions{Time: date}
	for _, f := range flags {
		opts.Flags = append(opts.Flags, imap.Flag(f))
	}
	var data *imap.AppendData
	err := c.run(func() error {
		cmd := c.c.Append(mailbox, int64(len(raw)), opts)
		if _, err := cmd.Write(raw); err != nil {
			_ = cmd.Close()
			return err
		}
		if err := cmd.Close(); err != nil {
			return err
		}
		var err error
		data, err = cmd.Wait()
		return err
	})
	if err != nil {
		return AppendResult{}, cmdErr(err)
	}
	return AppendResult{UID: uint32(data.UID), UIDValidity: data.UIDValidity}, nil
}

// SearchHeader liefert die UIDs ab from (0 oder 1 = alle) im gewählten
// Ordner, deren Kopfzeile field den Text value enthält (UID SEARCH HEADER;
// laut RFC 3501 eine Teilzeichenfolge ohne Rücksicht auf Groß- und
// Kleinschreibung), aufsteigend. Gebraucht, um eine abgelegte Kopie über
// ihre Message-ID wiederzufinden (Server ohne APPENDUID, Wiederaufnahme nach
// Verbindungsabbruch; S2.5).
func (c *Client) SearchHeader(field, value string, from uint32) ([]uint32, error) {
	crit := &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: field, Value: value}}}
	if from > 1 {
		crit.UID = []imap.UIDSet{{imap.UIDRange{Start: imap.UID(from), Stop: 0}}}
	}
	var data *imap.SearchData
	err := c.run(func() error {
		var err error
		data, err = c.c.UIDSearch(crit, nil).Wait()
		return err
	})
	if err != nil {
		return nil, cmdErr(err)
	}
	return c.receivedUIDs(data, from)
}

// StoreFlags setzt (add=true) oder entfernt Flags an den angegebenen UIDs
// im per Select(ReadOnly=false) gewählten Ordner. Im Sync nie benutzt (der
// Sync liest nur); vorbereitet für S3 (mark) und für Testwerkzeuge.
// Es gibt bewusst kein EXPUNGE in dieser Schicht (ADR-002).
func (c *Client) StoreFlags(uids []uint32, flags []string, add bool) error {
	if len(uids) == 0 {
		return nil // nichts zu tun; nie ein leeres Set an den Server
	}
	if c.sel == nil || c.sel.ReadOnly {
		return fmt.Errorf("imapx: StoreFlags needs a mailbox selected read-write")
	}
	for _, f := range flags {
		if strings.EqualFold(f, `\Deleted`) {
			return fmt.Errorf("imapx: setting \\Deleted is not allowed (no expunge path, ADR-002)")
		}
	}
	op := imap.StoreFlagsDel
	if add {
		op = imap.StoreFlagsAdd
	}
	sf := &imap.StoreFlags{Op: op, Silent: true}
	for _, f := range flags {
		sf.Flags = append(sf.Flags, imap.Flag(f))
	}
	set, err := uidSet(uids)
	if err != nil {
		return err
	}
	err = c.run(func() error { return c.c.Store(set, sf, nil).Close() })
	return cmdErr(err)
}

// unilateralHandler meldet unaufgeforderte Serverdaten über c.changed
// (nicht blockierend; IDLE wertet es aus).
func (c *Client) unilateralHandler() *imapclient.UnilateralDataHandler {
	kick := func() {
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}
	return &imapclient.UnilateralDataHandler{
		Expunge: func(uint32) { kick() },
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				c.exists.Store(*d.NumMessages)
				kick()
			}
		},
		Fetch: func(msg *imapclient.FetchMessageData) {
			for msg.Next() != nil {
			}
			kick()
		},
	}
}
