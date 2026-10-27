package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/KNUFFIXX/postkauz/internal/crash"
)

var errNotFoundLib = keyring.ErrNotFound

// canary ist ein Kanarienwert: taucht er irgendwo im Klartext auf, ist ein
// Geheimnis durchgesickert.
const canary = "CANARY-pw-7f3a9c1e-do-not-leak"

func TestSecretIsRedactedEverywhere(t *testing.T) {
	s := Secret(canary)
	var buf bytes.Buffer
	for _, f := range []string{"%v", "%s", "%q", "%+v", "%#v", "%x", "%d"} {
		fmt.Fprintf(&buf, f+"\n", s)
	}
	fmt.Fprintln(&buf, s, struct{ P Secret }{s}, &s)
	b, _ := json.Marshal(map[string]any{"p": s, "list": []Secret{s}})
	buf.Write(b)
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "pw", s, slog.Any("any", s))
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "pw", s)
	if strings.Contains(buf.String(), canary) {
		t.Fatalf("canary leaked:\n%s", buf.String())
	}
	if s.Reveal() != canary {
		t.Fatal("Reveal must return the plain value")
	}
}

func TestRefAndEnvName(t *testing.T) {
	if err := ValidRef(AccountRef("1a2b3c4d", "demo-1")); err != nil {
		t.Fatal(err)
	}
	if err := ValidRef(LegacyAccountRef("demo-1")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "demo", "account/", "account/Demo", "account/../x", "account/a b",
		"account/1a2b3c4d/", "account/1A2B3C4D/demo", "account/1a2b3c/demo", "account/1a2b3c4d/x/y", "account/1a2b3c4d/../x"} {
		if ValidRef(bad) == nil {
			t.Errorf("ref %q must be invalid", bad)
		}
	}
	if ValidDataDirID("1a2b3c4d") != nil || ValidDataDirID("1a2b3c4") == nil || ValidDataDirID("1a2b3c4g") == nil {
		t.Error("ValidDataDirID")
	}
	// Die Umgebungsvariable hängt nur am Kontonamen (alt und neu gleich).
	for _, ref := range []string{"account/demo-1", AccountRef("1a2b3c4d", "demo-1")} {
		if got := EnvName(ref); got != "POSTKAUZ_PASSWORD_DEMO_1" {
			t.Errorf("EnvName(%s) = %s", ref, got)
		}
	}
	if got := EnvName("oauth/demo"); got != "POSTKAUZ_SECRET_OAUTH_DEMO" {
		t.Errorf("EnvName = %s", got)
	}
}

func fakeKeyring(fail error, delay time.Duration) *Keyring {
	mem := map[string]string{}
	wrap := func(f func() error) error {
		time.Sleep(delay)
		if fail != nil {
			return fail
		}
		return f()
	}
	return &Keyring{service: "postkauz-test", timeout: 200 * time.Millisecond, api: keyringAPI{
		set: func(svc, user, pw string) error { return wrap(func() error { mem[svc+"|"+user] = pw; return nil }) },
		get: func(svc, user string) (string, error) {
			var v string
			err := wrap(func() error {
				var ok bool
				if v, ok = mem[svc+"|"+user]; !ok {
					return errNotFoundLib
				}
				return nil
			})
			return v, err
		},
		del: func(svc, user string) error {
			return wrap(func() error {
				if _, ok := mem[svc+"|"+user]; !ok {
					return errNotFoundLib
				}
				delete(mem, svc+"|"+user)
				return nil
			})
		},
	}}
}

func TestKeyringBackend(t *testing.T) {
	k := fakeKeyring(nil, 0)
	if st := k.Probe(); !st.Available || st.Backend != "keyring" {
		t.Fatalf("probe = %+v", st)
	}
	ref := AccountRef("1a2b3c4d", "demo")
	if _, err := k.Get(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing = %v", err)
	}
	if err := k.Set(ref, canary); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get(ref); err != nil || v.Reveal() != canary {
		t.Fatalf("get = %v", err)
	}
	if err := k.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

// P4: Linux ohne Secret Service. go-keyring meldet dann einen D-Bus-Fehler;
// der Speicher gilt als nicht verfügbar, es gibt keinen stillen Rückfall,
// die Umgebungsvariable bleibt als Lesequelle.
func TestKeyringUnavailableFallsBackToEnvOnly(t *testing.T) {
	dbusErr := errors.New("The name org.freedesktop.secrets was not provided by any .service files")
	for name, kr := range map[string]*Keyring{
		"dbus error": fakeKeyring(dbusErr, 0),
		"timeout":    fakeKeyring(nil, time.Second),
	} {
		t.Run(name, func(t *testing.T) {
			st, err := open(BackendAuto, t.TempDir(), kr)
			if err != nil {
				t.Fatal(err)
			}
			if s := st.Status(); s.Available || s.Backend != "none" {
				t.Fatalf("status = %+v", s)
			}
			ref := AccountRef("1a2b3c4d", "demo")
			err = st.Set(ref, canary)
			if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "insecure-file") {
				t.Fatalf("set = %v", err)
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatal("error leaks the secret")
			}
			if _, err := st.Get(ref); !errors.Is(err, ErrNotFound) {
				t.Fatalf("get without env = %v", err)
			}
			t.Setenv(EnvName(ref), canary)
			if v, err := st.Get(ref); err != nil || v.Reveal() != canary {
				t.Fatalf("get from env = %v", err)
			}
		})
	}
}

