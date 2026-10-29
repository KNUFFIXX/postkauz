package ops

// Zweite Nachbesserung T3, Punkt 5 (B1 der Nachprüfung): drafts.create legt
// auch übernommene Anhänge (attachment_id aus dem Spiegel, Anhänge der
// weitergeleiteten Nachricht) per Put ab. Laufen zwischen Einlesen und
// Speichern des Entwurfs purge (Quelle weg) und blob gc, legt das Put den
// Blob neu an; der Entwurf verweist nie ins Leere (schließt das Restfenster
// 6(a) der ersten Nachbesserung).

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/retention"
)

// t3n2HookBlobs ist ein echter Blob-Store; was er nicht hat (Rohmail und
// Anhänge der Testumgebung unter Testnamen), liest er aus fallback. Beim
// Schließen des ersten Lesers für sha läuft hook.
type t3n2HookBlobs struct {
	*blob.Store
	fallback BlobReader
	sha      string
	hook     func()
}

type t3n2HookRC struct {
	io.ReadCloser
	hook func()
}

func (r *t3n2HookRC) Close() error {
	err := r.ReadCloser.Close()
	if r.hook != nil {
		h := r.hook
		r.hook = nil
		h()
	}
	return err
}

func (b *t3n2HookBlobs) Get(sha string) (io.ReadCloser, error) {
	if !b.Store.Has(sha) {
		return b.fallback.Get(sha)
	}
	rc, err := b.Store.Get(sha)
	if err != nil || sha != b.sha || b.hook == nil {
		return rc, err
	}
	h := b.hook
	b.hook = nil
	return &t3n2HookRC{ReadCloser: rc, hook: h}, nil
}

func TestT3N2TakenOverAttachmentSurvivesPurgeAndGC(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
	}{
		{"mirrored attachment", map[string]any{"account": "demo", "kind": "new", "to": []string{"a@example.org"},
			"subject": "Weiter", "attachments": []map[string]any{{"attachment_id": 1}}}},
		{"forwarded attachment", map[string]any{"kind": "forward", "in_reply_to": 1, "to": []string{"c@example.org"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			e := newDraftEnv(t, false)
			bs, err := blob.Open(t.TempDir(), blob.Options{})
			if err != nil {
				t.Fatal(err)
			}
			content := "Rechnung 2026-117, PDF-Ersatz " + strings.Repeat("x", 100)
			info, err := bs.Put(strings.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			p, _, _ := bs.Path(info.SHA256)
			old := time.Now().Add(-90 * 24 * time.Hour)
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
			if err := e.st.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES (?,?,1)`, info.SHA256, len(content)); err != nil {
				t.Fatal(err)
			}
			if err := e.st.ExecForTest(`UPDATE attachments SET sha256 = ?, truncated = 0 WHERE id = 1`, info.SHA256); err != nil {
				t.Fatal(err)
			}
			svc := &retention.Service{Store: e.st, Blobs: bs}
			var gcRep retention.GCReport
			var gcErr error
			hb := &t3n2HookBlobs{Store: bs, fallback: e.blobs, sha: info.SHA256, hook: func() {
				// purge (Quelle weg) und blob gc laufen genau jetzt.
				if err := e.st.ExecForTest(`DELETE FROM attachments WHERE sha256 = ?`, info.SHA256); err != nil {
					t.Error(err)
				}
				gcRep, gcErr = svc.GC(ctx, time.Hour, false)
			}}
			e.deps.DraftBlobs = hb
			reg, err := NewDefaultRegistry(e.deps)
			if err != nil {
				t.Fatal(err)
			}
			e.x = &Executor{Registry: reg, Audit: e.st.AuditWriter()}

			out, err := e.do(t, assistant, "drafts.create", c.in, false)
			if gcErr != nil {
				t.Fatalf("gc: %v", gcErr)
			}
			if hb.hook != nil {
				t.Fatal("hook did not run")
			}
			if gcRep.RemovedFiles < 1 {
				t.Fatalf("blob gc removed nothing (report %+v); the test does not reach the race", gcRep)
			}
			if err != nil {
				t.Fatalf("drafts.create: %v", err)
			}
			d := out.(DraftsWriteOutput)
			if !bs.Has(info.SHA256) {
				t.Fatalf("draft %d saved, but its attachment blob %s was removed by blob gc (report %+v)", d.DraftID, info.SHA256[:12], gcRep)
			}
			refs, err := e.st.BlobReferences(ctx)
			if err != nil || !refs[info.SHA256] {
				t.Fatalf("draft %d does not reference the attachment blob: %v %v", d.DraftID, refs[info.SHA256], err)
			}
			// Ein späteres blob gc lässt ihn stehen (der Entwurf verweist darauf).
			if rep, err := svc.GC(ctx, 0, false); err != nil || !bs.Has(info.SHA256) {
				t.Fatalf("second gc: %+v %v, blob present %v", rep, err, bs.Has(info.SHA256))
			}
		})
	}
}
