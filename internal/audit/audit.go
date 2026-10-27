// Package audit definiert Ereignistyp und Schreibschnittstelle des
// Audit-Logs. Das Log ist append-only: Es gibt absichtlich kein Update und
// kein Delete in der Schnittstelle (die SQLite-Tabelle sperrt beides per
// Trigger, siehe store/migrations/0001_init.sql).
//
// Hier liegen Typ, Schnittstelle und zwei Test-Implementierungen; der
// SQLite-Writer liegt in internal/store (S1.1), Abfrage, Export mit
// Prüfsumme und Prüfung (audit.list, audit.export, audit.verify) in
// internal/ops (S1.8).
package audit

import (
	"context"
	"sync"
	"time"
)

// ActorType unterscheidet, wer eine Aktion ausgelöst hat.
type ActorType string

const (
	ActorHuman  ActorType = "human"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

// Result ist das Ergebnis der Policy-Kette für diesen Aufruf.
type Result string

const (
	// ResultStarted steht am Eintrag vor dem Handler (seit Migration 0018,
	// A1 Variante A); das Endergebnis trägt der Abschluss-Eintrag mit RefID.
	ResultStarted       Result = "started"
	ResultOK            Result = "ok"
	ResultDenied        Result = "denied"
	ResultNeedsApproval Result = "needs_approval"
	ResultError         Result = "error"
)

// Event ist ein Audit-Eintrag. Es enthält nie Mailinhalte und nie
// Geheimnisse: Parameter nur als IDs, Feldnamen, Zähler und Hashes.
type Event struct {
	TS        time.Time
	ActorType ActorType
	KeyID     string
	Scope     string
	Operation string
	// ParamsRedacted ist ein JSON-Objekt ohne Inhalte.
	ParamsRedacted []byte
	// Targets sind lokale Objekt-IDs (Nachrichten, Entwürfe, ...).
	Targets  []string
	Result   Result
	Reason   string
	DryRun   bool
	Duration time.Duration
	// PolicyVersion ist die Version der Richtlinie (policy.toml), unter der
	// die Entscheidung fiel (A02, E8; Tabelle policies). 0 = unbekannt
	// (Einträge vor S2.1, Werkzeuge ohne Richtlinie).
	PolicyVersion int64
	// RefID verweist im Abschluss-Eintrag auf den Start-Eintrag desselben
	// Aufrufs (seit Migration 0018); 0 an Start-, Policy- und
	// Systemeinträgen.
	RefID int64
}

// Writer schreibt Ereignisse. Implementierungen dürfen nur anhängen.
type Writer interface {
	Append(ctx context.Context, e Event) error
}

// IDWriter ist ein Writer, der die ID des geschriebenen Eintrags kennt.
// Der Executor meldet sie (CLI --explain, MCP-Ergebnisse), damit sich ein
// Aufruf im Audit wiederfinden lässt.
type IDWriter interface {
	Writer
	AppendID(ctx context.Context, e Event) (int64, error)
}

// Nop verwirft alle Ereignisse. Nur für Tests und Werkzeuge ohne Datenbank.
type Nop struct{}

func (Nop) Append(context.Context, Event) error { return nil }

// Memory hält Ereignisse im Speicher (Tests).
type Memory struct {
	mu     sync.Mutex
	events []Event
}

func (m *Memory) Append(ctx context.Context, e Event) error {
	_, err := m.AppendID(ctx, e)
	return err
}

// AppendID hängt an und liefert die laufende Nummer (ab 1).
func (m *Memory) AppendID(_ context.Context, e Event) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return int64(len(m.events)), nil
}

// Events liefert eine Kopie der bisher geschriebenen Ereignisse.
func (m *Memory) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event(nil), m.events...)
}
