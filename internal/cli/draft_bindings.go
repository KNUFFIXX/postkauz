package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Entwürfe, Freigaben und Identitäten (S2.3). Grundform der Kommandos;
// S2.7 baut sie aus (A02 S2.7: draft show --eml, weitere Optionen). Die
// Bindungen sind ausdrücklich geschrieben (ADR-004); eine Operation darf
// mehrere Kommandos haben (drafts.create: draft new|reply|forward), die
// zusammen alle Felder ihres Schemas erreichen.
func init() {
	bindings = append(bindings, draftBindings...)
}

var draftBindings = []binding{
	{op: "drafts.create", path: []string{"draft", "new"}, args: cobra.NoArgs, flags: draftNewFlags, input: draftCreateInput("new"),
		human:  renderDraftWrite,
		covers: []string{"account", "kind", "identity", "to", "cc", "bcc", "subject", "body_markdown", "body_html", "attachments", "signature"}},
	{op: "drafts.create", path: []string{"draft", "reply"}, use: "<message-id|Message-ID>", args: cobra.ExactArgs(1), flags: draftReplyFlags,
		input: draftCreateInput("reply"), human: renderDraftWrite,
		covers: []string{"account", "kind", "in_reply_to", "in_reply_to_message_id", "identity", "to", "cc", "bcc", "subject",
			"body_markdown", "body_html", "attachments", "quote", "signature"}},
	{op: "drafts.create", path: []string{"draft", "forward"}, use: "<message-id|Message-ID>", args: cobra.ExactArgs(1), flags: draftForwardFlags,
		input: draftCreateInput("forward"), human: renderDraftWrite,
		covers: []string{"account", "kind", "in_reply_to", "in_reply_to_message_id", "identity", "to", "cc", "bcc", "subject",
			"body_markdown", "body_html", "attachments", "signature", "include_attachments"}},
	{op: "drafts.update", path: []string{"draft", "edit"}, use: "<draft-id>", args: cobra.ExactArgs(1), flags: draftEditFlags,
		input: draftEditInput, human: renderDraftWrite,
		covers: []string{"draft_id", "to", "cc", "bcc", "subject", "body_markdown", "body_html", "attachments", "quote", "signature"}},
	{op: "drafts.get", path: []string{"draft", "show"}, use: "<draft-id>", args: cobra.ExactArgs(1), input: draftIDArg,
		human: renderDraftShow, covers: []string{"draft_id"}},
	{op: "drafts.list", path: []string{"draft", "list"}, args: cobra.NoArgs, flags: draftListFlags, input: draftListInput,
		human: renderDraftList, covers: []string{"account", "status", "limit"}},
	{op: "drafts.request_ready", path: []string{"draft", "request"}, use: "<draft-id>", args: cobra.ExactArgs(1), input: draftIDArg,
		human: renderDraftRequest, covers: []string{"draft_id"}},
	{op: "drafts.abandon", path: []string{"draft", "abandon"}, use: "<draft-id>", args: cobra.ExactArgs(1), input: draftIDArg,
		human: renderDraftClose, covers: []string{"draft_id"}},
	{op: "drafts.mark_sent", path: []string{"draft", "mark-sent"}, use: "<draft-id>", args: cobra.ExactArgs(1), input: draftIDArg,
		human: renderDraftClose, covers: []string{"draft_id"}},
	{op: "drafts.approve", path: []string{"approve"}, use: "<approval-id>", args: cobra.ExactArgs(1), input: approvalIDArg,
		human: renderDecide, covers: []string{"approval_id"}},
	{op: "drafts.reject", path: []string{"reject"}, use: "<approval-id> [--reason TEXT]", args: cobra.ExactArgs(1), flags: rejectFlags,
		input: rejectInput, human: renderDecide, covers: []string{"approval_id", "reason"}},
	{op: "approvals.get", path: []string{"approval", "show"}, use: "<approval-id>", args: cobra.ExactArgs(1), input: approvalIDArg,
		human: renderApproval, covers: []string{"approval_id"}},
	{op: "identities.add", path: []string{"identity", "add"}, args: cobra.NoArgs, flags: identityAddFlags, input: identityAddInput,
		human:  renderIdentityResult,
		covers: []string{"account", "name", "email", "reply_to", "signature_text", "signature_html", "style_file", "reply_prefix", "forward_prefix", "default"}},
	{op: "identities.list", path: []string{"identity", "list"}, args: cobra.NoArgs, input: accountOnlyInput, human: renderIdentities,
		covers: []string{"account"}},
	{op: "identities.set_default", path: []string{"identity", "set-default"}, use: "<identity-id>", args: cobra.ExactArgs(1),
		input: identityIDArg, human: renderIdentityResult, covers: []string{"id"}},
	{op: "identities.remove", path: []string{"identity", "remove"}, use: "<identity-id>", args: cobra.ExactArgs(1),
		input: identityIDArg, human: renderIdentityResult, covers: []string{"id"}},
}

