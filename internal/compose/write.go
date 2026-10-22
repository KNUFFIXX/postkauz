package compose

import (
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-message"
)

// dateLayout ist das Format der Date-Kopfzeile (RFC 5322).
const dateLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

// header baut einen Kopf mit Feldern in der gegebenen Reihenfolge
// (go-message schreibt zuletzt hinzugefügte Felder zuerst).
func header(fields ...[2]string) message.Header {
	var h message.Header
	for i := len(fields) - 1; i >= 0; i-- {
		if fields[i][1] != "" {
			h.Add(fields[i][0], fields[i][1])
		}
	}
	return h
}

// boundary erzeugt eine MIME-Grenze aus dem (injizierbaren) Zufall.
func (b *builder) boundary() (string, error) {
	var buf [12]byte
	if _, err := io.ReadFull(b.in.Rand, buf[:]); err != nil {
		return "", fmt.Errorf("compose: random source: %w", err)
	}
	return "pk-" + hex.EncodeToString(buf[:]), nil
}

func addrList(l []Address) string {
	s := make([]string, len(l))
	for i, a := range l {
		s[i] = (&mail.Address{Name: a.Name, Address: a.Address}).String()
	}
	return strings.Join(s, ", ")
}

func idList(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return "<" + strings.Join(ids, "> <") + ">"
}

func (b *builder) write(w io.Writer, atts []loadedAttachment) error {
	m := &b.msg
	alt, err := b.boundary()
	if err != nil {
		return err
	}
	topType := mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alt})
	mixed := ""
	if len(atts) > 0 {
		if mixed, err = b.boundary(); err != nil {
			return err
		}
		topType = mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mixed})
	}
	inReplyTo := ""
	if m.InReplyTo != "" {
		inReplyTo = "<" + m.InReplyTo + ">"
	}
	top := header(
		[2]string{"Date", m.Date.Format(dateLayout)},
		[2]string{"From", addrList([]Address{m.From})},
		[2]string{"Reply-To", addrList(m.ReplyTo)},
		[2]string{"To", addrList(m.To)},
		[2]string{"Cc", addrList(m.Cc)},
		[2]string{"Bcc", addrList(m.Bcc)},
		[2]string{"Subject", mime.QEncoding.Encode("utf-8", m.Subject)},
		[2]string{"Message-ID", "<" + m.MessageID + ">"},
		[2]string{"In-Reply-To", inReplyTo},
		[2]string{"References", idList(m.References)},
		[2]string{"MIME-Version", "1.0"},
		[2]string{"Content-Type", topType},
	)
	mw, err := message.CreateWriter(w, top)
	if err != nil {
		return fmt.Errorf("compose: write header: %w", err)
	}
	altW := mw
	if mixed != "" {
		altW, err = mw.CreatePart(header([2]string{"Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alt})}))
		if err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
	}
	text := m.BodyText
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	for _, p := range []struct{ ct, body string }{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", m.BodyHTML},
	} {
		pw, err := altW.CreatePart(header([2]string{"Content-Type", p.ct}, [2]string{"Content-Transfer-Encoding", "quoted-printable"}))
		if err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
		if _, err := io.WriteString(pw, p.body); err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
		if err := pw.Close(); err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
	}
	if mixed != "" {
		if err := altW.Close(); err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
		for _, a := range atts {
			if err := writeAttachment(mw, a); err != nil {
				return err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("compose: write message: %w", err)
	}
	return nil
}

func writeAttachment(mw *message.Writer, a loadedAttachment) error {
	var h message.Header
	ctParams, dispParams := map[string]string{}, map[string]string{}
	if a.info.Filename != "" {
		ctParams["name"] = a.info.Filename
		dispParams["filename"] = a.info.Filename
	}
	ct := a.info.ContentType
	if strings.HasPrefix(ct, "text/") && utf8.Valid(a.data) {
		ctParams["charset"] = "utf-8"
	}
	// Reihenfolge im Kopf: Content-Type, Content-Disposition, CTE.
	h.Set("Content-Transfer-Encoding", "base64")
	h.SetContentDisposition("attachment", dispParams)
	h.SetContentType(ct, ctParams)
	pw, err := mw.CreatePart(h)
	if err != nil {
		return fmt.Errorf("compose: write attachment: %w", err)
	}
	if _, err := pw.Write(a.data); err != nil {
		return fmt.Errorf("compose: write attachment: %w", err)
	}
	if err := pw.Close(); err != nil {
		return fmt.Errorf("compose: write attachment: %w", err)
	}
	return nil
}
