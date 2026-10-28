// Package blob ist der Blob-Store für Rohmails (.eml) und Anhänge
// (docs/datenmodell.md, Abschnitt „Blob-Store").
//
// Eigenschaften:
//
//   - Adressierung über SHA-256 der unkomprimierten Bytes; Pfad
//     <root>/ab/cd/<sha256> (zwei Verzeichnisebenen aus den ersten vier
//     Hex-Zeichen). Gleiche Inhalte liegen genau einmal.
//   - Schreiben atomar: Temporärdatei unter <root>/tmp (gleiches
//     Dateisystem), fsync, rename, fsync des Zielverzeichnisses. Eine
//     verwaiste Temporärdatei ist unschädlich; eine halbe Blob-Datei gibt
//     es nie.
//   - Optional zstd: komprimierte Blobs heißen <sha256>.zst; der Hash bleibt
//     der des Klartexts. Lesen erkennt beide Formen am Dateinamen, nicht am
//     Inhalt (ein Anhang darf selbst eine zstd-Datei sein).
//   - Größenlimit je Blob (MaxSize); Überschreitung bricht das Schreiben ab,
//     ohne einen Blob anzulegen.
//
// Die Zeile in der Tabelle `blobs` schreibt der Aufrufer erst NACH Put
// (eine Datei ohne Zeile ist unschädlich, eine Zeile ohne Datei nicht).
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// DefaultMaxSize begrenzt einen einzelnen Blob (unkomprimiert). Echte
// Anbieter liegen bei 25 bis 150 MB je Mail; 256 MiB lässt Luft.
const DefaultMaxSize int64 = 256 << 20

// Fehler, die Aufrufer unterscheiden.
var (
	ErrNotFound = errors.New("blob: not found")
	ErrTooLarge = errors.New("blob: size limit exceeded")
	ErrBadHash  = errors.New("blob: invalid sha256")
	ErrCorrupt  = errors.New("blob: content does not match its hash")
)

const zstSuffix = ".zst"

// Options steuert den Store.
type Options struct {
	// Compress schreibt neue Blobs zstd-komprimiert. Bestehende Blobs
	// bleiben, wie sie sind (lesen geht immer in beiden Formen).
	Compress bool
	// MaxSize begrenzt die unkomprimierte Größe; 0 = DefaultMaxSize.
	MaxSize int64
}

// Store ist ein Blob-Verzeichnis.
type Store struct {
	root string
	opts Options
}

// Info beschreibt einen geschriebenen Blob (Werte für die Tabelle blobs).
type Info struct {
	SHA256     string
	Size       int64 // unkomprimiert
	Compressed bool
	// Existed ist true, wenn der Inhalt schon vorhanden war (Deduplizierung).
	Existed bool
}

// Open öffnet (und legt bei Bedarf an) das Blob-Verzeichnis, Rechte 0700.
func Open(root string, opts Options) (*Store, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultMaxSize
	}
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o700); err != nil {
		return nil, fmt.Errorf("blob: create store: %w", err)
	}
	return &Store{root: root, opts: opts}, nil
}

// Root liefert das Verzeichnis des Stores.
func (s *Store) Root() string { return s.root }

