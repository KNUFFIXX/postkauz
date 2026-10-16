package importer

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// mbox: Nachrichten hintereinander, jede beginnt mit einer Trennzeile
// "From <absender> <datum>" (Thunderbird: "From - Thu Oct 15 10:00:00 2026").
// Zeilen im Text, die mit "From " beginnen, sind gequotet (">From ").
// Thunderbird (berkeleystore) schreibt vor den Kopf der Nachricht eigene
// Zeilen X-Mozilla-Status, X-Mozilla-Status2 und X-Mozilla-Keys; sie werden
// gelesen (Flags) und entfernt, damit die Nachricht der Kopie des Servers
// gleicht.

// ErrNotMbox: Die Datei beginnt nicht mit einer Trennzeile.
var ErrNotMbox = errors.New("not an mbox file (it does not start with a \"From \" line)")

// separatorRE erkennt eine Trennzeile: "From ", ein Wort (Absender oder
// "-"), dann ein Datum mit Uhrzeit. Eine nicht gequotete Zeile wie
// "From the desk of …" im Text gilt so nicht als Trennzeile.
var separatorRE = regexp.MustCompile(`^From \S+ +.*\d{1,2}:\d{2}`)

func isSeparator(line []byte) bool {
	return separatorRE.Match(bytes.TrimRight(line, "\r\n"))
}

