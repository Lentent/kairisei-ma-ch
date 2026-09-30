package gamestate

import "errors"

// Item boxes use the generic collection reward delivery path. Ordinary paid
// gacha keeps its existing reward restrictions and client presentation contract.
func ValidateItemGachaReward(r Reward) error {
	if r.Type == 14 || r.Type == 18 {
		if r.Num != 1 || r.RewardTypeID <= 0 || r.CardSkillLevels == nil {
			return errors.New("item gacha collection reward must be one identified skin or honor")
		}
		return nil
	}
	return ValidateGachaReward(r)
}

func ValidateItemGachaRewardPool(pool []WeightedReward) error {
	validationPool := append([]WeightedReward(nil), pool...)
	for i, entry := range pool {
		if err := ValidateItemGachaReward(entry.Reward); err != nil {
			return err
		}
		if entry.Reward.Type == 14 || entry.Reward.Type == 18 {
			// Reuse the ordinary weight/overflow checks without broadening the
			// ordinary gacha API or mutating the original reward identity.
			validationPool[i].Reward.Type = 8
		}
	}
	return ValidateRewardPool(validationPool)
}
