package game

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// Ranks and suits match server/poker/cards.mjs. A card is 0..51 with
// encoding (rank-2)*4+suit.
const (
	Ranks = "23456789TJQKA"
	Suits = "shdc"
)

// ParseCard parses a two-character card string such as "As", "Td" or "2c".
func ParseCard(text string) (int, error) {
	if len(text) != 2 {
		return 0, fmt.Errorf("bad card: %q", text)
	}
	r := strings.IndexByte(Ranks, byte(strings.ToUpper(text[:1])[0]))
	u := strings.IndexByte(Suits, byte(strings.ToLower(text[1:2])[0]))
	if r < 0 || u < 0 {
		return 0, fmt.Errorf("bad card: %q", text)
	}
	return r*4 + u, nil
}

// FormatCard renders a card as its two-character form.
func FormatCard(card int) (string, error) {
	if card < 0 || card > 51 {
		return "", fmt.Errorf("bad card: %d", card)
	}
	return string(Ranks[card/4]) + string(Suits[card%4]), nil
}

// RankOf returns the rank (2..14) of a 0..51 card.
func RankOf(card int) int { return card/4 + 2 }

// SuitOf returns the suit index (0=s,1=h,2=d,3=c) of a 0..51 card.
func SuitOf(card int) int { return card % 4 }

// NewDeck returns the 52 cards in order.
func NewDeck() []int {
	deck := make([]int, 52)
	for i := range deck {
		deck[i] = i
	}
	return deck
}

// RandomSeed returns 32 random bytes as lowercase hex.
func RandomSeed() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

// SeededShuffle is the deterministic Fisher-Yates of cards.mjs: each index
// draws a fresh 32-bit big-endian word from HMAC-SHA256(seed, "handNo:counter")
// and rejects the biased tail.
func SeededShuffle(seed string, handNo int) []int {
	deck := NewDeck()
	key := []byte(seed)
	counter := 0
	var pool []byte
	offset := 0
	word := func() uint32 {
		if offset+4 > len(pool) {
			mac := hmac.New(sha256.New, key)
			fmt.Fprintf(mac, "%d:%d", handNo, counter)
			pool = mac.Sum(nil)
			counter++
			offset = 0
		}
		n := binary.BigEndian.Uint32(pool[offset : offset+4])
		offset += 4
		return n
	}
	below := func(n int) int {
		limit := (uint64(1) << 32) / uint64(n) * uint64(n)
		for {
			v := word()
			if uint64(v) < limit {
				return int(v % uint32(n))
			}
		}
	}
	for i := len(deck) - 1; i > 0; i-- {
		j := below(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
	return deck
}

// CardsText renders cards space separated ("As Td").
func CardsText(cards []int) string {
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		s, err := FormatCard(c)
		if err != nil {
			parts = append(parts, "?")
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// cardString is the internal invariant-preserving formatter: table state only
// ever holds 0..51.
func cardString(card int) string {
	s, _ := FormatCard(card)
	return s
}

func cardStrings(cards []int) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, cardString(c))
	}
	return out
}
