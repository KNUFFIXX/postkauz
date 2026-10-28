package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeArchive ist ein Archiv im Speicher: Ordner mit Nachrichten, Blobs
// unter ihrem SHA-256.
type fakeArchive struct {
	blobs   map[string][]byte
	folders []Folder
	items   map[string][]Item
}

func newArchive() *fakeArchive {
	return &fakeArchive{blobs: map[string][]byte{}, items: map[string][]Item{}}
}

// add legt eine Nachricht in einen Ordner (Pfad mit "/").
func (a *fakeArchive) add(folder string, id int64, raw []byte, it Item) Item {
	it.ID = id
	if raw != nil {
		it.BlobSHA256 = sum(raw)
		a.blobs[it.BlobSHA256] = raw
	}
	if _, ok := a.items[folder]; !ok {
		a.folders = append(a.folders, Folder{Name: folder, Segments: strings.Split(folder, "/"), Key: folder})
	}
	a.items[folder] = append(a.items[folder], it)
	for i := range a.folders {
		if a.folders[i].Name == folder {
			a.folders[i].Count++
			if raw == nil {
				a.folders[i].NoBlob++
			}
		}
	}
	return it
}

func (a *fakeArchive) options(format, dir string) Options {
	return Options{Format: format, Dir: dir, Folders: a.folders,
		Items: func(_ context.Context, f Folder) ([]Item, error) { return a.items[f.Key.(string)], nil },
		Open: func(sha string) (io.ReadCloser, error) {
			b, ok := a.blobs[sha]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return io.NopCloser(bytes.NewReader(b)), nil
		}}
}

var when = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// mboxrd: Trennzeile mit Absender und asctime, ">From"-Quoting
// (umkehrbar), LF, Zeilenende und Leerzeile nach jeder Nachricht außer der
// letzten ohne Zeilenende.
func TestMboxrd(t *testing.T) {
	a := newArchive()
	m1 := []byte("From: Absender <a@example.org>\r\nSubject: eins\r\n\r\nFrom here\r\n>From there\r\n>>From x\r\nFromage\r\n From y\r\n")
	m2 := []byte("Subject: zwei\n\nohne Zeilenende")
	a.add("INBOX", 1, m1, Item{From: "Absender <a@example.org>", Received: when})
	a.add("INBOX", 2, m2, Item{From: "kaputt", Date: when})
	a.add("INBOX", 3, nil, Item{})
	m4 := []byte("Subject: vier\n\nauch ohne")
	a.add("INBOX", 4, m4, Item{From: "d@example.org", Received: when})
	dir := filepath.Join(t.TempDir(), "ziel")
	rep, err := Run(context.Background(), a.options(FormatMbox, dir))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "INBOX"))
	if err != nil {
		t.Fatal(err)
	}
	want := "From a@example.org Wed Mar  4 05:06:07 2026\n" +
		"From: Absender <a@example.org>\nSubject: eins\n\n>From here\n>>From there\n>>>From x\nFromage\n From y\n\n" +
		"From kaputt Wed Mar  4 05:06:07 2026\n" +
		"Subject: zwei\n\nohne Zeilenende\n\n" +
		"From d@example.org Wed Mar  4 05:06:07 2026\n" +
		"Subject: vier\n\nauch ohne"
	if string(got) != want {
		t.Fatalf("mbox =\n%q\nwant\n%q", got, want)
	}
	// Nachrichten ohne Zeilenende am Schluss kommen ans Ende der Datei; nur
	// die letzte bleibt Byte für Byte, die andere bekommt ein Zeilenende.
	if rep.Messages != 3 || rep.Skipped != 1 || rep.SkippedBy[SkipNoBlob] != 1 || rep.NewlineAdded != 1 ||
		len(rep.Folders) != 1 || rep.Folders[0].Path != "INBOX" || rep.Bytes != int64(len(m1)+len(m2)+len(m4)) {
		t.Fatalf("report = %+v", rep)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "INBOX")); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mbox mode %v", fi.Mode().Perm())
		}
	}
}

func TestEnvelopeSender(t *testing.T) {
	for in, want := range map[string]string{"Anna Beispiel <anna@example.org>": "anna@example.org", "b@example.net": "b@example.net",
		"": "MAILER-DAEMON", "-": "MAILER-DAEMON", "Name ohne Adresse": "MAILER-DAEMON", "<a b@example.org>": "MAILER-DAEMON",
		"Jörg <jörg@example.org>": "MAILER-DAEMON", `"Müller, Anna" <anna@example.org>`: "anna@example.org"} {
		if got := envelopeSender(in); got != want {
			t.Errorf("envelopeSender(%q) = %q, want %q", in, got, want)
		}
	}
}

