package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestItemShopOperatorPolicyControlsDisplayAndCharge(t *testing.T) {
	s := &Account{
		gold: 30000,
		itemShopTabs: []gamestate.ItemShopTab{{TabType: 0, Lineup: []gamestate.ItemShopLineup{{
			LineupID: 602004, PayType: 1, Price: 10000, BuyNumMax: 99,
			Interiors: []gamestate.ItemShopInterior{{BuyType: 1, BuyTypeID: 3999, Num: 1}},
		}}}},
		items:           map[int]gamestate.Item{},
		itemDefinitions: map[int]gamestate.ItemDefinition{3999: {ItemID: 3999, MaxOwned: 9999}},
	}
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: []ItemShopSetting{{LineupID: 602004, Enabled: true, Price: 12000}}})
	tabs, _ := s.ItemShopState()
	visible := tabs[0].Lineup
	if len(visible) != 1 || visible[0].Price != 12000 {
		t.Fatalf("operator price not displayed: %+v", visible)
	}
	if _, err := s.BuyItemShop(602004, 2); err != nil || s.gold != 6000 || s.items[3999].Num != 2 {
		t.Fatalf("operator purchase: gold=%d items=%+v err=%v", s.gold, s.items, err)
	}
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: []ItemShopSetting{{LineupID: 602004, Enabled: false, Price: 12000}}})
	tabs, _ = s.ItemShopState()
	if !tabs[0].Lineup[0].Disabled {
		t.Fatal("disabled offer remained visible")
	}
	if _, err := s.BuyItemShop(602004, 1); err == nil || s.gold != 6000 || s.items[3999].Num != 2 {
		t.Fatal("disabled offer changed player balances")
	}
	if base := s.itemShopTabs[0].Lineup[0]; base.Disabled || base.Price != 10000 {
		t.Fatal("operator override mutated the seed copied into persistence")
	}
}
