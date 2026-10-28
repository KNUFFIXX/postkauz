// Package config lädt die Konfiguration (TOML) und bestimmt die Pfade für
// Konfiguration, Datenverzeichnis, Datenbank, Blob-Store und Log.
//
// Reihenfolge je Wert: Kommandozeile (--config, --data-dir) vor
// Umgebungsvariable vor Konfigurationsdatei vor Standard. Die Konfigurationsdatei enthält nie Geheimnisse, nur Verweise
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
	"strconv"
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
	// Paths: weitere Ordner (seit S2.1: attach_dir).
	Paths    PathsConfig `toml:"paths"`
	Operator Operator    `toml:"operator"`
	Draft    Draft       `toml:"draft"`
	// Approval: Abschnitt [approval] (seit T2, E3): ttl gilt für Anträge
	// auf Entwürfe und Aktionen; neuer Name für [draft] approval_ttl, der
	// alte Schlüssel bleibt gültig.
	Approval Approval `toml:"approval"`
	// Tags: Abschnitt [tags] (seit T2, E5).
	Tags Tags `toml:"tags"`
	// Retention: Abschnitt [retention] (seit T3, E8: Löschkonzept).
	Retention Retention `toml:"retention"`
}

// Retention: Abschnitt [retention] der Konfiguration (E8, A11). Gelöscht
// wird nur über die ausdrücklichen Operationen purge und blob gc, nie
// nebenbei.
type Retention struct {
	// DeletedRemoteAfter: Standardfrist von purge für Nachrichten, die auf
	// dem Server gelöscht wurden (0 = nie; dann braucht purge
	// --older-than).
	DeletedRemoteAfter Duration `toml:"deleted_remote_after"`
	// BlobGCMinAge: blob gc löscht nur Dateien ohne Verweis, die älter sind
	// (0 = Standard 24h; mindestens 1h).
	BlobGCMinAge Duration `toml:"blob_gc_min_age"`
}

// Standards und Grenzen für [retention] und [audit] retain.
const (
	DefaultBlobGCMinAge = 24 * time.Hour
	MinBlobGCMinAge     = time.Hour
	DefaultAuditRetain  = 365 * 24 * time.Hour
	MinAuditRetain      = 24 * time.Hour
	// MaxDurationDays: größte Tagesangabe einer Dauer (wie ops.ParseSince;
	// time.Duration läuft bei rund 106.751 Tagen über).
	MaxDurationDays = 100000
)

// Duration ist eine Dauer in der Konfiguration: wie Go ("90m", "24h"),
// zusätzlich in Tagen ("365d") oder als Zahl 0. Negative Werte sind ein
// Fehler.
type Duration time.Duration

// D liefert den Wert als time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalTOML liest eine Dauer (Zeichenkette oder die Zahl 0).
func (d *Duration) UnmarshalTOML(v any) error {
	switch x := v.(type) {
	case int64:
		if x != 0 {
			return fmt.Errorf("a duration needs a unit, e.g. \"%dd\" or \"%dh\" (only 0 may stand alone)", x, x)
		}
		*d = 0
		return nil
	case string:
		p, err := ParseDuration(x)
		if err != nil {
			return err
		}
		*d = Duration(p)
		return nil
	}
	return fmt.Errorf("a duration must be a string like \"365d\" or \"24h\"")
}

// ParseDuration liest "<n>d" oder eine Go-Dauer ("24h", "90m", "0s");
// negativ oder über MaxDurationDays ist ein Fehler.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.ParseInt(n, 10, 64)
		if err != nil || days < 0 || days > MaxDurationDays {
			return 0, fmt.Errorf("invalid duration %q: use whole days from 0 to %dd", s, MaxDurationDays)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q: use e.g. 365d, 24h or 90m", s)
	}
	return d, nil
}

