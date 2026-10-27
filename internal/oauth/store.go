package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Ablage im Schlüsselbund (S3.7, A03_teile T4 Punkt 1).
//
// Gespeichert werden nur das Refresh-Token und der Ablauf des letzten
// Access-Tokens; das Access-Token selbst bleibt im Speicher des Prozesses
// (nach dem Start erneuert die Quelle es einmal). Der Windows-Schlüsselbund
// nimmt höchstens 2.560 Byte je Eintrag (go-keyring, keyring_windows.go),
// macOS höchstens 4.096 Byte je Kommando. Passt der Eintrag nicht in
// MaxEntryBytes, verteilt Save das Refresh-Token auf nummerierte Einträge
// (PartRef) und schreibt in den Haupteintrag nur Zahl und Prüfsumme der
// Teile.
//
// Kodiert wird ausdrücklich mit Reveal: json.Marshal eines Tokens liefert
// absichtlich "[redacted]" (secrets.Secret) und darf nie die Ablage sein.

// MaxEntryBytes ist die Obergrenze je Schlüsselbund-Eintrag (mit Abstand zu
// 2.560 Byte unter Windows und zur Kommandogrenze unter macOS).
const MaxEntryBytes = 2000

// maxParts begrenzt die Teile (32 × 2.000 Byte; Microsoft-Token sind
// deutlich kleiner).
const maxParts = 32

// storedVersion ist die Version des Formats.
const storedVersion = 1

// ErrTokenCorrupt: Der Eintrag ist unlesbar oder seine Teile passen nicht
// zusammen (etwa nach einem abgebrochenen Schreiben). Abhilfe: account login.
var ErrTokenCorrupt = errors.New("oauth: stored token is unreadable; authorize the account again")

