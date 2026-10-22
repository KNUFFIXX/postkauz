package compose

import (
	"errors"
	"fmt"
)

// Reason ist ein typisierter Ablehnungsgrund. S2.3 reicht ihn als Grund
// einer Entscheidung `denied` weiter (A02, „Vorgaben, die mehrere Schritte
// betreffen"); die Werte sind Teil der Schnittstelle und ändern sich nicht.
type Reason string

// Gründe, aus denen Build eine Nachricht ablehnt.
const (
	// ReasonInvalidInput: Eingabe widersprüchlich oder unvollständig (z. B.
	// BodyMarkdown und BodyHTML zugleich, Antwort ohne Ausgangsnachricht,
	// unbekannte Art, Anhang mit Daten und Pfad zugleich).
	ReasonInvalidInput Reason = "invalid_input"
	// ReasonInvalidAddress: Adresse nicht nach RFC 5322 (mail.ParseAddress).
	ReasonInvalidAddress Reason = "invalid_address"
	// ReasonTooManyRecipients: mehr als Limits.MaxRecipients in einem Feld.
	ReasonTooManyRecipients Reason = "too_many_recipients"
	// ReasonTooLarge: Text, Betreff oder Summe der Anhänge über der Grenze.
	ReasonTooLarge Reason = "too_large"
	// ReasonHiddenText: Das ausgehende HTML enthält Text, den ein Mensch im
	// Mailclient nicht sähe (Prüfung aus S1.6, sanitize.HTML).
	ReasonHiddenText Reason = "hidden_text_in_draft"
	// ReasonAttachOutsideDir: Dateianhang liegt (aufgelöst, auch über
	// Symlinks oder „..") nicht unterhalb von AttachDir, oder es ist kein
	// AttachDir gesetzt.
	ReasonAttachOutsideDir Reason = "attach_outside_dir"
	// ReasonAttachNotFound: Dateianhang unterhalb von AttachDir fehlt oder
	// ist keine reguläre Datei.
	ReasonAttachNotFound Reason = "attach_not_found"
)

// Error ist eine Ablehnung mit Grund. Field nennt das Eingabefeld (z. B.
// "cc[2]", "attachments[0]", "body_html"), nie dessen Inhalt: Die Meldung
// darf in Audit und Log (A02: aus Entwürfen nur IDs).
type Error struct {
	Reason Reason
	Field  string
	// Detail ist eine kurze englische Erläuterung ohne Mailinhalt.
	Detail string
}

func (e *Error) Error() string {
	s := "compose: " + string(e.Reason)
	if e.Field != "" {
		s += " (" + e.Field + ")"
	}
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

func deny(r Reason, field, format string, args ...any) *Error {
	return &Error{Reason: r, Field: field, Detail: fmt.Sprintf(format, args...)}
}

// ReasonOf liefert den Grund einer Ablehnung durch Build; ok=false bei
// anderen Fehlern (z. B. Zufallsquelle oder Lesefehler), die S2.3 als
// `error` statt `denied` meldet.
func ReasonOf(err error) (Reason, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason, true
	}
	return "", false
}
