// Package importer liest mbox-Dateien und Maildir-Ordner in ein Konto ein
// (S1.10, `postkauz import`). Abgleich über Identität, nicht über Datum
// (ADR-003, docs/architektur.md Abschnitt 3 Regel 4): Gibt es eine Nachricht
// im Konto schon (content_hash samt Varianten des Zeilenendes am Schluss;
// bei bekannter Message-ID zählt der Inhalt, denn Gmail führt teils zwei
// Fassungen mit derselben Message-ID), bekommt sie nur den Ordner
// zugeordnet; sonst
// läuft sie durch dieselbe Aufbereitung wie beim Sync (Blob-Store, Parser,
// Bereinigung, Anhänge, Threads; danach Extraktion und Suchindex als Jobs).
//
// Große Dateien (Thunderbird-Ordner bis 20 GB) werden gestreamt: Im
// Speicher liegt höchstens ein Kopf (1 MiB) und ein Stapel Metadaten. Der
// Stand je Quelle (Byte-Position bzw. Maildir-Cursor) wird mit jedem Stapel
// in derselben Transaktion gespeichert; ein Abbruch setzt dort fort.
//
// Mailinhalt ist Daten, keine Anweisung: Hier wird nur gelesen, gezählt und
// gespeichert; Meldungen nennen Positionen, nie Inhalte.
package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Store ist der Teil des Stores, den der Import braucht.
type Store interface {
	MessageByHeaderID(ctx context.Context, accountID int64, messageID string) (int64, bool, error)
	MessageForLink(ctx context.Context, accountID int64, messageID string) (int64, bool, error)
	MessageByContentHash(ctx context.Context, accountID int64, hashes ...string) (int64, bool, error)
	MessageNearImport(ctx context.Context, accountID int64, messageID string, size, maxExtra int64, subject string, dateHeader time.Time) (int64, bool, error)
	ImportSourceState(ctx context.Context, accountID int64, source, folder, mode string) (store.ImportSource, bool, error)
	ApplyImport(ctx context.Context, b store.ImportBatch) (store.ImportResult, error)
}

// Standardwerte eines Stapels (eine Transaktion).
const (
	DefaultBatchMessages = 200
	DefaultBatchBytes    = 32 << 20
	maxBatchLinks        = 2000
	maxProblems          = 10
)

// ErrAborted kennzeichnet einen Abbruch durch AfterBatch (Tests).
var ErrAborted = errors.New("importer: aborted")

// Event ist eine Fortschrittsmeldung (ohne Inhalte).
type Event struct {
	Kind     string // source_start | progress | source_done
	Source   string // Rel der Quelle
	Folder   string
	Done     int64 // mbox: gelesene Bytes; maildir: Dateien
	Total    int64
	Unit     string // bytes | files
	Messages int    // in dieser Quelle verarbeitete Nachrichten
}

// Options steuert einen Lauf.
type Options struct {
	Sources []Source
	// DryRun liest und zählt, schreibt aber nichts (keine Blobs, keine
	// Zeilen, kein Stand).
	DryRun bool
	// Rescan liest auch Quellen, die seit dem letzten Lauf unverändert
	// sind, und beginnt jede Quelle am Anfang.
	Rescan bool
}

// SourceReport ist das Ergebnis einer Quelle.
type SourceReport struct {
	Path      string   `json:"path"`
	Folder    string   `json:"folder"`
	Mode      string   `json:"mode"`
	Variant   string   `json:"variant,omitempty"` // thunderbird | mboxrd | maildir
	New       int      `json:"new"`
	Existing  int      `json:"existing"`
	Skipped   int      `json:"skipped"`
	Errors    int      `json:"errors"`
	Linked    int      `json:"linked"`
	Bytes     int64    `json:"bytes"`
	ResumedAt int64    `json:"resumed_at,omitempty"`
	Unchanged bool     `json:"unchanged,omitempty"`
	Problems  []string `json:"problems,omitempty"`
}

func (r *SourceReport) problem(format string, args ...any) {
	if len(r.Problems) < maxProblems {
		r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
	}
}

// Report ist das Ergebnis eines Laufs.
type Report struct {
	Sources   []SourceReport
	New       int
	Existing  int
	Skipped   int
	Errors    int
	Linked    int
	Unchanged int
	Bytes     int64
	Duration  time.Duration
}

