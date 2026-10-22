package ops

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Entwürfe im Register (S2.3, A02 E1–E3, E6, E7, E9–E11). Die Handler
// verbinden drei Teile: internal/compose baut aus den Eingaben die
// Nachricht (Kopfzeilen, Zitat, Signatur, Anhänge), internal/drafts prüft
// Grenzen und Zustandsmaschine und speichert in einer kurzen Transaktion
// (mit Audit-Eintrag des Übergangs und dem Job draft_push), und der Prüf-Hook
// (S2.4; bis dahin „bestanden") prüft vor der Vorlage. Nichts hier berührt
// einen Server: Die Ablage im Entwürfe-Ordner macht der Job draft_push im
// Prozess mit Zugangsdaten (E1, S2.5).
//
// Entwurfsinhalt (Betreff, Adressen, Text, Dateinamen) steht nur in den
// Ausgaben von drafts.create, drafts.update, drafts.get und drafts.list,
// nie in Audit, Log oder Fehlermeldungen (Redact: IDs, Art, Zähler).

// DraftConfig sind die Einstellungen der Entwürfe aus der Konfiguration.
type DraftConfig struct {
	// AttachDir: Dateianhänge nur von hier ([paths] attach_dir, E10).
	AttachDir string
	// MaxAttachmentBytes: Summe der Anhänge ([draft] max_attachment_bytes;
	// 0 = 25 MiB).
	MaxAttachmentBytes int64
	// QuoteLine: Einleitung des Zitats ([draft] quote_line; "" = Standard).
	QuoteLine string
	// Operator: [operator] name; wer freigibt (Entschiedene Frage 20).
	Operator string
	// Now und Rand für Tests (nil = time.Now, crypto/rand).
	Now  func() time.Time
	Rand io.Reader
}

func (c DraftConfig) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c DraftConfig) maxBytes() int64 {
	if c.MaxAttachmentBytes > 0 {
		return c.MaxAttachmentBytes
	}
	return compose.DefaultLimits.MaxAttachmentBytes
}

// BlobStore liest und schreibt Blobs (Anhänge von Entwürfen, E10).
type BlobStore interface {
	BlobReader
	Put(r io.Reader) (blob.Info, error)
}

// DraftChecker ist der Prüf-Hook vor der Vorlage (A02 E4). S2.4 liefert
// die Umsetzung (internal/hook) und hängt sie über Deps.DraftCheck ein;
// ohne ihn (nil) gilt der Entwurf als geprüft („pass"), wie bei leerem
// check_hook.
type DraftChecker interface {
	CheckDraft(ctx context.Context, in DraftCheckInput) (DraftCheckResult, error)
}

// DraftCheckInput ist, was der Hook prüft: der gespeicherte Entwurf, der
// Kontoname und die fertige Nachricht (für {eml}).
type DraftCheckInput struct {
	Draft   store.Draft
	Account string
	EML     []byte
}

// DraftCheckResult ist das Ergebnis des Hooks: pass | fail | timeout |
// error, dazu die ersten 4 KB seiner Ausgabe (nur im Ergebnis, nie im
// Audit; dort steht nur Result).
type DraftCheckResult struct {
	Result string
	Output string
}

// RemoteNotePending ist der ehrliche Hinweis zur Serverkopie (A02 E1): Die
// Kopie legt der Prozess mit Zugangsdaten ab, nicht der Aufruf selbst.
const RemoteNotePending = "the copy in the Drafts folder is queued: it appears in the mail client after the next sync; a running sync --watch pushes it within 10 s"

// remoteNote erklärt den Stand der Serverkopie.
func remoteNote(d store.Draft) string {
	switch d.RemoteState {
	case drafts.RemotePending:
		if drafts.Status(d.Status).Closed() {
			return ""
		}
		return RemoteNotePending
	case drafts.RemotePushed:
		return fmt.Sprintf("in the Drafts folder (uid %d)", d.RemoteUID)
	case drafts.RemoteTakenOver:
		return "taken over in the mail client; change it there"
	case drafts.RemoteMissing:
		return "the copy in the Drafts folder is gone; close the draft with draft mark-sent or draft abandon"
	}
	return ""
}

// draftOps hält die Abhängigkeiten der Entwurfsoperationen.
type draftOps struct {
	svc      *drafts.Service
	accounts AccountRepo
	msgs     MessageRepo
	blobs    BlobStore
	cfg      DraftConfig
	check    DraftChecker
}

func newDraftOps(d Deps) *draftOps {
	return &draftOps{svc: d.Drafts, accounts: d.Accounts, msgs: d.Messages, blobs: d.DraftBlobs, cfg: d.Draft, check: d.DraftCheck}
}

// as liefert den Dienst für einen Aufruf: Übergänge mit Akteur des
// Aufrufs auditiert, im Trockenlauf zurückgerollt.
func (o *draftOps) as(call Call, op string, scope policy.Scope) *drafts.Service {
	return o.svc.As(drafts.Who{Actor: call.Actor, KeyID: call.Key.ID, Scope: string(scope), Operation: op}, call.DryRun)
}

// draftErr übersetzt Ablehnungen aus internal/drafts und internal/compose
// in DeniedError (Exit 3, Audit denied mit Grund); widersprüchliche
// Eingaben des Nachrichtenbaus in InputError (Exit 2).
func draftErr(err error) error {
	if err == nil {
		return nil
	}
	var de *drafts.DeniedError
	if errors.As(err, &de) {
		return &DeniedError{Reason: de.Reason, Msg: de.Msg}
	}
	var ce *compose.Error
	if errors.As(err, &ce) {
		msg := ce.Detail
		if ce.Field != "" {
			msg = ce.Field + ": " + msg
		}
		if ce.Reason == compose.ReasonInvalidInput {
			return inputErr("invalid input: %s", msg)
		}
		return &DeniedError{Reason: string(ce.Reason), Msg: msg}
	}
	return err
}

// --- Eingaben -----------------------------------------------------------------

// DraftAttachmentInput nennt einen Anhang: die ID eines Anhangs einer
// gespiegelten Nachricht desselben Kontos (der Blob wird übernommen) oder
// eine Datei unterhalb von attach_dir (E10).
type DraftAttachmentInput struct {
	AttachmentID int64  `json:"attachment_id,omitempty"`
	File         string `json:"file,omitempty"`
	// Filename ersetzt den Namen im Entwurf (optional).
	Filename string `json:"filename,omitempty"`
}

// DraftsCreateInput ist die Eingabe von drafts.create.
type DraftsCreateInput struct {
	Account string `json:"account,omitempty"`
	Kind    string `json:"kind"`
	// InReplyTo ist die lokale ID der Ausgangsnachricht (reply, reply_all,
	// forward), InReplyToMessageID statt dessen ihre Message-ID.
	InReplyTo          int64                  `json:"in_reply_to,omitempty"`
	InReplyToMessageID string                 `json:"in_reply_to_message_id,omitempty"`
	Identity           string                 `json:"identity,omitempty"`
	To                 []string               `json:"to,omitempty"`
	Cc                 []string               `json:"cc,omitempty"`
	Bcc                []string               `json:"bcc,omitempty"`
	Subject            string                 `json:"subject,omitempty"`
	BodyMarkdown       string                 `json:"body_markdown,omitempty"`
	BodyHTML           string                 `json:"body_html,omitempty"`
	Attachments        []DraftAttachmentInput `json:"attachments,omitempty"`
	Quote              *bool                  `json:"quote,omitempty"`
	Signature          *bool                  `json:"signature,omitempty"`
	IncludeAttachments *bool                  `json:"include_attachments,omitempty"`
}

// DraftsUpdateInput ist die Eingabe von drafts.update. Ein fehlendes Feld
// bleibt; ein genanntes ersetzt den gespeicherten Wert vollständig (to:
// die ganze Liste, attachments: alle Anhänge, body_markdown bzw.
// body_html: den Text).
type DraftsUpdateInput struct {
	DraftID      int64                   `json:"draft_id"`
	To           *[]string               `json:"to,omitempty"`
	Cc           *[]string               `json:"cc,omitempty"`
	Bcc          *[]string               `json:"bcc,omitempty"`
	Subject      *string                 `json:"subject,omitempty"`
	BodyMarkdown *string                 `json:"body_markdown,omitempty"`
	BodyHTML     *string                 `json:"body_html,omitempty"`
	Attachments  *[]DraftAttachmentInput `json:"attachments,omitempty"`
	Quote        *bool                   `json:"quote,omitempty"`
	Signature    *bool                   `json:"signature,omitempty"`
}

// draftIDInput ist die Eingabe der Operationen mit nur einem Entwurf.
type draftIDInput struct {
	DraftID int64 `json:"draft_id"`
}

// --- Ausgaben -----------------------------------------------------------------

// DraftPreview ist der Kopf eines Entwurfs und der Anfang seines Texts
// (erste PreviewChars Zeichen von text/plain).
type DraftPreview struct {
	From          string   `json:"from"`
	To            []string `json:"to"`
	Cc            []string `json:"cc"`
	Bcc           []string `json:"bcc"`
	Subject       string   `json:"subject"`
	InReplyTo     string   `json:"in_reply_to,omitempty"`
	Text          string   `json:"text"`
	TextTruncated bool     `json:"text_truncated"`
}

