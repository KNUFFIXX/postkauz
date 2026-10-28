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
	// PushNotePushRejected: Der Server hat das Ablegen dieses Entwurfs
	// abgelehnt (NO/BAD auf APPEND, z. B. zu groß, Kontingent voll;
	// Gegenprüfung S2.4/S2.5, W3). Ohne Text des Servers.
	PushNotePushRejected = "push_rejected"
	// PushNotePushedAfterClose: Die Kopie kam in den Ordner, nachdem der
	// Entwurf abgeschlossen wurde (Wettlauf zwischen Prüfung und APPEND,
	// K5). Sie bleibt dort; PostKauz löscht nichts.
	PushNotePushedAfterClose = "pushed_after_close"
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
	// ReasonAppend: Grund des Start-Eintrags vor dem APPEND (A7).
	ReasonAppend = "append"
	// ReasonAttemptWithoutCopy: Ein Ablageversuch ist ohne Kopie zu Ende
	// (ein neuer Versuch ersetzt ihn, oder der Entwürfe-Ordner ist
	// gespiegelt und die Kopie fehlt); Abschluss mit Ergebnis error.
	ReasonAttemptWithoutCopy = "attempt_without_copy"
)

// pushEvent baut einen Audit-Eintrag des Pushs (Akteur system): Start
// (ref 0, Ergebnis started) oder Abschluss des Versuchs mit ref_id.
func (s *Service) pushEvent(now time.Time, d store.Draft, res audit.Result, reason string, ref int64, params map[string]any) audit.Event {
	p := map[string]any{"draft_id": d.ID}
	for k, v := range params {
		p[k] = v
	}
	b, _ := json.Marshal(p)
	return audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: OpPush, ParamsRedacted: b,
		Targets: []string{"draft:" + strconv.FormatInt(d.ID, 10)}, Result: res, Reason: reason, PolicyVersion: s.PolicyVersion, RefID: ref}
}

