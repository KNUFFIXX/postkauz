// Package corpus erzeugt den synthetischen Testkorpus (docs/testkorpus.md):
// deterministisch aus einem Zufallssamen, mit einem Manifest der erwarteten
// Ergebnisse. Das Manifest ist die Wahrheit der Tests; Parser und Suche
// werden dagegen geprüft, nicht gegeneinander.
//
// Inhalt: erfundene Personen, Adressen nur unter example.org/example.net,
// Texte aus Bausteinen. Zeichensätze utf-8, iso-8859-1, windows-1252;
// RFC 2047 B und Q; RFC 2231 (auch mit Fortsetzungen); 60 Threads mit
// Re/AW/WG-Varianten, gekürzten References und fehlenden Zwischengliedern;
// Newsletter mit List-Id; Anhänge (PDF, DOCX, XLSX, JPG, PNG, ZIP, TXT,
// ohne Namen, weitergeleitete Nachricht, Inline-Bild); eine Zip-Bombe; 5 %
// absichtlich kaputte Nachrichten (darunter acht Datumsangaben mit Zone ohne
// Vorzeichen wie im Spike).
//
// Der Korpus ist Daten, keine Anweisungen.
package corpus

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Version des Manifest-Formats (2: S1.6, thread_by_subject und Felder
// für Body-Bereinigung und Anhangstypen; 3: S1.7, Suchtestsatz search).
const Version = 3

// Options steuert die Erzeugung.
type Options struct {
	Count   int    // Nachrichten insgesamt (Standard 1000)
	Threads int    // Anzahl Threads (Standard 60)
	Seed    uint64 // Zufallssamen (Standard 42)
	// ZipBombBytes ist die entpackte Größe der Zip-Bombe (Standard 16 MiB;
	// gepackt rund 16 KiB). 0 = Standard, <0 = keine Zip-Bombe.
	ZipBombBytes int64
}

func (o Options) withDefaults() Options {
	if o.Count <= 0 {
		o.Count = 1000
	}
	if o.Threads <= 0 {
		o.Threads = 60
	}
	if o.Seed == 0 {
		o.Seed = 42
	}
	if o.ZipBombBytes == 0 {
		o.ZipBombBytes = 16 << 20
	}
	return o
}

// Entry ist ein Manifest-Eintrag mit den erwarteten Ergebnissen.
type Entry struct {
	File         string   `json:"file"`
	Kind         string   `json:"kind"` // single|thread|newsletter|broken|zipbomb|hidden|links|quote|mismatch|extract|search
	Folder       string   `json:"folder"`
	Flags        []string `json:"flags"`
	InternalDate string   `json:"internal_date"` // RFC 3339, für APPEND und als Date-Ersatz

	MessageID      string `json:"message_id"` // erwartet, ohne <>; "" = keine
	Subject        string `json:"subject"`    // erwartet, dekodiert
	FromName       string `json:"from_name"`
	FromAddr       string `json:"from_addr"`
	ToAddr         string `json:"to_addr"`
	Charset        string `json:"charset"`
	HeaderEncoding string `json:"header_encoding"` // B|Q|raw|ascii
	Date           string `json:"date"`            // erwartet, RFC 3339 UTC
	DateSource     string `json:"date_source"`     // header|internal
	Thread         string `json:"thread,omitempty"`
	// ThreadBySubject: Antwort ohne References/In-Reply-To; sie gehört nur
	// über den Betreff-Rückfall (Präfix, Zeitfenster) zum Thread.
	ThreadBySubject bool     `json:"thread_by_subject,omitempty"`
	Attachments     []string `json:"attachments"`  // Dateinamen der Anhänge (nicht inline), in Reihenfolge
	InlineParts     int      `json:"inline_parts"` // Inline-Teile ohne Body-Text (z. B. eingebettete Bilder)
	HTML            bool     `json:"html"`
	Newsletter      bool     `json:"newsletter,omitempty"`
	ListID          string   `json:"list_id,omitempty"`
	BodyContains    string   `json:"body_contains,omitempty"` // Satz, der im Text stehen muss
	Broken          string   `json:"broken,omitempty"`
	ZipBomb         bool     `json:"zip_bomb,omitempty"`
	// S1.6 (Manifest-Version 2): Body-Bereinigung, Links, Zitate, Anhangstypen.
	HiddenText       bool               `json:"hidden_text,omitempty"`       // HTML enthält versteckten Text
	HiddenContains   string             `json:"hidden_contains,omitempty"`   // darf im bereinigten Text nicht stehen
	HTMLContains     string             `json:"html_contains,omitempty"`     // muss im Text aus dem HTML stehen
	Links            []ExpectLink       `json:"links,omitempty"`             // http(s)-Links in Reihenfolge
	RemovedLinks     int                `json:"removed_links,omitempty"`     // entfernte Ziele (javascript:, mailto:)
	TrackingPixels   int                `json:"tracking_pixels,omitempty"`   // 1×1-Bilder
	UnquotedContains string             `json:"unquoted_contains,omitempty"` // steht im Text ohne Zitate
	QuotedContains   string             `json:"quoted_contains,omitempty"`   // zitiert: fehlt im Text ohne Zitate
	AttachmentTypes  []ExpectAttachment `json:"attachment_types,omitempty"`  // Typprüfung und Extraktion
	// Unchecked nennt Felder, die bei dieser kaputten Nachricht nicht
	// geprüft werden (z. B. Anhänge einer abgeschnittenen Nachricht).
	Unchecked []string `json:"unchecked,omitempty"`
}

// Manifest ist der Inhalt von corpus.json.
type Manifest struct {
	Version  int            `json:"version"`
	Seed     uint64         `json:"seed"`
	Count    int            `json:"count"`
	Summary  map[string]int `json:"summary"`
	Messages []Entry        `json:"messages"`
	// Search ist der Suchtestsatz (S1.7, Manifest-Version 3; s17.go).
	Search []SearchCase `json:"search,omitempty"`
}

// Corpus ist ein erzeugter Korpus.
type Corpus struct {
	Manifest Manifest
	Raw      [][]byte // Rohmails in Manifest-Reihenfolge
}

// ---- Bausteine (erfunden) ----------------------------------------------------

type person struct{ name, addr string }

var people = []person{
	{"Jürgen Testmann", "juergen.testmann@mieter.example.org"},
	{"Bärbel Beispiel", "baerbel@beispiel.example.org"},
	{"Ömer Platzhalter", "oemer.p@verwaltung.example.org"},
	{"Grete Größe", "grete@groesse.example.net"},
	{"Ännchen Übermut", "aennchen@uebermut.example.net"},
	{"Sören Straßburger", "soeren@strasse.example.org"},
	{"Hans-Jörg Fiktiv", "hj.fiktiv@handwerk.example.net"},
	{"Zoë Musterfrau", "zoe@muster.example.org"},
	{"Malte Mängel", "malte@maengel.example.net"},
	{"Dörte Dummy", "doerte@dummy.example.org"},
	{"Firma Rohr & Söhne GmbH", "auftrag@rohr-soehne.example.net"},
	{"", "noreply@portal.example.net"},
}

var me = person{"Postfach Test", "postfach@example.org"}

var subjects = []string{
	"Heizungsablesung im Erdgeschoß",
	"Schlüsselübergabe für Wohnung 3 links",
	"Nebenkostenabrechnung – Rückfrage zur Grundsteuer",
	"Wasserschaden im Badezimmer (dringend!) 💧",
	"Kündigung des Stellplatzes zum Monatsende",
	"Rauchwarnmelder-Wartung: Terminvorschläge",
	"Treppenhausreinigung – Beschwerde über Lärm",
	"Angebot für Fassadenanstrich „Südseite“",
	"Mieterhöhung gemäß § 558 BGB – Zustimmung",
	"Müllgebühren und Biotonne – Änderungen ab Januar",
	"Fenster schließt nicht – Reparatur nötig 🔧",
	"Übergabeprotokoll und Zählerstände",
	"Zwangsvollstreckung angedroht – bitte prüfen",
	"Rechnungen für das Erdgeschoss",
}

// longSubject wird gelegentlich verwendet (sehr lange, gefaltete Betreffzeile).
const longSubject = "Sehr lange Betreffzeile zur Nebenkostenabrechnung mit Grundsteuer, Müllgebühren, Straßenreinigung, Winterdienst, Treppenhausreinigung, Gartenpflege, Allgemeinstrom und Aufzugswartung für das gesamte Gebäude"

var sentences = []string{
	"Sehr geehrte Damen und Herren, ich wende mich wegen der Heizkörper an Sie.",
	"Die Thermostatventile im Wohnzimmer lassen sich nicht mehr drehen.",
	"Können Sie bitte einen Handwerker schicken? Ich bin werktags ab 15 Uhr erreichbar.",
	"Anbei übersende ich Ihnen die Zählerstände vom Übergabetermin.",
	"Die Grundsteuer ist im Vergleich zum Vorjahr um 12,5 % gestiegen – warum?",
	"Im Keller riecht es muffig; vermutlich ist die Lüftung defekt.",
	"Für Rückfragen stehe ich gern zur Verfügung. Mit freundlichen Grüßen",
	"Der Gärtner hat die Hecke zu stark gekürzt, die Nachbarn beschweren sich.",
	"Bitte bestätigen Sie den Eingang dieser Nachricht kurz per E-Mail.",
	"Die Rechnung über 1.234,56 € lege ich als PDF bei.",
	"Äpfel, Öfen und Übergrößen: Umlaute müssen überall korrekt ankommen.",
	"Straßenreinigung und Winterdienst sind laut Vertrag umlagefähig.",
	"Die Waschmaschine im Gemeinschaftsraum ist seit Dienstag außer Betrieb.",
	"Ich bitte um Zusendung einer Kopie des Mietvertrags inklusive Anlagen.",
	"Die Nebenkostenabrechnungen der letzten Jahre bitte ich prüfen zu dürfen.",
	"Im Erdgeschoss ist die Haustür seit gestern nicht mehr abschließbar.",
}

type attKind struct {
	name, ctype string
	gen         func(r *rand.Rand) []byte
}

var attachmentKinds = []attKind{
	{"Übergabeprotokoll Wohnung 3.pdf", "application/pdf", fakePDF},
	{"Zählerstände_März.pdf", "application/pdf", fakePDF},
	{"Mietvertrag Änderung.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", fakeDOCX},
	{"Kostenaufstellung 2025.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", fakeXLSX},
	{"Schadensfoto Küche.jpg", "image/jpeg", fakeJPG},
	{"Grundriss Erdgeschoß.png", "image/png", fakePNG},
	{"Kostenübersicht 2025 – Grundsteuer und Müllgebühren für das gesamte Gebäude.pdf", "application/pdf", fakePDF},
	{"Belege.zip", "application/zip", fakeZIP},
	{"Notiz.txt", "text/plain", fakeTXT},
	{"scan0001.pdf", "application/pdf", fakePDF},
}

type newsletter struct{ name, domain, subject string }

var newsletters = []newsletter{
	{"Mieterbund-Rundbrief", "rundbrief.verein.example.org", "Neues aus dem Mietrecht – Ausgabe %d"},
	{"Energie-Tipps", "news.energie.example.net", "Heizkosten sparen: %d Tipps für den Winter"},
	{"Handwerker-Börse", "liste.handwerk.example.org", "Wochenübersicht KW %d: Angebote für Hausverwaltungen"},
}

// Kaputte Nachrichten: Muster für 50 Stück (5 % von 1.000). Die Zone ohne
// Vorzeichen kommt achtmal vor, wie im Spike-Korpus.
var brokenPattern = func() []string {
	counts := []struct {
		kind string
		n    int
	}{
		{"tz_without_sign", 8}, {"missing_date", 7}, {"invalid_date", 4}, {"invalid_message_id", 5},
		{"missing_message_id", 4}, {"raw_8bit_header", 6}, {"deep_nesting", 3}, {"overlong_line", 3},
		{"lf_only", 4}, {"nul_bytes", 2}, {"truncated", 2}, {"header_no_colon", 2},
	}
	var out []string
	// reihum verteilen, damit kleine Korpora alle Arten enthalten
	for round := 0; len(out) < 50; round++ {
		for _, c := range counts {
			if round < c.n {
				out = append(out, c.kind)
			}
		}
	}
	return out
}()

// ---- Erzeugung --------------------------------------------------------------

type gen struct {
	r   *rand.Rand
	opt Options
	idx int
}

func (g *gen) pick(n int) int { return g.r.IntN(n) }

func (g *gen) chance(p float64) bool { return g.r.Float64() < p }

func (g *gen) charset() string {
	switch x := g.pick(100); {
	case x < 60:
		return "utf-8"
	case x < 85:
		return "iso-8859-1"
	default:
		return "windows-1252"
	}
}

func (g *gen) bodyText(n int) []string {
	if n == 0 {
		n = 2 + g.pick(5)
	}
	out := make([]string, n)
	for i := range out {
		out[i] = sentences[g.pick(len(sentences))]
	}
	return out
}

func (g *gen) boundary() string {
	return fmt.Sprintf("----=_Part_%d_%08x", g.idx, g.r.Uint32())
}

func (g *gen) msgID() string {
	return fmt.Sprintf("pk.%05d.%08x@corpus.example.org", g.idx, g.r.Uint32())
}

var base = time.Date(2024, 1, 3, 8, 0, 0, 0, time.FixedZone("CET", 3600))

func (g *gen) date() time.Time {
	return base.Add(time.Duration(g.pick(60*24*1000)) * time.Minute)
}

func folderFor(t time.Time) string {
	switch t.UTC().Year() {
	case 2024:
		return "Archiv/2024"
	case 2025:
		return "Archiv/2025"
	}
	return "INBOX"
}

type plan struct {
	kind      string
	thread    string
	pos       int
	subject   string
	sender    person
	to        person
	date      time.Time
	nl        newsletter
	nlNo      int
	broken    string
	dropped   bool // fehlendes Zwischenglied: Nachricht wird nicht geschrieben
	orphan    bool // Antwort ohne References/In-Reply-To (Betreff-Rückfall)
	tzVariant int
	s16       string // Sonderfall S1.6 (s16.go)
	s17       int    // Sonderfall S1.7 (s17.go), Index in s17Specs
}

// spec beschreibt eine normale Nachricht vor dem Schreiben.
type spec struct {
	charset, enc string
	subject      string
	from, to     person
	date         time.Time
	msgID        string
	inReplyTo    string
	refs         []string
	html         bool
	attachments  []int // Indizes in attachmentKinds; -1 = ohne Namen, -2 = weitergeleitete Nachricht
	related      bool  // HTML mit eingebettetem Bild
	newsletter   *newsletter
	nlNo         int
	cte          string
	cc           []person
	extra        []extraPart // fertige Anhangsteile (Zip-Bombe)
}

type extraPart struct {
	name   string
	entity []byte
}

// Generate erzeugt den Korpus.
func Generate(opt Options) (*Corpus, error) {
	opt = opt.withDefaults()
	g := &gen{r: rand.New(rand.NewPCG(opt.Seed, opt.Seed^0x9e3779b97f4a7c15)), opt: opt}
	n := opt.Count

	var plans []plan
	for t := 0; t < opt.Threads && len(plans) < n; t++ {
		size := 2 + g.pick(7)
		start := g.date()
		subj := subjects[g.pick(len(subjects))] + fmt.Sprintf(" (Vorgang %d)", t+1)
		a := people[g.pick(len(people)-1)] // ohne den namenlosen Absender
		dropAt := -1
		if size >= 4 && g.chance(1.0/6) {
			dropAt = 1
		}
		for k := 0; k < size; k++ {
			// Jeder sechste Thread ab drei Nachrichten endet mit einer Antwort
			// ohne Verweise (Rückfall über Betreff und Zeitfenster, S1.6).
			// Ohne zusätzlichen Zufall, damit alle übrigen Bytes gleich bleiben.
			p := plan{kind: "thread", thread: fmt.Sprintf("T%03d", t+1), pos: k, subject: subj,
				date: start.Add(time.Duration(g.pick(48)+1) * time.Hour * time.Duration(k+1)), dropped: k == dropAt,
				orphan: k == size-1 && size >= 3 && t%6 == 2}
			if k%2 == 0 {
				p.sender, p.to = a, me
			} else {
				p.sender, p.to = me, a
			}
			plans = append(plans, p)
		}
	}
	nNews := 45 * n / 1000
	for k := 0; k < nNews; k++ {
		plans = append(plans, plan{kind: "newsletter", nl: newsletters[k%len(newsletters)], nlNo: k + 1, date: g.date()})
	}
	nBroken := n * 5 / 100
	for k := 0; k < nBroken; k++ {
		plans = append(plans, plan{kind: "broken", broken: brokenPattern[k%len(brokenPattern)], date: g.date(), tzVariant: k})
	}
	if opt.ZipBombBytes > 0 {
		plans = append(plans, plan{kind: "zipbomb", date: g.date()})
	}
	for written(plans) < n {
		plans = append(plans, plan{kind: "single", date: g.date()})
	}
	// Sonderfälle S1.6 ersetzen die letzten Einzelnachrichten (gleiche
	// Zufallszüge beim Planen, Ausgabe am Ende: Alles davor bleibt gleich).
	if n >= 500 {
		k := 0
		for i := len(plans) - len(s16Kinds); i < len(plans); i++ {
			if plans[i].kind == "single" {
				plans[i].kind, plans[i].s16, plans[i].nlNo = "s16", s16Kinds[k], k
				k++
			}
		}
		// Sonderfälle S1.7 direkt davor (ihr Datum steht fest, damit
		// Ordner und Datumsfilter planbar sind).
		k = 0
		for i := len(plans) - len(s16Kinds) - len(s17Specs); i < len(plans)-len(s16Kinds); i++ {
			if plans[i].kind == "single" {
				plans[i].kind, plans[i].s17, plans[i].date = "s17", k, s17Specs[k].date
				k++
			}
		}
	}

	c := &Corpus{Manifest: Manifest{Version: Version, Seed: opt.Seed, Summary: map[string]int{}}}
	s17Files := map[string]string{}
	chains := map[string][]string{}
	drafts := 20 * n / 1000
	for _, p := range plans {
		g.idx++
		var raw []byte
		var e Entry
		switch p.kind {
		case "thread":
			chain := chains[p.thread]
			s := g.normalSpec(p.sender, p.to, p.date)
			s.subject = p.subject
			if p.pos > 0 {
				s.subject = []string{"AW: ", "Re: ", "WG: AW: ", "RE: ", "Fwd: "}[g.pick(5)] + p.subject
				refs := slices.Clone(chain)
				if g.chance(0.2) && len(refs) > 2 {
					refs = refs[len(refs)-2:] // manche Clients kürzen References
				}
				s.refs = refs
				if g.chance(0.85) {
					s.inReplyTo = chain[len(chain)-1]
				}
				if p.orphan {
					s.refs, s.inReplyTo = nil, ""
				}
			}
			chains[p.thread] = append(chain, s.msgID)
			if p.dropped {
				continue
			}
			raw, e = g.render(s)
			e.Thread = p.thread
			e.ThreadBySubject = p.orphan
		case "newsletter":
			s := g.normalSpec(person{p.nl.name, "news@" + p.nl.domain}, me, p.date)
			s.subject = fmt.Sprintf(p.nl.subject, p.nlNo)
			s.html = true
			s.attachments = nil
			s.newsletter = &p.nl
			s.nlNo = p.nlNo
			raw, e = g.render(s)
		case "broken":
			raw, e = g.broken(p)
		case "zipbomb":
			s := g.normalSpec(people[g.pick(len(people)-1)], me, p.date)
			s.subject = "Belege als Archiv (bitte nicht entpacken)"
			s.attachments = nil
			raw, e = g.renderZipBomb(s)
		case "s16":
			raw, e = g.s16(p.s16, p.nlNo, p.date)
		case "s17":
			raw, e = g.s17(p.s17)
		default:
			s := g.normalSpec(people[g.pick(len(people))], me, p.date)
			raw, e = g.render(s)
		}
		e.Kind = p.kind
		if p.kind == "s16" {
			e.Kind = p.s16
		}
		if p.kind == "s17" {
			e.Kind = "search"
		}
		if e.Broken != "" {
			e.Kind = "broken"
		}
		e.File = fmt.Sprintf("%05d.eml", len(c.Raw)+1)
		if p.kind == "s17" {
			s17Files[s17Specs[p.s17].key] = e.File
		}
		e.Folder = folderFor(p.date)
		if e.Folder == "INBOX" && p.kind == "single" && drafts > 0 {
			e.Folder, drafts = "Drafts", drafts-1
		}
		e.Flags = []string{}
		switch {
		case e.Folder == "Drafts":
			e.Flags = append(e.Flags, `\Draft`)
		case e.Folder != "INBOX" || g.chance(0.4):
			e.Flags = append(e.Flags, `\Seen`)
		}
		if g.chance(0.05) {
			e.Flags = append(e.Flags, `\Flagged`)
		}
		if e.Attachments == nil {
			e.Attachments = []string{}
		}
		c.Raw = append(c.Raw, raw)
		c.Manifest.Messages = append(c.Manifest.Messages, e)
		c.Manifest.Summary[e.Kind]++
		if e.Broken != "" {
			c.Manifest.Summary["broken:"+e.Broken]++
		}
		if len(e.Attachments) > 0 {
			c.Manifest.Summary["with_attachments"]++
		}
		if e.ThreadBySubject {
			c.Manifest.Summary["thread_by_subject"]++
		}
	}
	c.Manifest.Count = len(c.Raw)
	c.Manifest.Summary["threads"] = len(chains)
	if len(s17Files) == len(s17Specs) {
		c.Manifest.Search = searchCases(func(key string) string { return s17Files[key] }, func(k string) []string {
			var out []string
			for _, m := range c.Manifest.Messages {
				if m.Kind == k {
					out = append(out, m.File)
				}
			}
			return out
		})
	}
	return c, nil
}

// written zählt Pläne, die eine Datei ergeben.
func written(ps []plan) int {
	n := 0
	for _, p := range ps {
		if !p.dropped {
			n++
		}
	}
	return n
}

func (g *gen) normalSpec(from, to person, date time.Time) spec {
	s := spec{
		charset: g.charset(),
		enc:     "B",
		from:    from,
		to:      to,
		date:    date,
		msgID:   g.msgID(),
		html:    g.chance(0.35),
		cte:     []string{"quoted-printable", "quoted-printable", "base64", "8bit"}[g.pick(4)],
	}
	if g.chance(0.3) {
		s.enc = "Q"
	}
	s.subject = subjects[g.pick(len(subjects))]
	if g.chance(0.02) {
		s.subject = longSubject
	}
	if g.chance(0.18) {
		k := 1 + g.pick(2)
		for range k {
			s.attachments = append(s.attachments, g.pick(len(attachmentKinds)))
		}
		if g.chance(0.1) {
			s.attachments = append(s.attachments, -1) // ohne Dateinamen
		}
		if g.chance(0.05) {
			s.attachments = append(s.attachments, -2) // weitergeleitete Nachricht
		}
	}
	if s.html && g.chance(0.12) {
		s.related = true
	}
	if g.chance(0.1) {
		s.cc = []person{people[g.pick(len(people)-1)]}
	}
	return s
}

// render schreibt eine normale Nachricht und das passende Manifest.
func (g *gen) render(s spec) ([]byte, Entry) {
	cs := s.charset
	subject := adjust(s.subject, cs)
	text := adjust(strings.Join(g.bodyText(0), "\n")+"\n", cs)
	e := Entry{
		MessageID: s.msgID, Subject: subject, FromName: adjust(s.from.name, cs), FromAddr: s.from.addr,
		ToAddr: s.to.addr, Charset: cs, HeaderEncoding: s.enc, HTML: s.html,
		Date: s.date.UTC().Format(time.RFC3339), DateSource: "header",
		InternalDate: s.date.Add(time.Duration(1+g.pick(30)) * time.Minute).UTC().Format(time.RFC3339),
		BodyContains: firstLine(text),
	}
	if isASCII(subject) {
		e.HeaderEncoding = "ascii"
	}

	var body []byte
	plain := textEntity(text, "plain", cs, s.cte)
	if s.html {
		html := "<html><body>"
		for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
			html += "<p>" + htmlEscape(l) + "</p>"
		}
		var htmlPart []byte
		if s.related {
			cid := fmt.Sprintf("bild%05d@corpus.example.org", g.idx)
			html += `<p><img src="cid:` + cid + `"></p></body></html>`
			htmlPart = multipartEntity("related", g.boundary(), [][]byte{
				textEntity(html, "html", cs, "quoted-printable"),
				attachmentEntity("logo.png", "image/png", nameNone, "inline", fakePNG(g.r), cid),
			}, `; type="text/html"`)
			e.InlineParts = 1
		} else {
			html += "</body></html>"
			htmlPart = textEntity(html, "html", cs, "quoted-printable")
		}
		body = multipartEntity("alternative", g.boundary(), [][]byte{plain, htmlPart}, "")
	} else {
		body = plain
	}
	if len(s.attachments)+len(s.extra) > 0 {
		parts := [][]byte{body}
		for _, ai := range s.attachments {
			switch ai {
			case -1:
				parts = append(parts, attachmentEntity("", "application/octet-stream", nameNone, "attachment", fakePDF(g.r), ""))
				e.Attachments = append(e.Attachments, "")
			case -2:
				inner := g.innerMessage()
				parts = append(parts, rfc822Entity("Weiterleitung.eml", inner))
				e.Attachments = append(e.Attachments, "Weiterleitung.eml")
			default:
				k := attachmentKinds[ai]
				style := []string{nameRFC2231, nameRFC2231, nameRFC2047, nameContentTypeO}[g.pick(4)]
				if isASCII(k.name) {
					style = nameASCII
				}
				if len(k.name) > 60 {
					style = nameRFC2231Cont
				}
				parts = append(parts, attachmentEntity(k.name, k.ctype, style, "attachment", k.gen(g.r), ""))
				e.Attachments = append(e.Attachments, k.name)
			}
		}
		for _, x := range s.extra {
			parts = append(parts, x.entity)
			e.Attachments = append(e.Attachments, x.name)
		}
		body = multipartEntity("mixed", g.boundary(), parts, "")
	}

	hs := []header{
		{"From", formatAddr(s.from.name, s.from.addr, cs, s.enc)},
		{"To", formatAddr(s.to.name, s.to.addr, cs, s.enc)},
	}
	for _, c := range s.cc {
		hs = append(hs, header{"Cc", formatAddr(c.name, c.addr, cs, s.enc)})
	}
	hs = append(hs,
		header{"Subject", encodeWord(subject, cs, s.enc)},
		header{"Date", formatDate(s.date)},
		header{"Message-ID", "<" + s.msgID + ">"},
	)
	if s.inReplyTo != "" {
		hs = append(hs, header{"In-Reply-To", "<" + s.inReplyTo + ">"})
	}
	if len(s.refs) > 0 {
		hs = append(hs, header{"References", "<" + strings.Join(s.refs, "> <") + ">"})
	}
	if s.newsletter != nil {
		nl := s.newsletter
		e.Newsletter = true
		e.ListID = adjust(nl.name, cs) + " <" + nl.domain + ">"
		hs = append(hs,
			header{"List-Id", encodeWord(adjust(nl.name, cs), cs, s.enc) + " <" + nl.domain + ">"},
			header{"List-Unsubscribe", fmt.Sprintf("<mailto:unsubscribe@%s>, <https://%s/abmelden?id=%d>", nl.domain, nl.domain, s.nlNo)},
			header{"List-Unsubscribe-Post", "List-Unsubscribe=One-Click"},
			header{"Precedence", "bulk"},
		)
	}
	hs = append(hs, header{"MIME-Version", "1.0"})
	var b bytes.Buffer
	writeHeaders(&b, hs)
	b.Write(body)
	return b.Bytes(), e
}

