// Package actions enthält die Fachlogik der Postfach-Aktionen (A03 S3.1,
// S3.2; E1 bis E4): Ziele auflösen (nur aus dem Spiegel, nie aus einer
// Eingabe oder aus importierten Ordnern), Aktionen und Anträge anlegen,
// Freigaben entscheiden (service.go) und die Aktionen im Prozess mit
// Zugangsdaten ausführen (runner.go). Die Operationen in internal/ops sind
// dünn und rufen diesen Dienst; die Rechte prüft vorher die Policy.
package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Store ist der Teil des Stores, den der Dienst braucht.
type Store interface {
	MessageAccounts(ctx context.Context, ids []int64) (map[int64]int64, error)
	ServerCopies(ctx context.Context, messageID int64) ([]store.ServerCopy, error)
	Mailboxes(ctx context.Context, accountID int64) ([]store.Mailbox, error)
	CreateAction(ctx context.Context, n store.NewAction) (store.Action, error)
	Action(ctx context.Context, id int64) (store.Action, error)
	ListActions(ctx context.Context, f store.ActionFilter) ([]store.Action, error)
	DecideAction(ctx context.Context, d store.ActionDecision) (store.Action, error)
	ExpireActions(ctx context.Context, now time.Time) (int, error)
}

// Error ist eine fachliche Ablehnung mit Grund (Code) und englischer
// Meldung ohne Mailinhalt; internal/ops macht daraus eine Ablehnung
// (denied) mit diesem Grund.
type Error struct {
	Reason string
	Msg    string
}

func (e *Error) Error() string { return "denied (" + e.Reason + "): " + e.Msg }

func deny(reason, format string, args ...any) error {
	return &Error{Reason: reason, Msg: fmt.Sprintf(format, args...)}
}

// Gründe einer Ablehnung.
const (
	ReasonNoServerCopy    = "no_server_copy"
	ReasonAmbiguousSource = "ambiguous_source"
	ReasonNotInMailbox    = "not_in_mailbox"
	ReasonTargetNotFound  = "target_not_found"
	ReasonNoTrashMailbox  = "no_trash_mailbox"
	ReasonNothingToDo     = "nothing_to_do"
	ReasonNotRequested    = "not_requested"
)

// Service legt Aktionen an und entscheidet über Anträge.
type Service struct {
	Store Store
	// TTL: so lange gilt ein Antrag ([approval] ttl; 0 = 168h).
	TTL time.Duration
	// Now ist für Tests ersetzbar.
	Now func() time.Time
}

// DefaultTTL ist die Frist eines Antrags ohne Konfiguration.
const DefaultTTL = 168 * time.Hour

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return DefaultTTL
}