// PreviewChars ist die Länge des Textanfangs in der Vorschau (A02 S2.3).
const PreviewChars = 500

// DraftAttachmentOutput beschreibt einen Anhang eines Entwurfs.
type DraftAttachmentOutput struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	// Warning: Typ-Widerspruch oder ausführbarer Inhalt (Entscheid 39:
	// erlaubt, gesendet wird der erkannte Typ).
	Warning string `json:"warning,omitempty"`
}

// DraftsWriteOutput ist die Ausgabe von drafts.create und drafts.update.
type DraftsWriteOutput struct {
	DraftID        int64  `json:"draft_id,omitempty"` // fehlt im Trockenlauf von create
	Account        string `json:"account"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status,omitempty"`
	// Changed: neu angelegt bzw. Inhalt geändert (false: Änderung ohne Wirkung).
	Changed            bool                    `json:"changed"`
	MessageID          string                  `json:"message_id,omitempty"`
	OriginalID         int64                   `json:"original_id,omitempty"`
	Preview            DraftPreview            `json:"preview"`
	Attachments        []DraftAttachmentOutput `json:"attachments"`
	Warnings           []string                `json:"warnings"`
	Notes              []string                `json:"notes"`
	Remote             string                  `json:"remote"`
	RemoteNote         string                  `json:"remote_note,omitempty"`
	PushJobID          int64                   `json:"push_job_id,omitempty"`
	ApprovalSuperseded int64                   `json:"approval_superseded,omitempty"`
	PreviewHash        string                  `json:"preview_hash"`
	DryRun             bool                    `json:"dry_run"`
}

const draftAttachmentsSchema = `{"type": "array", "items": {"type": "object", "properties": {
      "filename": {"type": "string"}, "content_type": {"type": "string"}, "size": {"type": "integer"},
      "sha256": {"type": "string"}, "warning": {"type": "string"}},
      "required": ["filename", "content_type", "size", "sha256"]}}`

const draftPreviewSchema = `{"type": "object", "properties": {
      "from": {"type": "string"}, "to": {"type": "array", "items": {"type": "string"}},
      "cc": {"type": "array", "items": {"type": "string"}}, "bcc": {"type": "array", "items": {"type": "string"}},
      "subject": {"type": "string"}, "in_reply_to": {"type": "string"}, "text": {"type": "string"},
      "text_truncated": {"type": "boolean"}},
      "required": ["from", "to", "cc", "bcc", "subject", "text", "text_truncated"]}`

const draftStatusEnum = `["draft", "pending_approval", "ready", "sent_by_human", "abandoned"]`

const draftsWriteOutputSchema = `{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer"}, "account": {"type": "string"},
    "kind": {"type": "string", "enum": ["new", "reply", "reply_all", "forward"]},
    "status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "previous_status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "changed": {"type": "boolean"}, "message_id": {"type": "string"}, "original_id": {"type": "integer"},
    "preview": ` + draftPreviewSchema + `,
    "attachments": ` + draftAttachmentsSchema + `,
    "warnings": {"type": "array", "items": {"type": "string"}},
    "notes": {"type": "array", "items": {"type": "string"}},
    "remote": {"type": "string", "enum": ["pending", "pushed", "taken_over", "missing"]},
    "remote_note": {"type": "string"}, "push_job_id": {"type": "integer"},
    "approval_superseded": {"type": "integer"}, "preview_hash": {"type": "string"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "kind", "status", "changed", "preview", "attachments", "warnings", "notes", "remote", "preview_hash", "dry_run"]
}`

// draftAttachmentInputSchema ist das Schema eines Anhangs in der Eingabe.
const draftAttachmentInputSchema = `{"type": "array", "maxItems": 50, "items": {"type": "object", "properties": {
      "attachment_id": {"type": "integer", "minimum": 1, "description": "id of an attachment of a mirrored message of the same account"},
      "file": {"type": "string", "minLength": 1, "description": "file below attach_dir (relative or absolute)"},
      "filename": {"type": "string", "description": "name in the draft (default: the original name)"}},
      "additionalProperties": false}}`

const addrListSchema = `{"type": "array", "maxItems": 50, "items": {"type": "string", "minLength": 1}}`

// --- Hilfen -------------------------------------------------------------------

func formatAddr(a store.Addr) string {
	if a.Name == "" {
		return a.Address
	}
	return a.Name + " <" + a.Address + ">"
}

func formatAddrs(l []store.Addr) []string {
	out := make([]string, 0, len(l))
	for _, a := range l {
		out = append(out, formatAddr(a))
	}
	return out
}

func toStoreAddrs(l []compose.Address) []store.Addr {
	out := make([]store.Addr, 0, len(l))
	for _, a := range l {
		out = append(out, store.Addr{Name: a.Name, Address: a.Address})
	}
	return out
}

// attachmentWarning formuliert Entscheid 39 (ohne Inhalt außer dem
// Dateinamen, der nur in Ausgaben steht).
func attachmentWarning(a store.DraftAttachment) string {
	switch {
	case a.Executable:
		return fmt.Sprintf("attachment %q is a program or script (%s); mail clients may block it", a.Filename, a.ContentType)
	case a.Mismatch:
		return fmt.Sprintf("attachment %q: its name or declared type does not match the content; it is sent as %s", a.Filename, a.ContentType)
	}
	return ""
}

func attachmentOutputs(atts []store.DraftAttachment) ([]DraftAttachmentOutput, []string) {
	out := []DraftAttachmentOutput{}
	warn := []string{}
	for _, a := range atts {
		w := attachmentWarning(a)
		out = append(out, DraftAttachmentOutput{Filename: a.Filename, ContentType: a.ContentType, Size: a.Size, SHA256: a.SHA256, Warning: w})
		if w != "" {
			warn = append(warn, w)
		}
	}
	return out, warn
}

// composeNotes übersetzt die Hinweise des Nachrichtenbaus.
var composeNotes = map[string]string{
	compose.NoteNoIdentity:       drafts.NoteNoIdentity,
	compose.NoteSubjectTruncated: "the derived subject was cut to 500 characters",
	compose.NoteContentRemoved:   "the HTML cleaner removed something (scripts, forms, remote images or links other than https/mailto)",
	compose.NoteRecipientDropped: "an invalid address of the original message was left out",
	compose.NoteQuoteTruncated:   "the quoted or forwarded text of the original message was shortened to 200000 characters",
}

func addNote(notes []string, n string) []string {
	if n == "" || slices.Contains(notes, n) {
		return notes
	}
	return append(notes, n)
}

// headersOf sind die Kopfzeilen, die der Entwurf speichert (headers_json):
// In-Reply-To und References einer Antwort.
func headersOf(m *compose.Message) string {
	h := map[string]string{}
	if m.InReplyTo != "" {
		h[drafts.HeaderInReplyTo] = "<" + m.InReplyTo + ">"
	}
	if len(m.References) > 0 {
		refs := make([]string, len(m.References))
		for i, r := range m.References {
			refs[i] = "<" + r + ">"
		}
		h["References"] = strings.Join(refs, " ")
	}
	b, _ := json.Marshal(h)
	return string(b)
}

// composeOptions ist der Inhalt von drafts.compose_options.
type composeOptions struct {
	Quote     bool `json:"quote"`
	Signature bool `json:"signature"`
}

func parseOptions(s string) composeOptions {
	o := composeOptions{Quote: true, Signature: true}
	if s != "" {
		_ = json.Unmarshal([]byte(s), &o)
	}
	return o
}

func (o composeOptions) String() string {
	b, _ := json.Marshal(o)
	return string(b)
}

// accountName liefert den Namen eines Kontos per ID.
func (o *draftOps) accountByID(ctx context.Context, id int64) (store.Account, error) {
	list, err := o.accounts.ListAccounts(ctx)
	if err != nil {
		return store.Account{}, err
	}
	for _, a := range list {
		if a.ID == id {
			return a.Account, nil
		}
	}
	return store.Account{}, fmt.Errorf("%w: id %d", store.ErrAccountNotFound, id)
}

// attachBudget ist das Restbudget der Anhänge eines Aufrufs
// ([draft] max_attachment_bytes): Jeder geladene Anhang zieht seine Größe
// ab, der nächste wird höchstens bis zum Rest gelesen. Vorher las jeder
// Anhang einzeln bis zur vollen Grenze, und erst der Nachrichtenbau prüfte
// die Summe (Gegenprüfung S2.3, K2): 50 Anhänge à 25 MiB lagen dann im
// Speicher, bevor die Ablehnung kam.
type attachBudget struct{ left int64 }

func (o *draftOps) newBudget() *attachBudget { return &attachBudget{left: o.cfg.maxBytes()} }

var errAttachTooLarge = &DeniedError{Reason: string(compose.ReasonTooLarge), Msg: "attachments exceed the limit in total"}

