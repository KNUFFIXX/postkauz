package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/remote"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// liar gibt beim Lesen einen anderen Wert zurück, als geschrieben wurde.
type liar struct{ *memSecrets }

func (l liar) Get(ref string) (secrets.Secret, error) {
	if _, err := l.memSecrets.Get(ref); err != nil {
		return "", err
	}
	return "anders", nil
}

// brokenConfig: Lesen geht, Schreiben scheitert.
type brokenConfig struct{ contractRemotes }

func (b *brokenConfig) SetRemote(string, string) error { return errors.New("disk full") }

// A04 Teil 4: remote.add liest den Schlüssel zurück, bevor es die
// Konfiguration schreibt; scheitert das Schreiben, ist der Schlüsselbund
// wieder wie vorher. Echte Datei: nur der Abschnitt, der Schlüssel nie.
func TestA04RemoteAddVerifiesAndRollsBack(t *testing.T) {
	ctx := context.Background()
	key := secrets.Secret(KeyPrefix + "KANARIE-a04-0123456789")
	in := json.RawMessage(`{"name":"buero","url":"https://office-pc.example.ts.net/mcp"}`)

	// Zurückgelesen ≠ geschrieben: Fehler, Konfiguration unberührt.
	cfg := &contractRemotes{m: map[string]config.Remote{}}
	op := RemoteAdd(cfg, liar{newMemSecrets()}, "")
	if _, err := op.Handler(ctx, Call{Input: in, Secret: key}); err == nil || !strings.Contains(err.Error(), "did not give back") {
		t.Fatalf("read-back mismatch: %v", err)
	}
	if len(cfg.m) != 0 {
		t.Fatalf("configuration written: %v", cfg.m)
	}

	// Konfiguration nicht schreibbar: neuer Schlüssel wieder entfernt,
	// ersetzter wieder der alte.
	sec := newMemSecrets()
	bc := &brokenConfig{contractRemotes{m: map[string]config.Remote{}}}
	if _, err := RemoteAdd(bc, sec, "").Handler(ctx, Call{Input: in, Secret: key}); err == nil {
		t.Fatal("broken configuration accepted")
	}
	if _, err := sec.Get(remote.KeyRef("buero")); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("key left behind: %v", err)
	}
	// Gegenprüfung Teil 4, K5: Abschnitt steht schon, aber ohne Schlüssel:
	// Auch dann ist der neue Schlüssel nach dem Fehler wieder weg.
	bc.m["buero"] = config.Remote{URL: "https://alt.example.ts.net/mcp"}
	if _, err := RemoteAdd(bc, sec, "").Handler(ctx, Call{Input: in, Secret: key}); err == nil {
		t.Fatal("broken configuration accepted")
	}
	if _, err := sec.Get(remote.KeyRef("buero")); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("key left behind for an existing section: %v", err)
	}
	_ = sec.Set(remote.KeyRef("buero"), "pk_key_alt")
	_, _ = RemoteAdd(bc, sec, "").Handler(ctx, Call{Input: in, Secret: key})
	if v, _ := sec.Get(remote.KeyRef("buero")); v.Reveal() != "pk_key_alt" {
		t.Fatalf("old key not restored")
	}

	// Echte Datei.
	f := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(f, []byte("# Büro\nlog_level = \"info\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sec = newMemSecrets()
	out, err := RemoteAdd(config.RemoteFile{Path: f}, sec, f).Handler(ctx, Call{Input: in, Secret: key})
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(RemoteAddOutput); o.Action != "added" || !o.KeyVerified || o.CredentialRef != "remote/buero" {
		t.Fatalf("output %+v", o)
	}
	b, _ := os.ReadFile(f)
	if !strings.HasPrefix(string(b), "# Büro\nlog_level = \"info\"\n\n") || !strings.Contains(string(b), "[remote.buero]\nurl = \"https://office-pc.example.ts.net/mcp\"\n") ||
		strings.Contains(string(b), key.Reveal()) {
		t.Fatalf("file:\n%s", b)
	}
	// Ausgabe und Fehler nennen den Schlüssel nie.
	ob, _ := json.Marshal(out)
	if strings.Contains(string(ob), key.Reveal()) {
		t.Fatal("output contains the key")
	}
	_, err = RemoteAdd(config.RemoteFile{Path: f}, sec, f).Handler(ctx, Call{Input: in, Secret: "KANARIE-ohne-praefix"})
	if err == nil || strings.Contains(err.Error(), "KANARIE") {
		t.Fatalf("bad key: %v", err)
	}
	// Audit-Parameter: Feldnamen und Ziel, nie Adresse oder Schlüssel.
	params, targets := redactRemote(in)
	if strings.Contains(string(params), "office-pc") || len(targets) != 1 || targets[0] != "remote:buero" {
		t.Fatalf("redact: %s %v", params, targets)
	}
}
