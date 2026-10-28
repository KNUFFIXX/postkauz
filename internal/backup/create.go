package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// OpDone ist das Systemereignis nach einer Sicherung mit den Zählern
// (Dateien, Bytes, kopierte und vorhandene Blobs); der Aufruf selbst steht
// als backup.create im Audit, dessen Abschluss nur die Eingabe kennt.
const OpDone = "backup.done"

// EncryptNote ist der Inhalt von ENCRYPT-ME.txt (englisch, kurz;
// Akzeptanzkriterium 6).
const EncryptNote = `This folder is a backup of ` + brand.Name + `: your whole mail archive, unencrypted.
` + brand.Name + ` does not encrypt backups itself.

Keep it only on an encrypted drive or encrypt it before it leaves this computer:
  - an encrypted drive: BitLocker (Windows), LUKS (Linux), FileVault (macOS), VeraCrypt
  - or an encrypted archive: age, restic

Restore with: ` + brand.Name + ` restore --from <this folder> --data-dir <new folder>
`

// Warning ist die Warnung auf stderr nach jeder Sicherung.
const Warning = "the backup is not encrypted; keep it only on an encrypted drive (BitLocker, LUKS, VeraCrypt) " +
	"or encrypt it (age, restic); see " + EncryptNoteName

// Options steuert eine Sicherung.
type Options struct {
	// To ist der Sicherungsordner (neu, leer oder mit einer früheren
	// Sicherung dieses Programms). Leer, wenn Tar gesetzt ist.
	To string
	// Tar: statt eines Ordners ein Tar-Strom mit demselben Inhalt (für
	// age oder restic); Schnappschuss nur kurz im Datenverzeichnis.
	Tar io.Writer
	// DryRun zählt nur, was kopiert würde; nichts wird geschrieben.
	DryRun bool
	// CallAuditID ist der Start-Eintrag des Aufrufs (backup.done verweist
	// darauf).
	CallAuditID int64
}

// Result ist das Ergebnis einer Sicherung (Ausgabe von backup.create).
type Result struct {
	Mode           string `json:"mode"`   // folder | tar
	Target         string `json:"target"` // Ordner (absolut) bzw. "-" (stdout)
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	Database       string `json:"database"`
	SchemaVersion  int    `json:"schema_version"`
	ProgramVersion string `json:"program_version"`
	CreatedAt      string `json:"created_at"`
	Files          int    `json:"files"`
	Bytes          int64  `json:"bytes"`
	Blobs          int    `json:"blobs"`
	BlobsCopied    int    `json:"blobs_copied"`
	BlobsExisting  int    `json:"blobs_existing"`
	BytesCopied    int64  `json:"bytes_copied"`
	AuditArchives  int    `json:"audit_archives"`
	// SnapshotsRemoved: ältere Schnappschüsse im Ordner, die das neue
	// Manifest ersetzt hat.
	SnapshotsRemoved int    `json:"snapshots_removed"`
	EncryptNote      string `json:"encrypt_note"`
	Warning          string `json:"warning"`
	DryRun           bool   `json:"dry_run"`
}

// Runner sichert ein Datenverzeichnis.
type Runner struct {
	Store *store.SQLite
	// Blobs ist der Blob-Store des Datenverzeichnisses.
	Blobs *blob.Store
	// DataDir ist das Datenverzeichnis (Ziele darin sind verboten; der
	// Tar-Modus legt den Schnappschuss kurz dort ab; Archivdatenbanken
	// audit-archive-*.db liegen dort).
	DataDir string
	// Version ist die Programmversion (Manifest).
	Version string
	// Now ist in Tests ersetzbar.
	Now func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func targetErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTarget, fmt.Sprintf(format, args...))
}

