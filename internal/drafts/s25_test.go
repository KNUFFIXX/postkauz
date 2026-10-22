package drafts

import (
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.5, E6: Auswertung des Spiegels zur zuletzt abgelegten Kopie. Gemeldet
// wird nur, was der Spiegel belegt; was er noch nicht gesehen haben kann,
// gilt als unverändert.
func TestCopyState(t *testing.T) {
	d := store.Draft{MessageIDHeader: "pk.1@example.org", RemoteUID: 5, PushedContentHash: "h1"}
	synced := store.MirrorCopy{MailboxFound: true, Synced: true, UIDValidity: 9, UIDNext: 7}
	with := func(f func(*store.MirrorCopy)) store.MirrorCopy {
		mc := synced
		f(&mc)
		return mc
	}
	for _, c := range []struct {
		name  string
		d     store.Draft
		mc    store.MirrorCopy
		valid bool
		want  string
	}{
		{"unchanged", d, with(func(m *store.MirrorCopy) {
			m.AtUID, m.AtUIDMessageID, m.AtUIDContentHash = true, d.MessageIDHeader, "h1"
		}), true, ""},
		{"other content at the uid", d, with(func(m *store.MirrorCopy) {
			m.AtUID, m.AtUIDMessageID, m.AtUIDContentHash = true, d.MessageIDHeader, "h2"
		}), true, RemoteTakenOver},
		{"other message at the uid", d, with(func(m *store.MirrorCopy) {
			m.AtUID, m.AtUIDMessageID, m.AtUIDContentHash = true, "x@example.org", "h1"
		}), true, RemoteTakenOver},
		{"replaced by an edited version", d, with(func(m *store.MirrorCopy) { m.NewerSameID = true }), true, RemoteTakenOver},
		{"gone", d, synced, true, RemoteMissing},
		{"mirror has not seen the uid yet", d, with(func(m *store.MirrorCopy) { m.UIDNext = 5 }), true, ""},
		{"mailbox never mirrored", d, store.MirrorCopy{MailboxFound: true}, true, ""},
		{"mailbox gone", d, store.MirrorCopy{}, true, RemoteMissing},
		{"uid unknown", store.Draft{MessageIDHeader: d.MessageIDHeader, PushedContentHash: "h1"}, synced, true, ""},
		{"uidvalidity changed, copy found", d, with(func(m *store.MirrorCopy) { m.SameIDAndHash = true }), false, ""},
		{"uidvalidity changed, other version", d, with(func(m *store.MirrorCopy) { m.NewerSameID = true }), false, RemoteTakenOver},
		{"uidvalidity changed, gone", d, synced, false, RemoteMissing},
		// Gegenprüfung S2.4/S2.5, K4: Die Kopie ist größer als
		// [imap] max_message_bytes, der Spiegel lädt sie nicht: nicht
		// prüfbar, kein missing.
		{"too large for the mirror", d, with(func(m *store.MirrorCopy) { m.Skipped = true }), true, ""},
	} {
		if got := copyState(c.d, c.mc, c.valid); got != c.want {
			t.Errorf("%s: state = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPending(t *testing.T) {
	for _, c := range []struct {
		status, remote string
		want           bool
	}{
		{"draft", RemotePending, true}, {"pending_approval", RemotePending, true}, {"ready", RemotePending, true},
		{"sent_by_human", RemotePending, false}, {"abandoned", RemotePending, false},
		{"draft", RemotePushed, false}, {"draft", RemoteTakenOver, false}, {"draft", RemoteMissing, false},
	} {
		if got := Pending(store.Draft{Status: c.status, RemoteState: c.remote}); got != c.want {
			t.Errorf("%s/%s: %v", c.status, c.remote, got)
		}
	}
}
