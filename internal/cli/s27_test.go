package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S2.7: Ein fehlender Scope wird bei jedem Kommando gleich gemeldet, in
// derselben Form wie eine fachliche Ablehnung: "denied (missing_scope): <op>
// needs the scope <scope>, …" (vorher "ops: <op>: deny (missing_scope)",
// Befund der Teilabnahme S2.3), mit und ohne --json.
func TestMissingScopeMessageIsUniform(t *testing.T) {
	setupEnv(t)
	seedS16(t)
	if code, out, errs := run(t, "key", "create", "--name", "leser", "--role", "reader"); code != ExitOK {
		t.Fatalf("key create: %d %s %s", code, out, errs)
	}
	cases := []struct {
		args  []string
		op    string
		scope string
	}{
		{[]string{"draft", "new", "-a", "demo", "--to", "a@example.org"}, "drafts.create", "mail:draft"},
		{[]string{"draft", "edit", "1", "--subject", "x"}, "drafts.update", "mail:draft"},
		{[]string{"draft", "show", "1"}, "drafts.get", "mail:draft"},
		{[]string{"draft", "list"}, "drafts.list", "mail:draft"},
		{[]string{"draft", "request", "1"}, "drafts.request_ready", "mail:draft"},
		{[]string{"approval", "show", "1"}, "approvals.get", "mail:draft"},
		{[]string{"key", "list"}, "keys.list", "admin"},
		{[]string{"sync", "-a", "demo"}, "sync.run", "mail:sync"},
		{[]string{"audit", "export", "--to", t.TempDir()}, "audit.export", "admin"},
	}
	for _, c := range cases {
		for _, asJSON := range []bool{false, true} {
			args := append(append([]string{}, c.args...), "--key", "leser")
			if asJSON {
				args = append(args, "--json")
			}
			code, _, errs := run(t, args...)
			want := "denied (missing_scope): " + c.op + " needs the scope " + c.scope
			if code != ExitDenied || !strings.Contains(errs, want) || strings.Contains(errs, "ops: ") {
				t.Errorf("%v: exit %d, stderr %q; want exit 3 and %q", args, code, errs, want)
			}
			if asJSON && !strings.Contains(errs, `"code":"denied"`) {
				t.Errorf("%v: no JSON error: %q", args, errs)
			}
		}
	}
	// human_only behält seine Meldung (gleiche Form).
	if code, _, errs := run(t, "approve", "1", "--key", "leser"); code != ExitDenied ||
		!strings.Contains(errs, "denied (human_only): drafts.approve is for a human without a key") {
		t.Fatalf("approve --key: %d %q", code, errs)
	}
}

// S2.7 (Kleinkram aus der Gegenprüfung von S2.3): Fehlermeldungen nennen
// nie den eingegebenen Wert (Suche, account add, key create, audit, IDs,
// Pfade), weder als Text noch mit --json. Der Wert könnte Mailinhalt oder
// Personendaten sein und landete sonst in Skript-Protokollen.
func TestErrorsDoNotEchoInput(t *testing.T) {
	setupEnv(t)
	const k = "Zqkanarie7"
	cases := []struct {
		stdin string
		args  []string
	}{
		{"", []string{"search", "has:" + k}},
		{"", []string{"search", "is:" + k}},
		{"", []string{"search", "thread:" + k}},
		{"", []string{"search", "after:" + k}},
		{"", []string{"search", "x", "--json", "--fields", "id," + k}},
		{"pw\n", []string{"account", "add", "demo", "--imap", k + "/x:993", "--user", "u", "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", "imap.example.org:" + k, "--user", "u", "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", "imap.example.org:993", "--user", "u", "--tls", k, "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", "imap.example.org:993", "--user", "u", "--auth", k, "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", k + ".example.org:143", "--user", "u", "--insecure-test-server", "--password-stdin"}},
		{"", []string{"key", "create", "--name", k}},
		{"", []string{"key", "create", "--name", "x", "--role", k}},
		{"", []string{"key", "create", "--name", "x", "--role", "reader", "--scopes", k}},
		{"", []string{"key", "create", "--name", "x", "--expires", k}},
		{"", []string{"audit", "--since", k}},
		{"", []string{"draft", "show", k}},
		{"", []string{"thread", k}},
		{"", []string{"import", k, "."}},
		{"", []string{"version", "--config", "/nonexistent/" + k + ".toml"}},
	}
	for _, c := range cases {
		for _, asJSON := range []bool{false, true} {
			args := append([]string{}, c.args...)
			if asJSON {
				args = append(args, "--json")
			}
			code, out, errs := runIn(t, c.stdin, args...)
			if code != ExitUsage {
				t.Errorf("%v: exit %d, want 2 (stderr %q)", args, code, errs)
			}
			if strings.Contains(strings.ToLower(out+errs), strings.ToLower(k)) {
				t.Errorf("%v: the input value appears in the output: %q %q", args, out, errs)
			}
		}
	}
}

