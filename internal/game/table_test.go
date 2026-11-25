package game

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSeed = "abababababababababababababababababababababababababababababababab"

func defaultConfig(seats int) Config {
	return Config{
		SmallBlind:    10,
		BigBlind:      20,
		StartingChips: 1000,
		MaxSeats:      seats,
		ActionSeconds: 0,
	}
}

func testTable(t *testing.T, seats int, cfg Config) *Table {
	t.Helper()
	if cfg.MaxSeats == 0 {
		cfg.MaxSeats = seats
	}
	table := NewTable("t_test", "测试", "p0", cfg)
	for i := range seats {
		if _, err := table.AddPlayer(PlayerInput{ID: playerID(i), Name: "P" + strconv.Itoa(i)}); err != nil {
			t.Fatalf("AddPlayer %d: %v", i, err)
		}
	}
	return table
}

func playerID(i int) string { return "p" + strconv.Itoa(i) }

func mustPlayer(t *testing.T, table *Table, id string) *Player {
	t.Helper()
	p := table.findPlayer(id)
	if p == nil {
		t.Fatalf("no player %s", id)
	}
	return p
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var pe *PokerError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PokerError, got %v", err)
	}
	return pe.Code
}

func TestStartHandViewParityWithJS(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	v := table.ViewFor("p0", time.Now())
	if v.HandNo != 1 || v.Phase != "hand" || v.CurrentBet != 20 || v.MinRaiseTo != 40 || v.Pot != 30 {
		t.Fatalf("view header %+v", v)
	}
	if len(v.Pots) != 1 || v.Pots[0].Amount != 20 {
		t.Fatalf("pots %+v", v.Pots)
	}
	if v.Me == nil || strings.Join(v.Me.Cards, " ") != "4d Jd" {
		t.Fatalf("hole cards %+v", v.Me)
	}
	if v.Me.HandStrength == nil || v.Me.HandStrength.Label != "J4 同花" || v.Me.HandStrength.Score != nil {
		t.Fatalf("strength %+v", v.Me.HandStrength)
	}
	if v.Me.Legal == nil || v.Me.Legal.Call == nil || *v.Me.Legal.Call != 10 ||
		v.Me.Legal.Raise == nil || v.Me.Legal.Raise.Min != 40 || v.Me.Legal.Raise.Max != 1000 {
		t.Fatalf("legal %+v", v.Me.Legal)
	}
	for _, s := range v.Seats {
		switch s.ID {
		case "p0":
			if s.Seat != 0 || s.Chips != 990 || s.Bet != 10 || !s.IsDealer || !s.IsSB || strings.Join(s.Cards, " ") != "4d Jd" {
				t.Fatalf("p0 seat %+v", s)
			}
		case "p1":
			if s.Seat != 1 || s.Chips != 980 || s.Bet != 20 || !s.IsBB || s.Cards != nil {
				t.Fatalf("p1 seat %+v", s)
			}
		}
	}
}

func TestAnteIsPostedBeforeBlinds(t *testing.T) {
	cfg := defaultConfig(2)
	cfg.Ante = 5
	table := testTable(t, 2, cfg)
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if p := mustPlayer(t, table, "p0"); p.Committed != 15 || p.Bet != 10 {
		t.Fatalf("p0 committed %d bet %d", p.Committed, p.Bet)
	}
	if p := mustPlayer(t, table, "p1"); p.Committed != 25 || p.Bet != 20 {
		t.Fatalf("p1 committed %d bet %d", p.Committed, p.Bet)
	}
	if table.currentBet != 20 {
		t.Fatalf("currentBet %d", table.currentBet)
	}
	v := table.ViewFor("p0", time.Now())
	if v.Pot != 40 || v.Me == nil || v.Me.Legal == nil || v.Me.Legal.Call == nil || *v.Me.Legal.Call != 10 {
		t.Fatalf("pot %d legal %+v", v.Pot, v.Me)
	}
	if err := table.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "check"}); err != nil {
		t.Fatal(err)
	}
	if table.street != "flop" || table.ViewFor("p0", time.Now()).Pot != 50 {
		t.Fatalf("street %s pot %d", table.street, table.ViewFor("p0", time.Now()).Pot)
	}
}

