package httpapi

import (
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func gachaGiftsWire(profile gamestate.GachaProfile) []any {
	result := []any{}
	for _, reward := range profile.CurrentGifts() {
		result = append(result, map[string]any{"is_random": 0, "rewards": []any{map[string]any{"is_empty": 0, "reward": reward}}})
	}
	return result
}

// ItemCache.AddItemCache(ItemInfo[]) adds deltas, whereas the paid item uses
// UpdateItemCacheInfo and is an absolute snapshot. Do not send owned totals.
func gachaItemDeltas(result game.PresentReceiveResult) []gamestate.Item {
	items := []gamestate.Item{}
	for _, received := range result.Rewards {
		if received.Reward.Type == 8 && !received.InPresentBox {
			items = append(items, gamestate.Item{ItemID: received.Reward.RewardTypeID, Num: received.Reward.Num})
		}
	}
	return items
}
