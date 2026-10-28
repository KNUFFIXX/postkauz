package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
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
// (PartRef) und schreibt in den Haupteintrag nur Generation, Zahl und
// Prüfsumme der Teile.
//
// Generationen (Gegenprüfung T4, W2): Jedes Speichern in Teilen legt die
// Teile unter einer neuen, zufälligen Generation ab, stellt erst danach den
// Haupteintrag um und löscht zuletzt die alte Generation. Scheitert das
// Schreiben dazwischen, zeigt der Haupteintrag weiter auf die vollständigen
// alten Teile; zwei Prozesse, die gleichzeitig speichern, überschreiben nie
// die Teile des anderen. Der Schlüsselbund kennt kein Auflisten: Teile einer
// Generation, die kein Haupteintrag mehr nennt, findet nur das Speichern,
// das sie abgelöst hat, oder über PrevGen das darauf folgende (Rest: siehe
// Save).
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
// zusammen. Abhilfe: account login.
var ErrTokenCorrupt = errors.New("oauth: stored token is unreadable; authorize the account again")

// genRE ist die Form einer Generation (8 Hex; leer = Teile ohne Generation
// aus 0.3.0-ms.1).
var genRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// stored ist das Format des Haupteintrags. Nur hier stehen Klartexte, und
// nur zum Speichern.
type stored struct {
	V            int    `json:"v"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Gen          string `json:"gen,omitempty"`
	Parts        int    `json:"parts,omitempty"`
	PartsSHA256  string `json:"parts_sha256,omitempty"`
	// PrevGen und PrevParts nennen die Generation, die dieser Eintrag
	// abgelöst hat (Gegenprüfung T4, W): Kam das Schreiben nach einer
	// Zeitüberschreitung verspätet an, hat niemand sie gelöscht; das
	// nächste Speichern und Delete räumen sie darüber ab.
	PrevGen   string `json:"prev_gen,omitempty"`
	PrevParts int    `json:"prev_parts,omitempty"`
	Expiry    string `json:"expiry,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
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

// PartRef liefert den Verweis des i-ten Teils (ab 1) der Generation gen zum
// Haupteintrag ref: "account/1a2b3c4d/outlook", "9f2c01ab", 2 →
// "oauthpart2g9f2c01ab/1a2b3c4d/outlook"; ohne Generation (Format von
// 0.3.0-ms.1) "oauthpart2/1a2b3c4d/outlook".
func PartRef(ref, gen string, i int) string {
	_, rest, _ := strings.Cut(ref, "/")
	g := ""
	if gen != "" {
		g = "g" + gen
	}
	return "oauthpart" + strconv.Itoa(i) + g + "/" + rest
}

// readMain liest den Haupteintrag roh (ohne Teile). ok = lesbar.
func readMain(st secrets.Store, ref string) (raw string, rec stored, ok bool) {
	v, err := st.Get(ref)
	if err != nil {
		return "", stored{}, false
	}
	raw = v.Reveal()
	if json.Unmarshal([]byte(raw), &rec) != nil || rec.V != storedVersion || (rec.Gen != "" && !genRE.MatchString(rec.Gen)) ||
		rec.Parts < 0 || rec.Parts > maxParts || (rec.PrevGen != "" && !genRE.MatchString(rec.PrevGen)) ||
		rec.PrevParts < 0 || rec.PrevParts > maxParts {
		return raw, stored{}, false
	}
	return raw, rec, true
}

// newGen liefert eine neue Generation, verschieden von den genannten.
func newGen(not ...string) (string, error) {
	for {
		g, err := randomHex(4)
		if err != nil {
			return "", err
		}
		if !slices.Contains(not, g) {
			return g, nil
		}
	}
}

// Save speichert Refresh-Token und Ablauf unter ref (und nötigenfalls in
// Teilen unter einer neuen Generation), liest danach zurück und vergleicht.
// Reihenfolge: neue Teile, Haupteintrag, dann die Teile der abgelösten
// Generationen löschen. Scheitert ein Schreiben, bleibt das alte Token
// lesbar (die schon geschriebenen neuen Teile werden wieder gelöscht).
// Läuft das Schreiben des Haupteintrags in die Zeitgrenze
// (secrets.ErrTimeout), ist der Ausgang unsicher: Save löscht dann nichts
// und meldet den Fehler; kommt das Schreiben später an, gilt das neue Token,
// sonst weiter das alte. Rest: Kommt es nie an, bleiben die neuen Teile
// liegen (wie unten).
//
// Speichert ein zweiter Prozess zur selben Zeit, gewinnt der zuletzt
// geschriebene Haupteintrag; Save meldet dann trotzdem Erfolg (das neuere
// Token liegt im Speicher, die Quelle liest es vor dem nächsten Refresh,
// siehe ops.Credentials) und löscht die eigenen, nun verwaisten Teile.
// Rest: Liegt das Umstellen des anderen Prozesses genau zwischen dessen
// letztem Lesen und Schreiben und nach dem ganzen eigenen Speichern, bleiben
// die eigenen Teile liegen (ein abgelöstes Refresh-Token, das account remove
// nicht findet). Deshalb speichern alle Prozesse über SaveLocked (lock.go);
// Save allein schützt nur vor einem beschädigten Token.
//
// Der Speicher ist secrets.Stored: Werte aus der Umgebung gelten für
// OAuth2-Konten nicht.
func Save(st secrets.Store, ref string, tok Token, now time.Time) error {
	if tok.RefreshToken.IsZero() {
		return errors.New("oauth: no refresh token to store")
	}
	st = secrets.Stored(st)
	rec := stored{V: storedVersion, RefreshToken: tok.RefreshToken.Reveal(), UpdatedAt: now.UTC().Format(time.RFC3339)}
	if !tok.Expiry.IsZero() {
		rec.Expiry = tok.Expiry.UTC().Format(time.RFC3339)
	}
	// Größe mit dem längsten Verweis auf die abgelöste Generation prüfen.
	probe := rec
	probe.PrevGen, probe.PrevParts = "ffffffff", maxParts
	main, err := json.Marshal(probe)
	if err != nil {
		return err
	}
	oldRaw, old, oldOK := readMain(st, ref)
	if len(main) > MaxEntryBytes {
		rt := rec.RefreshToken
		parts := (len(rt) + MaxEntryBytes - 1) / MaxEntryBytes
		if parts > maxParts {
			return fmt.Errorf("oauth: refresh token too long to store (%d bytes)", len(rt))
		}
		gen, err := newGen(old.Gen, old.PrevGen)
		if err != nil {
			return err
		}
		for i := 0; i < parts; i++ {
			end := min((i+1)*MaxEntryBytes, len(rt))
			if err := st.Set(PartRef(ref, gen, i+1), secrets.Secret(rt[i*MaxEntryBytes:end])); err != nil {
				deleteParts(st, ref, gen, i+1)
				return fmt.Errorf("oauth: store token part %d: %w", i+1, err)
			}
		}
		sum := sha256.Sum256([]byte(rt))
		rec.RefreshToken, rec.Gen, rec.Parts, rec.PartsSHA256 = "", gen, parts, hex.EncodeToString(sum[:])
	}
	// Unmittelbar vor dem Umstellen nochmals lesen: Hat ein zweiter Prozess
	// inzwischen gespeichert, ist auch dessen Generation abgelöst.
	curRaw, cur, curOK := readMain(st, ref)
	switch {
	case curOK:
		rec.PrevGen, rec.PrevParts = cur.Gen, cur.Parts
	case oldOK:
		rec.PrevGen, rec.PrevParts = old.Gen, old.Parts
	}
	if main, err = json.Marshal(rec); err != nil {
		deleteParts(st, ref, rec.Gen, rec.Parts)
		return err
	}
	if err := st.Set(ref, secrets.Secret(main)); err != nil {
		if errors.Is(err, secrets.ErrTimeout) {
			// Unsicherer Ausgang (Gegenprüfung T4, W): Das Schreiben kann
			// noch ankommen und den Haupteintrag auf die neue Generation
			// stellen. Daher weder die neuen noch die alten Teile löschen;
			// das nächste Speichern räumt die dann abgelöste Generation über
			// alt/aktuell ab.
			return fmt.Errorf("oauth: store token (outcome unknown, nothing deleted): %w", err)
		}
		deleteParts(st, ref, rec.Gen, rec.Parts)
		return fmt.Errorf("oauth: store token: %w", err)
	}
	got, loadErr := Load(st, ref)
	nowRaw, _, _ := readMain(st, ref)
	// nowRaw == "": Lesefehler (z. B. Zeitüberschreitung) zählt nicht als
	// „abgelöst“, sonst löschte Save die eigenen, gültigen Teile.
	superseded := nowRaw != "" && nowRaw != string(main) && nowRaw != oldRaw && nowRaw != curRaw && loadErr == nil
	// Abgelöste Generationen löschen (eine Generation kehrt nie zurück; die
	// eigene ist neu, also nie die alte). Auch die Teile ohne Generation
	// (0.3.0-ms.1), wenn das neue Token in den Haupteintrag passt, und die
	// Generationen, die alt und aktuell selbst abgelöst hatten (nach einer
	// Zeitüberschreitung hat sie niemand gelöscht).
	drop := func(gen string, n int) {
		if n > 0 && (rec.Parts == 0 || gen != rec.Gen) {
			deleteParts(st, ref, gen, n)
		}
	}
	if oldOK {
		drop(old.Gen, old.Parts)
		drop(old.PrevGen, old.PrevParts)
	}
	if curOK && curRaw != oldRaw {
		drop(cur.Gen, cur.Parts)
		drop(cur.PrevGen, cur.PrevParts)
	}
	if superseded {
		// Ein zweiter Prozess hat nach uns gespeichert: Dessen Token gilt.
		deleteParts(st, ref, rec.Gen, rec.Parts)
		return nil
	}
	if loadErr != nil {
		return fmt.Errorf("oauth: stored token could not be read back: %w", loadErr)
	}
	if got.RefreshToken.Reveal() != tok.RefreshToken.Reveal() {
		return errors.New("oauth: stored token differs after writing")
	}
	// Ein Umstellen eines zweiten Prozesses während des Löschens findet
	// dieser nicht mehr; daher zuletzt prüfen, ob die eigenen Teile noch
	// gelten.
	if rec.Parts > 0 {
		if _, last, ok := readMain(st, ref); ok && last.Gen != rec.Gen {
			deleteParts(st, ref, rec.Gen, rec.Parts)
		}
	}
	return nil
}

// deleteParts löscht die Teile 1 bis n der Generation gen (Fehler und
// fehlende Teile zählen nicht; ein Teil bleibt höchstens als Rest liegen).
func deleteParts(st secrets.Store, ref, gen string, n int) {
	for i := 1; i <= n; i++ {
		_ = st.Delete(PartRef(ref, gen, i))
	}
}

// deletePartsErr löscht die Teile 1 bis n der Generation gen wie
// deleteParts, meldet aber jeden Fehler außer secrets.ErrNotFound (B-M1;
// für Delete, wo ein liegengebliebener Teil nicht mehr auffindbar wäre).
func deletePartsErr(st secrets.Store, ref, gen string, n int) error {
	var errs []error
	for i := 1; i <= n; i++ {
		if err := st.Delete(PartRef(ref, gen, i)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Load liest Refresh-Token und Ablauf. Fehlt der Eintrag: secrets.ErrNotFound.
// Fehlt ein Teil, weil ein zweiter Prozess währenddessen umgestellt und die
// alte Generation gelöscht hat, liest Load den neuen Haupteintrag.
func Load(st secrets.Store, ref string) (Saved, error) {
	st = secrets.Stored(st)
	// Neuer Versuch nur, wenn sich der Haupteintrag inzwischen geändert hat
	// (ein anderer Prozess kam voran); die Grenze schützt vor einer Schleife.
	for range 100 {
		raw, err := st.Get(ref)
		if err != nil {
			return Saved{}, err
		}
		out, err := load(st, ref, raw.Reveal())
		if errors.Is(err, ErrTokenCorrupt) {
			if again, gerr := st.Get(ref); gerr == nil && again.Reveal() != raw.Reveal() {
				continue
			}
		}
		return out, err
	}
	return Saved{}, ErrTokenCorrupt
}

// load liest einen Haupteintrag samt Teilen.
func load(st secrets.Store, ref, raw string) (Saved, error) {
	var rec stored
	if err := json.Unmarshal([]byte(raw), &rec); err != nil || rec.V != storedVersion {
		return Saved{}, ErrTokenCorrupt
	}
	out := Saved{Parts: rec.Parts}
	var err error
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
		if rec.Parts > maxParts || rt != "" || (rec.Gen != "" && !genRE.MatchString(rec.Gen)) {
			return Saved{}, ErrTokenCorrupt
		}
		var b strings.Builder
		for i := 1; i <= rec.Parts; i++ {
			p, err := st.Get(PartRef(ref, rec.Gen, i))
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

// Delete löscht Haupteintrag und alle Teile, die er kennt: die der
// Generation, die der Haupteintrag nennt, die der von ihm abgelösten
// Generation (PrevGen) und die Teile ohne Generation (0.3.0-ms.1), je bis
// maxParts (auch hinter einer Lücke). Teile älterer Generationen, die kein
// Haupteintrag mehr nennt (Rest aus Save), findet Delete nicht: Der
// Schlüsselbund kann nicht auflisten. Fehlt der Haupteintrag:
// secrets.ErrNotFound (Teile ohne Generation werden trotzdem gelöscht).
// Scheitert das Löschen eines Teils (außer "fehlt"), bleibt der
// Haupteintrag stehen und Delete meldet den Fehler (B-M1; sonst meldete
// account remove secret_deleted, und die Teile lägen unauffindbar).
func Delete(st secrets.Store, ref string) error {
	st = secrets.Stored(st)
	// B-M1: Erst alle Teile; scheitert einer (außer "fehlt"), bleibt der
	// Haupteintrag stehen, damit die Teile auffindbar bleiben und ein
	// zweiter Versuch sie findet.
	var errs []error
	if _, rec, ok := readMain(st, ref); ok {
		if rec.Gen != "" {
			errs = append(errs, deletePartsErr(st, ref, rec.Gen, maxParts))
		}
		if rec.PrevGen != "" {
			errs = append(errs, deletePartsErr(st, ref, rec.PrevGen, maxParts))
		}
	}
	errs = append(errs, deletePartsErr(st, ref, "", maxParts))
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("oauth: token parts could not be deleted, the main entry %s stays: %w", ref, err)
	}
	return st.Delete(ref)
}
