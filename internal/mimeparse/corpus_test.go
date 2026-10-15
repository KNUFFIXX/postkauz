package mimeparse_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/thread"
)

var (
	corpusOnce sync.Once
	theCorpus  *corpus.Corpus
	corpusErr  error
)

// standardCorpus erzeugt den Standardkorpus (1.000 Nachrichten) einmal je
// Testlauf im Speicher; `make corpus` ist dafür nicht nötig.
func standardCorpus(t *testing.T) *corpus.Corpus {
	t.Helper()
	corpusOnce.Do(func() { theCorpus, corpusErr = corpus.Generate(corpus.Options{}) })
	if corpusErr != nil {
		t.Fatal(corpusErr)
	}
	return theCorpus
}

func checked(e corpus.Entry, field string) bool { return !slices.Contains(e.Unchecked, field) }

// Akzeptanz S1.2: Der Parser besteht den Korpus gegen das Manifest
// (Betreff, Absender, Datum, Message-ID, Anhänge, Threads).
func TestParserAgainstCorpusManifest(t *testing.T) {
	c := standardCorpus(t)
	if c.Manifest.Count != 1000 || c.Manifest.Summary["threads"] != 60 || c.Manifest.Summary["broken"] != 50 {
		t.Fatalf("unexpected corpus shape: %+v", c.Manifest.Summary)
	}
	if n := c.Manifest.Summary["broken:tz_without_sign"]; n != 8 {
		t.Fatalf("tz_without_sign cases = %d, want 8 (spike)", n)
	}
	parsed := make([]*mimeparse.Message, len(c.Raw))
	fails := 0
	fail := func(e corpus.Entry, format string, args ...any) {
		fails++
		if fails <= 30 {
			t.Errorf("%s (%s/%s): %s", e.File, e.Kind, e.Broken, fmt.Sprintf(format, args...))
		}
	}
	for i, raw := range c.Raw {
		e := c.Manifest.Messages[i]
		internal, _ := time.Parse(time.RFC3339, e.InternalDate)
		m, err := mimeparse.Parse(bytes.NewReader(raw), mimeparse.Options{InternalDate: internal})
		if err != nil {
			fail(e, "parse: %v", err)
			continue
		}
		parsed[i] = m
		if m.Subject != e.Subject {
			fail(e, "subject %q, want %q", m.Subject, e.Subject)
		}
		if len(m.From) != 1 || m.From[0].Address != e.FromAddr || m.From[0].Name != e.FromName {
			fail(e, "from %+v, want %q <%s>", m.From, e.FromName, e.FromAddr)
		}
		if len(m.To) == 0 || m.To[0].Address != e.ToAddr {
			fail(e, "to %+v, want %s", m.To, e.ToAddr)
		}
		if m.MessageID != e.MessageID {
			fail(e, "message-id %q, want %q", m.MessageID, e.MessageID)
		}
		if got := m.Date.Format(time.RFC3339); got != e.Date || string(m.DateSource) != e.DateSource {
			fail(e, "date %s (%s), want %s (%s)", got, m.DateSource, e.Date, e.DateSource)
		}
		if checked(e, "attachments") {
			var names []string
			inline := 0
			for _, a := range m.Attachments {
				if a.Inline {
					inline++
				} else {
					names = append(names, a.Filename)
				}
			}
			if !slices.Equal(names, e.Attachments) && !(len(names) == 0 && len(e.Attachments) == 0) {
				fail(e, "attachments %q, want %q", names, e.Attachments)
			}
			if inline != e.InlineParts {
				fail(e, "inline parts %d, want %d", inline, e.InlineParts)
			}
		}
		if e.BodyContains != "" && checked(e, "body_contains") && !strings.Contains(m.Text, e.BodyContains) {
			fail(e, "text does not contain %q (text starts %q)", e.BodyContains, head(m.Text, 80))
		}
		if e.HTML && m.HTML == "" {
			fail(e, "html part missing")
		}
		if e.Newsletter && m.ListID != e.ListID {
			fail(e, "list-id %q, want %q", m.ListID, e.ListID)
		}
		if e.Broken == "" && len(m.Problems) > 0 {
			fail(e, "unexpected problems on a clean message: %v", m.Problems)
		}
		if e.ZipBomb {
			checkZipBomb(t, raw, m)
		}
	}
	if fails > 0 {
		t.Fatalf("%d mismatches against the manifest", fails)
	}
	checkThreads(t, c, parsed)
}

