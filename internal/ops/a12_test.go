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
