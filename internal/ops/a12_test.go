package ops

import (
	"strings"
	"testing"

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
