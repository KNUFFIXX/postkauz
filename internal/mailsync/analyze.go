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
const CleanVersion = 1

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
		na := store.NewAttachment{PartID: a.PartID, Filename: a.Filename, ContentType: p.detected.MIME,
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
	fillClean(&nm, m, atts)
	return store.Reprocessed{MessageID: it.ID, Body: nm}
}

// DefaultExtractBudget begrenzt die Zeit für Textextraktion je Lauf; der
// Rest folgt im nächsten Lauf.
const DefaultExtractBudget = 60 * time.Second

// extractPending extrahiert Text aus wartenden Anhängen, bis die Zeit
// abgelaufen oder nichts mehr zu tun ist. Ohne Extractor geschieht nichts
// (Status bleibt pending).
func (e *Engine) extractPending(ctx context.Context) (int, error) {
	if e.Extractor == nil {
		return 0, nil
	}
	budget := e.ExtractBudget
	if budget <= 0 {
		budget = DefaultExtractBudget
	}
	deadline := time.Now().Add(budget)
	done := 0
	for time.Now().Before(deadline) {
		items, err := e.Store.PendingExtractions(ctx, e.AccountID, 20)
		if err != nil || len(items) == 0 {
			return done, err
		}
		for _, it := range items {
			if err := ctx.Err(); err != nil {
				return done, err
			}
			res := e.extractOne(ctx, it)
			if err := ctx.Err(); err != nil {
				return done, err // abgebrochen: nicht als Fehler des Anhangs speichern
			}
			if err := e.Store.SetExtraction(ctx, it.ID, res.Status, res.Text, res.Error, res.Hidden); err != nil {
				return done, err
			}
			done++
			if !time.Now().Before(deadline) {
				break
			}
		}
	}
	return done, nil
}

func (e *Engine) extractOne(ctx context.Context, it store.ExtractItem) extract.Result {
	if it.SHA256 == "" {
		return extract.Result{Status: extract.StatusSkipped, Error: "not stored"}
	}
	rc, err := e.Blobs.Get(it.SHA256)
	if err != nil {
		return extract.Result{Status: extract.StatusFailed, Error: "attachment blob unreadable"}
	}
	defer rc.Close()
	return e.Extractor.Extract(ctx, extract.KindOf(it.ContentType), rc, it.Size)
}
