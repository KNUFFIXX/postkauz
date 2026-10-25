package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// --- import (S1.10) -----------------------------------------------------------

func importFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("mailbox", "", "target folder (a file or the maildir root) or prefix of the folders below (default: from the paths)")
	f.StringArray("labels-from", nil, "folder whose messages are only linked to existing ones with the same content, e.g. Gmail labels; copies with other content are skipped with a note, partial Thunderbird copies are linked (repeatable)")
	f.Bool("rescan", false, "read sources again that did not change since the last import")
	f.Bool("no-extract", false, "do not extract all attachment text now (only the time budget of a sync; the next syncs continue)")
}

// importInput: import <mbox|maildir> <pfad>.
func importInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	if args[0] != "mbox" && args[0] != "maildir" {
		return nil, "", usageError{errors.New("unknown format: use import mbox <file|folder> or import maildir <folder>")}
	}
	f := cmd.Flags()
	mailbox, _ := f.GetString("mailbox")
	labels, _ := f.GetStringArray("labels-from")
	rescan, _ := f.GetBool("rescan")
	noExtract, _ := f.GetBool("no-extract")
	if f.Changed("mailbox") && mailbox == "" {
		return nil, "", usageError{errors.New("--mailbox must not be empty")}
	}
	in := ops.ImportRunInput{Account: accountFlag(cmd), Format: args[0], Path: args[1], Mailbox: mailbox, LabelsFrom: labels, Rescan: rescan, NoExtract: noExtract}
	b, err := json.Marshal(in)
	return b, "", err
}

// importProgress: Fortschritt je Quelle auf stderr (höchstens eine Zeile je
// Sekunde), mit --json nichts.
func importProgress(cmd *cobra.Command, a *app) func(ops.Progress) {
	asJSON, _ := cmd.Flags().GetBool("json")
	var mu sync.Mutex
	var last time.Time
	started := map[string]time.Time{}
	return func(p ops.Progress) {
		if asJSON {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch p.Event {
		case "import_source_start":
			started[p.Source] = time.Now()
		case "import_progress":
			if p.Done >= p.Total || time.Since(last) < time.Second {
				return
			}
			last = time.Now()
			if p.Unit == "files" {
				fmt.Fprintf(a.stderr, "  %s: %d/%d files\n", termLine(p.Source), p.Done, p.Total)
				return
			}
			line := fmt.Sprintf("  %s: %d%% (%s of %s)", termLine(p.Source), int(float64(p.Done)*100/float64(max(p.Total, 1))),
				humanBytes(int64(p.Done)), humanBytes(int64(p.Total)))
			if s := time.Since(started[p.Source]).Seconds(); s > 0 {
				line += fmt.Sprintf(", %s/s", humanBytes(int64(float64(p.Done)/s)))
			}
			fmt.Fprintln(a.stderr, line)
		case "index":
			if p.Done >= p.Total || time.Since(last) < time.Second {
				return
			}
			last = time.Now()
			fmt.Fprintf(a.stderr, "  search index: %d/%d\n", p.Done, p.Total)
		case "extract":
			if p.Done >= p.Total || time.Since(last) < time.Second {
				return
			}
			last = time.Now()
			fmt.Fprintf(a.stderr, "  attachment text: %d/%d\n", p.Done, p.Total)
		}
	}
}

// humanBytes: 1.2 GB, 245.1 MB, 12.0 KB (Basis 1000, wie Explorer/Finder
// nicht; hier SI, eindeutig benannt).
func humanBytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}

func renderImport(w io.Writer, out any) error {
	o, ok := out.(ops.ImportOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	secs := float64(o.DurationMS) / 1000
	if o.DryRun {
		fmt.Fprintln(w, "dry run: nothing was written (counts show what an import would do)")
	}
	line := fmt.Sprintf("import %s into account %s (%.1fs, %s", o.Format, o.Account, secs, humanBytes(o.Bytes))
	if secs > 0 && o.Bytes > 0 {
		line += fmt.Sprintf(", %s/s", humanBytes(int64(float64(o.Bytes)/secs)))
	}
	fmt.Fprintln(w, line+")")
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tFOLDER\tMODE\tNEW\tEXISTING\tSKIPPED\tERRORS")
	for _, s := range o.Sources {
		if s.Unchanged {
			fmt.Fprintf(tw, "%s\t%s\t%s\tunchanged\t\t\t\n", termLine(s.Path), termLine(s.Folder), s.Mode)
			continue
		}
		note := ""
		if s.ResumedAt > 0 {
			note = "\t(resumed)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d%s\n", termLine(s.Path), termLine(s.Folder), s.Mode, s.New, s.Existing, s.Skipped, s.Errors, note)
	}
	_ = tw.Flush()
	for _, s := range o.Sources {
		for _, p := range s.Problems {
			fmt.Fprintf(w, "  %s: %s\n", termLine(s.Path), termLine(p))
		}
	}
	if o.SourcesUnchanged > 0 {
		fmt.Fprintf(w, "%d sources unchanged since the last import, not read again (--rescan reads them)\n", o.SourcesUnchanged)
	}
	if o.FilesIgnored > 0 {
		fmt.Fprintf(w, "%d files ignored (not mbox, e.g. Thunderbird .msf index files)\n", o.FilesIgnored)
	}
	if o.Linked > 0 {
		fmt.Fprintf(w, "%d folder assignments added to existing messages\n", o.Linked)
	}
	if o.AttachmentsExtracted > 0 || o.ExtractPending > 0 {
		line := fmt.Sprintf("attachment text: %d attachments extracted", o.AttachmentsExtracted)
		if o.ExtractPending > 0 {
			line += fmt.Sprintf(", %d still pending (the next sync continues)", o.ExtractPending)
		}
		fmt.Fprintln(w, line)
	}
	if o.MessagesIndexed > 0 || o.IndexPending > 0 {
		line := fmt.Sprintf("search index: %d messages indexed", o.MessagesIndexed)
		if o.IndexPending > 0 {
			line += fmt.Sprintf(", %d still pending (the next sync or import continues)", o.IndexPending)
		}
		fmt.Fprintln(w, line)
	}
	_, err := fmt.Fprintf(w, "new: %d, existing: %d, skipped: %d, errors: %d\n", o.New, o.Existing, o.Skipped, o.Errors)
	return err
}
