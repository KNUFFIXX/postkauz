package export

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Formate von messages.export.
const (
	FormatMbox = "mbox"
	FormatEML  = "eml"
)

// IndexFile ist die Übersicht im EML-Verzeichnis (ohne Betreff, ohne
// Adressen).
const IndexFile = "index.csv"

// Gründe, aus denen eine Nachricht nicht exportiert wird.
const (
	// SkipNoBlob: Nur der Kopf ist gespiegelt (keine Rohmail).
	SkipNoBlob = "no_blob"
	// SkipBlobMissing: Die Rohmail fehlt im Blob-Store.
	SkipBlobMissing = "blob_missing"
	// SkipBlobDamaged: Die Rohmail ist unlesbar oder passt nicht zu ihrem
	// SHA-256.
	SkipBlobDamaged = "blob_damaged"
)

// ErrTargetNotFresh: Das Ziel ist kein neuer oder leerer Ordner (frisches
// Ziel ist Pflicht; ein Export ergänzt oder ersetzt nie Dateien).
var ErrTargetNotFresh = errors.New("the target must be a new or empty folder")

// Folder ist ein Ordner des Exports: ein Serverordner, ein Import-Ordner
// (unter "_imported") oder der Sammelordner für Nachrichten ohne Ordner.
type Folder struct {
	// Name ist der Ordnerpfad zur Anzeige und in index.csv ("Archiv/2024").
	Name string
	// Segments ergeben die Ablage (je Teil bereinigt; Kollisionen werden
	// aufgelöst).
	Segments []string
	// Count und NoBlob zählen die Nachrichten (für Fortschritt und
	// Trockenlauf).
	Count  int
	NoBlob int
	// Key identifiziert den Ordner für Items (vom Aufrufer vergeben).
	Key any
}

// Item ist eine Nachricht eines Ordners.
type Item struct {
	ID        int64
	MessageID string
	// Date ist das Datum für index.csv (Date-Kopf, sonst INTERNALDATE);
	// Received das Datum der mbox-Trennzeile (INTERNALDATE, sonst Date).
	Date     time.Time
	Received time.Time
	// From ist der Absender wie gespeichert ("Name <adresse>"); nur die
	// Adresse kommt in die mbox-Trennzeile.
	From       string
	BlobSHA256 string
}

// Event ist eine Fortschrittsmeldung je Ordner.
type Event struct {
	Folder string
	Done   int
	Total  int
}

// Options steuert Run.
type Options struct {
	Format string
	// Dir ist das Ziel (absolut); es muss neu oder leer sein.
	Dir    string
	DryRun bool
	// Folders in der Reihenfolge des Exports.
	Folders []Folder
	// Items liefert die Nachrichten eines Ordners.
	Items func(ctx context.Context, f Folder) ([]Item, error)
	// Open öffnet eine Rohmail aus dem Blob-Store.
	Open func(sha256 string) (io.ReadCloser, error)
	// IsNotFound erkennt den Fehler von Open für eine fehlende Datei.
	IsNotFound func(error) bool
	// Progress empfängt eine Meldung nach jedem Ordner (darf nil sein).
	Progress func(Event)
}

// FolderReport ist das Ergebnis eines Ordners.
type FolderReport struct {
	Folder string
	// Path ist die Ablage relativ zum Ziel ("/"-getrennt): die mbox-Datei
	// bzw. der Ordner mit den EML-Dateien; leer ohne Nachrichten.
	Path     string
	Messages int
	Skipped  int
	Bytes    int64
}

// Skip ist eine nicht exportierte Nachricht mit Grund.
type Skip struct {
	ID     int64
	Reason string
}

// maxSkipList begrenzt die Liste der übersprungenen IDs im Bericht.
const maxSkipList = 50

// Report ist das Ergebnis von Run.
type Report struct {
	Folders  []FolderReport
	Messages int
	Skipped  int
	Bytes    int64
	// SkippedBy zählt die Gründe; SkippedList nennt die ersten IDs.
	SkippedBy   map[string]int
	SkippedList []Skip
	// NewlineAdded zählt Nachrichten ohne Zeilenende am Schluss, denen die
	// mbox eines geben muss (nur mbox; ihr content_hash ändert sich, der
	// Import findet sie trotzdem über die Varianten am Zeilenende).
	NewlineAdded int
}

func (r *Report) skip(id int64, reason string) {
	r.Skipped++
	r.SkippedBy[reason]++
	if len(r.SkippedList) < maxSkipList {
		r.SkippedList = append(r.SkippedList, Skip{ID: id, Reason: reason})
	}
}

