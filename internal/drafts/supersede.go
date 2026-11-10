package drafts

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachfolger eines Entwurfs (A07 Teil 5; Nachtrag zu E2 und E3 in
// docs/entscheidungen.md, vom Betreiber freigegeben, Board-Karte PK15-3,
// 10.11.2026). Befund: Legt ein Agent eine neue Fassung eines Entwurfs an
// (die alte ist übernommen, oder der Inhalt muss neu sein), bleibt die
// Kopie der alten im Entwürfe-Ordner; drafts.cleanup_copies und
// drafts.abandon darf nur der Mensch. Im Alltag löschte der Agent die alte
// Kopie deshalb über ein fremdes Gmail-Werkzeug, ohne Protokoll, und der
// alte Entwurf blieb in PostKauz offen.
//
// drafts.supersede (R2, Scope mail:draft, kein HumanOnly) schließt den
// alten Entwurf als abandoned (Grund superseded_by_draft, Freigabe
// superseded) und legt in derselben Transaktion eine Postfach-Aktion
// „in den Papierkorb“ (nie endgültig löschen) für seine eigene,
// unveränderte Kopie und frühere eigene Kopien an. Herkunft der Aktion
// draft_supersede: Der Runner verlangt dafür den Scope mail:draft statt
// mail:trash und keinen Antrag, solange jedes Ziel im Entwürfe-Ordner
// liegt; [delete] agent_may = none verbietet sie weiterhin.
//
// Bedingungen, alle zugleich, sonst denied mit eigenem Grund (unten).

// Gründe einer Ablehnung von drafts.supersede.
const (
	ReasonOtherAccount      = "other_account"
	ReasonNotOwnDraft       = "not_own_draft"
	ReasonChangedByOther    = "changed_by_other"
	ReasonSuccessorClosed   = "successor_closed"
	ReasonSuccessorNotNewer = "successor_not_newer"
	ReasonApprovedByHuman   = "approved_by_human"
	ReasonKindMismatch      = "kind_mismatch"
	ReasonOtherOriginal     = "other_original"
	ReasonOtherSubject      = "other_subject"
	ReasonCopyChanged       = "copy_changed"
	ReasonCopyMissing       = "copy_missing"
	ReasonNoTrashMailbox    = "no_trash_mailbox"
	ReasonSameDraft         = "same_draft"
	// ReasonPushInProgress: Beim alten Entwurf läuft noch eine Ablage
	// (Nachtrag 1 zu A07, B4).
	ReasonPushInProgress = "push_in_progress"
)

// OpSupersede ist die Operation (Register und Audit des Abschlusses).
const OpSupersede = "drafts.supersede"

// ReasonSupersededByDraft ist der Grund im Audit, mit dem drafts.supersede
// den alten Entwurf schließt (draft show: closed_reason).
const ReasonSupersededByDraft = "superseded_by_draft"

// SourceDraftSupersede ist die Herkunft der Aktion (store.ActionParams.Source).
const SourceDraftSupersede = store.ActionSourceDraftSupersede

// SupersedeRequest beschreibt einen Aufruf von Supersede.
type SupersedeRequest struct {
	OldID, NewID int64
	// KeyID: "cli" (Mensch) oder "key:<name>"; Human: Mensch ohne
	// Schlüssel (dann entfallen die Bedingungen zu created_by/updated_by).
	KeyID string
	Human bool
	// AuditID: Start-Eintrag des Aufrufs (Herkunft von Aktion und Job).
	AuditID int64
}

// SupersedeResult ist das Ergebnis (im Trockenlauf: was geschähe).
type SupersedeResult struct {
	Old store.Draft
	// Copies: Ziele der Aktion (die Kopie und frühere eigene Kopien).
	Copies int
	// ActionID: die angelegte Aktion (0 im Trockenlauf).
	ActionID int64
	// Trash: der Papierkorb (Pfad), in den die Kopien gehen.
	Trash string
	// Skipped: frühere Kopien außerhalb der Entwürfe-Ordner (bleiben
	// liegen; Nachtrag 1 zu A07, B3).
	Skipped int
}

// Supersede prüft alle Bedingungen in einer Transaktion, legt die Aktion an
// und schließt den alten Entwurf (Audit drafts.supersede, Grund
// superseded_by_draft, mit draft_id, by_draft_id und Zahl der Kopien, ohne
// Inhalt). Im Trockenlauf (As mit dryRun) läuft alles und wird
// zurückgerollt.
func (s *Service) Supersede(ctx context.Context, r SupersedeRequest) (SupersedeResult, error) {
	now := s.now()
	var res SupersedeResult
	if r.OldID == r.NewID {
		return res, denied(ReasonSameDraft, "a draft cannot supersede itself")
	}
	err := s.tx(ctx, func(x *store.DraftTx) error {
		old, err := x.Draft(r.OldID)
		if err != nil {
			return err
		}
		nw, err := x.Draft(r.NewID)
		if err != nil {
			return err
		}
		if old.AccountID != nw.AccountID {
			return denied(ReasonOtherAccount, "drafts %d and %d belong to different accounts", old.ID, nw.ID)
		}
		if !r.Human {
			if old.CreatedBy != r.KeyID || nw.CreatedBy != r.KeyID {
				return denied(ReasonNotOwnDraft, "both drafts must have been created with this key")
			}
			if updatedBy(old) != r.KeyID || updatedBy(nw) != r.KeyID {
				return denied(ReasonChangedByOther, "both drafts must have been changed last with this key")
			}
		}
		if Status(nw.Status).Closed() {
			return denied(ReasonSuccessorClosed, "draft %d is %s; the successor must be open", nw.ID, nw.Status)
		}
		if !nw.CreatedAt.After(old.CreatedAt) {
			return denied(ReasonSuccessorNotNewer, "draft %d is not newer than draft %d", nw.ID, old.ID)
		}
		switch Status(old.Status) {
		case StatusDraft, StatusPendingApproval:
		case StatusReady:
			return denied(ReasonApprovedByHuman, "draft %d was approved by the human (ready); only the human closes it", old.ID)
		default:
			return denied(ReasonDraftClosed, "draft %d is already %s", old.ID, old.Status)
		}
		if old.Kind != nw.Kind {
			return denied(ReasonKindMismatch, "drafts %d and %d are of different kinds (%s, %s)", old.ID, nw.ID, old.Kind, nw.Kind)
		}
		if old.Kind == string(KindNew) {
			if s := NormalizeSubject(old.Subject); s == "" || s != NormalizeSubject(nw.Subject) {
				return denied(ReasonOtherSubject, "drafts %d and %d do not have the same subject", old.ID, nw.ID)
			}
		} else if old.InReplyToMsg <= 0 || old.InReplyToMsg != nw.InReplyToMsg {
			return denied(ReasonOtherOriginal, "drafts %d and %d do not answer or forward the same message", old.ID, nw.ID)
		}
		// Die Kopie des alten: unverändert unter Ordner, UID und UIDVALIDITY
		// mit dem Inhalts-Hash der Ablage (wie cleanup_copies, A14); nie
		// über die Message-ID.
		// Nachtrag 1 zu A07, B4: Läuft noch eine Ablage (offener Versuch
		// oder eine neue Kopie wartet), ist unklar, welche Kopie im Ordner
		// liegt; dann nicht.
		if old.PushAttempt != nil || old.RemoteState == RemotePending {
			return denied(ReasonPushInProgress, "a copy of draft %d is being put into the Drafts folder; try again after the next sync", old.ID)
		}
		switch {
		case old.RemoteState == RemoteTakenOver:
			return denied(ReasonCopyChanged, "the copy of draft %d was changed in the mail client; only the human removes it", old.ID)
		case old.PushedContentHash == "" || old.RemoteMailboxID == 0 || old.RemoteUID <= 0 || old.RemoteUIDValidity == 0 ||
			old.RemoteState == RemoteMissing:
			return denied(ReasonCopyMissing, "draft %d has no copy in the Drafts folder (it may have been sent; the sync closes it then)", old.ID)
		}
		c, hash, ok, err := x.CopyAt(old.RemoteMailboxID, uint32(old.RemoteUID))
		if err != nil {
			return err
		}
		if !ok {
			return denied(ReasonCopyMissing, "the copy of draft %d is not in the mirror of the Drafts folder (it may have been sent)", old.ID)
		}
		if c.UIDValidity != old.RemoteUIDValidity || hash != old.PushedContentHash {
			return denied(ReasonCopyChanged, "the copy of draft %d is not the one PostKauz put there; only the human removes it", old.ID)
		}
		targets := []store.ActionTarget{{MessageID: c.MessageID, MailboxID: c.MailboxID, UID: c.UID, UIDValidity: c.UIDValidity,
			Mailbox: c.Mailbox, Role: c.Role}}
		ids := []int64{c.MessageID}
		// Nachtrag 1 zu A07, B3: Die Ausnahme von E3 gilt nur innerhalb der
		// Entwürfe-Ordner; eine frühere Kopie anderswo (etwa nach einem
		// Verschieben ins Archiv) wird übersprungen und gezählt.
		inDrafts, err := x.DraftMailboxIDs(old.AccountID)
		if err != nil {
			return err
		}
		for _, p := range old.RemotePrevious {
			if p.UIDValidity == 0 || p.UID <= 0 || p.ContentHash == "" {
				continue
			}
			if !inDrafts[p.MailboxID] {
				res.Skipped++
				continue
			}
			pc, ph, ok, err := x.CopyAt(p.MailboxID, uint32(p.UID))
			if err != nil {
				return err
			}
			if !ok || pc.UIDValidity != p.UIDValidity || ph != p.ContentHash {
				continue
			}
			targets = append(targets, store.ActionTarget{MessageID: pc.MessageID, MailboxID: pc.MailboxID, UID: pc.UID,
				UIDValidity: pc.UIDValidity, Mailbox: pc.Mailbox, Role: pc.Role})
			ids = append(ids, pc.MessageID)
		}
		trash, ok, err := x.TrashMailbox(old.AccountID)
		if err != nil {
			return err
		}
		if !ok {
			return denied(ReasonNoTrashMailbox, "the account has no mailbox with role trash in the mirror; run sync first")
		}
		res.Copies, res.Trash = len(targets), trash.Path
		actionID, err := x.CreateQueuedAction(store.NewAction{AccountID: old.AccountID, Kind: store.ActionTrash,
			Params:  store.ActionParams{TargetMailboxID: trash.ID, TargetMailbox: trash.Path, TargetRole: trash.Role, Source: SourceDraftSupersede},
			IDsHash: actions.IDsHash(ids), MessageCount: len(ids), RequestedBy: r.KeyID, AuditID: r.AuditID, Targets: targets, Now: now})
		if err != nil {
			return err
		}
		from := Status(old.Status)
		t, err := Next(from, EvAbandon)
		if err != nil {
			return err
		}
		if _, err := applyApproval(x, &old, t.Approval, now, "", ""); err != nil {
			return err
		}
		old.Status, old.UpdatedAt, old.ClosedAt = string(t.To), now, now
		if err := x.UpdateDraft(old); err != nil {
			return err
		}
		if !s.dry {
			res.ActionID = actionID
		}
		res.Old = old
		params := map[string]any{"draft_id": old.ID, "by_draft_id": nw.ID, "copies": len(targets), "skipped": res.Skipped, "action_id": actionID,
			"from": from, "to": t.To}
		targetsAudit := []string{"draft:" + strconv.FormatInt(old.ID, 10), "draft:" + strconv.FormatInt(nw.ID, 10),
			"action:" + strconv.FormatInt(actionID, 10)}
		if old.ApprovalID > 0 {
			params["approval_id"] = old.ApprovalID
			targetsAudit = append(targetsAudit, "approval:"+strconv.FormatInt(old.ApprovalID, 10))
		}
		b, _ := json.Marshal(params)
		ev := audit.Event{TS: now, ActorType: audit.ActorAgent, KeyID: r.KeyID, Scope: "mail:draft", Operation: OpSupersede,
			ParamsRedacted: b, Targets: targetsAudit, Result: audit.ResultOK, Reason: ReasonSupersededByDraft, PolicyVersion: s.PolicyVersion}
		if r.Human {
			ev.ActorType = audit.ActorHuman
		}
		if s.who != nil {
			ev.ActorType, ev.KeyID = s.who.Actor, s.who.KeyID
		}
		_, err = x.AppendAudit(ev)
		return err
	})
	return res, err
}

// updatedBy ist, wer den Inhalt zuletzt geändert hat (Bestand ohne
// updated_by: created_by).
func updatedBy(d store.Draft) string {
	if d.UpdatedBy == "" {
		return d.CreatedBy
	}
	return d.UpdatedBy
}
