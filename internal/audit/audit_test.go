package audit

import (
	"context"
	"testing"
)

func TestMemoryAppendOnly(t *testing.T) {
	var w Writer = &Memory{}
	for _, op := range []string{"a", "b"} {
		if err := w.Append(context.Background(), Event{Operation: op, Result: ResultOK}); err != nil {
			t.Fatal(err)
		}
	}
	got := w.(*Memory).Events()
	if len(got) != 2 || got[0].Operation != "a" || got[1].Operation != "b" {
		t.Fatalf("events = %+v", got)
	}
	// Die Kopie darf den internen Zustand nicht verändern.
	got[0].Operation = "x"
	if w.(*Memory).Events()[0].Operation != "a" {
		t.Fatal("Events() must return a copy")
	}
}
