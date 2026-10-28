package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Tags und Postfach-Aktionen (T2, A03 S3.1/S3.2; docs/cli.md Abschnitt
// „Tags und Aktionen“).
func init() {
	bindings = append(bindings,
		binding{op: "messages.tag", path: []string{"tag", "add"}, use: "<tag>[,<tag>...] <id>...", args: cobra.MinimumNArgs(2),
			flags: confirmFlag, input: tagInput("add"), human: renderTag, covers: []string{"account", "ids", "add", "confirm_count"}},
		binding{op: "messages.tag", path: []string{"tag", "remove"}, use: "<tag>[,<tag>...] <id>...", args: cobra.MinimumNArgs(2),
			flags: confirmFlag, input: tagInput("remove"), human: renderTag, covers: []string{"account", "ids", "remove", "confirm_count"}},
		binding{op: "tags.list", path: []string{"tag", "list"}, args: cobra.NoArgs, input: accountOnlyInput, human: renderTagList,
			covers: []string{"account"}},
		binding{op: "messages.mark", path: []string{"mark"}, use: "<id>... --seen|--unseen|--flagged|--unflagged", args: cobra.MinimumNArgs(1),
			flags: markFlags, input: markInput, human: renderAction, covers: []string{"account", "ids", "seen", "flagged", "confirm_count"}},
		binding{op: "messages.move", path: []string{"move"}, use: "<id>... --to <mailbox> | --role archive|all|inbox|junk", args: cobra.MinimumNArgs(1),
			flags: moveFlags, input: moveInput, human: renderAction, covers: []string{"account", "ids", "mailbox", "role", "from", "confirm_count"}},
		binding{op: "messages.trash", path: []string{"trash"}, use: "<id>... [--yes]", args: cobra.MinimumNArgs(1),
			flags: trashFlags, input: trashInput, human: renderAction, covers: []string{"account", "ids", "from", "confirm_count"},
			then: trashThen},
		binding{op: "actions.approve", path: []string{"action", "approve"}, use: "<action-id>", args: cobra.ExactArgs(1),
			input: actionIDInput, human: renderActionDecision, covers: []string{"action_id"}},
		binding{op: "actions.reject", path: []string{"action", "reject"}, use: "<action-id> [--reason text]", args: cobra.ExactArgs(1),
			flags: func(cmd *cobra.Command) {
				cmd.Flags().String("reason", "", "why (stays in the approval, not in the audit)")
			},
			input: actionIDInput, human: renderActionDecision, covers: []string{"action_id", "reason"}},
		binding{op: "actions.get", path: []string{"action", "show"}, use: "<action-id>", args: cobra.ExactArgs(1),
			input: actionIDInput, human: renderActionView, covers: []string{"action_id"}},
		binding{op: "actions.list", path: []string{"action", "list"}, args: cobra.NoArgs, flags: actionListFlags,
			input: actionListInput, human: renderActionList, covers: []string{"account", "state", "limit"}},
		binding{op: "drafts.cleanup_copies", path: []string{"draft", "cleanup"}, use: "<draft-id>", args: cobra.ExactArgs(1),
			input: draftIDInput, human: renderCleanup, covers: []string{"draft_id"}},
	)
}

func confirmFlag(cmd *cobra.Command) {
	cmd.Flags().Int("confirm-count", 0, "exactly the number of ids (needed with a key above [move] bulk_confirm_threshold)")
}

// idsArgs liest Nachrichten-IDs (auch durch Komma getrennt).
func idsArgs(args []string) ([]int64, error) {
	var out []int64
	for _, a := range args {
		for _, p := range strings.Split(a, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			n, err := strconv.ParseInt(p, 10, 64)
			if err != nil || n < 1 {
				return nil, usageError{fmt.Errorf("message ids are local numbers as shown by search (got %q)", termLine(p))}
			}
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, usageError{errors.New("give at least one message id")}
	}
	return out, nil
}

// actionMap baut die gemeinsamen Felder: Konto, IDs, confirm_count.
func actionMap(cmd *cobra.Command, args []string) (map[string]any, error) {
	ids, err := idsArgs(args)
	if err != nil {
		return nil, err
	}
	m := map[string]any{"ids": ids}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	if cmd.Flags().Changed("confirm-count") {
		n, _ := cmd.Flags().GetInt("confirm-count")
		m["confirm_count"] = n
	}
	if f := cmd.Flags().Lookup("from"); f != nil && f.Changed {
		m["from"] = f.Value.String()
	}
	return m, nil
}

func tagInput(field string) func(*cobra.Command, []string, *app) (json.RawMessage, secrets.Secret, error) {
	return func(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
		m, err := actionMap(cmd, args[1:])
		if err != nil {
			return nil, "", err
		}
		var names []string
		for _, t := range strings.Split(args[0], ",") {
			if t = strings.TrimSpace(t); t != "" {
				names = append(names, t)
			}
		}
		m[field] = names
		b, err := json.Marshal(m)
		return b, "", err
	}
}

func markFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Bool("seen", false, "mark as read")
	f.Bool("unseen", false, "mark as unread")
	f.Bool("flagged", false, "flag")
	f.Bool("unflagged", false, "remove the flag")
	confirmFlag(cmd)
}

func markInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m, err := actionMap(cmd, args)
	if err != nil {
		return nil, "", err
	}
	f := cmd.Flags()
	for _, pair := range [][3]string{{"seen", "seen", "unseen"}, {"flagged", "flagged", "unflagged"}} {
		on, _ := f.GetBool(pair[1])
		off, _ := f.GetBool(pair[2])
		switch {
		case on && off:
			return nil, "", usageError{fmt.Errorf("--%s and --%s exclude each other", pair[1], pair[2])}
		case on:
			m[pair[0]] = true
		case off:
			m[pair[0]] = false
		}
	}
	if _, s := m["seen"]; !s {
		if _, fl := m["flagged"]; !fl {
			return nil, "", usageError{errors.New("give --seen, --unseen, --flagged or --unflagged")}
		}
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func moveFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("to", "", "target mailbox path (see: mailbox list)")
	f.String("role", "", "target mailbox by role: archive, all (Gmail: remove a label), inbox, junk, sent, drafts")
	f.String("from", "", "source mailbox path, needed when a message is in several mailboxes")
	confirmFlag(cmd)
}

func moveInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m, err := actionMap(cmd, args)
	if err != nil {
		return nil, "", err
	}
	to, _ := cmd.Flags().GetString("to")
	role, _ := cmd.Flags().GetString("role")
	if (to == "") == (role == "") {
		return nil, "", usageError{errors.New("give exactly one of --to <mailbox> or --role <role>")}
	}
	if to != "" {
		m["mailbox"] = to
	} else {
		m["role"] = role
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func trashFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("from", "", "source mailbox path, needed when a message is in several mailboxes")
	f.Bool("yes", false, "approve the request at once (only a human without --key; two audit entries)")
	confirmFlag(cmd)
}

func trashInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m, err := actionMap(cmd, args)
	if err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(m)
	return b, "", err
}

