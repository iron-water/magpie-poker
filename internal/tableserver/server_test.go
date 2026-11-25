package tableserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"magpie-poker/internal/game"
	"magpie-poker/internal/netx"
	"magpie-poker/internal/peer"
)

type testTableServer struct {
	server   *Server
	table    *game.Table
	identity netx.Identity
	secret   string
	port     int
	changes  *atomic.Int32
}

func testSecret() string {
	raw := make([]byte, netx.KeySize)
	for i := range raw {
		raw[i] = byte(0x40 + i)
	}
	return netx.EncodeKey(raw)
}

func startTestServer(t *testing.T) *testTableServer {
	t.Helper()
	identity, err := netx.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	table := game.NewTable("t_1", "周五牌局", "p_host", game.Config{
		SmallBlind:    25,
		BigBlind:      50,
		StartingChips: 10000,
		MaxSeats:      4,
		ActionSeconds: 30,
	})
	changes := &atomic.Int32{}
	server := New(table, identity, testSecret(), 0, func() {
		changes.Add(1)
	}, t.Logf)
	port, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return &testTableServer{
		server:   server,
		table:    table,
		identity: identity,
		secret:   testSecret(),
		port:     port,
		changes:  changes,
	}
}

func (h *testTableServer) baseURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(h.port)
}

