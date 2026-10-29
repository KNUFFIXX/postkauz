package httpmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// maxJWKSBytes: größte angenommene JWKS-Antwort.
const maxJWKSBytes = 1 << 20

// errJWKSFetch: Die JWKS war nicht abrufbar (Verbindung, Frist, Status
// ungleich 200, Umleitung, kein JSON). Die Anmeldung antwortet dann 503,
// nicht 401: Am Token liegt es nicht.
var errJWKSFetch = errors.New("httpmcp: fetching the JWKS failed")

// jwksTransport ist der RoundTripper des JWKS-Clients. Er macht aus jedem
// Fehlschlag einen Fehler mit errJWKSFetch und liest die Antwort selbst
// (höchstens maxJWKSBytes), damit auch ein Lesefehler so ankommt: go-oidc
// reicht Fehler des HTTP-Clients mit %w weiter, Lesefehler aber nicht.
type jwksTransport struct {
	base http.RoundTripper
}

func (t *jwksTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errJWKSFetch, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errJWKSFetch, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errJWKSFetch, err)
	}
	if len(b) > maxJWKSBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", errJWKSFetch, maxJWKSBytes)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("%w: not JSON", errJWKSFetch)
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	res.ContentLength = int64(len(b))
	return res, nil
}

// newJWKSClient: Client für die JWKS auf dem Transport des Clients für den
// Autorisierungsserver, mit Frist asTimeout und ohne Umleitungen (eine
// Umleitung ist für jwksTransport ein Status ungleich 200).
func newJWKSClient(c *http.Client) *http.Client {
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Transport: &jwksTransport{base: base}, Timeout: asTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errASRedirect }}
}

// verifyState trägt im Kontext einer Prüfung, ob die JWKS nicht abrufbar
// war (go-oidc gibt den Fehler der Signaturprüfung nur als Text weiter).
type verifyState struct {
	unavailable bool
}

type verifyStateKey struct{}

// keySet hüllt den Schlüsselsatz von go-oidc ein und merkt sich, ob eine
// Prüfung an der JWKS (nicht am Token) scheiterte.
type keySet struct {
	inner oidc.KeySet
}

func (k *keySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	b, err := k.inner.VerifySignature(ctx, jwt)
	if err != nil && fetchFailed(err) {
		if st, ok := ctx.Value(verifyStateKey{}).(*verifyState); ok {
			st.unavailable = true
		}
	}
	return b, err
}

// fetchFailed: Der Fehler kommt vom Abruf der JWKS oder von einer Frist
// (Client-Frist oder Frist der Anmeldung), nicht von der Signatur.
func fetchFailed(err error) bool {
	return errors.Is(err, errJWKSFetch) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// checkJWKSURI: jwks_uri muss https sein; http nur für Loopback
// (localhost, 127.0.0.0/8, ::1), etwa in Tests.
func checkJWKSURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("httpmcp: jwks_uri %q is not a URL", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		h := u.Hostname()
		if strings.EqualFold(h, "localhost") {
			return nil
		}
		if ip, err := netip.ParseAddr(h); err == nil && ip.Unmap().IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("httpmcp: jwks_uri %q must use https", u.Redacted())
}
