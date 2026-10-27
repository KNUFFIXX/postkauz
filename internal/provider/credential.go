package provider

import (
	"context"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Credential ist, womit sich ein Konto am Server anmeldet: ein Passwort
// (LOGIN) oder für OAuth2-Konten eine Quelle für Access-Tokens (SASL
// OAUTHBEARER bzw. XOAUTH2, S3.7). Genau eines von beiden ist gesetzt.
type Credential struct {
	Password secrets.Secret
	// Token liefert vor jeder Anmeldung ein gültiges Access-Token und
	// erneuert es bei Bedarf (internal/oauth.Source). nil = Passwort.
	Token TokenSource
}

// TokenSource liefert ein gültiges Access-Token. Ein Fehler ist endgültig
// für diesen Anmeldeversuch (kein Wiederholen mit Backoff).
type TokenSource interface {
	Token(ctx context.Context) (secrets.Secret, error)
}

// PasswordCredential ist die Anmeldung mit Passwort.
func PasswordCredential(pw secrets.Secret) Credential { return Credential{Password: pw} }

// IsOAuth meldet die Anmeldung per Token.
func (c Credential) IsOAuth() bool { return c.Token != nil }
