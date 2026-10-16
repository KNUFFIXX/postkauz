package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	"github.com/KNUFFIXX/postkauz/internal/ops"
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

// newSyncRunner ist in Tests ersetzbar.
var newSyncRunner = func(st *store.SQLite, b *blob.Store, log *slog.Logger, c config.IMAP) ops.SyncRunner {
	return &mailsync.Runner{Store: st, Blobs: b, Log: log, CommandTimeout: c.CommandTimeout, CommandMaxDuration: c.CommandMaxDuration}
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

	st, err := store.Open(a.paths.Database)
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		_ = st.Close()
		return nil, err
	}
	a.st = st
	sec, err := openSecrets(a.cfg.Secrets.Backend, a.paths.SecretsDir)
	if err != nil {
		return nil, err
	}
	a.sec = sec
	blobs, err := blob.Open(a.paths.Blobs, blob.Options{})
	if err != nil {
		return nil, err
	}
	reg, err := ops.NewDefaultRegistry(ops.Deps{Accounts: st, Secrets: sec, Prober: newProber(a.cfg.IMAP), Mailboxes: st,
		Messages: st, Blobs: blobs, Runner: newSyncRunner(st, blobs, a.log, a.cfg.IMAP), Search: st, Extractor: extract.New(extract.Options{}),
		Keys: st, Audit: st, AuditExportDir: a.cfg.Audit.ExportDir})
	if err != nil {
		return nil, err
	}
	a.exec = &ops.Executor{Registry: reg, Audit: st.AuditWriter()}
	return a.exec, nil
}

func (a *app) close() {
	if a.st != nil {
		_ = a.st.Close()
	}
	if a.logFile != nil {
		_ = a.logFile.Close()
	}
}
