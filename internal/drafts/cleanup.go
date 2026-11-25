package drafts

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Eigene Entwurfskopien aufräumen (A11 Teil 1; Nachtrag zu ADR-009 / E2
// und E3 in docs/entscheidungen.md, vom Betreiber beim Start von A11
// freigegeben). Befund B1: Nach drafts.update legte der Push eine neue
// Kopie ab, die alte blieb im Entwürfe-Ordner, und der Mensch konnte die
// alte Fassung senden. B4: Eine gescheiterte Papierkorb-Aktion ließ die
// Kopie eines ersetzten Entwurfs für immer liegen.
//
// Eine eigene, unveränderte Kopie eines Entwurfs ist eine Kopie, die
// PostKauz für diesen Entwurf abgelegt hat (die aktuelle oder ein Eintrag
// aus remote_previous), die der Spiegel unter genau diesem Ordner, dieser
// UID und UIDVALIDITY mit genau diesem Inhalts-Hash führt (CopyAt) und
// deren Ordner ein Entwürfe-Ordner ist (DraftMailboxIDs). Nie über die
// Message-ID. classifyCopy ist diese Bedingung an einer Stelle; supersede,
// discard, das Aufräumen nach dem Push und der Nachlauf benutzen sie.
// Kopien, für die schon eine Aktion wartet oder läuft (store.CopyBusy),
// werden übersprungen und als skipped gezählt.
//
// Die Aktion hat die Art trash und eine Herkunft eines Entwurfs
// (store.IsDraftSource): Der Runner verlangt mail:draft statt mail:trash
// und keinen Antrag, solange jedes Ziel in einem Entwürfe-Ordner liegt;
// [delete] agent_may = none verbietet sie; ist ein Ziel stale, wird nichts
// verschoben. Ihr Entwurf steht als draft_id in actions.params.

// Werte von [draft] previous_copies.
const (
	// PreviousCopiesTrash (Standard): frühere eigene, unveränderte Kopien
	// gehen nach einer Änderung in den Papierkorb.
	PreviousCopiesTrash = "trash"
	// PreviousCopiesKeep: sie bleiben liegen (Verhalten bis 0.3.0-rc8).
	PreviousCopiesKeep = "keep"
)

// OpCleanupQueued ist das Systemereignis, wenn das Aufräumen nach dem Push
// oder der Nachlauf eine Aktion anlegt (Ergebnis ok) oder nicht anlegen
// darf (Ergebnis denied, Grund der Policy).
const OpCleanupQueued = "drafts.cleanup_queued"

// Herkünfte der Aktionen (store.ActionParams.Source).
const (
	SourceDraftUpdate  = store.ActionSourceDraftUpdate
	SourceDraftDiscard = store.ActionSourceDraftDiscard
)

// CleanupMaxAttempts: so viele Aktionen legen die Wege dieses Auftrags
// höchstens je Kopie an (der erste Versuch zählt mit); danach bleibt die
// Kopie liegen, und closed_note bzw. remote_note nennen sie.
const CleanupMaxAttempts = 3

// CleanupWindow: Der Nachlauf prüft geschlossene Entwürfe so lange nach
// dem Schließen; Zähler älter als das werden gelöscht.
const CleanupWindow = 7 * 24 * time.Hour

// CleanupCheck prüft vor dem Anlegen einer Aktion den Schlüssel keyID
// (gilt er noch, hat er mail:draft) und die Regeln der Policy für copies
// Kopien in den Papierkorb ([delete] agent_may): "" = erlaubt, sonst der
// Grund (key_revoked, policy_forbids, …). Wird außerhalb einer Transaktion
// aufgerufen.
type CleanupCheck func(ctx context.Context, keyID string, copies int) (string, error)

// Einordnung einer Kopie (classifyCopy).
type copyClass int

const (
	copyOwn         copyClass = iota // eigene, unveränderte Kopie im Entwürfe-Ordner, keine Aktion wartet
	copyInvalid                      // ohne Ordner, UID, UIDVALIDITY oder Hash (Bestand)
	copyOutside                      // nicht in einem Entwürfe-Ordner
	copyNotMirrored                  // der Spiegel hat die UID noch nicht gesehen
	copyGone                         // nicht mehr unter Ordner und UID, oder UIDVALIDITY anders
	copyChanged                      // unter Ordner und UID mit anderem Inhalt
	copyBusy                         // eine Aktion wartet oder läuft schon
)

// classifyCopy ordnet eine abgelegte Kopie rc des Entwurfs d ein; bei
// copyOwn ist sc die Serverkopie im Spiegel. inDrafts: Entwürfe-Ordner des
// Kontos.
//
// A12 Teil 1 (d) (iii): Liegt die Kopie nicht mehr unter ihrer UID (der
// Spiegel hat die UID gesehen, UIDVALIDITY gleich), aber im selben Ordner
// genau eine geladene Kopie mit der Message-ID des Entwurfs und exakt
// ihrem Inhalts-Hash unter einer UID, die keine bekannte Kopie des
// Entwurfs ist (Store.MirrorMoved), ist sie umgezogen: eingeordnet wird
// dann die neue Stelle, und moved ist die nachgeführte Kopie (sonst die
// Nullkopie).
func classifyCopy(x *store.DraftTx, d store.Draft, inDrafts map[int64]bool, rc store.RemoteCopy) (copyClass, store.ServerCopy, store.RemoteCopy, error) {
	cl, c, err := classifyAt(x, inDrafts, rc)
	if err != nil || cl != copyGone || d.MessageIDHeader == "" {
		return cl, c, store.RemoteCopy{}, err
	}
	cur, err := x.MailboxCursor(rc.MailboxID)
	if err != nil || !cur.Found || cur.UIDValidity != rc.UIDValidity {
		return cl, c, store.RemoteCopy{}, err
	}
	known := []int64{}
	if d.RemoteMailboxID == rc.MailboxID && d.RemoteUID > 0 {
		known = append(known, d.RemoteUID)
	}
	for _, p := range d.RemotePrevious {
		if p.MailboxID == rc.MailboxID {
			known = append(known, p.UID)
		}
	}
	uid, uv, err := x.MirrorMoved(rc.MailboxID, d.MessageIDHeader, rc.ContentHash, known)
	if err != nil || uid == 0 || uv != rc.UIDValidity {
		return cl, c, store.RemoteCopy{}, err
	}
	moved := store.RemoteCopy{MailboxID: rc.MailboxID, UID: uid, UIDValidity: uv, ContentHash: rc.ContentHash}
	cl, c, err = classifyAt(x, inDrafts, moved)
	return cl, c, moved, err
}

// classifyAt ist classifyCopy ohne Umzug: die Kopie genau unter Ordner,
// UID und UIDVALIDITY.
func classifyAt(x *store.DraftTx, inDrafts map[int64]bool, rc store.RemoteCopy) (copyClass, store.ServerCopy, error) {
	if rc.MailboxID <= 0 || rc.UID <= 0 || rc.UIDValidity == 0 || rc.ContentHash == "" {
		return copyInvalid, store.ServerCopy{}, nil
	}
	if !inDrafts[rc.MailboxID] {
		return copyOutside, store.ServerCopy{}, nil
	}
	c, hash, ok, err := x.CopyAt(rc.MailboxID, uint32(rc.UID))
	if err != nil {
		return 0, c, err
	}
	if !ok {
		cur, err := x.MailboxCursor(rc.MailboxID)
		if err != nil {
			return 0, c, err
		}
		if cur.Found && cur.UIDValidity == rc.UIDValidity && (cur.UIDNext == 0 || int64(cur.UIDNext) <= rc.UID) {
			return copyNotMirrored, c, nil
		}
		return copyGone, c, nil
	}
	if c.UIDValidity != rc.UIDValidity {
		return copyGone, c, nil
	}
	if hash != rc.ContentHash {
		return copyChanged, c, nil
	}
	busy, err := x.CopyBusy(rc.MailboxID, rc.UID, rc.UIDValidity)
	if err != nil {
		return 0, c, err
	}
	if busy {
		return copyBusy, c, nil
	}
	return copyOwn, c, nil
}

// currentCopy ist die aktuelle Kopie eines Entwurfs als RemoteCopy.
func currentCopy(d store.Draft) store.RemoteCopy {
	return store.RemoteCopy{MailboxID: d.RemoteMailboxID, UID: d.RemoteUID, UIDValidity: d.RemoteUIDValidity, ContentHash: d.PushedContentHash}
}

func sameCopy(a, b store.RemoteCopy) bool {
	return a.MailboxID == b.MailboxID && a.UID == b.UID && a.UIDValidity == b.UIDValidity
}

// copySet sind die Ziele einer Aktion und was übersprungen wurde.
type copySet struct {
	targets []store.ActionTarget
	ids     []int64
	copies  []store.RemoteCopy // je Ziel die abgelegte Kopie (Zähler)
	// skipped: außerhalb der Entwürfe-Ordner, im Mailprogramm geändert,
	// mit wartender Aktion oder (Nachlauf) ohne Versuche mehr.
	skipped int
	// unmirrored: noch nicht gespiegelt (holt der Nachlauf nach).
	unmirrored int
	// exhausted: Kopien mit CleanupMaxAttempts Versuchen.
	exhausted int
	// moves: umgezogene Kopien (A12 Teil 1 (d) (iii)), alt → neu; die
	// schreibenden Wege führen sie am Entwurf nach (recordMoves).
	moves [][2]store.RemoteCopy
}

func (cs *copySet) add(rc store.RemoteCopy, c store.ServerCopy) {
	cs.targets = append(cs.targets, store.ActionTarget{MessageID: c.MessageID, MailboxID: c.MailboxID, UID: c.UID,
		UIDValidity: c.UIDValidity, Mailbox: c.Mailbox, Role: c.Role})
	cs.ids = append(cs.ids, c.MessageID)
	cs.copies = append(cs.copies, rc)
}

// collect sammelt die eigenen, unveränderten Kopien aus list (ohne
// Dubletten und ohne except). limit: Kopien mit CleanupMaxAttempts
// Versuchen auslassen (Nachlauf).
func collect(x *store.DraftTx, d store.Draft, inDrafts map[int64]bool, list []store.RemoteCopy, except []store.RemoteCopy, limit bool, cs *copySet) error {
	seen := append([]store.RemoteCopy{}, except...)
	for _, rc := range list {
		dup := false
		for _, s := range seen {
			if sameCopy(s, rc) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		seen = append(seen, rc)
		cl, c, moved, err := classifyCopy(x, d, inDrafts, rc)
		if err != nil {
			return err
		}
		if moved.UID > 0 {
			cs.moves = append(cs.moves, [2]store.RemoteCopy{rc, moved})
			rc = moved
		}
		switch cl {
		case copyOwn:
			if limit {
				n, ok, err := x.CleanupCountOf(rc)
				if err != nil {
					return err
				}
				if ok && n.N >= CleanupMaxAttempts {
					cs.exhausted++
					cs.skipped++
					continue
				}
			}
			cs.add(rc, c)
		case copyOutside, copyChanged, copyBusy:
			cs.skipped++
		case copyNotMirrored:
			cs.unmirrored++
		}
	}
	return nil
}

// queueCleanup legt in der Transaktion die Aktion trash für cs an
// (Herkunft source, draft_id d.ID) und zählt je Ziel einen Versuch. Ohne
// Papierkorb im Spiegel entsteht keine Aktion (0). Ohne Ziele ebenso.
func (s *Service) queueCleanup(x *store.DraftTx, d store.Draft, source, requestedBy string, auditID int64, cs copySet, now time.Time) (int64, store.Mailbox, error) {
	if len(cs.targets) == 0 {
		return 0, store.Mailbox{}, nil
	}
	trash, ok, err := x.TrashMailbox(d.AccountID)
	if err != nil || !ok {
		return 0, trash, err
	}
	id, err := x.CreateQueuedAction(store.NewAction{AccountID: d.AccountID, Kind: store.ActionTrash,
		Params: store.ActionParams{TargetMailboxID: trash.ID, TargetMailbox: trash.Path, TargetRole: trash.Role, Source: source,
			DraftID: d.ID},
		IDsHash: actions.IDsHash(cs.ids), MessageCount: len(cs.ids), RequestedBy: requestedBy, AuditID: auditID, Targets: cs.targets, Now: now})
	if err != nil {
		return 0, trash, err
	}
	for _, rc := range cs.copies {
		if err := x.BumpCleanupCount(rc, now); err != nil {
			return 0, trash, err
		}
	}
	return id, trash, nil
}

// cleanupEvent ist das Systemereignis drafts.cleanup_queued (ohne Inhalt).
func (s *Service) cleanupEvent(now time.Time, d store.Draft, source string, actionID int64, copies, skipped int, res audit.Result, reason string) audit.Event {
	p := map[string]any{"draft_id": d.ID, "copies": copies, "skipped": skipped, "source": source}
	targets := []string{"draft:" + strconv.FormatInt(d.ID, 10)}
	if actionID > 0 {
		p["action_id"] = actionID
		targets = append(targets, "action:"+strconv.FormatInt(actionID, 10))
	}
	b, _ := json.Marshal(p)
	if reason == "" {
		reason = source
	}
	return audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Scope: "mail:draft", Operation: OpCleanupQueued,
		ParamsRedacted: b, Targets: targets, Result: res, Reason: reason, PolicyVersion: s.PolicyVersion}
}

// keepPrevious: [draft] previous_copies = "keep".
func (s *Service) keepPrevious() bool { return s.PreviousCopies == PreviousCopiesKeep }

// cleanupDenied fragt vor der Transaktion CleanupCheck (nil = erlaubt).
func (s *Service) cleanupDenied(ctx context.Context, keyID string, copies int) (string, error) {
	if s.CleanupCheck == nil || copies <= 0 {
		return "", nil
	}
	return s.CleanupCheck(ctx, keyID, copies)
}

// updateCleanupApplies meldet, ob die früheren Kopien eines offenen
// Entwurfs nach einer Änderung in den Papierkorb dürfen (A11 Teil 1 (c)):
// Status draft oder pending_approval (nie ready), Kopie aktuell (pushed),
// nicht geplant, und die letzte Inhaltsänderung stammt von dem, der ihn
// angelegt hat.
func updateCleanupApplies(x *store.DraftTx, d store.Draft) (bool, error) {
	switch Status(d.Status) {
	case StatusDraft, StatusPendingApproval:
	default:
		return false, nil
	}
	if d.RemoteState != RemotePushed || updatedBy(d) != d.CreatedBy || d.CreatedBy == "" {
		return false, nil
	}
	sc, ok, err := x.ScheduledOf(d.ID)
	if err != nil {
		return false, err
	}
	return !ScheduledActive(sc, ok), nil
}

// cleanupAfterPush legt nach dem Vermerk einer neuen, aktuellen Kopie in
// derselben Transaktion die Aktion draft_update für die früheren eigenen,
// unveränderten Kopien an (A11 Teil 1 (c)); deny ist das Ergebnis von
// cleanupDenied vor der Transaktion. Die gerade abgelegte Kopie ist nie
// Ziel. Was noch nicht gespiegelt ist, holt der Nachlauf nach.
func (s *Service) cleanupAfterPush(x *store.DraftTx, d store.Draft, deny string, now time.Time) error {
	if s.keepPrevious() || len(d.RemotePrevious) == 0 {
		return nil
	}
	ok, err := updateCleanupApplies(x, d)
	if err != nil || !ok {
		return err
	}
	inDrafts, err := x.DraftMailboxIDs(d.AccountID)
	if err != nil {
		return err
	}
	var cs copySet
	if err := collect(x, d, inDrafts, d.RemotePrevious, []store.RemoteCopy{currentCopy(d)}, true, &cs); err != nil {
		return err
	}
	if err := s.recordMoves(x, &d, cs.moves, now); err != nil {
		return err
	}
	if len(cs.targets) == 0 {
		return nil
	}
	if deny != "" {
		// [delete] agent_may = none oder Schlüssel ungültig: keine Aktion;
		// das Audit nennt den Grund. Der Versuch zählt (sonst stünde der
		// Eintrag in jedem Lauf des Nachlaufs erneut im Audit).
		for _, rc := range cs.copies {
			if err := x.BumpCleanupCount(rc, now); err != nil {
				return err
			}
		}
		_, err := x.AppendAudit(s.cleanupEvent(now, d, SourceDraftUpdate, 0, len(cs.targets), cs.skipped, audit.ResultDenied, deny))
		return err
	}
	id, _, err := s.queueCleanup(x, d, SourceDraftUpdate, d.CreatedBy, 0, cs, now)
	if err != nil || id == 0 {
		return err
	}
	_, err = x.AppendAudit(s.cleanupEvent(now, d, SourceDraftUpdate, id, len(cs.targets), cs.skipped, audit.ResultOK, ""))
	return err
}

// CleanupReport zählt, was der Nachlauf angelegt hat.
type CleanupReport struct {
	Actions int
	Copies  int
}

// RetryCleanup ist der Nachlauf (A11 Teil 1 (d)), nach der Prüfung der
// Entwürfe in jedem vollständigen Lauf eines Kontos: (i) offene Entwürfe
// mit früheren Kopien, für die die Bedingungen von (c) gelten; (ii)
// Entwürfe, die vor höchstens CleanupWindow mit superseded_by_draft oder
// discarded geschlossen wurden und von denen noch eine eigene, unveränderte
// Kopie im Entwürfe-Ordner liegt. Je Kopie ohne wartende Aktion höchstens
// CleanupMaxAttempts Versuche (Zähler in meta), jeder mit Audit
// drafts.cleanup_queued. Zuvor räumt er die Zähler auf (Erfolg, älter als
// CleanupWindow).
func (s *Service) RetryCleanup(ctx context.Context, accountID int64) (CleanupReport, error) {
	var rep CleanupReport
	now := s.now()
	if err := s.sweepCleanupCounts(ctx, now); err != nil {
		return rep, err
	}
	open, closed, err := s.Store.CleanupCandidates(ctx, accountID, now.Add(-CleanupWindow))
	if err != nil {
		return rep, err
	}
	if !s.keepPrevious() {
		for _, d := range open {
			if err := s.retryOne(ctx, d, SourceDraftUpdate, d.CreatedBy, false, &rep); err != nil {
				return rep, err
			}
		}
	}
	for _, d := range closed {
		ev, ok, err := s.Store.DraftClosedBy(ctx, d.ID, d.ClosedAt)
		if err != nil {
			return rep, err
		}
		if !ok {
			continue
		}
		var source string
		switch ev.Reason {
		case ReasonSupersededByDraft:
			source = SourceDraftSupersede
		case ReasonDiscarded:
			source = SourceDraftDiscard
		default:
			continue
		}
		by := ev.KeyID
		if by == "" || by == "system" {
			by = d.CreatedBy
		}
		if err := s.retryOne(ctx, d, source, by, true, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// retryOne legt für einen Entwurf des Nachlaufs die Aktion an (withCurrent:
// auch die aktuelle Kopie, bei geschlossenen Entwürfen).
func (s *Service) retryOne(ctx context.Context, pre store.Draft, source, by string, withCurrent bool, rep *CleanupReport) error {
	deny, err := s.cleanupDenied(ctx, by, len(pre.RemotePrevious)+1)
	if err != nil {
		return err
	}
	now := s.now()
	return s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(pre.ID)
		if err != nil {
			return err
		}
		list := d.RemotePrevious
		var except []store.RemoteCopy
		if withCurrent {
			if !Status(d.Status).Closed() {
				return nil
			}
			list = append([]store.RemoteCopy{currentCopy(d)}, d.RemotePrevious...)
		} else {
			ok, err := updateCleanupApplies(x, d)
			if err != nil || !ok {
				return err
			}
			except = []store.RemoteCopy{currentCopy(d)}
		}
		inDrafts, err := x.DraftMailboxIDs(d.AccountID)
		if err != nil {
			return err
		}
		var cs copySet
		if err := collect(x, d, inDrafts, list, except, true, &cs); err != nil {
			return err
		}
		if err := s.recordMoves(x, &d, cs.moves, now); err != nil {
			return err
		}
		if len(cs.targets) == 0 {
			return nil
		}
		if deny != "" {
			for _, rc := range cs.copies {
				if err := x.BumpCleanupCount(rc, now); err != nil {
					return err
				}
			}
			_, err := x.AppendAudit(s.cleanupEvent(now, d, source, 0, len(cs.targets), cs.skipped, audit.ResultDenied, deny))
			return err
		}
		id, _, err := s.queueCleanup(x, d, source, by, 0, cs, now)
		if err != nil || id == 0 {
			return err
		}
		rep.Actions++
		rep.Copies += len(cs.targets)
		_, err = x.AppendAudit(s.cleanupEvent(now, d, source, id, len(cs.targets), cs.skipped, audit.ResultOK, ""))
		return err
	})
}

// sweepCleanupCounts löscht Zähler nach dem Erfolg (das Ziel der jüngsten
// Aktion auf die Kopie ist ok) oder wenn der erste Versuch länger als
// CleanupWindow zurückliegt.
//
// A12 Teil 0 (c): Führt ein offener Entwurf die Kopie als aktuelle oder
// frühere, bleibt der Zähler bis zum Erfolg oder bis die Kopie laut Spiegel
// weg ist (der Nachlauf für offene Entwürfe hat kein Zeitfenster; sonst
// begänne er alle sieben Tage mit drei neuen Versuchen). Gehört sie zu
// einem in den letzten CleanupWindow geschlossenen Entwurf, bleibt er
// ebenso (Nachlauf (ii)). Nach drei Fehlschlägen wiederholt dann nichts
// mehr automatisch; der Mensch räumt mit draft cleanup auf.
func (s *Service) sweepCleanupCounts(ctx context.Context, now time.Time) error {
	list, err := s.Store.CleanupCounts(ctx)
	if err != nil || len(list) == 0 {
		return err
	}
	open, recent, err := s.Store.HeldCopies(ctx, now.Add(-CleanupWindow))
	if err != nil {
		return err
	}
	for _, n := range list {
		key := store.RemoteCopy{MailboxID: n.Copy.MailboxID, UIDValidity: n.Copy.UIDValidity, UID: n.Copy.UID}
		var drop bool
		switch {
		case n.N <= 0:
			drop = true
		case open[key]:
			if drop, err = s.copyGoneFromMirror(ctx, key); err != nil {
				return err
			}
		case recent[key]:
		default:
			drop = now.Sub(n.First) > CleanupWindow
		}
		if !drop {
			_, _, result, ok, err := s.Store.LastCopyResult(ctx, n.Copy)
			if err != nil {
				return err
			}
			drop = ok && result == store.TargetOK
		}
		if drop {
			if err := s.Store.DeleteCleanupCount(ctx, n.Copy); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyGoneFromMirror meldet, ob der Spiegel die Kopie gesehen hat und
// nicht mehr führt (unter Ordner und UID fehlt sie, UIDVALIDITY gleich und
// UID unter UIDNEXT, oder die UIDVALIDITY ist eine andere).
func (s *Service) copyGoneFromMirror(ctx context.Context, rc store.RemoteCopy) (bool, error) {
	c, _, ok, err := s.Store.CopyAt(ctx, rc.MailboxID, uint32(rc.UID))
	if err != nil {
		return false, err
	}
	if ok {
		return c.UIDValidity != rc.UIDValidity, nil
	}
	cur, err := s.Store.MailboxCursor(ctx, rc.MailboxID)
	if err != nil {
		return false, err
	}
	return !cur.Found || cur.UIDValidity != rc.UIDValidity || (cur.UIDNext != 0 && int64(cur.UIDNext) > rc.UID), nil
}

// ownCopiesFor prüft die aktuelle Kopie eines Entwurfs für supersede und
// discard und sammelt dazu die früheren eigenen Kopien (A11 Teil 1 (b)).
// Ablehnungen: copy_missing (keine abgelegte Kopie, remote missing,
// UIDVALIDITY anders oder der Spiegel hat die UID gesehen und führt sie
// nicht mehr), copy_not_mirrored (der Spiegel hat die UID noch nicht
// gesehen), copy_changed (unter Ordner und UID anderer Inhalt). Wartet für
// die aktuelle Kopie schon eine Aktion, zählt sie als übersprungen.
func ownCopiesFor(x *store.DraftTx, d store.Draft) (copySet, error) {
	var cs copySet
	name := "draft " + strconv.FormatInt(d.ID, 10)
	cur := currentCopy(d)
	if d.RemoteState == RemoteMissing || d.PushedContentHash == "" || d.RemoteMailboxID == 0 || d.RemoteUID <= 0 || d.RemoteUIDValidity == 0 {
		return cs, denied(ReasonCopyMissing, "%s has no copy in the Drafts folder (it may have been sent; the sync closes it then)", name)
	}
	inDrafts, err := x.DraftMailboxIDs(d.AccountID)
	if err != nil {
		return cs, err
	}
	cl, c, err := classifyAt(x, inDrafts, cur)
	if err != nil {
		return cs, err
	}
	switch cl {
	case copyOwn:
		cs.add(cur, c)
	case copyBusy:
		cs.skipped++
	case copyNotMirrored:
		return cs, denied(ReasonCopyNotMirrored, "the copy of %s is in the Drafts folder but not mirrored yet; try again in a few seconds", name)
	case copyChanged:
		return cs, denied(ReasonCopyChanged, "the copy of %s is not the one PostKauz put there; only the human removes it", name)
	default:
		return cs, denied(ReasonCopyMissing, "the copy of %s is no longer in the mirror of the Drafts folder (it may have been sent, "+
			"or changed in the mail client); try again after the next sync", name)
	}
	return cs, collect(x, d, inDrafts, d.RemotePrevious, []store.RemoteCopy{cur}, false, &cs)
}

// recordMoves führt umgezogene Kopien am Entwurf nach (A12 Teil 1 (d)
// (iii)), in der Transaktion des Aufrufers: den Eintrag in remote_previous
// (bzw. bei einem geschlossenen Entwurf auch die aktuelle Kopie) auf die
// neue UID, je Kopie Audit drafts.remote_check, Grund moved, mit previous
// (ohne Inhalt). Der Zähler der neuen Stelle beginnt neu (kein
// Fehlversuch).
func (s *Service) recordMoves(x *store.DraftTx, d *store.Draft, moves [][2]store.RemoteCopy, now time.Time) error {
	if len(moves) == 0 {
		return nil
	}
	for _, mv := range moves {
		old, nw := mv[0], mv[1]
		if sameCopy(old, currentCopy(*d)) {
			d.RemoteUID, d.RemoteUIDValidity = nw.UID, nw.UIDValidity
		}
		for i := range d.RemotePrevious {
			if sameCopy(d.RemotePrevious[i], old) {
				d.RemotePrevious[i].UID, d.RemotePrevious[i].UIDValidity = nw.UID, nw.UIDValidity
			}
		}
	}
	if err := x.UpdateDraft(*d); err != nil {
		return err
	}
	for _, mv := range moves {
		if err := s.remoteEvent(x, *d, ReasonMoved, now, map[string]any{"via": ViaMoved, "previous": true, "mailbox_id": mv[0].MailboxID,
			"old_uid": mv[0].UID, "new_uid": mv[1].UID, "uid": mv[1].UID, "loaded": true, "remote": d.RemoteState}); err != nil {
			return err
		}
	}
	return nil
}
