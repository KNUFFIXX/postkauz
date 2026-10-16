package ops

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Threads und Anhänge lesend (S1.6, minimal; voll mit S1.8).

// ThreadMessageOutput ist eine Nachricht in threads.get.
type ThreadMessageOutput struct {
	ID             int64    `json:"id"`
	Date           string   `json:"date,omitempty"`
	From           string   `json:"from"`
	Subject        string   `json:"subject"`
	Flags          []string `json:"flags"`
	HasAttachments bool     `json:"has_attachments"`
	DeletedRemote  bool     `json:"deleted_remote"`
	Snippet        string   `json:"snippet"`
	// Text ist der neue Text ohne Zitate (nur mit text=true).
	Text              *string `json:"text,omitempty"`
	HiddenTextRemoved bool    `json:"hidden_text_removed,omitempty"`
}

// ThreadOutput ist die Ausgabe von threads.get.
type ThreadOutput struct {
	ID           int64                 `json:"id"`
	Account      string                `json:"account"`
	Subject      string                `json:"subject"` // Betreff der ersten Nachricht
	MessageCount int                   `json:"message_count"`
	FirstDate    string                `json:"first_date,omitempty"`
	LastDate     string                `json:"last_date,omitempty"`
	Participants []string              `json:"participants"`
	Messages     []ThreadMessageOutput `json:"messages"`
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ThreadsGet zeigt einen Thread mit seinen Nachrichten (nach Datum),
// wahlweise mit dem neuen Text jeder Nachricht ohne Zitate. Lesend (R0).
func ThreadsGet(msgs MessageRepo) Operation {
	return Operation{
		Name:       "threads.get",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show a thread: its messages by date, optionally with each message's new text (quotes removed).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "integer", "minimum": 1, "description": "thread id"},
    "message": {"type": "integer", "minimum": 1, "description": "local message id; shows the thread of this message"},
    "text": {"type": "boolean", "description": "include each message's text without quotes"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "integer"}, "account": {"type": "string"}, "subject": {"type": "string"},
    "message_count": {"type": "integer"}, "first_date": {"type": "string", "format": "date-time"},
    "last_date": {"type": "string", "format": "date-time"},
    "participants": {"type": "array", "items": {"type": "string"}},
    "messages": {"type": "array", "items": {"type": "object", "properties": {
      "id": {"type": "integer"}, "date": {"type": "string", "format": "date-time"}, "from": {"type": "string"},
      "subject": {"type": "string"}, "flags": {"type": "array", "items": {"type": "string"}},
      "has_attachments": {"type": "boolean"}, "deleted_remote": {"type": "boolean"}, "snippet": {"type": "string"},
      "text": {"type": "string"}, "hidden_text_removed": {"type": "boolean"}},
      "required": ["id", "from", "subject", "flags", "has_attachments", "deleted_remote", "snippet"]}}
  },
  "required": ["id", "account", "subject", "message_count", "participants", "messages"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				ID      int64 `json:"id"`
				Message int64 `json:"message"`
			}
			params, _ := redactAccountRef(raw)
			_ = json.Unmarshal(raw, &in)
			switch {
			case in.ID > 0:
				return params, []string{"thread:" + itoa(in.ID)}
			case in.Message > 0:
				return params, []string{"message:" + itoa(in.Message)}
			}
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ID      int64 `json:"id,omitempty"`
				Message int64 `json:"message,omitempty"`
				Text    bool  `json:"text,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if (in.ID == 0) == (in.Message == 0) {
				return nil, inputErr("give either a thread id or a message id")
			}
			id := in.ID
			if id == 0 {
				m, err := msgs.MessageByID(ctx, in.Message)
				if err != nil {
					return nil, err
				}
				if m.ThreadID == 0 {
					return nil, store.ErrThreadNotFound
				}
				id = m.ThreadID
			}
			th, list, err := msgs.ThreadByID(ctx, id)
			if err != nil {
				return nil, err
			}
			out := ThreadOutput{ID: th.ID, Account: th.Account, MessageCount: th.MessageCount, FirstDate: rfc3339(th.FirstDate),
				LastDate: rfc3339(th.LastDate), Participants: th.Participants, Messages: []ThreadMessageOutput{}}
			if out.Participants == nil {
				out.Participants = []string{}
			}
			for i, m := range list {
				if i == 0 {
					out.Subject = m.Subject
				}
				tm := ThreadMessageOutput{ID: m.ID, Date: rfc3339(m.Date), From: m.From, Subject: m.Subject, Flags: []string{},
					HasAttachments: m.HasAttachments, DeletedRemote: m.DeletedRemote, Snippet: m.Snippet}
				if m.Seen {
					tm.Flags = append(tm.Flags, "seen")
				}
				if m.Flagged {
					tm.Flags = append(tm.Flags, "flagged")
				}
				if in.Text {
					b, err := msgs.MessageBody(ctx, m.ID)
					if err != nil {
						return nil, err
					}
					text := b.TextUnquoted
					if b.CleanVersion == 0 || strings.TrimSpace(text) == "" {
						text = b.TextPlain
						if strings.TrimSpace(text) == "" {
							text = b.TextHTML
						}
					}
					tm.Text, tm.HiddenTextRemoved = &text, b.HiddenText
				}
				out.Messages = append(out.Messages, tm)
			}
			return out, nil
		},
	}
}

// AttachmentOutput ist ein Anhang in attachments.list.
type AttachmentOutput struct {
	ID             int64  `json:"id"`
	PartID         string `json:"part_id"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"` // erkannt (Magic Bytes)
	DeclaredType   string `json:"declared_type"`
	Size           int64  `json:"size"`
	Inline         bool   `json:"inline"`
	Truncated      bool   `json:"truncated"`
	TypeMismatch   bool   `json:"type_mismatch"`
	MismatchReason string `json:"mismatch_reason,omitempty"`
	Executable     bool   `json:"executable"`
	ExtractStatus  string `json:"extract_status"`
	ExtractError   string `json:"extract_error,omitempty"`
	HiddenText     bool   `json:"hidden_text_removed"`
}

// AttachmentsListOutput ist die Ausgabe von attachments.list.
type AttachmentsListOutput struct {
	MessageID   int64              `json:"message_id"`
	Attachments []AttachmentOutput `json:"attachments"`
}

func attachmentOut(a store.Attachment) AttachmentOutput {
	return AttachmentOutput{ID: a.ID, PartID: a.PartID, Filename: a.Filename, ContentType: a.ContentType,
		DeclaredType: a.DeclaredType, Size: a.Size, Inline: a.Inline, Truncated: a.Truncated, TypeMismatch: a.TypeMismatch,
		MismatchReason: a.MismatchReason, Executable: a.Executable, ExtractStatus: a.ExtractStatus, ExtractError: a.ExtractError,
		HiddenText: a.HiddenText}
}

const attachmentSchema = `{"type": "object", "properties": {
      "id": {"type": "integer"}, "part_id": {"type": "string"}, "filename": {"type": "string"},
      "content_type": {"type": "string"}, "declared_type": {"type": "string"}, "size": {"type": "integer"},
      "inline": {"type": "boolean"}, "truncated": {"type": "boolean"}, "type_mismatch": {"type": "boolean"},
      "mismatch_reason": {"type": "string"}, "executable": {"type": "boolean"},
      "extract_status": {"type": "string", "enum": ["pending", "ok", "failed", "skipped"]},
      "extract_error": {"type": "string"}, "hidden_text_removed": {"type": "boolean"}},
      "required": ["id", "part_id", "filename", "content_type", "declared_type", "size", "inline", "truncated",
        "type_mismatch", "executable", "extract_status", "hidden_text_removed"]}`

// AttachmentsList listet die Anhänge einer Nachricht mit erkanntem Typ,
// Widerspruch und Stand der Textextraktion. Lesend (R0).
func AttachmentsList(msgs MessageRepo) Operation {
	return Operation{
		Name:       "attachments.list",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "List the attachments of a message with detected type, type mismatch and text extraction status.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"message": {"type": "integer", "minimum": 1, "description": "local message id"}},
  "required": ["message"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "message_id": {"type": "integer"},
    "attachments": {"type": "array", "items": ` + attachmentSchema + `}
  },
  "required": ["message_id", "attachments"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				Message int64 `json:"message"`
			}
			params, _ := redactAccountRef(raw)
			if json.Unmarshal(raw, &in) == nil && in.Message > 0 {
				return params, []string{"message:" + itoa(in.Message)}
			}
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Message int64 `json:"message"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.Message <= 0 {
				return nil, inputErr("give a message id")
			}
			if _, err := msgs.MessageByID(ctx, in.Message); err != nil {
				return nil, err
			}
			list, err := msgs.AttachmentsByMessage(ctx, in.Message)
			if err != nil {
				return nil, err
			}
			out := AttachmentsListOutput{MessageID: in.Message, Attachments: []AttachmentOutput{}}
			for _, a := range list {
				out.Attachments = append(out.Attachments, attachmentOut(a))
			}
			return out, nil
		},
	}
}

// AttachmentTextOutput ist die Ausgabe von attachments.text.
type AttachmentTextOutput struct {
	AttachmentOutput
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	// ExtractedNow: Der Text stand noch nicht in der Datenbank und wurde
	// für diese Anzeige extrahiert (nicht gespeichert; das tut der Sync).
	ExtractedNow bool `json:"extracted_now"`
	Truncated    bool `json:"text_truncated,omitempty"`
}

// AttachmentsText zeigt den extrahierten Text eines Anhangs. Steht die
// Extraktion noch aus, extrahiert sie jetzt (Subprozess mit Grenzen) ohne
// zu speichern; lesend (R0).
func AttachmentsText(msgs MessageRepo, blobs BlobReader, ex *extract.Extractor) Operation {
	return Operation{
		Name:       "attachments.text",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show the extracted text of an attachment (extracts it now if the sync has not yet).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"id": {"type": "integer", "minimum": 1, "description": "attachment id from attachments.list"}},
  "required": ["id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "integer"}, "message_id": {"type": "integer"}, "part_id": {"type": "string"},
    "filename": {"type": "string"}, "content_type": {"type": "string"}, "declared_type": {"type": "string"},
    "size": {"type": "integer"}, "inline": {"type": "boolean"}, "truncated": {"type": "boolean"},
    "type_mismatch": {"type": "boolean"}, "mismatch_reason": {"type": "string"}, "executable": {"type": "boolean"},
    "extract_status": {"type": "string", "enum": ["pending", "ok", "failed", "skipped"]},
    "extract_error": {"type": "string"}, "hidden_text_removed": {"type": "boolean"},
    "text": {"type": "string"}, "extracted_now": {"type": "boolean"}, "text_truncated": {"type": "boolean"}
  },
  "required": ["id", "message_id", "filename", "content_type", "extract_status", "text", "extracted_now"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				ID int64 `json:"id"`
			}
			params, _ := redactAccountRef(raw)
			if json.Unmarshal(raw, &in) == nil && in.ID > 0 {
				return params, []string{"attachment:" + itoa(in.ID)}
			}
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ID int64 `json:"id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.ID <= 0 {
				return nil, inputErr("give an attachment id")
			}
			a, text, err := msgs.AttachmentByID(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			out := AttachmentTextOutput{AttachmentOutput: attachmentOut(a), MessageID: a.MessageID, Text: text}
			if a.ExtractStatus != extract.StatusOK {
				out.Text = ""
			}
			if a.ExtractStatus == "pending" && ex != nil && a.SHA256 != "" {
				rc, err := blobs.Get(a.SHA256)
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				res := ex.Extract(ctx, extract.KindOf(a.ContentType), rc, a.Size)
				out.ExtractStatus, out.ExtractError, out.Text = res.Status, res.Error, res.Text
				out.ExtractedNow, out.Truncated = true, res.Truncated
				out.HiddenText = out.HiddenText || res.Hidden
			}
			return out, nil
		},
	}
}
