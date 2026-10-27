package corpus

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// MaildirInfo trennt in Maildir-Namen die Flags ab („<name>:2,S“). Unter
// Windows ist „:“ in Dateinamen ein NTFS-Datenstrom; dort gilt „!“, wie
// es Maildir-Programme unter Windows nutzen und der Importer liest
// (A12.4).
var MaildirInfo = func() string {
	if runtime.GOOS == "windows" {
		return "!"
	}
	return ":"
}()

// Ablagen für den Import (S1.10): der Korpus als Thunderbird-Profil (mbox,
// berkeleystore) und als Maildir. Was Thunderbird bzw. ein Maildir-Client
// dazutut, wird nachgebildet; die erwarteten Zählungen stehen in
// ImportExpect.
//
// Thunderbird (mbox):
//   - je Ordner eine Datei, Unterordner in "<ordner>.sbd/", daneben .msf
//     (Index, hier Füllbytes) und eine fremde Datei (msgFilterRules.dat);
//   - Trennzeile "From - <asctime>", davor eine Leerzeile;
//   - vor dem Kopf X-Mozilla-Status (alle Bits kommen vor), -Status2,
//     -Keys (80 Leerzeichen oder Schlagwörter);
//   - Zeilen "From " im Text als ">From " (mboxo);
//   - Ordnernamen in IMAP-UTF-7 ("Entw&APw-rfe" für „Entwürfe");
//   - gelöscht markierte Nachrichten (Bit 0x0008) bleiben in der Datei;
//   - ein Gmail-artiges Label „Wichtig" mit Kopien jeder zehnten Nachricht;
//   - Sonderfälle: Nachricht nur aus Kopf, letzte Nachricht ohne
//     Zeilenende, überlange Zeile (300 KiB), "From "-Zeilen im Text,
//     ein leerer Ordner (leere Datei).

// ImportOptions steuert WriteThunderbird und WriteMaildir.
type ImportOptions struct {
	// LF schreibt Zeilenenden LF statt CRLF (Thunderbird unter Windows: CRLF).
	LF bool
	// Only wählt Nachrichten des Korpus (Index in Manifest-Reihenfolge);
	// nil = alle. Sonderfälle und gelöscht markierte kommen immer dazu.
	Only func(i int) bool
}

// ImportExpect sind die erwarteten Zählungen eines Imports in ein leeres
// Konto (bzw. mit Abzug der vorhandenen Nachrichten).
type ImportExpect struct {
	New      int // verschiedene Nachrichten (gelöscht markierte nicht)
	Copies   int // weitere Kopien derselben Nachricht (existing)
	Deleted  int // gelöscht markiert (Thunderbird 0x0008, Maildir T) bzw. tmp/
	Sources  int // Dateien bzw. Maildir-Ordner mit Nachrichten
	Included []int
	// Folders: Ordner → Message-IDs (gelöscht markierte nicht).
	Folders map[string][]string
	// Flags: Message-ID → erwartete Flags nach einem Import in ein leeres
	// Konto (seen, flagged, answered, draft; vereinigt über die Kopien).
	Flags map[string][]string
	// Keywords: Message-ID → Schlagwörter (Thunderbird X-Mozilla-Keys).
	Keywords map[string][]string
	// FromLines: Message-ID einer Nachricht mit "From "-Zeilen im Text und
	// die Zeilen, die nach dem Import im Text stehen müssen.
	FromLines map[string][]string
}

// Sonderfälle (Message-IDs).
const (
	ExtraFromLines  = "extra-fromlines@import.example.org"
	ExtraHeaderOnly = "extra-headeronly@import.example.org"
	ExtraNoEOL      = "extra-noeol@import.example.org"
	ExtraKeywords   = "extra-keywords@import.example.org"
	ExtraLongLine   = "extra-longline@import.example.org"
)

// Gelöscht markierte Nachrichten (nie zu importieren).
func deletedID(n int) string { return fmt.Sprintf("extra-deleted-%d@import.example.org", n) }

