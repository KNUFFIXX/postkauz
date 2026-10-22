package imapx

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// S2.5: APPEND mit UIDVALIDITY (APPENDUID) und die Suche nach einer Kopie
// über ihre Message-ID (Server ohne APPENDUID, Wiederaufnahme nach Abbruch).
func TestAppendInfoAndSearchHeader(t *testing.T) {
	addr, _ := memServer(t)
	c := dialLogin(t, addr)
	date := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	var uv uint32
	for i := 1; i <= 3; i++ {
		raw := testMail(1) // zweimal dieselbe Message-ID (alte Kopien bleiben)
		if i == 3 {
			raw = testMail(2)
		}
		r, err := c.AppendInfo("Archiv", raw, []string{`\Draft`, `\Seen`}, date)
		if err != nil || r.UID != uint32(i) || r.UIDValidity == 0 || (uv != 0 && r.UIDValidity != uv) {
			t.Fatalf("append %d = %+v, %v", i, r, err)
		}
		uv = r.UIDValidity
	}
	if _, err := c.Select("Archiv", SelectOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if uids, err := c.SearchHeader("Message-ID", "m1@example.org", 0); err != nil || !slices.Equal(uids, []uint32{1, 2}) {
		t.Fatalf("search = %v, %v", uids, err)
	}
	if uids, err := c.SearchHeader("Message-ID", "m1@example.org", 2); err != nil || !slices.Equal(uids, []uint32{2}) {
		t.Fatalf("search from 2 = %v, %v", uids, err)
	}
	if uids, err := c.SearchHeader("Message-ID", "fehlt@example.org", 0); err != nil || len(uids) != 0 {
		t.Fatalf("search missing = %v, %v", uids, err)
	}
	meta, err := c.FetchMeta([]uint32{1}, MetaOptions{})
	if err != nil || len(meta) != 1 || !slices.Contains(meta[0].Flags, `\Draft`) || !slices.Contains(meta[0].Flags, `\Seen`) {
		t.Fatalf("flags = %+v, %v", meta, err)
	}
	// Ein Ordner, den es nicht gibt: abgelehnt, nicht wiederholbar.
	if _, err := c.AppendInfo("Fehlt", testMail(3), nil, date); !errors.Is(err, ErrRejected) {
		t.Fatalf("append to a missing mailbox = %v", err)
	}
}
