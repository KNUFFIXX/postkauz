// Package backup sichert ein Datenverzeichnis und stellt es wieder her
// (S3.4, E7, A10; docs/betrieb.md, „Sicherung und Wiederherstellung").
//
// Eine Sicherung ist ein Ordner (oder ein Tar-Strom mit demselben Inhalt):
//
//	manifest.json                    Beschreibung mit SHA-256 je Datei
//	ENCRYPT-ME.txt                   Hinweis: das Ziel muss verschlüsselt sein
//	<programm>-<UTC-Zeit>.db         Schnappschuss der Datenbank (VACUUM INTO)
//	blobs/ab/cd/<sha256>[.zst]       Blob-Store, Layout wie im Datenverzeichnis
//	audit-archive-*.db               Archivdatenbanken von audit.rotate, falls vorhanden
//
// PostKauz verschlüsselt nicht selbst (E7): Eigene Schlüssel wären neuer
// Code mit Verlustrisiko. Das Manifest beschreibt genau einen konsistenten
// Stand; ein weiteres backup create in denselben Ordner kopiert nur
// fehlende Blobs (inhaltsadressiert) und ersetzt Schnappschuss und
// Manifest. Pfade im Manifest sind immer mit "/" getrennt, auch unter
// Windows; restore lehnt alles ab, was nicht genau diesem Aufbau
// entspricht (kein Pfad außerhalb des Zielordners möglich).
package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/brand"
)

// Namen im Sicherungsordner.
const (
	ManifestName    = "manifest.json"
	EncryptNoteName = "ENCRYPT-ME.txt"
	// ManifestFormat ist die Version des Manifests.
	ManifestFormat = 1
	// maxManifestBytes begrenzt das Manifest beim Lesen (50.000 Nachrichten
	// mit Anhängen ergeben rund 20 MB).
	maxManifestBytes = 512 << 20
)

// Arten von Dateien im Manifest.
const (
	KindDatabase     = "database"
	KindBlob         = "blob"
	KindAuditArchive = "audit_archive"
)

// Fehler, die Aufrufer unterscheiden (CLI: Codes backup_target,
// backup_invalid).
var (
	// ErrTarget: Das Ziel (Sicherungsordner bzw. neues Datenverzeichnis)
	// ist nicht erlaubt; nichts wurde geschrieben.
	ErrTarget = errors.New("backup: target not allowed")
	// ErrManifest: Das Manifest fehlt, ist ungültig oder passt nicht zu
	// den Dateien; restore hat abgebrochen.
	ErrManifest = errors.New("backup: invalid backup")
	// ErrCorruptSource: Ein Blob im Datenverzeichnis passt nicht zu
	// seinem Namen (doctor --blobs=all zeigt ihn); die Sicherung bricht ab.
	ErrCorruptSource = errors.New("backup: a blob in the data directory is corrupt")
)

// File ist ein Eintrag im Manifest: Pfad relativ zum Sicherungsordner
// (mit "/"), Art, Größe und SHA-256 der Datei, wie sie dort liegt (bei
// .zst also der komprimierten Bytes).
type File struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Counts fasst das Manifest zusammen (restore prüft es gegen die Liste).
type Counts struct {
	Files         int   `json:"files"`
	Bytes         int64 `json:"bytes"`
	Blobs         int   `json:"blobs"`
	AuditArchives int   `json:"audit_archives"`
}

// Manifest beschreibt eine Sicherung.
type Manifest struct {
	Format         int    `json:"format"`
	Program        string `json:"program"`
	ProgramVersion string `json:"program_version"`
	SchemaVersion  int    `json:"schema_version"`
	CreatedAt      string `json:"created_at"` // RFC 3339, UTC
	// Database ist der Pfad des Schnappschusses (ein Eintrag in Files).
	Database string `json:"database"`
	Counts   Counts `json:"counts"`
	Files    []File `json:"files"`
}

var (
	hexRE       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	blobPathRE  = regexp.MustCompile(`^blobs/([0-9a-f]{2})/([0-9a-f]{2})/([0-9a-f]{64})(\.zst)?$`)
	archiveRE   = regexp.MustCompile(`^audit-archive-[A-Za-z0-9][A-Za-z0-9._-]{0,120}\.db$`)
	snapshotRE  = regexp.MustCompile(`^` + regexp.QuoteMeta(brand.Name) + `-[0-9]{8}T[0-9]{6}Z(-[0-9]{1,4})?\.db$`)
	snapshotFmt = "20060102T150405Z"
)

