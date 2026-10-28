// Erzeugt die Testdaten für TestSchema20OfAlpha4Migrates (Nachprüfung der
// Prüfsitzung zu fd70f19, Punkt 1) mit dem Stand T2 vor der Nachbesserung
// (da5386b, Build 0.3.0-alpha.4, Schema 20): ein Konto mit Gmail-ähnlichen
// Ordnern (INBOX, „[Gmail]/Alle Nachrichten“, „[Gmail]/Wichtig“, Papierkorb)
// und einer Nachricht in drei davon.
//
// Neu erzeugen (nur falls nötig; die Daten sollen den alten Stand belegen):
//
//	git worktree add /tmp/wt20 da5386b
//	mkdir /tmp/wt20/cmd/genfixture && cp gen/main.go /tmp/wt20/cmd/genfixture/
//	cd /tmp/wt20 && go run ./cmd/genfixture /tmp/pkfixture20
//
// danach postkauz.db mit gzip -9 nach postkauz.db.gz hierher. Das
// Verzeichnis testdata übersetzt go build/vet/test nicht.
package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

func main() {
	out := os.Args[1]
	ctx := context.Background()
	must(os.MkdirAll(out, 0o700))
	st, err := store.Open(filepath.Join(out, "postkauz.db"))
	must(err)
	must(st.Migrate(ctx))
	acct, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/demo"})
	must(err)
	_, _, err = st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "[Gmail]/Alle Nachrichten", Name: "Alle Nachrichten", Role: "all"},
		{Path: "[Gmail]/Wichtig", Name: "Wichtig", Role: "custom"},
		{Path: "[Gmail]/Papierkorb", Name: "Papierkorb", Role: "trash"}})
	must(err)
	boxes, err := st.Mailboxes(ctx, acct)
	must(err)
	ids := map[string]int64{}
	for _, b := range boxes {
		ids[b.Path] = b.ID
	}
	_, err = st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: ids["INBOX"],
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 1}, MessageID: "probe@example.org", Subject: "Probe", From: "demo@example.org",
			Size: 40, BlobSHA256: "sha-probe", BlobSize: 40, ContentHash: "h-probe"}},
		State: &store.MailboxState{UIDValidity: 7, UIDNext: 2, Phase: store.PhaseReady, Total: 1}})
	must(err)
	for i, p := range []string{"[Gmail]/Alle Nachrichten", "[Gmail]/Wichtig"} {
		_, err = st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: ids[p],
			Links: []store.Link{{MessageID: 1, Flags: store.LinkFlags{UID: 5}}},
			State: &store.MailboxState{UIDValidity: uint32(8 + i), UIDNext: 6, Phase: store.PhaseReady, Total: 1}})
		must(err)
	}
	must(st.Close())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
