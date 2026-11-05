package drafts

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// W2 (Gegenprüfung S2.6–S2.8): Prüfung nach dem Spiegeln bei einem großen
// Gesendet-Ordner (22.000 Nachrichten, wie „Alle Nachrichten" bei Gmail)
// und 30 offenen Entwürfen. „steady" ist eine Runde ohne neue Nachricht,
// „one_new" eine mit genau einer. Aufruf:
//
//	go test -run '^$' -bench BenchmarkCheckAfterSync22k ./internal/drafts/
func BenchmarkCheckAfterSync22k(b *testing.B) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		b.Fatal(err)
	}
	acc, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		b.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acc, []store.MailboxSpec{{Path: "All", Name: "All", Role: "all"}}); err != nil {
		b.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acc)
	box := boxes[0].ID
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	svc := &Service{Store: st, Now: func() time.Time { return now }}
	msg := func(i int) store.NewMessage {
		to, _ := json.Marshal([]store.Addr{{Name: "Mieter", Address: fmt.Sprintf("m%d@example.com", i)}})
		// A05 Teil 2: jede ungerade Nachricht antwortet auf die gerade davor
		// (In-Reply-To; Verläufe aus zwei Nachrichten), damit der Weg über
		// den Verlauf Kandidaten findet.
		irt := ""
		if i%2 == 1 {
			irt = fmt.Sprintf("s%d@example.org", i-1)
		}
		return store.NewMessage{Link: store.LinkFlags{UID: uint32(i + 1), Seen: true}, MessageID: fmt.Sprintf("s%d@example.org", i),
			InReplyTo: irt,
			Subject:   fmt.Sprintf("AW: Re: Nebenkostenabrechnung 2025 Wohnung %d", i), From: "demo@example.org", To: to,
			Cc: json.RawMessage(`[{"address":"cc@example.org"}]`), DateHeader: now.Add(-time.Duration(i) * time.Minute),
			InternalDate: now, Size: int64(100 + i), TextPlain: "Text", TextUnquoted: "Text", ContentHash: fmt.Sprintf("h%d", i),
			CleanVersion: 1, Links: json.RawMessage("[]")}
	}
	const total = 22000
	for from := 0; from < total; from += 1000 {
		var batch []store.NewMessage
		for i := from; i < from+1000; i++ {
			batch = append(batch, msg(i))
		}
		if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acc, MailboxID: box, New: batch,
			State: &store.MailboxState{UIDValidity: 1, UIDNext: uint32(from + 1001), Phase: store.PhaseReady}}); err != nil {
			b.Fatal(err)
		}
	}
	for j := 0; j < 30; j++ {
		// Offene Entwürfe über ein halbes Jahr verteilt (verfallen nie).
		now = now.Add(-time.Duration(j) * 6 * 24 * time.Hour)
		if _, err := svc.Create(ctx, NewDraft{AccountID: acc, Kind: KindNew, Content: Content{To: []store.Addr{{Address: "x@example.com"}},
			Subject: fmt.Sprintf("Re: Frage %d", j), BodyMarkdown: "Hallo Welt, kurze Antwort."}}); err != nil {
			b.Fatal(err)
		}
		now = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	}
	if _, err := svc.CheckAfterSync(ctx, acc, CheckOptions{}); err != nil { // erste Runde: Fenster aller Entwürfe
		b.Fatal(err)
	}
	b.Run("steady", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			rep, err := svc.CheckAfterSync(ctx, acc, CheckOptions{})
			if err != nil || rep.Examined != 0 {
				b.Fatalf("%+v %v", rep, err)
			}
		}
	})
	next := total
	b.Run("one_new", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			b.StopTimer()
			m := msg(next)
			m.DateHeader = now
			next++
			if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acc, MailboxID: box, New: []store.NewMessage{m},
				State: &store.MailboxState{UIDValidity: 1, UIDNext: uint32(next + 1), Phase: store.PhaseReady}}); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			rep, err := svc.CheckAfterSync(ctx, acc, CheckOptions{})
			if err != nil || rep.Examined != 1 {
				b.Fatalf("%+v %v", rep, err)
			}
		}
	})
	// A05 Teil 2: dazu 30 offene Antwort-Entwürfe mit Original (Weg über
	// den Verlauf: je Lauf eine Abfrage je Entwurf, Kandidaten passen nicht
	// zu den Empfängern). Vorher/nachher in docs/versanderkennung.md.
	for j := 0; j < 30; j++ {
		orig, err := st.QueryIntForTest(fmt.Sprintf(`SELECT id FROM messages WHERE message_id_header = 's%d@example.org'`, j*700))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := svc.Create(ctx, NewDraft{AccountID: acc, Kind: KindReply, InReplyToMsg: int64(orig), Content: Content{
			To: []store.Addr{{Address: "x@example.com"}}, Subject: fmt.Sprintf("Re: Antwort %d", j),
			BodyMarkdown: "Hallo, vielen Dank für die Nachricht, wir melden uns morgen mit allen Unterlagen."}}); err != nil {
			b.Fatal(err)
		}
	}
	// Als abgelegt vermerkt (sonst überspränge der Weg über den Verlauf sie
	// vor der Abfrage, Bedingung f).
	if err := st.ExecForTest(`UPDATE drafts SET pushed_content_hash = 'bench-copy' WHERE kind = 'reply'`); err != nil {
		b.Fatal(err)
	}
	if _, err := svc.CheckAfterSync(ctx, acc, CheckOptions{}); err != nil {
		b.Fatal(err)
	}
	b.Run("steady_replies", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			rep, err := svc.CheckAfterSync(ctx, acc, CheckOptions{})
			if err != nil || rep.Examined != 0 {
				b.Fatalf("%+v %v", rep, err)
			}
		}
	})
	// A06 Nachbesserung 2: Sicherung (c) der ersetzten Entwürfe, eine
	// Abfrage je Original (über thread_refs und messages_thread statt über
	// alle Nachrichten des Kontos); Zeit je Abfrage.
	var origs []int64
	for j := 0; j < 30; j++ {
		id, err := st.QueryIntForTest(fmt.Sprintf(`SELECT id FROM messages WHERE message_id_header = 's%d@example.org'`, j*700))
		if err != nil {
			b.Fatal(err)
		}
		origs = append(origs, int64(id))
	}
	b.Run("edited_reply_lookup", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			o := origs[n%len(origs)]
			if err := st.DraftTx(ctx, func(x *store.DraftTx) error {
				_, err := x.EditedReplyInDrafts(acc, o, now.Add(-24*time.Hour))
				return err
			}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
