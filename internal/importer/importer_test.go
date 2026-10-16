package importer

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

var (
	corpusOnce sync.Once
	theCorpus  *corpus.Corpus
)

func standardCorpus(t testing.TB) *corpus.Corpus {
	t.Helper()
	corpusOnce.Do(func() {
		c, err := corpus.Generate(corpus.Options{})
		if err != nil {
			panic(err)
		}
		theCorpus = c
	})
	return theCorpus
}

// skipShort: Tests mit dem vollen Korpus laufen nicht mit -short.
func skipShort(t testing.TB) {
	t.Helper()
	if testing.Short() {
		t.Skip("full corpus")
	}
}

type env struct {
	st     *store.SQLite
	blobs  *blob.Store
	acct   int64
	dir    string
	dbPath string
}

func newEnv(t testing.TB) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{dir: dir, dbPath: filepath.Join(dir, "pk.db")}
	st, err := store.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.st, e.blobs = st, b
	e.acct, err = st.CreateAccount(context.Background(), store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1",
		Port: 1, TLS: "none", Username: "tester@example.invalid", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) engine() *Engine {
	return &Engine{Store: e.st, Blobs: e.blobs, AccountID: e.acct}
}

func (e *env) count(t testing.TB, q string, args ...any) int {
	t.Helper()
	n, err := e.st.QueryIntForTest(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) run(t testing.TB, dir, format, mailbox string, o Options) Report {
	t.Helper()
	srcs, _, err := Discover(dir, format, mailbox, store.ImportContent)
	if err != nil {
		t.Fatal(err)
	}
	o.Sources = srcs
	rep, err := e.engine().Run(context.Background(), o)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return rep
}

// strings aus einer Abfrage (eine Spalte).
func (e *env) strs(t testing.TB, q string, args ...any) []string {
	t.Helper()
	rows, err := e.st.DBForTest().Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func checkCounts(t testing.TB, rep Report, exp corpus.ImportExpect, newOffset int) {
	t.Helper()
	if rep.New != exp.New-newOffset || rep.Existing != exp.Copies+newOffset || rep.Skipped != exp.Deleted || rep.Errors != 0 {
		var probs []string
		for _, s := range rep.Sources {
			probs = append(probs, s.Problems...)
		}
		t.Fatalf("new %d existing %d skipped %d errors %d, want %d/%d/%d/0; problems %v", rep.New, rep.Existing, rep.Skipped,
			rep.Errors, exp.New-newOffset, exp.Copies+newOffset, exp.Deleted, probs)
	}
}

// checkImported prüft den Bestand nach einem Import in ein leeres Konto.
func (e *env) checkImported(t testing.TB, exp corpus.ImportExpect, flagsFor func(mid string) []string) {
	t.Helper()
	if n := e.count(t, `SELECT count(*) FROM messages`); n != exp.New {
		t.Errorf("messages = %d, want %d", n, exp.New)
	}
	if n := e.count(t, `SELECT count(*) FROM (SELECT message_id_header FROM messages WHERE message_id_header IS NOT NULL
		GROUP BY message_id_header HAVING count(*) > 1)`); n != 0 {
		t.Errorf("%d duplicate Message-IDs", n)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE thread_id IS NULL OR deleted_remote_at IS NULL OR blob_sha256 IS NULL`); n != 0 {
		t.Errorf("%d messages without thread, deleted_remote_at or blob", n)
	}
	if n := e.count(t, `SELECT count(*) FROM messages m LEFT JOIN bodies b ON b.message_id = m.id WHERE b.message_id IS NULL`); n != 0 {
		t.Errorf("%d messages without body row", n)
	}
	if n := e.count(t, `SELECT count(*) FROM index_queue`); n != exp.New {
		t.Errorf("index queue = %d, want %d (import must feed the index job)", n, exp.New)
	}
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes`); n != 0 {
		t.Errorf("import wrote %d server folder links", n)
	}
	for folder, mids := range exp.Folders {
		got := e.strs(t, `SELECT m.message_id_header FROM messages m JOIN message_import_folders mf ON mf.message_id = m.id
			JOIN import_folders f ON f.id = mf.folder_id WHERE f.path = ? AND m.message_id_header IS NOT NULL`, folder)
		sort.Strings(got)
		want := slices.Clone(mids)
		sort.Strings(want)
		if !slices.Equal(got, want) {
			t.Errorf("folder %s: %d messages, want %d", folder, len(got), len(want))
		}
	}
	for mid, want := range exp.Flags {
		got := flagsFor(mid)
		if !sameSet(got, want) {
			t.Errorf("flags of %s = %v, want %v", mid, got, want)
		}
	}
	for mid, want := range exp.Keywords {
		kw := e.strs(t, `SELECT j.value FROM messages m, json_each(m.keywords) j WHERE m.message_id_header = ?`, mid)
		if !sameSet(kw, want) {
			t.Errorf("keywords of %s = %v, want %v", mid, kw, want)
		}
	}
	for mid, lines := range exp.FromLines {
		text := e.strs(t, `SELECT b.text_plain FROM bodies b JOIN messages m ON m.id = b.message_id WHERE m.message_id_header = ?`, mid)
		for _, l := range lines {
			if len(text) != 1 || !strings.Contains(text[0], "\n"+l) {
				t.Errorf("%s: line %q missing after unquoting: %q", mid, l, text)
			}
		}
	}
	// Keine Thunderbird-Zeile in einer Rohmail.
	for _, sum := range e.strs(t, `SELECT blob_sha256 FROM messages`) {
		rc, err := e.blobs.Get(sum)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if bytes.Contains(bytes.ToLower(b), []byte("x-mozilla-")) {
			t.Fatalf("raw message %s still has X-Mozilla lines", sum)
		}
	}
}

func (e *env) flags(t testing.TB) func(string) []string {
	return func(mid string) []string {
		var out []string
		for _, c := range []struct{ col, name string }{{"flag_seen", "seen"}, {"flag_flagged", "flagged"},
			{"flag_answered", "answered"}, {"flag_draft", "draft"}} {
			if e.count(t, `SELECT count(*) FROM messages WHERE message_id_header = ? AND `+c.col+` = 1`, mid) > 0 {
				out = append(out, c.name)
			}
		}
		return out
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}

// Der Korpus als Thunderbird-Profil: alle Nachrichten einmal, Kopien als
// Ordner, gelöscht markierte übersprungen, Flags aus den Status-Bits,
// Thunderbird-Zeilen entfernt, Inhalt gleich dem Original (content_hash),
// .msf und fremde Dateien ignoriert; zweiter Lauf new: 0.
func TestImportThunderbirdCorpus(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	dir := filepath.Join(t.TempDir(), "imap.example.org")
	exp, err := c.WriteThunderbird(dir, corpus.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	srcs, ignored, err := Discover(dir, FormatMbox, "", store.ImportContent)
	if err != nil {
		t.Fatal(err)
	}
	var folders []string
	for _, s := range srcs {
		folders = append(folders, s.Folder)
	}
	if want := []string{"Archiv/2024", "Archiv/2025", "Drafts", "Entwürfe", "INBOX", "Wichtig"}; !slices.Equal(folders, want) || ignored != len(srcs)+1 {
		t.Fatalf("sources = %v (ignored %d), want %v (ignored %d: .msf and msgFilterRules.dat)", folders, ignored, want, len(srcs)+1)
	}
	e := newEnv(t)
	rep := e.run(t, dir, FormatMbox, "", Options{})
	checkCounts(t, rep, exp, 0)
	for _, s := range rep.Sources {
		if s.Variant != "thunderbird" {
			t.Errorf("%s: variant %q", s.Path, s.Variant)
		}
	}
	e.checkImported(t, exp, e.flags(t))

	// content_hash: Nach dem Lösen aus dem mbox-Rahmen ist jede Nachricht des
	// Korpus wieder das Original (bis auf Zeilenenden am Schluss).
	for _, i := range exp.Included {
		h := newVariantHasher()
		_, _ = h.Write(c.Raw[i])
		_, variants := h.Sums()
		args := []any{e.acct}
		for _, v := range variants {
			args = append(args, v)
		}
		if n := e.count(t, `SELECT count(*) FROM messages WHERE account_id = ? AND content_hash IN (?,?,?)`, args...); n != 1 {
			t.Errorf("%s: content differs from the original after import", c.Manifest.Messages[i].File)
		}
	}

	// Zweiter Lauf: alles unverändert; mit --rescan alles vorhanden.
	rep = e.run(t, dir, FormatMbox, "", Options{})
	if rep.New != 0 || rep.Existing != 0 || rep.Unchanged != len(srcs) {
		t.Fatalf("second run = %+v", rep)
	}
	rep = e.run(t, dir, FormatMbox, "", Options{Rescan: true})
	if rep.New != 0 || rep.Existing != exp.New+exp.Copies || rep.Skipped != exp.Deleted || rep.Linked != 0 {
		t.Fatalf("rescan = new %d existing %d skipped %d linked %d", rep.New, rep.Existing, rep.Skipped, rep.Linked)
	}
	if n := e.count(t, `SELECT count(*) FROM messages`); n != exp.New {
		t.Fatalf("messages after rescan = %d", n)
	}
}

// Trockenlauf: dieselben Zahlen, nichts geschrieben (keine Zeile, kein Blob).
func TestImportDryRun(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	dir := t.TempDir()
	exp, err := c.WriteThunderbird(dir, corpus.ImportOptions{LF: true})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	rep := e.run(t, dir, FormatMbox, "Import", Options{DryRun: true})
	checkCounts(t, rep, exp, 0)
	for _, tb := range []string{"messages", "import_folders", "message_import_folders", "import_sources", "blobs"} {
		if n := e.count(t, `SELECT count(*) FROM `+tb); n != 0 {
			t.Errorf("dry run wrote %d rows into %s", n, tb)
		}
	}
	var files int
	_ = filepath.WalkDir(e.blobs.Root(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Errorf("dry run wrote %d blob files", files)
	}
	// Danach echt, mit Präfix: Ordner liegen unter "Import/".
	rep = e.run(t, dir, FormatMbox, "Import", Options{})
	checkCounts(t, rep, exp, 0)
	if n := e.count(t, `SELECT count(*) FROM import_folders WHERE path NOT LIKE 'Import/%'`); n != 0 {
		t.Errorf("%d folders outside the prefix", n)
	}
}

// Maildir mit cur/new/tmp, Flag-Suffixen, Maildir++-Unterordnern.
func TestImportMaildirCorpus(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	dir := filepath.Join(t.TempDir(), "Mail")
	exp, err := c.WriteMaildir(dir, corpus.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	rep := e.run(t, dir, FormatMaildir, "", Options{})
	checkCounts(t, rep, exp, 0)
	if len(rep.Sources) != exp.Sources {
		t.Fatalf("sources = %d, want %d", len(rep.Sources), exp.Sources)
	}
	e.checkImported(t, exp, e.flags(t))
	rep = e.run(t, dir, FormatMaildir, "", Options{})
	if rep.New != 0 || rep.Unchanged != exp.Sources {
		t.Fatalf("second run = %+v", rep)
	}
	// Neue Datei im Maildir: nur sie wird gelesen (Rest per Abgleich vorhanden).
	if err := os.WriteFile(filepath.Join(dir, "new", "1900000000.M1P1.neu"),
		[]byte("Subject: neu\r\nMessage-ID: <neu@import.example.org>\r\n\r\nneu\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep = e.run(t, dir, FormatMaildir, "", Options{})
	if rep.New != 1 || rep.Errors != 0 {
		t.Fatalf("after a new file = new %d existing %d errors %d", rep.New, rep.Existing, rep.Errors)
	}
}

// Labels: Ordner, deren Nachrichten nur zugeordnet werden (Gmail: Inhalt aus
// „Alle Nachrichten", Labels per Message-ID); was dort fehlt, wird
// übersprungen, nicht gespeichert.
func TestImportLabelsOnly(t *testing.T) {
	skipShort(t)
	c := standardCorpus(t)
	dir := t.TempDir()
	if _, err := c.WriteThunderbird(dir, corpus.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	content, _, err := Discover(filepath.Join(dir, "INBOX"), FormatMbox, "[Gmail]/Alle Nachrichten", store.ImportContent)
	if err != nil {
		t.Fatal(err)
	}
	labels, _, err := Discover(dir, FormatMbox, "", store.ImportLabels)
	if err != nil {
		t.Fatal(err)
	}
	labels = excludeSources(labels, content)
	rep, err := e.engine().Run(context.Background(), Options{Sources: append(content, labels...)})
	if err != nil {
		t.Fatal(err)
	}
	inbox := e.count(t, `SELECT count(*) FROM messages`)
	if rep.New != inbox || inbox == 0 {
		t.Fatalf("new = %d, messages = %d", rep.New, inbox)
	}
	// INBOX-Nachrichten mit Kopie in „Wichtig" sind dort zugeordnet, alles
	// andere aus den Label-Ordnern ist übersprungen.
	if n := e.count(t, `SELECT count(*) FROM import_folders WHERE path = 'INBOX'`); n != 0 {
		t.Fatal("the content source itself must not be read again as labels")
	}
	wichtig := e.count(t, `SELECT count(*) FROM message_import_folders mf JOIN import_folders f ON f.id = mf.folder_id WHERE f.path = 'Wichtig'`)
	if wichtig == 0 || rep.Skipped == 0 || rep.Linked != wichtig {
		t.Fatalf("labels: linked %d (Wichtig %d), skipped %d", rep.Linked, wichtig, rep.Skipped)
	}
}
