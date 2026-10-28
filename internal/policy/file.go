package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/KNUFFIXX/postkauz/internal/brand"
)

// Policy-Datei (A02 E8; docs/sicherheit.md, Abschnitt 5): policy.toml neben
// der Konfiguration, POSTKAUZ_POLICY überschreibt den Pfad. Jeder Prozess
// lädt und prüft sie beim Start (unbekannte Schlüssel und ungültige Werte
// sind ein Fehler: lieber kein Start als eine still falsch gelesene
// Richtlinie). Ihr SHA-256 und ihre Version stehen in der Tabelle policies
// (api_key_id NULL), die Version in jedem Audit-Eintrag (policy_version).
//
// Ausgewertet werden ab S2.1 [agent_defaults] max_chars (ersetzt den festen
// Standard 20.000) und [draft] require_approval_for_ready (nur true).
// [draft] check_hook und check_hook_timeout liest und prüft S2.1, seit S2.4
// führt internal/hook den Hook aus; [delete] und [move] wertet seit T1b
// CheckInput aus (File.Rules); [agent_defaults] dry_run_default wird
// gelesen und geprüft, ausgewertet erst in S3.

// EnvPolicy überschreibt den Pfad der Policy-Datei.
var EnvPolicy = brand.EnvPrefix + "_POLICY"

// FileName ist der Name der Policy-Datei neben der Konfiguration.
const FileName = "policy.toml"

// Standards der Policy-Datei.
const (
	// DefaultMaxChars: Zeichenbudget je Text für Agenten (vorher fest in
	// ops.DefaultMaxChars).
	DefaultMaxChars = 20000
	// MinMaxChars und MaxMaxChars begrenzen [agent_defaults] max_chars.
	MinMaxChars = 100
	MaxMaxChars = 1000000
	// DefaultCheckHookTimeout: Zeitgrenze des Prüf-Hooks (E4).
	DefaultCheckHookTimeout = 30 * time.Second
	// MaxCheckHookTimeout: länger wartet keine Vorlage.
	MaxCheckHookTimeout = 10 * time.Minute
	// DefaultBulkConfirmThreshold: bei mehr als so vielen Zielen (über, `>`) verlangt eine
	// Massenaktion confirm_count (S3).
	DefaultBulkConfirmThreshold = 50
)

// FileDoc ist der Inhalt der Policy-Datei mit aufgelösten Standards. Die
// JSON-Form steht in policies.document.
type FileDoc struct {
	Draft         DraftRules    `toml:"draft" json:"draft"`
	Delete        DeleteRules   `toml:"delete" json:"delete"`
	Move          MoveRules     `toml:"move" json:"move"`
	AgentDefaults AgentDefaults `toml:"agent_defaults" json:"agent_defaults"`
	// Keys verengt die Regeln je Schlüssel (E4, seit T2): Abschnitt
	// [keys.<name>] mit move, delete und max_chars. Ein Wert, der weiter ist
	// als der Standard, macht die Datei ungültig.
	Keys map[string]KeyRules `toml:"keys" json:"keys,omitempty"`
}

// KeyRules: Abschnitt [keys.<name>] (leere Werte = wie der Standard).
type KeyRules struct {
	Move     MoveRules   `toml:"move" json:"move"`
	Delete   DeleteRules `toml:"delete" json:"delete"`
	MaxChars int         `toml:"max_chars" json:"max_chars,omitempty"`
}

// DraftRules: Abschnitt [draft] der Policy-Datei (nicht zu verwechseln mit
// [draft] der Konfiguration: approval_ttl, Anhänge, Ordner, Zitatzeile).
type DraftRules struct {
	// RequireApprovalForReady: Nur ein Mensch setzt ready (E2, E7). Im MVP
	// ist nur true erlaubt.
	RequireApprovalForReady *bool `toml:"require_approval_for_ready" json:"require_approval_for_ready"`
	// CheckHook: Programm und Argumente des Prüf-Hooks als Liste, kein
	// Shell-Aufruf (E4); leer = kein Hook (gilt als bestanden).
	CheckHook []string `toml:"check_hook" json:"check_hook"`
	// CheckHookTimeout: Zeitgrenze des Hooks (Standard 30s).
	CheckHookTimeout time.Duration `toml:"check_hook_timeout" json:"-"`
}

