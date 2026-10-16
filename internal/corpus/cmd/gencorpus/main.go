// gencorpus schreibt den synthetischen Testkorpus (make corpus).
//
//	go run ./internal/corpus/cmd/gencorpus -out testdata/corpus
//	go run ./internal/corpus/cmd/gencorpus -out testdata/corpus-large -count 50000
//	go run ./internal/corpus/cmd/gencorpus -thunderbird testdata/import/tb -maildir testdata/import/maildir
//
// -thunderbird und -maildir schreiben zusätzlich die Ablagen für den Import
// (S1.10, docs/testkorpus.md Abschnitt 1b) und nennen die erwartete Zählung.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
)

func main() {
	out := flag.String("out", "testdata/corpus", "output directory (*.eml and corpus.json)")
	count := flag.Int("count", 1000, "number of messages")
	threads := flag.Int("threads", 60, "number of threads")
	seed := flag.Uint64("seed", 42, "random seed (same seed, same corpus)")
	tb := flag.String("thunderbird", "", "also write the corpus as a Thunderbird profile folder (mbox) here")
	md := flag.String("maildir", "", "also write the corpus as a Maildir++ tree here")
	flag.Parse()

	c, err := corpus.Generate(corpus.Options{Count: *count, Threads: *threads, Seed: *seed})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gencorpus:", err)
		os.Exit(1)
	}
	if err := c.Write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "gencorpus:", err)
		os.Exit(1)
	}
	sum, _ := json.Marshal(c.Manifest.Summary)
	fmt.Printf("corpus: %d messages in %s (seed %d)\n%s\n", c.Manifest.Count, *out, *seed, sum)
	for _, x := range []struct {
		dir   string
		write func(string, corpus.ImportOptions) (corpus.ImportExpect, error)
	}{{*tb, c.WriteThunderbird}, {*md, c.WriteMaildir}} {
		if x.dir == "" {
			continue
		}
		exp, err := x.write(x.dir, corpus.ImportOptions{})
		if err != nil {
			fmt.Fprintln(os.Stderr, "gencorpus:", err)
			os.Exit(1)
		}
		fmt.Printf("import data in %s: expect new: %d, existing: %d, skipped: %d, errors: 0\n", x.dir, exp.New, exp.Copies, exp.Deleted)
	}
}
