// Package mailsync spiegelt ein Postfach in den lokalen Store (S1.4/S1.5).
// Es arbeitet gegen eine schmale Provider-Schnittstelle (Remote) und ist
// damit unabhängig von IMAP; die Strategie (CONDSTORE, UID-Scan) steckt im
// Provider (docs/architektur.md, „Änderungserkennung").
//
// Ablauf je Ordner und Fenster:
//
//  1. Remote.Changes liefert neue UIDs (höchstens Window), Flag-Änderungen,
//     ggf. die vollständige UID-Liste (Löschabgleich) und den neuen Zustand.
//  2. Für neue UIDs zuerst die Köpfe (Message-ID, Größe, Flags). Gibt es die
//     Nachricht schon (Kopie, Label, Verschieben, UIDVALIDITY-Wechsel), wird
//     sie nur zugeordnet: kein erneutes Laden, keine Dublette.
//  3. Sonst Rohmail laden, in den Blob-Store schreiben, parsen.
//  4. Alles in einer Transaktion samt neuem Zustand speichern
//     (store.ApplyBatch). Ein Abbruch wiederholt höchstens das Fenster;
//     (mailbox_id, uid) verhindert Doppelte.
//
// Der Server löscht, das Archiv behält (ADR-003): Löschungen lösen nur die
// Zuordnung; ohne Zuordnung bekommt die Nachricht deleted_remote_at.
package mailsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Remote ist der Teil von provider.MailProvider, den der Sync braucht.
type Remote interface {
	Capabilities() provider.Capabilities
	ListMailboxes(ctx context.Context) ([]provider.Mailbox, error)
	Changes(ctx context.Context, mailbox string, since provider.SyncState, opts provider.ChangesOptions) (provider.ChangeSet, error)
	FetchHeaders(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID) ([]provider.Header, error)
	FetchRaw(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID, fn func(provider.UID, io.Reader) error) error
	Idle(ctx context.Context, mailbox string, notify chan<- struct{}) error
}

// Store ist der Teil des Stores, den der Sync braucht.
type Store interface {
	SyncMailboxes(ctx context.Context, accountID int64, boxes []store.MailboxSpec) (added, removed []string, err error)
	Mailboxes(ctx context.Context, accountID int64) ([]store.Mailbox, error)
	MailboxUIDs(ctx context.Context, mailboxID int64) ([]uint32, error)
	LinkCount(ctx context.Context, mailboxID int64) (int, error)
	CountPresentMessages(ctx context.Context, accountID int64) (int, error)
	FindLinkCandidate(ctx context.Context, accountID, mailboxID int64, messageID string, size int64) (int64, bool, error)
	FindByContentHash(ctx context.Context, accountID, mailboxID int64, hash string) (int64, bool, error)
	ApplyBatch(ctx context.Context, b store.MirrorBatch) (store.BatchResult, error)
}

// Standardwerte.
const (
	DefaultWindow = 200
	// maxBatchBytes teilt ein Fenster mit großen Nachrichten in mehrere
	// Transaktionen (der Cursor wandert erst mit der letzten).
	maxBatchBytes = 32 << 20
	snippetLen    = 200
)

// Options steuert einen Sync-Lauf.
type Options struct {
	// Window ist die Zahl neuer Nachrichten je Fenster (0 = 200).
	Window int
	// Full erzwingt Lösch- und Flag-Abgleich aller Nachrichten (ohne
	// CHANGEDSINCE) und holt Fehlendes nach (`sync --full`).
	Full bool
	// Mailboxes beschränkt den Lauf auf diese Pfade (leer = alle).
	Mailboxes []string
}

// Event ist eine Fortschrittsmeldung.
type Event struct {
	Kind    string // mailbox_start | window | mailbox_done | error
	Mailbox string
	Done    int // im Ordner bisher gespiegelt
	Total   int // Nachrichten laut Server
	Message string
}

// MailboxReport ist das Ergebnis eines Ordners.
type MailboxReport struct {
	Path     string   `json:"path"`
	Role     string   `json:"role"`
	New      int      `json:"new"`     // neu gespiegelte Kopien (geladen oder zugeordnet)
	Updated  int      `json:"updated"` // Kopien mit geänderten Flags
	Removed  int      `json:"removed"` // auf dem Server gelöschte Kopien
	Errors   int      `json:"errors"`
	Reset    bool     `json:"reset,omitempty"` // UIDVALIDITY-Wechsel erkannt
	Total    int      `json:"total"`           // laut Server
	Mirrored int      `json:"mirrored"`        // lokal nach dem Lauf
	Problems []string `json:"problems,omitempty"`
}

