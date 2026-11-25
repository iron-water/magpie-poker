// Package peer implements the guest side of the table peer protocol.
package peer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"magpie-poker/internal/netx"
)

const (
	eventsPath   = "/peer/events"
	maxSSEBuffer = 1 << 20
	joinTimeout  = 10 * time.Second
	postTimeout  = 15 * time.Second
	maxBackoff   = 15 * time.Second
	firstBackoff = time.Second
	joinBodyVer  = 1
)

// Profile is the guest's display/profile information.
type Profile struct {
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	GatewayURL string `json:"gatewayUrl"`
	Tailscale  bool   `json:"tailscale"`
}

// Error is a peer protocol or transport failure.
type Error struct {
	Message string
	Code    string
	Status  int
}

func (e *Error) Error() string { return e.Message }

func failure(data *apiMessage, status int, fallback string) *Error {
	message := fallback
	if data != nil {
		if data.Error != "" {
			message = data.Error
		} else if data.Text != "" {
			message = data.Text
		}
	}
	code := ""
	if data != nil {
		code = data.Code
	}
	return &Error{Message: message, Code: code, Status: status}
}

type apiMessage struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Text    string `json:"text"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type joinBody struct {
	V       int    `json:"v"`
	TableID string `json:"tableId"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Pub     string `json:"pub"`
	Nonce   string `json:"nonce"`
	Proof   string `json:"proof"`
	Profile struct {
		GatewayURL string `json:"gatewayUrl"`
		Tailscale  bool   `json:"tailscale"`
	} `json:"profile"`
}

type joinResponse struct {
	OK        bool    `json:"ok"`
	TableID   *string `json:"tableId"`
	PlayerID  string  `json:"playerId"`
	Seat      *int    `json:"seat"`
	HostPub   string  `json:"hostPub"`
	HostProof string  `json:"hostProof"`
	SessionID string  `json:"sessionId"`
	Error     string  `json:"error"`
	Text      string  `json:"text"`
	Code      string  `json:"code"`
}

// Client is a guest connected to one host table.
type Client struct {
	BaseURL  string
	PlayerID string
	Seat     int
	Status   string

	// SessionID is the opaque host session label returned by /peer/join.
	SessionID string

	invite   netx.Invite
	profile  Profile
	identity netx.Identity
	initErr  error

	onState  func(json.RawMessage)
	onNotice func(level, text string)
	onStatus func(string)

	http *http.Client

	mu      sync.Mutex
	session []byte
	closed  bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a client for invite. No network activity happens until Connect.
func New(invite netx.Invite, profile Profile, onState func(json.RawMessage), onNotice func(level, text string), onStatus func(string)) *Client {
	identity, err := netx.GenerateIdentity()
	c := &Client{
		Seat:     -1,
		Status:   "offline",
		invite:   invite,
		profile:  profile,
		identity: identity,
		initErr:  err,
		onState:  onState,
		onNotice: onNotice,
		onStatus: onStatus,
		http:     &http.Client{},
	}
	c.BaseURL = originOf(invite)
	return c
}

// Pub returns this client's X25519 public key.
func (c *Client) Pub() string { return c.identity.Pub }

func originOf(invite netx.Invite) string {
	host := strings.TrimSpace(invite.Host)
	if host == "" {
		return ""
	}
	shown := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		shown = "[" + host + "]"
	}
	scheme := "http"
	if invite.TLS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, shown, invite.Port)
}

