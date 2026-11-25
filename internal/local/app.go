package local

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"magpie-poker/internal/game"
	"magpie-poker/internal/magpiecli"
	"magpie-poker/internal/netx"
	"magpie-poker/internal/peer"
	"magpie-poker/internal/store"
	"magpie-poker/internal/tableserver"
)

const Name = "magpie-poker"
const Version = "0.2.0"

type Options struct {
	Port      int
	TablePort int
	WebRoot   string
	DataDir   string
	Addr      string
	Version   string
}

type Notice struct {
	Level string `json:"level"`
	Text  string `json:"text"`
	At    int64  `json:"at"`
}

type HostState struct {
	Identity     netx.Identity
	Secret       string
	TableID      string
	HostPlayerID string
	Port         int
	ServedURL    string
}

type App struct {
	mu      sync.Mutex
	opts    Options
	store   *store.Store
	profile store.Profile

	mode         string
	table        *game.Table
	host         *HostState
	tableServer  *tableserver.Server
	peer         *peer.Client
	peerState    json.RawMessage
	ts           magpiecli.TailscaleStatus
	serve        any
	keys         magpiecli.KeysState
	bank         *store.Bank
	notice       *Notice
	errText      string
	started      time.Time
	hub          *Hub
	server       *http.Server
	shuttingDown bool
}

func New(opts Options) (*App, error) {
	if opts.Port == 0 {
		opts.Port = 3481
	}
	if opts.TablePort == 0 {
		opts.TablePort = 3482
	}
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1"
	}
	if opts.Version == "" {
		opts.Version = Version
	}
	st, err := store.New(opts.DataDir)
	if err != nil {
		return nil, err
	}
	a := &App{
		opts:    opts,
		store:   st,
		profile: st.LoadProfile(),
		mode:    "idle",
		hub:     NewHub(),
		started: time.Now(),
	}
	a.bank = st.LoadBank()
	a.keys = magpiecli.KeysState{Magpie: magpiecli.Bin() != "", Bin: magpiecli.Bin(), All: []magpiecli.Key{}}
	return a, nil
}

func (a *App) Port() int { return a.opts.Port }

func (a *App) Profile() store.Profile {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.profile
}

func (a *App) bootstrap() {
	ts := magpiecli.TailscaleStatusNow()
	a.mu.Lock()
	a.ts = ts
	a.mu.Unlock()
	if v, err := magpiecli.ServeStatusNow(); err == nil {
		a.mu.Lock()
		a.serve = v
		a.mu.Unlock()
	}
	if err := a.resumeHost(); err != nil {
		a.setError("恢复上次牌桌失败：" + err.Error())
	}
	a.push()
}

func (a *App) resumeHost() error {
	rec, err := a.store.LoadHost()
	if err != nil {
		return nil
	}
	data, err := a.store.LoadTable(rec.TableID)
	if err != nil {
		return nil
	}
	table, err := game.Restore(data)
	if err != nil {
		return err
	}
	identity := netx.Identity{Pub: rec.Pub, Priv: rec.Priv}
	svr := tableserver.New(table, identity, rec.Secret, rec.Port, a.onTableChange, a.logf)
	port, err := svr.Listen()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.table = table
	a.host = &HostState{
		Identity: identity, Secret: rec.Secret, TableID: rec.TableID,
		HostPlayerID: rec.HostPlayerID, Port: port, ServedURL: rec.ServedURL,
	}
	a.tableServer = svr
	a.mode = "host"
	a.mu.Unlock()
	a.persist()
	a.logf("resumed table %s on :%d", table.ID(), port)
	return nil
}

func (a *App) onTableChange() {
	// Table callbacks can run while the engine holds its own lock; never call
	// Snapshot/ViewFor on that stack.
	go func() {
		a.persist()
		a.push()
		a.mu.Lock()
		svr := a.tableServer
		a.mu.Unlock()
		if svr != nil {
			svr.Broadcast()
		}
	}()
}

func (a *App) persist() {
	a.mu.Lock()
	table := a.table
	host := a.host
	profile := a.profile
	a.mu.Unlock()
	if table != nil {
		if data, err := table.Snapshot(); err == nil {
			_ = a.store.SaveTable(table.ID(), data)
		}
	}
	if host != nil {
		_ = a.store.SaveHost(&store.HostRecord{
			TableID: host.TableID, HostPlayerID: host.HostPlayerID,
			Pub: host.Identity.Pub, Priv: host.Identity.Priv, Secret: host.Secret,
			Port: host.Port, ServedURL: host.ServedURL,
		})
	}
	_ = a.store.SaveProfile(profile)
}