// Report ist das Ergebnis eines Laufs.
type Report struct {
	Mailboxes []MailboxReport `json:"mailboxes"`
	New       int             `json:"new"`
	Updated   int             `json:"updated"`
	Removed   int             `json:"removed"`
	Errors    int             `json:"errors"`
	// Mirrored zählt die Nachrichten des Kontos, die der Server führt und
	// die lokal gespiegelt sind (ohne Dubletten über Ordner hinweg).
	Mirrored int           `json:"messages_mirrored"`
	Duration time.Duration `json:"-"`
}

func (r *Report) add(m MailboxReport) {
	r.Mailboxes = append(r.Mailboxes, m)
	r.New += m.New
	r.Updated += m.Updated
	r.Removed += m.Removed
	r.Errors += m.Errors
}

// Engine synct ein Konto.
type Engine struct {
	Store     Store
	Blobs     *blob.Store
	Remote    Remote
	AccountID int64
	Log       *slog.Logger
	// Progress empfängt Fortschrittsmeldungen (optional).
	Progress func(Event)
	// AfterBatch wird nach jedem gespeicherten Fenster aufgerufen; ein Fehler
	// bricht den Lauf hart ab (Tests simulieren damit einen Absturz).
	AfterBatch func(mailbox string, mirrored int) error
}

func (e *Engine) log() *slog.Logger {
	if e.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return e.Log
}

func (e *Engine) emit(ev Event) {
	if e.Progress != nil {
		e.Progress(ev)
	}
}

// ErrAborted kennzeichnet einen Abbruch durch AfterBatch.
var ErrAborted = errors.New("mailsync: aborted")

// SyncAll gleicht die Ordnerliste ab und synct alle (bzw. die gewählten)
// Ordner, INBOX zuerst. Fehler eines Ordners zählen als Fehler und der Lauf
// geht weiter; Verbindungs- und Anmeldefehler sowie ctx-Ende brechen ab.
func (e *Engine) SyncAll(ctx context.Context, o Options) (Report, error) {
	start := time.Now()
	var rep Report
	boxes, err := e.Remote.ListMailboxes(ctx)
	if err != nil {
		return rep, err
	}
	specs := make([]store.MailboxSpec, 0, len(boxes))
	for _, b := range boxes {
		specs = append(specs, store.MailboxSpec{Path: b.Path, Name: b.Name, Role: string(b.Role)})
	}
	if _, removed, err := e.Store.SyncMailboxes(ctx, e.AccountID, specs); err != nil {
		return rep, err
	} else if len(removed) > 0 {
		e.log().Info("mailboxes removed on the server", "count", len(removed))
	}
	local, err := e.Store.Mailboxes(ctx, e.AccountID)
	if err != nil {
		return rep, err
	}
	for _, mb := range local {
		if len(o.Mailboxes) > 0 && !slices.Contains(o.Mailboxes, mb.Path) {
			continue
		}
		mr, err := e.SyncMailbox(ctx, mb, o)
		if err != nil && !fatal(ctx, err) {
			// Ordner überspringen, Lauf fortsetzen; der nächste Lauf
			// setzt am gespeicherten Stand wieder an.
			mr.Errors++
			mr.problem(err.Error())
			e.emit(Event{Kind: "error", Mailbox: mb.Path, Message: err.Error()})
		}
		rep.add(mr)
		if err != nil && fatal(ctx, err) {
			rep.Duration = time.Since(start)
			return rep, err
		}
	}
	if rep.Mirrored, err = e.Store.CountPresentMessages(ctx, e.AccountID); err != nil {
		return rep, err
	}
	rep.Duration = time.Since(start)
	return rep, nil
}

// fatal: Fehler, nach denen weitere Ordner keinen Sinn haben.
func fatal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, provider.ErrAuth) || errors.Is(err, provider.ErrConnection) ||
		errors.Is(err, ErrAborted) || errors.Is(err, context.Canceled)
}

