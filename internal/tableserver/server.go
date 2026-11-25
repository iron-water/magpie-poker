// Package tableserver exposes a game.Table over the /peer/* protocol.
package tableserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"magpie-poker/internal/game"
	"magpie-poker/internal/netx"
)

const (
	maxBody      = 64 * 1024
	tsWindowMS   = 120_000
	tickInterval = 500 * time.Millisecond
	heartbeat    = 15 * time.Second
	seenLimit    = 4000
	joinLimit    = 20
	joinWindow   = time.Minute
)

// disconnectGrace is how long a player may stay away before being marked
// disconnected. A variable so tests can shorten it.
var disconnectGrace = 8 * time.Second

// Server serves one game.Table to guests over HTTP.
type Server struct {
	table    *game.Table
	identity netx.Identity
	secret   string
	port     int
	onChange func()
	logf     func(string, ...any)

	mu           sync.Mutex
	sessions     map[string]*session
	seen         map[string]struct{}
	seenQueue    []string
	joinAttempts map[string]*rateAttempt
	closeTimers  map[string]*time.Timer
	closed       bool
	listener     net.Listener
	httpServer   *http.Server
	boundPort    int

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type session struct {
	key      []byte
	pub      string
	streams  map[*sseStream]struct{}
	joinedAt time.Time
}

type rateAttempt struct {
	count int
	reset time.Time
}

type sseStream struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	closed  bool
	done    chan struct{}
	once    sync.Once
}

func newSSEStream(w http.ResponseWriter) *sseStream {
	flusher, _ := w.(http.Flusher)
	return &sseStream{w: w, flusher: flusher, done: make(chan struct{})}
}

func (st *sseStream) write(payload []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return errors.New("stream closed")
	}
	if _, err := st.w.Write(payload); err != nil {
		return err
	}
	if st.flusher != nil {
		st.flusher.Flush()
	}
	return nil
}

func (st *sseStream) stop() {
	st.once.Do(func() {
		st.mu.Lock()
		st.closed = true
		st.mu.Unlock()
		close(st.done)
	})
}

// New wires a table server. port 0 asks Listen for an ephemeral port;
// a negative port falls back to 3482.
func New(table *game.Table, identity netx.Identity, secret string, port int, onChange func(), logf func(string, ...any)) *Server {
	if port < 0 {
		port = 3482
	}
	if onChange == nil {
		onChange = func() {}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{
		table:        table,
		identity:     identity,
		secret:       secret,
		port:         port,
		onChange:     onChange,
		logf:         logf,
		sessions:     make(map[string]*session),
		seen:         make(map[string]struct{}),
		joinAttempts: make(map[string]*rateAttempt),
		closeTimers:  make(map[string]*time.Timer),
		stop:         make(chan struct{}),
	}
}

// Listen starts the HTTP server and the host-authoritative ticker.
func (s *Server) Listen() (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, errors.New("牌桌服务已关闭")
	}
	if s.listener != nil {
		port := s.boundPort
		s.mu.Unlock()
		return port, nil
	}
	var (
		listener net.Listener
		lastErr  error
	)
	for offset := range 10 {
		addr := fmt.Sprintf("0.0.0.0:%d", s.port+offset)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			listener = ln
			break
		}
		lastErr = err
	}
	if listener == nil {
		s.mu.Unlock()
		if lastErr == nil {
			lastErr = errors.New("no address")
		}
		return 0, fmt.Errorf("牌桌端口 %d–%d 都被占用: %w", s.port, s.port+9, lastErr)
	}
	s.listener = listener
	s.boundPort = listener.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	s.httpServer = srv
	port := s.boundPort
	s.wg.Add(2)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("listen: %v", err)
		}
	}()
	go s.runTicker()
	return port, nil
}

func (s *Server) runTicker() {
	defer s.wg.Done()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			changed := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.logf("tick: %v", r)
					}
				}()
				changed = s.table.Tick(time.Now())
			}()
			if changed {
				s.markChanged()
				s.Broadcast()
			}
		}
	}
}