// Akzeptanz S3.3 Punkt 2: EML-Dateien sind Byte für Byte die Blobs;
// index.csv ohne Betreff und Adressen, gegen CSV-Injection geschützt;
// Ordnernamen bereinigt, Kollisionen aufgelöst.
func TestEMLByteForByte(t *testing.T) {
	a := newArchive()
	raws := map[int64][]byte{
		1: []byte("Subject: KANARIE-BETREFF\r\nFrom: kanarie@example.org\r\nMessage-ID: <=HYPERLINK(1)@example.org>\r\n\r\nText\r\n"),
		2: []byte("Subject: lf\n\nnur LF\nFrom x\n"),
		3: append([]byte("Subject: nul\r\n\r\n"), 0, 1, 2, 0xff, '\r'),
		4: []byte("Subject: kopie\r\n\r\nin zwei Ordnern\r\n"),
		5: []byte("Subject: fünf\r\n\r\nx\r\n"),
	}
	a.add("INBOX", 1, raws[1], Item{MessageID: "=HYPERLINK(1)@example.org", Date: when})
	a.add("INBOX", 2, raws[2], Item{MessageID: "+lf@example.org"})
	a.add("Inbox", 3, raws[3], Item{})
	a.add("a:b", 4, raws[4], Item{})
	a.add("a_b", 4, raws[4], Item{})
	a.add("index.csv", 5, raws[5], Item{})
	a.add("CON/2026.", 5, raws[5], Item{})
	dir := t.TempDir() // vorhanden und leer: erlaubt
	rep, err := Run(context.Background(), a.options(FormatEML, dir))
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, f := range rep.Folders {
		paths[f.Folder] = f.Path
	}
	wantPaths := map[string]string{"INBOX": "INBOX", "Inbox": "Inbox-2", "a:b": "a_b", "a_b": "a_b-2", "index.csv": "index-2.csv",
		"CON/2026.": "_CON/2026"}
	for k, v := range wantPaths {
		if paths[k] != v {
			t.Errorf("folder %q at %q, want %q", k, paths[k], v)
		}
	}
	// Jede Datei: Byte für Byte der Blob, Name <id>-<sha256 kurz>.eml.
	nameRE := regexp.MustCompile(`^(\d+)-([0-9a-f]{12})\.eml$`)
	files := 0
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == IndexFile {
			return err
		}
		m := nameRE.FindStringSubmatch(d.Name())
		if m == nil {
			t.Errorf("file name %s", p)
			return nil
		}
		got, _ := os.ReadFile(p)
		id := int64(0)
		for _, c := range m[1] {
			id = id*10 + int64(c-'0')
		}
		if !bytes.Equal(got, raws[id]) || !strings.HasPrefix(sum(got), m[2]) {
			t.Errorf("%s differs from its blob", p)
		}
		if fi, _ := d.Info(); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v", p, fi.Mode().Perm())
		}
		files++
		return nil
	})
	if err != nil || files != 7 || rep.Messages != 7 {
		t.Fatalf("files = %d, report %+v, err %v", files, rep, err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(idx, []byte("KANARIE")) || bytes.Contains(idx, []byte("kanarie@")) {
		t.Fatalf("index.csv names subject or sender:\n%s", idx)
	}
	rows, err := csv.NewReader(bytes.NewReader(idx)).ReadAll()
	if err != nil || len(rows) != 8 || !slices.Equal(rows[0], []string{"id", "message_id", "date", "folder", "size", "sha256", "file"}) {
		t.Fatalf("index.csv = %v %v", rows, err)
	}
	if rows[1][1] != "'=HYPERLINK(1)@example.org" || rows[2][1] != "'+lf@example.org" || rows[1][2] != "2026-03-04T05:06:07Z" ||
		rows[1][3] != "INBOX" || rows[1][5] != sum(raws[1]) || rows[1][6] != "INBOX/1-"+sum(raws[1])[:12]+".eml" {
		t.Fatalf("index rows = %v", rows[1:3])
	}
}

// Das Ziel muss neu oder leer sein; nichts wird ergänzt oder ersetzt.
func TestTargetMustBeFresh(t *testing.T) {
	a := newArchive()
	a.add("INBOX", 1, []byte("Subject: x\r\n\r\nx\r\n"), Item{})
	base := t.TempDir()
	full := filepath.Join(base, "voll")
	if err := os.MkdirAll(full, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(full, "alt.txt"), []byte("bleibt"), 0o600)
	file := filepath.Join(base, "datei")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	empty := filepath.Join(base, "leer")
	_ = os.MkdirAll(empty, 0o700)
	link := filepath.Join(base, "link")
	_ = os.Symlink(empty, link)
	for _, dry := range []bool{true, false} {
		for _, target := range []string{full, file, link} {
			o := a.options(FormatMbox, target)
			o.DryRun = dry
			if _, err := Run(context.Background(), o); !errors.Is(err, ErrTargetNotFresh) {
				t.Errorf("%s (dry %v): %v", target, dry, err)
			}
		}
	}
	if b, _ := os.ReadFile(filepath.Join(full, "alt.txt")); string(b) != "bleibt" {
		t.Fatal("existing file changed")
	}
	// Trockenlauf zählt nur und legt nichts an.
	fresh := filepath.Join(base, "neu", "tief")
	o := a.options(FormatEML, fresh)
	o.DryRun = true
	rep, err := Run(context.Background(), o)
	if err != nil || rep.Messages != 1 || rep.Folders[0].Path != "INBOX" {
		t.Fatalf("dry run: %+v %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(base, "neu")); err == nil {
		t.Fatal("dry run created the target")
	}
	if _, err := Run(context.Background(), a.options(FormatEML, empty)); err != nil {
		t.Fatalf("empty folder: %v", err)
	}
	if _, err := Run(context.Background(), a.options(FormatEML, "relativ")); err == nil {
		t.Fatal("relative target accepted")
	}
}

// Fehlende und beschädigte Blobs zählen als übersprungen mit Grund; in der
// mbox bleibt keine halbe Nachricht.
func TestSkippedBlobs(t *testing.T) {
	for _, format := range []string{FormatMbox, FormatEML} {
		a := newArchive()
		good := []byte("Subject: gut\r\n\r\ngut\r\n")
		a.add("INBOX", 1, good, Item{})
		missing := a.add("INBOX", 2, []byte("Subject: fehlt\r\n\r\n"), Item{})
		delete(a.blobs, missing.BlobSHA256)
		bad := a.add("INBOX", 3, []byte("Subject: kaputt\r\n\r\n"), Item{})
		a.blobs[bad.BlobSHA256] = []byte("Subject: verändert\r\n\r\n")
		a.add("INBOX", 4, nil, Item{})
		dir := filepath.Join(t.TempDir(), "z")
		rep, err := Run(context.Background(), a.options(format, dir))
		if err != nil {
			t.Fatal(err)
		}
		if rep.Messages != 1 || rep.Skipped != 3 || rep.SkippedBy[SkipBlobMissing] != 1 || rep.SkippedBy[SkipBlobDamaged] != 1 ||
			rep.SkippedBy[SkipNoBlob] != 1 || len(rep.SkippedList) != 3 {
			t.Fatalf("%s: report %+v", format, rep)
		}
		if format == FormatMbox {
			b, _ := os.ReadFile(filepath.Join(dir, "INBOX"))
			if bytes.Contains(b, []byte("verändert")) || bytes.Count(b, []byte("\nFrom ")) != 0 || !bytes.HasPrefix(b, []byte("From ")) {
				t.Fatalf("mbox: %q", b)
			}
		}
	}
}

// Ordnerbaum: mbox wie Thunderbird ("A.sbd/B"), eml als Unterordner; ein
// Knoten heißt für alle Ordner darunter gleich.
func TestLayout(t *testing.T) {
	folders := []Folder{{Segments: []string{"Archiv"}}, {Segments: []string{"Archiv", "2024"}}, {Segments: []string{"archiv", "2025"}},
		{Segments: []string{"Archiv.sbd"}}, {Segments: []string{"_imported", "INBOX"}}, {Segments: []string{"x.eml"}}, {}}
	mbox := layout(FormatMbox, folders)
	if want := []string{"Archiv", "Archiv.sbd/2024", "archiv-2.sbd/2025", "Archiv.sbd_", "_imported.sbd/INBOX", "x.eml_", "unnamed"}; !slices.Equal(mbox, want) {
		t.Fatalf("mbox layout = %q, want %q", mbox, want)
	}
	eml := layout(FormatEML, folders)
	if want := []string{"Archiv", "Archiv/2024", "archiv-2/2025", "Archiv.sbd_", "_imported/INBOX", "x.eml_", "unnamed"}; !slices.Equal(eml, want) {
		t.Fatalf("eml layout = %q, want %q", eml, want)
	}
}
