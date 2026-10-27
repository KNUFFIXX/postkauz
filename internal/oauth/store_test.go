package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	// failAt: das failAt-te Set ab jetzt scheitert (0 = keins); sets zählt.
	failAt, sets int
	// hook läuft vor jedem Get, Set und Delete (ohne Sperre): zweiter
	// Prozess an einer bestimmten Stelle.
	hook func(op, ref string)
}

func newMemStore() *memStore { return &memStore{m: map[string]string{}, max: 2560} }

func (s *memStore) Get(ref string) (secrets.Secret, error) {
	if s.hook != nil {
		s.hook("get", ref)
	}
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
	if s.hook != nil {
		s.hook("set", ref)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	if s.failSet || s.sets == s.failAt {
		return fmt.Errorf("%w: test keyring locked", secrets.ErrUnavailable)
	}
	if len(v.Reveal()) > s.max {
		return errors.New("secrets: value too large for the keyring")
	}
	s.m[ref] = v.Reveal()
	return nil
}

func (s *memStore) Delete(ref string) error {
	if s.hook != nil {
		s.hook("delete", ref)
	}
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

// genOf liefert die Generation des Haupteintrags.
func genOf(t *testing.T, st *memStore) string {
	t.Helper()
	_, rec, ok := readMain(st, testRef)
	if !ok {
		t.Fatal("main entry unreadable")
	}
	return rec.Gen
}

// bigToken liefert ein Refresh-Token, das drei Teile braucht.
func bigToken(tag string) secrets.Secret {
	return secrets.Secret("CANARY-big-" + tag + "-" + strings.Repeat("Ab9_-", 4096/5))
}

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
	gen := genOf(t, st)
	if _, ok := st.m[PartRef(testRef, gen, 3)]; !ok || PartRef(testRef, gen, 3) != "oauthpart3g"+gen+"/1a2b3c4d/outlook" || len(st.keys()) != 4 {
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
	gen = genOf(t, st)
	st.m[PartRef(testRef, gen, 2)] = strings.Repeat("y", MaxEntryBytes)
	if _, err := Load(st, testRef); !errors.Is(err, ErrTokenCorrupt) {
		t.Fatalf("corrupt part: %v", err)
	}
	delete(st.m, PartRef(testRef, gen, 2))
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

// W2 (Gegenprüfung T4): Scheitert das Schreiben an irgendeiner Stelle,
// bleibt das alte Token lesbar, und von der neuen Generation bleibt nichts
// liegen. Vorher überschrieb Save die Teile an Ort und Stelle; ein Fehler
// vor dem Haupteintrag zerstörte auch das alte Token (ErrTokenCorrupt).
func TestSaveFailureKeepsOldToken(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, c := range []struct {
		name     string
		old, new secrets.Secret
		sets     int // Set-Aufrufe des zweiten Speicherns
	}{
		{"big to big", bigToken("old"), bigToken("new"), 4},
		{"small to big", "CANARY-small-old", bigToken("new"), 4},
		{"big to small", bigToken("old"), "CANARY-small-new", 1},
	} {
		for k := 1; k <= c.sets; k++ {
			st := newMemStore()
			if err := Save(st, testRef, Token{RefreshToken: c.old}, now); err != nil {
				t.Fatal(err)
			}
			before := st.keys()
			slices.Sort(before)
			st.sets, st.failAt = 0, k
			if err := Save(st, testRef, Token{RefreshToken: c.new}, now); err == nil {
				t.Fatalf("%s, write %d fails: Save must fail", c.name, k)
			}
			got, err := Load(st, testRef)
			if err != nil || got.RefreshToken != c.old {
				t.Fatalf("%s, write %d fails: old token lost: %v", c.name, k, err)
			}
			after := st.keys()
			slices.Sort(after)
			if !slices.Equal(before, after) {
				t.Fatalf("%s, write %d fails: entries %v, want %v", c.name, k, after, before)
			}
		}
	}
}

// Teile ohne Generation (Format von 0.3.0-ms.1) werden gelesen und beim
// nächsten Speichern abgeräumt; Delete räumt sie ebenfalls ab.
func TestSaveReplacesLegacyParts(t *testing.T) {
	st := newMemStore()
	rt := string(bigToken("legacy"))
	sum := sha256.Sum256([]byte(rt))
	main, _ := json.Marshal(stored{V: storedVersion, Parts: 3, PartsSHA256: hex.EncodeToString(sum[:])})
	st.m[testRef] = string(main)
	for i := 0; i < 3; i++ {
		st.m[PartRef(testRef, "", i+1)] = rt[i*MaxEntryBytes : min((i+1)*MaxEntryBytes, len(rt))]
	}
	if got, err := Load(st, testRef); err != nil || got.RefreshToken.Reveal() != rt {
		t.Fatalf("legacy load: %v", err)
	}
	if err := Save(st, testRef, Token{RefreshToken: bigToken("new")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	gen := genOf(t, st)
	for _, k := range st.keys() {
		if k != testRef && !strings.HasPrefix(k, "oauthpart") || strings.HasPrefix(k, "oauthpart") && !strings.Contains(k, "g"+gen+"/") {
			t.Fatalf("legacy part left: %v", st.keys())
		}
	}
	st.m[PartRef(testRef, "", 5)] = "x" // Rest hinter einer Lücke
	if err := Delete(st, testRef); err != nil || len(st.keys()) != 0 {
		t.Fatalf("delete: %v %v", err, st.keys())
	}
}

// Zwei Prozesse speichern gleichzeitig: Der zweite läuft vollständig an
// jeder Stelle des ersten. Load liefert immer eines der beiden Token, nie
// ErrTokenCorrupt, und zwar das des zuletzt geschriebenen Haupteintrags.
// Teile bleiben nur im beschriebenen Rest liegen (der zweite stellt
// zwischen dem letzten Lesen und dem Schreiben des Haupteintrags um).
func TestConcurrentSavesNeverCorrupt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for k := 1; ; k++ {
		st := newMemStore()
		if err := Save(st, testRef, Token{RefreshToken: bigToken("old")}, now); err != nil {
			t.Fatal(err)
		}
		ops, fired, mainSet := 0, false, false
		var errB error
		st.hook = func(op, ref string) {
			ops++
			if ops != k || fired {
				return
			}
			fired = true
			mainSet = op == "set" && ref == testRef
			hook := st.hook
			st.hook = nil
			errB = Save(st, testRef, Token{RefreshToken: bigToken("B")}, now)
			// Der zweite Prozess liest mitten im ersten.
			if _, err := Load(st, testRef); err != nil {
				t.Fatalf("op %d: load during save: %v", k, err)
			}
			st.hook = hook
		}
		errA := Save(st, testRef, Token{RefreshToken: bigToken("A")}, now)
		st.hook = nil
		if !fired {
			break // alle Stellen durch
		}
		if errA != nil || errB != nil {
			t.Fatalf("op %d: A %v, B %v", k, errA, errB)
		}
		got, err := Load(st, testRef)
		if err != nil || (got.RefreshToken != bigToken("A") && got.RefreshToken != bigToken("B")) {
			t.Fatalf("op %d: load: %v", k, err)
		}
		gen := genOf(t, st)
		var left []string
		for _, r := range st.keys() {
			if r != testRef && !strings.Contains(r, "g"+gen+"/") {
				left = append(left, r)
			}
		}
		if len(left) > 0 && !mainSet {
			t.Errorf("op %d: parts left: %v", k, left)
		}
	}
}

// Zwei Prozesse (Goroutinen) speichern wiederholt, ein dritter liest:
// nie ErrTokenCorrupt (auch unter make race).
func TestConcurrentSavesStress(t *testing.T) {
	st := newMemStore()
	if err := Save(st, testRef, Token{RefreshToken: bigToken("start")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for p := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				tok := bigToken(fmt.Sprintf("p%d-%d", p, i))
				if i%3 == 2 {
					tok = secrets.Secret(fmt.Sprintf("CANARY-small-p%d-%d", p, i))
				}
				if err := Save(st, testRef, Token{RefreshToken: tok}, time.Now()); err != nil {
					t.Errorf("save: %v", err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := Load(st, testRef); err != nil {
				t.Errorf("load during saves: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-done
	if _, err := Load(st, testRef); err != nil {
		t.Fatal(err)
	}
}
