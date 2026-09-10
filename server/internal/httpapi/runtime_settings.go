package httpapi

import "kairisei.local/server/internal/release"

// RuntimeSettings is shared operator policy, never part of a player snapshot.
// The zero value deliberately leaves local crystal purchases disabled.
type RuntimeSettings struct {
	CrystalPurchaseEnabled bool              `json:"crystal_purchase_enabled"`
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

func (h *accountBusinessHandler) ApplyRuntimeSettings(settings RuntimeSettings) {
	h.api.store.mu.Lock()
	defer h.api.store.mu.Unlock()
	h.api.store.localCrystalPurchaseEnabled = settings.CrystalPurchaseEnabled
	h.api.store.itemShopSettings = make(map[int]ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		h.api.store.itemShopSettings[setting.LineupID] = setting
	}
}

func (s *store) configuredItemShopLineup(lineup release.ItemShopLineup) release.ItemShopLineup {
	if setting, ok := s.itemShopSettings[lineup.LineupID]; ok && !lineup.Hidden {
		lineup.Disabled, lineup.Price = !setting.Enabled, setting.Price
	}
	return lineup
}
