package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
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
	// ClientID und Tenant: öffentlicher OAuth2-Client (nur auth_type
	// oauth2, S3.7); kein Geheimnis.
	ClientID string `json:"client_id,omitempty"`
	Tenant   string `json:"tenant,omitempty"`
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
	// SetCredentialRef stellt den Schlüsselbund-Verweis um (S2.0/A2,
	// accounts.set mit neuem Passwort, Entscheid 36).
	SetCredentialRef(ctx context.Context, id int64, ref string) error
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
	// AuthMechanism: LOGIN, OAUTHBEARER oder XOAUTH2 (S3.7).
	AuthMechanism string `json:"auth_mechanism,omitempty"`
}

// AccountProber meldet sich an einem Konto an und liest Fähigkeiten und
// Ordner, ohne etwas zu verändern. Implementierung: internal/provider/imap.
type AccountProber interface {
	Probe(ctx context.Context, a store.Account, cred provider.Credential) (ProbeResult, error)
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
		// Die Meldung von encoding/json nennt unbekannte Feldnamen und Teile
		// der Eingabe; sie kommt weder in Fehler noch ins Log (B1).
		return inputErr("invalid input: does not match the input schema of the operation")
	}
	return nil
}

// AccountNameRE: Kleinbuchstaben, Ziffern, Bindestrich. Kein Unterstrich,
// damit die Umgebungsvariable POSTKAUZ_PASSWORD_<NAME> eindeutig bleibt.
var AccountNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func validAccountName(name string) error {
	if !AccountNameRE.MatchString(name) {
		return inputErr("invalid account name: use lowercase letters, digits and '-' (max 63)")
	}
	return nil
}

// redactAccountInput: Feldnamen und Kontoname, nie Werte wie Host oder Nutzer.
func redactAccountInput(raw json.RawMessage) (json.RawMessage, []string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	var targets []string
	var name string
	if v, ok := m["name"]; ok && json.Unmarshal(v, &name) == nil && AccountNameRE.MatchString(name) {
		targets = []string{"account:" + name}
	}
	b, _ := json.Marshal(redactFields(m))
	return b, targets
}

// inputFieldNames ist die Menge aller Feldnamen der obersten Ebene in den
// Eingabeschemas des Katalogs (einmal berechnet). Nur solche Namen kommen
// ins Audit; sie stammen aus dem Schema, nicht vom Aufrufer (K1).
// (Kein Paket-Initialisierer: Catalog verweist über die Redact-Funktionen
// zurück hierher.)
var (
	inputFieldsOnce sync.Once
	inputFields     map[string]bool
)

func inputFieldNames() map[string]bool {
	inputFieldsOnce.Do(func() {
		known := map[string]bool{}
		for _, op := range Catalog(Deps{}) {
			var s struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if json.Unmarshal(op.InputSchema, &s) == nil {
				for k := range s.Properties {
					known[k] = true
				}
			}
		}
		inputFields = known
	})
	return inputFields
}

// redactFields liefert die Parameter eines Audit-Eintrags mit den
// genannten Feldnamen: nur bekannte (sortiert); unbekannte nur gezählt
// (unknown_fields). Ein Schlüssel der Eingabe stammt vom Aufrufer und
// kann Inhalt tragen; ins Audit kommt er deshalb nie (Gegenprüfung S2.3, K1).
func redactFields(m map[string]json.RawMessage) map[string]any {
	known := inputFieldNames()
	fields := make([]string, 0, len(m))
	unknown := 0
	for k := range m {
		if known[k] {
			fields = append(fields, k)
		} else {
			unknown++
		}
	}
	sort.Strings(fields)
	params := map[string]any{"fields": fields}
	if unknown > 0 {
		params["unknown_fields"] = unknown
	}
	return params
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
	// ClientID und Tenant: öffentlicher OAuth2-Client des Nutzers (nur mit
	// auth_type oauth2, S3.7, E11). Tenant: common (Standard), consumers,
	// organizations oder eine Mandanten-ID.
	ClientID string `json:"client_id,omitempty"`
	Tenant   string `json:"tenant,omitempty"`
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
		return inputErr("invalid host: give a host name or IP address, without spaces, '/', '\\' or '@'")
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
		return inputErr("invalid tls mode (expected tls, starttls or none)")
	}
	if in.InsecureTestServer && !IsLoopbackHost(in.Host) {
		return inputErr("insecure_test_server is only allowed for localhost test servers (localhost, 127.0.0.1, ::1)")
	}
	if in.AuthType == "" {
		in.AuthType = "password"
	}
	switch in.AuthType {
	case "password", "app_password":
		if in.ClientID != "" || in.Tenant != "" {
			return inputErr("client_id and tenant only apply with auth_type oauth2")
		}
	case AuthOAuth2:
		if in.PasswordFromEnv {
			return inputErr("auth_type oauth2 uses no password; password_from_env does not apply")
		}
		if in.Tenant == "" {
			in.Tenant = oauth.DefaultTenant
		}
		if in.ClientID == "" {
			return inputErr("auth_type oauth2 needs client_id: the Application (client) ID of your own app registration (see docs/betrieb_microsoft.md)")
		}
		if err := (oauth.Client{ClientID: in.ClientID, Tenant: in.Tenant}).Validate(); err != nil {
			return inputErr("%s", err.Error())
		}
	default:
		return inputErr("invalid auth_type (expected password, app_password or oauth2)")
	}
	if in.Email == "" && strings.Contains(in.Username, "@") {
		in.Email = in.Username
	}
	return nil
}