// Engine importiert in ein Konto.
type Engine struct {
	Store     Store
	Blobs     *blob.Store
	AccountID int64
	Log       *slog.Logger
	Progress  func(Event)
	// AfterBatch wird nach jedem gespeicherten Stapel aufgerufen; ein
	// Fehler bricht hart ab (Tests simulieren damit einen Absturz).
	AfterBatch    func(src Source, position int64) error
	BatchMessages int   // 0 = DefaultBatchMessages
	BatchBytes    int64 // 0 = DefaultBatchBytes
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

// run ist der Zustand eines Laufs.
type run struct {
	e   *Engine
	o   Options
	ctx context.Context

	batch      store.ImportBatch
	batchBytes int64
	// Noch nicht gespeicherte neue Nachrichten (Index in batch.New) nach
	// Message-ID und Hash; im Trockenlauf alles Gesehene (Index -1).
	pendingMID  map[string]int
	pendingHash map[string]int
}

// Run importiert die Quellen der Reihe nach.
func (e *Engine) Run(ctx context.Context, o Options) (Report, error) {
	start := time.Now()
	r := &run{e: e, o: o, ctx: ctx, pendingMID: map[string]int{}, pendingHash: map[string]int{}}
	r.batch.AccountID = e.AccountID
	var rep Report
	for _, src := range o.Sources {
		sr := SourceReport{Path: src.Rel, Folder: src.Folder, Mode: src.Mode}
		// crash.Do (A02 A5): Ein Panic beim Lesen einer Quelle oder in einem
		// ihrer Stapel wird bereinigt gemeldet und beendet den Import mit
		// Fehler (fatal); gespeicherte Stapel und der Stand der Quelle bleiben.
		err := crash.Do("import source", func() error {
			switch src.Format {
			case FormatMbox:
				return r.mbox(src, &sr)
			case FormatMaildir:
				return r.maildir(src, &sr)
			}
			return fmt.Errorf("unknown format %q", src.Format)
		})
		if err != nil && fatal(ctx, err) {
			rep.add(sr)
			rep.Duration = time.Since(start)
			return rep, err
		}
		if err != nil {
			sr.Errors++
			sr.problem("%v", err)
			e.log().Warn("import source failed", "source", src.Rel, "error", err.Error())
		}
		rep.add(sr)
		e.emit(Event{Kind: "source_done", Source: src.Rel, Folder: src.Folder, Messages: sr.New + sr.Existing + sr.Skipped + sr.Errors})
	}
	rep.Duration = time.Since(start)
	return rep, nil
}

func (r *Report) add(s SourceReport) {
	r.Sources = append(r.Sources, s)
	r.New += s.New
	r.Existing += s.Existing
	r.Skipped += s.Skipped
	r.Errors += s.Errors
	r.Linked += s.Linked
	r.Bytes += s.Bytes
	if s.Unchanged {
		r.Unchanged++
	}
}

// storeError markiert Fehler der Datenbank (Lauf bricht ab).
type storeError struct{ err error }

func (s storeError) Error() string { return s.err.Error() }
func (s storeError) Unwrap() error { return s.err }

func fatal(ctx context.Context, err error) bool {
	var se storeError
	return ctx.Err() != nil || errors.Is(err, ErrAborted) || errors.Is(err, context.Canceled) || errors.As(err, &se) ||
		errors.Is(err, crash.ErrInternal)
}

// item ist eine gelesene Nachricht vor dem Abgleich.
type item struct {
	pos     string // Position für Meldungen ("offset 123", "cur/…")
	header  []byte
	tooLong bool
	body    io.Reader
	drain   func() error
	date    time.Time
	flags   store.ImportFolderFlags
	skip    bool
	partial bool
	// reopen liest dieselbe Nachricht noch einmal von vorn (Kopf ohne
	// Thunderbird-Zeilen, Rumpf), über dasselbe Datei-Handle (ReadAt): Ersetzt
	// Thunderbird die Datei inzwischen (Kompaktieren), liest es unter Linux
	// und macOS weiter die alte. Unter Windows öffnet os.Open ohne
	// FILE_SHARE_DELETE: Thunderbird kann die Datei während des Imports
	// nicht ersetzen und kompaktiert später. Gebraucht, wenn eine Nachricht mit bekannter Message-ID erst
	// gehasht wurde und sich als andere Fassung herausstellt.
	reopen func() (header []byte, body io.Reader, drain func() error, err error)
	// expect: content_hash des ersten Lesens; das zweite muss ihn ergeben,
	// sonst wird nichts gespeichert (Datei an Ort und Stelle geändert).
	expect string
}

// handle gleicht eine Nachricht ab und legt sie bei Bedarf an.
func (r *run) handle(src Source, it item, rep *SourceReport) error {
	defer func() { _ = it.drain() }()
	if it.skip {
		rep.Skipped++
		return nil
	}
	var hf headerInfo
	if !it.tooLong {
		hf = readHeaderInfo(it.header)
	}
	mid := hf.MessageID
	if mid != "" {
		known, err := r.knownMID(mid)
		if err != nil {
			return err
		}
		if known {
			if done, err := r.knownVersion(src, &it, hf, rep); err != nil || done {
				return err
			}
		}
	}
	h := mailsync.NewContentHasher()
	content := io.MultiReader(bytes.NewReader(it.header), it.body)
	var info blob.Info
	if src.Mode == store.ImportLabels || r.o.DryRun {
		n, err := io.Copy(h, content)
		if err != nil {
			return err
		}
		info.Size = n
	} else {
		var err error
		info, err = r.e.Blobs.PutContext(r.ctx, io.TeeReader(content, h))
		if err != nil {
			if errors.Is(err, blob.ErrTooLarge) {
				rep.Errors++
				rep.problem("%s: %v", it.pos, err)
				return nil
			}
			return err
		}
	}
	if info.Size == 0 {
		rep.Skipped++
		rep.problem("%s: empty message", it.pos)
		return nil
	}
	exact, variants := h.Sums()
	if it.expect != "" && exact != it.expect {
		// Zweites Lesen ergab etwas anderes (Datei an Ort und Stelle
		// geändert): nichts speichern. Ein schon geschriebener Blob bleibt
		// ohne Verweis.
		rep.Errors++
		rep.problem("%s: message changed between two reads (file rewritten, e.g. compacted by Thunderbird); not stored; the source counts as done, so run the import again with --rescan", it.pos)
		return nil
	}
	if ok, err := r.existing(variants, it.flags, rep); err != nil || ok {
		return err
	}
	if src.Mode == store.ImportLabels {
		// Nur Zuordnung: Ohne vorhandene Nachricht gibt es nichts zuzuordnen.
		rep.Skipped++
		return nil
	}
	if it.partial {
		rep.problem("%s: only partly downloaded by Thunderbird; imported as is", it.pos)
	}
	rep.New++
	if r.o.DryRun {
		if mid != "" {
			r.pendingMID[mid] = -1
		}
		r.pendingHash[exact] = -1
		return nil
	}
	nm := store.NewMessage{BlobSHA256: info.SHA256, BlobSize: info.Size, BlobCompressed: info.Compressed, Size: info.Size,
		InternalDate: it.date.UTC(), MessageID: mid, ContentHash: exact,
		Link: store.LinkFlags{Seen: it.flags.Seen, Flagged: it.flags.Flagged, Answered: it.flags.Answered, Draft: it.flags.Draft,
			Keywords: it.flags.Keywords}}
	if it.date.IsZero() {
		nm.InternalDate = time.Time{}
	}
	if info.Existed {
		if _, compressed, err := r.e.Blobs.Path(info.SHA256); err == nil {
			nm.BlobCompressed = compressed
		}
	}
	rc, err := r.e.Blobs.Get(info.SHA256)
	if err != nil {
		return err
	}
	parsed, err := mailsync.Prepare(r.e.Blobs, &nm, rc, it.date)
	_ = rc.Close()
	if err != nil {
		// Ein Anhang war nur vorübergehend nicht abzulegen (Sperre belegt
		// oder Kontextende; Nachbesserung 2, Punkt 8): wie ein Fehler beim
		// Ablegen der Rohmail oben.
		return err
	}
	if !parsed {
		r.e.log().Info("message header unreadable; imported without parsed fields", "position", it.pos)
	}
	idx := len(r.batch.New)
	r.batch.New = append(r.batch.New, store.ImportNew{Msg: nm, Folders: []store.ImportFolderFlags{it.flags}})
	r.batchBytes += info.Size
	if nm.MessageID != "" {
		r.pendingMID[nm.MessageID] = idx
	}
	r.pendingHash[exact] = idx
	return nil
}

// knownMID meldet, ob es im Konto (oder im noch nicht gespeicherten
// Stapel) schon eine Nachricht mit dieser Message-ID gibt.
func (r *run) knownMID(mid string) (bool, error) {
	if _, ok := r.pendingMID[mid]; ok {
		return true, nil
	}
	_, ok, err := r.e.Store.MessageByHeaderID(r.ctx, r.e.AccountID, mid)
	if err != nil {
		return false, storeError{err}
	}
	return ok, nil
}

// linkByMID ordnet den Ordner der (ältesten) Nachricht mit dieser
// Message-ID zu, auch einer noch nicht gespeicherten des Stapels.
func (r *run) linkByMID(mid string, f store.ImportFolderFlags, rep *SourceReport) error {
	if idx, ok := r.pendingMID[mid]; ok {
		rep.Existing++
		if idx >= 0 {
			addFolder(&r.batch.New[idx], f)
		}
		return nil
	}
	id, ok, err := r.e.Store.MessageForLink(r.ctx, r.e.AccountID, mid)
	if err != nil {
		return storeError{err}
	}
	if !ok {
		return errors.New("message with this Message-ID vanished")
	}
	rep.Existing++
	if !r.o.DryRun {
		r.batch.Links = append(r.batch.Links, store.ImportLink{MessageID: id, ImportFolderFlags: f})
	}
	return nil
}

// knownVersion: Eine Nachricht mit dieser Message-ID gibt es schon. Gmail
// führt aber teils zwei verschiedene Nachrichten mit derselben Message-ID
// (z. B. die Fassung des Absenders und die empfangene); vorhanden ist die
// neue nur, wenn der Inhalt passt (content_hash samt Varianten am
// Zeilenende) oder sie die Thunderbird-Fassung einer schon gesyncten
// Nachricht ist (am Ende 1 bis MaxLinkSizeExtra Bytes länger, gleicher
// Betreff und Date; Restfall von C2). Dafür wird sie zuerst nur gehasht
// (nichts gespeichert). Ist sie eine andere Fassung, liest reopen sie noch
// einmal, und handle legt sie an (done false). done true: erledigt
// (vorhanden, übersprungen, im Trockenlauf gezählt).
func (r *run) knownVersion(src Source, it *item, hf headerInfo, rep *SourceReport) (done bool, err error) {
	h := mailsync.NewContentHasher()
	n, err := io.Copy(h, io.MultiReader(bytes.NewReader(it.header), it.body))
	if err != nil {
		return true, err
	}
	if n == 0 {
		rep.Skipped++
		rep.problem("%s: empty message", it.pos)
		return true, nil
	}
	exact, variants := h.Sums()
	if ok, err := r.existing(variants, it.flags, rep); err != nil || ok {
		return true, err
	}
	if !it.flags.Draft {
		id, ok, err := r.e.Store.MessageNearImport(r.ctx, r.e.AccountID, hf.MessageID, n, mailsync.MaxLinkSizeExtra, hf.Subject, hf.Date)
		if err != nil {
			return true, storeError{err}
		}
		if ok {
			rep.Existing++
			if !r.o.DryRun {
				r.batch.Links = append(r.batch.Links, store.ImportLink{MessageID: id, ImportFolderFlags: it.flags})
			}
			return true, nil
		}
	}
	if it.partial {
		// Thunderbird hat nur einen Teil geladen (Bit 0x0400): keine eigene,
		// abgeschnittene Nachricht, sondern die vorhandene bekommt den Ordner.
		rep.problem("%s: only partly downloaded by Thunderbird; linked to the stored message with the same Message-ID", it.pos)
		return true, r.linkByMID(hf.MessageID, it.flags, rep)
	}
	if src.Mode == store.ImportLabels {
		rep.Skipped++
		rep.problem("%s: same Message-ID as a stored message but different content; not stored (labels only)", it.pos)
		return true, nil
	}
	if r.o.DryRun {
		rep.New++
		r.pendingHash[exact] = -1
		return true, nil
	}
	if it.reopen == nil {
		return true, errors.New("message can not be read a second time")
	}
	_ = it.drain()
	hdr, body, drain, err := it.reopen()
	if err != nil {
		return true, err
	}
	it.header, it.body, it.drain, it.expect = hdr, body, drain, exact
	return false, nil
}

// existing sucht die Nachricht über ihre Hashes (Wert und Varianten) unter
// den noch nicht gespeicherten und in der Datenbank und ordnet sie dem
// Ordner zu.
func (r *run) existing(hashes []string, f store.ImportFolderFlags, rep *SourceReport) (bool, error) {
	for _, h := range hashes {
		if idx, ok := r.pendingHash[h]; ok {
			rep.Existing++
			if idx >= 0 {
				addFolder(&r.batch.New[idx], f)
			}
			return true, nil
		}
	}
	id, ok, err := r.e.Store.MessageByContentHash(r.ctx, r.e.AccountID, hashes...)
	if err != nil {
		return false, storeError{err}
	}
	if !ok {
		return false, nil
	}
	rep.Existing++
	if !r.o.DryRun {
		r.batch.Links = append(r.batch.Links, store.ImportLink{MessageID: id, ImportFolderFlags: f})
	}
	return true, nil
}

// addFolder ergänzt eine neue Nachricht des Stapels um einen Ordner (oder
// vereinigt die Flags, wenn der Ordner schon dabei ist).
func addFolder(n *store.ImportNew, f store.ImportFolderFlags) {
	for i, g := range n.Folders {
		if g.Folder == f.Folder {
			n.Folders[i].Seen = g.Seen || f.Seen
			n.Folders[i].Flagged = g.Flagged || f.Flagged
			n.Folders[i].Answered = g.Answered || f.Answered
			n.Folders[i].Draft = g.Draft || f.Draft
			return
		}
	}
	n.Folders = append(n.Folders, f)
	m := &n.Msg.Link
	m.Seen, m.Flagged, m.Answered, m.Draft = m.Seen || f.Seen, m.Flagged || f.Flagged, m.Answered || f.Answered, m.Draft || f.Draft
}

func (r *run) full() bool {
	maxMsgs, maxBytes := r.e.BatchMessages, r.e.BatchBytes
	if maxMsgs <= 0 {
		maxMsgs = DefaultBatchMessages
	}
	if maxBytes <= 0 {
		maxBytes = DefaultBatchBytes
	}
	return len(r.batch.New) >= maxMsgs || r.batchBytes >= maxBytes || len(r.batch.Links) >= maxBatchLinks
}

// flush speichert den Stapel samt Stand der Quelle (nicht im Trockenlauf).
func (r *run) flush(src Source, state store.ImportSource, rep *SourceReport) error {
	if r.o.DryRun {
		return nil
	}
	state.AccountID = r.e.AccountID
	r.batch.Source = &state
	res, err := r.e.Store.ApplyImport(r.ctx, r.batch)
	if err != nil {
		return storeError{err}
	}
	rep.Linked += res.Linked
	r.batch = store.ImportBatch{AccountID: r.e.AccountID}
	r.batchBytes = 0
	clear(r.pendingMID)
	clear(r.pendingHash)
	if r.e.AfterBatch != nil {
		if err := r.e.AfterBatch(src, state.Position); err != nil {
			return fmt.Errorf("%w: %v", ErrAborted, err)
		}
	}
	return nil
}

// headerInfo sind die Kopffelder für den Abgleich, gelesen wie beim
// Speichern (Message-ID ohne <>, bereinigter Betreff, Date nur aus der
// Kopfzeile; leer bei unlesbarem Kopf).
type headerInfo struct {
	MessageID string
	Subject   string
	Date      time.Time
}

func readHeaderInfo(hdr []byte) headerInfo {
	m, err := mimeparse.Parse(bytes.NewReader(hdr), mimeparse.Options{})
	if err != nil {
		return headerInfo{}
	}
	hi := headerInfo{MessageID: m.MessageID, Subject: sanitize.Text(m.Subject)}
	if m.DateSource == mimeparse.DateFromHeader {
		hi.Date = m.Date
	}
	return hi
}

// headerMessageID liest die Message-ID aus dem Kopf (wie der Parser beim
// Speichern; "" ohne oder bei unlesbarem Kopf).
func headerMessageID(hdr []byte) string { return readHeaderInfo(hdr).MessageID }

func mozFlags(folder string, st mozStatus) store.ImportFolderFlags {
	f := store.ImportFolderFlags{Folder: folder, Seen: st.Flags&mozRead != 0, Answered: st.Flags&mozReplied != 0,
		Flagged: st.Flags&mozMarked != 0}
	for _, k := range st.Keys {
		if k != "" {
			f.Keywords = append(f.Keywords, k)
		}
	}
	return f
}

// --- mbox ---------------------------------------------------------------------

func (r *run) mbox(src Source, rep *SourceReport) error {
	f, err := os.Open(src.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size, mtime := fi.Size(), fi.ModTime().UnixMilli()
	start := int64(0)
	st, ok, err := r.e.Store.ImportSourceState(r.ctx, r.e.AccountID, src.Path, src.Folder, src.Mode)
	if err != nil {
		return storeError{err}
	}
	if ok && !r.o.Rescan {
		// Fertig und gleich groß: unverändert. Thunderbird ändert beim Lesen
		// X-Mozilla-Status an Ort und Stelle (Änderungszeit neu, Größe
		// gleich); das ändert am Import nichts (Flags vorhandener Nachrichten
		// bleiben). --rescan liest trotzdem neu.
		if st.State == store.ImportDone && st.Size == size {
			rep.Unchanged = true
			return nil
		}
		// Fortsetzen nach Abbruch oder an eine gewachsene Datei anschließen;
		// passt die Position nicht mehr (Thunderbird hat kompaktiert), von vorn.
		if st.Position > 0 && st.Position < size && separatorAt(f, st.Position) {
			start = st.Position
			rep.ResumedAt = start
		} else if st.Position == size && st.State == store.ImportRunning {
			start = size
			rep.ResumedAt = start
		}
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	r.e.emit(Event{Kind: "source_start", Source: src.Rel, Folder: src.Folder, Done: start, Total: size, Unit: "bytes"})
	sc := newMboxScanner(f, start)
	state := store.ImportSource{Source: src.Path, Format: FormatMbox, Folder: src.Folder, Mode: src.Mode, State: store.ImportRunning,
		Size: size, MTime: mtime}
	n := 0
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		m, err := sc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, ErrNotMbox) && start == 0 {
				return fmt.Errorf("%s: %w", src.Rel, err)
			}
			return err
		}
		it := item{pos: fmt.Sprintf("offset %d", m.Offset), header: m.Header, tooLong: m.TooLong, body: m.Body,
			drain: m.Body.drain, date: m.Date, flags: mozFlags(src.Folder, m.Status), skip: m.Status.deleted(),
			partial: m.Status.Flags&mozPartial != 0, reopen: reopenMbox(f, m.Offset, sc.quote)}
		if err := r.handle(src, it, rep); err != nil {
			if fatal(r.ctx, err) {
				return err
			}
			return fmt.Errorf("%s: %w", it.pos, err)
		}
		n++
		rep.Bytes = sc.Pos() - start
		r.e.emit(Event{Kind: "progress", Source: src.Rel, Folder: src.Folder, Done: sc.Pos(), Total: size, Unit: "bytes", Messages: n})
		if r.full() {
			state.Position = sc.Pos()
			if err := r.flush(src, state, rep); err != nil {
				return err
			}
		}
	}
	rep.Variant = "mboxrd"
	if sc.Thunderbird {
		rep.Variant = "thunderbird"
	}
	rep.Bytes = sc.Pos() - start
	state.State, state.Position = store.ImportDone, sc.Pos()
	return r.flush(src, state, rep)
}

