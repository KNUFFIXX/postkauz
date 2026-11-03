package ops

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/remote"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Ziele der Brücke mcp --remote (A04 Teil 4). Adresse in der
// Konfigurationsdatei ([remote.<name>] url), Schlüssel nur im
// Schlüsselbund (Verweis remote/<name>; ohne Umgebungsschicht, eine
// Umgebungsvariable ersetzt ihn nicht). Alle vier Operationen nur für
// Menschen (admin, kein MCP-Tool): Wer ein Ziel einträgt, gibt dessen
// Schlüssel an jeden Prozess weiter, der mcp --remote startet.

// RemoteConfig liest und ändert die Abschnitte [remote.<name>]
// (config.RemoteFile).
type RemoteConfig interface {
	Remotes() (map[string]config.Remote, error)
	SetRemote(name, url string) error
	RemoveRemote(name string) (bool, error)
}

// RemoteChecker prüft ein Ziel über die Brücke (remote.Check).
type RemoteChecker func(ctx context.Context, name, url string, key secrets.Secret) (remote.TestResult, error)

// ErrRemoteNotFound: kein Abschnitt [remote.<name>] (CLI Exit 1, not_found).
var ErrRemoteNotFound = errors.New("remote not found")

// RemoteCheckError: remote.test hat den Server nicht erreicht oder er hat
// abgelehnt; Code ist der JSON-RPC-Code der Brücke (remote.Code…).
type RemoteCheckError struct {
	Code int64
	Msg  string
}

func (e *RemoteCheckError) Error() string { return e.Msg }

const remoteNameSchema = `{"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"}`

// RemoteAddInput ist die Eingabe von remote.add; der Schlüssel kommt als
// Call.Secret (CLI: --key-stdin), nie in der Eingabe.
type RemoteAddInput struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// RemoteAddOutput ist die Ausgabe von remote.add.
type RemoteAddOutput struct {
	Remote string `json:"remote"`
	URL    string `json:"url"`
	// Action: added, changed (Ziel gab es schon), would_add, would_change.
	Action        string `json:"action"`
	CredentialRef string `json:"credential_ref"`
	SecretStore   string `json:"secret_store"`
	// KeyVerified: Der Schlüssel wurde zurückgelesen und ist gleich.
	KeyVerified bool     `json:"key_verified"`
	ConfigFile  string   `json:"config_file,omitempty"`
	Notes       []string `json:"notes"`
	DryRun      bool     `json:"dry_run"`
}

// RemoteSummary ist ein Ziel in remote.list.
type RemoteSummary struct {
	Remote string `json:"remote"`
	URL    string `json:"url"`
	// Key: stored, missing oder unavailable (Schlüsselbund nicht lesbar).
	Key string `json:"key"`
}

// RemoteListOutput ist die Ausgabe von remote.list.
type RemoteListOutput struct {
	Remotes []RemoteSummary `json:"remotes"`
}

// RemoteRemoveOutput ist die Ausgabe von remote.remove.
type RemoteRemoveOutput struct {
	Remote        string `json:"remote"`
	ConfigRemoved bool   `json:"config_removed"`
	KeyRemoved    bool   `json:"key_removed"`
	DryRun        bool   `json:"dry_run"`
}

// RemoteTestOutput ist die Ausgabe von remote.test (kein Mailinhalt).
type RemoteTestOutput struct {
	Remote string `json:"remote"`
	URL    string `json:"url"`
	remote.TestResult
}

func remoteInput(raw json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeInput(raw, &in); err != nil {
		return "", err
	}
	if err := config.CheckRemoteName(in.Name); err != nil {
		return "", inputErr("%s", err.Error())
	}
	return in.Name, nil
}

// redactRemote: nur der Name als Ziel, nie die Adresse (Knotenname im
// Tailnet) und nie der Schlüssel.
func redactRemote(raw json.RawMessage) (json.RawMessage, []string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	params := redactFields(m)
	var targets []string
	var name string
	if v, ok := m["name"]; ok && json.Unmarshal(v, &name) == nil && config.RemoteNameRE.MatchString(name) {
		targets = append(targets, "remote:"+name)
	}
	b, _ := json.Marshal(params)
	return b, targets
}

func remoteStore(sec secrets.Store) (secrets.Store, error) {
	if sec == nil {
		return nil, errors.New("remote: no secret store")
	}
	return secrets.Stored(sec), nil
}

