package game

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config is the table configuration, matching normalizeConfig in game.mjs.
type Config struct {
	SmallBlind    int     `json:"smallBlind"`
	BigBlind      int     `json:"bigBlind"`
	Ante          int     `json:"ante"`
	StartingChips int     `json:"startingChips"`
	MaxSeats      int     `json:"maxSeats"`
	Unit          string  `json:"unit"`
	ChipValue     float64 `json:"chipValue"`
	Currency      string  `json:"currency"`
	ActionSeconds int     `json:"actionSeconds"`
	MinSettlement float64 `json:"minSettlement"`
}

// Action is one player action; Amount is "to" for bet/raise.
type Action struct {
	Type   string `json:"type"`
	Amount int    `json:"amount"`
}

// PlayerInput is the AddPlayer argument.
type PlayerInput struct {
	ID         string
	Name       string
	Avatar     string
	GatewayURL string
	Tailscale  any
	Pub        string
}

// Player is one seat row. Only ID, Name, Avatar and Seat are published; the
// rest is table state.
type Player struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	Seat       int    `json:"seat"`
	Chips      int    `json:"chips"`
	BuyIn      int    `json:"buyIn"`
	GatewayURL string `json:"gatewayUrl"`
	Tailscale  any    `json:"tailscale"`
	Pub        string `json:"pub,omitempty"`
	Connected  bool   `json:"connected"`
	SittingOut bool   `json:"sittingOut"`
	Out        bool   `json:"out"`
	Left       bool   `json:"left"`
	Cards      []int  `json:"cards"`
	Bet        int    `json:"bet"`
	Committed  int    `json:"committed"`
	Folded     bool   `json:"folded"`
	AllIn      bool   `json:"allIn"`
	Acted      bool   `json:"acted"`
	MayRaise   bool   `json:"mayRaise"`
	LastAction string `json:"lastAction"`
	Won        int    `json:"won"`
}

// PokerError carries the JS error code (bad_action, out_of_turn, ...).
type PokerError struct {
	Message string
	Code    string
}

func (e *PokerError) Error() string { return e.Message }

func pokerError(code, format string, args ...any) *PokerError {
	return &PokerError{Message: fmt.Sprintf(format, args...), Code: code}
}

var streets = []string{"preflop", "flop", "turn", "river"}

func streetIndex(street string) int {
	for i, s := range streets {
		if s == street {
			return i
		}
	}
	return 0
}

func streetName(street string) string {
	switch street {
	case "preflop":
		return "翻牌前"
	case "flop":
		return "翻牌"
	case "turn":
		return "转牌"
	case "river":
		return "河牌"
	}
	return street
}

// Table is a Texas Hold'em table. Exported methods take the internal mutex, so
// the table server and the local server may call it concurrently.
type Table struct {
	mu sync.Mutex

	id            string
	name          string
	hostID        string
	config        Config
	phase         string
	players       []*Player
	seats         []string
	dealerSeat    int
	handNo        int
	street        string
	board         []int
	deck          []int
	deckPos       int
	handPlayers   []string
	currentBet    int
	minRaiseTo    int
	lastRaiseSize int
	toAct         *string
	deadline      *int64
	aggressor     *string
	showdown      *Showdown
	settlement    *Settlement
	log           []LogEntry
	hand          *Hand
	createdAt     int64
	updatedAt     int64
	sbSeat        *int
	bbSeat        *int
}

// NewTable creates a lobby table and records its creation in the log.
func NewTable(id, name, hostID string, cfg Config) *Table {
	if name == "" {
		name = "牌局"
	}
	now := time.Now().UnixMilli()
	t := &Table{
		id:          id,
		name:        truncateRunes(name, 40),
		hostID:      hostID,
		config:      normalizeConfig(cfg),
		phase:       "lobby",
		players:     make([]*Player, 0),
		dealerSeat:  -1,
		street:      "preflop",
		board:       make([]int, 0),
		deck:        make([]int, 0),
		handPlayers: make([]string, 0),
		log:         make([]LogEntry, 0),
		createdAt:   now,
		updatedAt:   now,
	}
	t.seats = make([]string, t.config.MaxSeats)
	t.lastRaiseSize = t.config.BigBlind
	t.note("system", fmt.Sprintf("牌桌「%s」已创建", t.name), nil)
	return t
}

// ID returns the table id.
func (t *Table) ID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.id
}

// Name returns the table name.
func (t *Table) Name() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.name
}

// Phase returns lobby|hand|showdown|ended.
func (t *Table) Phase() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.phase
}

// MaxSeats returns the configured seat count.
func (t *Table) MaxSeats() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.config.MaxSeats
}

