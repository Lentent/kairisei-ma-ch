package cnbootstrap

import (
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

func TestItemShopConfigMigrationPreservesOwnedItemBalances(t *testing.T) {
	state := gamestate.State{
		ItemShopConfigVersion: 1,
		Items: []gamestate.Item{
			{ItemID: 9010, Num: 7},
			{ItemID: 6062, Num: 3},
		},
		ItemShopTabs: []gamestate.ItemShopTab{{TabType: 0}},
	}
	seed := gamestate.State{
		ItemShopConfigVersion: accountstore.ItemShopConfigVersion,
		Items: []gamestate.Item{
			{ItemID: 9010, Num: 0},
			{ItemID: 1000, Num: 0},
		},
		ItemShopTabs: []gamestate.ItemShopTab{
			{TabType: 0, Lineup: []gamestate.ItemShopLineup{{LineupID: 992001}}},
			{TabType: 1}, {TabType: 2}, {TabType: 3}, {TabType: 4},
		},
	}

	changed, err := applyCNItemShopConfigMigration(&state, seed)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || state.ItemShopConfigVersion != accountstore.ItemShopConfigVersion ||
		len(state.ItemShopTabs) != 5 || state.ItemShopTabs[0].Lineup[0].LineupID != 992001 {
		t.Fatalf("item shop config was not replaced: %+v", state)
	}
	owned := make(map[int]int, len(state.Items))
	for _, item := range state.Items {
		owned[item.ItemID] = item.Num
	}
	if owned[9010] != 7 || owned[6062] != 3 || owned[1000] != 0 {
		t.Fatalf("item balances after migration = %+v", owned)
	}
}