func TestHeadsUpBlindsAndOrder(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if table.dealerSeat != 0 || table.sbSeat == nil || *table.sbSeat != 0 {
		t.Fatalf("dealer %d sb %v", table.dealerSeat, table.sbSeat)
	}
	if table.bbSeat == nil || *table.bbSeat != 1 {
		t.Fatalf("bb %v", table.bbSeat)
	}
	if table.toAct == nil || *table.toAct != "p0" {
		t.Fatalf("toAct %v", table.toAct)
	}
	if p := mustPlayer(t, table, "p0"); p.Bet != 10 {
		t.Fatalf("sb bet %d", p.Bet)
	}
	if p := mustPlayer(t, table, "p1"); p.Bet != 20 {
		t.Fatalf("bb bet %d", p.Bet)
	}
	if err := table.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if table.toAct == nil || *table.toAct != "p1" {
		t.Fatalf("toAct after call %v", table.toAct)
	}
	if err := table.Act("p1", Action{Type: "check"}); err != nil {
		t.Fatal(err)
	}
	if table.street != "flop" || len(table.board) != 3 {
		t.Fatalf("street %s board %d", table.street, len(table.board))
	}
	if table.toAct == nil || *table.toAct != "p1" {
		t.Fatalf("postflop toAct %v", table.toAct)
	}
	if err := table.Act("p1", Action{Type: "bet", Amount: 20}); err != nil {
		t.Fatal(err)
	}
	if table.toAct == nil || *table.toAct != "p0" {
		t.Fatalf("after bet toAct %v", table.toAct)
	}
	if err := table.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if table.street != "turn" {
		t.Fatalf("street %s", table.street)
	}
	if table.toAct == nil || *table.toAct != "p1" {
		t.Fatalf("turn toAct %v", table.toAct)
	}
}

func TestThreeHandedOrder(t *testing.T) {
	table := testTable(t, 3, defaultConfig(3))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if table.dealerSeat != 0 || *table.sbSeat != 1 || *table.bbSeat != 2 {
		t.Fatalf("dealer %d sb %d bb %d", table.dealerSeat, *table.sbSeat, *table.bbSeat)
	}
	if table.toAct == nil || *table.toAct != "p0" {
		t.Fatalf("preflop toAct %v", table.toAct)
	}
	if err := table.Act("p0", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p2", Action{Type: "check"}); err != nil {
		t.Fatal(err)
	}
	if table.street != "flop" || table.toAct == nil || *table.toAct != "p1" {
		t.Fatalf("flop street %s toAct %v", table.street, table.toAct)
	}
}

func TestMinRaiseEnforcedAndReopens(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if legal := table.legalForLocked(mustPlayer(t, table, "p0")); legal.Raise == nil || legal.Raise.Min != 40 {
		t.Fatalf("initial raise %+v", legal)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 40}); err != nil {
		t.Fatal(err)
	}
	if legal := table.legalForLocked(mustPlayer(t, table, "p1")); legal.Raise == nil || legal.Raise.Min != 60 {
		t.Fatalf("after raise %+v", legal)
	}
	if err := table.Act("p1", Action{Type: "raise", Amount: 50}); codeOf(t, err) != "too_small" {
		t.Fatalf("short raise error %v", err)
	}
	if err := table.Act("p1", Action{Type: "raise", Amount: 60}); err != nil {
		t.Fatal(err)
	}
	if legal := table.legalForLocked(mustPlayer(t, table, "p0")); legal.Raise == nil || legal.Raise.Min != 80 {
		t.Fatalf("after reraise %+v", legal)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 80}); err != nil {
		t.Fatal(err)
	}
	if table.currentBet != 80 {
		t.Fatalf("currentBet %d", table.currentBet)
	}
}

