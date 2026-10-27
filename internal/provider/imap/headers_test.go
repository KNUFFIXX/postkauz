package imap

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// K3: Passen die Köpfe nicht in das Budget eines FETCH (16 MiB), bricht
// imapx den FETCH ab; FetchHeaders holt den Rest auf neuer Verbindung und
// liefert alle Köpfe vollständig, nach UID sortiert.
func TestFetchHeadersOverBudget(t *testing.T) {
	addr, u := memServerFor(t, "INBOX")
	const n = 40
	pad := strings.Repeat("x", 900) // 600 Zeilen zu 900 Bytes: rund 540 KB je Kopf
	for i := range n {
		var b bytes.Buffer
		fmt.Fprintf(&b, "Message-Id: <big.%d@example.org>\r\nFrom: a@example.org\r\nSubject: Kopf %d\r\n", i, i)
		for range 600 {
			fmt.Fprintf(&b, "X-Pad: %s\r\n", pad)
		}
		b.WriteString("\r\nText\r\n")
		if _, err := u.Append("INBOX", rawLit{bytes.NewReader(b.Bytes())}, &goimap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Connect(context.Background(), Config{Host: "127.0.0.1", Port: portOf(addr), TLS: "none",
		Username: "tester@example.invalid", InsecureTestServer: true, CommandTimeout: 10 * time.Second}, "pw-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	cs, err := p.Changes(context.Background(), "INBOX", provider.SyncState{}, provider.ChangesOptions{Limit: 100})
	if err != nil || len(cs.Created) != n {
		t.Fatalf("changes = %d, %v", len(cs.Created), err)
	}
	hs, err := p.FetchHeaders(context.Background(), "INBOX", cs.NewState.UIDValidity, cs.Created)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != n {
		t.Fatalf("headers = %d, want %d", len(hs), n)
	}
	for i, h := range hs {
		if h.UID != cs.Created[i] || h.HeaderTooLarge || len(h.RawHeader) < 500<<10 ||
			h.MessageID != fmt.Sprintf("big.%d@example.org", i) {
			t.Fatalf("header %d: uid %d, len %d, mid %q", i, h.UID, len(h.RawHeader), h.MessageID)
		}
	}
}
