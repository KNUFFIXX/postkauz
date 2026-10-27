package ops

import (
	"errors"
	"testing"
	"time"
)

func TestParseSinceRejectsOverflow(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	var ie *InputError
	for _, s := range []string{"106752d", "9223372036854775807d", "99999999999999999999d", "100001d"} {
		got, err := ParseSince(s, now)
		if !errors.As(err, &ie) {
			t.Errorf("ParseSince(%q) = %v, %v; want an input error", s, got, err)
		}
	}
	got, err := ParseSince("100000d", now)
	if err != nil || !got.Before(now) {
		t.Fatalf("ParseSince(100000d) = %v, %v", got, err)
	}
	if got, err := ParseSince("7d", now); err != nil || !got.Equal(now.Add(-7*24*time.Hour)) {
		t.Fatalf("ParseSince(7d) = %v, %v", got, err)
	}
	if _, err := ParseSince("-1d", now); !errors.As(err, &ie) {
		t.Fatalf("ParseSince(-1d) = %v", err)
	}
}
