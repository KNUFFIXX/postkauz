package drafts

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Eigenen Entwurf verwerfen (A11 Teil 2; Nachtrag zu ADR-009 / E2 in
// docs/entscheidungen.md, vom Betreiber beim Start von A11 freigegeben).
// Befund B3: drafts.abandon ist HumanOnly. Brauchte niemand den Entwurf
// mehr, löschte der Agent die Kopie über ein fremdes Werkzeug, und der
// Entwurf blieb bis copy_gone_after offen (remote missing) und stand so
// lange in mail_list_drafts.
//
// drafts.discard (R2, Scope mail:draft, kein HumanOnly) schließt einen
// eigenen Entwurf als abandoned (Grund discarded, Freigabe wie bei
// supersede) und legt in derselben Transaktion seine eigene, unveränderte
// Kopie und frühere eigene Kopien als Aktion trash mit Herkunft
// draft_discard in den Papierkorb. Nie als gesendet; nie einen Entwurf, den
// der Mensch freigegeben, geplant oder im Mailprogramm geändert hat.

// OpDiscard ist die Operation (Register und Audit des Abschlusses).
const OpDiscard = "drafts.discard"

// DiscardRequest beschreibt einen Aufruf von Discard.
type DiscardRequest struct {
	ID int64
	// KeyID: "cli" (Mensch) oder "key:<name>"; Human: Mensch ohne
	// Schlüssel (dann entfallen die Bedingungen zu created_by/updated_by).
	KeyID string
	Human bool
	// AuditID: Start-Eintrag des Aufrufs (Herkunft von Aktion und Job).
	AuditID int64
}

// DiscardResult ist das Ergebnis (im Trockenlauf: was geschähe).
type DiscardResult struct {
	Draft store.Draft
	// Copies: Ziele der Aktion (die Kopie und frühere eigene Kopien); 0,
	// wenn der Entwurf keine Kopie hat (nie abgelegt, missing).
	Copies int
	// Skipped: eigene Kopien, die liegen bleiben (außerhalb der
	// Entwürfe-Ordner, geändert) oder schon in einer wartenden Aktion
	// stehen.
	Skipped int
	// ActionID: die angelegte Aktion (0 im Trockenlauf oder ohne Ziel).
	ActionID int64
	// Trash: der Papierkorb (Pfad), in den die Kopien gehen.
	Trash string
}

// Discard prüft alle Bedingungen in einer Transaktion, legt die Aktion an
// und schließt den Entwurf (Audit drafts.discard, Grund discarded, mit
// draft_id und Zahl der Kopien, ohne Inhalt). Im Trockenlauf läuft alles
// und wird zurückgerollt. Reihenfolge der Bedingungen: Schlüssel, geplant,
// Status, dann der Stand der Kopie (laufende Ablage, nie abgelegt,
// übernommen, fehlt, abgelegt).
func (s *Service) Discard(ctx context.Context, r DiscardRequest) (DiscardResult, error) {
	now := s.now()
	var res DiscardResult
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(r.ID)
		if err != nil {
			return err
		}
		if !r.Human {
			if d.CreatedBy != r.KeyID {
				return denied(ReasonNotOwnDraft, "draft %d was not created with this key", d.ID)
			}
			if updatedBy(d) != r.KeyID {
				return denied(ReasonChangedByOther, "draft %d was changed last by someone else; only the human closes it", d.ID)
			}
		}
		// A10: geplant hat Vorrang (wie bei supersede).
		if !Status(d.Status).Closed() {
			if err := checkScheduled(x, d.ID); err != nil {
				return err
			}
		}
		switch Status(d.Status) {
		case StatusDraft, StatusPendingApproval:
		case StatusReady:
			return denied(ReasonApprovedByHuman, "draft %d was approved by the human (ready); only the human closes it (draft abandon)", d.ID)
		default:
			return denied(ReasonDraftClosed, "draft %d is already %s", d.ID, d.Status)
		}
		var cs copySet
		withAction := false
		hasCopy := d.RemoteUID > 0 || len(d.RemotePrevious) > 0
		switch {
		case d.PushAttempt != nil || (d.RemoteState == RemotePending && hasCopy):
			if d.PushNote == PushNotePushRejected {
				return denied(ReasonPushInProgress, "the mail server refused to store the changed draft %d in the Drafts folder; "+
					"only the human closes it (draft abandon)", d.ID)
			}
			return denied(ReasonPushInProgress, "a copy of draft %d is being put into the Drafts folder; try again after the next sync", d.ID)
		case d.RemoteState == RemotePending:
			// Nie abgelegt: schließen ohne Aktion; der Push lässt
			// geschlossene Entwürfe aus (Pending).
		case d.RemoteState == RemoteTakenOver:
			return denied(ReasonCopyChanged, "the copy of draft %d was changed in the mail client; only the human removes it", d.ID)
		case d.RemoteState == RemoteMissing:
			// Die Kopie ist schon weg: schließen ohne Aktion. A12 Teil 1
			// (b): außer der Mensch hat sie weiterbearbeitet oder sie ist
			// nur umgezogen (noch nicht vermerkt).
			if err := liveCopyCheck(x, d); err != nil {
				return err
			}
		default:
			if err := liveCopyCheck(x, d); err != nil {
				return err
			}
			if cs, err = ownCopiesFor(x, d); err != nil {
				return err
			}
			if err := s.recordMoves(x, &d, cs.moves, now); err != nil {
				return err
			}
			withAction = true
		}
		if withAction && len(cs.targets) > 0 {
			trash, ok, err := x.TrashMailbox(d.AccountID)
			if err != nil {
				return err
			}
			if !ok {
				return denied(ReasonNoTrashMailbox, "the account has no mailbox with role trash in the mirror; run sync first")
			}
			res.Trash = trash.Path
		}
		res.Copies, res.Skipped = len(cs.targets), cs.skipped
		actionID, _, err := s.queueCleanup(x, d, SourceDraftDiscard, r.KeyID, r.AuditID, cs, now)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		t, err := Next(from, EvAbandon)
		if err != nil {
			return err
		}
		if _, err := applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
			return err
		}
		d.Status, d.UpdatedAt, d.ClosedAt = string(t.To), now, now
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		if !s.dry {
			res.ActionID = actionID
		}
		res.Draft = d
		params := map[string]any{"draft_id": d.ID, "copies": res.Copies, "skipped": res.Skipped, "from": from, "to": t.To}
		targets := []string{"draft:" + strconv.FormatInt(d.ID, 10)}
		if actionID > 0 {
			params["action_id"] = actionID
			targets = append(targets, "action:"+strconv.FormatInt(actionID, 10))
		}
		if d.ApprovalID > 0 {
			params["approval_id"] = d.ApprovalID
			targets = append(targets, "approval:"+strconv.FormatInt(d.ApprovalID, 10))
		}
		b, _ := json.Marshal(params)
		ev := audit.Event{TS: now, ActorType: audit.ActorAgent, KeyID: r.KeyID, Scope: "mail:draft", Operation: OpDiscard,
			ParamsRedacted: b, Targets: targets, Result: audit.ResultOK, Reason: ReasonDiscarded, PolicyVersion: s.PolicyVersion}
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

// liveCopyCheck prüft für discard und supersede in deren Transaktion den
// Stand mit Weiterbearbeitung und den Umzug (A12 Teil 1 (b)), nur zur
// Entscheidung: Es wird nichts vermerkt (eine Ablehnung rollt die
// Transaktion zurück); den neuen Stand schreibt der nächste Lauf.
// Weiterbearbeitete Fassung oder andere Übernahme → copy_changed; noch
// nicht vermerkter Umzug → draft_changed.
func liveCopyCheck(x *store.DraftTx, d store.Draft) error {
	cc, err := checkCopy(x, d)
	switch {
	case err != nil:
		return err
	case cc.State == RemoteTakenOver && cc.Via == ViaEditedElsewhere:
		return denied(ReasonCopyChanged, "draft %d was changed in the mail client (saved as a new message); only the human closes it", d.ID)
	case cc.State == RemoteTakenOver:
		return denied(ReasonCopyChanged, "the copy of draft %d was changed in the mail client; only the human removes it", d.ID)
	case cc.Move != nil:
		return denied(ReasonDraftChanged, "the copy of draft %d moved in the Drafts folder; try again after the next sync", d.ID)
	}
	return nil
}

// DiscardTargets schätzt für die Policy vor dem Aufruf, ob drafts.discard
// eine Papierkorb-Aktion anlegen würde, und mit wie vielen Zielen höchstens
// (A11 Teil 2: [delete] agent_may = none verbietet discard nur, wenn es ein
// Ziel gäbe). 0: keine Aktion (nie abgelegt, Kopie fehlt, abgeschlossen).
func DiscardTargets(d store.Draft) int {
	if Status(d.Status).Closed() || d.RemoteState == RemoteMissing {
		return 0
	}
	if d.RemoteUID <= 0 && len(d.RemotePrevious) == 0 && d.PushAttempt == nil {
		return 0
	}
	n := len(d.RemotePrevious)
	if d.RemoteUID > 0 {
		n++
	}
	return max(n, 1)
}