// --- maildir ------------------------------------------------------------------

func (r *run) maildir(src Source, rep *SourceReport) error {
	entries, tmp, stamp, err := listMaildir(src.Path)
	if err != nil {
		return err
	}
	rep.Variant = "maildir"
	rep.Skipped += tmp
	if tmp > 0 {
		rep.problem("%d file(s) in tmp/ (delivery in progress) skipped", tmp)
	}
	st, ok, err := r.e.Store.ImportSourceState(r.ctx, r.e.AccountID, src.Path, src.Folder, src.Mode)
	if err != nil {
		return storeError{err}
	}
	cursor := ""
	if ok && !r.o.Rescan {
		if st.State == store.ImportDone && st.Size == int64(len(entries)) && st.MTime == stamp {
			rep.Unchanged = true
			rep.Skipped -= tmp
			rep.Problems = nil
			return nil
		}
		if st.State == store.ImportRunning {
			cursor = st.Cursor
		}
	}
	total := int64(len(entries))
	r.e.emit(Event{Kind: "source_start", Source: src.Rel, Folder: src.Folder, Total: total, Unit: "files"})
	state := store.ImportSource{Source: src.Path, Format: FormatMaildir, Folder: src.Folder, Mode: src.Mode, State: store.ImportRunning,
		Size: total, MTime: stamp, Cursor: cursor}
	for i, ent := range entries {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if cursor != "" && ent.Key <= cursor {
			rep.ResumedAt++ // Maildir: Zahl der übersprungenen, schon gespeicherten Dateien
			continue
		}
		if err := r.maildirFile(src, ent, rep); err != nil {
			return err
		}
		rep.Bytes += ent.Size
		r.e.emit(Event{Kind: "progress", Source: src.Rel, Folder: src.Folder, Done: int64(i + 1), Total: total, Unit: "files", Messages: i + 1})
		state.Cursor = ent.Key
		if r.full() {
			if err := r.flush(src, state, rep); err != nil {
				return err
			}
		}
	}
	state.State = store.ImportDone
	return r.flush(src, state, rep)
}

