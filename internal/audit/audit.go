// Package audit definiert Ereignistyp und Schreibschnittstelle des
// Audit-Logs. Das Log ist append-only: Es gibt absichtlich kein Update und
// kein Delete in der Schnittstelle (die SQLite-Tabelle sperrt beides per
// Trigger, siehe store/migrations/0001_init.sql).
//
// Hier liegen Typ, Schnittstelle und zwei Test-Implementierungen; der
// SQLite-Writer liegt in internal/store (S1.1). Der tägliche Export mit
// Prüfsumme folgt mit S1.11.
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
}

// Writer schreibt Ereignisse. Implementierungen dürfen nur anhängen.
type Writer interface {
	Append(ctx context.Context, e Event) error
}

// Nop verwirft alle Ereignisse. Nur für Tests und Werkzeuge ohne Datenbank.
type Nop struct{}

func (Nop) Append(context.Context, Event) error { return nil }

// Memory hält Ereignisse im Speicher (Tests).
type Memory struct {
	mu     sync.Mutex
	events []Event
}

func (m *Memory) Append(_ context.Context, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

// Events liefert eine Kopie der bisher geschriebenen Ereignisse.
func (m *Memory) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event(nil), m.events...)
}
