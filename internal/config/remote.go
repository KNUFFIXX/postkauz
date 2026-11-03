package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Remote ist ein Abschnitt [remote.<name>] (A04 Teil 4): Ziel der Brücke
// `mcp --remote <name>`, ein MCP-Endpunkt über Streamable HTTP (in der
// Regel `mcp --http` eines anderen Rechners hinter tailscale serve). Die
// Datei nennt nur die Adresse; der Schlüssel liegt im Schlüsselbund
// (Verweis remote/<name>), nie hier.
type Remote struct {
	URL string `toml:"url"`
}

// RemoteNameRE ist die Form eines Namens (wie Kontonamen).
var RemoteNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// CheckRemoteName prüft einen Namen.
func CheckRemoteName(name string) error {
	if !RemoteNameRE.MatchString(name) {
		return fmt.Errorf("invalid remote name %q: use lowercase letters, digits and '-' (max 63, first a letter or digit)", name)
	}
	return nil
}

// CheckRemoteURL prüft die Adresse eines Ziels: https mit Host und Pfad
// (bis /mcp); http nur zu einer Loopback-Adresse (Tests, lokaler Versuch).
// Keine Zugangsdaten in der Adresse, kein Fragment, keine Zeichen, die in
// der TOML-Zeile oder im Log stören.
func CheckRemoteURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("remote url is empty")
	}
	if strings.ContainsAny(raw, "\"\\ \t\r\n") || strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil, errors.New("remote url contains a space, quote, backslash or control character")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("remote url is not a valid URL")
	}
	switch {
	case u.User != nil:
		return nil, errors.New("remote url must not contain a user name or password; the key is stored in the keyring")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return nil, errors.New("remote url must not contain a fragment (#...)")
	case u.Host == "" || u.Hostname() == "":
		return nil, errors.New("remote url needs a host, e.g. https://office-pc.example.ts.net/mcp")
	case u.Path == "" || u.Path == "/":
		return nil, errors.New("remote url needs the path of the MCP endpoint, e.g. https://office-pc.example.ts.net/mcp")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(strings.ToLower(u.Hostname())) {
			return nil, errors.New("remote url must use https; http is allowed only to a loopback address (127.0.0.1, ::1, localhost)")
		}
	default:
		return nil, errors.New("remote url must start with https://")
	}
	return u, nil
}

// checkRemotes prüft die gelesenen Abschnitte [remote.*].
func checkRemotes(m map[string]Remote) error {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := CheckRemoteName(n); err != nil {
			return fmt.Errorf("[remote.%s]: %w", n, err)
		}
		if _, err := CheckRemoteURL(m[n].URL); err != nil {
			return fmt.Errorf("[remote.%s]: %w", n, err)
		}
	}
	return nil
}

// RemoteFile ändert die Abschnitte [remote.<name>] einer
// Konfigurationsdatei (remote add, remote remove). Geändert wird nur der
// eine Abschnitt als Text; Kommentare und alles andere bleiben Zeichen für
// Zeichen. Vor dem Schreiben wird das Ergebnis neu gelesen und geprüft,
// geschrieben wird über eine Zwischendatei (kein halber Stand).
type RemoteFile struct {
	Path string
}

// Remotes liest die Ziele frisch aus der Datei (fehlt sie: keine).
func (f RemoteFile) Remotes() (map[string]Remote, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Remote{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return decodeRemotes(string(b))
}

func decodeRemotes(text string) (map[string]Remote, error) {
	var v struct {
		Remote map[string]Remote `toml:"remote"`
	}
	if _, err := toml.Decode(text, &v); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if v.Remote == nil {
		v.Remote = map[string]Remote{}
	}
	return v.Remote, nil
}

// SetRemote setzt die Adresse des Ziels name (neu oder ersetzt).
func (f RemoteFile) SetRemote(name, rawURL string) error {
	if err := CheckRemoteName(name); err != nil {
		return err
	}
	if _, err := CheckRemoteURL(rawURL); err != nil {
		return err
	}
	text, mode, err := f.read()
	if err != nil {
		return err
	}
	block := []string{"[remote." + name + "]", `url = "` + rawURL + `"`}
	lines := splitLines(text)
	start, end := findRemoteBlock(lines, name)
	if start >= 0 {
		lines = append(lines[:start], append(block, lines[end:]...)...)
	} else {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "# Target of postkauz mcp --remote "+name+"; the key is in the keyring, never here.")
		lines = append(lines, block...)
	}
	out := joinLines(lines, text)
	got, err := decodeRemotes(out)
	if err != nil || got[name].URL != rawURL {
		return fmt.Errorf("config: %s: [remote.%s] could not be written safely (is it written in another form, e.g. as a dotted key?); edit the file by hand", f.Path, name)
	}
	return f.write(out, mode)
}