// maxInputFile begrenzt Text aus Dateien (Markdown, HTML, Signatur); die
// Operation prüft danach ihre eigene Grenze (200.000 Zeichen).
const maxInputFile = 4 << 20

// readInputFile liest eine Textdatei des Menschen; "-" ist die
// Standardeingabe. Der Inhalt geht in die Eingabe der Operation, nie in
// Fehlermeldungen.
func readInputFile(a *app, flag, path string) (string, error) {
	var r io.Reader
	if path == "-" {
		r = a.stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			// Die Meldung des Betriebssystems nennt den Pfad; der Dateiname
			// kommt nie in Fehler oder Log (B1).
			if errors.Is(err, fs.ErrNotExist) {
				return "", usageError{fmt.Errorf("--%s: file not found", flag)}
			}
			return "", usageError{fmt.Errorf("--%s: cannot open the file", flag)}
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxInputFile+1))
	if err != nil {
		return "", usageError{fmt.Errorf("--%s: cannot read the file", flag)}
	}
	if len(b) > maxInputFile {
		return "", usageError{fmt.Errorf("--%s: file larger than %d bytes", flag, maxInputFile)}
	}
	return string(b), nil
}

func marshalInput(m map[string]any) (json.RawMessage, secrets.Secret, error) {
	b, err := json.Marshal(m)
	return b, "", err
}

// --- draft new|reply|forward|edit ------------------------------------------------

// draftContentFlags sind die Optionen für Empfänger, Betreff, Text und
// Anhänge (new, reply, forward, edit).
func draftContentFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringArray("to", nil, "recipient, \"Name <address>\" or address; repeat for more")
	f.StringArray("cc", nil, "copy recipient; repeat for more")
	f.StringArray("bcc", nil, "blind copy recipient; repeat for more")
	f.String("subject", "", "subject (reply/forward: default is the original subject with Re:/Fwd:)")
	f.String("body-file", "", "text of the draft as Markdown from this file (- = standard input)")
	f.String("html-file", "", "text of the draft as HTML from this file (- = standard input), instead of --body-file")
	f.StringArray("attach", nil, "attachment: an attachment id (attach list) or a file below attach_dir; repeat for more")
}

func draftNewFlags(cmd *cobra.Command) {
	draftContentFlags(cmd)
	cmd.Flags().String("identity", "", "sender identity (address or id; default: the default identity of the account)")
	cmd.Flags().Bool("no-signature", false, "do not append the signature of the identity")
}

func draftReplyFlags(cmd *cobra.Command) {
	draftNewFlags(cmd)
	cmd.Flags().Bool("all", false, "reply to all (others of To/Cc in Cc, without own addresses)")
	cmd.Flags().Bool("no-quote", false, "do not quote the original message")
}

func draftForwardFlags(cmd *cobra.Command) {
	draftNewFlags(cmd)
	cmd.Flags().Bool("no-attachments", false, "do not include the attachments of the original message")
}

func draftEditFlags(cmd *cobra.Command) {
	draftContentFlags(cmd)
	cmd.Flags().Bool("no-attachments", false, "remove all attachments")
	cmd.Flags().Bool("quote", true, "quote the original message (--quote=false removes the quote)")
	cmd.Flags().Bool("signature", true, "append the signature (--signature=false removes it)")
}

// addrFlag liest eine wiederholbare Adressoption; leere Einträge fallen weg
// (--cc "" leert die Liste bei draft edit).
func addrFlag(cmd *cobra.Command, name string) []string {
	v, _ := cmd.Flags().GetStringArray(name)
	out := []string{}
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// attachFlag: eine Zahl ist die ID eines Anhangs im Spiegel, sonst ein
// Dateipfad unter attach_dir.
func attachFlag(cmd *cobra.Command) []map[string]any {
	v, _ := cmd.Flags().GetStringArray("attach")
	out := []map[string]any{}
	for _, s := range v {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			out = append(out, map[string]any{"attachment_id": n})
		} else {
			out = append(out, map[string]any{"file": s})
		}
	}
	return out
}

