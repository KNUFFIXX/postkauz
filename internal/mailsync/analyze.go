package mailsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
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
//	4  A05 Teil 3: HTML-Quelltext im Klartextteil (sanitize.PlainIsHTML)
//	   gilt als HTML. Neu aufbereitet werden nur Nachrichten, auf die die
//	   Regel passen kann; alle übrigen mit Stand 3 hebt ein UPDATE auf 4
//	   (reprocess, fullCleanBelow).
const CleanVersion = 4

// fullCleanBelow: Nachrichten mit kleinerem Stand werden ganz neu
// aufbereitet. Ab diesem Stand bis unter CleanVersion nur die, deren
// Klartext HTML sein kann (store.ReprocessItem.PlainHTML); die übrigen
// hebt store.BumpCleanVersion in einem UPDATE (A05 Teil 3: nicht alle
// 61.500 Nachrichten des Betreibers neu aufbereiten).
const fullCleanBelow = 3

// storedPart ist ein beim Parsen gespeicherter Anhang.
type storedPart struct {
	info     blob.Info
	detected magic.Type
	err      error
}

// attachmentStoreError: Ein Anhang ließ sich nur vorübergehend nicht im
// Blob-Store ablegen: Die Sperre war bis zur Frist belegt (blob.ErrBusy)
// oder der Kontext endete (Nachbesserung 2, Punkt 8; eingeengt in der
// dritten Nachbesserung, Punkt 4). Die Nachricht wird dann nicht
// gespeichert, wie bei einem Fehler der Rohmail in mirrorRaw; der nächste
// Lauf spiegelt sie erneut, statt den Anhang dauerhaft als "not stored" zu
// führen. Jeder andere Fehler beim Ablegen eines Anhangs (Platte, Rechte,
// ein Virenscanner, der den Schadanhang nicht schreiben lässt) gilt als
// dauerhaft: Der Anhang bleibt "not stored", die Mail wird gespiegelt;
// sonst fehlte sie in jedem Lauf erneut.
type attachmentStoreError struct {
	PartID string
	Err    error
}

func (e *attachmentStoreError) Error() string {
	return fmt.Sprintf("store attachment %s: %v", e.PartID, e.Err)
}
func (e *attachmentStoreError) Unwrap() error { return e.Err }

