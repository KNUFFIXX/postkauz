// Package config lädt die Konfiguration (TOML) und bestimmt die Pfade für
// Konfiguration, Datenverzeichnis, Datenbank, Blob-Store und Log.
//
// Reihenfolge je Wert: Umgebungsvariable vor Konfigurationsdatei vor
// Standard. Die Konfigurationsdatei enthält nie Geheimnisse, nur Verweise
// (docs/sicherheit.md, Abschnitt 4).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/KNUFFIXX/postkauz/internal/brand"
)

// Umgebungsvariablen (Präfix aus internal/brand).
var (
	EnvConfig         = brand.EnvPrefix + "_CONFIG"
	EnvDataDir        = brand.EnvPrefix + "_DATA_DIR"
	EnvSecretsBackend = brand.EnvPrefix + "_SECRETS_BACKEND"
)

// Werte für secrets.backend.
const (
	// BackendAuto: Schlüsselbund des Betriebssystems; fehlt er, schlägt das
	// Speichern mit einem Hinweis fehl (kein stiller Rückfall auf eine Datei).
	BackendAuto = "auto"
	// BackendKeyring: ausdrücklich nur der Schlüsselbund.
	BackendKeyring = "keyring"
	// BackendInsecureFile: verschlüsselte Datei im Datenverzeichnis, deren
	// Schlüssel daneben liegt. UNSICHER, nur für Tests und Testserver.
	BackendInsecureFile = "insecure-file"
)

// Config ist der Inhalt der Konfigurationsdatei.
type Config struct {
	// DataDir überschreibt das Datenverzeichnis (Datenbank, Blobs, Log).
	DataDir string `toml:"data_dir"`
	// LogLevel: debug, info (Standard), warn, error.
	LogLevel string  `toml:"log_level"`
	Secrets  Secrets `toml:"secrets"`
	IMAP     IMAP    `toml:"imap"`
	Audit    Audit   `toml:"audit"`
}

// Audit steuert den Export des Audit-Logs (docs/sicherheit.md, Abschnitt 6).
type Audit struct {
	// ExportDir ist das Standardziel von `audit export`: ein zweiter Ort
	// (anderer Datenträger oder Rechner), nicht das Datenverzeichnis.
	ExportDir string `toml:"export_dir"`
}

// IMAP steuert den Watchdog der IMAP-Verbindungen (ADR-007, Nachtrag
// 16.10.2026). Werte als Dauer, z. B. "30s" oder "10m"; leer = Standard.
type IMAP struct {
	// CommandTimeout: so lange darf der Server während eines Kommandos
	// schweigen (Standard 30s).
	CommandTimeout time.Duration `toml:"command_timeout"`
	// CommandMaxDuration: Obergrenze je Kommando (Standard 10m).
	CommandMaxDuration time.Duration `toml:"command_max_duration"`
	// MaxMessageBytes: größte Rohmail, die der Sync annimmt (Standard
	// 256 MiB = MaxMessageBytesLimit, mindestens 1 MiB; A02 A4). Größere
	// zählen als Fehler je Nachricht, der Sync läuft weiter.
	MaxMessageBytes int64 `toml:"max_message_bytes"`
}

// MaxMessageBytesLimit ist Standard und Obergrenze von
// imap.max_message_bytes: Größere Nachrichten nimmt auch der Blob-Store
// nicht an (blob.DefaultMaxSize).
const MaxMessageBytesLimit int64 = 256 << 20

// MinMessageBytes ist der kleinste erlaubte Wert von
// imap.max_message_bytes (1 MiB): Darunter fiele schon gewöhnliche Post
// ohne Anhang heraus; ein Tippfehler (etwa 256 statt 256 MiB) soll nicht
// still fast jede Nachricht überspringen.
const MinMessageBytes int64 = 1 << 20

// Secrets wählt, wo Zugangsdaten liegen.
type Secrets struct {
	Backend string `toml:"backend"`
}

// Paths sind die aufgelösten Pfade einer Instanz.
type Paths struct {
	ConfigFile   string `json:"config_file"`
	ConfigExists bool   `json:"config_exists"`
	DataDir      string `json:"data_dir"`
	Database     string `json:"database"`
	Blobs        string `json:"blobs"`
	Log          string `json:"log"`
	SecretsDir   string `json:"secrets_dir"`
}

// ConfigPath liefert den Pfad der Konfigurationsdatei: POSTKAUZ_CONFIG, sonst
// <Konfigurationsordner des Betriebssystems>/postkauz/config.toml.
func ConfigPath() (string, error) {
	if p := os.Getenv(EnvConfig); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: no config directory: %w", err)
	}
	return filepath.Join(dir, brand.ConfigDir, "config.toml"), nil
}

