package mailsync

// Leck-Tests (A01 N3): Nach Erstsync und nach dem Dauerbetrieb (IDLE)
// bleiben keine Goroutinen zurück. Zwei Prüfungen:
//
//  1. Das Profil goroutineleak (Go 1.27, runtime/pprof): Goroutinen, die an
//     einem nicht mehr erreichbaren Kanal, Mutex o. Ä. für immer
//     blockieren. Es darf keine Zeile aus diesem Modul enthalten.
//  2. Ein Vergleich aller Goroutinen vor und nach dem Ablauf: Keine neue
//     Goroutine darf in Code dieses Moduls (ohne Tests) oder in einem
//     go-imap-Client stecken (eine offene Verbindung wäre so ein Leck,
//     das Profil 1 nicht sieht, weil sie im Netz und nicht an einem
//     Kanal wartet).

import (
	"bytes"
	"context"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

const modulePath = "github.com/KNUFFIXX/postkauz/"

// goroutineBlocks liefert die Stapel aller Goroutinen, nach Kennung.
func goroutineBlocks() map[string]string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	out := map[string]string{}
	for _, b := range strings.Split(string(buf), "\n\n") {
		head, _, _ := strings.Cut(b, "\n")
		if f := strings.Fields(head); len(f) >= 2 && f[0] == "goroutine" {
			out[f[1]] = b
		}
	}
	return out
}

// suspicious: Goroutine steckt in Code dieses Moduls (nicht in Tests) oder
// in einem go-imap-Client.
func suspicious(stack string) bool {
	if strings.Contains(stack, "emersion/go-imap/v2/imapclient.") {
		return true
	}
	for _, line := range strings.Split(stack, "\n") {
		if !strings.HasPrefix(line, "\t") {
			continue
		}
		file, _, _ := strings.Cut(strings.TrimSpace(line), ":")
		if strings.Contains(file, "/internal/") && strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") &&
			strings.Contains(stack, modulePath) {
			return true
		}
	}
	return false
}

// checkNoLeaks prüft nach f, dass keine Goroutine zurückbleibt.
func checkNoLeaks(t *testing.T, f func()) {
	t.Helper()
	before := goroutineBlocks()
	f()
	var leaked []string
	deadline := time.Now().Add(3 * time.Second)
	for {
		leaked = leaked[:0]
		for id, st := range goroutineBlocks() {
			if _, ok := before[id]; !ok && suspicious(st) && !strings.Contains(st, "checkNoLeaks") {
				leaked = append(leaked, st)
			}
		}
		if len(leaked) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond) // Goroutinen dürfen kurz nachlaufen
	}
	if len(leaked) > 0 {
		t.Fatalf("%d goroutine(s) left behind:\n\n%s", len(leaked), strings.Join(leaked, "\n\n"))
	}
	var prof bytes.Buffer
	if err := pprof.Lookup("goroutineleak").WriteTo(&prof, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prof.String(), "goroutineleak profile: total") {
		t.Fatalf("unexpected goroutineleak profile format:\n%s", prof.String())
	}
	if strings.Contains(prof.String(), modulePath) {
		t.Fatalf("goroutineleak profile names this module:\n%s", prof.String())
	}
}

func TestNoGoroutineLeakAfterInitialSync(t *testing.T) {
	addr, u := memServer(t, false)
	_ = u.Create("Archiv", nil)
	for i := 1; i <= 30; i++ {
		_, _ = u.Append([]string{"INBOX", "Archiv"}[i%2], lit(mail(i)), &imap.AppendOptions{})
	}
	e := newEnv(t, addr)
	checkNoLeaks(t, func() {
		prov := connect(t, addr, 0)
		eng := e.engine(t, prov)
		rep, err := eng.SyncAll(context.Background(), Options{Window: 7})
		if err != nil || rep.Mirrored != 30 {
			t.Fatalf("sync: %v, mirrored %d", err, rep.Mirrored)
		}
		_ = prov.Close()
	})
}

func TestNoGoroutineLeakAfterWatch(t *testing.T) {
	addr, u := memServer(t, false)
	e := newEnv(t, addr)
	checkNoLeaks(t, func() {
		prov := connect(t, addr, 200*time.Millisecond)
		eng := e.engine(t, prov)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- eng.Watch(ctx, WatchOptions{Interval: time.Hour, IdleRetry: 50 * time.Millisecond}) }()
		time.Sleep(150 * time.Millisecond) // IDLE läuft
		_, _ = u.Append("INBOX", lit(mail(1)), &imap.AppendOptions{})
		waitFor(t, 5*time.Second, func() bool { return e.count(t, `SELECT count(*) FROM messages`) == 1 })
		time.Sleep(300 * time.Millisecond) // mindestens ein IDLE-Neustart
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		_ = prov.Close()
	})
}

// Der Leck-Test selbst muss Lecks finden: eine absichtlich vergessene
// Goroutine in Code dieses Moduls (Attrappe) wird gemeldet.
func TestLeakCheckFindsLeak(t *testing.T) {
	stack := "goroutine 7 [chan receive]:\n" + modulePath + "internal/mailsync.(*Engine).idleLoop(...)\n" +
		"\t/src/" + modulePath + "internal/mailsync/watch.go:118 +0x1\n"
	if !suspicious(stack) {
		t.Fatal("stack in watch.go not reported")
	}
	if suspicious("goroutine 8 [IO wait]:\nnet.(*conn).Read(...)\n\t/go/src/net/net.go:1 +0x1\n") {
		t.Fatal("foreign stack reported")
	}
	if !suspicious("goroutine 9 [select]:\ngithub.com/emersion/go-imap/v2/imapclient.(*Client).read(...)\n") {
		t.Fatal("open IMAP client not reported")
	}
}