// Connect performs the join handshake. It is idempotent while online.
func (c *Client) Connect() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return &Error{Message: "连接已关闭", Code: "closed"}
	}
	if c.initErr != nil {
		c.mu.Unlock()
		return &Error{Message: "无法生成身份", Code: "no_identity"}
	}
	if len(c.session) > 0 && c.PlayerID != "" {
		c.mu.Unlock()
		c.setStatus("online")
		return nil
	}
	invite := c.invite
	identity := c.identity
	profile := c.profile
	c.mu.Unlock()

	if c.BaseURL == "" {
		return &Error{Message: "邀请缺少主机地址", Code: "bad_invite"}
	}
	c.setStatus("connecting")

	n := netx.Nonce()
	body := joinBody{
		V:       joinBodyVer,
		TableID: invite.TableID,
		Name:    profile.Name,
		Avatar:  profile.Avatar,
		Pub:     identity.Pub,
		Nonce:   n,
		Proof:   netx.JoinProof(invite.Secret, invite.TableID, identity.Pub, n),
	}
	body.Profile.GatewayURL = profile.GatewayURL
	body.Profile.Tailscale = profile.Tailscale
	payload, err := json.Marshal(body)
	if err != nil {
		c.setStatus("offline")
		return &Error{Message: "入桌请求无法编码", Code: "bad_request"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), joinTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/peer/join", bytes.NewReader(payload))
	if err != nil {
		c.setStatus("offline")
		return &Error{Message: "无法连接牌桌主机", Code: "offline"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.setStatus("offline")
		return &Error{Message: "无法连接牌桌主机", Code: "offline"}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var joined joinResponse
	decodeErr := json.Unmarshal(data, &joined)
	if decodeErr != nil || !joined.OK {
		c.setStatus("offline")
		if decodeErr != nil {
			return failure(nil, resp.StatusCode, fmt.Sprintf("入桌失败（%d）", resp.StatusCode))
		}
		msg := &apiMessage{Error: joined.Error, Text: joined.Text, Code: joined.Code}
		return failure(msg, resp.StatusCode, fmt.Sprintf("入桌失败（%d）", resp.StatusCode))
	}
	playerID := strings.TrimSpace(joined.PlayerID)
	if playerID == "" {
		c.setStatus("offline")
		return &Error{Message: "主机没有返回玩家 id", Code: "bad_response", Status: resp.StatusCode}
	}
	if joined.TableID != nil && *joined.TableID != invite.TableID {
		c.setStatus("offline")
		return &Error{Message: "牌桌不匹配", Code: "wrong_table", Status: resp.StatusCode}
	}
	session, err := netx.DeriveSession(identity.Priv, joined.HostPub, invite.Secret, invite.TableID, playerID)
	if err != nil {
		c.setStatus("offline")
		return &Error{Message: "主机公钥无效", Code: "bad_host_key", Status: resp.StatusCode}
	}
	if !netx.SafeEqual(netx.HostProof(session, playerID), joined.HostProof) {
		c.setStatus("offline")
		return &Error{Message: "主机身份校验失败", Code: "bad_host_proof", Status: resp.StatusCode}
	}

	seat := -1
	if joined.Seat != nil {
		seat = *joined.Seat
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return &Error{Message: "连接已关闭", Code: "closed"}
	}
	c.session = session
	c.PlayerID = playerID
	c.Seat = seat
	c.SessionID = joined.SessionID
	c.mu.Unlock()
	c.setStatus("online")
	return nil
}

// Post sends a signed POST and returns the raw JSON response.
func (c *Client) Post(path string, body any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, &Error{Message: "连接已关闭", Code: "closed"}
	}
	session := append([]byte(nil), c.session...)
	playerID := c.PlayerID
	c.mu.Unlock()
	if len(session) == 0 {
		return nil, &Error{Message: "尚未入桌", Code: "offline"}
	}
	target := path
	if !strings.HasPrefix(target, "/") {
		target = "/" + target
	}
	var bodyString string
	if body == nil {
		bodyString = "{}"
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, &Error{Message: "请求无法编码", Code: "bad_request"}
		}
		bodyString = string(encoded)
	}
	if c.BaseURL == "" {
		return nil, &Error{Message: "邀请缺少主机地址", Code: "bad_invite"}
	}
	ts := time.Now().UnixMilli()
	sig := netx.SignRequest(session, http.MethodPost, target, ts, bodyString)

	ctx, cancel := context.WithTimeout(context.Background(), postTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+target, strings.NewReader(bodyString))
	if err != nil {
		return nil, &Error{Message: "网络连接失败", Code: "offline"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-poker-player", playerID)
	req.Header.Set("x-poker-ts", fmt.Sprint(ts))
	req.Header.Set("x-poker-sig", sig)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Message: "网络连接失败", Code: "offline"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var message apiMessage
	json.Unmarshal(raw, &message)
	if !message.OK {
		return nil, failure(&message, resp.StatusCode, fmt.Sprintf("请求失败（%d）", resp.StatusCode))
	}
	return json.RawMessage(raw), nil
}

// Subscribe starts the SSE loop. It reconnects on its own until Close.
func (c *Client) Subscribe() {
	c.mu.Lock()
	if c.closed || len(c.session) == 0 {
		c.mu.Unlock()
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.wg.Add(1)
	c.mu.Unlock()
	go c.runStream(ctx)
}

// Close stops the stream and invalidates the session.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.session = nil
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.setStatus("closed")
}

func (c *Client) setStatus(next string) {
	c.mu.Lock()
	if c.Status == next {
		c.mu.Unlock()
		return
	}
	if c.closed && next != "closed" {
		c.mu.Unlock()
		return
	}
	c.Status = next
	cb := c.onStatus
	c.mu.Unlock()
	emit(func() { cb(next) })
}

func emit(fn func()) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn()
}

func backoffDelay(attempt int) time.Duration {
	shift := attempt
	if shift > 4 {
		shift = 4
	}
	base := firstBackoff * time.Duration(1<<uint(shift))
	if base > maxBackoff {
		base = maxBackoff
	}
	jitter := 0.7 + rand.Float64()*0.6
	delay := time.Duration(float64(base) * jitter)
	if delay < firstBackoff {
		return firstBackoff
	}
	if delay > maxBackoff {
		return maxBackoff
	}
	return delay
}

func (c *Client) runStream(ctx context.Context) {
	defer c.wg.Done()
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		c.mu.Lock()
		session := append([]byte(nil), c.session...)
		playerID := c.PlayerID
		closed := c.closed
		c.mu.Unlock()
		if closed || len(session) == 0 {
			return
		}
		opened := c.streamOnce(ctx, session, playerID)
		if ctx.Err() != nil {
			return
		}
		if opened {
			attempt = 0
		}
		c.setStatus("offline")
		delay := backoffDelay(attempt)
		attempt++
		if !sleepCtx(ctx, delay) {
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// streamOnce opens one SSE connection; it returns true if the stream opened.
func (c *Client) streamOnce(ctx context.Context, session []byte, playerID string) bool {
	ts := time.Now().UnixMilli()
	sig := netx.SignRequest(session, http.MethodGet, eventsPath, ts, "")
	query := url.Values{}
	query.Set("playerId", playerID)
	query.Set("ts", fmt.Sprint(ts))
	query.Set("sig", sig)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+eventsPath+"?"+query.Encode(), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return false
	}
	c.setStatus("online")
	parser := sseParser{client: c}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSEBuffer)
	for scanner.Scan() {
		line := scanner.Text()
		if ctx.Err() != nil {
			parser.dispatch()
			return true
		}
		parser.line(line)
	}
	parser.dispatch()
	return true
}

type sseParser struct {
	client    *Client
	eventName string
	dataLines []string
}

func (p *sseParser) line(line string) {
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		p.dispatch()
		return
	}
	if strings.HasPrefix(line, ":") {
		return
	}
	field, value := line, ""
	if idx := strings.IndexByte(line, ':'); idx >= 0 {
		field = line[:idx]
		value = line[idx+1:]
	}
	value = strings.TrimPrefix(value, " ")
	switch field {
	case "event":
		p.eventName = value
	case "data":
		p.dataLines = append(p.dataLines, value)
	}
}

