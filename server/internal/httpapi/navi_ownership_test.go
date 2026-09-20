package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestExpandedNavigatorOwnershipDoesNotWrapLegacyMask(t *testing.T) {
	s := testAccount(t, func(state *gamestate.State) {
		state.User.Coin, state.User.CoinFree = 0, 1000
		state.User.NaviPurchasePrice = 10
		state.User.NaviUnlockFlag = 1
		state.User.SelectableNaviIDs = []int8{0}
		state.User.NaviCatalogIDs = []int8{0, 62, 63, 64, 70, 127}
	})
	for _, id := range []int8{62, 63, 64, 70, 127} {
		if err := s.PurchaseNavi(id); err != nil || !s.SelectNavi(id) {
			t.Fatalf("purchase/select %d: %v", id, err)
		}
		balance := s.Snapshot(gamestate.State{}).User.CoinFree
		if err := s.PurchaseNavi(id); err == nil || s.Snapshot(gamestate.State{}).User.CoinFree != balance {
			t.Fatalf("duplicate navigator %d was charged", id)
		}
	}
	if s.SelectNavi(6) || s.SelectNavi(-1) || s.Snapshot(gamestate.State{}).User.CoinFree != 950 {
		t.Fatal("navigator IDs wrapped or purchase amount changed")
	}
	mask, ids := s.NaviUnlockState()
	want := []int8{0, 62, 63, 64, 70, 127}
	if mask != int64(1)|(int64(1)<<62) || !slices.Equal(ids, want) {
		t.Fatalf("connect ownership: mask %d, IDs %v", mask, ids)
	}
	// Exercise the real connect response, including array encoding for int8 IDs.
	a := &API{account: s, initialState: gamestate.State{}}
	w := httptest.NewRecorder()
	a.connect(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"session":"test"}`)))
	var body struct {
		Mask int64  `json:"navi_unlock_flag"`
		IDs  []int8 `json:"navi_unlock_ids"`
	}
	lines := bytes.Split(w.Body.Bytes(), []byte{'\n'})
	if len(lines) < 2 || json.Unmarshal(lines[1], &body) != nil || body.Mask != mask || !slices.Equal(body.IDs, want) {
		t.Fatalf("connect response lost ownership: %s", w.Body.String())
	}
}