// endAttempt schließt den Start-Eintrag eines offenen Versuchs ohne Kopie
// (Ergebnis error) und löscht den Versuch am Entwurf (nicht gespeichert).
func (s *Service) endAttempt(x *store.DraftTx, d *store.Draft, now time.Time, reason string) error {
	a := d.PushAttempt
	d.PushAttempt = nil
	if a == nil || a.AuditID == 0 {
		return nil
	}
	_, err := x.AppendAudit(s.pushEvent(now, *d, audit.ResultError, reason, a.AuditID, map[string]any{"mailbox_id": a.MailboxID}))
	return err
}

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
	if !mc.AtUID && mc.Skipped {
		return "" // zu groß für den Spiegel: nicht prüfbar (K4)
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

// adoptOrphan übernimmt die Kopie eines Ablageversuchs, dessen Ausgang ein
// abgebrochener Lauf nicht mehr vermerken konnte (Gegenprüfung S2.4/S2.5,
// W1): Liegt im Spiegel des Ordners eine Kopie mit der Message-ID des
// Entwurfs und dem Inhalts-Hash des Versuchs, deren UID keine bekannte
// Kopie des Entwurfs ist, wird sie vermerkt wie nach einem erfolgreichen
// APPEND (Pushed), statt eine zweite abzulegen. Ohne solche Kopie bleibt
// der Versuch stehen; der nächste Push überschreibt ihn. adopted meldet,
// ob übernommen wurde; d ist danach der gespeicherte Stand.
func (s *Service) adoptOrphan(x *store.DraftTx, d *store.Draft, now time.Time) (adopted bool, err error) {
	a := d.PushAttempt
	if a == nil || a.MailboxID == 0 || a.ContentHash == "" {
		return false, nil
	}
	known := []int64{}
	if d.RemoteUID > 0 && d.RemoteMailboxID == a.MailboxID {
		known = append(known, d.RemoteUID)
	}
	for _, c := range d.RemotePrevious {
		if c.MailboxID == a.MailboxID {
			known = append(known, c.UID)
		}
	}
	uid, uv, err := x.MirrorOrphan(a.MailboxID, d.MessageIDHeader, a.ContentHash, known)
	if err != nil || uid == 0 {
		return false, err
	}
	c := PushedCopy{MailboxID: a.MailboxID, UIDValidity: uv, UID: uid, ContentHash: a.ContentHash, MessageHash: a.MessageHash, Date: a.Date}
	if _, err := s.recordCopy(x, d, a.Fingerprint, c, now, true); err != nil {
		return false, err
	}
	return true, nil
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
		// W1: Kopie eines abgebrochenen Versuchs zuerst übernehmen (sonst
		// hielte die Prüfung sie für eine Fassung des Mailclients).
		if _, err := s.adoptOrphan(x, &d, now); err != nil {
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
	// MessageHash: compose.MessageHash der abgelegten Bytes (ohne Date; W2).
	MessageHash string
	// Date: die Date-Kopfzeile der Kopie (RFC 5322).
	Date string
}

// Attempt vermerkt vor dem APPEND einen Ablageversuch (Gegenprüfung
// S2.4/S2.5, W1): Ordner, Inhalts- und Nachrichten-Hash, Date und der
// Fingerprint des gebauten Stands. Bricht der Lauf ab, bevor die Kopie
// vermerkt ist, findet der nächste Push sie darüber im Spiegel wieder
// (adoptOrphan) und legt keine zweite ab. Ein früherer Versuch wird
// überschrieben (dessen Kopie hätte der Push vorher übernommen).
//
// A7 (T1b): In derselben Transaktion entsteht der Audit-Eintrag drafts.push
// mit Ergebnis started, **vor** dem APPEND; der Vermerk der Kopie
// (appended, auch adopted), die Ablehnung (push_rejected) oder das Ende
// ohne Kopie (attempt_without_copy) schließt ihn mit ref_id ab. Ein
// ersetzter früherer Versuch wird dabei als ohne Kopie abgeschlossen.
func (s *Service) Attempt(ctx context.Context, id int64, a store.PushAttempt) error {
	now := s.now()
	return s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		if err := s.endAttempt(x, &d, now, ReasonAttemptWithoutCopy); err != nil {
			return err
		}
		if a.AuditID, err = x.AppendAudit(s.pushEvent(now, d, audit.ResultStarted, ReasonAppend, 0,
			map[string]any{"mailbox_id": a.MailboxID})); err != nil {
			return err
		}
		d.PushAttempt = &a
		return x.UpdateDraft(d)
	})
}

// Rejected vermerkt, dass der Server das Ablegen eines Entwurfs abgelehnt
// hat (NO/BAD auf APPEND; W3): push_note push_rejected, der Versuch ist
// erledigt (abgelehnt heißt: nichts abgelegt). Der Entwurf bleibt pending;
// erneut versucht wird beim nächsten postkauz sync (im Dauerbetrieb beim
// stündlichen vollen Abgleich) oder sofort nach einer Änderung.
func (s *Service) Rejected(ctx context.Context, id int64) error {
	return s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		if a := d.PushAttempt; a != nil && a.AuditID > 0 {
			if _, err := x.AppendAudit(s.pushEvent(s.now(), d, audit.ResultError, PushNotePushRejected, a.AuditID,
				map[string]any{"mailbox_id": a.MailboxID})); err != nil {
				return err
			}
		}
		d.PushAttempt = nil
		if d.PushNote != PushNoteApprovalStale {
			d.PushNote = PushNotePushRejected
		}
		return x.UpdateDraft(d)
	})
}