func (r *run) maildirFile(src Source, ent maildirEntry, rep *SourceReport) error {
	f, err := os.Open(ent.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // inzwischen verschoben (anderer Client); der nächste Lauf sieht es
	}
	if err != nil {
		rep.Errors++
		rep.problem("%s: %v", ent.Key, err)
		return nil
	}
	defer f.Close()
	mr := newMsgReader(newSource(f, 0), false, quoteNone)
	hdr, tooLong, err := mr.readHeader()
	if err != nil {
		rep.Errors++
		rep.problem("%s: %v", ent.Key, err)
		return nil
	}
	hdr, moz := stripMozilla(hdr)
	fl := mozFlags(src.Folder, moz)
	fl.Seen = fl.Seen || ent.has('S')
	fl.Answered = fl.Answered || ent.has('R')
	fl.Flagged = fl.Flagged || ent.has('F')
	fl.Draft = ent.has('D')
	it := item{pos: ent.Key, header: hdr, tooLong: tooLong, body: mr, drain: mr.drain, date: ent.MTime, flags: fl,
		skip: ent.has('T') || moz.deleted(), reopen: reopenMaildir(f)}
	if err := r.handle(src, it, rep); err != nil {
		if fatal(r.ctx, err) {
			return err
		}
		rep.Errors++
		rep.problem("%s: %v", ent.Key, err)
	}
	return nil
}

