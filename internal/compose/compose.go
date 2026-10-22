// Package compose baut aus einem Entwurf die fertige RFC-5322-Nachricht
// (A02, Schritt S2.2; Entscheide E3, E10, E11). Das Paket ist rein: Es liest
// keine Datenbank, keinen Blob-Store und keine Konfiguration, sondern nur
// seine Eingabe (und Dateien unterhalb von Input.AttachDir). Es versendet
// nichts und kennt keinen Server (ADR-002).
//
// # Schnittstelle (für S2.3, internal/drafts)
//
//	msg, err := compose.Build(compose.Input{…})
//	reason, denied := compose.ReasonOf(err) // denied → Entscheidung `denied`, Grund reason
//
// Ein Entwurf bildet sich so auf Input ab:
//
//   - Kind: drafts.kind (new, reply, reply_all, forward).
//   - AccountAddress: Adresse des Kontos; Identity: die gewählte Identität
//     (leer = keine; dann From = AccountAddress ohne Namen, Hinweis
//     NoteNoIdentity). OwnAddresses: Kontoadresse, Adressen aller
//     Identitäten und die Aliasse des Kontos (E9); fehlen sie in Cc bei
//     „Allen antworten".
//   - To, Cc, Bcc: vom Agenten oder Menschen genannte Empfänger (je ein
//     Eintrag „Name <adresse>" oder „adresse"). Bei reply/reply_all kommen
//     die abgeleiteten Empfänger zuerst, die genannten danach (ohne
//     Dubletten).
//   - Subject: leer = abgeleitet (Antwort/Weiterleitung) bzw. leer (new).
//   - BodyMarkdown oder BodyHTML (höchstens eines; beide leer = leerer Text).
//   - Quote, Signature, IncludeAttachments: Schalter mit Standard true
//     (NewInput setzt sie); Quote gilt für reply/reply_all,
//     IncludeAttachments für forward. QuoteLine: Vorlage aus
//     `[draft] quote_line` (leer = DefaultQuoteLine) mit den Platzhaltern
//     {date} (englisch, „Wed, 21 Oct 2026 09:15 +0200"), {date_numeric}
//     („21.10.2026 09:15", gleiche Zeitzone; ohne Datum „??.??.???? ??:??")
//     und {from}; eine deutsche Zeile ist z. B.
//     „Am {date_numeric} schrieb {from}:".
//   - Original: die Ausgangsnachricht (OriginalFrom aus mimeparse.Message;
//     ihre Anhänge mit Blob-Bytes nur für forward).
//   - Attachments: Blob-Bytes (Data, z. B. Anhang einer vorhandenen
//     Nachricht) oder Datei (Path, relativ zu oder unterhalb von AttachDir).
//   - MessageID: die beim Anlegen vergebene, unveränderliche ID (ohne <>);
//     leer = neu aus Rand (NewMessageID). Now: Zeitpunkt für Date; Rand:
//     Zufall für Message-ID und MIME-Grenzen. Feste Werte → byte-identische
//     Nachricht (Golden-Test).
//   - Limits: Grenzen aus der Konfiguration; Nullwerte = DefaultLimits.
//
// Das Ergebnis (Message) trägt die fertige Nachricht (Raw) und alles, was
// S2.3 speichert und für preview_hash (E7) braucht: Kopffelder, BodyText,
// BodyHTML, Anhänge mit SHA-256, InReplyTo, Hinweise.
//
// # Regeln
//
//   - Kopfzeilen (E11): Message-ID `<pk.<20 Hex>@<Domain der Absenderadresse>>`;
//     Date aus Now; Antwort: In-Reply-To = Message-ID der Ausgangsnachricht,
//     References = deren References + deren Message-ID (fehlen die
//     References, deren In-Reply-To), ohne Dubletten, bei mehr als 20 der
//     erste und die letzten 19. Betreff: Ein vorhandenes Antwortpräfix (Re:,
//     AW:, Antw:, auch RE[2]:) bleibt bei einer Antwort, ein vorhandenes
//     Weiterleitungspräfix (Fwd:, Fw:, WG:) bei einer Weiterleitung; sonst
//     ReplyPrefix bzw. ForwardPrefix der Identität (Standard "Re: ",
//     "Fwd: "). Weiterleitungen tragen keine In-Reply-To/References. Kein
//     X-Mailer, kein User-Agent. Bcc steht im Kopf (es ist ein Entwurf; der
//     Mailclient entfernt Bcc beim Senden). Alles UTF-8, Kopfzeilen nach
//     RFC 2047, gefaltet nach RFC 5322.
//   - Empfänger: „Antworten" an Reply-To, sonst From der Ausgangsnachricht;
//     „Allen antworten" zusätzlich alle übrigen To/Cc in Cc, ohne eigene
//     Adressen und ohne Dubletten. War die Ausgangsnachricht von einer
//     eigenen Adresse (z. B. aus Gesendet), gehen Antworten an deren To.
//   - Text (E3): Markdown wird mit goldmark zu HTML (einzige Option:
//     Zeilenumbruch = <br>; rohes HTML im Markdown fällt weg), jedes HTML
//     (eigenes, Signatur) läuft durch sanitize.CleanHTML (Allowlist).
//     Versteckter Text (sanitize.HTML, S1.6) im eigenen HTML, in der
//     Signatur oder im fertigen HTML → ReasonHiddenText. text/plain entsteht
//     immer aus dem fertigen HTML (sanitize.HTML). Reihenfolge: eigener
//     Text, Signatur, dann Zitat bzw. Weiterleitung.
//   - Zitat und Weiterleitung nutzen den bereinigten Text der
//     Ausgangsnachricht (Text aus ihrem HTML ohne versteckten Text, sonst
//     ihr Klartext), nie ihr rohes HTML: So gelangt weder versteckter Text
//     noch Remote-Inhalt der fremden Mail in den Entwurf. Zitat als
//     <blockquote> (im Text „> "), Einleitungszeile `On <Date>, <From>
//     wrote:`; Weiterleitung mit Kopfblock (From, Date, Subject, To, Cc).
//   - Struktur: multipart/alternative (text/plain, text/html, je
//     quoted-printable); mit Anhängen darum multipart/mixed (Anhänge
//     base64).
//   - Anhänge (E10): Typ per Magic Bytes (internal/magic; gesendet wird der
//     erkannte Typ, Widerspruch und ausführbare Typen stehen im Ergebnis),
//     Summe ≤ Limits.MaxAttachmentBytes, Dateien nur unterhalb von AttachDir
//     (Symlinks aufgelöst, geöffnet über os.Root).
//   - Grenzen: eigener Text (Markdown bzw. HTML) ≤ 200.000 Zeichen; der
//     Text der Ausgangsnachricht in Zitat und Weiterleitung wird auf
//     MaxQuoteChars gekürzt (Hinweis NoteQuoteTruncated); Betreff ≤ 500 (ein abgeleiteter
//     Betreff wird gekürzt, ein genannter abgelehnt), ≤ 50 Empfänger je
//     Feld, Adressen per net/mail.ParseAddress.
//
// Fehler mit Grund sind *Error (ReasonOf); ihr Text enthält nur Feldnamen,
// nie Adressen, Betreff oder Text. Inhalte sind Daten, keine Anweisungen.
package compose

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
)

