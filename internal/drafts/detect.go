package drafts

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Prüfung nach dem Spiegeln (S2.6): Der Sync ruft CheckAfterSync am Ende
// jedes Laufs auf (postkauz sync und jede Runde von sync --watch), unter
// der Kontosperre. Sie
//
//  1. erkennt gesendete Entwürfe (E5, sentmatch.go): sent_by_human,
//     sent_message_id, Audit drafts.sent_detected mit Grund message_id,
//     fallback oder thread (A05 Teil 2; Akteur system, nur IDs und
//     Zustände); danach durch eine eigene Antwort ersetzte Entwürfe (A06
//     Teil 4, replaced.go): abandoned, Audit drafts.replaced_detected,
//     Grund replaced; zuletzt die Auffangregel „Kopie weg“ (A07 Teil 4,
//     copygone.go): abandoned, Audit drafts.copy_gone_detected,
//  2. lässt abgelaufene Freigaben verfallen (E7, Entscheid 38: ready und
//     pending_approval älter als approval_ttl → draft, Freigabe expired,
//     Audit drafts.expire mit Grund approval_expired),
//  3. frischt den Stand der Serverkopie auf (E6): Ist die zuletzt
//     abgelegte Kopie eines offenen Entwurfs laut Spiegel weg oder
//     bearbeitet, wird remote_state missing bzw. taken_over (Audit
//     drafts.remote_check), damit draft list es zeigt, auch ohne dass
//     jemand den Entwurf ändert.
//
// Die Versanderkennung kommt vor dem Ablauf (Gegenprüfung S2.6–S2.8, W1):
// Hat der Mensch einen ready-Entwurf vor dem Ablauf gesendet und sieht der
// Sync die Nachricht erst danach, ist der Entwurf gesendet, nicht
// abgelaufen. Das Zeitfenster des Rückfalls hängt an content_changed_at
// (letzte Inhaltsänderung), das weder Ablauf noch Lesen verschieben.
//
// Kein Treffer ist kein Fehler: Der Entwurf bleibt, bis der Mensch ihn mit
// draft mark-sent oder draft abandon schließt (oder eine eigene Antwort
// ihn ersetzt, A06 Teil 4).

// CheckReport fasst die Prüfung nach dem Spiegeln zusammen (Zähler und
// Draft-IDs, kein Inhalt).
type CheckReport struct {
	// SentByMessageID, SentByFallback, SentByThread: als gesendet erkannte
	// Entwürfe je Weg (SentByThread seit A05 Teil 2).
	SentByMessageID int `json:"sent_by_message_id,omitempty"`
	SentByFallback  int `json:"sent_by_fallback,omitempty"`
	SentByThread    int `json:"sent_by_thread,omitempty"`
	// SentBySubject: neue Mails über den Betreff (A07 Teil 3).
	SentBySubject int `json:"sent_by_subject,omitempty"`
	// Expired: Entwürfe, deren Freigabe abgelaufen ist (zurück auf draft).
	Expired int `json:"approval_expired,omitempty"`
	// Missing, TakenOver: Entwürfe, deren Kopie im Entwürfe-Ordner jetzt
	// fehlt bzw. im Mailclient bearbeitet wurde.
	Missing   int `json:"missing,omitempty"`
	TakenOver int `json:"taken_over,omitempty"`
	// SentDraftIDs: die als gesendet erkannten Entwürfe.
	SentDraftIDs []int64 `json:"sent_draft_ids,omitempty"`
	// ReplacedInThread, ReplacedDraftIDs: Entwürfe, die eine eigene Antwort
	// im Verlauf ersetzt hat; jetzt abandoned (A06 Teil 4, replaced.go).
	ReplacedInThread int     `json:"replaced_in_thread,omitempty"`
	ReplacedDraftIDs []int64 `json:"replaced_draft_ids,omitempty"`
	// CopyGone, CopyGoneDraftIDs: Entwürfe, deren Kopie länger als
	// [draft] copy_gone_after fehlt und die kein Weg geschlossen hat;
	// jetzt abandoned (A07 Teil 4, copygone.go).
	CopyGone         int     `json:"copy_gone,omitempty"`
	CopyGoneDraftIDs []int64 `json:"copy_gone_draft_ids,omitempty"`
	// AdoptedClosed: Kopien abgebrochener Ablageversuche abgeschlossener
	// Entwürfe, die die Prüfung übernommen hat (A7; push_note
	// pushed_after_close). LostAttempts: solche Versuche ohne Kopie im
	// gespiegelten Entwürfe-Ordner (abgeschlossen mit attempt_without_copy).
	AdoptedClosed int `json:"adopted_after_close,omitempty"`
	LostAttempts  int `json:"attempts_without_copy,omitempty"`
	// Examined: gesendete Nachrichten, die der Rückfall-Abgleich geladen
	// hat (neue und solche im Fenster neuer oder geänderter Entwürfe; W2).
	// Nur für Tests und Messungen, nicht in der Ausgabe.
	Examined int `json:"-"`
}

