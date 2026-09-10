package httpapi

import (
	"fmt"

	"kairisei.local/server/internal/release"
)

// Every display and play resolves the same account-owned step and collection.
func (s *store) currentGachaLocked(profile release.GachaProfile) release.GachaProfile {
	profile = profile.CurrentStep()
	if !profile.UnownedOnly {
		return profile
	}
	ownedFamilies := make(map[int]bool)
	for id := range s.cardCollectionIDs {
		if definition, ok := s.cardDefinitions[id]; ok {
			ownedFamilies[definition.SameCardID] = true
		}
	}
	ids, weights := []int{}, []int{}
	for i, id := range profile.CardIDs {
		if !ownedFamilies[s.cardDefinitions[id].SameCardID] {
			ids, weights = append(ids, id), append(weights, profile.CardWeights[i])
		}
	}
	profile.CardIDs, profile.CardWeights = ids, weights
	return profile
}

func drawWeightedReward(pool []release.WeightedReward) (release.Reward, error) {
	indices, weights := make([]int, len(pool)), make([]int, len(pool))
	for i, entry := range pool {
		indices[i], weights[i] = i, entry.Weight
	}
	index, err := weightedGachaCard(indices, weights)
	if err != nil {
		return release.Reward{}, err
	}
	return cloneReward(pool[index].Reward), nil
}

func gachaPoolRewards(profile release.GachaProfile) []release.Reward {
	if len(profile.RewardPool) > 0 {
		rewards := make([]release.Reward, len(profile.RewardPool))
		for i, entry := range profile.RewardPool {
			rewards[i] = entry.Reward
		}
		return rewards
	}
	rewards := make([]release.Reward, len(profile.CardIDs))
	for i, id := range profile.CardIDs {
		rewards[i] = gachaCardReward(id)
	}
	return rewards
}

// Original GachaDefs maps sphere expectancy 8/9/10 to presentation grades
// 0/3/5; taking the numeric maximum would put a common sphere above a LEGEND
// buddy. The local policy selects the highest actual presentation grade.
func (s *store) gachaMixedResultExpectancy(rewards []release.Reward) int {
	expectancy := gachaResultExpectancy(rewards, s.cardDefinitions)
	grade := expectancy
	if grade == 6 { // Original EXRARE uses the MILLIONRARE opening.
		grade = 5
	}
	for _, reward := range rewards {
		if reward.Num <= 0 {
			continue
		}
		candidate, candidateGrade := 0, 0
		switch reward.Type {
		case 19:
			if _, ok := s.buddyDefinitions[reward.RewardTypeID]; ok {
				candidate, candidateGrade = 7, 7
			}
		case 15:
			definition := s.sphereDefinitions[reward.RewardTypeID]
			if definition.Type != "CHALICE" {
				continue
			}
			switch definition.Rarity {
			case "NORMAL":
				candidate, candidateGrade = 8, 0
			case "RARE":
				candidate, candidateGrade = 9, 3
			case "MILLIONRARE":
				candidate, candidateGrade = 10, 5
			}
		}
		if candidateGrade > grade || (candidateGrade == 0 && grade == 0 && candidate == 8) {
			expectancy, grade = candidate, candidateGrade
		}
	}
	return expectancy
}

func (s *store) validateGachaRulesLocked(profile release.GachaProfile) error {
	if err := release.ValidateGachaRules(profile); err != nil {
		return err
	}
	pools := [][]release.WeightedReward{profile.RewardPool}
	for _, step := range profile.Steps {
		pools = append(pools, step.RewardPool)
	}
	for _, pool := range pools {
		for _, entry := range pool {
			if err := s.validateRewardLocked(entry.Reward); err != nil {
				return fmt.Errorf("gacha %d: %w", profile.GachaID, err)
			}
		}
	}
	for _, gift := range profile.Gifts {
		for _, reward := range gift.Rewards {
			if err := s.validateRewardLocked(reward); err != nil {
				return err
			}
		}
	}
	return nil
}

func gachaGiftsWire(profile release.GachaProfile) []any {
	result := []any{}
	for _, reward := range profile.CurrentGifts() {
		result = append(result, map[string]any{"is_random": 0, "rewards": []any{map[string]any{"is_empty": 0, "reward": reward}}})
	}
	return result
}

// ItemCache.AddItemCache(ItemInfo[]) adds deltas, whereas the paid item uses
// UpdateItemCacheInfo and is an absolute snapshot. Do not send owned totals.
func gachaItemDeltas(result presentReceiveResult) []release.Item {
	items := []release.Item{}
	for _, received := range result.Rewards {
		if received.Reward.Type == 8 && !received.InPresentBox {
			items = append(items, release.Item{ItemID: received.Reward.RewardTypeID, Num: received.Reward.Num})
		}
	}
	return items
}
