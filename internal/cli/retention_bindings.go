package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Löschkonzept (T3, S3.4, E8; docs/cli.md Abschnitt „Löschkonzept“,
// docs/betrieb.md Abschnitt „Löschkonzept“): purge und blob gc, nur für
// den Menschen; --dry-run zählt nur.
func init() {
	bindings = append(bindings,
		binding{op: "retention.purge", path: []string{"purge"}, use: "--older-than <period> [--dry-run]", args: cobra.NoArgs,
			flags: purgeFlags, input: purgeInput, human: renderPurge, covers: []string{"older_than", "account"}},
		binding{op: "blobs.gc", path: []string{"blob", "gc"}, use: "[--dry-run]", args: cobra.NoArgs, human: renderBlobGC},
	)
}

func purgeFlags(cmd *cobra.Command) {
	cmd.Flags().String("older-than", "", "delete messages deleted on the server longer ago than this (e.g. 90d, 12h, or a date 2026-01-31); default: [retention] deleted_remote_after")
}

func purgeInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	in := map[string]string{}
	if v, _ := cmd.Flags().GetString("older-than"); v != "" {
		in["older_than"] = v
	}
	if v := accountFlag(cmd); v != "" {
		in["account"] = v
	}
	b, err := json.Marshal(in)
	return b, "", err
}

func renderPurge(w io.Writer, out any) error {
	o, ok := out.(ops.RetentionPurgeOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	scope := "all accounts"
	if o.Account != "" {
		scope = "account " + termLine(o.Account)
	}
	fmt.Fprintf(w, "%s, deleted on the server before %s (older than %s):\n", scope, o.Cutoff, termLine(o.OlderThan))
	fmt.Fprintf(w, "  gone from the server: %d\n", o.GoneFromServer)
	fmt.Fprintf(w, "  kept (imported archive): %d\n", o.KeptImported)
	fmt.Fprintf(w, "  kept (a draft refers to them): %d\n", o.KeptDrafts)
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: %s would be deleted from the local archive; nothing changed\n", plural(o.Eligible, "message", "messages"))
		return err
	}
	fmt.Fprintf(w, "deleted: %s, %s, %s that became empty\n", plural(o.Purged, "message", "messages"),
		plural(o.Attachments, "attachment entry", "attachment entries"), plural(o.ThreadsRemoved, "thread", "threads"))
	_, err := fmt.Fprintln(w, "the stored files stay until: blob gc")
	return err
}

func renderBlobGC(w io.Writer, out any) error {
	o, ok := out.(ops.BlobsGCOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "blob store: %s, %s in table blobs, %d referenced\n", plural(o.Files, "file", "files"), plural(o.Rows, "row", "rows"), o.Referenced)
	fmt.Fprintf(w, "  without reference: %d, of these younger than %s (kept): %d\n", o.Unreferenced, o.MinAge, o.KeptYoung)
	verb := "deleted"
	if o.DryRun {
		verb = "would delete"
	}
	fmt.Fprintf(w, "%s: %s (%s), %s\n", verb, plural(o.RemovedFiles, "file", "files"), humanBytes(o.Bytes), plural(o.RemovedRows, "row", "rows"))
	if o.Skipped > 0 {
		fmt.Fprintf(w, "kept: %s referenced again during the run\n", plural(o.Skipped, "file", "files"))
	}
	if o.DryRun {
		_, err := fmt.Fprintln(w, "dry run: nothing changed")
		return err
	}
	return nil
}