// Approval: Abschnitt [approval] der Konfiguration.
type Approval struct {
	// TTL: so lange gilt ein offener Antrag bzw. eine erteilte Freigabe
	// (0 = [draft] approval_ttl bzw. 168h). Nach dem Laden steht hier immer
	// der wirksame Wert (wie Draft.ApprovalTTL).
	TTL time.Duration `toml:"ttl"`
}

// Tags: Abschnitt [tags] der Konfiguration.
type Tags struct {
	// SyncKeywords: Tags zusätzlich als IMAP-Keywords auf den Server
	// schreiben (nur in Ordnern mit PERMANENTFLAGS \*; Standard false).
	SyncKeywords bool `toml:"sync_keywords"`
}

// PathsConfig: Abschnitt [paths] der Konfiguration.
type PathsConfig struct {
	// AttachDir: Dateianhänge von Entwürfen nur von hier (A02 E10;
	// Standard <Datenverzeichnis>/attach). Relativ = zum Datenverzeichnis.
	AttachDir string `toml:"attach_dir"`
	// ExportDir: Ziel von attachments.save und Basis relativer Ziele von
	// messages.export (S3.3, E6; Standard <Datenverzeichnis>/export).
	// Relativ = zum Datenverzeichnis.
	ExportDir string `toml:"export_dir"`
}

// Operator: Abschnitt [operator]. Name ist, wer im MVP freigibt
// (approvals.decided_by; Entschiedene Frage 20); leer = Benutzername des
// Betriebssystems.
type Operator struct {
	Name string `toml:"name"`
}

// Draft: Abschnitt [draft] der Konfiguration (A02 E7, E10, E11, E12). Nicht
// zu verwechseln mit [draft] der Policy-Datei (Prüf-Hook, Freigabepflicht).
type Draft struct {
	// ApprovalTTL: so lange gilt eine offene oder erteilte Freigabe
	// (Standard 168h; mindestens 1m, höchstens 365 Tage).
	ApprovalTTL time.Duration `toml:"approval_ttl"`
	// MaxAttachmentBytes: Summe der Anhänge eines Entwurfs (Standard 25 MiB).
	MaxAttachmentBytes int64 `toml:"max_attachment_bytes"`
	// Mailbox: Ordner für die Entwurfskopien; leer = Ordner mit Rolle drafts.
	Mailbox string `toml:"mailbox"`
	// QuoteLine: Einleitungszeile des Zitats mit {date}, {date_numeric}
	// (21.10.2026 09:15) und {from}; leer = "On {date}, {from} wrote:".
	QuoteLine string `toml:"quote_line"`
}

// Standards für [draft] und [paths].
const (
	DefaultApprovalTTL        = 168 * time.Hour
	MinApprovalTTL            = time.Minute
	MaxApprovalTTL            = 365 * 24 * time.Hour
	DefaultMaxAttachmentBytes = 25 << 20
	// MaxAttachmentBytesLimit: größer nimmt der Blob-Store nichts an.
	MaxAttachmentBytesLimit = 256 << 20
	DefaultQuoteLine        = "On {date}, {from} wrote:"
	DefaultAttachDirName    = "attach"
	DefaultExportDirName    = "export"
)

