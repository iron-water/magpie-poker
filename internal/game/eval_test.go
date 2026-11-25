package game

import (
	"sort"
	"strings"
	"testing"
)

func mustCards(t *testing.T, text string) []int {
	t.Helper()
	fields := strings.Fields(text)
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		c, err := ParseCard(f)
		if err != nil {
			t.Fatalf("bad card %q: %v", f, err)
		}
		out = append(out, c)
	}
	return out
}

func evaluateCards(t *testing.T, text string) HandResult {
	t.Helper()
	r, err := Evaluate(mustCards(t, text))
	if err != nil {
		t.Fatalf("Evaluate(%q): %v", text, err)
	}
	return r
}

func subsetOf(t *testing.T, hand []int, best []int) {
	t.Helper()
	pool := append([]int(nil), hand...)
	sort.Ints(pool)
	got := append([]int(nil), best...)
	sort.Ints(got)
	i := 0
	for _, c := range got {
		for i < len(pool) && pool[i] != c {
			i++
		}
		if i >= len(pool) {
			t.Fatalf("best card %d not in hand %v", c, hand)
		}
		i++
	}
}

func checkShape(t *testing.T, text string) HandResult {
	t.Helper()
	r := evaluateCards(t, text)
	hand := mustCards(t, text)
	if len(r.Best) != 5 {
		t.Fatalf("best length %d", len(r.Best))
	}
	seen := map[int]bool{}
	for _, c := range r.Best {
		if c < 0 || c > 51 || seen[c] {
			t.Fatalf("bad best card %d", c)
		}
		seen[c] = true
	}
	subsetOf(t, hand, r.Best)
	replay := evaluateCardsFromInts(t, r.Best)
	if replay.Score != r.Score {
		t.Fatalf("best combination rescored %d != %d", replay.Score, r.Score)
	}
	if r.Score < 0 || r.Name == "" || r.NameZh == "" {
		t.Fatalf("bad shape: %+v", r)
	}
	return r
}

func evaluateCardsFromInts(t *testing.T, cards []int) HandResult {
	t.Helper()
	r, err := Evaluate(cards)
	if err != nil {
		t.Fatalf("Evaluate(%v): %v", cards, err)
	}
	return r
}

func TestEveryCategory(t *testing.T) {
	cases := []struct {
		cards    string
		category int
		name     string
		nameZh   string
	}{
		{"2c 4d 7h 9s Jc", HighCard, "High card J", "J 高牌"},
		{"As Ah 7d 4c 2s", Pair, "Pair of A", "一对 A"},
		{"As Ah Kd Kc 3s", TwoPair, "Two pair, A and K", "两对 A 与 K"},
		{"7s 7h 7d Kc 2s", Trips, "Three of a kind, 7", "三条 7"},
		{"9s 8h 7d 6c 5s", Straight, "Straight, 9 high", "9 高顺子"},
		{"As Ks 9s 5s 3s", Flush, "Flush, A high", "A 高同花"},
		{"Ks Kh Kd 4c 4s", FullHouse, "Full house, K over 4", "K 葫芦"},
		{"Qs Qh Qd Qc 8s", Quads, "Four of a kind, Q", "四条 Q"},
		{"9h 8h 7h 6h 5h", StraightFlush, "Straight flush, 9 high", "同花顺 9 高"},
	}
	for _, c := range cases {
		r := checkShape(t, c.cards)
		if r.Category != c.category || r.Name != c.name || r.NameZh != c.nameZh {
			t.Fatalf("%s: got category %d %q/%q", c.cards, r.Category, r.Name, r.NameZh)
		}
	}
}

