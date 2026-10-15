package corpus

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Der Generator ist deterministisch: gleicher Samen, gleiche Bytes.
func TestDeterministic(t *testing.T) {
	opt := Options{Count: 120, Threads: 10, Seed: 7, ZipBombBytes: 1 << 16}
	a, _ := Generate(opt)
	b, _ := Generate(opt)
	if len(a.Raw) != 120 || len(b.Raw) != 120 {
		t.Fatalf("counts %d %d", len(a.Raw), len(b.Raw))
	}
	for i := range a.Raw {
		if !bytes.Equal(a.Raw[i], b.Raw[i]) {
			t.Fatalf("message %d differs between runs", i)
		}
	}
	opt.Seed = 8
	c, _ := Generate(opt)
	if bytes.Equal(a.Raw[0], c.Raw[0]) {
		t.Fatal("different seeds produced identical output")
	}
}

var addrRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
var allowed = regexp.MustCompile(`@([a-z0-9-]+\.)*(example\.(com|org|net)|invalid)$`)

// Nur reservierte Domains (docs/testkorpus.md); Pflichtinhalte vorhanden.
func TestStandardCorpusContent(t *testing.T) {
	c, err := Generate(Options{ZipBombBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range c.Raw {
		for _, loc := range addrRe.FindAllIndex(raw, -1) {
			a := raw[loc[0]:loc[1]]
			if loc[1] < len(raw) && raw[loc[1]] == '=' {
				continue // quoted-printable-Umbruch mitten in der Adresse
			}
			if !allowed.Match(a) {
				t.Fatalf("%s: address outside reserved domains: %s", c.Manifest.Messages[i].File, a)
			}
		}
	}
	s := c.Manifest.Summary
	if c.Manifest.Count != 1000 || s["threads"] != 60 || s["broken"] != 50 || s["newsletter"] != 45 || s["zipbomb"] != 1 {
		t.Fatalf("summary = %v", s)
	}
	enc := map[string]int{}
	cs := map[string]int{}
	for _, e := range c.Manifest.Messages {
		enc[e.HeaderEncoding]++
		cs[e.Charset]++
	}
	for _, k := range []string{"B", "Q", "raw"} {
		if enc[k] == 0 {
			t.Errorf("no header encoding %s: %v", k, enc)
		}
	}
	for _, k := range []string{"utf-8", "iso-8859-1", "windows-1252"} {
		if cs[k] == 0 {
			t.Errorf("no charset %s: %v", k, cs)
		}
	}
}

func TestWrite(t *testing.T) {
	c, _ := Generate(Options{Count: 30, Threads: 3, ZipBombBytes: -1})
	dir := t.TempDir()
	if err := c.Write(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil || m.Count != 30 || len(m.Messages) != 30 {
		t.Fatalf("manifest: %v %+v", err, m.Summary)
	}
	raw, err := os.ReadFile(filepath.Join(dir, m.Messages[0].File))
	if err != nil || !bytes.Equal(raw, c.Raw[0]) {
		t.Fatalf("eml: %v", err)
	}
}