// s27Server: INBOX mit einer Anfrage und einer Mail mit Anhang, dazu ein
// leerer Entwürfe-Ordner (Fake-IMAP im Prozess; keine Docker-Testserver).
func s27Server(t *testing.T) string {
	t.Helper()
	u := imapmemserver.NewUser("tester@example.invalid", canary)
	for _, mb := range []string{"INBOX", "Drafts"} {
		_ = u.Create(mb, nil)
	}
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%Testanhang\n%%EOF\n"))
	return serveMails(t, u, []string{
		"Message-ID: <frage@example.org>\r\nFrom: Anna <anna@example.org>\r\nTo: <demo@example.org>, <c@example.org>\r\n" +
			"Cc: <d@example.org>\r\nSubject: Heizung defekt\r\nDate: Mon, 02 Mar 2026 09:00:00 +0100\r\n\r\nDie Heizung im Bad ist kalt.\r\n",
		"Message-ID: <anhang@example.org>\r\nFrom: Bert <b@example.org>\r\nTo: <demo@example.org>\r\nSubject: Angebot\r\n" +
			"Date: Tue, 03 Mar 2026 10:00:00 +0100\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
			"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nAnbei das Angebot.\r\n" +
			"--b1\r\nContent-Type: application/pdf; name=\"Angebot.pdf\"\r\nContent-Disposition: attachment; filename=\"Angebot.pdf\"\r\n" +
			"Content-Transfer-Encoding: base64\r\n\r\n" + pdf + "\r\n--b1--\r\n",
	})
}

// fetchDraftCopy liest die Kopie mit der UID uid aus dem Entwürfe-Ordner
// des Fake-Servers (zweiter IMAP-Client, nur lesend).
func fetchDraftCopy(t *testing.T, addr string, uid int64) []byte {
	t.Helper()
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("tester@example.invalid", canary).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("Drafts", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	sec := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch uid %d: %v (%d messages)", uid, err, len(msgs))
	}
	return msgs[0].FindBodySection(sec)
}

