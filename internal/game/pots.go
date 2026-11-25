package game

import "sort"

// PotPlayer is the pot-construction input for one player.
type PotPlayer struct {
	ID        string `json:"id"`
	Committed int    `json:"committed"`
	Folded    bool   `json:"folded"`
}

// Pot is a main or side pot.
type Pot struct {
	Amount   int      `json:"amount"`
	Eligible []string `json:"eligible"`
}

// Refund is an uncalled bet returned before pots are built.
type Refund struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

// PotResult is ComputePots' output.
type PotResult struct {
	Pots    []Pot    `json:"pots"`
	Refunds []Refund `json:"refunds"`
	Total   int      `json:"total"`
}

// PotWinner is one share of a pot.
type PotWinner struct {
	ID    string `json:"id"`
	Chips int    `json:"chips"`
}

// PotAward is a pot after awarding.
type PotAward struct {
	Amount   int         `json:"amount"`
	Eligible []string    `json:"eligible"`
	Winners  []PotWinner `json:"winners"`
}

// AwardOptions provides seat geometry for odd-chip assignment.
type AwardOptions struct {
	DealerSeat int
	SeatCount  int
	SeatOf     func(id string) int
}

type potRow struct {
	id        string
	committed int
	folded    bool
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, x := range b {
		if _, ok := set[x]; !ok {
			return false
		}
	}
	return true
}

// ComputePots builds main/side pots, refunding an uncalled bet first.
func ComputePots(players []PotPlayer) PotResult {
	rows := make([]potRow, 0, len(players))
	for _, p := range players {
		committed := p.Committed
		if committed < 0 {
			committed = 0
		}
		rows = append(rows, potRow{id: p.ID, committed: committed, folded: p.Folded})
	}
	byID := make(map[string]int, len(rows))
	for i, r := range rows {
		byID[r.id] = i
	}
	refunds := make([]Refund, 0)

	sorted := make([]potRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].committed > sorted[j].committed })
	top, second := 0, 0
	if len(sorted) > 0 {
		top = sorted[0].committed
	}
	if len(sorted) > 1 {
		second = sorted[1].committed
	}
	if top > second && len(sorted) > 0 {
		i := byID[sorted[0].id]
		back := top - second
		rows[i].committed -= back
		refunds = append(refunds, Refund{ID: rows[i].id, Amount: back})
	}

	levelSet := make(map[int]struct{})
	for _, r := range rows {
		if r.committed > 0 {
			levelSet[r.committed] = struct{}{}
		}
	}
	levels := make([]int, 0, len(levelSet))
	for level := range levelSet {
		levels = append(levels, level)
	}
	sort.Ints(levels)

	pots := make([]Pot, 0)
	prev := 0
	for _, level := range levels {
		amount := 0
		for _, r := range rows {
			amount += max(0, min(r.committed, level)-prev)
		}
		eligible := make([]string, 0)
		for _, r := range rows {
			if !r.folded && r.committed >= level {
				eligible = append(eligible, r.id)
			}
		}
		if amount > 0 {
			if len(pots) > 0 && sameSet(pots[len(pots)-1].Eligible, eligible) {
				pots[len(pots)-1].Amount += amount
			} else {
				pots = append(pots, Pot{Amount: amount, Eligible: eligible})
			}
		}
		prev = level
	}
	total := 0
	for _, p := range pots {
		total += p.Amount
	}
	return PotResult{Pots: pots, Refunds: refunds, Total: total}
}

// AwardPots splits every pot among its best-scoring eligible players. Odd
// chips go to the first winner left of the dealer.
func AwardPots(pots []Pot, scoreOf func(id string) int, opts AwardOptions) []PotAward {
	out := make([]PotAward, 0, len(pots))
	for _, pot := range pots {
		if len(pot.Eligible) == 0 {
			out = append(out, PotAward{Amount: pot.Amount, Eligible: pot.Eligible, Winners: make([]PotWinner, 0)})
			continue
		}
		best := scoreOf(pot.Eligible[0])
		for _, id := range pot.Eligible[1:] {
			if s := scoreOf(id); s > best {
				best = s
			}
		}
		winners := make([]string, 0)
		for _, id := range pot.Eligible {
			if scoreOf(id) == best {
				winners = append(winners, id)
			}
		}
		sort.SliceStable(winners, func(i, j int) bool {
			da := (opts.SeatOf(winners[i]) - opts.DealerSeat - 1 + opts.SeatCount*2) % opts.SeatCount
			db := (opts.SeatOf(winners[j]) - opts.DealerSeat - 1 + opts.SeatCount*2) % opts.SeatCount
			return da < db
		})
		share := pot.Amount / len(winners)
		odd := pot.Amount - share*len(winners)
		rows := make([]PotWinner, 0, len(winners))
		for _, id := range winners {
			chips := share
			if odd > 0 {
				chips++
				odd--
			}
			rows = append(rows, PotWinner{ID: id, Chips: chips})
		}
		out = append(out, PotAward{Amount: pot.Amount, Eligible: pot.Eligible, Winners: rows})
	}
	return out
}
