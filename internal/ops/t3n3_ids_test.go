package ops

import (
	"encoding/json"
	"testing"
)

// Dritte Nachbesserung T3, Punkt 3: threads.get und attachments.text
// nehmen keine ID über 2^53-1 an (sonst stand sie als Ziel im Audit, und
// ein JSON-Leser mit Gleitkommazahlen rundet sie).
func TestT3N3IDUpperBound(t *testing.T) {
	for _, op := range []Operation{ThreadsGet(nil), AttachmentsText(nil, nil, nil, nil)} {
		if err := op.validateInput(json.RawMessage(`{"id": 9007199254740991}`)); err != nil {
			t.Errorf("%s with id 2^53-1: %v", op.Name, err)
		}
		for _, in := range []string{`{"id": 9007199254740992}`, `{"id": 9223372036854775807}`} {
			if err := op.validateInput(json.RawMessage(in)); err == nil {
				t.Errorf("%s accepts %s", op.Name, in)
			}
		}
	}
}
