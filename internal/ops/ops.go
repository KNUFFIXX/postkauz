// Package ops enthält das Operationsregister: die einzige Stelle, an der
// fachliche Operationen definiert werden. CLI, MCP und später REST sind
// dünne, explizit geschriebene Adapter darüber (ADR-004). Es gibt keine
// Reflection und keinen Generator: Jede Oberfläche bindet Operationen per
// Namen und ruft Executor.Execute auf.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Call ist, was ein Handler vom Aufruf mitbekommt.
type Call struct {
	// Input ist das JSON-Objekt der Eingabe (entspricht InputSchema).
	Input json.RawMessage
	// DryRun ist true, wenn der Handler nichts verändern darf, sondern nur
	// beschreiben soll, was geschähe. Entschieden von der Policy.
	DryRun bool
	// Secret ist ein geheimer Wert außerhalb von Input (z. B. das Passwort
	// bei accounts.add). Er wird nie auditiert, nie geloggt und nie
	// ausgegeben; nur Operationen, die ihn brauchen, lesen ihn.
	Secret secrets.Secret
	// Progress empfängt Fortschrittsmeldungen langer Operationen (Sync).
	// Darf nil sein; Handler rufen es über Call.progress auf.
	Progress func(Progress)
	// Key ist der Schlüssel, unter dem der Aufruf läuft (geprüft vom
	// Executor). Handler dürfen ihn nur beschreiben (policy.get), nie für
	// eigene Rechteentscheidungen nutzen: Die trifft allein die Policy.
	Key policy.Key
	// AuditID ist der Start-Eintrag dieses Aufrufs im Audit (0, wenn der
	// Writer keine IDs kennt); Handler, die Jobs einreihen, tragen ihn als
	// Herkunft ein (A8).
	AuditID int64
	// Actor ist der Akteur des Aufrufs (wie im Audit-Eintrag des
	// Executors); Handler, die selbst Übergänge auditieren (Entwürfe,
	// S2.3), tragen ihn in deren Einträge ein.
	Actor audit.ActorType
}

// DeniedError ist eine fachliche Ablehnung im Handler (A02: z. B.
// draft_closed, approval_stale, attach_outside_dir): kein technischer
// Fehler, sondern ein Ergebnis mit Grund. errors.Is(err, ErrDenied) gilt
// (CLI Exit 3, MCP status denied); der Executor schreibt sie als Ergebnis
// denied mit diesem Grund ins Audit. Msg ist englisch und nennt nie
// Mailinhalt (Betreff, Adressen, Text), nur IDs, Feldnamen und Zahlen.
type DeniedError struct {
	Reason string
	Msg    string
}

func (e *DeniedError) Error() string {
	if e.Msg == "" {
		return "denied (" + e.Reason + ")"
	}
	return "denied (" + e.Reason + "): " + e.Msg
}

// Is macht die Ablehnung zu ErrDenied.
func (e *DeniedError) Is(target error) bool { return target == ErrDenied }

func deniedErr(reason, format string, args ...any) error {
	return &DeniedError{Reason: reason, Msg: fmt.Sprintf(format, args...)}
}

// Progress ist eine Fortschrittsmeldung einer langen Operation. Sie enthält
// nie Mailinhalte, nur Ordnernamen und Zähler.
type Progress struct {
	Event   string `json:"event"` // mailbox_start | window | mailbox_done | round | error | info | import_* | sync_request | account
	Mailbox string `json:"mailbox,omitempty"`
	Done    int    `json:"done,omitempty"`
	Total   int    `json:"total,omitempty"`
	Message string `json:"message,omitempty"`
	// Source und Unit: Quelle eines Imports (Pfad relativ zum angegebenen
	// Ordner) und Einheit von Done/Total (bytes | files; S1.10).
	Source string `json:"source,omitempty"`
	Unit   string `json:"unit,omitempty"`
	// Round ist das Ergebnis eines Laufs im Dauerbetrieb (sync --watch).
	Round *SyncOutput `json:"round,omitempty"`
	// Account: das Konto der Meldung bei sync --watch --all (T2; sonst
	// leer, es gibt nur eines).
	Account string `json:"account,omitempty"`
}

