package drafts

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A11 Teil 1: eigene Entwurfskopien räumt PostKauz selbst auf (cleanup.go).

// cuRemote ist der Server für den Runner: MOVE gezählt, neue UID im
// Papierkorb.
type cuRemote struct {
	moves []provider.UID
	next  provider.UID
}

func (r *cuRemote) FetchHeaders(context.Context, string, uint32, []provider.UID) ([]provider.Header, error) {
	return nil, nil
}

func (r *cuRemote) SetFlags(context.Context, string, uint32, []provider.UID, provider.FlagPatch) error {
	return nil
}

func (r *cuRemote) Move(_ context.Context, _ string, _ uint32, uids []provider.UID, _ string) (provider.MoveResult, error) {
	res := provider.MoveResult{UIDValidity: 1, UIDs: map[provider.UID]provider.UID{}}
	for _, u := range uids {
		r.moves = append(r.moves, u)
		r.next++
		res.UIDs[u] = 1000 + r.next
	}
	return res, nil
}

type cuEnv struct {
	*env
	m      *mirror
	remote *cuRemote
	rules  policy.Rules
}

const cuKey = "key:agent"

func newCuEnv(t *testing.T) *cuEnv {
	t.Helper()
	e := newEnv(t)
	m := newMirror(e)
	ctx := context.Background()
	specs := []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}, {Path: "Sent", Name: "Sent", Role: "sent"},
		{Path: "Drafts", Name: "Drafts", Role: "drafts"}, {Path: "All", Name: "All", Role: "all"}, {Path: "Trash", Name: "Trash", Role: "trash"},
		{Path: "Archiv", Name: "Archiv", Role: "custom"}}
	if _, _, err := e.st.SyncMailboxes(ctx, e.acc, specs); err != nil {
		t.Fatal(err)
	}
	mbs, _ := e.st.Mailboxes(ctx, e.acc)
	for _, b := range mbs {
		m.boxes[b.Role] = b.ID
	}
	if err := e.st.ExecForTest(`UPDATE mailboxes SET sync_phase = 'ready', uidvalidity = 1 WHERE account_id = ?`, e.acc); err != nil {
		t.Fatal(err)
	}
	return &cuEnv{env: e, m: m, remote: &cuRemote{}, rules: policy.DefaultRules()}
}

func (e *cuEnv) exec(q string, args ...any) {
	e.t.Helper()
	if err := e.st.ExecForTest(q, args...); err != nil {
		e.t.Fatal(err)
	}
}

func (e *cuEnv) count(q string, args ...any) int {
	e.t.Helper()
	n, err := e.st.QueryIntForTest(q, args...)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// newDraft legt einen Entwurf des Schlüssels cuKey an.
func (e *cuEnv) newDraft(subject string) store.Draft {
	e.t.Helper()
	r, err := e.svc.Create(context.Background(), NewDraft{AccountID: e.acc, Kind: KindNew, Content: content(subject), CreatedBy: cuKey})
	if err != nil {
		e.t.Fatal(err)
	}
	return r.Draft
}

// push legt die aktuelle Fassung im Entwürfe-Ordner ab: Kopie im Spiegel
// (mirrored), dann der Vermerk wie nach APPEND. Liefert die UID.
func (e *cuEnv) push(d store.Draft, text string, mirrored bool) int64 {
	e.t.Helper()
	d = e.draft(d.ID)
	box := e.m.boxes["drafts"]
	uid := int64(e.m.uid[box] + 1)
	if mirrored {
		e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, from: "demo@example.org", text: text, date: e.now, draftFlag: true})
		e.exec(`UPDATE message_mailboxes SET content_loaded = 1 WHERE mailbox_id = ? AND uid = ?`, box, uid)
	} else {
		e.m.uid[box]++ // die UID ist vergeben, der Spiegel kennt sie noch nicht
	}
	if _, _, err := e.svc.Pushed(context.Background(), d.ID, Fingerprint(d), PushedCopy{MailboxID: box, UIDValidity: 1, UID: uid,
		ContentHash: d.MessageIDHeader + text}); err != nil {
		e.t.Fatal(err)
	}
	return uid
}

