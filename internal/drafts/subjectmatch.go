package drafts

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Vierter Weg der Versanderkennung: der Betreff (A07 Teil 3;
// docs/versanderkennung.md, Abschnitt 3c).
//
// Befund (09.11.2026): Eine neue Mail (kind new, ein Empfänger, kein
// Original) wurde in Gmail um einen Satz ergänzt und gesendet; Empfänger
// und Betreff gleich, Kopie weg, Ähnlichkeit 0,763 (< 0,9 des Rückfalls).
// Der Weg über den Verlauf gilt nur für Antworten und Weiterleitungen mit
// Original; für neue Mails gab es nur Message-ID und Rückfall.
//
// Der Weg über den Betreff gilt nur für Entwürfe kind new, nach dem Verlauf
// und vor „ersetzt“ (3b), mit den Bedingungen des Verlaufs, aber (a) und
// (c) ersetzt durch:
//
//	(a') kind new; der Betreff ist nach NormalizeSubject gleich und nicht
//	     leer,
//	(c') die gesendete Nachricht trägt kein In-Reply-To, und ihr roher
//	     Betreff beginnt nicht mit Re:/AW:/Antw:/Fwd:/WG:/FW: (eine neue
//	     Mail; Webmail mancher Anbieter entfernt In-Reply-To aus Antworten).
//
// (d) gilt streng: ein Entwurf ohne To/Cc wird hier nie geschlossen. (b),
// (e), (f) und (g) wie beim Verlauf; (f) und der Fingerabdruck werden beim
// Schließen in derselben Transaktion erneut geprüft (close).
//
// Bei jedem Lauf gezielt je offenem neuen, abgelegten Entwurf mit fehlender
// Kopie (ohne Hochwassermarke, wie der Verlauf): (f) wird vor der Abfrage
// geprüft, die Zeitfenster der übrigen Entwürfe zusammengelegt
// (mergeWindows), Nachrichten ohne In-Reply-To im Fenster zuerst nur nach
// dem Betreff gefiltert (Store.NewMailSubjects), erst die Treffer auf
// „gesendet“ geprüft (Store.SentMessagesByID); der Text erst nach den
// Kopfbedingungen. Rangfolge wie Rückfall und Verlauf (pairBefore).
// SentCheckRev bleibt: Der Weg schlägt bei jedem Lauf nach.

// DetectSubject: Grund im Audit, wenn der Weg über den Betreff trifft.
const DetectSubject = "subject"

// detectSubject ist der Weg über den Betreff für die noch offenen Entwürfe
// open (nach Message-ID, Rückfall und Verlauf). claimed: schon zugeordnete
// gesendete Nachrichten; copies: Inhalts-Hashes der Entwurfskopien.
func (s *Service) detectSubject(ctx context.Context, accountID int64, open []store.Draft, claimed map[int64]bool, copies []string,
	rep *CheckReport) error {
	type cand struct {
		d       store.Draft
		subject string
		rk      string
	}
	var drafts []cand
	var windows [][2]time.Time
	for _, d := range open {
		if d.Kind != string(KindNew) || d.PushedContentHash == "" {
			continue // (a'), (f) nie abgelegt
		}
		rk, subj := RecipientKey(d.To, d.Cc), NormalizeSubject(d.Subject)
		if rk == "" || subj == "" {
			continue // (d) streng, (a') leerer Betreff
		}
		gone, err := s.copyGone(ctx, d) // (f) vor der Abfrage
		if err != nil {
			return err
		}
		if !gone {
			continue
		}
		drafts = append(drafts, cand{d, subj, rk})
		at := changedAt(d)
		windows = append(windows, [2]time.Time{at.Add(-FallbackBefore), at.Add(FallbackAfter)})
	}
	if len(drafts) == 0 {
		return nil
	}
	isCopy := map[string]bool{}
	for _, h := range copies {
		isCopy[h] = true
	}
	// Vorfilter nach dem Betreff (billig, ohne die Bedingung „gesendet“):
	// Ohne unsichtbare Zeichen ist sanitize.Text der rohe Betreff selbst,
	// und jedes Wort des normalisierten Betreffs eines Entwurfs steht darin;
	// erst dann wird normalisiert. Mit unsichtbaren Zeichen wird immer
	// normalisiert (genau wie NormalizeSubject, nur schneller).
	bySubject := map[string]bool{}
	var probes []string
	for _, dc := range drafts {
		if !bySubject[dc.subject] {
			bySubject[dc.subject] = true
			probe := ""
			for _, f := range strings.Fields(dc.subject) {
				if len(f) > len(probe) {
					probe = f // das längste Wort grenzt am stärksten ein
				}
			}
			probes = append(probes, probe)
		}
	}
	var ids []int64
	seen := map[int64]bool{}
	for _, w := range mergeWindows(windows) {
		rows, err := s.Store.NewMailSubjects(ctx, accountID, w[0], w[1]) // (c') ohne In-Reply-To
		if err != nil {
			return err
		}
		for _, r := range rows {
			if seen[r.ID] || claimed[r.ID] {
				continue
			}
			if !strings.ContainsFunc(r.Subject, sanitize.Invisible) && !containsAny(r.Subject, probes) {
				continue
			}
			clean := sanitize.Text(r.Subject)
			if subjectPrefixRe.MatchString(clean) || !bySubject[NormalizeSubject(clean)] {
				continue // (c') Präfix im rohen Betreff, (a') anderer Betreff
			}
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	list, err := s.Store.SentMessagesByID(ctx, accountID, ids) // (b)
	if err != nil {
		return err
	}
	var cands []store.SentMessage
	for _, c := range list {
		if c.ContentHash != "" && isCopy[c.ContentHash] || c.InReplyTo != "" {
			continue
		}
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return nil
	}
	var own mailaddr.Set
	sigs := map[int64]string{}
	texts := map[int64]*SentWords{}
	type pair struct {
		d     store.Draft
		fp    string
		c     store.SentMessage
		score float64
	}
	var pairs []pair
	for _, dc := range drafts {
		d := dc.d
		var fd *FallbackDraft
		for _, c := range cands {
			if NormalizeSubject(c.Subject) != dc.subject || RecipientKey(c.To, c.Cc) != dc.rk || !InFallbackWindow(changedAt(d), c.Date) {
				continue // (a'), (d), (e)
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
					continue // (b) nur „Alle Nachrichten“: eigener Absender
				}
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
			if len(fd.Words) < ThreadMinWords { // (g)
				break
			}
			w := texts[c.ID]
			if w == nil {
				b, err := s.Store.MessageBody(ctx, c.ID)
				if err != nil {
					return err
				}
				sw := NewSentWords(sentText(b))
				w = &sw
				texts[c.ID] = w
			}
			if sc := fd.ScoreSent(*w); sc >= ThreadMinJaccard { // (g)
				pairs = append(pairs, pair{d, Fingerprint(d), c, sc})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		return pairBefore(a.score, b.score, a.d, b.d, a.c, b.c, 0, 0)
	})
	used := map[int64]bool{}
	for _, p := range pairs {
		if used[p.d.ID] || claimed[p.c.ID] {
			continue
		}
		used[p.d.ID], claimed[p.c.ID] = true, true
		done, err := s.markSent(ctx, p.d.ID, p.c.ID, DetectSubject, p.fp)
		if err != nil {
			return err
		}
		if done {
			rep.SentBySubject++
			rep.SentDraftIDs = append(rep.SentDraftIDs, p.d.ID)
		}
	}
	return nil
}

// containsAny meldet, ob s eines der Wörter enthält.
func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