// draftContent setzt Empfänger, Betreff, Text und Anhänge (nur genannte
// Optionen; bei edit ersetzt jede genannte den gespeicherten Wert).
func draftContent(cmd *cobra.Command, a *app, m map[string]any) error {
	f := cmd.Flags()
	for _, n := range []string{"to", "cc", "bcc"} {
		if f.Changed(n) {
			m[n] = addrFlag(cmd, n)
		}
	}
	if f.Changed("subject") {
		m["subject"], _ = f.GetString("subject")
	}
	md, html := f.Changed("body-file"), f.Changed("html-file")
	if md && html {
		return usageError{errors.New("use either --body-file (Markdown) or --html-file (HTML)")}
	}
	for _, x := range []struct{ flag, field string }{{"body-file", "body_markdown"}, {"html-file", "body_html"}} {
		if f.Changed(x.flag) {
			p, _ := f.GetString(x.flag)
			text, err := readInputFile(a, x.flag, p)
			if err != nil {
				return err
			}
			m[x.field] = text
		}
	}
	if f.Changed("attach") {
		m["attachments"] = attachFlag(cmd)
	}
	return nil
}

// draftCreateInput baut die Eingabe von drafts.create für draft new,
// reply und forward.
func draftCreateInput(verb string) func(*cobra.Command, []string, *app) (json.RawMessage, secrets.Secret, error) {
	return func(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error) {
		f := cmd.Flags()
		m := map[string]any{"kind": verb}
		if v := accountFlag(cmd); v != "" {
			m["account"] = v
		}
		if verb != "new" {
			if n, err := strconv.ParseInt(args[0], 10, 64); err == nil {
				m["in_reply_to"] = n
			} else {
				m["in_reply_to_message_id"] = args[0]
			}
		}
		if all, _ := f.GetBool("all"); verb == "reply" && all {
			m["kind"] = "reply_all"
		}
		if v, _ := f.GetString("identity"); v != "" {
			m["identity"] = v
		}
		if err := draftContent(cmd, a, m); err != nil {
			return nil, "", err
		}
		if no, _ := f.GetBool("no-signature"); no {
			m["signature"] = false
		}
		if no, _ := f.GetBool("no-quote"); no {
			m["quote"] = false
		}
		if no, _ := f.GetBool("no-attachments"); no {
			m["include_attachments"] = false
		}
		return marshalInput(m)
	}
}

func draftEditInput(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "draft id")
	if err != nil {
		return nil, "", err
	}
	f := cmd.Flags()
	m := map[string]any{"draft_id": id}
	if err := draftContent(cmd, a, m); err != nil {
		return nil, "", err
	}
	if no, _ := f.GetBool("no-attachments"); no {
		if f.Changed("attach") {
			return nil, "", usageError{errors.New("use either --attach or --no-attachments")}
		}
		m["attachments"] = []any{}
	}
	for _, n := range []string{"quote", "signature"} {
		if f.Changed(n) {
			m[n], _ = f.GetBool(n)
		}
	}
	if len(m) == 1 {
		return nil, "", usageError{errors.New("nothing to change; see draft edit --help")}
	}
	return marshalInput(m)
}

func draftIDArg(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "draft id")
	if err != nil {
		return nil, "", err
	}
	return marshalInput(map[string]any{"draft_id": id})
}

func approvalIDArg(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "approval id")
	if err != nil {
		return nil, "", err
	}
	return marshalInput(map[string]any{"approval_id": id})
}

func identityIDArg(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "identity id")
	if err != nil {
		return nil, "", err
	}
	return marshalInput(map[string]any{"id": id})
}

// --- draft list, reject ------------------------------------------------------------

func draftListFlags(cmd *cobra.Command) {
	cmd.Flags().StringArray("status", nil, "only drafts in this status (draft, pending_approval, ready, sent_by_human, abandoned); repeat for more")
	cmd.Flags().Int("limit", 0, "at most this many drafts (default 100, at most 500)")
}

func draftListInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m := map[string]any{}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	if cmd.Flags().Changed("status") {
		var st []string
		v, _ := cmd.Flags().GetStringArray("status")
		for _, s := range v {
			for _, p := range strings.Split(s, ",") {
				if p = strings.TrimSpace(p); p != "" {
					st = append(st, p)
				}
			}
		}
		m["status"] = st
	}
	if cmd.Flags().Changed("limit") {
		m["limit"], _ = cmd.Flags().GetInt("limit")
	}
	return marshalInput(m)
}

