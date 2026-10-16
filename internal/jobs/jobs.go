// Package jobs arbeitet die Warteschlange in SQLite ab (Tabelle jobs;
// docs/architektur.md, Abschnitt 7): Textextraktion aus Anhängen
// (extract) und Volltextindex (index). Ein Job gilt einem Konto; der
// eigentliche Stand liegt in attachments.extract_status bzw. index_queue,
// deshalb ist jeder Schritt wiederholbar und ein Abbruch (Strg+C, kill -9)
// verliert höchstens die laufende Transaktion bzw. den laufenden Anhang.
//
// Der Sync ruft RunAccount am Ende jedes Laufs auf (auch im Dauerbetrieb);
// kurzlebige Prozesse (search, MCP) lesen nur und melden, was noch
// aussteht.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Store ist der Teil des Stores, den die Jobs brauchen.
type Store interface {
	EnsureFTSVersion(ctx context.Context, version int) (bool, error)
	PruneJobs(ctx context.Context, before time.Time) error
	ClaimJob(ctx context.Context, typ string, accountID int64, owner string, lease time.Duration, takeOver bool) (store.Job, bool, error)
	TouchJob(ctx context.Context, id int64, owner string) error
	CompleteJob(ctx context.Context, j store.Job, owner string) (bool, error)
	ReleaseJob(ctx context.Context, id int64, owner string, runAt time.Time) error
	FailJob(ctx context.Context, id int64, owner, errText string, maxAttempts int, retryAt time.Time) error
	PendingExtractions(ctx context.Context, accountID int64, limit int) ([]store.ExtractItem, error)
	SetExtraction(ctx context.Context, id int64, status, text, errText string, hidden bool) error
	IndexBatch(ctx context.Context, accountID int64, limit int, build func(search.Source) search.Doc) (int, error)
	IndexPending(ctx context.Context, accountID int64) (int, error)
}

// Blobs liest Anhänge aus dem Blob-Store.
type Blobs interface {
	Get(sha256 string) (io.ReadCloser, error)
}

// Standardwerte.
const (
	// DefaultExtractBudget begrenzt die Zeit für Textextraktion je Lauf;
	// der Rest folgt im nächsten Lauf (der Job bleibt fällig).
	DefaultExtractBudget = 60 * time.Second
	// DefaultLease: So lange gilt die Sperre eines Arbeiters ohne
	// Lebenszeichen; danach darf ein anderer den Job übernehmen.
	DefaultLease = 2 * time.Minute
	// DefaultBatch: Nachrichten je Index-Transaktion.
	DefaultBatch = 200
	// MaxAttempts: nach so vielen Fehlern des Jobs (nicht einzelner
	// Anhänge) endgültig failed.
	MaxAttempts = 5
	// KeepDone: erledigte und gescheiterte Jobs werden danach gelöscht.
	KeepDone = 7 * 24 * time.Hour
)

// Event ist eine Fortschrittsmeldung (ohne Inhalte).
type Event struct {
	Kind  string // extract | index
	Done  int
	Total int
}

// Report fasst einen Lauf zusammen.
type Report struct {
	Extracted    int // Anhänge, deren Extraktion abgeschlossen wurde (ok, failed oder skipped)
	Indexed      int // indizierte Nachrichten
	IndexPending int // danach noch eingereiht
}

// Runner führt die Jobs eines Kontos aus.
type Runner struct {
	Store Store
	Blobs Blobs
	// Extractor gewinnt Text aus Anhängen; nil = extract-Jobs bleiben
	// liegen (Anhänge bleiben pending).
	Extractor     *extract.Extractor
	ExtractBudget time.Duration // 0 = DefaultExtractBudget
	Log           *slog.Logger
	// TakeOver: Der Aufrufer hält die Kontosperre des Syncs. Ein laufender
	// Job dieses Kontos stammt dann von einem abgebrochenen Prozess und
	// wird sofort übernommen statt nach Ablauf der Sperre.
	TakeOver bool
	Lease    time.Duration // 0 = DefaultLease
	Batch    int           // 0 = DefaultBatch
	Owner    string        // "" = aus Prozess-ID und Startzeit
	Progress func(Event)
}

