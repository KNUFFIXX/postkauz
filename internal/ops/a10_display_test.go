package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A10 Teil 3: Treffer sagen, was sie sind. Spiegel mit Posteingang,
// Entwürfen, Gesendet, „Alle Nachrichten“ und einer virtuellen Sicht:
//
//	inbox:     empfangen, nur im Posteingang
//	gdraft:    Gmail-Entwurf ohne \Draft im Entwürfe-Ordner (B3)
//	scheduled: geplant (nur „Alle Nachrichten“, Termin in 3 Tagen, eigener
//	           Absender), zusätzlich in der virtuellen Sicht
//	sent:      gesendet (Gesendet und „Alle Nachrichten“)
//	gone:      gelöscht, ohne Serverkopie
type a10Mirror struct {
	*mirrorEnv
	ids map[string]int64
}

func newA10Mirror(t *testing.T) *a10Mirror {
	t.Helper()
	e := newMirrorEnv(t)
	ctx := context.Background()
	if _, _, err := e.st.SyncMailboxes(ctx, e.acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "[Gmail]/Entwürfe", Name: "Entwürfe", Role: "drafts"}, {Path: "[Gmail]/Gesendet", Name: "Gesendet", Role: "sent"},
		{Path: "[Gmail]/Alle Nachrichten", Name: "Alle Nachrichten", Role: "all"},
		{Path: "[Gmail]/Wichtig", Name: "Wichtig", Role: "custom", Virtual: true}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	box := map[string]int64{}
	for _, b := range boxes {
		box[b.Path] = b.ID
	}
	m := &a10Mirror{mirrorEnv: e, ids: map[string]int64{}}
	uid := map[int64]uint32{}
	put := func(name, from string, date time.Time, draft bool, paths ...string) {
		t.Helper()
		var id int64
		for i, p := range paths {
			uid[box[p]]++
			b := store.MirrorBatch{AccountID: e.acct, MailboxID: box[p],
				State: &store.MailboxState{UIDValidity: 1, UIDNext: uid[box[p]] + 1, Phase: store.PhaseReady}}
			fl := store.LinkFlags{UID: uid[box[p]], Seen: true, Draft: draft}
			if i == 0 {
				b.New = []store.NewMessage{{Link: fl, MessageID: name + "@example.org", Subject: "Probe " + name, From: from,
					To: json.RawMessage(`[{"address":"b@example.org"}]`), Size: int64(100 + len(name)), TextPlain: "Text " + name,
					TextUnquoted: "Text " + name, CleanVersion: 1, Links: json.RawMessage("[]"), DateHeader: date, InternalDate: date}}
			} else {
				b.Links = []store.Link{{MessageID: id, Flags: fl}}
			}
			if _, err := e.st.ApplyBatch(ctx, b); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, name+"@example.org")
				id = ids[0]
			}
		}
		m.ids[name] = id
	}
	now := time.Now().UTC().Truncate(time.Second)
	own := "Tester <tester@example.invalid>"
	put("inbox", "Anna <anna@example.org>", now.Add(-5*time.Hour), false, "INBOX")
	put("gdraft", own, now.Add(-4*time.Hour), false, "[Gmail]/Entwürfe", "[Gmail]/Alle Nachrichten")
	put("scheduled", own, now.Add(72*time.Hour), false, "[Gmail]/Alle Nachrichten", "[Gmail]/Wichtig")
	put("sent", own, now.Add(-2*time.Hour), false, "[Gmail]/Gesendet", "[Gmail]/Alle Nachrichten")
	put("gone", own, now.Add(-1*time.Hour), false, "INBOX")
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ?`, m.ids["gone"]); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE messages SET deleted_remote_at = 1 WHERE id = ?`, m.ids["gone"]); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m *a10Mirror) search(t *testing.T, in map[string]any) SearchOutput {
	t.Helper()
	out, err := m.run("messages.search", in, "", false)
	if err != nil {
		t.Fatalf("%v: %v", in, err)
	}
	return out.(SearchOutput)
}