// CheckTarget prüft das Ziel: Es fehlt (wird angelegt) oder ist ein leerer
// Ordner. Ein Symlink, eine Datei oder ein Ordner mit Inhalt ist
// ErrTargetNotFresh.
func CheckTarget(dir string) (exists bool, err error) {
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case fi.Mode()&fs.ModeSymlink != 0:
		return true, fmt.Errorf("%w: it is a symbolic link", ErrTargetNotFresh)
	case !fi.IsDir():
		return true, fmt.Errorf("%w: it is a file", ErrTargetNotFresh)
	}
	f, err := os.Open(dir)
	if err != nil {
		return true, err
	}
	defer f.Close()
	if names, _ := f.Readdirnames(1); len(names) > 0 {
		return true, fmt.Errorf("%w: it is not empty", ErrTargetNotFresh)
	}
	return true, nil
}

// Run exportiert die Ordner nach Dir: mboxrd mit einer Datei je Ordner
// (Thunderbird-Ablage: Unterordner in "<name>.sbd/") oder ein
// EML-Verzeichnis (je Ordner ein Unterordner, Dateien "<id>-<sha256
// kurz>.eml" Byte für Byte wie im Blob-Store, dazu index.csv). Der
// Trockenlauf prüft das Ziel und zählt nur.
func Run(ctx context.Context, o Options) (Report, error) {
	rep := Report{SkippedBy: map[string]int{}}
	if o.Format != FormatMbox && o.Format != FormatEML {
		return rep, fmt.Errorf("export: unknown format %q (mbox or eml)", o.Format)
	}
	if !filepath.IsAbs(o.Dir) {
		return rep, fmt.Errorf("export: the target must be an absolute path")
	}
	exists, err := CheckTarget(o.Dir)
	if err != nil {
		return rep, err
	}
	paths := layout(o.Format, o.Folders)
	if o.DryRun {
		for i, f := range o.Folders {
			fr := FolderReport{Folder: f.Name, Messages: f.Count - f.NoBlob, Skipped: f.NoBlob}
			if fr.Messages > 0 {
				fr.Path = paths[i]
			}
			rep.Folders = append(rep.Folders, fr)
			rep.Messages += fr.Messages
			rep.Skipped += f.NoBlob
			if f.NoBlob > 0 {
				rep.SkippedBy[SkipNoBlob] += f.NoBlob
			}
		}
		return rep, nil
	}
	if !exists {
		if err := os.MkdirAll(o.Dir, 0o700); err != nil {
			return rep, err
		}
	}
	root, err := os.OpenRoot(o.Dir)
	if err != nil {
		return rep, err
	}
	defer root.Close()
	w := &writer{o: o, root: root, rep: &rep}
	if o.Format == FormatEML {
		if err := w.openIndex(); err != nil {
			return rep, err
		}
	}
	for i, f := range o.Folders {
		if err := ctx.Err(); err != nil {
			_ = w.closeIndex()
			return rep, err
		}
		fr, err := w.folder(ctx, f, paths[i])
		rep.Folders = append(rep.Folders, fr)
		rep.Messages += fr.Messages
		rep.Bytes += fr.Bytes
		if err != nil {
			_ = w.closeIndex()
			return rep, err
		}
		if o.Progress != nil {
			o.Progress(Event{Folder: f.Name, Done: fr.Messages, Total: f.Count})
		}
	}
	return rep, w.closeIndex()
}

