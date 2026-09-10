package httpapi

import (
	"testing"

	"kairisei.local/server/internal/release"
)

func TestItemShopOperatorPolicyControlsDisplayAndCharge(t *testing.T) {
	s := &store{
		gold: 30000,
		itemShopTabs: []release.ItemShopTab{{TabType: 0, Lineup: []release.ItemShopLineup{{
			LineupID: 602004, PayType: 1, Price: 10000, BuyNumMax: 99,
			Interiors: []release.ItemShopInterior{{BuyType: 1, BuyTypeID: 3999, Num: 1}},
		}}}},
		items:           map[int]release.Item{},
		itemDefinitions: map[int]release.ItemDefinition{3999: {ItemID: 3999, MaxOwned: 9999}},
	}
	h := &accountBusinessHandler{api: &API{store: s}}
	h.ApplyRuntimeSettings(RuntimeSettings{ItemShop: []ItemShopSetting{{LineupID: 602004, Enabled: true, Price: 12000}}})
	tabs, owned := s.itemShopState()
	visible := itemShopTabsWire(tabs, owned)[0].(map[string]any)["lineup"].([]any)
	if len(visible) != 1 || visible[0].(map[string]any)["price"] != 12000 {
		t.Fatalf("operator price not displayed: %+v", visible)
	}
	if _, err := s.buyItemShop(602004, 2); err != nil || s.gold != 6000 || s.items[3999].Num != 2 {
		t.Fatalf("operator purchase: gold=%d items=%+v err=%v", s.gold, s.items, err)
	}
	h.ApplyRuntimeSettings(RuntimeSettings{ItemShop: []ItemShopSetting{{LineupID: 602004, Enabled: false, Price: 12000}}})
	tabs, owned = s.itemShopState()
	if len(itemShopTabsWire(tabs, owned)[0].(map[string]any)["lineup"].([]any)) != 0 {
		t.Fatal("disabled offer remained visible")
	}
	if _, err := s.buyItemShop(602004, 1); err == nil || s.gold != 6000 || s.items[3999].Num != 2 {
		t.Fatal("disabled offer changed player balances")
	}
	if base := s.itemShopTabs[0].Lineup[0]; base.Disabled || base.Price != 10000 {
		t.Fatal("operator override mutated the seed copied into persistence")
	}
}
