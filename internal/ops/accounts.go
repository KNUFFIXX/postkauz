package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// AccountInfo ist die Ausgabe von accounts.list je Konto. Enthält nie
// Zugangsdaten.
type AccountInfo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Provider    string `json:"provider"`
	LastSyncAt  string `json:"last_sync_at,omitempty"` // RFC 3339, UTC
	UnreadTotal int    `json:"unread_total"`
	// Aliases: weitere eigene Adressen (accounts.set; S2.0/A6).
	Aliases []string `json:"aliases"`
}

// AccountDetail beschreibt ein Konto ausführlich (accounts.add). Statt des
// Passworts steht nur, woher es kommt (secret_source) und unter welchem
// Verweis es liegt.
type AccountDetail struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Email              string `json:"email"`
	Provider           string `json:"provider"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	TLS                string `json:"tls"`
	Username           string `json:"username"`
	AuthType           string `json:"auth_type"`
	CredentialRef      string `json:"credential_ref"`
	SecretSource       string `json:"secret_source"`
	InsecureTestServer bool   `json:"insecure_test_server"`
	Status             string `json:"status"`
}

// AccountRepo ist der Teil des Stores, den die Kontenoperationen brauchen.
type AccountRepo interface {
	ListAccounts(ctx context.Context) ([]store.AccountSummary, error)
	AccountByName(ctx context.Context, name string) (store.Account, error)
	CreateAccount(ctx context.Context, a store.Account) (int64, error)
	DeleteAccount(ctx context.Context, id int64) error
	CountMessages(ctx context.Context, accountID int64) (int, error)
	UpdateAccountProbe(ctx context.Context, id int64, capabilities, status, lastError string) error
	// DataDirID ist die Kennung des Datenverzeichnisses im
	// Schlüsselbund-Verweis (S2.0/A2).
	DataDirID(ctx context.Context) (string, error)
	// SetAccountAliases ersetzt die weiteren eigenen Adressen (S2.0/A6).
	SetAccountAliases(ctx context.Context, id int64, aliases []string) error
}

// ProbeMailbox ist ein Ordner im Ergebnis von accounts.test.
type ProbeMailbox struct {
	Path string `json:"path"`
	Role string `json:"role"`
}

// ProbeResult ist das Ergebnis einer Verbindungsprüfung.
type ProbeResult struct {
	Capabilities []string       `json:"capabilities"`
	SyncStrategy string         `json:"sync_strategy"`
	Mailboxes    []ProbeMailbox `json:"mailboxes"`
	// CapabilitiesJSON ist provider.Capabilities als JSON für accounts.capabilities.
	CapabilitiesJSON string `json:"-"`
}

// AccountProber meldet sich an einem Konto an und liest Fähigkeiten und
// Ordner, ohne etwas zu verändern. Implementierung: internal/provider/imap.
type AccountProber interface {
	Probe(ctx context.Context, a store.Account, password secrets.Secret) (ProbeResult, error)
}

// InputError ist eine ungültige Eingabe (CLI: Exit-Code 2).
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

func inputErr(format string, args ...any) error {
	return &InputError{Msg: fmt.Sprintf(format, args...)}
}

// decodeInput liest die Eingabe streng (unbekannte Felder sind ein Fehler).
func decodeInput(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return inputErr("invalid input: %v", err)
	}
	return nil
}

// AccountNameRE: Kleinbuchstaben, Ziffern, Bindestrich. Kein Unterstrich,
// damit die Umgebungsvariable POSTKAUZ_PASSWORD_<NAME> eindeutig bleibt.
var AccountNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func validAccountName(name string) error {
	if !AccountNameRE.MatchString(name) {
		return inputErr("invalid account name %q: use lowercase letters, digits and '-' (max 63)", name)
	}
	return nil
}

// redactAccountInput: Feldnamen und Kontoname, nie Werte wie Host oder Nutzer.
func redactAccountInput(raw json.RawMessage) (json.RawMessage, []string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	fields := make([]string, 0, len(m))
	for k := range m {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	var targets []string
	var name string
	if v, ok := m["name"]; ok && json.Unmarshal(v, &name) == nil && AccountNameRE.MatchString(name) {
		targets = []string{"account:" + name}
	}
	b, _ := json.Marshal(map[string]any{"fields": fields})
	return b, targets
}

const accountNameSchema = `{
  "type": "object",
  "properties": {"name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"}},
  "required": ["name"],
  "additionalProperties": false
}`

// --- accounts.list ---------------------------------------------------------

// AccountsList listet die Konten aus der Datenbank (R0, idempotent).
func AccountsList(repo AccountRepo) Operation {
	return Operation{
		Name:       "accounts.list",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "List configured accounts with their sync state.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "accounts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "integer"},
          "name": {"type": "string"},
          "email": {"type": "string"},
          "provider": {"type": "string"},
          "last_sync_at": {"type": "string", "format": "date-time"},
          "unread_total": {"type": "integer"},
          "aliases": {"type": "array", "items": {"type": "string"}}
        },
        "required": ["id", "name", "email", "provider", "unread_total", "aliases"]
      }
    }
  },
  "required": ["accounts"]
}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			list, err := repo.ListAccounts(ctx)
			if err != nil {
				return nil, err
			}
			out := AccountsListOutput{Accounts: []AccountInfo{}} // JSON: [] statt null
			for _, a := range list {
				info := AccountInfo{ID: a.ID, Name: a.Name, Email: a.Email, Provider: a.Provider, UnreadTotal: a.UnreadTotal,
					Aliases: a.Aliases}
				if info.Aliases == nil {
					info.Aliases = []string{}
				}
				if !a.LastSyncAt.IsZero() {
					info.LastSyncAt = a.LastSyncAt.UTC().Format(time.RFC3339)
				}
				out.Accounts = append(out.Accounts, info)
			}
			return out, nil
		},
	}
}

// AccountsListOutput ist die Ausgabe von accounts.list.
type AccountsListOutput struct {
	Accounts []AccountInfo `json:"accounts"`
}

// --- accounts.add ----------------------------------------------------------

// AccountsAddInput ist die Eingabe von accounts.add. Das Passwort steht
// nicht hier, sondern in Call.Secret.
type AccountsAddInput struct {
	Name               string `json:"name"`
	Email              string `json:"email,omitempty"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	TLS                string `json:"tls"`
	Username           string `json:"username"`
	AuthType           string `json:"auth_type,omitempty"`
	InsecureTestServer bool   `json:"insecure_test_server,omitempty"`
	// PasswordFromEnv: Das Passwort steht in der Umgebungsvariable
	// POSTKAUZ_PASSWORD_<NAME> und wird nicht gespeichert (Server, Docker).
	PasswordFromEnv bool `json:"password_from_env,omitempty"`
}