// layout bestimmt je Ordner die Ablage relativ zum Ziel. Jeder Pfadteil
// wird bereinigt; gleiche Namen (ohne Unterschied der Groß- und
// Kleinschreibung, wie unter Windows und macOS) bekommen einen Zähler. Ein
// Knoten des Baums behält seinen Namen für alle Ordner darunter.
//
// mbox: Ordner "A/B" liegt in "A.sbd/B" (wie Thunderbird); für jeden
// Knoten sind Name und Name+".sbd" zugleich reserviert. eml: "A/B/" mit
// den Dateien; "index.csv" ist im Ziel reserviert.
func layout(format string, folders []Folder) []string {
	used := map[string]map[string]bool{} // Ordner → belegte Namen (klein)
	taken := func(dir, name string) bool { return used[dir][strings.ToLower(name)] }
	take := func(dir, name string) {
		if used[dir] == nil {
			used[dir] = map[string]bool{}
		}
		used[dir][strings.ToLower(name)] = true
	}
	if format == FormatEML {
		take("", IndexFile)
	}
	nodes := map[string]string{} // Schlüssel des Knotens → Name
	out := make([]string, len(folders))
	for i, f := range folders {
		segs := f.Segments
		if len(segs) == 0 {
			segs = []string{fallbackName}
		}
		dir, key := "", ""
		var rel []string
		for j, seg := range segs {
			key += "\x00" + seg
			name, ok := nodes[key]
			if !ok {
				base := SafeFileName(seg)
				// Ein Knoten, der wie eine Thunderbird-Unterordnerablage
				// (".sbd") oder wie eine EML-Datei (".eml") hieße, würde beim
				// Import bzw. neben den Dateien missverstanden.
				if l := strings.ToLower(base); strings.HasSuffix(l, ".sbd") || strings.HasSuffix(l, ".eml") {
					base += "_"
				}
				for n := 1; ; n++ {
					name = WithCounter(base, n)
					if !taken(dir, name) && (format != FormatMbox || !taken(dir, name+".sbd")) {
						break
					}
				}
				take(dir, name)
				if format == FormatMbox {
					take(dir, name+".sbd")
				}
				nodes[key] = name
			}
			last := j == len(segs)-1
			switch {
			case format == FormatMbox && !last:
				rel = append(rel, name+".sbd")
			default:
				rel = append(rel, name)
			}
			dir = strings.Join(rel, "/")
		}
		out[i] = strings.Join(rel, "/")
	}
	return out
}

// writer schreibt die Dateien eines Exports.
type writer struct {
	o     Options
	root  *os.Root
	rep   *Report
	index *os.File
	csv   *csv.Writer
}

func (w *writer) openIndex() error {
	f, err := w.root.OpenFile(IndexFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	w.index, w.csv = f, csv.NewWriter(f)
	return w.csv.Write([]string{"id", "message_id", "date", "folder", "size", "sha256", "file"})
}

func (w *writer) closeIndex() error {
	if w.index == nil {
		return nil
	}
	w.csv.Flush()
	err := w.csv.Error()
	if err == nil {
		err = w.index.Sync()
	}
	if cerr := w.index.Close(); err == nil {
		err = cerr
	}
	w.index = nil
	return err
}

// folder exportiert einen Ordner.
func (w *writer) folder(ctx context.Context, f Folder, rel string) (FolderReport, error) {
	fr := FolderReport{Folder: f.Name}
	items, err := w.o.Items(ctx, f)
	if err != nil {
		return fr, err
	}
	if w.o.Format == FormatMbox {
		return w.mboxFolder(ctx, fr, items, rel)
	}
	osRel := filepath.FromSlash(rel)
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return fr, err
		}
		if it.BlobSHA256 == "" {
			fr.Skipped++
			w.rep.skip(it.ID, SkipNoBlob)
			continue
		}
		if fr.Path == "" {
			if err := w.root.MkdirAll(osRel, 0o700); err != nil {
				return fr, err
			}
			fr.Path = rel
		}
		n, reason, err := w.eml(osRel, rel, f, it)
		if err != nil {
			return fr, err
		}
		if reason != "" {
			fr.Skipped++
			w.rep.skip(it.ID, reason)
			continue
		}
		fr.Messages++
		fr.Bytes += n
	}
	return fr, nil
}

