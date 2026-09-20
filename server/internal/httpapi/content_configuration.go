package httpapi

import (
	"kairisei.local/server/internal/game"
)

func (h *accountBusinessHandler) ApplyContentConfiguration(config game.ContentConfiguration) {
	if config.Revision == 0 || h.contentRevision == config.Revision {
		return
	}
	h.api.initialState = game.ApplyContentState(h.api.initialState, config)
	h.api.account.ApplyTradeShopConfiguration(config.State.TradeShopProfiles)
	h.api.account.ApplyBattleCatalogConfiguration(h.api.initialState)
	h.contentRevision = config.Revision
}
