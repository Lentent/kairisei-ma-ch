package game

import (
	"encoding/json"
)

// These are independent fields of the stock CN TeamBattleBossInfo, not rules
// inferred from the display name or the numerical difficulty suffix.
type teamBattleEntryRules struct {
	BossID     int `json:"0"`
	OnlyMyDeck int `json:"1"`
	BPUse      int `json:"5"`
	BPUseHalf  int `json:"6"`
	Continue   int `json:"7"`
	StartRule  int `json:"24"`
	StageType  int `json:"-"`
}

func (rules teamBattleEntryRules) AllowsMultiplayer() bool {
	// DeckTeamSt.OnCreateRm/OnEnterRm retain the WORLD_BOSS exception.
	return (rules.OnlyMyDeck == 0 || rules.StageType == 20) &&
		rules.StartRule != 1 && rules.StartRule != 2
}

func (rules teamBattleEntryRules) AllowsSolo() bool {
	// DeckTeamSt.SetNotification/OnSolo disable solo for MULTI_ONLY.
	return rules.StartRule != 3
}

func TeamBattleGroupEntryRules(group any, bossID int) (teamBattleEntryRules, bool) {
	encoded, err := json.Marshal(group)
	if err != nil {
		return teamBattleEntryRules{}, false
	}
	var identity struct {
		StageType int                    `json:"1"`
		Bosses    []teamBattleEntryRules `json:"10"`
	}
	if json.Unmarshal(encoded, &identity) != nil {
		return teamBattleEntryRules{}, false
	}
	for _, rules := range identity.Bosses {
		if rules.BossID == bossID {
			rules.StageType = identity.StageType
			return rules, true
		}
	}
	return teamBattleEntryRules{}, false
}

func (s *Account) TeamBattleEntryRulesForBoss(bossID int) (teamBattleEntryRules, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.teamBattleEntryRulesForBossLocked(bossID)
}

func (s *Account) teamBattleEntryRulesForBossLocked(bossID int) (teamBattleEntryRules, bool) {
	if s.disabledTeamBattleBossIDs[bossID] {
		return teamBattleEntryRules{}, false
	}
	if _, group, found := TeamBattleGroupForBoss(s.teamBattleSolo, bossID); found {
		return TeamBattleGroupEntryRules(group, bossID)
	}
	if _, floor, found := s.towerQuestBattleForBossLocked(bossID); found {
		var rules teamBattleEntryRules
		if json.Unmarshal(floor.Boss, &rules) == nil && rules.BossID == bossID {
			return rules, true
		}
	}
	return teamBattleEntryRules{}, false
}