// mboxFolder schreibt die mbox-Datei eines Ordners. Eine Nachricht ohne
// Zeilenende am Schluss lässt sich in mbox nur als letzte der Datei genau
// abbilden (ohne Rahmen-Leerzeile vor dem Dateiende); solche Nachrichten
// kommen deshalb ans Ende, die letzte davon unverändert, jede weitere mit
// einem ergänzten Zeilenende (NewlineAdded).
func (w *writer) mboxFolder(ctx context.Context, fr FolderReport, items []Item, rel string) (FolderReport, error) {
	osRel := filepath.FromSlash(rel)
	var mb *bufio.Writer
	var file *os.File
	open := func() error {
		if mb != nil {
			return nil
		}
		if dir := filepath.Dir(osRel); dir != "." {
			if err := w.root.MkdirAll(dir, 0o700); err != nil {
				return err
			}
		}
		f, err := w.root.OpenFile(osRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		file, mb, fr.Path = f, bufio.NewWriterSize(f, 256<<10), rel
		return nil
	}
	fail := func(err error) (FolderReport, error) {
		if file != nil {
			_ = file.Close()
		}
		return fr, err
	}
	type pending struct {
		it Item
		n  int64
	}
	var noNL []pending
	write := func(it Item, n int64, addNL, frame bool) error {
		if err := open(); err != nil {
			return err
		}
		if err := w.mbox(mb, it, addNL, frame); err != nil {
			return err
		}
		fr.Messages++
		fr.Bytes += n
		return nil
	}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if it.BlobSHA256 == "" {
			fr.Skipped++
			w.rep.skip(it.ID, SkipNoBlob)
			continue
		}
		n, endsNL, reason := w.mboxCheck(it)
		switch {
		case reason != "":
			fr.Skipped++
			w.rep.skip(it.ID, reason)
		case !endsNL:
			noNL = append(noNL, pending{it, n})
		default:
			if err := write(it, n, false, true); err != nil {
				return fail(err)
			}
		}
	}
	for i, p := range noNL {
		last := i == len(noNL)-1
		if !last {
			w.rep.NewlineAdded++
		}
		if err := write(p.it, p.n, !last, !last); err != nil {
			return fail(err)
		}
	}
	if file == nil {
		return fr, nil
	}
	err := mb.Flush()
	if err == nil {
		err = file.Sync()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	return fr, err
}

// openBlob öffnet eine Rohmail; eine fehlende Datei ist ein Grund, kein
// Fehler des Exports.
func (w *writer) openBlob(sha string) (io.ReadCloser, string) {
	rc, err := w.o.Open(sha)
	switch {
	case err == nil:
		return rc, ""
	case w.o.IsNotFound != nil && w.o.IsNotFound(err), errors.Is(err, fs.ErrNotExist):
		return nil, SkipBlobMissing
	}
	return nil, SkipBlobDamaged
}

// eml schreibt eine Nachricht als "<id>-<sha256 kurz>.eml", Byte für Byte
// wie im Blob-Store; passt der Inhalt nicht zu seinem Hash, fällt die Datei
// wieder weg (blob_damaged).
func (w *writer) eml(osDir, dir string, f Folder, it Item) (int64, string, error) {
	rc, reason := w.openBlob(it.BlobSHA256)
	if reason != "" {
		return 0, reason, nil
	}
	defer rc.Close()
	short := it.BlobSHA256
	if len(short) > 12 {
		short = short[:12]
	}
	base := SafeFileName(strconv.FormatInt(it.ID, 10) + "-" + short + ".eml")
	var file *os.File
	var name string
	for n := 1; ; n++ {
		if n > maxCounter {
			return 0, "", ErrNameTaken
		}
		name = WithCounter(base, n)
		var err error
		file, err = w.root.OpenFile(filepath.Join(osDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return 0, "", err
		}
		break
	}
	h := sha256.New()
	n, cerr := io.Copy(io.MultiWriter(file, h), rc)
	werr := file.Close()
	sum := hex.EncodeToString(h.Sum(nil))
	if cerr != nil || validSHA256(it.BlobSHA256) && sum != it.BlobSHA256 {
		_ = w.root.Remove(filepath.Join(osDir, name))
		return 0, SkipBlobDamaged, nil
	}
	if werr != nil {
		return 0, "", werr
	}
	rel := name
	if dir != "" {
		rel = dir + "/" + name
	}
	date := ""
	if !it.Date.IsZero() {
		date = it.Date.UTC().Format(time.RFC3339)
	}
	row := []string{strconv.FormatInt(it.ID, 10), it.MessageID, date, f.Name, strconv.FormatInt(n, 10), sum, rel}
	for i := range row {
		row[i] = csvSafe(row[i])
	}
	return n, "", w.csv.Write(row)
}

// csvSafe entschärft ein Feld gegen CSV-Injection: Tabellenprogramme lesen
// "=", "+", "-", "@" (und Tab/CR davor) am Anfang als Formel; ein
// vorangestelltes "'" macht daraus Text. Zeilenumbrüche und Steuerzeichen
// werden zu Leerzeichen (eine Zeile je Nachricht).
func csvSafe(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// mboxCheck liest die Rohmail einmal ganz und prüft sie (Hash), bevor
// mbox sie schreibt: Eine unlesbare Rohmail hinterlässt nie eine halbe
// Nachricht. endsNL: Sie endet mit einem Zeilenende (oder ist leer).
func (w *writer) mboxCheck(it Item) (n int64, endsNL bool, reason string) {
	rc, reason := w.openBlob(it.BlobSHA256)
	if reason != "" {
		return 0, false, reason
	}
	h := sha256.New()
	tail := &lastByte{}
	n, err := io.Copy(io.MultiWriter(h, tail), rc)
	_ = rc.Close()
	if err != nil || validSHA256(it.BlobSHA256) && hex.EncodeToString(h.Sum(nil)) != it.BlobSHA256 {
		return 0, false, SkipBlobDamaged
	}
	return n, n == 0 || tail.b == '\n', ""
}

// mbox schreibt eine Nachricht als mboxrd: Trennzeile "From <absender>
// <asctime>", Zeilen, die nach beliebig vielen ">" mit "From " beginnen,
// mit einem ">" mehr, Zeilenenden LF; addNL ergänzt ein Zeilenende am
// Schluss, frame die Leerzeile danach (nur die letzte Nachricht einer Datei
// darf ohne sie enden).
func (w *writer) mbox(mb *bufio.Writer, it Item, addNL, frame bool) error {
	rc, err := w.o.Open(it.BlobSHA256)
	if err != nil {
		return err
	}
	defer rc.Close()
	if _, err := mb.WriteString("From " + envelopeSender(it.From) + " " + asctime(it) + "\n"); err != nil {
		return err
	}
	if err := quoteMboxrd(mb, rc); err != nil {
		return err
	}
	if addNL {
		if err := mb.WriteByte('\n'); err != nil {
			return err
		}
	}
	if frame {
		return mb.WriteByte('\n')
	}
	return nil
}

// lastByte merkt sich das letzte geschriebene Byte.
type lastByte struct{ b byte }

func (l *lastByte) Write(p []byte) (int, error) {
	if len(p) > 0 {
		l.b = p[len(p)-1]
	}
	return len(p), nil
}

// quoteMboxrd kopiert r zeilenweise nach w: CRLF wird LF, Zeilen der Form
// ">*From " bekommen ein ">" mehr (mboxrd, umkehrbar).
func quoteMboxrd(w *bufio.Writer, r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)
	atStart, heldCR := true, false
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			if heldCR {
				// Ein CR am Ende des vorigen Stücks: vor LF fällt es weg.
				if chunk[0] != '\n' {
					if werr := w.WriteByte('\r'); werr != nil {
						return werr
					}
				}
				heldCR = false
			}
			if atStart && needsQuote(chunk) {
				if werr := w.WriteByte('>'); werr != nil {
					return werr
				}
			}
			out, lf := chunk, false
			switch {
			case bytes.HasSuffix(out, []byte("\r\n")):
				out, lf = out[:len(out)-2], true
			case err == bufio.ErrBufferFull && out[len(out)-1] == '\r':
				out, heldCR = out[:len(out)-1], true
			}
			if _, werr := w.Write(out); werr != nil {
				return werr
			}
			if lf {
				if werr := w.WriteByte('\n'); werr != nil {
					return werr
				}
			}
			atStart = chunk[len(chunk)-1] == '\n'
		}
		switch {
		case err == nil, err == bufio.ErrBufferFull:
			continue
		case err == io.EOF:
			if heldCR {
				return w.WriteByte('\r')
			}
			return nil
		default:
			return err
		}
	}
}

