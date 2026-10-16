package importer

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// Lesen einer Nachricht aus einem Datenstrom (mbox oder Maildir-Datei),
// streamend: Kopf bis zur Leerzeile im Speicher (höchstens maxHeaderBytes),
// der Rest wird zeilenweise weitergereicht. Dabei
//
//   - endet eine mbox-Nachricht vor der nächsten Trennzeile (die Zeile
//     bleibt ungelesen für die nächste Nachricht),
//   - wird das mbox-Quoting zurückgenommen (">From " → "From "),
//   - werden Leerzeilen am Ende weggelassen und die letzte Zeile mit genau
//     einem Zeilenende abgeschlossen. In mbox gehört die Leerzeile vor dem
//     nächsten "From " zum Rahmen; wie viele Zeilenenden die Nachricht
//     selbst hatte, ist nicht eindeutig. Der Abgleich per content_hash
//     prüft deshalb Varianten (hash.go).

const (
	readBufSize    = 256 << 10
	maxHeaderBytes = 1 << 20
	// maxHeldBlank begrenzt zurückgehaltene Leerzeilen; mehr werden als
	// Inhalt weitergegeben (Speicher bleibt flach, auch bei Unsinn).
	maxHeldBlank = 64 << 10
	// peekSeparator: so weit wird eine mögliche Trennzeile vorausgelesen.
	peekSeparator = 1024
)

// quoting ist die Art des mbox-Quotings.
type quoting int

const (
	quoteNone   quoting = iota
	quoteMboxo          // Thunderbird: nur ">From " → "From "
	quoteMboxrd         // ">>From " → ">From " usw.
)

// source ist der gepufferte Datenstrom mit Position (Bytes ab Dateianfang).
type source struct {
	br  *bufio.Reader
	pos int64
}

func newSource(r io.Reader, pos int64) *source {
	return &source{br: bufio.NewReaderSize(r, readBufSize), pos: pos}
}

// peekLine liefert die nächste Zeile, ohne sie zu lesen (höchstens n Bytes;
// ok=false, wenn innerhalb von n kein Zeilenende und kein Dateiende liegt).
func (s *source) peekLine(n int) ([]byte, bool) {
	p, err := s.br.Peek(n)
	if i := bytes.IndexByte(p, '\n'); i >= 0 {
		return p[:i+1], true
	}
	if err != nil && len(p) > 0 {
		return p, true // letzte Zeile ohne Zeilenende
	}
	return nil, false
}

// startsWithFrom meldet, ob die nächsten Bytes "From " sind.
func (s *source) startsWithFrom() bool {
	p, _ := s.br.Peek(5)
	return len(p) == 5 && string(p) == "From "
}

