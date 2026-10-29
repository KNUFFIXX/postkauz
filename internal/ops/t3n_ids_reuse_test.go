package ops

// Nachbesserung T3, Punkt 8 (G2-3 der Nachprüfung): Nach retention.purge
// darf eine neue Nachricht weder die Anhang-ID noch die Thread-ID einer
// gelöschten bekommen (A14: IDs ohne Wiederverwendung; attachments.save
// und get_attachment_text nehmen IDs, das Audit nennt attachment:<id> und
// thread:<id>).

import (
	"testing"
	"time"
)

func TestT3NPurgeDoesNotReuseAttachmentOrThreadIDs(t *testing.T) {
	e := newRetEnv(t)
	_ = e.mirror(t, 1, "alt")
	gone := e.mirror(t, 2, "weg") // jüngste Nachricht: größte Anhang- und Thread-ID
	oldAtt := e.count(t, `SELECT id FROM attachments WHERE message_id = ?`, gone)
	oldThread := e.count(t, `SELECT thread_id FROM messages WHERE id = ?`, gone)
	e.goneFromServer(t, gone, 2, 100*24*time.Hour)
	if _, err := e.run(t, "retention.purge", `{"older_than":"30d"}`, false); err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT count(*) FROM messages WHERE id = ?`, gone) != 0 {
		t.Fatal("not purged")
	}
	if e.count(t, `SELECT count(*) FROM threads WHERE id = ?`, oldThread) != 0 ||
		e.count(t, `SELECT count(*) FROM attachments WHERE id = ?`, oldAtt) != 0 {
		t.Fatal("thread or attachment of the purged message left over")
	}
	fresh := e.mirror(t, 3, "neu")
	newAtt := e.count(t, `SELECT id FROM attachments WHERE message_id = ?`, fresh)
	newThread := e.count(t, `SELECT thread_id FROM messages WHERE id = ?`, fresh)
	if fresh <= gone {
		t.Errorf("message id reused after purge: old %d, new %d", gone, fresh)
	}
	if newAtt <= oldAtt {
		t.Errorf("attachment id reused after purge: old %d, new %d", oldAtt, newAtt)
	}
	if newThread <= oldThread {
		t.Errorf("thread id reused after purge: old %d, new %d", oldThread, newThread)
	}
}
