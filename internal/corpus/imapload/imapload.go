// Package imapload legt den synthetischen Korpus per IMAP APPEND in einem
// Testserver ab (Ordner, Flags und INTERNALDATE laut Manifest). Benutzt vom
// Werkzeug corpusload (Abnahme D1.3) und von den Integrationstests.
//
// Nur für lokale Testserver: Load verweigert andere Hosts als localhost.
package imapload

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
)

// Options steuert Load.
type Options struct {
	// Prefix legt alle Ordner unterhalb dieses Ordners an (Integrationstests
	// auf gemeinsam genutzten Konten). Leer = Ordner wie im Manifest
	// (INBOX, Archiv/2024, Archiv/2025, Drafts).
	Prefix string
	// Force lädt auch in nicht leere Ordner (sonst Abbruch, damit ein
	// zweiter Aufruf keine Dubletten erzeugt).
	Force bool
	// Progress wird nach jeder Nachricht aufgerufen (optional).
	Progress func(done, total int)
}

// ErrNotEmpty: Ein Zielordner enthält schon Nachrichten.
var ErrNotEmpty = errors.New("imapload: target mailbox is not empty")

// Delimiter liefert das Ordner-Trennzeichen des Servers ("/" oder ".").
func Delimiter(c *imapx.Client) (string, error) {
	boxes, err := c.List()
	if err != nil {
		return "", err
	}
	for _, b := range boxes {
		if strings.EqualFold(b.Path, "INBOX") && b.Delim != "" {
			return b.Delim, nil
		}
	}
	for _, b := range boxes {
		if b.Delim != "" {
			return b.Delim, nil
		}
	}
	return "/", nil
}

// Folder bildet einen Manifest-Ordner (mit "/") auf den Serverpfad ab.
func Folder(manifestFolder, prefix, delim string) string {
	f := strings.ReplaceAll(manifestFolder, "/", delim)
	if prefix == "" {
		return f
	}
	return prefix + delim + f
}

// Load legt alle Nachrichten des Korpus ab und liefert die Zahl je
// Serverordner.
func Load(ctx context.Context, c *imapx.Client, cp *corpus.Corpus, o Options) (map[string]int, error) {
	delim, err := Delimiter(c)
	if err != nil {
		return nil, err
	}
	targets := map[string]int{}
	var order []string
	for _, e := range cp.Manifest.Messages {
		f := Folder(e.Folder, o.Prefix, delim)
		if _, ok := targets[f]; !ok {
			order = append(order, f)
		}
		targets[f] = 0
	}
	if o.Prefix != "" {
		if err := c.Create(o.Prefix); err != nil {
			return nil, fmt.Errorf("create %s: %w", o.Prefix, err)
		}
	}
	for _, f := range order {
		if !strings.EqualFold(f, "INBOX") {
			if err := createPath(c, f, delim); err != nil {
				return nil, err
			}
		}
		st, err := c.Status(f)
		if err != nil {
			return nil, fmt.Errorf("status %s: %w", f, err)
		}
		if st.Messages > 0 && !o.Force {
			return nil, fmt.Errorf("%w: %s has %d messages (already loaded?)", ErrNotEmpty, f, st.Messages)
		}
	}
	total := len(cp.Raw)
	for i, e := range cp.Manifest.Messages {
		if err := ctx.Err(); err != nil {
			return targets, err
		}
		f := Folder(e.Folder, o.Prefix, delim)
		date, _ := time.Parse(time.RFC3339, e.InternalDate)
		if _, err := c.Append(f, cp.Raw[i], e.Flags, date); err != nil {
			return targets, fmt.Errorf("append %s to %s: %w", e.File, f, err)
		}
		targets[f]++
		if o.Progress != nil {
			o.Progress(i+1, total)
		}
	}
	return targets, refresh(c, Folder("INBOX", o.Prefix, delim))
}

// refresh nimmt nach einer kurzen Pause eine kleine Änderung vor (Keyword
// setzen und wieder entfernen). Befund S1.4: Stalwart 0.11.8 kann seine
// zwischengespeicherte Ordnerliste nach vielen schnellen Schreibvorgängen
// veralten lassen, bis zur nächsten Änderung am Konto; neue Sitzungen (der
// Sync) sähen die eben angelegten Ordner sonst nicht.
func refresh(c *imapx.Client, mailbox string) error {
	time.Sleep(time.Second)
	if _, err := c.Select(mailbox, imapx.SelectOptions{}); err != nil {
		return err
	}
	uids, err := c.UIDSearch(0, imapx.SearchOptions{})
	if err != nil || len(uids) == 0 {
		return err
	}
	const kw = "$PostKauzRefresh"
	if err := c.StoreFlags(uids[:1], []string{kw}, true); err != nil {
		return nil // Server ohne freie Keywords: dann eben nicht
	}
	return c.StoreFlags(uids[:1], []string{kw}, false)
}

// createPath legt einen Ordner samt Elternordnern an (manche Server tun
// das nicht selbst).
func createPath(c *imapx.Client, path, delim string) error {
	parts := strings.Split(path, delim)
	for i := 1; i <= len(parts); i++ {
		p := strings.Join(parts[:i], delim)
		if strings.EqualFold(p, "INBOX") {
			continue
		}
		if err := c.Create(p); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
	return nil
}
