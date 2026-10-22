package cli

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// settleDrafts rückt die letzte Änderung aller Entwürfe 31 s zurück, als
// wäre die Ruhezeit nach einer Änderung (Entscheid 43) vorbei.
func settleDrafts(t *testing.T, dataDir string) {
	t.Helper()
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ExecForTest(`UPDATE drafts SET updated_at = updated_at - 31000`); err != nil {
		t.Fatal(err)
	}
}

// S2.5 über die CLI: draft new reiht ein, sync legt zuerst ab (Zeile
// "drafts: …"), draft show nennt die Kopie; nach einer Änderung legt der
// nächste sync eine neue Kopie ab, draft show nennt beide UIDs. Der Betreff
// steht weder im Log noch im Audit.
func TestSyncPushesDraftCLI(t *testing.T) {
	dataDir, _ := setupEnv(t)
	host, port := fakeIMAP(t, canary)
	const subject = "KanarienBetreff-S25"
	step := func(want int, stdin string, args ...string) string {
		t.Helper()
		code, out, errs := runIn(t, stdin, args...)
		if code != want {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		return out
	}
	step(ExitOK, canary+"\n", "account", "add", "demo", "--imap", net.JoinHostPort(host, port), "--user", "tester@example.invalid",
		"--email", "demo@example.org", "--tls", "none", "--insecure-test-server", "--password-stdin")
	out := step(ExitOK, "Hallo\n", "draft", "new", "-a", "demo", "--to", "b@example.org", "--subject", subject, "--body-file", "-")
	if !strings.Contains(out, "draft 1 created (status draft, remote pending)") || !strings.Contains(out, "within 10 s") {
		t.Fatalf("draft new: %s", out)
	}
	out = step(ExitOK, "", "sync", "-a", "demo")
	if !strings.Contains(out, "drafts: 1 put into Drafts (flags \\Draft \\Seen; nothing is sent)\n") || !strings.HasSuffix(out, "0 errors\n") {
		t.Fatalf("sync: %s", out)
	}
	out = step(ExitOK, "", "draft", "show", "1")
	if !strings.Contains(out, "remote:      pushed (in the Drafts folder (uid 1))") || !strings.Contains(out, "copies:      uid 1 (date of the copy: ") {
		t.Fatalf("draft show: %s", out)
	}
	// Zweiter Client: die Kopie mit \Draft \Seen.
	c, err := imapclient.DialInsecure(net.JoinHostPort(host, port), nil)
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
	msgs, err := c.Fetch(imap.UIDSetNum(1), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil || len(msgs) != 1 || !slices.Contains(msgs[0].Flags, imap.FlagDraft) || !slices.Contains(msgs[0].Flags, imap.FlagSeen) {
		t.Fatalf("server copy: %+v, %v", msgs, err)
	}

	out = step(ExitOK, "", "draft", "edit", "1", "--subject", subject+" 2")
	if !strings.Contains(out, "remote pending") || !strings.Contains(out, "earlier copy (uid 1) stays") {
		t.Fatalf("draft edit: %s", out)
	}
	// Entscheid 43 (S2.6): Ein geänderter Entwurf wird erst nach 30 s Ruhe
	// abgelegt; ein sync davor sagt das ehrlich und legt nichts ab.
	out = step(ExitOK, "", "sync", "-a", "demo")
	if !strings.Contains(out, "drafts: 1 changed less than 30 s ago, not put yet (a running sync --watch puts it once it has been "+
		"unchanged for 30 s; otherwise the next sync)\n") {
		t.Fatalf("sync within 30 s: %s", out)
	}
	settleDrafts(t, dataDir)
	step(ExitOK, "", "sync", "-a", "demo")
	out = step(ExitOK, "", "draft", "show", "1")
	if !strings.Contains(out, "copies:      uid 2, earlier uid 1") {
		t.Fatalf("draft show after change: %s", out)
	}
	out = step(ExitOK, "", "draft", "show", "1", "--json")
	var d struct {
		Remote             string  `json:"remote"`
		RemoteUID          int64   `json:"remote_uid"`
		RemotePreviousUIDs []int64 `json:"remote_previous_uids"`
		PushedDate         string  `json:"pushed_date"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil || d.Remote != "pushed" || d.RemoteUID != 2 ||
		!slices.Equal(d.RemotePreviousUIDs, []int64{1}) || d.PushedDate == "" {
		t.Fatalf("draft show --json: %s (%v)", out, err)
	}
	// --jobs-interval nur mit --watch.
	code, _, errs := run(t, "sync", "-a", "demo", "--jobs-interval", "5s")
	if code != ExitUsage || !strings.Contains(errs, "--jobs-interval") {
		t.Fatalf("--jobs-interval without --watch: %d %q", code, errs)
	}
	// Audit: Ablage als Systemereignis; kein Betreff im Audit und im Log.
	out = step(ExitOK, "", "audit", "--operation", "drafts.push", "--json")
	if !strings.Contains(out, `"reason":"appended"`) || strings.Contains(out, subject) {
		t.Fatalf("audit: %s", out)
	}
	logData, err := os.ReadFile(filepath.Join(dataDir, brand.Name+".log"))
	if err != nil || !strings.Contains(string(logData), "drafts pushed to the Drafts folder") || strings.Contains(string(logData), subject) {
		t.Fatalf("log (%v): %s", err, logData)
	}
}
