package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestExpandedNavigatorOwnershipDoesNotWrapLegacyMask(t *testing.T) {
	s := &store{coinFree: 1000, naviPurchasePrice: 10, naviUnlockFlag: 1,
		selectableNaviIDs: map[int8]struct{}{0: {}},
		naviCatalogIDs:    map[int8]struct{}{0: {}, 62: {}, 63: {}, 64: {}, 70: {}, 127: {}},
	}
	for _, id := range []int8{62, 63, 64, 70, 127} {
		if err := s.purchaseNavi(id); err != nil || !s.selectNavi(id) {
			t.Fatalf("purchase/select %d: %v", id, err)
		}
		balance := s.coinFree
		if err := s.purchaseNavi(id); err == nil || s.coinFree != balance {
			t.Fatalf("duplicate navigator %d was charged", id)
		}
	}
	if s.selectNavi(6) || s.selectNavi(-1) || s.coinFree != 950 {
		t.Fatal("navigator IDs wrapped or purchase amount changed")
	}
	mask, ids := s.naviUnlockState()
	want := []int8{0, 62, 63, 64, 70, 127}
	if mask != int64(1)|(int64(1)<<62) || !slices.Equal(ids, want) {
		t.Fatalf("connect ownership: mask %d, IDs %v", mask, ids)
	}
	// Exercise the real connect response, including array encoding for int8 IDs.
	a := &API{store: s, release: &release.Release{State: release.State{}}}
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
