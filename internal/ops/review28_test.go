package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Gegenprüfung S2.8: Nachbesserungen an den Entwurfsausgaben.

// K1: Ist der Zeiger auf die Ausgangsnachricht leer (Spalte mit ON DELETE
// SET NULL), bleibt das gespeicherte Zitat trotzdem in der Hülle und läuft
// nicht als eigener Text mit source "draft".
func TestReviewK1QuoteStaysWrappedWithoutOriginalPointer(t *testing.T) {
	e := newDraftEnv(t, false)
	const own = "EigenerText-K1"
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": 1, "body_markdown": own}).(DraftsWriteOutput)
	if err := e.st.ExecForTest(`UPDATE drafts SET in_reply_to_msg = NULL WHERE id = ?`, out.DraftID); err != nil {
		t.Fatal(err)
	}
	d := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if d.OriginalID != 0 || strings.Contains(d.BodyText, "> ") || d.Original == nil || d.Original.Part != "quote" ||
		d.Original.Source != "email" || !strings.Contains(d.Original.Text, "\n> ") {
		t.Fatalf("without pointer: body_text %q, original %+v", d.BodyText, d.Original)
	}
}

// K2: Die Stildatei muss eine gewöhnliche Datei sein. Ein Symlink wird
// weder beim Anlegen der Identität noch beim Lesen angenommen; der Fehler
// nennt den Grund, nie den Pfad.
func TestReviewK2StyleFileSymlinkRefused(t *testing.T) {
	e := newDraftEnv(t, false)
	dir := t.TempDir()
	target := filepath.Join(dir, "stil.md")
	if err := os.WriteFile(target, []byte("Kurz und freundlich.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "verweis.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := e.st.ExecForTest(`UPDATE identities SET style_file = ? WHERE id = 1`, link); err != nil {
		t.Fatal(err)
	}
	_, err := e.do(t, assistant, "drafts.style", map[string]any{"identity": "1"}, false)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") || strings.Contains(err.Error(), dir) {
		t.Fatalf("symlink: %v", err)
	}
	_, err = e.do(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "stil@example.org", "style_file": link}, false)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("identity add with a symlink: %v", err)
	}
	// Die Datei selbst geht.
	if err := e.st.ExecForTest(`UPDATE identities SET style_file = ? WHERE id = 1`, target); err != nil {
		t.Fatal(err)
	}
	if out := e.must(t, assistant, "drafts.style", map[string]any{"identity": "1"}).(DraftsStyleOutput); out.Text != "Kurz und freundlich.\n" {
		t.Fatalf("regular file: %+v", out)
	}
}

// W2: drafts.get prüft das Konto selbst (nicht der MCP-Adapter): Ein
// Entwurf eines anderen Kontos ist „nicht gefunden", und das Audit nennt
// das angefragte Konto als Ziel.
func TestReviewW2DraftsGetChecksAccount(t *testing.T) {
	e := newDraftEnv(t, false)
	out := e.must(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "to": []string{"a@example.org"}}).(DraftsWriteOutput)
	if d := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID, "account": "demo"}).(DraftDetail); d.DraftID != out.DraftID {
		t.Fatalf("own account: %+v", d)
	}
	_, err := e.do(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID, "account": "fremd"}, false)
	if !errors.Is(err, store.ErrDraftNotFound) {
		t.Fatalf("other account: %v", err)
	}
	evs, _ := e.auditJSON(t)
	last := evs[len(evs)-1]
	if last.Operation != "drafts.get" || last.Result == "ok" || !slices.Contains(last.Targets, "account:fremd") ||
		!slices.Contains(last.Targets, "draft:"+strconv.FormatInt(out.DraftID, 10)) {
		t.Fatalf("audit: %+v", last)
	}
}
