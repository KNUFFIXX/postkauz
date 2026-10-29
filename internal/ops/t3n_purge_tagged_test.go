package ops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T3 Punkt 9 (E5: „Tags überleben das Löschen auf dem
// Server“; Festlegung der Prüfsitzung): Lokal getaggte Nachrichten löscht
// retention.purge nicht. Trockenlauf, Ausgabe und Audit zählen sie als
// kept_tagged; ohne Tag wird dieselbe Nachricht danach gelöscht.
func TestT3NPurgeKeepsAndCountsTaggedMessages(t *testing.T) {
	e := newRetEnv(t)
	ctx := context.Background()
	tagged := e.mirror(t, 1, "getaggt")
	if _, err := e.st.ChangeTags(ctx, store.TagChange{AccountID: e.acct, MessageIDs: []int64{tagged}, Add: []string{"rechnung"},
		SetBy: "cli", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	e.goneFromServer(t, tagged, 1, 100*24*time.Hour)

	out, err := e.run(t, "retention.purge", `{"older_than":"30d"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["kept_tagged"] != float64(1) || m["eligible"] != float64(0) || m["gone_from_server"] != float64(1) {
		t.Errorf("dry run does not report the tagged message as kept_tagged: %s", b)
	}

	out, err = e.run(t, "retention.purge", `{"older_than":"30d"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(RetentionPurgeOutput); o.KeptTagged != 1 || o.Purged != 0 {
		t.Errorf("purge = %+v, want kept_tagged 1, purged 0", o)
	}
	if e.count(t, `SELECT count(*) FROM messages WHERE id = ?`, tagged) != 1 {
		t.Fatal("purge deleted a message carrying the local tag 'rechnung'")
	}
	if e.count(t, `SELECT count(*) FROM message_tags WHERE message_id = ?`, tagged) != 1 {
		t.Error("tag of the kept message lost")
	}
	evs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "retention.purge"})
	if last := evs[len(evs)-1]; !strings.Contains(string(last.ParamsRedacted), `"kept_tagged":1`) {
		t.Errorf("audit end entry lacks kept_tagged: %s", last.ParamsRedacted)
	}

	// Ohne Tag gilt wieder die übrige Regel.
	if _, err := e.st.ChangeTags(ctx, store.TagChange{AccountID: e.acct, MessageIDs: []int64{tagged}, Remove: []string{"rechnung"},
		SetBy: "cli", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	out, err = e.run(t, "retention.purge", `{"older_than":"30d"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(RetentionPurgeOutput); o.KeptTagged != 0 || o.Purged != 1 {
		t.Errorf("purge after removing the tag = %+v, want purged 1", o)
	}
}