func TestWheelAndBroadway(t *testing.T) {
	wheel := checkShape(t, "As 2h 3d 4c 5s")
	if wheel.Category != Straight || wheel.Ranks != [5]int{5, 0, 0, 0, 0} || wheel.NameZh != "5 高顺子" {
		t.Fatalf("wheel: %+v", wheel)
	}
	broadway := checkShape(t, "Ts Jd Qh Kc As")
	if broadway.Category != Straight || broadway.Ranks != [5]int{14, 0, 0, 0, 0} {
		t.Fatalf("broadway: %+v", broadway)
	}
	if CompareScores(wheel.Score, broadway.Score) != -1 {
		t.Fatal("wheel should lose to broadway")
	}
	six := evaluateCards(t, "As 2h 3d 4c 5s 6h")
	if six.Category != Straight || six.Ranks[0] != 6 {
		t.Fatalf("6-high straight: %+v", six)
	}
	seven := evaluateCards(t, "2h 3d 4c 5s 6h 7c")
	if seven.Ranks[0] != 7 {
		t.Fatalf("7-high straight: %+v", seven)
	}
	wheelSF := checkShape(t, "Ah 2h 3h 4h 5h")
	if wheelSF.Category != StraightFlush || wheelSF.Ranks != [5]int{5, 0, 0, 0, 0} {
		t.Fatalf("wheel straight flush: %+v", wheelSF)
	}
}

func TestKickerOrdering(t *testing.T) {
	higher := [][2]string{
		{"As Ah Kd 7c 2s", "As Ah Qd 7c 2s"},
		{"As Ah Kd Qc 2s", "As Ah Kd Jc 2s"},
		{"7s 7h 7d Ac 2s", "7s 7h 7d Kc 2s"},
		{"As Ah Kd Kc 3s", "As Ah Kd Kc 2s"},
		{"As Ks 9s 5s 3s", "As Ks 9s 5s 2s"},
		{"As Kd 9h 7c 3s", "As Kd 9h 7c 2s"},
	}
	for _, pair := range higher {
		a := evaluateCards(t, pair[0])
		b := evaluateCards(t, pair[1])
		if CompareScores(a.Score, b.Score) != 1 {
			t.Fatalf("%q should beat %q", pair[0], pair[1])
		}
	}
}

func TestFullHouseBeatsFlushAndStraight(t *testing.T) {
	boat := evaluateCards(t, "2s 2h 2d 3c 3s")
	flush := evaluateCards(t, "As Ks Qs Js 9s")
	if boat.Category != FullHouse || flush.Category != Flush {
		t.Fatalf("categories: %d %d", boat.Category, flush.Category)
	}
	if CompareScores(boat.Score, flush.Score) != 1 {
		t.Fatal("full house should beat flush")
	}
	straight := evaluateCards(t, "9h 8d 7c 6s 5h")
	if CompareScores(flush.Score, straight.Score) != 1 {
		t.Fatal("flush should beat straight")
	}
	seven := checkShape(t, "9s 9h 9d 4c 4s Kd 2c")
	if seven.Category != FullHouse || seven.Ranks[0] != 9 || seven.Ranks[1] != 4 {
		t.Fatalf("seven-card boat: %+v", seven)
	}
}

func TestQuadsWithKicker(t *testing.T) {
	a := checkShape(t, "2s 2h 2d 2c As")
	k := checkShape(t, "2s 2h 2d 2c Ks")
	if a.Category != Quads || a.Ranks != [5]int{2, 14, 0, 0, 0} {
		t.Fatalf("quads aces kicker: %+v", a)
	}
	if CompareScores(a.Score, k.Score) != 1 {
		t.Fatal("ace kicker should win")
	}
	seven := checkShape(t, "2s 2h 2d 2c As Ks Qs")
	if seven.Category != Quads || seven.Ranks != [5]int{2, 14, 0, 0, 0} {
		t.Fatalf("seven-card quads: %+v", seven)
	}
}

