package store

import (
	"context"
	"testing"
)

// S2.7 (Entscheid 43): MirrorDraftCopies liefert die Kopien mit derselben
// Message-ID nur aus den genannten Ordnern, nach UID aufsteigend, mit der
// UIDVALIDITY des Ordners.
func TestMirrorDraftCopies(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncMailboxes(ctx, acc, []MailboxSpec{{Path: "Drafts", Name: "Drafts", Role: "drafts"},
		{Path: "All", Name: "All", Role: "all"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acc)
	ids := map[string]int64{}
	for _, b := range boxes {
		ids[b.Path] = b.ID
	}
	for _, x := range []struct {
		box  string
		uids []uint32
	}{{"Drafts", []uint32{3, 7, 9}}, {"All", []uint32{40}}} {
		var add []NewMessage
		for i, u := range x.uids {
			mid := "pk.1@example.org"
			if x.box == "Drafts" && i == 2 {
				mid = "pk.2@example.org"
			}
			add = append(add, NewMessage{Link: LinkFlags{UID: u, Draft: true}, MessageID: mid, ContentHash: x.box + string(rune('a'+i)), Size: int64(10 + i)})
		}
		if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acc, MailboxID: ids[x.box], New: add,
			State: &MailboxState{UIDValidity: 5, UIDNext: 100, Phase: PhaseReady, Total: len(add)}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.MirrorDraftCopies(ctx, []int64{ids["Drafts"], 0}, "pk.1@example.org")
	if err != nil || len(got) != 2 || got[0].UID != 3 || got[1].UID != 7 || got[0].UIDValidity != 5 || got[0].MailboxID != ids["Drafts"] {
		t.Fatalf("copies = %+v, %v", got, err)
	}
	if got, err := s.MirrorDraftCopies(ctx, []int64{ids["Drafts"]}, "pk.9@example.org"); err != nil || len(got) != 0 {
		t.Fatalf("unknown id = %+v, %v", got, err)
	}
}
