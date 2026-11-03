package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Bridge reicht eine MCP-Verbindung (stdio) an den entfernten Endpunkt
// weiter.
type Bridge struct {
	// Name des Ziels ([remote.<name>]), nur für Fehlertexte.
	Name string
	// URL des Endpunkts (geprüft mit config.CheckRemoteURL).
	URL string
	// Key ist der PostKauz-Schlüssel für den entfernten Server.
	Key secrets.Secret
	// HTTPClient: nil = NewHTTPClient().
	HTTPClient *http.Client
	// Log: Warnungen ohne Inhalt und ohne Schlüssel; nil = keine.
	Log *slog.Logger
	// CallTimeout: 0 = DefaultCallTimeout. MaxMessageBytes: 0 =
	// DefaultMaxMessageBytes.
	CallTimeout     time.Duration
	MaxMessageBytes int
}

// Protokollversion, ab der der GET-Strom entfällt und Mcp-Method/Mcp-Name
// Pflicht sind (SEP-2575).
const version20260728 = "2026-07-28"

// Kopfzeilen von Streamable HTTP.
const (
	hdrSession  = "Mcp-Session-Id"
	hdrVersion  = "Mcp-Protocol-Version"
	hdrMethod   = "Mcp-Method"
	hdrName     = "Mcp-Name"
	hdrLastID   = "Last-Event-ID"
	contentJSON = "application/json"
	contentSSE  = "text/event-stream"
)

type bridge struct {
	b      *Bridge
	local  sdk.Connection
	client *http.Client
	host   string
	maxMsg int

	mu         sync.Mutex
	sessionID  string
	version    string
	initParams json.RawMessage
	inflight   map[any]context.CancelFunc
	canceled   map[any]bool
	listening  bool
	reinitN    int

	reinitMu sync.Mutex
	// calls: laufende Anfragen; streams: der GET-Strom.
	calls   sync.WaitGroup
	streams sync.WaitGroup
}

// Run liest Nachrichten von local, bis local endet (EOF) oder ctx
// abläuft. Ein Ende von local ist kein Fehler; laufende Anfragen bekommen
// danach noch höchstens drainTimeout für ihre Antwort.
func (b *Bridge) Run(ctx context.Context, local sdk.Connection) error {
	s := &bridge{b: b, local: local, client: b.HTTPClient, maxMsg: b.MaxMessageBytes,
		inflight: map[any]context.CancelFunc{}, canceled: map[any]bool{}}
	if s.client == nil {
		s.client = NewHTTPClient()
	}
	if s.maxMsg <= 0 {
		s.maxMsg = DefaultMaxMessageBytes
	}
	if u, err := url.Parse(b.URL); err == nil {
		s.host = u.Host
	}
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		done := make(chan struct{})
		go func() { s.calls.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
		case <-time.After(drainTimeout):
		}
		cancel()
		s.calls.Wait()
		s.streams.Wait()
		s.closeSession()
	}()
	for {
		msg, err := local.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, sdk.ErrConnectionClosed) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch m := msg.(type) {
		case *jsonrpc.Request:
			if m.IsCall() {
				s.startCall(ctx, m)
				continue
			}
			s.notify(ctx, m)
			switch m.Method {
			case "notifications/cancelled":
				s.cancelCall(m.Params)
			case "notifications/initialized":
				s.startListen(ctx)
			}
		case *jsonrpc.Response:
			// Antwort des Clients auf eine Anfrage des Servers.
			s.notify(ctx, m)
		}
	}
}

func (s *bridge) logWarn(msg string, args ...any) {
	if s.b.Log != nil {
		s.b.Log.Warn(msg, append([]any{"remote", s.b.Name}, args...)...)
	}
}

func (s *bridge) write(ctx context.Context, msg jsonrpc.Message) {
	if err := s.local.Write(ctx, msg); err != nil && ctx.Err() == nil {
		s.logWarn("writing to the MCP client failed", "error_type", fmt.Sprintf("%T", err))
	}
}

// --- Anfragen ------------------------------------------------------------------

