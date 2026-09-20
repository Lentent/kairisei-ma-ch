package game

import (
	"kairisei.local/server/internal/gamestate"
)

// RuntimeSettings is shared operator policy, never part of a player snapshot.
// The zero value deliberately leaves local crystal purchases disabled.
type RuntimeSettings struct {
	CrystalPurchaseEnabled bool              `json:"crystal_purchase_enabled"`
	TeamBattleSpeed        int               `json:"team_battle_speed"`
	ItemShop               []ItemShopSetting `json:"item_shop,omitempty"`
}

type ItemShopSetting struct {
	LineupID int  `json:"lineup_id"`
	Enabled  bool `json:"enabled"`
	Price    int  `json:"price"`
}

type RuntimeConfigurator interface {
	ApplyRuntimeSettings(RuntimeSettings)
}

func (s *Account) ApplyRuntimeSettings(settings RuntimeSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localCrystalPurchaseEnabled = settings.CrystalPurchaseEnabled
	s.itemShopSettings = make(map[int]ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		s.itemShopSettings[setting.LineupID] = setting
	}
}

func (s *Account) configuredItemShopLineup(lineup gamestate.ItemShopLineup) gamestate.ItemShopLineup {
	if setting, ok := s.itemShopSettings[lineup.LineupID]; ok && !lineup.Hidden {
		lineup.Disabled, lineup.Price = !setting.Enabled, setting.Price
	}
	return lineup
}
