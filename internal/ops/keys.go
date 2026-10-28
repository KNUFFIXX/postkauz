package ops

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// API-Schlüssel (S1.8, docs/sicherheit.md Abschnitt 3): Je Agent oder
// Client ein Schlüssel mit Rolle (Scopes), Ablauf und dry_run_default.
// Gespeichert wird nur der SHA-256 des Klartexts; der Klartext erscheint
// genau einmal, in der Ausgabe von keys.create. Verwaltet werden Schlüssel
// nur vom Menschen (Scope admin, nicht über MCP).

// KeyRepo ist der Teil des Stores, den die Schlüsseloperationen brauchen.
type KeyRepo interface {
	CreateAPIKey(ctx context.Context, k store.APIKey) (int64, error)
	APIKeys(ctx context.Context) ([]store.APIKey, error)
	APIKeyByName(ctx context.Context, name string) (store.APIKey, error)
	RevokeAPIKey(ctx context.Context, id int64, at time.Time) (bool, error)
	TouchAPIKey(ctx context.Context, id int64, at time.Time) error
}

// KeyPrefix steht vor jedem Schlüssel; so erkennt man ihn in Konfigurationen
// (und Werkzeuge zur Geheimnissuche finden ihn).
const KeyPrefix = brand.Alias + "_key_"

// DefaultKeyExpiryDays ist die Laufzeit neuer Schlüssel ohne Angabe.
const DefaultKeyExpiryDays = 90

// KeyInfo beschreibt einen Schlüssel ohne Klartext und ohne Hash.
type KeyInfo struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Role          string   `json:"role"`
	Scopes        []string `json:"scopes"`
	DryRunDefault bool     `json:"dry_run_default"`
	Status        string   `json:"status"` // active | expired | revoked
	CreatedAt     string   `json:"created_at,omitempty"`
	ExpiresAt     string   `json:"expires_at,omitempty"`
	LastUsedAt    string   `json:"last_used_at,omitempty"`
	RevokedAt     string   `json:"revoked_at,omitempty"`
}

const keyInfoSchema = `{"type": "object", "properties": {
      "id": {"type": "integer"}, "name": {"type": "string"},
      "role": {"type": "string", "enum": ["reader", "assistant", "operator", "admin"]},
      "scopes": {"type": "array", "items": {"type": "string"}}, "dry_run_default": {"type": "boolean"},
      "status": {"type": "string", "enum": ["active", "expired", "revoked"]},
      "created_at": {"type": "string", "format": "date-time"}, "expires_at": {"type": "string", "format": "date-time"},
      "last_used_at": {"type": "string", "format": "date-time"}, "revoked_at": {"type": "string", "format": "date-time"}},
      "required": ["id", "name", "role", "scopes", "dry_run_default", "status"]}`

// KeyStatus bestimmt den Zustand eines Schlüssels zum Zeitpunkt now.
func KeyStatus(k store.APIKey, now time.Time) string {
	switch {
	case !k.RevokedAt.IsZero():
		return "revoked"
	case !k.ExpiresAt.IsZero() && !now.Before(k.ExpiresAt):
		return "expired"
	}
	return "active"
}

func keyInfo(k store.APIKey, now time.Time) KeyInfo {
	info := KeyInfo{ID: k.ID, Name: k.Name, Role: k.Role, Scopes: k.Scopes, DryRunDefault: k.DryRunDefault,
		Status: KeyStatus(k, now), CreatedAt: rfc3339(k.CreatedAt), ExpiresAt: rfc3339(k.ExpiresAt),
		LastUsedAt: rfc3339(k.LastUsedAt), RevokedAt: rfc3339(k.RevokedAt)}
	if info.Scopes == nil {
		info.Scopes = []string{}
	}
	return info
}