// AccountsAddOutput ist die Ausgabe von accounts.add.
type AccountsAddOutput struct {
	Account AccountDetail `json:"account"`
	DryRun  bool          `json:"dry_run"`
}

// IsLoopbackHost meldet, ob host auf den eigenen Rechner zeigt.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (in *AccountsAddInput) normalize() error {
	if err := validAccountName(in.Name); err != nil {
		return err
	}
	in.Host = strings.TrimSpace(in.Host)
	if in.Host == "" || strings.ContainsAny(in.Host, " /\\@") {
		return inputErr("invalid host %q", in.Host)
	}
	if in.Port < 1 || in.Port > 65535 {
		return inputErr("invalid port %d", in.Port)
	}
	if strings.TrimSpace(in.Username) == "" {
		return inputErr("username is required")
	}
	switch in.TLS {
	case "tls", "starttls":
	case "none":
		if !in.InsecureTestServer {
			return inputErr("tls=none sends the password in plain text; only allowed with insecure_test_server for a local test server")
		}
	default:
		return inputErr("invalid tls mode %q (expected tls, starttls or none)", in.TLS)
	}
	if in.InsecureTestServer && !IsLoopbackHost(in.Host) {
		return inputErr("insecure_test_server is only allowed for localhost test servers, not %q", in.Host)
	}
	if in.AuthType == "" {
		in.AuthType = "password"
	}
	switch in.AuthType {
	case "password", "app_password":
	case "oauth2":
		return inputErr("auth_type oauth2 is not supported yet (planned: bring your own OAuth client)")
	default:
		return inputErr("invalid auth_type %q (expected password or app_password)", in.AuthType)
	}
	if in.Email == "" && strings.Contains(in.Username, "@") {
		in.Email = in.Username
	}
	return nil
}

