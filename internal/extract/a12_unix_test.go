//go:build unix

package extract

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A12 Teil 0 (c): Nur ein Fehler aus Start ist „start“ (vorübergehend);
// mehr als 2 KB auf stderr bei Exit 0 ist endgültig (io); ein Abbruch über
// den Kontext ist kein Fehler des Anhangs.
func TestA12ClassifyAfterStart(t *testing.T) {
	ctx := context.Background()
	t.Run("stderr full, exit 0", func(t *testing.T) {
		x := New(Options{PDFToText: "/bin/sh", PDFArgs: []string{"-c", "head -c 5000 /dev/zero | tr '\\0' x >&2; exit 0"}})
		r := x.Extract(ctx, KindPDF, strings.NewReader("%PDF"), 4)
		if r.Status != StatusFailed || r.ErrKind != ErrKindIO || r.Transient {
			t.Fatalf("result = %+v", r)
		}
	})
	t.Run("start fails", func(t *testing.T) {
		x := New(Options{PDFToText: t.TempDir()}) // ein Ordner ist kein Programm
		r := x.Extract(ctx, KindPDF, strings.NewReader("%PDF"), 4)
		if r.Status != StatusFailed || r.ErrKind != ErrKindStart || !r.Transient {
			t.Fatalf("result = %+v", r)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		x := New(Options{PDFToText: "/bin/sh", PDFArgs: []string{"-c", "sleep 5"}})
		c, cancel := context.WithCancel(ctx)
		time.AfterFunc(100*time.Millisecond, cancel)
		_, err := x.run(c, "/bin/sh", []string{"-c", "sleep 5"}, nil, strings.NewReader(""))
		var re *RunError
		if !errors.Is(err, context.Canceled) || errors.As(err, &re) {
			t.Fatalf("err = %v", err)
		}
	})
}