// needsQuote: Die Zeile beginnt nach null oder mehr ">" mit "From ".
func needsQuote(line []byte) bool {
	i := 0
	for i < len(line) && line[i] == '>' {
		i++
	}
	return bytes.HasPrefix(line[i:], []byte("From "))
}

// envelopeSender ist die Adresse für die Trennzeile: nur sichtbare
// ASCII-Zeichen ohne Leerzeichen, sonst "MAILER-DAEMON" (wie bei
// Nachrichten ohne Absender üblich). Nie "-": Das läse der Import als
// Thunderbird-Ablage.
func envelopeSender(from string) string {
	addr := ""
	if a, err := mail.ParseAddress(from); err == nil {
		addr = a.Address
	} else if i, j := strings.LastIndexByte(from, '<'), strings.LastIndexByte(from, '>'); i >= 0 && j > i {
		addr = from[i+1 : j]
	} else {
		addr = strings.TrimSpace(from)
	}
	if addr == "" || addr == "-" || len(addr) > 254 {
		return "MAILER-DAEMON"
	}
	for i := 0; i < len(addr); i++ {
		if addr[i] <= ' ' || addr[i] >= 0x7f {
			return "MAILER-DAEMON"
		}
	}
	return addr
}

// asctime ist das Datum der Trennzeile in UTC ("Mon Jan  2 15:04:05 2006").
func asctime(it Item) string {
	t := it.Received
	if t.IsZero() {
		t = it.Date
	}
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	return t.UTC().Format(time.ANSIC)
}
