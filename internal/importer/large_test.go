package importer

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/jobs"
)

// Große Dateien (Thunderbird-Ordner bis 20 GB): Der Import streamt, der
// Speicher bleibt flach. Der Generator schreibt eine mbox wie ein
// Gmail-Konto des Betreibers (Befund 15.10.2026: im Mittel 0,94 MB je Mail,
// 59 % mit Anhang): Textmails um 12 KB, Anhänge 80 KB, 500 KB, 2 MB, 5 MB
// (35/30/25/10 %), dazu wahlweise eine Nachricht mit 30 MB Anhang.
// Anhänge sind Zufallsbytes (nicht komprimierbar), base64 mit 76 Zeichen
// je Zeile, Zeilenenden CRLF.

// largeMbox schreibt nach path, bis mindestens size Bytes erreicht sind.
// Liefert die Zahl der Nachrichten.
func largeMbox(t testing.TB, path string, size int64, big bool) int {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	r := rand.New(rand.NewChaCha8([32]byte{1, 2, 3}))
	var written int64
	count := 0
	text := make([]byte, 0, 12<<10)
	for i := 0; written < size; i++ {
		att := 0
		switch {
		case big && i == 3:
			att = 30 << 20
		case r.Float64() < 0.59:
			att = []int{80 << 10, 500 << 10, 2 << 20, 5 << 20}[pick(r, []float64{0.35, 0.30, 0.25, 0.10})]
		}
		n := writeLargeMessage(w, r, i, att, text)
		written += n
		count++
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return count
}

func pick(r *rand.Rand, weights []float64) int {
	x := r.Float64()
	for i, w := range weights {
		if x < w {
			return i
		}
		x -= w
	}
	return len(weights) - 1
}

func writeLargeMessage(w *bufio.Writer, r *rand.Rand, i, att int, text []byte) int64 {
	var n int64
	put := func(s string) { k, _ := w.WriteString(s); n += int64(k) }
	put(fmt.Sprintf("From - Thu Oct 15 10:%02d:00 2026\r\nX-Mozilla-Status: 0001\r\nX-Mozilla-Status2: 00000000\r\n", i%60))
	put(fmt.Sprintf("From: Absender %d <absender%d@example.org>\r\nTo: verwaltung@example.org\r\nSubject: Testmail %d\r\n", i%97, i%97, i))
	put(fmt.Sprintf("Date: Thu, 15 Oct 2026 10:%02d:00 +0200\r\nMessage-ID: <large-%d@import.example.org>\r\nMIME-Version: 1.0\r\n", i%60, i))
	put("Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	text = text[:0]
	for len(text) < 11<<10 {
		text = append(text, "Sehr geehrte Damen und Herren, anbei die Unterlagen zur Nebenkostenabrechnung. Mit freundlichen Grüßen\r\n"...)
	}
	k, _ := w.Write(text)
	n += int64(k)
	if att > 0 {
		put(fmt.Sprintf("--b1\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"anlage-%d.bin\"\r\n"+
			"Content-Transfer-Encoding: base64\r\n\r\n", i))
		raw := make([]byte, 57*1024)
		line := make([]byte, 0, 78)
		for left := att; left > 0; left -= len(raw) {
			chunk := raw[:min(len(raw), left)]
			for j := range chunk {
				chunk[j] = byte(r.Uint32())
			}
			for off := 0; off < len(chunk); off += 57 {
				part := chunk[off:min(off+57, len(chunk))]
				line = base64.StdEncoding.AppendEncode(line[:0], part)
				line = append(line, '\r', '\n')
				k, _ := w.Write(line)
				n += int64(k)
			}
		}
	}
	put("--b1--\r\n\r\n")
	return n
}

// peakHeap misst den größten HeapAlloc, solange stop offen ist.
func peakHeap(stop <-chan struct{}) *atomic.Uint64 {
	var peak atomic.Uint64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ms runtime.MemStats
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak.Load() {
				peak.Store(ms.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
	return &peak
}

// 200 MB in einer Datei, darunter eine Nachricht mit 30 MB Anhang: Der
// Speicher bleibt flach (Heap weit unter der Dateigröße, unabhängig von
// ihr), alle Nachrichten kommen an, ein zweiter Lauf liest nichts.
func TestImportLargeFileStreams(t *testing.T) {
	skipShort(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "Alle Nachrichten")
	count := largeMbox(t, path, 200<<20, true)
	fi, _ := os.Stat(path)
	e := newEnv(t)
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	stop := make(chan struct{})
	peak := peakHeap(stop)
	start := time.Now()
	rep := e.run(t, path, FormatMbox, "[Gmail]/Alle Nachrichten", Options{})
	close(stop)
	d := time.Since(start)
	growth := int64(peak.Load()) - int64(base.HeapAlloc)
	t.Logf("%d messages, %.1f MB in %.1fs (%.1f MB/s); heap before %.1f MB, peak %.1f MB (+%.1f MB)", count,
		float64(fi.Size())/1e6, d.Seconds(), float64(fi.Size())/1e6/d.Seconds(), float64(base.HeapAlloc)/1e6,
		float64(peak.Load())/1e6, float64(growth)/1e6)
	if rep.New != count || rep.Errors != 0 || rep.Bytes != fi.Size() {
		t.Fatalf("report = new %d errors %d bytes %d, want %d/0/%d", rep.New, rep.Errors, rep.Bytes, count, fi.Size())
	}
	if growth > 64<<20 {
		t.Fatalf("heap grew by %.1f MB while importing a %.0f MB file; the import must stream", float64(growth)/1e6, float64(fi.Size())/1e6)
	}
	if n := e.count(t, `SELECT count(*) FROM attachments WHERE size >= 30000000`); n != 1 {
		t.Fatalf("30 MB attachment rows = %d", n)
	}
	if rep := e.run(t, path, FormatMbox, "[Gmail]/Alle Nachrichten", Options{}); rep.New != 0 || rep.Unchanged != 1 {
		t.Fatalf("second run = %+v", rep)
	}
}

// TestImportThroughput misst den Durchsatz (docs/import_messung.md):
// POSTKAUZ_IMPORT_MEASURE=<MB> (Größe der mbox), optional
// POSTKAUZ_IMPORT_DIR=<ordner> (Ablage der Testdaten, Standard: temporär).
// Gemessen werden Trockenlauf (nur lesen und abgleichen), Import ohne Jobs,
// zweiter Lauf mit --rescan (alles vorhanden) und danach die Jobs
// (Extraktion, Suchindex) getrennt.
func TestImportThroughput(t *testing.T) {
	mb, err := strconv.Atoi(os.Getenv("POSTKAUZ_IMPORT_MEASURE"))
	if err != nil || mb <= 0 {
		t.Skip("set POSTKAUZ_IMPORT_MEASURE=<MB> to measure the import (see docs/import_messung.md)")
	}
	dir := os.Getenv("POSTKAUZ_IMPORT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	path := filepath.Join(dir, "Alle Nachrichten")
	start := time.Now()
	count := largeMbox(t, path, int64(mb)<<20, false)
	fi, _ := os.Stat(path)
	size := float64(fi.Size()) / 1e6
	t.Logf("generated %d messages, %.1f MB (%.2f MB per message) in %.1fs", count, size, size/float64(count), time.Since(start).Seconds())
	e := newEnv(t)
	measure := func(name string, o Options) Report {
		runtime.GC()
		var base runtime.MemStats
		runtime.ReadMemStats(&base)
		stop := make(chan struct{})
		peak := peakHeap(stop)
		start := time.Now()
		rep := e.run(t, path, FormatMbox, "[Gmail]/Alle Nachrichten", o)
		close(stop)
		d := time.Since(start).Seconds()
		t.Logf("%-28s %6.1fs  %6.1f MB/s  %7.0f messages/s  new %d existing %d  heap peak %.1f MB (+%.1f)", name, d, size/d,
			float64(count)/d, rep.New, rep.Existing, float64(peak.Load())/1e6, float64(int64(peak.Load())-int64(base.HeapAlloc))/1e6)
		return rep
	}
	measure("dry run", Options{DryRun: true})
	if rep := measure("import (no jobs)", Options{}); rep.New != count {
		t.Fatalf("new = %d, want %d", rep.New, count)
	}
	measure("rescan (all existing)", Options{Rescan: true})
	jstart := time.Now()
	jr := &jobs.Runner{Store: e.st, Blobs: e.blobs, Extractor: extract.New(extract.Options{}), TakeOver: true}
	jrep, err := jr.RunAccount(context.Background(), e.acct)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("jobs after the import: %d messages indexed, %d attachments extracted in %.1fs", jrep.Indexed, jrep.Extracted,
		time.Since(jstart).Seconds())
	if n := e.count(t, `SELECT count(*) FROM messages`); n != count {
		t.Fatalf("messages = %d", n)
	}
}