// Empty meldet, ob die Prüfung nichts geändert hat.
func (r CheckReport) Empty() bool {
	return r.SentByMessageID+r.SentByFallback+r.SentByThread+r.SentBySubject+r.ReplacedInThread+r.CopyGone+r.Expired+r.Missing+r.TakenOver+r.AdoptedClosed+
		r.LostAttempts == 0
}

// Add zählt o dazu (Dauerbetrieb: eine Prüfung je Runde).
func (r *CheckReport) Add(o CheckReport) {
	r.SentByMessageID += o.SentByMessageID
	r.SentByFallback += o.SentByFallback
	r.SentByThread += o.SentByThread
	r.SentBySubject += o.SentBySubject
	r.Expired += o.Expired
	r.Missing += o.Missing
	r.TakenOver += o.TakenOver
	r.AdoptedClosed += o.AdoptedClosed
	r.LostAttempts += o.LostAttempts
	r.SentDraftIDs = append(r.SentDraftIDs, o.SentDraftIDs...)
	r.ReplacedInThread += o.ReplacedInThread
	r.ReplacedDraftIDs = append(r.ReplacedDraftIDs, o.ReplacedDraftIDs...)
	r.CopyGone += o.CopyGone
	r.CopyGoneDraftIDs = append(r.CopyGoneDraftIDs, o.CopyGoneDraftIDs...)
	r.Examined += o.Examined
}

var openStatuses = []string{string(StatusDraft), string(StatusPendingApproval), string(StatusReady)}

// CheckOptions sagt der Prüfung nach dem Spiegeln, was der Lauf gespiegelt
// hat (Gegenprüfung S2.6–S2.8, K3). Der Nullwert ist ein vollständiger
// Lauf.
type CheckOptions struct {
	// SkipSent: Der Entwürfe-Ordner wurde in diesem Lauf nicht oder nicht
	// fehlerfrei gespiegelt (sync --mailbox ohne ihn, IDLE-Runde nur mit
	// INBOX, Fehler im Ordner). Der Spiegel kennt dann frisch abgelegte
	// Kopien nicht im Entwürfe-Ordner, und eine Kopie, die Gmail schon in
	// „All Mail" führt, sähe wie gesendet aus. Versanderkennung und Stand
	// der Serverkopie entfallen; nur der Ablauf der Freigaben läuft.
	SkipSent bool
	// KeepMark: Ein Ordner mit Rolle sent oder all wurde nicht oder nicht
	// fehlerfrei gespiegelt. Die Erkennung läuft, aber Hochwassermarke und
	// fallback_checked_at bleiben stehen: Eine schon gespiegelte Nachricht
	// kann noch in Gesendet hinzukommen, und der nächste vollständige Lauf
	// soll sie als neu prüfen.
	KeepMark bool
	// MissingBefore: die offenen Entwürfe, deren remote_state schon vor
	// Beginn des Sync-Laufs missing war (A06 Nachbesserung 2, Sicherung a
	// für ersetzte Entwürfe). Der Sync liest den Stand vor dem Push zu
	// Beginn des Laufs (PushCheck kann missing im selben Lauf setzen,
	// MissingDrafts). nil: CheckAfterSync liest den Stand selbst zu Beginn
	// der Prüfung (direkte Aufrufe).
	MissingBefore map[int64]bool
}

// MissingDrafts liefert die offenen Entwürfe des Kontos mit remote_state
// missing (für CheckOptions.MissingBefore, vor Beginn eines Laufs).
func (s *Service) MissingDrafts(ctx context.Context, accountID int64) (map[int64]bool, error) {
	open, err := s.Store.ListDrafts(ctx, store.DraftFilter{AccountID: accountID, Status: openStatuses})
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, d := range open {
		if d.RemoteState == RemoteMissing {
			out[d.ID] = true
		}
	}
	return out, nil
}

