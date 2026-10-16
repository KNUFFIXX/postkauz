package search

import (
	"strings"

	"github.com/blevesearch/snowballstem"
	"github.com/blevesearch/snowballstem/german"
)

// Deutscher Stemmer: Snowball „german" aus github.com/blevesearch/snowballstem
// (vom Snowball-Compiler erzeugter Go-Code, BSD-3-Clause; Begründung in
// docs/datenmodell.md, „Stemmer"). Die Eingabe ist schon gefaltet (Fold):
// ß, Umlaute und ae/oe/ue sind dann bereits vereinheitlicht, der Stemmer
// entfernt nur noch Endungen (Rechnungen → rechnung, prüfen → pruf).

// maxTokenBytes: Längere „Wörter" (Base64-Reste, Hashes, URLs ohne
// Trennzeichen) kommen nicht in den Wortindex; die Teilwortsuche
// (Trigram) findet sie weiterhin.
const maxTokenBytes = 64

// Stemmer berechnet Wortstämme mit einem kleinen Zwischenspeicher (der
// Wortschatz einer Mailsammlung wiederholt sich stark). Nicht
// nebenläufig verwendbar; je Goroutine einen eigenen anlegen.
type Stemmer struct {
	env   *snowballstem.Env
	cache map[string]string
}

// stemCacheMax begrenzt den Zwischenspeicher; danach wird er geleert.
const stemCacheMax = 50000

// NewStemmer legt einen Stemmer an.
func NewStemmer() *Stemmer {
	return &Stemmer{env: snowballstem.NewEnv(""), cache: make(map[string]string, 1024)}
}

// Stem liefert den Wortstamm eines gefalteten Wortes.
func (s *Stemmer) Stem(tok string) string {
	if st, ok := s.cache[tok]; ok {
		return st
	}
	s.env.SetCurrent(tok)
	german.Stem(s.env)
	st := s.env.Current()
	if len(s.cache) >= stemCacheMax {
		clear(s.cache)
	}
	s.cache[tok] = st
	return st
}

// StemText faltet Text, zerlegt ihn in Wörter und liefert die Stämme,
// durch Leerzeichen getrennt (Inhalt einer Spalte des Wortindex).
func (s *Stemmer) StemText(text string) string {
	toks := Tokens(Fold(text))
	var b strings.Builder
	b.Grow(len(text))
	for _, t := range toks {
		if len(t) > maxTokenBytes {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s.Stem(t))
	}
	return b.String()
}
