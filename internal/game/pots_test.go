package game

import (
	"reflect"
	"testing"
)

func potPlayers(specs ...[3]any) []PotPlayer {
	out := make([]PotPlayer, 0, len(specs))
	for _, s := range specs {
		out = append(out, PotPlayer{
			ID:        s[0].(string),
			Committed: s[1].(int),
			Folded:    s[2].(bool),
		})
	}
	return out
}

func TestMatchedBetsMakeOnePot(t *testing.T) {
	r := ComputePots([]PotPlayer{{ID: "p0", Committed: 20}, {ID: "p1", Committed: 20}})
	if !reflect.DeepEqual(r.Pots, []Pot{{Amount: 40, Eligible: []string{"p0", "p1"}}}) {
		t.Fatalf("pots %#v", r.Pots)
	}
	if r.Total != 40 || len(r.Refunds) != 0 {
		t.Fatalf("total %d refunds %#v", r.Total, r.Refunds)
	}
}

func TestUncalledBetRefunded(t *testing.T) {
	r := ComputePots(potPlayers(
		[3]any{"p0", 100, false},
		[3]any{"p1", 30, true},
		[3]any{"p2", 30, false},
	))
	if !reflect.DeepEqual(r.Refunds, []Refund{{ID: "p0", Amount: 70}}) {
		t.Fatalf("refunds %#v", r.Refunds)
	}
	if !reflect.DeepEqual(r.Pots, []Pot{{Amount: 90, Eligible: []string{"p0", "p2"}}}) {
		t.Fatalf("pots %#v", r.Pots)
	}
	if r.Total != 90 {
		t.Fatalf("total %d", r.Total)
	}
}

func TestSidePotsByCommitmentLevel(t *testing.T) {
	r := ComputePots(potPlayers(
		[3]any{"p0", 100, false},
		[3]any{"p1", 60, false},
		[3]any{"p2", 100, false},
	))
	want := []Pot{
		{Amount: 180, Eligible: []string{"p0", "p1", "p2"}},
		{Amount: 80, Eligible: []string{"p0", "p2"}},
	}
	if !reflect.DeepEqual(r.Pots, want) {
		t.Fatalf("pots %#v", r.Pots)
	}
	if r.Total != 260 || len(r.Refunds) != 0 {
		t.Fatalf("total %d refunds %#v", r.Total, r.Refunds)
	}
}

func TestTopHeavyStacksRefundAndTwoPots(t *testing.T) {
	r := ComputePots(potPlayers(
		[3]any{"p0", 100, false},
		[3]any{"p1", 300, false},
		[3]any{"p2", 300, false},
	))
	if len(r.Refunds) != 0 {
		t.Fatalf("refunds %#v", r.Refunds)
	}
	want := []Pot{
		{Amount: 300, Eligible: []string{"p0", "p1", "p2"}},
		{Amount: 400, Eligible: []string{"p1", "p2"}},
	}
	if !reflect.DeepEqual(r.Pots, want) || r.Total != 700 {
		t.Fatalf("pots %#v total %d", r.Pots, r.Total)
	}
}

func TestFoldedChipsStayButCannotWin(t *testing.T) {
	r := ComputePots(potPlayers(
		[3]any{"p0", 50, true},
		[3]any{"p1", 50, false},
		[3]any{"p2", 50, false},
	))
	if !reflect.DeepEqual(r.Pots, []Pot{{Amount: 150, Eligible: []string{"p1", "p2"}}}) {
		t.Fatalf("pots %#v", r.Pots)
	}
}

func TestEveryoneAllInSameAmount(t *testing.T) {
	r := ComputePots(potPlayers(
		[3]any{"p0", 100, false},
		[3]any{"p1", 100, false},
		[3]any{"p2", 100, false},
	))
	if !reflect.DeepEqual(r.Pots, []Pot{{Amount: 300, Eligible: []string{"p0", "p1", "p2"}}}) {
		t.Fatalf("pots %#v", r.Pots)
	}
}

func TestSingleContributorGetsEverythingBack(t *testing.T) {
	r := ComputePots(potPlayers(
		[3]any{"p0", 25, false},
		[3]any{"p1", 0, true},
	))
	if !reflect.DeepEqual(r.Refunds, []Refund{{ID: "p0", Amount: 25}}) {
		t.Fatalf("refunds %#v", r.Refunds)
	}
	if len(r.Pots) != 0 || r.Total != 0 {
		t.Fatalf("pots %#v total %d", r.Pots, r.Total)
	}
}

func TestOddChipLeftOfDealer(t *testing.T) {
	pots := []Pot{{Amount: 101, Eligible: []string{"p0", "p1"}}}
	scores := map[string]int{"p0": 10, "p1": 10}
	got := AwardPots(pots, func(id string) int { return scores[id] }, AwardOptions{
		DealerSeat: 0,
		SeatCount:  3,
		SeatOf:     func(id string) int { return int(id[1] - '0') },
	})
	if len(got) != 1 || len(got[0].Winners) != 2 {
		t.Fatalf("award %#v", got)
	}
	want := []PotWinner{{ID: "p1", Chips: 51}, {ID: "p0", Chips: 50}}
	if !reflect.DeepEqual(got[0].Winners, want) {
		t.Fatalf("winners %#v", got[0].Winners)
	}
}

func TestAwardSplitAndNothingToLosers(t *testing.T) {
	pots := []Pot{{Amount: 200, Eligible: []string{"p0", "p1", "p2"}}}
	scores := map[string]int{"p0": 7, "p1": 9, "p2": 9}
	got := AwardPots(pots, func(id string) int { return scores[id] }, AwardOptions{
		DealerSeat: 0,
		SeatCount:  3,
		SeatOf:     func(id string) int { return int(id[1] - '0') },
	})
	if !reflect.DeepEqual(got[0].Winners, []PotWinner{{ID: "p1", Chips: 100}, {ID: "p2", Chips: 100}}) {
		t.Fatalf("winners %#v", got[0].Winners)
	}
}

func TestAwardEmptyEligible(t *testing.T) {
	got := AwardPots([]Pot{{Amount: 40, Eligible: []string{}}}, func(string) int { return 1 }, AwardOptions{SeatCount: 9, SeatOf: func(string) int { return 0 }})
	if len(got) != 1 || len(got[0].Winners) != 0 || got[0].Amount != 40 {
		t.Fatalf("award %#v", got)
	}
}

func TestComputePotsClampsNegative(t *testing.T) {
	r := ComputePots([]PotPlayer{{ID: "p0", Committed: -5}, {ID: "p1", Committed: 10}})
	if len(r.Pots) != 0 || !reflect.DeepEqual(r.Refunds, []Refund{{ID: "p1", Amount: 10}}) {
		t.Fatalf("pots %#v refunds %#v", r.Pots, r.Refunds)
	}
}
