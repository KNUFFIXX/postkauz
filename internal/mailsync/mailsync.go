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
	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
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
	FindByContentHash(ctx context.Context, accountID, mailboxID int64, hashes ...string) (int64, bool, error)
	ApplyBatch(ctx context.Context, b store.MirrorBatch) (store.BatchResult, error)
	ThreadUnassigned(ctx context.Context, accountID int64, limit int) (int, error)
	ReprocessCandidates(ctx context.Context, accountID int64, version, limit int) ([]store.ReprocessItem, error)
	SaveReprocessed(ctx context.Context, items []store.Reprocessed) error
	// Übersprungene Nachrichten (zu groß; Migration 0009).
	SkippedUIDs(ctx context.Context, mailboxID int64, uidValidity uint32) ([]uint32, error)
	MarkSkipped(ctx context.Context, mailboxID int64, uidValidity, uid uint32, size int64, reason string) error
	PruneSkipped(ctx context.Context, mailboxID int64, uidValidity uint32, present []uint32) (int, error)
	ClearSkipped(ctx context.Context, mailboxID int64) error
}

// JobRunner führt die Jobs eines Kontos aus (internal/jobs): Extraktion
// und Volltextindex.
type JobRunner interface {
	RunAccount(ctx context.Context, accountID int64) (jobs.Report, error)
}

// Standardwerte.
const (
	DefaultWindow = 200
	// maxBatchBytes teilt ein Fenster mit großen Nachrichten in mehrere
	// Transaktionen (der Cursor wandert erst mit der letzten).
	maxBatchBytes = 32 << 20
	// maxFetchBytes begrenzt die angekündigte Größe der Rohmails je FETCH
	// (eine größere Nachricht kommt allein). Bei 10 min Obergrenze je
	// Kommando reichen so rund 28 KB/s.
	maxFetchBytes = 16 << 20
	snippetLen    = 200
)

// Options steuert einen Sync-Lauf.
type Options struct {
	// Window ist die Zahl neuer Nachrichten je Fenster (0 = 200).
	Window int
	// Full erzwingt Lösch- und Flag-Abgleich aller Nachrichten (ohne
	// CHANGEDSINCE) und holt Fehlendes nach (`sync --full`).
	Full bool
	// CheckDeleted erzwingt den Löschabgleich (vollständige UID-Liste)
	// aller Ordner, ohne den vollen Flag-Abgleich von Full. Der Dauerbetrieb
	// setzt es alle WatchOptions.FullCheckInterval (A02 A1); der einmalige
	// Sync nicht.
	CheckDeleted bool
	// Mailboxes beschränkt den Lauf auf diese Pfade (leer = alle).
	Mailboxes []string
	// RetryDrafts: Entwürfe, deren Ablage der Server abgelehnt hat oder die
	// sich nicht bauen ließen (push_rejected, push_failed), in diesem Lauf
	// erneut ablegen (Gegenprüfung S2.4/S2.5, W3). Der einmalige Sync setzt
	// es immer, der Dauerbetrieb nur im ersten Lauf und bei jedem vollen
	// Abgleich (FullCheckInterval, stündlich): Ein zu großer Entwurf soll
	// nicht alle 5 Minuten oder bei jeder neuen Mail erneut hochgeladen
	// werden.
	RetryDrafts bool
}

// Event ist eine Fortschrittsmeldung.
type Event struct {
	Kind    string // mailbox_start | window | mailbox_done | error | backlog | extract | index | drafts
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
	// Failed: Der Ordner wurde wegen eines Fehlers übersprungen (nicht
	// einzelne Nachrichten); ein voller Abgleich gilt dann als nicht
	// erledigt (Watch).
	Failed bool `json:"-"`
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
	Mirrored int `json:"messages_mirrored"`
	// Extracted zählt Anhänge, deren Text in diesem Lauf extrahiert wurde
	// (oder deren Extraktion endgültig scheiterte).
	Extracted int `json:"attachments_extracted"`
	// Indexed zählt die in diesem Lauf indizierten Nachrichten,
	// IndexPending die danach noch eingereihten (S1.7).
	Indexed      int           `json:"messages_indexed"`
	IndexPending int           `json:"index_pending"`
	Duration     time.Duration `json:"-"`
	// Drafts: Ablage wartender Entwürfe zu Beginn des Laufs (Job
	// draft_push, S2.5); nil = nichts wartete.
	Drafts *ops.SyncDrafts `json:"drafts,omitempty"`
	// DraftCheck: Prüfung der Entwürfe nach dem Spiegeln (S2.6,
	// drafts.go); nil = nichts geändert.
	DraftCheck *drafts.CheckReport `json:"draft_check,omitempty"`
}

