package compose

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Anhang-Sperre (A08): Eine lokale Datei wird nie Anhang, wenn ihr Pfad
// unterhalb von attach_dir einen gesperrten Ordner enthält, ihr Name auf ein
// gesperrtes Muster passt, sie in den eigenen Ordnern von PostKauz liegt
// (Datenverzeichnis, Konfigurationsordner, export_dir) oder der Pfad einen
// alternativen Datenstrom nennt. Die Standardlisten sind fest eingebaut und
// nicht abschaltbar; die Konfiguration kann sie nur erweitern
// ([draft] attach_deny_dirs, attach_deny_files). Geprüft wird der Name,
// nicht der Inhalt: Eine umbenannte Schlüsseldatei erkennt die Sperre nicht.

// Regelarten einer Ablehnung durch die Anhang-Sperre (Error.Rule).
const (
	RuleDir     = "dir"
	RuleFile    = "file"
	RuleOwnData = "own_data"
	RuleStream  = "stream"
)

// DefaultDenyDirs sind die fest gesperrten Ordnernamen (Vergleich ohne
// Groß-/Kleinschreibung, Unicode-NFC).
var DefaultDenyDirs = []string{
	"_secrets", "secrets", ".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".config",
	".mcp-auth", ".gmail-mcp", ".email-mcp", ".credentials", "credentials",
	"Passwoerter", "Passwörter", "passwords", "keepass", ".git",
}

// DefaultDenyFiles sind die fest gesperrten Muster für Dateinamen (wie
// path.Match, ohne Groß-/Kleinschreibung). Jedes Muster gilt auch mit
// angehängter Sicherungsendung (backupSuffixes).
var DefaultDenyFiles = []string{
	"*.kdbx", "*.kdb", "*.keyx", "*.key", "*.pem", "*.pfx", "*.p12", "*.ppk", "*.jks", "*.keystore",
	"id_rsa*", "id_ecdsa*", "id_ed25519*",
	".env", ".env.*", "*.env", ".netrc", "_netrc", ".npmrc", ".pypirc", ".git-credentials",
	"credentials*.json", "client_secret*.json", "token.json", "tokens.json", "*token*.json",
	"*secret*", "*passw*", "*.ovpn", "config.toml", "claude_desktop_config*.json",
	"*.local.json", "*.local.toml", "*.kdbx.*",
}

// backupSuffixes hängt die Sperre an jedes Muster an: SECRETS.md.bak_x,
// config.toml.bak, config.toml.old, config.toml~, config.toml.orig.
var backupSuffixes = []string{".bak*", ".old", "~", ".orig"}

// OwnPaths sind die Orte von PostKauz selbst (Regel own_data). Ordner
// (DataDir, ConfigDir, ExportDir) sperren alles darunter, außer attach_dir
// liegt in ihnen oder ist mit ihnen gleich (Standard: attach_dir ist
// <Datenverzeichnis>/attach); dann sperren nur die einzelnen Einträge in
// Entries (Datenbank, Blobs, Log, Konfigurationsdatei …), die immer gelten.
type OwnPaths struct {
	DataDir   string
	ConfigDir string
	ExportDir string
	// Entries: einzelne Dateien oder Ordner, die immer gesperrt sind.
	Entries []string
}

// AttachGuard ist die Anhang-Sperre. Der Nullwert und nil gelten als
// Standardlisten ohne eigene Orte.
type AttachGuard struct {
	dirs    []string // gefaltet (NFC, klein)
	files   []string // gefaltet, ohne Sicherungsendungen
	nDirs   int
	nFiles  int
	broad   []string // Ordner von PostKauz (Regel own_data)
	entries []string // immer gesperrte Einträge
	set     bool
}

