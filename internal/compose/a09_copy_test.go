package compose

import (
	"strings"
	"testing"
	"time"
)

// A09 Teil 2 (c): Restfunde der Gegenprüfung von A08b (534e258).

// Verkettete Kopiezusätze ohne feste Rundenzahl: 17 und 40 Zusätze sind
// gesperrt (vorher gab ein Name mit 17 frei).
func TestA09CopyChains(t *testing.T) {
	g := (*AttachGuard)(nil).orDefault()
	for _, n := range []int{1, 16, 17, 40} {
		for _, c := range []struct{ base, suffix, ext string }{
			{"config", " (1)", ".toml"},
			{"config", " - Kopie", ".toml"},
			{"config", " - Copy (2)", ".toml"},
			{".env", " (1)", ""},
			{"config.toml", " - Kopie", ""},
		} {
			name := c.base + strings.Repeat(c.suffix, n) + c.ext
			if !g.DeniedName(name) {
				t.Errorf("%d x %q: %q allowed", n, c.suffix, name)
			}
		}
		if dir := "_secrets" + strings.Repeat(" (1)", n); !g.denyDir(dir) {
			t.Errorf("dir %q allowed", dir)
		}
	}
	mixed := "config" + strings.Repeat(" (1) - Kopie - Copy (3)", 20) + ".toml.bak"
	if !g.DeniedName(mixed) {
		t.Errorf("mixed chain allowed")
	}
	if v := copyVariants(fold("config" + strings.Repeat(" (1)", 40) + ".toml")); len(v) != 40 || v[len(v)-1] != "config.toml" {
		t.Errorf("variants: %d, last %q", len(v), v[len(v)-1])
	}
}

// Kopie-Formen nur bis 1024 Bytes; der Name selbst zählt immer. Ein Name
// aus 1 MiB „ (1)“ ist schnell geprüft.
func TestA09CopyLongNames(t *testing.T) {
	g := (*AttachGuard)(nil).orDefault()
	atLimit := "config" + strings.Repeat(" (1)", (maxCopyName-len("config.toml"))/4) + ".toml"
	if len(atLimit) > maxCopyName || !g.DeniedName(atLimit) {
		t.Fatalf("name of %d bytes allowed", len(atLimit))
	}
	if v := copyVariants(strings.Repeat("a", maxCopyName+1)); v != nil {
		t.Fatalf("variants for a long name: %d", len(v))
	}
	huge := "config" + strings.Repeat(" (1)", 1<<18) + ".toml" // 1 MiB
	if v := copyVariants(fold(huge)); v != nil {
		t.Fatalf("variants for 1 MiB: %d", len(v))
	}
	// Gemessen 17.11.2026 (Cloud, 4 Kerne): etwa 2,3 s, fast alles in
	// path.Match über den Namen selbst (vor A08b genauso); vorher mit den
	// Kopie-Formen etwa 30 s. Die Grenze hier fängt nur einen Rückfall.
	start := time.Now()
	g.DeniedName(huge)
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("1 MiB name took %v", d)
	}
	if !g.DeniedName(strings.Repeat("x", 1<<20) + ".kdbx") {
		t.Fatal("long name itself not checked")
	}
}

// Monotonie gegen eine feste Liste mit den Ergebnissen von main e8e9c54
// (vor A08b): Was dort gesperrt war, bleibt gesperrt; dazu die erwarteten
// neuen Sperren durch die Kopiezusätze.
func TestA09OnlyMoreThanMain(t *testing.T) {
	g := (*AttachGuard)(nil).orDefault()
	newly := map[string]bool{}
	for _, c := range a09MainNames {
		now := g.DeniedName(c.name)
		if c.denied && !now {
			t.Errorf("file %q was denied on e8e9c54, now allowed", c.name)
		}
		if !c.denied && now {
			newly[c.name] = true
		}
	}
	for _, c := range a09MainDirs {
		now := g.denyDir(c.name)
		if c.denied && !now {
			t.Errorf("dir %q was denied on e8e9c54, now allowed", c.name)
		}
		if !c.denied && now {
			newly["dir "+c.name] = true
		}
	}
	for _, n := range []string{"config - Kopie.toml", "config (1).toml", "config(1).toml", "config.toml - Kopie", "config - Copy (2).toml",
		"config (1) - Kopie.toml", ".env - Kopie", ".env (1)", ".npmrc - Kopie", "settings.local - Kopie.json", "dir _secrets - Kopie",
		"dir secrets (1)", "dir .ssh (1)"} {
		if !newly[n] {
			t.Errorf("%q not newly denied", n)
		}
	}
	for _, n := range []string{"Kopie von config.toml", "config copy.toml", "config-Kopie.toml", "brief.pdf", "Rechnung (1).pdf",
		"dir Projekte (2026)", "dir Archiv 2026 (1)", "dir Briefe - Kopie"} {
		if newly[n] {
			t.Errorf("%q newly denied", n)
		}
	}
}