// Pushed vermerkt eine abgelegte Kopie: Ordner, UIDVALIDITY, UID,
// Inhalts-Hash, Nachrichten-Hash und Date; eine frühere Kopie kommt in
// remote_previous (sie bleibt im Ordner, kein Expunge), ein offener
// Ablageversuch ist erledigt. Ist der Entwurf noch der Stand, den der Push
// gebaut hat (fingerprint), und nicht abgeschlossen, ist er danach pushed;
// hat ein anderer Schreiber inzwischen geändert, bleibt er pending, und der
// nächste Push legt die neue Fassung ab (die eben abgelegte ist dann die
// frühere Kopie). Wurde er inzwischen abgeschlossen (verworfen, als
// gesendet erkannt; K5), bleibt die Kopie vermerkt, aber nicht als aktuell:
// push_note pushed_after_close, draft show sagt, dass sie im Ordner liegt
// und nichts gelöscht wird. Audit: Systemereignis drafts.push, Grund
// appended, mit Draft-ID, Ordner, UID und current. current meldet, ob der
// Entwurf jetzt pushed ist.
func (s *Service) Pushed(ctx context.Context, id int64, fingerprint string, c PushedCopy) (d store.Draft, current bool, err error) {
	now := s.now()
	err = s.tx(ctx, func(x *store.DraftTx) error {
		if d, err = x.Draft(id); err != nil {
			return err
		}
		current, err = s.recordCopy(x, &d, fingerprint, c, now, false)
		return err
	})
	return d, current, err
}

// recordCopy ist Pushed in einer Transaktion; adopted kennzeichnet eine
// übernommene Kopie eines abgebrochenen Versuchs (W1) im Audit.
func (s *Service) recordCopy(x *store.DraftTx, d *store.Draft, fingerprint string, c PushedCopy, now time.Time, adopted bool) (bool, error) {
	var ref int64 // Start-Eintrag des Versuchs (A7)
	if d.PushAttempt != nil {
		ref = d.PushAttempt.AuditID
	}
	if d.PushedContentHash != "" && d.RemoteUID > 0 && d.RemoteMailboxID > 0 {
		d.RemotePrevious = append(d.RemotePrevious, store.RemoteCopy{MailboxID: d.RemoteMailboxID, UID: d.RemoteUID,
			UIDValidity: d.RemoteUIDValidity, ContentHash: d.PushedContentHash})
	}
	d.RemoteMailboxID, d.RemoteUIDValidity, d.RemoteUID = c.MailboxID, c.UIDValidity, c.UID
	d.PushedContentHash, d.PushedMessageHash, d.PushedDate, d.PushAttempt = c.ContentHash, c.MessageHash, c.Date, nil
	closed := Status(d.Status).Closed()
	switch {
	case closed:
		d.PushNote = PushNotePushedAfterClose
	case d.PushNote != PushNoteApprovalStale:
		d.PushNote = ""
	}
	current := !closed && Fingerprint(*d) == fingerprint && d.RemoteState == RemotePending
	if current {
		d.RemoteState = RemotePushed
	}
	if err := x.UpdateDraft(*d); err != nil {
		return false, err
	}
	p := map[string]any{"draft_id": d.ID, "mailbox_id": c.MailboxID, "uid": c.UID, "current": current}
	if closed {
		p["closed"] = true
	}
	if adopted {
		p["adopted"] = true
	}
	_, err := x.AppendAudit(s.pushEvent(now, *d, audit.ResultOK, ReasonAppended, ref, p))
	return current, err
}

