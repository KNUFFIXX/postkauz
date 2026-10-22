package mailsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Job draft_push im Sync (S2.5, A02 E1, E6, E12): Wer Zugangsdaten hat und
// die Kontosperre hält, legt die wartenden Entwürfe per APPEND im
// Entwürfe-Ordner ab, und zwar
//
//   - postkauz sync zu Beginn jedes Laufs, vor dem Spiegeln (der Spiegel
//     sieht die neue Kopie dann im selben Lauf), und
//   - postkauz sync --watch zusätzlich alle WatchOptions.JobsInterval
//     (Standard 10 s), auch zwischen zwei Läufen.
//
// Der MCP-Server und die übrigen kurzlebigen Prozesse reihen den Job nur
// ein (drafts.create/drafts.update); sie kennen keine Zugangsdaten. Die
// fachlichen Regeln (Übernahme, Freigabe, Vermerk der Kopie) stehen in
// internal/ops (DraftPusher) und internal/drafts.

// DefaultJobsInterval ist der Abstand, in dem sync --watch die Jobs des
// Kontos prüft (A02 E1, `--jobs-interval`).
const DefaultJobsInterval = 10 * time.Second

// DraftPusher ist der fachliche Teil des Jobs draft_push (ops.DraftPusher).
type DraftPusher interface {
	EnsureJob(ctx context.Context, accountID int64) error
	Pending(ctx context.Context, accountID int64, retry bool) (pending, withCopy int, err error)
	NoDraftsMailbox(ctx context.Context, accountID int64) error
	Push(ctx context.Context, acc store.Account, mailboxID int64, app ops.DraftAppender, retry bool) (ops.DraftPushReport, error)
}

var _ DraftPusher = (*ops.DraftPusher)(nil)

// draftJobs führt den Job draft_push aus (jobs.Runner).
type draftJobs interface {
	RunDraftPush(ctx context.Context, accountID int64, push func(context.Context) error) (bool, error)
}

// Drafts verbindet die Engine mit dem Job draft_push.
type Drafts struct {
	Pusher  DraftPusher
	Jobs    draftJobs
	Account store.Account
	// Mailbox ist [draft] mailbox der Konfiguration: der Pfad des
	// Entwürfe-Ordners; leer = der Ordner mit Rolle drafts (SPECIAL-USE,
	// sonst Name).
	Mailbox string
}

// ErrNoDraftsMailbox: Das Konto hat keinen Entwürfe-Ordner (A02 E12).
var ErrNoDraftsMailbox = errors.New(ops.ReasonNoDraftsMailbox +
	": the account has no Drafts folder (no mailbox with role drafts, or [draft] mailbox does not exist on the server)")

// appender ist der Teil des Providers, der ablegen kann (IMAP APPEND).
type appender interface {
	Append(ctx context.Context, mailbox string, raw []byte, o provider.AppendOptions) (provider.AppendResult, error)
}

// draftAppender legt Entwürfe im Ordner path ab: Flags \Draft und \Seen
// (E12), INTERNALDATE = Date der Nachricht, Message-ID zum Wiederfinden
// (Server ohne APPENDUID, Abbruch während APPEND). Der Inhalts-Hash ist der
// des Spiegels (ContentHasher), damit die Übernahme-Erkennung die Kopie im
// Spiegel wiedererkennt.
type draftAppender struct {
	r    appender
	path string
}

func (a draftAppender) AppendDraft(ctx context.Context, raw []byte, messageID string, date time.Time) (ops.AppendedCopy, error) {
	res, err := a.r.Append(ctx, a.path, raw, provider.AppendOptions{Flags: provider.Flags{Seen: true, Draft: true}, Date: date,
		MessageID: messageID})
	if errors.Is(err, provider.ErrRejected) {
		// Nur dieser Entwurf (W3); der Text des Servers bleibt draußen.
		return ops.AppendedCopy{}, ops.ErrAppendRejected
	}
	if err != nil {
		return ops.AppendedCopy{}, err
	}
	return ops.AppendedCopy{UID: uint32(res.UID), UIDValidity: res.UIDValidity, ContentHash: a.ContentHash(raw)}, nil
}

// ContentHash ist der Inhalts-Hash wie messages.content_hash des Spiegels.
func (draftAppender) ContentHash(raw []byte) string {
	h := NewContentHasher()
	_, _ = h.Write(raw)
	exact, _ := h.Sums()
	return exact
}