// Ordner der Ablage. Umlaute nur im dekodierten Namen.
const (
	importDrafts    = "Entwürfe"
	importDraftsEnc = "Entw&APw-rfe"
	importLabel     = "Wichtig"
)

type importMsg struct {
	raw     []byte
	mid     string
	folder  string
	status  uint32 // X-Mozilla-Status
	keys    string
	flags   []string // erwartet (seen, flagged, answered)
	maildir string   // Flag-Buchstaben
	deleted bool
	date    time.Time
}

func crlfToLF(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

func extraMessages() []importMsg {
	date := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	hdr := func(mid, subject string) string {
		return "From: Grete Größe <grete@groesse.example.net>\r\nTo: verwaltung@example.org\r\nSubject: " + subject +
			"\r\nDate: Wed, 04 Mar 2026 10:00:00 +0100\r\nMessage-ID: <" + mid + ">\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n"
	}
	long := strings.Repeat("Zählerstand 12345 ", 300<<10/18)
	out := []importMsg{
		{mid: ExtraFromLines, raw: []byte(hdr(ExtraFromLines, "Entwurf: Aushang Treppenhaus") + "\r\n" +
			"Liebe Mieter,\r\n\r\nFrom the desk of the caretaker: Bitte die Haustür schließen.\r\n" +
			"From - Mon Jan  5 10:00:00 2026\r\n\r\nGruß\r\n"), status: 0x0001, flags: []string{"seen"}, maildir: "S"},
		{mid: ExtraHeaderOnly, raw: []byte(hdr(ExtraHeaderOnly, "Nur ein Kopf"))},
		{mid: ExtraKeywords, raw: []byte(hdr(ExtraKeywords, "Mit Schlagwörtern") + "\r\nText mit Schlagwörtern.\r\n"),
			status: 0x0004 | 0x0100, keys: "$label1 todo", flags: []string{"flagged"}, maildir: "F"},
		{mid: ExtraLongLine, raw: []byte(hdr(ExtraLongLine, "Sehr lange Zeile") + "\r\n" + long + "\r\nEnde.\r\n"),
			status: 0x0002 | 0x0010 | 0x2000, flags: []string{"answered"}, maildir: "R"},
		{mid: ExtraNoEOL, raw: []byte(hdr(ExtraNoEOL, "Ohne Zeilenende") + "\r\nLetzte Zeile ohne Zeilenende"),
			status: 0x0001 | 0x0002 | 0x0004, flags: []string{"seen", "flagged", "answered"}, maildir: "FRS"},
	}
	for i := range out {
		out[i].folder, out[i].date = importDrafts, date
	}
	for n := 1; n <= 3; n++ {
		mid := deletedID(n)
		out = append(out, importMsg{mid: mid, raw: []byte(hdr(mid, "Gelöscht markiert") + "\r\nWeg damit.\r\n"),
			folder: "INBOX", status: 0x0008 | uint32(n&1), deleted: true, maildir: "ST", date: date})
	}
	return out
}

// importSet stellt die Nachrichten der Ablage zusammen und berechnet die
// Erwartung.
// maildir: Das Entwurf-Flag (D) gibt es nur dort, Thunderbird kennt kein Bit.
func (c *Corpus) importSet(o ImportOptions, maildir bool) ([]importMsg, ImportExpect) {
	exp := ImportExpect{Folders: map[string][]string{}, Flags: map[string][]string{}, Keywords: map[string][]string{},
		FromLines: map[string][]string{}}
	var msgs []importMsg
	add := func(m importMsg) {
		msgs = append(msgs, m)
		if m.deleted {
			exp.Deleted++
			return
		}
		if m.mid != "" {
			exp.Folders[m.folder] = append(exp.Folders[m.folder], m.mid)
			for _, f := range m.flags {
				if !slices.Contains(exp.Flags[m.mid], f) {
					exp.Flags[m.mid] = append(exp.Flags[m.mid], f)
				}
			}
		}
	}
	seen := map[string]bool{}
	for i, e := range c.Manifest.Messages {
		if o.Only != nil && !o.Only(i) {
			continue
		}
		if e.MessageID != "" {
			if seen[e.MessageID] {
				continue // doppelte Message-ID im Korpus: nur die erste
			}
			seen[e.MessageID] = true
		}
		exp.Included = append(exp.Included, i)
		// Alle Bits kommen vor: gelesen/beantwortet/markiert aus den Flags
		// des Korpus und dem Index, dazu Bits, die nichts bedeuten dürfen.
		var st uint32
		var flags []string
		md := ""
		if slices.Contains(e.Flags, `\Seen`) {
			st |= 0x0001
			flags = append(flags, "seen")
			md += "S"
		}
		if i%3 == 0 {
			st |= 0x0002
			flags = append(flags, "answered")
			md += "R"
		}
		if slices.Contains(e.Flags, `\Flagged`) {
			st |= 0x0004
			flags = append(flags, "flagged")
			md += "F"
		}
		st |= []uint32{0, 0x0010, 0x0020, 0x0040, 0x0080, 0x0100, 0x0200, 0x1000, 0x2000, 0x4000, 0x8000}[i%11]
		if slices.Contains(e.Flags, `\Draft`) {
			md += "D"
			if maildir {
				flags = append(flags, "draft")
			}
		}
		date, _ := time.Parse(time.RFC3339, e.InternalDate)
		add(importMsg{raw: c.Raw[i], mid: e.MessageID, folder: e.Folder, status: st, flags: flags, maildir: sortFlags(md), date: date})
		exp.New++
		if i%10 == 0 {
			add(importMsg{raw: c.Raw[i], mid: e.MessageID, folder: importLabel, status: 0x0004 | 0x0001,
				flags: []string{"flagged", "seen"}, maildir: "FS", date: date})
			exp.Copies++
		}
	}
	for _, m := range extraMessages() {
		add(m)
		if !m.deleted {
			exp.New++
		}
		if m.keys != "" && !maildir {
			exp.Keywords[m.mid] = strings.Fields(m.keys)
		}
	}
	exp.FromLines[ExtraFromLines] = []string{"From the desk of the caretaker", "From - Mon Jan  5 10:00:00 2026"}
	return msgs, exp
}

func sortFlags(s string) string {
	b := []byte(s)
	slices.Sort(b)
	return string(b)
}

// tbFile ist der Dateipfad eines Ordners im Thunderbird-Profil.
func tbFile(folder string) string {
	parts := strings.Split(folder, "/")
	for i := range parts {
		if parts[i] == importDrafts {
			parts[i] = importDraftsEnc
		}
		if i < len(parts)-1 {
			parts[i] += ".sbd"
		}
	}
	return filepath.Join(parts...)
}

// WriteThunderbird schreibt den Korpus als Thunderbird-Profilordner
// (ImapMail/<server>) nach dir.
func (c *Corpus) WriteThunderbird(dir string, o ImportOptions) (ImportExpect, error) {
	msgs, exp := c.importSet(o, false)
	files := map[string]*bytes.Buffer{}
	var order []string
	for _, m := range msgs {
		f := tbFile(m.folder)
		buf, ok := files[f]
		if !ok {
			buf = &bytes.Buffer{}
			files[f] = buf
			order = append(order, f)
		}
		writeMboxMessage(buf, m, !o.LF)
	}
	// Die Nachricht ohne Zeilenende muss die letzte ihrer Datei sein.
	if b := files[tbFile(importDrafts)]; b != nil {
		trimmed := bytes.TrimRight(b.Bytes(), "\r\n")
		b.Truncate(len(trimmed))
	}
	for _, f := range order {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return exp, err
		}
		if err := os.WriteFile(p, files[f].Bytes(), 0o644); err != nil {
			return exp, err
		}
		// Index daneben (Thunderbird .msf): Füllbytes, muss ignoriert werden.
		if err := os.WriteFile(p+".msf", []byte("// <!-- <mdb:mork:z v=\"1.4\"/> -->\n< <(a=c)> >\n"), 0o644); err != nil {
			return exp, err
		}
	}
	exp.Sources = len(order)
	// Elternordner "Archiv" als leere Datei, ein leerer Ordner und eine
	// fremde Datei.
	for _, p := range []string{"Archiv", "Leer"} {
		if _, err := os.Stat(filepath.Join(dir, p)); os.IsNotExist(err) {
			if err := os.WriteFile(filepath.Join(dir, p), nil, 0o644); err != nil {
				return exp, err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "msgFilterRules.dat"), []byte("version=\"9\"\nlogging=\"no\"\n"), 0o644); err != nil {
		return exp, err
	}
	return exp, nil
}

// writeMboxMessage schreibt eine Nachricht wie Thunderbird: Trennzeile,
// X-Mozilla-Zeilen, Nachricht mit ">From "-Quoting, Leerzeile.
func writeMboxMessage(buf *bytes.Buffer, m importMsg, crlf bool) {
	eol := "\r\n"
	if !crlf {
		eol = "\n"
	}
	fmt.Fprintf(buf, "From - %s%s", m.date.Format("Mon Jan _2 15:04:05 2006"), eol)
	keys := m.keys
	if keys == "" {
		keys = strings.Repeat(" ", 80)
	}
	fmt.Fprintf(buf, "X-Mozilla-Status: %04x%sX-Mozilla-Status2: %08x%sX-Mozilla-Keys: %s%s", m.status&0xffff, eol,
		uint32(0x00010000)*(m.status&1), eol, keys, eol)
	raw := crlfToLF(m.raw)
	lines := bytes.SplitAfter(raw, []byte("\n"))
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		if bytes.HasPrefix(l, []byte("From ")) {
			buf.WriteByte('>')
		}
		if bytes.HasSuffix(l, []byte("\n")) {
			buf.Write(l[:len(l)-1])
			buf.WriteString(eol)
		} else {
			buf.Write(l)
			buf.WriteString(eol)
		}
	}
	buf.WriteString(eol)
}