func TestSplitPotEqualScores(t *testing.T) {
	a := evaluateCards(t, "As Kd Qh Jc 9s")
	b := evaluateCards(t, "Ah Ks Qd Js 9c")
	if a.Score != b.Score || CompareScores(a.Score, b.Score) != 0 || CompareScores(b.Score, a.Score) != 0 {
		t.Fatal("equivalent hands should tie")
	}
	if CompareScores(1, 2) != -1 || CompareScores(2, 1) != 1 || CompareScores(7, 7) != 0 {
		t.Fatal("CompareScores")
	}
	flushA := evaluateCards(t, "As Ks Qs Js 9s")
	flushB := evaluateCards(t, "Ah Kh Qh Jh 9h")
	if CompareScores(flushA.Score, flushB.Score) != 0 {
		t.Fatal("same-rank flushes should tie")
	}
}

func TestSevenCardBoard(t *testing.T) {
	r := checkShape(t, "As Kd Qh Jc Ts")
	if r.Category != Straight || r.Ranks != [5]int{14, 0, 0, 0, 0} {
		t.Fatalf("broadway: %+v", r)
	}
	turned := checkShape(t, "9h 8h 7h Th 6h")
	if turned.Category != StraightFlush || turned.Ranks != [5]int{10, 0, 0, 0, 0} {
		t.Fatalf("turned straight flush: %+v", turned)
	}
	boardPair := checkShape(t, "As Ah 7d 2c 3s 9h Kd")
	if boardPair.Category != Pair || boardPair.Ranks != [5]int{14, 13, 9, 7, 0} {
		t.Fatalf("board pair: %+v", boardPair)
	}
}

func TestInputValidation(t *testing.T) {
	cases := []struct {
		cards []int
		want  string
	}{
		{mustCards(t, "As As Kd Qh Jc"), "duplicate card: As"},
		{mustCards(t, "As Ks Qd Jh"), "need 5 to 7 cards, got 4"},
		{mustCards(t, "As Ks Qd Jh Tc 9s 8h 7d"), "need 5 to 7 cards, got 8"},
		{nil, "need 5 to 7 cards, got 0"},
		{[]int{0, 1, 2, 3, 52}, "bad card: 52"},
		{[]int{0, 1, 2, 3, -1}, "bad card: -1"},
		{[]int{0, 1, 2, 3, 15, 15}, "duplicate card"},
	}
	for _, c := range cases {
		if _, err := Evaluate(c.cards); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Evaluate(%v) error = %v, want %q", c.cards, err, c.want)
		}
	}
}

func TestScoreFormatAndCategoryName(t *testing.T) {
	r := evaluateCards(t, "As Ah Kd Kc 3s")
	want := TwoPair<<20 | 14<<16 | 13<<12 | 3<<8
	if r.Score != want || r.Ranks != [5]int{14, 13, 3, 0, 0} {
		t.Fatalf("score %d ranks %v", r.Score, r.Ranks)
	}
	if label, err := CategoryName(Straight, []int{5, 0, 0, 0, 0}); err != nil || label.En != "Straight, 5 high" || label.Zh != "5 高顺子" {
		t.Fatalf("CategoryName straight: %+v %v", label, err)
	}
	if label, err := CategoryName(StraightFlush, []int{9}); err != nil || label.En != "Straight flush, 9 high" || label.Zh != "同花顺 9 高" {
		t.Fatalf("CategoryName straight flush: %+v %v", label, err)
	}
	a := evaluateCards(t, "As Ks Qs Js Ts")
	b := evaluateCardsFromInts(t, []int{48, 44, 40, 36, 32})
	if a.Score != b.Score {
		t.Fatal("rank-encoded cards should match")
	}
}