// RemoteAdd trägt ein Ziel ein oder ersetzt es (Adresse und Schlüssel).
func RemoteAdd(cfg RemoteConfig, sec secrets.Store, configFile string) Operation {
	return Operation{
		Name:       "remote.add",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R1,
		Idempotent: true,
		HumanOnly:  true,
		Summary:    "Add or replace a remote PostKauz MCP endpoint for mcp --remote: URL in the configuration, API key only in the keyring. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": ` + remoteNameSchema + `,
    "url": {"type": "string", "minLength": 1, "maxLength": 2000, "description": "https URL of the MCP endpoint, e.g. https://office-pc.example.ts.net/mcp; http only to a loopback address"}
  },
  "required": ["name", "url"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "remote": {"type": "string"}, "url": {"type": "string"},
    "action": {"type": "string", "enum": ["added", "changed", "would_add", "would_change"]},
    "credential_ref": {"type": "string"}, "secret_store": {"type": "string"},
    "key_verified": {"type": "boolean"}, "config_file": {"type": "string"},
    "notes": {"type": "array", "items": {"type": "string"}},
    "dry_run": {"type": "boolean"}
  },
  "required": ["remote", "url", "action", "credential_ref", "secret_store", "key_verified", "notes", "dry_run"]
}`),
		Redact: redactRemote,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in RemoteAddInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := config.CheckRemoteName(in.Name); err != nil {
				return nil, inputErr("%s", err.Error())
			}
			u, err := config.CheckRemoteURL(in.URL)
			if err != nil {
				return nil, inputErr("%s", err.Error())
			}
			key := secrets.Secret(strings.TrimSpace(call.Secret.Reveal()))
			switch {
			case key.IsZero():
				return nil, inputErr("remote add needs the API key on standard input (CLI: --key-stdin); it is never part of the input")
			case !strings.HasPrefix(key.Reveal(), KeyPrefix) || strings.ContainsAny(key.Reveal(), " \t\r\n"):
				return nil, inputErr("the key on standard input does not look like a PostKauz API key (it starts with %s); create one on the other PC with: key create --name <name> --role assistant", KeyPrefix)
			}
			st, err := remoteStore(sec)
			if err != nil {
				return nil, err
			}
			have, err := cfg.Remotes()
			if err != nil {
				return nil, err
			}
			_, exists := have[in.Name]
			ref := remote.KeyRef(in.Name)
			out := RemoteAddOutput{Remote: in.Name, URL: in.URL, CredentialRef: ref, SecretStore: st.Status().Backend,
				ConfigFile: configFile, Notes: []string{}, DryRun: call.DryRun, Action: "added"}
			if exists {
				out.Action = "changed"
			}
			if !strings.HasSuffix(u.Path, "/mcp") {
				out.Notes = append(out.Notes, "the URL does not end in /mcp; postkauz mcp --http answers only at /mcp")
			}
			if call.DryRun {
				if exists {
					out.Action = "would_change"
				} else {
					out.Action = "would_add"
				}
				return out, nil
			}
			var old secrets.Secret
			if exists {
				old, _ = st.Get(ref)
			}
			if err := st.Set(ref, key); err != nil {
				return nil, fmt.Errorf("storing the key in the keyring: %w", err)
			}
			back, err := st.Get(ref)
			if err != nil || subtle.ConstantTimeCompare([]byte(back.Reveal()), []byte(key.Reveal())) != 1 {
				return nil, errors.New("the keyring did not give back the stored key; nothing was written to the configuration, run remote add again")
			}
			out.KeyVerified = true
			if err := cfg.SetRemote(in.Name, in.URL); err != nil {
				// Schlüsselbund zurück auf den alten Stand.
				if exists && !old.IsZero() {
					_ = st.Set(ref, old)
				} else if !exists {
					_ = st.Delete(ref)
				}
				return nil, err
			}
			return out, nil
		},
	}
}

// RemoteList zeigt die eingetragenen Ziele und ob ihr Schlüssel im
// Schlüsselbund liegt (nie den Schlüssel).
func RemoteList(cfg RemoteConfig, sec secrets.Store) Operation {
	return Operation{
		Name:      "remote.list",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R0,
		HumanOnly: true,
		Summary:   "List the remote MCP endpoints for mcp --remote and whether their key is in the keyring. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "remotes": {"type": "array", "items": {"type": "object", "properties": {
      "remote": {"type": "string"}, "url": {"type": "string"}, "key": {"type": "string", "enum": ["stored", "missing", "unavailable"]}},
      "required": ["remote", "url", "key"]}}
  },
  "required": ["remotes"]
}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			have, err := cfg.Remotes()
			if err != nil {
				return nil, err
			}
			st, err := remoteStore(sec)
			if err != nil {
				return nil, err
			}
			out := RemoteListOutput{Remotes: []RemoteSummary{}}
			names := make([]string, 0, len(have))
			for n := range have {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				state := "stored"
				if _, err := st.Get(remote.KeyRef(n)); errors.Is(err, secrets.ErrNotFound) {
					state = "missing"
				} else if err != nil {
					state = "unavailable"
				}
				out.Remotes = append(out.Remotes, RemoteSummary{Remote: n, URL: have[n].URL, Key: state})
			}
			return out, nil
		},
	}
}

// RemoteRemove entfernt ein Ziel aus Konfiguration und Schlüsselbund.
func RemoteRemove(cfg RemoteConfig, sec secrets.Store) Operation {
	return Operation{
		Name:       "remote.remove",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R1,
		Idempotent: true,
		HumanOnly:  true,
		Summary:    "Remove a remote MCP endpoint: its section in the configuration and its key in the keyring. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"name": ` + remoteNameSchema + `},
  "required": ["name"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "remote": {"type": "string"}, "config_removed": {"type": "boolean"}, "key_removed": {"type": "boolean"}, "dry_run": {"type": "boolean"}
  },
  "required": ["remote", "config_removed", "key_removed", "dry_run"]
}`),
		Redact: redactRemote,
		Handler: func(ctx context.Context, call Call) (any, error) {
			name, err := remoteInput(call.Input)
			if err != nil {
				return nil, err
			}
			have, err := cfg.Remotes()
			if err != nil {
				return nil, err
			}
			st, err := remoteStore(sec)
			if err != nil {
				return nil, err
			}
			_, inConfig := have[name]
			_, kerr := st.Get(remote.KeyRef(name))
			hasKey := kerr == nil
			if !inConfig && !hasKey {
				return nil, ErrRemoteNotFound
			}
			out := RemoteRemoveOutput{Remote: name, DryRun: call.DryRun}
			if call.DryRun {
				out.ConfigRemoved, out.KeyRemoved = inConfig, hasKey
				return out, nil
			}
			if inConfig {
				if out.ConfigRemoved, err = cfg.RemoveRemote(name); err != nil {
					return nil, err
				}
			}
			if hasKey {
				if err := st.Delete(remote.KeyRef(name)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
					return out, fmt.Errorf("removing the key from the keyring: %w", err)
				}
				out.KeyRemoved = true
			}
			return out, nil
		},
	}
}