// NotePush setzt den Hinweis des Pushs: mit draftID > 0 an diesem Entwurf
// (push_failed, push_rejected), sonst an allen wartenden Entwürfen des Kontos
// (no_drafts_mailbox). Ein Hinweis approval_stale bleibt stehen.
func (s *Service) NotePush(ctx context.Context, accountID, draftID int64, note string) error {
	switch note {
	case PushNoteNoDraftsMailbox, PushNotePushFailed, PushNotePushRejected:
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

// HumanKeyID ist der Schlüssel des Menschen in created_by, updated_by und
// approvals.requested_by (wie audit_events.api_key_id).
const HumanKeyID = "cli"

// HoldForKey setzt einen offenen Entwurf des Menschen zurück, hinter dem
// ein Schlüssel steht, der nicht mehr gilt (A-F3; reason wie bei
// CloseForKey). Der Mensch verliert seinen Entwurf nicht: Status draft,
// eine offene oder erteilte Freigabe wird abgelehnt (DecidedBy system,
// Grund reason). content: Der Schlüssel hat den Inhalt zuletzt geändert
// (updated_by); dann hält der Push den Entwurf zurück (push_hold = reason),
// bis der Mensch ihn ändert oder selbst vorlegt. Sonst (nur die Vorlage
// kam vom Schlüssel) wird er wieder als gewöhnlicher Entwurf abgelegt.
// Audit: Systemereignis drafts.push mit dem Grund, in derselben
// Transaktion. Changed meldet, ob sich etwas geändert hat (sonst kein
// Eintrag).
func (s *Service) HoldForKey(ctx context.Context, id int64, reason string, content bool) (Result, error) {
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		res.Draft, res.From, res.To = d, from, from
		if from.Closed() {
			return nil
		}
		to := from
		if from != StatusDraft {
			t, err := Next(from, EvUpdate)
			if err != nil {
				return err
			}
			to = t.To
		}
		if d.ApprovalID > 0 {
			a, err := x.Approval(d.ApprovalID)
			if err != nil {
				return err
			}
			if a.Decision == DecisionPending || a.Decision == DecisionApproved {
				a.Decision, a.DecidedBy, a.DecidedAt, a.Reason = DecisionRejected, "system", now, reason
				if err := x.UpdateApproval(a); err != nil {
					return err
				}
				res.Approval = &a
			}
		}
		hold := d.PushHold
		if content {
			hold = reason
		}
		if to == from && res.Approval == nil && hold == d.PushHold {
			return nil
		}
		d.Status, d.PushHold, d.UpdatedAt = string(to), hold, now
		if to != from {
			d.HookResult = ""
		}
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.To, res.Changed = d, to, true
		_, err = x.AppendAudit(s.systemEvent(now, OpPush, reason, d, from, to, map[string]any{"held": content}))
		return err
	})
	return res, err
}

// CloseForKey schließt einen offenen Entwurf, dessen Schlüssel nicht mehr
// gilt (A8: widerrufen, abgelaufen, unbekannt oder ohne Scope mail:draft;
// reason ist der Grund wie im Audit des Executors, z. B. key_revoked).
// Der Push legt ihn nicht ab. Eine offene oder erteilte Freigabe wird
// abgelehnt (A03 A8, Standard rejected; DecidedBy system, Grund reason),
// der Entwurf wird verworfen (abandoned), damit kein späterer Push ihn
// doch noch als Entwurfskopie ablegt; sein Inhalt bleibt lokal lesbar.
// Audit: Systemereignis drafts.push mit diesem Grund, in derselben
// Transaktion. Ein schon abgeschlossener Entwurf bleibt, wie er ist.
func (s *Service) CloseForKey(ctx context.Context, id int64, reason string) (Result, error) {
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		res.Draft, res.From, res.To = d, from, from
		if from.Closed() {
			return nil
		}
		t, err := Next(from, EvAbandon)
		if err != nil {
			return err
		}
		if d.ApprovalID > 0 {
			a, err := x.Approval(d.ApprovalID)
			if err != nil {
				return err
			}
			if a.Decision == DecisionPending || a.Decision == DecisionApproved {
				a.Decision, a.DecidedBy, a.DecidedAt, a.Reason = DecisionRejected, "system", now, reason
				if err := x.UpdateApproval(a); err != nil {
					return err
				}
				res.Approval = &a
			}
		}
		d.Status, d.UpdatedAt, d.ClosedAt = string(t.To), now, now
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.To = d, t.To
		_, err = x.AppendAudit(s.systemEvent(now, OpPush, reason, d, from, t.To))
		return err
	})
	return res, err
}
