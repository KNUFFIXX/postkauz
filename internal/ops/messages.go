package ops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// MessageRepo liefert gespiegelte Nachrichten, Threads und Anhänge.
type MessageRepo interface {
	MessageByID(ctx context.Context, id int64) (store.Message, error)
	MessageIDsByHeader(ctx context.Context, accountID int64, messageID string) ([]int64, error)
	MessageBody(ctx context.Context, id int64) (store.Body, error)
	ThreadByID(ctx context.Context, id int64) (store.Thread, []store.ThreadMessage, error)
	AttachmentsByMessage(ctx context.Context, messageID int64) ([]store.Attachment, error)
	AttachmentByID(ctx context.Context, id int64) (store.Attachment, string, error)
}

// BlobReader liest Rohmails aus dem Blob-Store.
type BlobReader interface {
	Get(sha256 string) (io.ReadCloser, error)
}

// maxAmbiguousIDs begrenzt die IDs in der Meldung einer mehrdeutigen
// Message-ID.
const maxAmbiguousIDs = 20

// messageByHeader löst eine Message-ID (mit oder ohne spitze Klammern) im
// Konto zur lokalen ID auf. Führen mehrere Nachrichten dieselbe Message-ID
// (Gmail: Fassung des Absenders und empfangene Fassung, Entscheid C2 rc6;
// oder ein Absender verwendet sie wieder), ist das ein Eingabefehler mit den
// lokalen IDs statt still der ersten Nachricht (A03 Nachtrag 26.10.2026):
// Ein Agent soll nicht auf die falsche Fassung antworten.
func messageByHeader(ctx context.Context, msgs interface {
	MessageIDsByHeader(ctx context.Context, accountID int64, messageID string) ([]int64, error)
}, accountID int64, messageID string) (int64, error) {
	ids, err := msgs.MessageIDsByHeader(ctx, accountID, strings.Trim(strings.TrimSpace(messageID), "<>"))
	switch {
	case err != nil:
		return 0, err
	case len(ids) == 0:
		return 0, store.ErrMessageNotFound
	case len(ids) == 1:
		return ids[0], nil
	}
	list := make([]string, 0, min(len(ids), maxAmbiguousIDs))
	for _, id := range ids[:min(len(ids), maxAmbiguousIDs)] {
		list = append(list, itoa(id))
	}
	more := ""
	if len(ids) > maxAmbiguousIDs {
		more = ", …"
	}
	return 0, inputErr("ambiguous message_id: %d messages share it (ids %s%s); give the local id instead", len(ids), strings.Join(list, ", "), more)
}

// MessageLocation ist eine Ordnerkopie in messages.get.
type MessageLocation struct {
	Mailbox string `json:"mailbox"`
	Role    string `json:"role"`
	UID     uint32 `json:"uid"`
}

// MessageOutput ist die Ausgabe von messages.get: Metadaten, bereinigter
// Text (oder Kopfzeilen), Links, Anhänge, Absenderprüfung, in der
// Untrusted-Hülle (seit S1.8).
type MessageOutput struct {
	Envelope
	ID             int64             `json:"id"`
	Account        string            `json:"account"`
	MessageID      string            `json:"message_id,omitempty"`
	ThreadID       int64             `json:"thread_id,omitempty"`
	Subject        string            `json:"subject"`
	From           string            `json:"from"`
	To             json.RawMessage   `json:"to"`
	Cc             json.RawMessage   `json:"cc"`
	Date           string            `json:"date,omitempty"` // Date-Kopfzeile, sonst INTERNALDATE (RFC 3339)
	InternalDate   string            `json:"internal_date,omitempty"`
	Size           int64             `json:"size"`
	Flags          []string          `json:"flags"` // seen, flagged, answered, draft
	Keywords       []string          `json:"keywords"`
	HasAttachments bool              `json:"has_attachments"`
	DeletedRemote  bool              `json:"deleted_remote"`
	Mailboxes      []MessageLocation `json:"mailboxes"`
	Headers        string            `json:"headers,omitempty"` // nur mit headers=true
	Text           *string           `json:"text,omitempty"`    // nur ohne headers
	// TextSource: plain (Klartextteil) oder html (Text aus dem HTML-Teil,
	// wenn es keinen Klartext gibt); mit strip_quotes ohne Zitate.
	TextSource string `json:"text_source,omitempty"`
	// HiddenTextRemoved: Das HTML enthielt versteckten Text, der entfernt
	// wurde (Prompt-Injection-Indiz; docs/sicherheit.md).
	HiddenTextRemoved bool `json:"hidden_text_removed"`
	// AlternativeMismatch: Klartext und HTML-Teil (multipart/alternative)
	// weichen wesentlich ab; der Text hier (Klartext) ist nicht das, was ein
	// Mensch im Mailclient sieht (Prompt-Injection-Indiz).
	AlternativeMismatch bool              `json:"alternative_mismatch"`
	Links               []json.RawMessage `json:"links,omitempty"`
	// Truncated: Der Text wurde auf max_chars gekürzt.
	Truncated   bool               `json:"truncated"`
	Auth        AuthOutput         `json:"auth"`
	Attachments []AttachmentOutput `json:"attachments"`
}

