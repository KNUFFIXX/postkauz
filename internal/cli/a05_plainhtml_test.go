package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// A05 Teil 3: Ein Klartextteil mit HTML-Quelltext. show zeigt den Text,
// den der Mensch sieht (ohne Tags), text_source ist html, und
// alternative_mismatch steht nicht auf true; thread ebenso.
func TestShowPlainPartWithHTML(t *testing.T) {
	setupEnv(t)
	u := imapmemserver.NewUser("tester@example.invalid", canary)
	_ = u.Create("INBOX", nil)
	const src = `<html xmlns="http://www.w3.org/1999/xhtml"><head><meta charset="utf-8"></head><body>` +
		`<p>Die Ablesung ist am <b>Dienstag</b>.</p></body></html>`
	raw := "Message-ID: <plainhtml@example.org>\r\nFrom: Messdienst <info@messdienst.example.net>\r\nTo: <b@example.org>\r\n" +
		"Subject: Ablesung\r\nDate: Wed, 04 Nov 2026 08:00:00 +0100\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + src + "\r\n" +
		"--b1\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + src + "\r\n--b1--\r\n"
	addr := serveMails(t, u, []string{raw})
	run := func(stdin string, args ...string) string {
		t.Helper()
		code, out, errs := runIn(t, stdin, args...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		return out + errs
	}
	run(canary+"\n", "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-stdin")
	run("", "sync", "-a", "demo")
	var js map[string]any
	if err := json.Unmarshal([]byte(run("", "show", "plainhtml@example.org", "--json")), &js); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprint(js["text"])
	if js["alternative_mismatch"] == true || js["text_source"] != "html" || !strings.Contains(text, "Dienstag") ||
		strings.Contains(text, "<") {
		t.Fatalf("show --json: source %v, mismatch %v, text %q", js["text_source"], js["alternative_mismatch"], text)
	}
	id := fmt.Sprint(js["id"])
	for _, args := range [][]string{{"show", id}, {"thread", id, "--message", "--strip-quotes"}} {
		out := run("", args...)
		if strings.Contains(out, "<html") || strings.Contains(out, "<p>") || strings.Contains(out, "differs from the HTML part") ||
			!strings.Contains(out, "Dienstag") {
			t.Fatalf("%v:\n%s", args, out)
		}
	}
}
