package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Executor führt Operationen durch die feste Kette aus:
//
//	Scope + Policy  →  (Freigabe, ab Schnitt 2)  →  Audit  →  Handler
//
// Keine Oberfläche darf einen Handler direkt aufrufen; Operation.Handler
// anzufassen ist ein Review-Fehler (Regel in CLAUDE.md). Sobald mehrere
// Operationen existieren, kommt ein Vertragstest hinzu, der jede
// registrierte Operation mit jedem Scope und jeder Risikostufe durch den
// Executor schickt.
type Executor struct {
	Registry *Registry
	Audit    audit.Writer
	// Now ist für Tests ersetzbar.
	Now func() time.Time
}

// Request ist ein Aufruf einer Oberfläche.
type Request struct {
	Key       policy.Key
	Actor     audit.ActorType
	Operation string
	Input     json.RawMessage
	// DryRun ist nil, wenn der Aufrufer nichts angegeben hat
	// (dann gilt der Standard des Schlüssels).
	DryRun *bool
	// Secret wird unverändert an den Handler gereicht (Call.Secret), nie auditiert.
	Secret secrets.Secret
}

// Result ist das Ergebnis eines erfolgreichen Aufrufs.
type Result struct {
	Output   any
	Decision policy.Decision
}

// PolicyError wird geliefert, wenn die Policy den Handler nicht freigibt.
type PolicyError struct {
	Operation string
	Decision  policy.Decision
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("ops: %s: %s (%s)", e.Operation, e.Decision.Outcome, e.Decision.Reason)
}

// Sentinel-Fehler für errors.Is; die Oberflächen bilden sie auf Exit-Codes
// bzw. MCP-Statuswerte ab.
var (
	ErrDenied        = errors.New("ops: denied")
	ErrNeedsApproval = errors.New("ops: approval required")
)

func (e *PolicyError) Is(target error) bool {
	switch target {
	case ErrDenied:
		return e.Decision.Outcome == policy.Deny
	case ErrNeedsApproval:
		return e.Decision.Outcome == policy.NeedsApproval
	}
	return false
}

// Execute führt die Operation aus. Schlägt das Schreiben des Audit-Eintrags
// fehl, läuft der Handler nicht (fail closed).
func (x *Executor) Execute(ctx context.Context, req Request) (Result, error) {
	op, err := x.Registry.Get(req.Operation)
	if err != nil {
		return Result{}, err
	}
	now := x.Now
	if now == nil {
		now = time.Now
	}
	start := now()

	dryRun := policy.EffectiveDryRun(req.Key, req.DryRun)
	dec := policy.Check(req.Key, op.Requirement(), dryRun)

	params, targets := json.RawMessage(`{}`), []string(nil)
	if op.Redact != nil {
		params, targets = op.Redact(req.Input)
	}
	ev := audit.Event{
		TS:             start.UTC(),
		ActorType:      req.Actor,
		KeyID:          req.Key.ID,
		Scope:          string(op.Scope),
		Operation:      op.Name,
		ParamsRedacted: params,
		Targets:        targets,
		Reason:         dec.Reason,
		DryRun:         dec.DryRun,
	}

	if !dec.Allowed() {
		ev.Result = audit.ResultDenied
		if dec.Outcome == policy.NeedsApproval {
			ev.Result = audit.ResultNeedsApproval
		}
		if err := x.Audit.Append(ctx, ev); err != nil {
			return Result{}, fmt.Errorf("ops: audit: %w", err)
		}
		return Result{Decision: dec}, &PolicyError{Operation: op.Name, Decision: dec}
	}

	ev.Result = audit.ResultOK
	if err := x.Audit.Append(ctx, ev); err != nil {
		return Result{}, fmt.Errorf("ops: audit: %w", err)
	}

	out, herr := op.Handler(ctx, Call{Input: req.Input, DryRun: dec.DryRun, Secret: req.Secret})
	if herr != nil {
		ev.Result = audit.ResultError
		ev.Reason = "handler_error"
		ev.Duration = now().Sub(start)
		// Fehler beim Nachtragen dürfen den Handler-Fehler nicht verdecken.
		_ = x.Audit.Append(ctx, ev)
		return Result{Decision: dec}, herr
	}
	return Result{Output: out, Decision: dec}, nil
}