// SyncMailbox synct einen Ordner in Fenstern bis zum aktuellen Stand.
func (e *Engine) SyncMailbox(ctx context.Context, mb store.Mailbox, o Options) (MailboxReport, error) {
	rep := MailboxReport{Path: mb.Path, Role: mb.Role, Mirrored: mb.Mirrored, Total: mb.Total}
	window := o.Window
	if window <= 0 {
		window = DefaultWindow
	}
	state := provider.SyncState{UIDValidity: mb.UIDValidity, UIDNext: mb.UIDNext, HighestModSeq: mb.HighestModSeq}
	if mb.Phase == store.PhaseNew {
		state = provider.SyncState{}
	}
	e.emit(Event{Kind: "mailbox_start", Mailbox: mb.Path, Done: mb.Mirrored, Total: mb.Total})
	retries := 0
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		known, err := e.Store.LinkCount(ctx, mb.ID)
		if err != nil {
			return rep, err
		}
		// FullScan nur beim ersten Aufruf je Ordner: Er holt die vollständige
		// UID-Liste und prüft alle Flags; folgende Fenster brauchen das nicht.
		cs, err := e.Remote.Changes(ctx, mb.Path, state, provider.ChangesOptions{Limit: window, KnownCount: known, FullScan: o.Full && first})
		first = false
		if err != nil {
			return rep, fmt.Errorf("%s: %w", mb.Path, err)
		}
		if cs.Reset {
			rep.Reset = true
			e.log().Warn("UIDVALIDITY changed; re-matching mailbox by Message-ID", "mailbox", mb.Path)
			state = provider.SyncState{}
		}
		err = e.applyChanges(ctx, mb, state, cs, &rep)
		if errors.Is(err, provider.ErrUIDValidityChanged) && retries < 3 {
			retries++ // zwischen Changes und Fetch neu angelegt: nochmal von vorn
			continue
		}
		if err != nil {
			return rep, err
		}
		state = cs.NewState
		rep.Total = int(cs.Exists)
		e.emit(Event{Kind: "window", Mailbox: mb.Path, Done: rep.Mirrored, Total: rep.Total})
		if e.AfterBatch != nil {
			if err := e.AfterBatch(mb.Path, rep.Mirrored); err != nil {
				return rep, fmt.Errorf("%w: %v", ErrAborted, err)
			}
		}
		if !cs.More {
			break
		}
	}
	if n, err := e.Store.LinkCount(ctx, mb.ID); err == nil {
		rep.Mirrored = n
	}
	e.emit(Event{Kind: "mailbox_done", Mailbox: mb.Path, Done: rep.Mirrored, Total: rep.Total})
	return rep, nil
}

