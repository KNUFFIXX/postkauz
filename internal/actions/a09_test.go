package actions

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A09 Teil 1b: Der Kopfvergleich vor Aktionen entfaltet beide Seiten
// (RFC 5322 3.2.2). Gmail liefert eine lange Kopfzeile in
// BODY.PEEK[HEADER] gefaltet, in BODY.PEEK[] (abgelegte Kopie, lokaler
// Blob) ungefaltet.

// a09Subject: Betreff aus drei encoded-words auf einer Zeile (über 78
// Zeichen), Wörter getrennt durch sep.
func a09Subject(sep string) string {
	return "Subject: =?utf-8?q?R=C3=BCckfrage_zur_Abrechnung_der_Nebenkosten?=" + sep +
		"=?utf-8?q?f=C3=BCr_das_Jahr_mit_Belegen_und_Erl=C3=A4uterungen?=" + sep +
		"=?utf-8?q?und_der_Bitte_um_kurze_Pr=C3=BCfung?=\r\n"
}

func TestA09HeaderFolding(t *testing.T) {
	const head = "Message-ID: <b@example.org>\r\n"
	const tail = "From: demo@example.org\r\n\r\n"
	local := func(sep string) string { return head + a09Subject(sep) + tail + "B\r\n" }
	for _, tc := range []struct {
		name, sep, server, want string
		logHas                  []string
	}{
		{"folded with space", " ", head + a09Subject("\r\n ") + tail, store.TargetOK, nil},
		{"folded with tab", "\t", head + a09Subject("\r\n\t") + tail, store.TargetOK, nil},
		{"folded, LF only", " ", strings.ReplaceAll(head+a09Subject("\r\n ")+tail, "\r\n", "\n"), store.TargetOK, nil},
		{"extra word at a fold", " ", head + a09Subject("\r\n neu ") + tail, store.TargetStale,
			[]string{"Subject (local ", "; server "}},
		{"changed header", " ", head + strings.Replace(a09Subject("\r\n "), "Belegen", "Rechnung", 1) + tail, store.TargetStale,
			[]string{"Subject (local "}},
		{"additional header", " ", head + a09Subject("\r\n ") + "X-Extra: 1\r\n" + tail, store.TargetStale,
			[]string{"X-Extra (server 10)"}},
		{"whitespace between words removed", " ", head + a09Subject("\r\n") + tail, store.TargetStale, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, acct, ids := env(t)
			ctx := context.Background()
			svc := &Service{Store: st}
			dest := Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
			plan, err := svc.PlanMove(ctx, []int64{2}, "INBOX", dest)
			if err != nil {
				t.Fatal(err)
			}
			a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{2}, Plan: plan, RequestedBy: "cli",
				Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path}})
			if err != nil {
				t.Fatal(err)
			}
			b := blobs()
			b["sha-b@example.org"] = local(tc.sep)
			var logBuf bytes.Buffer
			f := &fakeRemote{headers: map[provider.UID]string{2: tc.server}}
			r := &Runner{Store: st, Blobs: b, Log: slog.New(slog.NewTextHandler(&logBuf, nil))}
			if _, err := r.Run(ctx, acct, f); err != nil {
				t.Fatal(err)
			}
			got, _ := st.Action(ctx, a.ID)
			if got.Targets[0].Result != tc.want {
				t.Fatalf("result = %+v", got.Targets)
			}
			log := logBuf.String()
			for _, s := range tc.logHas {
				if !strings.Contains(log, s) {
					t.Errorf("log lacks %q:\n%s", s, log)
				}
			}
			for _, s := range []string{"Abrechnung", "Rechnung", "utf-8", "neu", "example"} {
				if strings.Contains(log, s) {
					t.Errorf("log shows content %q:\n%s", s, log)
				}
			}
		})
	}
}

// normHeader und headerDiff einzeln; Namen vom Server nur aus
// [A-Za-z0-9-], höchstens 40 Zeichen.
func TestA09NormHeaderAndDiff(t *testing.T) {
	if got := string(normHeader([]byte("A: 1\r\n 2\r\n\t3\r\nB: x\r\n\r\n"))); got != "A: 1 2\t3\nB: x" {
		t.Fatalf("normHeader = %q", got)
	}
	if got := headerDiff([]byte("A: 1\r\n"), []byte("A: 1\r\n")); got != "none" {
		t.Fatalf("same = %q", got)
	}
	// A10 Teil 0 (b): nur umgestellt, nicht „none“.
	if got := headerDiff([]byte("A: 1\r\nB: 2\r\n"), []byte("B: 2\r\nA: 1\r\n")); got != "order differs" {
		t.Fatalf("reordered = %q", got)
	}
	long := strings.Repeat("X", 41)
	got := headerDiff([]byte("A: 1\r\nB: 22\r\n"), []byte("A: 1\r\nB: 2\r\nX y: z\r\n"+long+": 1\r\nkein Doppelpunkt\r\n"))
	want := "B (local 5; server 4), ? (server 6, 44, 16)"
	if got != want {
		t.Fatalf("diff = %q\nwant   %q", got, want)
	}
	var many strings.Builder
	for i := 0; i < 12; i++ {
		many.WriteString("H" + strings.Repeat("a", i) + ": 1\r\n")
	}
	if got := headerDiff(nil, []byte(many.String())); !strings.HasSuffix(got, ", and 2 more") {
		t.Fatalf("many = %q", got)
	}
}
