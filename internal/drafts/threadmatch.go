package drafts

import (
	"context"
	"sort"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Dritter Weg der Versanderkennung: der Verlauf (A05 Teil 2;
// docs/versanderkennung.md, Abschnitt „Dritter Weg: der Verlauf").
//
// Hat der Mensch einen Antwort- oder Weiterleitungs-Entwurf vor dem Senden
// im Mailprogramm geändert (etwa einen Satz ergänzt), trifft weder die
// Message-ID (das Mailprogramm vergibt eine neue) noch der Rückfall
// (Ähnlichkeit unter 0,9). Der Weg über den Verlauf schließt ihn, wenn
// alle Bedingungen zugleich gelten:
//
//	(a) kind reply, reply_all oder forward mit Original (in_reply_to_msg);
//	    H = Message-ID des Originals. Ohne Original (etwa nach purge) gilt
//	    der Weg nicht.
//	(b) die gesendete Nachricht liegt in Gesendet oder „Alle Nachrichten"
//	    (dort nur mit eigenem Absender), nicht \Draft, in keinem
//	    Entwürfe-Ordner, keine Kopie, die PostKauz abgelegt hat (dieselben
//	    Ausschlüsse wie bei Message-ID und Rückfall),
//	(c) ihr In-Reply-To ist H (Store.SentReplies),
//	(d) gleiche Empfängermenge (To und Cc) wie beim Rückfall; seit A07
//	    Teil 1 auch erfüllt, wenn der Entwurf in To und Cc keinen
//	    Empfänger hat und die gesendete Nachricht mindestens einen
//	    (threadRecipientsMatch),
//	(e) Zeitfenster wie beim Rückfall,
//	(f) die Kopie des Entwurfs fehlt im Entwürfe-Ordner, festgestellt am
//	    Spiegel wie checkCopy, unabhängig vom gespeicherten remote_state;
//	    ein nie abgelegter Entwurf erfüllt das nicht,
//	(g) ScoreSent ≥ ThreadMinJaccard (0,5) und der eigene Text des
//	    Entwurfs hat ohne Signatur mindestens ThreadMinWords (8)
//	    verschiedene Wörter.
//
// Der Betreff wird nicht verglichen (In-Reply-To ist das stärkere
// Merkmal). Gebaut wie der Weg über die Message-ID: bei jedem Lauf für
// jeden offenen Entwurf gezielt nachgeschlagen, ohne Hochwassermarke.
// Gelesen werden nur Nachrichten aus dem Verlauf des Originals mit
// passendem In-Reply-To; ihren Text erst, wenn (d), (e) und (f) erfüllt
// sind.

// threadKinds sind die Arten mit Original, für die der Weg gilt (a).
var threadKinds = map[string]bool{string(KindReply): true, string(KindReplyAll): true, string(KindForward): true}

// detectThread ist der Weg über den Verlauf für die noch offenen Entwürfe
// open (nach Message-ID und Rückfall). claimed: schon zugeordnete
// gesendete Nachrichten; copies: Inhalts-Hashes der Entwurfskopien.
func (s *Service) detectThread(ctx context.Context, accountID int64, open []store.Draft, claimed map[int64]bool, copies []string,
	rep *CheckReport) error {
	isCopy := map[string]bool{}
	for _, h := range copies {
		isCopy[h] = true
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
	for _, d := range open {
		rk := RecipientKey(d.To, d.Cc)
		if !threadKinds[d.Kind] || d.InReplyToMsg <= 0 || d.PushedContentHash == "" {
			continue // (a), (f) nie abgelegt
		}
		list, err := s.Store.SentReplies(ctx, accountID, d.InReplyToMsg) // (b), (c)
		if err != nil {
			return err
		}
		var cands []store.SentMessage
		for _, c := range list {
			if claimed[c.ID] || (c.ContentHash != "" && isCopy[c.ContentHash]) {
				continue
			}
			if !threadRecipientsMatch(rk, c) || !InFallbackWindow(changedAt(d), c.Date) { // (d), (e)
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
			cands = append(cands, c)
		}
		if len(cands) == 0 {
			continue
		}
		gone, err := s.copyGone(ctx, d) // (f)
		if err != nil {
			return err
		}
		if !gone {
			continue
		}
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
		if len(fd.Words) < ThreadMinWords { // (g)
			continue
		}
		fp := Fingerprint(d)
		for _, c := range cands {
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
				pairs = append(pairs, pair{d, fp, c, sc})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if !a.c.Date.Equal(b.c.Date) {
			return a.c.Date.Before(b.c.Date)
		}
		if a.c.ID != b.c.ID {
			return a.c.ID < b.c.ID
		}
		return a.d.ID < b.d.ID
	})
	used := map[int64]bool{}
	for _, p := range pairs {
		if used[p.d.ID] || claimed[p.c.ID] {
			continue
		}
		used[p.d.ID], claimed[p.c.ID] = true, true
		done, err := s.markSent(ctx, p.d.ID, p.c.ID, DetectThread, p.fp)
		if err != nil {
			return err
		}
		if done {
			rep.SentByThread++
			rep.SentDraftIDs = append(rep.SentDraftIDs, p.d.ID)
		}
	}
	return nil
}

// threadRecipientsMatch ist Bedingung (d) des Wegs über den Verlauf und der
// Regel „ersetzt“: gleiche Empfängermenge aus To und Cc (Bcc zählt nicht).
// Seit A07 Teil 1 gilt sie auch als erfüllt, wenn der Entwurf in To und Cc
// keinen Empfänger hat (draftKey leer; der Mensch trägt ihn im Mailprogramm
// ein, A04 Teil 2) und die gesendete Nachricht mindestens einen Empfänger in
// To oder Cc hat. Der Rückfall und der Weg über den Betreff bleiben streng.
func threadRecipientsMatch(draftKey string, c store.SentMessage) bool {
	sent := RecipientKey(c.To, c.Cc)
	if draftKey == "" {
		return sent != ""
	}
	return sent == draftKey
}

// copyGone meldet, ob die zuletzt abgelegte Kopie des Entwurfs laut
// Spiegel aus dem Entwürfe-Ordner verschwunden ist (checkCopy ergibt
// missing): weder die Kopie noch eine neuere Fassung mit seiner Message-ID
// (übernommen) liegt dort. Gelesen wird der aktuelle Stand des Entwurfs,
// nicht der gespeicherte remote_state (A05 Teil 2, Bedingung f).
func (s *Service) copyGone(ctx context.Context, d store.Draft) (bool, error) {
	var state string
	err := s.Store.DraftTx(ctx, func(x *store.DraftTx) error {
		cur, err := x.Draft(d.ID)
		if err != nil {
			return err
		}
		state, err = checkCopy(x, cur)
		return err
	})
	return state == RemoteMissing, err
}
