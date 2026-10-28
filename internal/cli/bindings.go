package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/prompt"
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
	// covers nennt die Felder der Eingabe (InputSchema.properties), die
	// dieses Kommando setzen kann. Der Vertragstest verlangt, dass sie genau
	// den Eigenschaften des Schemas entsprechen: Was die Operation kann,
	// kann auch die CLI.
	covers []string
	// input baut die Eingabe; Geheimnisse gehen getrennt zurück (nie in Input).
	input func(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error)
	human func(w io.Writer, out any) error
	// jsonOut ersetzt die JSON-Ausgabe (ein Objekt) für Listen, die als
	// NDJSON gestreamt werden (search, audit); nil = ein Objekt.
	jsonOut func(cmd *cobra.Command, w io.Writer, out any) error
	// progress liefert optional einen Empfänger für Fortschrittsmeldungen.
	progress func(cmd *cobra.Command, a *app) func(ops.Progress)
	// check prüft die Ausgabe nach dem Schreiben und kann so einen
	// Exit-Code ≠ 0 erzwingen (audit verify: mismatch → Exit 1).
	check func(out any) error
	// notes liefert Hinweise, die ohne --json auf stderr gehen, weil stdout
	// allein der Ausgabe gehört (draft show --eml); mit --json stehen sie in
	// der Ausgabe der Operation.
	notes func(out any) []string
	// outputOnError: Liefert die Operation bei einem Fehler (Abbruch) noch
	// eine Ausgabe, wird sie geschrieben, bevor der Fehler gemeldet wird
	// (import: Bericht trotz Strg+C; Exit bleibt interrupted).
	outputOnError bool
	// then nennt einen zweiten Aufruf nach Erfolg (trash --yes: der Mensch
	// gibt den eben gestellten Antrag frei, E3). Er läuft über den Executor
	// mit demselben Schlüssel und wird mit der Bindung dieser Operation
	// ausgegeben (mit --json als zweite Zeile).
	then func(cmd *cobra.Command, out any) (op string, input json.RawMessage, ok bool)
}

