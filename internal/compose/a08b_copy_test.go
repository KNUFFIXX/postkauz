package compose

import (
	"path/filepath"
	"slices"
	"testing"
)

// A08b (Offene Frage 5): Windows-Kopiezusätze vor dem Namensvergleich der
// Anhang-Sperre. Alle Namen sind Platzhalter.

func TestA08bCopyNames(t *testing.T) {
	g, err := NewAttachGuard([]string{"Ablage-Privat"}, []string{"*.xlsm"}, OwnPaths{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		want bool
	}{
		// Beispiele aus dem Auftrag und aus Offene Frage 5.
		{"config - Kopie.toml", true},
		{"config (1).toml", true},
		{".env (1)", true},
		{"config - Copy (2).toml", true},
		{"config - Copy.toml", true},
		{"config.toml - Kopie", true},
		{".env - Kopie", true},
		{".npmrc - Kopie", true},
		{"settings.local - Kopie.json", true},
		// Kopie (n), beliebige Zahl, Kopie einer Kopie.
		{"config - Kopie (2).toml", true},
		{"config (12).toml", true},
		{"config (2026).toml", true},
		{"config (1) - Kopie.toml", true},
		{"config - Kopie - Kopie.toml", true},
		{"config (1) (2).toml", true},
		{"config(1).toml", true},
		{"config  (3).toml", true},
		// Groß-/Kleinschreibung wie bei den bestehenden Regeln.
		{"CONFIG - KOPIE.TOML", true},
		{"Config - copy (4).Toml", true},
		{".ENV - KOPIE", true},
		// Sicherungsendungen in Kombination mit Kopiezusätzen.
		{"config - Kopie.toml.bak", true},
		{"config (1).toml.old", true},
		{"config.toml.bak - Kopie", true},
		{"config.toml.bak (3)", true},
		{"config.toml - Kopie.bak", true},
		{"config - Kopie.toml~", true},
		{"config.toml.orig (1)", true},
		{".env.bak_20261115 - Copy", true},
		// Kopie einer Datei, deren Name nur aus einer Endung besteht.
		{" - Kopie.env", true},
		{"(1).npmrc", true},
		// Weitere feste Namen der Standardliste.
		{".netrc - Copy", true},
		{"_netrc (1)", true},
		{".pypirc - Kopie", true},
		{".git-credentials (2)", true},
		{"tokens - Kopie.json", true},
		{"x.local - Kopie.toml", true},
		{".env.local (1)", true},
		// Eigene Erweiterung der Konfiguration gilt ebenso.
		{"liste - Kopie.xlsm", true},
		{"liste (1).xlsm", true},
		// Waren schon vorher gesperrt und bleiben es.
		{"Vault - Kopie.kdbx", true},
		{"token - Kopie.json", true},
		{"id_rsa (1)", true},
		// Negativ: harmlose Kopien und Formen, die Windows nicht erzeugt.
		{"Brief - Kopie.pdf", false},
		{"Rechnung (1).pdf", false},
		{"Mieter - Copy (2).docx", false},
		{"config - Kopie.toml.pdf", false},
		{"config.toml.pdf (1)", false},
		{"myconfig - Kopie.toml", false},
		{"config-Kopie.toml", false},
		{"config - Kopien.toml", false},
		{"config - Kopie 2.toml", false},
		{"config (a).toml", false},
		{"config (1)x.toml", false},
		{"config ().toml", false},
		{"Kopie von config.toml", false},
		{"config copy.toml", false},
		{"envelope (1).pdf", false},
		{" (1)", false},
		{" - Kopie", false},
	} {
		if got := g.DeniedName(c.name); got != c.want {
			t.Errorf("DeniedName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestA08bCopyDirs(t *testing.T) {
	g, err := NewAttachGuard([]string{"Ablage-Privat"}, nil, OwnPaths{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		want bool
	}{
		{"_secrets - Kopie", true},
		{"_SECRETS - Copy (3)", true},
		{".ssh (1)", true},
		{"Passwörter - Kopie", true},
		{"keepass (2)", true},
		{".git - Kopie", true},
		{"Ablage-Privat (1)", true},
		{"Kunden - Kopie", false},
		{"Archiv (2)", false},
		{"secrets-Kopie", false},
		{"Kopie von secrets", false},
	} {
		if got := g.denyDir(c.name); got != c.want {
			t.Errorf("denyDir(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// Der Weg über LoadFile: Kopien gesperrter Dateien und Kopien in Kopien
// gesperrter Ordner enden mit attach_denied, harmlose Kopien bleiben.
func TestA08bCopyLoadFile(t *testing.T) {
	attach := filepath.Join(t.TempDir(), "Arbeit")
	a08Tree(t, attach, "config - Kopie.toml", ".env (1)", "_secrets - Kopie/brief.pdf", "Brief - Kopie.pdf")
	for _, c := range []struct{ path, rule string }{
		{"config - Kopie.toml", RuleFile},
		{".env (1)", RuleFile},
		{"_secrets - Kopie/brief.pdf", RuleDir},
		{"Brief - Kopie.pdf", ""},
	} {
		data, _, err := LoadFile("attachments[0]", attach, filepath.FromSlash(c.path), 0, nil)
		if c.rule == "" {
			if err != nil || string(data) != "Probe A08\n" {
				t.Fatalf("%s: %v", c.path, err)
			}
			continue
		}
		wantGuard(t, err, c.rule, attach)
	}
	// Neubau eines gespeicherten Entwurfs prüft den Namen ebenso.
	in := baseInput(KindNew)
	in.AttachDir = attach
	in.Attachments = []Attachment{{Filename: "config (1).toml", Data: []byte("x"), Origin: SourceFile}}
	if _, err := Build(in); err == nil {
		t.Fatal("stored copy name not denied on rebuild")
	} else if r, _, ok := GuardOf(err); !ok || r != RuleFile {
		t.Fatalf("rebuild: %v", err)
	}
}

func TestA08bCopyVariants(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"config - kopie.toml", []string{"config.toml"}},
		{"config (1) - kopie.toml", []string{"config (1).toml", "config.toml"}},
		{"config.toml.bak - copy (2)", []string{"config.toml.bak"}},
		{"brief.pdf", nil},
		{"config (1)x.toml", nil},
		{" (1)", nil},
	} {
		if got := copyVariants(c.in); !slices.Equal(got, c.want) {
			t.Errorf("copyVariants(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Die Regel sperrt nur mehr: Was ohne Kopie-Normalisierung gesperrt war,
// bleibt gesperrt (alle Standardmuster, je mit und ohne Zusatz).
func TestA08bOnlyMore(t *testing.T) {
	g := (*AttachGuard)(nil).orDefault()
	names := []string{"config.toml", ".env", "vault.kdbx", "token.json", "SECRETS.md.bak", "id_rsa", "x.local.json",
		"brief.pdf", "Rechnung (1).pdf", "config - Kopie.toml", "a - copy (1)", "(1)", " - kopie"}
	for _, p := range DefaultDenyFiles {
		names = append(names, p, p+" - Kopie", p+" (1)")
	}
	for _, n := range names {
		if g.deniedFolded(fold(n)) && !g.DeniedName(n) {
			t.Errorf("%q was denied before A08b, now allowed", n)
		}
	}
}
