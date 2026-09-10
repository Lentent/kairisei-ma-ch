package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWireDeckEmptySphereIDsRemainArray(t *testing.T) {
	payload, err := json.Marshal(toWireDeck(deckInfo{
		CardUniqueIDs:        []int64{1},
		SupportCardUniqueIDs: []int64{0},
		SphereUniqueIDs:      []int64{},
		BuddyUniqueIDs:       []int64{2},
	}))
	if err != nil {
		t.Fatalf("marshal wire deck: %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"6":[]`) {
		t.Fatalf("sphr_uniqid must be an empty array, got %s", body)
	}
	if strings.Contains(body, `"6":null`) {
		t.Fatalf("sphr_uniqid must never be null, got %s", body)
	}
}

func TestWireDeckNilSphereIDsNormalizeToArray(t *testing.T) {
	payload, err := json.Marshal(toWireDeck(deckInfo{}))
	if err != nil {
		t.Fatalf("marshal wire deck: %v", err)
	}
	if !strings.Contains(string(payload), `"6":[]`) {
		t.Fatalf("nil sphr_uniqid must normalize to an empty array, got %s", payload)
	}
}
