package httpmcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Nachbesserung T5, Punkt 9 (l): Abgewiesene Anfragen (401, 404, 405,
// 415, 421, 429) und /healthz füllen das Log nicht: höchstens eine
// Sammelzeile je 10 s mit Zählern; angenommene Anfragen weiter einzeln.
func TestT5NRejectedRequestsDoNotFloodTheLog(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	clock := time.Now()
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }
	ts, s := e.start(t, nil, func(d *Deps) { d.Now = now })
	host := strings.TrimPrefix(ts.URL, "http://")
	do := func(method, path, host string, hdr map[string]string) int {
		req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(initBody))
		req.Host = host
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	jsonHdr := map[string]string{"Content-Type": "application/json"}
	wrong := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + ops.KeyPrefix + "wrong-0123456789abcdef"}
	codes := map[int]int{}
	for i := 0; i < 1000; i++ {
		var c int
		switch i % 6 {
		case 0:
			c = do(http.MethodPost, PathMCP, host, jsonHdr) // ohne Bearer
		case 1:
			c = do(http.MethodPost, PathMCP, host, wrong) // falscher Schlüssel
		case 2:
			c = do(http.MethodGet, PathHealth, host, nil) // ohne Grenze
		case 3:
			c = do(http.MethodGet, "/nothing-here", host, nil)
		case 4:
			c = do(http.MethodPost, PathMCP, "evil.example.org", jsonHdr) // fremder Host
		case 5:
			c = do(http.MethodPost, PathMCP, host, map[string]string{"Content-Type": "text/plain"})
		}
		codes[c]++
	}
	type line struct {
		Msg      string `json:"msg"`
		Count    int    `json:"count"`
		ByStatus string `json:"by_status"`
	}
	read := func() (single, summaries, total int) {
		for _, l := range strings.Split(strings.TrimSpace(e.log.String()), "\n") {
			var v line
			if l == "" || json.Unmarshal([]byte(l), &v) != nil {
				continue
			}
			switch v.Msg {
			case "http request":
				single++
			case "http requests rejected":
				summaries++
				total += v.Count
			}
		}
		return
	}
	if single, summaries, _ := read(); single != 0 || summaries > 2 {
		t.Errorf("1000 rejected requests (%v) gave %d single and %d summary lines, want 0 and at most 2:\n%s", codes, single, summaries, e.log.String())
	}
	// Nach 10 s schreibt die nächste Anfrage (hier eine angenommene) die
	// ausstehenden Zähler; die angenommene steht einzeln im Log.
	advance(rejectLogEvery + time.Second)
	if c := do(http.MethodPost, PathMCP, host, map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
		"Authorization": "Bearer " + e.keys["reader"]}); c != http.StatusOK {
		t.Fatalf("valid key: %d", c)
	}
	single, summaries, total := read()
	if single != 1 || summaries > 3 || total != 1000 {
		t.Errorf("after 10 s: %d single lines (want 1), %d summaries (want at most 3), %d counted (want 1000):\n%s", single, summaries, total, e.log.String())
	}
	if strings.Contains(e.log.String(), "wrong-0123456789abcdef") || strings.Contains(e.log.String(), "evil.example.org") {
		t.Error("log carries request data")
	}
}

// Punkt 9 (l): Am Ende von Serve stehen ausstehende Zähler im Log.
func TestT5NRejectLogFlush(t *testing.T) {
	var b syncBuffer
	l := &rejectLog{log: newJSONLogger(&b)}
	t0 := time.Now()
	for i := 0; i < 50; i++ {
		l.add(t0, http.StatusTooManyRequests, "")
	}
	l.add(t0, http.StatusUnauthorized, "invalid_token")
	if n := strings.Count(b.String(), "http requests rejected"); n != 1 {
		t.Fatalf("summary lines before flush: %d, want 1", n)
	}
	l.flush(t0.Add(time.Second))
	if !strings.Contains(b.String(), `"by_status":"401 invalid_token: 1, 429: 49"`) {
		t.Errorf("flush:\n%s", b.String())
	}
	l.flush(t0.Add(2 * time.Second)) // nichts mehr offen: keine Zeile
	if n := strings.Count(b.String(), "http requests rejected"); n != 2 {
		t.Errorf("summary lines after two flushes: %d, want 2", n)
	}
}

func newJSONLogger(w *syncBuffer) *slog.Logger { return slog.New(slog.NewJSONHandler(w, nil)) }
