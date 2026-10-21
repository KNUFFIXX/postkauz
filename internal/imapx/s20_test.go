package imapx

// Tests zu A02, Teil A (S2.0): Grenze für Rohmails in FetchRaw (A4).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// A02 A4 (Frage 6d), Server nennt die UID vor dem Inhalt (gestreamt): Eine
// Nachricht über MaxMessageBytes wird nicht gelesen, der Rückruf bekommt
// ErrMessageTooLarge; die übrigen kommen vollständig, die Verbindung bleibt
// nutzbar.
func TestFetchRawLimitsMessageSize(t *testing.T) {
	addr, u := memServer(t)
	big := append([]byte("Message-ID: <big@example.org>\r\nSubject: gross\r\n\r\n"), bytes.Repeat([]byte("x"), 4096)...)
	_, _ = u.Append("INBOX", literal(testMail(1)), &imap.AppendOptions{})
	_, _ = u.Append("INBOX", literal(big), &imap.AppendOptions{})
	_, _ = u.Append("INBOX", literal(testMail(3)), &imap.AppendOptions{})
	cfg := cfgFor(t, addr)
	cfg.MaxMessageBytes = 1024
	c, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login(testUser, testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select("INBOX", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	got, errs := fetchAll(t, c, []uint32{1, 2, 3})
	if got[1] != string(testMail(1)) || got[3] != string(testMail(3)) || errs[1] != nil || errs[3] != nil {
		t.Fatalf("small messages: %v %v", got, errs)
	}
	if !errors.Is(errs[2], ErrMessageTooLarge) || got[2] != "" || !strings.Contains(errs[2].Error(), "limit 1024") {
		t.Fatalf("big message: %q %v", got[2], errs[2])
	}
	if err := c.Noop(); err != nil {
		t.Fatalf("connection after the oversized message: %v", err)
	}
}

// fetchAll liest jede Nachricht ganz und merkt sich Inhalt bzw. Fehler je
// UID; Fehler einer Nachricht brechen nicht ab (wie der Sync).
func fetchAll(t *testing.T, c *Client, uids []uint32) (map[uint32]string, map[uint32]error) {
	t.Helper()
	got, errs := map[uint32]string{}, map[uint32]error{}
	if err := c.FetchRaw(uids, func(uid uint32, r io.Reader) error {
		b, err := io.ReadAll(r)
		if err != nil {
			errs[uid] = err
			return nil
		}
		got[uid] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return got, errs
}

// fakeLiteral ist ein Literal mit eigener (ggf. falscher) Größenangabe.
type fakeLiteral struct {
	io.Reader
	size int64
}

func (f fakeLiteral) Size() int64 { return f.size }

// A02 A4 (Frage 6d), Server nennt die UID erst nach dem Inhalt: FetchRaw
// puffert dann (vorher io.ReadAll ohne Grenze), jetzt höchstens
// MaxMessageBytes. Zu groß (angekündigt oder tatsächlich) ist ein Fehler
// dieser Nachricht, kein Fehler des Abrufs.
func TestFetchRawBufferLimit(t *testing.T) {
	c := &Client{maxMsg: 2048}
	small := testMail(2)
	r, err := c.buffer(fakeLiteral{bytes.NewReader(small), int64(len(small))})
	if err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(r); err != nil || string(b) != string(small) {
		t.Fatalf("small: %q %v", b, err)
	}
	big := bytes.Repeat([]byte("y"), 8192)
	for name, lit := range map[string]fakeLiteral{"announced": {bytes.NewReader(big), int64(len(big))},
		"understated": {bytes.NewReader(big), 100}} {
		r, err := c.buffer(lit)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := io.ReadAll(r); !errors.Is(err, ErrMessageTooLarge) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Ein Lesefehler des Servers bleibt ein Fehler des Abrufs.
	broken := errors.New("connection reset")
	if _, err := c.buffer(fakeLiteral{errReader{broken}, 10}); !errors.Is(err, broken) {
		t.Fatalf("read error: %v", err)
	}
}

// cappedReader: Ein Server kann weniger ankündigen, als er schickt; mehr als
// die Grenze ist auch dann ein Fehler, nicht still gekürzt.
func TestCappedReader(t *testing.T) {
	c := &Client{maxMsg: 5}
	if b, err := io.ReadAll(c.limited(strings.NewReader("12345"), 3)); err != nil || string(b) != "12345" {
		t.Fatalf("at the limit: %q %v", b, err)
	}
	if _, err := io.ReadAll(c.limited(strings.NewReader("123456"), 3)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := io.ReadAll(c.limited(strings.NewReader(""), 6)); !errors.Is(err, ErrMessageTooLarge) || !strings.Contains(err.Error(), "6 bytes") {
		t.Fatalf("announced too large: %v", err)
	}
}
