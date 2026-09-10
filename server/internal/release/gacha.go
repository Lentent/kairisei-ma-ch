package release

import (
	"errors"
	"math"
)

// CloneGachas also isolates nested reward slices from operations drafts and
// account snapshots; advancing one account never edits publication metadata.
func CloneGachas(source []GachaProfile) []GachaProfile {
	result := append([]GachaProfile(nil), source...)
	for i := range result {
		p := &result[i]
		p.CardIDs = append([]int(nil), p.CardIDs...)
		p.CardWeights = append([]int(nil), p.CardWeights...)
		p.RewardPool = cloneRewardPool(p.RewardPool)
		p.Steps = append([]GachaStep(nil), p.Steps...)
		for j := range p.Steps {
			p.Steps[j].RewardPool = cloneRewardPool(p.Steps[j].RewardPool)
		}
		p.Gifts = append([]GachaGiftRule(nil), p.Gifts...)
		for j := range p.Gifts {
			p.Gifts[j].Rewards = cloneGachaRewards(p.Gifts[j].Rewards)
		}
	}
	return result
}

func cloneGachaRewards(source []Reward) []Reward {
	result := append([]Reward(nil), source...)
	for i := range result {
		result[i].CardSkillLevels = append([]int16{}, result[i].CardSkillLevels...)
	}
	return result
}

func cloneRewardPool(source []WeightedReward) []WeightedReward {
	result := append([]WeightedReward(nil), source...)
	for i := range result {
		result[i].Reward.CardSkillLevels = append([]int16{}, result[i].Reward.CardSkillLevels...)
	}
	return result
}

func (p GachaProfile) CurrentStep() GachaProfile {
	if len(p.Steps) > 0 {
		step := p.Steps[min(max(p.PlayCount, 0), len(p.Steps)-1)]
		p.Price, p.RewardPool = step.Price, step.RewardPool
	}
	return p
}

func (p GachaProfile) CurrentGifts() []Reward {
	result := []Reward{}
	for _, rule := range p.Gifts {
		if p.PlayCount >= rule.FromPlay-1 && (rule.ToPlay == 0 || p.PlayCount < rule.ToPlay) {
			result = append(result, rule.Rewards...)
		}
	}
	return result
}

func ValidateRewardPool(pool []WeightedReward) error {
	if len(pool) == 0 {
		return errors.New("reward pool is empty")
	}
	total := 0
	for _, entry := range pool {
		// Keep the same bound as the client odds scale (percent * 100000).
		if entry.Weight <= 0 || entry.Weight > math.MaxInt/10000000-total {
			return errors.New("reward pool weights are invalid")
		}
		total += entry.Weight
		if err := ValidateGachaReward(entry.Reward); err != nil {
			return err
		}
	}
	return nil
}

func ValidateGachaReward(r Reward) error {
	if r.Num <= 0 || r.CardSkillLevels == nil {
		return errors.New("gacha reward is incomplete")
	}
	switch r.Type {
	case 4, 10, 12:
		if r.RewardTypeID != 0 {
			return errors.New("gacha currency reward has an ID")
		}
	case 6:
		if r.RewardTypeID <= 0 || r.CardLevel < 1 || r.CardFame < 1 || len(r.CardSkillLevels) == 0 {
			return errors.New("invalid gacha card reward")
		}
	case 8, 13, 15, 19:
		if r.RewardTypeID <= 0 {
			return errors.New("invalid gacha reward identity")
		}
	default:
		return errors.New("unsupported gacha reward type")
	}
	return nil
}

// ValidateGachaRules checks the static rule shape; runtime loaders additionally
// resolve every reward against the full CN masters before publication.
func ValidateGachaRules(p GachaProfile) error {
	mixed := len(p.RewardPool) > 0
	if mixed {
		if len(p.CardIDs) > 0 || p.UserSelectMax != 0 || p.GuaranteedCount != 0 || p.UnownedOnly || p.DailyFirstFree || p.CardNum != p.CardNumMax {
			return errors.New("mixed gacha has incompatible card-only rules")
		}
		if err := ValidateRewardPool(p.RewardPool); err != nil {
			return err
		}
	}
	if len(p.Steps) > 0 {
		if !mixed || p.PayType == 2 {
			return errors.New("step gacha requires a fixed-size reward pool")
		}
		for _, step := range p.Steps {
			if step.Price <= 0 {
				return errors.New("invalid step price")
			}
			if err := ValidateRewardPool(step.RewardPool); err != nil {
				return err
			}
		}
	}
	if p.UnownedOnly && (p.CardNum != 1 || p.CardNumMax != 1 || p.UserSelectMax != 0 || p.GuaranteedCount != 0) {
		return errors.New("unowned gacha must draw one card")
	}
	for _, gift := range p.Gifts {
		if gift.FromPlay <= 0 || (gift.ToPlay != 0 && gift.ToPlay < gift.FromPlay) || len(gift.Rewards) == 0 {
			return errors.New("invalid gacha gift interval")
		}
		for _, reward := range gift.Rewards {
			// Original ItemCache.AddItemCache(RewardInfo[]) creates an unseen
			// item with num=1. Published gift rules use unit item gifts; larger
			// grants belong in the ordinary ItemInfo delta result instead.
			if reward.Type == 8 && reward.Num != 1 {
				return errors.New("direct item gift must have unit quantity")
			}
			if err := ValidateGachaReward(reward); err != nil {
				return err
			}
		}
	}
	return nil
}
