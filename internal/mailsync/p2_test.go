package mailsync

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// TestP2UIDSearchCost misst die Kosten des Löschabgleichs per UID SEARCH
// (Prüfpunkt P2) an einem großen, schon gespiegelten Postfach. Nur
// ausdrücklich: POSTKAUZ_P2_USER (Dovecot-Nutzer mit Korpus) und
// POSTKAUZ_P2_DB (Datenbank nach `sync --full`). Ergebnisse stehen in
// spike/s0/REPORT.md (Nachtrag P2).
func TestP2UIDSearchCost(t *testing.T) {
	user, dbPath := os.Getenv("POSTKAUZ_P2_USER"), os.Getenv("POSTKAUZ_P2_DB")
	if user == "" || dbPath == "" {
		t.Skip("set POSTKAUZ_P2_USER and POSTKAUZ_P2_DB to measure (see spike/s0/REPORT.md, Nachtrag P2)")
	}
	ctx := context.Background()
	c, err := imapx.Dial(ctx, imapx.Config{Host: "127.0.0.1", Port: 11143, TLS: imapx.TLSNone, InsecureTestServer: true, CommandTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login(user, itPass); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	accts, _ := st.ListAccounts(ctx)
	boxes, _ := st.Mailboxes(ctx, accts[0].ID)
	var total int
	var tES, tPlain, tFlags, tLocal, tDiff time.Duration
	var bytesPlain int
	for _, mb := range boxes {
		if mb.Mirrored == 0 {
			continue
		}
		if _, err := c.Select(mb.Path, imapx.SelectOptions{ReadOnly: true, CondStore: true}); err != nil {
			t.Fatal(err)
		}
		s := time.Now()
		es, err := c.UIDSearch(0, imapx.SearchOptions{ESearch: true})
		tES += time.Since(s)
		if err != nil {
			t.Fatal(err)
		}
		s = time.Now()
		plain, err := c.UIDSearch(0, imapx.SearchOptions{})
		tPlain += time.Since(s)
		if err != nil || len(plain) != len(es) {
			t.Fatalf("%s: %d vs %d (%v)", mb.Path, len(plain), len(es), err)
		}
		for _, u := range plain {
			bytesPlain += len(strconv.FormatUint(uint64(u), 10)) + 1
		}
		s = time.Now()
		n := 0
		if err := c.FetchFlags(1, 0, imapx.FlagsOptions{}, func(imapx.FlagsData) { n++ }); err != nil {
			t.Fatal(err)
		}
		tFlags += time.Since(s)
		s = time.Now()
		local, err := st.MailboxUIDs(ctx, mb.ID)
		tLocal += time.Since(s)
		if err != nil {
			t.Fatal(err)
		}
		present := make([]provider.UID, len(es))
		for i, u := range es {
			present[i] = provider.UID(u)
		}
		s = time.Now()
		removed, missing := diff(local, present, mb.UIDNext)
		tDiff += time.Since(s)
		if len(removed)+len(missing) != 0 {
			t.Errorf("%s: removed %d, missing %d", mb.Path, len(removed), len(missing))
		}
		total += len(es)
		t.Logf("%-12s %6d UIDs", mb.Path, len(es))
	}
	t.Logf("total %d UIDs: UID SEARCH ALL with ESEARCH %s, without %s (about %d KB of UIDs on the wire)",
		total, tES.Round(time.Millisecond), tPlain.Round(time.Millisecond), bytesPlain/1024)
	t.Logf("FETCH 1:* (FLAGS) %s; local UID list %s; diff %s", tFlags.Round(time.Millisecond),
		tLocal.Round(time.Millisecond), tDiff.Round(time.Microsecond))
}