// NewAttachGuard baut die Sperre aus den Standardlisten, den Erweiterungen
// der Konfiguration und den eigenen Orten. Ein ungültiger Eintrag ist ein
// Fehler (leer, Pfadtrenner, „..“, ungültiges Muster).
func NewAttachGuard(extraDirs, extraFiles []string, own OwnPaths) (*AttachGuard, error) {
	if err := CheckDenyEntries("attach_deny_dirs", extraDirs, false); err != nil {
		return nil, err
	}
	if err := CheckDenyEntries("attach_deny_files", extraFiles, true); err != nil {
		return nil, err
	}
	g := &AttachGuard{set: true}
	for _, d := range append(slices.Clone(DefaultDenyDirs), extraDirs...) {
		if f := fold(strings.TrimSpace(d)); !slices.Contains(g.dirs, f) {
			g.dirs = append(g.dirs, f)
		}
	}
	for _, p := range append(slices.Clone(DefaultDenyFiles), extraFiles...) {
		if f := fold(strings.TrimSpace(p)); !slices.Contains(g.files, f) {
			g.files = append(g.files, f)
		}
	}
	g.nDirs, g.nFiles = len(g.dirs), len(g.files)
	for _, d := range []string{own.DataDir, own.ConfigDir, own.ExportDir} {
		if p := absClean(d); p != "" {
			g.broad = append(g.broad, p)
		}
	}
	for _, e := range own.Entries {
		if p := absClean(e); p != "" {
			g.entries = append(g.entries, p)
		}
	}
	return g, nil
}