// AccountsAdd legt ein Konto an; das Passwort landet im Schlüsselbund (bzw.
// dem gewählten Speicher), in der Datenbank nur der Verweis. R1, Scope admin.
func AccountsAdd(repo AccountRepo, sec secrets.Store) Operation {
	return Operation{
		Name:    "accounts.add",
		Scope:   policy.ScopeAdmin,
		Risk:    policy.R1,
		Summary: "Add a mail account; the password goes to the OS keyring, never into the database.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "email": {"type": "string"},
    "host": {"type": "string"},
    "port": {"type": "integer", "minimum": 1, "maximum": 65535},
    "tls": {"type": "string", "enum": ["tls", "starttls", "none"]},
    "username": {"type": "string"},
    "auth_type": {"type": "string", "enum": ["password", "app_password"]},
    "insecure_test_server": {"type": "boolean"},
    "password_from_env": {"type": "boolean"}
  },
  "required": ["name", "host", "port", "tls", "username"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {
      "type": "object",
      "properties": {
        "id": {"type": "integer"}, "name": {"type": "string"}, "email": {"type": "string"},
        "provider": {"type": "string"}, "host": {"type": "string"}, "port": {"type": "integer"},
        "tls": {"type": "string"}, "username": {"type": "string"}, "auth_type": {"type": "string"},
        "credential_ref": {"type": "string"},
        "secret_source": {"type": "string", "enum": ["keyring", "insecure-file", "environment"]},
        "insecure_test_server": {"type": "boolean"}, "status": {"type": "string"}
      },
      "required": ["name", "host", "port", "tls", "username", "credential_ref", "secret_source"]
    },
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "dry_run"]
}`),
		Redact: redactAccountInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in AccountsAddInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := in.normalize(); err != nil {
				return nil, err
			}
			dirID, err := repo.DataDirID(ctx)
			if err != nil {
				return nil, err
			}
			ref := secrets.AccountRef(dirID, in.Name)
			if _, err := repo.AccountByName(ctx, in.Name); err == nil {
				return nil, fmt.Errorf("%w: %s", store.ErrAccountExists, in.Name)
			} else if !errors.Is(err, store.ErrAccountNotFound) {
				return nil, err
			}
			source := sec.Status().Backend
			if in.PasswordFromEnv {
				source = "environment"
				if !call.Secret.IsZero() {
					return nil, inputErr("password_from_env and a password must not be combined")
				}
				if _, err := sec.Get(ref); err != nil {
					return nil, inputErr("password_from_env: environment variable %s is not set", secrets.EnvName(ref))
				}
			} else if call.Secret.IsZero() {
				return nil, inputErr("password is required")
			}
			acct := store.Account{Name: in.Name, Email: in.Email, Provider: "imap", Host: in.Host, Port: in.Port,
				TLS: in.TLS, Username: in.Username, AuthType: in.AuthType, CredentialRef: ref,
				InsecureTestServer: in.InsecureTestServer, Status: "new"}
			out := AccountsAddOutput{DryRun: call.DryRun, Account: detail(acct, source)}
			if call.DryRun {
				return out, nil
			}
			stored := false
			if !in.PasswordFromEnv {
				if err := sec.Set(ref, call.Secret); err != nil {
					return nil, err
				}
				stored = true
			}
			id, err := repo.CreateAccount(ctx, acct)
			if err != nil {
				if stored {
					_ = sec.Delete(ref) // kein verwaister Eintrag im Schlüsselbund
				}
				return nil, err
			}
			out.Account.ID = id
			return out, nil
		},
	}
}

func detail(a store.Account, source string) AccountDetail {
	return AccountDetail{ID: a.ID, Name: a.Name, Email: a.Email, Provider: a.Provider, Host: a.Host, Port: a.Port,
		TLS: a.TLS, Username: a.Username, AuthType: a.AuthType, CredentialRef: a.CredentialRef,
		SecretSource: source, InsecureTestServer: a.InsecureTestServer, Status: a.Status}
}

// --- accounts.set ----------------------------------------------------------

// MaxAliases begrenzt die weiteren eigenen Adressen je Konto.
const MaxAliases = 50

// AccountsSetInput ist die Eingabe von accounts.set. Fehlt ein Feld, bleibt
// der Wert, wie er ist; mindestens ein Feld außer name ist nötig.
type AccountsSetInput struct {
	Name string `json:"name"`
	// Aliases ersetzt die Liste vollständig ([] leert sie).
	Aliases *[]string `json:"aliases,omitempty"`
}

// AccountsSetOutput ist die Ausgabe von accounts.set.
type AccountsSetOutput struct {
	Account string   `json:"account"`
	Aliases []string `json:"aliases"`
	DryRun  bool     `json:"dry_run"`
}

// normalizeAliases prüft die Adressen (RFC 5322, nur die Adresse ohne
// Namen), schreibt sie klein, entfernt Dubletten und die Kontoadresse.
func normalizeAliases(in []string, own string) ([]string, error) {
	out := []string{}
	own = strings.ToLower(strings.TrimSpace(own))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		addr, err := mail.ParseAddress(s)
		if err != nil || addr.Name != "" || addr.Address != s || !strings.Contains(s, "@") {
			return nil, inputErr("invalid alias %q: give a plain e-mail address such as alias@example.org", raw)
		}
		a := strings.ToLower(addr.Address)
		if a == own || slices.Contains(out, a) {
			continue
		}
		out = append(out, a)
	}
	if len(out) > MaxAliases {
		return nil, inputErr("too many aliases (%d, at most %d)", len(out), MaxAliases)
	}
	sort.Strings(out)
	return out, nil
}

// redactAccountSet: Feldnamen, Zahl der Aliasse und das Konto als Ziel,
// nie die Adressen.
func redactAccountSet(raw json.RawMessage) (json.RawMessage, []string) {
	params, targets := redactAccountInput(raw)
	var in struct {
		Aliases *[]string `json:"aliases"`
	}
	if json.Unmarshal(raw, &in) == nil && in.Aliases != nil {
		var m map[string]any
		_ = json.Unmarshal(params, &m)
		m["aliases_count"] = len(*in.Aliases)
		params, _ = json.Marshal(m)
	}
	return params, targets
}

// AccountsSet ändert Einstellungen eines Kontos; seit S2.0 die weiteren
// eigenen Adressen (Aliasse). R1 (lokal), Scope admin, nur für Menschen
// (A02, E2): kein Schlüssel, auch nicht admin, darf eigene Adressen
// umdefinieren (sie steuern ab S2, wen „Allen antworten" weglässt).
func AccountsSet(repo AccountRepo) Operation {
	return Operation{
		Name:       "accounts.set",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R1,
		Idempotent: true,
		HumanOnly:  true,
		Summary:    "Change account settings: the further own addresses (aliases) of an account. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "aliases": {"type": "array", "items": {"type": "string"}, "maxItems": 50}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "aliases": {"type": "array", "items": {"type": "string"}},
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "aliases", "dry_run"]
}`),
		Redact: redactAccountSet,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in AccountsSetInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := validAccountName(in.Name); err != nil {
				return nil, err
			}
			if in.Aliases == nil {
				return nil, inputErr("nothing to change: give aliases (an empty list removes them)")
			}
			a, err := repo.AccountByName(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			aliases, err := normalizeAliases(*in.Aliases, a.Email)
			if err != nil {
				return nil, err
			}
			out := AccountsSetOutput{Account: a.Name, Aliases: aliases, DryRun: call.DryRun}
			if call.DryRun {
				return out, nil
			}
			if err := repo.SetAccountAliases(ctx, a.ID, aliases); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}

