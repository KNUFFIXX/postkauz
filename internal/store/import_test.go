package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/search"
)

func importSearch(t *testing.T, s *SQLite, acct int64, q string) []int64 {
	t.Helper()
	parsed, err := search.Parse(q, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Search(context.Background(), SearchParams{Query: parsed, AccountID: acct, Limit: 50, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	slices.Sort(ids)
	return ids
}

// Import neben dem Sync: neue Nachrichten tragen deleted_remote_at und die
// Flags der Kopie; vorhandene bekommen nur den Ordner, die Flags einer
// gesyncten Nachricht bleiben (Server gewinnt); der Stand der Quelle steht
// in derselben Transaktion.
func TestApplyImportNewLinksFlagsAndState(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	synced := newMsg(1, "synced@example.org", false)
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, New: []NewMessage{synced}}); err != nil {
		t.Fatal(err)
	}
	syncedID, ok, err := s.MessageByHeaderID(ctx, acct, "synced@example.org")
	if err != nil || !ok {
		t.Fatalf("by header: %v %v", ok, err)
	}
	if id, ok, _ := s.MessageByContentHash(ctx, acct, "nope", "h-synced@example.org"); !ok || id != syncedID {
		t.Fatalf("by hash = %d %v", id, ok)
	}

	imp := newMsg(0, "imported@example.org", true)
	imp.Link.Keywords = []string{"$label1"}
	src := &ImportSource{Source: "/x/Archiv", Format: "mbox", Folder: "Archiv/2024", Mode: ImportContent, State: ImportRunning,
		Position: 1234, Size: 5000, MTime: 42}
	res, err := s.ApplyImport(ctx, ImportBatch{AccountID: acct,
		New: []ImportNew{{Msg: imp, Folders: []ImportFolderFlags{{Folder: "Archiv/2024", Seen: true, Keywords: []string{"$label1"}},
			{Folder: "[Gmail]/Wichtig", Flagged: true}}}},
		Links:  []ImportLink{{MessageID: syncedID, ImportFolderFlags: ImportFolderFlags{Folder: "Archiv/2024", Flagged: true}}},
		Source: src})
	if err != nil || res.New != 1 || res.Linked != 1 {
		t.Fatalf("apply = %+v %v", res, err)
	}
	impID, ok, _ := s.MessageByHeaderID(ctx, acct, "imported@example.org")
	if !ok {
		t.Fatal("imported message missing")
	}
	m, err := s.MessageByID(ctx, impID)
	if err != nil {
		t.Fatal(err)
	}
	if m.DeletedRemoteAt.IsZero() || !m.Seen || !m.Flagged || !slices.Equal(m.Keywords, []string{"$label1"}) || m.ThreadID == 0 {
		t.Fatalf("imported message = %+v", m)
	}
	if len(m.Locations) != 2 || m.Locations[0].Role != "import" || m.Locations[0].Mailbox != "Archiv/2024" || m.Locations[1].Mailbox != "[Gmail]/Wichtig" {
		t.Fatalf("locations = %+v", m.Locations)
	}
	sm, _ := s.MessageByID(ctx, syncedID)
	if sm.Flagged || !sm.DeletedRemoteAt.IsZero() || len(sm.Locations) != 2 || sm.Locations[0].Mailbox != "INBOX" || sm.Locations[1].Role != "import" {
		t.Fatalf("synced message changed by import = %+v", sm)
	}
	if n, _ := s.CountPresentMessages(ctx, acct); n != 1 {
		t.Fatalf("present on server = %d, want 1 (the imported one is not)", n)
	}

	// Wiederholung: nichts doppelt; Flag einer weiteren Kopie (nur importiert)
	// wird übernommen.
	res, err = s.ApplyImport(ctx, ImportBatch{AccountID: acct, Links: []ImportLink{
		{MessageID: syncedID, ImportFolderFlags: ImportFolderFlags{Folder: "Archiv/2024"}},
		{MessageID: impID, ImportFolderFlags: ImportFolderFlags{Folder: "Gesendet", Answered: true}}}})
	if err != nil || res.New != 0 || res.Linked != 1 {
		t.Fatalf("repeat = %+v %v", res, err)
	}
	if m, _ := s.MessageByID(ctx, impID); !m.Answered {
		t.Fatal("flag of a further imported copy not merged")
	}

	// Stand der Quelle.
	got, ok, err := s.ImportSourceState(ctx, acct, "/x/Archiv", "Archiv/2024", ImportContent)
	if err != nil || !ok || got.State != ImportRunning || got.Position != 1234 || got.Size != 5000 || got.MTime != 42 || got.Format != "mbox" {
		t.Fatalf("state = %+v %v %v", got, ok, err)
	}
	src.State, src.Position = ImportDone, 5000
	if _, err := s.ApplyImport(ctx, ImportBatch{AccountID: acct, Source: src}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.ImportSourceState(ctx, acct, "/x/Archiv", "Archiv/2024", ImportContent); got.State != ImportDone || got.Position != 5000 {
		t.Fatalf("state after done = %+v", got)
	}
	if _, ok, _ := s.ImportSourceState(ctx, acct, "/x/Archiv", "Archiv/2024", ImportLabels); ok {
		t.Fatal("labels mode is a separate source")
	}

	// Suche: in: findet importierte Ordner (Pfad, Unterordner, Rolle import).
	for q, want := range map[string][]int64{
		"in:Archiv/2024": {syncedID, impID}, "in:archiv": {syncedID, impID}, "in:import": {syncedID, impID},
		"in:Gesendet": {impID}, "in:inbox": {syncedID}, `in:"[Gmail]/Wichtig"`: {impID},
	} {
		if got := importSearch(t, s, acct, q); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", q, got, want)
		}
	}

	// Der Sync findet die importierte Nachricht später auf dem Server: Sie
	// wird zugeordnet, deleted_remote_at verschwindet, Flags kommen vom Server.
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID,
		Links: []Link{{MessageID: impID, Flags: LinkFlags{UID: 2}}}}); err != nil {
		t.Fatal(err)
	}
	m, _ = s.MessageByID(ctx, impID)
	if !m.DeletedRemoteAt.IsZero() || m.Seen || m.Flagged {
		t.Fatalf("after sync link = %+v", m)
	}
	if folders, _ := s.ImportFolders(ctx, impID); len(folders) != 3 {
		t.Fatalf("import folders kept = %v", folders)
	}
	// Der Ordner des Servers verschwindet: Importierte Ordner bleiben.
	if _, _, err := s.SyncMailboxes(ctx, acct, nil); err != nil {
		t.Fatal(err)
	}
	if got := importSearch(t, s, acct, "in:import"); !slices.Equal(got, []int64{syncedID, impID}) {
		t.Fatalf("import folders after server folder removal = %v", got)
	}
}
