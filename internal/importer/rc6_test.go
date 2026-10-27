package importer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// rc6 (Befund mit Echtdaten 25.10.2026): Gmail führt teils zwei
// verschiedene Nachrichten mit derselben Message-ID in „Alle Nachrichten"
// (Fassung des Absenders mit Outlook-Kopfzeilen, empfangene Fassung mit
// ARC/Delivered-To). Der Import fasst nicht mehr nach Message-ID allein
// zusammen. Alles synthetisch.

const twinMID = "rc6-twin@example.org"

// twins: zwei Fassungen derselben Mail (gleiche Message-ID, Betreff, Date).
func twins() (sent, received []byte) {
	body := "Subject: Angebot\r\nMessage-ID: <" + twinMID + ">\r\nDate: Thu, 15 Oct 2026 10:00:00 +0200\r\n" +
		"From: absender@example.org\r\nTo: empfaenger@example.org\r\n"
	sent = []byte(body + "Thread-Index: AQHZexample\r\nX-OlkEid: 00000000\r\n\r\nText\r\n")
	received = []byte("Delivered-To: empfaenger@example.org\r\nARC-Seal: i=1; a=rsa-sha256; d=example.org; s=arc; cv=none; b=abc\r\n" +
		body + "\r\nText\r\n")
	return sent, received
}

