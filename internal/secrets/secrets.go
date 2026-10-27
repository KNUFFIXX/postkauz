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

// tokenFieldRE findet die Werte von Token-Feldern in JSON oder
// Formularkodierung (access_token, refresh_token, id_token, device_code,
// code, client_secret, assertion), tokenJWTRE JWT-förmige Folgen
// (drei Base64url-Teile, beginnend mit "eyJ").
var (
	tokenFieldRE = regexp.MustCompile(`(?i)("?\b(?:access_token|refresh_token|id_token|device_code|code|client_secret|assertion)"?\s*[:=]\s*"?)([^"&,\s}]+)`)
	tokenJWTRE   = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]*`)
)

// RedactTokens schwärzt Token-Werte in einem Text, bevor er in eine Ausgabe,
// ein Log oder einen Fehler gelangt (S3.7): Antworten eines Token-Endpunkts
// (JSON oder Formular) und JWT-förmige Folgen. Der Typ Secret schützt nur
// Werte, die als Secret vorliegen; diese Funktion fängt Text, der ein Token
// enthalten kann, ohne dass das sichtbar ist (z. B. der Antwortkörper in
// oauth2.RetrieveError).
func RedactTokens(s string) string {
	s = tokenFieldRE.ReplaceAllString(s, "${1}"+redacted)
	return tokenJWTRE.ReplaceAllString(s, redacted)
}

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

// refRE: Art, optional die Kennung des Datenverzeichnisses (8 Hex, seit
// S2.0/A2), Name. Beispiele: "account/demo" (bis S1, Altform),
// "account/1a2b3c4d/demo".
var refRE = regexp.MustCompile(`^[a-z][a-z0-9]*/(?:[0-9a-f]{8}/)?[a-z0-9][a-z0-9-]{0,62}$`)

// dataDirIDRE ist die Form der Kennung des Datenverzeichnisses im Verweis.
var dataDirIDRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ValidRef prüft die Form eines Verweises, z. B. "account/1a2b3c4d/demo".
func ValidRef(ref string) error {
	if !refRE.MatchString(ref) {
		return fmt.Errorf("secrets: invalid reference %q", ref)
	}
	return nil
}

// ValidDataDirID prüft die Kennung des Datenverzeichnisses (8 Hex).
func ValidDataDirID(id string) error {
	if !dataDirIDRE.MatchString(id) {
		return fmt.Errorf("secrets: invalid data directory id %q", id)
	}
	return nil
}

// AccountRef liefert den Verweis für das Passwort eines Kontos:
// "account/<datadir-id>/<name>". Die Kennung des Datenverzeichnisses
// (store.DataDirID) trennt die Einträge zweier Datenverzeichnisse (Profile)
// mit gleichem Kontonamen (S2.0/A2).
func AccountRef(dataDirID, account string) string {
	return "account/" + dataDirID + "/" + account
}

// LegacyAccountRef ist der Verweis bis S1 ("account/<name>"); beim ersten
// Öffnen mit Schema 8 zieht der Eintrag nach AccountRef um.
func LegacyAccountRef(account string) string { return "account/" + account }

// EnvName liefert die Umgebungsvariable, die einen Verweis überschreibt:
// "account/1a2b3c4d/demo-1" (wie "account/demo-1") → POSTKAUZ_PASSWORD_DEMO_1.
// Die Kennung des Datenverzeichnisses gehört nicht dazu: Die Umgebung gilt
// je Prozess, und der Name bleibt so von außen vorhersagbar. Andere
// Verweisarten: POSTKAUZ_SECRET_<ART>_<NAME>.
func EnvName(ref string) string {
	kind, name, _ := strings.Cut(ref, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if kind == "account" {
		return brand.EnvPrefix + "_PASSWORD_" + name
	}
	return brand.EnvPrefix + "_SECRET_" + strings.ToUpper(kind) + "_" + name
}

// Stored liefert den Speicher unter der Umgebungsschicht (WithEnv): Get
// liest dann nur, was wirklich gespeichert ist. Für den Umzug von Einträgen
// und für `doctor`; ein Wert aus der Umgebung darf nie in den Schlüsselbund
// kopiert werden.
func Stored(st Store) Store {
	if e, ok := st.(envStore); ok {
		return e.base
	}
	return st
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
