package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// memStore ist ein Schlüsselbund im Speicher mit der Grenze des Windows-
// Schlüsselbunds (2.560 Byte je Eintrag; go-keyring keyring_windows.go).
type memStore struct {
	mu      sync.Mutex
	m       map[string]string
	max     int
	failSet bool
}

func newMemStore() *memStore { return &memStore{m: map[string]string{}, max: 2560} }

func (s *memStore) Get(ref string) (secrets.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[ref]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return secrets.Secret(v), nil
}

func (s *memStore) Set(ref string, v secrets.Secret) error {
	if err := secrets.ValidRef(ref); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSet {
		return fmt.Errorf("%w: test keyring locked", secrets.ErrUnavailable)
	}
	if len(v.Reveal()) > s.max {
		return errors.New("secrets: value too large for the keyring")
	}
	s.m[ref] = v.Reveal()
	return nil
}

func (s *memStore) Delete(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[ref]; !ok {
		return secrets.ErrNotFound
	}
	delete(s.m, ref)
	return nil
}

func (s *memStore) Status() secrets.Status {
	return secrets.Status{Backend: "keyring", Available: true}
}

func (s *memStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var k []string
	for r := range s.m {
		k = append(k, r)
	}
	return k
}

const testRef = "account/1a2b3c4d/outlook"

func TestSaveLoadSmallToken(t *testing.T) {
	st := newMemStore()
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	tok := Token{AccessToken: "CANARY-access-never-stored", RefreshToken: "CANARY-refresh-1", Expiry: now.Add(time.Hour)}
	if err := Save(st, testRef, tok, now); err != nil {
		t.Fatal(err)
	}
	got, err := Load(st, testRef)
	if err != nil || got.RefreshToken.Reveal() != "CANARY-refresh-1" || !got.Expiry.Equal(tok.Expiry) || !got.UpdatedAt.Equal(now) || got.Parts != 0 {
		t.Fatalf("load = %+v, %v", got, err)
	}
	// Das Access-Token liegt nirgends im Schlüsselbund (nur im Speicher).
	for _, k := range st.keys() {
		if strings.Contains(st.m[k], "CANARY-access") {
			t.Fatalf("access token stored under %s", k)
		}
	}
	if len(st.keys()) != 1 {
		t.Fatalf("entries = %v", st.keys())
	}
}

// Ein 4-KB-Refresh-Token passt nicht in einen Windows-Eintrag (2.560 Byte):
// Save verteilt es auf nummerierte Einträge (A03_teile T4 Punkt 1).
func TestSaveLoad4KBTokenInParts(t *testing.T) {
	st := newMemStore()
	now := time.Now().UTC().Truncate(time.Second)
	big := "CANARY-big-" + strings.Repeat("Ab9_-", 4096/5)
	if err := Save(st, testRef, Token{RefreshToken: secrets.Secret(big), Expiry: now}, now); err != nil {
		t.Fatal(err)
	}
	for _, k := range st.keys() {
		if len(st.m[k]) > MaxEntryBytes {
			t.Fatalf("entry %s has %d bytes", k, len(st.m[k]))
		}
	}
	got, err := Load(st, testRef)
	if err != nil || got.RefreshToken.Reveal() != big || got.Parts != 3 {
		t.Fatalf("load: parts=%d err=%v", got.Parts, err)
	}
	if _, ok := st.m[PartRef(testRef, 3)]; !ok || PartRef(testRef, 3) != "oauthpart3/1a2b3c4d/outlook" {
		t.Fatalf("part refs: %v", st.keys())
	}

	// Kürzeres Token danach: alte Teile verschwinden.
	if err := Save(st, testRef, Token{RefreshToken: "CANARY-short"}, now); err != nil {
		t.Fatal(err)
	}
	if k := st.keys(); len(k) != 1 || k[0] != testRef {
		t.Fatalf("stale parts left: %v", k)
	}

	// Ein beschädigter Teil fällt über die Prüfsumme auf.
	if err := Save(st, testRef, Token{RefreshToken: secrets.Secret(big)}, now); err != nil {
		t.Fatal(err)
	}
	st.m[PartRef(testRef, 2)] = strings.Repeat("y", MaxEntryBytes)
	if _, err := Load(st, testRef); !errors.Is(err, ErrTokenCorrupt) {
		t.Fatalf("corrupt part: %v", err)
	}
	delete(st.m, PartRef(testRef, 2))
	if _, err := Load(st, testRef); !errors.Is(err, ErrTokenCorrupt) {
		t.Fatalf("missing part: %v", err)
	}
	if err := Delete(st, testRef); err != nil || len(st.keys()) != 0 {
		t.Fatalf("delete: %v %v", err, st.keys())
	}
}

// json.Marshal eines Tokens schwärzt (Typ Secret); die Ablage kodiert
// ausdrücklich mit Reveal und speichert den echten Wert.
func TestTokenJSONIsRedactedButStorageIsNot(t *testing.T) {
	tok := Token{AccessToken: "CANARY-a", RefreshToken: "CANARY-r"}
	b, _ := json.Marshal(tok)
	if strings.Contains(string(b), "CANARY") {
		t.Fatalf("token JSON leaks: %s", b)
	}
	if s := fmt.Sprintf("%v %+v", tok, tok); strings.Contains(s, "CANARY") {
		t.Fatalf("token format leaks: %s", s)
	}
	st := newMemStore()
	if err := Save(st, testRef, tok, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.m[testRef], "CANARY-r") {
		t.Fatalf("stored value is not the token: %s", st.m[testRef])
	}
	if raw, _ := st.Get(testRef); fmt.Sprint(raw) != "[redacted]" {
		t.Fatal("stored JSON must print redacted")
	}
}

func TestLoadRejectsGarbage(t *testing.T) {
	st := newMemStore()
	st.m[testRef] = "an old password, not a token"
	if _, err := Load(st, testRef); !errors.Is(err, ErrTokenCorrupt) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Load(st, "account/1a2b3c4d/none"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