// AccountsAdd legt ein Konto an; das Passwort landet im Schlüsselbund (bzw.
// dem gewählten Speicher), in der Datenbank nur der Verweis. R1, Scope admin.
// ms liefert die Anmeldeadressen für OAuth2-Konten (insecure-file nur mit
// lokalem Fake-AS, Gegenprüfung T4 K3).
func AccountsAdd(repo AccountRepo, sec secrets.Store, ms oauth.Microsoft) Operation {
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
    "auth_type": {"type": "string", "enum": ["password", "app_password", "oauth2"]},
    "insecure_test_server": {"type": "boolean"},
    "password_from_env": {"type": "boolean"},
    "client_id": {"type": "string", "description": "oauth2 only: Application (client) ID of your own app registration"},
    "tenant": {"type": "string", "description": "oauth2 only: common (default), consumers, organizations or a tenant id"}
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
        "insecure_test_server": {"type": "boolean"}, "status": {"type": "string"},
        "client_id": {"type": "string"}, "tenant": {"type": "string"}
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
			oauth2 := in.AuthType == AuthOAuth2
			if oauth2 {
				// Rückfrage R2: nur mit Schlüsselbund (insecure-file nur für
				// lokale Testserver); das Token kommt erst mit account login,
				// hier wird nichts gespeichert.
				if !call.Secret.IsZero() {
					return nil, inputErr("auth_type oauth2 uses no password; sign in with account login after adding the account")
				}
				if _, err := oauthStoreAllowed(sec, in.InsecureTestServer, ms, oauth.Client{ClientID: in.ClientID, Tenant: in.Tenant}); err != nil {
					return nil, err
				}
			} else if in.PasswordFromEnv {
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
			if oauth2 {
				acct.OAuth = &store.OAuthClient{ClientID: in.ClientID, Tenant: in.Tenant}
			}
			out := AccountsAddOutput{DryRun: call.DryRun, Account: detail(acct, source)}
			if call.DryRun {
				return out, nil
			}
			stored := false
			if !in.PasswordFromEnv && !oauth2 {
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
	d := AccountDetail{ID: a.ID, Name: a.Name, Email: a.Email, Provider: a.Provider, Host: a.Host, Port: a.Port,
		TLS: a.TLS, Username: a.Username, AuthType: a.AuthType, CredentialRef: a.CredentialRef,
		SecretSource: source, InsecureTestServer: a.InsecureTestServer, Status: a.Status}
	if a.OAuth != nil {
		d.ClientID, d.Tenant = a.OAuth.ClientID, a.OAuth.Tenant
	}
	return d
}

// --- accounts.set ----------------------------------------------------------

// MaxAliases begrenzt die weiteren eigenen Adressen je Konto, gezählt nach
// dem Entfernen von Dubletten und eigenen Adressen. Die Eingabe selbst darf
// mehr Einträge haben, höchstens 200 (maxItems im Schema, Schutz vor
// Riesenlisten); vorher griff maxItems 50 schon vor dem Entfernen.
const MaxAliases = 50

// AccountsSetInput ist die Eingabe von accounts.set. Fehlt ein Feld, bleibt
// der Wert, wie er ist; mindestens ein Feld außer name ist nötig.
type AccountsSetInput struct {
	Name string `json:"name"`
	// Aliases ersetzt die Liste vollständig ([] leert sie).
	Aliases *[]string `json:"aliases,omitempty"`
	// ReplacePassword: Das neue Passwort steht in Call.Secret (CLI:
	// --password-stdin) und ersetzt das gespeicherte (Entscheid 36). Das
	// Passwort selbst steht nie in der Eingabe.
	ReplacePassword bool `json:"replace_password,omitempty"`
}

// AccountsSetOutput ist die Ausgabe von accounts.set. Sie enthält nie ein
// Passwort, nur wohin es geschrieben wurde.
type AccountsSetOutput struct {
	Account string   `json:"account"`
	Aliases []string `json:"aliases"`
	// AliasesChanged: Die Eingabe enthielt aliases.
	AliasesChanged bool `json:"aliases_changed"`
	// Password: "" (unverändert) | replaced | would_replace (Trockenlauf).
	Password string `json:"password,omitempty"`
	// CredentialRef und SecretStore: wohin das Passwort ging (Verweis im
	// Schlüsselbund; keyring | insecure-file).
	CredentialRef string `json:"credential_ref,omitempty"`
	SecretStore   string `json:"secret_store,omitempty"`
	// EnvOverride nennt POSTKAUZ_PASSWORD_<NAME>, wenn die Variable gesetzt
	// ist: Sie geht dem gespeicherten Passwort zur Laufzeit vor.
	EnvOverride string `json:"env_override,omitempty"`
	DryRun      bool   `json:"dry_run"`
}

// normalizeAliases prüft die Adressen (RFC 5322, nur die Adresse ohne
// Namen), schreibt sie klein, entfernt Dubletten und die schon eigenen
// Adressen des Kontos (Kontoadresse, Anmeldename; ohne Rücksicht auf
// Groß- und Kleinschreibung). Die Grenze MaxAliases gilt danach.
func normalizeAliases(in []string, own ...string) ([]string, error) {
	out := []string{}
	for i := range own {
		own[i] = strings.ToLower(strings.TrimSpace(own[i]))
	}
	for i, raw := range in {
		s := strings.TrimSpace(raw)
		addr, err := mail.ParseAddress(s)
		if err != nil || addr.Name != "" || addr.Address != s || !strings.Contains(s, "@") {
			// Nie die Adresse selbst nennen (B1), nur ihre Stelle.
			return nil, inputErr("invalid alias (aliases[%d]): give a plain e-mail address such as alias@example.org", i)
		}
		a := strings.ToLower(addr.Address)
		if slices.Contains(own, a) || slices.Contains(out, a) {
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

// AccountsSet ändert Einstellungen eines Kontos: die weiteren eigenen
// Adressen (Aliasse, S2.0) und seit S2.1 das gespeicherte Passwort
// (Entscheid 36). R1 (lokal), Scope admin, nur für Menschen (A02, E2):
// Kein Schlüssel, auch nicht admin, darf eigene Adressen umdefinieren (sie
// steuern ab S2, wen „Allen antworten" weglässt) oder Zugangsdaten setzen.
//
// Das neue Passwort kommt nur über Call.Secret (nie in der Eingabe, also
// nie im Audit) und geht in den Schlüsselbund bzw. den gewählten Speicher
// unter account/<datadir-id>/<name>; danach wird es zurückgelesen und
// verglichen. Hatte das Konto noch einen anderen Verweis (alter Name von
// vor S2.0, Umzug ausstehend), zeigt es danach auf den neuen; der alte
// Eintrag bleibt unangetastet (ein anderes Datenverzeichnis einer älteren
// Version kann ihn noch nutzen; doctor meldet ihn).
func AccountsSet(repo AccountRepo, sec secrets.Store) Operation {
	return Operation{
		Name:       "accounts.set",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R1,
		Idempotent: true,
		HumanOnly:  true,
		Summary:    "Change account settings: the further own addresses (aliases) and the stored password. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "aliases": {"type": "array", "items": {"type": "string"}, "maxItems": 200},
    "replace_password": {"type": "boolean", "description": "replace the stored password with the secret passed separately (CLI: --password-stdin); the password is never part of the input"}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "aliases": {"type": "array", "items": {"type": "string"}},
    "aliases_changed": {"type": "boolean"},
    "password": {"type": "string", "enum": ["replaced", "would_replace"]},
    "credential_ref": {"type": "string"},
    "secret_store": {"type": "string"},
    "env_override": {"type": "string"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "aliases", "aliases_changed", "dry_run"]
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
			switch {
			case in.Aliases == nil && !in.ReplacePassword:
				return nil, inputErr("nothing to change: give aliases (an empty list removes them) or a new password")
			case in.ReplacePassword && call.Secret.IsZero():
				return nil, inputErr("replace_password needs the new password (CLI: --password-stdin)")
			case !in.ReplacePassword && !call.Secret.IsZero():
				return nil, inputErr("a password was passed without replace_password")
			}
			a, err := repo.AccountByName(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			out := AccountsSetOutput{Account: a.Name, Aliases: a.Aliases, DryRun: call.DryRun}
			if out.Aliases == nil {
				out.Aliases = []string{}
			}
			if in.Aliases != nil {
				if out.Aliases, err = normalizeAliases(*in.Aliases, a.Email, a.Username); err != nil {
					return nil, err
				}
				out.AliasesChanged = true
			}
			if in.ReplacePassword && a.AuthType == AuthOAuth2 {
				return nil, inputErr("account %s signs in with OAuth2 and has no password; sign in again with: account login %s", a.Name, a.Name)
			}
			if in.ReplacePassword {
				dirID, err := repo.DataDirID(ctx)
				if err != nil {
					return nil, err
				}
				out.CredentialRef, out.SecretStore, out.Password = secrets.AccountRef(dirID, a.Name), sec.Status().Backend, "would_replace"
				if v, ok := os.LookupEnv(secrets.EnvName(out.CredentialRef)); ok && v != "" {
					out.EnvOverride = secrets.EnvName(out.CredentialRef)
				}
			}
			if call.DryRun {
				return out, nil
			}
			// Erst das Passwort (scheitert eher, z. B. ohne Schlüsselbund),
			// dann die Aliasse: Scheitert das Passwort, ändert sich nichts.
			if in.ReplacePassword {
				if err := replacePassword(ctx, repo, sec, a, out.CredentialRef, call.Secret); err != nil {
					return nil, scrub(err, call.Secret)
				}
				out.Password = "replaced"
			}
			if in.Aliases != nil {
				if err := repo.SetAccountAliases(ctx, a.ID, out.Aliases); err != nil {
					return nil, err
				}
			}
			return out, nil
		},
	}
}

// replacePassword schreibt das neue Passwort unter ref, liest es zurück
// (unter der Umgebungsschicht, secrets.Stored) und stellt danach den
// Verweis des Kontos um, falls er noch anders lautet.
func replacePassword(ctx context.Context, repo AccountRepo, sec secrets.Store, a store.Account, ref string, pw secrets.Secret) error {
	base := secrets.Stored(sec)
	if err := base.Set(ref, pw); err != nil {
		return fmt.Errorf("account %s: password not stored: %w", a.Name, err)
	}
	if got, err := base.Get(ref); err != nil || got.Reveal() != pw.Reveal() {
		if err == nil {
			err = errors.New("value differs after writing")
		}
		return fmt.Errorf("account %s: stored password could not be verified (%s): %w", a.Name, ref, err)
	}
	if a.CredentialRef != ref {
		if err := repo.SetCredentialRef(ctx, a.ID, ref); err != nil {
			return fmt.Errorf("account %s: password stored as %s, but the account still points to %s: %w", a.Name, ref, a.CredentialRef, err)
		}
	}
	return nil
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
			del := sec.Delete
			if a.AuthType == AuthOAuth2 {
				// Haupteintrag und Teile des Tokens (S3.7).
				del = func(ref string) error { return oauth.Delete(sec, ref) }
			}
			switch err := del(a.CredentialRef); {
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
func AccountsTest(repo AccountRepo, creds Credentials, prober AccountProber) Operation {
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
    "auth_mechanism": {"type": "string", "description": "how the login went: LOGIN, OAUTHBEARER or XOAUTH2"},
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
			cred, err := creds.For(a, call.DryRun)
			if err != nil {
				return nil, err
			}
			res, perr := prober.Probe(ctx, a, cred)
			if perr != nil {
				// Defensiv: Ein Serverfehler darf das Passwort nie weitertragen.
				perr = scrub(perr, cred.Password)
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
