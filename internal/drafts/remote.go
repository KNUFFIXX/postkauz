package drafts

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Serverkopie eines Entwurfs (S2.5, A02 E1, E6, E12; docs/architektur.md,
// Abschnitt 3, Regel 5): Der Job draft_push legt einen Entwurf per APPEND im
// Entwürfe-Ordner ab und vermerkt die Kopie (Ordner, UIDVALIDITY, UID,
// Inhalts-Hash, Date). Ab dann ist die Serverkopie maßgeblich, sobald der
// Mensch sie im Mailclient anfasst: Vor drafts.update und vor jedem Push
// prüft der Dienst anhand des Spiegels, ob die zuletzt abgelegte Kopie noch
// unverändert da ist. Fehlt sie oder ist sie anders, hat der Mensch
// übernommen; der Entwurf bleibt lokal (Vorschau, Audit), Änderungen sind
// draft_taken_over.
//
// Der Spiegel kennt nur den Stand des letzten Syncs. Was er noch nicht
// gesehen haben kann (UID der Kopie nicht unter seinem UIDNEXT, Ordner nie
// gespiegelt, UID unbekannt), gilt als unverändert: Die Prüfung meldet nur,
// was sie belegen kann.

// Hinweise des Pushs (Spalte drafts.push_note; Code, nie Inhalt).
const (
	// PushNoteApprovalStale: Der Entwurf war ready, aber die beim Push
	// gebaute Nachricht passte nicht mehr zur Freigabe (message_hash); er
	// ist zurück auf draft, die Freigabe superseded (Festlegung S2.5).
	PushNoteApprovalStale = "approval_stale"
	// PushNoteNoDraftsMailbox: Es gibt keinen Entwürfe-Ordner (keine Rolle
	// drafts, kein [draft] mailbox bzw. dieser fehlt auf dem Server).
	PushNoteNoDraftsMailbox = "no_drafts_mailbox"
	// PushNotePushFailed: Die Nachricht ließ sich nicht bauen (z. B.
	// Ausgangsnachricht oder Anhang fehlt im Blob-Store).
	PushNotePushFailed = "push_failed"
)

// Operationen und Gründe der Systemereignisse im Audit (Akteur system).
const (
	// OpPush: Kopie abgelegt (Grund appended) oder ein ready-Entwurf wegen
	// Abweichung von der Freigabe zurückgesetzt (Grund approval_stale).
	OpPush = "drafts.push"
	// OpRemoteCheck: Übernahme erkannt (Grund taken_over oder missing).
	OpRemoteCheck = "drafts.remote_check"
	// ReasonAppended: Grund eines erfolgreichen Pushs.
	ReasonAppended = "appended"
)

// copyState wertet den Spiegel zur zuletzt abgelegten Kopie aus: "" (da und
// unverändert oder nicht prüfbar), RemoteTakenOver (eine bearbeitete
// Fassung liegt da oder die Kopie ist anders) oder RemoteMissing (weg).
func copyState(d store.Draft, mc store.MirrorCopy, uidValid bool) string {
	if !mc.MailboxFound {
		return RemoteMissing // der Ordner ist auf dem Server verschwunden
	}
	if !mc.Synced {
		return ""
	}
	if !uidValid {
		// UIDVALIDITY hat gewechselt: Die UID gilt nicht mehr, gesucht wird
		// über Message-ID und Inhalts-Hash im ganzen Ordner.
		switch {
		case mc.SameIDAndHash:
			return ""
		case mc.NewerSameID:
			return RemoteTakenOver
		}
		return RemoteMissing
	}
	if d.RemoteUID <= 0 || mc.UIDNext <= uint32(d.RemoteUID) {
		return "" // UID unbekannt oder vom Spiegel noch nicht gesehen
	}
	if mc.AtUID {
		if mc.AtUIDMessageID == d.MessageIDHeader && mc.AtUIDContentHash == d.PushedContentHash {
			return ""
		}
		return RemoteTakenOver
	}
	if mc.NewerSameID {
		return RemoteTakenOver
	}
	return RemoteMissing
}

// checkCopy prüft in der Transaktion die zuletzt abgelegte Kopie (E6).
func checkCopy(x *store.DraftTx, d store.Draft) (string, error) {
	if d.PushedContentHash == "" {
		return "", nil // noch nie abgelegt
	}
	if d.RemoteMailboxID == 0 {
		return RemoteMissing, nil // Ordner entfernt (ON DELETE SET NULL)
	}
	mc, err := x.MirrorCopy(d.RemoteMailboxID, d.RemoteUID, d.MessageIDHeader, d.PushedContentHash)
	if err != nil {
		return "", err
	}
	valid := d.RemoteUIDValidity == 0 || !mc.Synced || mc.UIDValidity == d.RemoteUIDValidity
	if !valid {
		if mc, err = x.MirrorCopy(d.RemoteMailboxID, 0, d.MessageIDHeader, d.PushedContentHash); err != nil {
			return "", err
		}
	}
	return copyState(d, mc, valid), nil
}

// markRemote setzt remote_state nach erkannter Übernahme und auditiert das
// als Systemereignis (Grund taken_over bzw. missing), in derselben
// Transaktion.
func (s *Service) markRemote(x *store.DraftTx, d *store.Draft, state string, now time.Time) error {
	d.RemoteState = state
	if err := x.UpdateDraft(*d); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"draft_id": d.ID, "remote": state, "mailbox_id": d.RemoteMailboxID, "uid": d.RemoteUID})
	_, err := x.AppendAudit(audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: OpRemoteCheck,
		ParamsRedacted: b, Targets: []string{"draft:" + strconv.FormatInt(d.ID, 10)}, Result: audit.ResultOK, Reason: state,
		PolicyVersion: s.PolicyVersion})
	return err
}

