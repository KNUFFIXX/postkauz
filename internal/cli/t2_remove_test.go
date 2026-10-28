package cli

import (
	"bytes"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// T2 Punkt 7 (g): account remove nennt bei OAuth2-Konten das Token, nicht
// ein Passwort (Ausgabe der Operation: secret_kind).
func TestRenderAccountRemoveNamesTheSecret(t *testing.T) {
	for _, c := range []struct {
		out  ops.AccountsRemoveOutput
		want string
	}{
		{ops.AccountsRemoveOutput{Removed: "demo", SecretDeleted: true, SecretKind: "password"}, "account demo removed; stored password deleted\n"},
		{ops.AccountsRemoveOutput{Removed: "demo", SecretKind: "password"}, "account demo removed; no stored password found\n"},
		{ops.AccountsRemoveOutput{Removed: "outlook", SecretDeleted: true, SecretKind: "token"}, "account outlook removed; stored token deleted\n"},
		{ops.AccountsRemoveOutput{Removed: "outlook", SecretKind: "token"}, "account outlook removed; no stored token found\n"},
		{ops.AccountsRemoveOutput{Removed: "outlook", DryRun: true, SecretKind: "token"}, "dry run: would remove account outlook and its stored token\n"},
	} {
		var b bytes.Buffer
		if err := renderAccountRemove(&b, c.out); err != nil || b.String() != c.want {
			t.Fatalf("%+v: %q %v, want %q", c.out, b.String(), err, c.want)
		}
	}
}
