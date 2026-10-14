package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// tcpProxy ist ein Mini-Proxy, um Verbindungsabbrueche zu simulieren:
// cutAll() schliesst alle Verbindungen hart, blackhole() leitet nichts mehr weiter.
type tcpProxy struct {
	ln       net.Listener
	target   string
	mu       sync.Mutex
	conns    []net.Conn
	blackout bool
}

func newProxy(target string) (*tcpProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &tcpProxy{ln: ln, target: target}
	go p.serve()
	return p, nil
}

func (p *tcpProxy) addr() string { return p.ln.Addr().String() }

func (p *tcpProxy) serve() {
	for {
		in, err := p.ln.Accept()
		if err != nil {
			return
		}
		out, err := net.Dial("tcp", p.target)
		if err != nil {
			in.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, in, out)
		p.mu.Unlock()
		go p.pipe(in, out)
		go p.pipe(out, in)
	}
}

func (p *tcpProxy) pipe(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			p.mu.Lock()
			bo := p.blackout
			p.mu.Unlock()
			if !bo {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				dst.Close()
			}
			dst.Close()
			return
		}
	}
}

func (p *tcpProxy) cutAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0) // RST statt FIN
		}
		c.Close()
	}
	p.conns = nil
}

func (p *tcpProxy) setBlackhole(on bool) {
	p.mu.Lock()
	p.blackout = on
	p.mu.Unlock()
}

func (p *tcpProxy) close() { p.ln.Close(); p.cutAll() }

// idleEvent ist eine empfangene unilaterale Antwort.
type idleEvent struct {
	At     time.Time
	Exists uint32
}

// idleResult ist der Report von IDLE + Reconnect.
type idleResult struct {
	Appends        []map[string]any `json:"appends"`
	EventsReceived int              `json:"events_received"`
	IdleCloseErr   string           `json:"idle_close_error,omitempty"`
	Reconnect      []map[string]any `json:"reconnect"`
	ResumeSync     map[string]any   `json:"resume_sync,omitempty"`
}

