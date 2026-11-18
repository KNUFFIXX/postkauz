package drafts

import (
	"context"
	"sort"
	"strings"
	"time"

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
//	(b) die gesendete Nachricht liegt in Gesendet (seit A10 Teil 1 nur
//	    dort; „Alle Nachrichten" nur bei Konten ohne Gesendet, dann mit
//	    eigenem Absender und ohne Termin in der Zukunft), nicht \Draft, in keinem
//	    Entwürfe-Ordner, keine Kopie, die PostKauz abgelegt hat (dieselben
//	    Ausschlüsse wie bei Message-ID und Rückfall),
//	(c) ihr In-Reply-To ist H (Store.SentReplies),
//	(d) gleiche Empfängermenge (To und Cc) wie beim Rückfall; seit A07
//	    Teil 1 auch erfüllt, wenn der Entwurf in To und Cc keinen
//	    Empfänger hat und die gesendete Nachricht mindestens einen
//	    (threadRecipientsMatch); seit A09 Teil 1 nur hier auch, wenn die
//	    Empfänger des Entwurfs eine echte, nicht leere Teilmenge der
//	    gesendeten sind (der Mensch hat im Mailprogramm nur ergänzt;
//	    threadRecipientsAdded),
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
	ext map[int64]time.Time, rep *CheckReport) error {
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
		added int // A09 Teil 1: Zahl der im Mailprogramm ergänzten Empfänger
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
		added := map[int64]int{}
		for _, c := range list {
			if claimed[c.ID] || (c.ContentHash != "" && isCopy[c.ContentHash]) {
				continue
			}
			ok, n := threadRecipientsAdded(rk, c)
			if !ok || !inWindow(d, c.Date, ext) { // (d), (e); A10 Teil 2: bis Termin + 14 Tage
				continue
			}
			if !c.InSent {
				if c.InternalDate.After(s.now().Add(ScheduledLead)) {
					continue // A10 Teil 1 (c): Termin in der Zukunft, nicht gesendet
				}
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
			added[c.ID] = n
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
				pairs = append(pairs, pair{d, fp, c, sc, added[c.ID]})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if (a.added == 0) != (b.added == 0) {
			return a.added == 0 // A09 Teil 1: gleiche Empfänger vor ergänzten
		}
		return pairBefore(a.score, b.score, a.d, b.d, a.c, b.c, 0, 0) // A07 Teil 2
	})
	used := map[int64]bool{}
	for _, p := range pairs {
		if used[p.d.ID] || claimed[p.c.ID] {
			continue
		}
		used[p.d.ID], claimed[p.c.ID] = true, true
		var extra map[string]any
		if p.added > 0 {
			// A09 Teil 1: nur die Zahl, nie Adressen.
			extra = map[string]any{"recipients_added": p.added}
		}
		done, err := s.markSentWith(ctx, p.d.ID, p.c.ID, DetectThread, p.fp, extra)
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

// threadRecipientsAdded ist Bedingung (d) nur für den Weg über den Verlauf
// (A09 Teil 1, Offene Frage 4): erfüllt wie threadRecipientsMatch (added
// 0) oder, wenn die Empfänger des Entwurfs (To und Cc, normalisiert wie
// RecipientKey) eine echte, nicht leere Teilmenge der Empfänger der
// gesendeten Nachricht (To und Cc) sind; added ist dann die Zahl der
// ergänzten Empfänger. Bcc zählt nicht. Ein Entwurf ohne Empfänger zählt
// nicht als Teilmenge (dafür gilt A07 Teil 1). Rückfall, Betreff (3c) und
// „ersetzt“ (3b, threadRecipientsMatch) bleiben streng.
func threadRecipientsAdded(draftKey string, c store.SentMessage) (ok bool, added int) {
	if threadRecipientsMatch(draftKey, c) {
		return true, 0
	}
	if draftKey == "" {
		return false, 0
	}
	sent := map[string]bool{}
	for _, a := range strings.Split(RecipientKey(c.To, c.Cc), "\n") {
		if a != "" {
			sent[a] = true
		}
	}
	mine := strings.Split(draftKey, "\n")
	for _, a := range mine {
		if !sent[a] {
			return false, 0
		}
	}
	if len(sent) <= len(mine) {
		return false, 0
	}
	return true, len(sent) - len(mine)
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
