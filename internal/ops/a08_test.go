package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
)

// A08 Teil 1: Anhang-Sperre über den Executor (CLI, MCP und HTTP nehmen
// denselben Weg) und im Push. Alle Pfade sind Platzhalter unter t.TempDir().

func a08Files(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("Probe A08\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// deniedParams liefert die Einzelheiten der letzten Ablehnung op im Audit.
func deniedParams(t *testing.T, evs []audit.Event, op string) map[string]string {
	t.Helper()
	for i := len(evs) - 1; i >= 0; i-- {
		ev := evs[i]
		if ev.Operation != op || ev.Result != audit.ResultDenied {
			continue
		}
		if ev.Reason != string(compose.ReasonAttachDenied) {
			t.Fatalf("reason = %s", ev.Reason)
		}
		var p struct {
			Denied map[string]string `json:"denied"`
		}
		if err := json.Unmarshal(ev.ParamsRedacted, &p); err != nil {
			t.Fatal(err)
		}
		return p.Denied
	}
	t.Fatalf("no denied %s in the audit", op)
	return nil
}

func TestA08DraftOpsDenyBlockedFiles(t *testing.T) {
	e := newDraftEnv(t, false)
	a08Files(t, e.attach, "_secrets/zugang.txt", "Vault.kdbx", "probe-token.json", "Mieter/Brief 2026-11-12.pdf")
	for _, c := range []struct {
		name string
		in   map[string]any
		dry  bool
		rule string
		file string
	}{
		{"create dir", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"}, "subject": "Probe",
			"body_markdown": "Text", "attachments": []map[string]any{{"file": "_secrets/zugang.txt"}}}, false, compose.RuleDir, "zugang.txt"},
		{"create dry run", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"}, "subject": "Probe",
			"body_markdown": "Text", "attachments": []map[string]any{{"file": "Vault.kdbx"}}}, true, compose.RuleFile, "Vault.kdbx"},
		{"absolute path", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"}, "subject": "Probe",
			"body_markdown": "Text", "attachments": []map[string]any{{"file": filepath.Join(e.attach, "probe-token.json")}}}, false,
			compose.RuleFile, "probe-token.json"},
		{"stream", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"}, "subject": "Probe",
			"body_markdown": "Text", "attachments": []map[string]any{{"file": "probe.txt:stream"}}}, false, compose.RuleStream, "probe.txt:stream"},
		{"reply", map[string]any{"kind": "reply", "in_reply_to": 1, "body_markdown": "Text",
			"attachments": []map[string]any{{"file": "Vault.kdbx"}}}, false, compose.RuleFile, "Vault.kdbx"},
		{"forward", map[string]any{"kind": "forward", "in_reply_to": 1, "to": []string{"b@example.org"}, "body_markdown": "Text",
			"attachments": []map[string]any{{"file": "_secrets/zugang.txt"}}}, false, compose.RuleDir, "zugang.txt"},
		// Ein harmloser Name ändert nichts: geprüft wird die Datei.
		{"renamed", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"}, "subject": "Probe",
			"body_markdown": "Text", "attachments": []map[string]any{{"file": "Vault.kdbx", "filename": "brief.pdf"}}}, false,
			compose.RuleFile, "Vault.kdbx"},
		// Ein gesperrter Name für eine harmlose Datei auch nicht.
		{"blocked filename", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"}, "subject": "Probe",
			"body_markdown": "Text", "attachments": []map[string]any{{"file": "Mieter/Brief 2026-11-12.pdf", "filename": "token.json"}}},
			false, compose.RuleFile, "token.json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.do(t, assistant, "drafts.create", c.in, c.dry)
			wantDenied(t, err, string(compose.ReasonAttachDenied))
			if msg := err.Error(); !strings.Contains(msg, "attachments[0]") || !strings.Contains(msg, "rule: "+c.rule) ||
				strings.Contains(msg, e.attach) || strings.Contains(msg, "_secrets") {
				t.Fatalf("message = %q", msg)
			}
			evs, text := e.auditJSON(t)
			p := deniedParams(t, evs, "drafts.create")
			if p["rule"] != c.rule || p["file"] != c.file || p["field"] != "attachments[0]" {
				t.Fatalf("audit denied = %v", p)
			}
			if strings.Contains(text, e.attach) || strings.Contains(text, "_secrets") || strings.Contains(text, "Probe A08") {
				t.Fatal("the audit names the path or the content")
			}
		})
	}
	// drafts.update mit einer gesperrten Datei.
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Probe", "body_markdown": "Text", "attachments": []map[string]any{{"file": "Mieter/Brief 2026-11-12.pdf"}}}).(DraftsWriteOutput)
	_, err := e.do(t, assistant, "drafts.update", map[string]any{"draft_id": out.DraftID,
		"attachments": []map[string]any{{"file": "probe-token.json"}}}, false)
	wantDenied(t, err, string(compose.ReasonAttachDenied))
	evs, _ := e.auditJSON(t)
	if p := deniedParams(t, evs, "drafts.update"); p["rule"] != compose.RuleFile {
		t.Fatalf("update audit = %v", p)
	}
	// Erlaubte Datei: gespeichert mit Herkunft file.
	d, err := e.st.Draft(context.Background(), out.DraftID)
	if err != nil || len(d.Attachments) != 1 || d.Attachments[0].Source != compose.SourceFile {
		t.Fatalf("stored attachments = %+v, %v", d.Attachments, err)
	}
}

