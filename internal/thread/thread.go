// Package thread ordnet Nachrichten zu Threads (Gesprächsfäden).
//
// Verfahren (docs/datenmodell.md, Tabelle threads):
//
//  1. Kopfzeilen: Message-ID, In-Reply-To und References verbinden
//     Nachrichten. Jede genannte ID ist ein Knoten, auch wenn die Nachricht
//     dazu fehlt (fehlende Zwischenglieder verbinden trotzdem). Verweisen
//     die IDs einer Nachricht auf mehrere Threads, werden diese vereinigt
//     (wie Union-Find; das Ergebnis hängt nicht von der Reihenfolge ab).
//  2. Rückfall Betreff: Eine Antwort ohne References und In-Reply-To
//     („Waise", Betreff mit Re:/AW:/WG:/Fwd: …) kommt in den Thread mit
//     gleichem normalisierten Betreff, dessen Zeitspanne höchstens 30 Tage
//     entfernt ist und mit dem sie mindestens einen Beteiligten (From, To,
//     Cc; ohne die eigenen Adressen des Kontos) teilt. Umgekehrt sammelt
//     ein Thread Waisen-Threads gleichen Betreffs im Zeitfenster mit
//     gemeinsamen Beteiligten ein, wenn er wächst; so ist auch dieser
//     Rückfall unabhängig davon, ob die Waise vor oder nach dem Rest
//     ankommt. Nachrichten ohne Antwort-Präfix werden nie allein über den
//     Betreff verbunden (sonst landeten gleichlautende Rundschreiben in
//     einem Thread); ohne gemeinsame Beteiligte auch Antworten nicht
//     („AW: Rechnung" zweier fremder Absender bleibt getrennt).
//
// Assign arbeitet gegen eine Index-Schnittstelle: im Speicher (Group, Tests)
// und in SQLite (internal/store, beim Sync). Beide nutzen dieselbe
// Entscheidung.
package thread

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Window ist das Zeitfenster des Betreff-Rückfalls.
const Window = 30 * 24 * time.Hour

// Message ist eine Nachricht aus Sicht des Threadings.
type Message struct {
	// MessageID ohne spitze Klammern; leer, wenn die Nachricht keine hat.
	MessageID  string
	InReplyTo  []string
	References []string
	Subject    string
	// Date ist das Datum der Nachricht (Kopfzeile, sonst INTERNALDATE);
	// Nullzeit = unbekannt (dann kein Betreff-Rückfall).
	Date time.Time
	// Participants: Adressen aus From, To und Cc (NormalizeAddress), ohne
	// die eigenen Adressen des Kontos. Leer = kein Betreff-Rückfall.
	Participants []string
}

// NormalizeAddress liefert die Adresse aus „Name <adresse>" oder
// „adresse", klein geschrieben, ohne Leerraum; "" ohne „@".
func NormalizeAddress(s string) string {
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			s = s[i+1 : i+j]
		}
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if !strings.Contains(s, "@") {
		return ""
	}
	return s
}

