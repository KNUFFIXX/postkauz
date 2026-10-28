package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Kanarienwerte der Exporttests: Betreff, Absender, Empfänger und
// Anhangsnamen dürfen weder ins Audit noch in index.csv.
const (
	exportCanarySubject = "KANARIE-Betreff-7f3a"
	exportCanaryFrom    = "kanarie-absender@example.org"
	exportCanaryTo      = "kanarie-empfaenger@example.net"
	exportCanaryFile    = "KANARIE-anhang"
)

type exportEnv struct {
	*mirrorEnv
	dir   string
	blobs *blob.Store
	ids   []int64
	atts  []store.Attachment
}

// newExportEnv: Konto demo mit INBOX und Archiv/2026; drei Nachrichten mit
// Rohmail im Blob-Store (eine mit PDF- und Programm-Anhang), eine nur mit
// Kopf, eine ohne Ordner (Serverkopie gelöst).
func newExportEnv(t *testing.T) *exportEnv {
	t.Helper()
	m := newMirrorEnv(t)
	ctx := context.Background()
	bs, err := blob.Open(filepath.Join(t.TempDir(), "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &exportEnv{mirrorEnv: m, dir: filepath.Join(t.TempDir(), "export"), blobs: bs}
	if _, _, err := m.st.SyncMailboxes(ctx, m.acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv/2026", Name: "2026", Role: "archive"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := m.st.Mailboxes(ctx, m.acct)
	put := func(b []byte) blob.Info {
		info, err := bs.Put(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	pdf := put([]byte("%PDF-1.4\n%%EOF\n"))
	exe := put(append([]byte("MZ\x90\x00"), make([]byte, 100)...))
	uid := uint32(1)
	for i, box := range boxes {
		for n := 0; n < 2; n++ {
			raw := []byte("From: Kanarie <" + exportCanaryFrom + ">\r\nTo: " + exportCanaryTo + "\r\nSubject: " + exportCanarySubject +
				"\r\nMessage-ID: <m" + itoa(int64(i*10+n)) + "@example.org>\r\n\r\nFrom the start\r\nText\r\n")
			nm := store.NewMessage{Link: store.LinkFlags{UID: uid}, MessageID: "m" + itoa(int64(i*10+n)) + "@example.org",
				Subject: exportCanarySubject, From: "Kanarie <" + exportCanaryFrom + ">", Size: int64(len(raw)),
				InternalDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
			if n == 0 || box.Path == "INBOX" {
				info := put(raw)
				nm.BlobSHA256, nm.BlobSize = info.SHA256, info.Size
			}
			if i == 0 && n == 0 {
				nm.Attachments = []store.NewAttachment{
					{PartID: "2", Filename: exportCanaryFile + ".pdf", ContentType: "application/pdf", DeclaredType: "application/pdf",
						Size: pdf.Size, SHA256: pdf.SHA256, BlobSize: pdf.Size},
					{PartID: "3", Filename: exportCanaryFile + ".exe", ContentType: "application/x-msdownload", DeclaredType: "application/octet-stream",
						Size: exe.Size, SHA256: exe.SHA256, BlobSize: exe.Size, Executable: true}}
			}
			uid++
			if _, err := m.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: m.acct, MailboxID: box.ID, New: []store.NewMessage{nm}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, err := m.st.DBForTest().Query(`SELECT id FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		e.ids = append(e.ids, id)
	}
	_ = rows.Close()
	e.atts, _ = m.st.AttachmentsByMessage(ctx, e.ids[0])
	// Die letzte Nachricht verliert ihre Serverkopie (auf dem Server
	// gelöscht): Sie landet in _no_folder.
	if err := m.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ?`, e.ids[len(e.ids)-1]); err != nil {
		t.Fatal(err)
	}
	d := Deps{Accounts: m.st, Messages: m.st, Blobs: bs, ExportDir: e.dir, Export: m.st}
	for _, op := range []Operation{AttachmentsSave(d), MessagesExport(d)} {
		if err := m.x.Registry.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *exportEnv) exec(key policy.Key, op string, in map[string]any) (Result, error) {
	b, _ := json.Marshal(in)
	no := false
	return e.x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: op, Input: b, DryRun: &no})
}

// Akzeptanz S3.3 Punkt 3 über den Executor: Ein Schlüssel mit mail:export
// legt Anhänge nur unter export_dir ab; ".." und ein Symlink nach außen
// sind denied mit Grund im Audit, ".exe" wird ".blocked", ein Doppel "-2".
func TestAttachmentsSaveThroughExecutor(t *testing.T) {
	e := newExportEnv(t)
	pdf, exe := e.atts[0].ID, e.atts[1].ID
	res, err := e.exec(operatorKey, "attachments.save", map[string]any{"id": pdf, "subdir": "belege"})
	if err != nil {
		t.Fatal(err)
	}
	o := res.Output.(AttachmentSaveOutput)
	if o.Path != "belege/"+exportCanaryFile+".pdf" || o.Blocked || o.SHA256 != e.atts[0].SHA256 || !o.Untrusted {
		t.Fatalf("save = %+v", o)
	}
	res, _ = e.exec(operatorKey, "attachments.save", map[string]any{"id": pdf, "subdir": "belege"})
	if p := res.Output.(AttachmentSaveOutput).Path; p != "belege/"+exportCanaryFile+"-2.pdf" {
		t.Fatalf("second save = %s", p)
	}
	res, _ = e.exec(operatorKey, "attachments.save", map[string]any{"id": exe, "account": "demo", "message": e.ids[0]})
	if o := res.Output.(AttachmentSaveOutput); o.Path != exportCanaryFile+".exe.blocked" || !o.Blocked {
		t.Fatalf("exe = %+v", o)
	}
	outside := filepath.Join(filepath.Dir(e.dir), "draussen")
	_ = os.MkdirAll(outside, 0o700)
	if err := os.Symlink(outside, filepath.Join(e.dir, "link")); err != nil {
		t.Fatal(err)
	}
	for sub, reason := range map[string]string{"..": "outside_export_dir", "../draussen": "outside_export_dir", "link": "outside_export_dir",
		"/tmp": "outside_export_dir", "CON": "invalid_subdir"} {
		before := len(e.au.Events())
		_, err := e.exec(operatorKey, "attachments.save", map[string]any{"id": pdf, "subdir": sub})
		var de *DeniedError
		if !errors.As(err, &de) || de.Reason != reason {
			t.Errorf("subdir %q: %v, want denied %s", sub, err, reason)
			continue
		}
		evs := e.au.Events()[before:]
		if len(evs) != 2 || evs[1].Result != audit.ResultDenied || evs[1].Reason != reason {
			t.Errorf("subdir %q: audit %+v", sub, evs)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("written outside export_dir: %v", entries)
	}
	// Fremde Nachricht bzw. fremdes Konto: nicht gefunden.
	if _, err := e.exec(operatorKey, "attachments.save", map[string]any{"id": pdf, "message": e.ids[1]}); !errors.Is(err, store.ErrAttachmentNotFound) {
		t.Fatalf("other message: %v", err)
	}
	// reader und assistant haben kein mail:export.
	for _, role := range []string{"reader", "assistant", "triage"} {
		_, err := e.exec(policy.Key{ID: "key:" + role, Scopes: policy.Roles[role]}, "attachments.save", map[string]any{"id": pdf})
		if !errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v", role, err)
		}
	}
	// Ein Agent erreicht messages.export nie, auch nicht als admin.
	_, err = e.exec(policy.Key{ID: "key:adm", Scopes: policy.Roles["admin"]}, "messages.export", map[string]any{"format": "eml", "to": "x"})
	var pe *PolicyError
	if !errors.As(err, &pe) || pe.Decision.Reason != "human_only" {
		t.Fatalf("admin key export: %v", err)
	}
}

// Kanarientest S3.3 Punkt 4: Betreff, Adressen und Anhangsnamen stehen
// weder im Audit noch in index.csv; der Export ordnet Nachrichten ohne
// Serverkopie unter _no_folder ein, Nachrichten nur mit Kopf zählen als
// übersprungen.
func TestExportCanary(t *testing.T) {
	e := newExportEnv(t)
	human := policy.Key{ID: "cli", Scopes: policy.Roles["admin"], Human: true}
	if _, err := e.exec(operatorKey, "attachments.save", map[string]any{"id": e.atts[0].ID, "subdir": "x"}); err != nil {
		t.Fatal(err)
	}
	var events []Progress
	b, _ := json.Marshal(map[string]any{"format": "eml", "to": "eml"})
	no := false
	res, err := e.x.Execute(context.Background(), Request{Key: human, Actor: audit.ActorHuman, Operation: "messages.export", Input: b,
		DryRun: &no, Progress: func(p Progress) { events = append(events, p) }})
	if err != nil {
		t.Fatal(err)
	}
	o := res.Output.(MessagesExportOutput)
	if o.Location != filepath.Join(e.dir, "eml") || o.Messages != 3 || o.Skipped != 1 || o.SkippedBy["no_blob"] != 1 ||
		len(o.SkippedIDs) != 1 || len(o.Folders) != 3 || o.Folders[0].Folder != "Archiv/2026" || o.Folders[0].Path != "Archiv/2026" ||
		o.Folders[2].Folder != ExportNoFolderName || len(events) != 3 || events[0].Event != "export_folder" {
		t.Fatalf("export = %+v, events %+v", o, events)
	}
	if _, err := e.exec(human, "messages.export", map[string]any{"format": "mbox", "to": filepath.Join(e.dir, "mbox")}); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(e.dir, "eml", "index.csv"))
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := json.Marshal(e.au.Events())
	for _, c := range []string{exportCanarySubject, exportCanaryFrom, exportCanaryTo, exportCanaryFile} {
		if bytes.Contains(evs, []byte(c)) {
			t.Errorf("audit contains %q", c)
		}
		for _, ev := range e.au.Events() {
			if strings.Contains(string(ev.ParamsRedacted), c) {
				t.Errorf("audit params contain %q", c)
			}
		}
		if bytes.Contains(idx, []byte(c)) {
			t.Errorf("index.csv contains %q", c)
		}
	}
	// Die mbox des Archivordners liegt wie bei Thunderbird in Archiv.sbd.
	if _, err := os.Stat(filepath.Join(e.dir, "mbox", "Archiv.sbd", "2026")); err != nil {
		t.Fatal(err)
	}
	// Ein Ziel mit Inhalt: denied, nichts ändert sich.
	_, err = e.exec(human, "messages.export", map[string]any{"format": "mbox", "to": "eml"})
	var de *DeniedError
	if !errors.As(err, &de) || de.Reason != "target_not_fresh" {
		t.Fatalf("existing target: %v", err)
	}
	// Unbekannter Ordner: nicht gefunden.
	if _, err := e.exec(human, "messages.export", map[string]any{"format": "mbox", "to": "neu", "mailbox": "Fehlt"}); !errors.Is(err, store.ErrMailboxNotFound) {
		t.Fatalf("unknown mailbox: %v", err)
	}
	// Trockenlauf und --mailbox/--since: zählt nur, legt nichts an.
	yes := true
	b, _ = json.Marshal(map[string]any{"format": "eml", "to": "trocken", "mailbox": "INBOX", "since": "2026-01-01"})
	res, err = e.x.Execute(context.Background(), Request{Key: human, Actor: audit.ActorHuman, Operation: "messages.export", Input: b, DryRun: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if o := res.Output.(MessagesExportOutput); !o.DryRun || o.Messages != 2 || len(o.Folders) != 1 {
		t.Fatalf("dry run = %+v", o)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "trocken")); err == nil {
		t.Fatal("dry run created the target")
	}
	b, _ = json.Marshal(map[string]any{"format": "eml", "to": "spaet", "since": "2026-06-01"})
	res, err = e.x.Execute(context.Background(), Request{Key: human, Actor: audit.ActorHuman, Operation: "messages.export", Input: b, DryRun: &yes})
	if err != nil || res.Output.(MessagesExportOutput).Messages != 0 {
		t.Fatalf("since: %+v %v", res.Output, err)
	}
}