// DefaultDataDir liefert das Standard-Datenverzeichnis des Betriebssystems:
//
//	Linux/BSD: $XDG_DATA_HOME/postkauz, sonst ~/.local/share/postkauz
//	Windows:   %LOCALAPPDATA%\postkauz (lokal, nicht „Roaming“: wird nicht synchronisiert)
//	macOS:     ~/Library/Application Support/postkauz
func DefaultDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, brand.ConfigDir), nil
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", brand.ConfigDir), nil
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" && filepath.IsAbs(d) {
			return filepath.Join(d, brand.ConfigDir), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: no home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", brand.ConfigDir), nil
}

// Load liest die Konfigurationsdatei (falls vorhanden), wendet
// Umgebungsvariablen und Standards an und bestimmt die Pfade. Unbekannte
// Schlüssel in der Datei sind ein Fehler (Tippfehler sollen auffallen).
func Load() (Config, Paths, error) { return LoadProfile("") }

// ProfileRE: erlaubte Profilnamen (wie Kontonamen).
var ProfileRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// LoadProfile ist Load für eine getrennte Instanz (--profile): Konfiguration
// und Daten liegen in einem Unterordner profiles/<name> des normalen
// Konfigurations- bzw. Datenordners (auch unter POSTKAUZ_CONFIG und
// POSTKAUZ_DATA_DIR). data_dir in der Profil-Konfiguration ersetzt den
// Datenordner ganz. Leerer Name = ohne Profil.
func LoadProfile(profile string) (Config, Paths, error) {
	var cfg Config
	var p Paths
	if profile != "" && !ProfileRE.MatchString(profile) {
		return cfg, p, fmt.Errorf("config: invalid profile name %q: use lowercase letters, digits and '-' (max 63)", profile)
	}
	path, err := ConfigPath()
	if err != nil {
		return cfg, p, err
	}
	if profile != "" {
		path = filepath.Join(filepath.Dir(path), "profiles", profile, "config.toml")
	}
	p.ConfigFile = path
	md, err := toml.DecodeFile(path, &cfg)
	switch {
	case err == nil:
		p.ConfigExists = true
		if und := md.Undecoded(); len(und) > 0 {
			keys := make([]string, 0, len(und))
			for _, k := range und {
				keys = append(keys, k.String())
			}
			sort.Strings(keys)
			return cfg, p, fmt.Errorf("config: %s: unknown keys: %s", path, strings.Join(keys, ", "))
		}
	case errors.Is(err, fs.ErrNotExist):
		// keine Datei: Standards
	default:
		return cfg, p, fmt.Errorf("config: %s: %w", path, err)
	}

	fileDataDir := cfg.DataDir
	if d := os.Getenv(EnvDataDir); d != "" {
		cfg.DataDir = d
	}
	if b := os.Getenv(EnvSecretsBackend); b != "" {
		cfg.Secrets.Backend = b
	}
	if cfg.Secrets.Backend == "" {
		cfg.Secrets.Backend = BackendAuto
	}
	switch cfg.Secrets.Backend {
	case BackendAuto, BackendKeyring, BackendInsecureFile:
	default:
		return cfg, p, fmt.Errorf("config: secrets.backend %q is invalid (expected auto, keyring or insecure-file)", cfg.Secrets.Backend)
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return cfg, p, fmt.Errorf("config: log_level %q is invalid (expected debug, info, warn or error)", cfg.LogLevel)
	}
	if cfg.IMAP.CommandTimeout < 0 || cfg.IMAP.CommandMaxDuration < 0 {
		return cfg, p, fmt.Errorf("config: imap timeouts must not be negative")
	}
	if cfg.IMAP.CommandTimeout != 0 && cfg.IMAP.CommandTimeout < time.Second {
		return cfg, p, fmt.Errorf("config: imap.command_timeout %s is too short (at least 1s)", cfg.IMAP.CommandTimeout)
	}
	if cfg.IMAP.CommandMaxDuration != 0 && cfg.IMAP.CommandMaxDuration < cfg.IMAP.CommandTimeout {
		return cfg, p, fmt.Errorf("config: imap.command_max_duration %s is shorter than imap.command_timeout %s",
			cfg.IMAP.CommandMaxDuration, cfg.IMAP.CommandTimeout)
	}
	if v := cfg.IMAP.MaxMessageBytes; v != 0 && (v < MinMessageBytes || v > MaxMessageBytesLimit) {
		return cfg, p, fmt.Errorf("config: imap.max_message_bytes %d is out of range (%d to %d; 0 = default)", v, MinMessageBytes, MaxMessageBytesLimit)
	}
	if cfg.DataDir == "" {
		if cfg.DataDir, err = DefaultDataDir(); err != nil {
			return cfg, p, err
		}
	}
	if profile != "" && (fileDataDir == "" || os.Getenv(EnvDataDir) != "") {
		cfg.DataDir = filepath.Join(cfg.DataDir, "profiles", profile)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		abs, err := filepath.Abs(cfg.DataDir)
		if err != nil {
			return cfg, p, err
		}
		cfg.DataDir = abs
	}
	p.DataDir = cfg.DataDir
	p.Database = filepath.Join(p.DataDir, brand.Name+".db")
	p.Blobs = filepath.Join(p.DataDir, "blobs")
	p.Log = filepath.Join(p.DataDir, brand.Name+".log")
	p.SecretsDir = filepath.Join(p.DataDir, "insecure-secrets")
	return cfg, p, nil
}