func (c Call) progress(p Progress) {
	if c.Progress != nil {
		c.Progress(p)
	}
}

// RedactFunc leitet aus der Eingabe die Audit-Parameter ab: nur IDs,
// Feldnamen, Zähler und Hashes, nie Inhalte oder Geheimnisse
// (docs/sicherheit.md, Abschnitt 6). targets sind Objektbezeichner wie
// "account:demo".
type RedactFunc func(input json.RawMessage) (params json.RawMessage, targets []string)

// Handler führt eine Operation aus. Der Rückgabewert muss sich nach JSON
// serialisieren lassen und OutputSchema entsprechen.
type Handler func(ctx context.Context, call Call) (any, error)

// Operation beschreibt eine Operation vollständig als Daten.
type Operation struct {
	// Name hat die Form "objekt.verb", z. B. "accounts.list", "messages.search".
	Name string
	// Scope, den der Aufrufer besitzen muss.
	Scope policy.Scope
	// Risk ist die Risikostufe R0–R3.
	Risk policy.Risk
	// Idempotent: wiederholter Aufruf mit gleicher Eingabe ändert nichts weiter.
	Idempotent bool
	// InputSchema und OutputSchema sind JSON-Schema-Dokumente (Objekt).
	// Sie sind die Quelle für CLI-Hilfe, MCP-Tool-Definition und OpenAPI.
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	// Summary ist ein Satz für Hilfetexte und Tool-Beschreibungen.
	Summary string
	Handler Handler
	// Redact ist optional; ohne sie stehen im Audit leere Parameter ({}).
	Redact RedactFunc
	// HumanOnly: nur der Mensch ohne API-Schlüssel darf die Operation
	// ausführen (A02, E2). Mit Schlüssel, gleich welcher Rolle und auch über
	// die CLI-Probe --key, lehnt die Policy mit Grund human_only ab; der
	// Handler läuft nie, die Ablehnung steht im Audit. Solche Operationen
	// haben kein MCP-Tool (notOverMCP).
	HumanOnly bool
	// OutputOnError: Der Executor reicht die Ausgabe des Handlers auch bei
	// einem Fehler weiter (Result.Output), etwa den Bericht bis zu einem
	// Abbruch (import.run). Ohne diese Erklärung ist Output bei Fehler nil,
	// auch wenn der Handler etwas liefert.
	OutputOnError bool
	// PolicyInput leitet aus der geprüften Eingabe ab, was die Policy über
	// sie wissen muss (A9: Zahl der Ziele, Rolle des Zielordners, Konto,
	// confirm_count). Der Executor ruft es nach der Schemaprüfung und vor
	// dem Start-Eintrag; policy.CheckInput entscheidet. nil = die Eingabe
	// spielt für die Policy keine Rolle. Jede Operation damit braucht einen
	// Policy-Testfall (TestContractPolicyInputCases).
	PolicyInput func(ctx context.Context, input json.RawMessage) (policy.Facts, error)
	// ApprovalRequest: Der Handler führt nichts aus, sondern legt den
	// Antrag auf Freigabe selbst an (R3, E3: messages.trash). Bei
	// needs_approval läuft er deshalb (außer im Trockenlauf beschreibt er
	// nur); die Wirkung gibt erst eine Freigabe des Menschen frei. Der
	// Abschluss-Eintrag im Audit lautet needs_approval.
	ApprovalRequest bool

	// input ist das aufgelöste InputSchema (Registry.Register setzt es).
	input *jsonschema.Resolved
}

// Requirement übersetzt die Operation in das, was policy.Check braucht.
func (o Operation) Requirement() policy.Requirement {
	return policy.Requirement{Name: o.Name, Scope: o.Scope, Risk: o.Risk, HumanOnly: o.HumanOnly}
}

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

