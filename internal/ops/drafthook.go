package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/hook"
)

// Prüf-Hook vor der Vorlage (A02 E4, S2.4): NewHookChecker verbindet
// drafts.request_ready (DraftChecker) mit internal/hook. Hier entsteht
// {meta_json}; Dateien, Umgebung, Zeitgrenze und Ergebnis regelt das Paket
// hook. Die Ausgabe des Hooks geht nur ins Ergebnis der Operation, nie ins
// Audit oder Log (dort steht nur pass|fail|timeout|error).

// NewHookChecker liefert den Prüf-Hook aus [draft] check_hook und
// check_hook_timeout der Policy-Datei; bei leerer Liste nil (kein Hook,
// „pass").
func NewHookChecker(command []string, timeout time.Duration) DraftChecker {
	if len(command) == 0 {
		return nil
	}
	return hookChecker{r: hook.Runner{Command: append([]string(nil), command...), Timeout: timeout}}
}

type hookChecker struct{ r hook.Runner }

func (h hookChecker) CheckDraft(ctx context.Context, in DraftCheckInput) (DraftCheckResult, error) {
	hi := hook.Input{EML: in.EML}
	if in.Message != nil {
		hi.BodyHTML, hi.BodyText = in.Message.BodyHTML, in.Message.BodyText
	}
	meta, err := HookMeta(in)
	if err != nil {
		return DraftCheckResult{}, err
	}
	hi.Meta = meta
	res, err := h.r.Run(ctx, hi)
	if err != nil {
		return DraftCheckResult{}, err
	}
	return DraftCheckResult{Result: res.Status, Output: res.Output, Truncated: res.Truncated}, nil
}

// hookMetaDoc ist {meta_json} (Version 1): Kopffelder der fertigen
// Nachricht, Anhänge (Name, Größe, Typ), Konto, Identität. Kein Geheimnis,
// kein Text (der steht in {body_text}/{body_html}/{eml}).
type hookMetaDoc struct {
	Version     int              `json:"version"`
	DraftID     int64            `json:"draft_id"`
	Kind        string           `json:"kind"`
	Account     string           `json:"account"`
	Identity    hookIdentity     `json:"identity"`
	Subject     string           `json:"subject"`
	From        hookAddr         `json:"from"`
	ReplyTo     []hookAddr       `json:"reply_to"`
	To          []hookAddr       `json:"to"`
	Cc          []hookAddr       `json:"cc"`
	Bcc         []hookAddr       `json:"bcc"`
	MessageID   string           `json:"message_id"`
	Date        string           `json:"date"`
	InReplyTo   string           `json:"in_reply_to"`
	References  []string         `json:"references"`
	Attachments []hookAttachment `json:"attachments"`
}

type hookIdentity struct {
	// ID 0 = keine Identität (Absender ist die Kontoadresse).
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type hookAddr struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type hookAttachment struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// HookMeta baut {meta_json}: eingerückt, ein Schlüssel der obersten Ebene
// je Zeile (so findet auch ein einfaches Skript ohne JSON-Werkzeug den
// Betreff, deploy/hooks/check-example.sh), HTML-Zeichen nicht maskiert.
func HookMeta(in DraftCheckInput) ([]byte, error) {
	d := in.Draft
	doc := hookMetaDoc{Version: 1, DraftID: d.ID, Kind: d.Kind, Account: in.Account,
		Identity: hookIdentity{ID: d.IdentityID, Name: d.FromName, Email: d.FromEmail},
		Subject:  d.Subject, From: hookAddr{Name: d.FromName, Address: d.FromEmail}, MessageID: d.MessageIDHeader,
		ReplyTo: []hookAddr{}, To: []hookAddr{}, Cc: []hookAddr{}, Bcc: []hookAddr{}, References: []string{}, Attachments: []hookAttachment{}}
	if m := in.Message; m != nil {
		doc.Subject, doc.From, doc.MessageID, doc.InReplyTo = m.Subject, addr(m.From), m.MessageID, m.InReplyTo
		if !m.Date.IsZero() {
			doc.Date = m.Date.Format(time.RFC3339)
		}
		doc.ReplyTo, doc.To, doc.Cc, doc.Bcc = addrs(m.ReplyTo), addrs(m.To), addrs(m.Cc), addrs(m.Bcc)
		doc.References = append(doc.References, m.References...)
		for _, a := range m.Attachments {
			doc.Attachments = append(doc.Attachments, hookAttachment{Name: a.Filename, Size: a.Size, ContentType: a.ContentType})
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addr(a compose.Address) hookAddr { return hookAddr{Name: a.Name, Address: a.Address} }

func addrs(as []compose.Address) []hookAddr {
	out := make([]hookAddr, 0, len(as))
	for _, a := range as {
		out = append(out, addr(a))
	}
	return out
}