// EnsureDataDir legt das Datenverzeichnis nur für den Nutzer lesbar an
// (0700, docs/sicherheit.md, Abschnitt 10). Ein schon vorhandenes
// Verzeichnis mit weiteren Rechten verschärft RestrictPermissions.
func EnsureDataDir(p Paths) error {
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		return fmt.Errorf("config: data directory: %w", err)
	}
	return nil
}

// chmod und euid sind in Tests ersetzbar (ein Fehler bzw. ein fremder
// Besitzer lässt sich sonst als Besitzer kaum auslösen).
var (
	chmod = os.Chmod
	euid  = os.Geteuid
)

// PermChange meldet eine Verschärfung durch RestrictPermissions: vorher
// (Old) und nachher (New), oder den Fehler, an dem sie scheiterte. Skip
// nennt den Grund, aus dem der Pfad bewusst nicht geändert wurde
// (symlink | foreign_owner); der Aufrufer schreibt dann eine Warnung ins
// Log.
type PermChange struct {
	Path string
	Old  fs.FileMode
	New  fs.FileMode
	Err  error
	Skip string
}

// permBits sind die Rechte samt Sonderbits (Setuid, Setgid, Sticky), wie
// os.Chmod sie setzt.
const permBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// RestrictPermissions nimmt Datenverzeichnis und Log die Rechte für Gruppe
// und andere (Datenverzeichnis danach 0700, Log 0600; A02 A3, Frage 6c).
// Die Datenbank setzt store.Open selbst auf 0600. Rechte des Besitzers und
// Sonderbits (etwa Setgid eines Verzeichnisses) bleiben, wie sie sind; nur
// die Bits für Gruppe und andere fallen weg. Ein Symlink (das Ziel kann ein
// fremdes, geteiltes Verzeichnis sein) und ein Pfad, der einem anderen
// Nutzer gehört, bleiben unverändert (Skip; Lstat, folgt keinem Symlink).
// Unter Windows gilt der Profilordner, dort geschieht nichts. Fehler
// brechen nichts ab: Der Aufrufer schreibt sie als Warnung ins Log (doctor
// --secrets meldet die Rechte dann als fail).
func RestrictPermissions(p Paths) []PermChange {
	if runtime.GOOS == "windows" {
		return nil
	}
	var out []PermChange
	for _, path := range []string{p.DataDir, p.Log} {
		if path == "" {
			continue
		}
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			out = append(out, PermChange{Path: path, Err: err})
			continue
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			// Nie dem Symlink folgen; warnen nur, wenn das Ziel zu offen ist.
			if t, err := os.Stat(path); err == nil && t.Mode().Perm()&0o077 != 0 {
				m := t.Mode() & permBits
				out = append(out, PermChange{Path: path, Old: m, New: m, Skip: "symlink"})
			}
			continue
		}
		mode := fi.Mode() & permBits
		if mode.Perm()&0o077 == 0 {
			continue
		}
		c := PermChange{Path: path, Old: mode, New: mode &^ 0o077}
		if uid, ok := fileOwner(fi); ok && uid != euid() {
			c.Skip, c.New = "foreign_owner", c.Old
			out = append(out, c)
			continue
		}
		c.Err = chmod(path, c.New)
		out = append(out, c)
	}
	return out
}

// OctalMode schreibt Rechte samt Sonderbits wie chmod (z. B. "2700").
func OctalMode(m fs.FileMode) string {
	v := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		v |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		v |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		v |= 0o1000
	}
	return fmt.Sprintf("%04o", v)
}

// cloudMarkers sind Namensteile typischer Cloud-Sync-Ordner. SQLite im
// WAL-Modus und Synchronisierung vertragen sich nicht (datenmodell.md).
var cloudMarkers = []string{"onedrive", "dropbox", "google drive", "googledrive", "icloud", "mobile documents", "nextcloud", "owncloud", "pcloud"}

// CloudSyncWarning liefert einen Hinweis, wenn der Pfad nach einem
// Cloud-synchronisierten Ordner aussieht, sonst "".
func CloudSyncWarning(path string) string {
	low := strings.ToLower(filepath.ToSlash(path))
	for _, m := range cloudMarkers {
		if strings.Contains(low, m) {
			return fmt.Sprintf("data directory looks like a cloud-synced folder (%q); move it, SQLite in WAL mode gets corrupted by sync tools", m)
		}
	}
	return ""
}
