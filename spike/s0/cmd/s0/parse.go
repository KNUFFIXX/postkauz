package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/emersion/go-message/charset" // registriert iso-8859-x, windows-125x usw.
	"github.com/emersion/go-message/mail"
	"github.com/jhillyerd/enmime"
)

// manifestEntry entspricht einem Eintrag aus manifest.json des Korpus.
type manifestEntry struct {
	File        string   `json:"file"`
	MessageID   string   `json:"message_id"`
	Charset     string   `json:"charset"`
	Thread      *string  `json:"thread"`
	Broken      string   `json:"broken"`
	Newsletter  bool     `json:"newsletter"`
	Attachments []string `json:"attachments"`
	Subject     string   `json:"subject"`
	Folder      string   `json:"folder"`
}

func loadManifestList(dir string) ([]manifestEntry, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var list []manifestEntry
	return list, json.Unmarshal(b, &list)
}

// loadManifest liefert Message-ID (ohne <>) -> Eintrag.
func loadManifest(dir string) (map[string]manifestEntry, error) {
	list, err := loadManifestList(dir)
	if err != nil {
		return nil, err
	}
	m := map[string]manifestEntry{}
	for _, e := range list {
		m[strings.Trim(e.MessageID, "<>")] = e
	}
	return m, nil
}

// parsedMsg sind die fuer Index und Threading benoetigten Felder.
type parsedMsg struct {
	MessageID   string
	InReplyTo   string
	References  []string
	Subject     string
	From, To    string
	Date        string
	Text        string
	Attachments []string
	Errors      []string
}

var msgIDRe = regexp.MustCompile(`<[^<>\s]+>`)

// msgIDs extrahiert Message-IDs tolerant (auch bei kaputter Syntax).
func msgIDs(v string) []string {
	var out []string
	for _, m := range msgIDRe.FindAllString(v, -1) {
		out = append(out, strings.Trim(m, "<>"))
	}
	return out
}

// parseGoMessage parst mit github.com/emersion/go-message.
func parseGoMessage(raw []byte) parsedMsg {
	var pm parsedMsg
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && mr == nil {
		pm.Errors = append(pm.Errors, "create: "+err.Error())
		return pm
	}
	if err != nil {
		pm.Errors = append(pm.Errors, "header: "+err.Error())
	}
	h := mr.Header
	if s, err := h.Subject(); err == nil {
		pm.Subject = s
	} else {
		pm.Subject = h.Get("Subject")
		pm.Errors = append(pm.Errors, "subject: "+err.Error())
	}
	if id, err := h.MessageID(); err == nil && id != "" {
		pm.MessageID = id
	} else {
		pm.MessageID = strings.Trim(strings.TrimSpace(h.Get("Message-Id")), "<>")
		if err != nil {
			pm.Errors = append(pm.Errors, "message-id: "+err.Error())
		}
	}
	if l := msgIDs(h.Get("In-Reply-To")); len(l) > 0 {
		pm.InReplyTo = l[0]
	}
	pm.References = msgIDs(h.Get("References"))
	if t, err := h.Date(); err == nil {
		pm.Date = t.UTC().Format(time.RFC3339)
	} else {
		pm.Errors = append(pm.Errors, "date: "+err.Error())
	}
	if l, err := h.AddressList("From"); err == nil {
		pm.From = addrString(l)
	} else {
		pm.Errors = append(pm.Errors, "from: "+err.Error())
	}
	if l, err := h.AddressList("To"); err == nil {
		pm.To = addrString(l)
	}
	var html string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			pm.Errors = append(pm.Errors, "part: "+err.Error())
			break
		}
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			b, err := io.ReadAll(p.Body)
			if err != nil {
				pm.Errors = append(pm.Errors, "body: "+err.Error())
			}
			if ct == "text/plain" && pm.Text == "" {
				pm.Text = string(b)
			} else if ct == "text/html" && html == "" {
				html = string(b)
			}
		case *mail.AttachmentHeader:
			name, err := ph.Filename()
			if err != nil {
				pm.Errors = append(pm.Errors, "filename: "+err.Error())
			}
			pm.Attachments = append(pm.Attachments, name)
			_, _ = io.Copy(io.Discard, p.Body)
		}
	}
	if pm.Text == "" && html != "" {
		pm.Text = stripTags(html)
	}
	return pm
}

func addrString(l []*mail.Address) string {
	var parts []string
	for _, a := range l {
		parts = append(parts, strings.TrimSpace(a.Name+" <"+a.Address+">"))
	}
	return strings.Join(parts, ", ")
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagRe.ReplaceAllString(s, " ") }

