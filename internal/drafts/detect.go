package drafts

import (
	"context"
	"sort"

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
func (s *Service) detectSent(ctx context.Context, accountID int64, rep *CheckReport) error {
	open, err := s.Store.ListDrafts(ctx, store.DraftFilter{AccountID: accountID, Status: openStatuses})
	if err != nil || len(open) == 0 {
		return err
	}
	sort.Slice(open, func(i, j int) bool { return open[i].ID < open[j].ID })
	claimed := map[int64]bool{}
	var rest []store.Draft
	for _, d := range open {
		if d.MessageIDHeader != "" {
			mid, ok, err := s.Store.SentByMessageID(ctx, accountID, d.MessageIDHeader)
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
	if len(rest) == 0 {
		return nil
	}
	return s.detectFallback(ctx, accountID, rest, claimed, rep)
}

func (s *Service) detectFallback(ctx context.Context, accountID int64, open []store.Draft, claimed map[int64]bool, rep *CheckReport) error {
	fds := make([]FallbackDraft, 0, len(open))
	fps := map[int64]string{} // Fingerprint des verglichenen Stands (K4)
	from, to := open[0].ContentChangedAt, open[0].ContentChangedAt
	sigs := map[int64]string{}
	for _, d := range open {
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
		if fd.Recipients == "" || len(fd.Words) == 0 {
			continue // nichts, woran sich der Rückfall festhalten könnte
		}
		fds = append(fds, fd)
		fps[d.ID] = Fingerprint(d)
		if d.ContentChangedAt.Before(from) {
			from = d.ContentChangedAt
		}
		if d.ContentChangedAt.After(to) {
			to = d.ContentChangedAt
		}
	}
	if len(fds) == 0 {
		return nil
	}
	cands, err := s.Store.SentCandidates(ctx, accountID, from.Add(-FallbackBefore), to.Add(FallbackAfter))
	if err != nil || len(cands) == 0 {
		return err
	}
	var own map[string]bool
	type pair struct {
		d, c  int
		score float64
	}
	var pairs []pair
	words := map[int64]map[string]struct{}{} // Wortmengen der gesendeten Texte, nur bei Bedarf gelesen
	for ci, c := range cands {
		if claimed[c.ID] {
			continue
		}
		if !c.InSent {
			// Nur „Alle Nachrichten" (Gmail): Dort liegen auch empfangene
			// Mails; gesendet hat nur, wer von einer eigenen Adresse
			// schreibt (Konto, Identitäten, Aliasse; Entscheid 26).
			if own == nil {
				list, err := s.Store.OwnAddresses(ctx, accountID)
				if err != nil {
					return err
				}
				own = map[string]bool{}
				for _, a := range list {
					own[normAddr(a)] = true
				}
			}
			if !own[fromAddress(c.From)] {
				continue
			}
		}
		for di, fd := range fds {
			if !fd.HeadersMatch(c) {
				continue
			}
			w, ok := words[c.ID]
			if !ok {
				b, err := s.Store.MessageBody(ctx, c.ID)
				if err != nil {
					return err
				}
				w = Words(SentOwnText(sentText(b)))
				words[c.ID] = w
			}
			if sc := fd.Score(w); sc >= FallbackMinJaccard {
				pairs = append(pairs, pair{di, ci, sc})
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
		return fds[a.d].ID < fds[b.d].ID
	})
	usedDraft := map[int]bool{}
	for _, p := range pairs {
		c := cands[p.c]
		if usedDraft[p.d] || claimed[c.ID] {
			continue
		}
		usedDraft[p.d], claimed[c.ID] = true, true
		id := fds[p.d].ID
		done, err := s.markSent(ctx, id, c.ID, DetectFallback, fps[id])
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
