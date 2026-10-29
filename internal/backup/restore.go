package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// OpRestore ist das Systemereignis in der wiederhergestellten Datenbank
// (E7, A10): Quelle als Ausweis (PathHash), Prüfsumme des Manifests,
// Zähler und was die neue Instanz geschlossen hat.
const OpRestore = "backup.restore"

// RestoreOptions steuert eine Wiederherstellung.
type RestoreOptions struct {
	// From ist der Sicherungsordner (bei einem Tar-Strom: der Ordner, in
	// den er entpackt wurde).
	From string
	// DataDir ist das neue Datenverzeichnis: Es darf nicht existieren oder
	// muss leer sein.
	DataDir string
	// Refuse sind Datenverzeichnisse, die nie Ziel sein dürfen (das
	// Standard-Datenverzeichnis und das aus Konfiguration und Umgebung),
	// auch wenn sie leer sind; ebenso kein Ordner darin (etwa ein Profil).
	Refuse []string
	// DryRun prüft nur: Manifest, Ziel und jede Datei (SHA-256, komprimierte
	// Blobs auch gegen ihren Namen); nichts wird angelegt.
	DryRun bool
	// KeepIdentity behält die Kennung des Originals (meta.datadir_id) und
	// damit die Schlüsselbund-Verweise der Konten (A10: Standard ist eine
	// neue Kennung).
	KeepIdentity bool
	// DBName ist der Dateiname der Datenbank im Datenverzeichnis.
	DBName string
	// Now ist in Tests ersetzbar.
	Now func() time.Time
}

// RestoreResult ist das Ergebnis einer Wiederherstellung.
type RestoreResult struct {
	Source               string `json:"source"`
	DataDir              string `json:"data_dir"`
	ManifestSHA256       string `json:"manifest_sha256"`
	BackupCreatedAt      string `json:"backup_created_at"`
	BackupProgramVersion string `json:"backup_program_version"`
	BackupSchemaVersion  int    `json:"backup_schema_version"`
	SchemaVersion        int    `json:"schema_version"`
	Files                int    `json:"files"`
	Bytes                int64  `json:"bytes"`
	Blobs                int    `json:"blobs"`
	AuditArchives        int    `json:"audit_archives"`
	MigrationSnapshot    string `json:"migration_snapshot,omitempty"`
	// MissingBlobs: Blobs, die schon beim Anlegen der Sicherung fehlten
	// (Manifest missing_blobs; T3-Nachbesserung 2, Punkt 4). Die Kopie
	// übernimmt diesen Zustand; doctor --blobs nennt sie weiter.
	MissingBlobs []string `json:"missing_blobs,omitempty"`
	// Leftovers: Arbeitsordner abgebrochener Wiederherstellungen neben dem
	// Ziel (RestoreLeftovers); löschen.
	Leftovers []string `json:"leftovers,omitempty"`
	// Accounts und StyleFiles zur Sichtprüfung (T3-Nachbesserung): Eine
	// Sicherung ist nicht signiert; wer sie verändern konnte, kann den
	// Server eines Kontos umlenken (das nächste account set schickte das
	// Passwort dorthin) oder eine Stildatei auf eine fremde Datei zeigen
	// lassen (ein Agent läse sie über mail://style). Keine Geheimnisse.
	Accounts   []RestoredAccount  `json:"accounts"`
	StyleFiles []RestoredStyle    `json:"style_files"`
	DryRun     bool               `json:"dry_run"`
	Fixes      store.RestoreFixes `json:"fixes"`
}

// RestoredAccount ist ein Konto der wiederhergestellten Kopie: wohin es
// sich verbindet und als wer.
type RestoredAccount struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      string `json:"tls"`
	Username string `json:"username"`
	AuthType string `json:"auth_type"`
}

// RestoredStyle ist eine Identität mit Stildatei (Pfad, den die Kopie
// liest).
type RestoredStyle struct {
	Account  string `json:"account"`
	Identity string `json:"identity"`
	Path     string `json:"path"`
}

