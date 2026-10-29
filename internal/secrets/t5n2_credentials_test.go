package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zweite Nachbesserung T5, Punkt 4b: Passwort als systemd-Credential.
func TestT5N2SystemdCredential(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{EnvCredentialsDir: dir}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	st := envStore{base: Unavailable{Reason: "test"}, lookup: lookup}
	ref := AccountRef("1a2b3c4d", "demo-1")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "POSTKAUZ_PASSWORD_DEMO_1"), []byte(content), 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(filepath.Join(dir, "POSTKAUZ_PASSWORD_DEMO_1")) })
	}

	if _, err := st.Get(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no credential: %v, want ErrNotFound", err)
	}
	for _, c := range []struct{ file, want string }{
		{"s3cret\n", "s3cret"}, {"s3cret\r\n", "s3cret"}, {"s3cret", "s3cret"}, {"a b \n\n", "a b \n"},
	} {
		write(c.file)
		v, src, ok, err := fromEnvironment(ref, lookup)
		if err != nil || !ok || src != "credential" || v.Reveal() != c.want {
			t.Errorf("%q: %q %q %v %v, want %q from credential", c.file, v.Reveal(), src, ok, err, c.want)
		}
		if g, err := st.Get(ref); err != nil || g.Reveal() != c.want {
			t.Errorf("%q: Get %q %v", c.file, g.Reveal(), err)
		}
	}
	write("\n")
	if _, err := st.Get(ref); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty credential: %v, want ErrNotFound", err)
	}

	// Umgebung vor Credential.
	write("from-credential")
	env["POSTKAUZ_PASSWORD_DEMO_1"] = "from-env"
	if v, src, _, _ := fromEnvironment(ref, lookup); v.Reveal() != "from-env" || src != "environment" {
		t.Errorf("environment first: %q %q", v.Reveal(), src)
	}
	delete(env, "POSTKAUZ_PASSWORD_DEMO_1")

	// Kein regulärer Eintrag, zu groß: Fehler mit Namen, ohne Inhalt, kein
	// Rückfall auf den Speicher darunter.
	_ = os.Remove(filepath.Join(dir, "POSTKAUZ_PASSWORD_DEMO_1"))
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(dir, "POSTKAUZ_PASSWORD_DEMO_1")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ref); !errors.Is(err, ErrCredential) || !strings.Contains(err.Error(), "POSTKAUZ_PASSWORD_DEMO_1") {
		t.Errorf("symlink: %v, want ErrCredential", err)
	}
	_ = os.Remove(filepath.Join(dir, "POSTKAUZ_PASSWORD_DEMO_1"))
	write("geheim-" + strings.Repeat("x", maxCredentialBytes))
	if _, err := st.Get(ref); !errors.Is(err, ErrCredential) || strings.Contains(err.Error(), "geheim") {
		t.Errorf("too large: %v, want ErrCredential without content", err)
	}

	// Relatives oder leeres Verzeichnis gilt nicht.
	for _, d := range []string{"", "relative/dir"} {
		env[EnvCredentialsDir] = d
		if _, _, ok, err := fromEnvironment(ref, lookup); ok || err != nil {
			t.Errorf("dir %q: ok %v err %v, want nothing", d, ok, err)
		}
	}
}
