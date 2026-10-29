package secrets

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnvCredentialsDir nennt das Verzeichnis der systemd-Credentials
// (LoadCredential=, ab systemd 247). Zweite Nachbesserung T5, Punkt 4: Auf
// dem VPS kommt das Passwort nicht mehr über die Umgebung
// (EnvironmentFile= stand in /proc/<pid>/environ, bis der Prozess sich
// schützte), sondern als Datei $CREDENTIALS_DIRECTORY/<EnvName(ref)>, etwa
// POSTKAUZ_PASSWORD_DEMO_1. systemd legt sie nur für den Nutzer des
// Dienstes lesbar in einen nicht auslagerbaren Speicher.
const EnvCredentialsDir = "CREDENTIALS_DIRECTORY"

// maxCredentialBytes: größer ist kein Passwort (systemd erlaubt 1 MB je
// Unit).
const maxCredentialBytes = 64 << 10

// ErrCredential: Die Credential-Datei ist da, aber nicht brauchbar (kein
// reguläre Datei, zu groß, nicht lesbar). Kein stiller Rückfall auf den
// Schlüsselbund. Die Meldung nennt nur den Namen, nie den Inhalt.
var ErrCredential = errors.New("secrets: systemd credential not usable")

// readCredential liest die Credential-Datei name aus dir. ok = false, wenn
// dir leer oder nicht absolut ist oder die Datei fehlt. Ein einzelner
// Zeilenumbruch am Ende ("\n" oder "\r\n") gehört nicht zum Passwort
// (Dateien aus einem Editor oder echo).
func readCredential(dir, name string) (string, bool, error) {
	if dir == "" || !filepath.IsAbs(dir) || name == "" || strings.ContainsAny(name, `/\`) {
		return "", false, nil
	}
	p := filepath.Join(dir, name)
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("%w: %s: %v", ErrCredential, name, errors.Unwrap(err))
	case !fi.Mode().IsRegular():
		return "", false, fmt.Errorf("%w: %s is not a regular file", ErrCredential, name)
	}
	f, err := os.Open(p)
	if err != nil {
		return "", false, fmt.Errorf("%w: %s: %v", ErrCredential, name, errors.Unwrap(err))
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxCredentialBytes+1))
	if err != nil {
		return "", false, fmt.Errorf("%w: %s: read failed", ErrCredential, name)
	}
	if len(b) > maxCredentialBytes {
		return "", false, fmt.Errorf("%w: %s is larger than %d bytes", ErrCredential, name, maxCredentialBytes)
	}
	s := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if s == "" {
		return "", false, nil
	}
	return s, true, nil
}

// FromEnvironment liefert den Wert für ref, wenn er von außen kommt: aus
// der Umgebungsvariable EnvName(ref) (Vorrang) oder aus der
// systemd-Credential-Datei gleichen Namens. source ist "environment" bzw.
// "credential"; ok = false, wenn beides fehlt.
func FromEnvironment(ref string) (s Secret, source string, ok bool, err error) {
	return fromEnvironment(ref, os.LookupEnv)
}

func fromEnvironment(ref string, lookup func(string) (string, bool)) (Secret, string, bool, error) {
	name := EnvName(ref)
	if v, ok := lookup(name); ok && v != "" {
		return Secret(v), "environment", true, nil
	}
	dir, _ := lookup(EnvCredentialsDir)
	v, ok, err := readCredential(dir, name)
	if err != nil || !ok {
		return "", "", false, err
	}
	return Secret(v), "credential", true, nil
}