// WriteMaildir schreibt den Korpus als Maildir++ nach dir: INBOX ist die
// Wurzel (cur/new/tmp), Unterordner ".Archiv.2024" usw.; gelesene
// Nachrichten in cur/ mit ":2,<flags>", ungelesene ohne Flag in new/, eine
// halbe Zustellung in tmp/, zum Löschen markierte mit "T".
func (c *Corpus) WriteMaildir(dir string, o ImportOptions) (ImportExpect, error) {
	msgs, exp := c.importSet(o, true)
	folders := map[string]bool{}
	for n, m := range msgs {
		sub := "INBOX"
		d := dir
		if m.folder != "INBOX" {
			name := strings.ReplaceAll(m.folder, "/", ".")
			name = strings.ReplaceAll(name, importDrafts, importDraftsEnc)
			d = filepath.Join(dir, "."+name)
			sub = m.folder
		}
		if !folders[sub] {
			for _, s := range []string{"cur", "new", "tmp"} {
				if err := os.MkdirAll(filepath.Join(d, s), 0o755); err != nil {
					return exp, err
				}
			}
			folders[sub] = true
		}
		base := fmt.Sprintf("%d.M%dP1.import", 1700000000+n, n)
		raw := m.raw
		if o.LF {
			raw = crlfToLF(raw)
		}
		var p string
		if m.maildir == "" && !m.deleted {
			p = filepath.Join(d, "new", base)
		} else {
			p = filepath.Join(d, "cur", base+MaildirInfo+"2,"+m.maildir)
		}
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			return exp, err
		}
		if !m.date.IsZero() {
			_ = os.Chtimes(p, m.date, m.date)
		}
	}
	// Halbe Zustellung in tmp/ (wird übersprungen und gezählt).
	if err := os.WriteFile(filepath.Join(dir, "tmp", "1800000000.M1P1.import"), []byte("Subject: halb\r\n"), 0o644); err != nil {
		return exp, err
	}
	exp.Deleted++
	exp.Sources = len(folders)
	return exp, nil
}
