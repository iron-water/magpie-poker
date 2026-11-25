package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"magpie-poker/internal/game"
	"magpie-poker/internal/magpiecli"
	"magpie-poker/internal/store"
)

type hostRequest struct {
	Name      string      `json:"name"`
	TableName string      `json:"tableName"`
	Config    game.Config `json:"config"`
}

func (a *App) ListenAndServe() error {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", a.opts.Addr, a.opts.Port))
	if err != nil {
		return err
	}
	a.opts.Port = ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{
		Handler:           http.HandlerFunc(a.handle),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()
	go a.bootstrap()
	return srv.Serve(ln)
}

func (a *App) Close() error {
	a.mu.Lock()
	a.shuttingDown = true
	svr := a.server
	ts := a.tableServer
	cl := a.peer
	a.tableServer = nil
	a.mu.Unlock()
	if cl != nil {
		cl.Close()
	}
	if ts != nil {
		_ = ts.Close()
	}
	if svr != nil {
		return svr.Close()
	}
	return nil
}

// localRequestOK binds the local control surface to loopback and to this
// origin. Host validation defeats DNS rebinding; the custom header defeats
// cross-site forms and simple fetches (a cross-site custom header needs a
// preflight that this server never grants).
func (a *App) localRequestOK(w http.ResponseWriter, r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || port != fmt.Sprint(a.opts.Port) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "local host required"})
		return false
	}
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
	default:
		writeJSON(w, 403, map[string]any{"ok": false, "error": "local host required"})
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "cross-site request refused"})
		return false
	}
	if r.Method == http.MethodPost {
		if r.Header.Get("X-Poker-Local") != "1" {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "local request required"})
			return false
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "origin refused"})
			return false
		}
	}
	return true
}

func (a *App) handle(w http.ResponseWriter, r *http.Request) {
	if !a.localRequestOK(w, r) {
		return
	}
	p := r.URL.Path
	if p == "/api/state" && r.Method == http.MethodGet {
		writeJSON(w, 200, a.state())
		return
	}
	if p == "/api/health" && r.Method == http.MethodGet {
		writeJSON(w, 200, a.status())
		return
	}
	if p == "/api/events" && r.Method == http.MethodGet {
		a.events(w, r)
		return
	}
	if p == "/api/keys" && r.Method == http.MethodGet {
		writeJSON(w, 200, a.refreshKeys())
		return
	}
	if p == "/api/tailscale" && r.Method == http.MethodGet {
		a.tailscaleInfo(w)
		return
	}
	if strings.HasPrefix(p, "/api/") && r.Method == http.MethodPost {
		a.post(w, r, p)
		return
	}
	if strings.HasPrefix(p, "/api/") {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "not found"})
		return
	}
	a.static(w, r, p)
}

func (a *App) post(w http.ResponseWriter, r *http.Request, path string) {
	body, err := readBody(r)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	switch path {
	case "/api/profile":
		var b struct {
			Name       string `json:"name"`
			Avatar     string `json:"avatar"`
			GatewayURL string `json:"gatewayUrl"`
		}
		if json.Unmarshal(body, &b) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "JSON 不合法"})
			return
		}
		p, err := a.setProfile(b.Name, b.Avatar, b.GatewayURL)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "profile": p})
	case "/api/table/host":
		var req hostRequest
		if json.Unmarshal(body, &req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "JSON 不合法"})
			return
		}
		view, err := a.hostTable(req)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "invite": a.inviteInfo(), "table": view})
	case "/api/table/join":
		var b struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &b) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "JSON 不合法"})
			return
		}
		res, err := a.joinTable(b.Code)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	case "/api/table/leave":
		var b struct {
			Force bool `json:"force"`
		}
		_ = json.Unmarshal(body, &b)
		if err := a.leaveTable(b.Force); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "/api/table/start", "/api/table/next":
		if err := a.hostStart(); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "/api/table/rebuy":
		var b struct {
			Amount int `json:"amount"`
		}
		_ = json.Unmarshal(body, &b)
		if err := a.rebuy(b.Amount); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "/api/table/end":
		s, err := a.endTable()
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "settlement": s})
	case "/api/action":
		var act game.Action
		if json.Unmarshal(body, &act) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "JSON 不合法"})
			return
		}
		if err := a.action(act); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "/api/chat":
		var b struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(body, &b)
		if err := a.chat(b.Text); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "/api/settlement/issue":
		var b struct {
			TransferID string `json:"transferId"`
		}
		_ = json.Unmarshal(body, &b)
		res, err := a.issueTransfer(b.TransferID)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	case "/api/settlement/verify":
		var b struct {
			TransferID string `json:"transferId"`
		}
		_ = json.Unmarshal(body, &b)
		res, err := a.verifyTransfer(b.TransferID)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	case "/api/keys/bank":
		var b struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &b)
		a.settleMu.Lock()
		key, err := magpiecli.EnsureBank(b.ID)
		a.settleMu.Unlock()
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		bank := &store.Bank{ID: key.ID, Name: key.Name}
		a.mu.Lock()
		a.bank = bank
		a.mu.Unlock()
		_ = a.store.SaveBank(bank)
		a.refreshKeys()
		a.push()
		writeJSON(w, 200, map[string]any{"ok": true, "bank": key})
	case "/api/keys/verify":
		var b struct{ URL, Key string }
		_ = json.Unmarshal(body, &b)
		ok, models, code, errText := magpiecli.VerifyKey(b.URL, b.Key)
		writeJSON(w, 200, map[string]any{"ok": ok, "models": models, "status": code, "error": errText})
	case "/api/tailscale/serve":
		a.serveTable(w, body)
	case "/api/tailscale/serve-gateway":
		a.serveGateway(w, body)
	default:
		writeJSON(w, 404, map[string]any{"ok": false, "error": "not found"})
	}
}