var bindings = []binding{
	{op: "accounts.list", path: []string{"accounts", "list"}, args: cobra.NoArgs, human: renderAccounts},
	{op: "accounts.add", path: []string{"account", "add"}, use: "<name>", args: cobra.ExactArgs(1),
		flags: accountAddFlags, input: accountAddInput, human: renderAccountAdd,
		covers: []string{"name", "email", "host", "port", "tls", "username", "auth_type", "insecure_test_server", "password_from_env", "client_id", "tenant"}},
	{op: "accounts.login", path: []string{"account", "login"}, use: "<name> [--device-code]", args: cobra.ExactArgs(1),
		flags: accountLoginFlags, input: accountLoginInput, human: renderAccountLogin, progress: loginProgress,
		covers: []string{"name", "device_code"}},
	{op: "accounts.set", path: []string{"account", "set"}, use: "<name> [--aliases a@example.org,b@example.org] [--password-stdin]", args: cobra.ExactArgs(1),
		flags: accountSetFlags, input: accountSetInput, human: renderAccountSet, covers: []string{"name", "aliases", "replace_password", "drafts_mailbox"}},
	{op: "accounts.test", path: []string{"account", "test"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderAccountTest, covers: []string{"name"}},
	{op: "accounts.remove", path: []string{"account", "remove"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderAccountRemove, covers: []string{"name"}},
	{op: "sync.run", path: []string{"sync"}, args: cobra.NoArgs, flags: syncFlags, input: syncInput,
		human: renderSync, progress: syncProgress,
		covers: []string{"account", "full", "watch", "window", "interval_seconds", "idle_restart_seconds", "full_check_interval_seconds", "jobs_interval_seconds", "mailboxes", "all"}},
	{op: "sync.request", path: []string{"sync", "request"}, args: cobra.NoArgs, flags: syncRequestFlags, input: syncRequestInput,
		human: renderSyncRequest, covers: []string{"account", "mailbox"}},
	{op: "import.run", path: []string{"import"}, use: "<mbox|maildir> <file-or-folder>", args: cobra.ExactArgs(2),
		flags: importFlags, input: importInput, human: renderImport, progress: importProgress,
		covers: []string{"account", "format", "path", "mailbox", "labels_from", "rescan", "no_extract"}, outputOnError: true},
	{op: "mailboxes.list", path: []string{"mailbox", "list"}, args: cobra.NoArgs, input: accountOnlyInput,
		human: renderMailboxes, covers: []string{"account"}},
	{op: "mailboxes.overview", path: []string{"mailbox", "overview"}, use: "<mailbox-id>", args: cobra.ExactArgs(1),
		flags: overviewFlags, input: overviewInput, human: renderOverview, covers: []string{"account", "mailbox", "since", "limit"}},
	{op: "messages.get", path: []string{"show"}, use: "<id>", args: cobra.ExactArgs(1), flags: showFlags,
		input: showInput, human: renderShow,
		covers: []string{"id", "message_id", "account", "headers", "strip_quotes", "max_chars"}},
	{op: "messages.search", path: []string{"search"}, use: `"<query>"`, args: cobra.MinimumNArgs(1), flags: searchFlags,
		input: searchInput, human: renderSearch, jsonOut: searchJSON,
		covers: []string{"query", "account", "mailbox", "limit", "cursor", "explain"}},
	{op: "threads.get", path: []string{"thread"}, use: "<thread-id>", args: cobra.ExactArgs(1), flags: threadFlags,
		input: threadInput, human: renderThread,
		covers: []string{"id", "message", "account", "text", "max_messages", "max_chars_per_message"}},
	{op: "attachments.list", path: []string{"attach", "list"}, use: "<message-id>", args: cobra.ExactArgs(1),
		input: attachListInput, human: renderAttachList, covers: []string{"message"}},
	{op: "attachments.text", path: []string{"attach", "text"}, use: "<attachment-id>", args: cobra.ExactArgs(1),
		flags: attachTextFlags, input: attachTextInput, human: renderAttachText, covers: []string{"id", "message", "account", "max_chars"}},
	{op: "keys.create", path: []string{"key", "create"}, args: cobra.NoArgs, flags: keyCreateFlags,
		input: keyCreateInput, human: renderKeyCreate,
		covers: []string{"name", "role", "scopes", "expires_in_days", "dry_run_default"}},
	{op: "keys.list", path: []string{"key", "list"}, args: cobra.NoArgs, human: renderKeyList},
	{op: "keys.revoke", path: []string{"key", "revoke"}, use: "<name>", args: cobra.ExactArgs(1),
		input: nameInput, human: renderKeyRevoke, covers: []string{"name"}},
	{op: "audit.list", path: []string{"audit"}, args: cobra.NoArgs, flags: auditFlags, input: auditInput,
		human: renderAudit, jsonOut: auditJSON,
		covers: []string{"since", "until", "actor", "key", "operation", "result", "raw", "limit", "after_id"}},
	{op: "audit.export", path: []string{"audit", "export"}, args: cobra.NoArgs, flags: auditExportFlags,
		input: auditExportInput, human: renderAuditExport, covers: []string{"to"}},
	{op: "audit.verify", path: []string{"audit", "verify"}, args: cobra.NoArgs, human: renderAuditVerify,
		check: checkAuditVerify},
	{op: "policy.get", path: []string{"policy", "show"}, args: cobra.NoArgs, human: renderPolicy},
}

// humanKey ist der Schlüssel, unter dem die CLI ohne --key handelt: der
// Mensch am Terminal mit allen Rechten, auch für HumanOnly-Operationen.
var humanKey = policy.Key{ID: "cli", Name: "cli (human)", Role: "admin", Scopes: policy.Roles["admin"], Human: true}

// observeRequest sieht jeden Aufruf vor dem Executor (nur Tests:
// Vertragstest Register ↔ CLI prüft die gebaute Eingabe gegen das Schema;
// ein Fehler bricht den Aufruf vor dem Executor ab).
var observeRequest func(ops.Request) error

// bindOperations hängt alle Bindungen unter root.
func bindOperations(root *cobra.Command, reg *ops.Registry, a *app) error {
	top := map[string]*cobra.Command{}
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
			group, ok := top[b.path[0]]
			if !ok {
				group = &cobra.Command{Use: b.path[0], Short: "Commands for " + b.path[0]}
				top[b.path[0]] = group
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
			RunE:  func(cmd *cobra.Command, args []string) error { return a.runBinding(cmd, args, b, op) },
		}
		if b.flags != nil {
			b.flags(cmd)
		}
		if len(b.path) == 1 {
			if _, dup := top[b.path[0]]; dup {
				return fmt.Errorf("cli: binding %s: command %q defined twice", b.op, b.path[0])
			}
			top[b.path[0]] = cmd
		}
		parent.AddCommand(cmd)
	}
	return nil
}

// runBinding führt eine Bindung aus: Eingabe bauen, Schlüssel wählen,
// Executor, Ausgabe (Text, JSON oder NDJSON), Erklärung (--explain).
func (a *app) runBinding(cmd *cobra.Command, args []string, b binding, op ops.Operation) error {
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
	key, err := a.key(cmd)
	if err != nil {
		return err
	}
	req := ops.Request{Key: key, Actor: audit.ActorHuman, Operation: b.op, Input: input, Secret: secret}
	if b.progress != nil && !a.quiet(cmd) {
		req.Progress = b.progress(cmd, a)
	}
	if cmd.Flags().Changed("dry-run") {
		v, _ := cmd.Flags().GetBool("dry-run")
		req.DryRun = &v
	}
	if observeRequest != nil {
		if err := observeRequest(req); err != nil {
			return err
		}
	}
	res, err := x.Execute(cmd.Context(), req)
	if explain, _ := cmd.Flags().GetBool("explain"); explain {
		a.explain(cmd, op, key, res, err)
	}
	if err != nil {
		// Nur Code und Grund, nie die Meldung: Sie kann Eingaben des
		// Aufrufers enthalten (Gegenprüfung S2.3, B1).
		code, reason := errLogInfo(err)
		a.log.Warn("operation failed", "op", b.op, "code", code, "reason", reason)
		if b.outputOnError && res.Output != nil {
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				_ = json.NewEncoder(a.stdout).Encode(res.Output)
			} else {
				_ = b.human(a.stdout, res.Output)
			}
		}
		return err
	}
	a.log.Info("operation", "op", b.op, "dry_run", res.Decision.DryRun)
	if os.Getenv(ops.EnvTestPanic) == "cli:"+b.op {
		panic(fmt.Sprintf("injected test panic with output %+v", res.Output))
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		if b.jsonOut != nil {
			err = b.jsonOut(cmd, a.stdout, res.Output)
		} else {
			err = json.NewEncoder(a.stdout).Encode(res.Output)
		}
	} else {
		err = b.human(a.stdout, res.Output)
		if err == nil && b.notes != nil {
			for _, n := range b.notes(res.Output) {
				fmt.Fprintf(a.stderr, "note: %s\n", termLine(n))
			}
		}
	}
	if err == nil && b.check != nil {
		err = b.check(res.Output)
	}
	if err == nil && b.then != nil {
		if op2, in2, ok := b.then(cmd, res.Output); ok {
			return a.runFollowUp(cmd, key, op2, in2)
		}
	}
	// E3: Ein Antrag (needs_approval) ist erfolgreich angelegt, aber noch
	// nicht ausgeführt: Exit 4, die Ausgabe nennt den nächsten Schritt.
	if err == nil && res.Decision.Outcome == policy.NeedsApproval {
		return &ops.PolicyError{Operation: b.op, Scope: op.Scope, Decision: res.Decision, AuditID: res.AuditID}
	}
	return err
}

