package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Identitäten (A02 E9): Absender eines Kontos mit Name, Adresse, Reply-To,
// Signatur (Text und HTML), Stildatei und Präfixen. Verwalten darf sie nur
// der Mensch ohne Schlüssel (HumanOnly, Scope admin): Eine Identität
// bestimmt den Absender jedes Entwurfs und zählt als eigene Adresse
// („Allen antworten" lässt sie weg).

// Grenzen einer Identität.
const (
	MaxSignatureChars = 20000
	MaxPrefixChars    = 50
)

// IdentitiesAddInput ist die Eingabe von identities.add. Signaturen kommen
// als Text (CLI: aus Dateien), style_file als Pfad.
type IdentitiesAddInput struct {
	Account       string `json:"account,omitempty"`
	Name          string `json:"name,omitempty"`
	Email         string `json:"email"`
	ReplyTo       string `json:"reply_to,omitempty"`
	SignatureText string `json:"signature_text,omitempty"`
	SignatureHTML string `json:"signature_html,omitempty"`
	StyleFile     string `json:"style_file,omitempty"`
	ReplyPrefix   string `json:"reply_prefix,omitempty"`
	ForwardPrefix string `json:"forward_prefix,omitempty"`
	Default       bool   `json:"default,omitempty"`
}

// IdentityOutput beschreibt eine Identität (ohne den Text der Signatur).
type IdentityOutput struct {
	ID            int64  `json:"id"`
	Account       string `json:"account"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	ReplyTo       string `json:"reply_to,omitempty"`
	Default       bool   `json:"default"`
	Signature     string `json:"signature"` // none | text | html | text+html
	StyleFile     string `json:"style_file,omitempty"`
	ReplyPrefix   string `json:"reply_prefix"`
	ForwardPrefix string `json:"forward_prefix"`
}

const identityOutputProps = `"id": {"type": "integer"}, "account": {"type": "string"}, "name": {"type": "string"},
      "email": {"type": "string"}, "reply_to": {"type": "string"}, "default": {"type": "boolean"},
      "signature": {"type": "string", "enum": ["none", "text", "html", "text+html"]}, "style_file": {"type": "string"},
      "reply_prefix": {"type": "string"}, "forward_prefix": {"type": "string"}`

const identityOutputSchema = `{"type": "object", "properties": {` + identityOutputProps + `},
      "required": ["id", "account", "name", "email", "default", "signature", "reply_prefix", "forward_prefix"]}`

func identityOutput(i store.Identity, account string) IdentityOutput {
	sig := "none"
	switch {
	case i.SignatureText != "" && i.SignatureHTML != "":
		sig = "text+html"
	case i.SignatureText != "":
		sig = "text"
	case i.SignatureHTML != "":
		sig = "html"
	}
	return IdentityOutput{ID: i.ID, Account: account, Name: i.Name, Email: i.Email, ReplyTo: i.ReplyTo, Default: i.IsDefault,
		Signature: sig, StyleFile: i.StyleFile, ReplyPrefix: i.ReplyPrefix, ForwardPrefix: i.ForwardPrefix}
}

// IdentityResultOutput ist die Ausgabe von identities.add, set_default
// und remove.
type IdentityResultOutput struct {
	Identity IdentityOutput `json:"identity"`
	DryRun   bool           `json:"dry_run"`
}

const identityResultSchema = `{
  "type": "object",
  "properties": {"identity": ` + identityOutputSchema + `, "dry_run": {"type": "boolean"}},
  "required": ["identity", "dry_run"]
}`

// IdentitiesListOutput ist die Ausgabe von identities.list.
type IdentitiesListOutput struct {
	Identities []IdentityOutput `json:"identities"`
}

func validPrefix(field, s string) error {
	if utf8.RuneCountInString(s) > MaxPrefixChars || strings.ContainsFunc(s, unicode.IsControl) {
		return inputErr("%s: at most %d characters, no line breaks", field, MaxPrefixChars)
	}
	return nil
}

// checkIdentity prüft eine neue Identität: Adressen (RFC 5322), Name ohne
// Zeilenumbrüche, Präfixe, Größe der Signatur, eine für die Message-ID
// taugliche Domain und einen Probebau mit dem Nachrichtenbau (versteckter
// Text in der HTML-Signatur → hidden_text_in_draft, wie später jeder
// Entwurf). Fehlermeldungen nennen Felder, nie Werte.
func checkIdentity(i store.Identity) error {
	if err := drafts.ValidateAddr(store.Addr{Name: i.Name, Address: i.Email}); err != nil {
		return draftErr(err)
	}
	if i.ReplyTo != "" {
		if err := drafts.ValidateAddr(store.Addr{Address: i.ReplyTo}); err != nil {
			return draftErr(err)
		}
	}
	for _, p := range []struct{ f, v string }{{"reply_prefix", i.ReplyPrefix}, {"forward_prefix", i.ForwardPrefix}} {
		if err := validPrefix(p.f, p.v); err != nil {
			return err
		}
	}
	for _, s := range []struct{ f, v string }{{"signature_text", i.SignatureText}, {"signature_html", i.SignatureHTML}} {
		if n := utf8.RuneCountInString(s.v); n > MaxSignatureChars {
			return &DeniedError{Reason: drafts.ReasonTooLarge, Msg: fmt.Sprintf("%s has %d characters (at most %d)", s.f, n, MaxSignatureChars)}
		}
	}
	if _, err := drafts.NewMessageID(bytes.NewReader(make([]byte, 10)), i.Email); err != nil {
		return inputErr("email: the domain cannot be used in a Message-ID")
	}
	in := compose.NewInput(compose.KindNew)
	in.Identity = composeIdentity(i)
	in.AccountAddress = i.Email
	if _, err := compose.Build(in); err != nil {
		return draftErr(err)
	}
	return nil
}

// IdentitiesAdd legt eine Identität an (nur Mensch; R1). Die erste eines
// Kontos oder mit default wird Standard.
func IdentitiesAdd(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:      "identities.add",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary:   "Add a sender identity to an account: name, address, reply-to, signature, style file, subject prefixes. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "name": {"type": "string", "description": "display name, at most 200 characters (longer: denied too_large)"},
    "email": {"type": "string", "minLength": 3},
    "reply_to": {"type": "string"},
    "signature_text": {"type": "string"},
    "signature_html": {"type": "string"},
    "style_file": {"type": "string", "description": "file with notes on tone and style (resource mail://style/{identity}, S2.8)"},
    "reply_prefix": {"type": "string", "description": "default \"Re: \""},
    "forward_prefix": {"type": "string", "description": "default \"Fwd: \""},
    "default": {"type": "boolean", "description": "make it the default identity of the account"}
  },
  "required": ["email"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(identityResultSchema),
		Redact:       redactIdentityInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in IdentitiesAddInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			acc, err := ResolveAccount(ctx, o.accounts, in.Account)
			if err != nil {
				return nil, err
			}
			i := store.Identity{AccountID: acc.ID, Name: strings.Join(strings.Fields(in.Name), " "), Email: strings.TrimSpace(in.Email),
				ReplyTo: strings.TrimSpace(in.ReplyTo), SignatureText: in.SignatureText, SignatureHTML: in.SignatureHTML,
				ReplyPrefix: in.ReplyPrefix, ForwardPrefix: in.ForwardPrefix, IsDefault: in.Default}
			if i.ReplyPrefix == "" {
				i.ReplyPrefix = compose.DefaultReplyPrefix
			}
			if i.ForwardPrefix == "" {
				i.ForwardPrefix = compose.DefaultForwardPrefix
			}
			if in.StyleFile != "" {
				p, err := filepath.Abs(in.StyleFile)
				if err != nil {
					return nil, inputErr("style_file: not a usable path")
				}
				// Lstat wie beim Lesen (drafts.style): kein Symlink, keine
				// FIFO (Gegenprüfung S2.8, K2).
				st, err := os.Lstat(p)
				switch {
				case err == nil && st.Mode()&os.ModeSymlink != 0:
					return nil, inputErr("style_file: a symbolic link is not allowed; give the file itself")
				case err != nil || !st.Mode().IsRegular():
					return nil, inputErr("style_file: not a readable regular file")
				}
				i.StyleFile = p
			}
			if err := checkIdentity(i); err != nil {
				return nil, err
			}
			list, err := o.svc.Store.Identities(ctx, acc.ID)
			if err != nil {
				return nil, err
			}
			// Dubletten nach mailaddr.Key: auch eine andere Gmail-Schreibweise
			// derselben Adresse (Punkte, Zusatz ab „+“, googlemail.com; zweite
			// Nachbesserung T3, Punkt 9).
			for _, x := range list {
				if mailaddr.Key(x.Email) == mailaddr.Key(i.Email) {
					return nil, inputErr("account %s already has identity %d with this address or another spelling of it (Gmail ignores dots and +tags); remove it first", acc.Name, x.ID)
				}
			}
			if len(list) == 0 {
				i.IsDefault = true
			}
			if call.DryRun {
				return IdentityResultOutput{Identity: identityOutput(i, acc.Name), DryRun: true}, nil
			}
			if i.ID, err = o.svc.Store.AddIdentity(ctx, i); err != nil {
				return nil, err
			}
			saved, err := o.svc.Store.Identity(ctx, i.ID)
			if err != nil {
				return nil, err
			}
			return IdentityResultOutput{Identity: identityOutput(saved, acc.Name)}, nil
		},
	}
}

// IdentitiesList listet die Identitäten (eines Kontos oder aller; nur
// Mensch, R0).
func IdentitiesList(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:       "identities.list",
		Scope:      policy.ScopeAdmin,
		Risk:       policy.R0,
		Idempotent: true,
		HumanOnly:  true,
		Summary:    "List the sender identities of an account (or of all accounts). Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"}},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"identities": {"type": "array", "items": ` + identityOutputSchema + `}},
  "required": ["identities"]
}`),
		Redact: redactIdentityInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Account string `json:"account,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			var accID int64
			if in.Account != "" {
				a, err := ResolveAccount(ctx, o.accounts, in.Account)
				if err != nil {
					return nil, err
				}
				accID = a.ID
			}
			list, err := o.svc.Store.Identities(ctx, accID)
			if err != nil {
				return nil, err
			}
			accs, err := o.accounts.ListAccounts(ctx)
			if err != nil {
				return nil, err
			}
			names := map[int64]string{}
			for _, a := range accs {
				names[a.ID] = a.Name
			}
			out := IdentitiesListOutput{Identities: []IdentityOutput{}}
			for _, i := range list {
				out.Identities = append(out.Identities, identityOutput(i, names[i.AccountID]))
			}
			return out, nil
		},
	}
}

// IdentitiesSetDefault macht eine Identität zur Standardidentität ihres
// Kontos (nur Mensch, R1).
func IdentitiesSetDefault(d Deps) Operation {
	return identityByID(d, "identities.set_default", "Make an identity the default identity of its account. Human only.")
}

// IdentitiesRemove entfernt eine Identität (nur Mensch, R1). Entwürfe
// behalten ihren Absender (Kopie); war sie Standard, wird die älteste
// verbleibende Standard.
func IdentitiesRemove(d Deps) Operation {
	return identityByID(d, "identities.remove", "Remove an identity; drafts keep their sender. Human only.")
}

func identityByID(d Deps, name, summary string) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:      name,
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary:   summary,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"id": {"type": "integer", "minimum": 1}},
  "required": ["id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(identityResultSchema),
		Redact:       redactIdentityInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ID int64 `json:"id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			i, err := o.svc.Store.Identity(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			acc, err := o.accountByID(ctx, i.AccountID)
			if err != nil {
				return nil, err
			}
			if call.DryRun {
				if name == "identities.set_default" {
					i.IsDefault = true
				}
				return IdentityResultOutput{Identity: identityOutput(i, acc.Name), DryRun: true}, nil
			}
			if name == "identities.set_default" {
				if err := o.svc.Store.SetDefaultIdentity(ctx, i.ID); err != nil {
					return nil, err
				}
				i.IsDefault = true
			} else if err := o.svc.Store.RemoveIdentity(ctx, i.ID); err != nil {
				return nil, err
			}
			return IdentityResultOutput{Identity: identityOutput(i, acc.Name)}, nil
		},
	}
}

// redactIdentityInput: Feldnamen, Identitäts-ID und Konto; nie Name,
// Adressen, Signatur oder Pfad.
func redactIdentityInput(raw json.RawMessage) (json.RawMessage, []string) {
	return redactDraftInput(raw)
}
