package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

func TestMessagesGetAmbiguousMessageID(t *testing.T) {
	e := newMirrorEnv(t)
	first := e.seed(t)
	ctx := context.Background()
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	_ = e.st.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('sha-2',41,1)`)
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: e.acct, MailboxID: boxes[0].ID,
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 6}, MessageID: "a@example.org", Subject: "Hallo", From: "a@example.org",
			Size: 41, BlobSHA256: "sha-2", BlobSize: 41, TextPlain: "andere Fassung", CleanVersion: 1}},
		State: &store.MailboxState{UIDValidity: 3, UIDNext: 7, Phase: store.PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, "a@example.org")
	if len(ids) != 2 || ids[0] != first {
		t.Fatalf("ids = %v", ids)
	}
	var ie *InputError
	_, err := e.run("messages.get", map[string]any{"message_id": "<a@example.org>"}, "", false)
	if !errors.As(err, &ie) || !strings.Contains(ie.Msg, "ambiguous message_id: 2 messages share it (ids "+itoa(ids[0])+", "+itoa(ids[1])+")") {
		t.Fatalf("ambiguous: %v", err)
	}
	// Mit der lokalen ID geht es weiter.
	if _, err := e.run("messages.get", map[string]any{"id": ids[1]}, "", false); err != nil {
		t.Fatal(err)
	}
}

type fakeHeaderIDs []int64

func (f fakeHeaderIDs) MessageIDsByHeader(context.Context, int64, string) ([]int64, error) {
	return f, nil
}

func TestMessageByHeaderLimitsTheList(t *testing.T) {
	many := make(fakeHeaderIDs, 25)
	for i := range many {
		many[i] = int64(i + 1)
	}
	_, err := messageByHeader(context.Background(), many, 1, "x@example.org")
	if err == nil || !strings.Contains(err.Error(), "25 messages share it (ids 1, 2,") || !strings.Contains(err.Error(), ", 20, …)") ||
		strings.Contains(err.Error(), "21") {
		t.Fatalf("err = %v", err)
	}
	if id, err := messageByHeader(context.Background(), fakeHeaderIDs{7}, 1, "<x@example.org>"); err != nil || id != 7 {
		t.Fatalf("one: %d %v", id, err)
	}
	if _, err := messageByHeader(context.Background(), fakeHeaderIDs{}, 1, "x"); !errors.Is(err, store.ErrMessageNotFound) {
		t.Fatalf("none: %v", err)
	}
}

// Gegenprüfung K10: Auch Antwort und Weiterleitung über die Message-ID
// (drafts.create mit in_reply_to_message_id; CLI draft reply/forward)
// brechen bei zwei Fassungen ab, statt still eine zu nehmen; es entsteht
// kein Entwurf.
func TestDraftsCreateAmbiguousMessageID(t *testing.T) {
	e := newDraftEnv(t, false)
	ctx := context.Background()
	in := func(kind string) map[string]any {
		return map[string]any{"account": "demo", "kind": kind, "in_reply_to_message_id": "<a@example.org>",
			"to": []string{"c@example.org"}, "body_markdown": "Antwort"}
	}
	// Gegenprobe: eindeutig geht es.
	e.must(t, assistant, "drafts.create", in("reply"))
	before := len(e.must(t, assistant, "drafts.list", map[string]any{}).(DraftsListOutput).Drafts)

	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	_ = e.st.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('sha-2',41,1)`)
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: e.acct, MailboxID: boxes[0].ID,
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 6}, MessageID: "a@example.org", Subject: "Hallo", From: "a@example.org",
			Size: 41, BlobSHA256: "sha-2", BlobSize: 41, TextPlain: "andere Fassung", CleanVersion: 1}},
		State: &store.MailboxState{UIDValidity: 3, UIDNext: 7, Phase: store.PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, "a@example.org")
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
	for _, kind := range []string{"reply", "reply_all", "forward"} {
		var ie *InputError
		_, err := e.do(t, assistant, "drafts.create", in(kind), false)
		if !errors.As(err, &ie) || !strings.Contains(ie.Msg, "ambiguous message_id: 2 messages share it (ids "+itoa(ids[0])+", "+itoa(ids[1])+")") {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if n := len(e.must(t, assistant, "drafts.list", map[string]any{}).(DraftsListOutput).Drafts); n != before {
		t.Fatalf("drafts: %d, want %d", n, before)
	}
	// Mit der lokalen ID geht es weiter.
	e.must(t, assistant, "drafts.create", map[string]any{"kind": "forward", "in_reply_to": ids[0], "to": []string{"c@example.org"}})
}
