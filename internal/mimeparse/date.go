package mimeparse

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// DateSource sagt, woher Message.Date stammt.
type DateSource string

const (
	DateFromHeader   DateSource = "header"   // Date-Kopfzeile (ggf. tolerant gelesen)
	DateFromInternal DateSource = "internal" // fehlend/unlesbar: INTERNALDATE des Servers
	DateNone         DateSource = "none"     // weder noch (z. B. Import ohne Datum)
)

var errNoDate = errors.New("mimeparse: no date")

// zoneWithoutSign: Uhrzeit, dann vier Ziffern ohne Vorzeichen am Ende,
// optional mit Kommentar, z. B. „10:00:00 0100" oder „10:00 0200 (CET)"
// (Spike-Befund: go-message und enmime scheitern daran).
var zoneWithoutSign = regexp.MustCompile(`(\d{1,2}:\d{2}(?::\d{2})?)\s+(\d{4})(\s*(?:\([^()]*\))?\s*)$`)

var trailingComment = regexp.MustCompile(`\([^()]*\)\s*$`)

// Rückfall-Layouts, wenn net/mail scheitert. Ohne Zone gilt UTC.
var fallbackLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05",
	"Mon, 2 Jan 2006 15:04:05",
	"2 Jan 2006 15:04:05",
	"Mon, 2 Jan 2006 15:04",
	"Mon Jan 2 15:04:05 2006", // asctime
	"Mon Jan 2 15:04:05 MST 2006",
	"Mon, 2 Jan 06 15:04:05 -0700",
}

// ParseDate liest eine Date-Kopfzeile tolerant: zuerst streng nach RFC 5322
// (net/mail), dann mit ergänztem Vorzeichen bei Zonen wie „0100", zuletzt
// über einige verbreitete Abweichungen. Das Ergebnis ist in UTC.
func ParseDate(v string) (time.Time, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\x00", ""))
	if v == "" {
		return time.Time{}, errNoDate
	}
	if t, err := mail.ParseDate(v); err == nil {
		return t.UTC(), nil
	}
	if m := zoneWithoutSign.FindStringSubmatchIndex(v); m != nil {
		fixed := v[:m[4]] + "+" + v[m[4]:]
		if t, err := mail.ParseDate(fixed); err == nil {
			return t.UTC(), nil
		}
	}
	// Kommentar am Ende („(CET)") und doppelte Leerzeichen entfernen.
	clean := strings.Join(strings.Fields(trailingComment.ReplaceAllString(v, "")), " ")
	for _, layout := range fallbackLayouts {
		if t, err := time.Parse(layout, clean); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("mimeparse: unparsable date")
}

// resolveDate wählt das Datum der Nachricht: Kopfzeile, sonst INTERNALDATE.
func resolveDate(header string, internal time.Time) (time.Time, DateSource, error) {
	t, err := ParseDate(header)
	if err == nil {
		return t, DateFromHeader, nil
	}
	if !internal.IsZero() {
		return internal.UTC(), DateFromInternal, err
	}
	return time.Time{}, DateNone, err
}