// Restore prüft eine Sicherung und stellt sie in ein neues
// Datenverzeichnis her:
//
//  1. Manifest lesen und streng prüfen (Aufbau, Pfade, Zähler, Schema
//     höchstens bekannt); jede Datei vorhanden, gewöhnliche Datei, Größe
//     passt. Bis hier wird nichts geschrieben.
//  2. Arbeitsordner <ziel>.restoring-<zufall> neben dem Ziel anlegen
//     (0700) und jede Datei hinein kopieren (0600), dabei SHA-256 prüfen;
//     komprimierte Blobs zusätzlich gegen ihren Namen (blob.Store.Verify).
//  3. Datenbank dort öffnen und migrieren (Schnappschuss vor einer
//     Migration wie immer), eine eigene Instanz daraus machen
//     (store.PrepareRestored, A10) mit dem Systemereignis backup.restore,
//     schließen.
//  4. Ein leeres Ziel entfernen und den Arbeitsordner in das Ziel
//     umbenennen.
//
// Jeder Fehler ab Schritt 2 entfernt den Arbeitsordner; das Ziel bleibt,
// wie es war. Ausnahme (T3-Nachbesserung 3, Punkt 7): Scheitert in Schritt
// 4 das Entfernen des leeren Ziels oder das Umbenennen endgültig, ist die
// Kopie schon fertig (eigene Instanz, Datenbank geschlossen); der
// Arbeitsordner bleibt dann stehen, und die Meldung nennt ihn und den Befehl,
// der ihn an seinen Platz bringt (keptCopyErr).
func Restore(ctx context.Context, o RestoreOptions) (res RestoreResult, err error) {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	from, err := filepath.Abs(o.From)
	if err != nil {
		return res, targetErr("invalid backup folder")
	}
	to, err := filepath.Abs(o.DataDir)
	if err != nil {
		return res, targetErr("invalid data directory")
	}
	from, to = filepath.Clean(from), filepath.Clean(to)
	res.Source, res.DataDir, res.DryRun = from, to, o.DryRun
	if o.DBName == "" {
		o.DBName = brand.Name + ".db"
	}

	// 1. Manifest und Dateien prüfen, ohne zu schreiben.
	mpath := filepath.Join(from, ManifestName)
	if _, err := regularFile(mpath); err != nil {
		return res, manifestErr("no %s in %s", ManifestName, from)
	}
	mb, err := readLimited(mpath, maxManifestBytes)
	if err != nil {
		return res, err
	}
	sum := sha256.Sum256(mb)
	res.ManifestSHA256 = hex.EncodeToString(sum[:])
	m, err := parseManifest(mb, store.KnownSchemaVersion())
	if err != nil {
		return res, err
	}
	res.BackupCreatedAt, res.BackupProgramVersion, res.BackupSchemaVersion = m.CreatedAt, m.ProgramVersion, m.SchemaVersion
	res.Files, res.Bytes, res.Blobs, res.AuditArchives = m.Counts.Files, m.Counts.Bytes, m.Counts.Blobs, m.Counts.AuditArchives
	res.MissingBlobs = m.MissingBlobs
	if err := checkRestoreTarget(from, to, o.Refuse); err != nil {
		return res, err
	}
	for _, f := range m.Files {
		size, err := regularFile(filepath.Join(from, filepath.FromSlash(f.Path)))
		switch {
		case isNotExist(err):
			return res, manifestErr("%s is listed in the manifest but missing", f.Path)
		case err != nil:
			return res, manifestErr("%s: %v", f.Path, err)
		case size != f.Size:
			return res, manifestErr("%s has %d bytes, the manifest says %d", f.Path, size, f.Size)
		}
	}
	res.Leftovers = RestoreLeftovers(to)
	if o.DryRun {
		return res, verifyFiles(ctx, from, m)
	}

	// 2. In einem Arbeitsordner neben dem Ziel anlegen und kopieren
	// (T3-Nachbesserung 4): Bis die Kopie eine eigene Instanz ist, liegt
	// unter dem Ziel keine Datenbank. Ein harter Abbruch (Stromausfall)
	// ließe sonst eine betriebsfähige Kopie mit Kennung, Schlüsselbund-
	// Verweisen und offenen Server-Jobs des Originals zurück. Bei jedem
	// Fehler wird der Arbeitsordner entfernt (außer die fertige Kopie lässt
	// sich nicht umbenennen, Schritt 4); nach einem harten Abbruch nennen ihn
	// doctor und der nächste restore (RestoreLeftovers).
	work, err := makeWork(to)
	if err != nil {
		return res, err
	}
	keep := false
	defer func() {
		if err != nil && !keep {
			_ = os.RemoveAll(work)
		}
	}()
	for _, f := range m.Files {
		if err = ctx.Err(); err != nil {
			return res, err
		}
		dst := filepath.Join(work, filepath.FromSlash(f.Path))
		if f.Kind == KindDatabase {
			dst = filepath.Join(work, o.DBName)
		}
		want := f
		if _, _, err = copyAtomic(filepath.Join(from, filepath.FromSlash(f.Path)), dst, func(got string, n int64) error {
			if got != want.SHA256 || n != want.Size {
				return manifestErr("%s does not match the manifest (sha256 or size); nothing was restored", want.Path)
			}
			return nil
		}); err != nil {
			return res, err
		}
	}
	if m.Counts.Blobs > 0 {
		bs, berr := blob.OpenExisting(filepath.Join(work, "blobs"), blob.Options{})
		if berr != nil {
			err = berr
			return res, err
		}
		for _, f := range m.Files {
			if f.Kind != KindBlob || !strings.HasSuffix(f.Path, ".zst") {
				continue
			}
			name := strings.TrimSuffix(filepath.Base(f.Path), ".zst")
			if verr := bs.Verify(name, 0); verr != nil {
				err = manifestErr("blob %s does not match its name; nothing was restored", f.Path)
				return res, err
			}
		}
	}

	// 3. Datenbank: Schema wie im Manifest, migrieren, eigene Instanz;
	// danach ist sie geschlossen.
	if err = prepareDB(ctx, filepath.Join(work, o.DBName), m.SchemaVersion, o.KeepIdentity, now, &res); err != nil {
		return res, err
	}

	// 4. Erst jetzt an seinen Platz: ein leeres Ziel weicht, dann umbenennen.
	// Hat sich das Ziel inzwischen verändert (ErrTarget), wird wie bisher
	// aufgeräumt; sonst bleibt die fertige Kopie stehen (Punkt 7).
	if err = moveIntoPlace(work, to); err != nil {
		if !errors.Is(err, ErrTarget) {
			keep = true
			err = keptCopyErr(err, work, to, runtime.GOOS)
		}
		return res, err
	}
	if res.MigrationSnapshot != "" {
		res.MigrationSnapshot = filepath.Join(to, filepath.Base(res.MigrationSnapshot))
	}
	return res, nil
}

