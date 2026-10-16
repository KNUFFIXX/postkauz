package mailsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/magic"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// CleanVersion ist der Stand der Aufbereitung (bereinigter Text, Links,
// versteckter Text, Anhänge mit Typprüfung). Nachrichten mit kleinerem
// Stand (z. B. vor S1.6 gespiegelt) bereitet der nächste Sync aus der
// Rohmail neu auf. Erhöhen, wenn sich die Aufbereitung ändert.
//
//	1  S1.6: Text, Links, versteckter Text, Anhänge
//	2  Betreff, Absender-/Empfängernamen und Dateinamen ohne unsichtbare
//	   Zeichen (sanitize.Text); die Kopffelder in messages werden mit
//	   nachgezogen
//	3  alternative_mismatch: Klartext und HTML weichen ab
const CleanVersion = 3

// storedPart ist ein beim Parsen gespeicherter Anhang.
type storedPart struct {
	info     blob.Info
	detected magic.Type
	err      error
}

// analyze parst eine Rohmail, speichert dabei jeden Anhang dekodiert im
// Blob-Store (Typ per Magic Bytes im selben Durchgang) und liefert die
// Anhänge für den Store.
func (e *Engine) analyze(r io.Reader, internal time.Time) (*mimeparse.Message, []store.NewAttachment, error) {
	parts := map[string]storedPart{}
	m, err := mimeparse.Parse(r, mimeparse.Options{InternalDate: internal,
		OnAttachment: func(a mimeparse.Attachment, body io.Reader) error {
			var sn magic.Sniffer
			info, err := e.Blobs.Put(io.TeeReader(body, &sn))
			parts[a.PartID] = storedPart{info: info, detected: sn.Detect(), err: err}
			if err != nil {
				return fmt.Errorf("store attachment: %w", err)
			}
			return nil
		}})
	if err != nil {
		return nil, nil, err
	}
	atts := make([]store.NewAttachment, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		p := parts[a.PartID]
		chk := magic.Check(a.ContentType, a.Filename, p.detected)
		na := store.NewAttachment{PartID: a.PartID, Filename: sanitize.Text(a.Filename), ContentType: p.detected.MIME,
			DeclaredType: a.ContentType, Size: a.Size, Inline: a.Inline, ContentID: a.ContentID, Truncated: a.Truncated,
			TypeMismatch: chk.Mismatch, MismatchReason: chk.Reason, Executable: chk.Executable}
		if na.ContentType == "" {
			na.ContentType = magic.OctetStream
		}
		switch {
		case a.Truncated:
			na.ExtractStatus, na.ExtractError = extract.StatusSkipped, "larger than the part limit; not stored"
		case p.err != nil || p.info.SHA256 == "":
			na.ExtractStatus, na.ExtractError = extract.StatusSkipped, "not stored"
		default:
			na.SHA256, na.BlobSize, na.BlobCompressed = p.info.SHA256, p.info.Size, p.info.Compressed
			if p.info.Existed {
				if _, compressed, err := e.Blobs.Path(p.info.SHA256); err == nil {
					na.BlobCompressed = compressed
				}
			}
			na.ExtractStatus, na.ExtractError = extractPlan(na, p.detected)
		}
		atts = append(atts, na)
	}
	return m, atts, nil
}

// extractPlan entscheidet, ob ein Anhang extrahiert werden soll.
func extractPlan(a store.NewAttachment, t magic.Type) (status, reason string) {
	switch {
	case a.Executable:
		return extract.StatusSkipped, "executable content is not extracted"
	case a.TypeMismatch:
		return extract.StatusSkipped, "type mismatch; not extracted"
	case extract.KindOf(t.MIME) == extract.KindNone:
		return extract.StatusSkipped, "no text extractor for " + t.MIME
	case a.Size > extract.DefaultMaxInput:
		return extract.StatusSkipped, fmt.Sprintf("larger than %d bytes", extract.DefaultMaxInput)
	}
	return "pending", ""
}

// fillClean setzt die bereinigten Textfelder (internal/sanitize) und die
// Anhänge.
func fillClean(nm *store.NewMessage, m *mimeparse.Message, atts []store.NewAttachment) {
	nm.TextPlain = sanitize.Text(m.Text)
	nm.TextCharset = m.TextCharset
	main := nm.TextPlain
	links := []sanitize.Link{}
	if m.HTML != "" {
		h := sanitize.HTML(m.HTML)
		nm.TextHTML, nm.HiddenText = h.Text, h.Hidden
		// Der Agent liest den Klartext, der Mensch sieht das HTML: weichen
		// die beiden Darstellungen ab, wird das gemeldet.
		nm.AlternativeMismatch = m.Alternative && sanitize.AlternativeMismatch(nm.TextPlain, h.Text)
		if h.Links != nil {
			links = h.Links
		}
		if strings.TrimSpace(main) == "" {
			main = h.Text
		}
	}
	nm.Links, _ = json.Marshal(links)
	nm.TextUnquoted = sanitize.StripQuotes(main)
	nm.Snippet = snippet(nm.TextUnquoted)
	nm.Attachments = atts
	nm.CleanVersion = CleanVersion
}

// Prepare füllt die geparsten und bereinigten Felder einer Rohmail wie der
// Sync (Parser, Bereinigung, Links, versteckter Text, Anhänge im Blob-Store
// mit Typprüfung); für den Import (S1.10). Blob, Größe, Datum, Flags und
// content_hash setzt der Aufrufer. false: Der Kopf ist unlesbar, die
// Nachricht wird ohne geparste Felder gespeichert (wie beim Sync).
func Prepare(blobs *blob.Store, nm *store.NewMessage, r io.Reader, internal time.Time) bool {
	e := &Engine{Blobs: blobs}
	nm.CleanVersion = CleanVersion
	nm.Links = json.RawMessage("[]")
	m, atts, err := e.analyze(r, internal)
	if err != nil {
		return false
	}
	fillParsed(nm, m)
	fillClean(nm, m, atts)
	return true
}

// reprocess bereitet Nachrichten mit älterer Aufbereitung aus der Rohmail
// neu auf (Bestand von vor S1.6). Liefert die Zahl der Nachrichten.
func (e *Engine) reprocess(ctx context.Context) (int, error) {
	total := 0
	for {
		items, err := e.Store.ReprocessCandidates(ctx, e.AccountID, CleanVersion, 100)
		if err != nil || len(items) == 0 {
			return total, err
		}
		out := make([]store.Reprocessed, 0, len(items))
		for _, it := range items {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			out = append(out, e.reprocessOne(it))
		}
		if err := e.Store.SaveReprocessed(ctx, out); err != nil {
			return total, err
		}
		total += len(out)
		e.emit(Event{Kind: "backlog", Done: total, Message: "cleanup"})
	}
}

func (e *Engine) reprocessOne(it store.ReprocessItem) store.Reprocessed {
	keep := store.Reprocessed{MessageID: it.ID, KeepBody: true, Body: store.NewMessage{CleanVersion: CleanVersion}}
	rc, err := e.Blobs.Get(it.BlobSHA256)
	if err != nil {
		e.log().Warn("raw message unreadable; cleanup skipped", "message", it.ID, "error", err.Error())
		return keep
	}
	defer rc.Close()
	m, atts, err := e.analyze(rc, it.InternalDate)
	if err != nil {
		return keep
	}
	var nm store.NewMessage
	fillParsed(&nm, m)
	fillClean(&nm, m, atts)
	return store.Reprocessed{MessageID: it.ID, Body: nm, Headers: true}
}
