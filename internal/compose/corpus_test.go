package compose_test

import (
	"bytes"
	"fmt"
	mrand "math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
	"github.com/KNUFFIXX/postkauz/internal/thread"
)

const alias = "alias@example.invalid"

// Erwartungen, unabhängig von compose formuliert (E11).
var replyPrefix = regexp.MustCompile(`(?i)^\s*(re|aw|antw)\s*(\[\d+\]|\(\d+\))?\s*:`)

func expectSubject(orig string) string {
	s := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, sanitize.Text(orig)))
	if !replyPrefix.MatchString(s) {
		s = strings.TrimSpace("Re: " + s)
	}
	if r := []rune(s); len(r) > 500 {
		s = strings.TrimSpace(string(r[:500]))
	}
	return s
}

func expectRefs(m *mimeparse.Message) (string, []string) {
	base := m.References
	if len(base) == 0 {
		base = m.InReplyTo
	}
	valid := func(id string) bool { return id != "" && !strings.ContainsAny(id, " \t\r\n<>") }
	var refs []string
	for _, id := range base {
		if valid(id) && !slices.Contains(refs, id) {
			refs = append(refs, id)
		}
	}
	irt := ""
	if valid(m.MessageID) {
		irt = m.MessageID
		refs = append(slices.DeleteFunc(refs, func(s string) bool { return s == irt }), irt)
	}
	if len(refs) > 20 {
		refs = append([]string{refs[0]}, refs[len(refs)-19:]...)
	}
	return irt, refs
}

func participants(m *mimeparse.Message) []string {
	var out []string
	for _, l := range [][]mimeparse.Address{m.From, m.To, m.Cc} {
		for _, a := range l {
			out = append(out, thread.NormalizeAddress(a.Address))
		}
	}
	return out
}

func threadMsg(m *mimeparse.Message) thread.Message {
	return thread.Message{MessageID: m.MessageID, InReplyTo: m.InReplyTo, References: m.References,
		Subject: m.Subject, Date: m.Date, Participants: participants(m)}
}

// threadCheck prüft, ob eine Antwort in den Thread ihrer Ausgangsnachricht
// fällt. thread.Group über den ganzen Korpus kostet je Aufruf rund 30 ms;
// deshalb gruppiert check nur die Nachrichten, mit denen sich eine neue
// Antwort überhaupt verbinden kann: den Thread der Ausgangsnachricht (über
// In-Reply-To/References) und alle Threads mit demselben normalisierten
// Betreff (Betreff-Rückfall). Mit full gruppiert check den ganzen Korpus.
type threadCheck struct {
	base    []thread.Message
	groups  []int         // thread.Group(base)
	members map[int][]int // Gruppe → Indizes
	subject map[string][]int
}

func newThreadCheck(base []thread.Message) *threadCheck {
	tc := &threadCheck{base: base, groups: thread.Group(base), members: map[int][]int{}, subject: map[string][]int{}}
	for i, g := range tc.groups {
		tc.members[g] = append(tc.members[g], i)
		norm, _ := thread.NormalizeSubject(base[i].Subject)
		tc.subject[norm] = append(tc.subject[norm], i)
	}
	return tc
}

func (tc *threadCheck) check(orig int, reply thread.Message, full bool) string {
	var idx []int
	if full {
		for i := range tc.base {
			idx = append(idx, i)
		}
	} else {
		set := map[int]bool{}
		norm, _ := thread.NormalizeSubject(reply.Subject)
		for _, j := range append([]int{orig}, tc.subject[norm]...) {
			for _, k := range tc.members[tc.groups[j]] {
				set[k] = true
			}
		}
		for k := range set {
			idx = append(idx, k)
		}
		slices.Sort(idx)
	}
	msgs := make([]thread.Message, 0, len(idx)+1)
	pos := -1
	for n, k := range idx {
		msgs = append(msgs, tc.base[k])
		if k == orig {
			pos = n
		}
	}
	g := thread.Group(append(msgs, reply))
	if g[len(g)-1] != g[pos] {
		return "reply not in the thread of the original"
	}
	for a := range idx {
		for b := a + 1; b < len(idx); b++ {
			if (g[a] == g[b]) != (tc.groups[idx[a]] == tc.groups[idx[b]]) {
				return "reply changed the threads of the corpus"
			}
		}
	}
	return ""
}