// Create sichert das Datenverzeichnis in einen Ordner oder als Tar-Strom.
func (r *Runner) Create(ctx context.Context, o Options) (Result, error) {
	if (o.To == "") == (o.Tar == nil) {
		return Result{}, targetErr("give either a target folder or a tar stream")
	}
	res := Result{Warning: Warning, DryRun: o.DryRun, ProgramVersion: r.Version}
	var err error
	if o.Tar != nil {
		res, err = r.createTar(ctx, o, res)
	} else {
		res, err = r.createFolder(ctx, o, res)
	}
	if err != nil || o.DryRun {
		return res, err
	}
	// Zähler ins Audit (nie Pfade, nur der Ausweis des Ziels).
	params, _ := json.Marshal(map[string]any{"call_audit_id": o.CallAuditID, "mode": res.Mode, "target_hash": targetHash(res),
		"manifest_sha256": res.ManifestSHA256, "schema_version": res.SchemaVersion, "files": res.Files, "bytes": res.Bytes,
		"blobs": res.Blobs, "blobs_copied": res.BlobsCopied, "blobs_existing": res.BlobsExisting, "bytes_copied": res.BytesCopied,
		"audit_archives": res.AuditArchives})
	if aerr := r.Store.AuditWriter().Append(ctx, audit.Event{TS: r.now().UTC(), ActorType: audit.ActorSystem, KeyID: "system",
		Operation: OpDone, Result: audit.ResultOK, ParamsRedacted: params}); aerr != nil {
		return res, fmt.Errorf("backup: written, but the audit entry %s failed: %w", OpDone, aerr)
	}
	return res, nil
}

// targetHash: Ausweis des Ziels im Audit ("stdout" beim Tar-Strom).
func targetHash(res Result) string {
	if res.Mode == "tar" {
		return "stdout"
	}
	return PathHash(res.Target)
}