// applyChanges verarbeitet ein ChangeSet und speichert es samt Zustand.
func (e *Engine) applyChanges(ctx context.Context, mb store.Mailbox, since provider.SyncState, cs provider.ChangeSet, rep *MailboxReport) error {
	uv := cs.NewState.UIDValidity
	batch := store.MirrorBatch{AccountID: e.AccountID, MailboxID: mb.ID}
	if cs.Reset {
		// Alte Zuordnungen sofort lösen: Erst danach sind die Nachrichten des
		// Ordners Kandidaten für den Abgleich per Message-ID (sonst würden sie
		// neu geladen und doppelt angelegt). Bricht der Lauf danach ab, sieht
		// der nächste wieder das alte UIDVALIDITY und setzt erneut zurück.
		res, err := e.Store.ApplyBatch(ctx, store.MirrorBatch{AccountID: e.AccountID, MailboxID: mb.ID, Reset: true})
		if err != nil {
			return err
		}
		rep.Mirrored -= res.Unlinked
	}

	fetch := slices.Clone(cs.Created)
	if cs.Present != nil {
		var local []uint32
		if !cs.Reset {
			var err error
			if local, err = e.Store.MailboxUIDs(ctx, mb.ID); err != nil {
				return err
			}
		}
		removed, missing := diff(local, cs.Present, max(since.UIDNext, 1))
		batch.Removed = removed
		if len(missing) > 0 {
			e.log().Info("fetching messages missing locally", "mailbox", mb.Path, "count", len(missing))
			fetch = append(fetch, missing...)
			slices.Sort(fetch)
			fetch = slices.Compact(fetch)
		}
	}
	for _, u := range cs.Updated {
		batch.Flags = append(batch.Flags, linkFlags(u.UID, u.ModSeq, u.Flags))
	}

	state := &store.MailboxState{UIDValidity: uv, UIDNext: cs.NewState.UIDNext, HighestModSeq: cs.NewState.HighestModSeq,
		Phase: store.PhaseReady, Total: int(cs.Exists)}
	if cs.More {
		state.Phase = store.PhaseInitial
	}

	var pending store.MirrorBatch
	var pendingBytes int64
	flush := func(final bool) error {
		b := pending
		b.AccountID, b.MailboxID = e.AccountID, mb.ID
		if final {
			b.Removed, b.Flags, b.State = batch.Removed, batch.Flags, state
		}
		res, err := e.Store.ApplyBatch(ctx, b)
		if err != nil {
			return err
		}
		rep.New += res.New + res.Linked
		rep.Updated += res.Updated
		rep.Removed += res.Removed
		rep.Mirrored += res.New + res.Linked - res.Removed
		pending, pendingBytes = store.MirrorBatch{}, 0
		return nil
	}

	for len(fetch) > 0 {
		n := min(len(fetch), DefaultWindow)
		chunk := fetch[:n]
		fetch = fetch[n:]
		headers, err := e.Remote.FetchHeaders(ctx, mb.Path, uv, chunk)
		if err != nil {
			return err
		}
		var need []provider.Header
		for _, h := range headers {
			if id, ok, err := e.Store.FindLinkCandidate(ctx, e.AccountID, mb.ID, h.MessageID, h.Size); err != nil {
				return err
			} else if ok && !linked(pending, id) {
				pending.Links = append(pending.Links, store.Link{MessageID: id, Flags: linkFlags(h.UID, h.ModSeq, h.Flags)})
				continue
			}
			need = append(need, h)
		}
		if len(need) == 0 {
			continue
		}
		byUID := map[provider.UID]provider.Header{}
		uids := make([]provider.UID, 0, len(need))
		for _, h := range need {
			byUID[h.UID] = h
			uids = append(uids, h.UID)
		}
		got := map[provider.UID]bool{}
		err = e.Remote.FetchRaw(ctx, mb.Path, uv, uids, func(uid provider.UID, r io.Reader) error {
			h, ok := byUID[uid]
			if !ok || got[uid] {
				_, _ = io.Copy(io.Discard, r)
				return nil
			}
			nm, err := e.mirrorRaw(r, h)
			if err != nil {
				rep.Errors++
				rep.problem(fmt.Sprintf("uid %d: %v", uid, err))
				e.log().Warn("message not mirrored", "mailbox", mb.Path, "uid", uid, "error", err.Error())
				got[uid] = true
				return nil
			}
			got[uid] = true
			if id, ok, err := e.Store.FindByContentHash(ctx, e.AccountID, mb.ID, nm.ContentHash); err != nil {
				return err
			} else if ok && !linked(pending, id) {
				pending.Links = append(pending.Links, store.Link{MessageID: id, Flags: nm.Link})
				return nil
			}
			pending.New = append(pending.New, nm)
			pendingBytes += nm.Size
			return nil
		})
		if err != nil {
			return err
		}
		for _, h := range need {
			if !got[h.UID] {
				// Zwischen Kopf und Inhalt gelöscht: kein Fehler, der nächste
				// Lauf gleicht ab.
				e.log().Info("message vanished during fetch", "mailbox", mb.Path, "uid", h.UID)
			}
		}
		if pendingBytes > maxBatchBytes && len(fetch) > 0 {
			if err := flush(false); err != nil {
				return err
			}
		}
	}
	return flush(true)
}

func (r *MailboxReport) problem(s string) {
	if len(r.Problems) < 10 {
		r.Problems = append(r.Problems, s)
	}
}

func linked(b store.MirrorBatch, messageID int64) bool {
	for _, l := range b.Links {
		if l.MessageID == messageID {
			return true
		}
	}
	return false
}

// diff liefert, was lokal bekannt, aber nicht mehr vorhanden ist
// (removed), und was vorhanden, unterhalb von below, aber lokal unbekannt
// ist (missing; frühere Fehler oder Lücken). Beide Listen sind sortiert.
func diff(local []uint32, present []provider.UID, below uint32) (removed []uint32, missing []provider.UID) {
	i, j := 0, 0
	for i < len(local) || j < len(present) {
		switch {
		case j >= len(present) || (i < len(local) && local[i] < uint32(present[j])):
			removed = append(removed, local[i])
			i++
		case i >= len(local) || uint32(present[j]) < local[i]:
			if uint32(present[j]) < below {
				missing = append(missing, present[j])
			}
			j++
		default:
			i++
			j++
		}
	}
	return removed, missing
}

func linkFlags(uid provider.UID, modseq uint64, f provider.Flags) store.LinkFlags {
	return store.LinkFlags{UID: uint32(uid), ModSeq: modseq, Seen: f.Seen, Flagged: f.Flagged, Answered: f.Answered,
		Draft: f.Draft, Keywords: f.Keywords}
}

