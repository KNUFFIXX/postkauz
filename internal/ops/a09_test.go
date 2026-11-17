package ops

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A09 Teil 1: drafts.get nennt bei einem über den Verlauf mit ergänzten
// Empfängern erkannten Entwurf closed_note „recipients added in the mail
// client (n)“, gelesen aus dem Audit; closed_reason bleibt leer.
func TestA09DraftGetNamesRecipientsAdded(t *testing.T) {
	for _, c := range []struct {
		name, params, note string
	}{
		{"added", `{"draft_id":%d,"sent_message_id":5,"recipients_added":2}`, "recipients added in the mail client (2)"},
		{"same recipients", `{"draft_id":%d,"sent_message_id":5}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDraftEnv(t, true)
			id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"a@example.org"}, "subject": "x",
				"body_markdown": "Text"}).(DraftsWriteOutput).DraftID
			const closed = int64(1793000000000)
			for _, q := range []string{
				fmt.Sprintf(`UPDATE drafts SET status = 'sent_by_human', closed_at = %d WHERE id = %d`, closed, id),
				fmt.Sprintf(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result, reason)
					VALUES (%d, 'system', 'system', 'drafts.sent_detected', '`+c.params+`', '["draft:%d"]', 'ok', 'thread')`, closed, id, id),
			} {
				if err := e.st.ExecForTest(q); err != nil {
					t.Fatal(err)
				}
			}
			got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
			if got.ClosedNote != c.note || got.ClosedReason != "" {
				t.Fatalf("closed_note = %q, closed_reason = %q", got.ClosedNote, got.ClosedReason)
			}
		})
	}
}

// A09 Teil 2 (b): Bei einem geschlossenen Entwurf gibt es keinen Hinweis
// zur Kopie mehr, der nur für offene stimmt; offene behalten ihre
// Hinweise, pushed_after_close bleibt.
func TestA09RemoteNoteClosed(t *testing.T) {
	for _, c := range []struct {
		name string
		d    store.Draft
		want string // Teilstring; "" = leer
	}{
		{"sent, missing", store.Draft{Status: string(drafts.StatusSentByHuman), RemoteState: drafts.RemoteMissing,
			To: []store.Addr{{Address: "a@example.org"}}}, ""},
		{"sent, missing, no recipients", store.Draft{Status: string(drafts.StatusSentByHuman), RemoteState: drafts.RemoteMissing,
			Kind: string(drafts.KindReply), InReplyToMsg: 7}, ""},
		{"abandoned, pushed", store.Draft{Status: string(drafts.StatusAbandoned), RemoteState: drafts.RemotePushed, RemoteUID: 9}, ""},
		{"abandoned, pushed, earlier copies", store.Draft{Status: string(drafts.StatusAbandoned), RemoteState: drafts.RemotePushed, RemoteUID: 9,
			RemotePrevious: []store.RemoteCopy{{UID: 3}}}, ""},
		{"sent, taken over", store.Draft{Status: string(drafts.StatusSentByHuman), RemoteState: drafts.RemoteTakenOver}, ""},
		{"abandoned, pending", store.Draft{Status: string(drafts.StatusAbandoned), RemoteState: drafts.RemotePending}, ""},
		{"closed, pushed after close", store.Draft{Status: string(drafts.StatusAbandoned), RemoteState: drafts.RemotePushed, RemoteUID: 9,
			PushNote: drafts.PushNotePushedAfterClose}, "just after the draft was closed"},
		{"open, pushed", store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemotePushed, RemoteUID: 9}, "in the Drafts folder (uid 9)"},
		{"open, missing", store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemoteMissing,
			To: []store.Addr{{Address: "a@example.org"}}}, "(copy_gone)"},
		{"open, taken over", store.Draft{Status: string(drafts.StatusReady), RemoteState: drafts.RemoteTakenOver}, "taken over"},
	} {
		got := remoteNote(c.d)
		if (c.want == "" && got != "") || (c.want != "" && !strings.Contains(got, c.want)) {
			t.Errorf("%s: remote_note = %q, want %q", c.name, got, c.want)
		}
	}
}

// A09 Teil 2 (b): drafts.get eines ersetzten Entwurfs: solange die Aktion
// offen ist oder ein Ziel ok hat, „queued for the trash“; ist sie
// abgeschlossen ohne Ziel ok (hier: Kopf am Server anders, stale), nennt
// closed_note die verbliebene Kopie und drafts.cleanup_copies. remote_note
// ist in jedem Fall leer.
func TestA09SupersedeClosedNote(t *testing.T) {
	for _, c := range []struct {
		name    string
		stale   bool
		run     bool
		wantNew bool
	}{
		{"queued", false, false, false},
		{"moved", false, true, false},
		{"stale", true, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, x := supEnv(t, triageKey)
			ctx := context.Background()
			res, err := execJSON(t, x, triageKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false)
			if err != nil {
				t.Fatal(err)
			}
			id := res.Output.(SupersedeOutput).ActionID
			remote := &supRemote{}
			if c.stale {
				e.blobs.m["hdr-sup"] = "Subject: Entwurf\r\nMessage-ID: <pk.alt@example.org>\r\n\r\nText"
				e.exec(t, `INSERT INTO blobs(sha256, size, created_at) VALUES ('hdr-sup', 60, 0)`)
				e.exec(t, `UPDATE messages SET blob_sha256 = 'hdr-sup' WHERE message_id_header = 'pk.alt@example.org'`)
				e.exec(t, `UPDATE message_mailboxes SET content_loaded = 0 WHERE uid = 9`)
				remote.headers = map[provider.UID]string{9: "Subject: Etwas anderes\r\n\r\n"}
			}
			if c.run {
				e.runActions(t, policy.DefaultRules(), remote)
			}
			a, _ := e.st.Action(ctx, id)
			g, err := execJSON(t, x, assistantKey, "drafts.get", `{"draft_id":1}`, false)
			if err != nil {
				t.Fatal(err)
			}
			got := g.Output.(DraftDetail)
			want := closedNoteSuperseded
			if c.wantNew {
				want = SupersedeNotMovedNote(id)
				if !strings.Contains(want, fmt.Sprintf("action %d did not move it", id)) || !strings.Contains(want, "drafts.cleanup_copies") {
					t.Fatalf("note text = %q", want)
				}
			}
			if got.ClosedNote != want || got.RemoteNote != "" || got.ClosedReason != drafts.ReasonSupersededByDraft {
				t.Fatalf("action %s: closed_note = %q, remote_note = %q", a.State, got.ClosedNote, got.RemoteNote)
			}
		})
	}
}

// actionFinishedWithoutOK je Zustand.
func TestA09ActionFinishedWithoutOK(t *testing.T) {
	for _, c := range []struct {
		state   string
		results []string
		want    bool
	}{
		{store.ActionQueued, []string{""}, false},
		{store.ActionRunning, []string{store.TargetStale}, false},
		{store.ActionDone, []string{store.TargetOK}, false},
		{store.ActionDone, []string{store.TargetStale}, true},
		{store.ActionPartial, []string{store.TargetOK, store.TargetStale}, false},
		{store.ActionFailed, []string{store.TargetFailed}, true},
		{store.ActionFailed, nil, true},
		{store.ActionExpired, []string{""}, true},
	} {
		a := store.Action{State: c.state}
		for _, r := range c.results {
			a.Targets = append(a.Targets, store.ActionTarget{Result: r})
		}
		if got := actionFinishedWithoutOK(a); got != c.want {
			t.Errorf("%s %v = %v", c.state, c.results, got)
		}
	}
}
