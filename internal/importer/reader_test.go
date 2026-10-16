package importer

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
)

func readAll(t *testing.T, mbox string) []string {
	t.Helper()
	sc := newMboxScanner(strings.NewReader(mbox), 0)
	var out []string
	for {
		m, err := sc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(m.Body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(m.Header)+string(b))
	}
	if sc.Pos() != int64(len(mbox)) {
		t.Fatalf("position %d after the last message, want %d", sc.Pos(), len(mbox))
	}
	return out
}

func TestMboxFramingQuotingAndTrailingLines(t *testing.T) {
	mbox := "From - Thu Oct 15 10:00:00 2026\n" +
		"X-Mozilla-Status: 0005\nX-Mozilla-Status2: 00000000\nX-Mozilla-Keys: $label1 todo\n" +
		"Subject: eins\nMessage-ID: <a@example.org>\n\n" +
		">From the desk: Text\n>>From zwei\nFrom the desk of nobody (kein Datum, also keine Trennzeile)\n\n\n" +
		"From - Thu Oct 15 10:01:00 2026\n" +
		"Subject: zwei\n\nletzte Zeile ohne Ende"
	got := readAll(t, mbox)
	want := []string{
		"Subject: eins\nMessage-ID: <a@example.org>\n\nFrom the desk: Text\n>>From zwei\nFrom the desk of nobody (kein Datum, also keine Trennzeile)\n",
		"Subject: zwei\n\nletzte Zeile ohne Ende\n",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("messages =\n%q\nwant\n%q", got, want)
	}
	// mboxrd (keine Thunderbird-Datei): eine Ebene Quoting weniger.
	got = readAll(t, "From someone@example.org Thu Oct 15 10:00:00 2026\r\nSubject: x\r\n\r\n>>From a\r\n>From b\r\n\r\n")
	if want := "Subject: x\r\n\r\n>From a\r\nFrom b\r\n"; len(got) != 1 || got[0] != want {
		t.Fatalf("mboxrd = %q, want %q", got, want)
	}
}

func TestMboxThunderbirdStatus(t *testing.T) {
	mbox := "From - Thu Oct 15 10:00:00 2026\r\nX-Mozilla-Status: 000f\r\nX-Mozilla-Status2: 00010000\r\nX-Mozilla-Keys: $label1\r\n" +
		"  todo\r\nSubject: s\r\nX-Other: bleibt\r\n\r\nText\r\n\r\n"
	sc := newMboxScanner(strings.NewReader(mbox), 0)
	m, err := sc.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Thunderbird || !m.Status.Present || m.Status.Flags != 0xf || m.Status.Flags2 != 0x10000 || !m.Status.deleted() ||
		!slices.Equal(m.Status.Keys, []string{"$label1", "todo"}) {
		t.Fatalf("status = %+v", m.Status)
	}
	if string(m.Header) != "Subject: s\r\nX-Other: bleibt\r\n\r\n" {
		t.Fatalf("header = %q", m.Header)
	}
	f := mozFlags("INBOX", m.Status)
	if !f.Seen || !f.Answered || !f.Flagged || f.Draft {
		t.Fatalf("flags = %+v", f)
	}
	if m.Date.IsZero() || m.Date.Year() != 2026 {
		t.Fatalf("date = %v", m.Date)
	}
}

func TestMboxNotMboxAndLongLines(t *testing.T) {
	if _, err := newMboxScanner(strings.NewReader("Subject: x\n\nkein mbox\n"), 0).Next(); err != ErrNotMbox {
		t.Fatalf("err = %v", err)
	}
	long := strings.Repeat("y", 3*readBufSize+17)
	got := readAll(t, "From - Thu Oct 15 10:00:00 2026\nSubject: lang\n\n"+long+"\nFrom x\n\n\nFrom - Thu Oct 15 10:00:00 2026\nSubject: b\n\nb\n")
	if len(got) != 2 || got[0] != "Subject: lang\n\n"+long+"\nFrom x\n" || got[1] != "Subject: b\n\nb\n" {
		t.Fatalf("long line: %d messages, first %d bytes", len(got), len(got[0]))
	}
}

func TestDecodeFolderName(t *testing.T) {
	for in, want := range map[string]string{
		"Entw&APw-rfe":          "Entwürfe",
		"Gel&APY-schte Objekte": "Gelöschte Objekte",
		"INBOX":                 "INBOX",
		"A&-B":                  "A&B",
		"kaputt&xyz":            "kaputt&xyz",
		"&AGE-":                 "&AGE-", // ASCII in Base64: kein gültiges IMAP-UTF-7
	} {
		if got := DecodeFolderName(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
	if got := folderPath("", "[Gmail].sbd/Alle Nachrichten"); got != "[Gmail]/Alle Nachrichten" {
		t.Errorf("sbd = %q", got)
	}
	if got := folderPath("Archiv", ".Projekte.Entw&APw-rfe"); got != "Archiv/Projekte/Entwürfe" {
		t.Errorf("maildir++ = %q", got)
	}
}

func TestMaildirFlags(t *testing.T) {
	for name, want := range map[string]string{"1.M1P1.host:2,FRS": "FRS", "1.M1P1.host!2,S": "S", "1.M1P1.host": "", "1.x;2,T": "T"} {
		if got := maildirFlags(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestStripMozillaLeavesOtherHeaders(t *testing.T) {
	in := []byte("Received: a\r\n b\r\nX-Mozilla-Status: 0001\r\nSubject: s\r\n\r\n")
	out, st := stripMozilla(in)
	if !bytes.Equal(out, []byte("Received: a\r\n b\r\nSubject: s\r\n\r\n")) || st.Flags != 1 {
		t.Fatalf("out = %q, st = %+v", out, st)
	}
	same := []byte("Subject: s\r\n\r\n")
	if out, _ := stripMozilla(same); !bytes.Equal(out, same) {
		t.Fatal("header without Thunderbird lines changed")
	}
}
