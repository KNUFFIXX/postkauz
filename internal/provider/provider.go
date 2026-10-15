// Package provider definiert die Schnittstelle zu Mailservern. Sie hat
// JMAP-Semantik (Zustandstokens, changes, Rollen) und wird für IMAP
// nachgebildet (ADR-003, docs/architektur.md).
//
// Stand S1.5: Die IMAP-Implementierung (internal/provider/imap) deckt
// Capabilities, ListMailboxes, Changes, FetchHeaders, FetchRaw und Idle ab;
// SetFlags, Move, Trash und Append folgen mit S2/S3.
//
// Verbindliche Eigenschaften:
//
//   - Der Server ist die Wahrheit für den aktuellen Postfachzustand. Ein
//     Provider liest ihn und schreibt nur die wenigen erlaubten Änderungen.
//   - Es gibt absichtlich KEIN Expunge und kein endgültiges Löschen. Löschen
//     heißt Verschieben in den Papierkorb (Trash).
//   - Es gibt im MVP keinen Versand (Submission, siehe submission.go).
//   - Alle Methoden sind kontextgesteuert und dürfen blockieren; Abbruch nur
//     über ctx.
package provider

import (
	"context"
	"errors"
	"io"
	"time"
)

// Fehler, die Aufrufer unterscheiden müssen.
var (
	// ErrUIDValidityChanged: Die UIDs des Ordners sind ungültig geworden
	// (IMAP UIDVALIDITY-Wechsel). Der Aufrufer muss den Ordner neu abgleichen
	// (Message-ID/Hash), nicht blind neu laden.
	ErrUIDValidityChanged = errors.New("provider: UIDVALIDITY changed")
	// ErrUnsupported: Die Fähigkeit fehlt (siehe Capabilities).
	ErrUnsupported = errors.New("provider: not supported by the server")
	// ErrNotFound: Ordner oder Nachricht existiert nicht (mehr).
	ErrNotFound = errors.New("provider: not found")
	// ErrAuth: Anmeldung fehlgeschlagen; kein automatischer Wiederholungsversuch.
	ErrAuth = errors.New("provider: authentication failed")
	// ErrConnection: Server nicht erreichbar, TLS-Fehler oder Zeitüberschreitung
	// (Watchdog). Wiederholbar mit Backoff; CLI-Exit-Code 5.
	ErrConnection = errors.New("provider: connection failed")
)

// Capabilities beschreibt, was der Server kann. Sie werden beim Verbinden
// erkannt, im Konto gespeichert (accounts.capabilities) und bestimmen die
// Sync-Strategie. Unbekannt = false.
type Capabilities struct {
	// Protokoll
	IMAP4rev2 bool // IMAP4rev2 (RFC 9051)
	JMAP      bool // JMAP Mail (RFC 8621); für spätere Adapter

	// Änderungserkennung
	CondStore bool // RFC 7162: MODSEQ, CHANGEDSINCE
	QResync   bool // RFC 7162: VANISHED
	UIDPlus   bool // RFC 4315: APPENDUID, UID EXPUNGE

	// Bedienung
	Idle       bool // RFC 2177
	Move       bool // RFC 6851: atomares MOVE (sonst COPY + Flag + Expunge-Ersatz)
	SpecialUse bool // RFC 6154: Ordnerrollen vom Server
	Namespace  bool // RFC 2342

	// Eigenheiten
	KeywordsPermanent bool // PERMANENTFLAGS enthält \* : freie Keywords erlaubt
	GmailExtensions   bool // X-GM-EXT-1: Labels, Alle-Nachrichten-Dubletten
	UTF8Accept        bool // RFC 6855
}

// Strategy ist das Verfahren, mit dem ein Ordner inkrementell abgeglichen wird.
type Strategy string

const (
	// StrategyQResync: Änderungen und Löschungen kommen vollständig vom Server.
	StrategyQResync Strategy = "qresync"
	// StrategyCondStore: Neue UIDs und Flag-Änderungen per MODSEQ; Löschungen
	// per periodischem UID-Abgleich.
	StrategyCondStore Strategy = "condstore"
	// StrategyUIDScan: Neue UIDs ab UIDNEXT; Flags und Löschungen per Fenster-Scan.
	StrategyUIDScan Strategy = "uid-scan"
)

