package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Steuersequenzen aus einer Mail dürfen in der Textausgabe von `show` nicht
// wirksam werden; die JSON-Ausgabe gibt die Daten unverändert (maskiert) weiter.
func TestShowFiltersTerminalControlSequences(t *testing.T) {
	const (
		csi = "\x1b[2J\x1b[H"                              // Bildschirm löschen
		osc = "\x1b]0;pwned\x07"                           // Fenstertitel setzen
		o8  = "\x1b]8;;https://example.invalid/\x1b\\link" // Hyperlink (OSC 8)
		c1  = "\u009b31m"                                  // C1-CSI
	)
	text := "Zeile 1\r\nZeile\t2" + csi + "\rFAKE\n"
	out := ops.MessageOutput{
		ID: 7, Account: "demo", MessageID: "x" + osc + "@example.org",
		Subject:   "Rechnung " + csi + osc + c1 + "\nflags:             seen",
		From:      "Absender" + o8 + " <a@example.org>",
		To:        json.RawMessage(`[{"name":"B\u001b[31m","address":"b@example.org"}]`),
		Flags:     []string{"seen"},
		Keywords:  []string{"tag" + osc},
		Mailboxes: []ops.MessageLocation{{Mailbox: "INBOX" + csi, UID: 1}},
		Text:      &text,
	}
	var buf bytes.Buffer
	if err := renderShow(&buf, out); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for i, r := range got {
		if r == '\n' || r == '\t' {
			continue
		}
		if r == '\r' && strings.HasPrefix(got[i:], "\r\n") {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			t.Fatalf("control character %U at %d in output:\n%q", r, i, got)
		}
	}
	if !strings.Contains(got, "Rechnung �[2J") || !strings.Contains(got, "Zeile 1\r\nZeile\t2") {
		t.Fatalf("filtered output not as expected:\n%q", got)
	}
	// Ein Zeilenumbruch im Betreff darf keine eigene „flags:"-Zeile erzeugen.
	if strings.Count(got, "\nflags:") != 1 {
		t.Fatalf("subject forged a header line:\n%s", got)
	}
	// Ein einzelnes CR (Zeile überschreiben) wird ersetzt.
	if strings.Contains(got, "\rFAKE") {
		t.Fatalf("bare CR survived:\n%q", got)
	}

	// Kopfzeilen-Ansicht: ebenso gefiltert.
	buf.Reset()
	out.Text = nil
	out.Headers = "Subject: a" + osc + "\r\nX: b\x1b[8m\r\n"
	if err := renderShow(&buf, out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(buf.String(), 0x1b) || strings.ContainsRune(buf.String(), 0x07) {
		t.Fatalf("headers not filtered: %q", buf.String())
	}

	// JSON bleibt unverändert: die Steuerzeichen kommen maskiert und
	// verlustfrei an.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsRune(b, 0x1b) {
		t.Fatalf("raw ESC in JSON: %s", b)
	}
	var back ops.MessageOutput
	if err := json.Unmarshal(b, &back); err != nil || back.Subject != out.Subject || back.Headers != out.Headers {
		t.Fatalf("JSON changed data: %v %q", err, back.Subject)
	}
}

func TestTermFilterLeavesCleanTextAlone(t *testing.T) {
	s := "Fenster schließt nicht – Reparatur nötig 🔧"
	if termLine(s) != s || termText(s+"\r\n\tx\n") != s+"\r\n\tx\n" {
		t.Fatal("clean text was changed")
	}
	if got := termLine("a\xffb"); got != "a�b" {
		t.Fatalf("invalid UTF-8: %q", got)
	}
}

func TestClassifySyncRunning(t *testing.T) {
	code, info := classify(fmt.Errorf("%w: account demo", ops.ErrSyncRunning))
	if code != ExitError || info.Code != "sync_running" || !strings.Contains(info.Message, "another sync is running") {
		t.Fatalf("classify = %d %+v", code, info)
	}
}
