package mailsync

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/jobs"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
	"github.com/emersion/go-imap/v2"
)

// Unsichtbare Zeichen in Betreff, Absendername und Dateiname werden beim
// Sync entfernt: Die Suche findet den Betreff, Listen und MCP zeigen ihn
// so, wie ein Mensch ihn liest. Bestand mit älterer Aufbereitung wird
// nachgezogen (CleanVersion).
func TestSyncStripsInvisibleCharactersFromHeaders(t *testing.T) {
	const zwSubject = "Neben\u200bkosten\u2060abrechnung\U000E0041 2025"
	enc := func(s string) string { return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?=" }
	raw := "Message-ID: <zw1@example.org>\r\nFrom: " + enc("Haus\u202everwaltung") + " <a@example.org>\r\n" +
		"To: " + enc("Mie\u200bter") + " <b@example.org>\r\nSubject: " + enc(zwSubject) + "\r\n" +
		"Date: Mon, 06 Jan 2025 10:00:00 +0100\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b1\r\n\r\n--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nText\r\n" +
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Disposition: attachment; filename*=UTF-8''Rech%E2%80%AEnung.txt\r\n\r\nInhalt\r\n--b1--\r\n"
	addr, u := memServer(t, false)
	if _, err := u.Append("INBOX", lit([]byte(raw)), &imap.AppendOptions{Time: time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e := newEnv(t, addr)
	eng := e.engine(t, connect(t, addr, 0))
	eng.Jobs = &jobs.Runner{Store: e.st, Blobs: e.blobs}
	check := func(round string) {
		t.Helper()
		db := e.st.DBForTest()
		var subject, from, to, file string
		if err := db.QueryRow(`SELECT m.subject, m.from_addr, m.to_addrs, a.filename FROM messages m
			JOIN attachments a ON a.message_id = m.id`).Scan(&subject, &from, &to, &file); err != nil {
			t.Fatalf("%s: %v", round, err)
		}
		if subject != "Nebenkostenabrechnung 2025" || from != "Hausverwaltung <a@example.org>" ||
			!strings.Contains(to, `"Mieter"`) || file != "Rechnung.txt" {
			t.Fatalf("%s: invisible characters kept: %q %q %q %q", round, subject, from, to, file)
		}
		q, err := search.Parse("subject:Nebenkostenabrechnung", time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		res, err := e.st.Search(ctx, store.SearchParams{Query: q, AccountID: e.acct, Limit: 10, Now: time.Now()})
		if err != nil || len(res.Hits) != 1 {
			t.Fatalf("%s: search = %+v %v", round, res.Hits, err)
		}
	}
	if _, err := eng.SyncAll(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	check("first sync")

	// Bestand einer älteren Version: Kopffelder noch mit unsichtbaren
	// Zeichen, Index entsprechend.
	for _, q := range []string{`UPDATE messages SET subject = '` + zwSubject + `', from_addr = 'Haus` + "\u202e" + `verwaltung <a@example.org>'`,
		`UPDATE attachments SET filename = 'Rech` + "\u202e" + `nung.txt'`, `UPDATE bodies SET clean_version = 1`} {
		if _, err := e.st.DBForTest().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := eng.SyncAll(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	check("after cleanup of older messages")
}
