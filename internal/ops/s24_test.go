package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Das Windows-Beispiel aus docs/cli.md (Abschnitt 5) ist gültiges TOML für
// die Policy-Datei: mehrzeilige Liste, einfache Anführungszeichen lassen
// die Backslashes des Pfads stehen.
func TestDocumentedWindowsHookPolicyParses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "policy.toml")
	doc := `[draft]
check_hook = ['powershell.exe', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
              '-File', 'C:\PostKauz\hooks\check-example.ps1', '{meta_json}', '{body_text}']
check_hook_timeout = "30s"
`
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := policy.LoadFile(p, true)
	if err != nil {
		t.Fatal(err)
	}
	h := f.Doc.Draft.CheckHook
	if len(h) != 9 || h[0] != "powershell.exe" || h[6] != `C:\PostKauz\hooks\check-example.ps1` || h[7] != "{meta_json}" ||
		f.Doc.Draft.CheckHookTimeout != 30*time.Second {
		t.Fatalf("check_hook = %q, timeout %s", h, f.Doc.Draft.CheckHookTimeout)
	}
	if NewHookChecker(h, f.Doc.Draft.CheckHookTimeout, nil) == nil {
		t.Fatal("no checker for a configured hook")
	}
}

// S2.4: Abbildung eines Entwurfs auf den Prüf-Hook (internal/hook). Die
// Prozesse selbst testet internal/hook, den Weg durch die CLI
// internal/cli (TestDraftRequestRunsCheckHook).

func TestNewHookCheckerEmptyIsNil(t *testing.T) {
	if c := NewHookChecker(nil, 0, nil); c != nil {
		t.Fatalf("empty check_hook gives %T, want nil (pass without a process)", c)
	}
	if c := NewHookChecker([]string{}, time.Second, nil); c != nil {
		t.Fatalf("empty list gives %T", c)
	}
	cmd := []string{"lint", "{eml}"}
	c := NewHookChecker(cmd, time.Second, nil).(hookChecker)
	cmd[0] = "changed"
	if c.r.Command[0] != "lint" || c.r.Timeout != time.Second {
		t.Fatalf("runner %+v shares the slice or lost the timeout", c.r)
	}
}

// {meta_json}: Kopffelder der fertigen Nachricht, Anhänge mit Name, Größe
// und Typ, Konto, Identität; kein Text, keine Hashes, ein Schlüssel der
// obersten Ebene je Zeile (das Beispiel-Skript sucht die Betreffzeile).
func TestHookMeta(t *testing.T) {
	m := &compose.Message{Raw: []byte("x"), MessageID: "pk.1@example.org", Date: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		From: compose.Address{Name: "Demo Büro", Address: "demo@example.org"}, ReplyTo: []compose.Address{{Address: "r@example.org"}},
		To: []compose.Address{{Name: "Bert", Address: "b@example.org"}}, Subject: `Re: <Heizung> "Bad" TODO`, InReplyTo: "a@example.org",
		References: []string{"a@example.org"}, BodyText: "GeheimerTextKanarie", BodyHTML: "<p>GeheimerTextKanarie</p>",
		Attachments: []compose.AttachmentInfo{{Filename: "plan.pdf", ContentType: "application/pdf", Size: 1234, SHA256: "abc123hash"}}}
	in := DraftCheckInput{Draft: store.Draft{ID: 7, Kind: "reply", IdentityID: 2, FromName: "Demo Büro", FromEmail: "demo@example.org"},
		Account: "demo", EML: m.Raw, Message: m}
	b, err := HookMeta(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "\n  \"subject\": \"Re: <Heizung> \\\"Bad\\\" TODO\",\n") {
		t.Fatalf("subject line:\n%s", s)
	}
	for _, bad := range []string{"GeheimerTextKanarie", "abc123hash"} {
		if strings.Contains(s, bad) {
			t.Fatalf("meta contains %q:\n%s", bad, s)
		}
	}
	var doc hookMetaDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.DraftID != 7 || doc.Kind != "reply" || doc.Account != "demo" || doc.Identity.ID != 2 ||
		doc.Identity.Email != "demo@example.org" || doc.From.Name != "Demo Büro" || len(doc.To) != 1 || doc.To[0].Address != "b@example.org" ||
		len(doc.ReplyTo) != 1 || doc.Cc == nil || doc.Bcc == nil || doc.MessageID != "pk.1@example.org" || doc.Date != "2026-10-22T09:00:00Z" ||
		doc.InReplyTo != "a@example.org" || len(doc.References) != 1 ||
		len(doc.Attachments) != 1 || doc.Attachments[0] != (hookAttachment{Name: "plan.pdf", Size: 1234, ContentType: "application/pdf"}) {
		t.Fatalf("meta = %+v", doc)
	}
}

// Längere Ausgabe als 4 KB: gekürzt (an einer Zeichengrenze), Hinweis im
// Ergebnis, nichts davon im Audit.
func TestRequestReadyHookOutputTruncated(t *testing.T) {
	e := newDraftEnv(t, true)
	e.check.res = DraftCheckResult{Result: drafts.HookFail, Output: "Kanarie" + strings.Repeat("ä", 3000)}
	r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}).(DraftsRequestOutput)
	if h := r.HookOutput; r.Hook != "fail" || h == nil || !h.Truncated || len(h.Text) > 4096 || len(h.Text) < 4090 || !strings.HasPrefix(h.Text, "Kanarie") {
		t.Fatalf("hook %s, output %+v", r.Hook, r.HookOutput)
	}
	if _, all := e.auditJSON(t); strings.Contains(all, "Kanarie") || strings.Contains(all, "ä") {
		t.Fatal("hook output in the audit")
	}
	// Kurze Ausgabe: kein Hinweis.
	e.check.res = DraftCheckResult{Result: drafts.HookTimeout, Output: "slow"}
	r = e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}).(DraftsRequestOutput)
	if r.Hook != "timeout" || r.HookOutput == nil || r.HookOutput.Text != "slow" || r.HookOutput.Truncated || r.Status != "draft" {
		t.Fatalf("timeout = %+v", r)
	}
}