// skipLine liest eine Zeile vollständig (auch überlange).
func (s *source) skipLine() ([]byte, error) {
	var first []byte
	for {
		line, err := s.br.ReadSlice('\n')
		s.pos += int64(len(line))
		if first == nil {
			first = append([]byte(nil), line[:min(len(line), peekSeparator)]...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err == io.EOF && len(line) == 0 && len(first) == 0 {
			return nil, io.EOF
		}
		if err != nil && err != io.EOF {
			return first, err
		}
		return first, nil
	}
}

// msgReader liest eine Nachricht (Kopf und Rumpf) aus source.
type msgReader struct {
	src       *source
	mbox      bool // Ende vor einer Trennzeile
	quote     quoting
	lineStart bool
	ended     bool // keine Zeilen mehr
	finished  bool // Ende verarbeitet (Zeilenende ergänzt)

	out      []byte
	outOff   int
	held     []byte // zurückgehaltene Leerzeilen
	lastEOL  string // Zeilenende der letzten Inhaltszeile
	partial  bool   // letzte ausgegebene Zeile ohne Zeilenende
	content  bool   // überhaupt Inhalt ausgegeben
	readErr  error
	consumed int64 // gelesene Bytes der Quelle (ohne Trennzeile)
}

func newMsgReader(src *source, mbox bool, q quoting) *msgReader {
	return &msgReader{src: src, mbox: mbox, quote: q, lineStart: true}
}

// chunk liefert das nächste Stück: eine ganze Zeile (complete) oder einen
// Teil einer überlangen Zeile. atStart: das Stück beginnt eine Zeile.
func (m *msgReader) chunk() (b []byte, complete, atStart bool, err error) {
	if m.ended {
		return nil, false, false, io.EOF
	}
	atStart = m.lineStart
	if atStart && m.mbox && m.src.startsWithFrom() {
		if line, ok := m.src.peekLine(peekSeparator); ok && isSeparator(line) {
			m.ended = true
			return nil, false, false, io.EOF
		}
	}
	line, rerr := m.src.br.ReadSlice('\n')
	m.src.pos += int64(len(line))
	m.consumed += int64(len(line))
	switch {
	case errors.Is(rerr, bufio.ErrBufferFull):
		m.lineStart = false
	case rerr == nil:
		m.lineStart, complete = true, true
	case rerr == io.EOF:
		m.ended = true
		if len(line) == 0 {
			return nil, false, false, io.EOF
		}
	default:
		m.ended = true
		return nil, false, false, rerr
	}
	if atStart {
		line = unquote(line, m.quote)
	}
	return line, complete, atStart, nil
}

// unquote nimmt das mbox-Quoting einer Zeile zurück (in place, Slice).
func unquote(line []byte, q quoting) []byte {
	switch q {
	case quoteMboxo:
		if bytes.HasPrefix(line, []byte(">From ")) {
			return line[1:]
		}
	case quoteMboxrd:
		i := 0
		for i < len(line) && line[i] == '>' {
			i++
		}
		if i > 0 && bytes.HasPrefix(line[i:], []byte("From ")) {
			return line[1:]
		}
	}
	return line
}

func isBlank(line []byte) bool {
	return len(line) == 1 && line[0] == '\n' || len(line) == 2 && line[0] == '\r' && line[1] == '\n'
}

func eolOf(line []byte) string {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// readHeader liest den Kopf bis einschließlich der Leerzeile (bzw. bis zum
// Ende der Nachricht). tooLong: Grenze erreicht, der Rest folgt im Rumpf.
func (m *msgReader) readHeader() (hdr []byte, tooLong bool, err error) {
	for {
		b, complete, atStart, err := m.chunk()
		if err == io.EOF {
			if len(hdr) > 0 && hdr[len(hdr)-1] != '\n' {
				eol := m.lastEOL
				if eol == "" {
					eol = "\n"
				}
				hdr = append(hdr, eol...)
			}
			return hdr, false, nil
		}
		if err != nil {
			return hdr, false, err
		}
		hdr = append(hdr, b...)
		if complete {
			m.lastEOL = eolOf(b)
		}
		if complete && atStart && isBlank(b) {
			return hdr, false, nil
		}
		if len(hdr) > maxHeaderBytes {
			m.partial = !complete
			m.content = true
			return hdr, true, nil
		}
	}
}

// Read liefert den Rumpf (nach readHeader). Es füllt p so weit wie möglich
// (viele Zeilen je Aufruf): Ein Read je Zeile hieße bei base64-Anhängen ein
// Schreibaufruf je 78 Bytes im Blob-Store.
func (m *msgReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if m.outOff >= len(m.out) {
			if m.readErr != nil {
				if n > 0 {
					return n, nil
				}
				return 0, m.readErr
			}
			if m.finished {
				break
			}
			m.out, m.outOff = m.out[:0], 0
			if err := m.fill(); err != nil {
				if n > 0 {
					return n, nil
				}
				return 0, err
			}
			continue
		}
		k := copy(p[n:], m.out[m.outOff:])
		m.outOff += k
		n += k
	}
	if n == 0 && m.finished {
		return 0, io.EOF
	}
	return n, nil
}

func (m *msgReader) fill() error {
	b, complete, atStart, err := m.chunk()
	if err == io.EOF {
		// Leerzeilen am Ende fallen weg; letzte Zeile bekommt ein Zeilenende.
		m.held = m.held[:0]
		if m.partial {
			eol := m.lastEOL
			if eol == "" {
				eol = "\n"
			}
			m.out = append(m.out, eol...)
			m.partial = false
		}
		m.finished = true
		return nil
	}
	if err != nil {
		m.readErr = err
		return err
	}
	if complete && atStart && isBlank(b) {
		m.held = append(m.held, b...)
		if len(m.held) > maxHeldBlank {
			m.out = append(m.out, m.held...)
			m.held = m.held[:0]
		}
		return nil
	}
	m.out = append(m.out, m.held...)
	m.held = m.held[:0]
	m.out = append(m.out, b...)
	m.content = true
	m.partial = !complete
	if complete {
		m.lastEOL = eolOf(b)
	}
	return nil
}

// drain liest den Rest der Nachricht ungenutzt.
func (m *msgReader) drain() error {
	for !m.ended {
		if _, _, _, err := m.chunk(); err != nil && err != io.EOF {
			return err
		}
	}
	m.finished = true
	return nil
}
