package actions

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// envAll erweitert env um weitere Ordner (Pfad → Rolle, UIDVALIDITY 9 + i)
// und verknüpft Nachrichten dort (Pfad → Nachricht → UID).
func envAll(t *testing.T, extra []store.MailboxSpec, links map[string]map[int64]uint32) (*store.SQLite, int64, map[string]int64) {
	t.Helper()
	st, acct, ids := env(t)
	ctx := context.Background()
	specs := []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}, {Path: "Archiv", Name: "Archiv", Role: "archive"}}
	specs = append(specs, extra...)
	if _, _, err := st.SyncMailboxes(ctx, acct, specs); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acct)
	for _, b := range boxes {
		ids[b.Path] = b.ID
	}
	for i, sp := range extra {
		var ls []store.Link
		for msg, uid := range links[sp.Path] {
			ls = append(ls, store.Link{MessageID: msg, Flags: store.LinkFlags{UID: uid}})
		}
		if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: ids[sp.Path], Links: ls,
			State: &store.MailboxState{UIDValidity: uint32(9 + i), UIDNext: 100, Phase: store.PhaseReady, Total: len(ls)}}); err != nil {
			t.Fatal(err)
		}
	}
	return st, acct, ids
}

// V9: Liegt eine Nachricht schon im Ziel (hier INBOX) und sonst nur in
// „Alle Nachrichten“, wird sie übersprungen; die Kopie in „Alle
// Nachrichten“ wird nicht zum Ziel.
func TestPlanMoveSkipsMessageAlreadyInTarget(t *testing.T) {
	st, _, ids := envAll(t, []store.MailboxSpec{{Path: "[Gmail]/Alle Nachrichten", Name: "Alle Nachrichten", Role: "all"}},
		map[string]map[int64]uint32{"[Gmail]/Alle Nachrichten": {1: 11, 2: 12}})
	ctx := context.Background()
	svc := &Service{Store: st}
	dest := Target{MailboxID: ids["INBOX"], Path: "INBOX", Role: "inbox"}
	p, err := svc.PlanMove(ctx, []int64{1}, "", dest)
	if !slices.Equal(p.Skipped, []int64{1}) || len(p.Targets) != 0 {
		t.Fatalf("plan = %+v, err %v", p, err)
	}
	var de *Error
	if err == nil || !errors.As(err, &de) || de.Reason != ReasonNothingToDo {
		t.Fatalf("err = %v", err)
	}
	// Gegenprobe: Liegt außer im Ziel noch eine Kopie in einem anderen
	// Ordner (Nachricht 2: Archiv und INBOX), wird diese verschoben.
	p, err = svc.PlanMove(ctx, []int64{2}, "", Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"})
	if err != nil || len(p.Targets) != 1 || p.Targets[0].Mailbox != "INBOX" || len(p.Skipped) != 0 {
		t.Fatalf("plan = %+v, err %v", p, err)
	}
}