// readBlob liest einen Blob vollständig, höchstens bis zum Restbudget, und
// zieht seine Größe davon ab.
func (o *draftOps) readBlob(sha string, b *attachBudget) ([]byte, error) {
	if b.left < 0 {
		return nil, errAttachTooLarge
	}
	rc, err := o.blobs.Get(sha)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, b.left+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > b.left {
		return nil, errAttachTooLarge
	}
	b.left -= int64(len(data))
	return data, nil
}

// loadFile liest einen Dateianhang unter attach_dir, höchstens bis zum
// Restbudget.
func (o *draftOps) loadFile(field, file string, b *attachBudget) ([]byte, string, error) {
	if b.left <= 0 {
		return nil, "", errAttachTooLarge
	}
	data, name, err := compose.LoadFile(field, o.cfg.AttachDir, file, b.left)
	if err != nil {
		return nil, "", draftErr(err)
	}
	b.left -= int64(len(data))
	return data, name, nil
}

// original lädt die Ausgangsnachricht aus dem Spiegel (lokale ID oder
// Message-ID im Konto acc) und parst ihre Rohmail. withAttachments lädt
// ihre Anhänge (nicht inline) für eine Weiterleitung.
func (o *draftOps) original(ctx context.Context, acc *store.Account, id int64, mid string, withAttachments bool, budget *attachBudget) (store.Message, *compose.Original, []string, error) {
	var notes []string
	if id == 0 {
		if acc == nil {
			return store.Message{}, nil, nil, inputErr("give the account of the original message (or its local id)")
		}
		ids, err := o.msgs.MessageIDsByHeader(ctx, acc.ID, strings.Trim(strings.TrimSpace(mid), "<>"))
		if err != nil {
			return store.Message{}, nil, nil, err
		}
		if len(ids) == 0 {
			return store.Message{}, nil, nil, store.ErrMessageNotFound
		}
		id = ids[0]
	}
	m, err := o.msgs.MessageByID(ctx, id)
	if err != nil {
		return m, nil, nil, err
	}
	if acc != nil && m.AccountID != acc.ID {
		return m, nil, nil, inputErr("message %d belongs to another account", m.ID)
	}
	if m.BlobSHA256 == "" {
		return m, nil, nil, inputErr("message %d has no stored raw message", m.ID)
	}
	rc, err := o.blobs.Get(m.BlobSHA256)
	if err != nil {
		return m, nil, nil, fmt.Errorf("original message %d: %w", m.ID, err)
	}
	pm, err := mimeparse.Parse(rc, mimeparse.Options{InternalDate: m.InternalDate})
	_ = rc.Close()
	if err != nil {
		return m, nil, nil, fmt.Errorf("original message %d: %w", m.ID, err)
	}
	orig := compose.OriginalFrom(pm)
	if withAttachments {
		atts, err := o.msgs.AttachmentsByMessage(ctx, m.ID)
		if err != nil {
			return m, nil, nil, err
		}
		for _, a := range atts {
			if a.Inline {
				continue
			}
			if a.SHA256 == "" || a.Truncated {
				notes = addNote(notes, fmt.Sprintf("attachment %d of the original message is not included (not stored when mirrored)", a.ID))
				continue
			}
			data, err := o.readBlob(a.SHA256, budget)
			if err != nil {
				return m, nil, nil, draftErr(err)
			}
			orig.Attachments = append(orig.Attachments, compose.Attachment{Filename: a.Filename, ContentType: a.DeclaredType, Data: data})
		}
	}
	return m, &orig, notes, nil
}

// loadedAttachment ist ein aufgelöster Anhang der Eingabe.
type loadedAttachment struct {
	att  compose.Attachment
	file bool // aus attach_dir: kommt beim Speichern in den Blob-Store
}

// attachments löst die Anhänge der Eingabe auf: IDs aus dem Spiegel
// (desselben Kontos), Dateien unter attach_dir.
func (o *draftOps) attachments(ctx context.Context, acc store.Account, in []DraftAttachmentInput, budget *attachBudget) ([]loadedAttachment, error) {
	var out []loadedAttachment
	for i, a := range in {
		field := fmt.Sprintf("attachments[%d]", i)
		switch {
		case (a.AttachmentID > 0) == (a.File != ""):
			return nil, inputErr("%s: give either attachment_id or file", field)
		case a.AttachmentID > 0:
			sa, _, err := o.msgs.AttachmentByID(ctx, a.AttachmentID)
			if err != nil {
				return nil, err
			}
			if sa.AccountID != acc.ID {
				return nil, inputErr("%s: attachment %d belongs to another account", field, a.AttachmentID)
			}
			if sa.SHA256 == "" || sa.Truncated {
				return nil, inputErr("%s: attachment %d was not stored when mirrored", field, a.AttachmentID)
			}
			data, err := o.readBlob(sa.SHA256, budget)
			if err != nil {
				return nil, draftErr(err)
			}
			name := a.Filename
			if name == "" {
				name = sa.Filename
			}
			out = append(out, loadedAttachment{att: compose.Attachment{Filename: name, ContentType: sa.DeclaredType, Data: data}})
		default:
			data, name, err := o.loadFile(field, a.File, budget)
			if err != nil {
				return nil, err
			}
			if a.Filename != "" {
				name = a.Filename
			}
			out = append(out, loadedAttachment{att: compose.Attachment{Filename: name, Data: data}, file: true})
		}
	}
	return out, nil
}

// storedAttachments lädt die gespeicherten Anhänge eines Entwurfs als Daten
// (Neubau bei einer Änderung).
func (o *draftOps) storedAttachments(atts []store.DraftAttachment, budget *attachBudget) ([]loadedAttachment, error) {
	var out []loadedAttachment
	for _, a := range atts {
		data, err := o.readBlob(a.SHA256, budget)
		if err != nil {
			return nil, draftErr(err)
		}
		out = append(out, loadedAttachment{att: compose.Attachment{Filename: a.Filename, ContentType: a.Declared, Data: data}})
	}
	return out, nil
}

// putFiles legt Dateianhänge in den Blob-Store (nicht im Trockenlauf).
// Anhänge aus dem Spiegel liegen schon dort.
func (o *draftOps) putFiles(atts []loadedAttachment) error {
	for _, a := range atts {
		if !a.file {
			continue
		}
		if _, err := o.blobs.Put(bytes.NewReader(a.att.Data)); err != nil {
			return fmt.Errorf("store attachment: %w", err)
		}
	}
	return nil
}

// identity wählt die Identität: sel = Adresse oder ID einer Identität des
// Kontos; leer = die Standardidentität (ok = false: keine).
func (o *draftOps) identity(ctx context.Context, accountID int64, sel string) (store.Identity, bool, error) {
	list, err := o.svc.Store.Identities(ctx, accountID)
	if err != nil {
		return store.Identity{}, false, err
	}
	sel = strings.ToLower(strings.TrimSpace(sel))
	for _, i := range list {
		if (sel == "" && i.IsDefault) || (sel != "" && (strings.ToLower(i.Email) == sel || strconv.FormatInt(i.ID, 10) == sel)) {
			return i, true, nil
		}
	}
	if sel != "" {
		return store.Identity{}, false, inputErr("unknown identity for this account; list them with: identity list")
	}
	return store.Identity{}, false, nil
}

// draftIdentity ist der Absender beim Neubau eines gespeicherten Entwurfs:
// Name und Adresse aus der Kopie im Entwurf (eine spätere Änderung der
// Identität ändert keinen Entwurf still), Signatur, Reply-To und Präfixe
// aus der Identität, solange es sie gibt (sonst ohne Signatur).
func (o *draftOps) draftIdentity(ctx context.Context, d store.Draft) (compose.Identity, error) {
	ci := compose.Identity{}
	if d.IdentityID > 0 {
		ident, err := o.svc.Store.Identity(ctx, d.IdentityID)
		switch {
		case err == nil:
			ci = composeIdentity(ident)
		case !drafts.IsNotFound(err):
			return ci, err
		}
	}
	ci.Name, ci.Email = d.FromName, d.FromEmail
	return ci, nil
}

func composeIdentity(i store.Identity) compose.Identity {
	return compose.Identity{Name: i.Name, Email: i.Email, ReplyTo: i.ReplyTo, SignatureText: i.SignatureText,
		SignatureHTML: i.SignatureHTML, ReplyPrefix: i.ReplyPrefix, ForwardPrefix: i.ForwardPrefix}
}

// build ruft den Nachrichtenbau.
func (o *draftOps) build(in compose.Input) (*compose.Message, error) {
	in.QuoteLine = o.cfg.QuoteLine
	in.AttachDir = o.cfg.AttachDir
	in.Limits = compose.Limits{MaxAttachmentBytes: o.cfg.maxBytes()}
	in.Now = o.cfg.now
	in.Rand = o.cfg.Rand
	if in.Rand == nil {
		in.Rand = rand.Reader
	}
	m, err := compose.Build(in)
	return m, draftErr(err)
}