// Validate prüft die formale Vollständigkeit einer Operation.
func (o Operation) Validate() error {
	switch {
	case !nameRE.MatchString(o.Name):
		return fmt.Errorf("ops: invalid name %q (expected object.verb)", o.Name)
	case o.Scope == "":
		return fmt.Errorf("ops: %s: scope missing", o.Name)
	case o.Risk < policy.R0 || o.Risk > policy.R3:
		return fmt.Errorf("ops: %s: risk level outside R0-R3", o.Name)
	case o.Handler == nil:
		return fmt.Errorf("ops: %s: handler missing", o.Name)
	case o.Summary == "":
		return fmt.Errorf("ops: %s: summary missing", o.Name)
	}
	for label, s := range map[string]json.RawMessage{"InputSchema": o.InputSchema, "OutputSchema": o.OutputSchema} {
		var obj map[string]any
		if err := json.Unmarshal(s, &obj); err != nil || obj["type"] != "object" {
			return fmt.Errorf("ops: %s: %s must be a JSON schema with type=object", o.Name, label)
		}
		if _, err := resolveSchema(s); err != nil {
			return fmt.Errorf("ops: %s: %s: %v", o.Name, label, err)
		}
	}
	return nil
}

// resolveSchema liest ein JSON-Schema und bereitet es für die Prüfung vor.
func resolveSchema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return s.Resolve(nil)
}

// validateInput prüft die Eingabe gegen das InputSchema (ungültig:
// InputError, CLI Exit 2). Ohne aufgelöstes Schema (Operation nicht über
// Register) wird es hier aufgelöst.
func (o Operation) validateInput(raw json.RawMessage) error {
	rs := o.input
	if rs == nil {
		var err error
		if rs, err = resolveSchema(o.InputSchema); err != nil {
			return err
		}
	}
	return validateAgainst(rs, raw)
}

// ResolveSchema bereitet ein JSON-Schema für Request.Narrow vor.
func ResolveSchema(raw json.RawMessage) (*jsonschema.Resolved, error) { return resolveSchema(raw) }

func validateAgainst(rs *jsonschema.Resolved, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		// Die Meldung von encoding/json zitiert Teile der Eingabe.
		return inputErr("invalid input: not a valid JSON object")
	}
	if err := rs.Validate(inst); err != nil {
		return schemaError(rs, err) // Feld und Regel, nie der Wert (B1)
	}
	return nil
}

// Registry ist die Tabelle aller Operationen.
type Registry struct {
	mu  sync.RWMutex
	ops map[string]Operation
}

// NewRegistry legt ein leeres Register an.
func NewRegistry() *Registry { return &Registry{ops: map[string]Operation{}} }

// ErrUnknownOperation wird von Get und Execute geliefert.
var ErrUnknownOperation = errors.New("ops: unknown operation")

// Register nimmt eine Operation auf. Doppelte Namen und formal unvollständige
// Operationen werden abgelehnt.
func (r *Registry) Register(op Operation) error {
	if err := op.Validate(); err != nil {
		return err
	}
	rs, err := resolveSchema(op.InputSchema)
	if err != nil {
		return err
	}
	op.input = rs
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.ops[op.Name]; dup {
		return fmt.Errorf("ops: %s is already registered", op.Name)
	}
	r.ops[op.Name] = op
	return nil
}

// Get liefert eine Operation per Name.
func (r *Registry) Get(name string) (Operation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	op, ok := r.ops[name]
	if !ok {
		return Operation{}, fmt.Errorf("%w: %s", ErrUnknownOperation, name)
	}
	return op, nil
}

// List liefert alle Operationen, nach Namen sortiert.
func (r *Registry) List() []Operation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Operation, 0, len(r.ops))
	for _, op := range r.ops {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SchemaVersion zählt inkompatible Änderungen an Ein-/Ausgabeschemas des
// Registers. CLI (`version --json`), MCP und später REST melden sie, damit
// Skripte und Agenten Brüche erkennen. Während Phase 0 und Pre-Alpha bleibt sie 0.
const SchemaVersion = 0
