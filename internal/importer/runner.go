package importer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Runner implementiert ops.ImportRunner: Quellen finden, Konto sperren
// (wie der Sync: ein schreibender Lauf je Konto), importieren, danach die
// Jobs des Kontos (Extraktion, Suchindex) wie am Ende eines Syncs.
type Runner struct {
	Store *store.SQLite
	Blobs *blob.Store
	Log   *slog.Logger
	// LockDir: Ordner der Sperrdateien (leer = neben der Datenbank).
	LockDir string
	// Extractor (nil = Standard).
	Extractor *extract.Extractor
	// AfterBatch: Tests (Absturz simulieren).
	AfterBatch func(src Source, position int64) error
}

var _ ops.ImportRunner = (*Runner)(nil)

// Import führt import.run aus.
func (r *Runner) Import(ctx context.Context, a store.Account, o ops.ImportOptions, progress func(ops.Progress)) (ops.ImportOutput, error) {
	out := ops.ImportOutput{Account: a.Name, Format: o.Format, Sources: []ops.ImportSource{}}
	start := time.Now()
	content, ignored, err := Discover(o.Path, o.Format, o.Mailbox, store.ImportContent)
	if err != nil {
		return out, inputError(err)
	}
	srcs := content
	for _, p := range o.LabelsFrom {
		labels, ign, err := Discover(p, o.Format, "", store.ImportLabels)
		if err != nil {
			return out, inputError(err)
		}
		ignored += ign
		srcs = append(srcs, excludeSources(labels, content)...)
	}
	out.FilesIgnored = ignored
	locked := false
	if !o.DryRun {
		release, l, err := mailsync.LockAccount(r.Store, r.LockDir, a)
		if err != nil {
			return out, err
		}
		defer release()
		locked = l
	}
	eng := &Engine{Store: r.Store, Blobs: r.Blobs, AccountID: a.ID, Log: r.Log, AfterBatch: r.AfterBatch}
	if progress != nil {
		eng.Progress = func(ev Event) {
			progress(ops.Progress{Event: "import_" + ev.Kind, Mailbox: ev.Folder, Source: ev.Source, Unit: ev.Unit,
				Done: int(ev.Done), Total: int(ev.Total)})
		}
	}
	rep, err := eng.Run(ctx, Options{Sources: srcs, DryRun: o.DryRun, Rescan: o.Rescan})
	fill(&out, rep)
	if err != nil {
		out.DurationMS = time.Since(start).Milliseconds()
		return out, err
	}
	if !o.DryRun {
		ex := r.Extractor
		if ex == nil {
			ex = extract.New(extract.Options{})
		}
		jr := &jobs.Runner{Store: r.Store, Blobs: r.Blobs, Extractor: ex, Log: r.Log, TakeOver: locked}
		if progress != nil {
			jr.Progress = func(ev jobs.Event) { progress(ops.Progress{Event: ev.Kind, Done: ev.Done, Total: ev.Total}) }
		}
		rep, err := jr.RunAccount(ctx, a.ID)
		out.AttachmentsExtracted, out.MessagesIndexed, out.IndexPending = rep.Extracted, rep.Indexed, rep.IndexPending
		if err != nil {
			out.DurationMS = time.Since(start).Milliseconds()
			return out, fmt.Errorf("jobs: %w", err)
		}
	}
	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}

// inputError: Pfad fehlt, kein mbox/Maildir o. Ä. ist ein Eingabefehler
// (CLI Exit 2), kein Programmfehler. Die Meldung nennt den Pfad; ins Audit
// kommt sie nicht (dort steht nur der Grund).
func inputError(err error) error { return &ops.InputError{Msg: err.Error()} }

func fill(out *ops.ImportOutput, rep Report) {
	for _, s := range rep.Sources {
		out.Sources = append(out.Sources, ops.ImportSource{Path: s.Path, Folder: s.Folder, Mode: s.Mode, Variant: s.Variant,
			New: s.New, Existing: s.Existing, Skipped: s.Skipped, Errors: s.Errors, Linked: s.Linked, Bytes: s.Bytes,
			ResumedAt: s.ResumedAt, Unchanged: s.Unchanged, Problems: s.Problems})
	}
	out.New, out.Existing, out.Skipped, out.Errors, out.Linked = rep.New, rep.Existing, rep.Skipped, rep.Errors, rep.Linked
	out.SourcesUnchanged, out.Bytes = rep.Unchanged, rep.Bytes
}
