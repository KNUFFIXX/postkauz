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
	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// LockName ist die Sperrdatei einer laufenden Sicherung im
// Datenverzeichnis (internal/flock; bleibt leer liegen).
const LockName = "backup.lock"

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
	// AuditArchivesSkipped: eingetragene Archive, die fehlen oder nicht
	// mehr zu ihrer Prüfsumme passen ("<name> (archive_missing)" bzw.
	// "(archive_changed)"); sie sind nicht in der Sicherung (Warnung).
	AuditArchivesSkipped []string `json:"audit_archives_skipped,omitempty"`
	// MissingBlobs: Hashes der Blobs, auf die die Datenbank verweist, die
	// aber schon vor der Sicherung im Blob-Store fehlten (etwa in der
	// Quarantäne eines Virenscanners); sie sind nicht in der Sicherung und
	// stehen auch im Manifest (Warnung; T3-Nachbesserung 2, Punkt 4).
	MissingBlobs []string `json:"missing_blobs,omitempty"`
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
	// Tar-Modus legt den Schnappschuss kurz dort ab; die Sperre LockName
	// liegt dort). Archive von audit.rotate findet Store.AuditArchivePath.
	DataDir string
	// Version ist die Programmversion (Manifest).
	Version string
	// Now ist in Tests ersetzbar.
	Now func() time.Time
	// hook ist ein Test-Haken: Er läuft an benannten Stellen einer
	// Sicherung (stage "snapshot-taken": Schnappschuss geschrieben, Abgleich
	// mit dem Blob-Store folgt; "snapshot": Schnappschuss liegt, Blobs
	// folgen).
	hook func(stage string)
}