// Akzeptanz S2.2 (A02 Teil C): Für jede Antwort auf eine Nachricht des
// Korpus (1.000, Samen 42) stimmen In-Reply-To/References (E11) und das
// Betreffpräfix, eigene Adressen fehlen in Cc, der eigene Parser liest die
// Antwort fehlerfrei, und sie landet im selben Thread wie die
// Ausgangsnachricht. Unter -short nur jede zehnte Nachricht.
func TestRepliesToCorpus(t *testing.T) {
	c, err := corpus.Generate(corpus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Manifest.Seed != 42 || len(c.Raw) != 1000 {
		t.Fatalf("corpus seed %d, %d messages", c.Manifest.Seed, len(c.Raw))
	}
	parsed := make([]*mimeparse.Message, len(c.Raw))
	base := make([]thread.Message, len(c.Raw))
	for i, raw := range c.Raw {
		e := c.Manifest.Messages[i]
		internal, _ := time.Parse(time.RFC3339, e.InternalDate)
		m, err := mimeparse.Parse(bytes.NewReader(raw), mimeparse.Options{InternalDate: internal})
		if err != nil {
			t.Fatalf("%s: parse original: %v", e.File, err)
		}
		parsed[i], base[i] = m, threadMsg(m)
	}
	threads := newThreadCheck(base)
	// Gegenprobe: Eine Nachricht ohne Verweise mit anderem Betreff gehört
	// nicht in den Thread; die Prüfung muss das melden.
	for _, full := range []bool{false, true} {
		stray := thread.Message{MessageID: "stray@example.invalid", Subject: "Re: etwas ganz anderes",
			Date: parsed[0].Date, Participants: base[0].Participants}
		if threads.check(0, stray, full) == "" {
			t.Fatalf("thread check (full=%v) accepts an unrelated message", full)
		}
	}

	fails, replies, withRefs, hidden := 0, 0, 0, 0
	fail := func(file string, format string, args ...any) {
		fails++
		if fails <= 30 {
			t.Errorf("%s: %s", file, fmt.Sprintf(format, args...))
		}
	}
	for i, orig := range parsed {
		if testing.Short() && i%10 != 0 {
			continue
		}
		e := c.Manifest.Messages[i]
		own := e.ToAddr
		for _, kind := range []compose.Kind{compose.KindReply, compose.KindReplyAll} {
			in := compose.NewInput(kind)
			in.AccountAddress = own
			if i%2 == 0 {
				in.Identity = compose.Identity{Name: "Postfach", Email: own, SignatureText: "Gruß"}
			}
			in.OwnAddresses = []string{own, alias}
			o := compose.OriginalFrom(orig)
			// Eine eigene Adresse (Alias) steht zusätzlich in Cc der Ausgangsnachricht.
			o.Cc = append(slices.Clone(o.Cc), compose.Address{Address: strings.ToUpper(alias)})
			in.Original = &o
			in.BodyMarkdown = "Danke, das **passt**."
			in.Now = func() time.Time { return orig.Date.Add(time.Hour) }
			var seed [32]byte
			seed[0], seed[1] = byte(i), byte(i>>8)
			in.Rand = mrand.NewChaCha8(seed)
			m, err := compose.Build(in)
			if err != nil {
				fail(e.File, "%s: build: %v", kind, err)
				continue
			}
			replies++
			wantIRT, wantRefs := expectRefs(orig)
			if len(wantRefs) > 1 {
				withRefs++
			}
			if m.InReplyTo != wantIRT || !slices.Equal(m.References, wantRefs) {
				fail(e.File, "%s: in-reply-to %q refs %v, want %q %v", kind, m.InReplyTo, m.References, wantIRT, wantRefs)
			}
			if want := expectSubject(orig.Subject); m.Subject != want {
				fail(e.File, "%s: subject %q, want %q", kind, m.Subject, want)
			}
			if i%2 == 1 && m.From.Address != own {
				fail(e.File, "%s: without identity from %q, want account address", kind, m.From.Address)
			}
			seen := map[string]bool{}
			for _, a := range append(slices.Clone(m.To), m.Cc...) {
				n := strings.ToLower(a.Address)
				if n == strings.ToLower(own) || n == alias {
					fail(e.File, "%s: own address %s among recipients", kind, n)
				}
				if seen[n] {
					fail(e.File, "%s: duplicate recipient %s", kind, n)
				}
				seen[n] = true
			}
			if kind == compose.KindReply && len(m.Cc) != 0 {
				fail(e.File, "reply: cc %v", m.Cc)
			}

			// Der eigene Parser liest die Antwort fehlerfrei.
			p, err := mimeparse.Parse(bytes.NewReader(m.Raw), mimeparse.Options{})
			if err != nil || len(p.Problems) > 0 {
				fail(e.File, "%s: parse reply: %v %v", kind, err, p.Problems)
				continue
			}
			pIRT := ""
			if len(p.InReplyTo) > 0 {
				pIRT = p.InReplyTo[0]
			}
			if p.MessageID != m.MessageID || pIRT != wantIRT || !slices.Equal(p.References, wantRefs) || p.Subject != m.Subject {
				fail(e.File, "%s: parsed id %q irt %v refs %v subject %q", kind, p.MessageID, p.InReplyTo, p.References, p.Subject)
			}
			if !p.Alternative || !strings.Contains(p.Text, "Danke, das passt.") {
				fail(e.File, "%s: parsed text %q", kind, p.Text)
			}

			// Dieselbe Thread-Zuordnung wie die Ausgangsnachricht (S1.6),
			// ohne fremde Threads zu vereinigen.
			if msg := threads.check(i, threadMsg(p), i%25 == 0); msg != "" {
				fail(e.File, "%s: %s", kind, msg)
			}
		}

		// HTML mit verstecktem Text (Sonderfälle S1.6) wird abgelehnt.
		if e.HiddenText && orig.HTML != "" {
			hidden++
			in := compose.NewInput(compose.KindNew)
			in.AccountAddress = own
			in.BodyHTML = orig.HTML
			_, err := compose.Build(in)
			if r, _ := compose.ReasonOf(err); r != compose.ReasonHiddenText {
				fail(e.File, "hidden text: error %v", err)
			}
		}
	}
	if fails > 0 {
		t.Fatalf("%d failures", fails)
	}
	t.Logf("%d replies built and checked, %d with a References chain, %d hidden-text messages rejected", replies, withRefs, hidden)
	if !testing.Short() && (replies != 2000 || hidden != 3) {
		t.Fatalf("checked %d replies and %d hidden-text messages, want 2000 and 3", replies, hidden)
	}
}