// checkThreads: Jeder Thread des Manifests ist genau eine Gruppe; alle
// anderen Nachrichten stehen allein.
func checkThreads(t *testing.T, c *corpus.Corpus, parsed []*mimeparse.Message) {
	t.Helper()
	nodes := make([]thread.Node, len(parsed))
	for i, m := range parsed {
		nodes[i] = thread.Node{MessageID: m.MessageID, InReplyTo: m.InReplyTo, References: m.References}
	}
	groups := thread.Group(nodes)
	byThread := map[string]map[int]bool{}
	members := map[int]int{}
	for i, g := range groups {
		members[g]++
		if th := c.Manifest.Messages[i].Thread; th != "" {
			if byThread[th] == nil {
				byThread[th] = map[int]bool{}
			}
			byThread[th][g] = true
		}
	}
	if len(byThread) != 60 {
		t.Fatalf("threads in manifest = %d", len(byThread))
	}
	threadGroups := map[int]string{}
	for th, gs := range byThread {
		if len(gs) != 1 {
			t.Errorf("thread %s split into %d groups", th, len(gs))
			continue
		}
		for g := range gs {
			if other, ok := threadGroups[g]; ok {
				t.Errorf("threads %s and %s merged", th, other)
			}
			threadGroups[g] = th
		}
	}
	for i, g := range groups {
		e := c.Manifest.Messages[i]
		if e.Thread == "" && members[g] != 1 {
			t.Errorf("%s: standalone message grouped with %d others", e.File, members[g]-1)
		}
	}
}

func checkZipBomb(t *testing.T, raw []byte, m *mimeparse.Message) {
	t.Helper()
	var bomb *mimeparse.Attachment
	for i := range m.Attachments {
		if m.Attachments[i].Filename == "Archiv.zip" {
			bomb = &m.Attachments[i]
		}
	}
	if bomb == nil {
		t.Fatal("zip bomb attachment not found")
	}
	z := corpus.ZipBomb(16 << 20)
	sum := sha256.Sum256(z)
	// Größe und Hash sind die der gepackten Bytes: nichts wurde entpackt.
	if bomb.Size != int64(len(z)) || bomb.SHA256 != hex.EncodeToString(sum[:]) || bomb.Truncated {
		t.Errorf("zip bomb attachment = %+v (packed %d bytes)", bomb, len(z))
	}
	if bomb.Size > 1<<20 {
		t.Errorf("zip bomb reported %d bytes: was it inflated?", bomb.Size)
	}
	_ = raw
}

func head(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Der Sync gleicht vor dem Laden per Message-ID aus BODY.PEEK[HEADER.FIELDS
// (MESSAGE-ID)] ab. HeaderMessageID muss dafür dieselbe ID liefern wie Parse
// über die ganze Nachricht (sonst entstünden Dubletten).
func TestHeaderMessageIDMatchesParse(t *testing.T) {
	c := standardCorpus(t)
	for i, raw := range c.Raw {
		m, err := mimeparse.Parse(bytes.NewReader(raw), mimeparse.Options{})
		if err != nil {
			continue
		}
		// Nachbildung von HEADER.FIELDS: nur die Message-ID-Zeile(n) samt Fortsetzung.
		head := raw
		if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
			head = raw[:i+2]
		} else if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
			head = raw[:i+1]
		}
		var field bytes.Buffer
		take := false
		for _, line := range bytes.SplitAfter(head, []byte("\n")) {
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				if take {
					field.Write(line)
				}
				continue
			}
			take = len(line) >= 11 && strings.EqualFold(string(line[:11]), "message-id:")
			if take {
				field.Write(line)
			}
		}
		field.WriteString("\r\n")
		if got := mimeparse.HeaderMessageID(field.Bytes()); got != m.MessageID {
			t.Errorf("%s: HeaderMessageID = %q, Parse = %q", c.Manifest.Messages[i].File, got, m.MessageID)
		}
	}
}
