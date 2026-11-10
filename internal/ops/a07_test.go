package ops

import (
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A07 Teil 1: Der Hinweis zur verschwundenen Kopie nennt den Fall „Antwort
// ohne Empfänger“ (nur der Weg über den Verlauf schließt automatisch).
func TestA07RemoteNoteWithoutRecipients(t *testing.T) {
	d := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemoteMissing, Kind: string(drafts.KindReply), InReplyToMsg: 7}
	if n := remoteNote(d); !strings.Contains(n, "no To or Cc") || !strings.Contains(n, "In-Reply-To") {
		t.Fatalf("note without recipients = %q", n)
	}
	d.To = []store.Addr{{Address: "anna@example.org"}}
	if n := remoteNote(d); strings.Contains(n, "no To or Cc") {
		t.Fatalf("note with recipients = %q", n)
	}
}
