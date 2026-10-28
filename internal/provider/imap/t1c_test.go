package imap

import (
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// A5.2 (D5): Ältere deutsche Gmail-Konten nennen ihre Ordner
// „[Google Mail]/…" statt „[Gmail]/…"; ohne SPECIAL-USE gelten dieselben
// Rollen wie unter „[Gmail]".
func TestGoogleMailRolesByName(t *testing.T) {
	boxes := []imapx.MailboxInfo{
		{Path: "INBOX", Delim: "/"},
		{Path: "[Google Mail]/Entwürfe", Delim: "/"},
		{Path: "[Google Mail]/Gesendet", Delim: "/"},
		{Path: "[Google Mail]/Alle Nachrichten", Delim: "/"},
		{Path: "[Google Mail]/Spam", Delim: "/"},
		{Path: "[Google Mail]/Papierkorb", Delim: "/"},
		{Path: "[Google Mail]/Markiert", Delim: "/"},
		{Path: "[google mail]/Wichtig", Delim: "/"},
	}
	want := []provider.Role{provider.RoleInbox, provider.RoleDrafts, provider.RoleSent, provider.RoleAll,
		provider.RoleJunk, provider.RoleTrash, provider.RoleCustom, provider.RoleCustom}
	got := AssignRoles(boxes)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: %s, want %s", boxes[i].Path, got[i], want[i])
		}
	}
	// Einzeln (Role) wie in der Liste; SPECIAL-USE hat weiter Vorrang.
	if r := Role(imapx.MailboxInfo{Path: "[Google Mail]/Gesendet", Delim: "/", Attrs: []string{`\Drafts`}}); r != provider.RoleDrafts {
		t.Errorf("special-use first: %s", r)
	}
}
