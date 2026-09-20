package game

import (
	"encoding/json"

	"kairisei.local/server/internal/gamestate"
)

// ContentConfiguration is a shared immutable operations snapshot. It contains
// public definitions only; purchase counts and accepted battle plans stay with
// their existing owners. Call under the account request lock, like gacha policy.
type ContentConfiguration struct {
	Revision uint64
	State    gamestate.State
}

type ContentConfigurator interface{ ApplyContentConfiguration(ContentConfiguration) }

func ApplyContentState(state gamestate.State, config ContentConfiguration) gamestate.State {
	if config.Revision == 0 {
		return state
	}
	state.TeamBattleRewards = config.State.TeamBattleRewards
	state.TeamBattleSolo = teamBattleCatalogWithProgress(config.State.TeamBattleSolo, state.TeamBattleSolo)
	state.TeamBattlePastBossGroups = config.State.TeamBattlePastBossGroups
	state.DisabledTeamBattleBossIDs = config.State.DisabledTeamBattleBossIDs
	state.TeamBattlePastBossGroups = FilterBattlePastBosses(state.TeamBattlePastBossGroups, state.DisabledTeamBattleBossIDs)
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

func (s *Account) ApplyTradeShopConfiguration(shops []gamestate.TradeShopProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tradeShopProfiles = make(map[int]gamestate.TradeShopProfile, len(shops))
	for _, shop := range shops {
		s.tradeShopProfiles[shop.TradeShopID] = shop
	}
}

func (s *Account) ApplyBattleCatalogConfiguration(state gamestate.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teamBattleSolo = teamBattleCatalogWithProgress(state.TeamBattleSolo, s.teamBattleSolo)
	s.disabledTeamBattleBossIDs = state.DisabledTeamBattleBossIDs
}

// Keep the full catalog in the account so closing an entry never erases clears.
func filterBattleGroupBosses(raw json.RawMessage, key string, disabled map[int]bool) json.RawMessage {
	var group map[string]json.RawMessage
	if json.Unmarshal(raw, &group) != nil {
		return raw
	}
	var bosses []json.RawMessage
	if json.Unmarshal(group[key], &bosses) != nil {
		return raw
	}
	kept := make([]json.RawMessage, 0, len(bosses))
	for _, boss := range bosses {
		var identity struct {
			ID int `json:"0"`
		}
		if json.Unmarshal(boss, &identity) != nil {
			return raw
		}
		if !disabled[identity.ID] {
			kept = append(kept, boss)
		}
	}
	group[key], _ = json.Marshal(kept)
	result, _ := json.Marshal(group)
	return result
}

func FilterBattlePastBosses(groups []json.RawMessage, disabled map[int]bool) []json.RawMessage {
	if len(disabled) == 0 {
		return groups
	}
	result := make([]json.RawMessage, len(groups))
	for i, raw := range groups {
		result[i] = filterBattleGroupBosses(raw, "13", disabled)
	}
	return result
}

func filterBattleCatalog(raw json.RawMessage, disabled map[int]bool) json.RawMessage {
	if len(disabled) == 0 {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		var groups []json.RawMessage
		if json.Unmarshal(top[key], &groups) != nil {
			return raw
		}
		for i, group := range groups {
			groups[i] = filterBattleGroupBosses(group, "10", disabled)
		}
		top[key], _ = json.Marshal(groups)
	}
	result, _ := json.Marshal(top)
	return result
}