// content bildet die gebaute Nachricht auf den gespeicherten Inhalt ab.
func content(m *compose.Message, md, html string, opts composeOptions) drafts.Content {
	c := drafts.Content{To: toStoreAddrs(m.To), Cc: toStoreAddrs(m.Cc), Bcc: toStoreAddrs(m.Bcc), Subject: m.Subject,
		BodyMarkdown: md, BodyText: m.BodyText, BodyHTML: html, HeadersJSON: headersOf(m), ComposeOptions: opts.String()}
	for _, a := range m.Attachments {
		c.Attachments = append(c.Attachments, store.DraftAttachment{SHA256: a.SHA256, Filename: a.Filename, ContentType: a.ContentType,
			Size: a.Size, Declared: a.Declared, Mismatch: a.Mismatch, Executable: a.Executable})
	}
	return c
}

func preview(d store.Draft) DraftPreview {
	text, cut := truncateRunes(d.BodyText, PreviewChars)
	p := DraftPreview{From: formatAddr(store.Addr{Name: d.FromName, Address: d.FromEmail}), To: formatAddrs(d.To), Cc: formatAddrs(d.Cc),
		Bcc: formatAddrs(d.Bcc), Subject: d.Subject, Text: text, TextTruncated: cut}
	var h map[string]any
	if json.Unmarshal([]byte(d.HeadersJSON), &h) == nil {
		if v, ok := h[drafts.HeaderInReplyTo].(string); ok {
			p.InReplyTo = v
		}
	}
	return p
}

func (o *draftOps) writeOutput(acc string, res drafts.Result, notes []string, dry bool) DraftsWriteOutput {
	d := res.Draft
	out := DraftsWriteOutput{DraftID: d.ID, Account: acc, Kind: d.Kind, Status: string(res.To), Changed: res.Changed,
		MessageID: d.MessageIDHeader, OriginalID: d.InReplyToMsg, Preview: preview(d), Notes: []string{}, Remote: d.RemoteState,
		RemoteNote: remoteNote(d), PushJobID: res.PushJobID, PreviewHash: d.PreviewHash, DryRun: dry}
	if res.From != "" {
		out.PreviousStatus = string(res.From)
	}
	if res.Approval != nil && res.Approval.Decision == drafts.DecisionSuperseded {
		out.ApprovalSuperseded = res.Approval.ID
	}
	out.Attachments, out.Warnings = attachmentOutputs(d.Attachments)
	for _, n := range append(res.Notes, notes...) {
		out.Notes = addNote(out.Notes, n)
	}
	if dry {
		// Im Trockenlauf wird nichts gespeichert: keine ID, keine
		// Message-ID (sie entsteht beim Anlegen), kein Job.
		if res.From == "" {
			out.DraftID, out.MessageID = 0, ""
		}
		out.PushJobID = 0
		if out.Changed {
			out.RemoteNote = "dry run: nothing was saved or queued"
		}
	}
	return out
}

// --- drafts.create -------------------------------------------------------------

// DraftsCreate legt einen Entwurf an (R2: lokal, dazu der Job draft_push,
// der die Kopie per IMAP APPEND in den Entwürfe-Ordner legt, E1). Mit
// Trockenlauf: nur die Vorschau (Kopf und die ersten 500 Zeichen), nichts
// gespeichert, nichts eingereiht.
func DraftsCreate(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:    "drafts.create",
		Scope:   policy.ScopeMailDraft,
		Risk:    policy.R2,
		Summary: "Create a draft (new, reply, reply to all, forward); a sync later puts it into the Drafts folder. Nothing is ever sent.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "account name (default: the account of the original message, or the only account)"},
    "kind": {"type": "string", "enum": ["new", "reply", "reply_all", "forward"]},
    "in_reply_to": {"type": "integer", "minimum": 1, "description": "local id of the original message (reply, reply_all, forward)"},
    "in_reply_to_message_id": {"type": "string", "minLength": 1, "description": "Message-ID of the original message instead of its local id"},
    "identity": {"type": "string", "description": "address or id of an identity of the account (default: the default identity)"},
    "to": ` + addrListSchema + `,
    "cc": ` + addrListSchema + `,
    "bcc": ` + addrListSchema + `,
    "subject": {"type": "string", "description": "at most 500 characters (longer: denied too_large); default for reply/forward: the subject of the original with Re:/Fwd:"},
    "body_markdown": {"type": "string", "description": "at most 200000 characters (longer: denied too_large)"},
    "body_html": {"type": "string", "description": "at most 200000 characters (longer: denied too_large)"},
    "attachments": ` + draftAttachmentInputSchema + `,
    "quote": {"type": "boolean", "description": "quote the original (reply, reply_all; default true)"},
    "signature": {"type": "boolean", "description": "append the signature of the identity (default true)"},
    "include_attachments": {"type": "boolean", "description": "forward: include the attachments of the original (default true)"}
  },
  "required": ["kind"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(draftsWriteOutputSchema),
		Redact:       redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in DraftsCreateInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			kind := compose.Kind(in.Kind)
			if !drafts.ValidKind(drafts.Kind(kind)) {
				return nil, inputErr("unknown kind %q", in.Kind)
			}
			hasOrig := in.InReplyTo > 0 || in.InReplyToMessageID != ""
			switch {
			case kind == compose.KindNew && hasOrig:
				return nil, inputErr("a new draft has no original message; use kind reply or forward")
			case kind != compose.KindNew && !hasOrig:
				return nil, inputErr("%s needs the original message (in_reply_to or in_reply_to_message_id)", kind)
			case in.InReplyTo > 0 && in.InReplyToMessageID != "":
				return nil, inputErr("give either in_reply_to or in_reply_to_message_id")
			case in.BodyMarkdown != "" && in.BodyHTML != "":
				return nil, inputErr("give either body_markdown or body_html")
			}

			// Konto: das der Ausgangsnachricht, sonst das genannte bzw. einzige.
			var acc store.Account
			var accp *store.Account
			if in.Account != "" || in.InReplyTo == 0 {
				a, err := ResolveAccount(ctx, o.accounts, in.Account)
				if err != nil {
					return nil, err
				}
				acc, accp = a, &a
			}
			var orig *compose.Original
			var origID int64
			var notes []string
			budget := o.newBudget() // eigene und weitergeleitete Anhänge zusammen (K2)
			if hasOrig {
				m, og, n, err := o.original(ctx, accp, in.InReplyTo, in.InReplyToMessageID, kind == compose.KindForward && boolOr(in.IncludeAttachments, true), budget)
				if err != nil {
					return nil, err
				}
				orig, origID, notes = og, m.ID, n
				if accp == nil {
					if acc, err = o.accountByID(ctx, m.AccountID); err != nil {
						return nil, err
					}
				}
			}
			ident, hasIdent, err := o.identity(ctx, acc.ID, in.Identity)
			if err != nil {
				return nil, err
			}
			atts, err := o.attachments(ctx, acc, in.Attachments, budget)
			if err != nil {
				return nil, err
			}
			own, err := o.svc.OwnAddresses(ctx, acc.ID)
			if err != nil {
				return nil, err
			}
			opts := composeOptions{Quote: boolOr(in.Quote, true), Signature: boolOr(in.Signature, true)}
			ci := compose.NewInput(kind)
			ci.AccountAddress, ci.OwnAddresses, ci.Original = acc.Email, own, orig
			if hasIdent {
				ci.Identity = composeIdentity(ident)
			}
			ci.To, ci.Cc, ci.Bcc, ci.Subject = in.To, in.Cc, in.Bcc, in.Subject
			ci.BodyMarkdown, ci.BodyHTML = in.BodyMarkdown, in.BodyHTML
			ci.Quote, ci.Signature, ci.IncludeAttachments = opts.Quote, opts.Signature, boolOr(in.IncludeAttachments, true)
			for _, a := range atts {
				ci.Attachments = append(ci.Attachments, a.att)
			}
			msg, err := o.build(ci)
			if err != nil {
				return nil, err
			}
			for _, n := range msg.Notes {
				notes = addNote(notes, composeNotes[n])
			}
			if !call.DryRun {
				if err := o.putFiles(atts); err != nil {
					return nil, err
				}
				// Weitergeleitete Anhänge stammen aus dem Blob-Store und liegen dort schon.
			}
			nd := drafts.NewDraft{AccountID: acc.ID, IdentityID: ident.ID, Kind: drafts.Kind(kind), InReplyToMsg: origID,
				Content: content(msg, in.BodyMarkdown, in.BodyHTML, opts), CreatedBy: call.Key.ID}
			res, err := o.as(call, "drafts.create", policy.ScopeMailDraft).Create(ctx, nd)
			if err != nil {
				return nil, draftErr(err)
			}
			return o.writeOutput(acc.Name, res, notes, call.DryRun), nil
		},
	}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// --- drafts.update -------------------------------------------------------------