func TestShortAllInDoesNotReopen(t *testing.T) {
	table := testTable(t, 3, defaultConfig(3))
	mustPlayer(t, table, "p1").Chips = 150
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 100}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "allin"}); err != nil {
		t.Fatalf("short all-in: %v", err)
	}
	if p := mustPlayer(t, table, "p1"); !p.AllIn || p.Bet != 150 {
		t.Fatalf("p1 allin %v bet %d", p.AllIn, p.Bet)
	}
	if table.toAct == nil || *table.toAct != "p2" {
		t.Fatalf("toAct %v", table.toAct)
	}
	if err := table.Act("p2", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if table.toAct == nil || *table.toAct != "p0" {
		t.Fatalf("toAct %v", table.toAct)
	}
	p0 := mustPlayer(t, table, "p0")
	if p0.MayRaise {
		t.Fatal("short all-in must not reopen for p0")
	}
	legal := table.legalForLocked(p0)
	if legal.Raise != nil {
		t.Fatalf("raise should be unavailable: %+v", legal.Raise)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 300}); codeOf(t, err) != "no_raise" {
		t.Fatalf("expected no_raise, got %v", err)
	}
	if err := table.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if table.street != "flop" {
		t.Fatalf("street %s", table.street)
	}
}

func TestFoldWinAwardsWholePot(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 60}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if table.phase != "showdown" || table.showdown == nil || table.showdown.Reason != "fold" {
		t.Fatalf("phase %s showdown %+v", table.phase, table.showdown)
	}
	if table.showdown.Total != 40 {
		t.Fatalf("total %d", table.showdown.Total)
	}
	if mustPlayer(t, table, "p0").Chips != 1020 || mustPlayer(t, table, "p1").Chips != 980 {
		t.Fatalf("chips %d %d", mustPlayer(t, table, "p0").Chips, mustPlayer(t, table, "p1").Chips)
	}
	if mustPlayer(t, table, "p0").Chips+mustPlayer(t, table, "p1").Chips != 2000 {
		t.Fatal("chips not conserved")
	}
}

