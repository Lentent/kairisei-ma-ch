package httpapi

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
	"slices"
)

func cloneTeamBattleDropPlan(source []release.TeamBattleEnemyDrop) []release.TeamBattleEnemyDrop {
	if source == nil {
		return nil
	}
	result := make([]release.TeamBattleEnemyDrop, len(source))
	copy(result, source)
	for index := range result {
		result[index].Reward.CardSkillLevels = slices.Clone(result[index].Reward.CardSkillLevels)
		if chance := result[index].ChancePerMillion; chance != nil {
			copy := *chance
			result[index].ChancePerMillion = &copy
		}
	}
	return result
}

func planTeamBattleDrops(profile release.TeamBattleRewardProfile, enemyTypes []int8, seed string) ([]release.TeamBattleEnemyDrop, error) {
	if len(enemyTypes) == 0 {
		return nil, errors.New("drop plan has no battle segments")
	}
	rules := cloneTeamBattleDropPlan(profile.EnemyDrops)
	if profile.EnemyDrops == nil {
		// Explicit legacy policy: aggregate tangible rewards belong to the last
		// mandatory body. Do not invent unknown official part probabilities.
		last := -1
		for index, kind := range enemyTypes {
			if kind != 4 {
				last = index
			}
		}
		if last < 0 {
			return nil, nil
		}
		for _, reward := range profile.ResultRewards {
			if isTangibleTeamBattleReward(reward) {
				rules = append(rules, release.TeamBattleEnemyDrop{BattleIndex: last, Reward: reward})
			}
		}
	}
	plan := make([]release.TeamBattleEnemyDrop, 0, len(rules))
	for index, rule := range rules {
		if rule.BattleIndex < 0 || rule.BattleIndex >= len(enemyTypes) || rule.EnemyIndex < 0 || rule.EnemyIndex >= 4 || enemyTypes[rule.BattleIndex] == 4 || !isTangibleTeamBattleReward(rule.Reward) {
			return nil, errors.New("configured enemy drop has an invalid wave, enemy or reward")
		}
		if rule.ChancePerMillion != nil {
			chance := *rule.ChancePerMillion
			if chance < 0 || chance > 1000000 {
				return nil, errors.New("configured enemy drop chance is invalid")
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|drop=%d|wave=%d|enemy=%d", seed, index, rule.BattleIndex, rule.EnemyIndex)))
			if int(binary.BigEndian.Uint64(sum[:8])%1000000) >= chance {
				continue
			}
		}
		rule.ChancePerMillion = nil
		plan = append(plan, rule)
	}
	return plan, nil
}

func isTangibleTeamBattleReward(reward release.Reward) bool {
	return multiplayer.ValidBattleDrop(multiplayer.BattleDrop{RewardType: reward.Type, RewardTypeID: reward.RewardTypeID, Num: reward.Num})
}

func teamBattleDropPlanSpecs(plan []release.TeamBattleEnemyDrop, battleIndex int) []multiplayer.BattleDrop {
	drops := make([]multiplayer.BattleDrop, 0, len(plan))
	for _, entry := range plan {
		if entry.BattleIndex == battleIndex {
			drops = append(drops, multiplayer.BattleDrop{EnemyIndex: entry.EnemyIndex, RewardType: entry.Reward.Type, Num: entry.Reward.Num, RewardTypeID: entry.Reward.RewardTypeID})
		}
	}
	return drops
}

func teamBattleDropPlanWire(plan []release.TeamBattleEnemyDrop, enemyTypes []int8, battleIndex int) []any {
	plan = release.TeamBattleWaveDrops(plan, enemyTypes, battleIndex)
	rows := make([]any, 0, 4)
	for enemy := 0; enemy < 4; enemy++ {
		drops := make([]release.Reward, 0)
		for _, entry := range plan {
			if entry.BattleIndex == battleIndex && entry.EnemyIndex == enemy {
				drops = append(drops, entry.Reward)
			}
		}
		if len(drops) > 0 {
			rows = append(rows, map[string]any{"enemy_idx": enemy, "drops": drops})
		}
	}
	return rows
}

// A native solo report provides dead bits per wave. A completed authoritative
// room instead provides exactly the drops its engine released (DESTRUCT can
// destroy an enemy without releasing a drop). Never re-roll either at result.
type teamBattleDropReport struct {
	FameRewardsSet bool
	FameRewards    []release.Reward
	Turns          int
	EnemyDeadBits  []int
	Authoritative  bool
	ReleasedDrops  []release.TeamBattleEnemyDrop
}

func multiplayerDropReport(completed multiplayer.CompletedBattle) teamBattleDropReport {
	if completed.DropLedgerVersion == 1 {
		return teamBattleDropReport{Turns: completed.Turns, Authoritative: true, ReleasedDrops: completed.ReleasedDrops,
			FameRewardsSet: completed.FameRewardsSet, FameRewards: completed.FameRewards}
	}
	// Pre-upgrade completed records represent victories, with the historical
	// aggregate body-only policy. They have no bit field; do not erase rewards.
	return teamBattleDropReport{EnemyDeadBits: []int{1}}
}

func settledTeamBattleRewards(profile release.TeamBattleRewardProfile, context teamBattleContext, report teamBattleDropReport) []release.Reward {
	if !context.DropPlanSet && !report.Authoritative {
		return append([]release.Reward(nil), profile.ResultRewards...)
	}
	rewards := make([]release.Reward, 0, len(profile.ResultRewards))
	for _, reward := range profile.ResultRewards {
		if !isTangibleTeamBattleReward(reward) {
			rewards = append(rewards, reward)
		}
	}
	if report.Authoritative {
		for _, entry := range report.ReleasedDrops {
			rewards = append(rewards, entry.Reward)
		}
	} else {
		for _, entry := range context.DropPlan {
			released := entry.BattleIndex < len(report.EnemyDeadBits) && report.EnemyDeadBits[entry.BattleIndex]&(1<<entry.EnemyIndex) != 0
			if entry.EnemyIndex == 0 && !released {
				for wave := entry.BattleIndex + 1; wave < len(context.BattleEnemyTypes) && context.BattleEnemyTypes[wave] == 4; wave++ {
					if wave < len(report.EnemyDeadBits) && report.EnemyDeadBits[wave]&1 != 0 {
						released = true
					}
				}
			}
			if released {
				rewards = append(rewards, entry.Reward)
			}
		}
	}
	return rewards
}