// maxHeaderBytes begrenzt die ausgegebenen Kopfzeilen.
const maxHeaderBytes = 256 << 10

// MessagesGet zeigt eine gespiegelte Nachricht: Metadaten, Flags,
// Absenderprüfung und Anhänge, dazu entweder den Text oder (headers) die
// Kopfzeilen der Rohmail. Lesend (R0).
func MessagesGet(accounts AccountRepo, msgs MessageRepo, blobs BlobReader) Operation {
	return Operation{
		Name:       "messages.get",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show a mirrored message: metadata, flags, sender checks, attachments and its cleaned text (or raw headers).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "integer", "minimum": 1},
    "message_id": {"type": "string"},
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "headers": {"type": "boolean", "description": "raw header lines instead of the text"},
    "strip_quotes": {"type": "boolean", "description": "only the new text of a reply (quoted parts removed)"},
    "max_chars": {"type": "integer", "minimum": 1, "description": "cut the text after this many characters (truncated=true)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + envelopeProps + `,
    "id": {"type": "integer"}, "account": {"type": "string"}, "message_id": {"type": "string"},
    "subject": {"type": "string"}, "from": {"type": "string"},
    "to": {"type": "array"}, "cc": {"type": "array"},
    "date": {"type": "string", "format": "date-time"}, "internal_date": {"type": "string", "format": "date-time"},
    "size": {"type": "integer"},
    "flags": {"type": "array", "items": {"type": "string", "enum": ["seen", "flagged", "answered", "draft"]}},
    "keywords": {"type": "array", "items": {"type": "string"}},
    "has_attachments": {"type": "boolean"}, "deleted_remote": {"type": "boolean"},
    "mailboxes": {"type": "array", "items": {"type": "object", "properties": {
      "mailbox": {"type": "string"}, "role": {"type": "string"}, "uid": {"type": "integer"}},
      "required": ["mailbox", "role", "uid"]}},
    "headers": {"type": "string"}, "text": {"type": "string"},
    "thread_id": {"type": "integer"},
    "text_source": {"type": "string", "enum": ["plain", "html", "plain_unquoted", "html_unquoted"]},
    "hidden_text_removed": {"type": "boolean"},
    "alternative_mismatch": {"type": "boolean"},
    "links": {"type": "array", "items": {"type": "object", "properties": {
      "text": {"type": "string"}, "url": {"type": "string"}, "host": {"type": "string"},
      "host_unicode": {"type": "string"}, "warnings": {"type": "array", "items": {"type": "string"}}},
      "required": ["text", "url", "host"]}},
    "truncated": {"type": "boolean"},
    "auth": ` + authSchema + `,
    "attachments": {"type": "array", "items": ` + attachmentSchema + `}
  },
  "required": ["untrusted", "source", "notice", "id", "account", "subject", "from", "to", "cc", "size", "flags", "keywords",
    "has_attachments", "deleted_remote", "mailboxes", "hidden_text_removed", "alternative_mismatch", "truncated", "auth",
    "attachments"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				ID int64 `json:"id"`
			}
			params, _ := redactAccountRef(raw)
			if json.Unmarshal(raw, &in) == nil && in.ID > 0 {
				return params, []string{"message:" + itoa(in.ID)}
			}
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ID          int64  `json:"id,omitempty"`
				MessageID   string `json:"message_id,omitempty"`
				Account     string `json:"account,omitempty"`
				Headers     bool   `json:"headers,omitempty"`
				StripQuotes bool   `json:"strip_quotes,omitempty"`
				MaxChars    int    `json:"max_chars,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if (in.ID == 0) == (in.MessageID == "") {
				return nil, inputErr("give either id or message_id")
			}
			id := in.ID
			if id == 0 {
				a, err := ResolveAccount(ctx, accounts, in.Account)
				if err != nil {
					return nil, err
				}
				if id, err = messageByHeader(ctx, msgs, a.ID, in.MessageID); err != nil {
					return nil, err
				}
			}
			m, err := msgs.MessageByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if in.Account != "" && m.Account != in.Account {
				return nil, store.ErrMessageNotFound
			}
			out := MessageOutput{Envelope: emailEnvelope(), Auth: parseAuthResults(m.AuthResults), Attachments: []AttachmentOutput{},
				ID: m.ID, Account: m.Account, MessageID: m.MessageID, ThreadID: m.ThreadID, Subject: m.Subject, From: m.From,
				To: arr(m.To), Cc: arr(m.Cc), Size: m.Size, Keywords: m.Keywords, HasAttachments: m.HasAttachments,
				DeletedRemote: !m.DeletedRemoteAt.IsZero(), Flags: []string{}, Mailboxes: []MessageLocation{}}
			if out.Keywords == nil {
				out.Keywords = []string{}
			}
			for _, f := range []struct {
				on   bool
				name string
			}{{m.Seen, "seen"}, {m.Flagged, "flagged"}, {m.Answered, "answered"}, {m.Draft, "draft"}} {
				if f.on {
					out.Flags = append(out.Flags, f.name)
				}
			}
			if !m.DateHeader.IsZero() {
				out.Date = m.DateHeader.Format(time.RFC3339)
			} else if !m.InternalDate.IsZero() {
				out.Date = m.InternalDate.Format(time.RFC3339)
			}
			if !m.InternalDate.IsZero() {
				out.InternalDate = m.InternalDate.Format(time.RFC3339)
			}
			for _, l := range m.Locations {
				out.Mailboxes = append(out.Mailboxes, MessageLocation{Mailbox: l.Mailbox, Role: l.Role, UID: l.UID})
			}
			if in.Headers {
				if m.BlobSHA256 != "" {
					h, err := readHeaders(blobs, m.BlobSHA256)
					if err != nil {
						return nil, err
					}
					out.Headers = h
				}
			}
			body, err := msgs.MessageBody(ctx, m.ID)
			if err != nil {
				return nil, err
			}
			out.HiddenTextRemoved, out.AlternativeMismatch = body.HiddenText, body.AlternativeMismatch
			var links []json.RawMessage
			if json.Unmarshal(body.Links, &links) == nil && len(links) > 0 {
				out.Links = links
			}
			if !in.Headers {
				text, src := body.TextPlain, "plain"
				if strings.TrimSpace(text) == "" && strings.TrimSpace(body.TextHTML) != "" {
					text, src = body.TextHTML, "html"
				}
				if in.StripQuotes && body.CleanVersion > 0 {
					text, src = body.TextUnquoted, src+"_unquoted"
				}
				text, out.Truncated = truncateRunes(text, in.MaxChars)
				out.Text, out.TextSource = &text, src
			} else {
				out.Headers, out.Truncated = truncateRunes(out.Headers, in.MaxChars)
			}
			atts, err := msgs.AttachmentsByMessage(ctx, m.ID)
			if err != nil {
				return nil, err
			}
			for _, a := range atts {
				out.Attachments = append(out.Attachments, attachmentOut(a))
			}
			return out, nil
		},
	}
}

func arr(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage("[]")
	}
	return raw
}

func itoa(i int64) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// readHeaders liest den Kopf der Rohmail (bis zur ersten Leerzeile).
func readHeaders(blobs BlobReader, sha string) (string, error) {
	rc, err := blobs.Get(sha)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	br := bufio.NewReader(io.LimitReader(rc, maxHeaderBytes))
	var out bytes.Buffer
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimRight(line, "\r\n")) == 0 && (len(line) > 0 || err != nil) {
			break
		}
		out.Write(bytes.TrimRight(line, "\r\n"))
		out.WriteByte('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
	}
	return strings.ToValidUTF8(out.String(), "�"), nil
}
