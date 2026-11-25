package game

import (
	"encoding/json"
	"sort"
	"time"
)

// LogEntry is one line of the table log.
type LogEntry struct {
	At       int64   `json:"at"`
	Kind     string  `json:"kind"`
	Text     string  `json:"text"`
	PlayerID *string `json:"playerId"`
}

// HandAction is one recorded action.
type HandAction struct {
	At     int64  `json:"at"`
	ID     string `json:"id"`
	Type   string `json:"type"`
	Target *int   `json:"target"`
}

// HandOutcome is the outcome of the hand.
type HandOutcome struct {
	Reason  string   `json:"reason"`
	Total   int      `json:"total"`
	Winners []string `json:"winners"`
}

// Hand is the current/last hand metadata.
type Hand struct {
	No        int          `json:"no"`
	Seed      string       `json:"seed"`
	StartedAt int64        `json:"startedAt"`
	Actions   []HandAction `json:"actions"`
	Result    *HandOutcome `json:"result,omitempty"`
}

// Reveal is one showdown player's revealed hand.
type Reveal struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Cards []string `json:"cards"`
	Hand  string   `json:"hand"`
	Won   int      `json:"won"`
}

// ShowdownPot is a pot in the showdown summary. Winners is raw JSON: a list of
// ids after a showdown, or [{id,chips}] after a fold win.
type ShowdownPot struct {
	Amount   int             `json:"amount"`
	Eligible []string        `json:"eligible,omitempty"`
	Winners  json.RawMessage `json:"winners"`
}

// Showdown is the showdown summary shown in the view.
type Showdown struct {
	Reason   string        `json:"reason"`
	Total    int           `json:"total"`
	Revealed []Reveal      `json:"revealed"`
	Pots     []ShowdownPot `json:"pots"`
}

// Transfer is one settlement transfer.
type Transfer struct {
	ID          string  `json:"id"`
	From        string  `json:"from"`
	FromName    string  `json:"fromName"`
	To          string  `json:"to"`
	ToName      string  `json:"toName"`
	AmountChips int     `json:"amountChips"`
	Amount      float64 `json:"amount"`
	Unit        string  `json:"unit"`
	Status      string  `json:"status"`
	GatewayURL  string  `json:"gatewayUrl"`
	Key         *string `json:"key"`
	Expires     *string `json:"expires"`
	Error       *string `json:"error"`
}

// Settlement is the end-of-table settlement.
type Settlement struct {
	At         int64      `json:"at"`
	TotalChips int        `json:"totalChips"`
	Transfers  []Transfer `json:"transfers"`
}

// AmountRange is a bet/raise range ("to" amounts).
type AmountRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Legal is the viewer's legal action set; only present on their turn.
type Legal struct {
	Fold  bool         `json:"fold"`
	Check bool         `json:"check"`
	Call  *int         `json:"call"`
	Bet   *AmountRange `json:"bet"`
	Raise *AmountRange `json:"raise"`
	AllIn int          `json:"allIn"`
}

// HandStrength is the viewer's hand summary.
type HandStrength struct {
	Label    string `json:"label"`
	Category *int   `json:"category"`
	Score    *int   `json:"score"`
}

// ViewMe is the viewer's own seat summary.
type ViewMe struct {
	ID           string        `json:"id"`
	Seat         int           `json:"seat"`
	Cards        []string      `json:"cards"`
	Legal        *Legal        `json:"legal"`
	HandStrength *HandStrength `json:"handStrength"`
}

