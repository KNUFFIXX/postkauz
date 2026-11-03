package compose_test

import (
	"bytes"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/thread"
)

// A04 Teil 1 (Lastenheft Vorgang 4, „nur im Original-Thread“): Für jede
// Weiterleitung einer Korpus-Nachricht (Samen 42) stimmen In-Reply-To und
// References wie bei einer Antwort (E11), der eigene Parser liest sie, und
// sie landet im Thread der Ausgangsnachricht, ohne fremde Threads zu
// vereinigen. Unter -short nur jede zehnte Nachricht.
func TestA04ForwardsToCorpus(t *testing.T) {
	c, err := corpus.Generate(corpus.Options{})
	if err != nil {
		t.Fatal(err)
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
	fails, built := 0, 0
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
		in := compose.NewInput(compose.KindForward)
		in.AccountAddress = e.ToAddr
		in.OwnAddresses = []string{e.ToAddr}
		o := compose.OriginalFrom(orig)
		in.Original = &o
		in.IncludeAttachments = false
		in.To = []string{"kollege@example.net"}
		in.BodyMarkdown = "Zur Info."
		in.Now = func() time.Time { return orig.Date.Add(time.Hour) }
		var seed [32]byte
		seed[0], seed[1] = byte(i), byte(i>>8)
		in.Rand = mrand.NewChaCha8(seed)
		m, err := compose.Build(in)
		if err != nil {
			fail(e.File, "build: %v", err)
			continue
		}
		built++
		wantIRT, wantRefs := expectRefs(orig)
		if m.InReplyTo != wantIRT || !slices.Equal(m.References, wantRefs) {
			fail(e.File, "in-reply-to %q refs %v, want %q %v", m.InReplyTo, m.References, wantIRT, wantRefs)
		}
		p, err := mimeparse.Parse(bytes.NewReader(m.Raw), mimeparse.Options{})
		if err != nil || len(p.Problems) > 0 {
			fail(e.File, "parse forward: %v %v", err, p.Problems)
			continue
		}
		if !slices.Equal(p.References, wantRefs) {
			fail(e.File, "parsed refs %v, want %v", p.References, wantRefs)
		}
		if msg := threads.check(i, threadMsg(p), i%25 == 0); msg != "" {
			fail(e.File, "forward: %s", msg)
		}
	}
	if fails > 0 {
		t.Fatalf("%d failures", fails)
	}
	t.Logf("%d forwards built and checked", built)
	if !testing.Short() && built != 1000 {
		t.Fatalf("checked %d forwards, want 1000", built)
	}
}
