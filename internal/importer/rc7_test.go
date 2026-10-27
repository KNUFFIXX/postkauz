package importer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Gegenprüfung rc6 (rc7): Teilkopien, zweites Lesen über dasselbe Handle
// samt Hash-Prüfung, Quoting beim zweiten Lesen. Alles synthetisch.

// writeTBStatus wie writeTB, mit eigenem X-Mozilla-Status (z. B. 0401 =
// gelesen und nur teilweise heruntergeladen).
func writeTBStatus(t testing.TB, path, status string, raws [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, raw := range raws {
		buf.WriteString("From - Thu Oct 15 10:00:00 2026\r\nX-Mozilla-Status: " + status + "\r\nX-Mozilla-Status2: 00000000\r\n")
		for _, l := range bytes.SplitAfter(raw, []byte("\n")) {
			if bytes.HasPrefix(l, []byte("From ")) {
				buf.WriteByte('>')
			}
			buf.Write(l)
		}
		buf.WriteString("\r\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Thunderbird-Teilkopie (Bit 0x0400) einer Nachricht, deren volle Fassung
// schon da ist: keine zweite, abgeschnittene Nachricht, nur der Ordner.
// Umgekehrt (Teilkopie zuerst, in einem früheren Ordner bzw. Lauf) bleiben
// beide (dokumentiert): Die Teilkopie hat dann keine bekannte Message-ID.
func TestImportPartialCopy(t *testing.T) {
	full := rc4Multipart(7)
	partial := append(append([]byte{}, full[:bytes.Index(full, []byte("\r\n\r\n"))+4]...), "\r\n"...)
	t.Run("full first", func(t *testing.T) {
		dir := t.TempDir()
		writeTBStatus(t, filepath.Join(dir, "A"), "0001", [][]byte{full})
		writeTBStatus(t, filepath.Join(dir, "B"), "0401", [][]byte{partial})
		e := newEnv(t)
		rep := e.run(t, dir, FormatMbox, "", Options{})
		if n := e.count(t, `SELECT count(*) FROM messages`); n != 1 || rep.Existing != 1 {
			t.Fatalf("messages %d existing %d, want 1/1", n, rep.Existing)
		}
		if p := rep.Sources[1].Problems; len(p) != 1 || !strings.Contains(p[0], "only partly downloaded by Thunderbird; linked") {
			t.Fatalf("problems = %q", p)
		}
		if n := e.count(t, `SELECT count(*) FROM message_import_folders`); n != 2 {
			t.Fatalf("import folders = %d, want 2 (A and B)", n)
		}
		if got := e.blobOf(t, "rc4-7@example.org"); !bytes.Equal(got, full) {
			t.Fatal("stored message is not the full version")
		}
	})
	t.Run("partial first", func(t *testing.T) {
		dir := t.TempDir()
		writeTBStatus(t, filepath.Join(dir, "A"), "0401", [][]byte{partial})
		writeTBStatus(t, filepath.Join(dir, "B"), "0001", [][]byte{full})
		e := newEnv(t)
		e.run(t, dir, FormatMbox, "", Options{})
		if n := e.count(t, `SELECT count(*) FROM messages`); n != 2 {
			t.Fatalf("messages = %d, want 2 (documented: the partial copy came first)", n)
		}
	})
}

// Zweite Fassung mit "From "-Zeile im Text: Nach dem zweiten Lesen (mit
// dem Quoting der Datei) byte-gleich.
func TestImportReopenKeepsQuoting(t *testing.T) {
	sent, received := twins()
	received = bytes.Replace(received, []byte("\r\nText\r\n"), []byte("\r\nFrom here on\r\nText\r\n"), 1)
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "X"), [][]byte{sent, received})
	e := newEnv(t)
	e.run(t, dir, FormatMbox, "", Options{})
	found := false
	for _, sum := range e.strs(t, `SELECT blob_sha256 FROM messages`) {
		rc, err := e.blobs.Get(sum)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		found = found || bytes.Equal(b, received)
	}
	if !found {
		t.Fatal("received version not stored byte-exact after the second read")
	}
}

// swapStore ruft vor MessageNearImport (also zwischen Hashen und zweitem
// Lesen) eine Funktion auf, die die Datei ersetzt.
type swapStore struct {
	Store
	swap func()
}

func (s swapStore) MessageNearImport(ctx context.Context, a int64, mid string, size, maxExtra int64, subj string, d time.Time) (int64, bool, error) {
	if s.swap != nil {
		s.swap()
		s.swap = nil
	}
	return s.Store.MessageNearImport(ctx, a, mid, size, maxExtra, subj, d)
}

// Thunderbird kompaktiert zwischen Hashen und zweitem Lesen.
//   - Datei per Umbenennen ersetzt: Das Handle liest weiter die alte Datei,
//     die richtige Fassung wird gespeichert.
//   - Datei an Ort und Stelle überschrieben: Der Hash des zweiten Lesens
//     weicht ab; Fehler für diese Nachricht, nichts Fremdes gespeichert.
func TestImportFileChangedBeforeSecondRead(t *testing.T) {
	sent, received := twins()
	other := bytes.Replace(rc4Multipart(9), []byte("rc4-9@"), []byte("other@"), 1)
	for _, tc := range []struct {
		name       string
		inPlace    bool
		twins, err int
	}{{"renamed", false, 2, 0}, {"rewritten in place", true, 1, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "X")
			writeTB(t, path, [][]byte{sent, received})
			alt := filepath.Join(t.TempDir(), "alt")
			writeTB(t, alt, [][]byte{sent, other})
			e := newEnv(t)
			eng := e.engine()
			var once bool
			eng.Store = swapStore{Store: e.st, swap: func() {
				if once {
					return
				}
				once = true
				b, err := os.ReadFile(alt)
				if err != nil {
					t.Error(err)
				}
				if tc.inPlace {
					f, err := os.OpenFile(path, os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = f.WriteAt(b, 0)
					_ = f.Close()
					return
				}
				_ = os.WriteFile(path+".tmp", b, 0o644)
				_ = os.Rename(path+".tmp", path)
			}}
			srcs, _, err := Discover(dir, FormatMbox, "", "content")
			if err != nil {
				t.Fatal(err)
			}
			rep, err := eng.Run(context.Background(), Options{Sources: srcs})
			if err != nil {
				t.Fatal(err)
			}
			if e.twinCount(t) != tc.twins || rep.Errors != tc.err ||
				e.count(t, `SELECT count(*) FROM messages WHERE message_id_header = 'other@example.org'`) != 0 {
				t.Fatalf("twins %d errors %d problems %q", e.twinCount(t), rep.Errors, rep.Sources[0].Problems)
			}
			if tc.inPlace && !strings.Contains(strings.Join(rep.Sources[0].Problems, "|"), "message changed between two reads") {
				t.Fatalf("problems = %q", rep.Sources[0].Problems)
			}
		})
	}
}

// Maildir: Die Datei wird zwischen Hashen und zweitem Lesen umbenannt bzw.
// entfernt (anderer Client setzt Flags): Das Handle liest sie trotzdem.
func TestImportMaildirFileMovedBeforeSecondRead(t *testing.T) {
	sent, received := twins()
	dir := t.TempDir()
	for _, s := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a := filepath.Join(dir, "cur", mdName("1700000000.M1P1.rc7:2,S"))
	b := filepath.Join(dir, "cur", mdName("1700000001.M1P1.rc7:2,S"))
	_ = os.WriteFile(a, sent, 0o644)
	_ = os.WriteFile(b, received, 0o644)
	e := newEnv(t)
	eng := e.engine()
	eng.Store = swapStore{Store: e.st, swap: func() { _ = os.Rename(b, filepath.Join(t.TempDir(), "weg")) }}
	srcs, _, err := Discover(dir, FormatMaildir, "", "content")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eng.Run(context.Background(), Options{Sources: srcs})
	if err != nil {
		t.Fatal(err)
	}
	if rep.New != 2 || rep.Errors != 0 || e.twinCount(t) != 2 {
		t.Fatalf("new %d errors %d twins %d, want 2/0/2", rep.New, rep.Errors, e.twinCount(t))
	}
}

// failApply: Speichern eines Fensters scheitert (z. B. Platte voll).
type failApply struct{ *store.SQLite }

func (failApply) ApplyBatch(context.Context, store.MirrorBatch) (store.BatchResult, error) {
	return store.BatchResult{}, errors.New("disk full")
}

// Der Zähler „linked despite size difference" steigt erst, wenn die
// Verknüpfung gespeichert ist: Scheitert das Speichern, bleibt er 0.
func TestLinkedSizeDiffCountedAfterSave(t *testing.T) {
	full := rc4Multipart(8)
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{full})
	e := newEnv(t)
	e.run(t, dir, FormatMbox, "", Options{})
	addr, _ := rc4Server(t, "INBOX", []imap.Flag{imap.FlagSeen}, bytes.TrimSuffix(full, []byte("\r\n")))
	srep := e.syncWith(t, addr, failApply{e.st})
	if srep.Errors == 0 || srep.LinkedSizeDiff != 0 {
		t.Fatalf("errors %d, linked despite size difference %d; want >0 and 0", srep.Errors, srep.LinkedSizeDiff)
	}
	if srep := e.sync(t, addr); srep.LinkedSizeDiff != 1 {
		t.Fatalf("after a successful sync: %d, want 1", srep.LinkedSizeDiff)
	}
}