func (s *bridge) startCall(ctx context.Context, req *jsonrpc.Request) {
	if req.Method == "initialize" {
		s.mu.Lock()
		s.initParams = append(json.RawMessage(nil), req.Params...)
		s.mu.Unlock()
	}
	timeout := s.b.CallTimeout
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	cctx, ccancel := context.WithTimeout(ctx, timeout)
	key := req.ID.Raw()
	s.mu.Lock()
	s.inflight[key] = ccancel
	s.mu.Unlock()
	s.calls.Add(1)
	go func() {
		defer s.calls.Done()
		defer func() {
			ccancel()
			s.mu.Lock()
			delete(s.inflight, key)
			delete(s.canceled, key)
			s.mu.Unlock()
		}()
		_ = crash.Do("remote call", func() error {
			s.call(ctx, cctx, req)
			return nil
		})
	}()
}

// cancelCall bricht die HTTP-Anfrage zu einer abgebrochenen Anfrage ab
// (die Benachrichtigung selbst ist schon weitergereicht).
func (s *bridge) cancelCall(params json.RawMessage) {
	var p struct {
		RequestID any `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || p.RequestID == nil {
		return
	}
	id, err := jsonrpc.MakeID(p.RequestID)
	if err != nil {
		return
	}
	key := id.Raw()
	s.mu.Lock()
	if c, ok := s.inflight[key]; ok {
		s.canceled[key] = true
		c()
	}
	s.mu.Unlock()
}

func (s *bridge) wasCanceled(id jsonrpc.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canceled[id.Raw()]
}

// call reicht eine Anfrage weiter und schreibt alles, was zurückkommt, an
// den Client; ohne Antwort einen Fehler mit derselben ID.
func (s *bridge) call(runCtx, ctx context.Context, req *jsonrpc.Request) {
	answered, err := s.roundTrip(ctx, req, func(m jsonrpc.Message) { s.write(runCtx, m) })
	if answered || s.wasCanceled(req.ID) || runCtx.Err() != nil {
		return
	}
	if err == nil {
		err = &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the server ended the answer without a result; try again"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = &bridgeError{code: CodeUnreachable, msg: s.prefix() + "no answer from the server in time; try again"}
	}
	var we *jsonrpc.Error
	var be *bridgeError
	switch {
	case errors.As(err, &we):
	case errors.As(err, &be):
		we = &jsonrpc.Error{Code: be.code, Message: be.msg}
	default:
		we = &jsonrpc.Error{Code: CodeBadResponse, Message: s.prefix() + "the request failed"}
	}
	s.write(runCtx, &jsonrpc.Response{ID: req.ID, Error: we})
}

// roundTrip schickt req und liefert die Nachrichten des Servers an deliver.
// answered: Die Antwort auf req kam an. Ein Fehler des Servers als
// JSON-RPC-Antwort (z. B. 400 mit Fehlerobjekt) ist eine Antwort.
func (s *bridge) roundTrip(ctx context.Context, req *jsonrpc.Request, deliver func(jsonrpc.Message)) (bool, error) {
	for attempt := 0; ; attempt++ {
		sid := s.session()
		resp, err := s.post(ctx, req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode == http.StatusNotFound && sid != "" && attempt == 0 && req.Method != "initialize" {
			drain(resp)
			if rerr := s.reinit(ctx, sid); rerr != nil {
				return false, rerr
			}
			continue
		}
		answered, err := s.readAnswer(ctx, resp, req, deliver)
		if err == nil || answered {
			return answered, err
		}
		if resp.StatusCode == http.StatusNotFound && sid != "" {
			return false, &bridgeError{code: CodeSessionExpired, msg: s.prefix() +
				"the session on the server has expired and could not be started again; restart the MCP client"}
		}
		return false, err
	}
}

// readAnswer wertet die Antwort auf eine Anfrage aus.
func (s *bridge) readAnswer(ctx context.Context, resp *http.Response, req *jsonrpc.Request, deliver func(jsonrpc.Message)) (bool, error) {
	defer resp.Body.Close()
	s.takeSession(resp)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Ein JSON-RPC-Fehler des Servers zur selben ID geht unverändert
		// weiter (z. B. 400 bei falscher Protokollversion).
		var body []byte
		if ct := mediaType(resp.Header.Get("Content-Type")); ct == contentJSON {
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if m, err := jsonrpc.DecodeMessage(body); err == nil {
				if r, ok := m.(*jsonrpc.Response); ok && r.Error != nil && r.ID.Raw() == req.ID.Raw() {
					deliver(r)
					return true, nil
				}
			}
		}
		return false, s.statusError(resp, body)
	}
	answered := false
	handle := func(m jsonrpc.Message) {
		if r, ok := m.(*jsonrpc.Response); ok && r.ID.Raw() == req.ID.Raw() {
			answered = true
			if req.Method == "initialize" && r.Error == nil {
				s.takeVersion(r.Result)
			}
		}
		deliver(m)
	}
	switch mediaType(resp.Header.Get("Content-Type")) {
	case contentJSON:
		body, err := readLimited(resp.Body, s.maxMsg)
		if err != nil {
			return false, s.readError(err)
		}
		msgs, err := decodeBody(body)
		if err != nil {
			return false, &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the server sent an answer that is not JSON-RPC"}
		}
		for _, m := range msgs {
			handle(m)
		}
	case contentSSE:
		_, _, err := s.readEvents(ctx, resp.Body, func(m jsonrpc.Message) bool {
			handle(m)
			return answered
		})
		if err != nil && !answered {
			return false, s.readError(err)
		}
	default:
		if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
			return false, &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the server accepted the request but sent no answer"}
		}
		return false, &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the server answered with an unknown content type; is the URL the MCP endpoint (…/mcp)?"}
	}
	return answered, nil
}

// notify schickt eine Benachrichtigung oder Antwort (erwartet 202).
func (s *bridge) notify(ctx context.Context, msg jsonrpc.Message) {
	nctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	resp, err := s.post(nctx, msg)
	if err != nil {
		if ctx.Err() == nil {
			s.logWarn("forwarding a notification failed", "reason", reasonOf(err))
		}
		return
	}
	defer drain(resp)
	s.takeSession(resp)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		s.logWarn("the server refused a notification", "status", resp.StatusCode)
	}
}

// post schickt eine Nachricht mit allen Kopfzeilen.
func (s *bridge) post(ctx context.Context, msg jsonrpc.Message) (*http.Response, error) {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return nil, &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the message could not be encoded"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.b.URL, bytes.NewReader(data))
	if err != nil {
		return nil, &bridgeError{code: CodeBadResponse, msg: s.prefix() + "invalid URL in [remote." + s.b.Name + "]"}
	}
	req.Header.Set("Content-Type", contentJSON)
	req.Header.Set("Accept", contentJSON+", "+contentSSE)
	s.setHeaders(req, msg)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, s.netError(ctx, err)
	}
	return resp, nil
}

func (s *bridge) setHeaders(req *http.Request, msg jsonrpc.Message) {
	req.Header.Set("Authorization", "Bearer "+s.b.Key.Reveal())
	s.mu.Lock()
	sid, ver := s.sessionID, s.version
	s.mu.Unlock()
	if sid != "" {
		req.Header.Set(hdrSession, sid)
	}
	r, isReq := msg.(*jsonrpc.Request)
	if isReq {
		if v := metaVersion(r.Params); v != "" {
			ver = v
		} else if r.Method == "initialize" {
			ver = ""
		}
	}
	if ver != "" {
		req.Header.Set(hdrVersion, ver)
	}
	if isReq && ver >= version20260728 {
		req.Header.Set(hdrMethod, r.Method)
		if name, ok := nameOf(r.Method, r.Params); ok {
			req.Header.Set(hdrName, headerValue(name))
		}
	}
}

func (s *bridge) session() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *bridge) takeSession(resp *http.Response) {
	if id := resp.Header.Get(hdrSession); id != "" {
		s.mu.Lock()
		if s.sessionID == "" {
			s.sessionID = id
		}
		s.mu.Unlock()
	}
}

func (s *bridge) takeVersion(result json.RawMessage) {
	var r struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(result, &r) == nil && r.ProtocolVersion != "" && validHeaderValue(r.ProtocolVersion) {
		s.mu.Lock()
		s.version = r.ProtocolVersion
		s.mu.Unlock()
	}
}

// reinit startet nach 404 eine neue Sitzung mit der gemerkten
// initialize-Anfrage des Clients (einmal je abgelaufener Sitzung).
func (s *bridge) reinit(ctx context.Context, expired string) error {
	s.reinitMu.Lock()
	defer s.reinitMu.Unlock()
	s.mu.Lock()
	if s.sessionID != expired {
		// Eine andere Anfrage hat schon neu angemeldet.
		s.mu.Unlock()
		return nil
	}
	s.sessionID = ""
	params := s.initParams
	s.reinitN++
	n := s.reinitN
	s.mu.Unlock()
	if params == nil {
		return &bridgeError{code: CodeSessionExpired, msg: s.prefix() + "the session on the server has expired; restart the MCP client"}
	}
	id, _ := jsonrpc.MakeID("postkauz-reinit-" + strconv.Itoa(n))
	init := &jsonrpc.Request{ID: id, Method: "initialize", Params: params}
	answered, err := s.roundTrip(ctx, init, func(jsonrpc.Message) {})
	if err != nil || !answered {
		return &bridgeError{code: CodeSessionExpired, msg: s.prefix() + "the session on the server has expired and could not be started again; restart the MCP client"}
	}
	s.notify(ctx, &jsonrpc.Request{Method: "notifications/initialized"})
	return nil
}

// closeSession beendet eine Sitzung mit DELETE (nur, wenn es eine gibt).
func (s *bridge) closeSession() {
	sid := s.session()
	if sid == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.b.URL, nil)
	if err != nil {
		return
	}
	s.setHeaders(req, nil)
	if resp, err := s.client.Do(req); err == nil {
		drain(resp)
	}
}

// --- GET-Strom -------------------------------------------------------------------

// startListen öffnet nach notifications/initialized einmal den GET-Strom
// für Nachrichten des Servers, falls er einen anbietet.
func (s *bridge) startListen(ctx context.Context) {
	s.mu.Lock()
	if s.listening || s.version >= version20260728 {
		s.mu.Unlock()
		return
	}
	s.listening = true
	s.mu.Unlock()
	s.streams.Add(1)
	go func() {
		defer s.streams.Done()
		_ = crash.Do("remote listen", func() error {
			s.listen(ctx)
			return nil
		})
	}()
}

func (s *bridge) listen(ctx context.Context) {
	lastID, delay, misses := "", time.Second, 0
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.b.URL, nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept", contentSSE)
		s.setHeaders(req, nil)
		if lastID != "" {
			req.Header.Set(hdrLastID, lastID)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				s.logWarn("server stream not reachable", "reason", reasonOf(err))
			}
		} else {
			if resp.StatusCode != http.StatusOK || mediaType(resp.Header.Get("Content-Type")) != contentSSE {
				// 405 (kein Strom, so bei mcp --http) oder etwas anderes:
				// ohne Strom weiter; Antworten kommen über POST.
				drain(resp)
				return
			}
			id, retry, _ := s.readEvents(ctx, resp.Body, func(m jsonrpc.Message) bool {
				s.write(ctx, m)
				return false
			})
			resp.Body.Close()
			if id != "" && id != lastID {
				lastID, misses = id, 0
			}
			if retry > 0 {
				delay = retry
			}
		}
		misses++
		if misses > maxListenRetries {
			s.logWarn("server stream given up after several attempts")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*3/2, 30*time.Second)
	}
}

// --- SSE -------------------------------------------------------------------------

// readEvents liest Ereignisse eines SSE-Stroms und gibt jede Nachricht an
// fn; fn true beendet das Lesen. Liefert die letzte Ereignis-ID und die
// Wartezeit aus retry:.
func (s *bridge) readEvents(ctx context.Context, body io.Reader, fn func(jsonrpc.Message) bool) (string, time.Duration, error) {
	br := bufio.NewReaderSize(body, 64<<10)
	var data bytes.Buffer
	var lastID, event string
	var retry time.Duration
	for {
		line, err := readLine(br, s.maxMsg)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return lastID, retry, nil
			}
			return lastID, retry, err
		}
		if line == "" {
			if data.Len() > 0 && (event == "" || event == "message") {
				msgs, derr := decodeBody(bytes.TrimSuffix(data.Bytes(), []byte("\n")))
				if derr != nil {
					return lastID, retry, &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the server sent an event that is not JSON-RPC"}
				}
				for _, m := range msgs {
					if fn(m) {
						return lastID, retry, nil
					}
				}
			}
			data.Reset()
			event = ""
			if ctx.Err() != nil {
				return lastID, retry, ctx.Err()
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			if data.Len()+len(value) > s.maxMsg {
				return lastID, retry, errTooLarge
			}
			data.WriteString(value)
			data.WriteByte('\n')
		case "event":
			event = value
		case "id":
			if !strings.ContainsRune(value, 0) {
				lastID = value
			}
		case "retry":
			if ms, err := strconv.Atoi(value); err == nil && ms > 0 {
				retry = time.Duration(ms) * time.Millisecond
			}
		}
	}
}

var errTooLarge = errors.New("message too large")

// readLine liest eine Zeile ohne Zeilenende (LF, CRLF oder CR am Ende)
// mit Obergrenze.
func readLine(br *bufio.Reader, max int) (string, error) {
	var b []byte
	for {
		chunk, err := br.ReadSlice('\n')
		b = append(b, chunk...)
		if len(b) > max+2 {
			return "", errTooLarge
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(b) > 0 && errors.Is(err, io.EOF) {
			break
		}
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// --- Hilfen ----------------------------------------------------------------------

// decodeBody liest eine Nachricht oder einen Stapel (JSON-Array).
func decodeBody(b []byte) ([]jsonrpc.Message, error) {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		var raw []json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, err
		}
		out := make([]jsonrpc.Message, 0, len(raw))
		for _, r := range raw {
			m, err := jsonrpc.DecodeMessage(r)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, nil
	}
	m, err := jsonrpc.DecodeMessage(b)
	if err != nil {
		return nil, err
	}
	return []jsonrpc.Message{m}, nil
}

func readLimited(r io.Reader, max int) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, errTooLarge
	}
	return b, nil
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

func mediaType(ct string) string {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return ""
	}
	return mt
}

// metaVersion: _meta.protocolVersion einer Anfrage (ab 2026-07-28).
func metaVersion(params json.RawMessage) string {
	var p struct {
		Meta struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"_meta"`
	}
	if len(params) == 0 || json.Unmarshal(params, &p) != nil || !validHeaderValue(p.Meta.ProtocolVersion) {
		return ""
	}
	return p.Meta.ProtocolVersion
}