func TestAllInBuildsMainAndSidePot(t *testing.T) {
	table := testTable(t, 3, defaultConfig(3))
	mustPlayer(t, table, "p0").Chips = 100
	mustPlayer(t, table, "p1").Chips = 60
	mustPlayer(t, table, "p2").Chips = 200
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if table.toAct == nil || *table.toAct != "p0" {
		t.Fatalf("toAct %v", table.toAct)
	}
	if err := table.Act("p0", Action{Type: "allin"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p2", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if table.phase != "showdown" || len(table.board) != 5 {
		t.Fatalf("phase %s board %d", table.phase, len(table.board))
	}
	if table.showdown.Total != 260 {
		t.Fatalf("total %d", table.showdown.Total)
	}
	amounts := make([]int, 0, len(table.showdown.Pots))
	for _, p := range table.showdown.Pots {
		amounts = append(amounts, p.Amount)
	}
	if len(amounts) != 2 || !(amounts[0] == 80 && amounts[1] == 180 || amounts[0] == 180 && amounts[1] == 80) {
		t.Fatalf("pots %v", amounts)
	}
	total := 0
	for _, p := range table.players {
		total += p.Chips
	}
	if total != 360 {
		t.Fatalf("chips total %d", total)
	}
}

func TestControlledShowdownPaysBestHand(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	table.phase = "hand"
	table.street = "river"
	table.board = mustCards(t, "2c 7d 9s Jc 3h")
	mustPlayer(t, table, "p0").Cards = mustCards(t, "As Ah")
	mustPlayer(t, table, "p1").Cards = mustCards(t, "Ks Kh")
	for _, p := range table.players {
		p.Committed = 100
		p.Chips = 900
		p.Folded = false
		p.AllIn = false
		p.Bet = 0
	}
	table.handPlayers = []string{"p0", "p1"}
	table.showdown = nil
	table.finishShowdownLocked()
	if table.phase != "showdown" {
		t.Fatalf("phase %s", table.phase)
	}
	if mustPlayer(t, table, "p0").Chips != 1100 || mustPlayer(t, table, "p1").Chips != 900 {
		t.Fatalf("chips %d %d", mustPlayer(t, table, "p0").Chips, mustPlayer(t, table, "p1").Chips)
	}
	var winners []string
	if err := json.Unmarshal(table.showdown.Pots[0].Winners, &winners); err != nil {
		t.Fatal(err)
	}
	if len(winners) != 1 || winners[0] != "p0" {
		t.Fatalf("winners %v", winners)
	}
}

func TestSplitPotBoardPlays(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	table.phase = "hand"
	table.street = "river"
	table.board = mustCards(t, "Ah Kh Qh Jh Th")
	mustPlayer(t, table, "p0").Cards = mustCards(t, "2c 3d")
	mustPlayer(t, table, "p1").Cards = mustCards(t, "4s 5c")
	for _, p := range table.players {
		p.Committed = 100
		p.Chips = 900
		p.Folded = false
		p.AllIn = false
		p.Bet = 0
	}
	table.handPlayers = []string{"p0", "p1"}
	table.showdown = nil
	table.finishShowdownLocked()
	if mustPlayer(t, table, "p0").Chips != 1000 || mustPlayer(t, table, "p1").Chips != 1000 {
		t.Fatalf("chips %d %d", mustPlayer(t, table, "p0").Chips, mustPlayer(t, table, "p1").Chips)
	}
	var winners []string
	if err := json.Unmarshal(table.showdown.Pots[0].Winners, &winners); err != nil {
		t.Fatal(err)
	}
	if len(winners) != 2 {
		t.Fatalf("winners %v", winners)
	}
}

func TestTickFoldsFacingBetAndChecksWhenAble(t *testing.T) {
	cfg := defaultConfig(2)
	cfg.ActionSeconds = 1
	table := testTable(t, 2, cfg)
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	table.deadline = new(time.Now().UnixMilli() - 1)
	if !table.Tick(time.Now()) {
		t.Fatal("tick should act")
	}
	if !mustPlayer(t, table, "p0").Folded || table.phase != "showdown" {
		t.Fatalf("folded %v phase %s", mustPlayer(t, table, "p0").Folded, table.phase)
	}

	other := testTable(t, 2, cfg)
	if err := other.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := other.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if err := other.Act("p1", Action{Type: "check"}); err != nil {
		t.Fatal(err)
	}
	if other.street != "flop" {
		t.Fatalf("street %s", other.street)
	}
	other.deadline = new(time.Now().UnixMilli() - 1)
	if !other.Tick(time.Now()) {
		t.Fatal("tick should check")
	}
	if got := mustPlayer(t, other, "p1").LastAction; got != "过牌" {
		t.Fatalf("lastAction %q", got)
	}
}

func TestTickWithoutDeadlineDoesNothing(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if table.Tick(time.Now()) {
		t.Fatal("no deadline should not tick")
	}
	table.deadline = new(time.Now().UnixMilli() + 60_000)
	if table.Tick(time.Now()) {
		t.Fatal("early tick should not fire")
	}
}

func TestIllegalActionsRejected(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.Act("p0", Action{Type: "fold"}); codeOf(t, err) != "no_hand" {
		t.Fatalf("no_hand: %v", err)
	}
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); codeOf(t, err) != "out_of_turn" {
		t.Fatalf("out_of_turn: %v", err)
	}
	if err := table.Act("p0", Action{Type: "check"}); codeOf(t, err) != "must_call" {
		t.Fatalf("must_call: %v", err)
	}
	if err := table.Act("p0", Action{Type: "bet", Amount: 50}); codeOf(t, err) != "use_raise" {
		t.Fatalf("use_raise: %v", err)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 5}); codeOf(t, err) != "too_small" {
		t.Fatalf("too_small: %v", err)
	}
	if err := table.Act("nobody", Action{Type: "fold"}); codeOf(t, err) != "out_of_turn" {
		t.Fatalf("unknown player: %v", err)
	}
}

func TestSettlementTransfer(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 60}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if err := table.EndTable(); err != nil {
		t.Fatal(err)
	}
	if table.phase != "ended" || table.settlement == nil || len(table.settlement.Transfers) != 1 {
		t.Fatalf("phase %s settlement %+v", table.phase, table.settlement)
	}
	x := table.settlement.Transfers[0]
	if x.From != "p1" || x.To != "p0" || x.AmountChips != 20 || x.Status != "pending" {
		t.Fatalf("transfer %+v", x)
	}
	if math.Abs(x.Amount-0.01) > 1e-12 {
		t.Fatalf("amount %v", x.Amount)
	}
	if x.Unit != "cost" || x.GatewayURL != "" || x.Key != nil || x.Expires != nil || x.Error != nil {
		t.Fatalf("transfer fields %+v", x)
	}
}

