package ops

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
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
//
// Nach der Gegenprüfung von S2.4/S2.5:
//
//   - W1, keine Dublette über die Aufrufgrenze: Vor dem APPEND vermerkt der
//     Push den Versuch am Entwurf (push_attempt: Inhalts- und
//     Nachrichten-Hash, Date, Fingerprint). Endet der Lauf, bevor die Kopie
//     vermerkt ist (Verbindung weg und Wiederaufbau gescheitert, Strg+C,
//     Protokollfehler, Prozessende), sucht der nächste Push im Spiegel des
//     Entwürfe-Ordners eine Kopie mit der Message-ID und dem Inhalts-Hash
//     des Versuchs, die keine bekannte Kopie des Entwurfs ist, und übernimmt
//     sie (drafts.Service.PushCheck), statt eine zweite abzulegen. Der
//     Aufrufer spiegelt den Ordner dafür vorher (Pending zählt solche
//     Entwürfe mit).
//   - W3, ein Entwurf blockiert nicht das Konto: Lehnt der Server das
//     Ablegen eines Entwurfs ab (ErrAppendRejected) oder lässt er sich nicht
//     bauen, bekommt nur er einen Hinweis (push_rejected bzw. push_failed),
//     und es geht mit den übrigen weiter. Nur Verbindungs-, Anmelde- und
//     Protokollfehler, ein fehlender Ordner und Fehler der Datenbank beenden
//     den Push. Vermerkte Entwürfe versucht erst der nächste postkauz sync
//     erneut (retry; im Dauerbetrieb der stündliche volle Abgleich), nicht
//     jede Prüfung des Watchers.

// DraftAppender legt eine gebaute Nachricht im Entwürfe-Ordner ab, mit den
// Flags \Draft und \Seen (E12), und liefert, was der Push vermerkt.
//
// Lehnt der Server das Ablegen genau dieser Nachricht ab (NO/BAD auf
// APPEND, z. B. zu groß oder Kontingent voll), ist der Fehler
// ErrAppendRejected (ohne Text des Servers); jeder andere Fehler beendet
// den Push. ContentHash liefert den Inhalts-Hash von Bytes in der Form von
// messages.content_hash des Spiegels (derselbe, den AppendedCopy nennt).
type DraftAppender interface {
	AppendDraft(ctx context.Context, raw []byte, messageID string, date time.Time) (AppendedCopy, error)
	ContentHash(raw []byte) string
}

// ErrAppendRejected: Der Server hat das Ablegen eines Entwurfs abgelehnt
// (W3). Nur dieser Entwurf bekommt den Hinweis push_rejected.
var ErrAppendRejected = errors.New("the server refused to store the draft in the Drafts folder")

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
	// Rejected: Entwürfe, deren Ablage der Server abgelehnt hat
	// (push_rejected; W3).
	Rejected int `json:"rejected,omitempty"`
	// Adopted: Kopien abgebrochener Versuche, die der Push übernommen hat,
	// statt neu abzulegen (W1).
	Adopted int `json:"adopted,omitempty"`
	// KeyRevoked: Entwürfe, deren Schlüssel nicht mehr gilt (A8); nicht
	// abgelegt, Freigabe abgelehnt, Entwurf verworfen.
	KeyRevoked int `json:"key_revoked,omitempty"`
	// DraftIDs: die abgelegten Entwürfe.
	DraftIDs []int64 `json:"draft_ids,omitempty"`
	// Waiting: geänderte Entwürfe, die noch nicht 30 s unverändert sind
	// und deshalb noch nicht abgelegt wurden (Entscheid 43); der nächste
	// Push nach Ablauf legt sie ab.
	Waiting int `json:"waiting,omitempty"`
}

