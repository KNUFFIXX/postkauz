package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/importer"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// openSecrets ist in Tests ersetzbar (Schlüsselbund im Speicher).
var openSecrets = secrets.Open

// newProber ist in Tests ersetzbar.
var newProber = func(c config.IMAP) ops.AccountProber {
	return imapprov.Prober{CommandTimeout: c.CommandTimeout, CommandMaxDuration: c.CommandMaxDuration}
}

// newOAuth liefert die Microsoft-Anmeldung (echte Endpunkte, Browser des
// Systems); in Tests ersetzbar (Fake-AS).
var newOAuth = func() oauth.Microsoft { return oauth.Microsoft{} }

// newImporter ist in Tests ersetzbar.
var newImporter = func(st *store.SQLite, b *blob.Store, log *slog.Logger) ops.ImportRunner {
	return &importer.Runner{Store: st, Blobs: b, Log: log}
}

// newSyncRunner ist in Tests ersetzbar.
var newSyncRunner = func(st *store.SQLite, b *blob.Store, log *slog.Logger, c config.IMAP) ops.SyncRunner {
	return &mailsync.Runner{Store: st, Blobs: b, Log: log, CommandTimeout: c.CommandTimeout, CommandMaxDuration: c.CommandMaxDuration,
		MaxMessageBytes: c.MaxMessageBytes}
}

// newDraftChecker liefert den Prüf-Hook vor der Vorlage eines Entwurfs
// (A02 E4, S2.4): [draft] check_hook und check_hook_timeout der
// Policy-Datei; leere Liste = keiner (nil = „pass"). In Tests ersetzbar.
var newDraftChecker = func(a *app) ops.DraftChecker {
	return ops.NewHookChecker(a.policy.Doc.Draft.CheckHook, a.policy.Doc.Draft.CheckHookTimeout, a.log)
}

// app hält die Laufzeitumgebung einer CLI-Ausführung. Datenverzeichnis,
// Datenbank, Log und Schlüsselbund werden erst geöffnet, wenn ein Kommando
// sie braucht (version, doctor und --help legen nichts an).
type app struct {
	version string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer

	cfg    config.Config
	paths  config.Paths
	cfgErr error

	st      *store.SQLite
	sec     secrets.Store
	log     *slog.Logger
	logFile *os.File
	exec    *ops.Executor
	// policy ist die geladene Policy-Datei (A02 E8), policyVersion ihre
	// Version in der Tabelle policies; beides setzt open.
	policy        policy.File
	policyVersion int64
	// noCredentials: Der Prozess fasst keine Passwörter an (MCP-Server,
	// docs/sicherheit.md, Abschnitt 4); open zieht dann auch keine
	// Schlüsselbund-Einträge um.
	noCredentials bool
}