func (h *testTableServer) invite(t *testing.T) netx.Invite {
	t.Helper()
	code, err := netx.CreateInvite(netx.Invite{
		TableID:   h.table.ID(),
		TableName: h.table.Name(),
		Host:      "127.0.0.1",
		Port:      h.port,
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

func waitChan[T any](t *testing.T, ch <-chan T, what string) T {
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

func getJSON(t *testing.T, target string) map[string]any {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func (h *testTableServer) sessionKey(t *testing.T, invite netx.Invite, playerID string, guest *peer.Client) []byte {
	t.Helper()
	key, err := netx.DeriveSession(h.identity.Priv, guest.Pub(), invite.Secret, invite.TableID, playerID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func signedPing(t *testing.T, base string, key []byte, playerID string, ts int64, body string) *http.Response {
	t.Helper()
	sig := netx.SignRequest(key, http.MethodPost, "/peer/ping", ts, body)
	req, err := http.NewRequest(http.MethodPost, base+"/peer/ping", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-poker-player", playerID)
	req.Header.Set("x-poker-ts", strconv.FormatInt(ts, 10))
	req.Header.Set("x-poker-sig", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("bad JSON %q: %v", raw, err)
		}
	}
	return body
}

func TestJoinServesStateAndSignedRoutes(t *testing.T) {
	host := startTestServer(t)
	invite := host.invite(t)

	states := make(chan json.RawMessage, 16)
	client := peer.New(invite, peer.Profile{Name: "Mio", Avatar: "fox"}, func(state json.RawMessage) {
		states <- state
	}, func(level, text string) {}, nil)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if client.PlayerID == "" || client.Seat < 0 {
		t.Fatalf("player=%q seat=%d", client.PlayerID, client.Seat)
	}
	player, ok := host.table.Player(client.PlayerID)
	if !ok || !player.Connected || player.Name != "Mio" {
		t.Fatalf("table player = %+v, ok=%v", player, ok)
	}
	if host.changes.Load() == 0 {
		t.Fatal("onChange was not called")
	}

	health := getJSON(t, host.baseURL()+"/peer/health")
	if health["tableId"] != "t_1" || health["tableName"] != "周五牌局" || health["phase"] != "lobby" {
		t.Fatalf("health = %v", health)
	}
	if health["players"] != float64(1) || health["maxSeats"] != float64(4) || health["version"] != float64(1) {
		t.Fatalf("health counts = %v", health)
	}

	client.Subscribe()
	raw := waitChan(t, states, "state event")
	var view struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Phase string `json:"phase"`
		Seats []struct {
			ID string `json:"id"`
		} `json:"seats"`
		Me *struct {
			ID string `json:"id"`
		} `json:"me"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.ID != "t_1" || view.Name != "周五牌局" || view.Phase != "lobby" {
		t.Fatalf("view = %+v", view)
	}
	if view.Me == nil || view.Me.ID != client.PlayerID {
		t.Fatalf("me = %+v", view.Me)
	}
	if len(view.Seats) != 1 || view.Seats[0].ID != client.PlayerID {
		t.Fatalf("seats = %+v", view.Seats)
	}

	if reply, err := client.Post("/peer/chat", map[string]string{"text": "hi"}); err != nil {
		t.Fatalf("chat: %v (%s)", err, reply)
	}
	logged := false
	for _, entry := range host.table.ViewFor(client.PlayerID, time.Now()).Log {
		if strings.Contains(entry.Text, "hi") {
			logged = true
		}
	}
	if !logged {
		t.Fatal("chat was not recorded in the table log")
	}

	before := host.table.ViewFor(client.PlayerID, time.Now()).Seats[0].Chips
	if _, err := client.Post("/peer/rebuy", map[string]int{"amount": 500}); err != nil {
		t.Fatal(err)
	}
	after := host.table.ViewFor(client.PlayerID, time.Now()).Seats[0].Chips
	if after != before+500 {
		t.Fatalf("chips %d -> %d", before, after)
	}

	_, err := client.Post("/peer/action", map[string]any{"type": "fold"})
	peerErr, ok := err.(*peer.Error)
	if !ok || peerErr.Code != "no_hand" || peerErr.Status != 400 {
		t.Fatalf("action error = %#v", err)
	}

	key := host.sessionKey(t, invite, client.PlayerID, client)
	ts := time.Now().UnixMilli()

	resp := signedPing(t, host.baseURL(), key, client.PlayerID, ts, "")
	if body := decodeBody(t, resp); resp.StatusCode != 200 || body["ok"] != true {
		t.Fatalf("ping status=%d body=%v", resp.StatusCode, body)
	}
	// Same signature again is a replay.
	resp = signedPing(t, host.baseURL(), key, client.PlayerID, ts, "")
	body := decodeBody(t, resp)
	if resp.StatusCode != http.StatusConflict || body["code"] != "replay" {
		t.Fatalf("replay status=%d body=%v", resp.StatusCode, body)
	}
	// Stale timestamp.
	resp = signedPing(t, host.baseURL(), key, client.PlayerID, time.Now().Add(-5*time.Minute).UnixMilli(), "")
	body = decodeBody(t, resp)
	if resp.StatusCode != http.StatusForbidden || body["code"] != "expired" {
		t.Fatalf("expired status=%d body=%v", resp.StatusCode, body)
	}
	// Tampered body.
	tamperedTS := time.Now().UnixMilli()
	tampered := netx.SignRequest(key, http.MethodPost, "/peer/ping", tamperedTS, `{"a":1}`)
	req, _ := http.NewRequest(http.MethodPost, host.baseURL()+"/peer/ping", strings.NewReader(`{"a":2}`))
	req.Header.Set("x-poker-player", client.PlayerID)
	req.Header.Set("x-poker-ts", strconv.FormatInt(tamperedTS, 10))
	req.Header.Set("x-poker-sig", tampered)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body = decodeBody(t, resp)
	if resp.StatusCode != http.StatusForbidden || body["code"] != "bad_sig" {
		t.Fatalf("tampered status=%d body=%v", resp.StatusCode, body)
	}

	if _, err := client.Post("/peer/leave", nil); err != nil {
		t.Fatal(err)
	}
	if player, ok := host.table.Player(client.PlayerID); ok && !player.Left {
		t.Fatalf("player after leave = %+v", player)
	}
	resp = signedPing(t, host.baseURL(), key, client.PlayerID, time.Now().UnixMilli(), "")
	body = decodeBody(t, resp)
	if resp.StatusCode != http.StatusForbidden || body["code"] != "no_session" {
		t.Fatalf("post-leave status=%d body=%v", resp.StatusCode, body)
	}
}

func TestJoinRejections(t *testing.T) {
	host := startTestServer(t)
	identity, err := netx.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	payload := func(mutate func(*joinRequest)) []byte {
		req := joinRequest{V: 1, TableID: "t_1", Name: "Mio", Avatar: "fox", Pub: identity.Pub, Nonce: netx.Nonce()}
		req.Proof = netx.JoinProof(host.secret, "t_1", identity.Pub, req.Nonce)
		if mutate != nil {
			mutate(&req)
		}
		raw, _ := json.Marshal(req)
		return raw
	}
	post := func(body []byte) (int, map[string]any) {
		resp, err := http.Post(host.baseURL()+"/peer/join", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, decodeBody(t, resp)
	}

	status, body := post(payload(func(r *joinRequest) { r.V = 2 }))
	if status != 400 || body["code"] != "bad_version" {
		t.Fatalf("version: %d %v", status, body)
	}
	status, body = post(payload(func(r *joinRequest) { r.TableID = "t_other" }))
	if status != 404 || body["code"] != "wrong_table" {
		t.Fatalf("table: %d %v", status, body)
	}
	status, body = post(payload(func(r *joinRequest) { r.Proof = strings.Repeat("0", 64) }))
	if status != 403 || body["code"] != "bad_proof" {
		t.Fatalf("proof: %d %v", status, body)
	}
	status, body = post(payload(func(r *joinRequest) { r.Name = "   " }))
	if status != 400 || body["code"] != "no_name" {
		t.Fatalf("name: %d %v", status, body)
	}
	status, body = post(payload(func(r *joinRequest) { r.Nonce = "" }))
	if status != 400 || body["code"] != "bad_join" {
		t.Fatalf("nonce: %d %v", status, body)
	}
	status, body = post(payload(func(r *joinRequest) { r.Pub = "not a key"; r.Nonce = "ab" }))
	if status != 400 || body["code"] != "bad_pub" {
		t.Fatalf("pub: %d %v", status, body)
	}
	status, body = post([]byte("{"))
	if status != 400 || body["code"] != "bad_json" {
		t.Fatalf("json: %d %v", status, body)
	}

	resp, err := http.Get(host.baseURL() + "/peer/events")
	if err != nil {
		t.Fatal(err)
	}
	body = decodeBody(t, resp)
	if resp.StatusCode != 403 || body["code"] != "no_session" {
		t.Fatalf("events: %d %v", resp.StatusCode, body)
	}
	if health := getJSON(t, host.baseURL()+"/peer/health"); health["players"] != float64(0) {
		t.Fatalf("failed joins added players: %v", health)
	}
}

func openSSE(t *testing.T, base string, key []byte, playerID string) (context.CancelFunc, *http.Response) {
	t.Helper()
	ts := time.Now().UnixMilli()
	sig := netx.SignRequest(key, http.MethodGet, "/peer/events", ts, "")
	query := url.Values{}
	query.Set("playerId", playerID)
	query.Set("ts", strconv.FormatInt(ts, 10))
	query.Set("sig", sig)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/peer/events?"+query.Encode(), nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		cancel()
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("events status=%d body=%s", resp.StatusCode, body)
	}
	return cancel, resp
}

func waitPlayerConnected(t *testing.T, table *game.Table, playerID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if player, ok := table.Player(playerID); ok && player.Connected == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	player, _ := table.Player(playerID)
	t.Fatalf("player %s connected=%v, want %v (%+v)", playerID, player.Connected, want, player)
}

func TestDisconnectGrace(t *testing.T) {
	previous := disconnectGrace
	disconnectGrace = 100 * time.Millisecond
	defer func() { disconnectGrace = previous }()

	host := startTestServer(t)
	invite := host.invite(t)
	client := peer.New(invite, peer.Profile{Name: "Mio"}, nil, nil, nil)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	key := host.sessionKey(t, invite, client.PlayerID, client)

	cancel, resp := openSSE(t, host.baseURL(), key, client.PlayerID)
	reader := bufio.NewReader(resp.Body)
	sawState := false
	for range 40 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data:") {
			sawState = true
			break
		}
	}
	if !sawState {
		t.Fatal("no state event on SSE")
	}
	waitPlayerConnected(t, host.table, client.PlayerID, true)
	cancel()
	resp.Body.Close()

	waitPlayerConnected(t, host.table, client.PlayerID, false)
	if player, _ := host.table.Player(client.PlayerID); !player.SittingOut {
		t.Fatalf("disconnected player not sitting out: %+v", player)
	}

	// Reconnecting inside the grace period keeps the seat connected.
	cancel, resp = openSSE(t, host.baseURL(), key, client.PlayerID)
	waitPlayerConnected(t, host.table, client.PlayerID, true)
	cancel()
	resp.Body.Close()
}

func TestCloseIsIdempotent(t *testing.T) {
	host := startTestServer(t)
	invite := host.invite(t)
	if invite.Port != host.port {
		t.Fatalf("invite port = %d, want %d", invite.Port, host.port)
	}
	if err := host.server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.server.Listen(); err == nil {
		t.Fatal("Listen after Close must fail")
	}
}