// takenOver ist die Ablehnung einer Änderung nach Übernahme (E6).
func takenOver(id int64, state string) error {
	if state == RemoteMissing {
		return denied(ReasonDraftTakenOver, "the copy of draft %d in the Drafts folder is gone (sent or deleted in the mail client); "+
			"it is no longer changed here, close it with draft mark-sent or draft abandon", id)
	}
	return denied(ReasonDraftTakenOver, "draft %d was taken over in the mail client; change it there", id)
}

// PushCheck liest einen Entwurf vor dem Push und prüft seine zuletzt
// abgelegte Kopie (E6). Hat der Mensch übernommen, wird remote_state
// taken_over bzw. missing gespeichert (Audit system) und als state
// geliefert; der Entwurf wird dann nicht abgelegt. state "" heißt: Der
// Push darf ablegen, sofern der Entwurf noch wartet (Pending).
func (s *Service) PushCheck(ctx context.Context, id int64) (d store.Draft, state string, err error) {
	now := s.now()
	err = s.tx(ctx, func(x *store.DraftTx) error {
		if d, err = x.Draft(id); err != nil {
			return err
		}
		if !Pending(d) {
			return nil
		}
		if state, err = checkCopy(x, d); err != nil || state == "" {
			return err
		}
		return s.markRemote(x, &d, state, now)
	})
	return d, state, err
}

// Pending meldet, ob die Kopie eines Entwurfs aussteht: remote_state
// pending und nicht abgeschlossen (sent_by_human und abandoned werden nie
// abgelegt).
func Pending(d store.Draft) bool {
	return d.RemoteState == RemotePending && !Status(d.Status).Closed()
}

// PushStale setzt einen Entwurf in ready zurück, dessen beim Push gebaute
// Nachricht nicht mehr zur erteilten Freigabe passt (Festlegung S2.5 aus
// der Gegenprüfung von S2.3, W1): kein Push, Status draft, Freigabe
// superseded, push_note approval_stale (Hinweis in draft show), Audit
// system mit Grund approval_stale. fingerprint ist der Stand, den der Push
// gebaut hat; hat ein anderer Schreiber inzwischen geändert, ist das
// draft_changed und nichts geschieht.
func (s *Service) PushStale(ctx context.Context, id int64, fingerprint string) (Result, error) {
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		if Fingerprint(d) != fingerprint {
			return changed(d.ID)
		}
		from := Status(d.Status)
		res.Draft, res.From, res.To = d, from, from
		if from != StatusReady {
			return nil
		}
		t, err := Next(from, EvUpdate)
		if err != nil {
			return err
		}
		if res.Approval, err = applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
			return err
		}
		d.Status, d.HookResult, d.PushNote, d.UpdatedAt = string(t.To), "", PushNoteApprovalStale, now
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.To = d, t.To
		_, err = x.AppendAudit(s.systemEvent(now, OpPush, ReasonApprovalStale, d, from, t.To))
		return err
	})
	return res, err
}