func (p *sseParser) dispatch() {
	name := p.eventName
	if name == "" {
		name = "message"
	}
	raw := strings.Join(p.dataLines, "\n")
	p.eventName = ""
	p.dataLines = nil
	if strings.TrimSpace(raw) == "" {
		return
	}
	if !json.Valid([]byte(raw)) {
		return
	}
	c := p.client
	switch name {
	case "state":
		if !isJSONObject(raw) {
			return
		}
		cb := c.onState
		payload := json.RawMessage(append([]byte(nil), raw...))
		emit(func() { cb(payload) })
	case "notice":
		var notice struct {
			Level string `json:"level"`
			Text  string `json:"text"`
		}
		if json.Unmarshal([]byte(raw), &notice) != nil {
			return
		}
		if notice.Level == "" {
			notice.Level = "info"
		}
		cb := c.onNotice
		emit(func() { cb(notice.Level, notice.Text) })
	case "error":
		var payload struct {
			Text    string `json:"text"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(raw), &payload) != nil {
			return
		}
		text := payload.Text
		if text == "" {
			text = payload.Error
		}
		if text == "" {
			text = payload.Message
		}
		if text == "" {
			text = "牌桌错误"
		}
		cb := c.onNotice
		emit(func() { cb("error", text) })
	}
}

func isJSONObject(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}
