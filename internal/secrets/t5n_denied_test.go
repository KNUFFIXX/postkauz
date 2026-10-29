package secrets

import (
	"errors"
	"strings"
	"testing"
)

// Nachbesserung T5, Punkt 6: Denied verweigert jeden Zugriff, auch wenn
// ein Passwort in der Umgebung steht, und meldet sich als nicht verfügbar.
func TestT5NDeniedRefusesEverything(t *testing.T) {
	ref := AccountRef("1a2b3c4d", "demo")
	const pw = "kanarien-passwort-denied"
	t.Setenv(EnvName(ref), pw)
	var st Store = Denied{}
	if s, err := st.Get(ref); !errors.Is(err, ErrDenied) || !s.IsZero() {
		t.Errorf("Get = %q, %v; want ErrDenied", s.Reveal(), err)
	}
	if err := st.Set(ref, "x"); !errors.Is(err, ErrDenied) {
		t.Errorf("Set = %v; want ErrDenied", err)
	}
	if err := st.Delete(ref); !errors.Is(err, ErrDenied) {
		t.Errorf("Delete = %v; want ErrDenied", err)
	}
	if _, err := Stored(st).Get(ref); !errors.Is(err, ErrDenied) {
		t.Errorf("Stored(Denied).Get = %v; want ErrDenied", err)
	}
	if s := st.Status(); s.Available || s.Backend != "denied" {
		t.Errorf("Status = %+v", s)
	}
	if strings.Contains(ErrDenied.Error(), pw) {
		t.Error("error contains the secret")
	}
}
