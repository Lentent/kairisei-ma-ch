package gamestate

import "errors"

// Both master loading and account construction accept the same provenance.
func ValidItemGachaEvidence(value string) bool {
	switch value {
	case "INFERRED_OFFICIAL_DESCRIPTION_EXACT_CARD_BASE", "PLACEHOLDER_LOCAL_POLICY_OFFICIAL_CN_ITEM_DESCRIPTION", "USER_LOCAL_CUSTOM_BOX_20260930_TEMPLATE_8887", "LOCAL_POLICY_ADMIN_CUSTOM_BOX":
		return true
	default:
		return false
	}
}

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