func (a *App) hostStart() error {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	a.mu.Lock()
	mode := a.mode
	table := a.table
	a.mu.Unlock()
	if mode != "host" || table == nil {
		return errors.New("只有房主能开始新的一手")
	}
	if err := table.StartHand(randomSeed()); err != nil {
		return err
	}
	a.onTableChange()
	return nil
}

func (a *App) rebuy(amount int) error {
	a.mu.Lock()
	mode, table, cl, hostID := a.mode, a.table, a.peer, ""
	if a.host != nil {
		hostID = a.host.HostPlayerID
	}
	a.mu.Unlock()
	if mode == "host" && table != nil {
		if err := table.Rebuy(hostID, amount); err != nil {
			return err
		}
		a.onTableChange()
		return nil
	}
	if mode == "guest" && cl != nil {
		_, err := cl.Post("/peer/rebuy", map[string]any{"amount": amount})
		return err
	}
	return errors.New("不在牌桌上")
}

func (a *App) action(act game.Action) error {
	a.mu.Lock()
	mode, table, cl, hostID := a.mode, a.table, a.peer, ""
	if a.host != nil {
		hostID = a.host.HostPlayerID
	}
	a.mu.Unlock()
	if mode == "host" && table != nil {
		if err := table.Act(hostID, act); err != nil {
			return err
		}
		a.onTableChange()
		return nil
	}
	if mode == "guest" && cl != nil {
		_, err := cl.Post("/peer/action", act)
		return err
	}
	return errors.New("不在牌桌上")
}

func (a *App) chat(text string) error {
	a.mu.Lock()
	mode, table, cl, hostID := a.mode, a.table, a.peer, ""
	if a.host != nil {
		hostID = a.host.HostPlayerID
	}
	a.mu.Unlock()
	if mode == "host" && table != nil {
		if err := table.Chat(hostID, text); err != nil {
			return err
		}
		a.onTableChange()
		return nil
	}
	if mode == "guest" && cl != nil {
		_, err := cl.Post("/peer/chat", map[string]any{"text": text})
		return err
	}
	return errors.New("不在牌桌上")
}

func (a *App) endTable() (any, error) {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	a.settleMu.Lock()
	defer a.settleMu.Unlock()
	a.mu.Lock()
	mode, table, host := a.mode, a.table, a.host
	a.mu.Unlock()
	if mode != "host" || table == nil || host == nil {
		return nil, errors.New("只有房主能结束牌局")
	}
	if err := table.EndTable(); err != nil {
		return nil, err
	}
	a.onTableChange()
	view := table.ViewFor(host.HostPlayerID, time.Now())
	b, _ := json.Marshal(view)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m["settlement"], nil
}

