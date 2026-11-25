package peer

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"magpie-poker/internal/netx"
)

func testSecret(t *testing.T) string {
	t.Helper()
	raw := make([]byte, netx.KeySize)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	return netx.EncodeKey(raw)
}

type hostState struct {
	mu          sync.Mutex
	session     []byte
	joins       int
	badProof    bool
	eventsSigOK bool
	actions     []actionRecord
}

type actionRecord struct {
	body  string
	okSig bool
}

func (h *hostState) setSession(key []byte) {
	h.mu.Lock()
	h.session = key
	h.mu.Unlock()
}

func (h *hostState) getSession() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.session...)
}

type testHost struct {
	server   *httptest.Server
	identity netx.Identity
	secret   string
	tableID  string
	playerID string
	state    *hostState
}

func startHost(t *testing.T) *testHost {
	t.Helper()
	identity, err := netx.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	host := &testHost{
		identity: identity,
		secret:   testSecret(t),
		tableID:  "t_live1",
		playerID: "p_guest01",
		state:    &hostState{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/peer/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Pub   string `json:"pub"`
			Nonce string `json:"nonce"`
			Proof string `json:"proof"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "code": "bad_json", "error": "JSON 不合法"})
			return
		}
		host.state.mu.Lock()
		host.state.joins++
		host.state.mu.Unlock()
		expected := netx.JoinProof(host.secret, host.tableID, body.Pub, body.Nonce)
		if !netx.SafeEqual(expected, body.Proof) {
			host.state.mu.Lock()
			host.state.badProof = true
			host.state.mu.Unlock()
			writeJSON(w, 400, map[string]any{"ok": false, "code": "bad_proof", "error": "邀请校验失败"})
			return
		}
		session, err := netx.DeriveSession(host.identity.Priv, body.Pub, host.secret, host.tableID, host.playerID)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "code": "bad_pub", "error": "公钥不合法"})
			return
		}
		host.state.setSession(session)
		seat := 2
		writeJSON(w, 200, map[string]any{
			"ok":        true,
			"playerId":  host.playerID,
			"seat":      seat,
			"hostPub":   host.identity.Pub,
			"hostProof": netx.HostProof(session, host.playerID),
			"sessionId": "s_live",
		})
	})
	mux.HandleFunc("/peer/events", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ts, err := strconv.ParseInt(q.Get("ts"), 10, 64)
		session := host.state.getSession()
		okSig := err == nil && q.Get("playerId") == host.playerID &&
			netx.VerifyRequest(session, http.MethodGet, "/peer/events", ts, "", q.Get("sig"))
		host.state.mu.Lock()
		host.state.eventsSigOK = okSig
		host.state.mu.Unlock()
		if !okSig {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "签名不匹配"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, ": 心跳\n\n")
		// Malformed event must be ignored; the notice arrives before the state.
		io.WriteString(w, "event: state\ndata: {not json\n\n")
		io.WriteString(w, "event: notice\r\ndata: {\"level\":\"info\",\r\ndata: \"text\":\"你好\"}\r\n\r\n")
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(5 * time.Millisecond)
		event := "event: state\nid: 1\ndata: " +
			`{"mode":"guest","table":{"id":"t_live1","name":"周五牌局"}}` + "\n\n"
		// Split inside a multi-byte character.
		cut := strings.Index(event, "五") + 1
		io.WriteString(w, event[:cut])
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(5 * time.Millisecond)
		io.WriteString(w, event[cut:])
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("/peer/action", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		ts, err := strconv.ParseInt(r.Header.Get("x-poker-ts"), 10, 64)
		session := host.state.getSession()
		okSig := err == nil &&
			r.Header.Get("x-poker-player") == host.playerID &&
			netx.VerifyRequest(session, http.MethodPost, "/peer/action", ts, string(raw), r.Header.Get("x-poker-sig"))
		host.state.mu.Lock()
		host.state.actions = append(host.state.actions, actionRecord{body: string(raw), okSig: okSig})
		host.state.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "accepted": okSig})
	})
	mux.HandleFunc("/peer/chat", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		writeJSON(w, 400, map[string]any{"ok": false, "code": "rate_limited", "error": "太快了"})
	})
	host.server = httptest.NewServer(mux)
	t.Cleanup(host.server.Close)
	return host
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

func (h *testHost) invite(t *testing.T) netx.Invite {
	t.Helper()
	code, err := netx.CreateInvite(netx.Invite{
		TableID:   h.tableID,
		TableName: "周五牌局",
		Host:      "127.0.0.1",
		Port:      h.server.Listener.Addr().(*net.TCPAddr).Port,
		HostPub:   h.identity.Pub,
		Secret:    h.secret,
		HostName:  "ironwater",
	})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := netx.ParseInvite(code)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(4 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
		var zero T
		return zero
	}
}

func TestIntegrationJoinStateAndSignedRequests(t *testing.T) {
	host := startHost(t)
	invite := host.invite(t)

	states := make(chan json.RawMessage, 8)
	notices := make(chan [2]string, 8)
	var statusMu sync.Mutex
	var statuses []string
	client := New(invite, Profile{
		Name:       "Mio",
		Avatar:     "fox",
		GatewayURL: "https://mio.example.ts.net:8444",
		Tailscale:  true,
	}, func(state json.RawMessage) {
		states <- state
	}, func(level, text string) {
		notices <- [2]string{level, text}
	}, func(status string) {
		statusMu.Lock()
		statuses = append(statuses, status)
		statusMu.Unlock()
	})

	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	if client.Status != "online" {
		t.Fatalf("status = %q", client.Status)
	}
	if client.PlayerID != host.playerID {
		t.Fatalf("playerId = %q", client.PlayerID)
	}
	if client.Seat != 2 {
		t.Fatalf("seat = %d", client.Seat)
	}
	if client.SessionID != "s_live" {
		t.Fatalf("sessionId = %q", client.SessionID)
	}
	if want := "http://" + host.server.Listener.Addr().String(); client.BaseURL != want {
		t.Fatalf("baseURL = %q, want %q", client.BaseURL, want)
	}
	statusMu.Lock()
	gotStatuses := append([]string(nil), statuses...)
	statusMu.Unlock()
	if len(gotStatuses) != 2 || gotStatuses[0] != "connecting" || gotStatuses[1] != "online" {
		t.Fatalf("statuses = %v", gotStatuses)
	}
	host.state.mu.Lock()
	badProof := host.state.badProof
	joins := host.state.joins
	host.state.mu.Unlock()
	if badProof {
		t.Fatal("host saw a bad proof")
	}

	// Connect while online is a no-op.
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	host.state.mu.Lock()
	if host.state.joins != joins {
		t.Fatalf("reconnect performed extra join: %d", host.state.joins)
	}
	host.state.mu.Unlock()

	client.Subscribe()
	state := waitFor(t, states, "state event")
	var snapshot struct {
		Mode  string `json:"mode"`
		Table struct {
			Name string `json:"name"`
		} `json:"table"`
	}
	if err := json.Unmarshal(state, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != "guest" || snapshot.Table.Name != "周五牌局" {
		t.Fatalf("bad state: %s", state)
	}
	notice := waitFor(t, notices, "notice event")
	if notice != [2]string{"info", "你好"} {
		t.Fatalf("notice = %v", notice)
	}
	host.state.mu.Lock()
	eventsSigOK := host.state.eventsSigOK
	host.state.mu.Unlock()
	if !eventsSigOK {
		t.Fatal("host rejected the events signature")
	}

	reply, err := client.Post("/peer/action", struct {
		Type   string `json:"type"`
		Amount int    `json:"amount"`
	}{Type: "raise", Amount: 300})
	if err != nil {
		t.Fatal(err)
	}
	var actionReply struct {
		OK       bool `json:"ok"`
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(reply, &actionReply); err != nil {
		t.Fatal(err)
	}
	if !actionReply.OK || !actionReply.Accepted {
		t.Fatalf("action reply = %s", reply)
	}
	host.state.mu.Lock()
	actions := append([]actionRecord(nil), host.state.actions...)
	host.state.mu.Unlock()
	if len(actions) != 1 || !actions[0].okSig {
		t.Fatalf("host actions = %+v", actions)
	}
	if actions[0].body != `{"type":"raise","amount":300}` {
		t.Fatalf("host body = %q", actions[0].body)
	}

	_, err = client.Post("/peer/chat", map[string]string{"text": "hi"})
	var peerErr *Error
	if err == nil || !asError(err, &peerErr) || peerErr.Code != "rate_limited" || peerErr.Status != 400 {
		t.Fatalf("chat error = %#v", err)
	}

	client.Close()
	if client.Status != "closed" {
		t.Fatalf("status after close = %q", client.Status)
	}
	if _, err := client.Post("/peer/action", map[string]string{"type": "fold"}); err == nil {
		t.Fatal("post after close must fail")
	} else if peerErr, ok := err.(*Error); !ok || peerErr.Code != "closed" {
		t.Fatalf("post after close error = %#v", err)
	}
	client.Subscribe() // no-op after close
}

func asError(err error, target **Error) bool {
	if typed, ok := err.(*Error); ok {
		*target = typed
		return true
	}
	return false
}

func TestJoinRejectsWrongSecretAndSpoofedHost(t *testing.T) {
	host := startHost(t)
	fields := host.invite(t)
	fields.Secret = testSecret(t)
	// Change one byte so it is still a valid key but the wrong one.
	raw, err := netx.DecodeKey(fields.Secret, "secret")
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xff
	fields.Secret = netx.EncodeKey(raw)
	wrong := New(fields, Profile{Name: "Mio"}, nil, nil, nil)
	err = wrong.Connect()
	var peerErr *Error
	if err == nil || !asError(err, &peerErr) || peerErr.Code != "bad_proof" || peerErr.Status != 400 {
		t.Fatalf("wrong secret error = %#v", err)
	}
	if wrong.Status != "offline" || wrong.PlayerID != "" {
		t.Fatalf("wrong client state: status=%q player=%q", wrong.Status, wrong.PlayerID)
	}

	// A host that proves nothing must be rejected.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/peer/join" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Pub   string `json:"pub"`
			Nonce string `json:"nonce"`
			Proof string `json:"proof"`
		}
		json.Unmarshal(raw, &body)
		if netx.JoinProof(host.secret, host.tableID, body.Pub, body.Nonce) != body.Proof {
			writeJSON(w, 400, map[string]any{"ok": false, "code": "bad_proof", "error": "邀请校验失败"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":        true,
			"playerId":  "p_bad",
			"seat":      0,
			"hostPub":   host.identity.Pub,
			"hostProof": strings.Repeat("0", 64),
			"sessionId": "s_bad",
		})
	}))
	defer server.Close()
	good := host.invite(t)
	good.Port = portOf(t, server.URL)
	spoofed := New(good, Profile{Name: "Mio"}, nil, nil, nil)
	err = spoofed.Connect()
	if err == nil || !asError(err, &peerErr) || peerErr.Code != "bad_host_proof" {
		t.Fatalf("spoofed host error = %#v", err)
	}
	if spoofed.Status != "offline" || spoofed.PlayerID != "" {
		t.Fatalf("spoofed client state: status=%q player=%q", spoofed.Status, spoofed.PlayerID)
	}
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	idx := strings.LastIndex(rawURL, ":")
	if idx < 0 {
		t.Fatalf("no port in %q", rawURL)
	}
	port, err := strconv.Atoi(rawURL[idx+1:])
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestBackoffBounds(t *testing.T) {
	for attempt := range 12 {
		delay := backoffDelay(attempt)
		if delay < time.Second || delay > 15*time.Second {
			t.Fatalf("attempt %d delay = %v", attempt, delay)
		}
	}
	if backoffDelay(0) > 2*time.Second {
		t.Fatalf("first delay too long: %v", backoffDelay(0))
	}
}

func TestOriginOfIPv6AndTLS(t *testing.T) {
	cases := []struct {
		invite netx.Invite
		want   string
	}{
		{netx.Invite{Host: "100.76.46.108", Port: 3482}, "http://100.76.46.108:3482"},
		{netx.Invite{Host: "fd00::1", Port: 3483, TLS: true}, "https://[fd00::1]:3483"},
		{netx.Invite{Host: "[fd00::1]", Port: 1}, "http://[fd00::1]:1"},
	}
	for _, tc := range cases {
		got := originOf(tc.invite)
		if got != tc.want {
			t.Fatalf("originOf(%+v) = %q, want %q", tc.invite, got, tc.want)
		}
	}
}

func TestNewRejectsClosedPost(t *testing.T) {
	client := New(netx.Invite{}, Profile{}, nil, nil, nil)
	client.Close()
	if _, err := client.Post("/peer/ping", nil); err == nil {
		t.Fatal("post on closed client must fail")
	}
	if _, err := client.Post("/peer/ping", map[string]int{"a": 1}); err == nil {
		t.Fatal("post without session must fail")
	}
}