// SyncStrategy wählt die beste verfügbare Strategie.
func (c Capabilities) SyncStrategy() Strategy {
	switch {
	case c.QResync && c.CondStore:
		return StrategyQResync
	case c.CondStore:
		return StrategyCondStore
	default:
		return StrategyUIDScan
	}
}

// Role ist die fachliche Rolle eines Ordners (JMAP Mailbox.role).
type Role string

const (
	RoleInbox   Role = "inbox"
	RoleSent    Role = "sent"
	RoleDrafts  Role = "drafts"
	RoleTrash   Role = "trash"
	RoleJunk    Role = "junk"
	RoleArchive Role = "archive"
	// RoleAll: Ordner mit allen Nachrichten (Gmail „Alle Nachrichten",
	// SPECIAL-USE \All). Jede Nachricht liegt dort ein weiteres Mal; der
	// Sync dedupliziert über Message-ID (docs/architektur.md).
	RoleAll    Role = "all"
	RoleCustom Role = "custom"
)

// UID ist die Nachrichtennummer innerhalb eines Ordners (IMAP UID).
type UID uint32

// Mailbox ist ein Ordner auf dem Server.
type Mailbox struct {
	Path string // Pfad in UTF-8 (IMAP-UTF-7 bereits dekodiert)
	Name string
	Role Role // aus SPECIAL-USE, sonst Namensheuristik je Anbieter
	// Zustand, falls beim Auflisten bekannt.
	State SyncState
}

// SyncState ist der Abgleichszustand eines Ordners (bei JMAP: ein Token).
type SyncState struct {
	UIDValidity   uint32 // IMAP
	UIDNext       uint32 // IMAP
	HighestModSeq uint64 // IMAP mit CONDSTORE; 0 = unbekannt
	Token         string // JMAP "state"; bei IMAP leer
}

// Flags sind die Systemflags einer Nachricht plus freie Keywords.
type Flags struct {
	Seen, Flagged, Answered, Draft bool
	Keywords                       []string
}

// FlagPatch ändert Flags; nil bedeutet „nicht anfassen".
type FlagPatch struct {
	Seen, Flagged, Answered, Draft *bool
	AddKeywords, RemoveKeywords    []string
}

// Header ist die Kopfsicht einer Nachricht, wie sie der Sync braucht.
type Header struct {
	UID          UID
	ModSeq       uint64
	Flags        Flags
	InternalDate time.Time
	Size         int64
	// MessageID ist die Message-ID ohne spitze Klammern ("" = keine oder
	// unlesbar). Der Sync gleicht damit vor dem Laden des Inhalts ab
	// (Dubletten, Verschieben, UIDVALIDITY-Wechsel).
	MessageID string
	// Roh-Kopfzeilen (IMAP: BODY.PEEK[HEADER]); geparst wird
	// im Store-nahen Code (einheitlich für IMAP, Import und Tests).
	RawHeader []byte
}

// FlagUpdate meldet geänderte Flags einer bekannten Nachricht.
type FlagUpdate struct {
	UID    UID
	ModSeq uint64
	Flags  Flags
}

// ChangeSet ist das Ergebnis von Changes (JMAP: Email/changes).
type ChangeSet struct {
	// Reset ist true bei UIDVALIDITY-Wechsel: Alle früheren UIDs des Ordners
	// sind ungültig; Created enthält dann den Ordnerinhalt (in Fenstern).
	Reset   bool
	Created []UID
	Updated []FlagUpdate
	Removed []UID
	// Present ist, falls ermittelt, die vollständige UID-Liste des Ordners
	// (IMAP: UID SEARCH ALL). Der Aufrufer leitet daraus Löschungen ab
	// (lokal bekannt, aber nicht mehr vorhanden) und holt Fehlendes nach.
	// nil heißt: nicht ermittelt (nichts deutet auf Löschungen hin).
	Present []UID
	// Exists ist die Nachrichtenzahl des Ordners laut Server.
	Exists uint32
	// NewState ist der Zustand, ab dem der nächste Aufruf weitermacht. Er wird
	// erst NACH erfolgreicher lokaler Verarbeitung gespeichert.
	NewState SyncState
	// More ist true, wenn das Fenster voll war und ein weiterer Aufruf folgt
	// (Erstsync in Fenstern, wiederaufnehmbar).
	More bool
}