// A02 S2.7: draft show --eml ist Byte für Byte die Kopie, die der Push in
// den Entwürfe-Ordner gelegt hat (Antwort an alle mit Zitat und Signatur,
// Weiterleitung mit Anhang, neuer Entwurf mit Umlauten); nach einer
// Änderung sagt die Ausgabe, dass die Kopie älter ist, nach dem nächsten
// Push gilt wieder Gleichheit. stdout enthält nur die Nachricht.
func TestDraftShowEMLIsPushedCopy(t *testing.T) {
	dataDir, _ := setupEnv(t)
	addr := s27Server(t)
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	step := func(want int, args ...string) (string, string) {
		t.Helper()
		code, out, errs := run(t, args...)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out, errs
	}
	if code, out, errs := runIn(t, canary+"\n", "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid",
		"--email", "demo@example.org", "--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %d %s %s", code, out, errs)
	}
	step(ExitOK, "sync", "-a", "demo")
	step(ExitOK, "identity", "add", "-a", "demo", "--email", "demo@example.org", "--name", "Demo Büro",
		"--signature-file", write("sig.txt", "Mit freundlichen Grüßen\nDemo Büro"))
	step(ExitOK, "draft", "reply", "frage@example.org", "-a", "demo", "--all", "--body-file", write("a.md", "Guten Tag,\n\n**morgen** kommt der Monteur."))
	step(ExitOK, "draft", "forward", "anhang@example.org", "-a", "demo", "--to", "e@example.org", "--body-file", write("f.md", "Zur Info."))
	step(ExitOK, "draft", "new", "-a", "demo", "--to", "Zoë Beispiel <z@example.org>", "--subject", "Grüße aus Köln – äöüß",
		"--html-file", write("n.html", "<p>Hallo <b>Zoë</b>, Grüße!</p>"))

	// Vor dem Push: Date = Anlegen, Hinweis auf stderr.
	out, errs := step(ExitOK, "draft", "show", "3", "--eml")
	if !strings.HasPrefix(out, "Date: ") || !strings.Contains(errs, "note: eml: not in the Drafts folder yet") {
		t.Fatalf("eml before push: %q / %q", out, errs)
	}

	step(ExitOK, "sync", "-a", "demo")
	for id := 1; id <= 3; id++ {
		ids := []string{"1", "2", "3"}[id-1]
		var d ops.DraftDetail
		js, _ := step(ExitOK, "draft", "show", ids, "--eml", "--json")
		if err := json.Unmarshal([]byte(js), &d); err != nil || d.RemoteUID == 0 || !d.EMLMatchesCopy || d.EMLDate != d.PushedDate {
			t.Fatalf("draft %d --eml --json: %v %+v", id, err, d)
		}
		raw := fetchDraftCopy(t, addr, d.RemoteUID)
		out, errs := step(ExitOK, "draft", "show", ids, "--eml")
		if !bytes.Equal([]byte(out), raw) || d.EML != out {
			t.Fatalf("draft %d: --eml differs from the copy in Drafts (uid %d)\n--- eml ---\n%s\n--- copy ---\n%s", id, d.RemoteUID, out, raw)
		}
		if !strings.Contains(errs, "note: eml: byte for byte the copy (uid ") {
			t.Fatalf("draft %d: note %q", id, errs)
		}
		if !strings.Contains(strings.ToLower(out), "message-id: <"+strings.ToLower(d.MessageID)+">") {
			t.Fatalf("draft %d: no Message-ID <%s> in the eml:\n%s", id, d.MessageID, out)
		}
	}
	// Die Weiterleitung trägt den Anhang, die Antwort die Signatur.
	if out, _ := step(ExitOK, "draft", "show", "2", "--eml"); !strings.Contains(out, "Angebot.pdf") {
		t.Fatalf("forward without attachment:\n%s", out)
	}

	// Änderung: Die Kopie ist älter; nach dem nächsten Push wieder gleich.
	step(ExitOK, "draft", "edit", "1", "--subject", "Re: Heizung defekt (Termin)")
	_, errs = step(ExitOK, "draft", "show", "1", "--eml")
	if !strings.Contains(errs, "changed after the copy (uid 1) was put into the Drafts folder") {
		t.Fatalf("eml after change: %q", errs)
	}
	// Entscheid 43 (S2.6): erst nach 30 s Ruhe wird die neue Kopie abgelegt.
	settleDrafts(t, dataDir)
	step(ExitOK, "sync", "-a", "demo")
	var d ops.DraftDetail
	js, _ := step(ExitOK, "draft", "show", "1", "--eml", "--json")
	if err := json.Unmarshal([]byte(js), &d); err != nil || !d.EMLMatchesCopy || d.RemoteUID != 4 {
		t.Fatalf("after the second push: %v %+v", err, d)
	}
	if raw := fetchDraftCopy(t, addr, d.RemoteUID); d.EML != string(raw) {
		t.Fatal("second copy differs from --eml")
	}
	// Ohne --eml bleibt draft show, wie es war (kein eml in --json).
	if js, _ := step(ExitOK, "draft", "show", "1", "--json"); strings.Contains(js, `"eml"`) {
		t.Fatalf("eml without --eml: %s", js)
	}

	// Entscheid 43: Nach dem zweiten Push liegen zwei Kopien von Entwurf 1
	// im Ordner (uid 1 und 4); draft show und draft list warnen, auch in
	// --json, bis der Mensch die alte Kopie im Mailclient löscht.
	const warn = "older copies in Drafts: 1 (delete them in your mail client; postkauz never deletes)"
	out, _ = step(ExitOK, "draft", "show", "1")
	if !strings.Contains(out, "warning: "+warn) {
		t.Fatalf("draft show without the warning:\n%s", out)
	}
	if !strings.Contains(js, `"older_copies":1`) || !strings.Contains(js, `"older_copy_uids":[1]`) || !strings.Contains(js, warn) {
		t.Fatalf("draft show --json: %s", js)
	}
	out, _ = step(ExitOK, "draft", "list")
	if !strings.Contains(out, "warning: draft 1: "+warn) || strings.Contains(out, "warning: draft 2") || strings.Contains(out, "warning: draft 3") {
		t.Fatalf("draft list:\n%s", out)
	}
	if out, _ = step(ExitOK, "draft", "list", "--json"); !strings.Contains(out, `"draft_id":1,`) || strings.Count(out, `"older_copies":1`) != 1 {
		t.Fatalf("draft list --json: %s", out)
	}
	if _, errs = step(ExitOK, "draft", "show", "1", "--eml"); !strings.Contains(errs, "note: "+warn) {
		t.Fatalf("draft show --eml: %q", errs)
	}
	deleteDraftCopy(t, addr, 1)
	step(ExitOK, "sync", "-a", "demo")
	if out, _ = step(ExitOK, "draft", "show", "1"); strings.Contains(out, "older copies") {
		t.Fatalf("warning after the old copy was deleted:\n%s", out)
	}
	if out, _ = step(ExitOK, "draft", "list"); strings.Contains(out, "warning:") {
		t.Fatalf("draft list after the old copy was deleted:\n%s", out)
	}
}

