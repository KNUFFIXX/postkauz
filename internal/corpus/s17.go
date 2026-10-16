package corpus

import (
	"bytes"
	"fmt"
	"time"
)

// Sonderfälle für S1.7 (Volltextsuche). Sie stehen vor den Sonderfällen
// von S1.6 und ersetzen dort Einzelnachrichten; alle Nachrichten davor
// behalten ihre Bytes. Jede trägt einen festen Schlüssel; die Suchfälle im
// Manifest (Feld search) nennen erwartete Treffer und Nichttreffer über die
// Dateinamen dieser Nachrichten. Die übrigen Korpus-Nachrichten enthalten
// dieselben Wörter teils auch („Rechnung", „Erdgeschoß"); geprüft wird
// deshalb „enthält" und „enthält nicht", nicht die genaue Treffermenge.
// Ausnahme: Anfragen mit same_as müssen genau dieselben Treffer liefern.

// SearchCase ist ein Fall des Suchtestsatzes.
type SearchCase struct {
	Query string   `json:"query"`
	Note  string   `json:"note"`          // was der Fall prüft
	Want  []string `json:"want"`          // Dateien, die Treffer sein müssen
	Not   []string `json:"not,omitempty"` // Dateien, die keine Treffer sein dürfen
	// SameAs: Diese Anfrage muss genau dieselben Treffer liefern.
	SameAs string `json:"same_as,omitempty"`
	// AllHaveAttachments: Jeder Treffer muss einen Anhang haben.
	AllHaveAttachments bool `json:"all_have_attachments,omitempty"`
}

type s17Spec struct {
	key     string
	subject string
	from    int // Index in people
	body    string
	date    time.Time
	attName string // Textanhang (leer = keiner)
	attText string
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 9, 30, 0, 0, time.FixedZone("CET", 3600))
}

// s17Specs: erfundene Inhalte; Absender aus people (Adressen unter
// example.org/example.net).
var s17Specs = []s17Spec{
	{key: "rechnungen", subject: "Offene Rechnungen Hausmeisterdienst", from: 1, body: "Anbei die offenen Posten aus dem dritten Quartal.", date: day(2026, 2, 10)},
	{key: "rechnung", subject: "Rechnung Gartenpflege Juni", from: 6, body: "Die Gartenpflege im Juni kostet 480 Euro.", date: day(2026, 6, 15)},
	{key: "gartenpflege_alt", subject: "Gartenpflege Herbst", from: 6, body: "Laub und Heckenschnitt im Oktober.", date: day(2025, 10, 10)},
	{key: "pruefen_umlaut", subject: "Unterlagen Wohnungsübergabe", from: 2, body: "Bitte die Unterlagen bis Freitag prüfen.", date: day(2026, 3, 3)},
	{key: "pruefen_ue", subject: "Unterlagen Schlüsselbund", from: 3, body: "Bitte die Unterlagen bis Montag pruefen.", date: day(2026, 3, 4)},
	{key: "vollstreckung", subject: "Zwangsvollstreckung aus dem Titel vom 3. März", from: 7, body: "Der Gerichtsvollzieher kündigt sich an.", date: day(2026, 4, 1)},
	{key: "eg_sz", subject: "Zählertausch im Haus", from: 8, body: "Der Zähler im Erdgeschoß links wurde getauscht.", date: day(2026, 5, 5)},
	{key: "eg_ss", subject: "Wasserhahn tropft", from: 9, body: "Links oben, im Erdgeschoss rechts tropft der Wasserhahn.", date: day(2026, 5, 6)},
	{key: "grundsteuer", subject: "Grundsteuerbescheid 2026", from: 6, body: "Der Steuerbescheid und die Kfz-Steuer liegen bei.", date: day(2026, 1, 20)},
	{key: "umsatzsteuer", subject: "Umsatzsteuer-Voranmeldung", from: 4, body: "Bitte die Voranmeldung abzeichnen.", date: day(2025, 11, 11)},
	{key: "abrechnung", subject: "Abrechnung Wasser", from: 5, body: "Die Wasserabrechnung liegt vor.", date: day(2026, 7, 7)},
	{key: "nebenkosten", subject: "Betriebskosten fertig", from: 5, body: "Die Nebenkostenabrechnung 2025 ist fertig.", date: day(2026, 7, 8)},
	{key: "anhang", subject: "Ablesebeleg Februar", from: 0, body: "Den Beleg finden Sie im Anhang.", date: day(2026, 3, 1),
		attName: "Ablesebeleg.txt", attText: "Wärmemengenzählerablesung vom 28.02.2026: 1.234 kWh\r\n"},
	{key: "ohne_anhang", subject: "Ablesebeleg folgt", from: 0, body: "Der Beleg folgt nächste Woche.", date: day(2026, 3, 2)},
}