// prepareDB öffnet die kopierte Datenbank, prüft das Schema gegen das
// Manifest, migriert und macht eine eigene Instanz daraus
// (store.PrepareRestored, A10); sie ist danach geschlossen.
func prepareDB(ctx context.Context, path string, schema int, keepIdentity bool, now func() time.Time, res *RestoreResult) (err error) {
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := st.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	v, err := st.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if v != schema {
		return manifestErr("the database has schema %d, the manifest says %d; nothing was restored", v, schema)
	}
	if err = st.Migrate(ctx); err != nil {
		return err
	}
	res.MigrationSnapshot = st.LastSnapshot
	if res.SchemaVersion, err = st.SchemaVersion(ctx); err != nil {
		return err
	}
	prep := store.RestorePrep{Now: now(), Event: func(fx store.RestoreFixes) audit.Event {
		return restoreEvent(*res, fx, now())
	}}
	if !keepIdentity {
		if prep.DataDirID, err = randHex(16); err != nil {
			return err
		}
		prep.CredentialRef = secrets.AccountRef
	}
	if res.Fixes, err = st.PrepareRestored(ctx, prep); err != nil {
		return err
	}
	return readForReview(ctx, st, res)
}

// readForReview liest Konten (Server, Benutzer) und Stildateien der Kopie
// für die Sichtprüfung nach der Wiederherstellung.
func readForReview(ctx context.Context, st *store.SQLite, res *RestoreResult) error {
	accs, err := st.ListAccounts(ctx)
	if err != nil {
		return err
	}
	names := map[int64]string{}
	res.Accounts = []RestoredAccount{}
	for _, a := range accs {
		names[a.ID] = a.Name
		res.Accounts = append(res.Accounts, RestoredAccount{Name: a.Name, Host: a.Host, Port: a.Port, TLS: a.TLS,
			Username: a.Username, AuthType: a.AuthType})
	}
	ids, err := st.Identities(ctx, 0)
	if err != nil {
		return err
	}
	res.StyleFiles = []RestoredStyle{}
	for _, i := range ids {
		if i.StyleFile != "" {
			res.StyleFiles = append(res.StyleFiles, RestoredStyle{Account: names[i.AccountID], Identity: i.Name, Path: i.StyleFile})
		}
	}
	return nil
}

