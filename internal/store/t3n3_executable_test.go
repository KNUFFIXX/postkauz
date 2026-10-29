package store

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/magic"
)

// Dritte Nachbesserung T3, Punkt 9c: 0022 setzt executable für Anhänge,
// deren Name nach der heutigen Regel (magic.ExecutableName) ein Programm
// ist; eine ältere Version hatte sie mit executable = 0 gespeichert.
func TestT3N3Migration0022MarksExecutables(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 21); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO threads(id,account_id,subject_norm) VALUES (1,1,'a')`,
		`INSERT INTO messages(id,account_id,thread_id,subject,created_at) VALUES (1,1,1,'a',1)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var names []string
	for _, e := range magic.RiskyExtensions() {
		names = append(names, "Rechnung"+strings.ToUpper(e), "a.pdf"+e+". ", "x"+e+"::$DATA")
	}
	names = append(names, "rechnung.pdf", "x.bat.pdf", `C:\temp\x.cmd`, `dir/x.exe`, `x.exe\y.pdf`, "x.json", "x.news",
		"x.ksh.txt", ".exe", "x.bat:stream", "x.vbs\t", "ohne")
	for i, n := range names {
		if _, err := s.db.Exec(`INSERT INTO attachments(id, message_id, part_id, filename, content_type, size) VALUES (?, 1, ?, ?, 'application/octet-stream', 1)`,
			i+1, fmt.Sprint(i+2), n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO attachments(id, message_id, part_id, content_type, size) VALUES (?, 1, 'x', 'text/plain', 1)`, len(names)+1); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		got, err := s.QueryIntForTest(`SELECT executable FROM attachments WHERE id = ?`, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if want := magic.ExecutableName(n); (got == 1) != want {
			t.Errorf("%q: executable = %d after 0022, magic.ExecutableName = %v", n, got, want)
		}
	}
	if got, _ := s.QueryIntForTest(`SELECT executable FROM attachments WHERE id = ?`, len(names)+1); got != 0 {
		t.Errorf("attachment without name: executable = %d", got)
	}
	// Die Liste in der Migration ist genau riskyExt.
	b, err := migrationFS.ReadFile("migrations/0022_t3_ids_and_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	var inSQL []string
	for _, m := range regexp.MustCompile(`'(\.[a-z0-9-]+)'`).FindAllStringSubmatch(string(b), -1) {
		inSQL = append(inSQL, m[1])
	}
	slices.Sort(inSQL)
	if !slices.Equal(inSQL, magic.RiskyExtensions()) {
		t.Errorf("extensions in 0022 differ from magic.RiskyExtensions:\n0022:  %v\nmagic: %v", inSQL, magic.RiskyExtensions())
	}
}
