package httpapi

import "kairisei.local/server/internal/release"

// ContentConfiguration is a shared immutable operations snapshot. It contains
// public definitions only; purchase counts and accepted battle plans stay with
// their existing owners. Call under the account request lock, like gacha policy.
type ContentConfiguration struct {
	Revision uint64
	State    release.State
}

type ContentConfigurator interface{ ApplyContentConfiguration(ContentConfiguration) }

func ApplyContentState(state release.State, config ContentConfiguration) release.State {
	if config.Revision == 0 {
		return state
	}
	state.TeamBattleRewards = config.State.TeamBattleRewards
	state.TeamBattleSolo = config.State.TeamBattleSolo
	state.TeamBattlePastBossGroups = config.State.TeamBattlePastBossGroups
	state.TradeShopProfiles = config.State.TradeShopProfiles
	return state
}

func (h *accountBusinessHandler) ApplyContentConfiguration(config ContentConfiguration) {
	if config.Revision == 0 || h.contentRevision == config.Revision {
		return
	}
	s := h.api.store
	s.mu.Lock()
	defer s.mu.Unlock()
	h.api.release.State = ApplyContentState(h.api.release.State, config)
	s.tradeShopProfiles = make(map[int]release.TradeShopProfile, len(config.State.TradeShopProfiles))
	for _, shop := range config.State.TradeShopProfiles {
		s.tradeShopProfiles[shop.TradeShopID] = shop
	}
	h.contentRevision = config.Revision
}