// runFollowUp führt den zweiten Aufruf einer Bindung aus (then) und gibt
// ihn mit der ersten Bindung dieser Operation aus.
func (a *app) runFollowUp(cmd *cobra.Command, key policy.Key, opName string, input json.RawMessage) error {
	var fb *binding
	for i := range bindings {
		if bindings[i].op == opName {
			fb = &bindings[i]
			break
		}
	}
	if fb == nil {
		return fmt.Errorf("cli: no binding for %s", opName)
	}
	x, err := a.open(cmd.Context())
	if err != nil {
		return err
	}
	req := ops.Request{Key: key, Actor: audit.ActorHuman, Operation: opName, Input: input}
	if cmd.Flags().Changed("dry-run") {
		v, _ := cmd.Flags().GetBool("dry-run")
		req.DryRun = &v
	}
	res, err := x.Execute(cmd.Context(), req)
	if err != nil {
		code, reason := errLogInfo(err)
		a.log.Warn("operation failed", "op", opName, "code", code, "reason", reason)
		return err
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return json.NewEncoder(a.stdout).Encode(res.Output)
	}
	return fb.human(a.stdout, res.Output)
}

// key wählt den Schlüssel: ohne --key der Mensch mit allen Rechten, mit
// --key <name> die Rechte dieses API-Schlüssels (Probe der Policy; der
// Klartext eines Schlüssels wird nie als Argument angenommen).
func (a *app) key(cmd *cobra.Command) (policy.Key, error) {
	name, _ := cmd.Flags().GetString("key")
	if name == "" {
		return humanKey, nil
	}
	if strings.HasPrefix(name, ops.KeyPrefix) {
		return policy.Key{}, usageError{errors.New("--key takes the name of a key, never the key itself (see: key list)")}
	}
	return ops.KeyByName(cmd.Context(), a.st, name, time.Now())
}