// parseEnmime parst mit github.com/jhillyerd/enmime.
func parseEnmime(raw []byte) parsedMsg {
	var pm parsedMsg
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		pm.Errors = append(pm.Errors, "read: "+err.Error())
		return pm
	}
	pm.Subject = env.GetHeader("Subject")
	pm.MessageID = strings.Trim(strings.TrimSpace(env.GetHeader("Message-ID")), "<>")
	if l := msgIDs(env.Root.Header.Get("In-Reply-To")); len(l) > 0 {
		pm.InReplyTo = l[0]
	}
	pm.References = msgIDs(env.Root.Header.Get("References"))
	if t, err := env.Date(); err == nil {
		pm.Date = t.UTC().Format(time.RFC3339)
	} else {
		pm.Errors = append(pm.Errors, "date: "+err.Error())
	}
	pm.From = env.GetHeader("From")
	pm.To = env.GetHeader("To")
	pm.Text = env.Text
	for _, a := range env.Attachments {
		pm.Attachments = append(pm.Attachments, a.FileName)
	}
	for _, e := range env.Errors {
		pm.Errors = append(pm.Errors, e.Error())
	}
	return pm
}

// parseForIndex wird im Sync verwendet (go-message).
func parseForIndex(raw []byte) parsedMsg { return parseGoMessage(raw) }

// parserStats sammelt Kennzahlen je Parser.
type parserStats struct {
	Parser             string         `json:"parser"`
	Seconds            float64        `json:"seconds"`
	HardFailures       int            `json:"hard_failures"`
	MessagesWithErrors int            `json:"messages_with_warnings"`
	ErrorKinds         map[string]int `json:"warning_kinds"`
	SubjectOK          int            `json:"subject_ok"`
	SubjectChecked     int            `json:"subject_checked"`
	SubjectFailEx      []string       `json:"subject_fail_examples,omitempty"`
	TextUmlautOK       int            `json:"text_umlaut_ok"`
	TextReplacement    int            `json:"text_with_replacement_char"`
	AttachOK           int            `json:"attachments_ok"`
	AttachChecked      int            `json:"attachments_checked"`
	AttachFailEx       []string       `json:"attachment_fail_examples,omitempty"`
	MessageIDOK        int            `json:"message_id_ok"`
	DateMissing        int            `json:"date_unparsed"`
	BrokenHandled      map[string]int `json:"broken_kind_parsed_without_hard_failure"`
	Threads            threadStats    `json:"threads"`
}

type threadStats struct {
	Multi         int `json:"threads_with_2plus"`
	Singletons    int `json:"singletons"`
	Expected      int `json:"expected_threads"`
	ExactMatch    int `json:"threads_exactly_matching_expected"`
	Impure        int `json:"threads_mixing_expected_threads"`
	SplitExpected int `json:"expected_threads_split"`
}

func runParse(cfg *config) (any, error) {
	list, err := loadManifestList(cfg.Corpus)
	if err != nil {
		return nil, err
	}
	raws := make([][]byte, len(list))
	for i, e := range list {
		raws[i], err = os.ReadFile(filepath.Join(cfg.Corpus, e.File))
		if err != nil {
			return nil, err
		}
	}
	out := map[string]any{"messages": len(list)}
	var stats []parserStats
	all := map[string][]parsedMsg{}
	for _, p := range []struct {
		name string
		fn   func([]byte) parsedMsg
	}{{"go-message v0.18.2", parseGoMessage}, {"enmime v1.3.0", parseEnmime}} {
		st := parserStats{Parser: p.name, ErrorKinds: map[string]int{}, BrokenHandled: map[string]int{}}
		t0 := time.Now()
		parsed := make([]parsedMsg, len(list))
		for i := range list {
			parsed[i] = safeParse(p.fn, raws[i])
		}
		st.Seconds = time.Since(t0).Seconds()
		for i, e := range list {
			pm := parsed[i]
			if len(pm.Errors) > 0 {
				st.MessagesWithErrors++
				for _, er := range pm.Errors {
					st.ErrorKinds[errKind(er)]++
				}
				if strings.HasPrefix(pm.Errors[0], "create:") || strings.HasPrefix(pm.Errors[0], "read:") || strings.HasPrefix(pm.Errors[0], "panic:") {
					st.HardFailures++
					continue
				}
			}
			if e.Broken != "" {
				st.BrokenHandled[e.Broken]++
			}
			if pm.MessageID == strings.Trim(e.MessageID, "<>") {
				st.MessageIDOK++
			}
			if pm.Date == "" {
				st.DateMissing++
			}
			if e.Broken == "" {
				st.SubjectChecked++
				if pm.Subject == e.Subject {
					st.SubjectOK++
				} else if len(st.SubjectFailEx) < 4 {
					st.SubjectFailEx = append(st.SubjectFailEx, fmt.Sprintf("%s [%s]: %q", e.File, e.Charset, pm.Subject))
				}
				if strings.ContainsAny(pm.Text, "äöüßÄÖÜ") && utf8.ValidString(pm.Text) {
					st.TextUmlautOK++
				}
				if strings.ContainsRune(pm.Text, utf8.RuneError) {
					st.TextReplacement++
				}
			}
			if len(e.Attachments) > 0 {
				st.AttachChecked++
				if sameSet(pm.Attachments, e.Attachments) {
					st.AttachOK++
				} else if len(st.AttachFailEx) < 4 {
					st.AttachFailEx = append(st.AttachFailEx, fmt.Sprintf("%s: %q", e.File, pm.Attachments))
				}
			}
		}
		st.Threads = threadCheck(list, parsed)
		all[p.name] = parsed
		stats = append(stats, st)
	}
	out["parsers"] = stats
	out["differences"] = parserDiffs(list, all["go-message v0.18.2"], all["enmime v1.3.0"])
	return out, nil
}

