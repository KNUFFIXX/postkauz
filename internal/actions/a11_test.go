package actions

import (
	"context"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A11 Teil 1: Eine Aktion mit Herkunft eines Entwurfs (draft_update,
// draft_discard wie draft_supersede) auf eine nie selbst geladene Kopie mit
// langem Umlaut-Betreff aus encoded-words (Fall aus A09 Teil 1b) endet
// done; außerhalb der Entwürfe-Ordner scheitert sie mit not_in_drafts.
func TestA11DraftSourcesInRunner(t *testing.T) {
	for _, src := range []string{store.ActionSourceDraftUpdate, store.ActionSourceDraftDiscard, store.ActionSourceDraftSupersede} {
		for _, inDrafts := range []bool{true, false} {
			t.Run(src+map[bool]string{true: "", false: " outside"}[inDrafts], func(t *testing.T) {
				st, acct, ids := env(t)
				ctx := context.Background()
				if inDrafts {
					if err := st.ExecForTest(`UPDATE mailboxes SET role = 'drafts' WHERE id = ?`, ids["INBOX"]); err != nil {
						t.Fatal(err)
					}
				}
				if err := st.ExecForTest(`INSERT INTO mailboxes(account_id, path, name, role, sync_phase, uidvalidity) VALUES (?, 'Papierkorb', 'Papierkorb', 'trash', 'ready', 9)`, acct); err != nil {
					t.Fatal(err)
				}
				trash, _ := st.QueryIntForTest(`SELECT id FROM mailboxes WHERE path = 'Papierkorb'`)
				a, err := st.CreateAction(ctx, store.NewAction{AccountID: acct, Kind: store.ActionTrash, State: store.ActionQueued,
					Params:      store.ActionParams{TargetMailboxID: int64(trash), TargetMailbox: "Papierkorb", TargetRole: "trash", Source: src, DraftID: 1},
					RequestedBy: "key:agent", Targets: []store.ActionTarget{{MessageID: 2, MailboxID: ids["INBOX"], UID: 2, UIDValidity: 7}}})
				if err != nil {
					t.Fatal(err)
				}
				const head = "Message-ID: <b@example.org>\r\n"
				const tail = "From: demo@example.org\r\n\r\n"
				b := blobs()
				b["sha-b@example.org"] = head + a09Subject(" ") + tail + "B\r\n"
				f := &fakeRemote{headers: map[provider.UID]string{2: head + a09Subject("\r\n ") + tail}}
				r := &Runner{Store: st, Blobs: b}
				if _, err := r.Run(ctx, acct, f); err != nil {
					t.Fatal(err)
				}
				got, _ := st.Action(ctx, a.ID)
				if inDrafts && (got.State != store.ActionDone || got.Targets[0].Result != store.TargetOK) {
					t.Fatalf("in drafts: %s %s %+v", got.State, got.Error, got.Targets)
				}
				if !inDrafts && (got.Error != ReasonNotInDrafts || got.Targets[0].Result == store.TargetOK) {
					t.Fatalf("outside: %s %s %+v", got.State, got.Error, got.Targets)
				}
				if got.Params.DraftID != 1 || got.Params.Source != src {
					t.Fatalf("params = %+v", got.Params)
				}
			})
		}
	}
}