// Audit steuert den Export des Audit-Logs (docs/sicherheit.md, Abschnitt 6).
type Audit struct {
	// ExportDir ist das Standardziel von `audit export`: ein zweiter Ort
	// (anderer Datenträger oder Rechner), nicht das Datenverzeichnis.
	ExportDir string `toml:"export_dir"`
	// Retain: `audit rotate` verschiebt nur exportierte und bestätigte
	// Tage, die älter sind, in eine Archivdatenbank (E8; 0 = Standard
	// 365d, mindestens 1d). Nach dem Laden steht hier der wirksame Wert.
	Retain Duration `toml:"retain"`
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
	// AttachDir: aufgelöster [paths] attach_dir (absolut).
	AttachDir string `json:"attach_dir"`
	// ExportDir: aufgelöster [paths] export_dir (absolut).
	ExportDir string `json:"export_dir"`
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
func Load() (Config, Paths, error) { return LoadWith(LoadOptions{}) }

// LoadOptions übersteuert die Pfade für einen Aufruf (globale Flags
// --profile, --config, --data-dir). Leere Felder = nicht gesetzt.
type LoadOptions struct {
	// Profile: getrennte Instanz (siehe LoadProfile).
	Profile string
	// ConfigFile ersetzt POSTKAUZ_CONFIG bzw. den Standardpfad der
	// Konfigurationsdatei (gleiche Bedeutung, höhere Priorität).
	ConfigFile string
	// DataDir ersetzt POSTKAUZ_DATA_DIR (gleiche Bedeutung, höhere
	// Priorität; mit Profil also <DataDir>/profiles/<name>).
	DataDir string
}

// ProfileRE: erlaubte Profilnamen (wie Kontonamen).
var ProfileRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// LoadProfile ist Load für eine getrennte Instanz (--profile): Konfiguration
// und Daten liegen in einem Unterordner profiles/<name> des normalen
// Konfigurations- bzw. Datenordners (auch unter POSTKAUZ_CONFIG und
// POSTKAUZ_DATA_DIR). data_dir in der Profil-Konfiguration ersetzt den
// Datenordner ganz. Leerer Name = ohne Profil.
func LoadProfile(profile string) (Config, Paths, error) {
	return LoadWith(LoadOptions{Profile: profile})
}

// LoadWith ist Load mit Übersteuerung durch die Kommandozeile. Damit kann
// eine geplante Aufgabe (Windows-Aufgabenplanung, systemd) genau das
// Datenverzeichnis und die Konfigurationsdatei benutzen, die `doctor` im
// interaktiven Fenster gezeigt hat (A7, Befund D3.3c).
func LoadWith(opts LoadOptions) (Config, Paths, error) {
	var cfg Config
	var p Paths
	profile := opts.Profile
	if profile != "" && !ProfileRE.MatchString(profile) {
		return cfg, p, fmt.Errorf("config: invalid profile name %q: use lowercase letters, digits and '-' (max 63)", profile)
	}
	path := opts.ConfigFile
	if path == "" {
		var err error
		if path, err = ConfigPath(); err != nil {
			return cfg, p, err
		}
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return cfg, p, err
		}
		path = abs
	}
	// Clean auch absolut: ein abschließender Trenner (C:\x\) zerstört unter
	// Windows das Quoting in der Kommandozeile einer Aufgabe (Gegenprüfung A7).
	path = filepath.Clean(path)
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
	overridden := false
	if d := os.Getenv(EnvDataDir); d != "" {
		cfg.DataDir, overridden = d, true
	}
	if opts.DataDir != "" {
		cfg.DataDir, overridden = opts.DataDir, true
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
	// [approval] ttl ist der neue Name für [draft] approval_ttl (E3); beide
	// verschieden gesetzt ist mehrdeutig.
	if a, d := cfg.Approval.TTL, cfg.Draft.ApprovalTTL; a != 0 && d != 0 && a != d {
		return cfg, p, fmt.Errorf("config: approval.ttl and draft.approval_ttl are both set and differ; keep only approval.ttl")
	} else if a != 0 {
		cfg.Draft.ApprovalTTL = a
	}
	if err := checkDraft(&cfg.Draft); err != nil {
		return cfg, p, err
	}
	if err := checkRetention(&cfg); err != nil {
		return cfg, p, err
	}
	cfg.Approval.TTL = cfg.Draft.ApprovalTTL
	if cfg.DataDir == "" {
		var err error
		if cfg.DataDir, err = DefaultDataDir(); err != nil {
			return cfg, p, err
		}
	}
	if profile != "" && (fileDataDir == "" || overridden) {
		cfg.DataDir = filepath.Join(cfg.DataDir, "profiles", profile)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		abs, err := filepath.Abs(cfg.DataDir)
		if err != nil {
			return cfg, p, err
		}
		cfg.DataDir = abs
	}
	cfg.DataDir = filepath.Clean(cfg.DataDir)
	p.DataDir = cfg.DataDir
	p.Database = filepath.Join(p.DataDir, brand.Name+".db")
	p.Blobs = filepath.Join(p.DataDir, "blobs")
	p.Log = filepath.Join(p.DataDir, brand.Name+".log")
	p.SecretsDir = filepath.Join(p.DataDir, "insecure-secrets")
	p.AttachDir = filepath.Join(p.DataDir, DefaultAttachDirName)
	if d := strings.TrimSpace(cfg.Paths.AttachDir); d != "" {
		if !filepath.IsAbs(d) {
			d = filepath.Join(p.DataDir, d)
		}
		p.AttachDir = filepath.Clean(d)
	}
	cfg.Paths.AttachDir = p.AttachDir
	p.ExportDir = filepath.Join(p.DataDir, DefaultExportDirName)
	if d := strings.TrimSpace(cfg.Paths.ExportDir); d != "" {
		if !filepath.IsAbs(d) {
			d = filepath.Join(p.DataDir, d)
		}
		p.ExportDir = filepath.Clean(d)
	}
	cfg.Paths.ExportDir = p.ExportDir
	return cfg, p, nil
}

// checkDraft prüft [draft] und setzt die Standards.
func checkDraft(d *Draft) error {
	switch {
	case d.ApprovalTTL == 0:
		d.ApprovalTTL = DefaultApprovalTTL
	case d.ApprovalTTL < MinApprovalTTL || d.ApprovalTTL > MaxApprovalTTL:
		return fmt.Errorf("config: draft.approval_ttl %s is out of range (%s to %s; 0 = default %s)", d.ApprovalTTL, MinApprovalTTL, MaxApprovalTTL, DefaultApprovalTTL)
	}
	switch v := d.MaxAttachmentBytes; {
	case v == 0:
		d.MaxAttachmentBytes = DefaultMaxAttachmentBytes
	case v < 1 || v > MaxAttachmentBytesLimit:
		return fmt.Errorf("config: draft.max_attachment_bytes %d is out of range (1 to %d; 0 = default %d)", v, MaxAttachmentBytesLimit, DefaultMaxAttachmentBytes)
	}
	d.Mailbox = strings.TrimSpace(d.Mailbox)
	if strings.ContainsAny(d.Mailbox, "\r\n\x00") {
		return fmt.Errorf("config: draft.mailbox contains a line break")
	}
	if d.QuoteLine == "" {
		d.QuoteLine = DefaultQuoteLine
	}
	if strings.ContainsAny(d.QuoteLine, "\r\n") {
		return fmt.Errorf("config: draft.quote_line must be one line")
	}
	rest := strings.NewReplacer("{date}", "", "{date_numeric}", "", "{from}", "").Replace(d.QuoteLine)
	if strings.ContainsAny(rest, "{}") {
		return fmt.Errorf("config: draft.quote_line knows only the placeholders {date}, {date_numeric} and {from}")
	}
	return nil
}

// checkRetention prüft [retention] und [audit] retain und setzt die
// Standards (T3, E8, A11).
func checkRetention(cfg *Config) error {
	switch v := cfg.Retention.BlobGCMinAge.D(); {
	case v == 0:
		cfg.Retention.BlobGCMinAge = Duration(DefaultBlobGCMinAge)
	case v < MinBlobGCMinAge:
		return fmt.Errorf("config: retention.blob_gc_min_age %s is too short (at least %s; 0 = default %s)", v, MinBlobGCMinAge, DefaultBlobGCMinAge)
	}
	switch v := cfg.Audit.Retain.D(); {
	case v == 0:
		cfg.Audit.Retain = Duration(DefaultAuditRetain)
	case v < MinAuditRetain:
		return fmt.Errorf("config: audit.retain %s is too short (at least 1d; 0 = default 365d)", v)
	}
	return nil
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