// --- accounts.remove -------------------------------------------------------

// AccountsRemoveOutput ist die Ausgabe von accounts.remove.
type AccountsRemoveOutput struct {
	Removed       string `json:"removed"`
	DryRun        bool   `json:"dry_run"`
	SecretDeleted bool   `json:"secret_deleted"`
}

// AccountsRemove entfernt ein Konto und sein Passwort. Solange Nachrichten
// archiviert sind, verweigert sie das: Der Server löscht, das Archiv behält
// (ADR-003); Archivlöschung ist eine eigene Operation (Löschkonzept, S3).
func AccountsRemove(repo AccountRepo, sec secrets.Store) Operation {
	return Operation{
		Name:        "accounts.remove",
		Scope:       policy.ScopeAdmin,
		Risk:        policy.R1,
		Summary:     "Remove an account without archived messages and delete its stored password.",
		InputSchema: json.RawMessage(accountNameSchema),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "removed": {"type": "string"},
    "dry_run": {"type": "boolean"},
    "secret_deleted": {"type": "boolean"}
  },
  "required": ["removed", "dry_run", "secret_deleted"]
}`),
		Redact: redactAccountInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := validAccountName(in.Name); err != nil {
				return nil, err
			}
			a, err := repo.AccountByName(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			n, err := repo.CountMessages(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, fmt.Errorf("account %s has %d archived messages; deleting the archive is a separate operation (not available yet)", a.Name, n)
			}
			out := AccountsRemoveOutput{Removed: a.Name, DryRun: call.DryRun}
			if call.DryRun {
				return out, nil
			}
			if err := repo.DeleteAccount(ctx, a.ID); err != nil {
				return nil, err
			}
			// Erst die Zeile, dann das Geheimnis: Ein verwaister Eintrag im
			// Schlüsselbund ist harmloser als ein Konto ohne Passwort.
			switch err := sec.Delete(a.CredentialRef); {
			case err == nil:
				out.SecretDeleted = true
			case errors.Is(err, secrets.ErrNotFound):
			default:
				return out, fmt.Errorf("account %s removed, but its keyring entry %s could not be deleted: %w", a.Name, a.CredentialRef, err)
			}
			return out, nil
		},
	}
}

// --- accounts.test ---------------------------------------------------------

// AccountsTestOutput ist die Ausgabe von accounts.test.
type AccountsTestOutput struct {
	Account string `json:"account"`
	Login   string `json:"login"`
	ProbeResult
	DryRun bool `json:"dry_run"`
}

// ErrNoSecret: Für das Konto ist kein Passwort hinterlegt.
var ErrNoSecret = errors.New("no password stored for this account")

// AccountsTest meldet sich am Server an und liest Fähigkeiten und Ordner.
// Sie verändert am Server nichts; lokal speichert sie Fähigkeiten und Status
// am Konto (deshalb R1; im Trockenlauf nicht).
func AccountsTest(repo AccountRepo, sec secrets.Store, prober AccountProber) Operation {
	return Operation{
		Name:        "accounts.test",
		Scope:       policy.ScopeAdmin,
		Risk:        policy.R1,
		Idempotent:  true,
		Summary:     "Log in to an account and report capabilities and mailboxes (read-only on the server).",
		InputSchema: json.RawMessage(accountNameSchema),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "login": {"type": "string", "enum": ["ok"]},
    "capabilities": {"type": "array", "items": {"type": "string"}},
    "sync_strategy": {"type": "string"},
    "mailboxes": {"type": "array", "items": {"type": "object",
      "properties": {"path": {"type": "string"}, "role": {"type": "string"}}, "required": ["path", "role"]}},
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "login", "capabilities", "sync_strategy", "mailboxes", "dry_run"]
}`),
		Redact: redactAccountInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := validAccountName(in.Name); err != nil {
				return nil, err
			}
			a, err := repo.AccountByName(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			pw, err := sec.Get(a.CredentialRef)
			if errors.Is(err, secrets.ErrNotFound) {
				return nil, fmt.Errorf("%w: %s (keyring entry %s or environment variable %s)", ErrNoSecret, a.Name, a.CredentialRef, secrets.EnvName(a.CredentialRef))
			} else if err != nil {
				return nil, err
			}
			res, perr := prober.Probe(ctx, a, pw)
			if perr != nil {
				// Defensiv: Ein Serverfehler darf das Passwort nie weitertragen.
				perr = scrub(perr, pw)
				if !call.DryRun {
					_ = repo.UpdateAccountProbe(ctx, a.ID, a.Capabilities, "error", perr.Error())
				}
				return nil, perr
			}
			if !call.DryRun {
				if err := repo.UpdateAccountProbe(ctx, a.ID, res.CapabilitiesJSON, "ok", ""); err != nil {
					return nil, err
				}
			}
			if res.Capabilities == nil {
				res.Capabilities = []string{}
			}
			if res.Mailboxes == nil {
				res.Mailboxes = []ProbeMailbox{}
			}
			return AccountsTestOutput{Account: a.Name, Login: "ok", ProbeResult: res, DryRun: call.DryRun}, nil
		},
	}
}

// scrubbedError ersetzt den Text eines Fehlers, behält aber die Kette für errors.Is.
type scrubbedError struct {
	msg string
	err error
}

func (e scrubbedError) Error() string { return e.msg }
func (e scrubbedError) Unwrap() error { return e.err }

func scrub(err error, pw secrets.Secret) error {
	if pw.IsZero() || !strings.Contains(err.Error(), pw.Reveal()) {
		return err
	}
	return scrubbedError{msg: strings.ReplaceAll(err.Error(), pw.Reveal(), "[redacted]"), err: err}
}