// trashThen: Mit --yes gibt der Mensch den eben gestellten Antrag im selben
// Zug frei (E3): ein zweiter Aufruf über den Executor (actions.approve,
// eigener Audit-Eintrag; mit --key lehnt die Policy ihn ab, human_only).
func trashThen(cmd *cobra.Command, out any) (string, json.RawMessage, bool) {
	yes, _ := cmd.Flags().GetBool("yes")
	o, ok := out.(ops.ActionOutput)
	if !yes || !ok || o.ActionID == 0 {
		return "", nil, false
	}
	b, _ := json.Marshal(map[string]int64{"action_id": o.ActionID})
	return "actions.approve", b, true
}

func actionIDInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return nil, "", usageError{errors.New("the action id is a number (see: action list)")}
	}
	m := map[string]any{"action_id": id}
	if f := cmd.Flags().Lookup("reason"); f != nil && f.Changed {
		m["reason"] = f.Value.String()
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func draftIDInput(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return nil, "", usageError{errors.New("the draft id is a number (see: draft list)")}
	}
	b, err := json.Marshal(map[string]int64{"draft_id": id})
	return b, "", err
}

func actionListFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("state", nil, "only these states: requested, queued, running, done, partial, failed, rejected, expired")
	cmd.Flags().Int("limit", 0, "at most this many (default 50)")
}

func actionListInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m := map[string]any{}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	if st, _ := cmd.Flags().GetStringSlice("state"); len(st) > 0 {
		m["state"] = st
	}
	if cmd.Flags().Changed("limit") {
		n, _ := cmd.Flags().GetInt("limit")
		m["limit"] = n
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func renderTag(w io.Writer, out any) error {
	o, ok := out.(ops.TagOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	prefix := ""
	if o.DryRun {
		prefix = "dry run: "
	}
	fmt.Fprintf(w, "%s%d tags added, %d removed in account %s\n", prefix, o.Added, o.Removed, o.Account)
	for _, m := range o.Messages {
		fmt.Fprintf(w, "  %d\t%s\n", m.ID, orDash(strings.Join(m.Tags, ", ")))
	}
	switch o.Keywords.Status {
	case "queued":
		fmt.Fprintf(w, "keywords: queued for the server (action %d)\n", o.Keywords.ActionID)
	case "no_server_copy":
		fmt.Fprintln(w, "keywords: no copy on the server, tags stay local")
	}
	return nil
}

func renderTagList(w io.Writer, out any) error {
	o, ok := out.(ops.TagsListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Tags) == 0 {
		fmt.Fprintf(w, "no tags in account %s\n", o.Account)
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TAG\tMESSAGES")
	for _, t := range o.Tags {
		fmt.Fprintf(tw, "%s\t%d\n", t.Name, t.Messages)
	}
	return tw.Flush()
}

func renderAction(w io.Writer, out any) error {
	o, ok := out.(ops.ActionOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	prefix := ""
	if o.DryRun {
		prefix = "dry run: "
	}
	target := ""
	if o.Target != nil {
		target = " to " + termLine(o.Target.Mailbox)
	}
	switch o.Status {
	case "needs_approval":
		fmt.Fprintf(w, "%s%s of %d messages%s requested (action %d, approval %d): needs the approval of a human\n", prefix, o.Kind, o.Messages,
			target, o.ActionID, o.ApprovalID)
	default:
		fmt.Fprintf(w, "%s%s of %d messages%s queued (action %d, %d server copies)\n", prefix, o.Kind, o.Messages, target, o.ActionID, o.ServerCopies)
	}
	if len(o.Skipped) > 0 {
		fmt.Fprintf(w, "skipped (already there): %s\n", joinIDs(o.Skipped))
	}
	fmt.Fprintln(w, termLine(o.NextStep))
	return nil
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ", ")
}

func renderActionDecision(w io.Writer, out any) error {
	o, ok := out.(ops.ActionDecisionOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		fmt.Fprintf(w, "dry run: action %d is waiting for approval; nothing decided\n", o.ActionID)
		return nil
	}
	fmt.Fprintf(w, "action %d: %s (approval %d by %s)\n", o.ActionID, o.State, o.ApprovalID, termLine(orDash(o.DecidedBy)))
	if o.Stale > 0 {
		fmt.Fprintf(w, "%d messages changed on the server since the request and are not touched (stale)\n", o.Stale)
	}
	if o.NextStep != "" {
		fmt.Fprintln(w, termLine(o.NextStep))
	}
	return nil
}

func renderActionView(w io.Writer, out any) error {
	o, ok := out.(ops.ActionView)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "action %d (%s) in account %s: %s, %d messages, requested by %s at %s\n", o.ID, o.Kind, o.Account, o.State, o.Messages,
		o.RequestedBy, o.CreatedAt)
	if o.Target != nil {
		fmt.Fprintf(w, "target: %s\n", termLine(o.Target.Mailbox))
	}
	if o.Error != "" {
		fmt.Fprintf(w, "error: %s\n", o.Error)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MESSAGE\tMAILBOX\tUID\tRESULT")
	for _, t := range o.Targets {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n", t.MessageID, termLine(t.Mailbox), t.UID, orDash(t.Result))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if o.NextStep != "" {
		fmt.Fprintln(w, termLine(o.NextStep))
	}
	return nil
}

func renderActionList(w io.Writer, out any) error {
	o, ok := out.(ops.ActionsListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Actions) == 0 {
		fmt.Fprintln(w, "no actions")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tACCOUNT\tKIND\tSTATE\tMESSAGES\tBY\tCREATED")
	for _, a := range o.Actions {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\t%s\n", a.ID, a.Account, a.Kind, a.State, a.Messages, a.RequestedBy, a.CreatedAt)
	}
	return tw.Flush()
}

func renderCleanup(w io.Writer, out any) error {
	o, ok := out.(ops.CleanupOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	prefix := ""
	if o.DryRun {
		prefix = "dry run: "
	}
	fmt.Fprintf(w, "%sdraft %d: %d earlier copies to the trash, %d not touched\n", prefix, o.DraftID, o.Copies, o.Skipped)
	fmt.Fprintln(w, termLine(o.NextStep))
	return nil
}