func validHash(sum string) bool {
	if len(sum) != 64 {
		return false
	}
	for _, r := range sum {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) dir(sum string) string {
	return filepath.Join(s.root, sum[0:2], sum[2:4])
}

// Path liefert den Pfad eines vorhandenen Blobs und ob er komprimiert ist.
func (s *Store) Path(sum string) (string, bool, error) {
	if !validHash(sum) {
		return "", false, ErrBadHash
	}
	base := filepath.Join(s.dir(sum), sum)
	if _, err := os.Stat(base); err == nil {
		return base, false, nil
	}
	if _, err := os.Stat(base + zstSuffix); err == nil {
		return base + zstSuffix, true, nil
	}
	return "", false, ErrNotFound
}

// Has meldet, ob ein Blob vorhanden ist.
func (s *Store) Has(sum string) bool {
	_, _, err := s.Path(sum)
	return err == nil
}

// limitWriter zählt und bricht bei Überschreitung ab.
type limitWriter struct {
	n, max int64
}

func (w *limitWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	if w.n > w.max {
		return 0, ErrTooLarge
	}
	return len(p), nil
}

// Put liest r vollständig, schreibt den Inhalt atomar und liefert Hash und
// Größe. Ist der Inhalt schon vorhanden, bleibt die vorhandene Datei
// inhaltlich unverändert (Existed=true); nur ihr Dateidatum wird erneuert.
func (s *Store) Put(r io.Reader) (Info, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "put-*")
	if err != nil {
		return Info{}, fmt.Errorf("blob: temp file: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	lw := &limitWriter{max: s.opts.MaxSize}
	var sink io.Writer = tmp
	var enc *zstd.Encoder
	if s.opts.Compress {
		enc, err = zstd.NewWriter(tmp, zstd.WithEncoderConcurrency(1))
		if err != nil {
			return Info{}, fmt.Errorf("blob: zstd: %w", err)
		}
		sink = enc
	}
	if _, err := io.Copy(io.MultiWriter(lw, h, sink), r); err != nil {
		if enc != nil {
			_ = enc.Close()
		}
		if errors.Is(err, ErrTooLarge) {
			return Info{}, fmt.Errorf("%w (max %d bytes)", ErrTooLarge, s.opts.MaxSize)
		}
		return Info{}, fmt.Errorf("blob: write: %w", err)
	}
	if enc != nil {
		if err := enc.Close(); err != nil {
			return Info{}, fmt.Errorf("blob: zstd close: %w", err)
		}
	}
	info := Info{SHA256: hex.EncodeToString(h.Sum(nil)), Size: lw.n, Compressed: s.opts.Compress}

	// Schon vorhanden (in einer der beiden Formen)? Dann nichts ersetzen,
	// aber das Dateidatum erneuern (A14): blobs.gc löscht nur Dateien ohne
	// Verweis, die älter als ein Mindestalter sind; ein eben wieder
	// abgelegter Inhalt, dessen Verweis gleich entsteht, gilt so als frisch.
	// Scheitert das (etwa fehlende Rechte), bleibt es beim alten Datum; die
	// Ablage selbst ist trotzdem gelungen.
	if p, compressed, err := s.Path(info.SHA256); err == nil {
		now := timeNow()
		_ = os.Chtimes(p, now, now)
		info.Compressed, info.Existed = compressed, true
		return info, nil
	}
	if err := tmp.Sync(); err != nil {
		return Info{}, fmt.Errorf("blob: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Info{}, fmt.Errorf("blob: close: %w", err)
	}
	dir := s.dir(info.SHA256)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, fmt.Errorf("blob: mkdir: %w", err)
	}
	final := filepath.Join(dir, info.SHA256)
	if info.Compressed {
		final += zstSuffix
	}
	if err := os.Rename(tmpName, final); err != nil {
		return Info{}, fmt.Errorf("blob: rename: %w", err)
	}
	committed = true
	syncDir(dir)
	return info, nil
}

// timeNow ist in Tests ersetzbar.
var timeNow = time.Now

// syncDir schreibt den Verzeichniseintrag fest (Linux/macOS). Windows kennt
// kein fsync auf Verzeichnissen; dort genügt das rename.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// Get öffnet einen Blob zum Lesen (entpackt zstd transparent). Der Inhalt
// wird beim Lesen nicht erneut geprüft; dafür gibt es Verify.
func (s *Store) Get(sum string) (io.ReadCloser, error) {
	p, compressed, err := s.Path(sum)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("blob: open: %w", err)
	}
	if !compressed {
		return f, nil
	}
	dec, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(s.opts.MaxSize)+(1<<20)))
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("blob: zstd: %w", err)
	}
	// Auch beim Lesen höchstens MaxSize (+1, um Überschreitung zu erkennen).
	return &zstdReadCloser{r: io.LimitReader(dec, s.opts.MaxSize+1), dec: dec, f: f}, nil
}

type zstdReadCloser struct {
	r   io.Reader
	dec *zstd.Decoder
	f   *os.File
}

func (z *zstdReadCloser) Read(p []byte) (int, error) { return z.r.Read(p) }
func (z *zstdReadCloser) Close() error {
	z.dec.Close()
	return z.f.Close()
}

// Verify liest einen Blob und prüft Hash und (optional, >0) Größe.
func (s *Store) Verify(sum string, size int64) error {
	rc, err := s.Get(sum)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum || (size > 0 && n != size) {
		return ErrCorrupt
	}
	return nil
}

// CleanTemp entfernt verwaiste Temporärdateien (nach einem Absturz). Nur
// aufrufen, wenn kein anderer Prozess gerade schreibt (Wartungsoperation).
func (s *Store) CleanTemp() (int, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "tmp"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "put-") {
			if os.Remove(filepath.Join(s.root, "tmp", e.Name())) == nil {
				n++
			}
		}
	}
	return n, nil
}
