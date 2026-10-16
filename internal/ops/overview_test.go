package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// mailboxes.overview: ungelesene und gelesene Köpfe eines Ordners als zwei
// Suchergebnisse; unbekannter Ordner oder fremdes Konto: nicht gefunden.
func TestMailboxesOverview(t *testing.T) {
	e := newMirrorEnv(t)
	e.seed(t)
	ctx := context.Background()
	if _, err := e.st.IndexBatch(ctx, 0, 100, search.NewStemmer().Build); err != nil {
		t.Fatal(err)
	}
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	out, err := e.run("mailboxes.overview", map[string]any{"account": "demo", "mailbox": boxes[0].ID, "since": "3650d", "limit": 5}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(MailboxOverviewOutput)
	if !o.Untrusted || o.Account != "demo" || o.Mailbox != "INBOX" || len(o.Unread.Hits) != 0 || len(o.Read.Hits) != 1 ||
		o.Read.Hits[0].Subject != "Hallo" || !o.Read.Untrusted {
		t.Fatalf("overview = %+v", o)
	}
	if _, err := e.run("mailboxes.overview", map[string]any{"account": "demo", "mailbox": 9999}, "", false); !errors.Is(err, store.ErrMailboxNotFound) {
		t.Fatalf("unknown mailbox: %v", err)
	}
	in := demoInput()
	in["name"] = "zweite"
	if _, err := e.run("accounts.add", in, canary, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("mailboxes.overview", map[string]any{"account": "zweite", "mailbox": boxes[0].ID}, "", false); !errors.Is(err, store.ErrMailboxNotFound) {
		t.Fatalf("mailbox of another account: %v", err)
	}
}

// Zugehörigkeit im Register: attachments.text und threads.get liefern
// „nicht gefunden", wenn Nachricht oder Konto nicht passen.
func TestAttachmentAndThreadMembership(t *testing.T) {
	e := newMirrorEnv(t)
	id := e.seed(t)
	ctx := context.Background()
	atts, _ := e.st.AttachmentsByMessage(ctx, id)
	in := demoInput()
	in["name"] = "zweite"
	if _, err := e.run("accounts.add", in, canary, false); err != nil {
		t.Fatal(err)
	}
	ok := map[string]any{"id": atts[0].ID, "message": id, "account": "demo"}
	if _, err := e.run("attachments.text", ok, "", false); err != nil {
		t.Fatalf("matching attachment: %v", err)
	}
	for _, bad := range []map[string]any{
		{"id": atts[0].ID, "message": id + 1},
		{"id": atts[0].ID, "account": "zweite"},
	} {
		if _, err := e.run("attachments.text", bad, "", false); !errors.Is(err, store.ErrAttachmentNotFound) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	if _, err := e.run("threads.get", map[string]any{"message": id, "account": "demo"}, "", false); err != nil {
		t.Fatalf("thread of demo: %v", err)
	}
	if _, err := e.run("threads.get", map[string]any{"message": id, "account": "zweite"}, "", false); !errors.Is(err, store.ErrThreadNotFound) {
		t.Fatalf("thread of another account: %v", err)
	}
}
