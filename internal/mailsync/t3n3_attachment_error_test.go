package mailsync

// Nachbesserung 3, Punkt 4 (Wegwerf-Test mailsync__zz_c7d_test.go.txt der
// Prüfsitzung): Ein dauerhafter Fehler beim Ablegen eines Anhangs (hier:
// eine Datei am Platz des Blob-Verzeichnisses; unter Windows vermutlich
// auch ein Virenscanner, der das Schreiben eines Schadanhangs verweigert)
// verhinderte seit der zweiten Nachbesserung das Spiegeln der ganzen Mail
// in jedem Lauf. Jetzt verschieben nur blob.ErrBusy und ein beendeter
// Kontext die Nachricht; sonst bleibt der Anhang "not stored", und die Mail
// wird gespiegelt, auch bei Import und Neuaufbereitung.

import (
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// notStored prüft, dass atts genau einen Anhang ohne Blob mit dem Grund
// "not stored" enthält.
func notStored(t *testing.T, what string, atts []store.NewAttachment) {
	t.Helper()
	if len(atts) != 1 || atts[0].SHA256 != "" || atts[0].ExtractError != "not stored" {
		t.Errorf("%s: attachments %+v, want one that is not stored", what, atts)
	}
}

func TestT3N3PermanentAttachmentErrorEveryRun(t *testing.T) {
	root := t.TempDir()
	bs, err := blob.Open(root, blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	blockAttachment(t, root, t3n2Raw)
	e := &Engine{Blobs: bs}
	for run := 1; run <= 3; run++ {
		nm, _, err := e.mirrorRaw(strings.NewReader(t3n2Raw), provider.Header{UID: 7, Size: int64(len(t3n2Raw))})
		if err != nil {
			t.Errorf("run %d: message not mirrored: %v", run, err)
			continue
		}
		if nm.BlobSHA256 == "" {
			t.Errorf("run %d: no raw blob", run)
		}
		notStored(t, "mirrorRaw", nm.Attachments)
	}

	// Import (Prepare) und Neuaufbereitung (reprocessOne) entscheiden
	// gleich: gefüllt bzw. gespeichert, der Anhang "not stored".
	var nm store.NewMessage
	if ok, err := Prepare(bs, &nm, strings.NewReader(t3n2Raw), time.Now()); !ok || err != nil {
		t.Fatalf("Prepare with a permanent attachment error = %v %v, want true nil", ok, err)
	}
	notStored(t, "Prepare", nm.Attachments)
	info, err := bs.Put(strings.NewReader(t3n2Raw))
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.reprocessOne(store.ReprocessItem{ID: 1, BlobSHA256: info.SHA256})
	if err != nil {
		t.Fatalf("reprocessOne with a permanent attachment error: %v (want the message saved)", err)
	}
	if r.KeepBody || !r.Headers {
		t.Fatalf("reprocessOne = %+v, want the new body", r)
	}
	notStored(t, "reprocessOne", r.Body.Attachments)
}
