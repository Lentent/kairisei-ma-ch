package httpapi

import (
	"reflect"
	"testing"
)

func TestPartnerDeckAttributes(t *testing.T) {
	view := friendPointPartnerView{Cards: map[int64]cardInfo{1: {CardID: 100}, 2: {CardID: 200}, 3: {CardID: 300}}, CardAttributes: map[int]uint8{100: 1, 200: 1 | 16, 300: 2}}
	deck := deckInfo{CardUniqueIDs: []int64{1, 2, 0}, SupportCardUniqueIDs: []int64{3}}
	if got := view.deckAttributeCounts(deck); !reflect.DeepEqual(got, []int{0, 2, 0, 0, 0, 1}) {
		t.Fatalf("main deck attributes %v", got)
	}
}