func (r *Runner) at(stage string) {
	if r.hook != nil {
		r.hook(stage)
	}
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
	// Eine Sicherung je Datenverzeichnis (T3-Nachbesserung 5a): Zwei Läufe
	// in denselben Ordner löschten gegenseitig Schnappschuss und
	// Temporärdateien. Die Sperre liegt im Datenverzeichnis, nicht im Ziel
	// (sie gilt so auch für den Tar-Strom und für verschiedene Ziele). Ein
	// Trockenlauf schreibt nichts und braucht sie nicht.
	if !o.DryRun {
		lk, err := flock.TryLock(filepath.Join(r.DataDir, LockName))
		switch {
		case errors.Is(err, flock.ErrLocked):
			return res, ErrBusy
		case err != nil:
			return res, fmt.Errorf("backup: lock: %w", err)
		}
		defer lk.Release()
	}
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
		"audit_archives": res.AuditArchives, "audit_archives_skipped": len(res.AuditArchivesSkipped),
		"missing_blobs": len(res.MissingBlobs)})
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
	case within(real, data), insideByID(abs, r.DataDir):
		return "", nil, targetErr("the target folder %s lies inside the data directory; choose a folder outside it (best on another drive)", abs)
	case within(data, real), insideByID(r.DataDir, abs):
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
	if len(entries) == 0 || onlyLeftovers(entries) {
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

// onlyLeftovers meldet, ob ein Ordner ohne Manifest nur Reste einer
// abgebrochenen ersten Sicherung enthält (T3-Nachbesserung 2, Nebenfund
// N1): ENCRYPT-ME.txt und Temporärdateien (tmpNameRE), dazu, wenn
// ENCRYPT-ME.txt dabei ist, Schnappschüsse (snapshotRE) und den Ordner
// blobs (Abbruch während des Kopierens). Ein solcher Ordner gilt als leer;
// sonst wiese jeder weitere Lauf ihn ab („holds no backup“), obwohl die
// Anleitung sagt, die Sicherung einfach noch einmal zu starten. Der nächste
// Lauf räumt die Temporärdateien und den alten Schnappschuss ab und prüft
// vorhandene Blob-Dateien gegen ihren Inhalt (syncBlob).
func onlyLeftovers(entries []os.DirEntry) bool {
	note, more := false, false
	for _, e := range entries {
		n := e.Name()
		switch {
		case n == EncryptNoteName && e.Type().IsRegular():
			note = true
		case tmpNameRE.MatchString(n) && e.Type().IsRegular():
		case snapshotRE.MatchString(n) && e.Type().IsRegular(), n == "blobs" && e.IsDir():
			more = true
		default:
			return false
		}
	}
	return note || !more
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

// blobsBefore hält fest, was vor dem Schnappschuss galt (T3-Nachbesserung
// 2, Punkt 4): die Blob-Verweise der Datenbank und danach die vorhandenen
// Blob-Dateien. Ein Blob, auf den schon vorher verwiesen wurde und dessen
// Datei schon vorher fehlte, ist kein Fehler dieser Sicherung (Quarantäne
// eines Virenscanners, verlorene Datei); er blockiert sie deshalb nicht.
type blobsBefore struct {
	refs  map[string]bool
	files map[string]bool
}

// blobsBeforeSnapshot liest erst die Verweise, dann den Blob-Store (Put
// schreibt die Datei vor der Zeile: Was hier verwiesen ist und fehlt,
// fehlte schon, bevor die Sicherung begann).
func (r *Runner) blobsBeforeSnapshot(ctx context.Context) (blobsBefore, error) {
	refs, err := r.Store.BlobReferences(ctx)
	if err != nil {
		return blobsBefore{}, err
	}
	list, err := r.listBlobs()
	if err != nil {
		return blobsBefore{}, err
	}
	return blobsBefore{refs: refs, files: blobSet(list)}, nil
}

func blobSet(blobs []blobFile) map[string]bool {
	have := make(map[string]bool, len(blobs))
	for _, b := range blobs {
		have[b.sum] = true
	}
	return have
}

// checkComplete prüft, dass jeder Blob, auf den der Schnappschuss
// verweist (refs), unter den gelesenen Blobs ist (T3-Nachbesserung 5b):
// listBlobs liest nur, was gerade da ist; laufen zwischen Schnappschuss und
// Verzeichnislauf purge und blob gc, fehlte ein Blob sonst still. Werte, die
// kein gültiger Hash sind, können keine Datei sein und zählen nicht
// (doctor --blobs meldet sie).
//
// Seit der T3-Nachbesserung 2 (Punkt 4) bricht die Sicherung nur noch für
// Blobs ab, die während der Sicherung verschwunden sind. Fehlte ein Blob
// schon vorher (before), kommt er sortiert in missing: Die Sicherung läuft
// mit Warnung weiter und hält ihn im Manifest fest (missing_blobs); sonst
// blockierte eine einzige verlorene Datei jede weitere Sicherung.
func checkComplete(refs map[string]bool, blobs []blobFile, before blobsBefore) (missing []string, err error) {
	have := blobSet(blobs)
	var vanished []string
	for h := range refs {
		if !blob.ValidHash(h) || have[h] {
			continue
		}
		if before.refs[h] && !before.files[h] {
			missing = append(missing, h)
		} else {
			vanished = append(vanished, h)
		}
	}
	sort.Strings(missing)
	if len(vanished) == 0 {
		return missing, nil
	}
	sort.Strings(vanished)
	return nil, fmt.Errorf("%w: %d blob(s) the database refers to disappeared from the blob store during the backup (first: %s)",
		ErrIncomplete, len(vanished), vanished[0])
}

// archiveFile ist ein Archiv von audit.rotate, das in die Sicherung kommt:
// Name, Pfad im Datenverzeichnis, Prüfsumme und Größe wie eingetragen bzw.
// eben gelesen.
type archiveFile struct {
	name string
	src  string
	sum  string
	size int64
}

// Probleme eines eingetragenen Archivs (dieselben Codes wie audit verify).
const (
	archiveMissing = "archive_missing"
	archiveChanged = "archive_changed"
)

// listArchives liefert die Archive von audit.rotate für die Sicherung
// (T3-Nachbesserung 3b): nur die in list eingetragenen (audit_archives des
// Schnappschusses bzw. beim Trockenlauf der Datenbank), nie eine Datei, die
// nur nach ihrem Namen aussieht (Rest eines hart abgebrochenen audit
// rotate). Jede Datei wird gelesen und mit der eingetragenen Prüfsumme
// verglichen; fehlt sie oder weicht sie ab, bleibt sie draußen und steht in
// skipped ("<name> (archive_missing|archive_changed)"). Festlegung: Die
// Sicherung der Mails bricht deswegen nicht ab (sonst schlüge jede weitere
// Sicherung fehl, denn audit_archives ist nur anfügbar); audit verify meldet
// denselben Befund.
func (r *Runner) listArchives(list []store.AuditArchive) (out []archiveFile, skipped []string, err error) {
	for _, a := range list {
		if !archiveRE.MatchString(a.File) {
			skipped = append(skipped, a.File+" ("+archiveChanged+")")
			continue
		}
		src := r.Store.AuditArchivePath(a.File)
		if _, err := regularFile(src); isNotExist(err) {
			skipped = append(skipped, a.File+" ("+archiveMissing+")")
			continue
		}
		sum, size, err := hashFile(src)
		switch {
		case isNotExist(err):
			skipped = append(skipped, a.File+" ("+archiveMissing+")")
			continue
		case err != nil:
			return nil, nil, err
		case sum != a.SHA256:
			skipped = append(skipped, a.File+" ("+archiveChanged+")")
			continue
		}
		out = append(out, archiveFile{name: a.File, src: src, sum: sum, size: size})
	}
	return out, skipped, nil
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
		return r.dryRunFolder(ctx, to, prev, now, res)
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
	before, err := r.blobsBeforeSnapshot(ctx)
	if err != nil {
		return res, err
	}
	if err := r.Store.SnapshotTo(ctx, tmpDB); err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	dbSum, dbSize, err := hashFile(tmpDB)
	if err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	r.at("snapshot-taken")
	// 2. Verweise des Schnappschusses gegen den Blob-Store, bevor er an
	// seinen Platz kommt: Verschwindet ein Blob während der Sicherung,
	// bleibt die frühere Sicherung im Ordner unberührt.
	contents, err := store.ReadSnapshot(ctx, tmpDB)
	if err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	blobs, err := r.listBlobs()
	if err == nil {
		res.MissingBlobs, err = checkComplete(contents.Blobs, blobs, before)
	}
	if err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	if err := os.Rename(tmpDB, filepath.Join(to, name)); err != nil {
		_ = os.Remove(tmpDB)
		return res, err
	}
	res.Database = name
	r.at("snapshot")
	m := Manifest{Format: ManifestFormat, Program: brand.Name, ProgramVersion: r.Version, SchemaVersion: res.SchemaVersion,
		CreatedAt: res.CreatedAt, Database: name, MissingBlobs: res.MissingBlobs,
		Files: []File{{Path: name, Kind: KindDatabase, Size: dbSize, SHA256: dbSum}}}

	// 3. Blobs: nur fehlende kopieren.
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

	// 4. Archive von audit.rotate, die der Schnappschuss kennt.
	archives, skipped, err := r.listArchives(contents.Archives)
	if err != nil {
		return res, err
	}
	res.AuditArchivesSkipped = skipped
	for _, a := range archives {
		dst := filepath.Join(to, a.name)
		if p, ok := prev[a.name]; !ok || p.SHA256 != a.sum || p.Size != a.size || !sameFile(dst, a.size) {
			got, n, err := copyAtomic(a.src, dst, nil)
			if err != nil {
				return res, err
			}
			if got != a.sum || n != a.size {
				return res, fmt.Errorf("backup: %s changed while it was copied; run backup create again", a.name)
			}
			res.BytesCopied += n
		}
		m.Files = append(m.Files, File{Path: a.name, Kind: KindAuditArchive, Size: a.size, SHA256: a.sum})
	}

	// 5. Manifest zuletzt: Erst mit ihm gilt der neue Stand.
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

	// 6. Ältere Schnappschüsse gehören zu keinem Manifest mehr.
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
// Blobs, auf die die Datenbank verweist und die fehlen, nennt er wie die
// Sicherung in MissingBlobs (erst die Verweise, dann das Verzeichnis: Put
// schreibt die Datei vor der Zeile; ohne Schnappschuss fehlten sie alle
// schon vorher).
func (r *Runner) dryRunFolder(ctx context.Context, to string, prev map[string]File, now time.Time, res Result) (Result, error) {
	res.Database = snapshotName(now, 1)
	res.Files = 1
	if fi, err := os.Stat(r.Store.Path()); err == nil {
		res.Bytes = fi.Size() // Schätzung: der Schnappschuss ist meist etwas kleiner
	}
	refs, err := r.Store.BlobReferences(ctx)
	if err != nil {
		return res, err
	}
	blobs, err := r.listBlobs()
	if err != nil {
		return res, err
	}
	if res.MissingBlobs, err = checkComplete(refs, blobs, blobsBefore{refs: refs, files: blobSet(blobs)}); err != nil {
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
	list, err := r.Store.AuditArchives(ctx)
	if err != nil {
		return res, err
	}
	archives, skipped, err := r.listArchives(list)
	if err != nil {
		return res, err
	}
	res.AuditArchivesSkipped = skipped
	for _, a := range archives {
		res.Files++
		res.AuditArchives++
		res.Bytes += a.size
		if p, ok := prev[a.name]; !ok || p.SHA256 != a.sum || p.Size != a.size {
			res.BytesCopied += a.size
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
		return r.dryRunFolder(ctx, filepath.Join(r.DataDir, snapshotDir, "dry-run-no-target"), nil, now, res)
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
	before, err := r.blobsBeforeSnapshot(ctx)
	if err != nil {
		return res, err
	}
	if err := r.Store.SnapshotTo(ctx, snap); err != nil {
		return res, err
	}
	r.at("snapshot-taken")
	r.at("snapshot")
	// Verweise des Schnappschusses gegen den Blob-Store, bevor das erste
	// Byte in den Strom geht (wie im Ordner-Modus).
	contents, err := store.ReadSnapshot(ctx, snap)
	if err != nil {
		return res, err
	}
	blobs, err := r.listBlobs()
	if err != nil {
		return res, err
	}
	if res.MissingBlobs, err = checkComplete(contents.Blobs, blobs, before); err != nil {
		return res, err
	}
	archives, skipped, err := r.listArchives(contents.Archives)
	if err != nil {
		return res, err
	}
	res.AuditArchivesSkipped = skipped
	name := snapshotName(now, 1)
	res.Database = name
	m := Manifest{Format: ManifestFormat, Program: brand.Name, ProgramVersion: r.Version, SchemaVersion: res.SchemaVersion,
		CreatedAt: res.CreatedAt, Database: name, MissingBlobs: res.MissingBlobs}

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

	for _, a := range archives {
		sum, size, err := tarw.file(a.name, a.src)
		if err != nil {
			return res, err
		}
		if sum != a.sum || size != a.size {
			return res, fmt.Errorf("backup: %s changed while it was written; run backup create again", a.name)
		}
		res.BytesCopied += size
		m.Files = append(m.Files, File{Path: a.name, Kind: KindAuditArchive, Size: size, SHA256: sum})
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