func normalizeConfig(cfg Config) Config {
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	bb := cfg.BigBlind
	if bb == 0 {
		bb = 50
	}
	bb = clamp(bb, 2, 10_000_000)
	sb := cfg.SmallBlind
	if sb == 0 {
		sb = max(1, bb/2)
	}
	sb = clamp(sb, 1, bb)
	unit := "cost"
	if cfg.Unit == "tokens" {
		unit = "tokens"
	}
	chipValue := cfg.ChipValue
	if !(chipValue > 0) {
		if unit == "tokens" {
			chipValue = 100
		} else {
			chipValue = 0.0005
		}
	}
	currency := cfg.Currency
	if currency == "" {
		currency = "USD"
	}
	currency = truncateRunes(currency, 4)
	minSettlement := cfg.MinSettlement
	if minSettlement == 0 {
		minSettlement = 0.01
	}
	if minSettlement < 0 {
		minSettlement = 0
	}
	startingChips := cfg.StartingChips
	if startingChips == 0 {
		startingChips = bb * 100
	}
	startingChips = clamp(startingChips, bb*2, 1_000_000_000)
	maxSeats := cfg.MaxSeats
	if maxSeats == 0 {
		maxSeats = 9
	}
	maxSeats = clamp(maxSeats, 2, 9)
	return Config{
		SmallBlind:    sb,
		BigBlind:      bb,
		Ante:          clamp(cfg.Ante, 0, bb*100),
		StartingChips: startingChips,
		MaxSeats:      maxSeats,
		Unit:          unit,
		ChipValue:     chipValue,
		Currency:      currency,
		ActionSeconds: clamp(cfg.ActionSeconds, 0, 600),
		MinSettlement: minSettlement,
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (t *Table) findPlayer(id string) *Player {
	for _, p := range t.players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Player returns a copy of the seat row for id.
func (t *Table) Player(id string) (*Player, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.findPlayer(id)
	if p == nil {
		return nil, false
	}
	cp := *p
	cp.Cards = append([]int(nil), p.Cards...)
	return &cp, true
}

func (t *Table) seatOf(id string) int {
	if p := t.findPlayer(id); p != nil {
		return p.Seat
	}
	return -1
}

func (t *Table) nextSeat(fromSeat int, pred func(*Player) bool) *Player {
	seats := t.config.MaxSeats
	for i := 1; i <= seats; i++ {
		seat := (fromSeat + i + seats*2) % seats
		if seat < 0 || seat >= len(t.seats) {
			continue
		}
		id := t.seats[seat]
		if id == "" {
			continue
		}
		p := t.findPlayer(id)
		if p != nil && pred(p) {
			return p
		}
	}
	return nil
}

func (t *Table) orderFrom(seat int, pred func(*Player) bool) []*Player {
	out := make([]*Player, 0, t.config.MaxSeats)
	seats := t.config.MaxSeats
	for i := 1; i <= seats; i++ {
		s := (seat + i + seats*2) % seats
		if s < 0 || s >= len(t.seats) {
			continue
		}
		id := t.seats[s]
		if id == "" {
			continue
		}
		p := t.findPlayer(id)
		if p != nil && pred(p) {
			out = append(out, p)
		}
	}
	return out
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// AddPlayer seats a player, reclaiming a disconnected player's seat by name or
// suffixing a duplicate connected name ("Mio 2").
func (t *Table) AddPlayer(in PlayerInput) (*Player, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == "ended" {
		return nil, pokerError("ended", "牌局已经结束")
	}
	clean := truncateRunes(strings.TrimSpace(in.Name), 16)
	if clean == "" {
		clean = "玩家"
	}
	for _, p := range t.players {
		if p.Left {
			continue
		}
		if p.Name == clean && !p.Connected && p.Seat >= 0 {
			// The seat and stack belong to the key that first joined, not to
			// whoever first types the display name. Old snapshots without Pub
			// are never reclaimable.
			if p.Pub == "" || in.Pub == "" || p.Pub != in.Pub {
				continue
			}
			p.Connected = true
			p.SittingOut = false
			if in.Avatar != "" {
				p.Avatar = in.Avatar
			}
			if in.GatewayURL != "" {
				p.GatewayURL = in.GatewayURL
			}
			if in.Tailscale != nil {
				p.Tailscale = in.Tailscale
			}
			t.note("system", fmt.Sprintf("%s 重新连接", clean), nil)
			t.touch()
			return p, nil
		}
	}
	free := -1
	for i, id := range t.seats {
		if id == "" {
			free = i
			break
		}
	}
	if free < 0 {
		return nil, pokerError("full", "牌桌已满")
	}
	taken := make(map[string]struct{})
	for _, p := range t.players {
		if p.Seat >= 0 && !p.Left {
			taken[p.Name] = struct{}{}
		}
	}
	finalName := clean
	if _, ok := taken[finalName]; ok {
		n := 2
		for {
			if _, ok := taken[fmt.Sprintf("%s %d", clean, n)]; !ok {
				break
			}
			n++
		}
		finalName = fmt.Sprintf("%s %d", clean, n)
	}
	avatar := in.Avatar
	if avatar == "" {
		avatar = "fox"
	}
	p := &Player{
		ID:         in.ID,
		Name:       finalName,
		Avatar:     avatar,
		Seat:       free,
		Chips:      t.config.StartingChips,
		BuyIn:      t.config.StartingChips,
		GatewayURL: in.GatewayURL,
		Tailscale:  in.Tailscale,
		Pub:        in.Pub,
		Connected:  true,
		SittingOut: t.phase == "hand",
		Cards:      make([]int, 0),
		MayRaise:   true,
	}
	t.players = append(t.players, p)
	sort.SliceStable(t.players, func(i, j int) bool { return t.players[i].Seat < t.players[j].Seat })
	t.seats[free] = p.ID
	t.note("system", fmt.Sprintf("%s 入座（%d 筹码）", p.Name, t.config.StartingChips), nil)
	t.touch()
	return p, nil
}

// MarkConnected clears the disconnected/sitting-out state on reconnect.
func (t *Table) MarkConnected(playerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.findPlayer(playerID)
	if p == nil || p.Left {
		return
	}
	p.Connected = true
	p.SittingOut = false
	if p.Chips > 0 {
		p.Out = false
	}
	t.touch()
}

// MarkDisconnected marks a player offline; on their turn they fold.
func (t *Table) MarkDisconnected(playerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.findPlayer(playerID)
	if p == nil || p.Left {
		return
	}
	p.Connected = false
	p.SittingOut = true
	if t.phase == "hand" && !p.Folded {
		if t.toAct != nil && *t.toAct == p.ID {
			_ = t.actLocked(p.ID, Action{Type: "fold"}, true)
		} else {
			t.note("system", fmt.Sprintf("%s 断线，本手轮到时会自动弃牌", p.Name), nil)
		}
	}
	t.touch()
}

// Leave removes a player; during a hand they first fold.
func (t *Table) Leave(playerID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.leaveLocked(playerID)
	return nil
}

func (t *Table) leaveLocked(playerID string) bool {
	p := t.findPlayer(playerID)
	if p == nil {
		return false
	}
	p.Left = true
	p.Connected = false
	p.SittingOut = true
	seat := p.Seat
	if seat >= 0 && seat < len(t.seats) {
		t.seats[seat] = ""
		p.Seat = -1
	}
	if t.phase == "hand" && !p.Folded {
		p.Folded = true
		p.LastAction = "离桌"
		t.note("action", fmt.Sprintf("%s 离桌并弃牌", p.Name), new(p.ID))
		if t.toAct != nil && *t.toAct == p.ID {
			t.toAct = nil
		}
		t.progressLocked(seat)
	} else if t.phase == "lobby" || t.phase == "ended" {
		kept := t.players[:0]
		for _, x := range t.players {
			if x.ID != p.ID {
				kept = append(kept, x)
			}
		}
		t.players = kept
	}
	t.touch()
	return true
}

// Chat appends a trimmed chat line (max 200 runes).
func (t *Table) Chat(playerID, text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.findPlayer(playerID)
	if p == nil {
		return pokerError("not_seated", "不在牌桌上")
	}
	msg := truncateRunes(strings.TrimSpace(text), 200)
	if msg == "" {
		return nil
	}
	t.note("chat", fmt.Sprintf("%s：%s", p.Name, msg), new(p.ID))
	t.touch()
	return nil
}

// Rebuy adds chips and buy-in between hands.
func (t *Table) Rebuy(playerID string, amount int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == "hand" {
		return pokerError("in_hand", "一手牌进行中，不能补码")
	}
	if t.phase == "ended" {
		return pokerError("ended", "牌局已经结束")
	}
	if amount <= 0 || amount > 100_000_000 {
		return pokerError("bad_action", "补码数量不合法")
	}
	p := t.findPlayer(playerID)
	if p == nil {
		return pokerError("not_seated", "不在牌桌上")
	}
	p.Chips += amount
	p.BuyIn += amount
	p.Out = false
	p.SittingOut = false
	t.note("system", fmt.Sprintf("%s 补码 %d", p.Name, amount), nil)
	t.touch()
	return nil
}

// StartHand starts a hand with the deterministic seeded shuffle.
func (t *Table) StartHand(seed string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.startHandLocked(seed)
}

func (t *Table) startHandLocked(seed string) error {
	if t.phase == "hand" {
		return pokerError("in_hand", "上一手还没结束")
	}
	if t.phase == "ended" {
		return pokerError("ended", "牌局已经结束")
	}
	eligible := make([]*Player, 0)
	for _, p := range t.players {
		if p.Connected && !p.Left && p.Seat >= 0 && p.Chips > 0 {
			eligible = append(eligible, p)
		}
	}
	for _, p := range t.players {
		p.Out = p.Seat >= 0 && p.Chips <= 0
		if p.Connected && p.Chips > 0 {
			p.SittingOut = false
		}
	}
	if len(eligible) < 2 {
		return pokerError("not_enough", "至少需要两位有筹码的玩家")
	}
	if seed == "" {
		s, err := RandomSeed()
		if err != nil {
			return err
		}
		seed = s
	}
	isEligible := func(p *Player) bool {
		for _, e := range eligible {
			if e.ID == p.ID {
				return true
			}
		}
		return false
	}

	t.handNo++
	t.phase = "hand"
	t.street = "preflop"
	t.board = make([]int, 0)
	t.showdown = nil
	t.settlement = nil
	t.aggressor = nil
	t.handPlayers = make([]string, 0, len(eligible))
	for _, p := range eligible {
		t.handPlayers = append(t.handPlayers, p.ID)
	}
	t.deck = SeededShuffle(seed, t.handNo)
	t.deckPos = 0
	t.lastRaiseSize = t.config.BigBlind

	for _, p := range t.players {
		p.Cards = make([]int, 0)
		p.Won = 0
		p.Bet = 0
		p.Committed = 0
		p.Folded = false
		p.AllIn = false
		p.Acted = false
		p.MayRaise = true
		p.LastAction = ""
	}

	if t.dealerSeat < 0 {
		t.dealerSeat = eligible[0].Seat
	} else if next := t.nextSeat(t.dealerSeat, isEligible); next != nil {
		t.dealerSeat = next.Seat
	} else {
		t.dealerSeat = eligible[0].Seat
	}

	order := t.orderFrom(t.dealerSeat, isEligible)
	for range 2 {
		for _, p := range order {
			p.Cards = append(p.Cards, t.deck[t.deckPos])
			t.deckPos++
		}
	}

	var sb, bb *Player
	if len(eligible) == 2 {
		sb = t.findPlayer(t.seats[t.dealerSeat])
	} else {
		if first := t.orderFrom(t.dealerSeat, isEligible); len(first) > 0 {
			sb = first[0]
		}
	}
	if sb == nil {
		return pokerError("not_enough", "至少需要两位有筹码的玩家")
	}
	bb = t.nextSeat(sb.Seat, isEligible)
	if bb == nil {
		return pokerError("not_enough", "至少需要两位有筹码的玩家")
	}
	t.sbSeat = new(sb.Seat)
	t.bbSeat = new(bb.Seat)

	if t.config.Ante > 0 {
		for _, p := range eligible {
			n := min(t.config.Ante, p.Chips)
			p.Chips -= n
			p.Committed += n
			if p.Chips == 0 {
				p.AllIn = true
			}
		}
	}
	t.putInLocked(sb, min(t.config.SmallBlind, sb.Chips))
	if sb.Chips == 0 {
		sb.AllIn = true
	}
	t.putInLocked(bb, min(t.config.BigBlind, bb.Chips))
	if bb.Chips == 0 {
		bb.AllIn = true
	}

	t.currentBet = t.config.BigBlind
	t.minRaiseTo = t.currentBet + t.config.BigBlind
	if len(eligible) == 2 {
		t.toAct = new(sb.ID)
	} else if next := t.nextSeat(bb.Seat, isEligible); next != nil {
		t.toAct = new(next.ID)
	} else {
		t.toAct = new(sb.ID)
	}
	t.hand = &Hand{
		No:        t.handNo,
		Seed:      seed,
		StartedAt: time.Now().UnixMilli(),
		Actions:   make([]HandAction, 0),
	}
	t.note("deal", fmt.Sprintf("第 %d 手开始 · %s 小盲 %d · %s 大盲 %d",
		t.handNo, sb.Name, min(t.config.SmallBlind, sb.Chips), bb.Name, min(t.config.BigBlind, bb.Chips)), nil)
	t.touch()
	t.progressLocked(bb.Seat)
	return nil
}

// Act applies a player action.
func (t *Table) Act(playerID string, a Action) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.actLocked(playerID, a, false)
}

func (t *Table) actLocked(playerID string, a Action, auto bool) error {
	if t.phase != "hand" {
		return pokerError("no_hand", "现在没有进行中的手牌")
	}
	if t.toAct == nil || *t.toAct != playerID {
		return pokerError("out_of_turn", "还没轮到你")
	}
	p := t.findPlayer(playerID)
	if p == nil || p.Folded || p.AllIn {
		return pokerError("inactive", "你不能行动")
	}
	actionType := a.Type
	toCall := max(0, t.currentBet-p.Bet)
	maxTo := p.Bet + p.Chips
	var target *int

	switch actionType {
	case "fold":
		p.Folded = true
		p.LastAction = "弃牌"
		t.note("action", fmt.Sprintf("%s 弃牌", p.Name), new(p.ID))
	case "check":
		if toCall > 0 {
			return pokerError("must_call", "需要跟注 %d", toCall)
		}
		p.LastAction = "过牌"
		t.note("action", fmt.Sprintf("%s 过牌", p.Name), new(p.ID))
	case "call":
		if toCall <= 0 {
			return pokerError("nothing_to_call", "没有需要跟注的额度")
		}
		pay := min(toCall, p.Chips)
		t.putInLocked(p, pay)
		if p.Chips == 0 {
			p.LastAction = fmt.Sprintf("全下 %d", p.Bet)
		} else {
			p.LastAction = fmt.Sprintf("跟注 %d", pay)
		}
		t.note("action", fmt.Sprintf("%s %s", p.Name, p.LastAction), new(p.ID))
	case "bet", "raise", "allin":
		if actionType == "bet" && t.currentBet > 0 {
			return pokerError("use_raise", "请使用加注")
		}
		if actionType == "raise" && t.currentBet <= 0 {
			return pokerError("use_bet", "请使用下注")
		}
		if actionType == "raise" && !p.MayRaise {
			return pokerError("no_raise", "本轮不能再次加注")
		}
		if actionType == "allin" {
			target = new(maxTo)
			if maxTo > t.currentBet && !p.MayRaise {
				return pokerError("no_raise", "本轮不能再次加注")
			}
		} else {
			if a.Amount <= 0 {
				return pokerError("bad_action", "下注额不合法")
			}
			if a.Amount > maxTo {
				return pokerError("not_enough_chips", "筹码不够")
			}
			target = new(a.Amount)
		}
		if *target <= t.currentBet {
			if *target != maxTo {
				return pokerError("too_small", "至少要加到 %d", t.currentBet+1)
			}
			pay := min(toCall, p.Chips)
			t.putInLocked(p, pay)
			if p.Chips == 0 {
				p.LastAction = fmt.Sprintf("全下 %d", p.Bet)
			} else {
				p.LastAction = fmt.Sprintf("跟注 %d", pay)
			}
			t.note("action", fmt.Sprintf("%s %s", p.Name, p.LastAction), new(p.ID))
		} else {
			minTo := t.config.BigBlind
			if t.currentBet > 0 {
				minTo = t.minRaiseTo
			}
			isAllIn := *target == maxTo
			if *target < minTo && !(isAllIn && *target > t.currentBet) {
				return pokerError("too_small", "至少要加到 %d", min(minTo, maxTo))
			}
			wasOpen := t.currentBet == 0
			raiseSize := *target - t.currentBet
			full := raiseSize >= t.lastRaiseSize
			beforeActed := make([]string, 0)
			for _, x := range t.players {
				if x.Acted {
					beforeActed = append(beforeActed, x.ID)
				}
			}
			t.putInLocked(p, *target-p.Bet)
			t.currentBet = *target
			if full {
				t.lastRaiseSize = raiseSize
				t.minRaiseTo = t.currentBet + t.lastRaiseSize
				for _, x := range t.players {
					if !x.Folded && !x.AllIn {
						x.MayRaise = true
					}
					x.Acted = false
				}
			} else {
				t.minRaiseTo = t.currentBet + t.lastRaiseSize
				for _, id := range beforeActed {
					if x := t.findPlayer(id); x != nil && x.ID != p.ID {
						x.MayRaise = false
					}
				}
			}
			t.aggressor = new(p.ID)
			if isAllIn {
				suffix := ""
				if !full {
					suffix = "（短加注）"
				}
				p.LastAction = fmt.Sprintf("全下 %d%s", p.Bet, suffix)
			} else if wasOpen {
				p.LastAction = fmt.Sprintf("下注 %d", p.Bet)
			} else {
				p.LastAction = fmt.Sprintf("加注到 %d", p.Bet)
			}
			t.note("action", fmt.Sprintf("%s %s", p.Name, p.LastAction), new(p.ID))
		}
	default:
		return pokerError("bad_action", "未知动作 %s", actionType)
	}

	p.Acted = true
	if t.hand != nil {
		t.hand.Actions = append(t.hand.Actions, HandAction{
			At:     time.Now().UnixMilli(),
			ID:     p.ID,
			Type:   actionType,
			Target: target,
		})
	}
	t.touch()
	t.progressLocked(p.Seat)
	return nil
}

func (t *Table) putInLocked(p *Player, amount int) {
	n := max(0, min(amount, p.Chips))
	p.Chips -= n
	p.Bet += n
	p.Committed += n
	if p.Chips == 0 {
		p.AllIn = true
	}
}

func (t *Table) progressLocked(fromSeat int) {
	live := make([]*Player, 0)
	for _, p := range t.players {
		if containsID(t.handPlayers, p.ID) && !p.Folded {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		return
	}
	if len(live) == 1 {
		t.finishByFoldLocked(live[0])
		return
	}
	active := make([]*Player, 0)
	for _, p := range live {
		if !p.AllIn {
			active = append(active, p)
		}
	}
	pending := make([]*Player, 0)
	for _, p := range active {
		if !p.Acted || p.Bet < t.currentBet {
			pending = append(pending, p)
		}
	}
	if len(pending) == 0 {
		switch {
		case t.street == "river":
			t.finishShowdownLocked()
		case len(active) == 0:
			t.runOutLocked()
		default:
			t.advanceStreetLocked()
		}
		return
	}
	isPending := func(p *Player) bool {
		for _, q := range pending {
			if q.ID == p.ID {
				return true
			}
		}
		return false
	}
	if next := t.orderFrom(fromSeat, isPending); len(next) > 0 {
		t.toAct = new(next[0].ID)
	} else {
		t.toAct = new(pending[0].ID)
	}
	t.deadline = t.actionDeadline()
}

func (t *Table) actionDeadline() *int64 {
	if t.config.ActionSeconds <= 0 {
		return nil
	}
	d := time.Now().UnixMilli() + int64(t.config.ActionSeconds)*1000
	return &d
}

func (t *Table) advanceStreetLocked() {
	idx := streetIndex(t.street)
	if idx+1 < len(streets) {
		idx++
	}
	t.street = streets[idx]
	for _, p := range t.players {
		if !containsID(t.handPlayers, p.ID) {
			continue
		}
		p.Bet = 0
		p.Acted = false
		p.MayRaise = true
	}
	t.currentBet = 0
	t.minRaiseTo = t.config.BigBlind
	t.lastRaiseSize = t.config.BigBlind
	t.aggressor = nil
	if t.street == "flop" {
		t.dealLocked(3)
	} else {
		t.dealLocked(1)
	}
	t.note("deal", fmt.Sprintf("%s：%s", streetName(t.street), t.boardText()), nil)
	active := make([]*Player, 0)
	for _, p := range t.players {
		if containsID(t.handPlayers, p.ID) && !p.Folded && !p.AllIn {
			active = append(active, p)
		}
	}
	isActive := func(p *Player) bool {
		for _, q := range active {
			if q.ID == p.ID {
				return true
			}
		}
		return false
	}
	first := t.orderFrom(t.dealerSeat, isActive)
	if len(first) == 0 || len(active) <= 1 {
		t.runOutLocked()
		return
	}
	t.toAct = new(first[0].ID)
	t.deadline = t.actionDeadline()
}

func (t *Table) dealLocked(n int) {
	t.deckPos++ // burn
	for range n {
		if t.deckPos < len(t.deck) {
			t.board = append(t.board, t.deck[t.deckPos])
		}
		t.deckPos++
	}
}

func (t *Table) runOutLocked() {
	for len(t.board) < 5 {
		t.deckPos++
		if len(t.board) == 0 {
			for range 3 {
				if t.deckPos < len(t.deck) {
					t.board = append(t.board, t.deck[t.deckPos])
				}
				t.deckPos++
			}
		} else {
			if t.deckPos < len(t.deck) {
				t.board = append(t.board, t.deck[t.deckPos])
			}
			t.deckPos++
		}
	}
	t.street = "river"
	t.note("deal", fmt.Sprintf("发完公共牌：%s", t.boardText()), nil)
	t.finishShowdownLocked()
}

func (t *Table) boardText() string { return CardsText(t.board) }

func (t *Table) finishByFoldLocked(winner *Player) {
	potPlayers := make([]PotPlayer, 0, len(t.handPlayers))
	for _, p := range t.players {
		if containsID(t.handPlayers, p.ID) {
			potPlayers = append(potPlayers, PotPlayer{ID: p.ID, Committed: p.Committed, Folded: p.Folded})
		}
	}
	built := ComputePots(potPlayers)
	for _, refund := range built.Refunds {
		if p := t.findPlayer(refund.ID); p != nil {
			p.Chips += refund.Amount
			p.Committed -= refund.Amount
		}
	}
	total := built.Total
	winner.Chips += total
	winner.Won = total
	for _, p := range t.players {
		p.Committed = 0
	}
	t.showdown = &Showdown{
		Reason:   "fold",
		Total:    total,
		Revealed: make([]Reveal, 0),
		Pots: []ShowdownPot{{
			Amount:   total,
			Eligible: []string{winner.ID},
			Winners:  winnerChipsJSON([]PotWinner{{ID: winner.ID, Chips: total}}),
		}},
	}
	t.phase = "showdown"
	t.toAct = nil
	t.deadline = nil
	if t.hand != nil {
		t.hand.Result = &HandOutcome{Reason: "fold", Total: total, Winners: []string{winner.ID}}
	}
	t.note("result", fmt.Sprintf("%s 赢得底池 %d（其他人弃牌）", winner.Name, total), new(winner.ID))
	t.touch()
}

func winnerChipsJSON(rows []PotWinner) json.RawMessage {
	b, _ := json.Marshal(rows)
	return b
}

func stringsJSON(ids []string) json.RawMessage {
	b, _ := json.Marshal(ids)
	return b
}

func (t *Table) finishShowdownLocked() {
	if t.phase != "hand" {
		return
	}
	live := make([]*Player, 0)
	for _, p := range t.players {
		if containsID(t.handPlayers, p.ID) && !p.Folded {
			live = append(live, p)
		}
	}
	scores := make(map[string]int, len(live))
	hands := make(map[string]HandResult, len(live))
	for _, p := range live {
		cards := make([]int, 0, len(p.Cards)+len(t.board))
		cards = append(cards, p.Cards...)
		cards = append(cards, t.board...)
		if r, err := Evaluate(cards); err == nil {
			scores[p.ID] = r.Score
			hands[p.ID] = r
		}
	}
	potPlayers := make([]PotPlayer, 0)
	for _, p := range t.players {
		if containsID(t.handPlayers, p.ID) {
			potPlayers = append(potPlayers, PotPlayer{ID: p.ID, Committed: p.Committed, Folded: p.Folded})
		}
	}
	built := ComputePots(potPlayers)
	for _, r := range built.Refunds {
		if p := t.findPlayer(r.ID); p != nil {
			p.Chips += r.Amount
		}
	}
	scoreOf := func(id string) int {
		if s, ok := scores[id]; ok {
			return s
		}
		return -1
	}
	awarded := AwardPots(built.Pots, scoreOf, AwardOptions{
		DealerSeat: t.dealerSeat,
		SeatCount:  t.config.MaxSeats,
		SeatOf:     t.seatOf,
	})
	winners := make([]string, 0)
	for _, pot := range awarded {
		for _, w := range pot.Winners {
			p := t.findPlayer(w.ID)
			if p == nil {
				continue
			}
			p.Chips += w.Chips
			p.Won += w.Chips
			winners = append(winners, w.ID)
		}
	}
	for _, p := range t.players {
		p.Committed = 0
	}
	revealed := make([]Reveal, 0, len(live))
	for _, p := range live {
		hand := ""
		if h, ok := hands[p.ID]; ok {
			hand = h.NameZh
		}
		revealed = append(revealed, Reveal{
			ID:    p.ID,
			Name:  p.Name,
			Cards: cardStrings(p.Cards),
			Hand:  hand,
			Won:   p.Won,
		})
	}
	pots := make([]ShowdownPot, 0, len(awarded))
	for _, pot := range awarded {
		ids := make([]string, 0, len(pot.Winners))
		for _, w := range pot.Winners {
			ids = append(ids, w.ID)
		}
		pots = append(pots, ShowdownPot{Amount: pot.Amount, Winners: stringsJSON(ids)})
	}
	t.showdown = &Showdown{Reason: "showdown", Total: built.Total, Revealed: revealed, Pots: pots}
	t.phase = "showdown"
	t.toAct = nil
	t.deadline = nil
	uniq := make([]string, 0, len(winners))
	for _, id := range winners {
		if !containsID(uniq, id) {
			uniq = append(uniq, id)
		}
	}
	if t.hand != nil {
		t.hand.Result = &HandOutcome{Reason: "showdown", Total: built.Total, Winners: uniq}
	}
	for _, pot := range awarded {
		names := make([]string, 0, len(pot.Winners))
		for _, w := range pot.Winners {
			if p := t.findPlayer(w.ID); p != nil {
				names = append(names, p.Name)
			}
		}
		if len(names) > 0 {
			t.note("result", fmt.Sprintf("%s 赢得 %d 的底池", strings.Join(names, "、"), pot.Amount), nil)
		}
	}
	t.touch()
}

// Tick auto-folds (facing a bet) or checks a timed-out player. It reports
// whether it acted.
func (t *Table) Tick(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase != "hand" || t.deadline == nil || now.UnixMilli() < *t.deadline || t.toAct == nil {
		return false
	}
	p := t.findPlayer(*t.toAct)
	if p == nil {
		return false
	}
	actionType := "check"
	if t.currentBet-p.Bet > 0 {
		actionType = "fold"
	}
	if err := t.actLocked(p.ID, Action{Type: actionType}, true); err != nil {
		if t.phase == "hand" {
			p.Folded = true
			p.LastAction = "超时弃牌"
			t.progressLocked(p.Seat)
		}
	}
	return true
}

// EndTable enforces zero-sum settlement and builds proportional transfers.
func (t *Table) EndTable() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == "hand" {
		return pokerError("in_hand", "请先打完这一手")
	}
	if t.phase == "ended" {
		return nil
	}
	type netRow struct {
		p     *Player
		delta int
		due   int
		owed  int
	}
	rows := make([]*netRow, 0)
	for _, p := range t.players {
		if p.Seat >= 0 || p.Left {
			rows = append(rows, &netRow{p: p, delta: p.Chips - p.BuyIn})
		}
	}
	winners := make([]*netRow, 0)
	losers := make([]*netRow, 0)
	totalWin, totalLost := 0, 0
	for _, r := range rows {
		switch {
		case r.delta > 0:
			r.due = r.delta
			winners = append(winners, r)
			totalWin += r.due
		case r.delta < 0:
			r.owed = -r.delta
			losers = append(losers, r)
			totalLost += r.owed
		}
	}
	if totalWin != totalLost {
		return pokerError("not_zero_sum", "筹码不守恒（赢 %d / 输 %d），已阻止结算", totalWin, totalLost)
	}
	sort.SliceStable(winners, func(i, j int) bool { return winners[i].due > winners[j].due })
	sort.SliceStable(losers, func(i, j int) bool { return losers[i].owed > losers[j].owed })

	transfers := make([]Transfer, 0)
	wi, li := 0, 0
	for wi < len(winners) && li < len(losers) {
		w := winners[wi]
		l := losers[li]
		chips := min(w.due, l.owed)
		if chips > 0 {
			amount := round4(float64(chips) * t.config.ChipValue)
			if t.config.Unit == "tokens" {
				// Gateway key issuance takes an integer token count; record
				// that same integer so the shown debt and issued key agree.
				amount = math.Round(amount)
			}
			status := "pending"
			if amount < t.config.MinSettlement {
				status = "small"
			}
			transfers = append(transfers, Transfer{
				ID:          fmt.Sprintf("x%d", len(transfers)+1),
				From:        l.p.ID,
				FromName:    l.p.Name,
				To:          w.p.ID,
				ToName:      w.p.Name,
				AmountChips: chips,
				Amount:      amount,
				Unit:        t.config.Unit,
				Status:      status,
				GatewayURL:  l.p.GatewayURL,
			})
			w.due -= chips
			l.owed -= chips
		}
		if w.due == 0 {
			wi++
		}
		if l.owed == 0 {
			li++
		}
	}
	t.settlement = &Settlement{
		At:         time.Now().UnixMilli(),
		TotalChips: totalLost,
		Transfers:  transfers,
	}
	t.phase = "ended"
	t.toAct = nil
	t.deadline = nil
	t.note("result", fmt.Sprintf("牌局结束：%d 位赢家，%d 位输家，共 %d 笔结算",
		len(winners), len(losers), len(transfers)), nil)
	t.touch()
	return nil
}

func validSettlementKey(key string) bool {
	if len(key) < 24 || len(key) > 512 {
		return false
	}
	if !strings.HasPrefix(key, "sk-") {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func round4(n float64) float64 {
	return math.Round(n*10000) / 10000
}

// MarkIssued records a settlement key issued by the debtor.
func (t *Table) MarkIssued(transferID, actorID, key, expires, gatewayURL string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.findTransfer(transferID)
	if tr == nil {
		return pokerError("no_transfer", "没有这笔结算")
	}
	if tr.From != actorID {
		return pokerError("not_payer", "只有欠款方能签发")
	}
	if tr.Status != "pending" {
		return pokerError("bad_settlement", "这笔结算已经处理过")
	}
	if !validSettlementKey(key) {
		return pokerError("bad_action", "key 不合法")
	}
	if expires != "" {
		if _, err := time.Parse(time.RFC3339, expires); err != nil {
			return pokerError("bad_action", "key 有效期不合法")
		}
	}
	if gatewayURL != "" {
		u, err := url.Parse(gatewayURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return pokerError("bad_action", "网关地址不合法")
		}
	}
	tr.Key = new(key)
	if expires != "" {
		tr.Expires = new(expires)
	} else {
		tr.Expires = nil
	}
	if gatewayURL != "" {
		tr.GatewayURL = gatewayURL
	}
	tr.Status = "issued"
	tr.Error = nil
	t.note("result", fmt.Sprintf("%s 已签发 %v 的 key 给 %s", tr.FromName, tr.Amount, tr.ToName), nil)
	t.touch()
	return nil
}

// MarkVerified records that the payee accepted the transfer.
func (t *Table) MarkVerified(transferID, actorID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.findTransfer(transferID)
	if tr == nil {
		return pokerError("no_transfer", "没有这笔结算")
	}
	if tr.To != actorID {
		return pokerError("not_payee", "只有收款方能验证")
	}
	if tr.Status != "issued" || tr.Key == nil || *tr.Key == "" {
		return pokerError("bad_settlement", "对方还没有签发 key")
	}
	tr.Status = "verified"
	t.touch()
	return nil
}

func (t *Table) findTransfer(id string) *Transfer {
	if t.settlement == nil {
		return nil
	}
	for i := range t.settlement.Transfers {
		if t.settlement.Transfers[i].ID == id {
			return &t.settlement.Transfers[i]
		}
	}
	return nil
}

func (t *Table) note(kind, text string, playerID *string) {
	t.log = append(t.log, LogEntry{
		At:       time.Now().UnixMilli(),
		Kind:     kind,
		Text:     text,
		PlayerID: playerID,
	})
	if len(t.log) > 240 {
		t.log = t.log[len(t.log)-240:]
	}
}

func (t *Table) touch() {
	t.updatedAt = time.Now().UnixMilli()
}
