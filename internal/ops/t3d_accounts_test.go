package ops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A14 (T3): accounts.remove nimmt offene Entwürfe und Aktionen nicht still
// per Cascade mit. Ohne force: denied (open_drafts bzw. open_actions), auch
// im Trockenlauf; force nur für den Menschen ohne Schlüssel (sonst
// human_only); mit force verschwindet das Konto samt Entwürfen.
func TestAccountsRemoveRefusesOpenWork(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	ctx := context.Background()
	leer, _ := e.st.AccountByName(ctx, "leer")
	if _, err := e.st.AddIdentity(ctx, store.Identity{AccountID: leer.ID, Name: "Leer", Email: "leer@example.org"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&drafts.Service{Store: e.st}).Create(ctx, drafts.NewDraft{AccountID: leer.ID, Kind: drafts.KindNew, CreatedBy: "cli",
		Content: drafts.Content{To: []store.Addr{{Address: "a@example.org"}}, Subject: "Offen", BodyText: "Text"}}); err != nil {
		t.Fatal(err)
	}
	adminKey := policy.Key{ID: "key:adm", Name: "adm", Scopes: policy.Roles["admin"]}
	denied := func(key policy.Key, in string, dry bool, reason string) {
		t.Helper()
		_, err := execJSON(t, x, key, "accounts.remove", in, dry)
		if !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "("+reason+")") {
			t.Fatalf("%s (dry %v): %v; want %s", in, dry, err, reason)
		}
	}
	denied(humanKey, `{"name":"leer"}`, false, "open_drafts")
	denied(humanKey, `{"name":"leer"}`, true, "open_drafts")
	denied(adminKey, `{"name":"leer"}`, false, "open_drafts")
	denied(adminKey, `{"name":"leer","force":true}`, false, "human_only")
	denied(adminKey, `{"name":"leer","force":true}`, true, "human_only")
	if _, err := e.st.AccountByName(ctx, "leer"); err != nil {
		t.Fatalf("account gone after refusals: %v", err)
	}

	// Offene Aktion am Konto ms (ohne Nachrichten).
	ms, _ := e.st.AccountByName(ctx, "ms")
	if _, err := e.st.CreateAction(ctx, store.NewAction{AccountID: ms.ID, Kind: store.ActionMove, State: store.ActionQueued, IDsHash: "h",
		RequestedBy: "cli", Params: store.ActionParams{TargetMailboxID: 1, TargetMailbox: "Archiv"}, Now: time.Now(),
		Targets: []store.ActionTarget{{MessageID: 999, MailboxID: 1, UID: 1, UIDValidity: 1}}}); err != nil {
		t.Fatal(err)
	}
	denied(humanKey, `{"name":"ms"}`, false, "open_actions")

	res, err := execJSON(t, x, humanKey, "accounts.remove", `{"name":"leer","force":true}`, false)
	if err != nil || res.Output.(AccountsRemoveOutput).Removed != "leer" {
		t.Fatalf("human with force: %+v %v", res.Output, err)
	}
	if _, err := e.st.AccountByName(ctx, "leer"); err == nil {
		t.Fatal("account still there")
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM drafts WHERE account_id = ?`, leer.ID); n != 0 {
		t.Fatalf("drafts left: %d", n)
	}
}

// A14 im Store: Die Prüfung läuft in der Transaktion des Löschens.
func TestRemoveAccountChecksOpenWorkAtomically(t *testing.T) {
	e := newContractEnv(t)
	ctx := context.Background()
	ms, _ := e.st.AccountByName(ctx, "ms")
	if _, err := e.st.CreateAction(ctx, store.NewAction{AccountID: ms.ID, Kind: store.ActionMove, State: store.ActionQueued, IDsHash: "h",
		RequestedBy: "cli", Params: store.ActionParams{TargetMailboxID: 1, TargetMailbox: "Archiv"}, Now: time.Now(),
		Targets: []store.ActionTarget{{MessageID: 999, MailboxID: 1, UID: 1, UIDValidity: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.RemoveAccount(ctx, ms.ID, false); !errors.Is(err, store.ErrAccountHasOpenWork) {
		t.Fatalf("remove with open action: %v", err)
	}
	if err := e.st.RemoveAccount(ctx, ms.ID, true); err != nil {
		t.Fatalf("remove with force: %v", err)
	}
}
