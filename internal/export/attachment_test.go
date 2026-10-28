package export

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func opener(data []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

var pdfBytes = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")

func save(t *testing.T, dir string, r SaveRequest) SaveResult {
	t.Helper()
	r.ExportDir = dir
	if r.Open == nil {
		r.Open = opener(pdfBytes)
	}
	res, err := SaveAttachment(r)
	if err != nil {
		t.Fatalf("SaveAttachment(%+v): %v", r, err)
	}
	return res
}

// Akzeptanz S3.3 Punkt 3: nur unter export_dir, Namensdoppel → "-2",
// reservierter Name bereinigt, Dateien 0600, Ordner 0700.
func TestSaveAttachmentNamesAndRights(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	r1 := save(t, dir, SaveRequest{Filename: "Rechnung.pdf", Subdir: "belege/2026", SHA256: sum(pdfBytes)})
	r2 := save(t, dir, SaveRequest{Filename: "Rechnung.pdf", Subdir: "belege/2026"})
	r3 := save(t, dir, SaveRequest{Filename: "RECHNUNG.pdf", Subdir: "belege/2026"})
	if r1.Path != "belege/2026/Rechnung.pdf" || r2.Path != "belege/2026/Rechnung-2.pdf" || r1.Blocked {
		t.Fatalf("paths %q %q", r1.Path, r2.Path)
	}
	if runtime.GOOS == "linux" && r3.Path != "belege/2026/RECHNUNG.pdf" {
		t.Fatalf("case-sensitive file system: %q", r3.Path)
	}
	if r1.Size != int64(len(pdfBytes)) || r1.SHA256 != sum(pdfBytes) {
		t.Fatalf("size/hash = %d %s", r1.Size, r1.SHA256)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(r2.Path)))
	if err != nil || !bytes.Equal(got, pdfBytes) {
		t.Fatalf("content: %v", err)
	}
	if res := save(t, dir, SaveRequest{Filename: "con.pdf"}); res.Path != "_con.pdf" {
		t.Fatalf("reserved name: %q", res.Path)
	}
	if res := save(t, dir, SaveRequest{Filename: "  ", Fallback: "attachment-7.pdf"}); res.Path != "attachment-7.pdf" {
		t.Fatalf("fallback: %q", res.Path)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "belege"): 0o700,
			filepath.Join(dir, "belege", "2026"): 0o700, filepath.Join(dir, "belege", "2026", "Rechnung.pdf"): 0o600} {
			fi, err := os.Stat(p)
			if err != nil || fi.Mode().Perm() != want {
				t.Errorf("%s: %v %v, want %v", p, fi.Mode().Perm(), err, want)
			}
		}
	}
}

// Ausführbare Typen bekommen ".blocked": nach Endung (auch mit Tricks),
// nach Inhalt (MZ unter PDF-Namen) und nach dem gespeicherten Befund.
func TestSaveAttachmentBlocked(t *testing.T) {
	dir := t.TempDir()
	mz := append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{0}, 200)...)
	cases := []struct {
		req  SaveRequest
		want string
	}{
		{SaveRequest{Filename: "setup.exe", Open: opener(mz)}, "setup.exe.blocked"},
		{SaveRequest{Filename: "setup.exe", Open: opener(mz)}, "setup-2.exe.blocked"},
		{SaveRequest{Filename: "Rechnung.pdf", DeclaredType: "application/pdf", Open: opener(mz)}, "Rechnung.pdf.blocked"},
		{SaveRequest{Filename: "start.bat.", Open: opener([]byte("echo hi\r\n"))}, "start.bat.blocked"},
		{SaveRequest{Filename: "x.bat::$DATA", Open: opener([]byte("echo hi\r\n"))}, "x.bat__$DATA.blocked"},
		{SaveRequest{Filename: "rechnung\u202efdp.exe", Open: opener([]byte("text"))}, "rechnungfdp.exe.blocked"},
		{SaveRequest{Filename: "x.exe\u202e", Open: opener([]byte("text"))}, "x.exe.blocked"},
		{SaveRequest{Filename: "harmlos.txt", Executable: true, Open: opener([]byte("text"))}, "harmlos.txt.blocked"},
		{SaveRequest{Filename: "notiz.txt", Open: opener([]byte("text"))}, "notiz.txt"},
	}
	for _, c := range cases {
		res := save(t, dir, c.req)
		if res.Path != c.want || res.Blocked != (filepath.Ext(c.want) == BlockedSuffix) {
			t.Errorf("%q: %q blocked=%v, want %q", c.req.Filename, res.Path, res.Blocked, c.want)
		}
	}
}

// ".." und absolute Pfade → ErrOutside, ein Symlink nach außen ebenso;
// nichts wird außerhalb angelegt.
func TestSaveAttachmentStaysInside(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "export")
	outside := filepath.Join(base, "draussen")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"..", "../draussen", "a/../../draussen", "/tmp", `C:\Windows`} {
		_, err := SaveAttachment(SaveRequest{ExportDir: dir, Subdir: sub, Filename: "x.pdf", Open: opener(pdfBytes)})
		if !errors.Is(err, ErrOutside) {
			t.Errorf("subdir %q: %v, want ErrOutside", sub, err)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	for _, dry := range []bool{true, false} {
		for _, sub := range []string{"link", "link/tiefer"} {
			_, err := SaveAttachment(SaveRequest{ExportDir: dir, Subdir: sub, Filename: "x.pdf", Open: opener(pdfBytes), DryRun: dry})
			if !errors.Is(err, ErrOutside) {
				t.Errorf("symlink %q (dry %v): %v, want ErrOutside", sub, dry, err)
			}
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("written outside: %v", entries)
	}
	// Ein Symlink als Datei (Name schon vergeben) wird nie überschrieben.
	if err := os.Symlink(filepath.Join(outside, "ziel.pdf"), filepath.Join(dir, "falle.pdf")); err != nil {
		t.Fatal(err)
	}
	if res := save(t, dir, SaveRequest{Filename: "falle.pdf"}); res.Path != "falle-2.pdf" {
		t.Fatalf("dangling symlink: %q", res.Path)
	}
	if _, err := os.Lstat(filepath.Join(outside, "ziel.pdf")); err == nil {
		t.Fatal("followed the symlink")
	}
}

// Trockenlauf: nennt den Namen, den es gäbe, und legt nichts an.
func TestSaveAttachmentDryRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	res := save(t, dir, SaveRequest{Filename: "a.pdf", Subdir: "neu", DryRun: true})
	if res.Path != "neu/a.pdf" {
		t.Fatalf("dry run: %q", res.Path)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("dry run created export_dir")
	}
	save(t, dir, SaveRequest{Filename: "a.pdf"})
	if res := save(t, dir, SaveRequest{Filename: "a.pdf", DryRun: true}); res.Path != "a-2.pdf" {
		t.Fatalf("dry run with existing file: %q", res.Path)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("dry run wrote: %v", entries)
	}
}

// Ein Blob, der nicht zu seinem Hash passt, wird nicht abgelegt.
func TestSaveAttachmentDamaged(t *testing.T) {
	dir := t.TempDir()
	_, err := SaveAttachment(SaveRequest{ExportDir: dir, Filename: "a.pdf", SHA256: sum([]byte("anders")), Open: opener(pdfBytes)})
	if !errors.Is(err, ErrDamaged) {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("written: %v", entries)
	}
}
