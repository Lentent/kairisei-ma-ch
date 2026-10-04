package gamestate

import "fmt"

const GachaBoxSize = 50
const GachaBoxTemplates = 11

// Stock counts reward slots, independently of the quantity granted per win.
type GachaBoxReward struct {
	Reward Reward `json:"reward"`
	Stock  int    `json:"stock"`
}

type GachaBoxRound struct {
	Rewards []GachaBoxReward `json:"rewards"`
}

// Remaining pins the current box's actual rewards across configuration edits.
// Later boxes are replenished from the then-current publication. Keyed by group.
type GachaBoxProgress struct {
	Round     uint64           `json:"round"`
	Remaining []GachaBoxReward `json:"remaining"`
}

func CloneGachaBoxRewards(source []GachaBoxReward) []GachaBoxReward {
	result := append([]GachaBoxReward(nil), source...)
	for i := range result {
		result[i].Reward.CardSkillLevels = append([]int16{}, result[i].Reward.CardSkillLevels...)
	}
	return result
}

func CloneGachaBoxes(source map[int]GachaBoxProgress) map[int]GachaBoxProgress {
	result := make(map[int]GachaBoxProgress, len(source))
	for id, progress := range source {
		progress.Remaining = CloneGachaBoxRewards(progress.Remaining)
		result[id] = progress
	}
	return result
}

func GachaBoxStock(rewards []GachaBoxReward) int {
	total := 0
	for _, entry := range rewards {
		total += entry.Stock
	}
	return total
}

func ValidateGachaBoxRewards(rewards []GachaBoxReward, full bool) error {
	if len(rewards) == 0 || len(rewards) > GachaBoxSize {
		return fmt.Errorf("箱池每轮须配置 1–50 项奖励")
	}
	total := 0
	for _, entry := range rewards {
		if entry.Stock < 1 || entry.Stock > GachaBoxSize {
			return fmt.Errorf("箱池奖励份数须为 1–50")
		}
		total += entry.Stock
		if err := ValidateGachaReward(entry.Reward); err != nil {
			return err
		}
	}
	if total > GachaBoxSize || (full && total != GachaBoxSize) {
		return fmt.Errorf("箱池每轮总份数须为 50；当前 %d", total)
	}
	return nil
}

func ValidateGachaBoxes(boxes map[int]GachaBoxProgress) error {
	for id, progress := range boxes {
		if id <= 0 || progress.Round == 0 {
			return fmt.Errorf("invalid gacha box progress identity")
		}
		if err := ValidateGachaBoxRewards(progress.Remaining, false); err != nil {
			return err
		}
	}
	return nil
}

func ValidateGachaBoxRules(p GachaProfile) error {
	if len(p.BoxRounds) != GachaBoxTemplates {
		return fmt.Errorf("箱池须配置前 10 轮和第 11 轮起的循环模板，共 11 套")
	}
	if p.CardNum != 1 || p.CardNumMax != 1 || p.PlayCountMax != 0 || len(p.CardIDs) != 0 || len(p.RewardPool) != 0 || len(p.Steps) != 0 || len(p.Gifts) != 0 || len(p.CardFames) != 0 || p.UserSelectMax != 0 || p.GuaranteedCount != 0 || p.UnownedOnly || p.DailyFirstFree {
		return fmt.Errorf("箱池须为单抽、不限次数，不能附加普通池或阶段池规则")
	}
	for i, round := range p.BoxRounds {
		if err := ValidateGachaBoxRewards(round.Rewards, true); err != nil {
			return fmt.Errorf("第 %d 套：%w", i+1, err)
		}
	}
	return nil
}
