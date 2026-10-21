package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Umzug der Schlüsselbund-Verweise (S2.0/A2): bis S1 "account/<name>", ab
// Schema 8 "account/<datadir-id>/<name>". Das ist keine Operation des
// Registers, sondern wie AuthenticateKey ein Schritt beim Öffnen: Die CLI
// ruft ihn einmal je Prozess auf, bevor ein Kommando läuft, außer im
// MCP-Server (der kennt keine Passwörter, docs/sicherheit.md, Abschnitt 4).

// CredentialRefRepo ist der Teil des Stores, den der Umzug braucht.
type CredentialRefRepo interface {
	ListAccounts(ctx context.Context) ([]store.AccountSummary, error)
	DataDirID(ctx context.Context) (string, error)
	SetCredentialRef(ctx context.Context, id int64, ref string) error
	AddCredentialRefLeft(ctx context.Context, ref string) error
}

// CredentialMove ist das Ergebnis je Konto, das noch einen alten Verweis
// hatte. Es enthält nie ein Geheimnis.
type CredentialMove struct {
	Account string
	From    string
	To      string
	// Result: moved (Eintrag umgezogen, alter gelöscht) | moved_old_left
	// (umgezogen, alter Eintrag blieb, doctor warnt) | ref_only (kein
	// gespeicherter Eintrag, z. B. Passwort aus der Umgebung: nur der
	// Verweis wechselt) | failed (nichts geändert, der alte Verweis gilt
	// weiter; nächster Versuch beim nächsten Öffnen) | postponed
	// (Schlüsselbund nicht erreichbar, nichts versucht).
	Result string
	// Err ist der Grund bei failed und moved_old_left (ohne Geheimnis).
	Err error
}

// MoveCredentialRefs zieht die Einträge aller Konten mit altem Verweis um:
// lesen alt → schreiben neu → prüfen → Verweis in der Datenbank umstellen →
// löschen alt. Der Verweis wechselt erst, wenn der neue Eintrag sicher
// steht; scheitert vorher etwas, bleibt alles beim Alten. Scheitert nur das
// Löschen, bleibt der alte Eintrag stehen und wird in meta vermerkt
// (`doctor` warnt). Gelesen wird unter der Umgebungsschicht
// (secrets.Stored): Ein Passwort aus POSTKAUZ_PASSWORD_<NAME> landet nie im
// Schlüsselbund. Ist der Speicher nicht erreichbar, wird nichts versucht
// (ein vorübergehend gesperrter Schlüsselbund meldet sonst „nicht
// gefunden", und der Verweis zeigte danach ins Leere).
func MoveCredentialRefs(ctx context.Context, repo CredentialRefRepo, sec secrets.Store) ([]CredentialMove, error) {
	dirID, err := repo.DataDirID(ctx)
	if err != nil {
		return nil, err
	}
	accts, err := repo.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var todo []store.Account
	for _, a := range accts {
		if a.CredentialRef != secrets.AccountRef(dirID, a.Name) {
			todo = append(todo, a.Account)
		}
	}
	if len(todo) == 0 {
		return nil, nil
	}
	base := secrets.Stored(sec)
	available := base.Status().Available
	var out []CredentialMove
	for _, a := range todo {
		mv := CredentialMove{Account: a.Name, From: a.CredentialRef, To: secrets.AccountRef(dirID, a.Name)}
		if !available {
			mv.Result = "postponed"
			out = append(out, mv)
			continue
		}
		mv.Result, mv.Err = moveOne(ctx, repo, base, a.ID, mv.From, mv.To)
		out = append(out, mv)
	}
	return out, nil
}

func moveOne(ctx context.Context, repo CredentialRefRepo, base secrets.Store, id int64, from, to string) (string, error) {
	if err := secrets.ValidRef(from); err != nil {
		return "failed", err
	}
	v, err := base.Get(from)
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		// Nichts gespeichert (Passwort aus der Umgebung oder nie
		// hinterlegt): Die Umgebungsvariable hängt nur am Kontonamen, der
		// Verweis kann sofort wechseln.
		if err := repo.SetCredentialRef(ctx, id, to); err != nil {
			return "failed", err
		}
		return "ref_only", nil
	case err != nil:
		return "failed", fmt.Errorf("read %s: %w", from, err)
	}
	if err := base.Set(to, v); err != nil {
		return "failed", fmt.Errorf("write %s: %w", to, err)
	}
	if got, err := base.Get(to); err != nil || got.Reveal() != v.Reveal() {
		if err == nil {
			err = errors.New("value differs after writing")
		}
		return "failed", fmt.Errorf("check %s: %w", to, err)
	}
	if err := repo.SetCredentialRef(ctx, id, to); err != nil {
		// Der neue Eintrag bleibt (harmlos, der nächste Versuch überschreibt
		// ihn mit demselben Wert); der alte gilt weiter.
		return "failed", err
	}
	if err := base.Delete(from); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		if lerr := repo.AddCredentialRefLeft(ctx, from); lerr != nil {
			return "moved_old_left", errors.Join(fmt.Errorf("delete %s: %w", from, err), lerr)
		}
		return "moved_old_left", fmt.Errorf("delete %s: %w", from, err)
	}
	return "moved", nil
}
