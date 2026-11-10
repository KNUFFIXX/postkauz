package drafts

import (
	"context"
	"fmt"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Auffangregel „Kopie weg“ (A07 Teil 4; docs/versanderkennung.md
// Abschnitt 3d). Ziel: Kein offener Entwurf bleibt dauerhaft liegen, nur
// weil keine Regel trifft; der Mensch schließt im Alltag nichts auf der
// Kommandozeile.
//
// Systemregel, letzter Weg der Versanderkennung (nach Message-ID,
// Rückfall, Verlauf, Betreff und „ersetzt“, nicht bei KeepMark und nicht
// bei SkipSent): Ein offener Entwurf, dessen Kopie seit mehr als
// CopyGoneAfter (Standard 24 h) fehlt, wird abandoned, Grund copy_gone,
// Akteur system, Audit drafts.copy_gone_detected (draft_id,
// missing_since), eine Freigabe superseded. „Seit“ ist der jüngste
// Audit-Eintrag drafts.remote_check mit Grund missing nach der letzten
// Ablage (Store.DraftMissingSince): der Zeitpunkt, an dem das Verschwinden
// bemerkt wurde, also die vorsichtige Seite. Fehlt der Eintrag (Rotation),
// gilt die Regel nicht.
//
// Weitere Bedingungen wie bei „ersetzt“: remote_state missing (nie
// taken_over; ein übernommener Entwurf wird erst über refreshRemote
// missing), Ablageordner bekannt (remote_mailbox_id ≠ 0) und im Spiegel
// (checkCopy meldet für einen nicht gespiegelten Ordner nie missing); beim
// Schließen in derselben Transaktion zuerst adoptOrphan (wurde etwas
// übernommen, endet die Transaktion mit draft_changed und wird samt der
// Übernahme zurückgerollt), dann checkCopy und der Fingerabdruck. Die Regel behauptet nicht, dass gesendet wurde:
// draft show nennt closed_reason copy_gone und sagt, dass der Versand nicht
// erkannt wurde.

// OpCopyGoneDetected ist die Operation im Audit der Auffangregel.
const OpCopyGoneDetected = "drafts.copy_gone_detected"

// ReasonCopyGone ist der Grund im Audit von drafts.copy_gone_detected.
const ReasonCopyGone = "copy_gone"

// DefaultCopyGoneAfter ist der Standard von Service.CopyGoneAfter.
const DefaultCopyGoneAfter = 24 * time.Hour

// copyGoneAfter liefert die wirksame Frist; ok false = abgeschaltet.
func (s *Service) copyGoneAfter() (time.Duration, bool) {
	switch {
	case s.CopyGoneAfter < 0:
		return 0, false
	case s.CopyGoneAfter == 0:
		return DefaultCopyGoneAfter, true
	}
	return s.CopyGoneAfter, true
}

// detectCopyGone wendet die Auffangregel auf die nach allen Wegen noch
// offenen Entwürfe open an.
func (s *Service) detectCopyGone(ctx context.Context, open []store.Draft, rep *CheckReport) error {
	after, on := s.copyGoneAfter()
	if !on {
		return nil
	}
	now := s.now()
	for _, d := range open {
		if d.RemoteState != RemoteMissing || d.RemoteMailboxID == 0 || d.PushedContentHash == "" {
			continue
		}
		since, ok, err := s.Store.DraftMissingSince(ctx, d.ID, lastPushAt(d))
		if err != nil {
			return err
		}
		if !ok || now.Sub(since) <= after {
			continue
		}
		done, err := s.markCopyGone(ctx, d.ID, since, Fingerprint(d))
		if err != nil {
			return err
		}
		if done {
			rep.CopyGone++
			rep.CopyGoneDraftIDs = append(rep.CopyGoneDraftIDs, d.ID)
		}
	}
	return nil
}

// markCopyGone schließt wie markReplaced; inzwischen abgeschlossen,
// geändert oder mit Kopie im Entwürfe-Ordner ist kein Fehler.
func (s *Service) markCopyGone(ctx context.Context, id int64, since time.Time, expect string) (bool, error) {
	_, err := s.CopyGone(ctx, id, since, expect)
	switch ReasonOf(err) {
	case ReasonDraftClosed, ReasonInvalidTransition, ReasonDraftChanged:
		return false, nil
	}
	return err == nil, err
}

// CopyGone schließt einen offenen Entwurf nach der Auffangregel (siehe
// oben): abandoned, closed_at gesetzt, Freigabe superseded wie bei
// EvAbandon. In derselben Transaktion noch einmal geprüft: Fingerabdruck
// (expect), remote_state missing, Ablageordner bekannt, keine übernommene
// Kopie eines abgebrochenen Ablageversuchs, Kopie laut Spiegel weg; sonst
// draft_changed, nichts wird geschrieben.
func (s *Service) CopyGone(ctx context.Context, id int64, since time.Time, expect string) (Result, error) {
	if expect == "" {
		return Result{}, fmt.Errorf("drafts: copy gone needs the compared state")
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
		if res.Approval, err = applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
			return err
		}
		d.Status, d.UpdatedAt, d.ClosedAt = string(t.To), now, now
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.From, res.To = d, from, t.To
		_, err = x.AppendAudit(s.systemEvent(now, OpCopyGoneDetected, ReasonCopyGone, d, from, t.To,
			map[string]any{"missing_since": since.UTC().Format(time.RFC3339)}))
		return err
	})
	return res, err
}