// RemoveRemote entfernt den Abschnitt; false, wenn es ihn nicht gab.
func (f RemoteFile) RemoveRemote(name string) (bool, error) {
	if err := CheckRemoteName(name); err != nil {
		return false, err
	}
	text, mode, err := f.read()
	if err != nil {
		return false, err
	}
	lines := splitLines(text)
	start, end := findRemoteBlock(lines, name)
	if start < 0 {
		got, err := decodeRemotes(text)
		if err == nil {
			if _, ok := got[name]; ok {
				return false, fmt.Errorf("config: %s: [remote.%s] is written in another form; remove it by hand", f.Path, name)
			}
		}
		return false, nil
	}
	// Ein Kommentar direkt über dem Abschnitt, den remote add geschrieben
	// hat, geht mit.
	if start > 0 && strings.HasPrefix(lines[start-1], "# Target of postkauz mcp --remote "+name+";") {
		start--
	}
	lines = append(lines[:start], lines[end:]...)
	// Die Leerzeile, die remote add davor gesetzt hat, nicht stehen lassen
	// (am Dateiende oder doppelt vor dem Folgenden).
	if start > 0 && strings.TrimSpace(lines[start-1]) == "" && (start == len(lines) || strings.TrimSpace(lines[start]) == "") {
		lines = append(lines[:start-1], lines[start:]...)
	}
	out := joinLines(lines, text)
	got, err := decodeRemotes(out)
	if err != nil {
		return false, fmt.Errorf("config: %s: removing [remote.%s] would break the file; edit it by hand", f.Path, name)
	}
	if _, ok := got[name]; ok {
		return false, fmt.Errorf("config: %s: [remote.%s] is still there after removing its section; edit the file by hand", f.Path, name)
	}
	return true, f.write(out, mode)
}

func (f RemoteFile) read() (string, os.FileMode, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0o600, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("config: %w", err)
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(f.Path); err == nil {
		mode = fi.Mode().Perm()
	}
	if _, err := toml.Decode(string(b), &struct{}{}); err != nil {
		return "", 0, fmt.Errorf("config: %s: %w", f.Path, err)
	}
	return string(b), mode, nil
}

func (f RemoteFile) write(text string, mode os.FileMode) error {
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(text)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, mode)
	}
	if werr == nil {
		werr = os.Rename(name, f.Path)
	}
	if werr != nil {
		_ = os.Remove(name)
		return fmt.Errorf("config: writing %s: %w", f.Path, werr)
	}
	return nil
}

var tableHeaderRE = regexp.MustCompile(`^\s*\[`)

// findRemoteBlock findet den Abschnitt [remote.<name>]: von der Kopfzeile
// bis vor die nächste Tabellenkopfzeile; Leerzeilen und Kommentare am
// Ende bleiben draußen. -1, wenn es ihn nicht gibt.
func findRemoteBlock(lines []string, name string) (int, int) {
	head := regexp.MustCompile(`^\s*\[\s*remote\s*\.\s*(?:` + regexp.QuoteMeta(name) + `|"` + regexp.QuoteMeta(name) + `")\s*\]\s*(?:#.*)?$`)
	for i, l := range lines {
		if !head.MatchString(l) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if tableHeaderRE.MatchString(lines[j]) {
				end = j
				break
			}
		}
		// Leerzeilen und Kommentare am Ende gehören zum nächsten Abschnitt.
		for end > i+1 {
			if l := strings.TrimSpace(lines[end-1]); l != "" && !strings.HasPrefix(l, "#") {
				break
			}
			end--
		}
		return i, end
	}
	return -1, -1
}

// splitLines trennt an \n; ein \r am Zeilenende bleibt beim Lesen der
// Kopfzeile egal und wird beim Zusammensetzen gleich behandelt.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// joinLines setzt die Zeilen mit dem Zeilenende der alten Datei zusammen
// (CRLF, wenn sie CRLF hatte, sonst LF).
func joinLines(lines []string, old string) string {
	if len(lines) == 0 {
		return ""
	}
	nl := "\n"
	if strings.Contains(old, "\r\n") {
		nl = "\r\n"
	}
	return strings.Join(lines, nl) + nl
}
