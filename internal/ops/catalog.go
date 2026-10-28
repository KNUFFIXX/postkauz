package ops

import (
	"log/slog"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/retention"
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
	// Drafts ist der Entwurfsdienst (S2.3); seine Datenbank führt auch
	// Identitäten und Freigaben. nil nur ohne Handler-Aufrufe (Hilfe).
	Drafts *drafts.Service
	// DraftBlobs liest und schreibt Anhänge und Rohmails der Entwürfe
	// (Blob-Store).
	DraftBlobs BlobStore
	// Draft: attach_dir, Grenzen, Zitatzeile, Freigebender (Konfiguration).
	Draft DraftConfig
	// DraftCheck ist der Prüf-Hook vor der Vorlage (S2.4); nil = „pass".
	DraftCheck DraftChecker
	// OAuth: Microsoft-Anmeldung für OAuth2-Konten (S3.7; Nullwert = echte
	// Endpunkte und Browser des Systems, Tests setzen einen Fake-AS).
	OAuth oauth.Microsoft
	// SystemAudit nimmt Systemereignisse außerhalb des Executors auf
	// (oauth.refresh); nil = keine.
	SystemAudit audit.Writer
	// Log nimmt Warnungen außerhalb des Executors auf (B-M2: Audit-Eintrag
	// oauth.refresh nicht geschrieben); nil = keine.
	Log *slog.Logger
	// Actions führt Postfach-Aktionen und Tags (T2; nil nur ohne
	// Handler-Aufrufe).
	Actions ActionStore
	// Policy ist die geladene Policy-Datei (policy.get zeigt Budget und
	// Regeln je Schlüssel; nil = Standards).
	Policy *policy.File
	// SyncKeywords: [tags] sync_keywords der Konfiguration (E5).
	SyncKeywords bool
	// KeyDryRunDefault ist [agent_defaults] dry_run_default der
	// Policy-Datei: Standard für keys.create ohne dry_run_default (T2,
	// E4; nil = true).
	KeyDryRunDefault *bool
	// TokenLockDir ist der Ordner der Sperrdateien beim Speichern eines
	// OAuth2-Tokens (neben der Datenbank; leer = ohne Sperre).
	TokenLockDir string
	// Retention ist das Löschkonzept (T3, E8: purge, blob gc; nil nur ohne
	// Handler-Aufrufe), RetentionConfig die Fristen aus der Konfiguration.
	Retention       *retention.Service
	RetentionConfig RetentionConfig
}

// Catalog ist die eine, ausdrücklich geschriebene Liste aller Operationen
// (ADR-004: keine Reflection, kein Generator). Oberflächen registrieren sie
// über NewDefaultRegistry; der Vertragstest (ops_test.go) schickt jede davon
// durch den Executor. Eine neue Operation gehört hierher.
func Catalog(d Deps) []Operation {
	return []Operation{
		AccountsList(d.Accounts),
		AccountsAdd(d.Accounts, d.Secrets, d.OAuth),
		AccountsSet(d.Accounts, d.Secrets),
		AccountsRemove(d.Accounts, d.Secrets),
		AccountsTest(d.Accounts, d.credentials(), d.Prober),
		AccountsLogin(d.Accounts, d.credentials()),
		SyncRun(d.Accounts, d.credentials(), d.Runner),
		SyncRequest(d),
		ImportRun(d.Accounts, d.Importer),
		MailboxesList(d.Accounts, d.Mailboxes),
		MailboxesOverview(d.Accounts, d.Mailboxes, d.Search),
		MessagesGet(d.Accounts, d.Messages, d.Blobs),
		MessagesSearch(d.Accounts, d.Search),
		ThreadsGet(d.Messages),
		AttachmentsList(d.Messages),
		AttachmentsText(d.Accounts, d.Messages, d.Blobs, d.Extractor),
		KeysCreate(d.Keys, d.KeyDryRunDefault),
		KeysList(d.Keys),
		KeysRevoke(d.Keys),
		AuditList(d.Audit),
		AuditExport(d.Audit, d.AuditExportDir),
		AuditVerify(d.Audit),
		PolicyGet(d.MaxChars, d.Policy),
		DraftsCreate(d),
		DraftsUpdate(d),
		DraftsGet(d),
		DraftsList(d),
		DraftsRequestReady(d),
		DraftsApprove(d),
		DraftsReject(d),
		DraftsAbandon(d),
		DraftsMarkSent(d),
		ApprovalsGet(d),
		DraftsStyle(d),
		IdentitiesAdd(d),
		IdentitiesList(d),
		IdentitiesSetDefault(d),
		IdentitiesRemove(d),
		MessagesTag(d),
		TagsList(d),
		MessagesMark(d),
		MessagesMove(d),
		MessagesTrash(d),
		ActionsApprove(d),
		ActionsReject(d),
		ActionsGet(d),
		ActionsList(d),
		DraftsCleanupCopies(d),
		RetentionPurge(d),
		BlobsGC(d),
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