// prepareFolder prüft den Sicherungsordner und liest eine frühere Sicherung.
func (r *Runner) prepareFolder(to string) (string, map[string]File, error) {
	abs, err := filepath.Abs(to)
	if err != nil {
		return "", nil, targetErr("invalid target folder")
	}
	abs = filepath.Clean(abs)
	real, data := realPath(abs), realPath(r.DataDir)
	switch {
	case within(real, data):
		return "", nil, targetErr("the target folder %s lies inside the data directory; choose a folder outside it (best on another drive)", abs)
	case within(data, real):
		return "", nil, targetErr("the target folder %s contains the data directory; choose another folder", abs)
	}
	fi, err := os.Stat(abs)
	switch {
	case isNotExist(err):
		return abs, nil, nil
	case err != nil:
		return "", nil, err
	case !fi.IsDir():
		return "", nil, targetErr("%s is not a folder", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", nil, err
	}
	if len(entries) == 0 {
		return abs, nil, nil
	}
	b, err := os.ReadFile(filepath.Join(abs, ManifestName))
	if err != nil {
		return "", nil, targetErr("the target folder %s is not empty and holds no backup of %s; choose an empty or new folder", abs, brand.Name)
	}
	// Eine frühere Sicherung: nur Programm und Liste zählen (auch ein
	// Manifest mit älterem Schema darf fortgesetzt werden).
	var prev Manifest
	if json.Unmarshal(b, &prev) != nil || prev.Program != brand.Name {
		return "", nil, targetErr("the target folder %s is not empty and holds no backup of %s; choose an empty or new folder", abs, brand.Name)
	}
	known := map[string]File{}
	for _, f := range prev.Files {
		if kindOf(f.Path) != "" {
			known[f.Path] = f
		}
	}
	return abs, known, nil
}

// blobFile ist ein Blob des Datenverzeichnisses.
type blobFile struct {
	sum        string
	compressed bool
	rel        string
	src        string
}

// listBlobs liest den Blob-Store (nach dem Schnappschuss: jeder Blob, auf
// den der Schnappschuss zeigt, ist dann schon da; Put schreibt die Datei
// vor der Zeile). Temporärdateien und fremde Dateien bleiben weg.
func (r *Runner) listBlobs() ([]blobFile, error) {
	var out []blobFile
	err := r.Blobs.Scan(func(e blob.Entry) {
		if e.Kind != blob.EntryBlob {
			return
		}
		rel := blobRel(e.SHA256, e.Compressed)
		out = append(out, blobFile{sum: e.SHA256, compressed: e.Compressed, rel: rel,
			src: filepath.Join(r.Blobs.Root(), filepath.FromSlash(strings.TrimPrefix(rel, "blobs/")))})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, err
}

// listArchives liefert die Archivdatenbanken von audit.rotate im
// Datenverzeichnis (audit-archive-<von>-<bis>.db; generisch jede Datei, die
// archiveRE erfüllt).
func (r *Runner) listArchives() ([]string, error) {
	entries, err := os.ReadDir(r.DataDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && archiveRE.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// checkBlob prüft einen eben gelesenen Blob gegen seinen Namen:
// unkomprimiert über den SHA-256 der Bytes, komprimiert über
// blob.Store.Verify (liest die Quelle ein zweites Mal).
func (r *Runner) checkBlob(b blobFile, sum string) error {
	if !b.compressed {
		if sum != b.sum {
			return fmt.Errorf("%w: %s (run doctor --blobs=all)", ErrCorruptSource, b.sum)
		}
		return nil
	}
	if err := r.Blobs.Verify(b.sum, 0); err != nil {
		return fmt.Errorf("%w: %s (run doctor --blobs=all)", ErrCorruptSource, b.sum)
	}
	return nil
}

// createFolder: Sicherung in einen Ordner, inkrementell.
func (r *Runner) createFolder(ctx context.Context, o Options, res Result) (Result, error) {
	to, prev, err := r.prepareFolder(o.To)
	if err != nil {
		return res, err
	}
	res.Mode, res.Target = "folder", to
	res.Manifest, res.EncryptNote = filepath.Join(to, ManifestName), filepath.Join(to, EncryptNoteName)
	now := r.now().UTC()
	res.CreatedAt = now.Format(time.RFC3339)
	if res.SchemaVersion, err = r.Store.SchemaVersion(ctx); err != nil {
		return res, err
	}
	if o.DryRun {
		return r.dryRunFolder(to, prev, now, res)
	}

	if err := os.MkdirAll(to, dirMode); err != nil {
		return res, err
	}
	// Ein vorhandener Ordner (USB-Laufwerk mit FAT) kennt keine Rechte;
	// dann bleibt es bei den Rechten des Laufwerks.
	_ = os.Chmod(to, dirMode)
	if err := writeFileAtomic(res.EncryptNote, []byte(EncryptNote)); err != nil {
		return res, err
	}
	cleanTemps(to)

	// 1. Schnappschuss (konsistent, neben einem laufenden sync --watch).
	name := ""
	for n := 1; ; n++ {
		name = snapshotName(now, n)
		if _, err := os.Lstat(filepath.Join(to, name)); isNotExist(err) {
			break
		}
	}
	tmpDB := filepath.Join(to, "."+name+".tmp")
	_ = os.Remove(tmpDB)
	if err := r.Store.SnapshotTo(ctx, tmpDB); err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	dbSum, dbSize, err := hashFile(tmpDB)
	if err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	if err := os.Rename(tmpDB, filepath.Join(to, name)); err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	res.Database = name
	m := Manifest{Format: ManifestFormat, Program: brand.Name, ProgramVersion: r.Version, SchemaVersion: res.SchemaVersion,
		CreatedAt: res.CreatedAt, Database: name, Files: []File{{Path: name, Kind: KindDatabase, Size: dbSize, SHA256: dbSum}}}

	// 2. Blobs: nur fehlende kopieren.
	blobs, err := r.listBlobs()
	if err != nil {
		return res, err
	}
	for _, b := range blobs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		dst := filepath.Join(to, filepath.FromSlash(b.rel))
		f, copied, err := r.syncBlob(b, dst, prev)
		if err != nil {
			return res, err
		}
		if copied {
			res.BlobsCopied++
			res.BytesCopied += f.Size
		} else {
			res.BlobsExisting++
		}
		m.Files = append(m.Files, f)
	}

	// 3. Archivdatenbanken von audit.rotate.
	archives, err := r.listArchives()
	if err != nil {
		return res, err
	}
	for _, a := range archives {
		src, dst := filepath.Join(r.DataDir, a), filepath.Join(to, a)
		sum, size, err := hashFile(src)
		if err != nil {
			return res, err
		}
		if p, ok := prev[a]; !ok || p.SHA256 != sum || p.Size != size || !sameFile(dst, size) {
			got, n, err := copyAtomic(src, dst, nil)
			if err != nil {
				return res, err
			}
			if got != sum || n != size {
				return res, fmt.Errorf("backup: %s changed while it was copied; run backup create again", a)
			}
			res.BytesCopied += n
		}
		m.Files = append(m.Files, File{Path: a, Kind: KindAuditArchive, Size: size, SHA256: sum})
	}

	// 4. Manifest zuletzt: Erst mit ihm gilt der neue Stand.
	b, err := m.encode()
	if err != nil {
		return res, err
	}
	if err := writeFileAtomic(res.Manifest, b); err != nil {
		return res, err
	}
	sum := sha256.Sum256(b)
	res.ManifestSHA256 = hex.EncodeToString(sum[:])
	res.Files, res.Bytes, res.Blobs, res.AuditArchives = m.Counts.Files, m.Counts.Bytes, m.Counts.Blobs, m.Counts.AuditArchives

	// 5. Ältere Schnappschüsse gehören zu keinem Manifest mehr.
	if entries, err := os.ReadDir(to); err == nil {
		for _, e := range entries {
			if e.Name() != name && snapshotRE.MatchString(e.Name()) && e.Type().IsRegular() {
				if os.Remove(filepath.Join(to, e.Name())) == nil {
					res.SnapshotsRemoved++
				}
			}
		}
	}
	return res, nil
}

// sameFile: dst ist eine gewöhnliche Datei der Größe size.
func sameFile(dst string, size int64) bool {
	n, err := regularFile(dst)
	return err == nil && n == size
}

// syncBlob sorgt dafür, dass der Blob im Ziel liegt. Vorhanden und
// unverändert heißt: gleiche Größe und schon im vorigen Manifest (dessen
// Prüfsumme gilt weiter, die Datei wird nicht gelesen), oder eine Datei
// ohne Eintrag (abgebrochener Lauf), deren Inhalt zum Blob passt.
func (r *Runner) syncBlob(b blobFile, dst string, prev map[string]File) (File, bool, error) {
	if size, err := regularFile(dst); err == nil {
		if p, ok := prev[b.rel]; ok && p.Size == size {
			return p, false, nil
		}
		sum, n, err := hashFile(dst)
		if err != nil {
			return File{}, false, err
		}
		ok := !b.compressed && sum == b.sum
		if b.compressed {
			srcSum, _, err := hashFile(b.src)
			if err != nil {
				return File{}, false, err
			}
			ok = srcSum == sum
		}
		if ok {
			return File{Path: b.rel, Kind: KindBlob, Size: n, SHA256: sum}, false, nil
		}
	}
	var check func(string, int64) error
	if !b.compressed {
		check = func(sum string, _ int64) error { return r.checkBlob(b, sum) }
	}
	sum, n, err := copyAtomic(b.src, dst, check)
	if err != nil {
		return File{}, false, err
	}
	if b.compressed {
		if err := r.checkBlob(b, sum); err != nil {
			_ = os.Remove(dst)
			return File{}, false, err
		}
	}
	return File{Path: b.rel, Kind: KindBlob, Size: n, SHA256: sum}, true, nil
}

// dryRunFolder zählt, was eine Sicherung in den Ordner kopieren würde.
func (r *Runner) dryRunFolder(to string, prev map[string]File, now time.Time, res Result) (Result, error) {
	res.Database = snapshotName(now, 1)
	res.Files = 1
	if fi, err := os.Stat(r.Store.Path()); err == nil {
		res.Bytes = fi.Size() // Schätzung: der Schnappschuss ist meist etwas kleiner
	}
	blobs, err := r.listBlobs()
	if err != nil {
		return res, err
	}
	for _, b := range blobs {
		size, err := regularFile(b.src)
		if err != nil {
			return res, err
		}
		res.Files++
		res.Blobs++
		res.Bytes += size
		if n, err := regularFile(filepath.Join(to, filepath.FromSlash(b.rel))); err == nil && n == size {
			res.BlobsExisting++
			continue
		}
		res.BlobsCopied++
		res.BytesCopied += size
	}
	archives, err := r.listArchives()
	if err != nil {
		return res, err
	}
	for _, a := range archives {
		size, err := regularFile(filepath.Join(r.DataDir, a))
		if err != nil {
			return res, err
		}
		res.Files++
		res.AuditArchives++
		res.Bytes += size
		if p, ok := prev[a]; !ok || p.Size != size {
			res.BytesCopied += size
		}
	}
	return res, nil
}

// tmpNameRE: liegen gebliebene Temporärdateien eines abgebrochenen Laufs.
var tmpNameRE = regexp.MustCompile(`^\.(tmp-[0-9a-f]{12}|.+\.db\.tmp)$`)

// cleanTemps entfernt Temporärdateien eines abgebrochenen Laufs im
// Sicherungsordner (nur im Hauptordner; unter blobs/ überschreibt die
// nächste Kopie sie nicht, sie sind unschädlich).
func cleanTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Type().IsRegular() && tmpNameRE.MatchString(e.Name()) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// snapshotDir ist der Ordner für den kurzen Schnappschuss des Tar-Modus im
// Datenverzeichnis (nie eine Klartextkopie außerhalb davon).
const snapshotDir = "backup-tmp"

// staleSnapshotAge: Ältere Schnappschüsse in snapshotDir stammen von einem
// abgebrochenen Lauf und werden entfernt.
const staleSnapshotAge = 24 * time.Hour

// createTar: Sicherung als Tar-Strom (für age oder restic). Der
// Schnappschuss der Datenbank liegt nur während des Laufs unter
// <Datenverzeichnis>/backup-tmp und wird danach gelöscht.
func (r *Runner) createTar(ctx context.Context, o Options, res Result) (Result, error) {
	res.Mode, res.Target, res.Manifest, res.EncryptNote = "tar", "-", ManifestName, EncryptNoteName
	now := r.now().UTC()
	res.CreatedAt = now.Format(time.RFC3339)
	var err error
	if res.SchemaVersion, err = r.Store.SchemaVersion(ctx); err != nil {
		return res, err
	}
	if o.DryRun {
		return r.dryRunFolder(filepath.Join(r.DataDir, snapshotDir, "dry-run-no-target"), nil, now, res)
	}
	sdir := filepath.Join(r.DataDir, snapshotDir)
	if err := os.MkdirAll(sdir, dirMode); err != nil {
		return res, err
	}
	if entries, err := os.ReadDir(sdir); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && e.Type().IsRegular() && now.Sub(info.ModTime()) > staleSnapshotAge {
				_ = os.Remove(filepath.Join(sdir, e.Name()))
			}
		}
	}
	tag, err := randHex(6)
	if err != nil {
		return res, err
	}
	snap := filepath.Join(sdir, "snapshot-"+tag+".db")
	defer os.Remove(snap)
	if err := r.Store.SnapshotTo(ctx, snap); err != nil {
		return res, err
	}
	name := snapshotName(now, 1)
	res.Database = name
	m := Manifest{Format: ManifestFormat, Program: brand.Name, ProgramVersion: r.Version, SchemaVersion: res.SchemaVersion,
		CreatedAt: res.CreatedAt, Database: name}

	tw := tar.NewWriter(o.Tar)
	tarw := &tarWriter{tw: tw, now: now, dirs: map[string]bool{}}
	if err := tarw.bytes(EncryptNoteName, []byte(EncryptNote)); err != nil {
		return res, err
	}
	sum, size, err := tarw.file(name, snap)
	if err != nil {
		return res, err
	}
	m.Files = append(m.Files, File{Path: name, Kind: KindDatabase, Size: size, SHA256: sum})
	_ = os.Remove(snap)

	archives, err := r.listArchives()
	if err != nil {
		return res, err
	}
	for _, a := range archives {
		sum, size, err := tarw.file(a, filepath.Join(r.DataDir, a))
		if err != nil {
			return res, err
		}
		res.BytesCopied += size
		m.Files = append(m.Files, File{Path: a, Kind: KindAuditArchive, Size: size, SHA256: sum})
	}
	blobs, err := r.listBlobs()
	if err != nil {
		return res, err
	}
	for _, b := range blobs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		sum, size, err := tarw.file(b.rel, b.src)
		if err != nil {
			return res, err
		}
		if err := r.checkBlob(b, sum); err != nil {
			return res, err
		}
		res.BlobsCopied++
		res.BytesCopied += size
		m.Files = append(m.Files, File{Path: b.rel, Kind: KindBlob, Size: size, SHA256: sum})
	}
	b, err := m.encode()
	if err != nil {
		return res, err
	}
	if err := tarw.bytes(ManifestName, b); err != nil {
		return res, err
	}
	if err := tw.Close(); err != nil {
		return res, err
	}
	h := sha256.Sum256(b)
	res.ManifestSHA256 = hex.EncodeToString(h[:])
	res.Files, res.Bytes, res.Blobs, res.AuditArchives = m.Counts.Files, m.Counts.Bytes, m.Counts.Blobs, m.Counts.AuditArchives
	return res, nil
}

// tarWriter schreibt Einträge mit festen Rechten (Ordner 0700, Dateien
// 0600) und legt Elternordner einmal an.
type tarWriter struct {
	tw   *tar.Writer
	now  time.Time
	dirs map[string]bool
}

func (t *tarWriter) parents(name string) error {
	dir := ""
	parts := strings.Split(name, "/")
	for _, p := range parts[:len(parts)-1] {
		dir += p + "/"
		if t.dirs[dir] {
			continue
		}
		t.dirs[dir] = true
		if err := t.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: dir, Mode: int64(dirMode), ModTime: t.now}); err != nil {
			return err
		}
	}
	return nil
}

func (t *tarWriter) bytes(name string, b []byte) error {
	if err := t.parents(name); err != nil {
		return err
	}
	if err := t.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: int64(fileMode), Size: int64(len(b)), ModTime: t.now}); err != nil {
		return err
	}
	_, err := t.tw.Write(b)
	return err
}

// file schreibt eine Datei des Datenverzeichnisses und liefert SHA-256 und
// Größe der geschriebenen Bytes.
func (t *tarWriter) file(name, src string) (string, int64, error) {
	size, err := regularFile(src)
	if err != nil {
		return "", 0, err
	}
	f, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	if err := t.parents(name); err != nil {
		return "", 0, err
	}
	if err := t.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: int64(fileMode), Size: size, ModTime: t.now}); err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(t.tw, io.TeeReader(io.LimitReader(f, size), h))
	if err != nil {
		return "", 0, err
	}
	if n != size {
		return "", 0, errors.New("backup: " + name + " changed while it was written; run backup create again")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
