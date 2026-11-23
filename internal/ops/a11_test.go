package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A11 Teil 1 (a), Befund B2: sync.request weist einen Ordner, den das Konto
// im Spiegel nicht hat, als Eingabefehler ab (vorher still eingereiht und
// im Lauf übersprungen). Ein bekannter Ordner wird eingereiht.
func TestA11SyncRequestUnknownMailbox(t *testing.T) {
	e := newMirrorEnv(t)
	if err := e.x.Registry.Register(SyncRequest(Deps{Accounts: e.st, Mailboxes: e.st, Drafts: &drafts.Service{Store: e.st}})); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`INSERT INTO mailboxes(account_id, path, name, role, sync_phase, uidvalidity) VALUES (?, '[Gmail]/Entwürfe', 'Entwürfe', 'drafts', 'ready', 1)`, e.acct); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var ie *InputError
	if _, err := e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.request", Input: json.RawMessage(`{"mailbox":"[Gmail]/Drafts"}`)}); !errors.As(err, &ie) {
		t.Fatalf("unknown mailbox: %v", err)
	}
	if e.syncJobs(t) != 0 {
		t.Fatal("job queued for an unknown mailbox")
	}
	res, err := e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.request", Input: json.RawMessage(`{"mailbox":"[Gmail]/Entwürfe"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if o := res.Output.(SyncRequestOutput); o.Status != "queued" || e.syncJobs(t) != 1 {
		t.Fatalf("known mailbox: %+v", o)
	}
}

// A11 Teil 1 (c): drafts.get nennt cleanup_action_ids, actions.get die
// Herkunft (source, draft_id); nach der Ausführung nennt drafts.get die
// verschobene Kopie nicht mehr als frühere Kopie.
func TestA11DraftsGetCleanupActions(t *testing.T) {
	_, x := supEnv(t, assistantKey)
	// Entwurf 1 (eigene, unveränderte Kopie UID 9) wird durch Entwurf 2
	// ersetzt.
	res, err := execJSON(t, x, assistantKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	actionID := res.Output.(SupersedeOutput).ActionID
	g, err := execJSON(t, x, assistantKey, "drafts.get", `{"draft_id":1}`, false)
	if err != nil {
		t.Fatal(err)
	}
	got := g.Output.(DraftDetail)
	if len(got.CleanupActionIDs) != 1 || got.CleanupActionIDs[0] != actionID {
		t.Fatalf("cleanup_action_ids = %v, want [%d]", got.CleanupActionIDs, actionID)
	}
	a, err := execJSON(t, x, assistantKey, "actions.get", `{"action_id":`+strconv.FormatInt(actionID, 10)+`}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if v := a.Output.(ActionView); v.Source != "draft_supersede" || v.DraftID != 1 {
		t.Fatalf("action view = %+v", v)
	}
}

// remote_note eines abgelegten Entwurfs nennt frühere Kopien nur, solange
// sie laut Spiegel im Entwürfe-Ordner liegen; mit previous_copies = keep
// der alte Text.
func TestA11RemoteNoteEarlierCopies(t *testing.T) {
	d := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemotePushed, RemoteUID: 9}
	if n := remoteNoteFor(d, nil, false); n != "in the Drafts folder (uid 9)" {
		t.Fatalf("no earlier: %q", n)
	}
	if n := remoteNoteFor(d, []int64{3}, false); !strings.Contains(n, "uid 3") || !strings.Contains(n, "go to the trash") {
		t.Fatalf("earlier: %q", n)
	}
	if n := remoteNoteFor(d, []int64{3}, true); !strings.Contains(n, "stay there, nothing is deleted") {
		t.Fatalf("keep: %q", n)
	}
	p := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemotePending, RemoteUID: 9, PushedContentHash: "h"}
	if n := remoteNoteFor(p, nil, false); !strings.Contains(n, "goes to the trash") || strings.Contains(n, "nothing is deleted") {
		t.Fatalf("pending: %q", n)
	}
	if n := remoteNoteFor(p, nil, true); !strings.Contains(n, "nothing is deleted") {
		t.Fatalf("pending keep: %q", n)
	}
}
