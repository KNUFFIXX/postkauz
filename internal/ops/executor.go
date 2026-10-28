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
//	Scope + Policy (+ Eingabe)  →  Freigabe (R3: needs_approval)  →  Audit (started)  →  Handler  →  Audit (Abschluss)
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
	// OnAuditError erfährt, dass der Abschluss-Eintrag eines Aufrufs nicht
	// geschrieben werden konnte (startID = sein Start-Eintrag); das
	// Ergebnis geht trotzdem an den Aufrufer. nil = nur doctor zählt es.
	OnAuditError func(op string, startID int64, err error)
	// Rules sind die Regeln der Policy-Datei für policy.CheckInput (A9);
	// nil = Operationen mit PolicyInput scheitern (fail closed, T2 Punkt 7).
	Rules *policy.Rules
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

// callPolicyInput ruft Operation.PolicyInput wie den Handler mit
// Panic-Schutz (T2 Punkt 7): Ein Absturz beim Lesen der Eingabe gibt weder
// Inhalte aus noch beendet er einen langlebigen Prozess.
func (x *Executor) callPolicyInput(ctx context.Context, op Operation, input json.RawMessage) (f policy.Facts, err error) {
	defer func() {
		if v := recover(); v != nil {
			rep := crash.FromPanic(v, debug.Stack())
			if x.OnPanic != nil {
				x.OnPanic(op.Name, rep)
			}
			f, err = policy.Facts{}, &PanicError{Operation: op.Name, Report: rep}
		}
	}()
	return op.PolicyInput(ctx, input)
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

// Result ist das Ergebnis eines Aufrufs. Bei einem Fehler ist Output nil,
// außer die Operation erklärt OutputOnError und der Handler liefert eine
// Ausgabe (Bericht bis zum Abbruch).
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

// auditGrace begrenzt das Schreiben des Abschluss-Eintrags nach einem
// abgebrochenen Aufruf (Strg+C).
const auditGrace = 5 * time.Second

// Execute führt die Operation aus (A1 Variante A, E12; A9):
//
//  1. Widerruf/Ablauf des Schlüssels, Policy (Scope, Mensch, Risiko),
//     Eingabe gegen das Schema, dann die Eingabe selbst gegen die Policy
//     (Operation.PolicyInput → policy.CheckInput). Scheitert einer dieser
//     Schritte, entsteht genau ein Audit-Eintrag (denied, needs_approval
//     bzw. error mit Grund invalid_input) und der Handler läuft nicht.
//  2. Sonst der Start-Eintrag (result started). Scheitert er, läuft der
//     Handler nicht (fail closed).
//  3. Handler, danach immer der Abschluss-Eintrag (ok | denied | error) mit
//     eigenem Zeitstempel, Dauer und ref_id auf den Start-Eintrag, auch
//     nach Strg+C (Frist auditGrace). Scheitert nur der Abschluss, kommt
//     das Ergebnis trotzdem zurück; OnAuditError erfährt davon, und doctor
//     zählt Start-Einträge ohne Abschluss.
//
// Ausnahme (E3): Eine Operation mit ApprovalRequest erreicht bei
// needs_approval den Handler, der nur den Antrag anlegt; ihr Abschluss
// lautet needs_approval, Output und Entscheidung gehen ohne Fehler an den
// Aufrufer (Result.Decision.Outcome = NeedsApproval).
//
// Redact sieht nur Eingaben, die das Schema bestanden haben; eine
// ungültige Eingabe steht mit leeren Parametern im Audit.
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

	// Die Eingabe muss dem InputSchema entsprechen, egal über welche
	// Oberfläche sie kam (CLI, MCP, später REST), und dem engeren Schema
	// der Oberfläche. Geprüft wird vor Redact: Redact-Funktionen dürfen von
	// einer gültigen Eingabe ausgehen.
	verr := op.validateInput(req.Input)
	if verr == nil && req.Narrow != nil {
		verr = validateAgainst(req.Narrow, req.Input)
	}
	params, targets := json.RawMessage(`{}`), []string(nil)
	if op.Redact != nil && verr == nil {
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
	// single schreibt den einzigen Eintrag eines Aufrufs, der den Handler
	// nicht erreicht.
	single := func(res audit.Result, reason string) (int64, error) {
		e := ev
		e.Result, e.Reason = res, reason
		id, err := x.appendAudit(ctx, e)
		if err != nil {
			return 0, fmt.Errorf("ops: audit: %w", err)
		}
		return id, nil
	}

	// Ein Schlüssel kann während einer langen Sitzung widerrufen werden
	// oder ablaufen: vor jedem Aufruf prüfen (mit kurzem Zwischenspeicher).
	if x.Keys != nil {
		if kerr := x.Keys.Check(ctx, req.Key, start); kerr != nil {
			res, reason := audit.ResultDenied, keyReason(kerr)
			if reason == "key_check_failed" {
				res = audit.ResultError
			}
			if _, err := single(res, reason); err != nil {
				return Result{}, err
			}
			return Result{}, kerr
		}
	}

	// Ein Antrag (ApprovalRequest, R3) läuft trotz needs_approval: Der
	// Handler legt nur den Antrag an (E3).
	request := dec.Outcome == policy.NeedsApproval && op.ApprovalRequest
	if !dec.Allowed() && !request {
		res := audit.ResultDenied
		if dec.Outcome == policy.NeedsApproval {
			res = audit.ResultNeedsApproval
		}
		id, err := single(res, dec.Reason)
		if err != nil {
			return Result{}, err
		}
		return Result{Decision: dec, AuditID: id}, &PolicyError{Operation: op.Name, Scope: op.Scope, Decision: dec, AuditID: id}
	}

	if verr != nil {
		ev.Duration = now().Sub(start)
		id, err := single(audit.ResultError, "invalid_input")
		if err != nil {
			return Result{}, err
		}
		return Result{Decision: dec, AuditID: id}, verr
	}

	// A9: Was die Eingabe anrichten würde (Zahl der Ziele, Zielordner),
	// prüft die Policy, nicht der Handler.
	if op.PolicyInput != nil {
		// Fail closed (T2 Punkt 7): Ohne Regeln der Policy-Datei entscheidet
		// nichts über die Eingabe; kein stiller Rückfall auf die Standards.
		if x.Rules == nil {
			if _, err := single(audit.ResultError, "policy_rules_missing"); err != nil {
				return Result{}, err
			}
			return Result{Decision: dec}, fmt.Errorf("ops: %s: the policy rules are not loaded", op.Name)
		}
		facts, ferr := x.callPolicyInput(ctx, op, req.Input)
		if ferr != nil {
			ev.Duration = now().Sub(start)
			reason := "policy_input"
			if errors.Is(ferr, ErrInternal) {
				reason = "panic"
			}
			id, err := single(audit.ResultError, reason)
			if err != nil {
				return Result{}, err
			}
			return Result{Decision: dec, AuditID: id}, ferr
		}
		if in := policy.CheckInput(req.Key, facts, *x.Rules); !in.Allowed() {
			d := policy.Decision{Outcome: in.Outcome, Reason: in.Reason, DryRun: dec.DryRun}
			id, err := single(audit.ResultDenied, in.Reason)
			if err != nil {
				return Result{}, err
			}
			return Result{Decision: d, AuditID: id}, &PolicyError{Operation: op.Name, Scope: op.Scope, Decision: d, AuditID: id}
		}
	}

	ev.Result = audit.ResultStarted
	auditID, err := x.appendAudit(ctx, ev)
	if err != nil {
		return Result{}, fmt.Errorf("ops: audit: %w", err)
	}

	out, herr := x.callHandler(ctx, op, Call{Input: req.Input, DryRun: dec.DryRun, Secret: req.Secret, Progress: req.Progress,
		Key: req.Key, Actor: req.Actor, AuditID: auditID})

	end := ev
	end.RefID = auditID
	end.Result = audit.ResultOK
	if request {
		end.Result = audit.ResultNeedsApproval
	}
	if herr != nil {
		end.Result, end.Reason = audit.ResultError, "handler_error"
		var de *DeniedError
		switch {
		case errors.Is(herr, ErrInternal):
			end.Reason = "panic"
		case errors.As(herr, &de):
			// Fachliche Ablehnung (S2.3): Ergebnis denied mit ihrem Grund.
			end.Result, end.Reason = audit.ResultDenied, de.Reason
		}
	}
	if herr == nil && op.AuditCounts != nil {
		end.ParamsRedacted = withCounts(end.ParamsRedacted, op.AuditCounts(out))
	}
	endAt := now()
	end.TS = endAt.UTC()
	end.Duration = endAt.Sub(start)
	// Der häufigste Handler-Fehler ist ein Abbruch (Strg+C): ctx ist dann
	// schon beendet. Der Abschluss läuft deshalb ohne dessen Abbruch, aber
	// mit eigener Frist. Ein Fehler hier verdeckt nie das Ergebnis.
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditGrace)
	if _, err := x.appendAudit(actx, end); err != nil && x.OnAuditError != nil {
		x.OnAuditError(op.Name, auditID, err)
	}
	cancel()
	if herr != nil {
		// Output trotz Fehler nur mit Opt-in der Operation (OutputOnError,
		// import.run: Bericht bis zum Abbruch); sonst nil, auch wenn der
		// Handler etwas liefert.
		res := Result{Decision: dec, AuditID: auditID}
		if op.OutputOnError {
			res.Output = out
		}
		return res, herr
	}
	return Result{Output: out, Decision: dec, AuditID: auditID}, nil
}

// withCounts ergänzt die Audit-Parameter (ein JSON-Objekt) um "counts"
// (Operation.AuditCounts). Sind die Parameter kein Objekt, bleiben sie.
func withCounts(params json.RawMessage, counts map[string]int64) json.RawMessage {
	if len(counts) == 0 {
		return params
	}
	obj := map[string]json.RawMessage{}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &obj); err != nil {
			return params
		}
	}
	c, err := json.Marshal(counts)
	if err != nil {
		return params
	}
	obj["counts"] = c
	b, err := json.Marshal(obj)
	if err != nil {
		return params
	}
	return b
}
