package secrets

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/KNUFFIXX/postkauz/internal/brand"
)

// keyringAPI sind die drei Funktionen von go-keyring; in Tests ersetzbar,
// ohne den globalen Mock der Bibliothek zu verändern.
type keyringAPI struct {
	set func(service, user, password string) error
	get func(service, user string) (string, error)
	del func(service, user string) error
}

var osKeyring = keyringAPI{set: keyring.Set, get: keyring.Get, del: keyring.Delete}

// keyringTimeout begrenzt jeden Aufruf: Ein hängender D-Bus oder ein
// unbeantworteter Entsperr-Dialog darf die CLI nicht blockieren.
const keyringTimeout = 10 * time.Second

// Keyring ist der Speicher im Schlüsselbund des Betriebssystems.
type Keyring struct {
	api     keyringAPI
	service string
	timeout time.Duration
}

// NewKeyring liefert den Schlüsselbund-Speicher (Dienstname aus internal/brand).
func NewKeyring() *Keyring {
	return &Keyring{api: osKeyring, service: brand.KeyringService, timeout: keyringTimeout}
}

func (k *Keyring) call(f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(k.timeout):
		return fmt.Errorf("%w: keyring did not answer within %s", ErrUnavailable, k.timeout)
	}
}

// mapErr übersetzt Fehler der Bibliothek. Fehlermeldungen von go-keyring
// enthalten nie den Wert, nur Dienst/Konto; trotzdem reichen wir sie nur
// gekürzt durch.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrUnavailable):
		return err
	case errors.Is(err, keyring.ErrSetDataTooBig):
		return fmt.Errorf("secrets: value too large for the keyring")
	default:
		return fmt.Errorf("%w: %s", ErrUnavailable, firstLine(err.Error()))
	}
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func (k *Keyring) Get(ref string) (Secret, error) {
	if err := ValidRef(ref); err != nil {
		return "", err
	}
	var v string
	err := k.call(func() error { var e error; v, e = k.api.get(k.service, ref); return e })
	if err != nil {
		return "", mapErr(err)
	}
	return Secret(v), nil
}

func (k *Keyring) Set(ref string, s Secret) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	return mapErr(k.call(func() error { return k.api.set(k.service, ref, s.Reveal()) }))
}

func (k *Keyring) Delete(ref string) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	return mapErr(k.call(func() error { return k.api.del(k.service, ref) }))
}

// probeRef ist ein Verweis, der nie beschrieben wird; ein Lesen ohne Treffer
// beweist, dass der Schlüsselbund antwortet.
const probeRef = "probe/availability-check"

// Probe prüft ohne Schreibzugriff, ob der Schlüsselbund erreichbar ist.
func (k *Keyring) Probe() Status {
	_, err := k.Get(probeRef)
	switch {
	case err == nil, errors.Is(err, ErrNotFound):
		return Status{Backend: "keyring", Available: true, Detail: keyringName()}
	default:
		detail := strings.TrimPrefix(firstLine(err.Error()), ErrUnavailable.Error()+": ")
		return Status{Backend: "keyring", Available: false, Detail: detail}
	}
}

// Status ruft Probe auf.
func (k *Keyring) Status() Status { return k.Probe() }
