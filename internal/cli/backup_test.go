package cli

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Betreff eines Entwurfs und Text seines Anhangs: Mailinhalt, der in keiner
// Ausgabe von backup/restore und in keinem Audit-Eintrag stehen darf.
const (
	backupCanarySubject = "CANARY-backup-subject-5d1e"
	backupCanaryAttach  = "CANARY-backup-attachment-93ab"
)

// noLeak prüft eine Ausgabe auf Passwort und Mailinhalt.
func noLeak(t *testing.T, what, s string, mail ...string) {
	t.Helper()
	for _, c := range append([]string{canary, backupCanarySubject, backupCanaryAttach}, mail...) {
		if strings.Contains(s, c) {
			t.Fatalf("%s contains %q:\n%s", what, c, s)
		}
	}
}

// auditLines liest audit --json (zusammengefasst, alle Einträge) und lässt
// die Aufrufe weg, die nach der Sicherung in Original oder Kopie
// entstehen (backup.*, Lesen, Prüfen, Suchen).
func auditLines(t *testing.T, dataDir string) []string {
	t.Helper()
	code, out, errs := run(t, "audit", "--json", "--since", "3650d", "--limit", "1000", "--data-dir", dataDir)
	if code != ExitOK {
		t.Fatalf("audit: %d %s", code, errs)
	}
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var e ops.AuditEventOutput
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("audit line %q: %v", l, err)
		}
		switch {
		case strings.HasPrefix(e.Operation, "backup."), e.Operation == "audit.list", e.Operation == "audit.verify",
			e.Operation == "messages.search", e.Operation == "accounts.list":
			continue
		}
		keep = append(keep, l)
	}
	return keep
}

// withoutCursor entfernt cursor und score aus NDJSON-Treffern: Beide
// hängen vom Zeitpunkt der Anfrage ab (Datumsanteil des Rangs mit Bezug
// auf jetzt), nicht vom Stand der Daten. Die Zeilen werden sortiert: Zwei
// fast gleich bewertete Treffer könnten sonst zwischen zwei Anfragen die
// Plätze tauschen.
func withoutCursor(t *testing.T, out string) string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("search line %q: %v", l, err)
		}
		delete(m, "cursor")
		delete(m, "score")
		j, _ := json.Marshal(m)
		lines = append(lines, string(j)+"\n")
	}
	slices.Sort(lines)
	return strings.Join(lines, "")
}

// doctorBlobsOf liest doctor --blobs=all --json (ohne Dauer).
func doctorBlobsOf(t *testing.T, dataDir string) (DoctorBlobs, DoctorReport) {
	t.Helper()
	code, out, errs := run(t, "doctor", "--blobs=all", "--json", "--data-dir", dataDir)
	var rep DoctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Blobs == nil {
		t.Fatalf("doctor: %d %v %s %s", code, err, out, errs)
	}
	b := *rep.Blobs
	b.DurationMS = 0
	return b, rep
}