// s17 schreibt den n-ten Sonderfall.
func (g *gen) s17(n int) ([]byte, Entry) {
	sp := s17Specs[n]
	from := people[sp.from]
	e := Entry{
		MessageID: fmt.Sprintf("pk.s17.%02d.%08x@corpus.example.org", n, g.r.Uint32()),
		Subject:   sp.subject, FromName: from.name, FromAddr: from.addr, ToAddr: me.addr, Charset: "utf-8", HeaderEncoding: "Q",
		Date: sp.date.UTC().Format(time.RFC3339), DateSource: "header", BodyContains: sp.body,
		InternalDate: sp.date.Add(5 * time.Minute).UTC().Format(time.RFC3339), Attachments: []string{},
	}
	if isASCII(e.Subject) {
		e.HeaderEncoding = "ascii"
	}
	body := textEntity(sp.body+"\n", "plain", "utf-8", "quoted-printable")
	if sp.attName != "" {
		body = multipartEntity("mixed", g.boundary(), [][]byte{body,
			attachmentEntity(sp.attName, "text/plain", nameASCII, "attachment", []byte(sp.attText), "")}, "")
		e.Attachments = []string{sp.attName}
		e.AttachmentTypes = []ExpectAttachment{{Name: sp.attName, Declared: "text/plain", Detected: "text/plain",
			TextContains: "Wärmemengenzählerablesung"}}
	}
	hs := []header{
		{"From", formatAddr(from.name, from.addr, "utf-8", "Q")},
		{"To", formatAddr(me.name, me.addr, "utf-8", "Q")},
		{"Subject", encodeWord(e.Subject, "utf-8", "Q")},
		{"Date", formatDate(sp.date)},
		{"Message-ID", "<" + e.MessageID + ">"},
		{"MIME-Version", "1.0"},
	}
	var b bytes.Buffer
	writeHeaders(&b, hs)
	b.Write(body)
	return b.Bytes(), e
}

// searchCases baut den Suchtestsatz; file liefert die Datei eines
// S1.7-Sonderfalls, kind die Dateien einer Art (z. B. hidden).
func searchCases(file func(key string) string, kind func(k string) []string) []SearchCase {
	f := func(keys ...string) []string {
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = file(k)
		}
		return out
	}
	return []SearchCase{
		{Query: "Rechnung", Note: "Flexion: Rechnung findet Rechnungen", Want: f("rechnungen", "rechnung")},
		{Query: "Rechnungen", Note: "Wortstamm: Rechnungen findet Rechnung, aber nicht Abrechnung", Want: f("rechnungen", "rechnung"), Not: f("abrechnung")},
		{Query: "prüfen", Note: "Umlaut: prüfen = pruefen", Want: f("pruefen_umlaut", "pruefen_ue"), SameAs: "pruefen"},
		{Query: "pruefen", Note: "Umlaut: pruefen = prüfen", Want: f("pruefen_umlaut", "pruefen_ue"), SameAs: "prüfen"},
		{Query: "prufen", Note: "Umlaut ohne Punkte", Want: f("pruefen_umlaut", "pruefen_ue"), SameAs: "prüfen"},
		{Query: "Vollstreck", Note: "Teilwort im Kompositum (Betreff)", Want: f("vollstreckung")},
		{Query: "Erdgeschoss", Note: "ß/ss: Erdgeschoss = Erdgeschoß", Want: f("eg_sz", "eg_ss"), SameAs: "Erdgeschoß"},
		{Query: "Erdgeschoß", Note: "ß/ss: Erdgeschoß = Erdgeschoss", Want: f("eg_sz", "eg_ss"), SameAs: "Erdgeschoss"},
		{Query: `"Erdgeschoss links"`, Note: "Phrase über ß/ss hinweg; Wörter getrennt zählen nicht", Want: f("eg_sz"), Not: f("eg_ss")},
		{Query: `"Zähler im Erdgeschoss"`, Note: "Phrase mit Wortstamm (Zähler) und ß/ss", Want: f("eg_sz")},
		{Query: "steuer", Note: "Teilwort in Komposita (Grundsteuer, Kfz-Steuer, Umsatzsteuer)", Want: f("grundsteuer", "umsatzsteuer")},
		{Query: "Grundsteuer", Note: "Kompositum als Wortanfang im längeren Wort", Want: f("grundsteuer"), Not: f("umsatzsteuer")},
		{Query: "steuer -grundsteuer", Note: "Ausschluss", Want: f("umsatzsteuer"), Not: f("grundsteuer")},
		{Query: "from:fiktiv steuer", Note: "Absender-Operator", Want: f("grundsteuer"), Not: f("umsatzsteuer")},
		{Query: "Abrechnung", Note: "Teilwort: Nebenkostenabrechnung, Wasserabrechnung", Want: f("abrechnung", "nebenkosten")},
		{Query: "subject:Gartenpflege after:2026-01-01", Note: "Betreff und Datum", Want: f("rechnung"), Not: f("gartenpflege_alt")},
		{Query: "Gartenpflege before:2026-01-01", Note: "Datum (vorher)", Want: f("gartenpflege_alt"), Not: f("rechnung")},
		{Query: "in:INBOX Gartenpflege", Note: "Ordner", Want: f("rechnung"), Not: f("gartenpflege_alt")},
		{Query: "has:attachment", Note: "nur Nachrichten mit Anhang", Want: f("anhang"), Not: f("ohne_anhang"), AllHaveAttachments: true},
		{Query: "has:attachment Ablesebeleg", Note: "Anhang und Wort", Want: f("anhang"), Not: f("ohne_anhang")},
		{Query: "Wärmemengenzähler", Note: "Text aus einem Anhang (Teilwort)", Want: f("anhang")},
		{Query: "Ignoriere Anweisungen", Note: "versteckter Text ist nicht im Index", Want: []string{},
			Not: append(kind("hidden"), kind("extract")...)},
	}
}