// DraftsUpdate ändert einen Entwurf (R2 wie create: die geänderte Kopie
// kommt wieder per Job in den Entwürfe-Ordner). Eine Änderung in
// pending_approval oder ready setzt den Entwurf auf draft und die Freigabe
// auf superseded (E7); nach sent_by_human/abandoned draft_closed, nach
// Übernahme im Mailclient draft_taken_over (E6).
func DraftsUpdate(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:    "drafts.update",
		Scope:   policy.ScopeMailDraft,
		Risk:    policy.R2,
		Summary: "Change a draft; a changed draft goes back to status draft and needs a new approval. Nothing is ever sent.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer", "minimum": 1},
    "to": ` + addrListSchema + `,
    "cc": ` + addrListSchema + `,
    "bcc": ` + addrListSchema + `,
    "subject": {"type": "string", "description": "at most 500 characters (longer: denied too_large); empty for reply/forward: derived from the original again"},
    "body_markdown": {"type": "string", "description": "replaces the text (and a former body_html); at most 200000 characters (longer: denied too_large)"},
    "body_html": {"type": "string", "description": "replaces the text (and a former body_markdown); at most 200000 characters (longer: denied too_large)"},
    "attachments": ` + draftAttachmentInputSchema + `,
    "quote": {"type": "boolean"},
    "signature": {"type": "boolean"}
  },
  "required": ["draft_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(draftsWriteOutputSchema),
		Redact:       redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in DraftsUpdateInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.BodyMarkdown != nil && in.BodyHTML != nil {
				return nil, inputErr("give either body_markdown or body_html")
			}
			// Lesen, zusammenführen und bauen geschieht außerhalb der
			// Schreibtransaktion; der Dienst schreibt nur, wenn der Entwurf
			// noch der gelesene ist (Fingerprint). Sonst hat ein anderer
			// Schreiber dazwischen geändert: neu lesen und die genannten
			// Felder auf seinem Stand zusammenführen, statt seine Änderung
			// zu überschreiben (Gegenprüfung S2.3, W4).
			for attempt := 1; ; attempt++ {
				out, err := o.updateOnce(ctx, call, in)
				var de *DeniedError
				if !errors.As(err, &de) || de.Reason != drafts.ReasonDraftChanged || attempt == updateAttempts {
					return out, err
				}
			}
		},
	}
}

// updateAttempts: so oft versucht drafts.update, die genannten Felder auf
// dem aktuellen Stand zusammenzuführen, bevor es draft_changed meldet.
const updateAttempts = 3

// testHookBeforeUpdateWrite läuft in Tests zwischen Lesen/Bauen und
// Schreiben einer Änderung (zweiter Schreiber, W4); nil = nichts.
var testHookBeforeUpdateWrite func()

// updateOnce ist ein Versuch von drafts.update auf dem gerade gelesenen Stand.
func (o *draftOps) updateOnce(ctx context.Context, call Call, in DraftsUpdateInput) (any, error) {
	cur, err := o.as(call, "drafts.update", policy.ScopeMailDraft).Get(ctx, in.DraftID)
	if err != nil {
		return nil, err
	}
	if st := drafts.Status(cur.Status); st.Closed() {
		_, err := drafts.Next(st, drafts.EvUpdate)
		return nil, draftErr(err)
	}
	if cur.RemoteState == drafts.RemoteTakenOver {
		return nil, &DeniedError{Reason: drafts.ReasonDraftTakenOver, Msg: fmt.Sprintf("draft %d was taken over in the mail client; change it there", cur.ID)}
	}
	acc, err := o.accountByID(ctx, cur.AccountID)
	if err != nil {
		return nil, err
	}
	kind := compose.Kind(cur.Kind)
	var orig *compose.Original
	var notes []string
	if cur.InReplyToMsg > 0 {
		_, og, _, err := o.original(ctx, &acc, cur.InReplyToMsg, "", false, nil)
		if err != nil {
			return nil, err
		}
		orig = og
	}
	// Absender: die Kopie im Entwurf; Signatur und Präfixe aus der
	// Identität, solange es sie gibt.
	ci := compose.NewInput(kind)
	ci.AccountAddress, ci.Original, ci.ExplicitRecipients = cur.FromEmail, orig, true
	if ci.Identity, err = o.draftIdentity(ctx, cur); err != nil {
		return nil, err
	}
	opts := parseOptions(cur.ComposeOptions)
	opts.Quote, opts.Signature = boolOr(in.Quote, opts.Quote), boolOr(in.Signature, opts.Signature)
	ci.Quote, ci.Signature, ci.IncludeAttachments = opts.Quote, opts.Signature, false
	ci.To, ci.Cc, ci.Bcc = formatAddrs(cur.To), formatAddrs(cur.Cc), formatAddrs(cur.Bcc)
	for _, p := range []struct {
		in  *[]string
		out *[]string
	}{{in.To, &ci.To}, {in.Cc, &ci.Cc}, {in.Bcc, &ci.Bcc}} {
		if p.in != nil {
			*p.out = *p.in
		}
	}
	ci.Subject = cur.Subject
	if in.Subject != nil {
		ci.Subject = *in.Subject
	}
	md, html := cur.BodyMarkdown, cur.BodyHTML
	switch {
	case in.BodyMarkdown != nil:
		md, html = *in.BodyMarkdown, ""
	case in.BodyHTML != nil:
		md, html = "", *in.BodyHTML
	}
	ci.BodyMarkdown, ci.BodyHTML = md, html
	var atts []loadedAttachment
	if in.Attachments != nil {
		atts, err = o.attachments(ctx, acc, *in.Attachments, o.newBudget())
	} else {
		atts, err = o.storedAttachments(cur.Attachments, o.newBudget())
	}
	if err != nil {
		return nil, err
	}
	for _, a := range atts {
		ci.Attachments = append(ci.Attachments, a.att)
	}
	ci.MessageID = cur.MessageIDHeader
	msg, err := o.build(ci)
	if err != nil {
		return nil, err
	}
	for _, n := range msg.Notes {
		if n != compose.NoteNoIdentity {
			notes = addNote(notes, composeNotes[n])
		}
	}
	if !call.DryRun {
		if err := o.putFiles(atts); err != nil {
			return nil, err
		}
	}
	if testHookBeforeUpdateWrite != nil {
		testHookBeforeUpdateWrite()
	}
	res, err := o.as(call, "drafts.update", policy.ScopeMailDraft).Update(ctx, cur.ID, content(msg, md, html, opts), drafts.Fingerprint(cur))
	if err != nil {
		return nil, draftErr(err)
	}
	if !res.Changed {
		notes = addNote(notes, "nothing changed")
	}
	return o.writeOutput(acc.Name, res, notes, call.DryRun), nil
}

// --- drafts.get ---------------------------------------------------------------

// DraftDetail ist die Ausgabe von drafts.get: der ganze Entwurf.
type DraftDetail struct {
	DraftID           int64                   `json:"draft_id"`
	Account           string                  `json:"account"`
	Kind              string                  `json:"kind"`
	Status            string                  `json:"status"`
	MessageID         string                  `json:"message_id"`
	OriginalID        int64                   `json:"original_id,omitempty"`
	InReplyTo         string                  `json:"in_reply_to,omitempty"`
	References        string                  `json:"references,omitempty"`
	From              string                  `json:"from"`
	To                []string                `json:"to"`
	Cc                []string                `json:"cc"`
	Bcc               []string                `json:"bcc"`
	Subject           string                  `json:"subject"`
	BodyText          string                  `json:"body_text"`
	BodyMarkdown      string                  `json:"body_markdown,omitempty"`
	BodyHTML          string                  `json:"body_html,omitempty"`
	Quote             bool                    `json:"quote"`
	Signature         bool                    `json:"signature"`
	Attachments       []DraftAttachmentOutput `json:"attachments"`
	Warnings          []string                `json:"warnings"`
	HookResult        string                  `json:"hook_result,omitempty"`
	ApprovalID        int64                   `json:"approval_id,omitempty"`
	ApprovalStatus    string                  `json:"approval_status,omitempty"`
	ApprovalExpiresAt string                  `json:"approval_expires_at,omitempty"`
	Remote            string                  `json:"remote"`
	RemoteNote        string                  `json:"remote_note,omitempty"`
	RemoteMailboxID   int64                   `json:"remote_mailbox_id,omitempty"`
	RemoteUID         int64                   `json:"remote_uid,omitempty"`
	PreviewHash       string                  `json:"preview_hash"`
	CreatedBy         string                  `json:"created_by"`
	CreatedAt         string                  `json:"created_at"`
	UpdatedAt         string                  `json:"updated_at"`
	ClosedAt          string                  `json:"closed_at,omitempty"`
	SentMessageID     int64                   `json:"sent_message_id,omitempty"`
}

