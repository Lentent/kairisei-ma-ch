package httpapi

import (
	"encoding/json"

	"kairisei.local/server/internal/release"
)

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
	state.TeamBattleSolo = teamBattleCatalogWithProgress(config.State.TeamBattleSolo, state.TeamBattleSolo)
	state.TeamBattlePastBossGroups = config.State.TeamBattlePastBossGroups
	state.TradeShopProfiles = config.State.TradeShopProfiles
	return state
}

// Operations owns definitions; a catalog refresh must not reset account clears.
func teamBattleCatalogWithProgress(catalog, account json.RawMessage) json.RawMessage {
	if len(catalog) == 0 {
		return account
	}
	decode := func(raw json.RawMessage) (map[string]json.RawMessage, map[string][]map[string]json.RawMessage, bool) {
		var top map[string]json.RawMessage
		if json.Unmarshal(raw, &top) != nil {
			return nil, nil, false
		}
		groups := make(map[string][]map[string]json.RawMessage)
		for _, key := range []string{"9", "10", "11", "12"} {
			var rows []map[string]json.RawMessage
			if len(top[key]) > 0 && json.Unmarshal(top[key], &rows) != nil {
				return nil, nil, false
			}
			groups[key] = rows
		}
		return top, groups, true
	}
	_, source, ok := decode(account)
	if !ok {
		return account
	}
	top, destination, ok := decode(catalog)
	if !ok {
		return account
	}
	states := make(map[int]int)
	for _, groups := range source {
		for _, group := range groups {
			var bosses []map[string]json.RawMessage
			if json.Unmarshal(group["10"], &bosses) != nil {
				return account
			}
			for _, boss := range bosses {
				var id, state int
				if json.Unmarshal(boss["0"], &id) != nil || json.Unmarshal(boss["10"], &state) != nil {
					return account
				}
				states[id] = max(states[id], state)
			}
		}
	}
	for key, groups := range destination {
		for _, group := range groups {
			var bosses []map[string]json.RawMessage
			if json.Unmarshal(group["10"], &bosses) != nil {
				return account
			}
			for _, boss := range bosses {
				var id int
				if json.Unmarshal(boss["0"], &id) != nil {
					return account
				}
				boss["10"], _ = json.Marshal(states[id])
			}
			group["10"], _ = json.Marshal(bosses)
		}
		if len(top[key]) > 0 {
			top[key], _ = json.Marshal(groups)
		}
	}
	result, _ := json.Marshal(top)
	return result
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
