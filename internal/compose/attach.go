package compose

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/magic"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

type loadedAttachment struct {
	info AttachmentInfo
	data []byte
}

// attachments lädt die Anhänge (eigene zuerst, dann bei forward die der
// Ausgangsnachricht) und prüft die Summe.
func (b *builder) attachments() ([]loadedAttachment, error) {
	in := b.in
	budget := b.lim.MaxAttachmentBytes
	var out []loadedAttachment
	take := func(field string, a Attachment, data []byte, source string) error {
		if int64(len(data)) > budget {
			return deny(ReasonTooLarge, field, "attachments exceed %d bytes in total", b.lim.MaxAttachmentBytes)
		}
		budget -= int64(len(data))
		out = append(out, loadedAttachment{info: describe(a, data, source), data: data})
		return nil
	}
	for i, a := range in.Attachments {
		field := fmt.Sprintf("attachments[%d]", i)
		switch {
		case a.Path != "" && a.Data != nil:
			return nil, deny(ReasonInvalidInput, field, "data and path are mutually exclusive")
		case a.Path != "":
			data, name, err := readAttachFile(field, in.AttachDir, a.Path, budget)
			if err != nil {
				return nil, err
			}
			if a.Filename == "" {
				a.Filename = name
			}
			if err := take(field, a, data, SourceFile); err != nil {
				return nil, err
			}
		default:
			if err := take(field, a, a.Data, SourceBlob); err != nil {
				return nil, err
			}
		}
	}
	if in.Kind == KindForward && in.IncludeAttachments {
		for i, a := range in.Original.Attachments {
			if err := take(fmt.Sprintf("original.attachments[%d]", i), a, a.Data, SourceForward); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range out {
		b.msg.Attachments = append(b.msg.Attachments, a.info)
	}
	return out, nil
}

// describe erkennt den Typ per Magic Bytes und vergleicht mit der Angabe.
func describe(a Attachment, data []byte, source string) AttachmentInfo {
	name := attachName(a.Filename)
	head, tail := data, data
	if len(head) > magic.HeadSize {
		head = head[:magic.HeadSize]
	}
	if len(tail) > magic.TailSize {
		tail = tail[len(tail)-magic.TailSize:]
	}
	det := magic.Detect(head, tail, int64(len(data)))
	chk := magic.Check(a.ContentType, name, det)
	ct := det.MIME
	if ct == "" {
		ct = magic.OctetStream
	}
	sum := sha256.Sum256(data)
	return AttachmentInfo{
		Filename: name, ContentType: ct, Declared: magic.Normalize(a.ContentType),
		Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Source: source,
		Mismatch: chk.Mismatch, Executable: chk.Executable || det.Executable,
	}
}

// attachName bereinigt einen Dateinamen: nur der letzte Pfadteil, ohne
// unsichtbare und Steuerzeichen, höchstens 255 Zeichen.
func attachName(s string) string {
	s = sanitize.Text(s)
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "." || s == ".." {
		return ""
	}
	if utf8.RuneCountInString(s) > 255 {
		s = string([]rune(s)[:255])
	}
	return s
}

// readAttachFile liest eine Datei, die (aufgelöst) unterhalb von dir liegen
// muss (E10). Relative Pfade gelten relativ zu dir. Symlinks und „..", die
// nach außen führen, sind ReasonAttachOutsideDir; geöffnet wird über
// os.Root, damit ein zwischen Prüfung und Öffnen getauschter Symlink nicht
// nach außen führt. Höchstens budget Bytes, sonst ReasonTooLarge.
func readAttachFile(field, dir, p string, budget int64) ([]byte, string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, "", deny(ReasonAttachOutsideDir, field, "no attach_dir configured")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", deny(ReasonAttachOutsideDir, field, "attach_dir cannot be resolved")
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return nil, "", deny(ReasonAttachNotFound, field, "attach_dir does not exist")
	}
	target := p
	if !filepath.IsAbs(target) {
		target = filepath.Join(absDir, target)
	}
	target = filepath.Clean(target)
	if !within(absDir, target) && !within(realDir, target) {
		return nil, "", deny(ReasonAttachOutsideDir, field, "path is outside attach_dir")
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", deny(ReasonAttachNotFound, field, "file not found")
		}
		return nil, "", deny(ReasonAttachOutsideDir, field, "path cannot be resolved")
	}
	rel, err := filepath.Rel(realDir, realTarget)
	if err != nil || !within(realDir, realTarget) || rel == "." {
		return nil, "", deny(ReasonAttachOutsideDir, field, "path resolves outside attach_dir")
	}
	root, err := os.OpenRoot(realDir)
	if err != nil {
		return nil, "", deny(ReasonAttachNotFound, field, "attach_dir cannot be opened")
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", deny(ReasonAttachNotFound, field, "file not found")
		}
		return nil, "", deny(ReasonAttachOutsideDir, field, "path escapes attach_dir")
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, "", deny(ReasonAttachNotFound, field, "not a regular file")
	}
	if st.Size() > budget {
		return nil, "", deny(ReasonTooLarge, field, "attachments exceed the limit in total")
	}
	data, err := io.ReadAll(io.LimitReader(f, budget+1))
	if err != nil {
		return nil, "", fmt.Errorf("compose: read %s: %w", field, err)
	}
	if int64(len(data)) > budget {
		return nil, "", deny(ReasonTooLarge, field, "attachments exceed the limit in total")
	}
	return data, filepath.Base(target), nil
}

// within: p liegt (lexikalisch) unterhalb von dir.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
