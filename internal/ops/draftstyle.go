package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// drafts.style (S2.8, A02 E9): Stilhinweise einer Identität für Agenten,
// die Entwürfe schreiben (Resource mail://style/{identity}). Der Mensch
// hinterlegt sie als Datei (identity add --style-file); gelesen wird der
// Inhalt beim Aufruf, höchstens max_chars Zeichen. Quelle ist die
// Konfiguration des Menschen, nicht eine E-Mail: Die Ausgabe trägt
// source "config" und steht nicht in der Untrusted-Hülle.
//
// Absichtlich nicht identities.*: Die Identitätsoperationen sind nur für
// Menschen (A02 E2); diese hier liest nur und gehört zum Scope mail:draft
// wie das Schreiben von Entwürfen.

// DraftsStyleOutput ist die Ausgabe von drafts.style.
type DraftsStyleOutput struct {
	Source     string `json:"source"` // config
	Account    string `json:"account"`
	IdentityID int64  `json:"identity_id"`
	Identity   string `json:"identity"` // Adresse der Identität
	// HasStyleFile: Für die Identität ist eine Stildatei hinterlegt.
	HasStyleFile bool   `json:"has_style_file"`
	Text         string `json:"text"`
	Truncated    bool   `json:"truncated"`
}

// DraftsStyle liest die Stildatei einer Identität (R0, mail:draft).
func DraftsStyle(d Deps) Operation {
	o := newDraftOps(d)
	budget := d.MaxChars
	if budget <= 0 {
		budget = DefaultMaxChars
	}
	return Operation{
		Name:       "drafts.style",
		Scope:      policy.ScopeMailDraft,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show the style notes of a sender identity (the file the user set with identity add --style-file), as a guide for writing drafts.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "identity": {"type": "string", "description": "address or id of the identity (default: the default identity of the account)"},
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "account name (default: the account of the identity, or the only account)"},
    "max_chars": {"type": "integer", "minimum": 1, "maximum": 1000000, "description": "cut the text after this many characters (default: the character budget of the policy file)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "source": {"type": "string", "enum": ["config"]}, "account": {"type": "string"},
    "identity_id": {"type": "integer"}, "identity": {"type": "string"},
    "has_style_file": {"type": "boolean"}, "text": {"type": "string"}, "truncated": {"type": "boolean"}
  },
  "required": ["source", "account", "identity_id", "identity", "has_style_file", "text", "truncated"]
}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Identity string `json:"identity,omitempty"`
				Account  string `json:"account,omitempty"`
				MaxChars int    `json:"max_chars,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			max := in.MaxChars
			if max == 0 {
				max = budget
			}
			ident, err := o.styleIdentity(ctx, in.Account, in.Identity)
			if err != nil {
				return nil, err
			}
			acc, err := o.accountByID(ctx, ident.AccountID)
			if err != nil {
				return nil, err
			}
			out := DraftsStyleOutput{Source: "config", Account: acc.Name, IdentityID: ident.ID, Identity: ident.Email,
				HasStyleFile: ident.StyleFile != ""}
			if ident.StyleFile != "" {
				if out.Text, out.Truncated, err = readStyleFile(ident.StyleFile, max); err != nil {
					// Ohne Pfad: Er nennt Ordner des Nutzers und geht über MCP an Agenten.
					why := ""
					if r := styleReadReason(err); r != "" {
						why = " (" + r + ")"
					}
					return nil, fmt.Errorf("the style file of identity %d cannot be read%s; check it with: identity list", ident.ID, why)
				}
			}
			return out, nil
		},
	}
}

// styleIdentity wählt die Identität für drafts.style: mit Konto wie beim
// Anlegen eines Entwurfs (Adresse, ID oder Standard); ohne Konto über alle
// Konten, solange die Wahl eindeutig ist.
func (o *draftOps) styleIdentity(ctx context.Context, account, sel string) (store.Identity, error) {
	sel = strings.TrimSpace(sel)
	if account != "" || sel == "" {
		acc, err := ResolveAccount(ctx, o.accounts, account)
		if err != nil {
			return store.Identity{}, err
		}
		list, err := o.svc.Store.Identities(ctx, acc.ID)
		if err != nil {
			return store.Identity{}, err
		}
		i, ok, err := pickIdentity(list, sel)
		switch {
		case err != nil:
			return store.Identity{}, err
		case !ok:
			return store.Identity{}, store.ErrIdentityNotFound
		}
		return i, nil
	}
	list, err := o.svc.Store.Identities(ctx, 0)
	if err != nil {
		return store.Identity{}, err
	}
	// Erst exakt, dann nach mailaddr.Key wie beim Anlegen eines Entwurfs
	// (zweite Nachbesserung T3, Punkt 9), über alle Konten.
	found, byKey := matchIdentities(list, sel)
	switch len(found) {
	case 0:
		return store.Identity{}, store.ErrIdentityNotFound
	case 1:
		return found[0], nil
	}
	accounts := map[int64]bool{}
	for _, i := range found {
		accounts[i.AccountID] = true
	}
	if byKey && len(accounts) < len(found) {
		return store.Identity{}, errAmbiguousSpelling(len(found))
	}
	return store.Identity{}, inputErr("the identity exists in %d accounts; choose one with account", len(found))
}

// Gründe, aus denen readStyleFile eine Stildatei nicht liest. Sie nennen
// keinen Pfad und dürfen deshalb in die Fehlermeldung (sie geht über MCP an
// Agenten).
var (
	errStyleSymlink    = errors.New("it is a symbolic link; give the file itself with identity add --style-file")
	errStyleNotRegular = errors.New("it is not a regular file")
	errStyleChanged    = errors.New("it was replaced while being opened")
)

// readStyleFile liest höchstens max Zeichen der Stildatei (ungültiges
// UTF-8 wird ersetzt). Nur eine gewöhnliche Datei, kein Symlink, keine
// FIFO und kein Gerät (Gegenprüfung S2.8, K2): os.Lstat prüft vor dem
// Öffnen, denn das Öffnen einer FIFO blockiert, bis jemand hineinschreibt
// (seit T2 zusätzlich ohne Warten geöffnet, falls sie danach kommt);
// nach dem Öffnen prüft f.Stat, dass es dieselbe gewöhnliche Datei ist
// (os.SameFile), falls sie dazwischen ausgetauscht wurde. Die Lesegrenze
// steht vor dem ersten Lesen fest.
func readStyleFile(path string, max int) (string, bool, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return "", false, err
	}
	switch {
	case before.Mode()&os.ModeSymlink != 0:
		return "", false, errStyleSymlink
	case !before.Mode().IsRegular():
		return "", false, errStyleNotRegular
	}
	// Ohne zu warten öffnen (openNonblock): Wird zwischen Lstat und Open
	// eine FIFO untergeschoben, blockiert das Öffnen nicht; die Prüfung
	// danach lehnt sie ab.
	f, err := os.OpenFile(path, os.O_RDONLY|openNonblock, 0)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if !fi.Mode().IsRegular() || !os.SameFile(before, fi) {
		return "", false, errStyleChanged
	}
	// Ein Zeichen hat in UTF-8 höchstens 4 Bytes: Ist die Datei länger als
	// limit, enthalten die gelesenen Bytes mehr als max Zeichen, und
	// truncateRunes meldet die Kürzung. Mehr wird nie gelesen.
	limit := int64(max)*utf8.UTFMax + 1
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", false, err
	}
	text, cut := truncateRunes(strings.ToValidUTF8(string(b), string(utf8.RuneError)), max)
	return text, cut, nil
}

// styleReadReason liefert den Grund ohne Pfad für die Fehlermeldung ("" =
// keiner, etwa bei einem Fehler des Betriebssystems, der den Pfad nennt).
func styleReadReason(err error) string {
	for _, e := range []error{errStyleSymlink, errStyleNotRegular, errStyleChanged} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "it does not exist"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "permission denied"
	}
	return ""
}