// MarshalJSON schreibt die Zeitgrenze als Dauer ("30s") statt in Nanosekunden.
func (d DraftRules) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		RequireApprovalForReady *bool    `json:"require_approval_for_ready"`
		CheckHook               []string `json:"check_hook"`
		CheckHookTimeout        string   `json:"check_hook_timeout"`
	}{d.RequireApprovalForReady, d.CheckHook, d.CheckHookTimeout.String()})
}

// DeleteRules: Abschnitt [delete] (ausgewertet ab S3).
type DeleteRules struct {
	// AgentMay: none | trash_only (endgültiges Löschen gibt es für Agenten nie).
	AgentMay string `toml:"agent_may" json:"agent_may"`
}

// MoveRules: Abschnitt [move] (ausgewertet ab S3).
type MoveRules struct {
	// AgentMay: none | archive_only | within_account.
	AgentMay string `toml:"agent_may" json:"agent_may"`
	// BulkConfirmThreshold: bei mehr als so vielen Zielen (über, `>`) confirm_count.
	BulkConfirmThreshold int `toml:"bulk_confirm_threshold" json:"bulk_confirm_threshold"`
}

// AgentDefaults: Abschnitt [agent_defaults].
type AgentDefaults struct {
	// DryRunDefault: Standard für Agenten (ab S3; heute gilt der Standard
	// je Schlüssel aus key create).
	DryRunDefault *bool `toml:"dry_run_default" json:"dry_run_default"`
	// MaxChars: Zeichenbudget je Text in MCP-Ausgaben (Standard 20000).
	MaxChars int `toml:"max_chars" json:"max_chars"`
}

// File ist eine geladene Policy-Datei.
type File struct {
	// Path ist der Pfad, unter dem gesucht wurde.
	Path string
	// Exists: Die Datei gibt es; sonst gelten die Standards.
	Exists bool
	// SHA256 der Datei (hex); "" ohne Datei.
	SHA256 string
	Doc    FileDoc
}

// Document liefert die JSON-Form für policies.document.
func (f File) Document() string {
	b, err := json.Marshal(f.Doc)
	if err != nil {
		panic("policy: document: " + err.Error()) // nur Zeichenketten, Zahlen, Wahrheitswerte
	}
	return string(b)
}

// MaxChars ist das Zeichenbudget für Agenten.
func (f File) MaxChars() int { return f.Doc.AgentDefaults.MaxChars }

// MaxCharsFor ist das Zeichenbudget für den Schlüssel name ([keys.<name>]
// max_chars, sonst der Standard).
func (f File) MaxCharsFor(name string) int {
	if k, ok := f.Doc.Keys[name]; ok && k.MaxChars > 0 {
		return k.MaxChars
	}
	return f.MaxChars()
}

// DryRunDefault ist [agent_defaults] dry_run_default: der Standard für
// key create ohne --dry-run-default (seit T2; ohne Datei true).
func (f File) DryRunDefault() bool {
	if p := f.Doc.AgentDefaults.DryRunDefault; p != nil {
		return *p
	}
	return true
}

// FilePath liefert den Pfad der Policy-Datei: POSTKAUZ_POLICY, sonst
// policy.toml im Ordner der Konfigurationsdatei. explicit meldet, ob der
// Pfad aus der Umgebung kommt (dann muss die Datei existieren).
func FilePath(configFile string) (path string, explicit bool) {
	if p := os.Getenv(EnvPolicy); p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return filepath.Clean(p), true
	}
	return filepath.Join(filepath.Dir(configFile), FileName), false
}

// ErrInvalidFile: Die Policy-Datei ist ungültig (Kommandos starten nicht).
var ErrInvalidFile = errors.New("policy file is invalid")