// deleteDraftCopy löscht im Fake-Server die Kopie uid im Entwürfe-Ordner,
// wie es der Mensch im Mailclient täte (PostKauz selbst löscht nie).
func deleteDraftCopy(t *testing.T, addr string, uid int64) {
	t.Helper()
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("tester@example.invalid", canary).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("Drafts", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	set := imap.UIDSetNum(imap.UID(uid))
	if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.UIDExpunge(set).Close(); err != nil {
		t.Fatal(err)
	}
}

// S2.7: identity add|list|set-default|remove über die CLI (Text und
// --json), nur für den Menschen; unbekannte ID ist not_found.
func TestIdentityCommands(t *testing.T) {
	setupEnv(t)
	seedS16(t)
	step := func(want int, args ...string) (string, string) {
		t.Helper()
		code, out, errs := run(t, args...)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out, errs
	}
	out, _ := step(ExitOK, "identity", "add", "-a", "demo", "--email", "demo@example.org", "--name", "Demo Büro")
	if !strings.Contains(out, "identity 1 of account demo: Demo Büro <demo@example.org> (signature none, default)") {
		t.Fatalf("add: %s", out)
	}
	out, _ = step(ExitOK, "identity", "add", "-a", "demo", "--email", "verwaltung@example.org", "--reply-prefix", "AW: ", "--json")
	var r ops.IdentityResultOutput
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Identity.ID != 2 || r.Identity.Default {
		t.Fatalf("add --json: %v %s", err, out)
	}
	out, _ = step(ExitOK, "identity", "set-default", "2")
	if !strings.Contains(out, "identity 2 of account demo: verwaltung@example.org (signature none, default)") {
		t.Fatalf("set-default: %s", out)
	}
	out, _ = step(ExitOK, "identity", "list", "-a", "demo")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[2], "2 ") || !strings.Contains(lines[2], "yes") || strings.Contains(lines[1], "yes") ||
		!strings.Contains(lines[2], `"AW: "`) {
		t.Fatalf("list:\n%s", out)
	}
	if _, errs := step(ExitError, "identity", "remove", "1", "--key", "unbekannt-s27"); strings.Contains(errs, "unbekannt-s27") ||
		!strings.Contains(errs, "api key not found") {
		t.Fatalf("unknown key: %s", errs)
	}
	step(ExitOK, "key", "create", "--name", "verwalter", "--role", "admin")
	var errs string
	if _, errs = step(ExitDenied, "identity", "remove", "1", "--key", "verwalter"); !strings.Contains(errs, "denied (human_only)") {
		t.Fatalf("remove --key: %s", errs)
	}
	out, _ = step(ExitOK, "identity", "remove", "1", "--dry-run")
	if !strings.HasPrefix(out, "dry run: identity 1") {
		t.Fatalf("remove --dry-run: %s", out)
	}
	step(ExitOK, "identity", "remove", "1")
	out, _ = step(ExitOK, "identity", "list", "--json")
	var l ops.IdentitiesListOutput
	if err := json.Unmarshal([]byte(out), &l); err != nil || len(l.Identities) != 1 || l.Identities[0].ID != 2 {
		t.Fatalf("list --json after remove: %v %s", err, out)
	}
	if _, errs = step(ExitError, "identity", "remove", "1", "--json"); !strings.Contains(errs, `"code":"not_found"`) {
		t.Fatalf("remove twice: %s", errs)
	}
}