// PushedCopy beschreibt eine abgelegte Kopie.
type PushedCopy struct {
	MailboxID   int64
	UIDValidity uint32
	// UID der Kopie (APPENDUID, sonst per Suche nach der Message-ID); 0 =
	// unbekannt.
	UID int64
	// ContentHash: Inhalts-Hash der abgelegten Bytes wie messages.content_hash
	// des Spiegels (SHA-256 ohne CR; internal/mailsync).
	ContentHash string
	// Date: die Date-Kopfzeile der Kopie (RFC 5322).
	Date string
}

// Pushed vermerkt eine abgelegte Kopie: Ordner, UIDVALIDITY, UID,
// Inhalts-Hash und Date; eine frühere Kopie kommt in remote_previous (sie
// bleibt im Ordner, kein Expunge). Ist der Entwurf noch der Stand, den der
// Push gebaut hat (fingerprint), ist er danach pushed; hat ein anderer
// Schreiber inzwischen geändert, bleibt er pending, und der nächste Push
// legt die neue Fassung ab (die eben abgelegte ist dann die frühere Kopie).
// Audit: Systemereignis drafts.push, Grund appended, mit Draft-ID, Ordner
// und UID. current meldet, ob der Entwurf jetzt pushed ist.
func (s *Service) Pushed(ctx context.Context, id int64, fingerprint string, c PushedCopy) (d store.Draft, current bool, err error) {
	now := s.now()
	err = s.tx(ctx, func(x *store.DraftTx) error {
		if d, err = x.Draft(id); err != nil {
			return err
		}
		if d.PushedContentHash != "" && d.RemoteUID > 0 && d.RemoteMailboxID > 0 {
			d.RemotePrevious = append(d.RemotePrevious, store.RemoteCopy{MailboxID: d.RemoteMailboxID, UID: d.RemoteUID,
				UIDValidity: d.RemoteUIDValidity})
		}
		d.RemoteMailboxID, d.RemoteUIDValidity, d.RemoteUID = c.MailboxID, c.UIDValidity, c.UID
		d.PushedContentHash, d.PushedDate = c.ContentHash, c.Date
		if d.PushNote != PushNoteApprovalStale {
			d.PushNote = ""
		}
		current = Fingerprint(d) == fingerprint && d.RemoteState == RemotePending
		if current {
			d.RemoteState = RemotePushed
		}
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		b, _ := json.Marshal(map[string]any{"draft_id": d.ID, "mailbox_id": c.MailboxID, "uid": c.UID, "current": current})
		_, err := x.AppendAudit(audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: OpPush,
			ParamsRedacted: b, Targets: []string{"draft:" + strconv.FormatInt(d.ID, 10)}, Result: audit.ResultOK,
			Reason: ReasonAppended, PolicyVersion: s.PolicyVersion})
		return err
	})
	return d, current, err
}

// NotePush setzt den Hinweis des Pushs: mit draftID > 0 an diesem Entwurf
// (push_failed), sonst an allen wartenden Entwürfen des Kontos
// (no_drafts_mailbox). Ein Hinweis approval_stale bleibt stehen.
func (s *Service) NotePush(ctx context.Context, accountID, draftID int64, note string) error {
	switch note {
	case PushNoteNoDraftsMailbox, PushNotePushFailed:
	default:
		return fmt.Errorf("drafts: unknown push note %q", note)
	}
	return s.tx(ctx, func(x *store.DraftTx) error {
		if draftID <= 0 {
			_, err := x.SetPushNotePending(accountID, note)
			return err
		}
		d, err := x.Draft(draftID)
		if err != nil || d.PushNote == PushNoteApprovalStale {
			return err
		}
		d.PushNote = note
		return x.UpdateDraft(d)
	})
}