// LoadFile liest und prüft die Policy-Datei. Fehlt sie, gelten die
// Standards (Exists false), außer der Pfad wurde ausdrücklich gesetzt
// (POSTKAUZ_POLICY): Dann ist eine fehlende Datei ein Fehler. Fehler nennen
// Datei und Schlüssel, nie Werte (eine Hook-Zeile könnte Pfade enthalten,
// die nicht ins Log gehören; die Datei selbst kennt der Betreiber).
func LoadFile(path string, explicit bool) (File, error) {
	f := File{Path: path}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		f.Exists = true
		sum := sha256.Sum256(raw)
		f.SHA256 = hex.EncodeToString(sum[:])
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		f.Doc = defaults(FileDoc{}, nil)
		return f, nil
	case errors.Is(err, fs.ErrNotExist):
		return f, fmt.Errorf("%w: %s does not exist (set by %s)", ErrInvalidFile, path, EnvPolicy)
	default:
		return f, fmt.Errorf("%w: %s: %v", ErrInvalidFile, path, err)
	}
	var doc FileDoc
	md, err := toml.NewDecoder(bytes.NewReader(raw)).Decode(&doc)
	if err != nil {
		return f, fmt.Errorf("%w: %s: %s", ErrInvalidFile, path, tomlErr(err))
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return f, fmt.Errorf("%w: %s: unknown keys: %s", ErrInvalidFile, path, strings.Join(keys, ", "))
	}
	doc = defaults(doc, md.IsDefined)
	if err := doc.validate(); err != nil {
		return f, fmt.Errorf("%w: %s: %v", ErrInvalidFile, path, err)
	}
	f.Doc = doc
	return f, nil
}

// tomlErr kürzt einen Parserfehler auf Zeile und Art (ohne den Inhalt der Zeile).
func tomlErr(err error) string {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		if pe.LastKey != "" {
			return fmt.Sprintf("line %d (key %s): %s", pe.Position.Line, pe.LastKey, pe.Message)
		}
		return fmt.Sprintf("line %d: %s", pe.Position.Line, pe.Message)
	}
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && strings.HasPrefix(msg, "toml") {
		msg = msg[i+2:]
	}
	return msg
}

// defaults setzt die Standards für Schlüssel, die in der Datei fehlen.
// defined meldet, ob ein Schlüssel in der Datei steht (nil = keine Datei).
// Ein ausdrücklich gesetzter Wert wird nie durch den Standard ersetzt, auch
// 0 oder "" nicht: max_chars = 0 oder bulk_confirm_threshold = 0 sind ein
// Fehler, kein stiller Standard (Gegenprüfung S2.3, K8).
func defaults(d FileDoc, defined func(key ...string) bool) FileDoc {
	missing := func(key ...string) bool { return defined == nil || !defined(key...) }
	yes := true
	if d.Draft.RequireApprovalForReady == nil {
		d.Draft.RequireApprovalForReady = &yes
	}
	if d.Draft.CheckHook == nil {
		d.Draft.CheckHook = []string{}
	}
	if missing("draft", "check_hook_timeout") {
		d.Draft.CheckHookTimeout = DefaultCheckHookTimeout
	}
	if missing("delete", "agent_may") {
		d.Delete.AgentMay = "trash_only"
	}
	if missing("move", "agent_may") {
		d.Move.AgentMay = "within_account"
	}
	if missing("move", "bulk_confirm_threshold") {
		d.Move.BulkConfirmThreshold = DefaultBulkConfirmThreshold
	}
	if d.AgentDefaults.DryRunDefault == nil {
		y := true
		d.AgentDefaults.DryRunDefault = &y
	}
	if missing("agent_defaults", "max_chars") {
		d.AgentDefaults.MaxChars = DefaultMaxChars
	}
	return d
}

