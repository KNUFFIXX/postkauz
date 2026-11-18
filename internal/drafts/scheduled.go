package drafts

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Geplantes Senden (A10 Teil 2; docs/versanderkennung.md, Abschnitt
// „Geplantes Senden“). Befund B1/B4: Plant der Mensch einen Entwurf in
// Gmail, verschwindet die Kopie aus den Entwürfen, und Gmail legt eine neue
// Nachricht an (neue Message-ID), die nur in „Alle Nachrichten“ liegt, kein
// \Draft trägt und den künftigen Termin als Date und INTERNALDATE hat. Seit
// Teil 1 gilt sie nicht mehr als gesendet; Teil 2 sagt, was sie ist.
//
// Regel für Entwürfe: Ein offener Entwurf, dessen Kopie am Spiegel fehlt
// (checkCopy ergibt missing, unabhängig vom gespeicherten remote_state), ist
// geplant, wenn eine geplante Nachricht (store.ScheduledMessages, eigener
// Absender) zu ihm passt und nach seiner letzten Ablage gespiegelt wurde.
// „Passt“: gleiche Message-ID, oder die Bedingungen eines der Wege
// Rückfall, Verlauf (samt A07 Teil 1 und A09 Teil 1) oder Betreff ohne die
// Bedingung „gesendet“ (b) und ohne das Zeitfenster (e). Rangfolge wie A07
// Teil 2 (pairBefore); eine geplante Nachricht gehört höchstens einem
// Entwurf.
//
// Der Vermerk (lokale ID der Nachricht, Termin, Zustand) steht in meta
// unter scheduled/<draft_id> (store.Scheduled); er endet in der
// Transaktion, die den Entwurf schließt (store.DraftTx.UpdateDraft). Audit
// drafts.scheduled_detected (Akteur system, draft_id, message_id,
// scheduled_for), einmal je Zuordnung.
//
// Übergänge je vollständigem Lauf, vor den vier Wegen
// (scheduledTransitions); eine vermerkte Nachricht ist für alle Wege und
// für „ersetzt“ gesperrt:
//
//	(i)   in Gesendet → sent_by_human, Grund scheduled; Konten ohne
//	      Gesendet: Termin + 2 Minuten verstrichen und weiter nur in
//	      „Alle Nachrichten“ → ebenso;
//	(ii)  im Entwürfe-Ordner (Plan abgebrochen, B2) → taken_over, Audit
//	      drafts.remote_check Grund schedule_cancelled; checkCopy wertet die
//	      Nachricht dort als übernommene Fassung, bis sie verschwindet;
//	(iii) weg (nicht mehr im Spiegel, ohne Serverkopie oder nur im
//	      Papierkorb) → missing, Audit drafts.remote_check Grund missing;
//	      das Zeitfenster der Wege reicht für diesen Entwurf bis Termin + 14
//	      Tage;
//	(iv)  nur Konten mit Gesendet: Termin verstrichen, weiter nur in „Alle
//	      Nachrichten“ → noch ScheduledGrace geplant, danach wie (iii).
//
// Solange ein Entwurf geplant ist, greifen „ersetzt“ und „Kopie weg“ nicht,
// und drafts.update, drafts.supersede und drafts.request_ready werden mit
// draft_scheduled abgelehnt; draft abandon und draft mark-sent des
// Menschen bleiben möglich.

const (
	// DetectScheduled: Grund von drafts.sent_detected, wenn die vermerkte
	// geplante Nachricht gesendet ist (i).
	DetectScheduled = "scheduled"
	// OpScheduledDetected: Operation im Audit, wenn der Sync einen Entwurf
	// als geplant erkennt.
	OpScheduledDetected = "drafts.scheduled_detected"
	// ReasonScheduled: Grund von drafts.scheduled_detected.
	ReasonScheduled = "scheduled"
	// ReasonScheduleCancelled: Grund von drafts.remote_check, wenn die
	// geplante Nachricht wieder im Entwürfe-Ordner liegt (ii).
	ReasonScheduleCancelled = "schedule_cancelled"
	// ReasonDraftScheduled: Ablehnung einer Änderung eines geplanten
	// Entwurfs.
	ReasonDraftScheduled = "draft_scheduled"
	// RemoteScheduled: abgeleiteter Stand der Kopie in den Ausgaben (der
	// gespeicherte remote_state bleibt im Rahmen des CHECK, meist missing).
	RemoteScheduled = "scheduled"
	// ScheduledGrace: So lange bleibt ein Entwurf nach dem Termin geplant,
	// wenn die Nachricht noch nicht in Gesendet liegt (iv).
	ScheduledGrace = time.Hour
)

// ScheduledActive meldet, ob ein Vermerk den Entwurf als geplant führt.
func ScheduledActive(sc store.Scheduled, ok bool) bool {
	return ok && sc.State == store.ScheduledActive
}

// scheduledDenied ist die Ablehnung draft_scheduled.
func scheduledDenied(id int64, sc store.Scheduled) error {
	return denied(ReasonDraftScheduled, "draft %d is scheduled in the mail client for %s and not sent yet; it is not changed here "+
		"(cancel the schedule in the mail client first; draft abandon and draft mark-sent stay possible)", id, sc.At.UTC().Format(time.RFC3339))
}

// checkScheduled lehnt in der Transaktion ab, wenn der Entwurf geplant ist.
func checkScheduled(x *store.DraftTx, id int64) error {
	sc, ok, err := x.ScheduledOf(id)
	if err != nil {
		return err
	}
	if ScheduledActive(sc, ok) {
		return scheduledDenied(id, sc)
	}
	return nil
}

// CheckScheduled lehnt außerhalb einer Transaktion ab, wenn der Entwurf
// geplant ist (Vorprüfung der Adapter-Operationen; der Dienst prüft in der
// Schreibtransaktion noch einmal).
func (s *Service) CheckScheduled(ctx context.Context, id int64) error {
	sc, ok, err := s.Store.ScheduledOf(ctx, id)
	if err != nil {
		return err
	}
	if ScheduledActive(sc, ok) {
		return scheduledDenied(id, sc)
	}
	return nil
}

// windowEnd ist das Ende des Zeitfensters der Wege für einen Entwurf:
// content_changed_at + 14 Tage, mit Vermerk zum geplanten Senden
// mindestens Termin + 14 Tage (iii; falls Gmail beim Versand eine neue
// Nachricht anlegt).
func windowEnd(d store.Draft, ext map[int64]time.Time) time.Time {
	end := changedAt(d).Add(FallbackAfter)
	if e, ok := ext[d.ID]; ok && e.After(end) {
		end = e
	}
	return end
}

// inWindow ist InFallbackWindow mit dem verlängerten Ende aus ext.
func inWindow(d store.Draft, sent time.Time, ext map[int64]time.Time) bool {
	return !sent.Before(changedAt(d).Add(-FallbackBefore)) && !sent.After(windowEnd(d, ext))
}

// scheduledTransitions prüft die Vermerke der offenen Entwürfe open (siehe
// oben). hasSent: Das Konto hat Gesendet. Liefert die geschlossenen
// Entwürfe.
func (s *Service) scheduledTransitions(ctx context.Context, accountID int64, hasSent bool, open []store.Draft,
	vm map[int64]store.Scheduled, rep *CheckReport) (map[int64]bool, error) {
	closed := map[int64]bool{}
	now := s.now()
	for _, d := range open {
		sc, ok := vm[d.ID]
		if !ok || sc.State == store.ScheduledGone {
			continue
		}
		p, err := s.Store.Place(ctx, accountID, sc.MessageID)
		if err != nil {
			return nil, err
		}
		stillScheduled := p.OnlyAll() && p.InternalDate.After(now.Add(ScheduledLead))
		switch {
		case p.Found && p.Roles["sent"]: // (i)
			done, err := s.markSent(ctx, d.ID, sc.MessageID, DetectScheduled, "")
			if err != nil {
				return nil, err
			}
			if done {
				closed[d.ID] = true
				rep.SentByScheduled++
				rep.SentDraftIDs = append(rep.SentDraftIDs, d.ID)
			}
		case p.Found && p.InDrafts: // (ii)
			if sc.State == store.ScheduledCancelled {
				continue
			}
			if err := s.setScheduledState(ctx, d.ID, sc, store.ScheduledCancelled, sc.At, RemoteTakenOver, ReasonScheduleCancelled); err != nil {
				return nil, err
			}
			rep.ScheduleCancelled++
		case p.Gone(): // (iii)
			if err := s.setScheduledState(ctx, d.ID, sc, store.ScheduledGone, sc.At, RemoteMissing, RemoteMissing); err != nil {
				return nil, err
			}
			rep.ScheduledGone++
		case stillScheduled:
			// Erneut geplant (nach einem Abbruch) oder neuer Termin: nur der
			// Vermerk folgt, ohne neues Audit (dieselbe Zuordnung).
			if sc.State != store.ScheduledActive || !sc.At.Equal(p.InternalDate) {
				if err := s.setScheduledState(ctx, d.ID, sc, store.ScheduledActive, p.InternalDate, "", ""); err != nil {
					return nil, err
				}
			}
		case sc.State == store.ScheduledCancelled:
			// Aus dem Entwürfe-Ordner verschwunden, aber weder geplant noch
			// gesendet: wie weg (iii).
			if err := s.setScheduledState(ctx, d.ID, sc, store.ScheduledGone, sc.At, RemoteMissing, RemoteMissing); err != nil {
				return nil, err
			}
			rep.ScheduledGone++
		case !hasSent:
			// (i) ohne Gesendet: Termin + 2 Minuten verstrichen, weiter nur in
			// „Alle Nachrichten“.
			if p.OnlyAll() && !now.Before(sc.At.Add(ScheduledLead)) {
				done, err := s.markSent(ctx, d.ID, sc.MessageID, DetectScheduled, "")
				if err != nil {
					return nil, err
				}
				if done {
					closed[d.ID] = true
					rep.SentByScheduled++
					rep.SentDraftIDs = append(rep.SentDraftIDs, d.ID)
				}
			}
		default:
			// (iv) Termin verstrichen, noch nicht in Gesendet: eine Stunde
			// warten, danach wie (iii).
			if !now.Before(sc.At.Add(ScheduledGrace)) {
				if err := s.setScheduledState(ctx, d.ID, sc, store.ScheduledGone, sc.At, RemoteMissing, RemoteMissing); err != nil {
					return nil, err
				}
				rep.ScheduledGone++
			}
		}
	}
	return closed, nil
}

// setScheduledState ändert den Zustand eines Vermerks in einer
// Transaktion; mit remote ≠ "" auch remote_state des Entwurfs, Audit
// drafts.remote_check mit Grund reason (Akteur system). Ist der Entwurf
// inzwischen abgeschlossen oder der Vermerk ein anderer, geschieht nichts.
func (s *Service) setScheduledState(ctx context.Context, id int64, was store.Scheduled, state string, at time.Time, remote, reason string) error {
	now := s.now()
	return s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil || Status(d.Status).Closed() {
			return err
		}
		cur, ok, err := x.ScheduledOf(id)
		if err != nil || !ok || cur.MessageID != was.MessageID || cur.State != was.State {
			return err
		}
		if err := x.SetScheduled(store.Scheduled{DraftID: id, MessageID: was.MessageID, At: at, State: state}); err != nil {
			return err
		}
		if remote == "" {
			return nil
		}
		d.RemoteState = remote
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		b, _ := json.Marshal(map[string]any{"draft_id": d.ID, "remote": remote, "mailbox_id": d.RemoteMailboxID, "uid": d.RemoteUID,
			"message_id": was.MessageID})
		_, err = x.AppendAudit(audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: OpRemoteCheck,
			ParamsRedacted: b, Targets: []string{"draft:" + strconv.FormatInt(d.ID, 10)}, Result: audit.ResultOK, Reason: reason,
			PolicyVersion: s.PolicyVersion})
		return err
	})
}