func (r *Runner) log() *slog.Logger {
	if r.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return r.Log
}

func (r *Runner) owner() string {
	if r.Owner == "" {
		r.Owner = fmt.Sprintf("pid %d at %d", os.Getpid(), time.Now().UnixNano())
	}
	return r.Owner
}

func (r *Runner) lease() time.Duration {
	if r.Lease > 0 {
		return r.Lease
	}
	return DefaultLease
}

func (r *Runner) emit(ev Event) {
	if r.Progress != nil {
		r.Progress(ev)
	}
}

// RunAccount führt die fälligen Jobs eines Kontos aus: zuerst die
// Extraktion (mit Zeitbudget), dann den Index (bis die Warteschlange leer
// ist; extrahierter Text kommt so im selben Lauf in den Index). Vorher wird
// der Index neu aufgebaut, wenn sich Normalisierer oder Stemmer geändert
// haben. Ein Abbruch (ctx) gibt die Jobs zurück und liefert ctx.Err().
func (r *Runner) RunAccount(ctx context.Context, accountID int64) (Report, error) {
	var rep Report
	if rebuilt, err := r.Store.EnsureFTSVersion(ctx, search.FTSVersion); err != nil {
		return rep, fmt.Errorf("index version: %w", err)
	} else if rebuilt {
		r.log().Info("search index is rebuilt for a new version", "fts_version", search.FTSVersion)
	}
	if err := r.Store.PruneJobs(ctx, time.Now().Add(-KeepDone)); err != nil {
		return rep, err
	}
	var err error
	if r.Extractor != nil {
		if rep.Extracted, err = r.runExtract(ctx, accountID); err != nil {
			return rep, fmt.Errorf("extract: %w", err)
		}
	}
	if rep.Indexed, err = r.runIndex(ctx, accountID); err != nil {
		return rep, fmt.Errorf("index: %w", err)
	}
	rep.IndexPending, err = r.Store.IndexPending(ctx, accountID)
	return rep, err
}

// release gibt einen Job nach Abbruch oder erschöpftem Budget zurück (auch
// wenn ctx schon beendet ist).
func (r *Runner) release(ctx context.Context, j store.Job) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.Store.ReleaseJob(c, j.ID, r.owner(), time.Now()); err != nil {
		r.log().Warn("job could not be released", "type", j.Type, "error", err.Error())
	}
}

// fail verbucht einen Fehler des Jobs mit Wiederholung nach 1, 2, 4 …
// Minuten (höchstens eine Stunde).
func (r *Runner) fail(ctx context.Context, j store.Job, err error) error {
	if ctx.Err() != nil {
		r.release(ctx, j)
		return ctx.Err()
	}
	backoff := time.Minute << min(j.Attempts, 6)
	backoff = min(backoff, time.Hour)
	if e := r.Store.FailJob(context.WithoutCancel(ctx), j.ID, r.owner(), err.Error(), MaxAttempts, time.Now().Add(backoff)); e != nil {
		r.log().Warn("job failure could not be recorded", "type", j.Type, "error", e.Error())
	}
	return err
}