func (a *App) setError(s string) {
	a.mu.Lock()
	a.errText = s
	a.mu.Unlock()
	a.push()
}

func (a *App) notify(level, text string) {
	a.mu.Lock()
	a.notice = &Notice{Level: level, Text: text, At: time.Now().UnixMilli()}
	n := *a.notice
	a.mu.Unlock()
	a.hub.BroadcastRaw([]byte(fmt.Sprintf("event: notice\ndata: %s\n\n", mustJSON(n))))
	a.push()
}

func (a *App) push() {
	b, err := json.Marshal(a.state())
	if err != nil {
		return
	}
	a.hub.BroadcastFrame("state", b)
}

// ---- host / guest -------------------------------------------------------

func randomID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func randomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomSeed() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *App) hostTable(req hostRequest) (map[string]any, error) {
	a.mu.Lock()
	if a.mode != "idle" {
		a.mu.Unlock()
		return nil, errors.New("已经在牌局中，请先离开")
	}
	profile := a.profile
	ts := a.ts
	a.mu.Unlock()

	tableID := randomID("t_")
	hostID := randomID("p_")
	tableName := strings.TrimSpace(req.TableName)
	if tableName == "" {
		tableName = strings.TrimSpace(req.Name)
	}
	if tableName == "" {
		tableName = "周五牌局"
	}
	table := game.NewTable(tableID, tableName, hostID, req.Config)
	_, err := table.AddPlayer(game.PlayerInput{
		ID: hostID, Name: profile.Name, Avatar: profile.Avatar, GatewayURL: profile.GatewayURL,
		Tailscale: map[string]any{"name": ts.Name, "dns": ts.DNS, "ip": ts.IP},
	})
	if err != nil {
		return nil, err
	}
	identity, err := netx.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	secret := randomSecret()
	svr := tableserver.New(table, identity, secret, a.opts.TablePort, a.onTableChange, a.logf)
	port, err := svr.Listen()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.table = table
	a.host = &HostState{Identity: identity, Secret: secret, TableID: tableID, HostPlayerID: hostID, Port: port}
	a.tableServer = svr
	a.mode = "host"
	a.errText = ""
	a.mu.Unlock()
	a.persist()
	a.notify("info", "牌桌已开："+tableName)
	a.push()
	a.tableServer.Broadcast()
	return a.hostViewMap(), nil
}

func (a *App) joinTable(code string) (map[string]any, error) {
	a.mu.Lock()
	if a.mode != "idle" {
		a.mu.Unlock()
		return nil, errors.New("已经在牌局中，请先离开")
	}
	profile := a.profile
	ts := a.ts
	a.mu.Unlock()

	invite, err := netx.ParseInvite(code)
	if err != nil {
		return nil, err
	}
	if invite.ExpiresAt > 0 && invite.ExpiresAt < time.Now().UnixMilli() {
		return nil, errors.New("邀请码已过期")
	}
	cl := peer.New(invite, peer.Profile{
		Name: profile.Name, Avatar: profile.Avatar, GatewayURL: profile.GatewayURL, Tailscale: ts.Installed,
	}, func(raw json.RawMessage) {
		a.mu.Lock()
		a.peerState = append(json.RawMessage(nil), raw...)
		a.errText = ""
		a.mu.Unlock()
		a.push()
	}, func(level, text string) {
		a.notify(level, text)
	}, func(status string) {
		a.push()
	})
	if err := cl.Connect(); err != nil {
		cl.Close()
		return nil, err
	}
	a.mu.Lock()
	a.peer = cl
	a.mode = "guest"
	a.peerState = nil
	a.errText = ""
	a.mu.Unlock()
	cl.Subscribe()
	a.notify("info", fmt.Sprintf("已加入「%s」，等待牌桌状态", invite.TableName))
	a.push()
	return map[string]any{"ok": true, "playerId": cl.PlayerID, "seat": cl.Seat}, nil
}