// verifyFiles liest jede Datei der Sicherung und vergleicht sie mit dem
// Manifest (restore --dry-run), ohne etwas zu schreiben.
func verifyFiles(ctx context.Context, from string, m Manifest) error {
	var bs *blob.Store
	for _, f := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		sum, n, err := hashFile(filepath.Join(from, filepath.FromSlash(f.Path)))
		if err != nil {
			return manifestErr("%s: %v", f.Path, err)
		}
		if sum != f.SHA256 || n != f.Size {
			return manifestErr("%s does not match the manifest (sha256 or size)", f.Path)
		}
		if f.Kind != KindBlob || !strings.HasSuffix(f.Path, ".zst") {
			continue
		}
		if bs == nil {
			if bs, err = blob.OpenExisting(filepath.Join(from, "blobs"), blob.Options{}); err != nil {
				return err
			}
		}
		if bs.Verify(strings.TrimSuffix(filepath.Base(f.Path), ".zst"), 0) != nil {
			return manifestErr("blob %s does not match its name", f.Path)
		}
	}
	return nil
}

// restoreEvent baut das Systemereignis backup.restore: nur Ausweise,
// Prüfsumme und Zähler.
func restoreEvent(res RestoreResult, fx store.RestoreFixes, now time.Time) audit.Event {
	p := map[string]any{"source_hash": PathHash(res.Source), "manifest_sha256": res.ManifestSHA256,
		"backup_created_at": res.BackupCreatedAt, "backup_program_version": res.BackupProgramVersion,
		"backup_schema_version": res.BackupSchemaVersion, "schema_version": res.SchemaVersion,
		"files": res.Files, "bytes": res.Bytes, "blobs": res.Blobs, "audit_archives": res.AuditArchives,
		"missing_blobs": len(res.MissingBlobs)}
	var fixes map[string]any
	if b, err := json.Marshal(fx); err == nil && json.Unmarshal(b, &fixes) == nil {
		maps.Copy(p, fixes)
	}
	b, _ := json.Marshal(p)
	return audit.Event{TS: now.UTC(), ActorType: audit.ActorSystem, KeyID: "system", Operation: OpRestore,
		Result: audit.ResultOK, ParamsRedacted: b}
}

// readLimited liest höchstens max Bytes (mehr ist ErrManifest).
func readLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, manifestErr("%s is larger than %d bytes", ManifestName, max)
	}
	return b, nil
}

