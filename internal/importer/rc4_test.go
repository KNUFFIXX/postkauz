package importer

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// Restfall von C2 (Befund mit Echtdaten 25.10.2026, rc4): Thunderbird
// ergänzt beim Speichern ein fehlendes Zeilenende am Schluss bzw. eine
// abgeschnittene abschließende MIME-Grenze. Die Kopie im Spiegel ist dann
// bis zu 6 Bytes größer als die des Servers; der Sync verknüpft sie per
// Message-ID ohne Inhaltsabruf, außer bei Entwürfen. Alles synthetisch.

// rc4Multipart ist eine multipart-Nachricht mit CRLF, die mit der
// abschließenden Grenze und einem Zeilenende endet.
func rc4Multipart(n int) []byte {
	return []byte(fmt.Sprintf("From: absender@example.org\r\nTo: empfaenger@example.org\r\nSubject: rc4 %d\r\n"+
		"Message-ID: <rc4-%d@example.org>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"grenze-%d\"\r\n\r\n"+
		"--grenze-%d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nText %d\r\n--grenze-%d--\r\n", n, n, n, n, n, n))
}

// rc4Server: IMAP-Server im Prozess mit einer Nachricht in folder (INBOX
// gibt es immer), Zähler der Inhaltsabrufe.
func rc4Server(t testing.TB, folder string, flags []imap.Flag, raw []byte) (string, *atomic.Int64) {
	t.Helper()
	u := imapmemserver.NewUser(memUser, string(memPass))
	_ = u.Create("INBOX", nil)
	if folder != "INBOX" {
		_ = u.Create(folder, nil)
	}
	if _, err := u.Append(folder, rawLit{bytes.NewReader(raw)}, &imap.AppendOptions{Flags: flags,
		Time: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	bodies := &atomic.Int64{}
	return serveMem(t, u, bodies), bodies
}

func TestSyncLinksDespiteSmallSizeDifference(t *testing.T) {
	full := rc4Multipart(1)
	noEOL := bytes.TrimSuffix(full, []byte("\r\n")) // endet mit "--grenze-1--"
	seen := []imap.Flag{imap.FlagSeen}
	for _, tc := range []struct {
		name           string
		folder         string
		flags          []imap.Flag
		server, mirror []byte
		tolerant       int  // erwarteter Zähler linked despite size difference
		fetched        bool // Inhalt geladen (FETCH BODY[])
		messages       int
	}{
		// Server endet mit der abschließenden Grenze ohne Zeilenende;
		// Thunderbird hat CRLF ergänzt: Spiegel +2.
		{"boundary without line end", "INBOX", seen, noEOL, full, 1, false, 1},
		// Server endet mit "--grenze-1" ohne "--" und ohne Zeilenende;
		// Thunderbird hat "--" und CRLF ergänzt: Spiegel +4.
		{"boundary without closing dashes", "INBOX", seen, full[:len(full)-4], full, 1, false, 1},
		// Gemessen auch +5 (ein Zeichen mehr fehlt am Server; Ursache offen,
		// hier nur abgesichert).
		{"cut inside boundary", "INBOX", seen, full[:len(full)-5], full, 1, false, 1},
		// Gleiche Message-ID, 2 Bytes kleiner, aber anderer Betreff
		// (wiederverwendete Message-ID): keine Verknüpfung per Kopfdaten;
		// der Inhalt ist anders, also eine zweite Nachricht.
		{"other subject", "INBOX", seen, bytes.Replace(noEOL, []byte("Subject: rc4 1"), []byte("Subject: rc4 X"), 1), full, 0, true, 2},
		// Entwürfe: exakt, Inhalt wird geladen (dann per content_hash).
		{"drafts folder", "Drafts", seen, noEOL, full, 0, true, 1},
		{"draft flag", "INBOX", []imap.Flag{imap.FlagSeen, imap.FlagDraft}, noEOL, full, 0, true, 1},
		// 7 Bytes: außerhalb der Toleranz, keine Verknüpfung per Kopfdaten
		// (der Inhalt ist hier wirklich anders: zweite Nachricht).
		{"difference 7", "INBOX", seen, full, append(append([]byte(nil), full...), "abcde\r\n"...), 0, true, 2},
		// Server größer als Spiegel: keine Toleranz (geladen, per Hash
		// verknüpft).
		{"server larger", "INBOX", seen, append(append([]byte(nil), full...), "\r\n"...), full, 0, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Thunderbird: Nachricht wie gespeichert, dazu die Rahmen-Leerzeile.
			writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{tc.mirror})
			e := newEnv(t)
			if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 1 {
				t.Fatalf("import = %+v", rep)
			}
			if got := e.blobOf(t, "rc4-1@example.org"); !bytes.Equal(got, tc.mirror) {
				t.Fatalf("mirror copy = %q, want %q", got, tc.mirror)
			}
			addr, bodies := rc4Server(t, tc.folder, tc.flags, tc.server)
			srep := e.sync(t, addr)
			if srep.Errors != 0 {
				t.Fatalf("sync errors = %d", srep.Errors)
			}
			if got := bodies.Load() > 0; got != tc.fetched {
				t.Fatalf("FETCH BODY[] commands = %d, want fetched %v", bodies.Load(), tc.fetched)
			}
			if srep.LinkedSizeDiff != tc.tolerant {
				t.Fatalf("linked despite size difference = %d, want %d", srep.LinkedSizeDiff, tc.tolerant)
			}
			if n := e.count(t, `SELECT count(*) FROM messages`); n != tc.messages {
				t.Fatalf("messages = %d, want %d", n, tc.messages)
			}
			if tc.messages == 1 {
				// Verknüpft: eine Nachricht mit Ordnerkopie und Import-Ordner;
				// der Spiegel behält die Fassung aus Thunderbird.
				if n := e.count(t, `SELECT count(*) FROM messages m WHERE EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = m.id)
					AND EXISTS (SELECT 1 FROM message_import_folders mf WHERE mf.message_id = m.id)`); n != 1 {
					t.Fatal("message not linked to the server copy")
				}
				if got := e.blobOf(t, "rc4-1@example.org"); !bytes.Equal(got, tc.mirror) {
					t.Fatalf("mirror copy changed to %q", got)
				}
			}
		})
	}
}