func (a *app) quiet(cmd *cobra.Command) bool {
	q, _ := cmd.Flags().GetBool("quiet")
	return q
}

// explainInfo ist die Erklärung eines Aufrufs (--explain): Policy-Entscheid
// und Audit-ID, nie Inhalte.
type explainInfo struct {
	Operation string `json:"operation"`
	Scope     string `json:"scope"`
	Risk      string `json:"risk"`
	Key       string `json:"key"`
	Actor     string `json:"actor"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	DryRun    bool   `json:"dry_run"`
	AuditID   int64  `json:"audit_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// explain schreibt die Erklärung auf stderr (stdout bleibt die Ausgabe der
// Operation): als Textzeile oder mit --json als {"explain":{…}}.
func (a *app) explain(cmd *cobra.Command, op ops.Operation, key policy.Key, res ops.Result, err error) {
	dec, id := res.Decision, res.AuditID
	var pe *ops.PolicyError
	if errors.As(err, &pe) {
		dec, id = pe.Decision, pe.AuditID
	}
	if dec.Outcome == "" {
		return // vor der Policy gescheitert (z. B. Datenbank); nichts zu erklären
	}
	info := explainInfo{Operation: op.Name, Scope: string(op.Scope), Risk: op.Risk.String(), Key: key.ID,
		Actor: string(audit.ActorHuman), Decision: string(dec.Outcome), Reason: dec.Reason, DryRun: dec.DryRun, AuditID: id}
	if err != nil && pe == nil {
		info.Error = "handler failed"
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		_ = json.NewEncoder(a.stderr).Encode(map[string]explainInfo{"explain": info})
		return
	}
	yn := map[bool]string{true: "yes", false: "no"}
	line := fmt.Sprintf("explain: %s (scope %s, risk %s) as key %s: %s (%s), dry run %s", info.Operation, info.Scope, info.Risk,
		info.Key, info.Decision, info.Reason, yn[info.DryRun])
	if id > 0 {
		line += fmt.Sprintf("; audit event %d", id)
	}
	if info.Error != "" {
		line += "; " + termLine(info.Error)
	}
	fmt.Fprintln(a.stderr, line)
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
	f.String("auth", "password", "password | app_password | oauth2 (Microsoft; then --client-id, no password, sign in with account login)")
	f.String("client-id", "", "oauth2: Application (client) ID of your own app registration (see docs/betrieb_microsoft.md)")
	f.String("tenant", "", "oauth2: common (default), consumers (personal Microsoft accounts), organizations or a tenant id")
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
	clientID, _ := f.GetString("client-id")
	tenant, _ := f.GetString("tenant")

	host, portStr, err := net.SplitHostPort(imapAddr)
	if err != nil {
		host, portStr = imapAddr, "993"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, "", usageError{errors.New("invalid --imap port: give host:port with a port number")}
	}
	if tlsMode == "" {
		tlsMode = defaultTLS(port, insecure)
	}
	in := ops.AccountsAddInput{Name: args[0], Email: email, Host: host, Port: port, TLS: tlsMode, Username: user,
		AuthType: authType, InsecureTestServer: insecure, PasswordFromEnv: fromEnv, ClientID: clientID, Tenant: tenant}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, "", err
	}
	if authType == ops.AuthOAuth2 {
		// Kein Passwort: Das Token kommt mit account login (S3.7).
		if fromStdin || fromEnv {
			return nil, "", usageError{errors.New("--auth oauth2 uses no password; leave out --password-stdin and --password-from-env and sign in with: account login " + args[0])}
		}
		return raw, "", nil
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
		if pw, err = readHiddenPassword(cmd.Context(), a, fmt.Sprintf("Password for %s at %s: ", user, host)); err != nil {
			return nil, "", err
		}
	}
	return raw, pw, nil
}