// separatorDate liest das Datum der Trennzeile (asctime, Ortszeit; ohne
// Datum Nullzeit). Es ersetzt ein fehlendes Date wie INTERNALDATE beim Sync.
func separatorDate(line []byte) time.Time {
	f := strings.Fields(strings.TrimSpace(string(line)))
	if len(f) < 3 {
		return time.Time{}
	}
	rest := strings.Join(f[2:], " ")
	for _, layout := range []string{"Mon Jan 2 15:04:05 2006", "Mon Jan 2 15:04:05 -0700 2006", "Mon Jan 2 15:04:05 MST 2006",
		"Mon, 2 Jan 2006 15:04:05 -0700"} {
		if t, err := time.ParseInLocation(layout, rest, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// mozStatus sind die Thunderbird-Angaben einer Nachricht.
type mozStatus struct {
	Present bool     // X-Mozilla-Status vorhanden
	Flags   uint32   // X-Mozilla-Status
	Flags2  uint32   // X-Mozilla-Status2
	Keys    []string // X-Mozilla-Keys (Schlagwörter)
}

// Bits von X-Mozilla-Status (nsMsgMessageFlags).
const (
	mozRead     = 0x0001
	mozReplied  = 0x0002
	mozMarked   = 0x0004
	mozExpunged = 0x0008 // gelöscht markiert, bis Thunderbird kompaktiert
	mozPartial  = 0x0400 // nur teilweise heruntergeladen
)

func (s mozStatus) deleted() bool { return s.Flags&mozExpunged != 0 }

// mozHeaders sind die Thunderbird-Zeilen, die entfernt werden.
var mozHeaders = map[string]bool{"x-mozilla-status": true, "x-mozilla-status2": true, "x-mozilla-keys": true}

// stripMozilla entfernt die Thunderbird-Zeilen (samt Fortsetzungszeilen)
// aus dem Kopf und liest ihre Werte. Andere Zeilen bleiben Byte für Byte.
func stripMozilla(hdr []byte) ([]byte, mozStatus) {
	var st mozStatus
	if !bytes.Contains(bytes.ToLower(hdr[:min(len(hdr), 64<<10)]), []byte("x-mozilla-")) {
		return hdr, st
	}
	out := make([]byte, 0, len(hdr))
	skipping := false
	var field string
	var value []byte
	flush := func() {
		if field == "" {
			return
		}
		v := strings.TrimSpace(string(value))
		switch field {
		case "x-mozilla-status":
			st.Present = true
			if n, err := strconv.ParseUint(v, 16, 32); err == nil {
				st.Flags = uint32(n)
			}
		case "x-mozilla-status2":
			if n, err := strconv.ParseUint(v, 16, 32); err == nil {
				st.Flags2 = uint32(n)
			}
		case "x-mozilla-keys":
			st.Keys = append(st.Keys, strings.Fields(v)...)
		}
		field, value = "", nil
	}
	rest := hdr
	inHeader := true
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		line := rest
		if i >= 0 {
			line = rest[:i+1]
		}
		rest = rest[len(line):]
		if !inHeader {
			out = append(out, line...)
			continue
		}
		if isBlank(line) {
			flush()
			inHeader = false
			out = append(out, line...)
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if skipping {
				value = append(value, line...)
				continue
			}
			out = append(out, line...)
			continue
		}
		flush()
		skipping = false
		if c := bytes.IndexByte(line, ':'); c > 0 {
			name := strings.ToLower(strings.TrimSpace(string(line[:c])))
			if mozHeaders[name] {
				skipping, field = true, name
				value = append(value, line[c+1:]...)
				continue
			}
		}
		out = append(out, line...)
	}
	flush()
	return out, st
}

// mboxMessage ist eine Nachricht aus einer mbox-Datei.
type mboxMessage struct {
	Offset  int64     // Position der Trennzeile
	Date    time.Time // aus der Trennzeile
	Header  []byte    // Kopf ohne Thunderbird-Zeilen, mit Leerzeile
	TooLong bool      // Kopf über der Grenze (keine Message-ID-Prüfung)
	Status  mozStatus
	Body    *msgReader
}

// mboxScanner liest eine mbox-Datei Nachricht für Nachricht.
type mboxScanner struct {
	src   *source
	cur   *msgReader
	quote quoting
	// Thunderbird: Trennzeile "From - " oder X-Mozilla-Status gesehen.
	Thunderbird bool
	started     bool
}

// newMboxScanner liest ab pos (Dateianfang oder Beginn einer Trennzeile).
func newMboxScanner(r io.Reader, pos int64) *mboxScanner {
	return &mboxScanner{src: newSource(r, pos)}
}

// Pos ist die Position der nächsten Nachricht (nach Next und dem Lesen des
// Rumpfs der vorigen; bzw. das Dateiende).
func (s *mboxScanner) Pos() int64 { return s.src.pos }

// Next liefert die nächste Nachricht; io.EOF am Ende. Ein nicht gelesener
// Rumpf der vorigen Nachricht wird übersprungen.
func (s *mboxScanner) Next() (*mboxMessage, error) {
	if s.cur != nil {
		if err := s.cur.drain(); err != nil {
			return nil, err
		}
		s.cur = nil
	}
	off := s.src.pos
	if !s.src.startsWithFrom() {
		if _, err := s.src.br.Peek(1); err == io.EOF {
			return nil, io.EOF
		}
		if !s.started {
			return nil, ErrNotMbox
		}
		// Kann nicht vorkommen (ein Rumpf endet nur vor einer Trennzeile).
		return nil, errors.New("mbox: lost track of message boundaries")
	}
	sep, err := s.src.skipLine()
	if err != nil {
		return nil, err
	}
	first := !s.started
	s.started = true
	m := &mboxMessage{Offset: off, Date: separatorDate(sep)}
	mr := newMsgReader(s.src, true, s.quote)
	hdr, tooLong, err := mr.readHeader()
	if err != nil {
		return nil, err
	}
	m.Header, m.Status = stripMozilla(hdr)
	m.TooLong = tooLong
	if first {
		s.Thunderbird = bytes.HasPrefix(sep, []byte("From - ")) || m.Status.Present
		s.quote = quoteMboxrd
		if s.Thunderbird {
			s.quote = quoteMboxo
		}
		mr.quote = s.quote
	}
	m.Body = mr
	s.cur = mr
	return m, nil
}

// separatorAt prüft, ob an pos eine Trennzeile beginnt (Wiederaufnahme).
func separatorAt(r io.ReaderAt, pos int64) bool {
	buf := make([]byte, peekSeparator)
	n, _ := r.ReadAt(buf, pos)
	buf = buf[:n]
	if i := bytes.IndexByte(buf, '\n'); i >= 0 {
		buf = buf[:i+1]
	}
	return isSeparator(buf)
}
