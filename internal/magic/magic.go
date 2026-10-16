// Package magic erkennt den tatsächlichen Typ eines Anhangs an seinen
// ersten und letzten Bytes (Magic Bytes) und vergleicht ihn mit der Angabe
// der Mail (Content-Type und Dateiendung; docs/sicherheit.md Abschnitt 7).
//
// Archive werden nie entpackt: Für ZIP-basierte Formate (DOCX, XLSX, PPTX,
// ODF, JAR) liest Detect nur die unkomprimiert gespeicherten Dateinamen
// aus lokalen Köpfen und dem zentralen Verzeichnis am Ende. Dieses Paket
// importiert kein archive/* und kein compress/* (Test).
package magic

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"strings"
)

// Größen der Puffer für Kopf und Ende.
const (
	HeadSize = 64 << 10
	TailSize = 64 << 10
)

// Typen, die Detect liefert (über net/http.DetectContentType hinaus).
const (
	OctetStream = "application/octet-stream"
	Empty       = "application/x-empty" // Teil ohne Inhalt
	PDF         = "application/pdf"
	ZIP         = "application/zip"
	DOCX        = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	XLSX        = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	PPTX        = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	JAR         = "application/java-archive"
	OLE         = "application/x-ole-storage" // alte Office-Dateien, .msg, .msi
	EXE         = "application/x-msdownload"
	ELF         = "application/x-executable"
	MachO       = "application/x-mach-binary"
	Script      = "text/x-shellscript"
	Shortcut    = "application/x-ms-shortcut"
	RTF         = "application/rtf"
	SevenZip    = "application/x-7z-compressed"
	RAR         = "application/vnd.rar"
	GZIP        = "application/gzip"
	CAB         = "application/vnd.ms-cab-compressed"
	XZ          = "application/x-xz"
	BZIP2       = "application/x-bzip2"
	ZSTD        = "application/zstd"
	ISO         = "application/x-iso9660-image"
	RFC822      = "message/rfc822"
	TextPlain   = "text/plain"
	TextHTML    = "text/html"
)

// Type ist ein erkannter Typ.
type Type struct {
	MIME       string `json:"mime"`
	Executable bool   `json:"executable,omitempty"` // Programm oder Skript
	Archive    bool   `json:"archive,omitempty"`    // Container mit weiteren Dateien
	Text       bool   `json:"text,omitempty"`
}

// Sniffer sammelt beim Schreiben Kopf und Ende eines Datenstroms (z. B.
// per io.TeeReader beim Speichern eines Anhangs) und erkennt danach den Typ.
type Sniffer struct {
	head []byte
	tail []byte // Ringpuffer der letzten TailSize Bytes
	pos  int    // Schreibposition im Ring
	n    int64
}

// Write nimmt Bytes auf; es scheitert nie.
func (s *Sniffer) Write(p []byte) (int, error) {
	s.n += int64(len(p))
	if rest := HeadSize - len(s.head); rest > 0 {
		k := min(rest, len(p))
		s.head = append(s.head, p[:k]...)
	}
	q := p
	if len(q) > TailSize {
		q = q[len(q)-TailSize:]
	}
	for _, b := range q {
		if len(s.tail) < TailSize {
			s.tail = append(s.tail, b)
			continue
		}
		s.tail[s.pos] = b
		s.pos = (s.pos + 1) % TailSize
	}
	return len(p), nil
}

// Size ist die Zahl der geschriebenen Bytes.
func (s *Sniffer) Size() int64 { return s.n }

// Detect erkennt den Typ des Geschriebenen.
func (s *Sniffer) Detect() Type {
	tail := s.tail
	if len(tail) == TailSize && s.pos > 0 {
		tail = append(append([]byte{}, s.tail[s.pos:]...), s.tail[:s.pos]...)
	}
	return Detect(s.head, tail, s.n)
}

// Detect erkennt den Typ aus Kopf (Anfang der Datei) und Ende (letzte
// Bytes; darf leer sein oder sich mit head überschneiden).
func Detect(head, tail []byte, size int64) Type {
	if size == 0 || len(head) == 0 {
		return Type{MIME: Empty}
	}
	has := func(prefix string) bool { return bytes.HasPrefix(head, []byte(prefix)) }
	switch {
	case has("MZ"):
		return Type{MIME: EXE, Executable: true}
	case has("\x7fELF"):
		return Type{MIME: ELF, Executable: true}
	case has("\xfe\xed\xfa\xce"), has("\xfe\xed\xfa\xcf"), has("\xce\xfa\xed\xfe"), has("\xcf\xfa\xed\xfe"):
		return Type{MIME: MachO, Executable: true}
	case has("#!"):
		return Type{MIME: Script, Executable: true, Text: true}
	case has("L\x00\x00\x00\x01\x14\x02\x00"):
		return Type{MIME: Shortcut, Executable: true}
	case has("%PDF-"):
		return Type{MIME: PDF}
	case has("PK\x03\x04"), has("PK\x05\x06"), has("PK\x07\x08"):
		return zipType(head, tail)
	case has("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"):
		return Type{MIME: OLE}
	case has(`{\rtf`):
		return Type{MIME: RTF}
	case has("7z\xbc\xaf\x27\x1c"):
		return Type{MIME: SevenZip, Archive: true}
	case has("Rar!\x1a\x07"):
		return Type{MIME: RAR, Archive: true}
	case has("\x1f\x8b"):
		return Type{MIME: GZIP, Archive: true}
	case has("MSCF\x00\x00\x00\x00"):
		return Type{MIME: CAB, Archive: true}
	case has("\xfd7zXZ\x00"):
		return Type{MIME: XZ, Archive: true}
	case has("BZh"):
		return Type{MIME: BZIP2, Archive: true}
	case has("\x28\xb5\x2f\xfd"):
		return Type{MIME: ZSTD, Archive: true}
	case len(head) > 0x8006 && string(head[0x8001:0x8006]) == "CD001":
		return Type{MIME: ISO, Archive: true}
	}
	ct := http.DetectContentType(head[:min(len(head), 512)])
	mime, _, _ := strings.Cut(ct, ";")
	mime = strings.TrimSpace(mime)
	switch mime {
	case "application/x-gzip":
		return Type{MIME: GZIP, Archive: true}
	case "application/x-rar-compressed":
		return Type{MIME: RAR, Archive: true}
	case "text/plain":
		if looksLikeMessage(head) {
			return Type{MIME: RFC822, Text: true}
		}
		return Type{MIME: TextPlain, Text: true}
	case "text/html", "text/xml":
		return Type{MIME: mime, Text: true}
	}
	return Type{MIME: mime}
}

