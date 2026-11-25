package drafts

import (
	"testing"
	"time"
)

// A12 Teil 0 (b): Eine geplante Nachricht, die schon einem geschlossenen
// Entwurf als sent_message_id zugeordnet ist, wird keinem zweiten Entwurf
// als geplant zugeordnet.
func TestA12ScheduledMessageOfClosedDraft(t *testing.T) {
	e := newThEnv(t)
	at := e.now.Add(3 * 24 * time.Hour)
	d1 := e.draftOf(KindReply, thOwn, true)
	e.later()
	d2 := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d1)
	e.dropCopy(d2)
	mid := e.schedule(at, "Re: Abrechnung", thOwn, thAnna, "", "")
	e.exec(`UPDATE drafts SET status = 'sent_by_human', sent_message_id = ?, closed_at = ? WHERE id = ?`, mid, e.now.UnixMilli(), d1.ID)
	e.check()
	if sc, ok := e.vermerk(d2.ID); ok {
		t.Fatalf("second draft scheduled with the message of a closed draft: %+v", sc)
	}
}
