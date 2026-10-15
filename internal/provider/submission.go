package provider

import "context"

// Envelope ist der SMTP-Umschlag einer Sendung.
type Envelope struct {
	From string
	To   []string // inklusive Cc und Bcc
}

// Submission ist der Versandweg (SMTP, JMAP EmailSubmission, später Gmail-API
// und Graph).
//
// AB SCHNITT 4. Im MVP (draft-only, ADR-002) gibt es keine Implementierung
// und keine Verdrahtung: kein SMTP-Code, kein Aufruf aus Operationen. Das
// Interface steht hier nur, damit das Provider-Modell vollständig beschrieben
// ist und Entwürfe schon jetzt alles tragen, was ein Versand später braucht
// (Message-ID, Umschlag, Anhang-Hashes).
type Submission interface {
	// Send versendet die Rohmail mit dem Umschlag.
	Send(ctx context.Context, raw []byte, env Envelope) error
	// SavesSentCopy meldet, ob der Anbieter die Kopie in „Gesendet" selbst
	// anlegt (Gmail: ja, Dovecot: nein). Sonst legt PostKauz sie per Append an.
	SavesSentCopy() bool
}
