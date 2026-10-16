package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// --- sync -------------------------------------------------------------------

func syncFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Bool("full", false, "also re-check all flags and deletions and fetch anything missing")
	f.Bool("watch", false, "keep running: IDLE on INBOX plus a sync of all mailboxes every --interval (stop with Ctrl+C)")
	f.Int("window", 0, "messages per window in the first sync (default 200)")
	f.Duration("interval", 0, "with --watch: time between syncs of all mailboxes (default 5m)")
	f.Duration("idle-restart", 0, "with --watch: restart IDLE after this time (5m to 10m, default 8m)")
	f.StringSlice("mailbox", nil, "only these mailboxes (repeatable)")
}

func accountFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("account")
	return v
}

func syncInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	full, _ := f.GetBool("full")
	watch, _ := f.GetBool("watch")
	window, _ := f.GetInt("window")
	interval, _ := f.GetDuration("interval")
	idle, _ := f.GetDuration("idle-restart")
	boxes, _ := f.GetStringSlice("mailbox")
	if (interval != 0 || idle != 0) && !watch {
		return nil, "", usageError{errors.New("--interval and --idle-restart need --watch")}
	}
	in := ops.SyncRunInput{Account: accountFlag(cmd), Full: full, Watch: watch, Window: window, Mailboxes: boxes,
		IntervalSeconds: int(interval / time.Second), IdleRestartSeconds: int(idle / time.Second)}
	if interval > 0 && in.IntervalSeconds == 0 {
		in.IntervalSeconds = 1
	}
	b, err := json.Marshal(in)
	return b, "", err
}

// syncProgress: ohne --json Fortschritt auf stderr (höchstens eine Zeile je
// Sekunde und Ordner), im Dauerbetrieb je Lauf eine Zeile auf stdout; mit
// --json nur die Läufe des Dauerbetriebs als NDJSON auf stdout.
func syncProgress(cmd *cobra.Command, a *app) func(ops.Progress) {
	asJSON, _ := cmd.Flags().GetBool("json")
	var mu sync.Mutex
	last := map[string]time.Time{}
	enc := json.NewEncoder(a.stdout)
	return func(p ops.Progress) {
		mu.Lock()
		defer mu.Unlock()
		if asJSON {
			if p.Event == "round" && p.Round != nil {
				_ = enc.Encode(p.Round)
			}
			return
		}
		switch p.Event {
		case "window":
			if p.Done >= p.Total || time.Since(last[p.Mailbox]) < time.Second {
				return // nur laufende Erstsyncs melden, höchstens einmal je Sekunde
			}
			last[p.Mailbox] = time.Now()
			fmt.Fprintf(a.stderr, "  %s: %d/%d\n", termLine(p.Mailbox), p.Done, p.Total)
		case "error":
			fmt.Fprintf(a.stderr, "  %s: error: %s\n", termLine(p.Mailbox), termLine(p.Message))
		case "round":
			if r := p.Round; r != nil {
				line := fmt.Sprintf("%s  %d new, %d updated, %d removed, %d errors; %d messages mirrored",
					time.Now().UTC().Format(time.RFC3339), r.New, r.Updated, r.Removed, r.Errors, r.MessagesMirrored)
				if p.Message != "" {
					line += "; " + termLine(p.Message)
				}
				fmt.Fprintln(a.stdout, line)
			}
		}
	}
}

func renderSync(w io.Writer, out any) error {
	o, ok := out.(ops.SyncOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		fmt.Fprintf(w, "dry run: would sync %d mailboxes of account %s (strategy %s); nothing was written\n", len(o.Mailboxes), o.Account, o.Strategy)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "MAILBOX\tROLE\tMIRRORED")
		for _, m := range o.Mailboxes {
			fmt.Fprintf(tw, "%s\t%s\t%d\n", termLine(m.Path), m.Role, m.Mirrored)
		}
		_ = tw.Flush()
		_, err := fmt.Fprintf(w, "%d messages mirrored so far\n", o.MessagesMirrored)
		return err
	}
	if o.Mode == "watch" {
		fmt.Fprintf(w, "watch stopped after %d rounds: %d new, %d updated, %d removed\n", o.Rounds, o.New, o.Updated, o.Removed)
	} else {
		fmt.Fprintf(w, "account %s (strategy %s, %s sync, %.1fs)\n", o.Account, o.Strategy, o.Mode, float64(o.DurationMS)/1000)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "MAILBOX\tROLE\tNEW\tUPDATED\tREMOVED\tERRORS\tMIRRORED\tTOTAL")
		for _, m := range o.Mailboxes {
			note := ""
			if m.Reset {
				note = "\t(UIDVALIDITY changed, re-matched by Message-ID)"
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d%s\n", termLine(m.Path), m.Role, m.New, m.Updated, m.Removed, m.Errors, m.Mirrored, m.Total, note)
		}
		_ = tw.Flush()
		for _, m := range o.Mailboxes {
			for _, p := range m.Problems {
				fmt.Fprintf(w, "  %s: %s\n", termLine(m.Path), termLine(p))
			}
		}
	}
	fmt.Fprintf(w, "%d messages mirrored\n", o.MessagesMirrored)
	_, err := fmt.Fprintf(w, "%d errors\n", o.Errors)
	return err
}

