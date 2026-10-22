package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// draft style (S2.8): die Stilhinweise einer Identität, wie ein Agent sie
// über die Resource mail://style/{identity} liest.
func init() {
	bindings = append(bindings, binding{op: "drafts.style", path: []string{"draft", "style"}, use: "[<identity address|id>]",
		args: cobra.MaximumNArgs(1), flags: draftStyleFlags, input: draftStyleInput, human: renderDraftStyle,
		covers: []string{"identity", "account", "max_chars"}})
}

func draftStyleFlags(cmd *cobra.Command) {
	cmd.Flags().Int("max-chars", 0, "cut the text after this many characters (default: [agent_defaults] max_chars of the policy file)")
}

func draftStyleInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	m := map[string]any{}
	if len(args) == 1 {
		m["identity"] = args[0]
	}
	if v := accountFlag(cmd); v != "" {
		m["account"] = v
	}
	if cmd.Flags().Changed("max-chars") {
		m["max_chars"], _ = cmd.Flags().GetInt("max-chars")
	}
	return marshalInput(m)
}

func renderDraftStyle(w io.Writer, out any) error {
	o, ok := out.(ops.DraftsStyleOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if !o.HasStyleFile {
		_, err := fmt.Fprintf(w, "identity %d (%s) of account %s has no style file; set one with: identity add --style-file <file>\n",
			o.IdentityID, termLine(o.Identity), o.Account)
		return err
	}
	fmt.Fprintf(w, "style of identity %d (%s) in account %s:\n\n", o.IdentityID, termLine(o.Identity), o.Account)
	text := strings.TrimRight(termText(o.Text), "\r\n")
	if o.Truncated {
		text += " […]"
	}
	_, err := fmt.Fprintf(w, "%s\n", text)
	return err
}