// crStripper entfernt CR-Bytes für content_hash (CRLF und LF gelten gleich).
type crStripper struct{ w io.Writer }

func (c crStripper) Write(p []byte) (int, error) {
	if !slicesContainsCR(p) {
		return c.w.Write(p)
	}
	q := make([]byte, 0, len(p))
	for _, b := range p {
		if b != '\r' {
			q = append(q, b)
		}
	}
	if _, err := c.w.Write(q); err != nil {
		return 0, err
	}
	return len(p), nil
}

func slicesContainsCR(p []byte) bool {
	for _, b := range p {
		if b == '\r' {
			return true
		}
	}
	return false
}

// ContentHash berechnet content_hash: SHA-256 über die Rohmail ohne
// CR-Bytes (Zeilenenden CRLF und LF gelten gleich; Grundlage für den
// Abgleich beim Import, S1.10).
func ContentHash(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(crStripper{h}, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// mirrorRaw schreibt die Rohmail in den Blob-Store und parst sie aus der
// gespeicherten Datei (der Inhalt liegt so nie ganz im Speicher).
func (e *Engine) mirrorRaw(r io.Reader, h provider.Header) (store.NewMessage, error) {
	info, err := e.Blobs.Put(r)
	if err != nil {
		return store.NewMessage{}, err
	}
	if info.Size == 0 && h.Size > 0 {
		// Server meldet eine Größe, liefert aber nichts (Serverfehler, z. B.
		// der go-imap-Testserver bei kaputten Kopfzeilen): nicht spiegeln,
		// der nächste Lauf versucht es erneut.
		return store.NewMessage{}, fmt.Errorf("server returned an empty body for a message of %d bytes", h.Size)
	}
	nm := store.NewMessage{Link: linkFlags(h.UID, h.ModSeq, h.Flags), BlobSHA256: info.SHA256, BlobSize: info.Size,
		BlobCompressed: info.Compressed, Size: info.Size, InternalDate: h.InternalDate.UTC(), MessageID: h.MessageID}
	if info.Existed {
		// Vorhandene Datei: Form (komprimiert oder nicht) von dort übernehmen.
		if _, compressed, err := e.Blobs.Path(info.SHA256); err == nil {
			nm.BlobCompressed = compressed
		}
	}
	rc, err := e.Blobs.Get(info.SHA256)
	if err != nil {
		return nm, err
	}
	defer rc.Close()
	hr := sha256.New()
	m, perr := mimeparse.Parse(io.TeeReader(rc, crStripper{hr}), mimeparse.Options{InternalDate: h.InternalDate})
	nm.ContentHash = hex.EncodeToString(hr.Sum(nil))
	if perr != nil {
		// Unlesbarer Kopf: Nachricht trotzdem spiegeln (Blob, Größe, Datum).
		e.log().Info("message header unreadable; mirrored without parsed fields", "uid", h.UID)
		return nm, nil
	}
	fillParsed(&nm, m)
	return nm, nil
}

func fillParsed(nm *store.NewMessage, m *mimeparse.Message) {
	if m.MessageID != "" {
		nm.MessageID = m.MessageID
	}
	nm.Subject = m.Subject
	if len(m.From) > 0 {
		nm.From = formatAddr(m.From[0])
	}
	nm.To = addrJSON(m.To)
	nm.Cc = addrJSON(m.Cc)
	if len(m.ReplyTo) > 0 {
		nm.ReplyTo = formatAddr(m.ReplyTo[0])
	}
	if m.DateSource == mimeparse.DateFromHeader {
		nm.DateHeader = m.Date
	}
	nm.HasAttachments = m.HasAttachments()
	nm.Snippet = snippet(m.Text)
	if len(m.InReplyTo) > 0 {
		nm.InReplyTo = m.InReplyTo[0]
	}
	nm.References = m.References
	nm.ListID = m.ListID
	nm.AuthResults = strings.Join(m.AuthResults, "\n")
	nm.TextPlain = m.Text
	nm.TextCharset = m.TextCharset
}

func formatAddr(a mimeparse.Address) string {
	if a.Name == "" {
		return a.Address
	}
	return a.Name + " <" + a.Address + ">"
}

func addrJSON(as []mimeparse.Address) json.RawMessage {
	if len(as) == 0 {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(as)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// snippet: die ersten Zeichen des Textes, Leerraum zusammengefasst.
func snippet(text string) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range text {
		if n >= snippetLen {
			break
		}
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			n++
			space = false
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