func TestSettlementProportionalTransfers(t *testing.T) {
	table := testTable(t, 4, defaultConfig(4))
	table.phase = "showdown"
	chips := map[string]int{"p0": 1200, "p1": 1100, "p2": 800, "p3": 900}
	for id, n := range chips {
		mustPlayer(t, table, id).Chips = n
	}
	if err := table.EndTable(); err != nil {
		t.Fatal(err)
	}
	got := table.settlement.Transfers
	if len(got) != 2 {
		t.Fatalf("transfers %+v", got)
	}
	if got[0].From != "p2" || got[0].To != "p0" || got[0].AmountChips != 200 || math.Abs(got[0].Amount-0.1) > 1e-12 {
		t.Fatalf("first transfer %+v", got[0])
	}
	if got[1].From != "p3" || got[1].To != "p1" || got[1].AmountChips != 100 || math.Abs(got[1].Amount-0.05) > 1e-12 {
		t.Fatalf("second transfer %+v", got[1])
	}
}

func TestEndTableRejectsNonZeroSum(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	table.phase = "showdown"
	mustPlayer(t, table, "p0").Chips = 1100
	mustPlayer(t, table, "p1").Chips = 950
	if err := table.EndTable(); codeOf(t, err) != "not_zero_sum" {
		t.Fatalf("expected not_zero_sum, got %v", err)
	}
	if table.phase == "ended" {
		t.Fatal("must not end on non-zero-sum")
	}
}

func TestMarkIssuedAndVerified(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 60}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if err := table.EndTable(); err != nil {
		t.Fatal(err)
	}
	if err := table.MarkIssued("x1", "p0", "sk-test_0123456789abcdef", "", ""); codeOf(t, err) != "not_payer" {
		t.Fatalf("not_payer: %v", err)
	}
	if err := table.MarkIssued("x1", "p1", "short", "", ""); codeOf(t, err) != "bad_action" {
		t.Fatalf("short key: %v", err)
	}
	if err := table.MarkIssued("x9", "p1", "sk-test_0123456789abcdef", "", ""); codeOf(t, err) != "no_transfer" {
		t.Fatalf("no_transfer: %v", err)
	}
	if err := table.MarkIssued("x1", "p1", "sk-test_0123456789abcdef", "2026-12-01T00:00:00Z", "https://gw"); err != nil {
		t.Fatal(err)
	}
	payee := table.ViewFor("p0", time.Now())
	if payee.Settlement == nil || payee.Settlement.Transfers[0].Key == nil || *payee.Settlement.Transfers[0].Key != "sk-test_0123456789abcdef" {
		t.Fatalf("payee should see key: %+v", payee.Settlement)
	}
	payer := table.ViewFor("p1", time.Now())
	if payer.Settlement == nil || payer.Settlement.Transfers[0].Key != nil {
		t.Fatalf("payer key must be redacted: %+v", payer.Settlement)
	}
	if table.settlement.Transfers[0].Status != "issued" || table.settlement.Transfers[0].Expires == nil {
		t.Fatalf("status %+v", table.settlement.Transfers[0])
	}
	if err := table.MarkVerified("x1", "p1"); codeOf(t, err) != "not_payee" {
		t.Fatalf("not_payee: %v", err)
	}
	if err := table.MarkVerified("x1", "p0"); err != nil {
		t.Fatal(err)
	}
	if table.settlement.Transfers[0].Status != "verified" {
		t.Fatalf("status %s", table.settlement.Transfers[0].Status)
	}
}

func TestViewRedactsCardsAndLegalActions(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	a := table.ViewFor("p0", time.Now())
	if a.Me == nil || len(a.Me.Cards) != 2 || a.Me.Legal == nil {
		t.Fatalf("me view %+v", a.Me)
	}
	if a.Me.Legal.Call == nil || *a.Me.Legal.Call != 10 {
		t.Fatalf("call %v", a.Me.Legal.Call)
	}
	for _, s := range a.Seats {
		switch s.ID {
		case "p0":
			if len(s.Cards) != 2 || s.CardsCount != 2 {
				t.Fatalf("own seat cards %+v", s)
			}
		case "p1":
			if s.Cards != nil || s.CardsCount != 2 {
				t.Fatalf("opponent cards must be null: %+v", s)
			}
		}
	}
	b := table.ViewFor("p1", time.Now())
	if b.Me == nil || b.Me.Legal != nil {
		t.Fatalf("only the player on turn gets legal actions: %+v", b.Me)
	}
	if b.Me.HandStrength == nil || b.Me.HandStrength.Category != nil || b.Me.HandStrength.Score != nil {
		t.Fatalf("preflop strength %+v", b.Me.HandStrength)
	}
}