// DraftsGet zeigt einen Entwurf vollständig (R0). Eine abgelaufene
// Freigabe fällt vorher zurück (E7: beim Lesen).
func DraftsGet(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:       "drafts.get",
		Scope:      policy.ScopeMailDraft,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show a draft: status, recipients, subject, text, attachments, approval and the state of its copy in the Drafts folder.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"draft_id": {"type": "integer", "minimum": 1}},
  "required": ["draft_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer"}, "account": {"type": "string"},
    "kind": {"type": "string", "enum": ["new", "reply", "reply_all", "forward"]},
    "status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "message_id": {"type": "string"}, "original_id": {"type": "integer"},
    "in_reply_to": {"type": "string"}, "references": {"type": "string"},
    "from": {"type": "string"}, "to": {"type": "array", "items": {"type": "string"}},
    "cc": {"type": "array", "items": {"type": "string"}}, "bcc": {"type": "array", "items": {"type": "string"}},
    "subject": {"type": "string"}, "body_text": {"type": "string"},
    "body_markdown": {"type": "string"}, "body_html": {"type": "string"},
    "quote": {"type": "boolean"}, "signature": {"type": "boolean"},
    "attachments": ` + draftAttachmentsSchema + `,
    "warnings": {"type": "array", "items": {"type": "string"}},
    "hook_result": {"type": "string", "enum": ["pass", "fail", "timeout", "error"]},
    "approval_id": {"type": "integer"},
    "approval_status": {"type": "string", "enum": ["pending", "approved", "rejected", "superseded", "expired"]},
    "approval_expires_at": {"type": "string", "format": "date-time"},
    "remote": {"type": "string", "enum": ["pending", "pushed", "taken_over", "missing"]},
    "remote_note": {"type": "string"}, "remote_mailbox_id": {"type": "integer"}, "remote_uid": {"type": "integer"},
    "preview_hash": {"type": "string"}, "created_by": {"type": "string"},
    "created_at": {"type": "string", "format": "date-time"}, "updated_at": {"type": "string", "format": "date-time"},
    "closed_at": {"type": "string", "format": "date-time"}, "sent_message_id": {"type": "integer"}
  },
  "required": ["draft_id", "account", "kind", "status", "message_id", "from", "to", "cc", "bcc", "subject", "body_text",
    "quote", "signature", "attachments", "warnings", "remote", "preview_hash", "created_by", "created_at", "updated_at"]
}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in draftIDInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			dr, err := o.svc.Get(ctx, in.DraftID)
			if err != nil {
				return nil, err
			}
			acc, err := o.accountByID(ctx, dr.AccountID)
			if err != nil {
				return nil, err
			}
			opts := parseOptions(dr.ComposeOptions)
			out := DraftDetail{DraftID: dr.ID, Account: acc.Name, Kind: dr.Kind, Status: dr.Status, MessageID: dr.MessageIDHeader,
				OriginalID: dr.InReplyToMsg, From: formatAddr(store.Addr{Name: dr.FromName, Address: dr.FromEmail}),
				To: formatAddrs(dr.To), Cc: formatAddrs(dr.Cc), Bcc: formatAddrs(dr.Bcc), Subject: dr.Subject, BodyText: dr.BodyText,
				BodyMarkdown: dr.BodyMarkdown, BodyHTML: dr.BodyHTML, Quote: opts.Quote, Signature: opts.Signature,
				HookResult: dr.HookResult, ApprovalID: dr.ApprovalID, Remote: dr.RemoteState, RemoteNote: remoteNote(dr),
				RemoteMailboxID: dr.RemoteMailboxID, RemoteUID: dr.RemoteUID, PreviewHash: dr.PreviewHash, CreatedBy: dr.CreatedBy,
				CreatedAt: rfc3339(dr.CreatedAt), UpdatedAt: rfc3339(dr.UpdatedAt), ClosedAt: rfc3339(dr.ClosedAt), SentMessageID: dr.SentMessageID}
			var h map[string]any
			if json.Unmarshal([]byte(dr.HeadersJSON), &h) == nil {
				out.InReplyTo, _ = h[drafts.HeaderInReplyTo].(string)
				out.References, _ = h["References"].(string)
			}
			out.Attachments, out.Warnings = attachmentOutputs(dr.Attachments)
			if dr.ApprovalID > 0 {
				if a, err := o.svc.Store.Approval(ctx, dr.ApprovalID); err == nil {
					out.ApprovalStatus, out.ApprovalExpiresAt = a.Decision, rfc3339(a.ExpiresAt)
				}
			}
			return out, nil
		},
	}
}

// --- drafts.list --------------------------------------------------------------

