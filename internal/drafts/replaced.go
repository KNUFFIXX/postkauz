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
func (s *Service) detectReplaced(ctx context.Context, accountID int64, open []store.Draft, copies []string, rep *CheckReport) error {
	isCopy := map[string]bool{}
	for _, h := range copies {
		isCopy[h] = true
	}
	var own mailaddr.Set
	for _, d := range open {
		rk := RecipientKey(d.To, d.Cc)
		if !threadKinds[d.Kind] || d.InReplyToMsg <= 0 || rk == "" || d.PushedContentHash == "" {
			continue // (a), (d) ohne Empfänger, (f) nie abgelegt
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
			if RecipientKey(c.To, c.Cc) != rk || !InFallbackWindow(changedAt(d), c.Date) || !c.Date.After(after) { // (d), (e), danach
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
		if Fingerprint(d) != expect {
			return changed(d.ID)
		}
		if st, err := checkCopy(x, d); err != nil {
			return err
		} else if st != RemoteMissing {
			return changed(d.ID)
		}
		if _, err := s.adoptOrphan(x, &d, now); err != nil {
			return err
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