func TestViewJSONShape(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	lobby := table.ViewFor("p0", time.Now())
	raw, err := json.Marshal(lobby)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "name", "phase", "handNo", "config", "dealerSeat", "toAct", "currentBet", "minRaiseTo", "pot", "pots", "board", "seats", "me", "log", "showdown", "deadline", "settlement"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("missing view key %q", key)
		}
	}
	if doc["toAct"] != nil || doc["showdown"] != nil || doc["deadline"] != nil || doc["settlement"] != nil {
		t.Fatalf("lobby nulls: toAct %v showdown %v deadline %v settlement %v", doc["toAct"], doc["showdown"], doc["deadline"], doc["settlement"])
	}
	if _, ok := doc["pots"].([]any); !ok {
		t.Fatalf("pots should be []: %T", doc["pots"])
	}
	if _, ok := doc["board"].([]any); !ok {
		t.Fatalf("board should be []: %T", doc["board"])
	}
	seats, ok := doc["seats"].([]any)
	if !ok || len(seats) != 2 {
		t.Fatalf("seats %T", doc["seats"])
	}
	config := doc["config"].(map[string]any)
	if _, ok := config["minSettlement"]; !ok {
		t.Fatal("config.minSettlement missing")
	}
	for _, s := range seats {
		row := s.(map[string]any)
		if v, ok := row["cards"]; !ok {
			t.Fatal("seat cards key missing")
		} else if row["id"] == "p1" && v != nil {
			t.Fatalf("hidden cards should be null, got %v", v)
		}
	}
	me := doc["me"].(map[string]any)
	if cards, ok := me["cards"].([]any); !ok || len(cards) != 0 {
		t.Fatalf("own empty cards should be []: %T", me["cards"])
	}

	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	view := table.ViewFor("p1", time.Now())
	raw, err = json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	doc = nil
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	pots, ok := doc["pots"].([]any)
	if !ok || len(pots) != 1 {
		t.Fatalf("pots %v", doc["pots"])
	}
	pot := pots[0].(map[string]any)
	if _, ok := pot["eligible"].([]any); !ok {
		t.Fatalf("hand pot eligible %T", pot["eligible"])
	}
	if _, ok := pot["winners"]; ok {
		t.Fatal("hand pot should not have winners")
	}
	if doc["showdown"] != nil {
		t.Fatalf("showdown should be null during hand: %v", doc["showdown"])
	}
	if doc["deadline"] != nil {
		t.Fatalf("deadline should be null with actionSeconds 0: %v", doc["deadline"])
	}
}

func TestShowdownJSONReveal(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	view := table.ViewFor("p2", time.Now())
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sd := doc["showdown"].(map[string]any)
	if sd["reason"] != "fold" {
		t.Fatalf("reason %v", sd["reason"])
	}
	if revealed, ok := sd["revealed"].([]any); !ok || len(revealed) != 0 {
		t.Fatalf("fold revealed should be []: %T", sd["revealed"])
	}
	pots := doc["pots"].([]any)
	if len(pots) != 1 {
		t.Fatalf("pots %v", pots)
	}
	pot := pots[0].(map[string]any)
	if _, ok := pot["eligible"].([]any); !ok {
		t.Fatalf("fold pot eligible %T", pot["eligible"])
	}
	winners, ok := pot["winners"].([]any)
	if !ok || len(winners) != 1 {
		t.Fatalf("fold pot winners %T", pot["winners"])
	}
	w := winners[0].(map[string]any)
	if w["id"] != "p0" {
		t.Fatalf("winner %v", w)
	}
	if _, ok := w["chips"]; !ok {
		t.Fatalf("winner chips missing: %v", w)
	}
	for _, s := range view.Seats {
		if s.ID == "p0" && s.Cards != nil {
			t.Fatal("fold winner must not reveal cards")
		}
	}
}