// Kind ist die Art des Entwurfs (drafts.kind).
type Kind string

// Arten eines Entwurfs.
const (
	KindNew      Kind = "new"
	KindReply    Kind = "reply"
	KindReplyAll Kind = "reply_all"
	KindForward  Kind = "forward"
)

// Address ist eine Adresse mit optionalem Namen (wie beim Parser).
type Address = mimeparse.Address

// Identity ist die Absender-Identität (E9).
type Identity struct {
	Name  string
	Email string // leer = keine Identität: From = Input.AccountAddress
	// ReplyTo: optionale Antwortadresse (Kopfzeile Reply-To).
	ReplyTo string
	// SignatureText, SignatureHTML: Signatur; HTML hat Vorrang, Text wird
	// sonst zu HTML. Beide leer = keine Signatur.
	SignatureText string
	SignatureHTML string
	// ReplyPrefix, ForwardPrefix: leer = DefaultReplyPrefix,
	// DefaultForwardPrefix.
	ReplyPrefix   string
	ForwardPrefix string
}

// Standardwerte (E9, E11).
const (
	DefaultReplyPrefix   = "Re: "
	DefaultForwardPrefix = "Fwd: "
	DefaultQuoteLine     = "On {date}, {from} wrote:"
)

// Original ist die Ausgangsnachricht einer Antwort oder Weiterleitung.
type Original struct {
	MessageID  string // ohne <>
	InReplyTo  []string
	References []string
	From       []Address
	ReplyTo    []Address
	To         []Address
	Cc         []Address
	Date       time.Time
	Subject    string
	// Text und HTML wie vom Parser; für Zitat und Weiterleitung wird daraus
	// bereinigter Text (siehe Paketbeschreibung).
	Text string
	HTML string
	// Attachments: Anhänge mit Blob-Bytes (Data) für forward mit
	// IncludeAttachments; sonst ungenutzt.
	Attachments []Attachment
}