// looksLikeMessage: Text beginnt mit typischen Kopfzeilen einer Mail.
func looksLikeMessage(head []byte) bool {
	lines := bytes.SplitN(head, []byte("\n"), 12)
	found := 0
	for _, l := range lines {
		l = bytes.TrimRight(l, "\r")
		if len(l) == 0 {
			break
		}
		name, _, ok := bytes.Cut(l, []byte(":"))
		if !ok {
			if l[0] == ' ' || l[0] == '\t' {
				continue // Fortsetzungszeile
			}
			return false
		}
		switch strings.ToLower(string(name)) {
		case "from", "to", "subject", "date", "message-id", "received", "return-path", "mime-version", "cc", "reply-to":
			found++
		}
	}
	return found >= 3
}

// zipType liest die Namen der Einträge (nur Köpfe, nichts entpackt) und
// unterscheidet ZIP, OOXML, ODF und JAR.
func zipType(head, tail []byte) Type {
	// ODF: erster Eintrag „mimetype", unkomprimiert; Inhalt ist der Typ.
	if len(head) > 30 && binary.LittleEndian.Uint16(head[8:10]) == 0 {
		nameLen := int(binary.LittleEndian.Uint16(head[26:28]))
		extraLen := int(binary.LittleEndian.Uint16(head[28:30]))
		if nameLen == 8 && 30+8 <= len(head) && string(head[30:38]) == "mimetype" {
			// Größe steht nicht immer im Kopf (Datendeskriptor): Inhalt bis
			// zum ersten Zeichen außerhalb eines Typnamens lesen.
			start := 38 + extraLen
			if start < len(head) {
				rest := head[start:min(len(head), start+100)]
				end := 0
				for end < len(rest) && (rest[end] == '.' || rest[end] == '/' || rest[end] == '-' ||
					(rest[end] >= 'a' && rest[end] <= 'z') || (rest[end] >= '0' && rest[end] <= '9')) {
					end++
				}
				if mt := string(rest[:end]); strings.HasPrefix(mt, "application/vnd.oasis.opendocument.") {
					return Type{MIME: mt}
				}
			}
		}
	}
	names := entryNames(head, "PK\x03\x04", 30, 26)
	names = append(names, entryNames(tail, "PK\x01\x02", 46, 28)...)
	var word, xl, ppt, manifest, contentTypes bool
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "word/"):
			word = true
		case strings.HasPrefix(n, "xl/"):
			xl = true
		case strings.HasPrefix(n, "ppt/"):
			ppt = true
		case n == "META-INF/MANIFEST.MF":
			manifest = true
		case n == "[Content_Types].xml":
			contentTypes = true
		}
	}
	switch {
	case word && !xl && !ppt:
		return Type{MIME: DOCX}
	case xl && !word && !ppt:
		return Type{MIME: XLSX}
	case ppt && !word && !xl:
		return Type{MIME: PPTX}
	case manifest && !contentTypes:
		return Type{MIME: JAR, Executable: true, Archive: true}
	}
	return Type{MIME: ZIP, Archive: true}
}

// entryNames sucht Einträge mit der Signatur sig und liest ihren Namen
// (Länge an Versatz lenOff, Name ab Versatz nameOff). Höchstens 2.000.
func entryNames(buf []byte, sig string, nameOff, lenOff int) []string {
	var out []string
	for i := 0; i < len(buf) && len(out) < 2000; {
		k := bytes.Index(buf[i:], []byte(sig))
		if k < 0 {
			break
		}
		p := i + k
		if p+nameOff <= len(buf) {
			n := int(binary.LittleEndian.Uint16(buf[p+lenOff : p+lenOff+2]))
			if n > 0 && n < 1024 && p+nameOff+n <= len(buf) {
				out = append(out, string(buf[p+nameOff:p+nameOff+n]))
			}
		}
		i = p + len(sig)
	}
	return out
}
