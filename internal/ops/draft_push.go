package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Push der Entwürfe (S2.5, A02 E1, E6, E11, E12): Der Job draft_push legt
// jeden Entwurf eines Kontos, dessen Kopie aussteht, per IMAP APPEND im
// Entwürfe-Ordner ab. Ausgeführt wird er nur vom Prozess mit Zugangsdaten
// (postkauz sync zu Beginn jedes Laufs, sync --watch alle --jobs-interval;
// internal/mailsync), nie vom MCP-Server. DraftPusher ist der fachliche Teil:
// Entwurf lesen, Übernahme prüfen (E6), bei ready die Freigabe prüfen
// (message_hash, Festlegung S2.5), die Nachricht mit frischem Date bauen
// (E11) und die abgelegte Kopie vermerken. Das APPEND selbst macht der
// Aufrufer über DraftAppender (internal/mailsync → internal/imapx).
//
// Ein Entwurf in draft oder pending_approval wird abgelegt, wie er ist (er
// ist ja ein Entwurf); einer in ready nur, wenn die jetzt gebaute Nachricht
// bis auf Date genau die freigegebene ist (approvals.message_hash), sonst
// gar nicht: zurück auf draft, Freigabe superseded, Hinweis approval_stale.
// sent_by_human und abandoned werden nie abgelegt.

// DraftAppender legt eine gebaute Nachricht im Entwürfe-Ordner ab, mit den
// Flags \Draft und \Seen (E12), und liefert, was der Push vermerkt.
type DraftAppender interface {
	AppendDraft(ctx context.Context, raw []byte, messageID string, date time.Time) (AppendedCopy, error)
}

// AppendedCopy ist die abgelegte Kopie: UID (0 = unbekannt), UIDVALIDITY
// des Ordners und der Inhalts-Hash der Bytes wie messages.content_hash des
// Spiegels (für die Übernahme-Erkennung).
type AppendedCopy struct {
	UID         uint32
	UIDValidity uint32
	ContentHash string
}

// DraftPushReport fasst einen Push zusammen (Zähler, kein Inhalt).
type DraftPushReport struct {
	// Pushed: abgelegte Kopien.
	Pushed int `json:"pushed"`
	// TakenOver, Missing: Entwürfe, deren frühere Kopie der Mensch im
	// Mailclient übernommen bzw. entfernt hat (E6); nicht abgelegt.
	TakenOver int `json:"taken_over,omitempty"`
	Missing   int `json:"missing,omitempty"`
	// Stale: Entwürfe in ready, deren Nachricht nicht mehr zur Freigabe
	// passte; zurück auf draft, nicht abgelegt.
	Stale int `json:"approval_stale,omitempty"`
	// Failed: Entwürfe, deren Nachricht sich nicht bauen ließ (push_failed).
	Failed int `json:"failed,omitempty"`
	// DraftIDs: die abgelegten Entwürfe.
	DraftIDs []int64 `json:"draft_ids,omitempty"`
}

// Add zählt r dazu (Dauerbetrieb: mehrere Pushs je Lauf).
func (r *DraftPushReport) Add(o DraftPushReport) {
	r.Pushed += o.Pushed
	r.TakenOver += o.TakenOver
	r.Missing += o.Missing
	r.Stale += o.Stale
	r.Failed += o.Failed
	r.DraftIDs = append(r.DraftIDs, o.DraftIDs...)
}

// ErrDraftBuild: Mindestens ein Entwurf ließ sich nicht bauen (push_failed
// am Entwurf); der Job zählt das als Fehler und versucht es später erneut.
var ErrDraftBuild = errors.New("some drafts could not be built for the Drafts folder")

// DraftPusher ist der fachliche Teil des Jobs draft_push.
type DraftPusher struct {
	o *draftOps
	// Now ist die Uhr des Date-Kopfs (nil = time.Now; Tests setzen eine feste).
	Now func() time.Time
}

// NewDraftPusher baut den Pusher aus denselben Abhängigkeiten wie die
// Entwurfsoperationen (Dienst, Spiegel, Blob-Store, Konfiguration).
func NewDraftPusher(d Deps) *DraftPusher { return &DraftPusher{o: newDraftOps(d)} }

func (p *DraftPusher) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// EnsureJob reiht draft_push ein, wenn Entwürfe des Kontos auf ihre Kopie
// warten und kein Job offen ist (der Sync holt so auch nach, was ein
// gescheiterter Job liegen ließ).
func (p *DraftPusher) EnsureJob(ctx context.Context, accountID int64) error {
	_, err := p.o.svc.Store.EnsureDraftPushJob(ctx, accountID, time.Now())
	return err
}