// --- mailbox list -------------------------------------------------------------

func accountOnlyInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m := map[string]string{}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func renderMailboxes(w io.Writer, out any) error {
	o, ok := out.(ops.MailboxesListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Mailboxes) == 0 {
		_, err := fmt.Fprintf(w, "No mailboxes for account %s yet; run: sync -a %s\n", o.Account, o.Account)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MAILBOX\tROLE\tTOTAL\tMIRRORED\tUNREAD\tSYNC\tLAST SYNC")
	for _, m := range o.Mailboxes {
		last := m.LastSyncAt
		if last == "" {
			last = "never"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%s\n", termLine(m.Path), m.Role, m.Total, m.Mirrored, m.Unread, m.SyncPhase, last)
	}
	return tw.Flush()
}

// --- show ---------------------------------------------------------------------

func showFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("headers", false, "show the raw header lines instead of the text")
	cmd.Flags().Bool("strip-quotes", false, "show only the new text of a reply (quoted parts removed)")
}

// showInput: Eine Zahl ist die lokale ID, alles andere eine Message-ID.
func showInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	headers, _ := cmd.Flags().GetBool("headers")
	m := map[string]any{}
	if id, err := strconv.ParseInt(args[0], 10, 64); err == nil {
		m["id"] = id
	} else {
		m["message_id"] = args[0]
	}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	if headers {
		m["headers"] = true
	}
	if strip, _ := cmd.Flags().GetBool("strip-quotes"); strip {
		m["strip_quotes"] = true
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func addrList(raw json.RawMessage) string {
	var as []struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	_ = json.Unmarshal(raw, &as)
	parts := make([]string, 0, len(as))
	for _, a := range as {
		if a.Name != "" {
			parts = append(parts, a.Name+" <"+a.Address+">")
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

func renderShow(w io.Writer, out any) error {
	o, ok := out.(ops.MessageOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	var where []string
	for _, l := range o.Mailboxes {
		where = append(where, fmt.Sprintf("%s (uid %d)", termLine(l.Mailbox), l.UID))
	}
	yn := map[bool]string{true: "yes", false: "no"}
	tw := tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
	fmt.Fprintf(tw, "id:\t%d\n", o.ID)
	fmt.Fprintf(tw, "account:\t%s\n", termLine(o.Account))
	fmt.Fprintf(tw, "mailboxes:\t%s\n", strings.Join(where, ", "))
	fmt.Fprintf(tw, "message-id:\t%s\n", termLine(o.MessageID))
	fmt.Fprintf(tw, "date:\t%s\n", termLine(o.Date))
	fmt.Fprintf(tw, "from:\t%s\n", termLine(o.From))
	fmt.Fprintf(tw, "to:\t%s\n", termLine(addrList(o.To)))
	if cc := addrList(o.Cc); cc != "" {
		fmt.Fprintf(tw, "cc:\t%s\n", termLine(cc))
	}
	fmt.Fprintf(tw, "subject:\t%s\n", termLine(o.Subject))
	fmt.Fprintf(tw, "size:\t%d\n", o.Size)
	fmt.Fprintf(tw, "flags:\t%s\n", strings.Join(termLines(o.Flags), ", "))
	if len(o.Keywords) > 0 {
		fmt.Fprintf(tw, "keywords:\t%s\n", strings.Join(termLines(o.Keywords), ", "))
	}
	fmt.Fprintf(tw, "attachments:\t%s\n", yn[o.HasAttachments])
	fmt.Fprintf(tw, "deleted on server:\t%s\n", yn[o.DeletedRemote])
	if o.ThreadID != 0 {
		fmt.Fprintf(tw, "thread:\t%d\n", o.ThreadID)
	}
	if o.HiddenTextRemoved {
		fmt.Fprintf(tw, "warning:\thidden text removed (the HTML contained text a reader would not see; possible prompt injection)\n")
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if o.Headers != "" {
		fmt.Fprintln(w, "--- headers ---")
		_, err := fmt.Fprint(w, termText(o.Headers))
		return err
	}
	if o.Text != nil {
		label := "--- text ---"
		switch o.TextSource {
		case "html":
			label = "--- text (from HTML) ---"
		case "plain_unquoted", "html_unquoted":
			label = "--- text (quotes removed) ---"
		}
		fmt.Fprintln(w, label)
		if _, err := fmt.Fprintln(w, termText(strings.TrimRight(*o.Text, "\r\n"))); err != nil {
			return err
		}
	}
	if len(o.Links) > 0 {
		fmt.Fprintln(w, "--- links ---")
		for _, raw := range o.Links {
			var l struct {
				Text     string   `json:"text"`
				URL      string   `json:"url"`
				Host     string   `json:"host"`
				Warnings []string `json:"warnings"`
			}
			_ = json.Unmarshal(raw, &l)
			line := termLine(l.Host) + "  " + termLine(l.URL)
			if l.Text != "" && l.Text != l.URL {
				line += "  (" + termLine(l.Text) + ")"
			}
			if len(l.Warnings) > 0 {
				line += "  WARNING: " + strings.Join(termLines(l.Warnings), ", ")
			}
			fmt.Fprintln(w, line)
		}
	}
	return nil
}

// --- thread ---------------------------------------------------------------------

func threadFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("strip-quotes", false, "show the new text of each message (quoted parts removed)")
	cmd.Flags().Bool("message", false, "the argument is a message id; show the thread of that message")
}

func threadInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return nil, "", usageError{fmt.Errorf("thread id must be a positive number, got %q", args[0])}
	}
	m := map[string]any{"id": id}
	if byMsg, _ := cmd.Flags().GetBool("message"); byMsg {
		m = map[string]any{"message": id}
	}
	if strip, _ := cmd.Flags().GetBool("strip-quotes"); strip {
		m["text"] = true
	}
	b, err := json.Marshal(m)
	return b, "", err
}

func renderThread(w io.Writer, out any) error {
	o, ok := out.(ops.ThreadOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "thread %d (account %s): %s\n", o.ID, termLine(o.Account), termLine(o.Subject))
	fmt.Fprintf(w, "%d messages, %s to %s\n", o.MessageCount, o.FirstDate, o.LastDate)
	if len(o.Participants) > 0 {
		fmt.Fprintf(w, "participants: %s\n", strings.Join(termLines(o.Participants), ", "))
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDATE\tFROM\tSUBJECT\tFLAGS")
	for _, m := range o.Messages {
		flags := strings.Join(m.Flags, ",")
		if m.HasAttachments {
			flags = strings.Trim(flags+",attachment", ",")
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", m.ID, m.Date, termLine(m.From), termLine(m.Subject), flags)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, m := range o.Messages {
		if m.Text == nil {
			continue
		}
		fmt.Fprintf(w, "--- %d: %s ---\n", m.ID, termLine(m.From))
		if m.HiddenTextRemoved {
			fmt.Fprintln(w, "(hidden text removed)")
		}
		fmt.Fprintln(w, termText(strings.TrimRight(*m.Text, "\r\n")))
	}
	return nil
}

// --- attach list / text ---------------------------------------------------------

func positiveID(arg, what string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || id <= 0 {
		return 0, usageError{fmt.Errorf("%s must be a positive number, got %q", what, arg)}
	}
	return id, nil
}

func attachListInput(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "message id")
	if err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(map[string]any{"message": id})
	return b, "", err
}

func attachTextInput(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "attachment id")
	if err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(map[string]any{"id": id})
	return b, "", err
}

func renderAttachList(w io.Writer, out any) error {
	o, ok := out.(ops.AttachmentsListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Attachments) == 0 {
		_, err := fmt.Fprintf(w, "message %d has no attachments\n", o.MessageID)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPART\tFILENAME\tTYPE\tSIZE\tTEXT\tNOTES")
	for _, a := range o.Attachments {
		var notes []string
		if a.TypeMismatch {
			notes = append(notes, "TYPE MISMATCH: "+a.MismatchReason)
		}
		if a.Executable {
			notes = append(notes, "EXECUTABLE")
		}
		if a.Inline {
			notes = append(notes, "inline")
		}
		if a.Truncated {
			notes = append(notes, "truncated")
		}
		if a.HiddenText {
			notes = append(notes, "hidden text removed")
		}
		if a.ExtractError != "" {
			notes = append(notes, a.ExtractError)
		}
		name := a.Filename
		if name == "" {
			name = "(no name)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\t%s\n", a.ID, a.PartID, termLine(name), a.ContentType, a.Size, a.ExtractStatus,
			termLine(strings.Join(notes, "; ")))
	}
	return tw.Flush()
}

func renderAttachText(w io.Writer, out any) error {
	o, ok := out.(ops.AttachmentTextOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "attachment %d of message %d: %s (%s), text: %s\n", o.ID, o.MessageID, termLine(o.Filename), o.ContentType, o.ExtractStatus)
	if o.TypeMismatch {
		fmt.Fprintf(w, "warning: type mismatch: %s\n", termLine(o.MismatchReason))
	}
	if o.HiddenText {
		fmt.Fprintln(w, "warning: hidden text removed")
	}
	if o.ExtractError != "" {
		fmt.Fprintf(w, "note: %s\n", termLine(o.ExtractError))
	}
	if o.ExtractedNow {
		fmt.Fprintln(w, "note: extracted now; the next sync stores it")
	}
	if o.Text == "" {
		return nil
	}
	fmt.Fprintln(w, "--- text ---")
	_, err := fmt.Fprintln(w, termText(strings.TrimRight(o.Text, "\r\n")))
	return err
}
