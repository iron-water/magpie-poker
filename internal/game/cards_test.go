package game

import (
	"strings"
	"testing"
)

func TestParseAndFormatCard(t *testing.T) {
	cases := []struct {
		text string
		card int
	}{
		{"As", 48},
		{"Ah", 49},
		{"Ad", 50},
		{"Ac", 51},
		{"2s", 0},
		{"2c", 3},
		{"Td", 34},
		{"Kc", 47},
	}
	for _, c := range cases {
		got, err := ParseCard(c.text)
		if err != nil || got != c.card {
			t.Fatalf("ParseCard(%q) = %d, %v; want %d", c.text, got, err, c.card)
		}
		out, err := FormatCard(c.card)
		if err != nil || out != c.text {
			t.Fatalf("FormatCard(%d) = %q, %v; want %q", c.card, out, err, c.text)
		}
	}
	// T is ten, case-insensitive input, and the rank/suit accessors agree.
	if c, err := ParseCard("td"); err != nil || c != 34 {
		t.Fatalf("lowercase td: %d, %v", c, err)
	}
	if RankOf(34) != 10 || SuitOf(34) != 2 {
		t.Fatalf("Td accessors: rank %d suit %d", RankOf(34), SuitOf(34))
	}
	for _, bad := range []string{"", "A", "1s", "Ax", "10s", "sA", "As "} {
		if _, err := ParseCard(bad); err == nil {
			t.Fatalf("ParseCard(%q) should fail", bad)
		}
	}
	for _, bad := range []int{-1, 52, 100} {
		if _, err := FormatCard(bad); err == nil {
			t.Fatalf("FormatCard(%d) should fail", bad)
		}
	}
}

func TestNewDeck(t *testing.T) {
	deck := NewDeck()
	if len(deck) != 52 {
		t.Fatalf("deck length %d", len(deck))
	}
	for i, c := range deck {
		if c != i {
			t.Fatalf("deck[%d] = %d", i, c)
		}
	}
}

func TestSeededShuffleMatchesReference(t *testing.T) {
	// Reference output of server/poker/cards.mjs seededShuffle("ab"*32, 1).
	want := []int{
		25, 10, 3, 38, 45, 51, 22, 32, 35, 7, 19, 15, 47, 17, 37, 20, 39, 8, 28, 50,
		5, 11, 43, 13, 18, 24, 44, 9, 21, 48, 29, 30, 16, 6, 46, 31, 27, 23, 12, 1,
		41, 2, 49, 26, 36, 42, 0, 14, 33, 4, 34, 40,
	}
	seed := strings.Repeat("ab", 32)
	got := SeededShuffle(seed, 1)
	if len(got) != 52 {
		t.Fatalf("deck length %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("deck[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	// Deterministic and a permutation.
	again := SeededShuffle(seed, 1)
	var seen [52]bool
	for i, c := range again {
		if c != got[i] {
			t.Fatal("shuffle is not deterministic")
		}
		if c < 0 || c > 51 || seen[c] {
			t.Fatalf("not a permutation at %d: %d", i, c)
		}
		seen[c] = true
	}
	// A different hand number draws a different deck.
	other := SeededShuffle(seed, 2)
	same := true
	for i := range other {
		if other[i] != got[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("handNo should change the deck")
	}
	if CardsText(got[:3]) != "8h 4d 2c" {
		t.Fatalf("CardsText = %q", CardsText(got[:3]))
	}
}

func TestRandomSeed(t *testing.T) {
	a, err := RandomSeed()
	if err != nil || len(a) != 64 {
		t.Fatalf("RandomSeed = %q, %v", a, err)
	}
	b, err := RandomSeed()
	if err != nil || len(b) != 64 {
		t.Fatalf("RandomSeed = %q, %v", b, err)
	}
	if a == b {
		t.Fatal("seeds should differ")
	}
}