func rejectFlags(cmd *cobra.Command) {
	cmd.Flags().String("reason", "", "why; kept with the approval (not in the audit log)")
}

func rejectInput(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error) {
	id, err := positiveID(args[0], "approval id")
	if err != nil {
		return nil, "", err
	}
	m := map[string]any{"approval_id": id}
	if cmd.Flags().Changed("reason") {
		m["reason"], _ = cmd.Flags().GetString("reason")
	}
	return marshalInput(m)
}

// --- identity add ------------------------------------------------------------------

func identityAddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("email", "", "sender address")
	f.String("name", "", "sender name")
	f.String("reply-to", "", "reply-to address (optional)")
	f.String("signature-file", "", "signature as plain text from this file")
	f.String("signature-html-file", "", "signature as HTML from this file (cleaned like every draft)")
	f.String("style-file", "", "file with notes on tone and style for agents")
	f.String("reply-prefix", "", "subject prefix of replies (default \"Re: \")")
	f.String("forward-prefix", "", "subject prefix of forwards (default \"Fwd: \")")
	f.Bool("default", false, "make it the default identity of the account")
	_ = cmd.MarkFlagRequired("email")
}

func identityAddInput(cmd *cobra.Command, _ []string, a *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	m := map[string]any{}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	for _, x := range []struct{ flag, field string }{{"email", "email"}, {"name", "name"}, {"reply-to", "reply_to"},
		{"style-file", "style_file"}, {"reply-prefix", "reply_prefix"}, {"forward-prefix", "forward_prefix"}} {
		if f.Changed(x.flag) {
			m[x.field], _ = f.GetString(x.flag)
		}
	}
	for _, x := range []struct{ flag, field string }{{"signature-file", "signature_text"}, {"signature-html-file", "signature_html"}} {
		if f.Changed(x.flag) {
			p, _ := f.GetString(x.flag)
			text, err := readInputFile(a, x.flag, p)
			if err != nil {
				return nil, "", err
			}
			m[x.field] = strings.TrimRight(text, "\r\n")
		}
	}
	if def, _ := f.GetBool("default"); def {
		m["default"] = true
	}
	return marshalInput(m)
}

// --- Ausgaben ohne --json ------------------------------------------------------------

func addrLine(l []string) string {
	if len(l) == 0 {
		return "-"
	}
	return termLine(strings.Join(l, ", "))
}