func runIdle(cfg *config) (*idleResult, error) {
	res := &idleResult{}
	events := make(chan idleEvent, 64)
	handler := &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				events <- idleEvent{At: time.Now(), Exists: *d.NumMessages}
			}
		},
	}
	// Verbindung A ueber den Mini-Proxy, damit wir sie spaeter kappen koennen.
	px, err := newProxy(cfg.Addr)
	if err != nil {
		return nil, err
	}
	defer px.close()
	pcfg := *cfg
	pcfg.Addr = px.addr()
	a, err := dial(&pcfg, handler)
	if err != nil {
		return nil, err
	}
	if _, err := a.Select("INBOX", nil).Wait(); err != nil {
		return nil, err
	}
	idle, err := a.Idle()
	if err != nil {
		return nil, fmt.Errorf("IDLE: %w", err)
	}
	b, err := dial(cfg, nil)
	if err != nil {
		return nil, err
	}
	// Zwei Fremd-APPENDs: nach 3 s und nach 10 s (Dovecot hibernisiert IDLE nach 5 s).
	deadline := time.Now().Add(time.Duration(cfg.IdleSecs) * time.Second)
	for _, at := range []time.Duration{3 * time.Second, 10 * time.Second} {
		if time.Duration(cfg.IdleSecs)*time.Second <= at {
			break
		}
		time.Sleep(time.Until(deadline.Add(-time.Duration(cfg.IdleSecs) * time.Second).Add(at)))
		raw, id, _ := testMail("IDLE-Test: Benachrichtigung über neue Post", "idle")
		t0 := time.Now()
		uid, err := appendRaw(b, "INBOX", raw, nil)
		entry := map[string]any{"after_s": at.Seconds(), "uid": uid, "message_id": id, "append_ms": ms(time.Since(t0))}
		if err != nil {
			entry["error"] = err.Error()
		}
		select {
		case ev := <-events:
			entry["notified"] = true
			entry["latency_ms_from_append_start"] = ms(ev.At.Sub(t0))
			entry["exists"] = ev.Exists
			res.EventsReceived++
		case <-time.After(time.Until(deadline)):
			entry["notified"] = false
		}
		res.Appends = append(res.Appends, entry)
	}
	time.Sleep(time.Until(deadline))
	if err := idle.Close(); err != nil {
		res.IdleCloseErr = err.Error()
	}
	if err := idle.Wait(); err != nil {
		res.IdleCloseErr = err.Error()
	}

	// --- Reconnect-Tests ---
	// 1) harter Abbruch (RST) auf Netzwerkebene waehrend IDLE
	res.Reconnect = append(res.Reconnect, cutTest(&pcfg, a, "rst_during_idle", func() { px.cutAll() }, nil))
	// 2) serverseitiger Kick (z. B. doveadm kick), falls konfiguriert
	if cfg.KickCmd != "" {
		a2, err := dialRetry(&pcfg, handler)
		if err == nil {
			_, _ = a2.Select("INBOX", nil).Wait()
			res.Reconnect = append(res.Reconnect, cutTest(&pcfg, a2, "server_kick", func() {
				out, err := exec.Command("sh", "-c", cfg.KickCmd).CombinedOutput()
				if err != nil {
					fmt.Println("kick:", err, string(out))
				}
			}, nil))
		}
	}
	// 3) stilles Wegbrechen (Paketverlust ohne FIN/RST): wird es erkannt?
	a3, err := dialRetry(&pcfg, handler)
	if err == nil {
		_, _ = a3.Select("INBOX", nil).Wait()
		// Achtung: ohne repair haengt der Reconnect-Login endlos (kein Kommando-Timeout in go-imap).
		r := cutTest(&pcfg, a3, "blackhole_during_idle", func() { px.setBlackhole(true) },
			func() { px.setBlackhole(false); px.cutAll() })
		res.Reconnect = append(res.Reconnect, r)
	}
	// Waehrend die Verbindung weg ist, kommt noch eine Mail an.
	// (b wurde ggf. vom Server-Kick mit getrennt, daher frisch verbinden)
	b.Close()
	b, err = dialRetry(cfg, nil)
	if err != nil {
		return res, err
	}
	raw, _, _ := testMail("Während der Trennung eingegangen", "offline")
	offUID, err := appendRaw(b, "INBOX", raw, nil)
	if err != nil {
		res.ResumeSync = map[string]any{"offline_append_error": err.Error()}
	}
	b.Logout().Wait()

	// Wiederaufnahme mit gespeichertem Zustand: Inkrementalsync auf neuer Verbindung
	st, err := openStore(cfg.DataDir)
	if err != nil {
		return res, err
	}
	defer st.Close()
	t0 := time.Now()
	c, err := dialRetry(cfg, nil)
	if err != nil {
		return res, err
	}
	defer c.Logout()
	sr, err := syncAll(cfg, c, st)
	if err != nil {
		return res, err
	}
	var newUIDs []uint32
	for _, mb := range sr.Mailboxes {
		if mb.Name == "INBOX" {
			newUIDs = mb.NewUIDs
		}
	}
	want := []uint32{offUID}
	for _, a := range res.Appends {
		if u, ok := a["uid"].(uint32); ok {
			want = append(want, u)
		}
	}
	have := map[uint32]bool{}
	for _, u := range newUIDs {
		have[u] = true
	}
	ok := offUID > 0
	for _, u := range want {
		ok = ok && have[u]
	}
	res.ResumeSync = map[string]any{"mode": sr.Mode, "inbox_new_uids": newUIDs, "expected_uids": want,
		"ok": ok, "seconds": time.Since(t0).Seconds()}
	return res, nil
}