// failedMailboxes zählt die Ordner, die wegen eines Fehlers übersprungen
// wurden.
func (r Report) failedMailboxes() int {
	n := 0
	for _, m := range r.Mailboxes {
		if m.Failed {
			n++
		}
	}
	return n
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
	// Jobs führt am Ende jedes Laufs die Warteschlange des Kontos aus
	// (Textextraktion mit Zeitbudget, Volltextindex); nil = keine Jobs
	// (Anhänge bleiben pending, Nachrichten eingereiht).
	Jobs JobRunner
	// MaxMessageBytes ist die größte Rohmail, die der Sync anfordert
	// ([imap] max_message_bytes; 0 = 256 MiB wie der Blob-Store). Größer
	// laut Server (RFC822.SIZE aus den Köpfen) = Fehler dieser Nachricht,
	// ohne sie zu laden; sie wird gemerkt und erst mit `sync --full` neu
	// geprüft. Die Lese-Grenze im IMAP-Abruf bleibt die zweite Sicherung.
	MaxMessageBytes int64
	// Drafts legt zu Beginn jedes Laufs wartende Entwürfe im
	// Entwürfe-Ordner ab (Job draft_push, S2.5; drafts.go); nil = nicht.
	Drafts *Drafts
}

func (e *Engine) maxMessageBytes() int64 {
	if e.MaxMessageBytes > 0 {
		return e.MaxMessageBytes
	}
	return blob.DefaultMaxSize
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

// SyncAll legt wartende Entwürfe im Entwürfe-Ordner ab (Engine.Drafts),
// gleicht die Ordnerliste ab und synct alle (bzw. die gewählten) Ordner,
// INBOX zuerst. Fehler eines Ordners zählen als Fehler und der Lauf
// geht weiter; Verbindungs- und Anmeldefehler sowie ctx-Ende brechen ab.
func (e *Engine) SyncAll(ctx context.Context, o Options) (Report, error) {
	start := time.Now()
	var rep Report
	// Zuerst die Entwürfe (A02 E1): Der Spiegel sieht ihre Kopien dann in
	// diesem Lauf. Ein Push-Fehler hält den Lauf nur bei Verbindungs- und
	// Anmeldefehlern auf (die Entwürfe bleiben pending, der nächste Lauf
	// holt sie nach).
	dr, err := e.pushDrafts(ctx, true, o.RetryDrafts)
	rep.Drafts = dr
	if err != nil {
		if fatal(ctx, err) {
			rep.Duration = time.Since(start)
			return rep, fmt.Errorf("drafts: %w", err)
		}
		e.log().Warn("drafts not pushed", "error", err.Error())
	}
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
	// K3: Was dieser Lauf (fehlerfrei) gespiegelt hat, bestimmt, was die
	// Prüfung der Entwürfe danach darf (drafts.CheckOptions).
	var check drafts.CheckOptions
	for _, mb := range local {
		isDrafts := mb.Role == string(provider.RoleDrafts) || (e.Drafts != nil && e.Drafts.Mailbox != "" && mb.Path == e.Drafts.Mailbox)
		isSent := mb.Role == string(provider.RoleSent) || mb.Role == string(provider.RoleAll)
		if len(o.Mailboxes) > 0 && !slices.Contains(o.Mailboxes, mb.Path) {
			check.SkipSent = check.SkipSent || isDrafts
			check.KeepMark = check.KeepMark || isSent
			continue
		}
		// crash.Do (A02 A5): Ein Panic in einem Fenster des Ordners (Abruf,
		// Parser, Aufbereitung) wird bereinigt gemeldet und beendet den Lauf
		// mit Fehler (fatal), statt als Ordnerfehler weiterzulaufen.
		var mr MailboxReport
		err := crash.Do("sync mailbox", func() error {
			var err error
			mr, err = e.SyncMailbox(ctx, mb, o)
			return err
		})
		if mr.Path == "" {
			mr = MailboxReport{Path: mb.Path, Role: mb.Role, Mirrored: mb.Mirrored, Total: mb.Total}
		}
		if err != nil && !fatal(ctx, err) {
			// Ordner überspringen, Lauf fortsetzen; der nächste Lauf
			// setzt am gespeicherten Stand wieder an.
			mr.Errors++
			mr.Failed = true
			mr.problem(err.Error())
			check.SkipSent = check.SkipSent || isDrafts
			check.KeepMark = check.KeepMark || isSent
			e.emit(Event{Kind: "error", Mailbox: mb.Path, Message: err.Error()})
		}
		rep.add(mr)
		if err != nil && fatal(ctx, err) {
			rep.Duration = time.Since(start)
			return rep, err
		}
	}
	// S2.6: Entwürfe gegen den eben gespiegelten Stand prüfen (gesendet?
	// Freigabe abgelaufen? Kopie im Entwürfe-Ordner noch da?). Ein Fehler
	// der Prüfung zählt als Fehler des Laufs, hält aber Nacharbeit, Jobs und
	// Zählung nicht auf (Gegenprüfung S2.6–S2.8, K5); die Prüfung ist beim
	// nächsten Lauf wiederholbar. Nur Abbruch, Verbindungs-/Anmeldefehler
	// und ein Panic beenden den Lauf.
	if check.SkipSent && e.Drafts != nil {
		e.log().Debug("sent detection skipped: the Drafts folder was not mirrored in this run")
	}
	if rep.DraftCheck, err = e.checkDrafts(ctx, check); err != nil {
		if fatal(ctx, err) {
			rep.Duration = time.Since(start)
			return rep, err
		}
		rep.Errors++
		e.log().Warn("draft check failed; repeating it in the next run", "error", err.Error())
		e.emit(Event{Kind: "error", Mailbox: "drafts", Message: err.Error()})
	}
	if err := e.backlog(ctx); err != nil {
		rep.Duration = time.Since(start)
		return rep, err
	}
	if e.Jobs != nil {
		jr, err := e.Jobs.RunAccount(ctx, e.AccountID)
		rep.Extracted, rep.Indexed, rep.IndexPending = jr.Extracted, jr.Indexed, jr.IndexPending
		if err != nil {
			rep.Duration = time.Since(start)
			return rep, fmt.Errorf("jobs: %w", err)
		}
	}
	if rep.Mirrored, err = e.Store.CountPresentMessages(ctx, e.AccountID); err != nil {
		return rep, err
	}
	rep.Duration = time.Since(start)
	return rep, nil
}

// backlog zieht den Bestand nach, der vor S1.6 gespiegelt wurde: Threads
// für Nachrichten ohne Zuordnung und die Aufbereitung (Text, Links,
// versteckter Text, Anhänge) aus der Rohmail. Neue Nachrichten bekommen
// beides schon beim Speichern; hier bleibt dann nichts zu tun.
func (e *Engine) backlog(ctx context.Context) error {
	total := 0
	for {
		n, err := e.Store.ThreadUnassigned(ctx, e.AccountID, 500)
		if err != nil {
			return fmt.Errorf("threads: %w", err)
		}
		total += n
		if n == 0 {
			break
		}
		e.emit(Event{Kind: "backlog", Done: total, Message: "threads"})
	}
	if total > 0 {
		e.log().Info("threaded messages mirrored before threading existed", "count", total)
	}
	n, err := e.reprocess(ctx)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	if n > 0 {
		e.log().Info("cleaned up messages mirrored with an older version", "count", n)
	}
	return nil
}

// fatal: Fehler, nach denen weitere Ordner keinen Sinn haben.
func fatal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, provider.ErrAuth) || errors.Is(err, provider.ErrConnection) ||
		errors.Is(err, ErrAborted) || errors.Is(err, context.Canceled) || errors.Is(err, crash.ErrInternal)
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
	if o.Full {
		// sync --full prüft übersprungene Nachrichten erneut (etwa nach
		// einer höheren Grenze); noch zu große kommen wieder auf die Liste.
		if err := e.Store.ClearSkipped(ctx, mb.ID); err != nil {
			return rep, err
		}
	}
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
		// Übersprungene Nachrichten führt der Server, lokal fehlen sie: Sie
		// zählen als bekannt, sonst löste jede einen UID-Abgleich aus.
		skipped, err := e.Store.SkippedUIDs(ctx, mb.ID, state.UIDValidity)
		if err != nil {
			return rep, err
		}
		known += len(skipped)
		// FullScan und CheckDeleted nur beim ersten Aufruf je Ordner: Er holt
		// die vollständige UID-Liste (FullScan prüft dazu alle Flags);
		// folgende Fenster brauchen das nicht.
		cs, err := e.Remote.Changes(ctx, mb.Path, state, provider.ChangesOptions{Limit: window, KnownCount: known,
			FullScan: o.Full && first, CheckDeleted: o.CheckDeleted && first})
		first = false
		if err != nil {
			return rep, fmt.Errorf("%s: %w", mb.Path, err)
		}
		if cs.Reset {
			rep.Reset = true
			e.log().Warn("UIDVALIDITY changed; re-matching mailbox by Message-ID", "mailbox", mb.Path)
			state = provider.SyncState{}
		}
		err = e.applyChanges(ctx, mb, state, cs, o.Full, &rep)
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
func (e *Engine) applyChanges(ctx context.Context, mb store.Mailbox, since provider.SyncState, cs provider.ChangeSet, full bool, rep *MailboxReport) error {
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
		present := make([]uint32, len(cs.Present))
		for i, u := range cs.Present {
			present[i] = uint32(u)
		}
		if n, err := e.Store.PruneSkipped(ctx, mb.ID, uv, present); err != nil {
			return err
		} else if n > 0 {
			e.log().Info("skipped messages no longer on the server", "mailbox", mb.Path, "count", n)
		}
		if !full && len(missing) > 0 {
			// Gemerkte zu große Nachrichten nicht erneut anfordern.
			skipped, err := e.Store.SkippedUIDs(ctx, mb.ID, uv)
			if err != nil {
				return err
			}
			missing = slices.DeleteFunc(missing, func(u provider.UID) bool {
				_, found := slices.BinarySearch(skipped, uint32(u))
				return found
			})
		}
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
		headers, err := e.fetchHeaders(ctx, mb.Path, uv, chunk, rep)
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
		need, err = e.skipTooLarge(ctx, mb, uv, need, rep)
		if err != nil {
			return err
		}
		if len(need) == 0 {
			continue
		}
		byUID := map[provider.UID]provider.Header{}
		for _, h := range need {
			byUID[h.UID] = h
		}
		got := map[provider.UID]bool{}
		mirror := func(uid provider.UID, r io.Reader) error {
			h, ok := byUID[uid]
			if !ok || got[uid] {
				_, _ = io.Copy(io.Discard, r)
				return nil
			}
			nm, variants, err := e.mirrorRaw(r, h)
			if err != nil {
				rep.Errors++
				rep.problem(fmt.Sprintf("uid %d: %v", uid, err))
				e.log().Warn("message not mirrored", "mailbox", mb.Path, "uid", uid, "error", err.Error())
				got[uid] = true
				return nil
			}
			got[uid] = true
			if id, ok, err := e.Store.FindByContentHash(ctx, e.AccountID, mb.ID, variants...); err != nil {
				return err
			} else if ok && !linked(pending, id) {
				pending.Links = append(pending.Links, store.Link{MessageID: id, Flags: nm.Link})
				return nil
			}
			pending.New = append(pending.New, nm)
			pendingBytes += nm.Size
			return nil
		}
		// Rohmails in Gruppen nach angekündigter Größe holen: Ein FETCH
		// bleibt so klein genug für die Obergrenze je Kommando des
		// Watchdogs (ADR-007, Nachtrag 16.10.2026), auch über langsame
		// Leitungen.
		for _, group := range sizeGroups(need, maxFetchBytes) {
			if err := e.fetchRaw(ctx, mb.Path, uv, group, got, rep, mirror); err != nil {
				return err
			}
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

// skipTooLarge sortiert Nachrichten aus, die laut Server (RFC822.SIZE)
// größer als MaxMessageBytes sind, bevor ihr Inhalt angefordert wird
// (sonst überträgt der Server sie ganz, und der Abruf verwirft sie). Jede
// zählt als Fehler dieser Nachricht und wird gemerkt, damit folgende Läufe
// sie nicht erneut anfordern; `sync --full` prüft sie wieder.
func (e *Engine) skipTooLarge(ctx context.Context, mb store.Mailbox, uv uint32, hs []provider.Header, rep *MailboxReport) ([]provider.Header, error) {
	limit := e.maxMessageBytes()
	out := hs[:0:0]
	for _, h := range hs {
		if h.Size <= limit {
			out = append(out, h)
			continue
		}
		if err := e.Store.MarkSkipped(ctx, mb.ID, uv, uint32(h.UID), h.Size, store.SkipTooLarge); err != nil {
			return nil, err
		}
		rep.Errors++
		rep.problem(fmt.Sprintf("uid %d: message of %d bytes is larger than imap.max_message_bytes (%d); not fetched, sync --full checks it again", h.UID, h.Size, limit))
		e.log().Warn("message larger than max_message_bytes; not fetched", "mailbox", mb.Path, "uid", h.UID, "size", h.Size, "limit", limit)
	}
	return out, nil
}

// fetchHeaders holt Köpfe; schickt der Server eine unlesbare Antwort
// (ErrProtocol), wird das Fenster halbiert, bis die betroffene Nachricht
// allein steht. Sie zählt dann als Fehler und wird übersprungen (der
// nächste Lauf versucht sie erneut), der Rest wird gespiegelt.
func (e *Engine) fetchHeaders(ctx context.Context, path string, uv uint32, uids []provider.UID, rep *MailboxReport) ([]provider.Header, error) {
	h, err := e.Remote.FetchHeaders(ctx, path, uv, uids)
	if err == nil || !errors.Is(err, provider.ErrProtocol) {
		return h, err
	}
	if len(uids) == 1 {
		rep.Errors++
		rep.problem(fmt.Sprintf("uid %d: %v", uids[0], err))
		e.log().Warn("unreadable server response; message skipped", "mailbox", path, "uid", uids[0])
		return nil, nil
	}
	mid := len(uids) / 2
	a, err := e.fetchHeaders(ctx, path, uv, uids[:mid], rep)
	if err != nil {
		return nil, err
	}
	b, err := e.fetchHeaders(ctx, path, uv, uids[mid:], rep)
	return append(a, b...), err
}

// fetchRaw wie fetchHeaders für die Rohmails; got verhindert, dass eine
// Nachricht beim Halbieren zweimal verarbeitet wird.
func (e *Engine) fetchRaw(ctx context.Context, path string, uv uint32, uids []provider.UID, got map[provider.UID]bool,
	rep *MailboxReport, fn func(provider.UID, io.Reader) error) error {
	var todo []provider.UID
	for _, u := range uids {
		if !got[u] {
			todo = append(todo, u)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	err := e.Remote.FetchRaw(ctx, path, uv, todo, fn)
	if err == nil || !errors.Is(err, provider.ErrProtocol) {
		return err
	}
	if len(todo) == 1 {
		got[todo[0]] = true
		rep.Errors++
		rep.problem(fmt.Sprintf("uid %d: %v", todo[0], err))
		e.log().Warn("unreadable server response; message skipped", "mailbox", path, "uid", todo[0])
		return nil
	}
	mid := len(todo) / 2
	if err := e.fetchRaw(ctx, path, uv, todo[:mid], got, rep, fn); err != nil {
		return err
	}
	return e.fetchRaw(ctx, path, uv, todo[mid:], got, rep, fn)
}

// sizeGroups teilt Köpfe in Gruppen, deren angekündigte Größe zusammen
// höchstens max ist (eine größere Nachricht bildet eine eigene Gruppe).
func sizeGroups(hs []provider.Header, max int64) [][]provider.UID {
	var out [][]provider.UID
	var cur []provider.UID
	var sum int64
	for _, h := range hs {
		if len(cur) > 0 && sum+h.Size > max {
			out = append(out, cur)
			cur, sum = nil, 0
		}
		cur = append(cur, h.UID)
		sum += h.Size
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
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
// gespeicherten Datei (der Inhalt liegt so nie ganz im Speicher). variants
// sind content_hash und seine Varianten am Zeilenende (ContentHasher) für
// den Abgleich, z. B. mit importierten Nachrichten.
func (e *Engine) mirrorRaw(r io.Reader, h provider.Header) (nm store.NewMessage, variants []string, err error) {
	info, err := e.Blobs.Put(r)
	if err != nil {
		return store.NewMessage{}, nil, err
	}
	if info.Size == 0 && h.Size > 0 {
		// Server meldet eine Größe, liefert aber nichts (Serverfehler, z. B.
		// der go-imap-Testserver bei kaputten Kopfzeilen): nicht spiegeln,
		// der nächste Lauf versucht es erneut.
		return store.NewMessage{}, nil, fmt.Errorf("server returned an empty body for a message of %d bytes", h.Size)
	}
	nm = store.NewMessage{Link: linkFlags(h.UID, h.ModSeq, h.Flags), BlobSHA256: info.SHA256, BlobSize: info.Size,
		BlobCompressed: info.Compressed, Size: info.Size, InternalDate: h.InternalDate.UTC(), MessageID: h.MessageID}
	if info.Existed {
		// Vorhandene Datei: Form (komprimiert oder nicht) von dort übernehmen.
		if _, compressed, err := e.Blobs.Path(info.SHA256); err == nil {
			nm.BlobCompressed = compressed
		}
	}
	rc, err := e.Blobs.Get(info.SHA256)
	if err != nil {
		return nm, nil, err
	}
	defer rc.Close()
	hr := NewContentHasher()
	tee := io.TeeReader(rc, hr)
	m, atts, perr := e.analyze(tee, h.InternalDate)
	// Parse hört bei unlesbarem Kopf oder an seinen Grenzen vorzeitig auf;
	// content_hash muss aber die ganze Rohmail abdecken, sonst bekommen zwei
	// verschiedene Mails mit gleichem Anfang denselben Hash.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return nm, nil, fmt.Errorf("hash message: %w", err)
	}
	nm.ContentHash, variants = hr.Sums()
	nm.CleanVersion = CleanVersion
	nm.Links = json.RawMessage("[]")
	if perr != nil {
		// Unlesbarer Kopf: Nachricht trotzdem spiegeln (Blob, Größe, Datum).
		e.log().Info("message header unreadable; mirrored without parsed fields", "uid", h.UID)
		return nm, variants, nil
	}
	fillParsed(&nm, m)
	fillClean(&nm, m, atts)
	return nm, variants, nil
}

func fillParsed(nm *store.NewMessage, m *mimeparse.Message) {
	if m.MessageID != "" {
		nm.MessageID = m.MessageID
	}
	// Kopffelder ohne unsichtbare Zeichen (sanitize.Invisible): Sie stehen
	// in Suche, Listen und MCP-Ausgaben und dürfen dort nichts verbergen.
	nm.Subject = sanitize.Text(m.Subject)
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
	a = cleanAddr(a)
	if a.Name == "" {
		return a.Address
	}
	return a.Name + " <" + a.Address + ">"
}

// cleanAddr entfernt unsichtbare Zeichen aus Name und Adresse.
func cleanAddr(a mimeparse.Address) mimeparse.Address {
	return mimeparse.Address{Name: sanitize.Text(a.Name), Address: sanitize.Text(a.Address)}
}

func addrJSON(as []mimeparse.Address) json.RawMessage {
	if len(as) == 0 {
		return json.RawMessage("[]")
	}
	clean := make([]mimeparse.Address, len(as))
	for i, a := range as {
		clean[i] = cleanAddr(a)
	}
	b, err := json.Marshal(clean)
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