// detectScheduled erkennt geplante Entwürfe (siehe oben) unter den offenen
// Entwürfen open, die keinen aktiven oder abgebrochenen Vermerk haben.
// locked: Nachrichten, die ein Vermerk hält oder die einem Entwurf
// zugeordnet sind; copies: Inhalts-Hashes der Entwurfskopien.
func (s *Service) detectScheduled(ctx context.Context, accountID int64, open []store.Draft, vm map[int64]store.Scheduled,
	locked map[int64]bool, copies []string, rep *CheckReport) error {
	var pushed []store.Draft
	for _, d := range open {
		if sc, ok := vm[d.ID]; ok && sc.State != store.ScheduledGone {
			continue // Übergänge oben
		}
		if d.PushedContentHash != "" {
			pushed = append(pushed, d) // nie abgelegt: nie geplant
		}
	}
	if len(pushed) == 0 {
		return nil
	}
	// Erst die geplanten Nachrichten (über den Datumsindex, die Menge ist
	// klein), dann die Kopie nur der Entwürfe, für die eine nach ihrer
	// Ablage gespiegelt wurde (BenchmarkCheckAfterSync22k, steady_replies:
	// sonst je Lauf eine Transaktion je abgelegtem Entwurf).
	now := s.now()
	msgs, err := s.Store.ScheduledMessages(ctx, accountID, now.Add(ScheduledLead))
	if err != nil || len(msgs) == 0 {
		return err
	}
	isCopy := map[string]bool{}
	for _, h := range copies {
		isCopy[h] = true
	}
	addrs, err := s.Store.OwnAddresses(ctx, accountID)
	if err != nil {
		return err
	}
	own := mailaddr.NewSet(addrs...)
	var list []store.ScheduledMessage
	var newest time.Time
	for _, m := range msgs {
		if locked[m.ID] || (m.ContentHash != "" && isCopy[m.ContentHash]) || !own.Has(fromAddress(m.From)) {
			continue // (c) eigener Absender
		}
		list = append(list, m)
		if m.CreatedAt.After(newest) {
			newest = m.CreatedAt
		}
	}
	if len(list) == 0 {
		return nil
	}
	var cands []store.Draft
	for _, d := range pushed {
		if !newest.After(lastPushAt(d)) {
			continue // keine geplante Nachricht nach der letzten Ablage gespiegelt
		}
		gone, err := s.copyGone(ctx, d)
		if err != nil {
			return err
		}
		if gone {
			cands = append(cands, d)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	type pair struct {
		d     store.Draft
		fp    string
		m     store.ScheduledMessage
		score float64
	}
	var pairs []pair
	sigs := map[int64]string{}
	texts := map[int64]*SentWords{}
	origIDs := map[int64]string{}
	for _, d := range cands {
		after := lastPushAt(d)
		var fd *FallbackDraft
		rk, subj := RecipientKey(d.To, d.Cc), NormalizeSubject(d.Subject)
		for _, m := range list {
			if !m.CreatedAt.After(after) {
				continue // vor der letzten Ablage gespiegelt
			}
			if d.MessageIDHeader != "" && m.MessageIDHeader == d.MessageIDHeader {
				pairs = append(pairs, pair{d, Fingerprint(d), m, 2}) // gleiche Message-ID vor jedem Text
				continue
			}
			mk := RecipientKey(m.To, m.Cc)
			fallback := rk != "" && mk == rk && subj == NormalizeSubject(m.Subject)
			thread := false
			if threadKinds[d.Kind] && d.InReplyToMsg > 0 && m.InReplyTo != "" {
				h, ok := origIDs[d.InReplyToMsg]
				if !ok {
					if h, err = s.Store.MessageIDHeaderOf(ctx, accountID, d.InReplyToMsg); err != nil {
						return err
					}
					origIDs[d.InReplyToMsg] = h
				}
				if ok, _ := threadRecipientsAdded(rk, m.SentMessage); ok && h != "" && m.InReplyTo == h {
					thread = true
				}
			}
			subject := d.Kind == string(KindNew) && subj != "" && rk != "" && mk == rk && m.InReplyTo == "" &&
				subj == NormalizeSubject(m.Subject) && !subjectPrefixRe.MatchString(sanitize.Text(m.Subject))
			if !fallback && !thread && !subject {
				continue
			}
			if fd == nil {
				sig, ok := sigs[d.IdentityID]
				if !ok && d.IdentityID > 0 {
					id, err := s.Store.Identity(ctx, d.IdentityID)
					if err != nil && !IsNotFound(err) {
						return err
					}
					if err == nil {
						sig = compose.SignatureText(id.SignatureText, id.SignatureHTML)
					}
					sigs[d.IdentityID] = sig
				}
				f := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, d.BodyHTML), sig)
				fd = &f
			}
			if len(fd.Words) == 0 {
				break
			}
			w := texts[m.ID]
			if w == nil {
				b, err := s.Store.MessageBody(ctx, m.ID)
				if err != nil {
					return err
				}
				sw := NewSentWords(sentText(b))
				w = &sw
				texts[m.ID] = w
			}
			sc := fd.ScoreSent(*w)
			ok := fallback && sc >= FallbackMinJaccard
			ok = ok || ((thread || subject) && len(fd.Words) >= ThreadMinWords && sc >= ThreadMinJaccard)
			if ok {
				pairs = append(pairs, pair{d, Fingerprint(d), m, sc})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		return pairBefore(a.score, b.score, a.d, b.d, a.m.SentMessage, b.m.SentMessage, 0, 0)
	})
	used := map[int64]bool{}
	for _, p := range pairs {
		if used[p.d.ID] || locked[p.m.ID] {
			continue
		}
		done, err := s.markScheduled(ctx, p.d.ID, p.m, p.fp)
		if err != nil {
			return err
		}
		if done {
			used[p.d.ID], locked[p.m.ID] = true, true
			rep.Scheduled++
			rep.ScheduledDraftIDs = append(rep.ScheduledDraftIDs, p.d.ID)
		}
	}
	return nil
}

// markScheduled schreibt den Vermerk (Zustand scheduled) und das Audit
// drafts.scheduled_detected in einer Transaktion. Ist der Entwurf
// inzwischen abgeschlossen, geändert (expect) oder liegt seine Kopie
// wieder im Entwürfe-Ordner, geschieht nichts (done false).
func (s *Service) markScheduled(ctx context.Context, id int64, m store.ScheduledMessage, expect string) (bool, error) {
	now := s.now()
	done := false
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil || Status(d.Status).Closed() || Fingerprint(d) != expect {
			return err
		}
		if st, err := checkCopy(x, d); err != nil || st != RemoteMissing {
			return err
		}
		if err := x.SetScheduled(store.Scheduled{DraftID: id, MessageID: m.ID, At: m.InternalDate, State: store.ScheduledActive}); err != nil {
			return err
		}
		from := Status(d.Status)
		if _, err := x.AppendAudit(s.systemEvent(now, OpScheduledDetected, ReasonScheduled, d, from, from,
			map[string]any{"message_id": m.ID, "scheduled_for": m.InternalDate.UTC().Format(time.RFC3339)})); err != nil {
			return err
		}
		done = true
		return nil
	})
	return done, err
}

// cancelledCopy meldet für checkCopy, ob die Nachricht eines Vermerks
// (geplant oder abgebrochen) im Entwürfe-Ordner liegt (ii): Sie gilt dann
// als übernommene Fassung des Entwurfs, obwohl sie eine andere Message-ID
// trägt.
func cancelledCopy(x *store.DraftTx, d store.Draft) (bool, error) {
	sc, ok, err := x.ScheduledOf(d.ID)
	if err != nil || !ok || sc.State == store.ScheduledGone {
		return false, err
	}
	p, err := x.Place(d.AccountID, sc.MessageID)
	if err != nil {
		return false, err
	}
	return p.Found && p.InDrafts, nil
}