// RemoteTest meldet sich über die Brücke beim Ziel an (initialize,
// tools/list, mail_get_policy) und zeigt Zahl der Tools, Name und Rolle des
// Schlüssels; kein Mailinhalt.
func RemoteTest(cfg RemoteConfig, sec secrets.Store, check RemoteChecker) Operation {
	return Operation{
		Name:      "remote.test",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R0,
		HumanOnly: true,
		Summary:   "Connect to a remote MCP endpoint through the same bridge as mcp --remote and show the number of tools and the role of the key. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"name": ` + remoteNameSchema + `},
  "required": ["name"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "remote": {"type": "string"}, "url": {"type": "string"},
    "protocol_version": {"type": "string"}, "server_name": {"type": "string"}, "server_version": {"type": "string"},
    "tools": {"type": "integer"}, "tool_names": {"type": "array", "items": {"type": "string"}},
    "key_name": {"type": "string"}, "role": {"type": "string"}
  },
  "required": ["remote", "url", "protocol_version", "server_name", "server_version", "tools", "tool_names"]
}`),
		Redact: redactRemote,
		Handler: func(ctx context.Context, call Call) (any, error) {
			name, err := remoteInput(call.Input)
			if err != nil {
				return nil, err
			}
			have, err := cfg.Remotes()
			if err != nil {
				return nil, err
			}
			r, ok := have[name]
			if !ok {
				return nil, ErrRemoteNotFound
			}
			if _, err := config.CheckRemoteURL(r.URL); err != nil {
				return nil, inputErr("[remote.%s]: %s", name, err.Error())
			}
			st, err := remoteStore(sec)
			if err != nil {
				return nil, err
			}
			key, err := st.Get(remote.KeyRef(name))
			if errors.Is(err, secrets.ErrNotFound) {
				return nil, &RemoteCheckError{Code: remote.CodeUnauthorized, Msg: "no key for remote " + name + " in the keyring; store it with: remote add " + name + " --url " + r.URL + " --key-stdin"}
			} else if err != nil {
				return nil, fmt.Errorf("reading the key from the keyring: %w", err)
			}
			if check == nil {
				check = func(ctx context.Context, name, url string, key secrets.Secret) (remote.TestResult, error) {
					return remote.Check(ctx, name, url, key, nil, nil)
				}
			}
			res, err := check(ctx, name, r.URL, key)
			if err != nil {
				var re *remote.Error
				if errors.As(err, &re) {
					return nil, &RemoteCheckError{Code: re.Code, Msg: scrub(re, key).Error()}
				}
				return nil, scrub(err, key)
			}
			return RemoteTestOutput{Remote: name, URL: r.URL, TestResult: res}, nil
		},
	}
}