func TestSnapshotRestoreMidHand(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "call"}); err != nil {
		t.Fatal(err)
	}
	data, err := table.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	clone, err := Restore(data)
	if err != nil {
		t.Fatal(err)
	}
	if clone.street != "preflop" || clone.toAct == nil || *clone.toAct != "p1" {
		t.Fatalf("clone street %s toAct %v", clone.street, clone.toAct)
	}
	if got := mustPlayer(t, clone, "p0").Chips; got != 980 {
		t.Fatalf("clone p0 chips %d", got)
	}
	if err := clone.Act("p1", Action{Type: "check"}); err != nil {
		t.Fatal(err)
	}
	if clone.street != "flop" || len(clone.board) != 3 {
		t.Fatalf("clone street %s board %d", clone.street, len(clone.board))
	}
}

func TestSnapshotRestoreEndedSettlement(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "raise", Amount: 60}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if err := table.EndTable(); err != nil {
		t.Fatal(err)
	}
	if err := table.MarkIssued("x1", "p1", "sk-test_0123456789abcdef", "2026-12-01T00:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
	data, err := table.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	clone, err := Restore(data)
	if err != nil {
		t.Fatal(err)
	}
	if clone.phase != "ended" || clone.settlement == nil || len(clone.settlement.Transfers) != 1 {
		t.Fatalf("clone phase %s settlement %+v", clone.phase, clone.settlement)
	}
	view := clone.ViewFor("p0", time.Now())
	if view.Settlement == nil || view.Settlement.Transfers[0].Key == nil {
		t.Fatal("restored key should survive for payee")
	}
	other := clone.ViewFor("p1", time.Now())
	if other.Settlement.Transfers[0].Key != nil {
		t.Fatal("restored key must stay redacted for payer")
	}
}

func TestRebuyBetweenHands(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p0", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	before := mustPlayer(t, table, "p1").Chips
	if err := table.Rebuy("p1", 500); err != nil {
		t.Fatal(err)
	}
	if mustPlayer(t, table, "p1").Chips != before+500 || mustPlayer(t, table, "p1").BuyIn != 1500 {
		t.Fatalf("chips %d buyIn %d", mustPlayer(t, table, "p1").Chips, mustPlayer(t, table, "p1").BuyIn)
	}
	if err := table.Rebuy("p1", 0); codeOf(t, err) != "bad_action" {
		t.Fatalf("bad rebuy: %v", err)
	}
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.Rebuy("p0", 100); codeOf(t, err) != "in_hand" {
		t.Fatalf("rebuy in hand: %v", err)
	}
}

