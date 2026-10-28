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

// Runner implementiert ops.ImportRunner: Konto sperren (wie der Sync: ein
// schreibender Lauf je Konto; seit T2 vor dem Finden der Quellen), Quellen
// finden, importieren, danach die
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
	// BudgetWithoutExtract: Zeitbudget der Extraktion bei --no-extract
	// (0 = jobs.DefaultExtractBudget wie beim Sync; Tests setzen weniger).
	BudgetWithoutExtract time.Duration
}

var _ ops.ImportRunner = (*Runner)(nil)

// Import führt import.run aus.
func (r *Runner) Import(ctx context.Context, a store.Account, o ops.ImportOptions, progress func(ops.Progress)) (ops.ImportOutput, error) {
	out := ops.ImportOutput{Account: a.Name, Format: o.Format, Sources: []ops.ImportSource{}}
	start := time.Now()
	// Die Kontosperre zuerst (T2 Punkt 7): Discover über eine große Quelle
	// kann dauern; solange gälte der offene Start dieses Aufrufs sonst einem
	// anderen Lauf des Kontos als tot (store.CloseDeadStarts).
	locked := false
	if !o.DryRun {
		release, l, err := mailsync.LockAccount(ctx, r.Store, r.LockDir, a, o.AuditID, r.Log)
		if err != nil {
			return out, err
		}
		defer release()
		locked = l
	}
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
		// Auch bei Abbruch schon beim Einlesen: offene Arbeit aus der
		// Datenbank (ohne den beendeten Kontext).
		if !o.DryRun {
			r.pending(ctx, a.ID, &out)
		}
		out.DurationMS = time.Since(start).Milliseconds()
		return out, err
	}
	if !o.DryRun {
		ex := r.Extractor
		if ex == nil {
			ex = extract.New(extract.Options{})
		}
		jr := &jobs.Runner{Store: r.Store, Blobs: r.Blobs, Log: r.Log, TakeOver: locked}
		if progress != nil {
			jr.Progress = func(ev jobs.Event) { progress(ops.Progress{Event: ev.Kind, Done: ev.Done, Total: ev.Total}) }
		}
		// Zuerst nur der Index (ohne Extraktor): Die neuen Nachrichten sind
		// suchbar, auch wenn die lange Extraktion danach abgebrochen wird.
		// Dann die Extraktion, beim Import bis zum Ende (rc6; mit
		// --no-extract nur das Budget wie beim Sync), und der Index des
		// extrahierten Textes. Strg+C lässt den Rest pending, der nächste
		// Sync macht weiter; der Bericht kommt trotzdem (W3, rc7).
		first, err := jr.RunAccount(ctx, a.ID)
		out.MessagesIndexed = first.Indexed
		if err == nil {
			jr.Extractor = ex
			jr.ExtractBudget = jobs.NoExtractBudget
			if o.NoExtract {
				jr.ExtractBudget = r.BudgetWithoutExtract
			}
			var second jobs.Report
			second, err = jr.RunAccount(ctx, a.ID)
			out.AttachmentsExtracted = second.Extracted
			out.MessagesIndexed += second.Indexed
		}
		r.pending(ctx, a.ID, &out)
		if err != nil {
			out.DurationMS = time.Since(start).Milliseconds()
			return out, fmt.Errorf("jobs: %w", err)
		}
	}
	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}

// pending trägt die offene Arbeit (Extraktion, Index) aus der Datenbank
// ein, auch nach einem Abbruch (ohne den beendeten Kontext).
func (r *Runner) pending(ctx context.Context, accountID int64, out *ops.ImportOutput) {
	c := context.WithoutCancel(ctx)
	if n, err := r.Store.ExtractPending(c, accountID); err == nil {
		out.ExtractPending = n
	}
	if n, err := r.Store.IndexPending(c, accountID); err == nil {
		out.IndexPending = n
	}
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