// Ein vor A08 angelegter Entwurf mit gesperrtem Anhang (gespeichert ohne
// Herkunft) wird beim Ablegen nicht hochgeladen: attach_denied im Bericht,
// push_failed mit Grund am Entwurf, ein Systemereignis denied. Ein Anhang
// aus dem Spiegel (Herkunft blob) mit demselben Namen ist nicht betroffen.
func TestA08PushDeniesOldDraftWithBlockedAttachment(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	a08Files(t, e.attach, "vertrag.txt")
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Probe A08", "body_markdown": "Text", "attachments": []map[string]any{{"file": "vertrag.txt"}}}).(DraftsWriteOutput)
	// Stand vor A08: Name gesperrt, keine Herkunft im JSON.
	if err := e.st.ExecForTest(`UPDATE drafts SET attachment_blobs = replace(replace(attachment_blobs, '"vertrag.txt"', '"Vault.kdbx"'),
		',"source":"file"', '') WHERE id = ?`, out.DraftID); err != nil {
		t.Fatal(err)
	}
	if d := e.draft(t, out.DraftID); len(d.Attachments) != 1 || d.Attachments[0].Filename != "Vault.kdbx" || d.Attachments[0].Source != "" {
		t.Fatalf("setup = %+v", d.Attachments)
	}
	rep := e.push(t)
	if rep.AttachDenied != 1 || rep.Failed != 0 || rep.Pushed != 2 {
		t.Fatalf("report = %+v", rep)
	}
	for i, mid := range e.app.mids {
		if mid == out.MessageID || bytes.Contains(e.app.raws[i], []byte("Probe A08")) {
			t.Fatal("the blocked draft was appended")
		}
	}
	d := e.draft(t, out.DraftID)
	if d.PushNote != drafts.PushNotePushFailed || d.RemoteState != drafts.RemotePending {
		t.Fatalf("draft = note %q remote %q", d.PushNote, d.RemoteState)
	}
	evs, text := e.auditJSON(t)
	count := func() int {
		n := 0
		evs, _ := e.auditJSON(t)
		for _, ev := range evs {
			if ev.Operation == drafts.OpPush && ev.Result == audit.ResultDenied && ev.Reason == string(compose.ReasonAttachDenied) {
				n++
			}
		}
		return n
	}
	if p := deniedPush(t, evs); p["rule"] != compose.RuleFile || p["file"] != "Vault.kdbx" || p["field"] != "attachments[0]" {
		t.Fatalf("push audit = %v", p)
	}
	if strings.Contains(text, e.attach) {
		t.Fatal("audit names the path")
	}
	// Erneuter Versuch: wieder nicht abgelegt, kein zweites Ereignis.
	if rep := e.push(t); rep.AttachDenied != 1 || rep.Pushed != 0 {
		t.Fatalf("second push = %+v", rep)
	}
	if n := count(); n != 1 {
		t.Fatalf("denied events = %d", n)
	}
	// draft show nennt den Grund.
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if !strings.Contains(got.PushNoteText, "attach_denied in attachments[0]") {
		t.Fatalf("push note text = %q", got.PushNoteText)
	}
	// Derselbe Name aus dem Spiegel (Herkunft blob) wird abgelegt.
	if err := e.st.ExecForTest(`UPDATE drafts SET attachment_blobs = replace(attachment_blobs, '"Vault.kdbx"', '"Vault.kdbx","source":"blob"')
		WHERE id = ?`, out.DraftID); err != nil {
		t.Fatal(err)
	}
	if rep := e.push(t); rep.Pushed != 1 || rep.AttachDenied != 0 {
		t.Fatalf("blob source push = %+v", rep)
	}
	if e.app.mids[len(e.app.mids)-1] != out.MessageID {
		t.Fatal("draft with a mirrored attachment was not appended")
	}
	_ = ctx
}

