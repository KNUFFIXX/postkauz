package mailsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Runner implementiert ops.SyncRunner: Er verbindet das Konto (IMAP),
// baut die Engine und führt einen Lauf bzw. den Dauerbetrieb aus.
type Runner struct {
	Store *store.SQLite
	Blobs *blob.Store
	Log   *slog.Logger
	// LockDir ist der Ordner der Sperrdateien je Konto; leer = neben der
	// Datenbank (bei :memory: ohne Sperre).
	LockDir string
	// CommandTimeout und CommandMaxDuration steuern den Watchdog des
	// IMAP-Providers (0 = Standard; Konfiguration [imap]).
	CommandTimeout     time.Duration
	CommandMaxDuration time.Duration
	// Connect ersetzt den Verbindungsaufbau (Tests); nil = IMAP-Provider.
	Connect func(ctx context.Context, a store.Account, pw secrets.Secret, idleRestart time.Duration) (RemoteCloser, error)
}

// RemoteCloser ist ein Remote mit Close und Strategie.
type RemoteCloser interface {
	Remote
	Close() error
}

var _ ops.SyncRunner = (*Runner)(nil)

type stratRemote interface{ Strategy() string }

// imapRemote passt den IMAP-Provider an (Strategy als Text).
type imapRemote struct{ *imapprov.Provider }

func (r imapRemote) Strategy() string { return string(r.Provider.Strategy()) }

func (r *Runner) connectIMAP(ctx context.Context, a store.Account, pw secrets.Secret, idleRestart time.Duration) (RemoteCloser, error) {
	p, err := imapprov.Connect(ctx, imapprov.Config{Host: a.Host, Port: a.Port, TLS: a.TLS, Username: a.Username,
		InsecureTestServer: a.InsecureTestServer, IdleRestart: idleRestart,
		CommandTimeout: r.CommandTimeout, CommandMaxDuration: r.CommandMaxDuration}, pw)
	if err != nil {
		return nil, err
	}
	return imapRemote{p}, nil
}

// Sync führt sync.run aus.
func (r *Runner) Sync(ctx context.Context, a store.Account, pw secrets.Secret, o ops.SyncOptions, progress func(ops.Progress)) (ops.SyncOutput, error) {
	out := ops.SyncOutput{Account: a.Name, Mode: "incremental", Mailboxes: []ops.SyncMailbox{}}
	if o.Full {
		out.Mode = "full"
	}
	if o.Watch {
		out.Mode = "watch"
	}
	connect := r.Connect
	if connect == nil {
		connect = r.connectIMAP
	}
	idleRestart := imapx.DefaultIdleRestart
	if o.IdleRestart > 0 {
		idleRestart = ClampIdleRestart(o.IdleRestart)
	}
	if !o.DryRun {
		// Vor dem Verbindungsaufbau: Ein zweiter Sync desselben Kontos
		// meldet sich gar nicht erst am Server an.
		lock, err := r.lock(a)
		if err != nil {
			return out, err
		}
		defer lock.release()
	}
	start := time.Now()
	remote, err := connect(ctx, a, pw, idleRestart)
	if err != nil {
		return out, err
	}
	defer remote.Close()
	if s, ok := remote.(stratRemote); ok {
		out.Strategy = s.Strategy()
	}
	if o.DryRun {
		return r.dryRun(ctx, a, remote, out)
	}
	eng := &Engine{Store: r.Store, Blobs: r.Blobs, Remote: remote, AccountID: a.ID, Log: r.Log}
	if progress != nil {
		eng.Progress = func(ev Event) {
			progress(ops.Progress{Event: ev.Kind, Mailbox: ev.Mailbox, Done: ev.Done, Total: ev.Total, Message: ev.Message})
		}
	}
	so := Options{Window: o.Window, Full: o.Full, Mailboxes: o.Mailboxes}
	if !o.Watch {
		rep, err := eng.SyncAll(ctx, so)
		fill(&out, rep)
		out.DurationMS = time.Since(start).Milliseconds()
		return out, err
	}
	err = eng.Watch(ctx, WatchOptions{Options: so, Interval: o.Interval, OnRound: func(rep Report, err error) {
		out.Rounds++
		round := ops.SyncOutput{Account: a.Name, Mode: "watch", Strategy: out.Strategy, Mailboxes: []ops.SyncMailbox{}}
		fill(&round, rep)
		round.DurationMS = rep.Duration.Milliseconds()
		out.New += round.New
		out.Updated += round.Updated
		out.Removed += round.Removed
		out.Errors += round.Errors
		out.MessagesMirrored = round.MessagesMirrored
		if err != nil {
			out.Errors++
		}
		if progress != nil {
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			progress(ops.Progress{Event: "round", Message: msg, Round: &round})
		}
	}})
	out.DurationMS = time.Since(start).Milliseconds()
	return out, err
}

// lock sperrt das Konto für einen schreibenden Sync (siehe lock.go).
func (r *Runner) lock(a store.Account) (*accountLock, error) {
	dir := r.LockDir
	if dir == "" && r.Store != nil && r.Store.Path() != "" {
		dir = filepath.Dir(r.Store.Path())
	}
	if dir == "" {
		return nil, nil
	}
	l, err := lockAccount(dir, a.ID)
	if errors.Is(err, errLocked) {
		return nil, fmt.Errorf("%w: account %s", ops.ErrSyncRunning, a.Name)
	}
	return l, err
}

func fill(out *ops.SyncOutput, rep Report) {
	for _, m := range rep.Mailboxes {
		out.Mailboxes = append(out.Mailboxes, ops.SyncMailbox{Path: m.Path, Role: m.Role, New: m.New, Updated: m.Updated,
			Removed: m.Removed, Errors: m.Errors, Reset: m.Reset, Total: m.Total, Mirrored: m.Mirrored, Problems: m.Problems})
	}
	out.New, out.Updated, out.Removed, out.Errors, out.MessagesMirrored = rep.New, rep.Updated, rep.Removed, rep.Errors, rep.Mirrored
}

// dryRun meldet Ordner des Servers und den lokalen Stand, ohne zu schreiben.
func (r *Runner) dryRun(ctx context.Context, a store.Account, remote Remote, out ops.SyncOutput) (ops.SyncOutput, error) {
	boxes, err := remote.ListMailboxes(ctx)
	if err != nil {
		return out, err
	}
	local, err := r.Store.Mailboxes(ctx, a.ID)
	if err != nil {
		return out, err
	}
	mirrored := map[string]int{}
	for _, m := range local {
		mirrored[m.Path] = m.Mirrored
	}
	for _, b := range boxes {
		out.Mailboxes = append(out.Mailboxes, ops.SyncMailbox{Path: b.Path, Role: string(b.Role), Mirrored: mirrored[b.Path]})
	}
	if out.MessagesMirrored, err = r.Store.CountPresentMessages(ctx, a.ID); err != nil {
		return out, err
	}
	return out, nil
}
