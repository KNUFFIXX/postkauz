package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// canary steht als Betreff in der Datenbank und als Inhalt eines Blobs:
// Er darf in keiner Ausgabe, keinem Audit-Eintrag und keiner Fehlermeldung
// stehen (Akzeptanz 4).
const canary = "CANARY-backup-mailtext-71c2e0"

type env struct {
	dir   string
	st    *store.SQLite
	blobs *blob.Store
	r     *Runner
	now   time.Time
	sums  []string
}

// newEnv: Datenverzeichnis mit Datenbank (ein Konto, eine Nachricht mit
// dem Kanarienwert als Betreff), drei Blobs (einer zstd-komprimiert).
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, brand.Name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "u", AuthType: "password", CredentialRef: "account/demo"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO messages(account_id, subject, created_at) VALUES (1, ?, 1)`, canary); err != nil {
		t.Fatal(err)
	}
	bs, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{dir: dir, st: st, blobs: bs, now: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
	e.r = &Runner{Store: st, Blobs: bs, DataDir: dir, Version: "test", Now: func() time.Time { return e.now }}
	for _, c := range []string{"Text " + canary, "zweiter Blob"} {
		e.put(t, bs, c)
	}
	zs, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{Compress: true})
	if err != nil {
		t.Fatal(err)
	}
	e.put(t, zs, "komprimiert "+strings.Repeat("x", 500))
	return e
}

func (e *env) put(t *testing.T, bs *blob.Store, content string) string {
	t.Helper()
	info, err := bs.Put(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	e.sums = append(e.sums, info.SHA256)
	return info.SHA256
}

func readManifest(t *testing.T, dir string) Manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseManifest(b, store.KnownSchemaVersion())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func lastAudit(t *testing.T, st *store.SQLite, op string) string {
	t.Helper()
	var params string
	if err := st.DBForTest().QueryRow(`SELECT params_redacted FROM audit_events WHERE operation = ? ORDER BY id DESC LIMIT 1`, op).Scan(&params); err != nil {
		t.Fatalf("audit %s: %v", op, err)
	}
	return params
}

func mustNotLeak(t *testing.T, what string, s string) {
	t.Helper()
	if strings.Contains(s, canary) {
		t.Fatalf("%s contains mail content: %s", what, s)
	}
}

// Sicherung in einen Ordner: Manifest mit SHA-256 je Datei, Schnappschuss,
// alle Blobs, ENCRYPT-ME.txt, Rechte 0600/0700, Zähler im Audit ohne Pfad.
// Ein zweiter Lauf kopiert 0 Blobs und ersetzt den Schnappschuss; ein neuer
// Blob und ein Audit-Archiv kommen beim dritten Lauf dazu.
func TestCreateFolderIncremental(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	to := filepath.Join(t.TempDir(), "Sicherung")
	res, err := e.r.Create(ctx, Options{To: to, CallAuditID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "folder" || res.Blobs != 3 || res.BlobsCopied != 3 || res.BlobsExisting != 0 || res.Files != 4 ||
		res.SchemaVersion != store.KnownSchemaVersion() || res.ManifestSHA256 == "" || res.Warning == "" {
		t.Fatalf("first backup = %+v", res)
	}
	m := readManifest(t, to)
	if m.Database != res.Database || m.Counts.Blobs != 3 || m.ProgramVersion != "test" {
		t.Fatalf("manifest = %+v", m)
	}
	note, err := os.ReadFile(filepath.Join(to, EncryptNoteName))
	if err != nil || !strings.Contains(string(note), "age") || !strings.Contains(string(note), "LUKS") {
		t.Fatalf("ENCRYPT-ME.txt = %q %v", note, err)
	}
	if runtime.GOOS != "windows" {
		_ = filepath.Walk(to, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o600)
			if fi.IsDir() {
				want = 0o700
			}
			if fi.Mode().Perm() != want {
				t.Errorf("%s: mode %v, want %v", p, fi.Mode().Perm(), want)
			}
			return nil
		})
	}
	params := lastAudit(t, e.st, OpDone)
	var p map[string]any
	if json.Unmarshal([]byte(params), &p) != nil || p["blobs_copied"] != float64(3) || p["call_audit_id"] != float64(7) ||
		p["target_hash"] != PathHash(to) || strings.Contains(params, "Sicherung") {
		t.Fatalf("backup.done = %s", params)
	}
	out, _ := json.Marshal(res)
	mustNotLeak(t, "result", string(out))
	mustNotLeak(t, "audit", params)

	// Zweiter Lauf: nichts Neues.
	e.now = e.now.Add(time.Minute)
	res2, err := e.r.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatal(err)
	}
	if res2.BlobsCopied != 0 || res2.BlobsExisting != 3 || res2.SnapshotsRemoved != 1 || res2.Database == res.Database {
		t.Fatalf("second backup = %+v", res2)
	}
	snaps, _ := filepath.Glob(filepath.Join(to, brand.Name+"-*.db"))
	if len(snaps) != 1 || filepath.Base(snaps[0]) != res2.Database {
		t.Fatalf("snapshots = %v", snaps)
	}

	// Dritter Lauf: ein neuer Blob, ein Audit-Archiv.
	e.put(t, e.blobs, "neu")
	e.archive(t, "audit-archive-2025-01-01-2025-12-31.db", "archiv")
	e.now = e.now.Add(time.Minute)
	res3, err := e.r.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatal(err)
	}
	if res3.BlobsCopied != 1 || res3.BlobsExisting != 3 || res3.AuditArchives != 1 || res3.Files != 6 {
		t.Fatalf("third backup = %+v", res3)
	}
	if _, err := os.Stat(filepath.Join(to, "audit-archive-2025-01-01-2025-12-31.db")); err != nil {
		t.Fatal(err)
	}

	// Trockenlauf: nichts geschrieben.
	dry := filepath.Join(t.TempDir(), "dry")
	res4, err := e.r.Create(ctx, Options{To: dry, DryRun: true})
	if err != nil || !res4.DryRun || res4.BlobsCopied != 4 {
		t.Fatalf("dry run = %+v %v", res4, err)
	}
	if _, err := os.Stat(dry); !os.IsNotExist(err) {
		t.Fatalf("dry run created the target: %v", err)
	}
}

// Ziele im Datenverzeichnis, ein Ziel, das das Datenverzeichnis enthält,
// und ein fremder, nicht leerer Ordner werden abgelehnt; nichts entsteht.
func TestCreateRejectsTargets(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "Steuer.pdf"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "datei")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	for name, to := range map[string]string{
		"inside":   filepath.Join(e.dir, "backup"),
		"same":     e.dir,
		"parent":   filepath.Dir(e.dir),
		"foreign":  foreign,
		"file":     file,
		"relative": filepath.Join(e.dir, "blobs", "..", "x"),
	} {
		_, err := e.r.Create(ctx, Options{To: to})
		if !errors.Is(err, ErrTarget) {
			t.Errorf("%s: err = %v", name, err)
		}
		mustNotLeak(t, name, err.Error())
	}
	if _, err := os.Stat(filepath.Join(e.dir, "backup")); !os.IsNotExist(err) {
		t.Fatal("target inside the data directory was created")
	}
	if _, err := e.r.Create(ctx, Options{}); !errors.Is(err, ErrTarget) {
		t.Fatalf("no target: %v", err)
	}
}

// Ein Blob, der nicht zu seinem Namen passt, bricht die Sicherung ab; die
// Meldung nennt nur den Hash.
func TestCreateCorruptBlob(t *testing.T) {
	e := newEnv(t)
	p, _, err := e.blobs.Path(e.sums[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("kaputt "+canary), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = e.r.Create(context.Background(), Options{To: filepath.Join(t.TempDir(), "b")})
	if !errors.Is(err, ErrCorruptSource) || !strings.Contains(err.Error(), e.sums[0]) {
		t.Fatalf("err = %v", err)
	}
	mustNotLeak(t, "error", err.Error())
}

// untar entpackt einen Strom in einen Ordner (wie tar -x) und liefert die
// Namen in Reihenfolge.
func untar(t *testing.T, r io.Reader, dir string) []string {
	t.Helper()
	tr := tar.NewReader(r)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		p := filepath.Join(dir, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Mode != 0o700 {
				t.Errorf("%s: mode %o", h.Name, h.Mode)
			}
			if err := os.MkdirAll(p, 0o700); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			if h.Mode != 0o600 {
				t.Errorf("%s: mode %o", h.Name, h.Mode)
			}
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o600); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("%s: type %c", h.Name, h.Typeflag)
		}
	}
}

// Tar-Strom: ENCRYPT-ME.txt zuerst, manifest.json zuletzt, derselbe Aufbau
// wie ein Sicherungsordner (entpackt wiederherstellbar); der Schnappschuss
// lag nur kurz im Datenverzeichnis.
func TestCreateTarStream(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	var buf bytes.Buffer
	res, err := e.r.Create(ctx, Options{Tar: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "tar" || res.Target != "-" || res.Blobs != 3 || res.BlobsCopied != 3 {
		t.Fatalf("tar = %+v", res)
	}
	mustNotLeak(t, "audit", lastAudit(t, e.st, OpDone))
	dir := t.TempDir()
	names := untar(t, &buf, dir)
	if names[0] != EncryptNoteName || names[len(names)-1] != ManifestName {
		t.Fatalf("tar entries = %v", names)
	}
	if left, _ := os.ReadDir(filepath.Join(e.dir, snapshotDir)); len(left) != 0 {
		t.Fatalf("snapshot left in the data directory: %v", left)
	}
	r, err := Restore(ctx, RestoreOptions{From: dir, DataDir: filepath.Join(t.TempDir(), "neu")})
	if err != nil || r.Blobs != 3 {
		t.Fatalf("restore from tar = %+v %v", r, err)
	}
}

// backupOf legt eine Sicherung in einem frischen Ordner an.
func backupOf(t *testing.T, e *env) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "backup")
	if _, err := e.r.Create(context.Background(), Options{To: to}); err != nil {
		t.Fatal(err)
	}
	return to
}

// Wiederherstellung: Dateien wie im Datenverzeichnis (Rechte 0600/0700),
// Datenbank geöffnet und migriert, neue Kennung, Systemereignis mit
// Prüfsumme des Manifests und Ausweis der Quelle.
func TestRestore(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	to := filepath.Join(t.TempDir(), "neu", "data")
	// Trockenlauf: prüft alles, legt nichts an.
	if res, err := Restore(ctx, RestoreOptions{From: from, DataDir: to, DryRun: true}); err != nil || !res.DryRun || res.Blobs != 3 {
		t.Fatalf("dry run = %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Dir(to)); !os.IsNotExist(err) {
		t.Fatalf("dry run created something: %v", err)
	}
	res, err := Restore(ctx, RestoreOptions{From: from, DataDir: to, DBName: brand.Name + ".db"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Blobs != 3 || res.SchemaVersion != store.KnownSchemaVersion() || !res.Fixes.NewIdentity || res.Fixes.AuditStarts != 0 {
		t.Fatalf("restore = %+v", res)
	}
	for _, sum := range e.sums {
		bs, _ := blob.OpenExisting(filepath.Join(to, "blobs"), blob.Options{})
		if err := bs.Verify(sum, 0); err != nil {
			t.Fatalf("blob %s: %v", sum, err)
		}
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{to, filepath.Join(to, brand.Name+".db"), filepath.Join(to, "blobs")} {
			fi, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o600)
			if fi.IsDir() {
				want = 0o700
			}
			if fi.Mode().Perm() != want {
				t.Errorf("%s: %v", p, fi.Mode().Perm())
			}
		}
	}
	st, err := store.Open(filepath.Join(to, brand.Name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	orig, _ := e.st.DataDirIDFull(ctx)
	got, _ := st.DataDirIDFull(ctx)
	if got == orig || len(got) != 32 {
		t.Fatalf("datadir id %s (original %s)", got, orig)
	}
	params := lastAudit(t, st, OpRestore)
	var p map[string]any
	if json.Unmarshal([]byte(params), &p) != nil || p["manifest_sha256"] != res.ManifestSHA256 || p["source_hash"] != PathHash(from) ||
		p["new_identity"] != true || strings.Contains(params, from) {
		t.Fatalf("backup.restore = %s", params)
	}
	mustNotLeak(t, "audit", params)
	out, _ := json.Marshal(res)
	mustNotLeak(t, "result", string(out))
	var subject string
	_ = st.DBForTest().QueryRow(`SELECT subject FROM messages`).Scan(&subject)
	if subject != canary {
		t.Fatalf("restored message = %q", subject)
	}
}

// Manipulierte Sicherungen brechen ab, ohne dass etwas bleibt: geändertes
// Manifest (Größe, Prüfsumme, Zähler, Pfad außerhalb, unbekanntes Feld,
// neueres Schema), geänderter Blob, geänderte Datenbank, fehlende Datei.
func TestRestoreRejectsManipulation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	orig, err := os.ReadFile(filepath.Join(from, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, from)
	edit := func(f func(*Manifest)) []byte {
		c := m
		c.Files = append([]File(nil), m.Files...)
		f(&c)
		b, _ := json.MarshalIndent(c, "", "  ")
		return b
	}
	blobIdx, zstIdx := -1, -1
	for i, f := range m.Files {
		if f.Kind == KindBlob && !strings.HasSuffix(f.Path, ".zst") && blobIdx < 0 {
			blobIdx = i
		}
		if strings.HasSuffix(f.Path, ".zst") {
			zstIdx = i
		}
	}
	cases := map[string][]byte{
		"size":      edit(func(c *Manifest) { c.Files[blobIdx].Size++; c.Counts.Bytes++ }),
		"sha":       edit(func(c *Manifest) { c.Files[zstIdx].SHA256 = strings.Repeat("0", 64) }),
		"plain sha": edit(func(c *Manifest) { c.Files[blobIdx].SHA256 = strings.Repeat("0", 64) }),
		"counts":    edit(func(c *Manifest) { c.Counts.Blobs++ }),
		"traversal": edit(func(c *Manifest) { c.Files[blobIdx].Path = "../../etc/" + c.Files[blobIdx].Path }),
		"absolute":  edit(func(c *Manifest) { c.Files[blobIdx].Path = "/" + c.Files[blobIdx].Path }),
		"backslash": edit(func(c *Manifest) { c.Files[blobIdx].Path = strings.ReplaceAll(c.Files[blobIdx].Path, "/", `\`) }),
		"newer":     edit(func(c *Manifest) { c.SchemaVersion = store.KnownSchemaVersion() + 1 }),
		"program":   edit(func(c *Manifest) { c.Program = "fremd" }),
		"two dbs": edit(func(c *Manifest) {
			c.Files = append(c.Files, File{Path: brand.Name + "-20200101T000000Z.db", Kind: KindDatabase})
		}),
		"unknown":    bytes.Replace(orig, []byte(`"format"`), []byte(`"extra": 1, "format"`), 1),
		"not json":   []byte("{"),
		"wrong kind": edit(func(c *Manifest) { c.Files[blobIdx].Kind = KindAuditArchive }),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(from, ManifestName), b, 0o600); err != nil {
				t.Fatal(err)
			}
			to := filepath.Join(t.TempDir(), "neu")
			_, err := Restore(ctx, RestoreOptions{From: from, DataDir: to})
			if !errors.Is(err, ErrManifest) {
				t.Fatalf("err = %v", err)
			}
			if name == "backslash" && !strings.Contains(err.Error(), "backslashes") {
				t.Fatalf("backslash: %v", err)
			}
			mustNotLeak(t, "error", err.Error())
			if _, err := os.Stat(to); !os.IsNotExist(err) {
				t.Fatalf("target left behind: %v", err)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(from, ManifestName), orig, 0o600); err != nil {
		t.Fatal(err)
	}

	// Geänderte Dateien (gleiche Größe): Abbruch, Ziel wieder entfernt;
	// ein vorher leerer Ordner bleibt leer.
	flip := func(rel string) func() {
		p := filepath.Join(from, filepath.FromSlash(rel))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		c := append([]byte(nil), b...)
		c[len(c)-1] ^= 0x01
		if err := os.WriteFile(p, c, 0o600); err != nil {
			t.Fatal(err)
		}
		return func() { _ = os.WriteFile(p, b, 0o600) }
	}
	for _, rel := range []string{m.Files[blobIdx].Path, m.Files[zstIdx].Path, m.Database} {
		undo := flip(rel)
		if _, err := Restore(ctx, RestoreOptions{From: from, DataDir: filepath.Join(t.TempDir(), "x"), DryRun: true}); !errors.Is(err, ErrManifest) {
			t.Fatalf("%s changed, dry run: err = %v", rel, err)
		}
		empty := t.TempDir()
		_, err := Restore(ctx, RestoreOptions{From: from, DataDir: empty})
		if !errors.Is(err, ErrManifest) {
			t.Fatalf("%s changed: err = %v", rel, err)
		}
		mustNotLeak(t, "error", err.Error())
		if left, _ := os.ReadDir(empty); len(left) != 0 {
			t.Fatalf("%s changed: left %v", rel, left)
		}
		undo()
	}
	// Fehlende Datei.
	p := filepath.Join(from, filepath.FromSlash(m.Files[blobIdx].Path))
	b, _ := os.ReadFile(p)
	_ = os.Remove(p)
	if _, err := Restore(ctx, RestoreOptions{From: from, DataDir: filepath.Join(t.TempDir(), "x")}); !errors.Is(err, ErrManifest) {
		t.Fatalf("missing file: %v", err)
	}
	_ = os.WriteFile(p, b, 0o600)
	if _, err := Restore(ctx, RestoreOptions{From: from, DataDir: filepath.Join(t.TempDir(), "ok")}); err != nil {
		t.Fatalf("intact backup: %v", err)
	}
}

// Ziele: nicht leer, geschütztes Datenverzeichnis (auch leer), innerhalb
// der Sicherung, eine Datei; kein Manifest. Nichts wird geschrieben.
func TestRestoreRejectsTargets(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	full := t.TempDir()
	_ = os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o600)
	protected := filepath.Join(t.TempDir(), "standard")
	file := filepath.Join(t.TempDir(), "datei")
	_ = os.WriteFile(file, nil, 0o600)
	for name, o := range map[string]RestoreOptions{
		"not empty":      {From: from, DataDir: full},
		"protected":      {From: from, DataDir: protected, Refuse: []string{protected}},
		"protected leer": {From: from, DataDir: filepath.Join(protected, "."), Refuse: []string{protected + string(filepath.Separator)}},
		"inside backup":  {From: from, DataDir: filepath.Join(from, "neu")},
		"file":           {From: from, DataDir: file},
		"live data dir":  {From: from, DataDir: e.dir},
	} {
		_, err := Restore(ctx, o)
		if !errors.Is(err, ErrTarget) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := os.Stat(protected); !os.IsNotExist(err) {
		t.Fatal("protected directory created")
	}
	if left, _ := os.ReadDir(full); len(left) != 1 {
		t.Fatalf("non-empty target changed: %v", left)
	}
	if _, err := Restore(ctx, RestoreOptions{From: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "x")}); !errors.Is(err, ErrManifest) {
		t.Fatalf("no manifest: %v", err)
	}
}

// Portabilität: Eine unter Windows erzeugte Sicherung hat dieselben Pfade
// mit "/" (das Manifest kennt nur sie); Zeilenenden CRLF in Manifest und
// ENCRYPT-ME.txt ändern nichts. Rückwärtsschrägstriche lehnt restore ab
// (TestRestoreRejectsManipulation/backslash).
func TestRestoreWindowsBackup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	for _, name := range []string{ManifestName, EncryptNoteName} {
		p := filepath.Join(from, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Restore(ctx, RestoreOptions{From: from, DataDir: filepath.Join(t.TempDir(), "linux")})
	if err != nil || res.Blobs != 3 {
		t.Fatalf("restore = %+v %v", res, err)
	}
}

// PathHash ist stabil und unabhängig von der Schreibweise des Pfads.
func TestPathHash(t *testing.T) {
	d := t.TempDir()
	if PathHash(d) != PathHash(d+string(filepath.Separator)) || PathHash(d) == PathHash(d+"x") || len(PathHash(d)) != 16 {
		t.Fatal("PathHash")
	}
}
