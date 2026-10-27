// Package oauth: OAuth2-Zugangsdaten für Postfächer (Prüfpunkt P3, S3.7).
//
// Grundsätze (docs/sicherheit.md, Abschnitt 4): „bring your own client"
// (Client-ID des Nutzers, öffentlicher Client mit PKCE, kein Secret, keine
// eigene PostKauz-App); Token werden wie Passwörter behandelt
// (Schlüsselbund, nie in Datenbank, Log, Audit oder Ausgabe); ein
// fehlgeschlagener Refresh erzeugt einen Fehler, ein Audit-Ereignis und eine
// Sperrzeit, keine Schleife.
//
// Bausteine: Source (erneuernde Tokenquelle mit Sperrzeit), Save/Load
// (Ablage im Schlüsselbund, store.go), Microsoft (Endpunkte, Refresher,
// Loopback- und Device-Code-Flow, microsoft.go).
package oauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Token ist ein OAuth2-Token-Paar.
type Token struct {
	AccessToken  secrets.Secret
	RefreshToken secrets.Secret
	Expiry       time.Time // Nullzeit = unbekannt (gilt als gültig)
}

// Refresher tauscht ein Refresh-Token gegen ein neues Token (Token-Endpunkt
// des Anbieters). Liefert er kein neues Refresh-Token, bleibt das alte.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken secrets.Secret) (Token, error)
}

// ErrRefresh: Erneuerung fehlgeschlagen. Der Aufrufer meldet das (Audit,
// doctor) und versucht es erst nach der Sperrzeit wieder.
var ErrRefresh = errors.New("oauth: token refresh failed")

// ErrTokenNotStored: Ein neues Token kam an, ließ sich aber nicht im
// Schlüsselbund speichern. Die Quelle gibt dann kein Token mehr aus, bis das
// Speichern gelingt: Microsoft ersetzt das Refresh-Token bei jedem Refresh,
// ein Weiterlaufen nur im Speicher verlöre den Zugang beim nächsten Start
// still (A03_teile T4 Punkt 2).
var ErrTokenNotStored = errors.New("oauth: the renewed token could not be stored in the keyring")

// Codes für Report (Audit oauth.refresh, nie Token oder Anbietertext).
const (
	ReasonOK        = "ok"
	ReasonNoRefresh = "no_refresh_token"
	ReasonEmpty     = "empty_access_token"
	ReasonNotStored = "token_not_stored"
)

// Source liefert ein gültiges Access-Token und erneuert es kurz vor Ablauf.
type Source struct {
	mu        sync.Mutex
	tok       Token
	refresher Refresher
	// Margin: so lange vor Ablauf wird erneuert (Standard 2 Minuten).
	Margin time.Duration
	// Cooldown: Sperrzeit nach einem fehlgeschlagenen Refresh (Standard 5 Minuten).
	Cooldown time.Duration
	// OnRefresh wird nach jedem erfolgreichen Refresh aufgerufen (Token im
	// Schlüsselbund aktualisieren). Scheitert es, liefert Token
	// ErrTokenNotStored und versucht beim nächsten Aufruf nur das Speichern
	// erneut, keinen weiteren Refresh.
	OnRefresh func(Token) error
	// Report meldet jeden Refresh-Versuch: ok, dessen Code (ReasonOK,
	// ReasonNotStored, ein Code des Anbieters wie invalid_grant oder ein
	// fester Code). Für das Audit-Ereignis oauth.refresh; nie mit Token.
	Report func(ok bool, reason string)
	// Reload liest das gespeicherte Refresh-Token neu (nil = nie). Die
	// Quelle ruft es vor jedem Refresh (Gegenprüfung T4, K7): Weicht das
	// gespeicherte vom zuletzt gelesenen oder selbst gespeicherten ab, hat
	// ein anderer Prozess danach gespeichert (etwa sync --watch neben
	// account test); es ist also neuer, und die Quelle erneuert damit.
	// Fehler beim Lesen zählen nicht (dann mit dem eigenen).
	Reload func() (secrets.Secret, error)
	Now    func() time.Time

	failedAt time.Time
	lastErr  error
	// unstored: neues Token, das noch nicht gespeichert ist.
	unstored *Token
	// stored: das zuletzt gelesene oder selbst gespeicherte Refresh-Token.
	stored secrets.Secret
}