// Akzeptanz S3.4 Nr. 1 und A10: Datenverzeichnis mit Korpus (1.000
// Nachrichten per Import), einem Entwurf mit Dateianhang (Blob ohne Zeile)
// und einem Audit-Export; Sicherung, zweite Sicherung kopiert 0 Blobs;
// Wiederherstellung in ein frisches Verzeichnis. Danach liefern die 22
// Fälle des Suchtestsatzes dieselben Treffer, audit --json ist bis auf die
// neuen Einträge gleich, doctor --blobs=all ebenso; audit verify ist in
// beiden ok (ohne „changed"); der Schlüsselbund-Eintrag des Originals
// bleibt unberührt, die Kopie meldet ein fehlendes Passwort; die Kopie
// legt nichts still ab (Entwurf zurückgehalten, Job geschlossen).
func TestBackupRestoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full corpus (1,000 messages); skipped with -short")
	}
	dataDir, kr := setupEnv(t)
	if code, out, errs := runIn(t, canary+"\n", "account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %d %s %s", code, out, errs)
	}
	c, err := corpus.Generate(corpus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	tb := filepath.Join(t.TempDir(), "tb")
	if _, err := c.WriteThunderbird(tb, corpus.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "import", "mbox", tb, "-a", "demo", "--mailbox", "Archiv", "-q"); code != ExitOK {
		t.Fatalf("import: %d %s %s", code, out, errs)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "attach"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "attach", "notiz.txt"), []byte(backupCanaryAttach), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "draft", "new", "-a", "demo", "--to", "b@example.org", "--subject", backupCanarySubject, "--attach", "notiz.txt"); code != ExitOK {
		t.Fatalf("draft new: %d %s %s", code, out, errs)
	}
	exportDir := filepath.Join(t.TempDir(), "audit-export")
	if code, _, errs := run(t, "audit", "export", "--to", exportDir); code != ExitOK {
		t.Fatalf("audit export: %s", errs)
	}
	// Ein Wort aus dem Korpus, das in keiner Ausgabe stehen darf.
	mailWord := "Zwangsvollstreckung"

	// Sicherung, Warnung auf stderr, ENCRYPT-ME.txt.
	backupDir := filepath.Join(t.TempDir(), "Sicherung")
	start := time.Now()
	code, out, errs := run(t, "backup", "create", "--to", backupDir)
	t.Logf("backup create: %s", time.Since(start).Round(time.Millisecond))
	if code != ExitOK || !strings.Contains(out, "backup written to "+backupDir) || !strings.Contains(errs, "warning: the backup is not encrypted") {
		t.Fatalf("backup create: %d\n%s\n%s", code, out, errs)
	}
	noLeak(t, "backup output", out+errs, mailWord)
	if _, err := os.Stat(filepath.Join(backupDir, backup.EncryptNoteName)); err != nil {
		t.Fatal(err)
	}
	// Zweite Sicherung: 0 Blobs kopiert (auch mit --json kommt die Warnung).
	start = time.Now()
	code, out, errs = run(t, "backup", "create", "--to", backupDir, "--json")
	t.Logf("second backup create: %s", time.Since(start).Round(time.Millisecond))
	var res backup.Result
	if code != ExitOK || json.Unmarshal([]byte(out), &res) != nil || res.BlobsCopied != 0 || res.BlobsExisting != res.Blobs || res.Blobs < 1000 ||
		!strings.Contains(errs, "warning: the backup is not encrypted") {
		t.Fatalf("second backup: %d %+v\n%s", code, res, errs)
	}
	// Kein Passwort in der Sicherung.
	_ = filepath.Walk(backupDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(canary)) {
				t.Errorf("password in backup file %s", p)
			}
		}
		return nil
	})

	// Wiederherstellung in ein frisches Verzeichnis.
	restored := filepath.Join(t.TempDir(), "restored")
	start = time.Now()
	code, out, errs = run(t, "restore", "--from", backupDir, "--data-dir", restored, "--json")
	t.Logf("restore (with doctor --blobs=all): %s", time.Since(start).Round(time.Millisecond))
	var rep RestoreReport
	if code != ExitOK || json.Unmarshal([]byte(out), &rep) != nil {
		t.Fatalf("restore: %d\n%s\n%s", code, out, errs)
	}
	noLeak(t, "restore output", out+errs, mailWord)
	fx := rep.Restore.Fixes
	if !fx.NewIdentity || fx.CredentialRefs != 1 || fx.AuditExports == 0 || fx.DraftsHeld != 1 || fx.Jobs == 0 || fx.AuditStarts != 1 ||
		rep.Doctor.Blobs == nil || rep.Doctor.Blobs.Status != "ok" || rep.Doctor.Blobs.DraftAttachments != 1 {
		t.Fatalf("restore report: %+v / blobs %+v", fx, rep.Doctor.Blobs)
	}
	if rep.Doctor == nil || len(rep.Doctor.Accounts) != 1 || rep.Doctor.Accounts[0].Password != "missing" {
		t.Fatalf("restored copy must report the missing password: %+v", rep.Doctor.Accounts)
	}
	code, out, _ = run(t, "restore", "--from", backupDir, "--data-dir", filepath.Join(t.TempDir(), "r2"))
	if code != ExitOK || !strings.Contains(out, "identity:  new data directory id") || !strings.Contains(out, "result:  ok") {
		t.Fatalf("restore (text): %d\n%s", code, out)
	}

	// audit --json: gleich bis auf die neuen Einträge.
	if a, b := auditLines(t, dataDir), auditLines(t, restored); strings.Join(a, "\n") != strings.Join(b, "\n") || len(a) < 4 {
		t.Fatalf("audit differs:\noriginal:\n%s\nrestored:\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
	code, out, _ = run(t, "audit", "--json", "--raw", "--since", "3650d", "--limit", "1000", "--data-dir", restored)
	if code != ExitOK || !strings.Contains(out, `"operation":"backup.restore"`) {
		t.Fatalf("restored audit lacks backup.restore: %s", out)
	}
	noLeak(t, "restored audit", out, mailWord)
	_, out, _ = run(t, "audit", "--json", "--raw", "--since", "3650d", "--limit", "1000")
	noLeak(t, "original audit", out, mailWord)

	// 22 Fälle des Suchtestsatzes: dieselben Treffer.
	if len(c.Manifest.Search) < 20 {
		t.Fatalf("search cases: %d", len(c.Manifest.Search))
	}
	hits := 0
	for _, sc := range append([]corpus.SearchCase{{Query: "Rechnung"}}, c.Manifest.Search...) {
		_, a, _ := run(t, "search", sc.Query, "-a", "demo", "--json", "--limit", "200")
		_, b, _ := run(t, "search", sc.Query, "-a", "demo", "--json", "--limit", "200", "--data-dir", restored)
		a, b = withoutCursor(t, a), withoutCursor(t, b)
		if a != b {
			t.Fatalf("search %q differs:\n%s\n---\n%s", sc.Query, a, b)
		}
		hits += strings.Count(a, "\n")
	}
	if hits < 50 {
		t.Fatalf("search set found only %d hits; the comparison proves nothing", hits)
	}

	// doctor --blobs=all: gleich.
	ob, _ := doctorBlobsOf(t, dataDir)
	rb, rrep := doctorBlobsOf(t, restored)
	if !reflect.DeepEqual(ob, rb) {
		t.Fatalf("doctor --blobs=all differs:\n%+v\n%+v", ob, rb)
	}
	if ob.Files < 1000 || ob.Problems != 0 {
		t.Fatalf("doctor blobs: %+v", ob)
	}
	if !strings.Contains(strings.Join(rrep.Warnings, "\n"), "account demo has no password") {
		t.Fatalf("restored doctor warnings: %v", rrep.Warnings)
	}

	// A10: audit verify in beiden ohne „changed".
	for _, d := range []string{dataDir, restored} {
		code, out, errs := run(t, "audit", "verify", "--json", "--data-dir", d)
		var v ops.AuditVerifyOutput
		if code != ExitOK || json.Unmarshal([]byte(out), &v) != nil || v.Status != "ok" || len(v.Problems) != 0 || v.DaysChecked == 0 {
			t.Fatalf("audit verify %s: %d %s %s", d, code, out, errs)
		}
		if d == restored && v.OriginDays != v.DaysChecked {
			t.Fatalf("restored verify: origin days %d of %d", v.OriginDays, v.DaysChecked)
		}
	}

	// Die Kopie exportiert an einen eigenen Ort: danach prüft verify dort
	// die Dateien, die Tage des Originals sind ihre eigenen geworden.
	if code, _, errs := run(t, "audit", "export", "--to", filepath.Join(t.TempDir(), "export-restored"), "--data-dir", restored); code != ExitOK {
		t.Fatalf("audit export (restored): %s", errs)
	}
	code, out, errs = run(t, "audit", "verify", "--json", "--data-dir", restored)
	var v ops.AuditVerifyOutput
	if code != ExitOK || json.Unmarshal([]byte(out), &v) != nil || v.Status != "ok" || v.OriginDays != 0 {
		t.Fatalf("verify after export (restored): %d %s %s", code, out, errs)
	}
	if code, out, _ := run(t, "audit", "verify", "--data-dir", dataDir); code != ExitOK || !strings.HasPrefix(out, "ok:") {
		t.Fatalf("original after the copy's export: %d %s", code, out)
	}

	// Schlüsselbund: Der Eintrag des Originals bleibt, auch nachdem die
	// Kopie geöffnet wurde (sie zieht nichts um und liest ihn nie).
	if code, _, errs := run(t, "accounts", "list", "--data-dir", restored); code != ExitOK {
		t.Fatalf("accounts list (restored): %s", errs)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	origID, _ := st.DataDirID(t.Context())
	_ = st.Close()
	if v, err := kr.Get(secrets.AccountRef(origID, "demo")); err != nil || string(v) != canary {
		t.Fatalf("keyring entry of the original: %v", err)
	}
	if len(kr.m) != 1 {
		t.Fatalf("keyring entries: %d", len(kr.m))
	}
	// Der Entwurf der Kopie wird nicht abgelegt.
	code, out, _ = run(t, "draft", "show", "1", "--data-dir", restored)
	if code != ExitOK || !strings.Contains(out, "restored from a backup") {
		t.Fatalf("draft show (restored): %d\n%s", code, out)
	}
}

// Akzeptanz S3.4 Nr. 3 und 5 und Teile von 4: restore lehnt ein nicht
// leeres Verzeichnis, das Standard-Datenverzeichnis und das konfigurierte
// ab und verlangt --data-dir ausdrücklich; backup create mit Schlüssel ist
// human_only (Exit 3); der Tar-Strom gehört stdout, Ausgabe und Warnung
// gehen auf stderr; Fehlermeldungen ohne Mailinhalt und Passwort.
func TestBackupRestoreCommandRules(t *testing.T) {
	dataDir, _ := setupEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	if code, _, errs := runIn(t, canary+"\n", "account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %s", errs)
	}
	if code, _, errs := run(t, "draft", "new", "-a", "demo", "--to", "b@example.org", "--subject", backupCanarySubject); code != ExitOK {
		t.Fatalf("draft new: %s", errs)
	}

	// Mit Schlüssel: human_only, auch für admin.
	if code, _, _ := run(t, "key", "create", "--name", "adm", "--role", "admin"); code != ExitOK {
		t.Fatal("key create")
	}
	code, _, errs := run(t, "backup", "create", "--to", filepath.Join(t.TempDir(), "b"), "--key", "adm", "--json")
	if code != ExitDenied || !strings.Contains(errs, "human_only") {
		t.Fatalf("backup with key: %d %s", code, errs)
	}
	// Eingabefehler.
	for _, args := range [][]string{{"backup", "create"}, {"backup", "create", "--tar", "x.tar"},
		{"backup", "create", "--to", "x", "--tar", "-"}} {
		if code, _, errs := run(t, args...); code != ExitUsage {
			t.Errorf("%v: %d %s", args, code, errs)
		}
	}
	// Ziel im Datenverzeichnis.
	code, _, errs = run(t, "backup", "create", "--to", filepath.Join(dataDir, "backup"), "--json")
	if code != ExitError || !strings.Contains(errs, `"code":"backup_target"`) {
		t.Fatalf("backup into the data directory: %d %s", code, errs)
	}

	// Tar-Strom: stdout ist der Strom, Ausgabe (JSON) und Warnung auf stderr.
	code, out, errs := run(t, "backup", "create", "--tar", "-", "--json")
	if code != ExitOK {
		t.Fatalf("tar: %d %s", code, errs)
	}
	tr := tar.NewReader(strings.NewReader(out))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stdout is not a tar stream: %v", err)
		}
		names = append(names, h.Name)
	}
	if names[0] != backup.EncryptNoteName || names[len(names)-1] != backup.ManifestName ||
		!strings.Contains(errs, `"mode":"tar"`) || !strings.Contains(errs, "warning: the backup is not encrypted") {
		t.Fatalf("tar entries %v, stderr %s", names, errs)
	}
	noLeak(t, "tar stderr", errs)

	backupDir := filepath.Join(t.TempDir(), "b")
	if code, _, errs := run(t, "backup", "create", "--to", backupDir); code != ExitOK {
		t.Fatalf("backup: %s", errs)
	}
	// Trockenlauf: prüft die Sicherung, legt nichts an.
	dryTarget := filepath.Join(t.TempDir(), "dry")
	code, out, errs = run(t, "restore", "--from", backupDir, "--data-dir", dryTarget, "--dry-run")
	if code != ExitOK || !strings.Contains(out, "is complete and unchanged") || !strings.Contains(out, "nothing was written") {
		t.Fatalf("restore --dry-run: %d %s %s", code, out, errs)
	}
	if _, err := os.Stat(dryTarget); !os.IsNotExist(err) {
		t.Fatal("restore --dry-run created the target")
	}
	full := t.TempDir()
	_ = os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o600)
	def, err := config.DefaultDataDir()
	if err != nil || !strings.HasPrefix(def, home) {
		t.Fatalf("default data dir %s %v", def, err)
	}
	for name, c := range map[string]struct {
		args []string
		code int
		want string
	}{
		"no --from":       {[]string{"restore", "--data-dir", filepath.Join(t.TempDir(), "x")}, ExitUsage, "--from"},
		"no --data-dir":   {[]string{"restore", "--from", backupDir}, ExitUsage, "--data-dir"},
		"not empty":       {[]string{"restore", "--from", backupDir, "--data-dir", full}, ExitError, "backup_target"},
		"default dir":     {[]string{"restore", "--from", backupDir, "--data-dir", def}, ExitError, "backup_target"},
		"configured dir":  {[]string{"restore", "--from", backupDir, "--data-dir", dataDir}, ExitError, "backup_target"},
		"no manifest":     {[]string{"restore", "--from", t.TempDir(), "--data-dir", filepath.Join(t.TempDir(), "x")}, ExitError, "backup_invalid"},
		"inside backup":   {[]string{"restore", "--from", backupDir, "--data-dir", filepath.Join(backupDir, "x")}, ExitError, "backup_target"},
		"default, profil": {[]string{"restore", "--from", backupDir, "--data-dir", def, "--profile", "p"}, ExitError, "backup_target"},
	} {
		code, out, errs := run(t, append(c.args, "--json")...)
		if code != c.code || !strings.Contains(errs, c.want) {
			t.Errorf("%s: %d %s %s", name, code, out, errs)
		}
		noLeak(t, name, out+errs)
	}
	if _, err := os.Stat(def); !os.IsNotExist(err) {
		t.Fatalf("default data directory was created: %v", err)
	}
	// Manipulierte Datenbank: Abbruch, nichts bleibt, keine Inhalte in der Meldung.
	m, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m {
		if strings.HasSuffix(e.Name(), ".db") {
			p := filepath.Join(backupDir, e.Name())
			b, _ := os.ReadFile(p)
			b[100] ^= 0xff
			_ = os.WriteFile(p, b, 0o600)
		}
	}
	target := filepath.Join(t.TempDir(), "neu")
	code, out, errs = run(t, "restore", "--from", backupDir, "--data-dir", target)
	if code != ExitError || !strings.Contains(errs, "does not match the manifest") {
		t.Fatalf("manipulated database: %d %s %s", code, out, errs)
	}
	noLeak(t, "manipulated", out+errs)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("target left behind")
	}
}