// stored ist das Format des Haupteintrags. Nur hier stehen Klartexte, und
// nur zum Speichern.
type stored struct {
	V            int    `json:"v"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Parts        int    `json:"parts,omitempty"`
	PartsSHA256  string `json:"parts_sha256,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// Saved ist, was Load aus dem Schlüsselbund liest.
type Saved struct {
	RefreshToken secrets.Secret
	// Expiry ist der Ablauf des zuletzt ausgegebenen Access-Tokens
	// (Auskunft für doctor; das Token selbst ist nicht gespeichert).
	Expiry time.Time
	// UpdatedAt ist der Zeitpunkt des letzten Speicherns (Anmeldung oder
	// Refresh).
	UpdatedAt time.Time
	// Parts ist die Zahl der Teil-Einträge (0 = alles im Haupteintrag).
	Parts int
}

// PartRef liefert den Verweis des i-ten Teils (ab 1) zum Haupteintrag ref:
// "account/1a2b3c4d/outlook" → "oauthpart2/1a2b3c4d/outlook".
func PartRef(ref string, i int) string {
	_, rest, _ := strings.Cut(ref, "/")
	return "oauthpart" + strconv.Itoa(i) + "/" + rest
}

// Save speichert Refresh-Token und Ablauf unter ref (und nötigenfalls in
// Teilen), liest danach zurück und vergleicht. Teile eines früheren, längeren
// Tokens werden gelöscht. Der Speicher ist secrets.Stored: Werte aus der
// Umgebung gelten für OAuth2-Konten nicht.
func Save(st secrets.Store, ref string, tok Token, now time.Time) error {
	if tok.RefreshToken.IsZero() {
		return errors.New("oauth: no refresh token to store")
	}
	st = secrets.Stored(st)
	rec := stored{V: storedVersion, RefreshToken: tok.RefreshToken.Reveal(), UpdatedAt: now.UTC().Format(time.RFC3339)}
	if !tok.Expiry.IsZero() {
		rec.Expiry = tok.Expiry.UTC().Format(time.RFC3339)
	}
	main, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	parts := 0
	if len(main) > MaxEntryBytes {
		rt := rec.RefreshToken
		for i := 0; i < len(rt); i += MaxEntryBytes {
			parts++
		}
		if parts > maxParts {
			return fmt.Errorf("oauth: refresh token too long to store (%d bytes)", len(rt))
		}
		for i := 0; i < parts; i++ {
			end := min((i+1)*MaxEntryBytes, len(rt))
			if err := st.Set(PartRef(ref, i+1), secrets.Secret(rt[i*MaxEntryBytes:end])); err != nil {
				return fmt.Errorf("oauth: store token part %d: %w", i+1, err)
			}
		}
		sum := sha256.Sum256([]byte(rt))
		rec.RefreshToken, rec.Parts, rec.PartsSHA256 = "", parts, hex.EncodeToString(sum[:])
		if main, err = json.Marshal(rec); err != nil {
			return err
		}
	}
	if err := st.Set(ref, secrets.Secret(main)); err != nil {
		return fmt.Errorf("oauth: store token: %w", err)
	}
	deleteParts(st, ref, parts+1)
	got, err := Load(st, ref)
	if err != nil {
		return fmt.Errorf("oauth: stored token could not be read back: %w", err)
	}
	if got.RefreshToken.Reveal() != tok.RefreshToken.Reveal() {
		return errors.New("oauth: stored token differs after writing")
	}
	return nil
}

// deleteParts löscht die Teile ab from, bis einer fehlt (nach Save sind
// die Teile lückenlos).
func deleteParts(st secrets.Store, ref string, from int) {
	for i := from; i <= maxParts; i++ {
		if err := st.Delete(PartRef(ref, i)); err != nil {
			return
		}
	}
}

// Load liest Refresh-Token und Ablauf. Fehlt der Eintrag: secrets.ErrNotFound.
func Load(st secrets.Store, ref string) (Saved, error) {
	st = secrets.Stored(st)
	raw, err := st.Get(ref)
	if err != nil {
		return Saved{}, err
	}
	var rec stored
	if err := json.Unmarshal([]byte(raw.Reveal()), &rec); err != nil || rec.V != storedVersion {
		return Saved{}, ErrTokenCorrupt
	}
	out := Saved{Parts: rec.Parts}
	if rec.Expiry != "" {
		if out.Expiry, err = time.Parse(time.RFC3339, rec.Expiry); err != nil {
			return Saved{}, ErrTokenCorrupt
		}
	}
	if rec.UpdatedAt != "" {
		if out.UpdatedAt, err = time.Parse(time.RFC3339, rec.UpdatedAt); err != nil {
			return Saved{}, ErrTokenCorrupt
		}
	}
	rt := rec.RefreshToken
	if rec.Parts > 0 {
		if rec.Parts > maxParts || rt != "" {
			return Saved{}, ErrTokenCorrupt
		}
		var b strings.Builder
		for i := 1; i <= rec.Parts; i++ {
			p, err := st.Get(PartRef(ref, i))
			if errors.Is(err, secrets.ErrNotFound) {
				return Saved{}, ErrTokenCorrupt
			} else if err != nil {
				return Saved{}, err
			}
			b.WriteString(p.Reveal())
		}
		rt = b.String()
		sum := sha256.Sum256([]byte(rt))
		if hex.EncodeToString(sum[:]) != rec.PartsSHA256 {
			return Saved{}, ErrTokenCorrupt
		}
	}
	if rt == "" {
		return Saved{}, ErrTokenCorrupt
	}
	out.RefreshToken = secrets.Secret(rt)
	return out, nil
}

// Delete löscht Haupteintrag und alle Teile (auch hinter einer Lücke).
// Fehlt der Haupteintrag: secrets.ErrNotFound (Teile werden trotzdem
// gelöscht).
func Delete(st secrets.Store, ref string) error {
	st = secrets.Stored(st)
	err := st.Delete(ref)
	for i := 1; i <= maxParts; i++ {
		_ = st.Delete(PartRef(ref, i))
	}
	return err
}