func (a *App) leaveTable(force bool) error {
	a.mu.Lock()
	mode := a.mode
	table := a.table
	svr := a.tableServer
	cl := a.peer
	host := a.host
	a.mu.Unlock()

	if mode == "host" {
		if table != nil && table.Phase() == "hand" {
			return errors.New("先打完这一手再离桌")
		}
		if !force && unsettled(table) {
			return errors.New("还有未结清的转账；确认后再离桌")
		}
		if svr != nil {
			_ = svr.Close()
		}
		a.mu.Lock()
		a.table = nil
		a.host = nil
		a.tableServer = nil
		a.mode = "idle"
		a.errText = ""
		a.mu.Unlock()
		a.store.ClearHost()
		a.notify("info", "已离开牌桌")
		return nil
	}
	if mode == "guest" {
		if cl != nil {
			_, _ = cl.Post("/peer/leave", map[string]any{})
			cl.Close()
		}
		a.mu.Lock()
		a.peer = nil
		a.peerState = nil
		a.mode = "idle"
		a.errText = ""
		a.mu.Unlock()
		a.notify("info", "已离开牌桌")
		return nil
	}
	_ = host
	return nil
}

func unsettled(table *game.Table) bool {
	if table == nil {
		return false
	}
	v := table.ViewFor("", time.Now())
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s, _ := m["settlement"].(map[string]any)
	ts, _ := s["transfers"].([]any)
	for _, x := range ts {
		if t, ok := x.(map[string]any); ok {
			st, _ := t["status"].(string)
			if st == "pending" || st == "issued" {
				return true
			}
		}
	}
	return false
}

// ---- state --------------------------------------------------------------

func (a *App) tableAndMeLocked() (json.RawMessage, map[string]any) {
	now := time.Now()
	if a.mode == "host" && a.table != nil {
		view := a.table.ViewFor(a.host.HostPlayerID, now)
		b, _ := json.Marshal(view)
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			m["invite"] = a.inviteInfoLocked()
			me, _ := m["me"].(map[string]any)
			if me == nil {
				me = map[string]any{}
			}
			me["name"] = a.profile.Name
			me["isHost"] = true
			if me["id"] == nil {
				me["id"] = a.host.HostPlayerID
			}
			b, _ = json.Marshal(m)
			return b, me
		}
		return b, map[string]any{"name": a.profile.Name, "isHost": true, "id": a.host.HostPlayerID}
	}
	if a.mode == "guest" && len(a.peerState) > 0 {
		var m map[string]any
		if json.Unmarshal(a.peerState, &m) == nil {
			me, _ := m["me"].(map[string]any)
			if me == nil {
				me = map[string]any{}
			}
			me["name"] = a.profile.Name
			me["isHost"] = false
			if a.peer != nil {
				if me["id"] == nil {
					me["id"] = a.peer.PlayerID
				}
				if me["seat"] == nil {
					me["seat"] = a.peer.Seat
				}
			}
			return append(json.RawMessage(nil), a.peerState...), me
		}
	}
	return nil, map[string]any{"id": "", "seat": nil, "name": a.profile.Name, "isHost": false}
}

func (a *App) state() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	tableRaw, me := a.tableAndMeLocked()
	status := any(nil)
	if a.peer != nil {
		status = a.peer.Status
	}
	host := map[string]any{
		"name": a.ts.Name, "ip": a.ts.IP, "dns": a.ts.DNS,
		"tailscale": a.ts.Installed, "serve": "", "serveGateway": a.profile.GatewayURL,
		"error": nil,
	}
	if a.host != nil && a.host.ServedURL != "" {
		host["serve"] = a.host.ServedURL
	}
	if a.ts.Error != "" {
		host["error"] = a.ts.Error
	}
	var notice any
	if a.notice != nil {
		notice = a.notice
	}
	var errVal any
	if a.errText != "" {
		errVal = a.errText
	}
	return map[string]any{
		"mode": a.mode, "version": a.opts.Version,
		"me": me, "profile": a.profile,
		"host": host, "connection": map[string]any{"status": status},
		"keys": a.keys, "table": tableRaw,
		"notice": notice, "error": errVal,
	}
}

func (a *App) hostViewMap() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.table == nil || a.host == nil {
		return nil
	}
	view := a.table.ViewFor(a.host.HostPlayerID, time.Now())
	b, _ := json.Marshal(view)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// ---- invites ------------------------------------------------------------