// reopenMbox liest die Nachricht an offset (Beginn ihrer Trennzeile) noch
// einmal, mit dem Quoting der Datei, über dasselbe Datei-Handle (ReadAt;
// die Leseposition des laufenden Imports bleibt, wo sie ist).
func reopenMbox(f io.ReaderAt, offset int64, q quoting) func() ([]byte, io.Reader, func() error, error) {
	return func() ([]byte, io.Reader, func() error, error) {
		sc := newMboxScanner(io.NewSectionReader(f, offset, math.MaxInt64-offset), offset)
		sc.started, sc.quote = true, q
		m, err := sc.Next()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read the message again: %w", err)
		}
		return m.Header, m.Body, m.Body.drain, nil
	}
}

// reopenMaildir liest eine Maildir-Datei über dasselbe Handle noch einmal
// (ein Umbenennen durch einen anderen Client stört so nicht).
func reopenMaildir(f io.ReaderAt) func() ([]byte, io.Reader, func() error, error) {
	return func() ([]byte, io.Reader, func() error, error) {
		mr := newMsgReader(newSource(io.NewSectionReader(f, 0, math.MaxInt64), 0), false, quoteNone)
		hdr, _, err := mr.readHeader()
		if err != nil {
			return nil, nil, nil, err
		}
		hdr, _ = stripMozilla(hdr)
		return hdr, mr, mr.drain, nil
	}
}