// roles, scheduled_for und Marken in messages.search, threads.get und
// messages.get.
func TestA10SearchRolesAndScheduled(t *testing.T) {
	m := newA10Mirror(t)
	o := m.search(t, map[string]any{"query": "after:2000-01-01", "limit": 50})
	want := map[string][]string{"inbox": {"inbox"}, "gdraft": {"all", "drafts"}, "scheduled": {"all"}, "sent": {"all", "sent"},
		"gone": {}}
	if len(o.Hits) != len(want) {
		t.Fatalf("hits = %d", len(o.Hits))
	}
	for name, roles := range want {
		var hit *SearchHitOutput
		for i := range o.Hits {
			if o.Hits[i].ID == m.ids[name] {
				hit = &o.Hits[i]
			}
		}
		if hit == nil || !slices.Equal(hit.Roles, roles) || hit.Roles == nil {
			t.Fatalf("%s: roles = %#v", name, hit)
		}
		if (hit.ScheduledFor != "") != (name == "scheduled") {
			t.Fatalf("%s: scheduled_for = %q", name, hit.ScheduledFor)
		}
		// flags zeigt draft weiter nur bei \Draft.
		if slices.Contains(hit.Flags, "draft") {
			t.Fatalf("%s: flags = %v", name, hit.Flags)
		}
	}
	// JSON: roles immer da (auch leer), scheduled_for nur bei geplanten.
	b, _ := json.Marshal(o.Hits)
	var raw []map[string]any
	_ = json.Unmarshal(b, &raw)
	for _, h := range raw {
		if _, ok := h["roles"]; !ok {
			t.Fatalf("hit without roles: %v", h)
		}
	}
	th, err := m.run("threads.get", map[string]any{"message": m.ids["scheduled"]}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	tm := th.(ThreadOutput).Messages
	if len(tm) != 1 || !slices.Equal(tm[0].Roles, []string{"all"}) || tm[0].ScheduledFor == "" {
		t.Fatalf("thread = %+v", tm)
	}
	g, err := m.run("messages.get", map[string]any{"id": m.ids["scheduled"]}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if mo := g.(MessageOutput); mo.ScheduledFor == "" || mo.ScheduledFor != tm[0].ScheduledFor {
		t.Fatalf("get scheduled_for = %q", mo.ScheduledFor)
	}
	g, _ = m.run("messages.get", map[string]any{"id": m.ids["sent"]}, "", false)
	if mo := g.(MessageOutput); mo.ScheduledFor != "" {
		t.Fatalf("sent message scheduled: %q", mo.ScheduledFor)
	}
}

// is:draft trifft auch einen Entwurf ohne \Draft im Entwürfe-Ordner;
// is:scheduled nur die geplante Nachricht; -is:scheduled den Rest.
func TestA10SearchIsDraftAndScheduled(t *testing.T) {
	m := newA10Mirror(t)
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"is:draft", []string{"gdraft"}},
		{"is:scheduled", []string{"scheduled"}},
		{"-is:scheduled", []string{"gone", "sent", "gdraft", "inbox"}},
		{"is:scheduled in:all", []string{"scheduled"}},
	} {
		o := m.search(t, map[string]any{"query": c.q, "limit": 50})
		var got []string
		for _, h := range o.Hits {
			for name, id := range m.ids {
				if id == h.ID {
					got = append(got, name)
				}
			}
		}
		if !slices.Equal(got, c.want) || o.Total != len(c.want) {
			t.Errorf("%s = %v (total %d), want %v", c.q, got, o.Total, c.want)
		}
	}
	// Ein fremder Absender mit Termin in der Zukunft ist nicht geplant.
	if err := m.st.ExecForTest(`UPDATE messages SET from_addr = 'Anna <anna@example.org>' WHERE id = ?`, m.ids["scheduled"]); err != nil {
		t.Fatal(err)
	}
	if o := m.search(t, map[string]any{"query": "is:scheduled"}); len(o.Hits) != 0 {
		t.Fatalf("other sender = %+v", o.Hits)
	}
	if o := m.search(t, map[string]any{"query": "after:2000-01-01", "limit": 50}); o.Hits[0].ScheduledFor != "" {
		t.Fatalf("other sender shows scheduled_for")
	}
	// Unbekannter is:-Wert: der Fehlertext nennt die neuen Werte.
	_, err := m.run("messages.search", map[string]any{"query": "is:sent"}, "", false)
	var ie *InputError
	if !errors.As(err, &ie) || !strings.Contains(ie.Error(), "is:scheduled") || !strings.Contains(ie.Error(), "in:sent") ||
		!strings.Contains(ie.Error(), "Drafts folder") {
		t.Fatalf("is:sent = %v", err)
	}
}

// Cursor und Sortierung bleiben: Seiten zu je 2 Treffern ergeben dieselbe
// Reihenfolge wie eine Seite mit allen.
func TestA10SearchCursorUnchanged(t *testing.T) {
	m := newA10Mirror(t)
	all := m.search(t, map[string]any{"query": "after:2000-01-01", "limit": 50})
	var paged []int64
	cursor := ""
	for i := 0; i < 10; i++ {
		in := map[string]any{"query": "after:2000-01-01", "limit": 2}
		if cursor != "" {
			in["cursor"] = cursor
		}
		o := m.search(t, in)
		for _, h := range o.Hits {
			paged = append(paged, h.ID)
		}
		if cursor = o.NextCursor; cursor == "" {
			break
		}
	}
	var whole []int64
	for _, h := range all.Hits {
		whole = append(whole, h.ID)
	}
	if !slices.Equal(paged, whole) {
		t.Fatalf("paged %v, whole %v", paged, whole)
	}
	if fmt.Sprint(whole[0]) != fmt.Sprint(m.ids["scheduled"]) {
		t.Fatalf("newest first: %v", whole) // Termin in der Zukunft steht oben (B2)
	}
}