// Overlap meldet, ob a und b mindestens eine Adresse gemeinsam haben.
func Overlap(a, b []string) bool {
	for _, x := range a {
		if x != "" && slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// refs liefert die Verweise ohne die eigene ID, ohne Dubletten.
func (m Message) refs() []string {
	var out []string
	for _, r := range append(slices.Clone(m.References), m.InReplyTo...) {
		if r != "" && r != m.MessageID && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// ids liefert eigene ID und Verweise.
func (m Message) ids() []string {
	out := m.refs()
	if m.MessageID != "" {
		out = append([]string{m.MessageID}, out...)
	}
	return out
}

// Orphan: Antwort (Präfix im Betreff) ohne jeden Verweis.
func (m Message) Orphan() bool {
	_, reply := NormalizeSubject(m.Subject)
	return reply && len(m.refs()) == 0
}

// prefixRe erkennt ein Antwort- oder Weiterleitungspräfix bzw. eine
// Listenmarke in eckigen Klammern am Anfang des Betreffs.
var prefixRe = regexp.MustCompile(`(?i)^\s*(?:((?:re|aw|antw|wg|fw|fwd|sv|vs|tr|rif|odp)\s*(?:\[\d+\]|\(\d+\))?\s*[:：])|(\[[^\[\]]{1,40}\]))\s*`)

// NormalizeSubject entfernt Antwort-/Weiterleitungspräfixe (Re:, AW:, WG:,
// Fwd:, RE[2]: …) und Listenmarken ([Liste]), faltet Groß-/Kleinschreibung
// und behält nur Buchstaben und Ziffern (durch je ein Leerzeichen getrennt). reply ist true, wenn ein Antwort- oder Weiterleitungspräfix
// entfernt wurde (eine Listenmarke allein zählt nicht).
func NormalizeSubject(s string) (norm string, reply bool) {
	for i := 0; i < 20; i++ {
		m := prefixRe.FindStringSubmatchIndex(s)
		if m == nil {
			break
		}
		if m[2] >= 0 {
			reply = true
		}
		s = s[m[1]:]
	}
	low := strings.TrimSpace(strings.ToLower(s))
	if strings.HasSuffix(low, "(fwd)") {
		low = strings.TrimSpace(strings.TrimSuffix(low, "(fwd)"))
	}
	// Nur Buchstaben und Ziffern zählen: Clients ersetzen Gedankenstriche,
	// Anführungszeichen und Emoji je nach Zeichensatz (iso-8859-1 kennt
	// weder „–" noch Emoji), der Betreff bleibt aber derselbe.
	words := strings.FieldsFunc(low, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return strings.Join(words, " "), reply
}

// Info beschreibt einen vorhandenen Thread für die Entscheidung.
type Info struct {
	ID          int64
	First, Last time.Time // Zeitspanne der Nachrichten mit Datum
	// OrphanRoot: Die früheste Nachricht ist eine Waise (Antwort ohne
	// Verweise); der Thread ist dann ein Kandidat zum Einsammeln.
	OrphanRoot bool
}

func (i Info) near(d time.Time) (time.Duration, bool) {
	if i.First.IsZero() || d.IsZero() {
		return 0, false
	}
	switch {
	case d.Before(i.First):
		gap := i.First.Sub(d)
		return gap, gap <= Window
	case d.After(i.Last):
		gap := d.Sub(i.Last)
		return gap, gap <= Window
	}
	return 0, true
}

func spansClose(a, b Info) bool {
	if a.First.IsZero() || b.First.IsZero() {
		return false
	}
	return !b.First.After(a.Last.Add(Window)) && !a.First.After(b.Last.Add(Window))
}

// Index ist der Speicher der Threads eines Kontos.
type Index interface {
	// Lookup liefert die Threads, in denen eine der IDs (eigene oder
	// verwiesene Message-ID) schon vorkommt, ohne Dubletten.
	Lookup(ids []string) ([]int64, error)
	// Candidates liefert die Threads mit diesem normalisierten Betreff.
	Candidates(subjectNorm string) ([]Info, error)
	// Info liefert die aktuelle Beschreibung eines Threads.
	Info(id int64) (Info, error)
	// Participants liefert die Beteiligten aller Nachrichten des Threads
	// (wie Message.Participants: ohne die eigenen Adressen des Kontos).
	Participants(id int64) ([]string, error)
	// Create legt einen leeren Thread an.
	Create(subjectNorm string) (int64, error)
	// Merge vereinigt die Threads from in into (Nachrichten und IDs).
	Merge(into int64, from []int64) error
	// Add trägt die IDs ein, ordnet die Nachricht zu und aktualisiert die
	// Kennzahlen des Threads (Zeitspanne, Wurzel, Betreff).
	Add(thread int64, ids []string, m Message) error
}

// Assign ordnet m einem Thread zu (vorhanden, vereinigt oder neu) und
// liefert dessen Kennung.
func Assign(ix Index, m Message) (int64, error) {
	ids := m.ids()
	found, err := ix.Lookup(ids)
	if err != nil {
		return 0, err
	}
	norm, _ := NormalizeSubject(m.Subject)
	if len(found) == 0 && m.Orphan() && norm != "" && !m.Date.IsZero() {
		cands, err := ix.Candidates(norm)
		if err != nil {
			return 0, err
		}
		best, bestGap := int64(0), time.Duration(-1)
		for _, c := range cands {
			gap, ok := c.near(m.Date)
			if !ok || (bestGap >= 0 && (gap > bestGap || (gap == bestGap && c.ID > best))) {
				continue
			}
			// Nur bei gemeinsamen Beteiligten: Gleicher Betreff allein
			// verbindet fremde Gespräche nicht.
			parts, err := ix.Participants(c.ID)
			if err != nil {
				return 0, err
			}
			if Overlap(m.Participants, parts) {
				best, bestGap = c.ID, gap
			}
		}
		if best != 0 {
			found = []int64{best}
		}
	}
	var t int64
	if len(found) == 0 {
		if t, err = ix.Create(norm); err != nil {
			return 0, err
		}
	} else {
		slices.Sort(found)
		t = found[0]
		if len(found) > 1 {
			if err := ix.Merge(t, found[1:]); err != nil {
				return 0, err
			}
		}
	}
	if err := ix.Add(t, ids, m); err != nil {
		return 0, err
	}
	// Waisen-Threads gleichen Betreffs im Zeitfenster einsammeln.
	if norm == "" || m.Date.IsZero() {
		return t, nil
	}
	ti, err := ix.Info(t)
	if err != nil {
		return 0, err
	}
	cands, err := ix.Candidates(norm)
	if err != nil {
		return 0, err
	}
	var merge []int64
	var own []string
	for _, c := range cands {
		if c.ID == t || !c.OrphanRoot || !spansClose(c, ti) {
			continue
		}
		if own == nil {
			if own, err = ix.Participants(t); err != nil {
				return 0, err
			}
		}
		parts, err := ix.Participants(c.ID)
		if err != nil {
			return 0, err
		}
		if Overlap(parts, own) {
			merge = append(merge, c.ID)
		}
	}
	if len(merge) > 0 {
		if err := ix.Merge(t, merge); err != nil {
			return 0, err
		}
	}
	return t, nil
}

// Memory ist ein Index im Speicher (Tests, Group).
type Memory struct {
	next    int64
	refs    map[string]int64
	threads map[int64]*memThread
	// seq ist die laufende Nummer der Nachricht, die Add gerade einträgt;
	// owner hält je Nummer den aktuellen Thread (auch nach Vereinigungen).
	seq   int
	owner map[int]int64
}

type memThread struct {
	norm string
	msgs []Message
}

// NewMemory legt einen leeren Index an.
func NewMemory() *Memory {
	return &Memory{refs: map[string]int64{}, threads: map[int64]*memThread{}, owner: map[int]int64{}}
}

func (x *Memory) Lookup(ids []string) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if t, ok := x.refs[id]; ok && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (x *Memory) Candidates(norm string) ([]Info, error) {
	var out []Info
	for id, th := range x.threads {
		if th.norm == norm {
			out = append(out, x.info(id))
		}
	}
	slices.SortFunc(out, func(a, b Info) int { return int(a.ID - b.ID) })
	return out, nil
}

func (x *Memory) Info(id int64) (Info, error) { return x.info(id), nil }

func (x *Memory) Participants(id int64) ([]string, error) {
	var out []string
	for _, m := range x.threads[id].msgs {
		for _, p := range m.Participants {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (x *Memory) info(id int64) Info {
	th := x.threads[id]
	i := Info{ID: id}
	root := -1
	for k, m := range th.msgs {
		if m.Date.IsZero() {
			continue
		}
		if i.First.IsZero() || m.Date.Before(i.First) {
			i.First, root = m.Date, k
		}
		if m.Date.After(i.Last) {
			i.Last = m.Date
		}
	}
	if root >= 0 {
		i.OrphanRoot = th.msgs[root].Orphan()
	}
	return i
}

func (x *Memory) Create(norm string) (int64, error) {
	x.next++
	x.threads[x.next] = &memThread{norm: norm}
	return x.next, nil
}

func (x *Memory) Merge(into int64, from []int64) error {
	for _, f := range from {
		th := x.threads[f]
		if th == nil || f == into {
			continue
		}
		x.threads[into].msgs = append(x.threads[into].msgs, th.msgs...)
		delete(x.threads, f)
		for id, t := range x.refs {
			if t == f {
				x.refs[id] = into
			}
		}
		for n, t := range x.owner {
			if t == f {
				x.owner[n] = into
			}
		}
	}
	x.renorm(into)
	return nil
}

func (x *Memory) Add(t int64, ids []string, m Message) error {
	for _, id := range ids {
		if _, ok := x.refs[id]; !ok {
			x.refs[id] = t
		}
	}
	x.threads[t].msgs = append(x.threads[t].msgs, m)
	x.owner[x.seq] = t
	x.seq++
	x.renorm(t)
	return nil
}

// renorm: Der Betreff eines Threads ist der seiner frühesten Nachricht.
func (x *Memory) renorm(t int64) {
	th := x.threads[t]
	var root *Message
	for k := range th.msgs {
		m := &th.msgs[k]
		if root == nil || (!m.Date.IsZero() && (root.Date.IsZero() || m.Date.Before(root.Date))) {
			root = m
		}
	}
	if root != nil {
		th.norm, _ = NormalizeSubject(root.Subject)
	}
}

// Group ordnet die Nachrichten in der gegebenen Reihenfolge zu und liefert
// je Eingabe eine Thread-Nummer (0, 1, 2, … in der Reihenfolge des ersten
// Auftretens). Gleiche Nummer = gleicher Thread.
func Group(msgs []Message) []int {
	x := NewMemory()
	for _, m := range msgs {
		_, _ = Assign(x, m) // Memory liefert keine Fehler
	}
	out := make([]int, len(msgs))
	numbers := map[int64]int{}
	for i := range msgs {
		t := x.owner[i]
		n, ok := numbers[t]
		if !ok {
			n = len(numbers)
			numbers[t] = n
		}
		out[i] = n
	}
	return out
}
