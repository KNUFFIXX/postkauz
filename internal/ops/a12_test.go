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