// Ein exakter und ein toleranter Kandidat mit gleicher Message-ID: der
// exakte gewinnt (kein Inhaltsabruf, Zähler 0). Der Import legt eine
// zweite Kopie mit gleicher Message-ID nie an; sie kommt hier als Zeile
// direkt in den Bestand (gleiche Rohmail, 2 Bytes kleinere Größe).
func TestSyncPrefersExactSizeCandidate(t *testing.T) {
	full := rc4Multipart(2)
	noEOL := bytes.TrimSuffix(full, []byte("\r\n"))
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{full}) // Spiegel +2
	e := newEnv(t)
	if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 1 {
		t.Fatalf("import = %+v", rep)
	}
	if err := e.st.ExecForTest(`INSERT INTO messages (account_id, message_id_header, size, blob_sha256, content_hash, internal_date, created_at)
		SELECT account_id, message_id_header, size - 2, blob_sha256, content_hash || '-exact', internal_date, created_at FROM messages`); err != nil {
		t.Fatal(err)
	}
	exactID := e.count(t, `SELECT id FROM messages WHERE content_hash LIKE '%-exact'`)
	addr, bodies := rc4Server(t, "INBOX", []imap.Flag{imap.FlagSeen}, noEOL)
	srep := e.sync(t, addr)
	if bodies.Load() != 0 || srep.LinkedSizeDiff != 0 || srep.Errors != 0 {
		t.Fatalf("fetched %d, tolerant %d, errors %d; want 0/0/0", bodies.Load(), srep.LinkedSizeDiff, srep.Errors)
	}
	if n := e.count(t, `SELECT message_id FROM message_mailboxes`); n != exactID {
		t.Fatalf("linked message %d, want the exact one %d", n, exactID)
	}
}

// Gmail führt eine Mail in INBOX und „Alle Nachrichten": Der Zähler nennt
// verschiedene Nachrichten, nicht Ordnerkopien.
func TestSyncLinkedSizeDiffCountsMessages(t *testing.T) {
	full := rc4Multipart(3)
	noEOL := bytes.TrimSuffix(full, []byte("\r\n"))
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "INBOX"), [][]byte{full})
	e := newEnv(t)
	if rep := e.run(t, dir, FormatMbox, "", Options{}); rep.New != 1 {
		t.Fatalf("import = %+v", rep)
	}
	u := imapmemserver.NewUser(memUser, string(memPass))
	for _, f := range []string{"INBOX", "Alle Nachrichten"} {
		_ = u.Create(f, nil)
		if _, err := u.Append(f, rawLit{bytes.NewReader(noEOL)}, &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen},
			Time: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	bodies := &atomic.Int64{}
	srep := e.sync(t, serveMem(t, u, bodies))
	if bodies.Load() != 0 || srep.LinkedSizeDiff != 1 || srep.Errors != 0 {
		t.Fatalf("fetched %d, linked despite size difference %d (want 1 message), errors %d", bodies.Load(), srep.LinkedSizeDiff, srep.Errors)
	}
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes`); n != 2 {
		t.Fatalf("folder copies = %d, want 2", n)
	}
}