// PolicyKey macht aus einem gespeicherten Schlüssel den Prüfschlüssel der
// Policy. Die ID im Audit ist "key:<name>" (Namen bleiben eindeutig).
func PolicyKey(k store.APIKey) policy.Key {
	scopes := make([]policy.Scope, 0, len(k.Scopes))
	for _, s := range k.Scopes {
		scopes = append(scopes, policy.Scope(s))
	}
	return policy.Key{ID: "key:" + k.Name, Name: k.Name, Role: k.Role, Scopes: scopes, DryRunDefault: k.DryRunDefault,
		ExpiresAt: k.ExpiresAt}
}

// HashKey liefert den gespeicherten Hash eines Schlüssels (SHA-256, hex).
func HashKey(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

// Fehler der Schlüsselprüfung. Sie nennen den Grund, aber nie den Schlüssel.
var (
	ErrKeyMissing = errors.New("no API key given")
	ErrKeyInvalid = errors.New("unknown API key")
	ErrKeyRevoked = errors.New("API key revoked")
	ErrKeyExpired = errors.New("API key expired")
)

// AuthenticateKey prüft einen Schlüssel im Klartext (MCP: POSTKAUZ_KEY) und
// liefert den Prüfschlüssel der Policy. Verglichen wird der Hash in
// konstanter Zeit gegen alle Schlüssel (wenige Zeilen); last_used_at wird
// gesetzt. Das ist keine Operation des Registers, sondern die Anmeldung
// davor: Der Executor braucht ihr Ergebnis.
func AuthenticateKey(ctx context.Context, repo KeyRepo, plain string, now time.Time) (policy.Key, error) {
	k, _, err := authenticateKey(ctx, repo, plain, now)
	return k, err
}

// OpAuthenticate ist die Operation im Audit für eine gescheiterte
// Anmeldung mit einem Schlüssel (kein Eintrag im Register).
const OpAuthenticate = "keys.authenticate"

// AuthenticateKeyAudited ist AuthenticateKey mit Audit (T1b): Scheitert die
// Anmeldung an einem unbekannten, widerrufenen oder abgelaufenen Schlüssel,
// steht ein Eintrag keys.authenticate mit Ergebnis denied und dem Grund
// (key_invalid, key_revoked, key_expired) im Audit, Akteur agent, Schlüssel
// key:<name>, wenn er bekannt ist; Parameter nur der Weg (transport, z. B.
// stdio). Nie der Schlüssel selbst und kein Teil davon. Scheitert das
// Schreiben, bleibt es bei der Ablehnung (die Anmeldung ist ohnehin
// gescheitert); der Fehler der Anmeldung geht an den Aufrufer.
func AuthenticateKeyAudited(ctx context.Context, repo KeyRepo, w audit.Writer, transport string, plain string, now time.Time) (policy.Key, error) {
	k, name, err := authenticateKey(ctx, repo, plain, now)
	if err == nil || w == nil {
		return k, err
	}
	reason := keyReason(err)
	if reason == "key_check_failed" {
		return k, err // Lesefehler der Datenbank: keine Aussage über den Schlüssel
	}
	id := ""
	if name != "" {
		id = "key:" + name
	}
	params, _ := json.Marshal(map[string]string{"transport": transport})
	_ = w.Append(ctx, audit.Event{TS: now.UTC(), ActorType: audit.ActorAgent, KeyID: id, Operation: OpAuthenticate,
		ParamsRedacted: params, Result: audit.ResultDenied, Reason: reason})
	return k, err
}

func authenticateKey(ctx context.Context, repo KeyRepo, plain string, now time.Time) (policy.Key, string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return policy.Key{}, "", ErrKeyMissing
	}
	want := []byte(HashKey(plain))
	keys, err := repo.APIKeys(ctx)
	if err != nil {
		return policy.Key{}, "", err
	}
	var found *store.APIKey
	for i := range keys {
		if subtle.ConstantTimeCompare([]byte(keys[i].KeyHash), want) == 1 {
			found = &keys[i]
		}
	}
	if found == nil {
		return policy.Key{}, "", ErrKeyInvalid
	}
	switch KeyStatus(*found, now) {
	case "revoked":
		return policy.Key{}, found.Name, fmt.Errorf("%w: %s", ErrKeyRevoked, found.Name)
	case "expired":
		return policy.Key{}, found.Name, fmt.Errorf("%w: %s", ErrKeyExpired, found.Name)
	}
	if err := repo.TouchAPIKey(ctx, found.ID, now); err != nil {
		return policy.Key{}, found.Name, err
	}
	return PolicyKey(*found), found.Name, nil
}

