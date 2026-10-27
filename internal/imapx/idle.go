package imapx

import (
	"context"
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// Standardwerte für IDLE (ADR-007: selbst alle 5 bis 10 Minuten neu
// starten; go-imap startet fest erst nach 28 Minuten neu).
const (
	DefaultIdleRestart = 8 * time.Minute
	MinIdleRestart     = 5 * time.Minute
	MaxIdleRestart     = 10 * time.Minute
)

// Idle wählt mailbox (EXAMINE) und wartet im IDLE, bis der Server eine
// Änderung meldet (true), maxWait abläuft (false, Neustart fällig) oder ctx
// endet. Danach wird IDLE mit DONE beendet; das Warten auf die Antwort
// steht unter dem Watchdog. Ein stummer Server (Blackhole) fällt also
// spätestens nach maxWait plus Deadline auf, eine abgebrochene Verbindung
// sofort. Fehler sind ErrConnection (wiederholbar).
func (c *Client) Idle(ctx context.Context, mailbox string, maxWait time.Duration) (bool, error) {
	if maxWait <= 0 {
		maxWait = DefaultIdleRestart
	}
	if _, err := c.ensure(mailbox, 0, SelectOptions{ReadOnly: true}); err != nil {
		return false, err
	}
	// Meldungen aus der Zeit vor IDLE verwerfen: Der Aufrufer synct nach
	// jedem Idle ohnehin; sie würden nur einen leeren Lauf auslösen.
	select {
	case <-c.changed:
	default:
	}
	var cmd *imapclient.IdleCommand
	if err := c.run(func() error {
		var err error
		cmd, err = c.c.Idle()
		return err
	}); err != nil {
		return false, cmdErr(err)
	}
	t := time.NewTimer(maxWait)
	defer t.Stop()
	changed := false
	select {
	case <-c.changed:
		changed = true
	case <-t.C:
	case <-ctx.Done():
	case <-c.c.Closed():
		return false, fmt.Errorf("%w: connection closed during IDLE", provider.ErrConnection)
	}
	if err := c.run(func() error {
		if err := cmd.Close(); err != nil {
			return err
		}
		return cmd.Wait()
	}); err != nil {
		if ctx.Err() != nil {
			return changed, ctx.Err()
		}
		return changed, connErr(err)
	}
	if err := ctx.Err(); err != nil {
		return changed, err
	}
	return changed, nil
}