// DraftSummary ist ein Entwurf in drafts.list.
type DraftSummary struct {
	DraftID     int64  `json:"draft_id"`
	Account     string `json:"account"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Subject     string `json:"subject"`
	Recipients  int    `json:"recipients"`
	Attachments int    `json:"attachments"`
	Remote      string `json:"remote"`
	ApprovalID  int64  `json:"approval_id,omitempty"`
	HookResult  string `json:"hook_result,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

// DraftsListOutput ist die Ausgabe von drafts.list.
type DraftsListOutput struct {
	Drafts []DraftSummary `json:"drafts"`
}

// DraftsListInput ist die Eingabe von drafts.list.
type DraftsListInput struct {
	Account string   `json:"account,omitempty"`
	Status  []string `json:"status,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

// DraftsList listet Entwürfe, zuletzt geänderte zuerst (R0); abgelaufene
// Freigaben fallen vorher zurück.
func DraftsList(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:       "drafts.list",
		Scope:      policy.ScopeMailDraft,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "List drafts with status, subject, number of recipients and the state of their copy in the Drafts folder.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "status": {"type": "array", "items": {"type": "string", "enum": ` + draftStatusEnum + `}},
    "limit": {"type": "integer", "minimum": 1, "maximum": 500, "description": "default 100"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "drafts": {"type": "array", "items": {"type": "object", "properties": {
      "draft_id": {"type": "integer"}, "account": {"type": "string"},
      "kind": {"type": "string", "enum": ["new", "reply", "reply_all", "forward"]},
      "status": {"type": "string", "enum": ` + draftStatusEnum + `}, "subject": {"type": "string"},
      "recipients": {"type": "integer"}, "attachments": {"type": "integer"},
      "remote": {"type": "string", "enum": ["pending", "pushed", "taken_over", "missing"]},
      "approval_id": {"type": "integer"}, "hook_result": {"type": "string", "enum": ["pass", "fail", "timeout", "error"]},
      "updated_at": {"type": "string", "format": "date-time"}},
      "required": ["draft_id", "account", "kind", "status", "subject", "recipients", "attachments", "remote", "updated_at"]}}
  },
  "required": ["drafts"]
}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in DraftsListInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			f := store.DraftFilter{Status: in.Status, Limit: in.Limit}
			if f.Limit == 0 {
				f.Limit = 100
			}
			if in.Account != "" {
				a, err := ResolveAccount(ctx, o.accounts, in.Account)
				if err != nil {
					return nil, err
				}
				f.AccountID = a.ID
			}
			list, err := o.svc.List(ctx, f)
			if err != nil {
				return nil, err
			}
			accs, err := o.accounts.ListAccounts(ctx)
			if err != nil {
				return nil, err
			}
			names := map[int64]string{}
			for _, a := range accs {
				names[a.ID] = a.Name
			}
			out := DraftsListOutput{Drafts: []DraftSummary{}}
			for _, dr := range list {
				out.Drafts = append(out.Drafts, DraftSummary{DraftID: dr.ID, Account: names[dr.AccountID], Kind: dr.Kind, Status: dr.Status,
					Subject: dr.Subject, Recipients: len(dr.To) + len(dr.Cc) + len(dr.Bcc), Attachments: len(dr.Attachments),
					Remote: dr.RemoteState, ApprovalID: dr.ApprovalID, HookResult: dr.HookResult, UpdatedAt: rfc3339(dr.UpdatedAt)})
			}
			return out, nil
		},
	}
}

// --- approvals.get ------------------------------------------------------------

// ApprovalOutput ist die Ausgabe von approvals.get und ein Teil der
// Ausgaben von approve/reject. Sie enthält keinen Mailinhalt; reason ist
// die Begründung des Menschen bei einer Ablehnung.
type ApprovalOutput struct {
	ApprovalID  int64  `json:"approval_id"`
	DraftID     int64  `json:"draft_id"`
	Status      string `json:"status"`
	DraftStatus string `json:"draft_status"`
	Description string `json:"description"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	DecidedBy   string `json:"decided_by,omitempty"`
	DecidedAt   string `json:"decided_at,omitempty"`
	Reason      string `json:"reason,omitempty"`
	PreviewHash string `json:"preview_hash"`
	// MessageHash: Hash der vorgelegten Nachricht (W1; "" vor Schema 12).
	MessageHash string `json:"message_hash,omitempty"`
}

const approvalOutputProps = `"approval_id": {"type": "integer"}, "draft_id": {"type": "integer"},
    "status": {"type": "string", "enum": ["pending", "approved", "rejected", "superseded", "expired"]},
    "draft_status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "description": {"type": "string"}, "requested_by": {"type": "string"},
    "requested_at": {"type": "string", "format": "date-time"}, "expires_at": {"type": "string", "format": "date-time"},
    "decided_by": {"type": "string"}, "decided_at": {"type": "string", "format": "date-time"},
    "reason": {"type": "string"}, "preview_hash": {"type": "string"}, "message_hash": {"type": "string"}`

func approvalOutput(a store.Approval, d store.Draft) ApprovalOutput {
	return ApprovalOutput{ApprovalID: a.ID, DraftID: a.SubjectID, Status: a.Decision, DraftStatus: d.Status, Description: a.Description,
		RequestedBy: a.RequestedBy, RequestedAt: rfc3339(a.RequestedAt), ExpiresAt: rfc3339(a.ExpiresAt), DecidedBy: a.DecidedBy,
		DecidedAt: rfc3339(a.DecidedAt), Reason: a.Reason, PreviewHash: a.PreviewHash, MessageHash: a.MessageHash}
}

// ApprovalsGet zeigt den Stand einer Freigabe (R0): offen, erteilt,
// abgelehnt, ersetzt (Entwurf geändert) oder abgelaufen.
func ApprovalsGet(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:       "approvals.get",
		Scope:      policy.ScopeMailDraft,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show the state of an approval request: pending, approved, rejected, superseded or expired, and who decided.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"approval_id": {"type": "integer", "minimum": 1}},
  "required": ["approval_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {` + approvalOutputProps + `},
  "required": ["approval_id", "draft_id", "status", "draft_status", "description", "requested_by", "requested_at", "preview_hash"]
}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ApprovalID int64 `json:"approval_id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			a, err := o.svc.Store.Approval(ctx, in.ApprovalID)
			if err != nil {
				return nil, err
			}
			if a.SubjectType != "draft" {
				return nil, fmt.Errorf("%w: %d", store.ErrApprovalNotFound, in.ApprovalID)
			}
			// Ablauf beim Lesen (E7), dann frisch lesen.
			dr, err := o.svc.Get(ctx, a.SubjectID)
			if err != nil {
				return nil, err
			}
			if a, err = o.svc.Store.Approval(ctx, in.ApprovalID); err != nil {
				return nil, err
			}
			return approvalOutput(a, dr), nil
		},
	}
}

// --- drafts.request_ready -----------------------------------------------------

// DraftsRequestOutput ist die Ausgabe von drafts.request_ready.
type DraftsRequestOutput struct {
	DraftID        int64  `json:"draft_id"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status"`
	Hook           string `json:"hook"`
	// HookOutput: die ersten 4 KB der Ausgabe des Hooks, wenn er nicht
	// bestand (nur hier, nie im Audit).
	HookOutput  string `json:"hook_output,omitempty"`
	ApprovalID  int64  `json:"approval_id,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	PreviewHash string `json:"preview_hash"`
	// MessageHash: SHA-256 der gebauten Nachricht ohne Date
	// (compose.MessageHash); die Freigabe bindet ihn (W1).
	MessageHash string `json:"message_hash"`
	DryRun      bool   `json:"dry_run"`
}

// DraftsRequestReady legt einen Entwurf zur Freigabe durch einen Menschen
// vor (R1): erst der Prüf-Hook (S2.4; bis dahin „pass"), dann eine
// Freigabe (pending) mit dem preview_hash, Entwurf pending_approval.
// Scheitert der Hook, bleibt der Entwurf draft (kein Fehler; hook und
// hook_output im Ergebnis). Im Trockenlauf wird nur geprüft.
func DraftsRequestReady(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:    "drafts.request_ready",
		Scope:   policy.ScopeMailDraft,
		Risk:    policy.R1,
		Summary: "Submit a draft for approval by a human (runs the check hook first); only a human can approve.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"draft_id": {"type": "integer", "minimum": 1}},
  "required": ["draft_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer"},
    "status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "previous_status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "hook": {"type": "string", "enum": ["pass", "fail", "timeout", "error"]},
    "hook_output": {"type": "string"}, "approval_id": {"type": "integer"},
    "expires_at": {"type": "string", "format": "date-time"}, "preview_hash": {"type": "string"},
    "message_hash": {"type": "string"}, "dry_run": {"type": "boolean"}
  },
  "required": ["draft_id", "status", "previous_status", "hook", "preview_hash", "message_hash", "dry_run"]
}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in draftIDInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			svc := o.as(call, "drafts.request_ready", policy.ScopeMailDraft)
			dr, err := svc.Get(ctx, in.DraftID)
			if err != nil {
				return nil, err
			}
			// Erst der Übergang (kein Hook für einen abgeschlossenen oder
			// schon vorgelegten Entwurf), dann Nachrichtenbau und Hook.
			if _, err := drafts.Next(drafts.Status(dr.Status), drafts.EvRequestReady); err != nil {
				return nil, draftErr(err)
			}
			acc, err := o.accountByID(ctx, dr.AccountID)
			if err != nil {
				return nil, err
			}
			msg, err := o.rebuild(ctx, acc, dr)
			if err != nil {
				return nil, err
			}
			hook, err := o.runCheck(ctx, dr, acc.Name, msg.Raw)
			if err != nil {
				return nil, err
			}
			// Die Freigabe bindet genau den Stand, den Nachrichtenbau und Hook
			// gesehen haben (W1, W2).
			mh := compose.MessageHash(msg.Raw)
			res, err := svc.RequestReady(ctx, dr.ID, call.Key.ID, hook.Result, drafts.Checked{Fingerprint: drafts.Fingerprint(dr), MessageHash: mh})
			if err != nil {
				return nil, draftErr(err)
			}
			out := DraftsRequestOutput{DraftID: res.Draft.ID, Status: string(res.To), PreviousStatus: string(res.From), Hook: hook.Result,
				PreviewHash: res.Draft.PreviewHash, MessageHash: mh, DryRun: call.DryRun}
			if hook.Result != drafts.HookPass {
				out.HookOutput = hook.Output
			}
			if res.Approval != nil && !call.DryRun {
				out.ApprovalID, out.ExpiresAt = res.Approval.ID, rfc3339(res.Approval.ExpiresAt)
			}
			return out, nil
		},
	}
}

// runCheck ist die Stelle, an der S2.4 den Prüf-Hook einhängt
// (Deps.DraftCheck): Er bekommt den Entwurf und die fertige Nachricht.
// Ohne Hook gilt „pass" (wie ein leerer check_hook, A02 E4).
func (o *draftOps) runCheck(ctx context.Context, dr store.Draft, account string, raw []byte) (DraftCheckResult, error) {
	if o.check == nil {
		return DraftCheckResult{Result: drafts.HookPass}, nil
	}
	r, err := o.check.CheckDraft(ctx, DraftCheckInput{Draft: dr, Account: account, EML: raw})
	if err != nil {
		return DraftCheckResult{}, err
	}
	switch r.Result {
	case drafts.HookPass, drafts.HookFail, drafts.HookTimeout, drafts.HookError:
	default:
		return DraftCheckResult{}, fmt.Errorf("check hook: unknown result %q", r.Result)
	}
	if out, cut := truncateBytes(r.Output, 4096); cut {
		r.Output = out
	}
	return r, nil
}

// truncateBytes kürzt s auf höchstens n Bytes an einer Zeichengrenze.
func truncateBytes(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

// rebuild baut die Nachricht eines gespeicherten Entwurfs aus seinen
// Eingaben neu: für den Prüf-Hook, für den Hash, den die Freigabe bindet
// (Vorlage und Freigabe, W1) und ab S2.5 für den Push. Bis auf Date ist das
// Ergebnis bei gleichem Entwurf, gleicher Identität und gleicher
// Ausgangsnachricht byte-gleich (compose.MessageHash).
func (o *draftOps) rebuild(ctx context.Context, acc store.Account, dr store.Draft) (*compose.Message, error) {
	ci := compose.NewInput(compose.Kind(dr.Kind))
	ci.AccountAddress, ci.ExplicitRecipients = dr.FromEmail, true
	if dr.InReplyToMsg > 0 {
		_, og, _, err := o.original(ctx, &acc, dr.InReplyToMsg, "", false, nil)
		if err != nil {
			return nil, err
		}
		ci.Original = og
	}
	var err error
	if ci.Identity, err = o.draftIdentity(ctx, dr); err != nil {
		return nil, err
	}
	opts := parseOptions(dr.ComposeOptions)
	ci.Quote, ci.Signature, ci.IncludeAttachments = opts.Quote, opts.Signature, false
	ci.To, ci.Cc, ci.Bcc, ci.Subject = formatAddrs(dr.To), formatAddrs(dr.Cc), formatAddrs(dr.Bcc), dr.Subject
	ci.BodyMarkdown, ci.BodyHTML, ci.MessageID = dr.BodyMarkdown, dr.BodyHTML, dr.MessageIDHeader
	atts, err := o.storedAttachments(dr.Attachments, o.newBudget())
	if err != nil {
		return nil, err
	}
	for _, a := range atts {
		ci.Attachments = append(ci.Attachments, a.att)
	}
	return o.build(ci)
}

// messageHashFor baut den Entwurf einer Freigabe neu und liefert den Hash
// der Nachricht (W1). Für eine Freigabe, die nicht mehr offen ist, oder
// einen Entwurf außerhalb von pending_approval "" (die Ablehnung kommt dann
// vom Dienst mit ihrem eigenen Grund).
func (o *draftOps) messageHashFor(ctx context.Context, approvalID int64) (string, error) {
	a, err := o.svc.Store.Approval(ctx, approvalID)
	if err != nil || a.SubjectType != "draft" || a.Decision != drafts.DecisionPending {
		return "", nil
	}
	dr, err := o.svc.Store.Draft(ctx, a.SubjectID)
	if err != nil || dr.Status != string(drafts.StatusPendingApproval) {
		return "", nil
	}
	acc, err := o.accountByID(ctx, dr.AccountID)
	if err != nil {
		return "", err
	}
	msg, err := o.rebuild(ctx, acc, dr)
	if err != nil {
		return "", err
	}
	return compose.MessageHash(msg.Raw), nil
}

// --- drafts.approve, drafts.reject ------------------------------------------------

// DraftsDecideOutput ist die Ausgabe von drafts.approve und drafts.reject.
type DraftsDecideOutput struct {
	ApprovalID     int64  `json:"approval_id"`
	DraftID        int64  `json:"draft_id"`
	Decision       string `json:"decision"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status"`
	DecidedBy      string `json:"decided_by,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	DryRun         bool   `json:"dry_run"`
}

const decideOutputSchema = `{
  "type": "object",
  "properties": {
    "approval_id": {"type": "integer"}, "draft_id": {"type": "integer"},
    "decision": {"type": "string", "enum": ["pending", "approved", "rejected", "superseded", "expired"]},
    "status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "previous_status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "decided_by": {"type": "string"}, "expires_at": {"type": "string", "format": "date-time"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["approval_id", "draft_id", "decision", "status", "previous_status", "dry_run"]
}`

// DraftsApprove gibt einen vorgelegten Entwurf frei (nur Mensch, A02 E2):
// Der Entwurf wird ready, wenn er seit der Vorlage unverändert ist
// (preview_hash, sonst approval_stale) und die Freigabe nicht abgelaufen
// ist (approval_expired). ready heißt: Der Mensch sendet im Mailclient;
// PostKauz sendet nie.
func DraftsApprove(d Deps) Operation {
	return draftsDecide(d, "drafts.approve", "Approve a submitted draft (human only); it becomes ready, the human sends it in the mail client.")
}

// DraftsReject lehnt eine Vorlage ab (nur Mensch); der Entwurf wird
// wieder draft. Die Begründung steht nur in der Freigabe, nie im Audit.
func DraftsReject(d Deps) Operation {
	return draftsDecide(d, "drafts.reject", "Reject a submitted draft (human only); it goes back to status draft.")
}

// MaxRejectReasonChars begrenzt die Begründung einer Ablehnung (Zeichen).
const MaxRejectReasonChars = 2000

func draftsDecide(d Deps, name, summary string) Operation {
	o := newDraftOps(d)
	reject := name == "drafts.reject"
	props := `"approval_id": {"type": "integer", "minimum": 1}`
	if reject {
		props += `, "reason": {"type": "string", "description": "why, at most 2000 characters (longer: denied too_large); kept in the approval, not in the audit log"}`
	}
	return Operation{
		Name:      name,
		Scope:     policy.ScopeMailDraft,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary:   summary,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {` + props + `},
  "required": ["approval_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(decideOutputSchema),
		Redact:       redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ApprovalID int64  `json:"approval_id"`
				Reason     string `json:"reason,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if n := utf8.RuneCountInString(in.Reason); n > MaxRejectReasonChars {
				return nil, &DeniedError{Reason: drafts.ReasonTooLarge, Msg: fmt.Sprintf("reason has %d characters (at most %d)", n, MaxRejectReasonChars)}
			}
			by := drafts.DecidedBy(o.cfg.Operator)
			svc := o.as(call, name, policy.ScopeMailDraft)
			var res drafts.Result
			var err error
			if reject {
				res, err = svc.Reject(ctx, in.ApprovalID, by, in.Reason)
			} else {
				// Die Freigabe gilt der fertigen Nachricht: neu bauen und mit
				// dem Hash der Vorlage vergleichen (W1).
				mh, herr := o.messageHashFor(ctx, in.ApprovalID)
				if herr != nil {
					return nil, draftErr(herr)
				}
				res, err = svc.Approve(ctx, in.ApprovalID, by, mh)
			}
			if err != nil {
				return nil, draftErr(err)
			}
			out := DraftsDecideOutput{ApprovalID: in.ApprovalID, DraftID: res.Draft.ID, Status: string(res.To),
				PreviousStatus: string(res.From), DryRun: call.DryRun}
			if res.Approval != nil {
				out.Decision, out.DecidedBy, out.ExpiresAt = res.Approval.Decision, res.Approval.DecidedBy, rfc3339(res.Approval.ExpiresAt)
			}
			return out, nil
		},
	}
}