// Close stops the ticker, drops all streams and shuts the listener down.
func (s *Server) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	s.mu.Lock()
	s.closed = true
	srv := s.httpServer
	var streams []*sseStream
	for _, sess := range s.sessions {
		for stream := range sess.streams {
			streams = append(streams, stream)
		}
	}
	s.sessions = make(map[string]*session)
	for id, timer := range s.closeTimers {
		timer.Stop()
		delete(s.closeTimers, id)
	}
	s.mu.Unlock()
	for _, stream := range streams {
		stream.stop()
	}
	var err error
	if srv != nil {
		err = srv.Close()
	}
	s.wg.Wait()
	return err
}

// markChanged calls onChange without letting a panic escape.
func (s *Server) markChanged() {
	defer func() {
		if r := recover(); r != nil {
			s.logf("onChange: %v", r)
		}
	}()
	s.onChange()
}

// Broadcast sends each player their own trimmed view.
func (s *Server) Broadcast() {
	type target struct {
		playerID string
		streams  []*sseStream
	}
	s.mu.Lock()
	targets := make([]target, 0, len(s.sessions))
	for playerID, sess := range s.sessions {
		if len(sess.streams) == 0 {
			continue
		}
		streams := make([]*sseStream, 0, len(sess.streams))
		for stream := range sess.streams {
			streams = append(streams, stream)
		}
		targets = append(targets, target{playerID: playerID, streams: streams})
	}
	s.mu.Unlock()

	now := time.Now()
	for _, tgt := range targets {
		view, err := json.Marshal(s.table.ViewFor(tgt.playerID, now))
		if err != nil {
			s.logf("broadcast: %v", err)
			continue
		}
		frame := stateFrame(view)
		for _, stream := range tgt.streams {
			stream.write(frame)
		}
	}
}

func stateFrame(view []byte) []byte {
	frame := make([]byte, 0, len(view)+32)
	frame = append(frame, "event: state\ndata: "...)
	frame = append(frame, view...)
	frame = append(frame, '\n', '\n')
	return frame
}

// ServeHTTP routes /peer/* requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/peer/health" || path == "/peer/peek":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "", "method not allowed")
			return
		}
		s.handleHealth(w)
	case path == "/peer/join" && r.Method == http.MethodPost:
		s.handleJoin(w, r)
	case path == "/peer/events" && r.Method == http.MethodGet:
		s.handleEvents(w, r)
	case strings.HasPrefix(path, "/peer/") && r.Method == http.MethodPost:
		s.handleSigned(w, r, path)
	default:
		writeError(w, http.StatusNotFound, "", "not found")
	}
}

func (s *Server) handleHealth(w http.ResponseWriter) {
	view := s.table.ViewFor("", time.Now())
	writeJSON(w, http.StatusOK, struct {
		OK        bool   `json:"ok"`
		TableID   string `json:"tableId"`
		TableName string `json:"tableName"`
		Phase     string `json:"phase"`
		Players   int    `json:"players"`
		MaxSeats  int    `json:"maxSeats"`
		Version   int    `json:"version"`
	}{
		OK:        true,
		TableID:   s.table.ID(),
		TableName: s.table.Name(),
		Phase:     s.table.Phase(),
		Players:   len(view.Seats),
		MaxSeats:  s.table.MaxSeats(),
		Version:   1,
	})
}