func (d FileDoc) validate() error {
	if !*d.Draft.RequireApprovalForReady {
		return errors.New("draft.require_approval_for_ready = false is not supported: only a human can set a draft ready")
	}
	if len(d.Draft.CheckHook) > 0 && strings.TrimSpace(d.Draft.CheckHook[0]) == "" {
		return errors.New("draft.check_hook: the first entry must be the program")
	}
	for i, a := range d.Draft.CheckHook {
		if strings.ContainsAny(a, "\x00\r\n") {
			return fmt.Errorf("draft.check_hook[%d] contains a line break or NUL", i)
		}
	}
	// Meldungen nennen Schlüssel und erlaubten Bereich, nie den Wert (K8).
	if t := d.Draft.CheckHookTimeout; t < time.Second || t > MaxCheckHookTimeout {
		return fmt.Errorf("draft.check_hook_timeout is out of range (1s to %s)", MaxCheckHookTimeout)
	}
	switch d.Delete.AgentMay {
	case "none", "trash_only":
	default:
		return errors.New("delete.agent_may is invalid (expected none or trash_only)")
	}
	switch d.Move.AgentMay {
	case "none", "archive_only", "within_account":
	default:
		return errors.New("move.agent_may is invalid (expected none, archive_only or within_account)")
	}
	// Höchstens MaxTargets (T2 Punkt 7): darüber lehnt CheckInput ohnehin
	// jeden Aufruf ab (too_many_ids); ein größerer Wert hieße „nie
	// confirm_count“.
	if v := d.Move.BulkConfirmThreshold; v < 1 || v > MaxTargets {
		return fmt.Errorf("move.bulk_confirm_threshold is out of range (1 to %d)", MaxTargets)
	}
	if v := d.AgentDefaults.MaxChars; v < MinMaxChars || v > MaxMaxChars {
		return fmt.Errorf("agent_defaults.max_chars is out of range (%d to %d)", MinMaxChars, MaxMaxChars)
	}
	names := make([]string, 0, len(d.Keys))
	for n := range d.Keys {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := d.validateKey(n, d.Keys[n]); err != nil {
			return err
		}
	}
	return nil
}

// Reihenfolge der Werte von agent_may, eng nach weit (E4: [keys.<name>]
// darf nur verengen).
var (
	moveRank   = map[string]int{"none": 0, "archive_only": 1, "within_account": 2}
	deleteRank = map[string]int{"none": 0, "trash_only": 1}
	keyNameRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// validateKey prüft [keys.<name>]: gültige Werte, nie weiter als der
// Standard. Meldungen nennen Schlüssel, nie Werte.
func (d FileDoc) validateKey(name string, k KeyRules) error {
	if !keyNameRE.MatchString(name) {
		return errors.New("keys: a key name is invalid (lowercase letters, digits and '-')")
	}
	pre := "keys." + name + "."
	if v := k.Move.AgentMay; v != "" {
		r, ok := moveRank[v]
		switch {
		case !ok:
			return errors.New(pre + "move.agent_may is invalid (expected none, archive_only or within_account)")
		case r > moveRank[d.Move.AgentMay]:
			return errors.New(pre + "move.agent_may is wider than [move] agent_may; a key can only narrow the policy")
		}
	}
	if v := k.Delete.AgentMay; v != "" {
		r, ok := deleteRank[v]
		switch {
		case !ok:
			return errors.New(pre + "delete.agent_may is invalid (expected none or trash_only)")
		case r > deleteRank[d.Delete.AgentMay]:
			return errors.New(pre + "delete.agent_may is wider than [delete] agent_may; a key can only narrow the policy")
		}
	}
	if v := k.Move.BulkConfirmThreshold; v != 0 && (v < 1 || v > d.Move.BulkConfirmThreshold) {
		return errors.New(pre + "move.bulk_confirm_threshold must be between 1 and [move] bulk_confirm_threshold")
	}
	if v := k.MaxChars; v != 0 && (v < MinMaxChars || v > d.AgentDefaults.MaxChars) {
		return fmt.Errorf("%smax_chars must be between %d and [agent_defaults] max_chars", pre, MinMaxChars)
	}
	return nil
}