// deferAttachmentErr meldet, ob ein Fehler beim Ablegen eines Anhangs die
// Nachricht verschiebt (*attachmentStoreError): nur eine bis zur Frist
// belegte Sperre (blob.ErrBusy) und ein beendeter Kontext.
func deferAttachmentErr(err error) bool {
	return errors.Is(err, blob.ErrBusy) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// putAttachment legt einen Anhang ab; in Tests ersetzbar.
var putAttachment = (*blob.Store).Put

// analyze parst eine Rohmail, speichert dabei jeden Anhang dekodiert im
// Blob-Store (Typ per Magic Bytes im selben Durchgang) und liefert die
// Anhänge für den Store. Scheitert das Ablegen eines Anhangs nur
// vorübergehend (deferAttachmentErr), kommt *attachmentStoreError
// (Nachricht nicht speichern, nächster Lauf versucht es erneut); bei jedem
// anderen Fehler (Größengrenze, unlesbarer Inhalt wie kaputtes Base64,
// dauerhafter Fehler des Stores) bleibt der Anhang "not stored".
func (e *Engine) analyze(r io.Reader, internal time.Time) (*mimeparse.Message, []store.NewAttachment, error) {
	parts := map[string]storedPart{}
	var storeErr *attachmentStoreError
	m, err := mimeparse.Parse(r, mimeparse.Options{InternalDate: internal,
		OnAttachment: func(a mimeparse.Attachment, body io.Reader) error {
			var sn magic.Sniffer
			info, err := putAttachment(e.Blobs, io.TeeReader(body, &sn))
			parts[a.PartID] = storedPart{info: info, detected: sn.Detect(), err: err}
			if err != nil {
				if storeErr == nil && deferAttachmentErr(err) {
					storeErr = &attachmentStoreError{PartID: a.PartID, Err: err}
				}
				return fmt.Errorf("store attachment: %w", err)
			}
			return nil
		}})
	if err != nil {
		return nil, nil, err
	}
	if storeErr != nil {
		return nil, nil, storeErr
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
//
// HTML-Quelltext im Klartextteil (A05 Teil 3, sanitize.PlainIsHTML): Der
// Klartext wird leer gespeichert, der Text kommt aus dem HTML-Teil bzw.,
// ohne HTML-Teil, aus dem Klartext-HTML (versteckter Text entfernt und
// gemeldet, Links gesammelt); text_source ist dann html, und
// alternative_mismatch bleibt false (der Agent liest, was der Mensch sieht).
func fillClean(nm *store.NewMessage, m *mimeparse.Message, atts []store.NewAttachment) {
	nm.TextPlain = sanitize.Text(m.Text)
	nm.TextCharset = m.TextCharset
	html := m.HTML
	plainHTML := sanitize.PlainIsHTML(nm.TextPlain)
	if plainHTML {
		if strings.TrimSpace(html) == "" {
			html = nm.TextPlain
		}
		nm.TextPlain = ""
	}
	main := nm.TextPlain
	links := []sanitize.Link{}
	if html != "" {
		h := sanitize.HTML(html)
		nm.TextHTML, nm.HiddenText = h.Text, h.Hidden
		// Der Agent liest den Klartext, der Mensch sieht das HTML: weichen
		// die beiden Darstellungen ab, wird das gemeldet.
		nm.AlternativeMismatch = !plainHTML && m.Alternative && sanitize.AlternativeMismatch(nm.TextPlain, h.Text)
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
// Nachricht wird ohne geparste Felder gespeichert (wie beim Sync). Ein
// Fehler heißt: Ein Anhang ließ sich nur vorübergehend nicht ablegen
// (Sperre belegt oder Kontextende, *attachmentStoreError); die Nachricht
// nicht speichern, wie bei einem Fehler beim Ablegen der Rohmail.
func Prepare(blobs *blob.Store, nm *store.NewMessage, r io.Reader, internal time.Time) (bool, error) {
	e := &Engine{Blobs: blobs}
	nm.CleanVersion = CleanVersion
	nm.Links = json.RawMessage("[]")
	m, atts, err := e.analyze(r, internal)
	var se *attachmentStoreError
	if errors.As(err, &se) {
		return false, err
	}
	if err != nil {
		return false, nil
	}
	fillParsed(nm, m)
	fillClean(nm, m, atts)
	return true, nil
}

// reprocess bereitet Nachrichten mit älterer Aufbereitung aus der Rohmail
// neu auf (Bestand von vor S1.6). Liefert die Zahl der Nachrichten.
// Scheitert das Ablegen eines Anhangs nur vorübergehend (Sperre belegt
// oder Kontextende; Nachbesserung 2, Punkt 8), bleibt diese Nachricht auf
// dem alten Stand; die übrigen des
// Pakets werden gespeichert, und die Aufbereitung endet für diesen Lauf
// (sonst käme dieselbe Nachricht sofort wieder an die Reihe). Der nächste
// Lauf versucht es erneut.
//
// Seit A05 Teil 3 (CleanVersion 4): Liegen unter den Kandidaten Nachrichten
// mit Stand ab fullCleanBelow, auf die die Regel für HTML im Klartextteil
// nicht passt, hebt ein UPDATE alle solchen des Kontos auf den neuen Stand
// (einmal je Lauf); neu aufbereitet werden dann nur die Treffer. Das läuft
// bei jedem Lauf, so dass auch Nachrichten erfasst werden, die ein älteres
// Programm nach einem Rückweg mit Stand 3 gespiegelt hat.
func (e *Engine) reprocess(ctx context.Context) (int, error) {
	total := 0
	bumped := false
	for {
		items, err := e.Store.ReprocessCandidates(ctx, e.AccountID, CleanVersion, 100)
		if err != nil || len(items) == 0 {
			return total, err
		}
		if !bumped && slices.ContainsFunc(items, func(it store.ReprocessItem) bool {
			return it.Version >= fullCleanBelow && !it.PlainHTML
		}) {
			bumped = true
			n, err := e.Store.BumpCleanVersion(ctx, e.AccountID, fullCleanBelow, CleanVersion)
			if err != nil {
				return total, err
			}
			if n > 0 {
				e.log().Info("cleanup version raised without reprocessing (no HTML in the plain part)", "count", n)
			}
			continue
		}
		out := make([]store.Reprocessed, 0, len(items))
		deferred := 0
		for _, it := range items {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			r, err := e.reprocessOne(it)
			if err != nil {
				deferred++
				e.log().Warn("attachment not stored; cleanup of this message repeated in the next run", "message", it.ID, "error", err.Error())
				continue
			}
			out = append(out, r)
		}
		if err := e.Store.SaveReprocessed(ctx, out); err != nil {
			return total, err
		}
		total += len(out)
		e.emit(Event{Kind: "backlog", Done: total, Message: "cleanup"})
		if deferred > 0 {
			return total, nil
		}
	}
}

// reprocessOne bereitet eine Nachricht neu auf. Ein Fehler kommt nur, wenn
// ein Anhang vorübergehend nicht abzulegen war (*attachmentStoreError);
// dann nichts speichern.
func (e *Engine) reprocessOne(it store.ReprocessItem) (store.Reprocessed, error) {
	keep := store.Reprocessed{MessageID: it.ID, KeepBody: true, Body: store.NewMessage{CleanVersion: CleanVersion}}
	rc, err := e.Blobs.Get(it.BlobSHA256)
	if err != nil {
		e.log().Warn("raw message unreadable; cleanup skipped", "message", it.ID, "error", err.Error())
		return keep, nil
	}
	defer rc.Close()
	m, atts, err := e.analyze(rc, it.InternalDate)
	var se *attachmentStoreError
	if errors.As(err, &se) {
		return store.Reprocessed{}, err
	}
	if err != nil {
		return keep, nil
	}
	var nm store.NewMessage
	fillParsed(&nm, m)
	fillClean(&nm, m, atts)
	return store.Reprocessed{MessageID: it.ID, Body: nm, Headers: true}, nil
}