// mirrorLate holt eine nicht gespiegelte Kopie in den Spiegel nach.
func (e *cuEnv) mirrorLate(d store.Draft, uid int64, text string) {
	e.t.Helper()
	box := e.m.boxes["drafts"]
	keep := e.m.uid[box]
	e.m.uid[box] = uint32(uid) - 1
	e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, from: "demo@example.org", text: text, date: e.now, draftFlag: true})
	e.m.uid[box] = max(keep, uint32(uid))
	e.exec(`UPDATE mailboxes SET uidnext = ? WHERE id = ?`, e.m.uid[box]+1, box)
	e.exec(`UPDATE message_mailboxes SET content_loaded = 1 WHERE mailbox_id = ? AND uid = ?`, box, uid)
}

func (e *cuEnv) update(d store.Draft, subject string) store.Draft {
	e.t.Helper()
	e.now = e.now.Add(time.Minute)
	c := content(subject)
	c.BodyText += " " + e.now.Format(time.RFC3339)
	c.BodyMarkdown = c.BodyText
	r, err := e.svc.As(Who{Actor: "agent", KeyID: cuKey, Scope: "mail:draft", Operation: "drafts.update"}, false).
		Update(context.Background(), d.ID, c, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return r.Draft
}

// run führt die wartenden Aktionen aus (Herkunftsprüfung wie im Sync).
func (e *cuEnv) run() actions.Report {
	e.t.Helper()
	keys := func(ctx context.Context, o actions.Origin) (string, error) {
		if !strings.HasPrefix(o.KeyID, "key:") {
			return "", nil
		}
		if o.KeyID == "key:gesperrt" {
			return "key_revoked", nil
		}
		k := policy.Key{ID: o.KeyID, Scopes: policy.Roles["assistant"]}
		if dec := policy.CheckInput(k, o.Facts, e.rules); !dec.Allowed() {
			return dec.Reason, nil
		}
		return "", nil
	}
	r := &actions.Runner{Store: e.st, Origin: keys, Now: func() time.Time { return e.now }}
	rep, err := r.Run(context.Background(), e.acc, e.remote)
	if err != nil {
		e.t.Fatal(err)
	}
	return rep
}

func (e *cuEnv) actionsOf(id int64) []store.Action {
	e.t.Helper()
	l, err := e.st.DraftCleanupActions(context.Background(), e.draft(id))
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *cuEnv) inDrafts(uid int64) bool {
	return e.count(`SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], uid) == 1
}

func (e *cuEnv) draftCopies() int {
	return e.count(`SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ?`, e.m.boxes["drafts"])
}

// Änderung eines abgelegten Entwurfs durch den anlegenden Schlüssel: nach
// dem Push eine Aktion draft_update mit genau der früheren Kopie; nach der
// Ausführung liegt nur die neue Kopie im Entwürfe-Ordner, Audit ohne
// Inhalt, DraftCopyHashes unverändert.
func TestA11UpdateTrashesEarlierCopy(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Abrechnung")
	u1 := e.push(d, "eins", true)
	d = e.update(d, "Abrechnung neu")
	hashesBefore, _ := e.st.DraftCopyHashes(ctx, e.acc)
	u2 := e.push(d, "zwei", true)
	acts := e.actionsOf(d.ID)
	if len(acts) != 1 {
		t.Fatalf("actions = %d", len(acts))
	}
	a := acts[0]
	if a.Params.Source != store.ActionSourceDraftUpdate || a.Params.DraftID != d.ID || a.Kind != store.ActionTrash ||
		a.State != store.ActionQueued || a.RequestedBy != cuKey || len(a.Targets) != 1 || int64(a.Targets[0].UID) != u1 {
		t.Fatalf("action = %+v", a)
	}
	r, ok := e.lastAudit(OpCleanupQueued)
	if !ok || r.ActorType != "system" || r.Result != "ok" || r.Reason != SourceDraftUpdate {
		t.Fatalf("audit = %+v", r)
	}
	var p map[string]any
	_ = json.Unmarshal(r.ParamsRedacted, &p)
	if p["draft_id"] != float64(d.ID) || p["action_id"] != float64(a.ID) || p["copies"] != float64(1) || p["skipped"] != float64(0) ||
		p["source"] != SourceDraftUpdate {
		t.Fatalf("params = %v", p)
	}
	if strings.Contains(string(r.ParamsRedacted), "Abrechnung") || strings.Contains(string(r.ParamsRedacted), "anna") {
		t.Fatalf("audit names content: %s", r.ParamsRedacted)
	}
	e.run()
	a, _ = e.st.Action(ctx, a.ID)
	if a.State != store.ActionDone || len(e.remote.moves) != 1 || int64(e.remote.moves[0]) != u1 {
		t.Fatalf("after run: %s %s, moves %v", a.State, a.Error, e.remote.moves)
	}
	if e.inDrafts(u1) || !e.inDrafts(u2) || e.draftCopies() != 1 {
		t.Fatalf("drafts folder: u1 %v u2 %v n %d", e.inDrafts(u1), e.inDrafts(u2), e.draftCopies())
	}
	hashesAfter, _ := e.st.DraftCopyHashes(ctx, e.acc)
	if strings.Join(hashesBefore, ",") == "" || len(hashesAfter) < len(hashesBefore) {
		t.Fatalf("DraftCopyHashes before %v after %v", hashesBefore, hashesAfter)
	}
	for _, h := range hashesBefore {
		found := false
		for _, g := range hashesAfter {
			found = found || g == h
		}
		if !found {
			t.Fatalf("hash %s lost after cleanup", h)
		}
	}
	// Erfolg: Der Zähler der Kopie wird im nächsten Nachlauf gelöscht.
	if _, err := e.svc.RetryCleanup(ctx, e.acc); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key LIKE 'draft_copy_cleanup/%'`); n != 0 {
		t.Fatalf("counters left: %d", n)
	}
}

// Drei Änderungen nacheinander, die Aktionen laufen erst am Ende: Keine
// Kopie steht in zwei Aktionen, keine Aktion wird stale, nur die neueste
// Kopie bleibt.
func TestA11ThreeUpdatesNoStale(t *testing.T) {
	e := newCuEnv(t)
	d := e.newDraft("Termin")
	var last int64
	for i, text := range []string{"a", "b", "c", "d"} {
		if i > 0 {
			d = e.update(d, "Termin "+text)
		}
		last = e.push(d, text, true)
	}
	acts := e.actionsOf(d.ID)
	if len(acts) != 3 {
		t.Fatalf("actions = %d", len(acts))
	}
	seen := map[uint32]bool{}
	for _, a := range acts {
		for _, tg := range a.Targets {
			if seen[tg.UID] {
				t.Fatalf("uid %d in two actions", tg.UID)
			}
			seen[tg.UID] = true
		}
	}
	rep := e.run()
	if rep.Stale != 0 || rep.Failed != 0 {
		t.Fatalf("run = %+v", rep)
	}
	for _, a := range e.actionsOf(d.ID) {
		if a.State != store.ActionDone {
			t.Fatalf("action %d = %s %s", a.ID, a.State, a.Error)
		}
	}
	if e.draftCopies() != 1 || !e.inDrafts(last) {
		t.Fatalf("copies = %d, last %v", e.draftCopies(), e.inDrafts(last))
	}
}

// Zwischen den Läufen: die zweite Änderung, während die erste Aktion noch
// wartet; die Kopie dieser Aktion wird übersprungen (skipped), beide
// Aktionen enden done.
func TestA11BusyCopySkipped(t *testing.T) {
	e := newCuEnv(t)
	d := e.newDraft("Termin")
	e.push(d, "a", true)
	d = e.update(d, "Termin b")
	e.push(d, "b", true)
	d = e.update(d, "Termin c")
	e.push(d, "c", true)
	r, _ := e.lastAudit(OpCleanupQueued)
	var p map[string]any
	_ = json.Unmarshal(r.ParamsRedacted, &p)
	if p["copies"] != float64(1) || p["skipped"] != float64(1) {
		t.Fatalf("second cleanup = %v", p)
	}
	e.run()
	for _, a := range e.actionsOf(d.ID) {
		if a.State != store.ActionDone {
			t.Fatalf("action %d = %s", a.ID, a.State)
		}
	}
	if e.draftCopies() != 1 {
		t.Fatalf("copies = %d", e.draftCopies())
	}
}

// Keine Aktion, wenn eine Bedingung fehlt; die frühere Kopie bleibt.
func TestA11UpdateCleanupConditions(t *testing.T) {
	for _, c := range []struct {
		name string
		prep func(e *cuEnv, d store.Draft)
		keep bool
		deny string
	}{
		{"ready", func(e *cuEnv, d store.Draft) { e.exec(`UPDATE drafts SET status = 'ready' WHERE id = ?`, d.ID) }, false, ""},
		{"updated by another key", func(e *cuEnv, d store.Draft) { e.exec(`UPDATE drafts SET updated_by = 'key:assi' WHERE id = ?`, d.ID) }, false, ""},
		{"scheduled", func(e *cuEnv, d store.Draft) {
			e.exec(`INSERT INTO meta(key, value) VALUES (?, ?)`, "scheduled/"+itoa(d.ID), `{"message_id":1,"scheduled_for":"2026-12-01T00:00:00Z","state":"scheduled"}`)
		}, false, ""},
		{"earlier copy changed in the mail client", func(e *cuEnv, d store.Draft) {
			e.exec(`UPDATE messages SET content_hash = 'anders' WHERE id = (SELECT message_id FROM message_mailboxes WHERE mailbox_id = ? AND uid = 1)`,
				e.m.boxes["drafts"])
		}, false, ""},
		{"earlier copy outside the Drafts folders", func(e *cuEnv, d store.Draft) {
			// Der Mensch hat die frühere Kopie ins Archiv verschoben (gleiche
			// UID dort, wie nach einem Umzug ohne neue UID nachgestellt).
			e.exec(`UPDATE message_mailboxes SET mailbox_id = ? WHERE mailbox_id = ? AND uid = 1`, e.m.boxes["custom"], e.m.boxes["drafts"])
			e.exec(`UPDATE drafts SET remote_mailbox_id = ? WHERE id = ?`, e.m.boxes["custom"], d.ID)
		}, false, ""},
		{"previous_copies keep", nil, true, ""},
		{"agent_may none", nil, false, "policy_forbids"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newCuEnv(t)
			if c.keep {
				e.svc.PreviousCopies = PreviousCopiesKeep
			}
			if c.deny != "" {
				e.svc.CleanupCheck = func(context.Context, string, int) (string, error) { return c.deny, nil }
			}
			d := e.newDraft("Abrechnung")
			u1 := e.push(d, "eins", true)
			d = e.update(d, "Abrechnung neu")
			if c.prep != nil {
				c.prep(e, d)
			}
			e.push(d, "zwei", true)
			if n := e.count(`SELECT count(*) FROM actions`); n != 0 {
				t.Fatalf("actions = %d", n)
			}
			r, ok := e.lastAudit(OpCleanupQueued)
			switch {
			case c.deny != "":
				if !ok || r.Result != "denied" || r.Reason != c.deny {
					t.Fatalf("audit = %+v", r)
				}
			case ok:
				t.Fatalf("unexpected audit %+v", r)
			}
			if c.name == "earlier copy outside the Drafts folders" {
				if e.count(`SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ? AND uid = 1`, e.m.boxes["custom"]) != 1 {
					t.Fatal("copy in the archive touched")
				}
				return
			}
			if !e.inDrafts(u1) {
				t.Fatal("earlier copy gone")
			}
			// Der Nachlauf legt auch nichts an (Bedingungen, Zähler bei
			// agent_may = none, keep).
			if _, err := e.svc.RetryCleanup(context.Background(), e.acc); err != nil {
				t.Fatal(err)
			}
			if c.deny == "" {
				if n := e.count(`SELECT count(*) FROM actions`); n != 0 {
					t.Fatalf("retry created %d actions", n)
				}
			}
		})
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// Frühere Kopie beim Push noch nicht gespiegelt: kein Ziel; der Nachlauf
// legt die Aktion an, sobald der Spiegel sie kennt.
func TestA11RetryAfterLateMirror(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Abrechnung")
	u1 := e.push(d, "eins", false)
	d = e.update(d, "Abrechnung neu")
	e.push(d, "zwei", true)
	if n := e.count(`SELECT count(*) FROM actions`); n != 0 {
		t.Fatalf("actions before mirror = %d", n)
	}
	e.mirrorLate(d, u1, "eins")
	rep, err := e.svc.RetryCleanup(ctx, e.acc)
	if err != nil || rep.Actions != 1 || rep.Copies != 1 {
		t.Fatalf("retry = %+v %v", rep, err)
	}
	acts := e.actionsOf(d.ID)
	if len(acts) != 1 || acts[0].Params.Source != SourceDraftUpdate || int64(acts[0].Targets[0].UID) != u1 {
		t.Fatalf("actions = %+v", acts)
	}
	e.run()
	if e.inDrafts(u1) {
		t.Fatal("earlier copy still there")
	}
}

// Aktion endet stale: Der Nachlauf versucht es noch zweimal (insgesamt
// drei Versuche), dann bleibt die Kopie liegen; der Zähler überlebt das
// Schließen des Entwurfs.
func TestA11RetryThreeTimes(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Abrechnung")
	u1 := e.push(d, "eins", true)
	d = e.update(d, "Abrechnung neu")
	e.push(d, "zwei", true)
	stale := func() {
		// Der Kopf passt nicht mehr: Kopie als nie geladen, ohne Blob → der
		// Runner verschiebt nichts (stale bzw. ohne lokale Kopie).
		e.exec(`UPDATE message_mailboxes SET content_loaded = 0 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u1)
		e.run()
	}
	stale()
	if a := e.actionsOf(d.ID); len(a) != 1 || a[0].State == store.ActionDone && a[0].Targets[0].Result == store.TargetOK {
		t.Fatalf("first action = %+v", a)
	}
	for i := 2; i <= 3; i++ {
		rep, err := e.svc.RetryCleanup(ctx, e.acc)
		if err != nil || rep.Actions != 1 {
			t.Fatalf("retry %d = %+v %v", i, rep, err)
		}
		stale()
	}
	// Schließen (Mensch verwirft) und ein weiterer Nachlauf: kein vierter
	// Versuch, der Zähler bleibt.
	if _, err := e.svc.Abandon(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	rep, err := e.svc.RetryCleanup(ctx, e.acc)
	if err != nil || rep.Actions != 0 {
		t.Fatalf("fourth = %+v %v", rep, err)
	}
	if n := e.count(`SELECT count(*) FROM actions`); n != 3 {
		t.Fatalf("actions = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key LIKE 'draft_copy_cleanup/%'`); n != 1 {
		t.Fatalf("counter = %d", n)
	}
	if !e.inDrafts(u1) {
		t.Fatal("copy moved")
	}
	// Nach 7 Tagen wird der Zähler gelöscht.
	e.now = e.now.Add(CleanupWindow + time.Hour)
	if _, err := e.svc.RetryCleanup(ctx, e.acc); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key LIKE 'draft_copy_cleanup/%'`); n != 0 {
		t.Fatalf("counter after 7 days = %d", n)
	}
}

// Schlüssel inzwischen gesperrt: Die Aktion scheitert an der
// Herkunftsprüfung wie bisher, nichts wird verschoben.
func TestA11RevokedKeyFails(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Abrechnung")
	u1 := e.push(d, "eins", true)
	d = e.update(d, "Abrechnung neu")
	e.push(d, "zwei", true)
	e.exec(`UPDATE actions SET requested_by = 'key:gesperrt'`)
	e.run()
	a := e.actionsOf(d.ID)[0]
	if a.State != store.ActionFailed || a.Error != "key_revoked" || len(e.remote.moves) != 0 || !e.inDrafts(u1) {
		t.Fatalf("action = %s %s, moves %v", a.State, a.Error, e.remote.moves)
	}
	_ = ctx
}

// Nachlauf (ii): ein mit superseded_by_draft geschlossener Entwurf, dessen
// Aktion stale endete, wird erneut aufgeräumt; eine Kopie, die der Mensch
// danach verändert hat, ist nie Ziel.
func TestA11RetryClosedSuperseded(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			e := newCuEnv(t)
			ctx := context.Background()
			old := e.newDraft("Abrechnung")
			u1 := e.push(old, "eins", true)
			e.now = e.now.Add(time.Minute)
			nw := e.newDraft("Abrechnung")
			if _, err := e.svc.Supersede(ctx, SupersedeRequest{OldID: old.ID, NewID: nw.ID, KeyID: cuKey}); err != nil {
				t.Fatal(err)
			}
			e.exec(`UPDATE message_mailboxes SET content_loaded = 0 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u1)
			e.run() // stale (Kopf nicht prüfbar)
			if !e.inDrafts(u1) {
				t.Fatal("copy moved by the first action")
			}
			e.exec(`UPDATE message_mailboxes SET content_loaded = 1 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u1)
			if changed {
				e.exec(`UPDATE messages SET content_hash = 'bearbeitet' WHERE id = (SELECT message_id FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?)`,
					e.m.boxes["drafts"], u1)
			}
			rep, err := e.svc.RetryCleanup(ctx, e.acc)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				if rep.Actions != 0 {
					t.Fatalf("changed copy targeted: %+v", rep)
				}
				return
			}
			if rep.Actions != 1 {
				t.Fatalf("retry = %+v", rep)
			}
			acts := e.actionsOf(old.ID)
			if len(acts) != 2 || acts[0].Params.Source != SourceDraftSupersede || acts[0].RequestedBy != cuKey {
				t.Fatalf("actions = %+v", acts)
			}
			e.run()
			if e.inDrafts(u1) {
				t.Fatal("copy still in the Drafts folder")
			}
		})
	}
}

// supersede gleich nach der Ablage, ehe der Spiegel die Kopie kennt
// (uidnext ≤ UID): copy_not_mirrored; nach dem Teil-Lauf ok.
func TestA11SupersedeNotMirrored(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	old := e.newDraft("Abrechnung")
	u1 := e.push(old, "eins", false)
	e.now = e.now.Add(time.Minute)
	nw := e.newDraft("Abrechnung")
	_, err := e.svc.Supersede(ctx, SupersedeRequest{OldID: old.ID, NewID: nw.ID, KeyID: cuKey})
	if ReasonOf(err) != ReasonCopyNotMirrored {
		t.Fatalf("err = %v", err)
	}
	e.mirrorLate(old, u1, "eins")
	res, err := e.svc.Supersede(ctx, SupersedeRequest{OldID: old.ID, NewID: nw.ID, KeyID: cuKey})
	if err != nil || res.Copies != 1 {
		t.Fatalf("after mirror: %+v %v", res, err)
	}
}

// supersede, während für eine frühere Kopie schon eine Aktion aus (c)
// wartet: die Kopie zählt als skipped, beide Aktionen enden done.
func TestA11SupersedeSkipsBusyCopy(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	old := e.newDraft("Abrechnung")
	e.push(old, "eins", true)
	old = e.update(old, "Abrechnung")
	e.push(old, "zwei", true)
	e.now = e.now.Add(time.Minute)
	nw := e.newDraft("Abrechnung")
	res, err := e.svc.Supersede(ctx, SupersedeRequest{OldID: old.ID, NewID: nw.ID, KeyID: cuKey})
	if err != nil || res.Copies != 1 || res.Skipped != 1 {
		t.Fatalf("supersede = %+v %v", res, err)
	}
	rep := e.run()
	if rep.Stale != 0 {
		t.Fatalf("run = %+v", rep)
	}
	for _, a := range e.actionsOf(old.ID) {
		if a.State != store.ActionDone {
			t.Fatalf("action %d (%s) = %s", a.ID, a.Params.Source, a.State)
		}
	}
	if e.draftCopies() != 0 {
		t.Fatalf("copies left = %d", e.draftCopies())
	}
}