// --- account set ------------------------------------------------------------

func accountSetFlags(cmd *cobra.Command) {
	cmd.Flags().String("aliases", "", "further own addresses of the account, comma separated; replaces the list (\"\" removes all)")
	cmd.Flags().Bool("password-stdin", false, "replace the stored password with one read from standard input (hidden prompt on a terminal; never as an argument)")
	cmd.Flags().String("drafts-mailbox", "", "Drafts folder of this account (path on the server; \"\" = [draft] mailbox of the configuration, else role drafts)")
}

func accountSetInput(cmd *cobra.Command, args []string, a *app) (json.RawMessage, secrets.Secret, error) {
	in := ops.AccountsSetInput{Name: args[0]}
	if cmd.Flags().Changed("aliases") {
		v, _ := cmd.Flags().GetString("aliases")
		list := []string{}
		for _, a := range strings.Split(v, ",") {
			if a = strings.TrimSpace(a); a != "" {
				list = append(list, a)
			}
		}
		in.Aliases = &list
	}
	var pw secrets.Secret
	if fromStdin, _ := cmd.Flags().GetBool("password-stdin"); fromStdin {
		var err error
		if pw, err = readNewPassword(cmd.Context(), a, args[0]); err != nil {
			return nil, "", err
		}
		in.ReplacePassword = true
	}
	if cmd.Flags().Changed("drafts-mailbox") {
		v, _ := cmd.Flags().GetString("drafts-mailbox")
		in.DraftsMailbox = &v
	}
	if in.Aliases == nil && !in.ReplacePassword && in.DraftsMailbox == nil {
		return nil, "", usageError{errors.New("nothing to change; use --aliases a@example.org,b@example.org (--aliases \"\" removes all), --drafts-mailbox <path> or --password-stdin")}
	}
	b, err := json.Marshal(in)
	return b, pw, err
}

// readNewPassword liest ein neues Passwort von stdin (Entscheid 36): am
// Terminal verdeckt mit Aufforderung auf stderr, sonst die erste Zeile.
// Das Passwort ist nie ein Argument (Prozessliste, Verlauf der Shell).
func readNewPassword(ctx context.Context, a *app, account string) (secrets.Secret, error) {
	if f, ok := a.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return readHiddenPassword(ctx, a, fmt.Sprintf("New password for account %s: ", account))
	}
	line, err := bufio.NewReader(a.stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	pw := secrets.Secret(strings.TrimRight(line, "\r\n"))
	if pw.IsZero() {
		return "", usageError{errors.New("empty password on standard input")}
	}
	return pw, nil
}

