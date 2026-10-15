// gencorpus schreibt den synthetischen Testkorpus (make corpus).
//
//	go run ./internal/corpus/cmd/gencorpus -out testdata/corpus
//	go run ./internal/corpus/cmd/gencorpus -out testdata/corpus-large -count 50000
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
}