// S2.7: draft edit kennt die Optionen von draft new|reply (--no-quote,
// --no-signature) zusätzlich zu --quote=false/--signature=false;
// widersprüchliche Angaben sind Eingabefehler.
func TestDraftEditNoQuoteNoSignature(t *testing.T) {
	setupEnv(t)
	seedS16(t)
	dir := t.TempDir()
	sig := filepath.Join(dir, "sig.txt")
	if err := os.WriteFile(sig, []byte("Signaturzeile-S27"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "identity", "add", "-a", "demo", "--email", "demo@example.org", "--signature-file", sig); code != ExitOK {
		t.Fatalf("identity add: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, "draft", "reply", "frage@example.org", "-a", "demo"); code != ExitOK {
		t.Fatalf("reply: %d %s %s", code, out, errs)
	}
	show := func() ops.DraftDetail {
		t.Helper()
		_, out, _ := run(t, "draft", "show", "1", "--json")
		var d ops.DraftDetail
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			t.Fatalf("show: %v %s", err, out)
		}
		return d
	}
	if d := show(); !d.Quote || !d.Signature || !strings.Contains(d.BodyText, "Signaturzeile-S27") || !strings.Contains(d.BodyText, "> Die Heizung") {
		t.Fatalf("before: %+v", d)
	}
	if code, _, errs := run(t, "draft", "edit", "1", "--no-quote", "--quote=true"); code != ExitUsage || !strings.Contains(errs, "--quote or --no-quote") {
		t.Fatalf("contradiction: %d %s", code, errs)
	}
	if code, out, errs := run(t, "draft", "edit", "1", "--no-quote", "--no-signature"); code != ExitOK || !strings.Contains(out, "draft 1 changed") {
		t.Fatalf("edit: %d %s %s", code, out, errs)
	}
	if d := show(); d.Quote || d.Signature || strings.Contains(d.BodyText, "Signaturzeile-S27") || strings.Contains(d.BodyText, "> Die Heizung") {
		t.Fatalf("after: %+v", d)
	}
	if code, out, errs := run(t, "draft", "edit", "1", "--signature=true"); code != ExitOK || !strings.Contains(out, "draft 1 changed") {
		t.Fatalf("signature back: %d %s %s", code, out, errs)
	}
	if d := show(); !d.Signature || d.Quote {
		t.Fatalf("signature back: %+v", d)
	}
}