// readHiddenPassword fragt am Terminal verdeckt nach einem Passwort
// (internal/prompt, A3: unter Windows über ReadConsoleW, sonst x/term;
// Abbruch nach prompt.DefaultTimeout ohne Eingabe).
func readHiddenPassword(ctx context.Context, a *app, label string) (secrets.Secret, error) {
	f, ok := a.stdin.(*os.File)
	if !ok {
		return "", usageError{errNotTerminal}
	}
	b, _, err := prompt.Password(ctx, f, a.stderr, label, prompt.Options{})
	switch {
	case errors.Is(err, prompt.ErrNotTerminal):
		return "", usageError{errNotTerminal}
	case errors.Is(err, prompt.ErrTooLong):
		return "", usageError{fmt.Errorf("password longer than %d characters", prompt.MaxLen)}
	case errors.Is(err, prompt.ErrUnknownReader):
		return "", usageError{err}
	case err != nil:
		return "", err
	case len(b) == 0:
		return "", usageError{errors.New("empty password")}
	}
	return secrets.Secret(b), nil
}

var errNotTerminal = errors.New("standard input is not a terminal; use --password-stdin or --password-from-env")

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
	fmt.Fprintln(tw, "ID\tNAME\tEMAIL\tPROVIDER\tUNREAD\tLAST SYNC\tALIASES")
	for _, a := range o.Accounts {
		last := a.LastSyncAt
		if last == "" {
			last = "never"
		}
		aliases := strings.Join(a.Aliases, ", ")
		if aliases == "" {
			aliases = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\t%s\n", a.ID, a.Name, a.Email, a.Provider, a.UnreadTotal, last, aliases)
	}
	return tw.Flush()
}

func renderAccountAdd(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsAddOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	a := o.Account
	if a.AuthType == ops.AuthOAuth2 {
		verb := "added"
		if o.DryRun {
			verb = "would be added (dry run)"
		}
		_, err := fmt.Fprintf(w, "account %s %s (%s:%d, tls %s, user %s); OAuth2 client %s, tenant %s; no token yet, sign in with: account login %s\n",
			a.Name, verb, a.Host, a.Port, a.TLS, a.Username, a.ClientID, a.Tenant, a.Name)
		return err
	}
	where := map[string]string{
		"keyring":       "password stored in the OS keyring as " + a.CredentialRef,
		"insecure-file": "password stored in the INSECURE test file store as " + a.CredentialRef,
		"environment":   "password read from " + secrets.EnvName(a.CredentialRef) + " at runtime (not stored)",
	}[a.SecretSource]
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would add account %s (%s:%d, tls %s, user %s); %s\n", a.Name, a.Host, a.Port, a.TLS, a.Username, where)
		return err
	}
	if _, err := fmt.Fprintf(w, "account %s added (%s:%d, tls %s, user %s); %s\n", a.Name, a.Host, a.Port, a.TLS, a.Username, where); err != nil {
		return err
	}
	// A5.3 (D5): Ein großer Gesendet-Ordner lädt beim ersten Sync jeden
	// Anhang; die Grenze steht in der Konfiguration.
	_, err := fmt.Fprintln(w, maxMessageBytesTip)
	return err
}

// maxMessageBytesTip nennt nach account add die Grenze für große
// Nachrichten mit Beispiel (A5.3).
const maxMessageBytesTip = "tip: the first sync fetches every message; to skip very large ones (e.g. in a big Sent folder), set " +
	"max_message_bytes = 10485760 (10 MiB) under [imap] in the configuration file (path: see doctor); " +
	"sync --full fetches them once you raise the limit"

