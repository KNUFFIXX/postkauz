package imapx

import (
	"errors"
	"fmt"

	"github.com/emersion/go-sasl"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// OAuth2 über SASL (Prüfpunkt P3, Entwurf). Zwei Mechanismen sind verbreitet:
//
//   - XOAUTH2 (Google, Microsoft): nicht standardisiert, in go-sasl nicht
//     enthalten; hier selbst umgesetzt (wenige Zeilen).
//   - OAUTHBEARER (RFC 7628): aus go-sasl.
//
// Das Token liefert eine oauth.TokenSource (internal/oauth), die bei Bedarf
// erneuert. Kontenanlage mit auth_type=oauth2 ist noch gesperrt; die
// Verdrahtung folgt, sobald „bring your own client" (Client-ID des Nutzers,
// Loopback-Flow) entschieden und gebaut ist.

// Mechanismen-Namen.
const (
	MechXOAuth2     = "XOAUTH2"
	MechOAuthBearer = "OAUTHBEARER"
)

// NewXOAuth2Client liefert einen SASL-Client für XOAUTH2.
// Format der Anfangsantwort: "user=" user "\x01auth=Bearer " token "\x01\x01".
func NewXOAuth2Client(user string, token secrets.Secret) sasl.Client {
	return &xoauth2Client{user: user, token: token}
}

type xoauth2Client struct {
	user  string
	token secrets.Secret
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	if c.user == "" || c.token.IsZero() {
		return "", nil, errors.New("imapx: XOAUTH2 needs user and token")
	}
	return MechXOAuth2, []byte("user=" + c.user + "\x01auth=Bearer " + c.token.Reveal() + "\x01\x01"), nil
}

// Next: Bei einem Fehler schickt der Server eine JSON-Fehlerbeschreibung als
// Challenge; der Client muss mit einer leeren Antwort abschließen, danach
// meldet der Server NO. Den Inhalt (kann Scopes nennen) geben wir nicht weiter.
func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	return []byte{}, nil
}

// NewOAuthBearerClient liefert einen SASL-Client für OAUTHBEARER (RFC 7628).
func NewOAuthBearerClient(user, host string, port int, token secrets.Secret) sasl.Client {
	return oauthBearerClient{sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{Username: user, Token: token.Reveal(), Host: host, Port: port})}
}

// oauthBearerClient beantwortet die Fehler-Challenge des Servers mit %x01
// (RFC 7628, Abschnitt 3.2.3), statt den Austausch abzubrechen wie go-sasl;
// so endet er sauber mit NO und wird als ErrAuth erkannt.
type oauthBearerClient struct{ sasl.Client }

func (c oauthBearerClient) Next([]byte) ([]byte, error) { return []byte{0x01}, nil }

// OAuthClient wählt den Mechanismus nach den Fähigkeiten des Servers:
// OAUTHBEARER (Standard) vor XOAUTH2.
func OAuthClient(caps []string, user, host string, port int, token secrets.Secret) (sasl.Client, error) {
	has := map[string]bool{}
	for _, c := range caps {
		has[c] = true
	}
	switch {
	case has["AUTH="+MechOAuthBearer]:
		return NewOAuthBearerClient(user, host, port, token), nil
	case has["AUTH="+MechXOAuth2]:
		return NewXOAuth2Client(user, token), nil
	default:
		return nil, fmt.Errorf("imapx: server offers neither %s nor %s", MechOAuthBearer, MechXOAuth2)
	}
}
