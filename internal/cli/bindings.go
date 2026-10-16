package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// binding verbindet genau eine Operation mit genau einem Kommando.
// Bewusst eine handgeschriebene Tabelle (ADR-004): Namen, Hilfetexte und
// Ausgabeformate sind Entscheidungen, keine Ableitungen. Der Adapter
// übersetzt nur Argumente und Flags in die Eingabe der Operation.
type binding struct {
	op    string   // Name im Register
	path  []string // Kommandopfad, z. B. {"accounts","list"}
	use   string   // Argumente in der Hilfe, z. B. "<name>"
	args  cobra.PositionalArgs
	flags func(cmd *cobra.Command)
	// input baut die Eingabe; Geheimnisse gehen getrennt zurück (nie in Input).
	input func(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error)
	human func(w io.Writer, out any) error
	// jsonOut ersetzt die JSON-Ausgabe (ein Objekt) für Listen, die als
	// NDJSON gestreamt werden (search); nil = ein Objekt.
	jsonOut func(w io.Writer, out any) error
	// progress liefert optional einen Empfänger für Fortschrittsmeldungen.
	progress func(cmd *cobra.Command, a *app) func(ops.Progress)
}

var bindings = []binding{
	{op: "accounts.list", path: []string{"accounts", "list"}, args: cobra.NoArgs, human: renderAccounts},
	{op: "accounts.add", path: []string{"account", "add"}, use: "<name>", args: cobra.ExactArgs(1),
		flags: accountAddFlags, input: accountAddInput, human: renderAccountAdd},
	{op: "accounts.test", path: []string{"account", "test"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderAccountTest},
	{op: "accounts.remove", path: []string{"account", "remove"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderAccountRemove},
	{op: "sync.run", path: []string{"sync"}, args: cobra.NoArgs, flags: syncFlags, input: syncInput,
		human: renderSync, progress: syncProgress},
	{op: "mailboxes.list", path: []string{"mailbox", "list"}, args: cobra.NoArgs, input: accountOnlyInput,
		human: renderMailboxes},
	{op: "messages.get", path: []string{"show"}, use: "<id>", args: cobra.ExactArgs(1), flags: showFlags,
		input: showInput, human: renderShow},
	{op: "messages.search", path: []string{"search"}, use: `"<query>"`, args: cobra.MinimumNArgs(1), flags: searchFlags,
		input: searchInput, human: renderSearch, jsonOut: searchJSON},
	{op: "threads.get", path: []string{"thread"}, use: "<thread-id>", args: cobra.ExactArgs(1), flags: threadFlags,
		input: threadInput, human: renderThread},
	{op: "attachments.list", path: []string{"attach", "list"}, use: "<message-id>", args: cobra.ExactArgs(1),
		input: attachListInput, human: renderAttachList},
	{op: "attachments.text", path: []string{"attach", "text"}, use: "<attachment-id>", args: cobra.ExactArgs(1),
		input: attachTextInput, human: renderAttachText},
}

// humanKey ist der Schlüssel, unter dem die CLI handelt: der Mensch am
// Terminal mit allen Rechten. Agenten-Schlüssel (--key) folgen mit S1.8.
var humanKey = policy.Key{ID: "cli", Name: "cli (human)", Scopes: policy.Roles["admin"]}

// bindOperations hängt alle Bindungen unter root.
func bindOperations(root *cobra.Command, reg *ops.Registry, a *app) error {
	groups := map[string]*cobra.Command{}
	for _, b := range bindings {
		op, err := reg.Get(b.op)
		if err != nil {
			return fmt.Errorf("cli: binding for unregistered operation: %w", err)
		}
		if len(b.path) != 1 && len(b.path) != 2 {
			return fmt.Errorf("cli: binding %s: path must be <verb> or <object> <verb>", b.op)
		}
		parent := root
		if len(b.path) == 2 {
			group, ok := groups[b.path[0]]
			if !ok {
				group = &cobra.Command{Use: b.path[0], Short: "Commands for " + b.path[0]}
				groups[b.path[0]] = group
				root.AddCommand(group)
			}
			parent = group
		}
		b := b
		use := b.path[len(b.path)-1]
		if b.use != "" {
			use += " " + b.use
		}
		cmd := &cobra.Command{
			Use:   use,
			Short: op.Summary,
			Args:  b.args,
			RunE: func(cmd *cobra.Command, args []string) error {
				input, secret := json.RawMessage(`{}`), secrets.Secret("")
				if b.input != nil {
					var err error
					if input, secret, err = b.input(cmd, args, a); err != nil {
						return err
					}
				}
				x, err := a.open(cmd.Context())
				if err != nil {
					return err
				}
				req := ops.Request{Key: humanKey, Actor: audit.ActorHuman, Operation: b.op, Input: input, Secret: secret}
				if b.progress != nil {
					req.Progress = b.progress(cmd, a)
				}
				if cmd.Flags().Changed("dry-run") {
					v, _ := cmd.Flags().GetBool("dry-run")
					req.DryRun = &v
				}
				res, err := x.Execute(cmd.Context(), req)
				if err != nil {
					a.log.Warn("operation failed", "op", b.op, "error", err.Error())
					return err
				}
				a.log.Info("operation", "op", b.op, "dry_run", res.Decision.DryRun)
				if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
					if b.jsonOut != nil {
						return b.jsonOut(a.stdout, res.Output)
					}
					return json.NewEncoder(a.stdout).Encode(res.Output)
				}
				return b.human(a.stdout, res.Output)
			},
		}
		if b.flags != nil {
			b.flags(cmd)
		}
		parent.AddCommand(cmd)
	}
	return nil
}

func nameInput(_ *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	b, err := json.Marshal(map[string]string{"name": args[0]})
	return b, "", err
}

// --- account add ------------------------------------------------------------

func accountAddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("imap", "", "IMAP server as host:port (port defaults to 993)")
	f.String("user", "", "login name")
	f.String("email", "", "e-mail address (defaults to the login name if it contains @)")
	f.String("tls", "", "tls | starttls | none (default: tls for port 993; starttls otherwise; none with --insecure-test-server)")
	f.String("auth", "password", "password | app_password")
	f.Bool("insecure-test-server", false, "allow plain text and self-signed certificates; only for localhost test servers")
	f.Bool("password-stdin", false, "read the password from standard input instead of prompting")
	f.Bool("password-from-env", false, "do not store a password; read it from POSTKAUZ_PASSWORD_<NAME> at runtime")
	_ = cmd.MarkFlagRequired("imap")
	_ = cmd.MarkFlagRequired("user")
}