func (a *App) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("content-type", "text/event-stream; charset=utf-8")
	w.Header().Set("cache-control", "no-cache, no-transform")
	w.Header().Set("connection", "keep-alive")
	w.Header().Set("x-accel-buffering", "no")
	w.WriteHeader(200)
	ch := a.hub.Subscribe()
	defer a.hub.Unsubscribe(ch)
	if b, err := json.Marshal(a.state()); err == nil {
		_, _ = fmt.Fprintf(w, "event: state\ndata: %s\n\n", b)
		fl.Flush()
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-ch:
			_, _ = w.Write(frame)
			fl.Flush()
		case <-tick.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func (a *App) static(w http.ResponseWriter, r *http.Request, p string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	root := a.opts.WebRoot
	if root == "" {
		root = "."
	}
	webDir := filepath.Join(root, "web")
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil {
		webDir = root
	}
	assetsDir := filepath.Join(root, "assets")
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		rel = "index.html"
	}
	dir := webDir
	if strings.HasPrefix(rel, "assets/") {
		dir = assetsDir
		rel = strings.TrimPrefix(rel, "assets/")
	}
	clean := filepath.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		http.NotFound(w, r)
		return
	}
	file := filepath.Join(dir, clean)
	if !strings.HasPrefix(file, dir+string(os.PathSeparator)) && file != dir {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(file))
	mime := map[string]string{
		".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
		".mjs": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
		".json": "application/json; charset=utf-8", ".svg": "image/svg+xml",
		".png": "image/png", ".webp": "image/webp", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".woff2": "font/woff2", ".ico": "image/x-icon", ".txt": "text/plain; charset=utf-8",
	}[ext]
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("content-type", mime)
	if ext == ".html" || ext == ".js" || ext == ".css" {
		w.Header().Set("cache-control", "no-cache")
	} else {
		w.Header().Set("cache-control", "public, max-age=3600")
	}
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("referrer-policy", "no-referrer")
	w.Header().Set("content-security-policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'none'")
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

func (a *App) tailscaleInfo(w http.ResponseWriter) {
	ts := magpiecli.TailscaleStatusNow()
	serve, serr := magpiecli.ServeStatusNow()
	a.mu.Lock()
	a.ts = ts
	if serr == nil {
		a.serve = serve
	}
	host := a.host
	profile := a.profile
	a.mu.Unlock()
	configured := false
	if m, ok := serve.(map[string]any); ok {
		if v, ok := m["TCP"].(map[string]any); ok && len(v) > 0 {
			configured = true
		}
	}
	serveText := ""
	if host != nil && host.ServedURL != "" {
		serveText = host.ServedURL
	} else if configured {
		serveText = "已配置"
	}
	errText := ts.Error
	if errText == "" && serr != nil {
		errText = serr.Error()
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "installed": ts.Installed, "name": ts.Name, "ip": ts.IP, "dns": ts.DNS,
		"online": ts.Online, "serve": serveText, "serveGateway": profile.GatewayURL,
		"error": errText, "invite": a.inviteInfo(),
	})
}

func (a *App) serveTable(w http.ResponseWriter, body []byte) {
	var b struct {
		Port int `json:"port"`
	}
	_ = json.Unmarshal(body, &b)
	if b.Port <= 0 {
		b.Port = 8443
	}
	a.mu.Lock()
	host := a.host
	a.mu.Unlock()
	if host == nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "只有房主能共享牌桌"})
		return
	}
	u, err := magpiecli.EnableServe(host.Port, b.Port)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	a.mu.Lock()
	host.ServedURL = u
	host.AutoServed = true
	inv := a.inviteInfoLocked()
	a.mu.Unlock()
	a.persist()
	a.push()
	writeJSON(w, 200, map[string]any{"ok": true, "url": u, "invite": inv})
}

func (a *App) serveGateway(w http.ResponseWriter, body []byte) {
	var b struct {
		Port int `json:"port"`
	}
	_ = json.Unmarshal(body, &b)
	if b.Port <= 0 {
		b.Port = 8444
	}
	u, err := magpiecli.EnableServe(magpiecli.LocalGatewayPort(), b.Port)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	a.mu.Lock()
	a.profile.GatewayURL = u
	p := a.profile
	a.mu.Unlock()
	_ = a.store.SaveProfile(p)
	a.push()
	writeJSON(w, 200, map[string]any{"ok": true, "url": u})
}

func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return []byte("{}"), nil
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "json error", 500)
		return
	}
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.Header().Set("cache-control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