// CheckAfterSync führt die Prüfung nach dem Spiegeln für ein Konto aus
// (siehe oben). Ein Fehler bricht ab; was bis dahin gespeichert ist, bleibt
// (jeder Schritt ist eine eigene kurze Transaktion und beim nächsten Lauf
// wiederholbar).
func (s *Service) CheckAfterSync(ctx context.Context, accountID int64, o CheckOptions) (CheckReport, error) {
	var rep CheckReport
	if !o.SkipSent {
		if o.MissingBefore == nil {
			m, err := s.MissingDrafts(ctx, accountID)
			if err != nil {
				return rep, err
			}
			o.MissingBefore = m
		}
		if err := s.detectSent(ctx, accountID, o.KeepMark, o.MissingBefore, &rep); err != nil {
			return rep, err
		}
	}
	ids, err := s.ExpireApprovals(ctx, accountID)
	rep.Expired = len(ids)
	if err != nil || o.SkipSent {
		return rep, err
	}
	if err := s.settleClosedAttempts(ctx, accountID, &rep); err != nil {
		return rep, err
	}
	return rep, s.refreshRemote(ctx, accountID, &rep)
}

// settleClosedAttempts erledigt offene Ablageversuche abgeschlossener
// Entwürfe (A7): Der Lauf brach zwischen APPEND und Vermerk ab, danach
// wurde der Entwurf verworfen oder als gesendet markiert. Der Entwürfe-
// Ordner ist in diesem Lauf fehlerfrei gespiegelt (sonst SkipSent): Liegt
// die Kopie dort, wird sie übernommen (pushed_after_close, Audit adopted,
// draft show nennt sie; nichts wird gelöscht); sonst kam der Versuch nie an
// und wird mit attempt_without_copy abgeschlossen. Das Audit ist danach
// vollständig (kein Start-Eintrag ohne Abschluss).
func (s *Service) settleClosedAttempts(ctx context.Context, accountID int64, rep *CheckReport) error {
	ids, err := s.Store.ClosedDraftsWithAttempt(ctx, accountID)
	if err != nil {
		return err
	}
	now := s.now()
	for _, id := range ids {
		err := s.tx(ctx, func(x *store.DraftTx) error {
			d, err := x.Draft(id)
			if err != nil || d.PushAttempt == nil || !Status(d.Status).Closed() {
				return err
			}
			adopted, err := s.adoptOrphan(x, &d, now)
			if err != nil {
				return err
			}
			if adopted {
				rep.AdoptedClosed++
				return nil
			}
			if err := s.endAttempt(x, &d, now, ReasonAttemptWithoutCopy); err != nil {
				return err
			}
			rep.LostAttempts++
			return x.UpdateDraft(d)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// detectSent erkennt gesendete Entwürfe: zuerst über die Message-ID, für
// die übrigen über den Rückfall-Abgleich, danach über den Verlauf (A05
// Teil 2, detectThread). Eine gesendete Nachricht schließt höchstens einen
// Entwurf; passen im Rückfall bzw. im Verlauf mehrere, gilt die Rangfolge
// aus pairBefore (A07 Teil 2: höchste Ähnlichkeit, dann taken_over, dann
// die jüngere Fassung, dann die älteste Nachricht).
//
// Der Weg über die Message-ID fragt je offenem Entwurf den Index
// messages_message_id (billig, jedes Mal vollständig). Der Rückfall prüft
// nur, was neu ist (Gegenprüfung S2.6–S2.8, W2; detectFallback).
func (s *Service) detectSent(ctx context.Context, accountID int64, keepMark bool, missingBefore map[int64]bool, rep *CheckReport) error {
	open, err := s.Store.ListDrafts(ctx, store.DraftFilter{AccountID: accountID, Status: openStatuses})
	if err != nil {
		return err
	}
	sort.Slice(open, func(i, j int) bool { return open[i].ID < open[j].ID })
	// K3: Kopien, die PostKauz abgelegt hat, sind nie gesendete Nachrichten,
	// auch wenn der Spiegel sie nicht im Entwürfe-Ordner sieht.
	copies, err := s.Store.DraftCopyHashes(ctx, accountID)
	if err != nil {
		return err
	}
	claimed := map[int64]bool{}
	var rest []store.Draft
	for _, d := range open {
		if d.MessageIDHeader != "" {
			mid, ok, err := s.Store.SentByMessageID(ctx, accountID, d.MessageIDHeader, copies)
			if err != nil {
				return err
			}
			if ok && !claimed[mid] {
				claimed[mid] = true
				if done, err := s.markSent(ctx, d.ID, mid, DetectMessageID, ""); err != nil {
					return err
				} else if done {
					rep.SentByMessageID++
					rep.SentDraftIDs = append(rep.SentDraftIDs, d.ID)
				}
				continue
			}
		}
		rest = append(rest, d)
	}
	before := len(rep.SentDraftIDs)
	if err := s.detectFallback(ctx, accountID, rest, claimed, copies, keepMark, rep); err != nil {
		return err
	}
	closed := map[int64]bool{}
	for _, id := range rep.SentDraftIDs[before:] {
		closed[id] = true
	}
	left := rest[:0:0]
	for _, d := range rest {
		if !closed[d.ID] {
			left = append(left, d)
		}
	}
	before = len(rep.SentDraftIDs)
	if err := s.detectThread(ctx, accountID, left, claimed, copies, rep); err != nil {
		return err
	}
	// A07 Teil 3: neue Mails über den Betreff (läuft auch bei KeepMark,
	// wie Rückfall und Verlauf).
	for _, id := range rep.SentDraftIDs[before:] {
		closed[id] = true
	}
	left = drop(left, closed)
	before = len(rep.SentDraftIDs)
	if err := s.detectSubject(ctx, accountID, left, claimed, copies, rep); err != nil {
		return err
	}
	// A06 Teil 4: Was danach noch offen ist, kann eine eigene Antwort
	// ersetzt haben. Nicht bei KeepMark: Gesendet bzw. „Alle Nachrichten“
	// fehlte im Lauf, und falsches Schließen ist endgültig.
	if keepMark {
		return nil
	}
	for _, id := range rep.SentDraftIDs[before:] {
		closed[id] = true
	}
	rest = drop(left, closed)
	if err := s.detectReplaced(ctx, accountID, rest, copies, missingBefore, rep); err != nil {
		return err
	}
	// A07 Teil 4: Auffangregel „Kopie weg“ als letzter Weg.
	for _, id := range rep.ReplacedDraftIDs {
		closed[id] = true
	}
	return s.detectCopyGone(ctx, drop(rest, closed), rep)
}

// drop liefert die Entwürfe aus list, die nicht in closed stehen (neue
// Liste).
func drop(list []store.Draft, closed map[int64]bool) []store.Draft {
	out := list[:0:0]
	for _, d := range list {
		if !closed[d.ID] {
			out = append(out, d)
		}
	}
	return out
}

// detectFallback ist der Rückfall-Abgleich (E5) für die offenen Entwürfe
// open. Er lädt nicht mehr alle gesendeten Nachrichten im Zeitfenster aller
// offenen Entwürfe (offene draft-Entwürfe verfallen nie; bei Gmail waren
// das bald alle Nachrichten aus „Alle Nachrichten"), sondern nur
// (Gegenprüfung S2.6–S2.8, W2):
//
//   - die seit der letzten Prüfung neu gespiegelten Nachrichten (messages.id
//     über der Hochwassermarke des Kontos, sent_check_marks), verglichen
//     mit allen offenen Entwürfen, und
//   - für Entwürfe, deren Inhalt sich seit der letzten Prüfung geändert hat
//     oder die neu sind (fallback_checked_at ≠ content_changed_at), die
//     Nachrichten in ihrem eigenen Zeitfenster (zusammengelegt, wo sich
//     Fenster überschneiden). Ohne Marke (neues Konto, Bestand vor
//     Migration 0016) gilt das für alle offenen Entwürfe.
//
// Empfängerschlüssel und Betreff eines Kandidaten werden einmal berechnet;
// die Entwürfe liegen in einer Map nach (Empfänger, Betreff), und nur
// Entwürfe, deren Fenster in die Spanne der Kandidaten fällt, kommen
// hinein. Text und Signatur eines Entwurfs werden erst gelesen, wenn ein
// Kandidat zu Empfängern und Betreff passt. Erst nach einer vollständigen
// Prüfung steigen Marke und fallback_checked_at; ein Fehler lässt beides,
// und der nächste Lauf prüft dieselben Nachrichten erneut (zugeordnete
// zählen dann nicht mehr).
func (s *Service) detectFallback(ctx context.Context, accountID int64, open []store.Draft, claimed map[int64]bool, copies []string,
	keepMark bool, rep *CheckReport) error {
	isCopy := map[string]bool{}
	for _, h := range copies {
		isCopy[h] = true
	}
	last, marked, err := s.Store.SentCheckMark(ctx, accountID)
	if err != nil {
		return err
	}
	upto, err := s.Store.MaxMessageID(ctx)
	if err != nil {
		return err
	}
	// A05 Teil 1: Nach einer Änderung der Regeln (SentCheckRev) wird jeder
	// offene Entwurf einmal neu über sein ganzes Fenster geprüft, auch
	// wenn sein Inhalt gleich blieb.
	rev, err := s.Store.SentCheckRev(ctx, accountID)
	if err != nil {
		return err
	}
	recheck := rev < SentCheckRev
	// Entwürfe, deren ganzes Fenster zu prüfen ist (neu oder geändert);
	// ohne Empfänger in To/Cc kann der Rückfall nie treffen (K7: ihr Fenster
	// zählt nicht).
	var changed []store.Draft
	var windows [][2]time.Time
	for _, d := range open {
		if marked && !recheck && d.FallbackCheckedAt.Equal(d.ContentChangedAt) {
			continue
		}
		changed = append(changed, d)
		if RecipientKey(d.To, d.Cc) != "" {
			at := changedAt(d)
			windows = append(windows, [2]time.Time{at.Add(-FallbackBefore), at.Add(FallbackAfter)})
		}
	}
	var cands []store.SentMessage
	seen := map[int64]bool{}
	add := func(list []store.SentMessage) {
		for _, c := range list {
			if !seen[c.ID] && !(c.ContentHash != "" && isCopy[c.ContentHash]) {
				seen[c.ID] = true
				cands = append(cands, c)
			}
		}
	}
	if marked && upto > last && len(open) > 0 {
		list, err := s.Store.NewSentCandidates(ctx, accountID, last, upto)
		if err != nil {
			return err
		}
		add(list)
	}
	for _, w := range mergeWindows(windows) {
		list, err := s.Store.SentCandidates(ctx, accountID, w[0], w[1])
		if err != nil {
			return err
		}
		add(list)
	}
	rep.Examined += len(cands)
	if len(cands) > 0 {
		if err := s.matchFallback(ctx, accountID, open, cands, claimed, rep); err != nil {
			return err
		}
	}
	if keepMark {
		return nil // K3: Gesendet nicht vollständig gespiegelt, nichts vermerken
	}
	for _, d := range changed {
		if err := s.Store.SetFallbackChecked(ctx, d.ID, d.ContentChangedAt); err != nil {
			return err
		}
	}
	if recheck {
		if err := s.Store.SetSentCheckRev(ctx, accountID, SentCheckRev); err != nil {
			return err
		}
	}
	if upto > last || !marked {
		return s.Store.SetSentCheckMark(ctx, accountID, upto)
	}
	return nil
}

// SentCheckRev ist der Stand der Regeln des Rückfalls. Erhöhen, wenn sich
// eine Regel so ändert, dass schon geprüfte offene Entwürfe jetzt treffen
// könnten; die nächste vollständige Prüfung prüft dann jeden offenen
// Entwurf einmal über sein ganzes Fenster (wie einen neuen) und vermerkt
// den Stand erst danach (nicht bei KeepMark, nicht nach einem Fehler).
//
//	1  A05 Teil 1: symmetrische Kürzung des Zitats (Outlook-Verlauf)
//	2  A06 Teil 3: verwaiste Fortsetzungszeilen umbrochener Zitatzeilen
//	   (sentText, sanitize.JoinWrappedQuotes); eine Antwort mit eigenem
//	   Betreff, die Gmail ohne In-Reply-To sendet, findet nur der Rückfall
const SentCheckRev = 2

// changedAt ist die letzte Inhaltsänderung eines Entwurfs (Bestand ohne
// content_changed_at: updated_at).
func changedAt(d store.Draft) time.Time {
	if d.ContentChangedAt.IsZero() {
		return d.UpdatedAt
	}
	return d.ContentChangedAt
}

// mergeWindows legt sich überschneidende Zeitfenster zusammen (eine Abfrage
// je zusammenhängender Spanne).
func mergeWindows(ws [][2]time.Time) [][2]time.Time {
	sort.Slice(ws, func(i, j int) bool { return ws[i][0].Before(ws[j][0]) })
	var out [][2]time.Time
	for _, w := range ws {
		if n := len(out); n > 0 && !w[0].After(out[n-1][1]) {
			if w[1].After(out[n-1][1]) {
				out[n-1][1] = w[1]
			}
			continue
		}
		out = append(out, w)
	}
	return out
}

// fallbackKey ist der Schlüssel der Map im Rückfall: Empfängermenge und
// normalisierter Betreff.
func fallbackKey(recipients, subject string) string {
	return recipients + "\x00" + subject
}

// matchFallback vergleicht die Kandidaten cands (nach Datum sortiert) mit
// den offenen Entwürfen und schließt die Treffer (siehe detectSent).
func (s *Service) matchFallback(ctx context.Context, accountID int64, open []store.Draft, cands []store.SentMessage,
	claimed map[int64]bool, rep *CheckReport) error {
	sort.SliceStable(cands, func(i, j int) bool {
		if !cands[i].Date.Equal(cands[j].Date) {
			return cands[i].Date.Before(cands[j].Date)
		}
		return cands[i].ID < cands[j].ID
	})
	// Nur Entwürfe, deren Fenster die Spanne der Kandidaten berührt.
	first, lastDate := cands[0].Date, cands[len(cands)-1].Date
	type entry struct {
		d        store.Draft
		fd       *FallbackDraft // nil = noch nicht vorbereitet
		unusable bool           // ohne eigenen Text
		fp       string         // Fingerprint des verglichenen Stands (K4)
	}
	var entries []*entry
	index := map[string][]*entry{}
	for _, d := range open {
		at := changedAt(d)
		if lastDate.Before(at.Add(-FallbackBefore)) || first.After(at.Add(FallbackAfter)) {
			continue // Fenster abgelaufen bzw. noch nicht erreicht
		}
		rk := RecipientKey(d.To, d.Cc)
		if rk == "" {
			continue // nichts, woran sich der Rückfall festhalten könnte
		}
		e := &entry{d: d}
		entries = append(entries, e)
		k := fallbackKey(rk, NormalizeSubject(d.Subject))
		index[k] = append(index[k], e)
	}
	if len(entries) == 0 {
		return nil
	}
	sigs := map[int64]string{}
	prepare := func(e *entry) error {
		if e.fd != nil || e.unusable {
			return nil
		}
		d := e.d
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
		fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, d.BodyHTML), sig)
		if len(fd.Words) == 0 {
			e.unusable = true
			return nil
		}
		e.fd, e.fp = &fd, Fingerprint(d)
		return nil
	}
	var own mailaddr.Set
	type pair struct {
		e     *entry
		c     int
		score float64
	}
	var pairs []pair
	for ci, c := range cands {
		if claimed[c.ID] {
			continue
		}
		list := index[fallbackKey(RecipientKey(c.To, c.Cc), NormalizeSubject(c.Subject))]
		if len(list) == 0 {
			continue
		}
		if !c.InSent {
			// Nur „Alle Nachrichten" (Gmail): Dort liegen auch empfangene
			// Mails; gesendet hat nur, wer von einer eigenen Adresse
			// schreibt (Konto, Identitäten, Aliasse; Entscheid 26).
			if own == nil {
				addrs, err := s.Store.OwnAddresses(ctx, accountID)
				if err != nil {
					return err
				}
				// Gmail: Punkte, „+“, googlemail.com (Punkt 15).
				own = mailaddr.NewSet(addrs...)
			}
			if !own.Has(fromAddress(c.From)) {
				continue
			}
		}
		var w *SentWords // Wortmengen des gesendeten Texts, nur bei Bedarf gelesen
		for _, e := range list {
			if !InFallbackWindow(changedAt(e.d), c.Date) {
				continue
			}
			if err := prepare(e); err != nil {
				return err
			}
			if e.unusable {
				continue
			}
			if w == nil {
				b, err := s.Store.MessageBody(ctx, c.ID)
				if err != nil {
					return err
				}
				sw := NewSentWords(sentText(b))
				w = &sw
			}
			if sc := e.fd.ScoreSent(*w); sc >= FallbackMinJaccard {
				pairs = append(pairs, pair{e, ci, sc})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		return pairBefore(a.score, b.score, a.e.d, b.e.d, cands[a.c], cands[b.c], a.c, b.c)
	})
	used := map[int64]bool{}
	for _, p := range pairs {
		c := cands[p.c]
		id := p.e.d.ID
		if used[id] || claimed[c.ID] {
			continue
		}
		used[id], claimed[c.ID] = true, true
		done, err := s.markSent(ctx, id, c.ID, DetectFallback, p.e.fp)
		if err != nil {
			return err
		}
		if done {
			rep.SentByFallback++
			rep.SentDraftIDs = append(rep.SentDraftIDs, id)
		}
	}
	return nil
}

// pairBefore ist die Rangfolge der Paare (Entwurf, gesendete Nachricht) in
// Rückfall, Verlauf und Betreff (A07 Teil 2): zuerst die höhere
// Ähnlichkeit (auf drei Nachkommastellen gerundet), bei Gleichstand der
// Entwurf mit remote_state taken_over (der Mensch hat ihn im Mailprogramm
// angefasst), dann der mit dem jüngeren content_changed_at (die neuere
// Fassung), dann die ältere Nachricht (Datum, dann Reihenfolge ci/cj bzw.
// lokale ID), dann die kleinere Entwurfs-ID. Vorher gewann bei Gleichstand
// der älteste Entwurf; zwei Fassungen derselben Antwort bekamen so die
// gesendete Nachricht der älteren, obwohl der Mensch die jüngere gesendet
// hatte (Befund 09.11.2026). Bekannte Grenze: Bei zwei gleichen Entwürfen
// und zwei Nachrichten bekommt der jüngere Entwurf die ältere Nachricht;
// für den Zustand gleichgültig.
func pairBefore(sa, sb float64, da, db store.Draft, ca, cb store.SentMessage, ci, cj int) bool {
	if ra, rb := roundScore(sa), roundScore(sb); ra != rb {
		return ra > rb
	}
	if ta, tb := da.RemoteState == RemoteTakenOver, db.RemoteState == RemoteTakenOver; ta != tb {
		return ta
	}
	if a, b := changedAt(da), changedAt(db); !a.Equal(b) {
		return a.After(b)
	}
	if !ca.Date.Equal(cb.Date) {
		return ca.Date.Before(cb.Date)
	}
	if ci != cj {
		return ci < cj
	}
	if ca.ID != cb.ID {
		return ca.ID < cb.ID
	}
	return da.ID < db.ID
}

// roundScore rundet eine Ähnlichkeit auf drei Nachkommastellen (Rangfolge,
// pairBefore): Rundungsrauschen in der letzten Stelle entscheidet nicht.
func roundScore(s float64) int64 { return int64(math.Round(s * 1000)) }

// sentText ist der zitatfreie Text einer gesendeten Nachricht für den
// Vergleich in Rückfall und Verlauf. Seit A06 Teil 3 aus dem Text vor der
// Zitatentfernung, nicht mehr aus text_unquoted: dieselbe Wahl wie
// mailsync.fillClean (Klartext, sonst Text aus dem HTML), dann
// sanitize.JoinWrappedQuotes (verwaiste Fortsetzungszeilen umbrochener
// Zitatzeilen an die Zeile mit „>“), dann StripQuotes. Spiegel, Suche und
// Snippet behalten text_unquoted unverändert.
func sentText(b store.Body) string {
	main := b.TextPlain
	if strings.TrimSpace(main) == "" {
		main = b.TextHTML
	}
	if strings.TrimSpace(main) == "" {
		return b.TextUnquoted
	}
	return sanitize.StripQuotes(sanitize.JoinWrappedQuotes(main))
}

// markSent schließt einen Entwurf als gesendet (SentDetected). Ist er
// inzwischen abgeschlossen (ein anderer Prozess war schneller: mark-sent,
// abandon) oder sein Inhalt nicht mehr der verglichene (expect, K4), ist
// das kein Fehler; done ist dann false.
func (s *Service) markSent(ctx context.Context, id, messageID int64, reason, expect string) (bool, error) {
	_, err := s.SentDetected(ctx, id, messageID, reason, expect)
	switch ReasonOf(err) {
	case ReasonDraftClosed, ReasonInvalidTransition, ReasonDraftChanged:
		return false, nil
	}
	return err == nil, err
}

// refreshRemote frischt remote_state der offenen Entwürfe mit abgelegter
// Kopie auf (pushed → missing oder taken_over), anhand des Spiegels. Seit
// A07 Teil 2 auch taken_over → missing: Liegt laut Spiegel weder die
// zuletzt abgelegte Kopie noch eine Fassung mit der Message-ID des Entwurfs
// im Entwürfe-Ordner (checkCopy, dieselbe Prüfung wie Bedingung f), hat der
// Mensch die übernommene Fassung gesendet oder gelöscht; dann greifen im
// nächsten Lauf „ersetzt“ (3b) und „Kopie weg“ (3d). Nur dieser Wechsel
// wird geschrieben; bleibt checkCopy bei taken_over oder liefert es "",
// ändert sich nichts und es entsteht kein Audit-Eintrag.
func (s *Service) refreshRemote(ctx context.Context, accountID int64, rep *CheckReport) error {
	open, err := s.Store.ListDrafts(ctx, store.DraftFilter{AccountID: accountID, Status: openStatuses})
	if err != nil {
		return err
	}
	now := s.now()
	for _, o := range open {
		if o.RemoteState == RemoteMissing {
			if err := s.refreshMissing(ctx, o.ID, now); err != nil {
				return err
			}
			continue
		}
		if o.RemoteState != RemotePushed && o.RemoteState != RemoteTakenOver {
			continue
		}
		var state string
		err := s.tx(ctx, func(x *store.DraftTx) error {
			d, err := x.Draft(o.ID)
			if err != nil {
				return err
			}
			if Status(d.Status).Closed() || d.RemoteState != o.RemoteState {
				return nil
			}
			if state, err = checkCopy(x, d); err != nil || state == "" || state == d.RemoteState {
				state = ""
				return err
			}
			if d.RemoteState == RemoteTakenOver && state != RemoteMissing {
				state = ""
				return nil
			}
			return s.markRemote(x, &d, state, now)
		})
		if err != nil {
			return err
		}
		switch state {
		case RemoteMissing:
			rep.Missing++
		case RemoteTakenOver:
			rep.TakenOver++
		}
	}
	return nil
}

// ReasonPresent ist der Grund von drafts.remote_check, wenn die Kopie eines
// missing-Entwurfs nachweisbar wieder im Entwürfe-Ordner liegt (Nachtrag 1
// zu A07, B2); Store.DraftMissingSince zählt missing-Einträge davor nicht.
const ReasonPresent = "present"

// refreshMissing setzt einen missing-Entwurf wieder auf pushed, wenn seine
// zuletzt abgelegte Kopie nachweisbar wieder am Ablageplatz liegt (gleiche
// Message-ID und gleicher Inhalts-Hash, Ordner gespiegelt; „nicht prüfbar“
// genügt nicht), mit Audit drafts.remote_check, Grund present. Sonst
// ändert sich nichts.
func (s *Service) refreshMissing(ctx context.Context, id int64, now time.Time) error {
	return s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		if Status(d.Status).Closed() || d.RemoteState != RemoteMissing {
			return nil
		}
		back, err := copyPresent(x, d)
		if err != nil || !back {
			return err
		}
		d.RemoteState = RemotePushed
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		b, _ := json.Marshal(map[string]any{"draft_id": d.ID, "remote": RemotePushed, "mailbox_id": d.RemoteMailboxID, "uid": d.RemoteUID})
		_, err = x.AppendAudit(audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: OpRemoteCheck,
			ParamsRedacted: b, Targets: []string{"draft:" + strconv.FormatInt(d.ID, 10)}, Result: audit.ResultOK, Reason: ReasonPresent,
			PolicyVersion: s.PolicyVersion})
		return err
	})
}