func (a *App) inviteInfoLocked() map[string]any {
	if a.host == nil {
		return nil
	}
	hostIP := a.ts.IP
	if hostIP == "" {
		hostIP = "127.0.0.1"
	}
	direct, err := netx.CreateInvite(netx.Invite{
		V: 1, TableID: a.host.TableID, TableName: a.table.Name(), Host: hostIP,
		Port: a.host.Port, TLS: false, HostPub: a.host.Identity.Pub, Secret: a.host.Secret,
		HostName: a.ts.Name,
	})
	if err != nil {
		return nil
	}
	info := map[string]any{
		"code":   direct,
		"direct": inviteURL(direct, fmt.Sprintf("http://%s:%d/", hostIP, a.host.Port)),
		"served": nil, "servedCode": nil, "servedUrl": nil,
		"tablePort": a.host.Port,
	}
	if a.host.ServedURL != "" {
		if u, err := url.Parse(a.host.ServedURL); err == nil {
			code, err := netx.CreateInvite(netx.Invite{
				V: 1, TableID: a.host.TableID, TableName: a.table.Name(),
				Host: u.Hostname(), Port: portOf(u, 443), TLS: u.Scheme == "https",
				HostPub: a.host.Identity.Pub, Secret: a.host.Secret, HostName: a.ts.Name,
			})
			if err == nil {
				info["servedCode"] = code
				info["served"] = inviteURL(code, a.host.ServedURL)
				info["servedUrl"] = a.host.ServedURL
			}
		}
	}
	return info
}

func (a *App) inviteInfo() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inviteInfoLocked()
}

func inviteURL(code, base string) string {
	base = strings.TrimRight(strings.SplitN(strings.SplitN(base, "?", 2)[0], "#", 2)[0], "/")
	return base + "/#" + code
}

func portOf(u *url.URL, def int) int {
	if p := u.Port(); p != "" {
		var n int
		_, _ = fmt.Sscanf(p, "%d", &n)
		if n > 0 {
			return n
		}
	}
	return def
}

// ---- keys / settlement --------------------------------------------------

func (a *App) refreshKeys() magpiecli.KeysState {
	if magpiecli.Bin() == "" {
		a.mu.Lock()
		var issuer *magpiecli.Key
		if a.bank != nil {
			issuer = &magpiecli.Key{ID: a.bank.ID, Name: a.bank.Name}
		}
		a.keys = magpiecli.KeysState{Magpie: false, Issuer: issuer, All: []magpiecli.Key{}}
		ks := a.keys
		a.mu.Unlock()
		return ks
	}
	all, err := magpiecli.ListKeys()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		var issuer *magpiecli.Key
		if a.bank != nil {
			issuer = &magpiecli.Key{ID: a.bank.ID, Name: a.bank.Name}
		}
		a.keys = magpiecli.KeysState{Magpie: true, Bin: magpiecli.Bin(), Issuer: issuer, All: []magpiecli.Key{}, Error: magpiecli.Redact(err.Error())}
		return a.keys
	}
	issuer := (*magpiecli.Key)(nil)
	if a.bank != nil {
		for i := range all {
			if all[i].ID == a.bank.ID {
				issuer = &all[i]
				break
			}
		}
		if issuer == nil {
			issuer = &magpiecli.Key{ID: a.bank.ID, Name: a.bank.Name}
		}
	}
	if all == nil {
		all = []magpiecli.Key{}
	}
	a.keys = magpiecli.KeysState{Magpie: true, Bin: magpiecli.Bin(), Issuer: issuer, All: all}
	return a.keys
}

func (a *App) ensureBank() (*magpiecli.Key, error) {
	a.mu.Lock()
	bank := a.bank
	a.mu.Unlock()
	preferred := ""
	if bank != nil {
		preferred = bank.ID
	}
	key, err := magpiecli.EnsureBank(preferred)
	if err != nil {
		return nil, err
	}
	b := &store.Bank{ID: key.ID, Name: key.Name}
	a.mu.Lock()
	a.bank = b
	a.mu.Unlock()
	_ = a.store.SaveBank(b)
	a.refreshKeys()
	return key, nil
}

func (a *App) settlementTransfer(transferID string) (map[string]any, string, error) {
	tableRaw, me := func() (json.RawMessage, map[string]any) {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.tableAndMeLocked()
	}()
	if len(tableRaw) == 0 {
		return nil, "", errors.New("没有牌局")
	}
	var m map[string]any
	if err := json.Unmarshal(tableRaw, &m); err != nil {
		return nil, "", err
	}
	s, _ := m["settlement"].(map[string]any)
	transfers, _ := s["transfers"].([]any)
	var found map[string]any
	for _, x := range transfers {
		t, _ := x.(map[string]any)
		if t["id"] == transferID {
			found = t
			break
		}
	}
	if found == nil {
		return nil, "", errors.New("没有这笔结算")
	}
	meID, _ := me["id"].(string)
	return found, meID, nil
}

