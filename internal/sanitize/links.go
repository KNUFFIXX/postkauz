package sanitize

import (
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// Link ist ein sichtbar gemachter Link.
type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
	Host string `json:"host"` // ASCII (Punycode), klein
	// HostUnicode ist die Darstellung, die ein Mensch sähe, wenn sie sich
	// vom ASCII-Namen unterscheidet (internationalisierte Domain).
	HostUnicode string `json:"host_unicode,omitempty"`
	// Warnings: punycode, mixed_scripts, ip_address, userinfo,
	// text_domain_mismatch.
	Warnings []string `json:"warnings,omitempty"`
}

// Warnungen eines Links.
const (
	WarnPunycode     = "punycode"
	WarnMixedScripts = "mixed_scripts"
	WarnIPAddress    = "ip_address"
	WarnUserinfo     = "userinfo"
	WarnTextMismatch = "text_domain_mismatch"
)

var lookup = idna.Lookup

// CheckLink prüft ein Linkziel. Nur http und https gelten (ok); alles
// andere (javascript:, mailto:, data:, file:, relative Ziele) wird
// entfernt. text ist der sichtbare Text des Links (für den Abgleich).
func CheckLink(href, text string) (Link, bool) {
	href = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || invisible(r) || unicode.IsSpace(r) {
			return -1 // Browser ignorieren eingestreuten Leerraum im Schema
		}
		return r
	}, href)
	u, err := url.Parse(href)
	if err != nil {
		return Link{}, false
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Host == "" {
		return Link{}, false
	}
	u.Scheme = scheme
	l := Link{Text: Text(text)}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	ascii, err := lookup.ToASCII(host)
	if err != nil {
		ascii = host
	}
	l.Host = ascii
	if uni, err := lookup.ToUnicode(ascii); err == nil && uni != ascii {
		l.HostUnicode = uni
	} else if host != ascii {
		l.HostUnicode = host
	}
	if strings.Contains(ascii, "xn--") {
		l.Warnings = append(l.Warnings, WarnPunycode)
	}
	if mixedScripts(l.HostUnicode) {
		l.Warnings = append(l.Warnings, WarnMixedScripts)
	}
	if net.ParseIP(host) != nil {
		l.Warnings = append(l.Warnings, WarnIPAddress)
	}
	if u.User != nil {
		l.Warnings = append(l.Warnings, WarnUserinfo)
	}
	if d := textDomain(l.Text); d != "" && !sameSite(d, ascii) {
		l.Warnings = append(l.Warnings, WarnTextMismatch)
	}
	// Ausgabe mit dem ASCII-Namen: Ein Mensch sieht so, wohin es geht.
	if port := u.Port(); port != "" {
		u.Host = ascii + ":" + port
	} else {
		u.Host = ascii
	}
	l.URL = u.String()
	return l, true
}

// mixedScripts: Ein Namensteil mischt lateinische mit kyrillischen oder
// griechischen Buchstaben (Homoglyphen wie „exаmple" mit kyrillischem а).
func mixedScripts(host string) bool {
	if host == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		var latin, other bool
		for _, r := range label {
			switch {
			case unicode.Is(unicode.Latin, r):
				latin = true
			case unicode.Is(unicode.Cyrillic, r), unicode.Is(unicode.Greek, r), unicode.Is(unicode.Armenian, r):
				other = true
			}
		}
		if latin && other {
			return true
		}
	}
	return false
}

var domainInText = regexp.MustCompile(`(?i)(?:https?://)?((?:[\p{L}\p{N}-]+\.)+[\p{L}]{2,})(?:[/:?#]|$|\s)`)

// textDomain: Der sichtbare Text nennt eine Domain (z. B. „www.bank.example").
func textDomain(text string) string {
	m := domainInText.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return ""
	}
	d := strings.ToLower(m[1])
	if a, err := lookup.ToASCII(d); err == nil {
		d = a
	}
	return d
}

// sameSite vergleicht die letzten beiden Namensteile (Annäherung an die
// registrierbare Domain; bei co.uk u. Ä. die letzten drei).
func sameSite(a, b string) bool {
	return site(a) == site(b)
}

func site(h string) string {
	h = strings.TrimPrefix(strings.ToLower(h), "www.")
	parts := strings.Split(h, ".")
	n := 2
	if len(parts) >= 3 && len(parts[len(parts)-2]) <= 3 && len(parts[len(parts)-1]) == 2 {
		n = 3 // z. B. example.co.uk
	}
	if len(parts) <= n {
		return h
	}
	return strings.Join(parts[len(parts)-n:], ".")
}