// runExtract extrahiert wartende Anhänge, bis nichts mehr wartet oder das
// Zeitbudget erschöpft ist (dann bleibt der Job fällig).
func (r *Runner) runExtract(ctx context.Context, accountID int64) (int, error) {
	j, ok, err := r.Store.ClaimJob(ctx, store.JobExtract, accountID, r.owner(), r.lease(), r.TakeOver)
	if err != nil || !ok {
		return 0, err
	}
	budget := r.ExtractBudget
	if budget <= 0 {
		budget = DefaultExtractBudget
	}
	deadline := time.Now().Add(budget)
	done := 0
	// Mindestens ein Anhang je Lauf, auch bei winzigem Budget (Fortschritt).
	for done == 0 || time.Now().Before(deadline) {
		items, err := r.Store.PendingExtractions(ctx, accountID, 20)
		if err != nil {
			return done, r.fail(ctx, j, err)
		}
		if len(items) == 0 {
			if _, err := r.Store.CompleteJob(ctx, j, r.owner()); err != nil && !errors.Is(err, store.ErrJobLost) {
				return done, err
			}
			return done, nil
		}
		for _, it := range items {
			res := r.extractOne(ctx, it)
			if ctx.Err() != nil {
				r.release(ctx, j) // abgebrochen: nicht als Fehler des Anhangs speichern
				return done, ctx.Err()
			}
			if err := r.Store.SetExtraction(ctx, it.ID, res.Status, res.Text, res.Error, res.Hidden); err != nil {
				return done, r.fail(ctx, j, err)
			}
			done++
			if !time.Now().Before(deadline) {
				break
			}
		}
		r.emit(Event{Kind: "extract", Done: done})
		if ctx.Err() != nil {
			r.release(ctx, j)
			return done, ctx.Err()
		}
		if err := r.Store.TouchJob(ctx, j.ID, r.owner()); err != nil {
			if errors.Is(err, store.ErrJobLost) {
				return done, nil // ein anderer Arbeiter hat übernommen
			}
			return done, err
		}
	}
	r.release(ctx, j)
	r.log().Info("extraction budget used up; the rest follows in the next run", "extracted", done)
	return done, nil
}

func (r *Runner) extractOne(ctx context.Context, it store.ExtractItem) extract.Result {
	if it.SHA256 == "" {
		return extract.Result{Status: extract.StatusSkipped, Error: "not stored"}
	}
	rc, err := r.Blobs.Get(it.SHA256)
	if err != nil {
		return extract.Result{Status: extract.StatusFailed, Error: "attachment blob unreadable"}
	}
	defer rc.Close()
	return r.Extractor.Extract(ctx, extract.KindOf(it.ContentType), rc, it.Size)
}

// runIndex arbeitet die Index-Warteschlange des Kontos ab.
func (r *Runner) runIndex(ctx context.Context, accountID int64) (int, error) {
	batch := r.Batch
	if batch <= 0 {
		batch = DefaultBatch
	}
	st := search.NewStemmer()
	done := 0
	// Kommt während des Laufs neue Arbeit dazu (anderer Prozess), lässt
	// CompleteJob den Job fällig; dann noch einmal (höchstens dreimal).
	for round := 0; round < 3; round++ {
		j, ok, err := r.Store.ClaimJob(ctx, store.JobIndex, accountID, r.owner(), r.lease(), r.TakeOver)
		if err != nil || !ok {
			return done, err
		}
		total, err := r.Store.IndexPending(ctx, accountID)
		if err != nil {
			return done, r.fail(ctx, j, err)
		}
		total += done
		for {
			n, err := r.Store.IndexBatch(ctx, accountID, batch, st.Build)
			if err != nil {
				return done, r.fail(ctx, j, err)
			}
			if n == 0 {
				break
			}
			done += n
			r.emit(Event{Kind: "index", Done: done, Total: max(total, done)})
			if ctx.Err() != nil {
				r.release(ctx, j)
				return done, ctx.Err()
			}
			if err := r.Store.TouchJob(ctx, j.ID, r.owner()); err != nil {
				if errors.Is(err, store.ErrJobLost) {
					return done, nil
				}
				return done, err
			}
		}
		finished, err := r.Store.CompleteJob(ctx, j, r.owner())
		if err != nil && !errors.Is(err, store.ErrJobLost) {
			return done, err
		}
		if finished || err != nil {
			return done, nil
		}
	}
	return done, nil
}