func renderDraftWrite(w io.Writer, out any) error {
	o, ok := out.(ops.DraftsWriteOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	kind := strings.ReplaceAll(o.Kind, "_", " ")
	switch {
	case o.DryRun && o.PreviousStatus == "":
		fmt.Fprintf(w, "dry run: would create a draft (%s) in account %s, status draft; nothing saved\n", kind, o.Account)
	case o.DryRun && o.Changed:
		fmt.Fprintf(w, "dry run: would change draft %d (status %s -> %s); nothing saved\n", o.DraftID, o.PreviousStatus, o.Status)
	case o.DryRun:
		fmt.Fprintf(w, "dry run: draft %d would not change (status %s)\n", o.DraftID, o.Status)
	case o.PreviousStatus == "":
		fmt.Fprintf(w, "draft %d created (status %s, remote %s)\n", o.DraftID, o.Status, o.Remote)
	case o.Changed:
		fmt.Fprintf(w, "draft %d changed (status %s -> %s, remote %s)\n", o.DraftID, o.PreviousStatus, o.Status, o.Remote)
	default:
		fmt.Fprintf(w, "draft %d unchanged (status %s)\n", o.DraftID, o.Status)
	}
	if o.ApprovalSuperseded > 0 {
		fmt.Fprintf(w, "approval %d superseded: the draft changed, submit it again with: draft request %d\n", o.ApprovalSuperseded, o.DraftID)
	}
	if o.MessageID != "" {
		fmt.Fprintf(w, "message-id:  <%s>\n", termLine(o.MessageID))
	}
	p := o.Preview
	fmt.Fprintf(w, "from:        %s\nto:          %s\n", termLine(p.From), addrLine(p.To))
	if len(p.Cc) > 0 {
		fmt.Fprintf(w, "cc:          %s\n", addrLine(p.Cc))
	}
	if len(p.Bcc) > 0 {
		fmt.Fprintf(w, "bcc:         %s\n", addrLine(p.Bcc))
	}
	fmt.Fprintf(w, "subject:     %s\n", termLine(p.Subject))
	if p.InReplyTo != "" {
		fmt.Fprintf(w, "in-reply-to: %s\n", termLine(p.InReplyTo))
	}
	text := termText(p.Text)
	if p.TextTruncated {
		text += " […]"
	}
	fmt.Fprintf(w, "\n%s\n\n", strings.TrimRight(text, "\r\n"))
	renderDraftAttachments(w, o.Attachments)
	for _, s := range o.Warnings {
		fmt.Fprintf(w, "warning: %s\n", termLine(s))
	}
	for _, s := range o.Notes {
		fmt.Fprintf(w, "note: %s\n", termLine(s))
	}
	if o.RemoteNote != "" {
		fmt.Fprintf(w, "remote: %s\n", o.RemoteNote)
	}
	_, err := fmt.Fprintf(w, "nothing is sent: a human sends the draft in the mail client\n")
	return err
}

func renderDraftAttachments(w io.Writer, atts []ops.DraftAttachmentOutput) {
	if len(atts) == 0 {
		return
	}
	fmt.Fprintln(w, "attachments:")
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, a := range atts {
		fmt.Fprintf(tw, "  %s\t%s\t%d bytes\n", termLine(a.Filename), a.ContentType, a.Size)
	}
	_ = tw.Flush()
}

func renderDraftShow(w io.Writer, out any) error {
	o, ok := out.(ops.DraftDetail)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "draft %d (%s) in account %s, status %s\n", o.DraftID, strings.ReplaceAll(o.Kind, "_", " "), o.Account, o.Status)
	if o.ApprovalID > 0 {
		fmt.Fprintf(w, "approval:    %d (%s", o.ApprovalID, orDash(o.ApprovalStatus))
		if o.ApprovalExpiresAt != "" {
			fmt.Fprintf(w, ", until %s", o.ApprovalExpiresAt)
		}
		fmt.Fprintln(w, ")")
	}
	if o.HookResult != "" {
		fmt.Fprintf(w, "hook:        %s\n", o.HookResult)
	}
	fmt.Fprintf(w, "remote:      %s", o.Remote)
	if o.RemoteNote != "" {
		fmt.Fprintf(w, " (%s)", o.RemoteNote)
	}
	fmt.Fprintf(w, "\nmessage-id:  <%s>\nfrom:        %s\nto:          %s\n", termLine(o.MessageID), termLine(o.From), addrLine(o.To))
	if len(o.Cc) > 0 {
		fmt.Fprintf(w, "cc:          %s\n", addrLine(o.Cc))
	}
	if len(o.Bcc) > 0 {
		fmt.Fprintf(w, "bcc:         %s\n", addrLine(o.Bcc))
	}
	fmt.Fprintf(w, "subject:     %s\n", termLine(o.Subject))
	if o.InReplyTo != "" {
		fmt.Fprintf(w, "in-reply-to: %s\nreferences:  %s\n", termLine(o.InReplyTo), termLine(o.References))
	}
	fmt.Fprintf(w, "created:     %s by %s, changed %s\n", o.CreatedAt, termLine(o.CreatedBy), o.UpdatedAt)
	fmt.Fprintf(w, "\n%s\n\n", strings.TrimRight(termText(o.BodyText), "\r\n"))
	renderDraftAttachments(w, o.Attachments)
	for _, s := range o.Warnings {
		fmt.Fprintf(w, "warning: %s\n", termLine(s))
	}
	_, err := fmt.Fprintf(w, "preview hash: %s\n", o.PreviewHash)
	return err
}

