package drafts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/user"
	"strings"

	"golang.org/x/net/idna"
)

// NewMessageID erzeugt die Message-ID eines Entwurfs (A02 E11), ohne
// spitze Klammern: "pk.<20 Hex>@<Domain der Absenderadresse>". Die 20 Hex
// sind 10 Byte aus r (nil = crypto/rand; Tests setzen einen festen
// Zufall). Die Domain wird klein und, falls nötig, als Punycode (IDNA)
// geschrieben. Vergeben wird sie beim Anlegen; sie ändert sich nie.
func NewMessageID(r io.Reader, fromEmail string) (string, error) {
	at := strings.LastIndexByte(fromEmail, '@')
	if at < 0 || at == len(fromEmail)-1 {
		return "", errors.New("drafts: sender address has no domain")
	}
	domain, err := idna.Lookup.ToASCII(strings.ToLower(strings.TrimSpace(fromEmail[at+1:])))
	if err != nil || domain == "" || strings.ContainsAny(domain, "<>@ \t\r\n") {
		return "", fmt.Errorf("drafts: sender domain is not usable in a Message-ID")
	}
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, 10)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("drafts: random: %w", err)
	}
	return "pk." + hex.EncodeToString(b) + "@" + domain, nil
}

// DecidedBy liefert, wer im MVP freigibt (Entschiedene Frage 20): den Namen
// aus [operator] name der Konfiguration, sonst den Benutzernamen des
// Betriebssystems, sonst "human".
func DecidedBy(configured string) string {
	if s := strings.TrimSpace(configured); s != "" {
		return s
	}
	if u, err := currentUser(); err == nil && strings.TrimSpace(u) != "" {
		return strings.TrimSpace(u)
	}
	return "human"
}

// currentUser ist in Tests ersetzbar.
var currentUser = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func isJSONObject(s string) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal([]byte(s), &m) == nil && m != nil
}
