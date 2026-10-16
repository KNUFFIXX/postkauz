package search

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Spalten beider FTS5-Tabellen (fts_messages: Wortstämme, fts_trigram:
// gefalteter Text), in dieser Reihenfolge (Migration 0004).
const (
	ColSubject = iota
	ColSender
	ColRecipients
	ColBody
	ColAttachments
	NumCols
)

// ColNames sind die Spaltennamen in der Datenbank.
var ColNames = [NumCols]string{"subject", "sender", "recipients", "body", "attachments"}

// Grenzen je Nachricht für den Index (Bytes vor dem Falten). Längere
// Texte werden am Ende abgeschnitten; die Nachricht bleibt vollständig
// gespeichert, nur der Index endet dort.
const (
	MaxBodyBytes        = 256 << 10
	MaxAttachmentsBytes = 256 << 10
)

// Source ist der Text einer Nachricht, wie er gespeichert ist.
type Source struct {
	Subject string
	From    string          // "Name <adresse>"
	To, Cc  json.RawMessage // JSON-Arrays von {name,address}
	Body    string          // bereinigter Text (Klartext, sonst aus HTML)
	// Attachments: Dateinamen und extrahierter Text der Anhänge.
	Attachments []string
}

// Doc sind die Spalteninhalte einer Nachricht für beide Tabellen.
type Doc struct {
	Word [NumCols]string // Wortstämme (fts_messages)
	Tri  [NumCols]string // gefalteter Text (fts_trigram)
}

// Build berechnet die Index-Spalten einer Nachricht.
func (s *Stemmer) Build(src Source) Doc {
	cols := [NumCols]string{
		ColSubject:     src.Subject,
		ColSender:      src.From,
		ColRecipients:  recipients(src.To, src.Cc),
		ColBody:        truncate(src.Body, MaxBodyBytes),
		ColAttachments: truncate(strings.Join(src.Attachments, "\n"), MaxAttachmentsBytes),
	}
	var d Doc
	for i, c := range cols {
		d.Word[i] = s.StemText(c)
		d.Tri[i] = FoldText(c)
	}
	return d
}

// recipients macht aus den JSON-Adresslisten „Name adresse"-Zeilen.
func recipients(lists ...json.RawMessage) string {
	var b strings.Builder
	for _, raw := range lists {
		var as []struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		}
		if len(raw) == 0 || json.Unmarshal(raw, &as) != nil {
			continue
		}
		for _, a := range as {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			if a.Name != "" {
				b.WriteString(a.Name)
				b.WriteByte(' ')
			}
			b.WriteString(a.Address)
		}
	}
	return b.String()
}

// truncate kürzt auf höchstens n Bytes an einer Zeichengrenze.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
