// Package secrets verwaltet Zugangsdaten (Passwörter, später OAuth-Token).
//
// Rangfolge (docs/sicherheit.md, Abschnitt 4):
//
//  1. Umgebungsvariable POSTKAUZ_PASSWORD_<KONTO> (nur lesen; für Server und
//     Docker, wo es keinen Schlüsselbund gibt),
//  2. Schlüsselbund des Betriebssystems über github.com/zalando/go-keyring
//     (Windows Credential Manager, macOS Keychain, Secret Service),
//  3. nur auf ausdrücklichen Wunsch (secrets.backend = "insecure-file"): eine
//     AES-GCM-verschlüsselte Datei im Datenverzeichnis, deren Schlüssel
//     daneben liegt. Das schützt nur vor versehentlichem Mitlesen (grep,
//     Kopie der Datenbank), nicht vor einem Angreifer mit Dateizugriff.
//     UNSICHER, nur für Tests und Testserver.
//
// Es gibt keinen stillen Rückfall: Fehlt der Schlüsselbund, schlägt das
// Speichern mit einem Hinweis fehl.
//
// Datenbank und Konfiguration enthalten nur den Verweis (credential_ref).
// Geheimnisse erscheinen nie in Logs, Audit, Fehlermeldungen oder JSON;
// der Typ Secret schwärzt sich bei jeder Formatierung selbst.
package secrets

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/brand"
)

// Secret ist ein geheimer Wert. Formatierung, JSON und slog liefern
// "[redacted]"; den Klartext gibt nur Reveal heraus.
type Secret string

const redacted = "[redacted]"

// Reveal liefert den Klartext. Nur an der Stelle aufrufen, die ihn an den
// Server übergibt.
func (s Secret) Reveal() string { return string(s) }

// IsZero meldet einen leeren Wert.
func (s Secret) IsZero() bool { return s == "" }

func (s Secret) String() string                { return redacted }
func (s Secret) GoString() string              { return redacted }
func (s Secret) MarshalJSON() ([]byte, error)  { return []byte(`"` + redacted + `"`), nil }
func (s Secret) MarshalText() ([]byte, error)  { return []byte(redacted), nil }
func (s Secret) LogValue() slog.Value          { return slog.StringValue(redacted) }
func (s Secret) Format(f fmt.State, verb rune) { _, _ = f.Write([]byte(redacted)) }

// Fehler, die Aufrufer unterscheiden.
var (
	// ErrNotFound: Unter dem Verweis ist nichts gespeichert.
	ErrNotFound = errors.New("secrets: not found")
	// ErrUnavailable: Der gewählte Speicher ist nicht erreichbar (z. B. kein
	// Secret Service unter Linux).
	ErrUnavailable = errors.New("secrets: secret store unavailable")
)

// Store speichert Geheimnisse unter einem Verweis (credential_ref).
type Store interface {
	Get(ref string) (Secret, error)
	Set(ref string, s Secret) error
	Delete(ref string) error
	// Status beschreibt den Speicher für `doctor`.
	Status() Status
}

// Status ist die Selbstauskunft eines Speichers.
type Status struct {
	Backend   string `json:"backend"`   // keyring | insecure-file | none
	Available bool   `json:"available"` // Lesen und Schreiben möglich
	Insecure  bool   `json:"insecure"`  // nur für Tests geeignet
	Detail    string `json:"detail,omitempty"`
}

var refRE = regexp.MustCompile(`^[a-z][a-z0-9]*/[a-z0-9][a-z0-9-]{0,62}$`)

// ValidRef prüft die Form eines Verweises, z. B. "account/demo".
func ValidRef(ref string) error {
	if !refRE.MatchString(ref) {
		return fmt.Errorf("secrets: invalid reference %q", ref)
	}
	return nil
}

// AccountRef liefert den Verweis für das Passwort eines Kontos.
func AccountRef(account string) string { return "account/" + account }

// EnvName liefert die Umgebungsvariable, die einen Verweis überschreibt:
// "account/demo-1" → POSTKAUZ_PASSWORD_DEMO_1. Andere Verweisarten:
// POSTKAUZ_SECRET_<ART>_<NAME>.
func EnvName(ref string) string {
	kind, name, _ := strings.Cut(ref, "/")
	name = strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if kind == "account" {
		return brand.EnvPrefix + "_PASSWORD_" + name
	}
	return brand.EnvPrefix + "_SECRET_" + strings.ToUpper(kind) + "_" + name
}

// WithEnv legt die Umgebungsvariablen über einen Speicher: Get liest zuerst
// die Umgebung; Set und Delete gehen an den Speicher darunter.
func WithEnv(base Store) Store { return envStore{base: base, lookup: os.LookupEnv} }

type envStore struct {
	base   Store
	lookup func(string) (string, bool)
}

func (e envStore) Get(ref string) (Secret, error) {
	if err := ValidRef(ref); err != nil {
		return "", err
	}
	if v, ok := e.lookup(EnvName(ref)); ok && v != "" {
		return Secret(v), nil
	}
	return e.base.Get(ref)
}
func (e envStore) Set(ref string, s Secret) error { return e.base.Set(ref, s) }
func (e envStore) Delete(ref string) error        { return e.base.Delete(ref) }
func (e envStore) Status() Status                 { return e.base.Status() }

// Unavailable ist der Speicher, wenn der Schlüsselbund fehlt und kein
// ausdrücklicher Ersatz gewählt ist. Lesen liefert ErrNotFound (die
// Umgebung kann darüber liegen), Schreiben ErrUnavailable mit Hinweis.
type Unavailable struct{ Reason string }

func (u Unavailable) err() error {
	return fmt.Errorf("%w (%s); in containers or on servers set the password via the environment variable shown by `%s doctor`, or for test servers only set secrets.backend = \"insecure-file\"",
		ErrUnavailable, u.Reason, brand.Name)
}
func (u Unavailable) Get(ref string) (Secret, error) {
	if err := ValidRef(ref); err != nil {
		return "", err
	}
	return "", ErrNotFound
}
func (u Unavailable) Set(string, Secret) error { return u.err() }
func (u Unavailable) Delete(string) error      { return ErrNotFound }
func (u Unavailable) Status() Status {
	return Status{Backend: "none", Available: false, Detail: u.Reason}
}
