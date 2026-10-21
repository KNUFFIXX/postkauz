package store

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
)

// S2.0/A2: Migration 0008 erzeugt die Kennung des Datenverzeichnisses beim
// ersten Öffnen, auch für einen Bestand mit Schema 7; sie bleibt über
// weitere Öffnungen gleich, zwei Datenbanken haben verschiedene Kennungen,
// credential_ref bleibt unverändert (den Umzug macht ops.MoveCredentialRefs).
func TestDataDirIDCreatedOnceAndDistinct(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p1 := filepath.Join(dir, "one.db")
	s, err := Open(p1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.migrateTo(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DataDirID(ctx); !errors.Is(err, ErrNoDataDirID) {
		t.Fatalf("schema 7: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at)
		VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	id1, err := s.DataDirID(ctx)
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(id1) {
		t.Fatalf("id = %q, %v", id1, err)
	}
	if a, _ := s.AccountByName(ctx, "demo"); a.CredentialRef != "account/demo" {
		t.Fatalf("migration must not change credential_ref: %q", a.CredentialRef)
	}
	_ = s.Close()

	s, err = Open(p1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.DataDirID(ctx); again != id1 {
		t.Fatalf("id changed on reopen: %s -> %s", id1, again)
	}
	_ = s.Close()

	s2, err := Open(filepath.Join(dir, "two.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if id2, err := s2.DataDirID(ctx); err != nil || id2 == id1 {
		t.Fatalf("second database: id %q (first %q), %v", id2, id1, err)
	}
}

func TestCredentialRefsLeftAndSetRef(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	id, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.invalid", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "u", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCredentialRef(ctx, id, "account/1a2b3c4d/demo"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.AccountByName(ctx, "demo"); a.CredentialRef != "account/1a2b3c4d/demo" {
		t.Fatalf("ref = %q", a.CredentialRef)
	}
	if err := s.SetCredentialRef(ctx, 999, "account/1a2b3c4d/x"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
	if left, err := s.CredentialRefsLeft(ctx); err != nil || len(left) != 0 {
		t.Fatalf("left = %v, %v", left, err)
	}
	for _, r := range []string{"account/demo", "account/demo", "account/other"} {
		if err := s.AddCredentialRefLeft(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if left, _ := s.CredentialRefsLeft(ctx); len(left) != 2 || left[0] != "account/demo" || left[1] != "account/other" {
		t.Fatalf("left = %v", left)
	}
}