// pushDrafts führt den fälligen Job draft_push des Kontos aus. ensure reiht
// ihn vorher ein, wenn Entwürfe warten und keiner offen ist (Beginn eines
// Laufs), und versucht dann auch Entwürfe erneut, deren Ablage der Server
// abgelehnt hat oder die sich nicht bauen ließen (push_rejected,
// push_failed; W3). nil = kein Job fällig oder nichts wartet. Ein Fehler kommt zurück
// (der Job ist dann zurückgegeben bzw. als Fehler verbucht); der Aufrufer
// entscheidet, ob der Lauf weitergeht (nur Verbindungs- und Anmeldefehler
// beenden ihn).
func (e *Engine) pushDrafts(ctx context.Context, ensure bool) (*ops.SyncDrafts, error) {
	d := e.Drafts
	if d == nil {
		return nil, nil
	}
	if ensure {
		if err := d.Pusher.EnsureJob(ctx, e.AccountID); err != nil {
			return nil, fmt.Errorf("drafts: %w", err)
		}
	}
	var out *ops.SyncDrafts
	_, err := d.Jobs.RunDraftPush(ctx, e.AccountID, func(ctx context.Context) error {
		// crash.Do (A02 A5): Ein Panic beim Bauen oder Ablegen beendet den
		// Push mit Fehler statt den Prozess.
		return crash.Do("draft push", func() error {
			var err error
			out, err = e.pushPending(ctx, d, ensure)
			return err
		})
	})
	switch {
	case err != nil && out == nil:
		out = &ops.SyncDrafts{Error: err.Error()}
	case err != nil && out.Error == "":
		out.Error = err.Error()
	}
	if out != nil && out.Pushed > 0 {
		e.emit(Event{Kind: "drafts", Mailbox: out.Mailbox, Done: out.Pushed})
	}
	if err != nil {
		e.emit(Event{Kind: "error", Mailbox: "drafts", Message: err.Error()})
	}
	return out, err
}

func (e *Engine) pushPending(ctx context.Context, d *Drafts, retry bool) (*ops.SyncDrafts, error) {
	n, withCopy, err := d.Pusher.Pending(ctx, e.AccountID, retry)
	if err != nil || n == 0 {
		return nil, err // nichts wartet
	}
	out := &ops.SyncDrafts{}
	mb, err := e.draftsMailbox(ctx, d.Mailbox)
	if errors.Is(err, ErrNoDraftsMailbox) {
		out.Error = ops.ReasonNoDraftsMailbox
		if nerr := d.Pusher.NoDraftsMailbox(ctx, e.AccountID); nerr != nil {
			return out, nerr
		}
		e.log().Warn("drafts not pushed: no Drafts folder", "pending", n)
		return out, err
	}
	if err != nil {
		return out, err
	}
	out.Mailbox = mb.Path
	if withCopy > 0 {
		// E6: Vor dem erneuten Push prüft der Pusher, ob der Mensch die
		// frühere Kopie übernommen hat, und W1: ob ein abgebrochener
		// Versuch doch abgelegt hat; dafür den Spiegel des Ordners
		// auffrischen (sonst sähe er erst den Stand des letzten Laufs).
		if _, err := e.SyncMailbox(ctx, mb, Options{}); err != nil {
			return out, fmt.Errorf("%s: %w", mb.Path, err)
		}
	}
	app, ok := e.Remote.(appender)
	if !ok {
		return out, fmt.Errorf("drafts: this connection cannot append messages")
	}
	rep, err := d.Pusher.Push(ctx, d.Account, mb.ID, draftAppender{r: app, path: mb.Path}, retry)
	out.DraftPushReport = rep
	if rep.Pushed > 0 {
		e.log().Info("drafts pushed to the Drafts folder", "count", rep.Pushed, "draft_ids", rep.DraftIDs)
	}
	if rep.Rejected+rep.Failed > 0 {
		e.log().Warn("drafts not pushed; see draft show", "rejected", rep.Rejected, "failed", rep.Failed)
	}
	if rep.Adopted > 0 {
		e.log().Info("copies of interrupted pushes found in the Drafts folder and recorded", "count", rep.Adopted)
	}
	return out, err
}

// draftsMailbox liefert den Entwürfe-Ordner: path aus [draft] mailbox,
// sonst den Ordner mit Rolle drafts. Die Ordnerliste kommt frisch vom
// Server (beim ersten Lauf eines Kontos kennt der Spiegel noch keinen).
func (e *Engine) draftsMailbox(ctx context.Context, path string) (store.Mailbox, error) {
	boxes, err := e.Remote.ListMailboxes(ctx)
	if err != nil {
		return store.Mailbox{}, err
	}
	specs := make([]store.MailboxSpec, 0, len(boxes))
	for _, b := range boxes {
		specs = append(specs, store.MailboxSpec{Path: b.Path, Name: b.Name, Role: string(b.Role)})
	}
	if _, _, err := e.Store.SyncMailboxes(ctx, e.AccountID, specs); err != nil {
		return store.Mailbox{}, err
	}
	local, err := e.Store.Mailboxes(ctx, e.AccountID)
	if err != nil {
		return store.Mailbox{}, err
	}
	for _, m := range local {
		if (path != "" && m.Path == path) || (path == "" && m.Role == string(provider.RoleDrafts)) {
			return m, nil
		}
	}
	return store.Mailbox{}, ErrNoDraftsMailbox
}
