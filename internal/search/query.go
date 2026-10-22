package search

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Abfragesprache (docs/cli.md, Abschnitt „Suche"): Freitext, Phrasen in
// Anführungszeichen, „-" schließt aus, dazu Operatoren nach Gmail-Vorbild.
// Alle Teile werden mit UND verknüpft. Nutzereingaben gelangen nie
// unmaskiert in einen FTS5-Ausdruck: Jedes Wort und jede Phrase steht als
// FTS5-Zeichenkette in Anführungszeichen (innere Anführungszeichen
// verdoppelt), Operatoren werden zu SQL-Filtern mit Parametern.

// Grenzen einer Anfrage.
const (
	MaxQueryBytes = 1024
	MaxTerms      = 16
)

// QueryError ist ein Fehler in der Anfrage (Eingabefehler des Nutzers).
type QueryError struct{ Msg string }

func (e *QueryError) Error() string { return e.Msg }

func qerr(format string, args ...any) error { return &QueryError{Msg: fmt.Sprintf(format, args...)} }

// Term ist ein Textbestandteil der Anfrage mit seiner Übersetzung.
type Term struct {
	Text    string `json:"text"`            // wie eingegeben (ohne Anführungszeichen)
	Field   string `json:"field,omitempty"` // from | to | subject; leer = alle Textspalten
	Phrase  bool   `json:"phrase,omitempty"`
	Negated bool   `json:"negated,omitempty"`
	// Mode: stem (Wortstamm), prefix (kurzes Wort, nur Wortindex mit
	// Präfix), phrase (mehrere Wörter) oder substring (nur Teilwort).
	Mode string `json:"mode"`
	// Word ist der FTS5-Ausdruck für fts_messages (Wortstämme), Tri der für
	// fts_trigram (Teilwort ab 3 Zeichen); leer = diese Tabelle nicht.
	Word string `json:"word_match,omitempty"`
	Tri  string `json:"substring_match,omitempty"`
}

// Filter ist ein Operator, der als SQL-Bedingung wirkt.
type Filter struct {
	// Kind: has_attachment | is | mailbox | thread | tag | after | before.
	Kind    string `json:"kind"`
	Value   string `json:"value,omitempty"`
	Negated bool   `json:"negated,omitempty"`
	// Time (after/before, Mitternacht in der Zeitzone der Anfrage) und ID
	// (thread) sind die geprüften Werte.
	Time time.Time `json:"-"`
	ID   int64     `json:"-"`
}

// Query ist eine geparste und übersetzte Anfrage.
type Query struct {
	Terms   []Term   `json:"terms"`
	Filters []Filter `json:"filters"`
	// Ignored nennt Teile ohne Wirkung (z. B. nur Satzzeichen).
	Ignored []string `json:"ignored,omitempty"`
}

// Positive liefert die Textterme, die erfüllt sein müssen.
func (q Query) Positive() []Term {
	var out []Term
	for _, t := range q.Terms {
		if !t.Negated {
			out = append(out, t)
		}
	}
	return out
}

// Negative liefert die ausschließenden Textterme.
func (q Query) Negative() []Term {
	var out []Term
	for _, t := range q.Terms {
		if t.Negated {
			out = append(out, t)
		}
	}
	return out
}

// Order nennt die Sortierung: relevance (BM25 und Datum) mit Texttermen,
// sonst date (neueste zuerst).
func (q Query) Order() string {
	if len(q.Positive()) > 0 {
		return "relevance"
	}
	return "date"
}

var operators = map[string]bool{"from": true, "to": true, "subject": true, "has": true, "is": true, "in": true,
	"mailbox": true, "thread": true, "tag": true, "after": true, "before": true}

// Parse liest eine Anfrage und übersetzt sie (Wortstämme, Teilwörter,
// Filter). Datumsangaben (after:/before:) gelten in loc (nil = time.Local).
func Parse(input string, loc *time.Location) (Query, error) {
	if loc == nil {
		loc = time.Local
	}
	q := Query{Terms: []Term{}, Filters: []Filter{}}
	if len(input) > MaxQueryBytes {
		return q, qerr("query is longer than %d bytes", MaxQueryBytes)
	}
	input = strings.ToValidUTF8(input, " ")
	rs := []rune(input)
	n := len(rs)
	var terms []Term
	for i := 0; ; {
		for i < n && unicode.IsSpace(rs[i]) {
			i++
		}
		if i >= n {
			break
		}
		neg := false
		if rs[i] == '-' && i+1 < n && !unicode.IsSpace(rs[i+1]) {
			neg, i = true, i+1
		}
		if rs[i] == '"' {
			var text string
			text, i = readQuoted(rs, i)
			terms = append(terms, Term{Text: text, Phrase: true, Negated: neg})
			continue
		}
		// Operator: name:wert oder name:"wert mit Leerzeichen"
		j := i
		for j < n && rs[j] < utf8.RuneSelf && (unicode.IsLetter(rs[j]) || rs[j] == '_') {
			j++
		}
		if j > i && j < n && rs[j] == ':' {
			key := strings.ToLower(string(rs[i:j]))
			if operators[key] {
				i = j + 1
				var val string
				quoted := false
				if i < n && rs[i] == '"' {
					val, i = readQuoted(rs, i)
					quoted = true
				} else {
					s := i
					for i < n && !unicode.IsSpace(rs[i]) {
						i++
					}
					val = string(rs[s:i])
				}
				t, f, err := operator(key, val, quoted, neg, loc)
				if err != nil {
					return q, err
				}
				if t != nil {
					terms = append(terms, *t)
				}
				if f != nil {
					q.Filters = append(q.Filters, *f)
				}
				continue
			}
		}
		s := i
		for i < n && !unicode.IsSpace(rs[i]) {
			i++
		}
		terms = append(terms, Term{Text: string(rs[s:i]), Negated: neg})
	}
	st := NewStemmer()
	for _, t := range terms {
		if !st.compile(&t) {
			q.Ignored = append(q.Ignored, t.Text)
			continue
		}
		q.Terms = append(q.Terms, t)
	}
	if len(q.Terms) > MaxTerms {
		return q, qerr("query has %d search terms; at most %d are allowed", len(q.Terms), MaxTerms)
	}
	if len(q.Terms) == 0 && len(q.Filters) == 0 {
		return q, qerr("empty query: give words to search for or an operator such as has:attachment")
	}
	return q, nil
}

// readQuoted liest ab rs[i] == '"' bis zum schließenden Anführungszeichen
// (fehlt es, bis zum Ende) und liefert den Inhalt und die Position danach.
func readQuoted(rs []rune, i int) (string, int) {
	i++
	s := i
	for i < len(rs) && rs[i] != '"' {
		i++
	}
	text := string(rs[s:i])
	if i < len(rs) {
		i++
	}
	return text, i
}

var dateLayouts = []string{"2006-01-02", "2006/01/02", "02.01.2006", "2.1.2006"}

func operator(key, val string, quoted, neg bool, loc *time.Location) (*Term, *Filter, error) {
	v := strings.TrimSpace(val)
	if v == "" {
		return nil, nil, qerr("%s: needs a value", key)
	}
	lv := strings.ToLower(v)
	switch key {
	case "from", "to", "subject":
		return &Term{Text: v, Field: key, Phrase: quoted, Negated: neg}, nil, nil
	case "has":
		if lv == "attachment" || lv == "attachments" {
			return nil, &Filter{Kind: "has_attachment", Negated: neg}, nil
		}
		return nil, nil, qerr("has: only has:attachment is supported")
	case "is":
		canon := map[string]string{"unread": "unread", "read": "read", "seen": "read", "flagged": "flagged",
			"starred": "flagged", "answered": "answered", "draft": "draft"}[lv]
		if canon == "" {
			return nil, nil, qerr("is: not a supported value; use is:unread, is:read, is:flagged, is:answered or is:draft")
		}
		return nil, &Filter{Kind: "is", Value: canon, Negated: neg}, nil
	case "in", "mailbox":
		return nil, &Filter{Kind: "mailbox", Value: v, Negated: neg}, nil
	case "tag":
		return nil, &Filter{Kind: "tag", Value: v, Negated: neg}, nil
	case "thread":
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return nil, nil, qerr("thread: give a thread id (a positive number)")
		}
		return nil, &Filter{Kind: "thread", Value: v, ID: id, Negated: neg}, nil
	case "after", "before":
		for _, l := range dateLayouts {
			if t, err := time.ParseInLocation(l, v, loc); err == nil {
				return nil, &Filter{Kind: key, Value: t.Format("2006-01-02"), Time: t, Negated: neg}, nil
			}
		}
		return nil, nil, qerr("%s: give a date as YYYY-MM-DD", key)
	}
	return nil, nil, qerr("unknown operator %s:", key)
}

