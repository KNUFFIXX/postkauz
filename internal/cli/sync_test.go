package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

type litReader struct{ *bytes.Reader }

func (l litReader) Size() int64 { return int64(l.Reader.Len()) }

// mailServer startet einen In-Prozess-Server mit drei Nachrichten in INBOX
// und einer im Archiv.
func mailServer(t *testing.T, password string) (addr string) {
	t.Helper()
	u := imapmemserver.NewUser("tester@example.invalid", password)
	for _, mb := range []string{"INBOX", "Archiv", "Sent"} {
		_ = u.Create(mb, nil)
	}
	add := func(mb string, i int, flags ...imap.Flag) {
		raw := fmt.Sprintf("Message-ID: <cli%d@example.org>\r\nFrom: Absender <a@example.org>\r\nTo: Empfang <b@example.org>\r\n"+
			"Subject: Testnachricht %d\r\nDate: Mon, 06 Jan 2025 10:00:00 +0100\r\n\r\nHallo %d\r\n", i, i, i)
		if _, err := u.Append(mb, litReader{bytes.NewReader([]byte(raw))}, &imap.AppendOptions{Flags: flags}); err != nil {
			t.Fatal(err)
		}
	}
	add("INBOX", 1)
	add("INBOX", 2, imap.FlagSeen)
	add("INBOX", 3)
	add("Archiv", 4, imap.FlagSeen)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// Akzeptanz D1.3–D1.6 im Kleinen: sync, mailbox list --json, show --headers
// nach einer Flag-Änderung von außen; das Passwort (Kanarienwert) taucht
// nirgends auf.
func TestSyncMailboxListShow(t *testing.T) {
	dataDir, _ := setupEnv(t)
	addr := mailServer(t, canary)
	var all strings.Builder
	step := func(want int, args ...string) string {
		t.Helper()
		stdin := ""
		if args[0] == "account" {
			stdin = canary + "\n"
		}
		code, out, errs := runIn(t, stdin, args...)
		all.WriteString(out + errs)
		if code != want {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		return out
	}
	step(ExitOK, "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid", "--insecure-test-server", "--password-stdin")

	out := step(ExitOK, "sync", "--dry-run", "-a", "demo")
	if !strings.Contains(out, "dry run: would sync 3 mailboxes") || !strings.Contains(out, "0 messages mirrored so far") {
		t.Fatalf("dry run: %s", out)
	}
	out = step(ExitOK, "sync", "--full", "-a", "demo")
	if !strings.Contains(out, "4 messages mirrored\n0 errors\n") || !strings.Contains(out, "strategy uid-scan") {
		t.Fatalf("sync: %s", out)
	}
	out = step(ExitOK, "sync", "-a", "demo", "--json")
	var so map[string]any
	if json.Unmarshal([]byte(out), &so) != nil || so["messages_mirrored"] != float64(4) || so["new"] != float64(0) || so["mode"] != "incremental" {
		t.Fatalf("sync json: %s", out)
	}

	out = step(ExitOK, "mailbox", "list", "-a", "demo", "--json")
	var ml struct {
		Account   string
		Mailboxes []struct {
			Path     string
			Role     string
			Total    int
			Mirrored int
			Unread   int
		}
	}
	if err := json.Unmarshal([]byte(out), &ml); err != nil || ml.Account != "demo" || len(ml.Mailboxes) != 3 {
		t.Fatalf("mailbox list: %s (%v)", out, err)
	}
	if b := ml.Mailboxes[0]; b.Path != "INBOX" || b.Role != "inbox" || b.Total != 3 || b.Mirrored != 3 || b.Unread != 2 {
		t.Fatalf("INBOX = %+v", b)
	}
	out = step(ExitOK, "mailbox", "list")
	if !strings.Contains(out, "INBOX") || !strings.Contains(out, "ready") {
		t.Fatalf("mailbox list human: %s", out)
	}

	out = step(ExitOK, "show", "<cli1@example.org>", "--json")
	var msg map[string]any
	if json.Unmarshal([]byte(out), &msg) != nil || msg["subject"] != "Testnachricht 1" {
		t.Fatalf("show json: %s", out)
	}
	id := fmt.Sprint(int64(msg["id"].(float64)))
	out = step(ExitOK, "show", id, "--headers")
	if !strings.Contains(out, "flags:") || strings.Contains(out, "seen") || !strings.Contains(out, "Subject: Testnachricht 1") {
		t.Fatalf("show before: %s", out)
	}

	// Von außen als gelesen markieren (zweiter Client), dann inkrementell syncen.
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("tester@example.invalid", canary).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := c.Store(imap.UIDSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	out = step(ExitOK, "sync", "-a", "demo")
	if !strings.Contains(out, "4 messages mirrored") {
		t.Fatalf("sync after flag: %s", out)
	}
	out = step(ExitOK, "show", id, "--headers")
	if !strings.Contains(out, "flags:") || !strings.Contains(out, "seen") {
		t.Fatalf("show after flag change: %s", out)
	}
	out = step(ExitOK, "show", id)
	if !strings.Contains(out, "--- text ---\nHallo 1") {
		t.Fatalf("show text: %s", out)
	}
	step(ExitError, "show", "999999")
	step(ExitUsage, "sync", "--interval", "1m")

	if strings.Contains(all.String(), canary) {
		t.Fatal("canary in CLI output")
	}
	_ = filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if bytes.Contains(b, []byte(canary)) {
			t.Errorf("canary found in %s", p)
		}
		return nil
	})
}