// snapshotName ist der Name des Schnappschusses zu einer Zeit.
func snapshotName(t time.Time, n int) string {
	s := brand.Name + "-" + t.UTC().Format(snapshotFmt)
	if n > 1 {
		s += fmt.Sprintf("-%d", n)
	}
	return s + ".db"
}

// blobRel ist der Pfad eines Blobs im Sicherungsordner.
func blobRel(sum string, compressed bool) string {
	p := "blobs/" + sum[0:2] + "/" + sum[2:4] + "/" + sum
	if compressed {
		p += ".zst"
	}
	return p
}

// kindOf ordnet einen Pfad seiner Art zu ("" = unzulässig).
func kindOf(p string) string {
	switch {
	case snapshotRE.MatchString(p):
		return KindDatabase
	case archiveRE.MatchString(p):
		return KindAuditArchive
	}
	if m := blobPathRE.FindStringSubmatch(p); m != nil && m[1] == m[3][0:2] && m[2] == m[3][2:4] {
		return KindBlob
	}
	return ""
}

// encode schreibt das Manifest (eingerückt, LF, Dateien nach Pfad sortiert).
func (m *Manifest) encode() ([]byte, error) {
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	m.Counts = Counts{}
	for _, f := range m.Files {
		m.Counts.Files++
		m.Counts.Bytes += f.Size
		switch f.Kind {
		case KindBlob:
			m.Counts.Blobs++
		case KindAuditArchive:
			m.Counts.AuditArchives++
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// manifestErr ist ErrManifest mit Grund (englisch, ohne Inhalte).
func manifestErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrManifest, fmt.Sprintf(format, args...))
}

// parseManifest liest und prüft ein Manifest streng: unbekannte Felder,
// fremdes Programm, neueres Schema, unzulässige Pfade, doppelte Einträge
// und Zähler, die nicht zur Liste passen, sind ErrManifest. knownSchema ist
// die höchste Schemaversion dieses Programms.
func parseManifest(b []byte, knownSchema int) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, manifestErr("%s is not a valid manifest", ManifestName)
	}
	if dec.More() {
		return m, manifestErr("%s has trailing data", ManifestName)
	}
	switch {
	case m.Program != brand.Name:
		return m, manifestErr("%s was not written by %s", ManifestName, brand.Name)
	case m.Format != ManifestFormat:
		return m, manifestErr("manifest format %d is not supported (this program reads format %d)", m.Format, ManifestFormat)
	case m.SchemaVersion < 1:
		return m, manifestErr("manifest has no schema version")
	case m.SchemaVersion > knownSchema:
		return m, manifestErr("the backup has database schema %d, this program knows up to %d; use a newer version", m.SchemaVersion, knownSchema)
	}
	if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
		return m, manifestErr("manifest has no valid creation time")
	}
	if len(m.Files) == 0 {
		return m, manifestErr("manifest lists no files")
	}
	seen := map[string]bool{}
	var c Counts
	dbs := 0
	for _, f := range m.Files {
		if strings.Contains(f.Path, `\`) {
			return m, manifestErr("path %q uses backslashes; backups of this program always use '/'", f.Path)
		}
		k := kindOf(f.Path)
		if k == "" || k != f.Kind {
			return m, manifestErr("path %q is not part of a backup (kind %q)", f.Path, f.Kind)
		}
		if seen[f.Path] {
			return m, manifestErr("path %q is listed twice", f.Path)
		}
		seen[f.Path] = true
		if f.Size < 0 || !hexRE.MatchString(f.SHA256) {
			return m, manifestErr("entry %q has an invalid size or sha256", f.Path)
		}
		if k == KindBlob && !strings.HasSuffix(f.Path, ".zst") && f.SHA256 != f.Path[len(f.Path)-64:] {
			// Unkomprimierte Blobs heißen nach dem SHA-256 ihres Inhalts.
			return m, manifestErr("blob %q: sha256 differs from its name", f.Path)
		}
		c.Files++
		c.Bytes += f.Size
		switch k {
		case KindDatabase:
			dbs++
			if f.Path != m.Database {
				return m, manifestErr("database %q is not the one named in the manifest", f.Path)
			}
		case KindBlob:
			c.Blobs++
		case KindAuditArchive:
			c.AuditArchives++
		}
	}
	if dbs != 1 {
		return m, manifestErr("manifest must list exactly one database, not %d", dbs)
	}
	if c != m.Counts {
		return m, manifestErr("counts in the manifest do not match its file list")
	}
	return m, nil
}
