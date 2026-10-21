package mailsync

import (
	"context"
	"errors"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// Standardwerte für Watch.
const (
	DefaultInterval = 5 * time.Minute
	// DefaultFullCheckInterval ist der Abstand der vollen UID-Abgleiche
	// aller Ordner im Dauerbetrieb (A02 A1, `--full-check-interval`).
	DefaultFullCheckInterval = time.Hour
	// idleRetry ist die Wartezeit, bevor eine gescheiterte IDLE-Verbindung
	// neu aufgebaut wird (der Provider hat dann schon mit Backoff
	// wiederholt; Polling läuft in der Zeit weiter).
	idleRetry = 30 * time.Second
)

// WatchOptions steuert Watch.
type WatchOptions struct {
	Options
	// Interval ist der Abstand der Läufe über alle Ordner (0 = 5 min).
	Interval time.Duration
	// FullCheckInterval ist der Abstand der vollen UID-Abgleiche (0 = 1 h):
	// Der erste Lauf über alle Ordner und danach jeder, der mindestens so
	// lange nach dem letzten erfolgreichen vollen Abgleich beginnt, gleicht
	// in jedem Ordner die vollständige UID-Liste ab (Options.CheckDeleted),
	// auch wenn die Zahlen passen. IDLE-Läufe (nur INBOX) tun das nie.
	FullCheckInterval time.Duration
	// IdleMailbox wird per IDLE beobachtet (Standard "INBOX"); eine Meldung
	// löst sofort einen Lauf für diesen Ordner aus.
	IdleMailbox string
	// IdleRetry überschreibt die Wartezeit nach einem IDLE-Fehler (Tests).
	IdleRetry time.Duration
	// OnRound bekommt das Ergebnis jedes Laufs (auch fehlerhafter).
	OnRound func(r Report, err error)
}

// Watch synct sofort, danach alle Interval und zusätzlich bei jeder
// IDLE-Meldung für die INBOX, bis ctx endet (Rückgabe nil). Läufe über
// alle Ordner gleichen alle FullCheckInterval die UID-Listen vollständig ab
// (Löschungserkennung, A02 A1). Fehler eines
// Laufs (Verbindung) beenden Watch nicht; der nächste Lauf versucht es neu
// (die Session hat dann schon mit Backoff wiederholt). Nur ein
// Anmeldefehler beendet Watch: Ein Wiederholen würde das Konto sperren.
func (e *Engine) Watch(ctx context.Context, o WatchOptions) error {
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
	if o.IdleMailbox == "" {
		o.IdleMailbox = "INBOX"
	}
	if o.IdleRetry <= 0 {
		o.IdleRetry = idleRetry
	}
	if o.FullCheckInterval <= 0 {
		o.FullCheckInterval = DefaultFullCheckInterval
	}
	// Die IDLE-Schleife endet mit Watch: eigener Kontext, und Watch wartet
	// auf sie (sonst liefe sie nach einem Anmeldefehler eines Laufs mit
	// eigener Verbindung weiter; Leck-Test in leak_test.go). Sie läuft
	// unter crash.Go (A02 A5): Ein Panic darin beendet Watch mit einem
	// *crash.PanicError statt den Prozess mit Go-Stack und Argumenten.
	ctx, cancel := context.WithCancel(ctx)
	kick := make(chan struct{}, 1)
	var idleDone <-chan error // nil = keine IDLE-Schleife (mehr)
	defer func() {
		cancel()
		if idleDone != nil {
			<-idleDone
		}
	}()
	if e.Remote.Capabilities().Idle {
		idleDone = crash.Go("IDLE watcher", func() error { return e.idleLoop(ctx, o, kick) })
	} else {
		e.log().Info("server has no IDLE; polling only", "interval", o.Interval.String())
	}

	var lastFull time.Time // Beginn des letzten erfolgreichen vollen Abgleichs
	round := func(only []string) error {
		ro := o.Options
		start := time.Now()
		if only != nil {
			ro.Mailboxes = only
		} else if lastFull.IsZero() || start.Sub(lastFull) >= o.FullCheckInterval {
			ro.CheckDeleted = true
			e.log().Info("full UID check of all mailboxes")
		}
		rep, err := e.SyncAll(ctx, ro)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil && ro.CheckDeleted {
			lastFull = start
		}
		if o.OnRound != nil {
			o.OnRound(rep, err)
		}
		if errors.Is(err, provider.ErrAuth) {
			return err
		}
		if err != nil {
			e.log().Warn("sync round failed; retrying at the next interval", "error", err.Error())
		}
		return nil
	}

	if err := round(nil); err != nil {
		return err
	}
	t := time.NewTicker(o.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-idleDone:
			idleDone = nil // beendet; ohne Fehler (IDLE nicht unterstützt) bleibt das Polling
			if err != nil {
				return err
			}
		case <-kick:
			if err := round([]string{o.IdleMailbox}); err != nil {
				return err
			}
		case <-t.C:
			if err := round(nil); err != nil {
				return err
			}
		}
	}
}

// idleLoop hält IDLE auf dem Ordner; nach einem Fehler wartet sie und baut
// neu auf. Ein Anmeldefehler kommt zurück und beendet Watch; nil heißt
// beendet (ctx) oder IDLE nicht unterstützt.
func (e *Engine) idleLoop(ctx context.Context, o WatchOptions, kick chan<- struct{}) error {
	for ctx.Err() == nil {
		err := e.Remote.Idle(ctx, o.IdleMailbox, kick)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, provider.ErrAuth) {
			return err
		}
		if errors.Is(err, provider.ErrUnsupported) {
			return nil
		}
		if err != nil {
			e.log().Warn("IDLE connection failed; reconnecting later", "error", err.Error(), "retry_in", o.IdleRetry.String())
		}
		t := time.NewTimer(o.IdleRetry)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
		// Nach dem Wiederaufbau einmal synchronisieren: Während der Pause
		// gemeldete Änderungen wären sonst bis zum nächsten Intervall unsichtbar.
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	return nil
}

// ClampIdleRestart hält den IDLE-Neustart im Rahmen von ADR-007 (5 bis
// 10 Minuten); 0 heißt Standard (8 min). Kürzere Werte nur in Tests.
func ClampIdleRestart(d time.Duration) time.Duration {
	switch {
	case d <= 0:
		return imapx.DefaultIdleRestart
	case d < imapx.MinIdleRestart:
		return imapx.MinIdleRestart
	case d > imapx.MaxIdleRestart:
		return imapx.MaxIdleRestart
	}
	return d
}
