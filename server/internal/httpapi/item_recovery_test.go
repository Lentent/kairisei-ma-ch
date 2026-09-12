package httpapi

import (
	"errors"
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

func TestRecoveryItemFunctions(t *testing.T) {
	for _, tc := range []struct {
		function string
		bp, ap   int
	}{
		{"BP_HEAL_FULL", 87, 1}, {"BP_HEAL_HALF", 64, 1}, {"BP_HEAL_30", 50, 1},
		{"AP_HEAL_FULL", 20, 3}, {"AP_HEAL_1", 20, 2},
	} {
		t.Run(tc.function, func(t *testing.T) {
			s := &store{bp: 20, bpMax: 87, ap: 1, apMax: 3, bpRecoveryInterval: time.Hour, apRecoveryInterval: time.Hour,
				items: map[int]release.Item{1002: {ItemID: 1002, Num: 1}}, itemDefinitions: map[int]release.ItemDefinition{1002: {ItemID: 1002, Function: tc.function}}}
			result, err := s.useItem(1002)
			if err != nil {
				t.Fatal(err)
			}
			if result.BP.Current != tc.bp || result.AP.Current != tc.ap || result.Item.Num != 0 {
				t.Fatalf("result %+v", result)
			}
			if _, err = s.useItem(1002); !errors.Is(err, errInsufficientMaterials) {
				t.Fatalf("empty inventory: %v", err)
			}
		})
	}
	t.Run("cap and full rejection", func(t *testing.T) {
		s := &store{bp: 80, bpMax: 87, ap: 3, apMax: 3, bpRecoveryInterval: time.Hour,
			items: map[int]release.Item{1002: {ItemID: 1002, Num: 2}}, itemDefinitions: map[int]release.ItemDefinition{1002: {ItemID: 1002, Function: "BP_HEAL_30"}}}
		if result, err := s.useItem(1002); err != nil || result.BP.Current != 87 || !s.bpNextRecovery.IsZero() {
			t.Fatalf("cap: %+v %v", result, err)
		}
		_, err := s.useItem(1002)
		var business *businessError
		if !errors.As(err, &business) || s.items[1002].Num != 1 {
			t.Fatalf("full: %v / %v", err, s.items)
		}
	})
}