// --- drafts.abandon, drafts.mark_sent ---------------------------------------------

// DraftsCloseOutput ist die Ausgabe von drafts.abandon und drafts.mark_sent.
type DraftsCloseOutput struct {
	DraftID            int64  `json:"draft_id"`
	Status             string `json:"status"`
	PreviousStatus     string `json:"previous_status"`
	ApprovalSuperseded int64  `json:"approval_superseded,omitempty"`
	DryRun             bool   `json:"dry_run"`
}

// DraftsAbandon verwirft einen Entwurf (nur Mensch). Die Kopie im
// Entwürfe-Ordner bleibt (kein Löschen im MVP).
func DraftsAbandon(d Deps) Operation {
	return draftsClose(d, "drafts.abandon", "Abandon a draft (human only); its copy in the Drafts folder stays, nothing is deleted.")
}

// DraftsMarkSent erklärt einen Entwurf für gesendet (nur Mensch), wenn die
// Versanderkennung ihn nicht fand (E5).
func DraftsMarkSent(d Deps) Operation {
	return draftsClose(d, "drafts.mark_sent", "Mark a draft as sent by the human (human only), when the sync did not detect it.")
}

func draftsClose(d Deps, name, summary string) Operation {
	o := newDraftOps(d)
	return Operation{
		Name:      name,
		Scope:     policy.ScopeMailDraft,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary:   summary,
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {"draft_id": {"type": "integer", "minimum": 1}},
  "required": ["draft_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer"},
    "status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "previous_status": {"type": "string", "enum": ` + draftStatusEnum + `},
    "approval_superseded": {"type": "integer"}, "dry_run": {"type": "boolean"}
  },
  "required": ["draft_id", "status", "previous_status", "dry_run"]
}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in draftIDInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			svc := o.as(call, name, policy.ScopeMailDraft)
			var res drafts.Result
			var err error
			if name == "drafts.abandon" {
				res, err = svc.Abandon(ctx, in.DraftID)
			} else {
				res, err = svc.MarkSent(ctx, in.DraftID)
			}
			if err != nil {
				return nil, draftErr(err)
			}
			out := DraftsCloseOutput{DraftID: res.Draft.ID, Status: string(res.To), PreviousStatus: string(res.From), DryRun: call.DryRun}
			if res.Approval != nil && res.Approval.Decision == drafts.DecisionSuperseded {
				out.ApprovalSuperseded = res.Approval.ID
			}
			return out, nil
		},
	}
}

// --- Redact -------------------------------------------------------------------

var (
	draftKinds    = []string{"new", "reply", "reply_all", "forward"}
	draftStatuses = []string{"draft", "pending_approval", "ready", "sent_by_human", "abandoned"}
)

// redactDraftInput: nur bekannte Feldnamen (redactFields), IDs (Entwurf, Freigabe, Ausgangsnachricht,
// Identität), Art, Zustände und Zähler (Empfänger, Anhänge); nie Betreff,
// Adressen, Text, Dateinamen, Pfade oder die Begründung einer Ablehnung.
func redactDraftInput(raw json.RawMessage) (json.RawMessage, []string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	params := redactFields(m)
	var targets []string
	for _, k := range []struct{ field, target string }{{"draft_id", "draft"}, {"approval_id", "approval"}, {"in_reply_to", "message"}, {"id", "identity"}} {
		var n int64
		if v, ok := m[k.field]; ok && json.Unmarshal(v, &n) == nil && n > 0 {
			params[k.field] = n
			targets = append(targets, k.target+":"+strconv.FormatInt(n, 10))
		}
	}
	var s string
	if v, ok := m["account"]; ok && json.Unmarshal(v, &s) == nil && AccountNameRE.MatchString(s) {
		targets = append(targets, "account:"+s)
	}
	if v, ok := m["kind"]; ok && json.Unmarshal(v, &s) == nil && slices.Contains(draftKinds, s) {
		params["kind"] = s
	}
	var list []json.RawMessage
	for _, k := range []string{"to", "cc", "bcc", "attachments"} {
		if v, ok := m[k]; ok && json.Unmarshal(v, &list) == nil {
			params[k] = len(list)
		}
	}
	var sts []string
	if v, ok := m["status"]; ok && json.Unmarshal(v, &sts) == nil {
		var keep []string
		for _, st := range sts {
			if slices.Contains(draftStatuses, st) {
				keep = append(keep, st)
			}
		}
		params["status"] = keep
	}
	b, _ := json.Marshal(params)
	return b, targets
}