// Add zählt r dazu (Dauerbetrieb: mehrere Pushs je Lauf).
func (r *DraftPushReport) Add(o DraftPushReport) {
	r.Pushed += o.Pushed
	r.TakenOver += o.TakenOver
	r.Missing += o.Missing
	r.Stale += o.Stale
	r.Failed += o.Failed
	r.Rejected += o.Rejected
	r.Adopted += o.Adopted
	r.KeyRevoked += o.KeyRevoked
	r.DraftIDs = append(r.DraftIDs, o.DraftIDs...)
	// Wartende zählen nicht zusammen: Derselbe Entwurf wartet über mehrere
	// Prüfungen; es gilt der letzte Stand.
	r.Waiting = o.Waiting
}

// DefaultChangeSettle ist die Ruhezeit nach einer Änderung (Entscheid 43):
// Ein geänderter Entwurf (er hat schon eine Kopie im Entwürfe-Ordner) wird
// erst abgelegt, wenn er so lange unverändert ist (gemessen an
// drafts.updated_at). So entstehen weniger Zwischenstände im Ordner, denn
// PostKauz löscht alte Kopien nicht. Die erste Ablage eines neuen
// Entwurfs wartet nicht (Festlegung S2.6: Entscheid 43 nennt nur
// geänderte Entwürfe, ein neuer hat noch keine Kopie).
const DefaultChangeSettle = 30 * time.Second

// DraftPusher ist der fachliche Teil des Jobs draft_push.
type DraftPusher struct {
	o *draftOps
	// Now ist die Uhr des Date-Kopfs und der Ruhezeit nach einer Änderung
	// (nil = time.Now; Tests setzen eine feste).
	Now func() time.Time
}

// settling meldet, ob ein geänderter Entwurf noch in seiner Ruhezeit ist
// (Entscheid 43): schon einmal abgelegt und vor weniger als
// DefaultChangeSettle geändert.
func (p *DraftPusher) settling(pushedBefore bool, updated time.Time) bool {
	return pushedBefore && p.now().Sub(updated) < DefaultChangeSettle
}