// safeParse faengt Panics der Parser ab (tief verschachtelte Multiparts usw.).
func safeParse(fn func([]byte) parsedMsg, raw []byte) (pm parsedMsg) {
	defer func() {
		if r := recover(); r != nil {
			pm = parsedMsg{Errors: []string{fmt.Sprintf("panic: %v", r)}}
		}
	}()
	return fn(raw)
}

func errKind(s string) string {
	s = regexp.MustCompile(`[0-9]+`).ReplaceAllString(s, "N")
	if len(s) > 70 {
		s = s[:70]
	}
	return s
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// buildThreads ist ein vereinfachter JWZ-Algorithmus: Union-Find ueber
// Message-ID, In-Reply-To und alle References (ohne Betreff-Zusammenfuehrung).
func buildThreads(parsed []parsedMsg) []int {
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if _, ok := parent[x]; !ok {
			parent[x] = x
		}
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	keys := make([]string, len(parsed))
	for i, pm := range parsed {
		k := pm.MessageID
		if k == "" {
			k = fmt.Sprintf("__ohne_id_%d", i)
		}
		keys[i] = k
		find(k)
		for _, r := range pm.References {
			union(k, r)
		}
		if pm.InReplyTo != "" {
			union(k, pm.InReplyTo)
		}
	}
	roots := map[string]int{}
	ids := make([]int, len(parsed))
	for i, k := range keys {
		r := find(k)
		if _, ok := roots[r]; !ok {
			roots[r] = len(roots)
		}
		ids[i] = roots[r]
	}
	return ids
}

func threadCheck(list []manifestEntry, parsed []parsedMsg) threadStats {
	ids := buildThreads(parsed)
	members := map[int][]int{}
	for i, id := range ids {
		members[id] = append(members[id], i)
	}
	expected := map[string]map[int]bool{}
	for i, e := range list {
		if e.Thread != nil {
			if expected[*e.Thread] == nil {
				expected[*e.Thread] = map[int]bool{}
			}
			expected[*e.Thread][ids[i]] = true
		}
	}
	var ts threadStats
	ts.Expected = len(expected)
	for _, m := range members {
		if len(m) < 2 {
			ts.Singletons++
			continue
		}
		ts.Multi++
		labels := map[string]bool{}
		for _, i := range m {
			if list[i].Thread != nil {
				labels[*list[i].Thread] = true
			} else {
				labels["-"] = true
			}
		}
		if len(labels) > 1 {
			ts.Impure++
		}
	}
	for t, set := range expected {
		if len(set) > 1 {
			ts.SplitExpected++
			continue
		}
		for id := range set {
			n := 0
			for _, e := range list {
				if e.Thread != nil && *e.Thread == t {
					n++
				}
			}
			if len(members[id]) == n {
				ts.ExactMatch++
			}
		}
	}
	return ts
}

// parserDiffs listet Beispiele, in denen sich die Parser unterscheiden, plus
// die Ergebnisse fuer die absichtlich kaputten Mails.
func parserDiffs(list []manifestEntry, a, b []parsedMsg) map[string]any {
	out := map[string]any{}
	var textDiff []string
	brokenEx := map[string]any{}
	short := func(s string, n int) string {
		r := []rune(s)
		if len(r) > n {
			return string(r[:n]) + "..."
		}
		return s
	}
	for i, e := range list {
		ta, tb := strings.TrimSpace(a[i].Text), strings.TrimSpace(b[i].Text)
		if ta != tb && len(textDiff) < 6 {
			textDiff = append(textDiff, fmt.Sprintf("%s [%s] go-message=%q enmime=%q", e.File, e.Charset, short(ta, 60), short(tb, 60)))
		}
		if e.Broken != "" {
			if _, ok := brokenEx[e.Broken]; !ok {
				brokenEx[e.Broken] = map[string]any{
					"file":       e.File,
					"go-message": map[string]any{"subject": a[i].Subject, "date": a[i].Date, "msgid": a[i].MessageID, "text_len": len(a[i].Text), "errors": a[i].Errors},
					"enmime":     map[string]any{"subject": b[i].Subject, "date": b[i].Date, "msgid": b[i].MessageID, "text_len": len(b[i].Text), "errors": b[i].Errors},
				}
			}
		}
	}
	n := 0
	for i := range list {
		if strings.TrimSpace(a[i].Text) != strings.TrimSpace(b[i].Text) {
			n++
		}
	}
	out["text_differs_count"] = n
	out["text_differs_examples"] = textDiff
	out["broken_examples"] = brokenEx
	return out
}