// innerMessage ist eine kleine weitergeleitete Nachricht (message/rfc822).
func (g *gen) innerMessage() []byte {
	p := people[g.pick(len(people)-1)]
	var b bytes.Buffer
	writeHeaders(&b, []header{
		{"From", formatAddr(p.name, p.addr, "utf-8", "Q")},
		{"To", formatAddr(me.name, me.addr, "utf-8", "Q")},
		{"Subject", encodeWord("Ursprüngliche Nachricht", "utf-8", "Q")},
		{"Date", formatDate(g.date())},
		{"Message-ID", "<inner." + g.msgID() + ">"},
		{"MIME-Version", "1.0"},
	})
	b.Write(textEntity(strings.Join(g.bodyText(2), "\n")+"\n", "plain", "utf-8", "quoted-printable"))
	return b.Bytes()
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// renderZipBomb: kleine ZIP-Datei, die entpackt ZipBombBytes groß wäre.
func (g *gen) renderZipBomb(s spec) ([]byte, Entry) {
	s.extra = append(s.extra, extraPart{
		name:   "Archiv.zip",
		entity: attachmentEntity("Archiv.zip", "application/zip", nameASCII, "attachment", ZipBomb(g.opt.ZipBombBytes), ""),
	})
	raw, e := g.render(s)
	e.ZipBomb = true
	return raw, e
}

// ZipBomb erzeugt eine ZIP-Datei mit einer Datei aus Nullen der Größe n
// (gepackt etwa n/1000). Nur für Tests; sie darf nie entpackt werden.
func ZipBomb(n int64) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "nullen.bin", Method: zip.Deflate,
		Modified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		panic(err)
	}
	zeros := make([]byte, 1<<20)
	for left := n; left > 0; left -= int64(len(zeros)) {
		chunk := zeros
		if left < int64(len(chunk)) {
			chunk = chunk[:left]
		}
		_, _ = w.Write(chunk)
	}
	_ = zw.Close()
	return buf.Bytes()
}

// Write schreibt die Rohmails und corpus.json nach dir.
func (c *Corpus) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, e := range c.Manifest.Messages {
		if err := os.WriteFile(filepath.Join(dir, e.File), c.Raw[i], 0o644); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(c.Manifest, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "corpus.json"), append(b, '\n'), 0o644)
}

// Read liest einen mit Write geschriebenen Korpus (corpus.json und *.eml).
func Read(dir string) (*Corpus, error) {
	b, err := os.ReadFile(filepath.Join(dir, "corpus.json"))
	if err != nil {
		return nil, err
	}
	c := &Corpus{}
	if err := json.Unmarshal(b, &c.Manifest); err != nil {
		return nil, fmt.Errorf("corpus: %s: %w", dir, err)
	}
	for _, e := range c.Manifest.Messages {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(e.File)))
		if err != nil {
			return nil, err
		}
		c.Raw = append(c.Raw, raw)
	}
	return c, nil
}