// SeatView is one occupied seat.
type SeatView struct {
	Seat       int      `json:"seat"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Avatar     string   `json:"avatar"`
	Chips      int      `json:"chips"`
	Bet        int      `json:"bet"`
	Committed  int      `json:"committed"`
	Folded     bool     `json:"folded"`
	AllIn      bool     `json:"allIn"`
	Out        bool     `json:"out"`
	Left       bool     `json:"left"`
	SittingOut bool     `json:"sittingOut"`
	Connected  bool     `json:"connected"`
	IsDealer   bool     `json:"isDealer"`
	IsSB       bool     `json:"isSB"`
	IsBB       bool     `json:"isBB"`
	Cards      []string `json:"cards"`
	CardsCount int      `json:"cardsCount"`
	LastAction string   `json:"lastAction"`
	IsMe       bool     `json:"isMe"`
	MayRaise   bool     `json:"mayRaise"`
	TimeLeft   *int     `json:"timeLeft"`
}

// PotView is a pot in the view: only amount+eligible while the hand runs, and
// amount+eligible+winners at showdown.
type PotView struct {
	Amount   int             `json:"amount"`
	Eligible json.RawMessage `json:"eligible,omitempty"`
	Winners  json.RawMessage `json:"winners,omitempty"`
}

// View is the exact JSON of Table.viewFor in game.mjs.
type View struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Phase      string      `json:"phase"`
	HandNo     int         `json:"handNo"`
	Config     Config      `json:"config"`
	DealerSeat int         `json:"dealerSeat"`
	ToAct      *string     `json:"toAct"`
	CurrentBet int         `json:"currentBet"`
	MinRaiseTo int         `json:"minRaiseTo"`
	Pot        int         `json:"pot"`
	Pots       []PotView   `json:"pots"`
	Board      []string    `json:"board"`
	Seats      []SeatView  `json:"seats"`
	Me         *ViewMe     `json:"me"`
	Log        []LogEntry  `json:"log"`
	Showdown   *Showdown   `json:"showdown"`
	Deadline   *int64      `json:"deadline"`
	Settlement *Settlement `json:"settlement"`
}

// ViewFor builds the viewer's projection of the table.
func (t *Table) ViewFor(viewerID string, now time.Time) View {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.viewForLocked(viewerID, now.UnixMilli())
}

func (t *Table) viewForLocked(viewerID string, nowMS int64) View {
	var me *Player
	if viewerID != "" {
		me = t.findPlayer(viewerID)
	}
	revealed := make(map[string]struct{})
	if t.showdown != nil {
		for _, r := range t.showdown.Revealed {
			revealed[r.ID] = struct{}{}
		}
	}

	rows := make([]*Player, 0, len(t.players))
	for _, p := range t.players {
		if p.Seat >= 0 {
			rows = append(rows, p)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Seat < rows[j].Seat })

	seats := make([]SeatView, 0, len(rows))
	for _, p := range rows {
		_, isRevealed := revealed[p.ID]
		show := (viewerID != "" && p.ID == viewerID) || (isRevealed && !p.Folded)
		var cards []string
		if show {
			cards = cardStrings(p.Cards)
		}
		cardsCount := 0
		if t.phase == "hand" && containsID(t.handPlayers, p.ID) {
			cardsCount = len(p.Cards)
		}
		var timeLeft *int
		if t.toAct != nil && *t.toAct == p.ID && t.deadline != nil {
			ms := *t.deadline - nowMS
			sec := int((ms + 999) / 1000)
			if sec < 0 {
				sec = 0
			}
			timeLeft = new(sec)
		}
		seats = append(seats, SeatView{
			Seat:       p.Seat,
			ID:         p.ID,
			Name:       p.Name,
			Avatar:     p.Avatar,
			Chips:      p.Chips,
			Bet:        p.Bet,
			Committed:  p.Committed,
			Folded:     p.Folded,
			AllIn:      p.AllIn,
			Out:        p.Out,
			Left:       p.Left,
			SittingOut: p.SittingOut,
			Connected:  p.Connected,
			IsDealer:   p.Seat == t.dealerSeat,
			IsSB:       t.sbSeat != nil && p.Seat == *t.sbSeat,
			IsBB:       t.bbSeat != nil && p.Seat == *t.bbSeat,
			Cards:      cards,
			CardsCount: cardsCount,
			LastAction: p.LastAction,
			IsMe:       viewerID != "" && p.ID == viewerID,
			MayRaise:   p.MayRaise,
			TimeLeft:   timeLeft,
		})
	}

	pots := make([]PotView, 0)
	pot := 0
	if t.phase == "hand" {
		potPlayers := make([]PotPlayer, 0, len(t.handPlayers))
		for _, p := range t.players {
			if containsID(t.handPlayers, p.ID) {
				potPlayers = append(potPlayers, PotPlayer{ID: p.ID, Committed: p.Committed, Folded: p.Folded})
			}
		}
		built := ComputePots(potPlayers)
		for _, p := range built.Pots {
			pots = append(pots, PotView{Amount: p.Amount, Eligible: stringsJSON(p.Eligible)})
		}
		pot = built.Total
	} else if t.showdown != nil {
		for _, x := range t.showdown.Pots {
			pots = append(pots, PotView{Amount: x.Amount, Eligible: x.Winners, Winners: x.Winners})
		}
		pot = 0
	}

	var legal *Legal
	if t.phase == "hand" && t.toAct != nil && *t.toAct == viewerID && me != nil {
		legal = t.legalForLocked(me)
	}

	var strength *HandStrength
	if me != nil && len(me.Cards) == 2 {
		if len(t.board) >= 3 {
			cards := make([]int, 0, 7)
			cards = append(cards, me.Cards...)
			cards = append(cards, t.board...)
			if r, err := Evaluate(cards); err == nil {
				strength = &HandStrength{Label: r.NameZh, Category: new(r.Category), Score: new(r.Score)}
			}
		} else {
			strength = &HandStrength{Label: handStrengthLabel(me.Cards)}
		}
	}

	var meView *ViewMe
	if me != nil {
		meView = &ViewMe{
			ID:           me.ID,
			Seat:         me.Seat,
			Cards:        cardStrings(me.Cards),
			Legal:        legal,
			HandStrength: strength,
		}
	}

	logStart := max(0, len(t.log)-80)
	logCopy := append([]LogEntry(nil), t.log[logStart:]...)

	var toAct *string
	var deadline *int64
	if t.phase == "hand" {
		toAct = t.toAct
		deadline = t.deadline
	}
	var settlement *Settlement
	if t.settlement != nil {
		settlement = redactSettlement(t.settlement, viewerID)
	}

	return View{
		ID:         t.id,
		Name:       t.name,
		Phase:      t.phase,
		HandNo:     t.handNo,
		Config:     t.config,
		DealerSeat: t.dealerSeat,
		ToAct:      toAct,
		CurrentBet: t.currentBet,
		MinRaiseTo: t.minRaiseTo,
		Pot:        pot,
		Pots:       pots,
		Board:      cardStrings(t.board),
		Seats:      seats,
		Me:         meView,
		Log:        logCopy,
		Showdown:   t.showdown,
		Deadline:   deadline,
		Settlement: settlement,
	}
}

func (t *Table) legalForLocked(p *Player) *Legal {
	toCall := max(0, t.currentBet-p.Bet)
	maxTo := p.Bet + p.Chips
	legal := &Legal{
		Fold:  true,
		Check: toCall == 0,
		AllIn: p.Chips,
	}
	if toCall > 0 {
		legal.Call = new(min(toCall, p.Chips))
	}
	if t.currentBet == 0 {
		legal.Bet = &AmountRange{Min: min(t.config.BigBlind, p.Chips), Max: p.Chips}
	}
	if t.currentBet > 0 && p.MayRaise && maxTo > t.currentBet {
		legal.Raise = &AmountRange{
			Min: min(max(t.minRaiseTo, t.currentBet+1), maxTo),
			Max: maxTo,
		}
	}
	return legal
}

func redactSettlement(s *Settlement, viewerID string) *Settlement {
	cp := *s
	cp.Transfers = make([]Transfer, len(s.Transfers))
	copy(cp.Transfers, s.Transfers)
	for i := range cp.Transfers {
		if cp.Transfers[i].To != viewerID {
			cp.Transfers[i].Key = nil
		}
	}
	return &cp
}

// tableState is the JSON snapshot shape (the JS object spread).
type tableState struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	HostID        string      `json:"hostId"`
	Config        Config      `json:"config"`
	Phase         string      `json:"phase"`
	Players       []*Player   `json:"players"`
	Seats         []string    `json:"seats"`
	DealerSeat    int         `json:"dealerSeat"`
	HandNo        int         `json:"handNo"`
	Street        string      `json:"street"`
	Board         []int       `json:"board"`
	Deck          []int       `json:"deck"`
	DeckPos       int         `json:"deckPos"`
	HandPlayers   []string    `json:"handPlayers"`
	CurrentBet    int         `json:"currentBet"`
	MinRaiseTo    int         `json:"minRaiseTo"`
	LastRaiseSize int         `json:"lastRaiseSize"`
	ToAct         *string     `json:"toAct"`
	Deadline      *int64      `json:"deadline"`
	Aggressor     *string     `json:"aggressor"`
	Showdown      *Showdown   `json:"showdown"`
	Settlement    *Settlement `json:"settlement"`
	Log           []LogEntry  `json:"log"`
	Hand          *Hand       `json:"hand"`
	CreatedAt     int64       `json:"createdAt"`
	UpdatedAt     int64       `json:"updatedAt"`
	SBSeat        *int        `json:"sbSeat,omitempty"`
	BBSeat        *int        `json:"bbSeat,omitempty"`
}

// Snapshot serializes the whole table for persistence.
func (t *Table) Snapshot() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := tableState{
		ID:            t.id,
		Name:          t.name,
		HostID:        t.hostID,
		Config:        t.config,
		Phase:         t.phase,
		Players:       t.players,
		Seats:         t.seats,
		DealerSeat:    t.dealerSeat,
		HandNo:        t.handNo,
		Street:        t.street,
		Board:         t.board,
		Deck:          t.deck,
		DeckPos:       t.deckPos,
		HandPlayers:   t.handPlayers,
		CurrentBet:    t.currentBet,
		MinRaiseTo:    t.minRaiseTo,
		LastRaiseSize: t.lastRaiseSize,
		ToAct:         t.toAct,
		Deadline:      t.deadline,
		Aggressor:     t.aggressor,
		Showdown:      t.showdown,
		Settlement:    t.settlement,
		Log:           t.log,
		Hand:          t.hand,
		CreatedAt:     t.createdAt,
		UpdatedAt:     t.updatedAt,
		SBSeat:        t.sbSeat,
		BBSeat:        t.bbSeat,
	}
	return json.Marshal(state)
}

// Restore rebuilds a table from Snapshot output.
func Restore(data []byte) (*Table, error) {
	var state tableState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	t := &Table{
		id:            state.ID,
		name:          state.Name,
		hostID:        state.HostID,
		config:        normalizeConfig(state.Config),
		phase:         state.Phase,
		players:       state.Players,
		dealerSeat:    state.DealerSeat,
		handNo:        state.HandNo,
		street:        state.Street,
		board:         state.Board,
		deck:          state.Deck,
		deckPos:       state.DeckPos,
		handPlayers:   state.HandPlayers,
		currentBet:    state.CurrentBet,
		minRaiseTo:    state.MinRaiseTo,
		lastRaiseSize: state.LastRaiseSize,
		toAct:         state.ToAct,
		deadline:      state.Deadline,
		aggressor:     state.Aggressor,
		showdown:      state.Showdown,
		settlement:    state.Settlement,
		log:           state.Log,
		hand:          state.Hand,
		createdAt:     state.CreatedAt,
		updatedAt:     state.UpdatedAt,
		sbSeat:        state.SBSeat,
		bbSeat:        state.BBSeat,
	}
	if t.players == nil {
		t.players = make([]*Player, 0)
	}
	if t.board == nil {
		t.board = make([]int, 0)
	}
	if t.log == nil {
		t.log = make([]LogEntry, 0)
	}
	if t.handPlayers == nil {
		t.handPlayers = make([]string, 0)
	}
	t.seats = make([]string, t.config.MaxSeats)
	for _, p := range t.players {
		if p == nil {
			continue
		}
		if p.Seat >= 0 && p.Seat < len(t.seats) {
			t.seats[p.Seat] = p.ID
		}
	}
	if t.street == "" {
		t.street = "preflop"
	}
	if t.lastRaiseSize == 0 {
		t.lastRaiseSize = t.config.BigBlind
	}
	return t, nil
}