func renderDraftList(w io.Writer, out any) error {
	o, ok := out.(ops.DraftsListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Drafts) == 0 {
		_, err := fmt.Fprintln(w, "No drafts.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tACCOUNT\tKIND\tSTATUS\tAPPROVAL\tREMOTE\tTO\tATT\tCHANGED\tSUBJECT")
	for _, d := range o.Drafts {
		ap := "-"
		if d.ApprovalID > 0 {
			ap = strconv.FormatInt(d.ApprovalID, 10)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", d.DraftID, d.Account, d.Kind, d.Status, ap, d.Remote, d.Recipients,
			d.Attachments, d.UpdatedAt, termLine(d.Subject))
	}
	return tw.Flush()
}

func renderDraftRequest(w io.Writer, out any) error {
	o, ok := out.(ops.DraftsRequestOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	prefix := ""
	if o.DryRun {
		prefix = "dry run: "
	}
	fmt.Fprintf(w, "%shook: %s\n", prefix, o.Hook)
	if o.HookOutput != "" {
		fmt.Fprintf(w, "%s\n", strings.TrimRight(termText(o.HookOutput), "\r\n"))
		if o.HookOutputTruncated {
			fmt.Fprintln(w, "(hook output cut after the first 4 KB)")
		}
	}
	switch {
	case o.Status == "pending_approval" && o.DryRun:
		_, err := fmt.Fprintf(w, "dry run: draft %d could be submitted (status %s -> pending_approval); nothing saved\n", o.DraftID, o.PreviousStatus)
		return err
	case o.Status == "pending_approval":
		_, err := fmt.Fprintf(w, "draft %d: status pending_approval, approval %d (until %s); a human approves with: approve %d\n",
			o.DraftID, o.ApprovalID, o.ExpiresAt, o.ApprovalID)
		return err
	}
	_, err := fmt.Fprintf(w, "%sdraft %d: status stays %s; fix the draft and request again\n", prefix, o.DraftID, o.Status)
	return err
}

func renderDraftClose(w io.Writer, out any) error {
	o, ok := out.(ops.DraftsCloseOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	msg := fmt.Sprintf("draft %d: %s (was %s)", o.DraftID, o.Status, o.PreviousStatus)
	if o.DryRun {
		msg = fmt.Sprintf("dry run: draft %d would be %s (now %s)", o.DraftID, o.Status, o.PreviousStatus)
	}
	if o.ApprovalSuperseded > 0 {
		msg += fmt.Sprintf("; approval %d superseded", o.ApprovalSuperseded)
	}
	_, err := fmt.Fprintln(w, msg)
	return err
}

func renderDecide(w io.Writer, out any) error {
	o, ok := out.(ops.DraftsDecideOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: approval %d could be decided; draft %d would be %s (now %s)\n", o.ApprovalID, o.DraftID, o.Status, o.PreviousStatus)
		return err
	}
	msg := fmt.Sprintf("approval %d %s by %s: draft %d is %s", o.ApprovalID, o.Decision, termLine(o.DecidedBy), o.DraftID, o.Status)
	if o.Status == "ready" {
		msg += fmt.Sprintf(" (valid until %s); send it in your mail client, postkauz never sends", o.ExpiresAt)
	}
	_, err := fmt.Fprintln(w, msg)
	return err
}

func renderApproval(w io.Writer, out any) error {
	o, ok := out.(ops.ApprovalOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "approval %d: %s (draft %d, status %s)\n%s\nrequested by %s at %s", o.ApprovalID, o.Status, o.DraftID, o.DraftStatus,
		termLine(o.Description), termLine(o.RequestedBy), o.RequestedAt)
	if o.ExpiresAt != "" {
		fmt.Fprintf(w, ", valid until %s", o.ExpiresAt)
	}
	fmt.Fprintln(w)
	if o.DecidedBy != "" {
		fmt.Fprintf(w, "decided by %s at %s\n", termLine(o.DecidedBy), o.DecidedAt)
	}
	if o.Reason != "" {
		fmt.Fprintf(w, "reason: %s\n", termLine(o.Reason))
	}
	return nil
}

func renderIdentityResult(w io.Writer, out any) error {
	o, ok := out.(ops.IdentityResultOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	i := o.Identity
	who := termLine(i.Email)
	if i.Name != "" {
		who = termLine(i.Name) + " <" + who + ">"
	}
	def := ""
	if i.Default {
		def = ", default"
	}
	prefix := "identity"
	if o.DryRun {
		prefix = "dry run: identity"
	}
	_, err := fmt.Fprintf(w, "%s %d of account %s: %s (signature %s%s)\n", prefix, i.ID, i.Account, who, i.Signature, def)
	return err
}

func renderIdentities(w io.Writer, out any) error {
	o, ok := out.(ops.IdentitiesListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Identities) == 0 {
		_, err := fmt.Fprintln(w, "No identities; drafts use the account address. Add one with: identity add --email <address> --name <name>")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tACCOUNT\tDEFAULT\tNAME\tEMAIL\tREPLY-TO\tSIGNATURE\tPREFIXES")
	for _, i := range o.Identities {
		def := "-"
		if i.Default {
			def = "yes"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%q %q\n", i.ID, i.Account, def, orDash(termLine(i.Name)), termLine(i.Email),
			orDash(termLine(i.ReplyTo)), i.Signature, i.ReplyPrefix, i.ForwardPrefix)
	}
	return tw.Flush()
}
