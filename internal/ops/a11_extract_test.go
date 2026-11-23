package ops

import (
	"context"
	"testing"
)

// A11 Teil 3 (b): attachments.text versucht den Auszug bei failed nur dann
// jetzt (ohne zu speichern), wenn der gespeicherte Grund vorübergehend
// ist; sonst bleibt die Ausgabe wie bisher.
func TestA11AttachmentTextRetriesTransient(t *testing.T) {
	for _, c := range []struct {
		reason string
		now    bool
	}{
		{"extractor failed: exit status 0xc0000142", true},
		{"extractor failed: Syntax Error: kaputt", false},
	} {
		t.Run(c.reason, func(t *testing.T) {
			e := newMirrorEnv(t)
			msg := e.seed(t)
			atts, _ := e.st.AttachmentsByMessage(context.Background(), msg)
			id := atts[1].ID // Aushang.html (im Prozess lesbar)
			if err := e.st.ExecForTest(`UPDATE attachments SET extract_status = 'failed', extract_error = ? WHERE id = ?`, c.reason, id); err != nil {
				t.Fatal(err)
			}
			out, err := e.run("attachments.text", map[string]any{"id": id}, "", false)
			if err != nil {
				t.Fatal(err)
			}
			o := out.(AttachmentTextOutput)
			if o.ExtractedNow != c.now {
				t.Fatalf("extracted_now = %v (status %s, error %q)", o.ExtractedNow, o.ExtractStatus, o.ExtractError)
			}
			if c.now && (o.ExtractStatus != "ok" || o.Text == "") {
				t.Fatalf("now: %s %q", o.ExtractStatus, o.Text)
			}
			if !c.now && (o.ExtractStatus != "failed" || o.Text != "") {
				t.Fatalf("permanent: %s %q", o.ExtractStatus, o.Text)
			}
			// Nichts gespeichert.
			a, _, _ := e.st.AttachmentByID(context.Background(), id)
			if a.ExtractStatus != "failed" {
				t.Fatalf("stored status = %s", a.ExtractStatus)
			}
		})
	}
}
