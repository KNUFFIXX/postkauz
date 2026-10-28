package mailsync

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

type litBytes struct{ *bytes.Reader }

func (l litBytes) Size() int64 { return int64(l.Len()) }

// T2, S3.1 Akzeptanz 2 (Server im Prozess): mark --seen und move kommen
// mit sync --watch binnen --jobs-interval auf den Server (zweiter Client
// prüft), ohne auf einen Lauf über alle Ordner zu warten; die Aktion ist danach
// done und der Spiegel stimmt (Flag ohne pending, Kopie im Zielordner).
func TestWatchAppliesActionsBetweenRounds(t *testing.T) {
	e := newPushEnv(t, "Archiv")
	for i := 1; i <= 2; i++ {
		raw := fmt.Sprintf("Message-ID: <t2-%d@example.org>\r\nFrom: a@example.org\r\nSubject: T2 %d\r\n\r\nText %d\r\n", i, i, i)
		if _, err := e.u.Append("INBOX", litBytes{bytes.NewReader([]byte(raw))}, &imap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	e.runner.Actions = &actions.Runner{Store: e.st, Blobs: e.blobs}
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rounds atomic.Int32
	done := make(chan error, 1)
	go func() {
		out, err := e.runner.Sync(ctx, e.account(t), provider.PasswordCredential(testPass), ops.SyncOptions{Watch: true, Interval: time.Hour,
			JobsInterval: 100 * time.Millisecond}, func(p ops.Progress) {
			if p.Event == "round" {
				rounds.Add(1)
			}
		})
		if err == nil && (out.Actions == nil || out.Actions.OK != 2) {
			err = fmt.Errorf("watch output actions = %+v", out.Actions)
		}
		done <- err
	}()
	waitFor(t, 5*time.Second, func() bool { return rounds.Load() == 1 })

	ids1, _ := e.st.MessageIDsByHeader(context.Background(), e.acct, "t2-1@example.org")
	ids2, _ := e.st.MessageIDsByHeader(context.Background(), e.acct, "t2-2@example.org")
	svc := &actions.Service{Store: e.st}
	yes := true
	plan, err := svc.PlanFlags(context.Background(), ids1)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := svc.Create(context.Background(), actions.Request{AccountID: e.acct, Kind: store.ActionMark, IDs: ids1, Plan: plan,
		RequestedBy: "cli", Params: store.ActionParams{Mark: &store.MarkParams{Seen: &yes}}}); err != nil {
		t.Fatal(err)
	}
	dest, _, _ := svc.FindMailbox(context.Background(), e.acct, "Archiv", "")
	plan, err = svc.PlanMove(context.Background(), ids2, "", dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), actions.Request{AccountID: e.acct, Kind: store.ActionMove, IDs: ids2, Plan: plan,
		RequestedBy: "cli", Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path}}); err != nil {
		t.Fatal(err)
	}
	client := imapSession(t, e.addr)
	waitFor(t, 5*time.Second, func() bool {
		for _, fl := range serverCopies(t, client, "INBOX", "t2-1@example.org") {
			return slices.Contains(fl, imap.FlagSeen)
		}
		return false
	})
	waitFor(t, 5*time.Second, func() bool { return len(serverCopies(t, client, "Archiv", "t2-2@example.org")) == 1 })
	t.Logf("mark and move on the server after %s", time.Since(start).Round(time.Millisecond))
	if len(serverCopies(t, client, "INBOX", "t2-2@example.org")) != 0 {
		t.Fatal("moved message still in INBOX")
	}
	// Ein weiterer Lauf (nur INBOX) folgt hier aus der IDLE-Meldung über die
	// Änderung; die Aktionen selbst warten nicht auf einen Lauf (109 ms bei
	// 100 ms jobs-interval, gemessen).
	// Der Spiegel folgt, sobald der Job die Aktionen abgeschlossen hat.
	waitFor(t, 5*time.Second, func() bool {
		m, _ := e.st.MessageByID(context.Background(), ids1[0])
		cs, _ := e.st.ServerCopies(context.Background(), ids2[0])
		return m.Seen && !m.PendingFlags && len(cs) == 1 && cs[0].Mailbox == "Archiv"
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A5.1: Der Entwürfe-Ordner je Konto (account set --drafts-mailbox) geht
// [draft] mailbox der Konfiguration und der Rolle drafts vor; ohne ihn
// gilt die Konfiguration.
func TestDraftsMailboxPerAccount(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Eigene", "Global")
	e.runner.DraftMailbox = "Global"
	ctx := context.Background()
	d1 := e.newDraft(t, "Konfiguration")
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	client := imapSession(t, e.addr)
	if len(serverCopies(t, client, "Global", d1.MessageIDHeader)) != 1 {
		t.Fatal("draft not in the configured mailbox")
	}
	if err := e.st.SetAccountDraftsMailbox(ctx, e.acct, "Eigene"); err != nil {
		t.Fatal(err)
	}
	d2 := e.newDraft(t, "Konto")
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(serverCopies(t, client, "Eigene", d2.MessageIDHeader)) != 1 || len(serverCopies(t, client, "Drafts", d2.MessageIDHeader)) != 0 {
		t.Fatal("draft not in the mailbox of the account")
	}
	// Ein zweites Konto ohne eigenen Ordner nimmt weiter die Konfiguration.
	other, err := e.st.CreateAccount(ctx, store.Account{Name: "zwei", Email: "zwei@example.org", Host: "127.0.0.1", Port: 1, TLS: "none",
		Username: "z", AuthType: "password", CredentialRef: "account/zwei"})
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := e.st.AccountByName(ctx, "zwei")
	if a2.ID != other || e.runner.draftsMailbox(a2) != "Global" || e.runner.draftsMailbox(e.account(t)) != "Eigene" {
		t.Fatalf("drafts mailbox: %q / %q", e.runner.draftsMailbox(a2), e.runner.draftsMailbox(e.account(t)))
	}
}

// Nachprüfung zu fd70f19, Punkt 2: Vor den Aktionen gleicht der Sync die
// Ordnerliste ab. Hier verliert „Archive“ am Server die Rolle archive (ein
// neuer Ordner „Archiv“ gewinnt sie nach der Namensregel), ohne dass ein
// Lauf es schon gesehen hat; die V1-Prüfung des Ziels muss die neue Rolle
// sehen: kein MOVE, Ziel stale, Fehler target_changed.
func TestActionsSeeServerRoleChangeBeforeMove(t *testing.T) {
	e := newPushEnv(t, "Archive")
	raw := "Message-ID: <rolle@example.org>\r\nFrom: a@example.org\r\nSubject: Rolle\r\n\r\nText\r\n"
	if _, err := e.u.Append("INBOX", litBytes{bytes.NewReader([]byte(raw))}, &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	e.runner.Actions = &actions.Runner{Store: e.st, Blobs: e.blobs}
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	svc := &actions.Service{Store: e.st}
	dest, ok, err := svc.FindMailbox(ctx, e.acct, "", "archive")
	if err != nil || !ok || dest.Path != "Archive" {
		t.Fatalf("archive mailbox = %+v %v %v", dest, ok, err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, "rolle@example.org")
	plan, err := svc.PlanMove(ctx, ids, "", dest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, actions.Request{AccountID: e.acct, Kind: store.ActionMove, IDs: ids, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.u.Create("Archiv", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.st.Action(ctx, a.ID)
	if got.Error != "target_changed" || got.Targets[0].Result != store.TargetStale {
		t.Fatalf("action = %+v", got)
	}
	client := imapSession(t, e.addr)
	if len(serverCopies(t, client, "Archive", "rolle@example.org")) != 0 || len(serverCopies(t, client, "INBOX", "rolle@example.org")) != 1 {
		t.Fatal("message was moved although the target lost its role")
	}
}