// compile übersetzt einen Textterm (siehe Term.Mode). false: Der Term hat
// keine Wirkung (nur Satzzeichen, kürzer als 3 Zeichen ohne Wort).
func (s *Stemmer) compile(t *Term) bool {
	folded := FoldText(t.Text)
	var toks []string
	for _, tok := range Tokens(folded) {
		if len(tok) <= maxTokenBytes { // wie im Index (StemText)
			toks = append(toks, tok)
		}
	}
	switch {
	case len(toks) == 0:
	case len(toks) == 1 && !t.Phrase && runeLen(toks[0]) < 3:
		t.Mode, t.Word = "prefix", ftsString(toks[0])+"*"
	case len(toks) == 1:
		t.Mode, t.Word = "stem", ftsString(s.Stem(toks[0]))
	default:
		stems := make([]string, len(toks))
		for i, tok := range toks {
			stems[i] = s.Stem(tok)
		}
		t.Mode, t.Word = "phrase", ftsString(strings.Join(stems, " "))
	}
	// Teilwörter (Komposita) über den Trigram-Index, erst ab 3 Zeichen;
	// kürzere Anfragen nur über den Wortindex mit Präfix.
	if tri := FoldTrigramText(t.Text); trigramLen(t.Text) >= 3 && runeLen(tri) >= 3 {
		t.Tri = ftsString(tri)
	}
	if t.Word == "" && t.Tri == "" {
		return false
	}
	if t.Word == "" {
		t.Mode = "substring"
	}
	if col := fieldColumn(t.Field); col != "" {
		if t.Word != "" {
			t.Word = "{" + col + "} : " + t.Word
		}
		if t.Tri != "" {
			t.Tri = "{" + col + "} : " + t.Tri
		}
	}
	return true
}

