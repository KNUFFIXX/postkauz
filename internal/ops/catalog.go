package ops

import (
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Deps sind die Abhängigkeiten aller Operationen. Ohne Abhängigkeiten (für
// Hilfe und Bindungsprüfung) entsteht dieselbe Tabelle; Handler laufen dann nie.
type Deps struct {
	Accounts  AccountRepo
	Secrets   secrets.Store
	Prober    AccountProber
	Mailboxes MailboxRepo
	Messages  MessageRepo
	Blobs     BlobReader
	Runner    SyncRunner
	// Extractor extrahiert Anhangstext bei Bedarf (attachments.text);
	// nil = nur gespeicherte Ergebnisse zeigen.
	Extractor *extract.Extractor
}

// Catalog ist die eine, ausdrücklich geschriebene Liste aller Operationen
// (ADR-004: keine Reflection, kein Generator). Oberflächen registrieren sie
// über NewDefaultRegistry; der Vertragstest (ops_test.go) schickt jede davon
// durch den Executor. Eine neue Operation gehört hierher.
func Catalog(d Deps) []Operation {
	return []Operation{
		AccountsList(d.Accounts),
		AccountsAdd(d.Accounts, d.Secrets),
		AccountsRemove(d.Accounts, d.Secrets),
		AccountsTest(d.Accounts, d.Secrets, d.Prober),
		SyncRun(d.Accounts, d.Secrets, d.Runner),
		MailboxesList(d.Accounts, d.Mailboxes),
		MessagesGet(d.Accounts, d.Messages, d.Blobs),
		ThreadsGet(d.Messages),
		AttachmentsList(d.Messages),
		AttachmentsText(d.Messages, d.Blobs, d.Extractor),
	}
}

// NewDefaultRegistry registriert alle Operationen des Katalogs.
func NewDefaultRegistry(d Deps) (*Registry, error) {
	reg := NewRegistry()
	for _, op := range Catalog(d) {
		if err := reg.Register(op); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