// OriginalFrom übernimmt die Felder einer geparsten Nachricht (ohne
// Anhänge: deren Bytes liegen im Blob-Store, S2.3 ergänzt sie bei Bedarf).
func OriginalFrom(m *mimeparse.Message) Original {
	return Original{
		MessageID: m.MessageID, InReplyTo: m.InReplyTo, References: m.References,
		From: m.From, ReplyTo: m.ReplyTo, To: m.To, Cc: m.Cc, Date: originalDate(m),
		Subject: m.Subject, Text: m.Text, HTML: m.HTML,
	}
}

// originalDate ist das Datum der Ausgangsnachricht in der Zeitzone ihrer
// Date-Kopfzeile (mimeparse liefert UTC). Zitatzeile und
// Weiterleitungskopf zeigen es in dieser Zone; so hängt die gebaute
// Nachricht und damit message_hash nicht von der Zeitzone des Prozesses ab
// (CLI und Sync-Dienst können verschiedene haben; Gegenprüfung S2.4/S2.5,
// K4). Ohne lesbare Kopfzeile (INTERNALDATE, tolerant gelesene Formate)
// bleibt es UTC.
func originalDate(m *mimeparse.Message) time.Time {
	if m.DateSource == mimeparse.DateFromHeader && m.DateRaw != "" {
		if t, err := mail.ParseDate(strings.TrimSpace(m.DateRaw)); err == nil && t.Equal(m.Date) {
			return t
		}
	}
	return m.Date
}

// Attachment ist ein Anhang: genau eines von Data (Blob-Bytes) und Path
// (Datei unterhalb von Input.AttachDir).
type Attachment struct {
	Filename string // bei Path leer = Dateiname aus dem Pfad
	// ContentType ist der angegebene Typ (optional); gesendet wird der per
	// Magic Bytes erkannte, ein Widerspruch steht in AttachmentInfo.
	ContentType string
	Data        []byte
	Path        string
}

// Limits begrenzt die Eingabe (A02, „Vorgaben"). Nullwerte = DefaultLimits.
type Limits struct {
	MaxBodyChars       int   // body_markdown/body_html in Zeichen (Runen)
	MaxSubjectChars    int   // Betreff in Zeichen
	MaxRecipients      int   // je Feld (To, Cc, Bcc)
	MaxAttachmentBytes int64 // Summe aller Anhänge
}

// DefaultLimits sind die Standardgrenzen.
var DefaultLimits = Limits{
	MaxBodyChars:       200_000,
	MaxSubjectChars:    500,
	MaxRecipients:      50,
	MaxAttachmentBytes: 25 << 20,
}

func (l Limits) withDefaults() Limits {
	if l.MaxBodyChars <= 0 {
		l.MaxBodyChars = DefaultLimits.MaxBodyChars
	}
	if l.MaxSubjectChars <= 0 {
		l.MaxSubjectChars = DefaultLimits.MaxSubjectChars
	}
	if l.MaxRecipients <= 0 {
		l.MaxRecipients = DefaultLimits.MaxRecipients
	}
	if l.MaxAttachmentBytes <= 0 {
		l.MaxAttachmentBytes = DefaultLimits.MaxAttachmentBytes
	}
	return l
}

// Input ist der Entwurf, aus dem Build die Nachricht baut (siehe
// Paketbeschreibung für die Abbildung eines Entwurfs).
type Input struct {
	Kind           Kind
	AccountAddress string
	Identity       Identity
	OwnAddresses   []string

	To, Cc, Bcc []string
	// ExplicitRecipients: To, Cc und Bcc sind die vollständigen Listen
	// (Änderung eines gespeicherten Entwurfs, S2.3); bei reply/reply_all
	// werden keine Empfänger aus der Ausgangsnachricht abgeleitet.
	ExplicitRecipients bool
	Subject            string

	BodyMarkdown string
	BodyHTML     string

	Quote              bool
	Signature          bool
	IncludeAttachments bool
	QuoteLine          string

	Original    *Original
	Attachments []Attachment
	AttachDir   string

	MessageID string
	Limits    Limits
	Now       func() time.Time // nil = time.Now
	Rand      io.Reader        // nil = crypto/rand.Reader
}