// Pending liefert, wie viele Entwürfe des Kontos auf ihre Kopie warten und
// wie viele davon schon eine frühere Kopie haben (deren Übernahme der Push
// prüft; der Aufrufer spiegelt dafür vorher den Entwürfe-Ordner).
func (p *DraftPusher) Pending(ctx context.Context, accountID int64) (pending, withCopy int, err error) {
	ids, withCopy, err := p.o.svc.Store.DraftPushCandidates(ctx, accountID)
	return len(ids), withCopy, err
}

// NoDraftsMailbox vermerkt an allen wartenden Entwürfen des Kontos, dass es
// keinen Entwürfe-Ordner gibt (draft show nennt den Grund).
func (p *DraftPusher) NoDraftsMailbox(ctx context.Context, accountID int64) error {
	return p.o.svc.NotePush(ctx, accountID, 0, drafts.PushNoteNoDraftsMailbox)
}

// Push legt die wartenden Entwürfe des Kontos acc im Ordner mailboxID ab
// (app hängt am Pfad dieses Ordners). Ein Fehler von app (Verbindung,
// abgelehntes APPEND) beendet den Push sofort; die übrigen Entwürfe bleiben
// pending. Ließ sich ein Entwurf nicht bauen, geht es mit den übrigen
// weiter, und Push liefert am Ende ErrDraftBuild.
func (p *DraftPusher) Push(ctx context.Context, acc store.Account, mailboxID int64, app DraftAppender) (DraftPushReport, error) {
	var rep DraftPushReport
	ids, _, err := p.o.svc.Store.DraftPushCandidates(ctx, acc.ID)
	if err != nil {
		return rep, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if err := p.pushOne(ctx, acc, mailboxID, id, app, &rep); err != nil {
			return rep, err
		}
	}
	if rep.Failed > 0 {
		return rep, fmt.Errorf("%w (%d)", ErrDraftBuild, rep.Failed)
	}
	return rep, nil
}

func (p *DraftPusher) pushOne(ctx context.Context, acc store.Account, mailboxID, id int64, app DraftAppender, rep *DraftPushReport) error {
	svc := p.o.svc
	// E6: Ist die zuletzt abgelegte Kopie laut Spiegel noch unverändert da?
	d, state, err := svc.PushCheck(ctx, id)
	switch {
	case err != nil:
		return err
	case state == drafts.RemoteTakenOver:
		rep.TakenOver++
		return nil
	case state == drafts.RemoteMissing:
		rep.Missing++
		return nil
	case !drafts.Pending(d):
		return nil // inzwischen abgelegt, abgeschlossen oder übernommen
	}
	fp := drafts.Fingerprint(d)
	// E11: Date setzt jeder Push neu (sekundengenau wie die Kopfzeile).
	date := p.now().Truncate(time.Second)
	msg, err := p.o.rebuildAt(ctx, acc, d, date)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		rep.Failed++
		return svc.NotePush(ctx, acc.ID, d.ID, drafts.PushNotePushFailed)
	}
	if d.Status == string(drafts.StatusReady) {
		ok, err := p.approvedMessage(ctx, d, compose.MessageHash(msg.Raw))
		if err != nil {
			return err
		}
		if !ok {
			// Festlegung S2.5: kein Push, zurück auf draft, Freigabe superseded.
			if _, err := svc.PushStale(ctx, d.ID, fp); err != nil && drafts.ReasonOf(err) != drafts.ReasonDraftChanged {
				return err
			}
			rep.Stale++
			return nil
		}
	}
	cp, err := app.AppendDraft(ctx, msg.Raw, d.MessageIDHeader, date)
	if err != nil {
		return err
	}
	if _, _, err := svc.Pushed(ctx, d.ID, fp, drafts.PushedCopy{MailboxID: mailboxID, UIDValidity: cp.UIDValidity, UID: int64(cp.UID),
		ContentHash: cp.ContentHash, Date: date.Format(time.RFC1123Z)}); err != nil {
		return err
	}
	rep.Pushed++
	rep.DraftIDs = append(rep.DraftIDs, d.ID)
	return nil
}

// approvedMessage meldet, ob die gebaute Nachricht (Hash ohne Date) die
// ist, die der Mensch freigegeben hat: erteilte Freigabe des Entwurfs mit
// demselben message_hash.
func (p *DraftPusher) approvedMessage(ctx context.Context, d store.Draft, hash string) (bool, error) {
	if d.ApprovalID == 0 {
		return false, nil
	}
	a, err := p.o.svc.Store.Approval(ctx, d.ApprovalID)
	if err != nil {
		if drafts.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return a.Decision == drafts.DecisionApproved && a.MessageHash != "" && a.MessageHash == hash, nil
}