// KeyByName liefert den Prüfschlüssel eines aktiven Schlüssels per Namen
// (CLI --key: der Mensch prüft, was ein Schlüssel dürfte).
func KeyByName(ctx context.Context, repo KeyRepo, name string, now time.Time) (policy.Key, error) {
	k, err := repo.APIKeyByName(ctx, name)
	if err != nil {
		return policy.Key{}, err
	}
	switch KeyStatus(k, now) {
	case "revoked":
		return policy.Key{}, fmt.Errorf("%w: %s", ErrKeyRevoked, k.Name)
	case "expired":
		return policy.Key{}, fmt.Errorf("%w: %s", ErrKeyExpired, k.Name)
	}
	return PolicyKey(k), nil
}

func newKeySecret() (string, error) {
	b := make([]byte, 32) // 256 Bit
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// KeysCreateOutput ist die Ausgabe von keys.create. Secret ist der
// Klartext des neuen Schlüssels: Er steht nur hier, genau einmal (nicht im
// Trockenlauf, nie im Audit, nie in der Datenbank).
type KeysCreateOutput struct {
	Key    KeyInfo `json:"key"`
	Secret string  `json:"secret,omitempty"`
	DryRun bool    `json:"dry_run"`
}

func redactKeyName(raw json.RawMessage) (json.RawMessage, []string) {
	params, _ := redactAccountInput(raw)
	var in struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &in) == nil && AccountNameRE.MatchString(in.Name) {
		return params, []string{"key:" + in.Name}
	}
	return params, nil
}

// KeysCreate legt einen API-Schlüssel an (R1, admin).
//
// dryDefault ist der Standard für dry_run_default ohne Angabe
// ([agent_defaults] dry_run_default der Policy-Datei, seit T2; nil = true).
func KeysCreate(repo KeyRepo, dryDefault *bool) Operation {
	return Operation{
		Name:    "keys.create",
		Scope:   policy.ScopeAdmin,
		Risk:    policy.R1,
		Summary: "Create an API key for an agent; the key is shown exactly once, only its hash is stored.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "unique name, shown in the audit log"},
    "role": {"type": "string", "enum": ["reader", "assistant", "operator", "admin"], "description": "default assistant"},
    "scopes": {"type": "array", "items": {"type": "string"}, "description": "narrow the role to these scopes (never widen)"},
    "expires_in_days": {"type": "integer", "minimum": 0, "maximum": 3650, "description": "default 90; 0 = never"},
    "dry_run_default": {"type": "boolean", "description": "writing calls run as dry run unless dry_run=false (default: [agent_defaults] dry_run_default of the policy file, true without it)"}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "key": ` + keyInfoSchema + `,
    "secret": {"type": "string", "description": "the key in clear text; shown only here"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["key", "dry_run"]
}`),
		Redact: redactKeyName,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Name          string   `json:"name"`
				Role          string   `json:"role,omitempty"`
				Scopes        []string `json:"scopes,omitempty"`
				ExpiresInDays *int     `json:"expires_in_days,omitempty"`
				DryRunDefault *bool    `json:"dry_run_default,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if !AccountNameRE.MatchString(in.Name) {
				return nil, inputErr("invalid key name: use lowercase letters, digits and '-' (max 63)")
			}
			if in.Role == "" {
				in.Role = "assistant"
			}
			roleScopes, ok := policy.Roles[in.Role]
			if !ok {
				return nil, inputErr("unknown role (reader, assistant, operator, admin)")
			}
			scopes := make([]string, 0, len(roleScopes))
			for _, s := range roleScopes {
				scopes = append(scopes, string(s))
			}
			if len(in.Scopes) > 0 {
				// Verengen, nie erweitern (docs/sicherheit.md, Abschnitt 3).
				for _, s := range in.Scopes {
					if !slices.Contains(scopes, s) {
						return nil, inputErr("a scope is not part of role %s; scopes can only narrow a role (see: policy show)", in.Role)
					}
				}
				narrowed := []string{}
				for _, s := range scopes {
					if slices.Contains(in.Scopes, s) {
						narrowed = append(narrowed, s)
					}
				}
				scopes = narrowed
			}
			days := DefaultKeyExpiryDays
			if in.ExpiresInDays != nil {
				days = *in.ExpiresInDays
			}
			dry := true
			if dryDefault != nil {
				dry = *dryDefault
			}
			if in.DryRunDefault != nil {
				dry = *in.DryRunDefault
			}
			now := time.Now().UTC().Truncate(time.Millisecond)
			k := store.APIKey{Name: in.Name, Role: in.Role, Scopes: scopes, DryRunDefault: dry, CreatedAt: now}
			if days > 0 {
				k.ExpiresAt = now.Add(time.Duration(days) * 24 * time.Hour)
			}
			if _, err := repo.APIKeyByName(ctx, in.Name); err == nil {
				return nil, fmt.Errorf("%w: %s", store.ErrKeyExists, in.Name)
			} else if !errors.Is(err, store.ErrKeyNotFound) {
				return nil, err
			}
			out := KeysCreateOutput{DryRun: call.DryRun}
			if call.DryRun {
				out.Key = keyInfo(k, now)
				return out, nil
			}
			secret, err := newKeySecret()
			if err != nil {
				return nil, err
			}
			k.KeyHash = HashKey(secret)
			if k.ID, err = repo.CreateAPIKey(ctx, k); err != nil {
				return nil, err
			}
			out.Key, out.Secret = keyInfo(k, now), secret
			return out, nil
		},
	}
}