// defaultTLS wählt den TLS-Modus, wenn --tls fehlt.
func defaultTLS(port int, insecure bool) string {
	switch {
	case port%1000 == 993:
		return "tls"
	case insecure:
		return "none"
	default:
		return "starttls"
	}
}

func accountAddInput(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	imapAddr, _ := f.GetString("imap")
	user, _ := f.GetString("user")
	email, _ := f.GetString("email")
	tlsMode, _ := f.GetString("tls")
	authType, _ := f.GetString("auth")
	insecure, _ := f.GetBool("insecure-test-server")
	fromStdin, _ := f.GetBool("password-stdin")
	fromEnv, _ := f.GetBool("password-from-env")
	asJSON, _ := f.GetBool("json")

	host, portStr, err := net.SplitHostPort(imapAddr)
	if err != nil {
		host, portStr = imapAddr, "993"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, "", usageError{fmt.Errorf("invalid --imap port %q", portStr)}
	}
	if tlsMode == "" {
		tlsMode = defaultTLS(port, insecure)
	}
	in := ops.AccountsAddInput{Name: args[0], Email: email, Host: host, Port: port, TLS: tlsMode, Username: user,
		AuthType: authType, InsecureTestServer: insecure, PasswordFromEnv: fromEnv}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, "", err
	}

	var pw secrets.Secret
	switch {
	case fromEnv && fromStdin:
		return nil, "", usageError{errors.New("use either --password-stdin or --password-from-env")}
	case fromEnv:
	case fromStdin:
		line, err := bufio.NewReader(a.stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, "", err
		}
		pw = secrets.Secret(strings.TrimRight(line, "\r\n"))
		if pw.IsZero() {
			return nil, "", usageError{errors.New("empty password on standard input")}
		}
	case asJSON:
		return nil, "", usageError{errors.New("no interactive prompt in --json mode; use --password-stdin or --password-from-env")}
	default:
		fd, ok := terminalFD(a.stdin)
		if !ok {
			return nil, "", usageError{errors.New("standard input is not a terminal; use --password-stdin or --password-from-env")}
		}
		fmt.Fprintf(a.stderr, "Password for %s at %s: ", user, host)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(a.stderr)
		if err != nil {
			return nil, "", err
		}
		pw = secrets.Secret(b)
		if pw.IsZero() {
			return nil, "", usageError{errors.New("empty password")}
		}
	}
	return raw, pw, nil
}

func terminalFD(r io.Reader) (int, bool) {
	f, ok := r.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return 0, false
	}
	return int(f.Fd()), true
}

// --- Ausgaben ohne --json ------------------------------------------------------

func renderAccounts(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Accounts) == 0 {
		_, err := fmt.Fprintln(w, "No accounts configured yet.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tEMAIL\tPROVIDER\tUNREAD\tLAST SYNC")
	for _, a := range o.Accounts {
		last := a.LastSyncAt
		if last == "" {
			last = "never"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\n", a.ID, a.Name, a.Email, a.Provider, a.UnreadTotal, last)
	}
	return tw.Flush()
}

func renderAccountAdd(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsAddOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	a := o.Account
	where := map[string]string{
		"keyring":       "password stored in the OS keyring as " + a.CredentialRef,
		"insecure-file": "password stored in the INSECURE test file store as " + a.CredentialRef,
		"environment":   "password read from " + secrets.EnvName(a.CredentialRef) + " at runtime (not stored)",
	}[a.SecretSource]
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would add account %s (%s:%d, tls %s, user %s); %s\n", a.Name, a.Host, a.Port, a.TLS, a.Username, where)
		return err
	}
	_, err := fmt.Fprintf(w, "account %s added (%s:%d, tls %s, user %s); %s\n", a.Name, a.Host, a.Port, a.TLS, a.Username, where)
	return err
}

func renderAccountTest(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsTestOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fmt.Fprintf(w, "account:       %s\nlogin:         %s\nsync strategy: %s\ncapabilities:  %s\nmailboxes:\n",
		o.Account, o.Login, o.SyncStrategy, strings.Join(o.Capabilities, " "))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, m := range o.Mailboxes {
		fmt.Fprintf(tw, "  %s\t%s\n", m.Path, m.Role)
	}
	return tw.Flush()
}

func renderAccountRemove(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsRemoveOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would remove account %s and its stored password\n", o.Removed)
		return err
	}
	msg := "stored password deleted"
	if !o.SecretDeleted {
		msg = "no stored password found"
	}
	_, err := fmt.Fprintf(w, "account %s removed; %s\n", o.Removed, msg)
	return err
}