// NewSource legt eine Quelle für ein vorhandenes Token an. Ohne
// Access-Token (gespeichert ist nur das Refresh-Token) erneuert der erste
// Aufruf von Token.
func NewSource(tok Token, r Refresher) *Source {
	return &Source{tok: tok, refresher: r, stored: tok.RefreshToken}
}

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Source) report(ok bool, reason string) {
	if s.Report != nil {
		s.Report(ok, reason)
	}
}

// Expiry liefert den Ablauf des aktuellen Access-Tokens (Nullzeit =
// unbekannt oder noch keins).
func (s *Source) Expiry() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tok.Expiry
}

// Token liefert das Access-Token, nötigenfalls nach einem Refresh. Nach einem
// Fehlschlag liefert sie bis zum Ende der Sperrzeit sofort denselben Fehler.
func (s *Source) Token(ctx context.Context) (secrets.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	margin, cooldown := s.Margin, s.Cooldown
	if margin == 0 {
		margin = 2 * time.Minute
	}
	if cooldown == 0 {
		cooldown = 5 * time.Minute
	}
	if s.unstored != nil {
		if err := s.store(*s.unstored); err != nil {
			return "", err
		}
	}
	now := s.now()
	if !s.tok.AccessToken.IsZero() && (s.tok.Expiry.IsZero() || now.Add(margin).Before(s.tok.Expiry)) {
		return s.tok.AccessToken, nil
	}
	if s.lastErr != nil && now.Sub(s.failedAt) < cooldown {
		return "", s.lastErr
	}
	if s.Reload != nil {
		if rt, err := s.Reload(); err == nil && !rt.IsZero() && rt.Reveal() != s.stored.Reveal() {
			s.tok.RefreshToken, s.stored = rt, rt
		}
	}
	if s.tok.RefreshToken.IsZero() {
		s.report(false, ReasonNoRefresh)
		return "", fmt.Errorf("%w: no refresh token; authorize the account again", ErrRefresh)
	}
	nt, err := s.refresher.Refresh(ctx, s.tok.RefreshToken)
	if err == nil && nt.AccessToken.IsZero() {
		err = &RefreshError{Code: ReasonEmpty}
	}
	if err != nil {
		// Den Fehlertext des Anbieters nicht weiterreichen (kann Token-Teile
		// oder Client-Daten enthalten), nur die Tatsache und den Code.
		s.report(false, ReasonOf(err))
		s.failedAt, s.lastErr = now, fmt.Errorf("%w; retry after %s or authorize the account again", ErrRefresh, cooldown)
		return "", s.lastErr
	}
	if nt.RefreshToken.IsZero() {
		nt.RefreshToken = s.tok.RefreshToken
	}
	s.tok, s.lastErr = nt, nil
	s.unstored = &nt
	if err := s.store(nt); err != nil {
		s.report(false, ReasonNotStored)
		return "", err
	}
	s.report(true, ReasonOK)
	return nt.AccessToken, nil
}

// store ruft OnRefresh; scheitert es, bleibt das Token als ungespeichert
// markiert, und Token liefert keinen Zugang.
func (s *Source) store(t Token) error {
	if s.OnRefresh != nil {
		if err := s.OnRefresh(t); err != nil {
			return fmt.Errorf("%w (%s); stopping, fix the keyring and run the command again", ErrTokenNotStored, secrets.RedactTokens(err.Error()))
		}
		s.stored = t.RefreshToken
	}
	s.unstored = nil
	return nil
}

// RefreshError ist ein Fehler des Token-Endpunkts mit dem Code nach RFC 6749
// (invalid_grant, invalid_client, ...) oder einem eigenen festen Code. Der
// Text des Anbieters steht nie darin.
type RefreshError struct {
	Code string
	// AADSTS ist die Fehlernummer von Microsoft (z. B. AADSTS70000), falls
	// vorhanden; sie hilft bei der Fehlersuche und enthält keine Daten.
	AADSTS string
}

func (e *RefreshError) Error() string {
	if e.AADSTS != "" {
		return "oauth: token endpoint: " + e.Code + " (" + e.AADSTS + ")"
	}
	return "oauth: token endpoint: " + e.Code
}

// ReasonOf liefert den Code eines Fehlers für Audit und Ausgabe.
func ReasonOf(err error) string {
	var re *RefreshError
	switch {
	case err == nil:
		return ReasonOK
	case errors.As(err, &re):
		return re.Code
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	default:
		return "network"
	}
}