func deniedPush(t *testing.T, evs []audit.Event) map[string]any {
	t.Helper()
	for i := len(evs) - 1; i >= 0; i-- {
		ev := evs[i]
		if ev.Operation == drafts.OpPush && ev.Result == audit.ResultDenied && ev.Reason == string(compose.ReasonAttachDenied) {
			p := map[string]any{}
			if err := json.Unmarshal(ev.ParamsRedacted, &p); err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	t.Fatal("no denied drafts.push in the audit")
	return nil
}

// Nachbesserung A08, Punkt 5: Ein Altentwurf, der schon aus anderem Grund
// mit push_failed vermerkt ist (etwa ein früherer Netzfehler), bekommt bei
// seiner ersten Erkennung als attach_denied trotzdem genau ein
// Systemereignis; weitere Versuche schreiben keins. Nach einer Änderung des
// Inhalts (neuer Stand, wieder gesperrt) entsteht ein neues.
func TestA08PushDeniedEventDespitePushFailedNote(t *testing.T) {
	e := newPushEnv(t)
	a08Files(t, e.attach, "vertrag.txt")
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Probe A08", "body_markdown": "Text", "attachments": []map[string]any{{"file": "vertrag.txt"}}}).(DraftsWriteOutput)
	if err := e.st.ExecForTest(`UPDATE drafts SET push_note = 'push_failed',
		attachment_blobs = replace(replace(attachment_blobs, '"vertrag.txt"', '"Vault.kdbx"'), ',"source":"file"', '')
		WHERE id = ?`, out.DraftID); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		n := 0
		evs, _ := e.auditJSON(t)
		for _, ev := range evs {
			if ev.Operation == drafts.OpPush && ev.Result == audit.ResultDenied && ev.Reason == string(compose.ReasonAttachDenied) &&
				slices.Contains(ev.Targets, "draft:"+strconv.FormatInt(out.DraftID, 10)) {
				n++
			}
		}
		return n
	}
	for i := 1; i <= 2; i++ {
		if rep := e.push(t); rep.AttachDenied != 1 {
			t.Fatalf("push %d = %+v", i, rep)
		}
		if n := count(); n != 1 {
			t.Fatalf("push %d: denied events = %d, want 1", i, n)
		}
	}
	if d := e.draft(t, out.DraftID); d.PushNote != drafts.PushNotePushFailed {
		t.Fatalf("push note = %q", d.PushNote)
	}
	// Punkt 4: drafts.get nennt die fehlende Herkunft (D14, Schritt 2.6).
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if len(got.Attachments) != 1 || got.Attachments[0].Origin != OriginUnknown {
		t.Fatalf("attachments = %+v", got.Attachments)
	}
	// Neuer Inhaltsstand (später geändert), Anhang weiter gesperrt: ein
	// neues Ereignis.
	if err := e.st.ExecForTest(`UPDATE drafts SET content_changed_at = ? WHERE id = ?`,
		time.Now().Add(time.Hour).UnixMilli(), out.DraftID); err != nil {
		t.Fatal(err)
	}
	if rep := e.push(t); rep.AttachDenied != 1 {
		t.Fatalf("push after change = %+v", rep)
	}
	if n := count(); n != 2 {
		t.Fatalf("after a content change: denied events = %d, want 2", n)
	}
}