// IDsHash ist der SHA-256 der sortierten IDs (Dezimal, durch Komma
// getrennt): steht in der Aktion und im Audit statt der vollen Liste.
func IDsHash(ids []int64) string {
	s := slices.Clone(ids)
	slices.Sort(s)
	parts := make([]string, len(s))
	for i, id := range s {
		parts[i] = strconv.FormatInt(id, 10)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(sum[:])
}

// Unique liefert die IDs ohne Doppelte, aufsteigend.
func Unique(ids []int64) []int64 {
	s := slices.Clone(ids)
	slices.Sort(s)
	return slices.Compact(s)
}

// CheckAccount prüft, dass alle Nachrichten zum Konto gehören; eine fremde
// oder unbekannte ID ist „nicht gefunden“ (die Meldung nennt bis zu 20 IDs,
// keine Inhalte).
func (s *Service) CheckAccount(ctx context.Context, accountID int64, ids []int64) error {
	acc, err := s.Store.MessageAccounts(ctx, ids)
	if err != nil {
		return err
	}
	var missing []string
	for _, id := range ids {
		if a, ok := acc[id]; !ok || a != accountID {
			missing = append(missing, strconv.FormatInt(id, 10))
		}
	}
	if len(missing) > 0 {
		if len(missing) > 20 {
			missing = append(missing[:20], "…")
		}
		return fmt.Errorf("%w: ids %s not in this account", store.ErrMessageNotFound, strings.Join(missing, ", "))
	}
	return nil
}

// Target nennt den Zielordner von move und trash.
type Target struct {
	MailboxID int64
	Path      string
	Role      string
}

// FindMailbox sucht im Spiegel des Kontos den Ordner per Pfad oder Rolle
// (genau eines von beiden). ok = false: keiner.
func (s *Service) FindMailbox(ctx context.Context, accountID int64, path, role string) (Target, bool, error) {
	boxes, err := s.Store.Mailboxes(ctx, accountID)
	if err != nil {
		return Target{}, false, err
	}
	for _, b := range boxes {
		if (path != "" && b.Path == path) || (path == "" && role != "" && b.Role == role) {
			return Target{MailboxID: b.ID, Path: b.Path, Role: b.Role}, true, nil
		}
	}
	return Target{}, false, nil
}

// Plan ist das Ergebnis der Auflösung: Ziele je Serverkopie und
// Nachrichten, die übersprungen werden (schon im Zielordner bzw. im
// Papierkorb).
type Plan struct {
	Targets []store.ActionTarget
	Skipped []int64
}

// PlanFlags löst die Ziele von mark und keywords auf: alle Serverkopien
// jeder Nachricht (in Gmail genügt eine, andere Server führen Flags je
// Kopie). Eine Nachricht ohne Serverkopie (nur importiert, auf dem Server
// gelöscht) lehnt den ganzen Aufruf ab.
func (s *Service) PlanFlags(ctx context.Context, ids []int64) (Plan, error) {
	var p Plan
	var none []string
	for _, id := range ids {
		cs, err := s.Store.ServerCopies(ctx, id)
		if err != nil {
			return p, err
		}
		if len(cs) == 0 {
			none = append(none, strconv.FormatInt(id, 10))
			continue
		}
		for _, c := range cs {
			p.Targets = append(p.Targets, target(c))
		}
	}
	if len(none) > 0 {
		return p, deny(ReasonNoServerCopy, "messages %s have no copy on the server (imported only or deleted there)", short(none))
	}
	return p, nil
}

// PlanMove löst die Quellkopien von move und trash auf: je Nachricht genau
// eine Kopie. from (Pfad) wählt den Ordner; sonst die einzige Kopie
// außerhalb von „Alle Nachrichten“ (Rolle all), ohne solche die Kopie dort.
// Kopien im Zielordner zählen nicht; liegt eine Nachricht nur dort, wird
// sie übersprungen. Mehrere mögliche Kopien ohne from lehnen den Aufruf ab
// (ambiguous_source), statt zu raten.
func (s *Service) PlanMove(ctx context.Context, ids []int64, from string, dest Target) (Plan, error) {
	var p Plan
	var none, amb, notIn []string
	for _, id := range ids {
		cs, err := s.Store.ServerCopies(ctx, id)
		if err != nil {
			return p, err
		}
		var cand []store.ServerCopy
		inDest := false
		for _, c := range cs {
			if c.MailboxID == dest.MailboxID {
				inDest = true
				continue
			}
			cand = append(cand, c)
		}
		if from != "" {
			i := slices.IndexFunc(cand, func(c store.ServerCopy) bool { return c.Mailbox == from })
			switch {
			case i >= 0:
				p.Targets = append(p.Targets, target(cand[i]))
			case inDest:
				p.Skipped = append(p.Skipped, id)
			default:
				notIn = append(notIn, strconv.FormatInt(id, 10))
			}
			continue
		}
		var main []store.ServerCopy
		for _, c := range cand {
			if c.Role != string(provider.RoleAll) {
				main = append(main, c)
			}
		}
		switch {
		case len(main) == 1:
			p.Targets = append(p.Targets, target(main[0]))
		case len(main) > 1:
			amb = append(amb, strconv.FormatInt(id, 10))
		case len(cand) == 1:
			p.Targets = append(p.Targets, target(cand[0]))
		case len(cand) == 0 && inDest:
			p.Skipped = append(p.Skipped, id)
		case len(cand) == 0:
			none = append(none, strconv.FormatInt(id, 10))
		default:
			amb = append(amb, strconv.FormatInt(id, 10))
		}
	}
	switch {
	case len(none) > 0:
		return p, deny(ReasonNoServerCopy, "messages %s have no copy on the server (imported only or deleted there)", short(none))
	case len(notIn) > 0:
		return p, deny(ReasonNotInMailbox, "messages %s are not in the mailbox given as source", short(notIn))
	case len(amb) > 0:
		return p, deny(ReasonAmbiguousSource, "messages %s are in several mailboxes; name the source mailbox (from)", short(amb))
	case len(p.Targets) == 0:
		return p, deny(ReasonNothingToDo, "all messages are already in the target mailbox")
	}
	return p, nil
}

func target(c store.ServerCopy) store.ActionTarget {
	return store.ActionTarget{MessageID: c.MessageID, MailboxID: c.MailboxID, UID: c.UID, UIDValidity: c.UIDValidity,
		Mailbox: c.Mailbox, Role: c.Role}
}

func short(ids []string) string {
	if len(ids) > 20 {
		ids = append(slices.Clone(ids[:20]), "…")
	}
	return strings.Join(ids, ", ")
}

// Request beschreibt eine neue Aktion.
type Request struct {
	AccountID    int64
	AccountName  string
	Kind         string
	IDs          []int64
	Params       store.ActionParams
	Plan         Plan
	ConfirmCount *int
	// RequestedBy: "cli" | "key:<name>"; AuditID: Start-Eintrag des Aufrufs.
	RequestedBy string
	AuditID     int64
	// Approval: als Antrag anlegen (Papierkorb mit Schlüssel und ohne
	// Freigabe im selben Zug, E3); sonst sofort ausführbar.
	Approval bool
}

// Create legt die Aktion an (queued, bzw. requested mit Antrag).
func (s *Service) Create(ctx context.Context, r Request) (store.Action, error) {
	now := s.now()
	n := store.NewAction{AccountID: r.AccountID, Kind: r.Kind, Params: r.Params, State: store.ActionQueued, IDsHash: IDsHash(r.IDs),
		MessageCount: len(r.IDs) - len(r.Plan.Skipped), ConfirmCount: r.ConfirmCount, RequestedBy: r.RequestedBy, AuditID: r.AuditID,
		Targets: r.Plan.Targets, Now: now}
	if r.Approval {
		n.State = store.ActionRequested
		n.Approval = &store.Approval{
			Description: fmt.Sprintf("%s of %d messages (%d server copies) in account %s, ids hash %s",
				r.Kind, n.MessageCount, len(r.Plan.Targets), r.AccountName, n.IDsHash[:12]),
			RequestedBy: r.RequestedBy, RequestedAt: now, PreviewHash: previewHash(r), ExpiresAt: now.Add(s.ttl())}
	}
	return s.Store.CreateAction(ctx, n)
}

// previewHash bindet den Antrag an Art, Nachrichten und Serverkopien.
func previewHash(r Request) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d", r.Kind, IDsHash(r.IDs), r.Params.TargetMailboxID)
	for _, t := range r.Plan.Targets {
		fmt.Fprintf(h, "|%d:%d:%d:%d", t.MessageID, t.MailboxID, t.UIDValidity, t.UID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Expire lässt abgelaufene Anträge verfallen (vor jedem Lesen und
// Entscheiden; Systemereignis im Audit).
func (s *Service) Expire(ctx context.Context) error {
	_, err := s.Store.ExpireActions(ctx, s.now())
	return err
}

// Decide gibt einen Antrag frei oder lehnt ihn ab (nur der Mensch;
// HumanOnly prüft die Policy). keyID und auditID werden die Herkunft des
// Jobs.
func (s *Service) Decide(ctx context.Context, actionID int64, approve bool, decidedBy, reason, keyID string, auditID int64) (store.Action, error) {
	if err := s.Expire(ctx); err != nil {
		return store.Action{}, err
	}
	a, err := s.Store.DecideAction(ctx, store.ActionDecision{ActionID: actionID, Approve: approve, DecidedBy: decidedBy, Reason: reason,
		Now: s.now(), KeyID: keyID, AuditID: auditID})
	if errors.Is(err, store.ErrActionState) {
		cur, gerr := s.Store.Action(ctx, actionID)
		if gerr != nil {
			return store.Action{}, gerr
		}
		return cur, deny(ReasonNotRequested, "action %d is %s, not waiting for approval", actionID, cur.State)
	}
	return a, err
}
