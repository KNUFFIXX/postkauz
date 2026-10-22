package drafts

import (
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Regeln der Versanderkennung (S2.6, A02 E5; docs/versanderkennung.md).
// Hier stehen nur reine Funktionen ohne Datenbank; den Ablauf beim Sync
// macht Service.CheckAfterSync (detect.go).
//
// Zuerst gilt die Message-ID: Liegt in Gesendet (Rolle sent) oder „Alle
// Nachrichten" (Rolle all) eine Nachricht mit der Message-ID des Entwurfs,
// ist er gesendet (Grund message_id). Sonst der Rückfall-Abgleich (Grund
// fallback), alle Bedingungen zugleich:
//
//   - gleiche Empfängermenge (To und Cc, nur die Adressen, klein),
//   - gleicher Betreff nach Entfernen der Präfixe Re:, AW:, Antw:, Fwd:,
//     WG:, FW: (auch mehrfach, Groß- und Kleinschreibung egal) und
//     Zusammenfassen von Leerraum,
//   - Versandzeit zwischen content_changed_at − 1 h und
//     content_changed_at + 14 Tage (beide Grenzen eingeschlossen;
//     content_changed_at ist die letzte Änderung des Inhalts, nicht
//     updated_at: Ablauf und Lesen verschieben das Fenster nicht,
//     Gegenprüfung S2.6–S2.8, W1),
//   - Jaccard-Ähnlichkeit der Wortmengen ≥ 0,9: eigener Entwurfstext (ohne
//     Zitat, Signatur, Weiterleitung) gegen den zitatfreien Text der
//     gesendeten Nachricht (ohne weitergeleiteten Teil); die Wörter der
//     Signatur der Identität fallen auf beiden Seiten weg.

// Gründe einer erkannten Sendung (Audit drafts.sent_detected).
const (
	DetectMessageID = "message_id"
	DetectFallback  = "fallback"
)

// Grenzen des Rückfall-Abgleichs (E5).
const (
	FallbackBefore     = time.Hour
	FallbackAfter      = 14 * 24 * time.Hour
	FallbackMinJaccard = 0.9
)

// subjectPrefixRe erkennt ein Antwort- oder Weiterleitungspräfix am Anfang.
var subjectPrefixRe = regexp.MustCompile(`(?i)^\s*(?:re|aw|antw|fwd|wg|fw)\s*:`)

// NormalizeSubject entfernt unsichtbare Zeichen, alle führenden Präfixe
// (Re:, AW:, Antw:, Fwd:, WG:, FW:) und fasst Leerraum zusammen.
func NormalizeSubject(s string) string {
	s = sanitize.Text(s)
	for {
		loc := subjectPrefixRe.FindStringIndex(s)
		if loc == nil {
			break
		}
		s = s[loc[1]:]
	}
	return strings.Join(strings.Fields(s), " ")
}

// RecipientKey ist die normalisierte Empfängermenge: Adressen klein, ohne
// Namen, ohne Dubletten, sortiert, mit Zeilenumbruch verbunden. Leere
// Adressen fallen weg.
func RecipientKey(lists ...[]store.Addr) string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, a := range l {
			addr := normAddr(a.Address)
			if addr == "" || seen[addr] {
				continue
			}
			seen[addr] = true
			out = append(out, addr)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func normAddr(s string) string {
	return strings.ToLower(strings.TrimSpace(sanitize.Text(s)))
}

// fromAddress liefert die Adresse aus messages.from_addr („Name <adresse>"
// oder nur die Adresse), klein.
func fromAddress(s string) string {
	if a, err := mail.ParseAddress(s); err == nil {
		return normAddr(a.Address)
	}
	if i, j := strings.LastIndex(s, "<"), strings.LastIndex(s, ">"); i >= 0 && j > i {
		return normAddr(s[i+1 : j])
	}
	return normAddr(s)
}

// Words liefert die Wortmenge eines Texts: Folgen aus Buchstaben und
// Ziffern, klein geschrieben.
func Words(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		out[w] = struct{}{}
	}
	return out
}

// Jaccard ist |A ∩ B| / |A ∪ B|. Zwei leere Mengen ergeben 0: Ohne Text
// gibt es nichts zu vergleichen, und der Rückfall soll im Zweifel nichts
// schließen.
func Jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if _, ok := b[w]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// without liefert a ohne die Wörter aus drop.
func without(a, drop map[string]struct{}) map[string]struct{} {
	if len(drop) == 0 {
		return a
	}
	out := make(map[string]struct{}, len(a))
	for w := range a {
		if _, ok := drop[w]; !ok {
			out[w] = struct{}{}
		}
	}
	return out
}

// InFallbackWindow meldet, ob sent zwischen changed − 1 h und changed + 14
// Tage liegt (beide Grenzen eingeschlossen); changed ist die letzte
// Inhaltsänderung des Entwurfs (content_changed_at).
func InFallbackWindow(changed, sent time.Time) bool {
	return !sent.Before(changed.Add(-FallbackBefore)) && !sent.After(changed.Add(FallbackAfter))
}

// forwardRe erkennt die Trennzeile vor einer weitergeleiteten Nachricht
// (PostKauz, Gmail, Thunderbird, deutsche Clients).
var forwardRe = regexp.MustCompile(`(?i)^\s*-{2,}\s*(?:forwarded message|weitergeleitete nachricht|message transféré)\s*-{2,}\s*$`)

// SentOwnText liefert aus dem zitatfreien Text einer gesendeten Nachricht
// (bodies.text_unquoted) den Teil vor einer Weiterleitung: Der
// weitergeleitete Inhalt ist kein eigener Text.
func SentOwnText(unquoted string) string {
	lines := strings.Split(strings.ReplaceAll(unquoted, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if forwardRe.MatchString(l) {
			return strings.Join(lines[:i], "\n")
		}
	}
	return unquoted
}

// DraftOwnText liefert den eigenen Text eines Entwurfs ohne Zitat: own ist
// compose.OwnText (ohne Signatur und Weiterleitung); Zeilen mit „>", die
// ein Agent selbst geschrieben hat, fallen weg wie beim gesendeten Text.
func DraftOwnText(own string) string {
	return SentOwnText(sanitize.StripQuotes(own))
}

// FallbackDraft ist die vorbereitete Seite eines Entwurfs im
// Rückfall-Abgleich.
type FallbackDraft struct {
	ID int64
	// ChangedAt: letzte Inhaltsänderung (content_changed_at); daran hängt
	// das Zeitfenster.
	ChangedAt  time.Time
	Subject    string // NormalizeSubject
	Recipients string // RecipientKey(To, Cc)
	// Words: Wortmenge des eigenen Texts (DraftOwnText); Signature: die der
	// Signatur der Identität (fallen auf beiden Seiten weg).
	Words     map[string]struct{}
	Signature map[string]struct{}
}

// NewFallbackDraft bereitet einen Entwurf vor: own ist sein eigener Text
// (compose.OwnText), signature der Klartext der Signatur seiner Identität
// ("" = keine).
func NewFallbackDraft(d store.Draft, own, signature string) FallbackDraft {
	sig := Words(signature)
	changed := d.ContentChangedAt
	if changed.IsZero() {
		changed = d.UpdatedAt
	}
	return FallbackDraft{ID: d.ID, ChangedAt: changed, Subject: NormalizeSubject(d.Subject),
		Recipients: RecipientKey(d.To, d.Cc), Words: without(Words(DraftOwnText(own)), sig), Signature: sig}
}

// HeadersMatch prüft die Bedingungen ohne Text: Empfänger, Betreff,
// Zeitfenster.
func (f FallbackDraft) HeadersMatch(m store.SentMessage) bool {
	return f.Recipients != "" && RecipientKey(m.To, m.Cc) == f.Recipients && NormalizeSubject(m.Subject) == f.Subject &&
		InFallbackWindow(f.ChangedAt, m.Date)
}

// Score ist die Jaccard-Ähnlichkeit des eigenen Texts mit sentWords (der
// Wortmenge von SentOwnText der gesendeten Nachricht), ohne die Wörter der
// Signatur.
func (f FallbackDraft) Score(sentWords map[string]struct{}) float64 {
	return Jaccard(f.Words, without(sentWords, f.Signature))
}

// Match meldet, ob die gesendete Nachricht m mit dem Text sentText
// (zitatfrei, bodies.text_unquoted) im Rückfall-Abgleich zum Entwurf passt,
// und die Ähnlichkeit.
func (f FallbackDraft) Match(m store.SentMessage, sentText string) (float64, bool) {
	if !f.HeadersMatch(m) {
		return 0, false
	}
	s := f.Score(Words(SentOwnText(sentText)))
	return s, s >= FallbackMinJaccard
}