type joinRequest struct {
	V       int    `json:"v"`
	TableID string `json:"tableId"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Pub     string `json:"pub"`
	Nonce   string `json:"nonce"`
	Proof   string `json:"proof"`
	Profile struct {
		GatewayURL string `json:"gatewayUrl"`
		Tailscale  any    `json:"tailscale"`
	} `json:"profile"`
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	ip := remoteIP(r)
	now := time.Now()
	s.mu.Lock()
	attempt := s.joinAttempts[ip]
	if attempt == nil || now.After(attempt.reset) {
		attempt = &rateAttempt{reset: now.Add(joinWindow)}
		s.joinAttempts[ip] = attempt
	}
	attempt.count++
	tooMany := attempt.count > joinLimit
	s.mu.Unlock()
	if tooMany {
		writeError(w, http.StatusTooManyRequests, "", "尝试太频繁")
		return
	}

	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "请求体太大")
		return
	}
	if strings.TrimSpace(body) == "" {
		body = "{}"
	}
	var req joinRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "JSON 不合法")
		return
	}
	if req.V != 1 {
		writeError(w, http.StatusBadRequest, "bad_version", "协议版本不匹配")
		return
	}
	if req.TableID != s.table.ID() {
		writeError(w, http.StatusNotFound, "wrong_table", "没有这张牌桌")
		return
	}
	if s.table.Phase() == "ended" {
		writeError(w, http.StatusConflict, "ended", "牌局已经结束")
		return
	}
	name := truncateRunes(strings.TrimSpace(req.Name), 16)
	if name == "" {
		writeError(w, http.StatusBadRequest, "no_name", "缺少名字")
		return
	}
	if req.Pub == "" || req.Nonce == "" || req.Proof == "" {
		writeError(w, http.StatusBadRequest, "bad_join", "入桌信息不完整")
		return
	}
	if _, err := netx.PublicKey(req.Pub); err != nil {
		writeError(w, http.StatusBadRequest, "bad_pub", "公钥不合法")
		return
	}
	expected := netx.JoinProof(s.secret, s.table.ID(), req.Pub, req.Nonce)
	if expected == "" {
		writeError(w, http.StatusBadRequest, "bad_pub", "公钥不合法")
		return
	}
	if !netx.SafeEqual(expected, req.Proof) {
		writeError(w, http.StatusForbidden, "bad_proof", "邀请码不匹配")
		return
	}

	id := newPlayerID()
	for {
		if _, ok := s.table.Player(id); !ok {
			break
		}
		id = newPlayerID()
	}
	avatar := truncateRunes(req.Avatar, 16)
	if avatar == "" {
		avatar = "fox"
	}
	gateway := truncateRunes(req.Profile.GatewayURL, 300)
	player, err := s.table.AddPlayer(game.PlayerInput{
		ID:         id,
		Name:       name,
		Avatar:     avatar,
		GatewayURL: gateway,
		Tailscale:  req.Profile.Tailscale,
	})
	if err != nil {
		message, code := gameError(err)
		if code == "" {
			code = "join_failed"
		}
		writeError(w, http.StatusConflict, code, message)
		return
	}
	if player.ID != id {
		s.dropSession(player.ID)
		id = player.ID
	}
	sessionKey, err := netx.DeriveSession(s.identity.Priv, req.Pub, s.secret, s.table.ID(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_pub", "公钥不合法")
		return
	}
	s.mu.Lock()
	oldStreams := s.dropSessionLocked(id)
	s.sessions[id] = &session{
		key:      sessionKey,
		pub:      req.Pub,
		streams:  make(map[*sseStream]struct{}),
		joinedAt: time.Now(),
	}
	s.mu.Unlock()
	for _, stream := range oldStreams {
		stream.stop()
	}
	s.markChanged()
	s.Broadcast()
	s.logf("join %s seat=%d", id, player.Seat)

	writeJSON(w, http.StatusOK, struct {
		OK        bool   `json:"ok"`
		TableID   string `json:"tableId"`
		PlayerID  string `json:"playerId"`
		Seat      int    `json:"seat"`
		HostPub   string `json:"hostPub"`
		HostProof string `json:"hostProof"`
		SessionID string `json:"sessionId"`
	}{
		OK:        true,
		TableID:   s.table.ID(),
		PlayerID:  id,
		Seat:      player.Seat,
		HostPub:   s.identity.Pub,
		HostProof: netx.HostProof(sessionKey, id),
		SessionID: randomHex(6),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	playerID := query.Get("playerId")
	ts, err := strconv.ParseInt(strings.TrimSpace(query.Get("ts")), 10, 64)
	sig := query.Get("sig")

	s.mu.Lock()
	sess := s.sessions[playerID]
	s.mu.Unlock()
	if sess == nil {
		writeError(w, http.StatusForbidden, "no_session", "入桌已失效")
		return
	}
	if err != nil || absDuration(time.Now().UnixMilli()-ts) > tsWindowMS {
		writeError(w, http.StatusForbidden, "expired", "请求已过期")
		return
	}
	if !netx.VerifyRequest(sess.key, http.MethodGet, "/peer/events", ts, "", sig) {
		writeError(w, http.StatusForbidden, "bad_sig", "签名不匹配")
		return
	}
	player, ok := s.table.Player(playerID)
	if !ok || player.Left {
		writeError(w, http.StatusForbidden, "left", "不在牌桌上")
		return
	}
	wasOffline := !player.Connected
	s.table.MarkConnected(playerID)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	stream := newSSEStream(w)
	if err := stream.write([]byte("retry: 1500\n\n")); err != nil {
		return
	}

	s.mu.Lock()
	sess = s.sessions[playerID]
	if sess == nil {
		s.mu.Unlock()
		return
	}
	if timer := s.closeTimers[playerID]; timer != nil {
		timer.Stop()
		delete(s.closeTimers, playerID)
	}
	sess.streams[stream] = struct{}{}
	s.mu.Unlock()

	if view, err := json.Marshal(s.table.ViewFor(playerID, time.Now())); err == nil {
		stream.write(stateFrame(view))
	}
	if wasOffline {
		s.markChanged()
		s.Broadcast()
	}

	beat := time.NewTicker(heartbeat)
	defer beat.Stop()
	defer s.cleanupStream(playerID, sess, stream)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-stream.done:
			return
		case <-s.stop:
			return
		case <-beat.C:
			if err := stream.write([]byte(": ping\n\n")); err != nil {
				return
			}
		}
	}
}

func (s *Server) cleanupStream(playerID string, sess *session, stream *sseStream) {
	stream.stop()
	s.mu.Lock()
	current := s.sessions[playerID]
	if current == sess {
		delete(current.streams, stream)
		if len(current.streams) == 0 && !s.closed {
			if timer := s.closeTimers[playerID]; timer != nil {
				timer.Stop()
			}
			s.closeTimers[playerID] = time.AfterFunc(disconnectGrace, func() {
				s.disconnect(playerID, sess)
			})
		}
	}
	s.mu.Unlock()
}

func (s *Server) disconnect(playerID string, sess *session) {
	s.mu.Lock()
	delete(s.closeTimers, playerID)
	active := !s.closed && s.sessions[playerID] == sess && len(sess.streams) == 0
	s.mu.Unlock()
	if !active {
		return
	}
	s.table.MarkDisconnected(playerID)
	s.markChanged()
	s.Broadcast()
}

func (s *Server) handleSigned(w http.ResponseWriter, r *http.Request, path string) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "请求体太大")
		return
	}
	playerID, authErr := s.authenticate(r, path, body)
	if authErr != nil {
		writeError(w, authErr.status, authErr.code, authErr.message)
		return
	}

	jsonBody := strings.TrimSpace(body)
	if jsonBody == "" {
		jsonBody = "{}"
	}
	switch path {
	case "/peer/ping":
		writeJSON(w, http.StatusOK, struct {
			OK bool  `json:"ok"`
			T  int64 `json:"t"`
		}{OK: true, T: time.Now().UnixMilli()})
		return
	case "/peer/action":
		var payload struct {
			Type   string `json:"type"`
			Amount int    `json:"amount"`
		}
		if err := json.Unmarshal([]byte(jsonBody), &payload); err != nil {
			writeError(w, http.StatusBadRequest, "bad_json", "JSON 不合法")
			return
		}
		if err := s.table.Act(playerID, game.Action{Type: payload.Type, Amount: payload.Amount}); err != nil {
			s.writeGameError(w, err)
			return
		}
	case "/peer/chat":
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(jsonBody), &payload); err != nil {
			writeError(w, http.StatusBadRequest, "bad_json", "JSON 不合法")
			return
		}
		if err := s.table.Chat(playerID, payload.Text); err != nil {
			s.writeGameError(w, err)
			return
		}
	case "/peer/rebuy":
		var payload struct {
			Amount int `json:"amount"`
		}
		if err := json.Unmarshal([]byte(jsonBody), &payload); err != nil {
			writeError(w, http.StatusBadRequest, "bad_json", "JSON 不合法")
			return
		}
		if err := s.table.Rebuy(playerID, payload.Amount); err != nil {
			s.writeGameError(w, err)
			return
		}
	case "/peer/settle":
		var payload struct {
			TransferID string `json:"transferId"`
			Key        string `json:"key"`
			Expires    any    `json:"expires"`
			GatewayURL string `json:"gatewayUrl"`
		}
		if err := json.Unmarshal([]byte(jsonBody), &payload); err != nil {
			writeError(w, http.StatusBadRequest, "bad_json", "JSON 不合法")
			return
		}
		expires := asText(payload.Expires)
		if err := s.table.MarkIssued(payload.TransferID, playerID, payload.Key, expires, payload.GatewayURL); err != nil {
			s.writeGameError(w, err)
			return
		}
	case "/peer/verify":
		var payload struct {
			TransferID string `json:"transferId"`
		}
		if err := json.Unmarshal([]byte(jsonBody), &payload); err != nil {
			writeError(w, http.StatusBadRequest, "bad_json", "JSON 不合法")
			return
		}
		if err := s.table.MarkVerified(payload.TransferID, playerID); err != nil {
			s.writeGameError(w, err)
			return
		}
	case "/peer/leave":
		s.table.Leave(playerID)
	default:
		writeError(w, http.StatusNotFound, "", "not found")
		return
	}

	s.markChanged()
	s.Broadcast()
	if path == "/peer/leave" {
		s.dropSession(playerID)
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{OK: true})
}

type authError struct {
	status  int
	code    string
	message string
}

func (s *Server) authenticate(r *http.Request, path, body string) (string, *authError) {
	playerID := r.Header.Get("x-poker-player")
	ts, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("x-poker-ts")), 10, 64)
	sig := r.Header.Get("x-poker-sig")

	s.mu.Lock()
	sess := s.sessions[playerID]
	s.mu.Unlock()
	if sess == nil {
		return "", &authError{http.StatusForbidden, "no_session", "入桌已失效"}
	}
	if err != nil || absDuration(time.Now().UnixMilli()-ts) > tsWindowMS {
		return "", &authError{http.StatusForbidden, "expired", "请求已过期"}
	}
	if !netx.VerifyRequest(sess.key, r.Method, path, ts, body, sig) {
		return "", &authError{http.StatusForbidden, "bad_sig", "签名不匹配"}
	}
	s.mu.Lock()
	if _, seen := s.seen[sig]; seen {
		s.mu.Unlock()
		return "", &authError{http.StatusConflict, "replay", "重复请求"}
	}
	s.seen[sig] = struct{}{}
	s.seenQueue = append(s.seenQueue, sig)
	if len(s.seenQueue) > seenLimit {
		delete(s.seen, s.seenQueue[0])
		s.seenQueue = s.seenQueue[1:]
	}
	s.mu.Unlock()
	return playerID, nil
}

func (s *Server) writeGameError(w http.ResponseWriter, err error) {
	message, code := gameError(err)
	writeError(w, http.StatusBadRequest, code, message)
}

func gameError(err error) (string, string) {
	var poker *game.PokerError
	if errors.As(err, &poker) {
		return poker.Message, poker.Code
	}
	return err.Error(), ""
}

func (s *Server) dropSession(playerID string) {
	s.mu.Lock()
	streams := s.dropSessionLocked(playerID)
	s.mu.Unlock()
	for _, stream := range streams {
		stream.stop()
	}
}

// dropSessionLocked removes the session and returns its streams.
func (s *Server) dropSessionLocked(playerID string) []*sseStream {
	sess := s.sessions[playerID]
	if sess == nil {
		return nil
	}
	delete(s.sessions, playerID)
	if timer := s.closeTimers[playerID]; timer != nil {
		timer.Stop()
		delete(s.closeTimers, playerID)
	}
	streams := make([]*sseStream, 0, len(sess.streams))
	for stream := range sess.streams {
		streams = append(streams, stream)
	}
	return streams
}

func readBody(r *http.Request) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxBody {
		return "", errors.New("request body too large")
	}
	return string(raw), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"ok":false,"error":"encoding failed"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Code  string `json:"code,omitempty"`
	}{OK: false, Error: message, Code: code})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func newPlayerID() string {
	return "p_" + randomHex(4)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("tableserver: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

func truncateRunes(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}

func asText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

func absDuration(ms int64) int64 {
	if ms < 0 {
		return -ms
	}
	return ms
}