func (a *App) issueTransfer(transferID string) (map[string]any, error) {
	t, meID, err := a.settlementTransfer(transferID)
	if err != nil {
		return nil, err
	}
	from, _ := t["from"].(string)
	if from != meID {
		return nil, errors.New("只有欠款方能签发")
	}
	status, _ := t["status"].(string)
	if status == "issued" || status == "verified" {
		return nil, errors.New("已经签发过了")
	}
	if status == "small" {
		return nil, errors.New("金额低于最小结算额")
	}
	amount, _ := t["amount"].(float64)
	unit, _ := t["unit"].(string)
	to, _ := t["to"].(string)
	tableName := ""
	a.mu.Lock()
	if a.table != nil {
		tableName = a.table.ID()
	}
	a.mu.Unlock()
	key, err := a.ensureBank()
	if err != nil {
		return nil, err
	}
	keyText, expires, err := magpiecli.IssueKey(key.ID, fmt.Sprintf("poker-%s-%s-to-%s", tableName, transferID, to), unit, amount)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	mode := a.mode
	gateway := a.profile.GatewayURL
	if gateway == "" {
		if g, _ := t["gatewayUrl"].(string); g != "" {
			gateway = g
		}
	}
	table := a.table
	host := a.host
	cl := a.peer
	a.mu.Unlock()
	if mode == "host" && table != nil && host != nil {
		if err := table.MarkIssued(transferID, meID, keyText, expires, gateway); err != nil {
			return nil, err
		}
		a.onTableChange()
	} else if mode == "guest" && cl != nil {
		if _, err := cl.Post("/peer/settle", map[string]any{"transferId": transferID, "key": keyText, "expires": expires, "gatewayUrl": gateway}); err != nil {
			return nil, err
		}
	}
	a.notify("info", "已签发 key，等待对方验证")
	return map[string]any{"ok": true, "expires": expires}, nil
}

func (a *App) verifyTransfer(transferID string) (map[string]any, error) {
	t, meID, err := a.settlementTransfer(transferID)
	if err != nil {
		return nil, err
	}
	to, _ := t["to"].(string)
	if to != meID {
		return nil, errors.New("只有收款方能验证")
	}
	key, _ := t["key"].(string)
	if key == "" {
		return nil, errors.New("对方还没有签发 key")
	}
	gateway, _ := t["gatewayUrl"].(string)
	ok, models, code, errText := magpiecli.VerifyKey(gateway, key)
	if !ok {
		msg := errText
		if msg == "" {
			msg = fmt.Sprintf("key 验证失败（HTTP %d）", code)
		}
		return map[string]any{"ok": false, "error": msg, "status": code}, nil
	}
	a.mu.Lock()
	mode := a.mode
	table := a.table
	cl := a.peer
	a.mu.Unlock()
	if mode == "host" && table != nil {
		if err := table.MarkVerified(transferID, meID); err != nil {
			return nil, err
		}
		a.onTableChange()
	} else if mode == "guest" && cl != nil {
		if _, err := cl.Post("/peer/verify", map[string]any{"transferId": transferID}); err != nil {
			return nil, err
		}
	}
	a.notify("info", fmt.Sprintf("key 可用，网关返回 %d 个模型", models))
	return map[string]any{"ok": true, "models": models}, nil
}

func (a *App) setProfile(name, avatar, gateway string) (store.Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Profile{}, errors.New("名字不能为空")
	}
	if len([]rune(name)) > 16 {
		name = string([]rune(name)[:16])
	}
	if gateway != "" && !strings.HasPrefix(gateway, "http://") && !strings.HasPrefix(gateway, "https://") {
		return store.Profile{}, errors.New("网关地址要以 http(s):// 开头")
	}
	a.mu.Lock()
	p := store.Profile{Name: name, Avatar: avatar, GatewayURL: strings.TrimSpace(gateway)}
	if p.Avatar == "" {
		p.Avatar = "fox"
	}
	a.profile = p
	a.mu.Unlock()
	_ = a.store.SaveProfile(p)
	a.push()
	return p, nil
}

func (a *App) status() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]any{"ok": true, "name": Name, "version": a.opts.Version, "pid": os.Getpid(), "uptime": time.Since(a.started).Seconds(), "mode": a.mode}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (a *App) logf(format string, args ...any) {
	// magpie's plugin host captures stdout; keep it terse and never log secrets.
	fmt.Printf("[poker] "+format+"\n", args...)
}
