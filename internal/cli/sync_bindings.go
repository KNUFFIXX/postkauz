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
			fmt.Fprintf(a.stderr, "  %s: %d/%d\n", p.Mailbox, p.Done, p.Total)
		case "error":
			fmt.Fprintf(a.stderr, "  %s: error: %s\n", p.Mailbox, p.Message)
		case "round":
			if r := p.Round; r != nil {
				line := fmt.Sprintf("%s  %d new, %d updated, %d removed, %d errors; %d messages mirrored",
					time.Now().UTC().Format(time.RFC3339), r.New, r.Updated, r.Removed, r.Errors, r.MessagesMirrored)
				if p.Message != "" {
					line += "; " + p.Message
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
			fmt.Fprintf(tw, "%s\t%s\t%d\n", m.Path, m.Role, m.Mirrored)
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
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d%s\n", m.Path, m.Role, m.New, m.Updated, m.Removed, m.Errors, m.Mirrored, m.Total, note)
		}
		_ = tw.Flush()
		for _, m := range o.Mailboxes {
			for _, p := range m.Problems {
				fmt.Fprintf(w, "  %s: %s\n", m.Path, p)
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
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%s\n", m.Path, m.Role, m.Total, m.Mirrored, m.Unread, m.SyncPhase, last)
	}
	return tw.Flush()
}

// --- show ---------------------------------------------------------------------

func showFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("headers", false, "show the raw header lines instead of the text")
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
		where = append(where, fmt.Sprintf("%s (uid %d)", l.Mailbox, l.UID))
	}
	yn := map[bool]string{true: "yes", false: "no"}
	tw := tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
	fmt.Fprintf(tw, "id:\t%d\n", o.ID)
	fmt.Fprintf(tw, "account:\t%s\n", o.Account)
	fmt.Fprintf(tw, "mailboxes:\t%s\n", strings.Join(where, ", "))
	fmt.Fprintf(tw, "message-id:\t%s\n", o.MessageID)
	fmt.Fprintf(tw, "date:\t%s\n", o.Date)
	fmt.Fprintf(tw, "from:\t%s\n", o.From)
	fmt.Fprintf(tw, "to:\t%s\n", addrList(o.To))
	if cc := addrList(o.Cc); cc != "" {
		fmt.Fprintf(tw, "cc:\t%s\n", cc)
	}
	fmt.Fprintf(tw, "subject:\t%s\n", o.Subject)
	fmt.Fprintf(tw, "size:\t%d\n", o.Size)
	fmt.Fprintf(tw, "flags:\t%s\n", strings.Join(o.Flags, ", "))
	if len(o.Keywords) > 0 {
		fmt.Fprintf(tw, "keywords:\t%s\n", strings.Join(o.Keywords, ", "))
	}
	fmt.Fprintf(tw, "attachments:\t%s\n", yn[o.HasAttachments])
	fmt.Fprintf(tw, "deleted on server:\t%s\n", yn[o.DeletedRemote])
	if err := tw.Flush(); err != nil {
		return err
	}
	if o.Headers != "" {
		fmt.Fprintln(w, "--- headers ---")
		_, err := fmt.Fprint(w, o.Headers)
		return err
	}
	if o.Text != nil {
		fmt.Fprintln(w, "--- text ---")
		_, err := fmt.Fprintln(w, strings.TrimRight(*o.Text, "\r\n"))
		return err
	}
	return nil
}
