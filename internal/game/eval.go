package game

import (
	"fmt"
	"sort"
	"strings"
)

// Hand categories, same values as eval.mjs.
const (
	HighCard = iota
	Pair
	TwoPair
	Trips
	Straight
	Flush
	FullHouse
	Quads
	StraightFlush
)

// CategoryLabel is the bilingual hand name returned by CategoryName.
type CategoryLabel struct {
	En string
	Zh string
}

// HandResult is the evaluated value of 5 to 7 cards.
type HandResult struct {
	Score    int
	Category int
	Name     string
	NameZh   string
	Ranks    [5]int
	Best     []int
}

// CompareScores returns -1, 0 or 1 in the manner of compareScores.
func CompareScores(a, b int) int {
	switch {
	case a == b:
		return 0
	case a < b:
		return -1
	default:
		return 1
	}
}

func pack(category, r1, r2, r3, r4, r5 int) int {
	return category<<20 | r1<<16 | r2<<12 | r3<<8 | r4<<4 | r5
}

func score5(a, b, c, d, e int) int {
	var count [15]int
	ranks := [5]int{a, b, c, d, e}
	for _, card := range ranks {
		count[RankOf(card)]++
	}
	suit := SuitOf(a)
	flush := SuitOf(b) == suit && SuitOf(c) == suit && SuitOf(d) == suit && SuitOf(e) == suit

	var desc [5]int
	n := 0
	quad, trips, hiPair, loPair := 0, 0, 0, 0
	for r := 14; r >= 2; r-- {
		k := count[r]
		if k == 0 {
			continue
		}
		desc[n] = r
		n++
		switch k {
		case 4:
			quad = r
		case 3:
			trips = r
		case 2:
			if hiPair != 0 {
				loPair = r
			} else {
				hiPair = r
			}
		}
	}

	if n == 5 {
		high := 0
		if desc[0]-desc[4] == 4 {
			high = desc[0]
		} else if desc[0] == 14 && desc[1] == 5 && desc[4] == 2 {
			high = 5
		}
		if high != 0 {
			if flush {
				return pack(StraightFlush, high, 0, 0, 0, 0)
			}
			return pack(Straight, high, 0, 0, 0, 0)
		}
	}
	if flush {
		return pack(Flush, desc[0], desc[1], desc[2], desc[3], desc[4])
	}
	if quad != 0 {
		kicker := desc[0]
		if desc[0] == quad {
			kicker = desc[1]
		}
		return pack(Quads, quad, kicker, 0, 0, 0)
	}
	if trips != 0 {
		if hiPair != 0 {
			return pack(FullHouse, trips, hiPair, 0, 0, 0)
		}
		k1, k2 := 0, 0
		for i := range n {
			r := desc[i]
			if r == trips {
				continue
			}
			if k1 == 0 {
				k1 = r
			} else {
				k2 = r
				break
			}
		}
		return pack(Trips, trips, k1, k2, 0, 0)
	}
	if loPair != 0 {
		kicker := 0
		for i := range n {
			if desc[i] != hiPair && desc[i] != loPair {
				kicker = desc[i]
				break
			}
		}
		return pack(TwoPair, hiPair, loPair, kicker, 0, 0)
	}
	if hiPair != 0 {
		k1, k2, k3 := 0, 0, 0
		for i := range n {
			r := desc[i]
			if r == hiPair {
				continue
			}
			if k1 == 0 {
				k1 = r
			} else if k2 == 0 {
				k2 = r
			} else {
				k3 = r
				break
			}
		}
		return pack(Pair, hiPair, k1, k2, k3, 0)
	}
	return pack(HighCard, desc[0], desc[1], desc[2], desc[3], desc[4])
}

func normalizeCards(cards []int) ([]int, error) {
	n := len(cards)
	if n < 5 || n > 7 {
		return nil, fmt.Errorf("need 5 to 7 cards, got %d", n)
	}
	out := make([]int, n)
	var seen [52]bool
	for i, card := range cards {
		if card < 0 || card > 51 {
			return nil, fmt.Errorf("bad card: %d", card)
		}
		if seen[card] {
			s, _ := FormatCard(card)
			return nil, fmt.Errorf("duplicate card: %s", s)
		}
		seen[card] = true
		out[i] = card
	}
	return out, nil
}

