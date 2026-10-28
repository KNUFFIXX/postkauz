package mailsync

import (
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Gegenprüfung K9: sync --watch nennt max_message_bytes nur, wenn eine
// Runde Nachrichten übersprungen hat; die Zahl summiert sich über Runden.
func TestAddRoundMaxMessageBytesOnlyWithSkipped(t *testing.T) {
	var out ops.SyncOutput
	addRound(&out, &ops.SyncOutput{New: 2, MaxMessageBytes: 1 << 20})
	if out.New != 2 || out.SkippedTooLarge != 0 || out.MaxMessageBytes != 0 {
		t.Fatalf("round without skipped: %+v", out)
	}
	addRound(&out, &ops.SyncOutput{SkippedTooLarge: 3, MaxMessageBytes: 1 << 20})
	addRound(&out, &ops.SyncOutput{New: 1, MaxMessageBytes: 2 << 20})
	addRound(&out, &ops.SyncOutput{SkippedTooLarge: 1, MaxMessageBytes: 1 << 20})
	if out.New != 3 || out.SkippedTooLarge != 4 || out.MaxMessageBytes != 1<<20 {
		t.Fatalf("rounds: %+v", out)
	}
}