// a09MainNames und a09MainDirs: feste Namen mit dem Ergebnis von
// DeniedName bzw. denyDir auf main e8e9c54 (vor A08b), erzeugt am
// 17.11.2026 in einem Worktree dieses Commits. Die Kopiezusätze (A08b)
// und die Runden ohne feste Zahl (A09 Teil 2 c) dürfen nur mehr sperren.
var a09MainNames = []struct {
	name   string
	denied bool
}{
	{"config.toml", true},
	{"CONFIG.TOML", true},
	{"config.toml.bak", true},
	{"config.toml.bak_20261105", true},
	{"config.toml.old", true},
	{"config.toml~", true},
	{"config.toml.orig", true},
	{"config - Kopie.toml", false},
	{"config (1).toml", false},
	{"config(1).toml", false},
	{"config.toml - Kopie", false},
	{"config - Copy (2).toml", false},
	{"config (1) - Kopie.toml", false},
	{"Kopie von config.toml", false},
	{"config copy.toml", false},
	{"config-Kopie.toml", false},
	{".env", true},
	{".env.local", true},
	{".env - Kopie", false},
	{".env (1)", false},
	{"prod.env", true},
	{".netrc", true},
	{"_netrc", true},
	{".npmrc", true},
	{".npmrc - Kopie", false},
	{".pypirc", true},
	{".git-credentials", true},
	{"vault.kdbx", true},
	{"Vault - Kopie.kdbx", true},
	{"vault.kdbx.bak", true},
	{"vault.kdbx.1", true},
	{"vault.kdb", true},
	{"id_rsa", true},
	{"id_rsa.pub", true},
	{"id_ed25519 (1)", true},
	{"server.key", true},
	{"server.pem", true},
	{"cert.pfx", true},
	{"cert.p12", true},
	{"putty.ppk", true},
	{"store.jks", true},
	{"app.keystore", true},
	{"token.json", true},
	{"tokens.json", true},
	{"token - Kopie.json", true},
	{"access_token_gmail.json", true},
	{"credentials.json", true},
	{"credentials (2).json", true},
	{"client_secret_123.json", true},
	{"SECRETS.md", true},
	{"SECRETS.md.bak", true},
	{"secretary_letter.pdf", true},
	{"passwort.txt", true},
	{"Passwörter.xlsx", true},
	{"vpn.ovpn", true},
	{"claude_desktop_config.json", true},
	{"settings.local.json", true},
	{"settings.local - Kopie.json", false},
	{"x.local.toml", true},
	{"brief.pdf", false},
	{"Rechnung (1).pdf", false},
	{"Rechnung 2026 (3).pdf", false},
	{"foto (12).jpg", false},
	{"a - copy (1)", false},
	{"(1)", false},
	{" - kopie", false},
	{"schluesseluebergabe Protokoll.pdf", false},
	{"rechnung_key_account.pdf", false},
	{"notes.txt", false},
}

var a09MainDirs = []struct {
	name   string
	denied bool
}{
	{"_secrets", true},
	{"_secrets - Kopie", false},
	{"secrets (1)", false},
	{".ssh", true},
	{".ssh (1)", false},
	{".SSH", true},
	{"Passwörter", true},
	{"passwoerter", true},
	{"keepass", true},
	{".git", true},
	{".config", true},
	{"credentials", true},
	{"Steuer", false},
	{"Projekte (2026)", false},
	{"Archiv 2026 (1)", false},
	{"Briefe - Kopie", false},
	{"docs", false},
}