func renderAccountSet(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsSetOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	prefix := "account " + o.Account
	if o.DryRun {
		prefix = "dry run: would set account " + o.Account
	}
	if o.AliasesChanged {
		aliases := "none"
		if len(o.Aliases) > 0 {
			aliases = strings.Join(o.Aliases, ", ")
		}
		if _, err := fmt.Fprintf(w, "%s: aliases %s\n", prefix, aliases); err != nil {
			return err
		}
	}
	if o.DraftsMailboxChanged {
		box := "the configured one ([draft] mailbox) or the mailbox with role drafts"
		if o.DraftsMailbox != "" {
			box = termLine(o.DraftsMailbox)
		}
		if _, err := fmt.Fprintf(w, "%s: Drafts folder %s\n", prefix, box); err != nil {
			return err
		}
	}
	if o.Password != "" {
		where := map[string]string{"keyring": "in the OS keyring", "insecure-file": "in the INSECURE test file store"}[o.SecretStore]
		if where == "" {
			where = "in the secret store"
		}
		msg := fmt.Sprintf("account %s: password replaced %s as %s", o.Account, where, o.CredentialRef)
		if o.DryRun {
			msg = fmt.Sprintf("dry run: would replace the password of account %s %s as %s", o.Account, where, o.CredentialRef)
		}
		if _, err := fmt.Fprintln(w, msg); err != nil {
			return err
		}
		if o.EnvOverride != "" {
			if _, err := fmt.Fprintf(w, "note: %s is set and takes precedence over the stored password at runtime\n", o.EnvOverride); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderAccountTest(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsTestOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	login := termLine(o.Login)
	// Das Verfahren nur bei SASL nennen; Passwort-Konten zeigen weiter
	// „login: ok" wie in den Abnahmen D1 bis D5.
	if o.AuthMechanism != "" && o.AuthMechanism != "LOGIN" {
		login += " (" + termLine(o.AuthMechanism) + ")"
	}
	// Fähigkeiten und Ordnernamen kommen vom Server: terminalsicher.
	fmt.Fprintf(w, "account:       %s\nlogin:         %s\nsync strategy: %s\ncapabilities:  %s\nmailboxes:\n",
		o.Account, login, o.SyncStrategy, strings.Join(termLines(o.Capabilities), " "))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, m := range o.Mailboxes {
		fmt.Fprintf(tw, "  %s\t%s\n", termLine(m.Path), m.Role)
	}
	return tw.Flush()
}

func renderAccountRemove(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsRemoveOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	// OAuth2-Konten haben kein Passwort, sondern ein Token (T2 Punkt 7).
	kind := "password"
	if o.SecretKind == "token" {
		kind = "token"
	}
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would remove account %s and its stored %s\n", o.Removed, kind)
		return err
	}
	msg := "stored " + kind + " deleted"
	if !o.SecretDeleted {
		msg = "no stored " + kind + " found"
	}
	_, err := fmt.Fprintf(w, "account %s removed; %s\n", o.Removed, msg)
	return err
}

// --- account login ------------------------------------------------------------

func accountLoginFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("device-code", false, "sign in on another device with a code instead of the browser on this computer")
}

func accountLoginInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	dc, _ := cmd.Flags().GetBool("device-code")
	b, err := json.Marshal(ops.AccountsLoginInput{Name: args[0], DeviceCode: dc})
	return b, "", err
}

// loginProgress zeigt die Hinweise der Anmeldung (Adresse, Code) auf
// stderr, auch mit --json: Der Mensch braucht sie, stdout gehört der Ausgabe.
func loginProgress(_ *cobra.Command, a *app) func(ops.Progress) {
	return func(p ops.Progress) {
		if p.Event == "oauth_prompt" {
			for _, line := range strings.Split(p.Message, "\n") {
				fmt.Fprintln(a.stderr, termLine(line))
			}
		}
	}
}

func renderAccountLogin(w io.Writer, out any) error {
	o, ok := out.(ops.AccountsLoginOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	where := map[string]string{"keyring": "in the OS keyring", "insecure-file": "in the INSECURE test file store"}[o.SecretStore]
	if where == "" {
		where = "in the secret store"
	}
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would sign in to account %s (%s) and store the refresh token %s as %s\n",
			o.Account, strings.ReplaceAll(o.Method, "_", " "), where, o.CredentialRef)
		return err
	}
	exp := ""
	if o.TokenExpiry != "" {
		exp = "; access token valid until " + o.TokenExpiry + " (kept in memory only, renewed automatically)"
	}
	_, err := fmt.Fprintf(w, "account %s: signed in; refresh token stored %s as %s%s\nnext: account test %s\n",
		o.Account, where, o.CredentialRef, exp, o.Account)
	return err
}