// naiveScore is an independent rank-bitmask/group-by-count evaluator.
func naiveScore(cards []int) (score, category int) {
	ranks := make([]int, len(cards))
	for i, c := range cards {
		ranks[i] = RankOf(c)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ranks)))
	flush := true
	for _, c := range cards[1:] {
		if SuitOf(c) != SuitOf(cards[0]) {
			flush = false
			break
		}
	}
	mask := 0
	for _, r := range ranks {
		mask |= 1 << r
	}
	has := func(r int) bool { return mask&(1<<r) != 0 }
	straightHigh := 0
	for high := 14; high >= 6 && straightHigh == 0; high-- {
		if has(high) && has(high-1) && has(high-2) && has(high-3) && has(high-4) {
			straightHigh = high
		}
	}
	if straightHigh == 0 && has(14) && has(5) && has(4) && has(3) && has(2) {
		straightHigh = 5
	}
	counts := map[int]int{}
	for _, r := range ranks {
		counts[r]++
	}
	of := func(n int) []int {
		out := make([]int, 0)
		for r, c := range counts {
			if c == n {
				out = append(out, r)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(out)))
		return out
	}
	quads, trips, pairs, singles := of(4), of(3), of(2), of(1)
	var sig []int
	switch {
	case straightHigh != 0 && flush:
		category, sig = StraightFlush, []int{straightHigh}
	case len(quads) > 0:
		category, sig = Quads, []int{quads[0], singles[0]}
	case len(trips) > 0 && len(pairs) > 0:
		category, sig = FullHouse, []int{trips[0], pairs[0]}
	case flush:
		category, sig = Flush, ranks[:5]
	case straightHigh != 0:
		category, sig = Straight, []int{straightHigh}
	case len(trips) > 0:
		category, sig = Trips, []int{trips[0], singles[0], singles[1]}
	case len(pairs) >= 2:
		category, sig = TwoPair, []int{pairs[0], pairs[1], singles[0]}
	case len(pairs) == 1:
		category, sig = Pair, []int{pairs[0], singles[0], singles[1], singles[2]}
	default:
		category, sig = HighCard, ranks[:5]
	}
	full := make([]int, 5)
	copy(full, sig)
	shifts := []int{16, 12, 8, 4, 0}
	score = category << 20
	for i := range 5 {
		score += full[i] << shifts[i]
	}
	return score, category
}

func naiveBest(cards []int) (score, category int) {
	n := len(cards)
	best := -1
	for i := range n - 4 {
		for j := i + 1; j < n-3; j++ {
			for k := j + 1; k < n-2; k++ {
				for l := k + 1; l < n-1; l++ {
					for m := l + 1; m < n; m++ {
						s, c := naiveScore([]int{cards[i], cards[j], cards[k], cards[l], cards[m]})
						if s > best {
							best, score, category = s, s, c
						}
					}
				}
			}
		}
	}
	return score, category
}

func TestRandomCrossCheckAgainstNaive(t *testing.T) {
	for i := range 10000 {
		deck := SeededShuffle("eval-cross-check", i)
		hand := deck[:7]
		mine, err := Evaluate(hand)
		if err != nil {
			t.Fatalf("hand %d: %v", i, err)
		}
		theirScore, theirCategory := naiveBest(hand)
		if mine.Score != theirScore || mine.Category != theirCategory {
			t.Fatalf("hand %d %v: got %d/%d want %d/%d", i, hand, mine.Score, mine.Category, theirScore, theirCategory)
		}
		if replay, err := Evaluate(mine.Best); err != nil || replay.Score != mine.Score {
			t.Fatalf("hand %d best replay: %v %v", i, replay.Score, err)
		}
	}
}

func TestRandomCrossCheckFiveAndSix(t *testing.T) {
	for i := range 2000 {
		deck := SeededShuffle("eval-short", i)
		size := 5
		if i%2 == 1 {
			size = 6
		}
		hand := deck[:size]
		mine, err := Evaluate(hand)
		if err != nil {
			t.Fatalf("%d cards hand %d: %v", size, i, err)
		}
		theirScore, theirCategory := naiveBest(hand)
		if mine.Score != theirScore || mine.Category != theirCategory {
			t.Fatalf("%d cards hand %d: got %d/%d want %d/%d", size, i, mine.Score, mine.Category, theirScore, theirCategory)
		}
	}
}