// Stored liest unter der Umgebung: Ein Passwort aus POSTKAUZ_PASSWORD_<NAME>
// gilt dort nicht als gespeichert (der Umzug der Einträge darf es nie in
// den Schlüsselbund kopieren).
func TestStoredIgnoresEnvironment(t *testing.T) {
	k := fakeKeyring(nil, 0)
	st := WithEnv(k)
	ref := AccountRef("1a2b3c4d", "demo")
	t.Setenv(EnvName(ref), canary)
	if v, err := st.Get(ref); err != nil || v.Reveal() != canary {
		t.Fatalf("env layer: %v", err)
	}
	if _, err := Stored(st).Get(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stored = %v, want not found", err)
	}
	if Stored(k) != Store(k) {
		t.Fatal("Stored of a plain store must return it unchanged")
	}
}

func TestInsecureFileBackend(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "insecure-secrets")
	st, err := open(BackendInsecureFile, dir, fakeKeyring(errors.New("no keyring"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if s := st.Status(); !s.Available || !s.Insecure || !strings.Contains(s.Detail, "INSECURE") {
		t.Fatalf("status = %+v", s)
	}
	ref, ref2 := AccountRef("1a2b3c4d", "demo"), AccountRef("1a2b3c4d", "other")
	if _, err := st.Get(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get before set = %v", err)
	}
	if err := st.Set(ref, canary); err != nil {
		t.Fatal(err)
	}
	if err := st.Set(ref2, "second"); err != nil {
		t.Fatal(err)
	}
	if v, err := st.Get(ref); err != nil || v.Reveal() != canary {
		t.Fatalf("get = %v", err)
	}
	// Neuer Speicher auf demselben Verzeichnis liest dasselbe.
	if v, err := NewInsecureFile(dir).Get(ref2); err != nil || v.Reveal() != "second" {
		t.Fatalf("reopen = %v", err)
	}
	// Kein Klartext auf der Platte, Rechte 0600/0700.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("files = %v", entries)
	}
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if bytes.Contains(b, []byte(canary)) {
			t.Fatalf("%s contains the plain secret", e.Name())
		}
		if fi, _ := e.Info(); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s perm = %o", e.Name(), fi.Mode().Perm())
		}
	}
	if fi, _ := os.Stat(dir); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o", fi.Mode().Perm())
	}
	// Ein unter einen anderen Verweis kopierter Eintrag lässt sich nicht öffnen (AAD).
	f := NewInsecureFile(dir)
	m, _ := f.load()
	m[ref2] = m[ref]
	_ = f.save(m)
	if _, err := f.Get(ref2); err == nil {
		t.Fatal("swapped entry must not decrypt")
	}
	if err := st.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
}

// P4 gegen den echten Schlüsselbund dieses Rechners, ohne ihn zu verändern
// (nur Lesen eines nie geschriebenen Verweises). Unter Linux ohne Secret
// Service (CI, Container) muss das sauber als „nicht verfügbar" enden.
// Mit POSTKAUZ_TEST_REAL_KEYRING=1 zusätzlich Schreiben/Lesen/Löschen.
func TestRealKeyringProbe(t *testing.T) {
	k := NewKeyring()
	k.timeout = 5 * time.Second
	st := k.Probe()
	t.Logf("real keyring: %+v", st)
	if !st.Available {
		s, err := open(BackendAuto, t.TempDir(), k)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Set(AccountRef("1a2b3c4d", "probe-test"), canary); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("set on unavailable keyring = %v", err)
		}
		return
	}
	if os.Getenv("POSTKAUZ_TEST_REAL_KEYRING") != "1" {
		t.Skip("keyring available; set POSTKAUZ_TEST_REAL_KEYRING=1 to run a write/read/delete round trip")
	}
	ref := AccountRef("1a2b3c4d", "selftest-"+fmt.Sprint(time.Now().UnixNano()%100000))
	if err := k.Set(ref, canary); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Delete(ref) }()
	if v, err := k.Get(ref); err != nil || v.Reveal() != canary {
		t.Fatalf("round trip: %v", err)
	}
}

// TestKeyringCallRecoversPanic: Ein Panic in der Schlüsselbund-Bibliothek
// beendet nicht den Prozess, sondern kommt als Fehler zurück (S2.0/A5).
func TestKeyringCallRecoversPanic(t *testing.T) {
	k := &Keyring{service: "postkauz-test", timeout: time.Second, api: keyringAPI{
		get: func(svc, user string) (string, error) { panic("secret-canary-value in panic") },
	}}
	_, err := k.Get("account/demo")
	if err == nil {
		t.Fatal("expected an error from a panicking keyring call")
	}
	var pe *crash.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *crash.PanicError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "secret-canary-value") {
		t.Fatalf("panic value leaked into the error: %v", err)
	}
}

// W (Gegenprüfung T4): Nach der Zeitgrenze läuft der Aufruf weiter; ein Set
// kann verspätet ankommen. call meldet das als ErrTimeout (unsicherer
// Ausgang), weiterhin auch als ErrUnavailable.
func TestKeyringTimeoutIsUncertain(t *testing.T) {
	release := make(chan struct{})
	landed := make(chan string, 1)
	k := &Keyring{service: "postkauz-test", timeout: 50 * time.Millisecond, api: keyringAPI{
		set: func(svc, user, pw string) error {
			<-release
			landed <- pw
			return nil
		},
	}}
	err := k.Set(AccountRef("1a2b3c4d", "demo"), canary)
	if !errors.Is(err, ErrTimeout) || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("set = %v, want ErrTimeout and ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatal("error leaks the secret")
	}
	close(release)
	if pw := <-landed; pw != canary {
		t.Fatalf("late write = %q", pw)
	}
}