// Evaluate scores the best five-card hand from 5 to 7 cards.
func Evaluate(cards []int) (HandResult, error) {
	hand, err := normalizeCards(cards)
	if err != nil {
		return HandResult{}, err
	}
	n := len(hand)
	idx := [5]int{0, 1, 2, 3, 4}
	bestScore := -1
	var bestCombo [5]int
	for {
		s := score5(hand[idx[0]], hand[idx[1]], hand[idx[2]], hand[idx[3]], hand[idx[4]])
		if s > bestScore {
			bestScore = s
			bestCombo = idx
		}
		i := 4
		for i >= 0 && idx[i] == n-5+i {
			i--
		}
		if i < 0 {
			break
		}
		idx[i]++
		for j := i + 1; j < 5; j++ {
			idx[j] = idx[j-1] + 1
		}
	}
	category := bestScore >> 20
	ranks := [5]int{
		(bestScore >> 16) & 15,
		(bestScore >> 12) & 15,
		(bestScore >> 8) & 15,
		(bestScore >> 4) & 15,
		bestScore & 15,
	}
	names, err := CategoryName(category, ranks[:])
	if err != nil {
		return HandResult{}, err
	}
	best := make([]int, 5)
	for i, j := range bestCombo {
		best[i] = hand[j]
	}
	sort.Ints(best)
	return HandResult{
		Score:    bestScore,
		Category: category,
		Name:     names.En,
		NameZh:   names.Zh,
		Ranks:    ranks,
		Best:     best,
	}, nil
}

func rankLabel(rank int) string {
	if rank < 2 || rank > 14 {
		return "?"
	}
	return string(Ranks[rank-2])
}

// CategoryName returns the English and Chinese names of a scored category.
func CategoryName(category int, ranks []int) (CategoryLabel, error) {
	if category < HighCard || category > StraightFlush {
		return CategoryLabel{}, fmt.Errorf("bad category: %d", category)
	}
	r := func(i int) string {
		if i < 0 || i >= len(ranks) {
			return "?"
		}
		return rankLabel(ranks[i])
	}
	switch category {
	case HighCard:
		return CategoryLabel{En: fmt.Sprintf("High card %s", r(0)), Zh: fmt.Sprintf("%s 高牌", r(0))}, nil
	case Pair:
		return CategoryLabel{En: fmt.Sprintf("Pair of %s", r(0)), Zh: fmt.Sprintf("一对 %s", r(0))}, nil
	case TwoPair:
		return CategoryLabel{En: fmt.Sprintf("Two pair, %s and %s", r(0), r(1)), Zh: fmt.Sprintf("两对 %s 与 %s", r(0), r(1))}, nil
	case Trips:
		return CategoryLabel{En: fmt.Sprintf("Three of a kind, %s", r(0)), Zh: fmt.Sprintf("三条 %s", r(0))}, nil
	case Straight:
		return CategoryLabel{En: fmt.Sprintf("Straight, %s high", r(0)), Zh: fmt.Sprintf("%s 高顺子", r(0))}, nil
	case Flush:
		return CategoryLabel{En: fmt.Sprintf("Flush, %s high", r(0)), Zh: fmt.Sprintf("%s 高同花", r(0))}, nil
	case FullHouse:
		return CategoryLabel{En: fmt.Sprintf("Full house, %s over %s", r(0), r(1)), Zh: fmt.Sprintf("%s 葫芦", r(0))}, nil
	case Quads:
		return CategoryLabel{En: fmt.Sprintf("Four of a kind, %s", r(0)), Zh: fmt.Sprintf("四条 %s", r(0))}, nil
	default:
		return CategoryLabel{En: fmt.Sprintf("Straight flush, %s high", r(0)), Zh: fmt.Sprintf("同花顺 %s 高", r(0))}, nil
	}
}

// handStrengthLabel is the preflop label used by the view (no board yet).
func handStrengthLabel(cards []int) string {
	if len(cards) < 2 {
		return ""
	}
	a, b := cards[0], cards[1]
	if RankOf(b) > RankOf(a) {
		a, b = b, a
	}
	ra := string(Ranks[RankOf(a)-2])
	rb := string(Ranks[RankOf(b)-2])
	suited := SuitOf(a) == SuitOf(b)
	if RankOf(a) == RankOf(b) {
		return "对 " + ra
	}
	var sb strings.Builder
	sb.WriteString(ra)
	sb.WriteString(rb)
	sb.WriteByte(' ')
	if suited {
		sb.WriteString("同花")
	} else {
		sb.WriteString("杂色")
	}
	gap := RankOf(a) - RankOf(b)
	if gap == 1 {
		sb.WriteString(" · 连牌")
	}
	return sb.String()
}
