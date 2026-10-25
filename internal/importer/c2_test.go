package importer

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// Betreiber-Entscheid C2 (25.10.2026): Import byte-genau, damit der
// Erstabgleich nach einem Thunderbird-Import über Message-ID und Größe
// verknüpft, ohne den Inhalt erneut zu laden. Alles synthetisch.

// c2Raw ist eine Servernachricht (CRLF) mit blanks Leerzeilen am Ende;
// mid leer = ohne Message-ID.
func c2Raw(n int, mid string, blanks int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: absender@example.org\r\nTo: empfaenger@example.org\r\nSubject: C2 %d\r\n", n)
	if mid != "" {
		fmt.Fprintf(&b, "Message-ID: <%s>\r\n", mid)
	}
	fmt.Fprintf(&b, "Date: Thu, 15 Oct 2026 10:%02d:00 +0200\r\n\r\nText %d\r\nFrom der Zeile\r\nEnde\r\n", n, n)
	b.WriteString(strings.Repeat("\r\n", blanks))
	return []byte(b.String())
}

// writeTB schreibt die Nachrichten wie Thunderbird (Trennzeile,
// X-Mozilla-Zeilen, ">From "-Quoting, eine Leerzeile als Rahmen).
func writeTB(t testing.TB, path string, raws [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, raw := range raws {
		buf.WriteString("From - Thu Oct 15 10:00:00 2026\r\nX-Mozilla-Status: 0001\r\nX-Mozilla-Status2: 00000000\r\n")
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

// oldForm ist eine Nachricht, wie der Import bis 0.2.0-rc2 sie speicherte:
// alle Zeilenenden am Ende weg, genau eines angehängt.
func oldForm(raw []byte) []byte {
	return append(bytes.TrimRight(raw, "\r\n"), '\r', '\n')
}

func (e *env) blobOf(t testing.TB, mid string) []byte {
	t.Helper()
	sums := e.strs(t, `SELECT blob_sha256 FROM messages WHERE message_id_header = ?`, mid)
	if len(sums) != 1 {
		t.Fatalf("%s: %d messages", mid, len(sums))
	}
	rc, err := e.blobs.Get(sums[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mbox und Maildir: gespeicherte Rohmail und Größe gleich dem Original,
// mit 0 bis 3 Leerzeilen am Ende.
func TestImportByteExactMboxAndMaildir(t *testing.T) {
	var raws [][]byte
	for blanks := range 4 {
		raws = append(raws, c2Raw(blanks, fmt.Sprintf("c2-%d@example.org", blanks), blanks))
	}
	// Maildir: dieselben Dateien plus eine ohne Zeilenende am Schluss.
	noEOL := []byte("Subject: ohne Ende\r\nMessage-ID: <c2-noeol@example.org>\r\n\r\nletzte Zeile")
	for _, format := range []string{FormatMbox, FormatMaildir} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			all := raws
			if format == FormatMbox {
				writeTB(t, filepath.Join(dir, "INBOX"), raws)
			} else {
				all = append(append([][]byte(nil), raws...), noEOL)
				for _, s := range []string{"cur", "new", "tmp"} {
					if err := os.MkdirAll(filepath.Join(dir, s), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				for i, raw := range all {
					if err := os.WriteFile(filepath.Join(dir, "cur", fmt.Sprintf("17000000%02d.M1P1.c2:2,S", i)), raw, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			e := newEnv(t)
			if rep := e.run(t, dir, format, "", Options{}); rep.New != len(all) || rep.Errors != 0 {
				t.Fatalf("import = %+v", rep)
			}
			for _, raw := range all {
				mid := headerMessageID(raw)
				if got := e.blobOf(t, mid); !bytes.Equal(got, raw) {
					t.Errorf("%s: stored %q, want %q", mid, got, raw)
				}
				if n := e.count(t, `SELECT size FROM messages WHERE message_id_header = ?`, mid); n != len(raw) {
					t.Errorf("%s: size %d, want %d", mid, n, len(raw))
				}
			}
		})
	}
}

// Ein Bestand aus dem alten Import (gekürzte Nachrichten) und ein erneuter
// Import desselben mbox mit dem neuen Verhalten: keine Doppelten, auch ohne
// Message-ID (content_hash-Varianten, hash.go).
func TestReimportOverOldTrimmedStore(t *testing.T) {
	var raws, old [][]byte
	for i := range 4 {
		mid := ""
		if i%2 == 0 {
			mid = fmt.Sprintf("c2-old-%d@example.org", i)
		}
		raw := c2Raw(i, mid, i)
		raws = append(raws, raw)
		old = append(old, oldForm(raw))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "INBOX")
	// Der alte Bestand: Das neue Verhalten speichert eine Datei aus
	// gekürzten Nachrichten genau so, wie das alte die echte Datei
	// gespeichert hätte.
	writeTB(t, path, old)
	e := newEnv(t)
	if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != len(raws) {
		t.Fatalf("old import = %+v", rep)
	}
	for i, sum := range e.strs(t, `SELECT blob_sha256 FROM messages ORDER BY id`) {
		rc, err := e.blobs.Get(sum)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(b, old[i]) {
			t.Fatalf("simulated old store: message %d = %q, want %q", i, b, old[i])
		}
	}
	// Dieselbe Datei, jetzt mit den Leerzeilen am Ende (andere Größe, also
	// neu gelesen).
	writeTB(t, path, raws)
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(path, future, future)
	rep := e.run(t, dir, FormatMbox, "", Options{})
	if rep.New != 0 || rep.Existing != len(raws) || rep.Errors != 0 {
		t.Fatalf("re-import = new %d existing %d errors %d, want 0/%d/0", rep.New, rep.Existing, rep.Errors, len(raws))
	}
	if n := e.count(t, `SELECT count(*) FROM messages`); n != len(raws) {
		t.Fatalf("messages = %d, want %d", n, len(raws))
	}
}

// c2Server: IMAP-Server im Prozess, INBOX mit den Rohmails.
func c2Server(t testing.TB, raws [][]byte) string {
	t.Helper()
	addr, _ := c2CountingServer(t, raws)
	return addr
}

// c2CountingServer wie c2Server, dazu der Zähler der Inhaltsabrufe
// (FETCH BODY[]).
func c2CountingServer(t testing.TB, raws [][]byte) (string, *atomic.Int64) {
	t.Helper()
	u := imapmemserver.NewUser(memUser, string(memPass))
	_ = u.Create("INBOX", nil)
	for _, raw := range raws {
		if _, err := u.Append("INBOX", rawLit{bytes.NewReader(raw)}, &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen},
			Time: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	bodies := &atomic.Int64{}
	return serveMem(t, u, bodies), bodies
}

// Erstabgleich nach einem Import (ADR-003): Eine Nachricht mit zwei
// Leerzeilen am Ende hat nach dem byte-genauen Import die Größe des Servers
// und wird über Message-ID und Größe verknüpft, ohne Inhaltsabruf (Zähler
// 0). Weicht die Größe ab (Bestand aus dem alten Import), lädt der Sync den
// Inhalt, verknüpft per content_hash und zählt sie samt Bytes.
func TestSyncAfterImportLinksByHeader(t *testing.T) {
	raw := c2Raw(1, "c2-sync@example.org", 2)
	for _, tc := range []struct {
		name      string
		file      []byte
		wantCount int
		wantBytes int64
	}{
		{"same size", raw, 0, 0},
		{"size differs", oldForm(raw), 1, int64(len(raw))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{tc.file})
			e := newEnv(t)
			if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 1 {
				t.Fatalf("import = %+v", rep)
			}
			addr, bodies := c2CountingServer(t, [][]byte{raw})
			srep := e.sync(t, addr)
			// Direkt am Server gemessen: ohne Inhaltsabruf heißt kein
			// FETCH BODY[] (Köpfe und Größe kommen über BODY.PEEK[HEADER]).
			if got := bodies.Load(); (got == 0) != (tc.wantCount == 0) {
				t.Fatalf("FETCH BODY[] commands = %d, want %s", got, map[bool]string{true: "0", false: "at least 1"}[tc.wantCount == 0])
			}
			if srep.Errors != 0 || srep.New != 1 || srep.Mirrored != 1 {
				t.Fatalf("sync = errors %d new %d mirrored %d", srep.Errors, srep.New, srep.Mirrored)
			}
			if srep.KnownDownloaded != tc.wantCount || srep.KnownDownloadedBytes != tc.wantBytes {
				t.Fatalf("already known, downloaded anyway = %d (%d bytes), want %d (%d bytes)",
					srep.KnownDownloaded, srep.KnownDownloadedBytes, tc.wantCount, tc.wantBytes)
			}
			if len(srep.Mailboxes) != 1 || srep.Mailboxes[0].KnownDownloaded != tc.wantCount {
				t.Fatalf("mailbox reports = %+v", srep.Mailboxes)
			}
			if n := e.count(t, `SELECT count(*) FROM messages`); n != 1 {
				t.Fatalf("messages = %d, want 1 (no duplicate)", n)
			}
			if n := e.count(t, `SELECT count(*) FROM messages m JOIN message_mailboxes mm ON mm.message_id = m.id
				WHERE m.deleted_remote_at IS NULL`); n != 1 {
				t.Fatalf("linked copies = %d, want 1", n)
			}
		})
	}
}

// writeNoFrame schreibt eine mboxrd-Datei ohne Rahmen-Leerzeile: Auf das
// Ende einer Nachricht folgt direkt die nächste Trennzeile bzw. das
// Dateiende (andere Programme als Thunderbird, letzte Nachricht einer
// Datei).
func writeNoFrame(t testing.TB, path string, raws [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, raw := range raws {
		buf.WriteString("From someone@example.org Thu Oct 15 10:00:00 2026\r\n")
		for _, l := range bytes.SplitAfter(raw, []byte("\n")) {
			if bytes.HasPrefix(l, []byte("From ")) {
				buf.WriteByte('>')
			}
			buf.Write(l)
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Gegenprüfung C2 (Wichtig 1): Ohne Rahmen-Leerzeile hält der Leser eine
// echte Leerzeile für den Rahmen; die Kopie weicht dann um ein Zeilenende
// ab, bei beliebig vielen Leerzeilen am Ende. Die Varianten ±1 finden sie
// in beiden Richtungen (Import, dann Sync; Sync, dann Import), mit und ohne
// Message-ID; ein alter gekürzter Bestand wird weiter gefunden.
func TestNoFrameLineNoDuplicates(t *testing.T) {
	for k := range 6 {
		for _, mid := range []string{fmt.Sprintf("nf-%d@example.org", k), ""} {
			raw := c2Raw(k, mid, k)
			name := fmt.Sprintf("%d blank lines, message-id %v", k, mid != "")
			t.Run("import then sync/"+name, func(t *testing.T) {
				dir := t.TempDir()
				writeNoFrame(t, filepath.Join(dir, "INBOX"), [][]byte{raw})
				e := newEnv(t)
				if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 1 {
					t.Fatalf("import = %+v", rep)
				}
				srep := e.sync(t, c2Server(t, [][]byte{raw}))
				if n := e.count(t, `SELECT count(*) FROM messages`); n != 1 || srep.Errors != 0 {
					t.Fatalf("messages = %d (errors %d), want 1 (no duplicate)", n, srep.Errors)
				}
			})
			t.Run("sync then import/"+name, func(t *testing.T) {
				e := newEnv(t)
				if srep := e.sync(t, c2Server(t, [][]byte{raw})); srep.New != 1 {
					t.Fatalf("sync = %+v", srep)
				}
				dir := t.TempDir()
				writeNoFrame(t, filepath.Join(dir, "INBOX"), [][]byte{raw})
				if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 0 || rep.Existing != 1 {
					t.Fatalf("import = new %d existing %d, want 0/1", rep.New, rep.Existing)
				}
				if n := e.count(t, `SELECT count(*) FROM messages`); n != 1 {
					t.Fatalf("messages = %d, want 1", n)
				}
			})
			t.Run("old trimmed store then sync/"+name, func(t *testing.T) {
				dir := t.TempDir()
				writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{oldForm(raw)})
				e := newEnv(t)
				if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 1 {
					t.Fatalf("import = %+v", rep)
				}
				e.sync(t, c2Server(t, [][]byte{raw}))
				if n := e.count(t, `SELECT count(*) FROM messages`); n != 1 {
					t.Fatalf("messages = %d, want 1", n)
				}
			})
		}
	}
}
