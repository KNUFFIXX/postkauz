package drafts

import (
	"context"
	"sort"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Prüfung nach dem Spiegeln (S2.6): Der Sync ruft CheckAfterSync am Ende
// jedes Laufs auf (postkauz sync und jede Runde von sync --watch), unter
// der Kontosperre. Sie
//
//  1. erkennt gesendete Entwürfe (E5, sentmatch.go): sent_by_human,
//     sent_message_id, Audit drafts.sent_detected mit Grund message_id
//     oder fallback (Akteur system, nur IDs und Zustände),
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
// draft mark-sent oder draft abandon schließt.

// CheckReport fasst die Prüfung nach dem Spiegeln zusammen (Zähler und
// Draft-IDs, kein Inhalt).
type CheckReport struct {
	// SentByMessageID, SentByFallback: als gesendet erkannte Entwürfe.
	SentByMessageID int `json:"sent_by_message_id,omitempty"`
	SentByFallback  int `json:"sent_by_fallback,omitempty"`
	// Expired: Entwürfe, deren Freigabe abgelaufen ist (zurück auf draft).
	Expired int `json:"approval_expired,omitempty"`
	// Missing, TakenOver: Entwürfe, deren Kopie im Entwürfe-Ordner jetzt
	// fehlt bzw. im Mailclient bearbeitet wurde.
	Missing   int `json:"missing,omitempty"`
	TakenOver int `json:"taken_over,omitempty"`
	// SentDraftIDs: die als gesendet erkannten Entwürfe.
	SentDraftIDs []int64 `json:"sent_draft_ids,omitempty"`
	// Examined: gesendete Nachrichten, die der Rückfall-Abgleich geladen
	// hat (neue und solche im Fenster neuer oder geänderter Entwürfe; W2).
	// Nur für Tests und Messungen, nicht in der Ausgabe.
	Examined int `json:"-"`
}

// Empty meldet, ob die Prüfung nichts geändert hat.
func (r CheckReport) Empty() bool {
	return r.SentByMessageID+r.SentByFallback+r.Expired+r.Missing+r.TakenOver == 0
}

// Add zählt o dazu (Dauerbetrieb: eine Prüfung je Runde).
func (r *CheckReport) Add(o CheckReport) {
	r.SentByMessageID += o.SentByMessageID
	r.SentByFallback += o.SentByFallback
	r.Expired += o.Expired
	r.Missing += o.Missing
	r.TakenOver += o.TakenOver
	r.SentDraftIDs = append(r.SentDraftIDs, o.SentDraftIDs...)
	r.Examined += o.Examined
}

var openStatuses = []string{string(StatusDraft), string(StatusPendingApproval), string(StatusReady)}

// CheckAfterSync führt die Prüfung nach dem Spiegeln für ein Konto aus
// (siehe oben). Ein Fehler bricht ab; was bis dahin gespeichert ist, bleibt
// (jeder Schritt ist eine eigene kurze Transaktion und beim nächsten Lauf
// wiederholbar).
func (s *Service) CheckAfterSync(ctx context.Context, accountID int64) (CheckReport, error) {
	var rep CheckReport
	if err := s.detectSent(ctx, accountID, &rep); err != nil {
		return rep, err
	}
	ids, err := s.ExpireApprovals(ctx, accountID)
	rep.Expired = len(ids)
	if err != nil {
		return rep, err
	}
	return rep, s.refreshRemote(ctx, accountID, &rep)
}

// detectSent erkennt gesendete Entwürfe: zuerst über die Message-ID, für
// die übrigen über den Rückfall-Abgleich. Eine gesendete Nachricht schließt
// höchstens einen Entwurf; passen im Rückfall mehrere, gewinnt die höchste
// Ähnlichkeit, dann die älteste Nachricht, dann der älteste Entwurf.
//
// Der Weg über die Message-ID fragt je offenem Entwurf den Index
// messages_message_id (billig, jedes Mal vollständig). Der Rückfall prüft
// nur, was neu ist (Gegenprüfung S2.6–S2.8, W2; detectFallback).
func (s *Service) detectSent(ctx context.Context, accountID int64, rep *CheckReport) error {
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
	return s.detectFallback(ctx, accountID, rest, claimed, copies, rep)
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
	rep *CheckReport) error {
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
	// Entwürfe, deren ganzes Fenster zu prüfen ist (neu oder geändert);
	// ohne Empfänger in To/Cc kann der Rückfall nie treffen (K7: ihr Fenster
	// zählt nicht).
	var changed []store.Draft
	var windows [][2]time.Time
	for _, d := range open {
		if marked && d.FallbackCheckedAt.Equal(d.ContentChangedAt) {
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
	for _, d := range changed {
		if err := s.Store.SetFallbackChecked(ctx, d.ID, d.ContentChangedAt); err != nil {
			return err
		}
	}
	if upto > last || !marked {
		return s.Store.SetSentCheckMark(ctx, accountID, upto)
	}
	return nil
}

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
	var own map[string]bool
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
				own = map[string]bool{}
				for _, a := range addrs {
					own[normAddr(a)] = true
				}
			}
			if !own[fromAddress(c.From)] {
				continue
			}
		}
		var w map[string]struct{} // Wortmenge des gesendeten Texts, nur bei Bedarf gelesen
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
				w = Words(SentOwnText(sentText(b)))
			}
			if sc := e.fd.Score(w); sc >= FallbackMinJaccard {
				pairs = append(pairs, pair{e, ci, sc})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.c != b.c {
			return a.c < b.c // Kandidaten sind nach Datum sortiert
		}
		return a.e.d.ID < b.e.d.ID
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

// sentText ist der zitatfreie Text einer gespiegelten Nachricht; bei
// Nachrichten aus älteren Versionen ohne text_unquoted der bereinigte
// Klartext bzw. der Text aus dem HTML ohne Zitat.
func sentText(b store.Body) string {
	switch {
	case b.TextUnquoted != "":
		return b.TextUnquoted
	case b.TextPlain != "":
		return sanitize.StripQuotes(b.TextPlain)
	}
	return sanitize.StripQuotes(b.TextHTML)
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
// Kopie auf (pushed → missing oder taken_over), anhand des Spiegels.
func (s *Service) refreshRemote(ctx context.Context, accountID int64, rep *CheckReport) error {
	open, err := s.Store.ListDrafts(ctx, store.DraftFilter{AccountID: accountID, Status: openStatuses})
	if err != nil {
		return err
	}
	now := s.now()
	for _, o := range open {
		if o.RemoteState != RemotePushed {
			continue
		}
		var state string
		err := s.tx(ctx, func(x *store.DraftTx) error {
			d, err := x.Draft(o.ID)
			if err != nil {
				return err
			}
			if Status(d.Status).Closed() || d.RemoteState != RemotePushed {
				return nil
			}
			if state, err = checkCopy(x, d); err != nil || state == "" {
				return err
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
