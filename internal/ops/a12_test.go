package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A12 Teil 1 (c), (d) (vi): remote_note bei weiterbearbeiteter Fassung und
// während eines laufenden APPEND.
func TestA12RemoteNoteEditedAndAppend(t *testing.T) {
	d := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemoteTakenOver, EditedMessageID: 42}
	if n := remoteNote(d); !strings.Contains(n, "saved as a new message (message 42)") {
		t.Fatalf("edited: %q", n)
	}
	d.EditedMessageID = 0
	if n := remoteNote(d); n != "taken over in the mail client; change it there" {
		t.Fatalf("taken over: %q", n)
	}
	p := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemotePending, PushAttempt: &store.PushAttempt{MailboxID: 1}}
	if n := remoteNote(p); !strings.Contains(n, "being stored in the Drafts folder") {
		t.Fatalf("append running: %q", n)
	}
}

// A12 Teil 0 (b): is:scheduled vergleicht den eigenen Absender mit ESCAPE;
// „_“ in der Adresse des Kontos ist kein Platzhalter.
func TestA12IsScheduledEscapesUnderscore(t *testing.T) {
	m := newA10Mirror(t)
	if err := m.st.ExecForTest(`UPDATE accounts SET email = 'de_mo@example.org'`); err != nil {
		t.Fatal(err)
	}
	for from, want := range map[string]int{"Demo <dexmo@example.org>": 0, "Demo <de_mo@example.org>": 1} {
		if err := m.st.ExecForTest(`UPDATE messages SET from_addr = ? WHERE id = ?`, from, m.ids["scheduled"]); err != nil {
			t.Fatal(err)
		}
		if o := m.search(t, map[string]any{"query": "is:scheduled"}); len(o.Hits) != want {
			t.Fatalf("from %s: hits %d, want %d", from, len(o.Hits), want)
		}
	}
}

// A12 Teil 2 (b): remote_note nennt frühere Kopien nach ihrem Stand; ohne
// Stände (und mit previous_copies = "keep") der Text von rc9.
func TestA12RemoteNotePreviousStates(t *testing.T) {
	d := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemotePushed, RemoteUID: 9}
	n := remoteNoteWith(d, []int64{3, 4, 5, 6}, false, []drafts.PreviousCopy{{UID: 3, State: drafts.PrevQueued, ActionID: 7},
		{UID: 4, State: drafts.PrevGivenUp}, {UID: 5, State: drafts.PrevChanged}, {UID: 6, State: drafts.PrevNotMirrored}})
	for _, want := range []string{"uid 3 goes to the trash (action 7)", "uid 4 stays in the Drafts folder; remove it in the mail client or with draft cleanup",
		"uid 5 changed in the mail client; only the human removes it", "uid 6 not mirrored yet"} {
		if !strings.Contains(n, want) {
			t.Fatalf("note %q lacks %q", n, want)
		}
	}
	if n := remoteNoteWith(d, []int64{3}, true, nil); !strings.Contains(n, "stay there, nothing is deleted") {
		t.Fatalf("keep: %q", n)
	}
	if n := remoteNoteWith(d, []int64{3}, false, nil); !strings.Contains(n, "own unchanged copies go to the trash") {
		t.Fatalf("rc9 text: %q", n)
	}
}

// A12 Teil 3 (c): attachments.retry, nur der Mensch. Jede Auswahl,
// Trockenlauf, ohne Auswahl Fehler, mit Schlüssel abgelehnt, Audit ohne
// Dateinamen.
func TestA12AttachmentsRetry(t *testing.T) {
	e := newMirrorEnv(t)
	if err := e.x.Registry.Register(AttachmentsRetry(Deps{Accounts: e.st, ExtractRetry: e.st})); err != nil {
		t.Fatal(err)
	}
	msg := e.seed(t)
	atts, _ := e.st.AttachmentsByMessage(context.Background(), msg)
	if len(atts) < 2 {
		t.Fatalf("attachments = %d", len(atts))
	}
	fail := func() {
		t.Helper()
		if err := e.st.ExecForTest(`UPDATE attachments SET extract_status = 'failed', extract_error = 'pdftotext version 4.06'`); err != nil {
			t.Fatal(err)
		}
		if err := e.st.ExecForTest(`INSERT OR REPLACE INTO meta(key, value) VALUES ('extract_retry/' || ?, '{"n":2}')`, atts[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	count := func(status string) int {
		n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM attachments WHERE extract_status = ?`, status)
		return n
	}
	fail()
	all := len(atts)
	// Ohne Auswahl: Eingabefehler.
	var ie *InputError
	if _, err := e.run("attachments.retry", map[string]any{"account": "demo"}, "", false); !errors.As(err, &ie) {
		t.Fatalf("without a choice: %v", err)
	}
	// Trockenlauf: nennt die Zahl, ändert nichts.
	out, err := e.run("attachments.retry", map[string]any{"account": "demo", "all_failed": true}, "", true)
	if err != nil || out.(AttachRetryOutput).Count != all || count("failed") != all {
		t.Fatalf("dry run: %+v %v, failed %d", out, err, count("failed"))
	}
	// Ein Anhang.
	out, err = e.run("attachments.retry", map[string]any{"attachments": []int64{atts[0].ID}}, "", false)
	if err != nil || out.(AttachRetryOutput).Count != 1 || count("pending") != 1 {
		t.Fatalf("one attachment: %+v %v", out, err)
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM meta WHERE key LIKE 'extract_retry/%'`); n != 0 {
		t.Fatalf("retry notes left: %d", n)
	}
	// Eine Nachricht.
	fail()
	out, err = e.run("attachments.retry", map[string]any{"account": "demo", "message": msg}, "", false)
	if err != nil || out.(AttachRetryOutput).Count != all || count("failed") != 0 {
		t.Fatalf("message: %+v %v", out, err)
	}
	// Mit Schlüssel: human_only.
	b, _ := json.Marshal(map[string]any{"all_failed": true})
	_, err = e.x.Execute(context.Background(), Request{Key: policy.Key{ID: "key:assi", Scopes: policy.Roles["admin"]}, Actor: audit.ActorAgent,
		Operation: "attachments.retry", Input: b})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("with a key: %v", err)
	}
	for _, ev := range e.au.Events() {
		if ev.Operation == "attachments.retry" && (strings.Contains(string(ev.ParamsRedacted), atts[0].Filename) || atts[0].Filename == "") {
			t.Fatalf("audit names the file: %s", ev.ParamsRedacted)
		}
	}
}
