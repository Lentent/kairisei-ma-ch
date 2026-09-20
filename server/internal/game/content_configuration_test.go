package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestExchangeConfigurationPreservesCountsAndChargesDisplayedPrice(t *testing.T) {
	s := &Account{items: map[int]gamestate.Item{4000: {ItemID: 4000, Num: 1000}}, itemDefinitions: map[int]gamestate.ItemDefinition{4000: {ItemID: 4000, MaxOwned: 9999}, 1000: {ItemID: 1000, MaxOwned: 9999}}, tradeShopPurchases: map[int]int{2: 2}}
	base := gamestate.State{TradeShopProfiles: []gamestate.TradeShopProfile{{TradeShopID: 1, Name: "交换所", Lineups: []gamestate.TradeShopLineupProfile{{LineupID: 2, LineupName: "药水", StockNum: 3, Prices: []gamestate.TradeShopPointProfile{{Type: 4, ID: 4000, Num: 25, PointCardCondition: []gamestate.TradeShopPointCardCondition{}}}, Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 1000, Num: 2, CardSkillLevels: []int16{}}}}}}}}
	s.ApplyTradeShopConfiguration(base.TradeShopProfiles)
	rows := s.TradeShopState()
	if len(rows) != 1 || rows[0].Lineups[0].StockRemain != 1 || rows[0].Lineups[0].Profile.Prices[0].Num != 25 {
		t.Fatal("display lost price or purchase count")
	}
	if _, err := s.BuyTradeShop(2, 1, nil); err != nil || s.items[4000].Num != 975 || s.items[1000].Num != 2 || s.tradeShopPurchases[2] != 3 {
		t.Fatalf("wrong exchange %v", err)
	}
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("limit not enforced")
	}
	base.TradeShopProfiles[0].Lineups[0].Disabled = true
	s.ApplyTradeShopConfiguration(base.TradeShopProfiles)
	if len(s.TradeShopState()) != 0 {
		t.Fatal("disabled offer displayed")
	}
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("stale page purchased disabled offer")
	}
	if s.tradeShopPurchases[2] != 3 {
		t.Fatal("operator changes reset player history")
	}
	s.ApplyTradeShopConfiguration(nil)
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.tradeShopPurchases[2] != 3 {
		t.Fatal("deleted shop accepted a stale purchase or lost history")
	}
	base.TradeShopProfiles[0].Lineups[0].Disabled = false
	s.ApplyTradeShopConfiguration(base.TradeShopProfiles)
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("restoring shop reset its purchase limit")
	}
	// The same reward in a newly configured shop has its own limit identity.
	second := cloneTradeShopProfile(base.TradeShopProfiles[0])
	second.TradeShopID, second.Lineups[0].LineupID = 61000001, 62000001
	s.ApplyTradeShopConfiguration(append(base.TradeShopProfiles, second))
	if rows := s.TradeShopState(); rows[0].Lineups[0].StockRemain != 0 || rows[1].Lineups[0].StockRemain != 3 {
		t.Fatal("new shop inherited another lineup's exhausted stock")
	}
	if _, err := s.BuyTradeShop(62000001, 1, nil); err != nil || s.tradeShopPurchases[2] != 3 || s.tradeShopPurchases[62000001] != 1 || s.items[4000].Num != 950 {
		t.Fatal("same-item offers share purchase counts", err)
	}
}

func TestContentCatalogKeepsProgressWhenMovingGroups(t *testing.T) {
	account := json.RawMessage(`{"9":[],"10":[{"10":[{"0":7,"10":2,"12":[]}]}],"11":[],"12":[]}`)
	catalog := json.RawMessage(`{"9":[{"10":[{"0":7,"10":0,"12":[{"0":99}]}]}],"10":[],"11":[],"12":[]}`)
	state := ApplyContentState(gamestate.State{TeamBattleSolo: account}, ContentConfiguration{Revision: 1, State: gamestate.State{TeamBattleSolo: catalog}})
	var top map[string][]struct {
		Bosses []struct {
			ID    int              `json:"0"`
			State int              `json:"10"`
			Cards []map[string]int `json:"12"`
		} `json:"10"`
	}
	if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
		t.Fatal(err)
	}
	if top["9"][0].Bosses[0].State != 2 || top["9"][0].Bosses[0].Cards[0]["0"] != 99 {
		t.Fatal("catalog update did not preserve progress and replace definitions")
	}
	s := &Account{teamBattleSolo: state.TeamBattleSolo}
	s.ApplyBattleCatalogConfiguration(gamestate.State{TeamBattleSolo: catalog, DisabledTeamBattleBossIDs: map[int]bool{7: true}})
	if err := json.Unmarshal(filterBattleCatalog(s.teamBattleSolo, s.disabledTeamBattleBossIDs), &top); err != nil || len(top["9"][0].Bosses) != 0 {
		t.Fatal("closed entry is still published", err)
	}
	s.ApplyBattleCatalogConfiguration(gamestate.State{TeamBattleSolo: catalog})
	if err := json.Unmarshal(s.teamBattleSolo, &top); err != nil || top["9"][0].Bosses[0].State != 2 {
		t.Fatal("reopening difficulty lost clear progress", err)
	}
}