// CheckDenyEntries prüft die Einträge einer Erweiterung (Konfiguration):
// nicht leer, kein Pfadtrenner, kein „..“, bei Mustern gültig für
// path.Match. key nennt den Schlüssel in der Meldung.
func CheckDenyEntries(key string, list []string, pattern bool) error {
	for i, e := range list {
		s := strings.TrimSpace(e)
		switch {
		case s == "":
			return fmt.Errorf("draft.%s[%d] is empty; give a folder or file name (it can only add to the built-in list)", key, i)
		case strings.ContainsAny(s, `/\`):
			return fmt.Errorf("draft.%s[%d] %q contains a path separator; give a single folder or file name, not a path", key, i, e)
		case strings.Contains(s, ".."):
			return fmt.Errorf("draft.%s[%d] %q contains \"..\"; give a single folder or file name", key, i, e)
		case strings.ContainsAny(s, "\x00\r\n"):
			return fmt.Errorf("draft.%s[%d] contains a control character", key, i)
		}
		if pattern {
			if _, err := path.Match(fold(s), ""); err != nil {
				return fmt.Errorf("draft.%s[%d] %q is not a valid pattern (use * and ? as in *.xlsm)", key, i, e)
			}
		}
	}
	return nil
}

func (g *AttachGuard) orDefault() *AttachGuard {
	if g != nil && g.set {
		return g
	}
	d, _ := NewAttachGuard(nil, nil, OwnPaths{})
	return d
}

// Rules liefert die Zahl der Ordner- und Dateiregeln (doctor).
func (g *AttachGuard) Rules() (dirs, files int) {
	g = g.orDefault()
	return g.nDirs, g.nFiles
}

// foldCaser faltet nach Unicode (zustandslos, nebenläufig nutzbar).
var foldCaser = cases.Fold()

// fold vergleicht ohne Groß-/Kleinschreibung in Unicode-NFC
// („Passwörter“ in NFD ist derselbe Name). Unicode-Faltung statt
// strings.ToLower (Nachbesserung A08, Punkt 6; golang.org/x/text ist schon
// Abhängigkeit): auch Sonderfälle wie „ẞ“/„ß“ → „ss“ oder das Kelvin-Zeichen
// gelten als gleich; das sperrt höchstens mehr. Die Faltung erhält keine
// Normalform, daher danach erneut NFC.
func fold(s string) string {
	return norm.NFC.String(foldCaser.String(norm.NFC.String(s)))
}

func absClean(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	return filepath.Clean(a)
}

// copyMarker sind die Zusätze, die Windows (Explorer, auch Browser beim
// Herunterladen) an Kopien hängt, in gefalteter Form: „ - Kopie“, „ - Copy“,
// je mit oder ohne „ (n)“, und „ (n)“ allein mit beliebiger Zahl (A08b).
// Vor „(n)“ sind beliebig viele Leerzeichen erlaubt, auch keins
// („config(1).toml“); das sperrt höchstens mehr.
var copyMarker = regexp.MustCompile(` - (?:kopie|copy)(?: *\([0-9]+\))?| *\([0-9]+\)`)

// maxCopyName: Kopie-Formen (copyVariants) nur für Namen bis zu dieser
// Länge in Bytes (A09 Teil 2 c). Ein echter Datei- oder Ordnername ist
// kürzer (NTFS und ext4: 255 Zeichen bzw. Bytes je Pfadteil).
const maxCopyName = 1024

// copyVariants liefert zu einem gefalteten Namen die Formen ohne
// Kopiezusätze (A08b, Offene Frage 5): Ein Zusatz zählt nur direkt vor
// einem Punkt (Dateiendung, auch „.bak“), vor „~“ oder am Namensende
// („config - Kopie.toml“, „config.toml - Kopie“, „.env (1)“,
// „config - Copy (2).toml“). Jede Runde entfernt alle solchen Zusätze; die
// Runden laufen, bis nichts mehr wegfällt, so dass auch die Kopie einer
// Kopie („config (1) - Kopie.toml“) auf „config.toml“ kommt. Alle
// Zwischenformen zählen mit. Der Name selbst gehört nicht zum Ergebnis.
//
// Seit A09 Teil 2 (c) ohne feste Rundenzahl (vorher 16: ein Name mit 17
// verketteten Zusätzen ging durch); die Schleife endet, weil jede Runde den
// Namen um mindestens drei Bytes kürzt („(0)“). Formen gibt es nur für
// Namen bis maxCopyName Bytes (ein Name aus 1 MiB „ (1)“ kostete sonst
// Sekunden); der Name selbst wird immer geprüft.
func copyVariants(n string) []string {
	if len(n) > maxCopyName {
		return nil
	}
	var out []string
	for {
		next := stripCopyMarkers(n)
		if next == n || next == "" {
			break
		}
		out = append(out, next)
		n = next
	}
	return out
}

// stripCopyMarkers entfernt in einer Runde jeden Kopiezusatz, der direkt
// vor „.“, „~“ oder am Ende steht.
func stripCopyMarkers(n string) string {
	var b strings.Builder
	last := 0
	for _, m := range copyMarker.FindAllStringIndex(n, -1) {
		if m[1] < len(n) && n[m[1]] != '.' && n[m[1]] != '~' {
			continue
		}
		b.WriteString(n[last:m[0]])
		last = m[1]
	}
	if last == 0 {
		return n
	}
	b.WriteString(n[last:])
	return b.String()
}

// denyDir: ein Ordnername der Liste, auch ohne Kopiezusätze
// („_secrets - Kopie“, „.ssh (1)“; A08b).
func (g *AttachGuard) denyDir(name string) bool {
	n := fold(name)
	if slices.Contains(g.dirs, n) {
		return true
	}
	for _, v := range copyVariants(n) {
		if slices.Contains(g.dirs, v) {
			return true
		}
	}
	return false
}

// DeniedName sagt, ob ein Dateiname auf ein Muster der Sperre passt
// (Regel file); nur der letzte Pfadteil zählt. Geprüft wird der Name selbst
// und jede Form ohne Windows-Kopiezusätze (copyVariants, A08b), jeweils
// auch mit Sicherungsendung: „config (1).toml.bak“ wie „config.toml.bak“.
// Die Formen kommen nur hinzu, der Name selbst wird immer geprüft; die
// Regel sperrt dadurch höchstens mehr.
func (g *AttachGuard) DeniedName(name string) bool {
	g = g.orDefault()
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	n := fold(name)
	if n == "" {
		return false
	}
	if g.deniedFolded(n) {
		return true
	}
	for _, v := range copyVariants(n) {
		if g.deniedFolded(v) {
			return true
		}
	}
	return false
}

// deniedFolded: ein gefalteter Name passt auf ein Muster, auch mit
// Sicherungsendung.
func (g *AttachGuard) deniedFolded(n string) bool {
	for _, p := range g.files {
		if ok, _ := path.Match(p, n); ok {
			return true
		}
		for _, s := range backupSuffixes {
			if ok, _ := path.Match(p+s, n); ok {
				return true
			}
		}
	}
	return false
}

// deniedAttachName prüft einen Anhangsnamen so, wie er gespeichert und
// verschickt würde: nach der Bereinigung durch attachName (unsichtbare und
// Steuerzeichen fallen weg, „.e\u200bnv“ wird „.env“) und zur Vorsicht auch
// in der angegebenen Form (Nachbesserung A08, Punkt 3).
func (g *AttachGuard) deniedAttachName(name string) bool {
	return g.DeniedName(attachName(name)) || g.DeniedName(name)
}

// hasStream: Der Pfad nennt einen alternativen Datenstrom (NTFS
// „datei:strom“). Ein Laufwerksteil („C:“, „\\?\C:“) zählt nicht; jeder
// andere Doppelpunkt wird auf allen Systemen abgelehnt.
func hasStream(p string) bool {
	rest := p
	if v := filepath.VolumeName(rest); v != "" {
		rest = rest[len(v):]
	}
	for _, pre := range []string{`\\?\`, `\\.\`, `//?/`, `//./`} {
		rest = strings.TrimPrefix(rest, pre)
	}
	if len(rest) >= 2 && rest[1] == ':' && isLetter(rest[0]) {
		rest = rest[2:]
	}
	return strings.Contains(rest, ":")
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// components zerlegt einen Pfad relativ zu dir in seine Teile (ohne „.“);
// ok=false, wenn p nicht unterhalb von dir liegt.
func components(dir, p string) ([]string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, false
	}
	var out []string
	for _, c := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if c != "." && c != "" {
			out = append(out, c)
		}
	}
	return out, true
}

// attachDenied prüft eine Datei gegen die Sperre (A08): dir ist attach_dir
// (absolut, wie angegeben), realDir aufgelöst, target der angefragte Pfad
// (absolut, bereinigt, noch nicht aufgelöst), realTarget der aufgelöste
// ("" = noch nicht aufgelöst: nur die lexikalischen Regeln). Liefert die
// Regelart oder "".
func (g *AttachGuard) attachDenied(dir, realDir, target, realTarget string) string {
	g = g.orDefault()
	type cand struct{ base, p string }
	cands := []cand{{dir, target}, {realDir, target}}
	if realTarget != "" {
		cands = append(cands, cand{realDir, realTarget})
	}
	rule := ""
	for _, c := range cands {
		parts, ok := components(c.base, c.p)
		if !ok || len(parts) == 0 {
			continue
		}
		for _, d := range parts[:len(parts)-1] {
			if g.denyDir(d) {
				return RuleDir
			}
		}
		if rule == "" && g.DeniedName(parts[len(parts)-1]) {
			rule = RuleFile
		}
	}
	if rule == "" && g.DeniedName(filepath.Base(target)) {
		rule = RuleFile
	}
	if rule != "" {
		return rule
	}
	if realTarget != "" && g.ownData(realDir, realTarget) {
		return RuleOwnData
	}
	return ""
}

// ownData: realTarget liegt in einem Ort von PostKauz. Ein Ordner zählt
// nicht, wenn attach_dir in ihm liegt oder mit ihm gleich ist (sonst wäre
// der Standard <Datenverzeichnis>/attach ganz gesperrt); die einzelnen
// Einträge gelten immer.
func (g *AttachGuard) ownData(realDir, realTarget string) bool {
	for _, e := range g.entries {
		if withinFold(e, realTarget) {
			return true
		}
	}
	for _, b := range g.broad {
		if withinFold(b, realDir) {
			continue
		}
		if withinFold(b, realTarget) {
			return true
		}
	}
	return false
}

// withinFold: p liegt in dir oder ist dir, ohne Groß-/Kleinschreibung
// (Windows und macOS unterscheiden sie nicht; auf Linux sperrt das
// höchstens mehr).
func withinFold(dir, p string) bool {
	d, q := fold(filepath.Clean(dir)), fold(filepath.Clean(p))
	if d == q {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(d, sep) {
		d += sep
	}
	return strings.HasPrefix(q, d)
}

// guardName ist der Dateiname für Audit und Fehler (Error.Name): nur der
// letzte Pfadteil, höchstens 40 Zeichen, ohne Steuerzeichen.
func guardName(p string) string {
	s := attachName(p)
	if utf8.RuneCountInString(s) > 40 {
		s = string([]rune(s)[:40])
	}
	return s
}

// denyGuard baut die Ablehnung attach_denied: Meldung nur mit Feld und
// Regelart, nie mit Pfad oder Inhalt.
func denyGuard(field, rule, name string) *Error {
	e := deny(ReasonAttachDenied, field, "the file is blocked by the attachment guard (rule: %s)", rule)
	e.Rule, e.Name = rule, guardName(name)
	return e
}

// GuardOf liefert Regelart und gekürzten Dateinamen einer Ablehnung
// attach_denied (für das Audit); ok=false bei anderen Fehlern.
func GuardOf(err error) (rule, name string, ok bool) {
	var e *Error
	if errors.As(err, &e) && e.Reason == ReasonAttachDenied {
		return e.Rule, e.Name, true
	}
	return "", "", false
}
