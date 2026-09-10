package httpapi

import (
	"time"

	"kairisei.local/server/internal/release"
)

func inventoryReward(reward release.Reward) bool {
	switch reward.Type {
	case 6, 8, 15, 19:
		return true
	default:
		return false
	}
}

// Settlement cannot be undone by freeing bag space. Validate definitions and
// direct balances first; full inventory rewards are delivered through presents.
func (s *store) validateSettlementRewardsLocked(rewards []release.Reward) error {
	direct := make([]release.Reward, 0, len(rewards))
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
		if !inventoryReward(reward) {
			direct = append(direct, reward)
		}
	}
	return s.validateRewardBatchCapacityLocked(direct)
}

func (s *store) applySettlementRewardLocked(reward release.Reward, result *presentReceiveResult) error {
	return s.applyRewardOrPresentLocked(reward, result, "结算奖励")
}

func (s *store) applyRewardOrPresentLocked(reward release.Reward, result *presentReceiveResult, title string) error {
	if !inventoryReward(reward) || s.validateRewardBatchCapacityLocked([]release.Reward{reward}) == nil {
		return s.applyRewardLocked(reward, result)
	}
	// Account request serialization and the store lock keep allocation and the
	// completion receipt in the same saved state. Include unclaimed mission IDs.
	used := make(map[int64]bool, len(s.presents)+len(s.presentHistories)+len(s.missions))
	for _, group := range [][]release.Present{s.presents, s.presentHistories} {
		for _, present := range group {
			used[present.PresentID] = true
		}
	}
	for _, mission := range s.missions {
		used[mission.RewardPresent.PresentID] = true
	}
	presentID := time.Now().UnixNano()
	for used[presentID] {
		presentID++
	}
	stored := cloneReward(reward)
	if stored.CardSkillLevels == nil {
		stored.CardSkillLevels = []int16{}
	}
	empty := release.Reward{CardSkillLevels: []int16{}}
	s.presents = append(s.presents, release.Present{
		PresentID: presentID, IssuedAtUnix: time.Now().Unix(), Title: title, Reward: stored,
		Reward0: empty, Reward1: empty, Reward2: empty,
	})
	result.Rewards = append(result.Rewards, receivedReward{Reward: stored, UniqueID: []int64{}, InPresentBox: true})
	result.InPresentBox = true
	return nil
}

func battleAwardsInPresentBox(awards []teamBattleFameAward) int {
	for _, award := range awards {
		if award.Result.InPresentBox {
			return 1
		}
	}
	return 0
}
