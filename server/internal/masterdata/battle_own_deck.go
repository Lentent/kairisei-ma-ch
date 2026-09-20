package masterdata

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// These are independent local quest IDs, not aliases for persisted progress.
// Only publication identity and entry mode differ from the validated source.
const ownDeckBossIDOffset = 100000000

type battleOwnDeckVariant struct {
	SourceBossID int `json:"source_boss_id"`
	BossID       int `json:"boss_id"`
}

// Offer the existing own-deck mode for every validated standalone standard
// encounter. New variants remain closed until an operator enables them; explicit
// entries keep their original defaults. This changes no prepared master bytes.
func withConfigurableOwnDeckEntries(master BattleRuntimeMaster) (BattleRuntimeMaster, error) {
	known := make(map[int]bool, len(master.OwnDeckVariants))
	for _, variant := range master.OwnDeckVariants {
		known[variant.SourceBossID] = true
	}
	standalone := make(map[int]bool)
	for _, reward := range master.Rewards {
		standalone[reward.BossID] = reward.StageQuestAreaID == 0 && reward.TowerID == 0
	}
	master.OwnDeckVariants = slices.Clone(master.OwnDeckVariants)
	master.OptionalOwnDeckBossIDs = slices.Clone(master.OptionalOwnDeckBossIDs)
	for _, raw := range master.Groups {
		var group BattleGroupIdentity
		if err := json.Unmarshal(raw, &group); err != nil {
			return BattleRuntimeMaster{}, err
		}
		for _, boss := range group.Bosses {
			if known[boss.BossID] || !standalone[boss.BossID] || boss.BossID <= 0 ||
				boss.BossID >= ownDeckBossIDOffset || boss.OnlyMyDeck != 0 || boss.StartRule != 0 {
				continue
			}
			id := boss.BossID + ownDeckBossIDOffset
			master.OwnDeckVariants = append(master.OwnDeckVariants, battleOwnDeckVariant{SourceBossID: boss.BossID, BossID: id})
			master.OptionalOwnDeckBossIDs = append(master.OptionalOwnDeckBossIDs, id)
			known[boss.BossID] = true
		}
	}
	if err := validateBattleOwnDeckVariants(master); err != nil {
		return BattleRuntimeMaster{}, err
	}
	return master, nil
}

func validateBattleOwnDeckVariants(master BattleRuntimeMaster) error {
	if len(master.OwnDeckVariants) == 0 {
		return nil
	}
	sources := make(map[int]battleEntryRules)
	reserved := make(map[int]bool)
	for _, raw := range master.Groups {
		var group BattleGroupIdentity
		if err := json.Unmarshal(raw, &group); err != nil {
			return err
		}
		for _, boss := range group.Bosses {
			sources[boss.BossID] = boss.battleEntryRules
		}
	}
	for _, replay := range master.Replays {
		reserved[replay.BossID] = true
	}
	for _, id := range master.ExcludedBossIDs {
		reserved[id] = true
	}
	for id := range battleCanonicalBossAliases(master.DeduplicatedFamilies) {
		reserved[id] = true
	}
	seen := make(map[int]bool)
	for _, variant := range master.OwnDeckVariants {
		rules, exists := sources[variant.SourceBossID]
		if !exists || variant.SourceBossID <= 0 || variant.SourceBossID >= ownDeckBossIDOffset ||
			variant.BossID != variant.SourceBossID+ownDeckBossIDOffset || reserved[variant.BossID] ||
			seen[variant.SourceBossID] || rules.OnlyMyDeck != 0 || rules.StartRule != 0 {
			return fmt.Errorf("invalid local own-deck quest mapping %d -> %d", variant.SourceBossID, variant.BossID)
		}
		seen[variant.SourceBossID] = true
		reserved[variant.BossID] = true
	}
	return nil
}

// Build a publication view from the complete prepared master. The source master
// and its provenance/summary stay unchanged; no official inputs are needed here.
// Admin and player entry points use this same projection.
func ExpandBattleEntries(master BattleRuntimeMaster) (BattleRuntimeMaster, error) {
	if err := validateBattleOwnDeckVariants(master); err != nil {
		return BattleRuntimeMaster{}, err
	}
	if len(master.OwnDeckVariants) == 0 {
		return master, nil
	}
	bySource := make(map[int]int, len(master.OwnDeckVariants))
	for _, variant := range master.OwnDeckVariants {
		bySource[variant.SourceBossID] = variant.BossID
	}
	projectGroups := func(groups []json.RawMessage, bossKey string) ([]json.RawMessage, error) {
		result := make([]json.RawMessage, 0, len(groups))
		for _, raw := range groups {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raw, &group); err != nil {
				return nil, err
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group[bossKey], &bosses); err != nil {
				return nil, err
			}
			selected := make([]map[string]json.RawMessage, 0, len(bosses)*2)
			for _, boss := range bosses {
				selected = append(selected, boss)
				var sourceID int
				if err := json.Unmarshal(boss["0"], &sourceID); err != nil {
					return nil, err
				}
				if targetID, exists := bySource[sourceID]; exists {
					variant := maps.Clone(boss)
					variant["0"] = json.RawMessage(fmt.Sprint(targetID))
					variant["1"], variant["24"] = json.RawMessage("1"), json.RawMessage("1")
					selected = append(selected, variant)
				}
			}
			if len(selected) == len(bosses) {
				result = append(result, raw)
				continue
			}
			// Rule variants belong beside their source difficulty. Keep the
			// parent identity stable; progress remains keyed by the distinct boss ID.
			group[bossKey], _ = json.Marshal(selected)
			encoded, err := json.Marshal(group)
			if err != nil {
				return nil, err
			}
			result = append(result, encoded)
		}
		return result, nil
	}
	var err error
	master.Groups, err = projectGroups(master.Groups, "10")
	if err != nil {
		return BattleRuntimeMaster{}, err
	}
	master.PastBossGroups, err = projectGroups(master.PastBossGroups, "13")
	if err != nil {
		return BattleRuntimeMaster{}, err
	}
	// Clone source slices before appending; retain enemy parties, RNG seed,
	// costs, rewards and per-monster/part drop coordinates verbatim.
	master.Replays = slices.Clone(master.Replays)
	for _, replay := range master.Replays {
		if id, exists := bySource[replay.BossID]; exists {
			replay.BossID = id
			master.Replays = append(master.Replays, replay)
		}
	}
	master.Rewards = slices.Clone(master.Rewards)
	for _, reward := range master.Rewards {
		if id, exists := bySource[reward.BossID]; exists {
			reward.BossID = id
			master.Rewards = append(master.Rewards, reward)
		}
	}
	master.Recommendations = slices.Clone(master.Recommendations)
	for _, recommendation := range master.Recommendations {
		if id, exists := bySource[recommendation.BossID]; exists {
			recommendation.BossID = id
			master.Recommendations = append(master.Recommendations, recommendation)
		}
	}
	// A projected view must not be projected a second time.
	master.OwnDeckVariants = nil
	return master, nil
}
