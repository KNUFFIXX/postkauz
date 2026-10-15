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
}

// Progress ist eine Fortschrittsmeldung einer langen Operation. Sie enthält
// nie Mailinhalte, nur Ordnernamen und Zähler.
type Progress struct {
	Event   string `json:"event"` // mailbox_start | window | mailbox_done | round | error | info
	Mailbox string `json:"mailbox,omitempty"`
	Done    int    `json:"done,omitempty"`
	Total   int    `json:"total,omitempty"`
	Message string `json:"message,omitempty"`
	// Round ist das Ergebnis eines Laufs im Dauerbetrieb (sync --watch).
	Round *SyncOutput `json:"round,omitempty"`
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
}

// Requirement übersetzt die Operation in das, was policy.Check braucht.
func (o Operation) Requirement() policy.Requirement {
	return policy.Requirement{Name: o.Name, Scope: o.Scope, Risk: o.Risk}
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
