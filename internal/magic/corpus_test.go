package magic

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
)

// Akzeptanz S1.6 (Magic Bytes): Die Typ-Widersprüche des Korpus werden
// erkannt (Programm als PDF, PNG als JPEG), alle übrigen Anhänge des
// Korpus (PDF, DOCX, XLSX, Bilder, ZIP, Zip-Bombe, TXT, weitergeleitete
// Nachrichten, ohne Namen) gelten nicht als Widerspruch.
func TestCorpusAttachmentTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("full corpus (1,000 messages); skipped with -short")
	}
	c, err := corpus.Generate(corpus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen, mismatches := 0, 0
	for i, raw := range c.Raw {
		e := c.Manifest.Messages[i]
		expect := map[string]corpus.ExpectAttachment{}
		for _, x := range e.AttachmentTypes {
			expect[x.Name] = x
		}
		internal, _ := time.Parse(time.RFC3339, e.InternalDate)
		_, err := mimeparse.Parse(bytes.NewReader(raw), mimeparse.Options{InternalDate: internal,
			OnAttachment: func(a mimeparse.Attachment, r io.Reader) error {
				var s Sniffer
				_, _ = io.Copy(&s, r) // auch bei Lesefehler (abgeschnitten) prüfen, wie der Sync
				res := Check(a.ContentType, a.Filename, s.Detect())
				seen++
				if res.Mismatch {
					mismatches++
				}
				x, ok := expect[a.Filename]
				if !ok {
					if res.Mismatch || res.Executable {
						t.Errorf("%s: %q (%s): unexpected %+v", e.File, a.Filename, a.ContentType, res)
					}
					return nil
				}
				if res.Detected.MIME != x.Detected || res.Mismatch != x.Mismatch || res.Executable != x.Executable {
					t.Errorf("%s: %q: %+v, want %+v", e.File, a.Filename, res, x)
				}
				return nil
			}})
		if err != nil {
			continue
		}
	}
	if mismatches != 2 || seen < 200 {
		t.Fatalf("attachments seen %d, mismatches %d (want 2)", seen, mismatches)
	}
}
