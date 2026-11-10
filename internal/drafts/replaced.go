package drafts

import (
	"context"
	"fmt"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Ersetzte Entwürfe (A06 Teil 4, 05.11.2026): Befund aus dem Dauerbetrieb.
// Der Betreiber schrieb einen Antwort-Entwurf in Gmail fast ganz neu und
// sendete ihn (Ähnlichkeit 0,29). Die Kopie war aus den Entwürfen weg, die
// gesendete Antwort hatte In-Reply-To = Original und dieselben Empfänger;
// der Entwurf blieb draft mit remote: missing, und schließen konnte ihn nur
// der Mensch auf der Kommandozeile. Offene Entwürfe sammelten sich an.
//
// Systemregel nach Message-ID, Rückfall und Verlauf, im selben Lauf:
// Ersetzt ist ein offener Entwurf, wenn eine gesendete Nachricht für ihn
// die Bedingungen (a) bis (f) des Wegs über den Verlauf erfüllt
// (docs/versanderkennung.md Abschnitt 3a) und kein Weg ihn in diesem Lauf
// als gesendet schließt, gleich ob die Ähnlichkeit (g) erfüllt ist. Als
// ersetzende Nachricht zählt auch eine, die schon einem anderen Entwurf als
// gesendet zugeordnet ist (zwei Fassungen derselben Antwort). Zusätzlich,
// weil falsches Schließen endgültig ist:
//
//   - nicht bei KeepMark (Gesendet bzw. „Alle Nachrichten“ im Lauf nicht
//     vollständig gespiegelt);
//   - die Antwort ist nach der letzten Ablage und nach content_changed_at
//     datiert (nicht nur im Fenster −1 h);
//   - (f) und der Inhalts-Fingerabdruck werden beim Schließen in derselben
//     Transaktion erneut geprüft.
//
// Nachbesserung 1 (Gegenprüfung von fc46320), weitere Sicherungen:
//
//   - remote_state war schon vor diesem Lauf missing (in einem früheren
//     Lauf festgestellt): Eine gerade gesendete Nachricht, die erst noch
//     gespiegelt wird, schließt den Entwurf so zuerst als gesendet;
//   - der Ablageordner des Entwurfs ist bekannt und gespiegelt
//     (remote_mailbox_id gesetzt, Ordner im Spiegel; fehlerfrei in diesem
//     Lauf garantiert CheckOptions.SkipSent);
//   - im Entwürfe-Ordner liegt keine nach der letzten Ablage (Push-Zeitpunkt,
//     lastPushAt) datierte Nachricht mit In-Reply-To = Original, die keine
//     Kopie von PostKauz ist (in Gmail unter neuer Message-ID
//     weiterbearbeitet);
//   - beim Schließen zuerst adoptOrphan, dann checkCopy (wie Update):
//     Findet adoptOrphan die Kopie eines abgebrochenen Ablageversuchs (auch
//     in einem anderen Ordner), endet die Transaktion mit draft_changed und
//     wird zurückgerollt, auch die Übernahme selbst; der Entwurf bleibt
//     offen, und die Übernahme holen der nächste Push bzw. die nächste
//     Prüfung nach (richtiggestellt in A07 Teil 6, Sammelpunkt 0e).
//
// Ergebnis: abandoned (nicht sent_by_human: der Text des Entwurfs ging
// nicht hinaus), eine Freigabe wird superseded (wie EvAbandon), Akteur
// system, Audit drafts.replaced_detected mit Grund replaced und den
// lokalen IDs von Entwurf und Antwort, ohne Inhalt; sent_message_id bleibt
// leer. Ein Agent kann einen Entwurf weiterhin nie selbst schließen (E2).

// OpReplacedDetected ist die Operation im Audit, wenn der Sync einen
// Entwurf als durch eine eigene Antwort ersetzt schließt (A06 Teil 4).
const OpReplacedDetected = "drafts.replaced_detected"

// ReasonReplaced ist der Grund im Audit von drafts.replaced_detected.
const ReasonReplaced = "replaced"

// detectReplaced schließt die nach allen drei Wegen noch offenen Entwürfe
// open, die eine eigene Antwort ersetzt hat (siehe oben). copies:
// Inhalts-Hashes der Entwurfskopien (nie eine Antwort).
func (s *Service) detectReplaced(ctx context.Context, accountID int64, open []store.Draft, copies []string, missingBefore map[int64]bool,
	rep *CheckReport) error {
	isCopy := map[string]bool{}
	for _, h := range copies {
		isCopy[h] = true
	}
	var own mailaddr.Set
	for _, d := range open {
		rk := RecipientKey(d.To, d.Cc)
		if !threadKinds[d.Kind] || d.InReplyToMsg <= 0 || d.PushedContentHash == "" {
			continue // (a), (f) nie abgelegt
		}
		if !missingBefore[d.ID] || d.RemoteState != RemoteMissing || d.RemoteMailboxID == 0 {
			continue // schon vor Beginn des Laufs missing (Nachbesserung 2); Ablageordner bekannt
		}
		list, err := s.Store.OwnReplies(ctx, accountID, d.InReplyToMsg) // (b), (c), auch zugeordnete
		if err != nil {
			return err
		}
		after := replacedAfter(d)
		var reply int64
		for _, c := range list {
			if c.ContentHash != "" && isCopy[c.ContentHash] {
				continue
			}
			if !threadRecipientsMatch(rk, c) || !InFallbackWindow(changedAt(d), c.Date) || !c.Date.After(after) { // (d), (e), danach
				continue
			}
			if !c.InSent {
				if own == nil {
					addrs, err := s.Store.OwnAddresses(ctx, accountID)
					if err != nil {
						return err
					}
					own = mailaddr.NewSet(addrs...)
				}
				if !own.Has(fromAddress(c.From)) {
					continue
				}
			}
			reply = c.ID // älteste passende Antwort (Liste nach Datum sortiert)
			break
		}
		if reply == 0 {
			continue
		}
		if gone, err := s.copyGone(ctx, d); err != nil || !gone { // (f)
			if err != nil {
				return err
			}
			continue
		}
		done, err := s.markReplaced(ctx, d.ID, reply, Fingerprint(d))
		if err != nil {
			return err
		}
		if done {
			rep.ReplacedInThread++
			rep.ReplacedDraftIDs = append(rep.ReplacedDraftIDs, d.ID)
		}
	}
	return nil
}

// replacedAfter ist der Zeitpunkt, nach dem eine ersetzende Antwort datiert
// sein muss: die letzte Ablage (Date der abgelegten Kopie) bzw. die letzte
// Inhaltsänderung, das Spätere; auf Sekunden gekürzt wie die
// Date-Kopfzeile.
func replacedAfter(d store.Draft) time.Time {
	at := changedAt(d)
	if t, err := time.Parse(time.RFC1123Z, d.PushedDate); err == nil && t.After(at) {
		at = t
	}
	return at.Truncate(time.Second)
}

// lastPushAt ist der Zeitpunkt der letzten Ablage (Date der abgelegten
// Kopie, pushed_date), auf Sekunden; ohne lesbares Datum der Zeitpunkt der
// letzten Inhaltsänderung. Grenze für Sicherung (c) (Nachbesserung 2): Eine
// Fassung, die Gmail nach der Ablage gespeichert hat, zählt auch dann, wenn
// der Entwurf danach lokal geändert wurde.
func lastPushAt(d store.Draft) time.Time {
	if t, err := time.Parse(time.RFC1123Z, d.PushedDate); err == nil {
		return t.Truncate(time.Second)
	}
	return changedAt(d).Truncate(time.Second)
}

// markReplaced schließt einen Entwurf als ersetzt (Replaced). Wie markSent:
// inzwischen abgeschlossen, geändert oder mit Kopie im Entwürfe-Ordner ist
// kein Fehler; done ist dann false.
func (s *Service) markReplaced(ctx context.Context, id, replyID int64, expect string) (bool, error) {
	_, err := s.Replaced(ctx, id, replyID, expect)
	switch ReasonOf(err) {
	case ReasonDraftClosed, ReasonInvalidTransition, ReasonDraftChanged:
		return false, nil
	}
	return err == nil, err
}

// Replaced schließt einen offenen Entwurf, den eine eigene Antwort ersetzt
// hat (A06 Teil 4, Systemregel des Syncs): abandoned, closed_at gesetzt,
// Freigabe superseded wie bei EvAbandon, sent_message_id bleibt leer.
// Geprüft wird in derselben Transaktion noch einmal, dass der Inhalt der
// verglichene ist (expect, Fingerprint) und die Kopie im Entwürfe-Ordner
// laut Spiegel fehlt (Bedingung f); sonst draft_changed, nichts wird
// geschrieben. Audit drafts.replaced_detected (Akteur system, Grund
// replaced) mit draft_id und reply_message_id, ohne Inhalt.
func (s *Service) Replaced(ctx context.Context, id, replyID int64, expect string) (Result, error) {
	if replyID <= 0 || expect == "" {
		return Result{}, fmt.Errorf("drafts: replaced needs the reply and the compared state")
	}
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		t, err := Next(from, EvAbandon)
		if err != nil {
			return err
		}
		if Fingerprint(d) != expect || d.RemoteState != RemoteMissing || d.RemoteMailboxID == 0 {
			return changed(d.ID)
		}
		// Erst die Kopie eines abgebrochenen Ablageversuchs übernehmen,
		// dann die Kopie prüfen (wie Update). Die Übernahme macht die
		// übernommene Kopie zur zuletzt abgelegten (auch in einem anderen
		// Ordner als remote_mailbox_id); die Prüfung danach findet sie, ohne
		// Übernahme hielte checkCopy den Entwurf für missing. Wurde etwas
		// übernommen, wird ausdrücklich nicht geschlossen: changed() lässt die
		// Transaktion scheitern, und sie wird samt der Übernahme
		// zurückgerollt (die Übernahme bleibt nicht stehen; nachgeholt wird
		// sie beim nächsten Push bzw. in settleClosedAttempts).
		// Ein nicht gespiegelter Ablageordner braucht keine eigene Prüfung:
		// checkCopy liefert dann nie missing (copyState), und ein entfernter
		// Ordner setzt remote_mailbox_id auf 0 (oben ausgeschlossen).
		if adopted, err := s.adoptOrphan(x, &d, now); err != nil {
			return err
		} else if adopted {
			return changed(d.ID)
		}
		if st, err := checkCopy(x, d); err != nil {
			return err
		} else if st != RemoteMissing {
			return changed(d.ID)
		}
		if edited, err := x.EditedReplyInDrafts(d.AccountID, d.InReplyToMsg, lastPushAt(d)); err != nil {
			return err
		} else if edited {
			return changed(d.ID) // in Gmail unter neuer Message-ID weiterbearbeitet
		}
		if res.Approval, err = applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
			return err
		}
		d.Status, d.UpdatedAt, d.ClosedAt = string(t.To), now, now
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.From, res.To = d, from, t.To
		_, err = x.AppendAudit(s.systemEvent(now, OpReplacedDetected, ReasonReplaced, d, from, t.To,
			map[string]any{"reply_message_id": replyID}))
		return err
	})
	return res, err
}