func newApp(version string, stdin io.Reader, stdout, stderr io.Writer) *app {
	a := &app{version: version, stdin: stdin, stdout: stdout, stderr: stderr,
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.cfg, a.paths, a.cfgErr = config.Load()
	return a
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// open legt Datenverzeichnis, Log, Datenbank (mit Migration) und
// Schlüsselbund an und baut den Executor mit SQLite-Audit.
func (a *app) open(ctx context.Context) (*ops.Executor, error) {
	if a.exec != nil {
		return a.exec, nil
	}
	if a.cfgErr != nil {
		return nil, a.cfgErr
	}
	if err := config.EnsureDataDir(a.paths); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(a.paths.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("log file: %w", err)
	}
	a.logFile = lf
	a.log = slog.New(slog.NewJSONHandler(lf, &slog.HandlerOptions{Level: parseLevel(a.cfg.LogLevel)}))
	// Panics in Hilfsgoroutinen (crash.Go/Do) bereinigt ins Log (A02 A5).
	crash.SetLogger(a.log)
	// Vorhandenes Datenverzeichnis und Log nur für den Nutzer (A02 A3);
	// scheitert das, läuft das Kommando trotzdem, mit Warnung im Log.
	for _, c := range config.RestrictPermissions(a.paths) {
		if c.Err != nil {
			a.log.Warn("could not restrict permissions", "path", c.Path, "error", c.Err.Error())
			continue
		}
		if c.Skip != "" {
			a.log.Warn("did not restrict permissions", "path", c.Path, "reason", c.Skip, "mode", config.OctalMode(c.Old))
			continue
		}
		a.log.Info("restricted permissions", "path", c.Path, "from", config.OctalMode(c.Old), "to", config.OctalMode(c.New))
	}

	st, err := store.Open(a.paths.Database)
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		_ = st.Close()
		return nil, err
	}
	a.st = st
	// Policy-Datei: laden, prüfen, Version eintragen (A02 E8). Eine
	// ungültige Datei hält jedes Kommando auf (fail closed).
	if err := a.loadPolicy(ctx); err != nil {
		return nil, err
	}
	sec, err := openSecrets(a.cfg.Secrets.Backend, a.paths.SecretsDir)
	if err != nil {
		return nil, err
	}
	a.sec = sec
	if !a.noCredentials {
		a.moveCredentialRefs(ctx)
	}
	blobs, err := blob.Open(a.paths.Blobs, blob.Options{})
	if err != nil {
		return nil, err
	}
	deps := ops.Deps{Accounts: st, Secrets: sec, Prober: newProber(a.cfg.IMAP), Mailboxes: st,
		Messages: st, Blobs: blobs, Runner: newSyncRunner(st, blobs, a.log, a.cfg.IMAP), Importer: newImporter(st, blobs, a.log), Search: st, Extractor: extract.New(extract.Options{}),
		Keys: st, Audit: st, AuditExportDir: a.cfg.Audit.ExportDir, MaxChars: a.policy.MaxChars(),
		Drafts:     &drafts.Service{Store: st, ApprovalTTL: a.cfg.Draft.ApprovalTTL, PolicyVersion: a.policyVersion},
		DraftBlobs: blobs,
		Draft: ops.DraftConfig{AttachDir: a.paths.AttachDir, MaxAttachmentBytes: a.cfg.Draft.MaxAttachmentBytes,
			QuoteLine: a.cfg.Draft.QuoteLine, Operator: a.cfg.Operator.Name},
		DraftCheck: newDraftChecker(a), OAuth: newOAuth(), SystemAudit: st.AuditWriter()}
	// Sperrdateien beim Speichern eines OAuth2-Tokens neben der Datenbank,
	// wie die Sperren je Konto (K2, zweite Nachbesserung T4).
	if p := st.Path(); p != "" {
		deps.TokenLockDir = filepath.Dir(p)
	}
	// draft_push (A02 E1, S2.5): Der Sync legt wartende Entwürfe im
	// Entwürfe-Ordner ab; nur er hat die Zugangsdaten (der MCP-Server ruft
	// sync.run nie auf).
	if r, ok := deps.Runner.(*mailsync.Runner); ok {
		r.Pusher, r.DraftMailbox = ops.NewDraftPusher(deps), a.cfg.Draft.Mailbox
	}
	reg, err := ops.NewDefaultRegistry(deps)
	if err != nil {
		return nil, err
	}
	rules := a.policy.Rules()
	a.exec = &ops.Executor{Registry: reg, Audit: st.AuditWriter(), Keys: &ops.KeyGuard{Repo: st}, PolicyVersion: a.policyVersion,
		Rules: &rules,
		OnPanic: func(op string, r crash.Report) {
			a.log.Error("panic in operation", "op", op, "message", r.Message, "stack", r.Stack)
		},
		// A1: Das Ergebnis geht trotzdem an den Aufrufer; doctor zählt den
		// Start ohne Abschluss. Nur Operation, ID und Fehlerart, kein Text
		// der Datenbank mit Eingaben.
		OnAuditError: func(op string, startID int64, err error) {
			a.log.Error("audit end entry not written", "op", op, "start_id", startID, "error_type", ops.ErrorKinds(err))
		}}
	return a.exec, nil
}

// loadPolicy lädt die Policy-Datei neben der Konfiguration (oder aus
// POSTKAUZ_POLICY) und trägt sie in die Tabelle policies ein.
func (a *app) loadPolicy(ctx context.Context) error {
	path, explicit := policy.FilePath(a.paths.ConfigFile)
	f, err := policy.LoadFile(path, explicit)
	if err != nil {
		return err
	}
	v, err := a.st.RecordPolicy(ctx, f.SHA256, f.Document(), time.Now())
	if err != nil {
		return fmt.Errorf("policy: %w", err)
	}
	a.policy, a.policyVersion = f, v
	a.log.Info("policy loaded", "path", f.Path, "exists", f.Exists, "version", v)
	return nil
}

// moveCredentialRefs zieht Schlüsselbund-Einträge auf den Verweis mit der
// Kennung des Datenverzeichnisses um (S2.0/A2). Ein Fehler hält kein
// Kommando auf: Der alte Verweis gilt dann weiter, der nächste Start
// versucht es erneut, und `doctor` warnt.
func (a *app) moveCredentialRefs(ctx context.Context) {
	moves, err := ops.MoveCredentialRefs(ctx, a.st, a.sec)
	if err != nil {
		a.log.Warn("keyring entries not moved", "error", err.Error())
		fmt.Fprintf(a.stderr, "warning: keyring entries not moved to the new names: %v\n", err)
		return
	}
	for _, m := range moves {
		args := []any{"account", m.Account, "from", m.From, "to", m.To, "result", m.Result}
		if m.Err != nil {
			args = append(args, "error", m.Err.Error())
		}
		a.log.Info("keyring entry moved", args...)
		switch m.Result {
		case "failed":
			fmt.Fprintf(a.stderr, "warning: keyring entry %s of account %s not moved to %s (%v); the old name stays in use, the next start tries again\n",
				m.From, m.Account, m.To, m.Err)
		case "moved_old_left":
			fmt.Fprintf(a.stderr, "warning: keyring entry of account %s moved to %s, but the old entry %s could not be deleted (%v); delete it yourself (see: %s doctor)\n",
				m.Account, m.To, m.From, m.Err, brand.Name)
		}
	}
}

func (a *app) close() {
	if a.st != nil {
		_ = a.st.Close()
	}
	if a.logFile != nil {
		_ = a.logFile.Close()
	}
}