// Eine Importkopie mit \Draft (Maildir-Flag D) nutzt die Toleranz für die
// Thunderbird-Fassung nie: Erst Sync, dann Import der um 4 Bytes längeren
// Kopie: ohne D dieselbe Nachricht, mit D eine eigene (rc8).
func TestImportNearMatchNotForDrafts(t *testing.T) {
	full := rc4Multipart(10)
	server := full[:len(full)-4]
	for _, tc := range []struct {
		flags    string
		messages int
	}{{"S", 1}, {"DS", 2}} {
		t.Run(tc.flags, func(t *testing.T) {
			e := newEnv(t)
			addr, _ := rc4Server(t, "INBOX", []imap.Flag{imap.FlagSeen}, server)
			if srep := e.sync(t, addr); srep.New != 1 {
				t.Fatalf("sync = %+v", srep)
			}
			dir := t.TempDir()
			for _, s := range []string{"cur", "new", "tmp"} {
				if err := os.MkdirAll(filepath.Join(dir, s), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "cur", mdName("1700000000.M1P1.rc8:2,"+tc.flags)), full, 0o644); err != nil {
				t.Fatal(err)
			}
			e.run(t, dir, FormatMaildir, "", Options{})
			if n := e.count(t, `SELECT count(*) FROM messages`); n != tc.messages {
				t.Fatalf("messages = %d, want %d", n, tc.messages)
			}
		})
	}
}

