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
	// Importer liest mbox/Maildir ein (import.run, S1.10).
	Importer ImportRunner
	Search   SearchRepo
	// Extractor extrahiert Anhangstext bei Bedarf (attachments.text);
	// nil = nur gespeicherte Ergebnisse zeigen.
	Extractor *extract.Extractor
	Keys      KeyRepo
	Audit     AuditRepo
	// AuditExportDir ist das Standardziel von audit.export ([audit]
	// export_dir in der Konfiguration; leer = Ziel muss angegeben werden).
	AuditExportDir string
	// MaxChars ist das Zeichenbudget für Agenten aus der Policy-Datei
	// ([agent_defaults] max_chars; 0 = DefaultMaxChars). policy.get nennt es.
	MaxChars int
}

// Catalog ist die eine, ausdrücklich geschriebene Liste aller Operationen
// (ADR-004: keine Reflection, kein Generator). Oberflächen registrieren sie
// über NewDefaultRegistry; der Vertragstest (ops_test.go) schickt jede davon
// durch den Executor. Eine neue Operation gehört hierher.
func Catalog(d Deps) []Operation {
	return []Operation{
		AccountsList(d.Accounts),
		AccountsAdd(d.Accounts, d.Secrets),
		AccountsSet(d.Accounts, d.Secrets),
		AccountsRemove(d.Accounts, d.Secrets),
		AccountsTest(d.Accounts, d.Secrets, d.Prober),
		SyncRun(d.Accounts, d.Secrets, d.Runner),
		ImportRun(d.Accounts, d.Importer),
		MailboxesList(d.Accounts, d.Mailboxes),
		MailboxesOverview(d.Accounts, d.Mailboxes, d.Search),
		MessagesGet(d.Accounts, d.Messages, d.Blobs),
		MessagesSearch(d.Accounts, d.Search),
		ThreadsGet(d.Messages),
		AttachmentsList(d.Messages),
		AttachmentsText(d.Accounts, d.Messages, d.Blobs, d.Extractor),
		KeysCreate(d.Keys),
		KeysList(d.Keys),
		KeysRevoke(d.Keys),
		AuditList(d.Audit),
		AuditExport(d.Audit, d.AuditExportDir),
		AuditVerify(d.Audit),
		PolicyGet(d.MaxChars),
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
