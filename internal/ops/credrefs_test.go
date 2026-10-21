package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// noDelete ist ein Schlüsselbund, in dem Löschen scheitert.
type noDelete struct{ *memSecrets }

func (noDelete) Delete(string) error { return errors.New("access denied (test)") }

// legacyAccount legt ein Konto mit Verweis aus S1 an ("account/<name>").
func legacyAccount(t *testing.T, st *store.SQLite, name string) {
	t.Helper()
	if _, err := st.CreateAccount(context.Background(), store.Account{Name: name, Email: name + "@example.invalid",
		Host: "127.0.0.1", Port: 143, TLS: "none", Username: "u", AuthType: "password",
		CredentialRef: secrets.LegacyAccountRef(name), InsecureTestServer: true}); err != nil {
		t.Fatal(err)
	}
}

func refOf(t *testing.T, st *store.SQLite, name string) string {
	t.Helper()
	a, err := st.AccountByName(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return a.CredentialRef
}

// S2.0/A2, Umzug: lesen alt → schreiben neu → Verweis umstellen → löschen
// alt; ohne gespeicherten Eintrag (Umgebung) wechselt nur der Verweis, und
// der Wert aus der Umgebung landet nicht im Schlüsselbund; ein zweiter Lauf
// tut nichts.
func TestMoveCredentialRefs(t *testing.T) {
	e := newAccountsEnv(t)
	ctx := context.Background()
	legacyAccount(t, e.st, "demo")
	legacyAccount(t, e.st, "env-only")
	_ = e.sec.Set("account/demo", canary)
	t.Setenv(secrets.EnvName("account/env-only"), "from-env")

	moves, err := MoveCredentialRefs(ctx, e.st, secrets.WithEnv(e.sec))
	if err != nil || len(moves) != 2 {
		t.Fatalf("moves = %+v, %v", moves, err)
	}
	got := map[string]string{}
	for _, m := range moves {
		got[m.Account] = m.Result
		if m.Err != nil {
			t.Errorf("%s: %v", m.Account, m.Err)
		}
	}
	if got["demo"] != "moved" || got["env-only"] != "ref_only" {
		t.Fatalf("results = %v", got)
	}
	if v, err := e.sec.Get(e.ref(t, "demo")); err != nil || v.Reveal() != canary {
		t.Fatalf("new entry: %v", err)
	}
	if _, err := e.sec.Get("account/demo"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("old entry not deleted")
	}
	if refOf(t, e.st, "demo") != e.ref(t, "demo") || refOf(t, e.st, "env-only") != e.ref(t, "env-only") {
		t.Fatal("credential_ref not switched")
	}
	if len(e.sec.m) != 1 {
		t.Fatalf("keyring must hold exactly the moved entry, has %d (environment value copied?)", len(e.sec.m))
	}
	if left, _ := e.st.CredentialRefsLeft(ctx); len(left) != 0 {
		t.Fatalf("left = %v", left)
	}
	if moves, err := MoveCredentialRefs(ctx, e.st, e.sec); err != nil || len(moves) != 0 {
		t.Fatalf("second run: %+v %v", moves, err)
	}
}

// Scheitert das Löschen, bleibt der alte Eintrag, der Verweis zeigt auf den
// neuen, und meta merkt sich den alten (für doctor).
func TestMoveCredentialRefsOldEntryLeft(t *testing.T) {
	e := newAccountsEnv(t)
	ctx := context.Background()
	legacyAccount(t, e.st, "demo")
	_ = e.sec.Set("account/demo", canary)
	moves, err := MoveCredentialRefs(ctx, e.st, noDelete{e.sec})
	if err != nil || len(moves) != 1 || moves[0].Result != "moved_old_left" || moves[0].Err == nil {
		t.Fatalf("moves = %+v, %v", moves, err)
	}
	if v, _ := e.sec.Get("account/demo"); v.Reveal() != canary {
		t.Fatal("old entry must stay")
	}
	if v, _ := e.sec.Get(e.ref(t, "demo")); v.Reveal() != canary || refOf(t, e.st, "demo") != e.ref(t, "demo") {
		t.Fatal("new entry or reference missing")
	}
	if left, _ := e.st.CredentialRefsLeft(ctx); len(left) != 1 || left[0] != "account/demo" {
		t.Fatalf("left = %v", left)
	}
}

// Ohne erreichbaren Schlüsselbund wird nichts versucht; scheitert das
// Schreiben, bleibt der alte Verweis (nichts verloren).
func TestMoveCredentialRefsUnavailableOrWriteFails(t *testing.T) {
	e := newAccountsEnv(t)
	ctx := context.Background()
	legacyAccount(t, e.st, "demo")
	_ = e.sec.Set("account/demo", canary)
	e.sec.down = true // Status: nicht verfügbar, Set scheitert
	moves, err := MoveCredentialRefs(ctx, e.st, e.sec)
	if err != nil || len(moves) != 1 || moves[0].Result != "postponed" || refOf(t, e.st, "demo") != "account/demo" {
		t.Fatalf("unavailable: %+v %v", moves, err)
	}
	moves, err = MoveCredentialRefs(ctx, e.st, availableButReadOnly{e.sec})
	if err != nil || len(moves) != 1 || moves[0].Result != "failed" || refOf(t, e.st, "demo") != "account/demo" {
		t.Fatalf("write fails: %+v %v", moves, err)
	}
	if v, _ := e.sec.Get("account/demo"); v.Reveal() != canary {
		t.Fatal("old entry lost")
	}
}

// availableButReadOnly meldet sich verfügbar, aber Set scheitert.
type availableButReadOnly struct{ *memSecrets }

func (availableButReadOnly) Status() secrets.Status {
	return secrets.Status{Backend: "keyring", Available: true}
}
