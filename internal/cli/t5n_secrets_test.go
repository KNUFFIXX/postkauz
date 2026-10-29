package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Nachbesserung T5, Punkt 6: Ein Prozess ohne Zugangsdaten (mcp über stdio
// und HTTP) bekommt den Speicher, der jeden Zugriff verweigert; das
// Secrets-Backend wird gar nicht erst geöffnet. Ohne noCredentials bleibt
// es beim gewählten Backend.
func TestT5NNoCredentialsGetsDeniedSecrets(t *testing.T) {
	setupEnv(t)
	orig := openSecrets
	opened := 0
	openSecrets = func(backend, dir string) (secrets.Store, error) {
		opened++
		return orig(backend, dir)
	}
	t.Cleanup(func() { openSecrets = orig })
	// Punkt 6: Nur ein Prozess mit Zugangsdaten schützt sich vor dem
	// Auslesen (procguard); der MCP-Prozess braucht das nicht.
	origDeny := denyInspection
	denied := 0
	denyInspection = func() error { denied++; return nil }
	t.Cleanup(func() { denyInspection = origDeny })

	a := newApp("test", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	a.noCredentials = true
	if _, err := a.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.sec.(secrets.Denied); !ok {
		t.Errorf("noCredentials: secrets store %T, want secrets.Denied", a.sec)
	}
	if _, err := a.sec.Get(secrets.AccountRef("1a2b3c4d", "demo")); !errors.Is(err, secrets.ErrDenied) {
		t.Errorf("Get = %v, want ErrDenied", err)
	}
	if opened != 0 || denied != 0 {
		t.Errorf("noCredentials opened the secrets backend %d times, process guard %d times", opened, denied)
	}
	a.close()

	b := newApp("test", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if _, err := b.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.close()
	if _, ok := b.sec.(secrets.Denied); ok || opened != 1 || denied != 1 {
		t.Errorf("without noCredentials: store %T, opened %d, process guard %d", b.sec, opened, denied)
	}
}