func (e *env) twinCount(t testing.TB) int {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM messages WHERE message_id_header = ?`, twinMID)
}

// Zwei Fassungen in einer mbox → zwei Nachrichten; dieselbe Fassung zweimal
// → eine; zweiter Lauf, --rescan und Trockenlauf ohne Doppelte; danach Sync
// gegen „Alle Nachrichten" mit beiden Fassungen: beide per Kopf verknüpft,
// kein Inhaltsabruf.
func TestImportKeepsTwoVersionsWithSameMessageID(t *testing.T) {
	sent, received := twins()
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "Alle Nachrichten"), [][]byte{sent, received, sent})
	e := newEnv(t)
	if rep := e.run(t, dir, FormatMbox, "", Options{DryRun: true}); rep.New != 2 || rep.Existing != 1 {
		t.Fatalf("dry run = new %d existing %d, want 2/1", rep.New, rep.Existing)
	}
	rep := e.run(t, dir, FormatMbox, "", Options{})
	if rep.New != 2 || rep.Existing != 1 || rep.Errors != 0 {
		t.Fatalf("import = new %d existing %d errors %d, want 2/1/0", rep.New, rep.Existing, rep.Errors)
	}
	if n := e.twinCount(t); n != 2 {
		t.Fatalf("messages with the Message-ID = %d, want 2", n)
	}
	for _, raw := range [][]byte{sent, received} {
		if n := e.count(t, `SELECT count(*) FROM messages WHERE size = ?`, len(raw)); n != 1 {
			t.Fatalf("version of %d bytes stored %d times", len(raw), n)
		}
	}
	if rep := e.run(t, dir, FormatMbox, "", Options{Rescan: true}); rep.New != 0 || rep.Existing != 3 {
		t.Fatalf("rescan = new %d existing %d, want 0/3", rep.New, rep.Existing)
	}
	if n := e.twinCount(t); n != 2 {
		t.Fatalf("after rescan = %d messages", n)
	}

	u := imapmemserver.NewUser(memUser, string(memPass))
	_ = u.Create("INBOX", nil)
	_ = u.Create("Alle Nachrichten", nil)
	for _, raw := range [][]byte{sent, received} {
		if _, err := u.Append("Alle Nachrichten", rawLit{bytes.NewReader(raw)}, &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen},
			Time: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	bodies := &atomic.Int64{}
	srep := e.sync(t, serveMem(t, u, bodies))
	if bodies.Load() != 0 || srep.Errors != 0 || srep.New != 2 {
		t.Fatalf("sync: FETCH BODY[] %d, errors %d, new %d; want 0/0/2", bodies.Load(), srep.Errors, srep.New)
	}
	// Jede Serverkopie an ihrer Fassung (gleiche Größe), keine Dublette.
	if n := e.count(t, `SELECT count(*) FROM messages m JOIN message_mailboxes mm ON mm.message_id = m.id
		JOIN blobs b ON b.sha256 = m.blob_sha256 WHERE m.message_id_header = ?`, twinMID); n != 2 || e.twinCount(t) != 2 {
		t.Fatalf("linked copies %d, messages %d; want 2/2", n, e.twinCount(t))
	}
}

// Ein Bestand, der beim alten Import nach Message-ID zusammengefasst wurde
// (nur eine Fassung): Der erneute Import mit --rescan ergänzt die
// verworfene Fassung (ohne --rescan liest der Import eine unveränderte
// Datei nicht noch einmal).
func TestReimportAddsVersionDroppedByOldImport(t *testing.T) {
	sent, received := twins()
	dir := t.TempDir()
	path := filepath.Join(dir, "INBOX")
	writeTB(t, path, [][]byte{sent}) // so sah der alte Bestand aus
	e := newEnv(t)
	e.run(t, dir, FormatMbox, "", Options{})
	writeTB(t, path, [][]byte{sent, received})
	rep := e.run(t, dir, FormatMbox, "", Options{Rescan: true})
	if rep.New != 1 || rep.Existing != 1 || e.twinCount(t) != 2 {
		t.Fatalf("re-import = new %d existing %d, messages %d; want 1/1/2", rep.New, rep.Existing, e.twinCount(t))
	}
}

// Labels (nur zuordnen): eine andere Fassung mit bekannter Message-ID wird
// übersprungen, mit Hinweis.
func TestImportLabelsSkipsOtherVersion(t *testing.T) {
	sent, received := twins()
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "content", "INBOX"), [][]byte{sent})
	writeTB(t, filepath.Join(dir, "labels", "Wichtig"), [][]byte{received, sent})
	e := newEnv(t)
	content, _, err := Discover(filepath.Join(dir, "content"), FormatMbox, "", store.ImportContent)
	if err != nil {
		t.Fatal(err)
	}
	labels, _, err := Discover(filepath.Join(dir, "labels"), FormatMbox, "", store.ImportLabels)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := e.engine().Run(t.Context(), Options{Sources: append(content, labels...)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.New != 1 || rep.Existing != 1 || rep.Skipped != 1 || e.twinCount(t) != 1 {
		t.Fatalf("new %d existing %d skipped %d messages %d; want 1/1/1/1", rep.New, rep.Existing, rep.Skipped, e.twinCount(t))
	}
	if p := rep.Sources[1].Problems; len(p) != 1 || !strings.Contains(p[0], "different content; not stored (labels only)") {
		t.Fatalf("problems = %q", p)
	}
}

// Erst Sync, dann Import der Thunderbird-Fassung (Ende ergänzt, +4: die
// abschließende Grenze ohne "--" am Server): dieselbe Nachricht, keine
// Dublette. Mit anderem Betreff: eigene Nachricht.
func TestImportAfterSyncThunderbirdEnding(t *testing.T) {
	full := rc4Multipart(6)
	server := full[:len(full)-4]
	for _, tc := range []struct {
		name     string
		file     []byte
		messages int
	}{
		{"same message", full, 1},
		{"other subject", bytes.Replace(full, []byte("Subject: rc4 6"), []byte("Subject: rc4 X"), 1), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			addr, _ := rc4Server(t, "INBOX", []imap.Flag{imap.FlagSeen}, server)
			if srep := e.sync(t, addr); srep.New != 1 {
				t.Fatalf("sync = %+v", srep)
			}
			dir := t.TempDir()
			writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{tc.file})
			e.run(t, dir, FormatMbox, "", Options{})
			if n := e.count(t, `SELECT count(*) FROM messages`); n != tc.messages {
				t.Fatalf("messages = %d, want %d", n, tc.messages)
			}
		})
	}
}

// Maildir: zwei Fassungen als zwei Dateien → zwei Nachrichten (die zweite
// wird nach dem Hashen noch einmal gelesen und gespeichert, byte-gleich).
func TestImportMaildirTwoVersions(t *testing.T) {
	sent, received := twins()
	dir := t.TempDir()
	for _, s := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i, raw := range [][]byte{sent, received, received} {
		if err := os.WriteFile(filepath.Join(dir, "cur", mdName("170000000"+string(rune('0'+i))+".M1P1.rc6:2,S")), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := newEnv(t)
	rep := e.run(t, dir, FormatMaildir, "", Options{})
	if rep.New != 2 || rep.Existing != 1 || e.twinCount(t) != 2 {
		t.Fatalf("new %d existing %d messages %d; want 2/1/2", rep.New, rep.Existing, e.twinCount(t))
	}
	for _, sum := range e.strs(t, `SELECT blob_sha256 FROM messages`) {
		rc, err := e.blobs.Get(sum)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(b, sent) && !bytes.Equal(b, received) {
			t.Fatalf("stored %q", b)
		}
	}
}
