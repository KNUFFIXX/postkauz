package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Anhänge sichern und Export (S3.3, A03 E6; docs/cli.md Abschnitt
// „Anhänge sichern und Export“).
func init() {
	bindings = append(bindings,
		binding{op: "attachments.save", path: []string{"attach", "save"}, use: "<attachment-id> [--subdir folder]", args: cobra.ExactArgs(1),
			flags: attachSaveFlags, input: attachSaveInput, human: renderAttachSave, covers: []string{"id", "message", "account", "subdir"}},
		binding{op: "messages.export", path: []string{"export"}, use: "<mbox|eml> --to <folder>", args: cobra.ExactArgs(1),
			flags: exportFlags, input: exportInput, human: renderExport, progress: exportProgress, outputOnError: true,
			covers: []string{"format", "to", "account", "mailbox", "since"}},
	)
}

func attachSaveFlags(cmd *cobra.Command) {
	cmd.Flags().String("subdir", "", "folder below export_dir, e.g. invoices/2026 (created if missing)")
	cmd.Flags().Int64("message", 0, "only if the attachment belongs to this message id")
}

// attachSaveInput: attach save <id> [--subdir …] [--message …] [--account …].
func attachSaveInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "attachment id")
	if err != nil {
		return nil, "", err
	}
	m := map[string]any{"id": id}
	if msg, _ := cmd.Flags().GetInt64("message"); msg != 0 {
		if msg < 0 {
			return nil, "", usageError{fmt.Errorf("--message must be a positive number")}
		}
		m["message"] = msg
	}
	if acct := accountFlag(cmd); acct != "" {
		m["account"] = acct
	}
	if sub, _ := cmd.Flags().GetString("subdir"); sub != "" {
		m["subdir"] = sub
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func renderAttachSave(w io.Writer, out any) error {
	o, ok := out.(ops.AttachmentSaveOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	verb := "saved"
	if o.DryRun {
		verb = "dry run: would save"
	}
	fmt.Fprintf(w, "%s attachment %d of message %d as %s below export_dir (%s, sha256 %s)\n", verb, o.ID, o.MessageID,
		termLine(o.Path), humanBytes(o.Size), o.SHA256)
	if o.Blocked {
		fmt.Fprintln(w, "warning: an executable type (program, script or shortcut): saved with the ending .blocked; do not open it")
	}
	if o.Truncated {
		fmt.Fprintln(w, "note: the attachment was already incomplete when the message was mirrored")
	}
	_, err := fmt.Fprintln(w, "export_dir: see doctor")
	return err
}

func exportFlags(cmd *cobra.Command) {
	cmd.Flags().String("to", "", "new or empty folder for the export (required)")
	cmd.Flags().String("mailbox", "", "only this server mailbox (path, see: mailbox list)")
	cmd.Flags().String("since", "", "only messages since (30d, 2026-01-01, RFC 3339)")
}

// exportInput: export <mbox|eml> --to <ordner>. Ein relatives Ziel gilt
// wie gewohnt ab dem aktuellen Ordner (die Operation läse es sonst unter
// export_dir).
func exportInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	if args[0] != "mbox" && args[0] != "eml" {
		return nil, "", usageError{errors.New("unknown format: use export mbox --to <folder> or export eml --to <folder>")}
	}
	f := cmd.Flags()
	to, _ := f.GetString("to")
	if strings.TrimSpace(to) == "" {
		return nil, "", usageError{errors.New("give the target folder with --to (a new or empty folder)")}
	}
	abs, err := filepath.Abs(to)
	if err != nil {
		return nil, "", usageError{err}
	}
	in := ops.MessagesExportInput{Format: args[0], To: abs, Account: accountFlag(cmd)}
	if mb, _ := f.GetString("mailbox"); f.Changed("mailbox") {
		if mb == "" {
			return nil, "", usageError{errors.New("--mailbox must not be empty")}
		}
		in.Mailbox = mb
	}
	in.Since, _ = f.GetString("since")
	b, err := json.Marshal(in)
	return b, "", err
}

// exportProgress: eine Zeile je fertigem Ordner auf stderr, mit --json
// nichts.
func exportProgress(cmd *cobra.Command, a *app) func(ops.Progress) {
	asJSON, _ := cmd.Flags().GetBool("json")
	return func(p ops.Progress) {
		if asJSON || p.Event != "export_folder" {
			return
		}
		fmt.Fprintf(a.stderr, "  %s: %d of %d messages\n", termLine(p.Mailbox), p.Done, p.Total)
	}
}

func renderExport(w io.Writer, out any) error {
	o, ok := out.(ops.MessagesExportOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		fmt.Fprintln(w, "dry run: nothing was written (counts show what an export would write)")
	}
	fmt.Fprintf(w, "export %s of account %s to %s (%s)\n", o.Format, o.Account, termLine(o.Location), humanBytes(o.Bytes))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FOLDER\tPATH\tMESSAGES\tSKIPPED")
	for _, f := range o.Folders {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", termLine(f.Folder), termLine(f.Path), f.Messages, f.Skipped)
	}
	_ = tw.Flush()
	line := fmt.Sprintf("messages: %d, skipped: %d", o.Messages, o.Skipped)
	if len(o.SkippedBy) > 0 {
		reasons := make([]string, 0, len(o.SkippedBy))
		for r, n := range o.SkippedBy {
			reasons = append(reasons, fmt.Sprintf("%s: %d", r, n))
		}
		sort.Strings(reasons)
		line += " (" + strings.Join(reasons, ", ") + ")"
	}
	fmt.Fprintln(w, line)
	if len(o.SkippedIDs) > 0 {
		ids := make([]string, 0, len(o.SkippedIDs))
		for _, s := range o.SkippedIDs {
			ids = append(ids, fmt.Sprintf("%d (%s)", s.ID, s.Reason))
		}
		more := ""
		if o.Skipped > len(o.SkippedIDs) {
			more = ", …"
		}
		fmt.Fprintf(w, "skipped ids: %s%s\n", strings.Join(ids, ", "), more)
	}
	if o.NewlineAdded > 0 {
		fmt.Fprintf(w, "note: %d messages had no line break at the end; the mbox format adds one\n", o.NewlineAdded)
	}
	return nil
}