func TestAddPlayerNameSuffixAndReclaim(t *testing.T) {
	table := NewTable("t", "测试", "p0", defaultConfig(3))
	first, err := table.AddPlayer(PlayerInput{ID: "a", Name: "Mio", Pub: "pub-a"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "Mio" {
		t.Fatalf("first name %q", first.Name)
	}
	second, err := table.AddPlayer(PlayerInput{ID: "b", Name: "Mio", Pub: "pub-b"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "Mio 2" {
		t.Fatalf("second name %q", second.Name)
	}
	// Reconnecting with the stored duplicate name reclaims that seat.
	table.MarkDisconnected("b")
	again, err := table.AddPlayer(PlayerInput{ID: "b2", Name: "Mio 2", Pub: "pub-b"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != "b" || !again.Connected || again.SittingOut || len(table.players) != 2 {
		t.Fatalf("reclaim %+v players %d", again, len(table.players))
	}
	// A disconnected original name is reclaimed too.
	table.MarkDisconnected("a")
	back, err := table.AddPlayer(PlayerInput{ID: "a2", Name: "Mio", Pub: "pub-a"})
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != "a" || !back.Connected || len(table.players) != 2 {
		t.Fatalf("reclaim original %+v", back)
	}
}

func TestAddPlayerFullAndEnded(t *testing.T) {
	table := NewTable("t", "测试", "p0", defaultConfig(2))
	for _, id := range []string{"a", "b"} {
		if _, err := table.AddPlayer(PlayerInput{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := table.AddPlayer(PlayerInput{ID: "c", Name: "c"}); codeOf(t, err) != "full" {
		t.Fatalf("full: %v", err)
	}
	table.phase = "ended"
	if _, err := table.AddPlayer(PlayerInput{ID: "d", Name: "d"}); codeOf(t, err) != "ended" {
		t.Fatalf("ended: %v", err)
	}
}

func TestMarkDisconnectedFoldsOnTurn(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	table.MarkDisconnected("p0")
	if !mustPlayer(t, table, "p0").Folded || table.phase != "showdown" {
		t.Fatalf("folded %v phase %s", mustPlayer(t, table, "p0").Folded, table.phase)
	}
	table.MarkConnected("p0")
	if !mustPlayer(t, table, "p0").Connected || mustPlayer(t, table, "p0").SittingOut {
		t.Fatal("MarkConnected should clear flags")
	}
	if _, ok := table.Player("p0"); !ok {
		t.Fatal("Player lookup failed")
	}
	if p, ok := table.Player("p9"); ok || p != nil {
		t.Fatal("unknown player should not be found")
	}
}

func TestLeaveDuringHandFolds(t *testing.T) {
	table := testTable(t, 3, defaultConfig(3))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	table.Leave("p0")
	p := mustPlayer(t, table, "p0")
	if !p.Left || !p.Folded || table.toAct == nil || *table.toAct != "p1" {
		t.Fatalf("leave p0=%+v toAct %v", p, table.toAct)
	}
	if table.phase != "hand" {
		t.Fatalf("phase %s", table.phase)
	}
}

func TestLeaveLobbyRemovesPlayer(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	table.Leave("p0")
	if len(table.players) != 1 || table.seats[0] != "" {
		t.Fatalf("players %d seats %v", len(table.players), table.seats)
	}
}

func TestChat(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	if err := table.Chat("p0", "  hello  "); err != nil {
		t.Fatal(err)
	}
	view := table.ViewFor("p1", time.Now())
	last := view.Log[len(view.Log)-1]
	if last.Kind != "chat" || last.Text != "P0：hello" || last.PlayerID == nil || *last.PlayerID != "p0" {
		t.Fatalf("last log %+v", last)
	}
	if err := table.Chat("ghost", "hi"); codeOf(t, err) != "not_seated" {
		t.Fatalf("chat ghost: %v", err)
	}
}

func TestStartHandErrors(t *testing.T) {
	table := testTable(t, 2, defaultConfig(2))
	table.MarkDisconnected("p0")
	if err := table.StartHand(testSeed); codeOf(t, err) != "not_enough" {
		t.Fatalf("not_enough: %v", err)
	}
	table.MarkConnected("p0")
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if err := table.StartHand(testSeed); codeOf(t, err) != "in_hand" {
		t.Fatalf("in_hand: %v", err)
	}
}

func TestBlindsRotateBetweenHands(t *testing.T) {
	table := testTable(t, 3, defaultConfig(3))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if table.dealerSeat != 0 {
		t.Fatalf("first dealer %d", table.dealerSeat)
	}
	if err := table.Act("p0", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if err := table.Act("p1", Action{Type: "fold"}); err != nil {
		t.Fatal(err)
	}
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	if table.dealerSeat != 1 || *table.sbSeat != 2 || *table.bbSeat != 0 {
		t.Fatalf("second dealer %d sb %d bb %d", table.dealerSeat, *table.sbSeat, *table.bbSeat)
	}
}

func TestViewForConcurrent(t *testing.T) {
	table := testTable(t, 3, defaultConfig(3))
	if err := table.StartHand(testSeed); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = table.ViewFor("p0", time.Now())
				if _, err := table.Snapshot(); err != nil {
					t.Errorf("snapshot: %v", err)
					return
				}
				_ = table.Phase()
				_ = table.MaxSeats()
			}
		}()
	}
	wg.Wait()
}

func TestAddPlayerCannotReclaimWithAnotherPub(t *testing.T) {
	table := NewTable("t", "测试", "p0", defaultConfig(3))
	first, err := table.AddPlayer(PlayerInput{ID: "a", Name: "Mio", Pub: "pub-a"})
	if err != nil {
		t.Fatal(err)
	}
	table.MarkDisconnected("a")
	other, err := table.AddPlayer(PlayerInput{ID: "b", Name: "Mio", Pub: "pub-b"})
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID || other.Name == "Mio" {
		t.Fatalf("another key took the seat: %+v", other)
	}
	if first.Connected {
		t.Fatal("original seat was marked connected")
	}
}