func fieldColumn(field string) string {
	switch field {
	case "from":
		return ColNames[ColSender]
	case "to":
		return ColNames[ColRecipients]
	case "subject":
		return ColNames[ColSubject]
	}
	return ""
}

// ftsString setzt Text als FTS5-Zeichenkette (Phrase) in Anführungszeichen;
// innere Anführungszeichen werden verdoppelt. So bleibt jede Eingabe ein
// Suchbegriff und wird nie zu FTS5-Syntax (AND, NOT, NEAR, Spalten, *).
func ftsString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// Fingerprint kennzeichnet die übersetzte Anfrage (für Cursor).
func (q Query) Fingerprint(scope string) string {
	type fp struct {
		T []Term   `json:"t"`
		F []Filter `json:"f"`
		S string   `json:"s"`
		D []int64  `json:"d"`
	}
	var dates []int64
	for _, f := range q.Filters {
		dates = append(dates, f.Time.UnixMilli(), f.ID)
	}
	b, _ := json.Marshal(fp{q.Terms, q.Filters, scope, dates})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// Cursor ist die Position nach dem letzten gelieferten Treffer
// (Keyset-Paginierung über Sortierwert und Nachrichten-ID). Now hält den
// Zeitpunkt der ersten Seite fest, damit der Datumsanteil der Relevanz
// auf allen Seiten gleich gerechnet wird.
type Cursor struct {
	Fingerprint string  `json:"q"`
	Now         int64   `json:"n"`
	Score       float64 `json:"s"`
	ID          int64   `json:"i"`
}

// Encode liefert den Cursor als undurchsichtige Zeichenkette.
func (c Cursor) Encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor liest einen Cursor; fp muss zur Anfrage passen.
func DecodeCursor(s, fp string) (Cursor, error) {
	var c Cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID <= 0 {
		return c, qerr("invalid cursor")
	}
	if c.Fingerprint != fp {
		return c, qerr("the cursor belongs to a different query")
	}
	return c, nil
}
