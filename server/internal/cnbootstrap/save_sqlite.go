package cnbootstrap

import (
	"errors"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

func applyCNItemShopConfigMigration(state *gamestate.State, seed gamestate.State) (bool, error) {
	if state.ItemShopConfigVersion >= seed.ItemShopConfigVersion {
		return false, nil
	}
	if seed.ItemShopConfigVersion < accountstore.ItemShopConfigVersion || len(seed.ItemShopTabs) != 5 {
		return false, errors.New("CN save item shop migration seed is incomplete")
	}
	owned := make(map[int]struct{}, len(state.Items))
	for _, item := range state.Items {
		if item.ItemID <= 0 || item.Num < 0 {
			return false, errors.New("CN save persisted item is invalid during item shop migration")
		}
		owned[item.ItemID] = struct{}{}
	}
	for _, item := range seed.Items {
		if _, exists := owned[item.ItemID]; exists {
			continue
		}
		state.Items = append(state.Items, item)
		owned[item.ItemID] = struct{}{}
	}
	state.ItemShopConfigVersion = seed.ItemShopConfigVersion
	state.ItemShopTabs = accountstore.CloneItemShopTabs(seed.ItemShopTabs)
	return true, nil
}