// copyPresent meldet, ob die zuletzt abgelegte Kopie laut Spiegel belegt
// am Ablageplatz liegt: Ordner gespiegelt, unter der UID (bei gleicher
// UIDVALIDITY) bzw. nach einem UIDVALIDITY-Wechsel irgendwo im Ordner eine
// Nachricht mit der Message-ID und dem Inhalts-Hash des Entwurfs.
func copyPresent(x *store.DraftTx, d store.Draft) (bool, error) {
	if d.PushedContentHash == "" || d.RemoteMailboxID == 0 {
		return false, nil
	}
	mc, err := x.MirrorCopy(d.RemoteMailboxID, d.RemoteUID, d.MessageIDHeader, d.PushedContentHash)
	if err != nil || !mc.MailboxFound || !mc.Synced {
		return false, err
	}
	if d.RemoteUIDValidity != 0 && mc.UIDValidity != d.RemoteUIDValidity {
		if mc, err = x.MirrorCopy(d.RemoteMailboxID, 0, d.MessageIDHeader, d.PushedContentHash); err != nil {
			return false, err
		}
		return mc.SameIDAndHash, nil
	}
	return d.RemoteUID > 0 && mc.AtUID && mc.AtUIDMessageID == d.MessageIDHeader && mc.AtUIDContentHash == d.PushedContentHash, nil
}