// cutTest startet IDLE, kappt die Verbindung und misst, wann der Client es merkt.
func cutTest(cfg *config, c *imapclient.Client, name string, cut, repair func()) map[string]any {
	r := map[string]any{"case": name}
	idle, err := c.Idle()
	if err != nil {
		r["idle_error"] = err.Error()
	}
	time.Sleep(500 * time.Millisecond)
	t0 := time.Now()
	cut()
	select {
	case <-c.Closed():
		r["detected"] = true
		r["detect_ms"] = ms(time.Since(t0))
	case <-time.After(20 * time.Second):
		r["detected"] = false
		r["note"] = "nach 20 s nicht erkannt (kein Keepalive)"
		// Noop scheitert, solange IDLE laeuft; deshalb Client hart schliessen
	}
	if idle != nil {
		done := make(chan error, 1)
		go func() { _ = idle.Close(); done <- idle.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				r["idle_wait_error"] = err.Error()
			}
		case <-time.After(3 * time.Second):
			r["idle_close_hang"] = true
		}
	}
	c.Close()
	if repair != nil {
		repair()
	}
	// Reconnect mit Backoff und erneutem SELECT
	t1 := time.Now()
	nc, err := dialRetry(cfg, nil)
	if err != nil {
		r["reconnect_ok"] = false
		r["reconnect_error"] = err.Error()
		return r
	}
	_, err = nc.Select("INBOX", nil).Wait()
	r["reconnect_ok"] = err == nil
	r["reconnect_ms"] = ms(time.Since(t1))
	nc.Logout().Wait()
	nc.Close()
	return r
}

// dialRetry verbindet mit exponentiellem Backoff (0,25 s ... 4 s).
func dialRetry(cfg *config, h *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	var err error
	delay := 250 * time.Millisecond
	for i := 0; i < 6; i++ {
		var c *imapclient.Client
		c, err = dial(cfg, h)
		if err == nil {
			return c, nil
		}
		time.Sleep(delay)
		delay *= 2
	}
	return nil, err
}

// runQResync prueft, was nach ENABLE QRESYNC mit unilateralem VANISHED passiert.
func runQResync(cfg *config) (map[string]any, error) {
	r := map[string]any{}
	var expunges int
	a, err := dial(cfg, &imapclient.UnilateralDataHandler{Expunge: func(uint32) { expunges++ }})
	if err != nil {
		return nil, err
	}
	defer a.Close()
	if !a.Caps().Has(imap.CapQResync) {
		r["supported_by_server"] = false
		return r, nil
	}
	r["supported_by_server"] = true
	en, err := a.Enable(imap.CapQResync).Wait()
	if err != nil {
		r["enable_error"] = err.Error()
		return r, nil
	}
	r["enabled"] = en.Caps.Has(imap.CapQResync)
	if _, err := a.Select("INBOX", &imap.SelectOptions{CondStore: true}).Wait(); err != nil {
		r["select_error"] = err.Error()
		return r, nil
	}
	b, err := dial(cfg, nil)
	if err != nil {
		return nil, err
	}
	raw, _, _ := testMail("QRESYNC-Test (wird gelöscht)", "qresync")
	if _, err := b.Select("INBOX", nil).Wait(); err != nil {
		return nil, err
	}
	uid, err := appendRaw(b, "INBOX", raw, []imap.Flag{imap.FlagDeleted})
	if err != nil {
		return nil, err
	}
	// A soll die neue Mail erst sehen (EXISTS), dann loescht B sie.
	if err := a.Noop().Wait(); err != nil {
		r["noop1_error"] = err.Error()
	}
	if err := b.UIDExpunge(imap.UIDSetNum(imap.UID(uid))).Close(); err != nil {
		r["expunge_error"] = err.Error()
	}
	b.Logout().Wait()
	err = a.Noop().Wait()
	if err != nil {
		r["noop_after_vanished_error"] = err.Error()
	}
	select {
	case <-a.Closed():
		r["connection_closed_by_client"] = true
	case <-time.After(500 * time.Millisecond):
		r["connection_closed_by_client"] = false
	}
	r["expunge_callbacks"] = expunges
	return r, nil
}