// checkRestoreTarget: Das Ziel ist kein geschütztes Datenverzeichnis,
// liegt nicht in der Sicherung und existiert nicht oder ist leer.
func checkRestoreTarget(from, to string, refuse []string) error {
	real := realPath(to)
	for _, r := range refuse {
		if r == "" {
			continue
		}
		if samePath(real, realPath(r)) || samePath(to, r) || within(real, realPath(r)) || insideByID(to, r) {
			return targetErr("%s is (or lies inside) the default or configured data directory; restore never writes there: "+
				"restore into a new folder, then point --data-dir or data_dir in the configuration to it", to)
		}
	}
	if within(real, realPath(from)) || insideByID(to, from) {
		return targetErr("the new data directory %s lies inside the backup folder; choose a folder outside it", to)
	}
	fi, err := os.Stat(to)
	switch {
	case isNotExist(err):
		return nil
	case err != nil:
		return err
	case !fi.IsDir():
		return targetErr("%s exists and is not a folder", to)
	}
	entries, err := os.ReadDir(to)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return targetErr("%s is not empty; restore only into a new or empty folder", to)
	}
	return nil
}

// restoringInfix steht im Namen des Arbeitsordners einer Wiederherstellung:
// <ziel>.restoring-<zufall>, neben dem Ziel (gleiches Laufwerk, damit das
// Umbenennen atomar ist).
const restoringInfix = ".restoring-"

// restoringRE erkennt den Zufallsteil eines Arbeitsordners.
var restoringRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// makeWork legt den Arbeitsordner neben dem Ziel an (0700; fehlende
// Elternordner ebenfalls 0700, wie zuvor das Ziel selbst).
func makeWork(to string) (string, error) {
	parent := filepath.Dir(to)
	if err := os.MkdirAll(parent, dirMode); err != nil {
		return "", err
	}
	tag, err := randHex(4)
	if err != nil {
		return "", err
	}
	work := filepath.Join(parent, filepath.Base(to)+restoringInfix+tag)
	if err := os.Mkdir(work, dirMode); err != nil {
		return "", err
	}
	// Mkdir wirkt nach umask; 0700 soll es genau sein.
	if err := os.Chmod(work, dirMode); err != nil {
		_ = os.Remove(work)
		return "", fmt.Errorf("restore: cannot restrict the permissions of %s: %w", work, err)
	}
	return work, nil
}

// Umbenennen und Entfernen im letzten Schritt von restore
// (T3-Nachbesserung 2, Punkt 13; in Tests ersetzbar). Unter Windows hält
// ein Virenscanner, der Indexdienst oder ein Sicherungsprogramm eine eben
// geschriebene Datei oft kurz offen; Rename und Remove scheitern dann mit
// ERROR_ACCESS_DENIED oder ERROR_SHARING_VIOLATION. Solche Fehler
// (transientFSErr; unter macOS und Linux keiner) werden nach den Pausen
// in fsRetryPauses wiederholt (fünfmal, zusammen gut 3 s).
var (
	fsRename       = os.Rename
	fsRemove       = os.Remove
	transientFSErr = isTransientFSErr
	fsRetryPauses  = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond,
		800 * time.Millisecond, 1600 * time.Millisecond}
)

// retryFS führt op aus und wiederholt sie nach einem vorübergehenden
// Fehler; zurück kommt der letzte Fehler (der echte, nicht umgedeutet).
func retryFS(op func() error) error {
	err := op()
	for _, pause := range fsRetryPauses {
		if err == nil || !transientFSErr(err) {
			return err
		}
		time.Sleep(pause)
		err = op()
	}
	return err
}

// moveIntoPlace benennt den fertigen Arbeitsordner in das Ziel um. Ein
// vorhandenes Ziel war bei der Prüfung leer und wird erst jetzt entfernt;
// ist es inzwischen nicht mehr leer, scheitert das Entfernen und nichts
// wird überschrieben. Scheitert es aus einem anderen Grund, nennt der
// Fehler den echten Grund (T3-Nachbesserung 2, Punkt 13; vorher hieß es
// immer „no longer empty“).
func moveIntoPlace(work, to string) error {
	fi, err := os.Lstat(to)
	switch {
	case isNotExist(err):
	case err != nil:
		return err
	case !fi.IsDir():
		return targetErr("%s exists and is not a folder", to)
	default:
		if err := retryFS(func() error { return fsRemove(to) }); err != nil {
			if entries, rerr := os.ReadDir(to); rerr == nil && len(entries) > 0 {
				return targetErr("%s is no longer empty; restore only into a new or empty folder", to)
			}
			return fmt.Errorf("restore: cannot remove the empty folder %s to put the copy there "+
				"(is another program using it?): %w", to, err)
		}
	}
	if err := retryFS(func() error { return fsRename(work, to) }); err != nil {
		return fmt.Errorf("restore: cannot move the restored copy into place (is another program using it?): %w", err)
	}
	return nil
}

