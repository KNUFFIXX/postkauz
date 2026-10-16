package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// multipart/alternative mit einem Klartext, der etwas anderes sagt als das
// HTML: show, thread und --json melden alternative_mismatch; eine Mail mit
// gleichem Inhalt in anderer Formatierung nicht.
func TestShowReportsAlternativeMismatch(t *testing.T) {
	setupEnv(t)
	u := imapmemserver.NewUser("tester@example.invalid", canary)
	_ = u.Create("INBOX", nil)
	alt := func(id, plain, html string) string {
		return "Message-ID: <" + id + "@example.org>\r\nFrom: Portal <noreply@portal.example.net>\r\nTo: <b@example.org>\r\n" +
			"Subject: Rechnung " + id + "\r\nDate: Wed, 04 Mar 2026 08:00:00 +0100\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: multipart/alternative; boundary=\"b1\"\r\n\r\n" +
			"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + plain + "\r\n" +
			"--b1\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html + "\r\n--b1--\r\n"
	}
	const html = "<p>Ihre Rechnung über <b>120,00 EUR</b> ist da. Zahlbar bis 31.03.2026.</p>"
	addr := serveMails(t, u, []string{
		alt("gleich", "Ihre Rechnung über *120,00 EUR* ist da.\r\nZahlbar bis 31.03.2026.", html),
		alt("anders", "Ihre Rechnung über 120,00 EUR ist da. Zahlbar bis 31.03.2026.\r\n"+injectionText, html),
	})
	run := func(args ...string) string {
		t.Helper()
		stdin := ""
		if args[0] == "account" {
			stdin = canary + "\n"
		}
		code, out, errs := runIn(t, stdin, args...)
		if code != ExitOK {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		return out + errs
	}
	run("account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-stdin")
	run("sync", "-a", "demo")
	for _, c := range []struct {
		id   string
		want bool
	}{{"gleich", false}, {"anders", true}} {
		var js map[string]any
		if err := json.Unmarshal([]byte(run("show", c.id+"@example.org", "--json")), &js); err != nil {
			t.Fatal(err)
		}
		if js["alternative_mismatch"] != c.want {
			t.Fatalf("%s: alternative_mismatch = %v", c.id, js["alternative_mismatch"])
		}
		id := fmt.Sprint(js["id"])
		out := run("show", id)
		if strings.Contains(out, "plain text differs from the HTML part") != c.want {
			t.Fatalf("%s: show:\n%s", c.id, out)
		}
		out = run("thread", id, "--message", "--strip-quotes")
		if strings.Contains(out, "(plain text differs from the HTML part)") != c.want {
			t.Fatalf("%s: thread:\n%s", c.id, out)
		}
	}
}
