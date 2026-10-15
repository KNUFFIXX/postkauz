package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// InsecureFile speichert Geheimnisse AES-256-GCM-verschlüsselt in
// <dir>/secrets.json; der Schlüssel liegt in <dir>/key. Beide Dateien 0600,
// das Verzeichnis 0700.
//
// UNSICHER, NUR FÜR TESTS: Wer die Dateien lesen kann, kann entschlüsseln.
// Die Verschlüsselung verhindert nur, dass der Klartext in Kopien der
// Datenbank, in grep-Treffern oder in Backups ohne den Schlüssel auftaucht.
// Ein Speicher mit Passphrase (docs/sicherheit.md) ist erst bei Bedarf geplant.
type InsecureFile struct {
	dir string
	mu  sync.Mutex
}

// NewInsecureFile liefert den Dateispeicher im Verzeichnis dir.
func NewInsecureFile(dir string) *InsecureFile { return &InsecureFile{dir: dir} }

func (f *InsecureFile) keyPath() string  { return filepath.Join(f.dir, "key") }
func (f *InsecureFile) dataPath() string { return filepath.Join(f.dir, "secrets.json") }

func (f *InsecureFile) aead(create bool) (cipher.AEAD, error) {
	key, err := os.ReadFile(f.keyPath())
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := os.MkdirAll(f.dir, 0o700); err != nil {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(f.keyPath(), key); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key file %s is corrupt", f.keyPath())
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (f *InsecureFile) load() (map[string]string, error) {
	b, err := os.ReadFile(f.dataPath())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("secrets: %s is corrupt", f.dataPath())
	}
	return m, nil
}

func (f *InsecureFile) save(m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(f.dataPath(), b)
}

// writeFileAtomic schreibt über eine Temporärdatei (0600) und rename.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // nach erfolgreichem Rename wirkungslos
	if err := tmp.Chmod(0o600); err != nil && !isWindows() {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (f *InsecureFile) Get(ref string) (Secret, error) {
	if err := ValidRef(ref); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	enc, ok := m[ref]
	if !ok {
		return "", ErrNotFound
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("secrets: entry %s is corrupt", ref)
	}
	a, err := f.aead(false)
	if err != nil {
		return "", err
	}
	if len(raw) < a.NonceSize() {
		return "", fmt.Errorf("secrets: entry %s is corrupt", ref)
	}
	plain, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(ref))
	if err != nil {
		return "", fmt.Errorf("secrets: entry %s cannot be decrypted", ref)
	}
	return Secret(plain), nil
}

func (f *InsecureFile) Set(ref string, s Secret) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	a, err := f.aead(true)
	if err != nil {
		return err
	}
	m, err := f.load()
	if err != nil {
		return err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	// Der Verweis ist Zusatzdatum (AAD): Ein Eintrag lässt sich nicht unter
	// einen anderen Verweis kopieren.
	m[ref] = base64.StdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(s.Reveal()), []byte(ref)))
	return f.save(m)
}

func (f *InsecureFile) Delete(ref string) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[ref]; !ok {
		return ErrNotFound
	}
	delete(m, ref)
	return f.save(m)
}

func (f *InsecureFile) Status() Status {
	return Status{Backend: "insecure-file", Available: true, Insecure: true,
		Detail: "INSECURE, for tests only: encrypted file with its key next to it in " + f.dir}
}
