package mailsync

// Nachbesserung 2, Punkt 8: Scheitert das Ablegen eines Anhangs nur
// vorübergehend (Sperre bis zur Frist belegt, blob.ErrBusy), wird die
// Nachricht nicht gespeichert (wie bei einem Fehler der Rohmail in
// mirrorRaw; der nächste Lauf spiegelt sie erneut), statt den Anhang
// dauerhaft als "not stored" zu führen. Ein Anhang über der Größengrenze,
// mit unlesbarem Inhalt oder mit einem dauerhaften Fehler des Stores
// bleibt "not stored" (Nachbesserung 3, Punkt 4:
// t3n3_attachment_error_test.go).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Die Sperre bleibt bis zur Frist belegt (blobs.gc hält sie exklusiv):
// ErrBusy verschiebt die Nachricht.
func TestT3N2AnalyzeBusyLockFailsMessage(t *testing.T) {
	root := t.TempDir()
	bs, err := blob.Open(root, blob.Options{LockWait: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := bs.LockForGC(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	e := &Engine{Blobs: bs}
	_, atts, err := e.analyze(strings.NewReader(t3n2Raw), time.Now())
	if !errors.Is(err, blob.ErrBusy) {
		t.Fatalf("analyze with a busy lock = %v (attachments %+v), want ErrBusy", err, atts)
	}
	var se *attachmentStoreError
	if !errors.As(err, &se) || se.PartID == "" {
		t.Fatalf("analyze error %v is not an attachmentStoreError", err)
	}

	// Import (Prepare): derselbe Fehler, nichts gefüllt.
	var nm store.NewMessage
	if ok, err := Prepare(bs, &nm, strings.NewReader(t3n2Raw), time.Now()); ok || !errors.Is(err, blob.ErrBusy) {
		t.Fatalf("Prepare with a busy lock = %v %v, want false and ErrBusy", ok, err)
	}
}

// attachmentSHA liefert den Hash des (einzigen) Anhangs von raw.
func attachmentSHA(t *testing.T, raw string) string {
	t.Helper()
	bs, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, atts, err := (&Engine{Blobs: bs}).analyze(strings.NewReader(raw), time.Now())
	if err != nil || len(atts) != 1 || atts[0].SHA256 == "" {
		t.Fatalf("attachment of the test mail: %+v %v", atts, err)
	}
	return atts[0].SHA256
}

// blockAttachment lässt das Ablegen des Anhangs von raw scheitern, ohne die
// Rohmail zu treffen: Am Platz des Verzeichnisses <root>/<ab> liegt eine
// Datei (MkdirAll scheitert, ein Fehler des Stores).
func blockAttachment(t *testing.T, root, raw string) {
	t.Helper()
	sum := attachmentSHA(t, raw)
	bs, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := bs.Put(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if info.SHA256[:2] == sum[:2] {
		t.Skip("raw mail and attachment share the first directory level; change the test mail")
	}
	if err := os.WriteFile(filepath.Join(root, sum[:2]), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// busyAttachments lässt das Ablegen jedes Anhangs mit ErrBusy scheitern
// (die Rohmail legt mirrorRaw unabhängig davon ab).
func busyAttachments(t *testing.T) {
	t.Helper()
	orig := putAttachment
	t.Cleanup(func() { putAttachment = orig })
	putAttachment = func(_ *blob.Store, r io.Reader) (blob.Info, error) {
		_, _ = io.Copy(io.Discard, r)
		return blob.Info{}, fmt.Errorf("%w (test)", blob.ErrBusy)
	}
}

func TestT3N2MirrorRawBusyAttachmentNotMirrored(t *testing.T) {
	bs, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	busyAttachments(t)
	e := &Engine{Blobs: bs}
	nm, variants, err := e.mirrorRaw(strings.NewReader(t3n2Raw), provider.Header{UID: 7, Size: int64(len(t3n2Raw))})
	var se *attachmentStoreError
	if !errors.As(err, &se) || !errors.Is(err, blob.ErrBusy) {
		t.Fatalf("mirrorRaw with a busy attachment store = %v (message %+v), want an attachmentStoreError with ErrBusy", err, nm)
	}
	if nm.BlobSHA256 != "" || variants != nil {
		t.Fatalf("mirrorRaw returned a message to save: %+v %v", nm, variants)
	}
}

// Ein unlesbarer Anhang (kaputtes Base64) ist ein Fehler des Inhalts, nicht
// des Stores: Er bleibt "not stored", die Nachricht wird gespiegelt (sonst
// scheiterte sie in jedem Lauf erneut).
func TestT3N2AnalyzeBadContentStaysNotStored(t *testing.T) {
	raw := "From: a@example.org\r\nTo: b@example.org\r\nSubject: y\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nHallo\r\n" +
		"--B\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=r.pdf\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQg!!!!####\r\n--B--\r\n"
	bs, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, atts, err := (&Engine{Blobs: bs}).analyze(strings.NewReader(raw), time.Now())
	if err != nil {
		t.Fatalf("analyze with an unreadable attachment: %v (want the message mirrored)", err)
	}
	if len(atts) != 1 || atts[0].SHA256 != "" || atts[0].ExtractError != "not stored" {
		t.Fatalf("unreadable attachment: %+v, want not stored", atts)
	}
}

// reprocessStore liefert zwei Nachrichten zur Neuaufbereitung und merkt
// sich, was gespeichert wird.
type reprocessStore struct {
	nopStore
	items []store.ReprocessItem
	calls int
	saved []store.Reprocessed
}

func (s *reprocessStore) ReprocessCandidates(context.Context, int64, int, int) ([]store.ReprocessItem, error) {
	s.calls++
	if s.calls > 3 {
		return nil, errors.New("reprocess keeps asking for the same messages")
	}
	return s.items, nil
}

func (s *reprocessStore) SaveReprocessed(_ context.Context, items []store.Reprocessed) error {
	s.saved = append(s.saved, items...)
	return nil
}

// Neuaufbereitung: Die Nachricht, deren Anhang an der belegten Sperre
// scheitert, bleibt auf dem alten Stand (kein KeepBody, der sie als
// erledigt markierte), die andere (ohne Anhang) wird gespeichert, und die
// Runde endet ohne Fehler und ohne Endlosschleife.
func TestT3N2ReprocessDefersBusyAttachment(t *testing.T) {
	bs, err := blob.Open(t.TempDir(), blob.Options{LockWait: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	other := "From: a@example.org\r\nTo: b@example.org\r\nSubject: other\r\n\r\nHallo ohne Anhang\r\n"
	var items []store.ReprocessItem
	for i, raw := range []string{t3n2Raw, other} {
		info, err := bs.Put(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, store.ReprocessItem{ID: int64(i + 1), BlobSHA256: info.SHA256})
	}
	lock, err := bs.LockForGC(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	st := &reprocessStore{items: items}
	e := &Engine{Blobs: bs, Store: st}
	n, err := e.reprocess(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("reprocess = %d %v, want 1 nil", n, err)
	}
	if st.calls != 1 {
		t.Fatalf("ReprocessCandidates called %d times, want 1 (cleanup ends for this run)", st.calls)
	}
	if len(st.saved) != 1 || st.saved[0].MessageID != 2 || st.saved[0].KeepBody {
		t.Fatalf("saved %+v, want only message 2 with its body", st.saved)
	}
}