// queue liefert die Entwürfe, die jetzt abgelegt werden dürfen, und die
// Zahl derer, die nach einer Änderung noch warten.
func (p *DraftPusher) queue(ctx context.Context, accountID int64, retry bool) (due []store.DraftPushItem, waiting int, err error) {
	items, err := p.o.svc.Store.DraftPushQueue(ctx, accountID, retry)
	for _, it := range items {
		if p.settling(it.Pushed, it.UpdatedAt) {
			waiting++
			continue
		}
		due = append(due, it)
	}
	return due, waiting, err
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
// wie viele davon schon eine frühere Kopie oder einen Ablageversuch mit
// offenem Ausgang haben (deren Kopien der Push im Spiegel prüft; der
// Aufrufer spiegelt dafür vorher den Entwürfe-Ordner). retry wie bei Push.
//
// Seit S2.6 (Entscheid 43) zählen geänderte Entwürfe in ihrer Ruhezeit
// nicht mit (Waiting nennt sie).
func (p *DraftPusher) Pending(ctx context.Context, accountID int64, retry bool) (pending, withCopy int, err error) {
	due, _, err := p.queue(ctx, accountID, retry)
	for _, it := range due {
		if it.HadCopy {
			withCopy++
		}
	}
	return len(due), withCopy, err
}

// Waiting liefert, wie viele geänderte Entwürfe des Kontos noch in ihrer
// Ruhezeit sind (Entscheid 43). Ein einmaliger sync meldet sie ehrlich als
// noch nicht abgelegt; sync --watch legt sie nach Ablauf ab (der Job bleibt
// dafür fällig, store.CompleteJob).
func (p *DraftPusher) Waiting(ctx context.Context, accountID int64) (int, error) {
	_, waiting, err := p.queue(ctx, accountID, true)
	return waiting, err
}

// NoDraftsMailbox vermerkt an allen wartenden Entwürfen des Kontos, dass es
// keinen Entwürfe-Ordner gibt (draft show nennt den Grund).
func (p *DraftPusher) NoDraftsMailbox(ctx context.Context, accountID int64) error {
	return p.o.svc.NotePush(ctx, accountID, 0, drafts.PushNoteNoDraftsMailbox)
}

// Push legt die wartenden Entwürfe des Kontos acc im Ordner mailboxID ab
// (app hängt am Pfad dieses Ordners). Lehnt der Server einen Entwurf ab
// oder lässt er sich nicht bauen, bekommt er einen Hinweis, und es geht mit
// den übrigen weiter (W3). Jeder andere Fehler (Verbindung, Anmeldung,
// Protokoll, Ordner fehlt, Datenbank) beendet den Push sofort; die übrigen
// Entwürfe bleiben pending. retry: auch Entwürfe mit push_rejected oder
// push_failed erneut versuchen (postkauz sync; im Dauerbetrieb der
// stündliche volle Abgleich); sonst nur die fälligen (Prüfung des Watchers
// alle 10 s, übrige Runden).
func (p *DraftPusher) Push(ctx context.Context, acc store.Account, mailboxID int64, app DraftAppender, retry bool) (DraftPushReport, error) {
	var rep DraftPushReport
	due, waiting, err := p.queue(ctx, acc.ID, retry)
	rep.Waiting = waiting
	if err != nil {
		return rep, err
	}
	for _, it := range due {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if err := p.pushOne(ctx, acc, mailboxID, it.ID, app, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func (p *DraftPusher) pushOne(ctx context.Context, acc store.Account, mailboxID, id int64, app DraftAppender, rep *DraftPushReport) error {
	svc := p.o.svc
	// W1: Kopie eines abgebrochenen Versuchs übernehmen; E6: Ist die zuletzt
	// abgelegte Kopie laut Spiegel noch unverändert da?
	before, err := svc.Store.Draft(ctx, id)
	if err != nil {
		return err
	}
	if drafts.Pending(before) && p.settling(before.PushedContentHash != "", before.UpdatedAt) {
		// Entscheid 43: inzwischen erneut geändert (ein anderer Prozess,
		// nachdem die Warteschlange gelesen war).
		rep.Waiting++
		return nil
	}
	d, state, err := svc.PushCheck(ctx, id)
	if err == nil && before.PushAttempt != nil && d.PushAttempt == nil {
		rep.Adopted++
	}
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
	// A8, A-F3: Gilt der Schlüssel, für den der Entwurf entstand, der ihn
	// zuletzt geändert oder vorgelegt hat, noch (Widerruf, Ablauf, Scope)?
	// Sonst kein APPEND: Entwurf eines Agenten verworfen, Entwurf des
	// Menschen zurück auf draft (bei fremdem Inhalt zurückgehalten).
	if den, err := p.originDenied(ctx, d); err != nil {
		return err
	} else if den.reason != "" {
		var res drafts.Result
		if d.CreatedBy == drafts.HumanKeyID {
			res, err = svc.HoldForKey(ctx, d.ID, den.reason, den.content)
		} else {
			res, err = svc.CloseForKey(ctx, d.ID, den.reason)
			res.Changed = res.To != res.From
		}
		if err != nil {
			return err
		}
		if res.Changed {
			rep.KeyRevoked++
		}
		return nil
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
	mh, dateText := compose.MessageHash(msg.Raw), date.Format(time.RFC1123Z)
	// W1: Versuch vor dem APPEND vermerken; endet der Lauf vor dem Vermerk
	// der Kopie, übernimmt der nächste Push sie statt einer zweiten.
	if err := svc.Attempt(ctx, d.ID, store.PushAttempt{MailboxID: mailboxID, ContentHash: app.ContentHash(msg.Raw), MessageHash: mh,
		Date: dateText, Fingerprint: fp}); err != nil {
		return err
	}
	cp, err := app.AppendDraft(ctx, msg.Raw, d.MessageIDHeader, date)
	if errors.Is(err, ErrAppendRejected) {
		// W3: nur dieser Entwurf; die übrigen werden weiter abgelegt.
		rep.Rejected++
		return svc.Rejected(ctx, d.ID)
	}
	if err != nil {
		return err
	}
	if _, _, err := svc.Pushed(ctx, d.ID, fp, drafts.PushedCopy{MailboxID: mailboxID, UIDValidity: cp.UIDValidity, UID: int64(cp.UID),
		ContentHash: cp.ContentHash, MessageHash: mh, Date: dateText}); err != nil {
		return err
	}
	rep.Pushed++
	rep.DraftIDs = append(rep.DraftIDs, d.ID)
	return nil
}

// denial ist das Ergebnis von originDenied: der Grund (key_revoked,
// key_expired, key_invalid, missing_scope; "" = alles gültig) und ob der
// abgelehnte Schlüssel den Inhalt zuletzt geändert hat (updated_by).
type denial struct {
	reason  string
	content bool
}

// originDenied prüft die Schlüssel hinter einem Entwurf (A8, A-F3): den,
// mit dem er angelegt wurde (created_by), den, der den Inhalt zuletzt
// geändert hat (updated_by), und den, der die offene oder erteilte Freigabe
// beantragt hat (requested_by). Hat der Mensch selbst vorgelegt, steht er
// für den Inhalt (die Freigabe bindet ihn über preview_hash); updated_by
// zählt dann nicht. Für "key:<name>" frisch aus der Datenbank: Widerruf und
// Ablauf wie im Executor (KeyGuard), dann die Policy mit Scope mail:draft.
// Frisch heißt: je Entwurf eine Abfrage, kein Zwischenspeicher wie im
// KeyGuard des Executors (Widerruf wirkt sofort).
func (p *DraftPusher) originDenied(ctx context.Context, d store.Draft) (denial, error) {
	type origin struct {
		id      string
		content bool
	}
	ids := []origin{{id: d.CreatedBy}}
	humanRequested := false
	if d.ApprovalID > 0 {
		a, err := p.o.svc.Store.Approval(ctx, d.ApprovalID)
		if err != nil && !drafts.IsNotFound(err) {
			return denial{}, err
		}
		if err == nil && (a.Decision == drafts.DecisionPending || a.Decision == drafts.DecisionApproved) {
			ids = append(ids, origin{id: a.RequestedBy})
			humanRequested = a.RequestedBy == drafts.HumanKeyID
		}
	}
	if d.UpdatedBy != "" && d.UpdatedBy != d.CreatedBy && !humanRequested {
		ids = append(ids, origin{id: d.UpdatedBy, content: true})
	}
	st := p.o.svc.Store
	for _, o := range ids {
		name, ok := strings.CutPrefix(o.id, "key:")
		if !ok {
			continue // "cli" (Mensch) oder leer
		}
		k, err := st.APIKeyByName(ctx, name)
		if errors.Is(err, store.ErrKeyNotFound) {
			return denial{"key_invalid", o.content}, nil
		}
		if err != nil {
			return denial{}, err
		}
		switch KeyStatus(k, p.now()) {
		case "revoked":
			return denial{"key_revoked", o.content}, nil
		case "expired":
			return denial{"key_expired", o.content}, nil
		}
		pk := PolicyKey(k)
		if dec := policy.Check(pk, policy.Requirement{Name: drafts.OpPush, Scope: policy.ScopeMailDraft, Risk: policy.R2}, false); !dec.Allowed() {
			return denial{dec.Reason, o.content}, nil
		}
	}
	return denial{}, nil
}

// AfterSync prüft die Entwürfe des Kontos nach dem Spiegeln (S2.6;
// drafts.Service.CheckAfterSync): abgelaufene Freigaben (E7), vom Menschen
// gesendete Entwürfe (E5) und verschwundene oder bearbeitete Kopien im
// Entwürfe-Ordner (E6). Der Sync ruft sie am Ende jedes Laufs auf; o sagt,
// was der Lauf gespiegelt hat (K3).
func (p *DraftPusher) AfterSync(ctx context.Context, accountID int64, o drafts.CheckOptions) (drafts.CheckReport, error) {
	return p.o.svc.CheckAfterSync(ctx, accountID, o)
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
