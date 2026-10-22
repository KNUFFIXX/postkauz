package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Executor führt Operationen durch die feste Kette aus:
//
//	Scope + Policy  →  (Freigabe, ab Schnitt 2)  →  Audit  →  Handler
//
// Keine Oberfläche darf einen Handler direkt aufrufen; Operation.Handler
// anzufassen ist ein Review-Fehler (Regel in CLAUDE.md). Der Vertragstest
// (TestContractEveryOperationRoleAndDryRun) schickt jede Operation aus
// Catalog mit jeder Rolle und jeder Trockenlauf-Variante durch den Executor.
type Executor struct {
	Registry *Registry
	Audit    audit.Writer
	// Keys prüft bei jedem Aufruf mit einem API-Schlüssel, ob er noch gilt
	// (Widerruf, Ablauf); nil = keine Prüfung (Tests, Werkzeuge ohne
	// Schlüssel).
	Keys *KeyGuard
	// Now ist für Tests ersetzbar.
	Now func() time.Time
	// PolicyVersion ist die Version der geladenen Policy-Datei (Tabelle
	// policies, A02 E8); sie steht in jedem Audit-Eintrag (0 = keine).
	PolicyVersion int64
	// OnPanic erfährt von einem abgefangenen Panic im Handler (bereinigt:
	// ohne Panic-Wert, ohne Argumente im Stack); die CLI schreibt ihn ins
	// Log. nil = nur der Fehler an den Aufrufer.
	OnPanic func(op string, r crash.Report)
}

// ErrInternal: Der Handler oder einer seiner Arbeitsschritte ist
// abgestürzt (Panic). Der Aufruf scheitert, der Prozess (z. B. der
// MCP-Server) läuft weiter. Derselbe Wert wie crash.ErrInternal: Ein
// *crash.PanicError aus einer Hilfsgoroutine (crash.Go) zählt ebenso
// (Code internal, Audit-Grund panic).
var ErrInternal = crash.ErrInternal

// PanicError trägt den bereinigten Bericht eines Panics im Handler.
type PanicError struct {
	Operation string
	Report    crash.Report
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("internal error in %s: %s", e.Operation, e.Report.Message)
}

func (e *PanicError) Is(target error) bool { return target == ErrInternal }

// EnvTestPanic löst zu Testzwecken einen Panic mit Inhalt aus (Kanarientest,
// S1.11): "handler:<operation>" im Executor nach dem Handler, mit dessen
// Ausgabe im Panic-Wert; "cli:<operation>" in der CLI vor der Ausgabe. Wer
// die Umgebung eines Prozesses setzt, kann ihn ohnehin beenden; mehr kann
// der Schalter nicht.
const EnvTestPanic = "POSTKAUZ_INTERNAL_TEST_PANIC"

// callHandler ruft den Handler und fängt einen Panic ab: Ein Fehler in
// einer Operation darf weder Inhalte (Panic-Wert, Argumente im Stack)
// ausgeben noch einen langlebigen Prozess beenden.
func (x *Executor) callHandler(ctx context.Context, op Operation, call Call) (out any, err error) {
	defer func() {
		if v := recover(); v != nil {
			rep := crash.FromPanic(v, debug.Stack())
			if x.OnPanic != nil {
				x.OnPanic(op.Name, rep)
			}
			out, err = nil, &PanicError{Operation: op.Name, Report: rep}
		}
	}()
	out, err = op.Handler(ctx, call)
	if err == nil && os.Getenv(EnvTestPanic) == "handler:"+op.Name {
		panic(fmt.Sprintf("injected test panic with output %+v", out))
	}
	return out, err
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
	// Progress wird an den Handler gereicht (Call.Progress); nil = keine Meldungen.
	Progress func(Progress)
	// Narrow ist ein zusätzliches, strengeres Eingabeschema der Oberfläche
	// (MCP: limit ≤ 100 statt 500). Es wird wie das InputSchema der
	// Operation geprüft und nie statt dessen; nil = keins.
	Narrow *jsonschema.Resolved
}

// Result ist das Ergebnis eines erfolgreichen Aufrufs.
type Result struct {
	Output   any
	Decision policy.Decision
	// AuditID ist die ID des Audit-Eintrags dieses Aufrufs (0, wenn der
	// Writer keine IDs kennt). CLI --explain und MCP nennen sie.
	AuditID int64
}

// PolicyError wird geliefert, wenn die Policy den Handler nicht freigibt.
type PolicyError struct {
	Operation string
	// Scope ist der Scope der Operation (für die Meldung bei missing_scope).
	Scope    policy.Scope
	Decision policy.Decision
	AuditID  int64
}

