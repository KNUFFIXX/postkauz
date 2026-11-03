package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Fernziele für mcp --remote (A04 Teil 4): remote add|list|remove|test.
// Nur für Menschen; der Schlüssel kommt über die Standardeingabe oder eine
// verdeckte Abfrage, nie als Argument.
func init() {
	bindings = append(bindings, remoteBindings...)
}

var remoteBindings = []binding{
	{op: "remote.add", path: []string{"remote", "add"}, use: "<name> --url <https-url> [--key-stdin]", args: cobra.ExactArgs(1),
		flags: remoteAddFlags, input: remoteAddInput, human: renderRemoteAdd, covers: []string{"name", "url"}},
	{op: "remote.list", path: []string{"remote", "list"}, args: cobra.NoArgs, human: renderRemoteList},
	{op: "remote.remove", path: []string{"remote", "remove"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderRemoteRemove, covers: []string{"name"}},
	{op: "remote.test", path: []string{"remote", "test"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderRemoteTest, covers: []string{"name"}},
}

func remoteAddFlags(cmd *cobra.Command) {
	cmd.Flags().String("url", "", "https URL of the MCP endpoint on the other PC, e.g. https://office-pc.example.ts.net/mcp")
	cmd.Flags().Bool("key-stdin", false, "read the API key from standard input instead of prompting (never as an argument)")
}

func remoteAddInput(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	u, _ := f.GetString("url")
	if strings.TrimSpace(u) == "" {
		return nil, "", usageError{errors.New("remote add needs --url, e.g. --url https://office-pc.example.ts.net/mcp")}
	}
	fromStdin, _ := f.GetBool("key-stdin")
	asJSON, _ := f.GetBool("json")
	var key secrets.Secret
	switch {
	case fromStdin:
		line, err := bufio.NewReader(a.stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, "", err
		}
		key = secrets.Secret(strings.TrimSpace(line))
		if key.IsZero() {
			return nil, "", usageError{errors.New("empty key on standard input")}
		}
	case asJSON:
		return nil, "", usageError{errors.New("no interactive prompt in --json mode; use --key-stdin")}
	default:
		var err error
		if key, err = readHiddenPassword(cmd.Context(), a, "API key for remote "+args[0]+": "); err != nil {
			return nil, "", err
		}
	}
	b, err := json.Marshal(ops.RemoteAddInput{Name: args[0], URL: strings.TrimSpace(u)})
	return b, key, err
}

func renderRemoteAdd(w io.Writer, out any) error {
	o, ok := out.(ops.RemoteAddOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	switch o.Action {
	case "would_add", "would_change":
		fmt.Fprintf(w, "dry run: would %s remote %s (%s); key into %s (%s); nothing changed\n",
			strings.TrimPrefix(o.Action, "would_"), o.Remote, termLine(o.URL), o.CredentialRef, o.SecretStore)
	default:
		fmt.Fprintf(w, "remote %s %s: %s\nkey: stored in %s (%s), read back and compared\n", o.Remote, o.Action, termLine(o.URL), o.CredentialRef, o.SecretStore)
		if o.ConfigFile != "" {
			fmt.Fprintf(w, "configuration: %s\n", termLine(o.ConfigFile))
		}
		fmt.Fprintf(w, "next: postkauz remote test %s\n", o.Remote)
	}
	for _, n := range o.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	return nil
}

func renderRemoteList(w io.Writer, out any) error {
	o, ok := out.(ops.RemoteListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Remotes) == 0 {
		_, err := fmt.Fprintln(w, "no remotes; add one with: remote add <name> --url https://<office-pc>.<tailnet>.ts.net/mcp --key-stdin")
		return err
	}
	fmt.Fprintf(w, "%-20s %-8s %s\n", "NAME", "KEY", "URL")
	for _, r := range o.Remotes {
		fmt.Fprintf(w, "%-20s %-8s %s\n", r.Remote, r.Key, termLine(r.URL))
	}
	return nil
}

func renderRemoteRemove(w io.Writer, out any) error {
	o, ok := out.(ops.RemoteRemoveOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	what := []string{}
	if o.ConfigRemoved {
		what = append(what, "its section in the configuration")
	}
	if o.KeyRemoved {
		what = append(what, "its key in the keyring")
	}
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would remove remote %s (%s)\n", o.Remote, strings.Join(what, " and "))
		return err
	}
	_, err := fmt.Fprintf(w, "remote %s removed (%s); MCP clients that start mcp --remote %s now get an error\n", o.Remote, strings.Join(what, " and "), o.Remote)
	return err
}

func renderRemoteTest(w io.Writer, out any) error {
	o, ok := out.(ops.RemoteTestOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "remote:   %s (%s)\nserver:   %s %s, protocol %s\ntools:    %d\n", o.Remote, termLine(o.URL),
		termLine(o.ServerName), termLine(o.ServerVersion), termLine(o.ProtocolVersion), o.Tools)
	if o.Role != "" || o.KeyName != "" {
		fmt.Fprintf(w, "key:      %s (role %s)\n", termLine(o.KeyName), termLine(o.Role))
	}
	_, err := fmt.Fprintln(w, "ok: mcp --remote "+o.Remote+" reaches the server")
	return err
}
