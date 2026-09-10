package httpapi

import (
	"kairisei.local/server/internal/release"
	"testing"
)

func TestExchangeConfigurationPreservesCountsAndChargesDisplayedPrice(t *testing.T) {
	s := &store{items: map[int]release.Item{4000: {ItemID: 4000, Num: 1000}}, itemDefinitions: map[int]release.ItemDefinition{4000: {ItemID: 4000, MaxOwned: 9999}, 1000: {ItemID: 1000, MaxOwned: 9999}}, tradeShopPurchases: map[int]int{2: 2}}
	base := release.State{TradeShopProfiles: []release.TradeShopProfile{{TradeShopID: 1, Name: "交换所", Lineups: []release.TradeShopLineupProfile{{LineupID: 2, LineupName: "药水", StockNum: 3, Prices: []release.TradeShopPointProfile{{Type: 4, ID: 4000, Num: 25, PointCardCondition: []release.TradeShopPointCardCondition{}}}, Rewards: []release.Reward{{Type: 8, RewardTypeID: 1000, Num: 2, CardSkillLevels: []int16{}}}}}}}}
	h := &accountBusinessHandler{api: &API{store: s, release: &release.Release{}}}
	h.ApplyContentConfiguration(ContentConfiguration{Revision: 1, State: base})
	rows := s.tradeShopState()
	if len(rows) != 1 || rows[0].Lineups[0].StockRemain != 1 || rows[0].Lineups[0].Profile.Prices[0].Num != 25 {
		t.Fatal("display lost price or purchase count")
	}
	if _, err := s.buyTradeShop(2, 1, nil); err != nil || s.items[4000].Num != 975 || s.items[1000].Num != 2 || s.tradeShopPurchases[2] != 3 {
		t.Fatalf("wrong exchange %v", err)
	}
	if _, err := s.buyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("limit not enforced")
	}
	base.TradeShopProfiles[0].Lineups[0].Disabled = true
	h.ApplyContentConfiguration(ContentConfiguration{Revision: 2, State: base})
	if len(s.tradeShopState()) != 0 {
		t.Fatal("disabled offer displayed")
	}
	if _, err := s.buyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("stale page purchased disabled offer")
	}
	if s.tradeShopPurchases[2] != 3 {
		t.Fatal("operator changes reset player history")
	}
}