// keptCopyErr ergänzt den Fehler von moveIntoPlace, wenn die fertige Kopie
// im Arbeitsordner stehen bleibt (T3-Nachbesserung 3, Punkt 7; vorher
// löschte restore die Kopie, bei einem großen Archiv mehrere GB, und alles
// musste neu kopiert werden). Die Meldung nennt den Arbeitsordner und einen
// Befehl, der ihn an seinen Platz bringt, passend zum Betriebssystem goos:
// unter Windows für PowerShell (Directory.Delete, Rename-Item), sonst für
// die Shell (rmdir, mv). Steht das leere Ziel noch, wird es zuerst entfernt
// (sonst schöbe mv die Kopie in das Ziel hinein); rmdir und
// [System.IO.Directory]::Delete ohne recursive entfernen nur einen leeren
// Ordner (Remove-Item fragte bei einem nicht leeren nach und löschte auf
// Ja alles darin). Rename-Item scheitert, wenn das Ziel noch da ist.
func keptCopyErr(err error, work, to, goos string) error {
	_, serr := os.Lstat(to)
	targetThere := serr == nil
	var cmd string
	if goos == "windows" {
		cmd = "Rename-Item -LiteralPath " + psQuote(work) + " -NewName " + psQuote(filepath.Base(to))
		if targetThere {
			cmd = "[System.IO.Directory]::Delete(" + psQuote(to) + "); " + cmd
		}
	} else {
		cmd = "mv " + shQuote(work) + " " + shQuote(to)
		if targetThere {
			cmd = "rmdir " + shQuote(to) + " && " + cmd
		}
	}
	return fmt.Errorf("%w; the restored copy is complete and was kept in \"%s\" (do not delete it); "+
		"when no other program uses it any more, put it in place with: %s", err, work, cmd)
}

// shQuote setzt s in einfache Anführungszeichen (sh: nichts darin wird
// ausgewertet); ein Apostroph im Pfad schließt, maskiert und öffnet neu.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// psQuote setzt s in einfache Anführungszeichen (PowerShell); ein
// Apostroph im Pfad wird verdoppelt, ebenso die typografischen einfachen
// Anführungszeichen U+2018 bis U+201B, die PowerShell genauso wertet.
func psQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		b.WriteRune(r)
		switch r {
		case '\'', '\u2018', '\u2019', '\u201a', '\u201b':
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// RestoreLeftovers nennt Arbeitsordner abgebrochener Wiederherstellungen
// neben dem Datenverzeichnis dataDir (<dataDir>.restoring-<zufall>; nach
// einem harten Abbruch, etwa Stromausfall). Sie sind kein benutzbares
// Datenverzeichnis (die Datenbank darin trägt womöglich noch die Kennung
// des Originals) und gehören gelöscht; restore und doctor nennen sie,
// löschen sie aber nicht (es könnte ein restore sein, der gerade läuft).
// Ausnahme: den Ordner, den restore nach einem endgültig gescheiterten
// Umbenennen behalten hat (keptCopyErr), bringt der Befehl aus dessen
// Meldung an seinen Platz.
func RestoreLeftovers(dataDir string) []string {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil
	}
	abs = filepath.Clean(abs)
	parent, prefix := filepath.Dir(abs), filepath.Base(abs)+restoringInfix
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && strings.HasPrefix(n, prefix) && restoringRE.MatchString(strings.TrimPrefix(n, prefix)) {
			out = append(out, filepath.Join(parent, n))
		}
	}
	return out
}
