package httpapi

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"kairisei.local/server/internal/release"
)

func teamBattleFamePool(profile release.TeamBattleRewardProfile, policy release.TeamBattleFameBonusPolicy) []release.Reward {
	if profile.FameRewards != nil {
		return cloneRewards(profile.FameRewards)
	}
	for _, reward := range profile.ResultRewards {
		if slices.Contains(policy.EligibleRewardTypes, reward.Type) {
			return []release.Reward{cloneReward(reward)}
		}
	}
	return nil
}

func teamBattleFameReward(pool []release.Reward, seed string, arthur, box int) release.Reward {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|arthur=%d|fame-box=%d", seed, arthur, box)))
	return cloneReward(pool[binary.BigEndian.Uint64(sum[:8])%uint64(len(pool))])
}