// Teilkopie bei zwei Nachrichten mit gleicher Message-ID: Zugeordnet wird
// die mit Rohmail, die kein Entwurf ist (rc8).
func TestImportPartialPrefersNonDraft(t *testing.T) {
	full := rc4Multipart(11)
	partial := append(append([]byte{}, full[:bytes.Index(full, []byte("\r\n\r\n"))+4]...), "\r\n"...)
	other := bytes.Replace(full, []byte("Text 11"), []byte("Text elf"), 1) // zweite Fassung, gleiche Message-ID
	dir := t.TempDir()
	writeTBStatus(t, filepath.Join(dir, "A"), "0001", [][]byte{full, other})
	e := newEnv(t)
	e.run(t, dir, FormatMbox, "", Options{})
	// Die ältere Fassung wird zum Entwurf (Flag), die jüngere nicht.
	if err := e.st.ExecForTest(`UPDATE messages SET flag_draft = 1 WHERE id = (SELECT min(id) FROM messages)`); err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	writeTBStatus(t, filepath.Join(dir2, "B"), "0401", [][]byte{partial})
	e.run(t, dir2, FormatMbox, "", Options{})
	if n := e.count(t, `SELECT count(*) FROM messages`); n != 2 {
		t.Fatalf("messages = %d, want 2", n)
	}
	if n := e.count(t, `SELECT count(*) FROM message_import_folders mf JOIN import_folders f ON f.id = mf.folder_id
		JOIN messages m ON m.id = mf.message_id WHERE f.path = 'B' AND m.flag_draft = 0`); n != 1 {
		t.Fatal("partial copy not linked to the non-draft message")
	}
}