// Error hat dieselbe Form wie eine fachliche Ablehnung (DeniedError):
// "denied (<grund>): …" bzw. "needs_approval (<grund>): …" (S2.7; vorher
// "ops: <op>: deny (<grund>)", Befund der Teilabnahme S2.3). Die Meldung
// nennt nur Operation und Scope, nie Eingaben.
func (e *PolicyError) Error() string {
	switch {
	case e.Decision.Outcome == policy.NeedsApproval:
		return fmt.Sprintf("needs_approval (%s): %s needs the approval of a human", e.Decision.Reason, e.Operation)
	case e.Decision.Reason == "missing_scope" && e.Scope != "":
		return fmt.Sprintf("denied (missing_scope): %s needs the scope %s, which the key does not have", e.Operation, e.Scope)
	case e.Decision.Reason == "human_only":
		return fmt.Sprintf("denied (human_only): %s is for a human without a key", e.Operation)
	case e.Decision.Reason != "":
		return fmt.Sprintf("denied (%s): %s", e.Decision.Reason, e.Operation)
	}
	return fmt.Sprintf("denied: %s", e.Operation)
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

// appendAudit schreibt ein Ereignis und liefert dessen ID, wenn der Writer
// sie kennt (audit.IDWriter).
func (x *Executor) appendAudit(ctx context.Context, ev audit.Event) (int64, error) {
	if w, ok := x.Audit.(audit.IDWriter); ok {
		return w.AppendID(ctx, ev)
	}
	return 0, x.Audit.Append(ctx, ev)
}

// auditGrace begrenzt das Nachtragen des Fehler-Ereignisses nach einem
// abgebrochenen Aufruf.
const auditGrace = 5 * time.Second

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
		PolicyVersion:  x.PolicyVersion,
	}

	// Ein Schlüssel kann während einer langen Sitzung widerrufen werden
	// oder ablaufen: vor jedem Aufruf prüfen (mit kurzem Zwischenspeicher).
	if x.Keys != nil {
		if kerr := x.Keys.Check(ctx, req.Key, start); kerr != nil {
			ev.Result, ev.Reason = audit.ResultDenied, keyReason(kerr)
			if ev.Reason == "key_check_failed" {
				ev.Result = audit.ResultError
			}
			if _, err := x.appendAudit(ctx, ev); err != nil {
				return Result{}, fmt.Errorf("ops: audit: %w", err)
			}
			return Result{}, kerr
		}
	}

	if !dec.Allowed() {
		ev.Result = audit.ResultDenied
		if dec.Outcome == policy.NeedsApproval {
			ev.Result = audit.ResultNeedsApproval
		}
		id, err := x.appendAudit(ctx, ev)
		if err != nil {
			return Result{}, fmt.Errorf("ops: audit: %w", err)
		}
		return Result{Decision: dec, AuditID: id}, &PolicyError{Operation: op.Name, Scope: op.Scope, Decision: dec, AuditID: id}
	}

	ev.Result = audit.ResultOK
	auditID, err := x.appendAudit(ctx, ev)
	if err != nil {
		return Result{}, fmt.Errorf("ops: audit: %w", err)
	}

	// Die Eingabe muss dem InputSchema entsprechen, egal über welche
	// Oberfläche sie kam (CLI, MCP, später REST). Geprüft wird nach dem
	// Audit-Eintrag: Auch ein ungültiger Aufruf ist nachvollziehbar.
	reason := "handler_error"
	var out any
	herr := op.validateInput(req.Input)
	if herr == nil && req.Narrow != nil {
		herr = validateAgainst(req.Narrow, req.Input)
	}
	if herr != nil {
		reason = "invalid_input"
	} else {
		out, herr = x.callHandler(ctx, op, Call{Input: req.Input, DryRun: dec.DryRun, Secret: req.Secret, Progress: req.Progress,
			Key: req.Key, Actor: req.Actor})
	}
	if herr != nil {
		ev.Result = audit.ResultError
		ev.Reason = reason
		var de *DeniedError
		switch {
		case errors.Is(herr, ErrInternal):
			ev.Reason = "panic"
		case errors.As(herr, &de):
			// Fachliche Ablehnung (S2.3): Ergebnis denied mit ihrem Grund.
			ev.Result, ev.Reason = audit.ResultDenied, de.Reason
		}
		ev.Duration = now().Sub(start)
		// Der häufigste Handler-Fehler ist ein Abbruch (Strg+C): ctx ist dann
		// schon beendet. Das Nachtragen läuft deshalb ohne dessen Abbruch,
		// aber mit eigener Frist. Fehler beim Nachtragen dürfen den
		// Handler-Fehler nicht verdecken.
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditGrace)
		_, _ = x.appendAudit(actx, ev)
		cancel()
		return Result{Decision: dec, AuditID: auditID}, herr
	}
	return Result{Output: out, Decision: dec, AuditID: auditID}, nil
}