// ChangesOptions steuert Changes.
type ChangesOptions struct {
	// Limit begrenzt die neuen Nachrichten je Aufruf (Fenster); 0 =
	// Standard des Providers.
	Limit int
	// KnownCount ist die Zahl der lokal bekannten, vorhandenen Nachrichten
	// des Ordners. Stimmt sie mit dem Server überein (plus neue), entfällt
	// der teure Löschabgleich.
	KnownCount int
	// FullScan erzwingt Löschabgleich und Flag-Abgleich aller Nachrichten
	// (ohne CHANGEDSINCE), z. B. für `sync --full`.
	FullScan bool
}

// MailProvider ist die Schnittstelle zu einem Postfach.
type MailProvider interface {
	// Capabilities liefert die beim Verbinden erkannten Fähigkeiten.
	Capabilities() Capabilities

	// ListMailboxes liefert alle Ordner mit Rolle.
	ListMailboxes(ctx context.Context) ([]Mailbox, error)

	// Changes liefert die Änderungen eines Ordners seit since. Ein leerer
	// SyncState verlangt den Erstabgleich; Limit begrenzt das Fenster.
	//
	// JMAP: Email/changes. IMAP: UIDVALIDITY-Wechsel setzt Reset; neue UIDs ab
	// UIDNEXT; Flags per FETCH CHANGEDSINCE (CONDSTORE) sonst Fenster-Scan;
	// Löschungen per VANISHED (QRESYNC) sonst UID-SEARCH-Abgleich (Present).
	Changes(ctx context.Context, mailbox string, since SyncState, opts ChangesOptions) (ChangeSet, error)

	// FetchHeaders liefert die Köpfe der angegebenen UIDs. uidValidity ist
	// der Stand, unter dem die UIDs gelten; weicht der Server ab, kommt
	// ErrUIDValidityChanged (nichts wird geliefert).
	FetchHeaders(ctx context.Context, mailbox string, uidValidity uint32, uids []UID) ([]Header, error)

	// FetchRaw streamt die vollständigen Rohmails (RFC 5322) der UIDs und
	// ruft fn je Nachricht auf; der Reader gilt nur während fn. Nach einem
	// Verbindungsabbruch kann fn für dieselbe UID erneut kommen (idempotent
	// verarbeiten). uidValidity wie bei FetchHeaders.
	FetchRaw(ctx context.Context, mailbox string, uidValidity uint32, uids []UID, fn func(UID, io.Reader) error) error

	// SetFlags ändert Flags und Keywords. Nur Nachrichten, die der Aufrufer
	// zuvor gelesen hat (Policy-Regel „Ziele aus Leseergebnissen").
	SetFlags(ctx context.Context, mailbox string, uids []UID, patch FlagPatch) error

	// Move verschiebt Nachrichten (IMAP MOVE oder COPY-Ersatz) und liefert die
	// neuen UIDs im Zielordner, soweit der Server sie nennt (UIDPLUS).
	Move(ctx context.Context, from string, uids []UID, to string) (map[UID]UID, error)

	// Trash verschiebt Nachrichten in den Papierkorb. Es gibt kein
	// endgültiges Löschen in dieser Schnittstelle. Bei Gmail bedeutet das
	// „in [Gmail]/Papierkorb verschieben", nicht „Label entfernen".
	Trash(ctx context.Context, mailbox string, uids []UID) error

	// Append legt eine Rohmail in einem Ordner ab (Entwürfe!). Liefert die
	// neue UID, falls der Server sie nennt (UIDPLUS), sonst 0.
	Append(ctx context.Context, mailbox string, raw []byte, flags Flags, date time.Time) (UID, error)

	// Idle blockiert und sendet auf notify, sobald sich der Ordner ändert,
	// bis ctx endet (Rückgabe nil) oder die Verbindung trotz Wiederholung
	// scheitert (Rückgabe des Fehlers; der Aufrufer startet später neu).
	// Nur aufrufen, wenn Capabilities().Idle gilt; sonst ErrUnsupported
	// (der Aufrufer fällt auf Polling zurück).
	Idle(ctx context.Context, mailbox string, notify chan<- struct{}) error

	// Close beendet die Verbindung.
	Close() error
}