// nameOf: Name des Ziels für Mcp-Name (wie das SDK).
func nameOf(method string, params json.RawMessage) (string, bool) {
	var p struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	}
	if json.Unmarshal(params, &p) != nil {
		return "", false
	}
	switch method {
	case "tools/call", "prompts/get":
		return p.Name, true
	case "resources/read":
		return p.URI, true
	}
	return "", false
}

func validHeaderValue(v string) bool {
	if v == "" || len(v) > 256 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// headerValue kodiert Nicht-ASCII wie das SDK (=?base64?…?=).
func headerValue(v string) string {
	if validHeaderValue(v) && !strings.HasPrefix(v, "=?base64?") {
		return v
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(v)) + "?="
}

// --- Fehler ----------------------------------------------------------------------

type bridgeError struct {
	code int64
	msg  string
}

func (e *bridgeError) Error() string { return e.msg }

// ErrorCode liefert den JSON-RPC-Code eines Fehlers der Brücke oder des
// Servers (für remote test).
func ErrorCode(err error) (int64, bool) {
	var be *bridgeError
	if errors.As(err, &be) {
		return be.code, true
	}
	var we *jsonrpc.Error
	if errors.As(err, &we) {
		return we.Code, true
	}
	return 0, false
}

func (s *bridge) prefix() string {
	return "postkauz remote " + s.b.Name + ": "
}

func (s *bridge) addCmd() string {
	return "postkauz remote add " + s.b.Name + " --url " + s.b.URL + " --key-stdin"
}

// statusError übersetzt einen HTTP-Status in einen verständlichen Fehler.
// body: schon gelesener Inhalt bei JSON (sonst nil).
func (s *bridge) statusError(resp *http.Response, body []byte) error {
	p := s.prefix()
	switch c := resp.StatusCode; {
	case c == http.StatusUnauthorized:
		return &bridgeError{code: CodeUnauthorized, msg: p + "the server at " + s.host + " refused the API key (401: unknown or wrong key). " +
			"Store the right key on this PC with: " + s.addCmd()}
	case c == http.StatusForbidden:
		var d struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(body, &d)
		if d.Code != "" && validHeaderValue(d.Code) && len(d.Code) <= 40 {
			return &bridgeError{code: CodeForbidden, msg: p + "the API key was revoked or has expired (403 " + d.Code + "). " +
				"Create a new key on the office PC (key create) and store it on this PC with: " + s.addCmd()}
		}
		return &bridgeError{code: CodeForbidden, msg: p + "the server refused access with this key (403)"}
	case c == http.StatusNotFound:
		return &bridgeError{code: CodeBadResponse, msg: p + "no MCP endpoint at this URL (404); check url in [remote." + s.b.Name + "] (it usually ends in /mcp)"}
	case c == http.StatusMethodNotAllowed || c == http.StatusUnsupportedMediaType:
		return &bridgeError{code: CodeBadResponse, msg: p + "the URL does not answer like an MCP endpoint (" + strconv.Itoa(c) + "); check url in [remote." + s.b.Name + "]"}
	case c == http.StatusMisdirectedRequest:
		return &bridgeError{code: CodeBadResponse, msg: p + "the server does not accept the host name " + s.host + " (421); on the office PC add it to [http] allowed_hosts or resource"}
	case c == http.StatusTooManyRequests:
		wait := ""
		if ra, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && ra > 0 && ra < 3600 {
			wait = " in " + strconv.Itoa(ra) + " s"
		}
		return &bridgeError{code: CodeRateLimited, msg: p + "too many requests (429); try again" + wait}
	case c == http.StatusBadGateway || c == http.StatusServiceUnavailable || c == http.StatusGatewayTimeout:
		return &bridgeError{code: CodeUnavailable, msg: p + "the server is temporarily unavailable (" + strconv.Itoa(c) + "); is mcp --http running on the office PC? Try again shortly"}
	case c >= 300 && c < 400:
		return &bridgeError{code: CodeBadResponse, msg: p + "the server redirects (" + strconv.Itoa(c) + "); put the final https URL into [remote." + s.b.Name + "] (redirects are not followed, the key would go elsewhere)"}
	default:
		return &bridgeError{code: CodeBadResponse, msg: p + "unexpected answer from the server (HTTP " + strconv.Itoa(c) + ")"}
	}
}

// netError: Server nicht erreichbar, mit Grund ohne Einzelheiten der
// Antwort.
func (s *bridge) netError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &bridgeError{code: CodeUnreachable, msg: s.prefix() + "the server at " + s.host + " is not reachable (" + reasonOf(err) + "); " +
		"is the office PC on, Tailscale connected on both PCs and mcp --http running?"}
}

func (s *bridge) readError(err error) error {
	if errors.Is(err, errTooLarge) {
		return &bridgeError{code: CodeBadResponse, msg: s.prefix() + "the answer of the server is too large"}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var be *bridgeError
	if errors.As(err, &be) {
		return err
	}
	return &bridgeError{code: CodeUnreachable, msg: s.prefix() + "the connection to the server broke off (" + reasonOf(err) + "); try again"}
}

// reasonOf ordnet einen Netzfehler einer festen Beschreibung zu (keine
// Fehlertexte des Netzes, keine Adresse mit möglichem Inhalt).
func reasonOf(err error) string {
	var dns *net.DNSError
	var opErr *net.OpError
	msg := err.Error()
	switch {
	case errors.As(err, &dns):
		return "name not found"
	case strings.Contains(msg, "x509:") || strings.Contains(msg, "tls:") || strings.Contains(msg, "certificate"):
		return "TLS certificate not accepted"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused"):
		return "connection refused"
	case errors.As(err, &opErr):
		return "network error"
	case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
		return "connection closed"
	}
	return "connection failed"
}