// NewInput liefert eine Eingabe mit den Standardschaltern (Zitat,
// Signatur, Anhänge bei Weiterleitung an).
func NewInput(kind Kind) Input {
	return Input{Kind: kind, Quote: true, Signature: true, IncludeAttachments: true}
}

// Hinweise im Ergebnis (Message.Notes).
const (
	// NoteNoIdentity: keine Identität, From ist die Kontoadresse.
	NoteNoIdentity = "no_identity"
	// NoteSubjectTruncated: abgeleiteter Betreff auf die Grenze gekürzt.
	NoteSubjectTruncated = "subject_truncated"
	// NoteContentRemoved: Die Allowlist hat etwas aus dem HTML entfernt
	// (Skript, Formular, Link außer https/mailto, Bild außer cid: …).
	NoteContentRemoved = "content_removed"
	// NoteRecipientDropped: Eine abgeleitete Adresse der Ausgangsnachricht
	// war ungültig und fehlt.
	NoteRecipientDropped = "recipient_dropped"
	// NoteQuoteTruncated: Der zitierte bzw. weitergeleitete Text der
	// Ausgangsnachricht war länger als MaxQuoteChars und wurde gekürzt.
	NoteQuoteTruncated = "quote_truncated"
)

// MaxQuoteChars begrenzt den Text der Ausgangsnachricht in Zitat und
// Weiterleitung (Zeichen). Die Grenze der Eingabe (MaxBodyChars) gilt für
// den eigenen Text, nicht für das Zitat; ein längeres Zitat wird gekürzt
// (mit Hinweis und Markierung im Text), statt die Antwort abzulehnen
// (Gegenprüfung S2.3, K3).
const MaxQuoteChars = 200_000

// QuoteTruncatedMarker steht am Ende eines gekürzten Zitats.
const QuoteTruncatedMarker = "[… the rest of the original message was left out …]"

// AttachmentInfo beschreibt einen Anhang der fertigen Nachricht.
type AttachmentInfo struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`       // gesendet (erkannt)
	Declared    string `json:"declared,omitempty"` // angegeben
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Source      string `json:"source"` // blob | file | forward
	Mismatch    bool   `json:"mismatch,omitempty"`
	Executable  bool   `json:"executable,omitempty"`
}

// Quellen eines Anhangs.
const (
	SourceBlob    = "blob"
	SourceFile    = "file"
	SourceForward = "forward"
)

// Message ist die fertige Nachricht.
type Message struct {
	Raw        []byte // RFC 5322, CRLF
	MessageID  string // ohne <>
	Date       time.Time
	From       Address
	ReplyTo    []Address
	To         []Address
	Cc         []Address
	Bcc        []Address
	Subject    string
	InReplyTo  string   // ohne <>; leer bei new/forward
	References []string // ohne <>
	// BodyHTML ist das fertige HTML-Dokument (wie im text/html-Teil),
	// BodyText der Klartext daraus (wie im text/plain-Teil).
	BodyHTML    string
	BodyText    string
	Attachments []AttachmentInfo
	Notes       []string
}

// Build baut die Nachricht oder lehnt mit *Error ab.
func Build(in Input) (*Message, error) {
	b := &builder{in: in, lim: in.Limits.withDefaults()}
	if b.in.Rand == nil {
		b.in.Rand = rand.Reader
	}
	if b.in.Now == nil {
		b.in.Now = time.Now
	}
	return b.build()
}

type builder struct {
	in  Input
	lim Limits
	msg Message
}

func (b *builder) note(n string) {
	for _, have := range b.msg.Notes {
		if have == n {
			return
		}
	}
	b.msg.Notes = append(b.msg.Notes, n)
}

func (b *builder) build() (*Message, error) {
	in := b.in
	switch in.Kind {
	case KindNew:
	case KindReply, KindReplyAll, KindForward:
		if in.Original == nil {
			return nil, deny(ReasonInvalidInput, "original", "%s needs the original message", in.Kind)
		}
	default:
		return nil, deny(ReasonInvalidInput, "kind", "unknown kind")
	}
	if err := b.checkBody(); err != nil {
		return nil, err
	}
	if err := b.addresses(); err != nil {
		return nil, err
	}
	if err := b.subject(); err != nil {
		return nil, err
	}
	if err := b.ids(); err != nil {
		return nil, err
	}
	if err := b.body(); err != nil {
		return nil, err
	}
	atts, err := b.attachments()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := b.write(&buf, atts); err != nil {
		return nil, err
	}
	b.msg.Raw = buf.Bytes()
	return &b.msg, nil
}