// KeysListOutput ist die Ausgabe von keys.list.
type KeysListOutput struct {
	Keys []KeyInfo `json:"keys"`
}

// KeysList listet die Schlüssel ohne Klartext und ohne Hash (R0, admin).
func KeysList(repo KeyRepo) Operation {
	return Operation{
		Name:       "keys.list",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "List API keys with role, scopes and status (never the key itself).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"keys": {"type": "array", "items": ` + keyInfoSchema + `}},
  "required": ["keys"]
}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			list, err := repo.APIKeys(ctx)
			if err != nil {
				return nil, err
			}
			now := time.Now()
			out := KeysListOutput{Keys: []KeyInfo{}}
			for _, k := range list {
				out.Keys = append(out.Keys, keyInfo(k, now))
			}
			return out, nil
		},
	}
}

// KeysRevokeOutput ist die Ausgabe von keys.revoke.
type KeysRevokeOutput struct {
	Name           string `json:"name"`
	Revoked        bool   `json:"revoked"`         // jetzt widerrufen
	AlreadyRevoked bool   `json:"already_revoked"` // war schon widerrufen
	DryRun         bool   `json:"dry_run"`
}

// KeysRevoke widerruft einen Schlüssel sofort (R1, admin). Der Name bleibt
// belegt, damit das Audit eindeutig bleibt.
func KeysRevoke(repo KeyRepo) Operation {
	return Operation{
		Name:        "keys.revoke",
		Scope:       policy.ScopeAdmin,
		Risk:        policy.R1,
		Idempotent:  true,
		Summary:     "Revoke an API key immediately (the name stays reserved for the audit log).",
		InputSchema: json.RawMessage(accountNameSchema),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string"}, "revoked": {"type": "boolean"}, "already_revoked": {"type": "boolean"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["name", "revoked", "already_revoked", "dry_run"]
}`),
		Redact: redactKeyName,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			k, err := repo.APIKeyByName(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			out := KeysRevokeOutput{Name: k.Name, AlreadyRevoked: !k.RevokedAt.IsZero(), DryRun: call.DryRun}
			if call.DryRun || out.AlreadyRevoked {
				return out, nil
			}
			if out.Revoked, err = repo.RevokeAPIKey(ctx, k.ID, time.Now()); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}
