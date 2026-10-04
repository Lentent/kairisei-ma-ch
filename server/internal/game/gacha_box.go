package game

import (
	"fmt"
	"math"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) GachaBoxOddsMessage(profile gamestate.GachaProfile) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lines := []string{fmt.Sprintf("第 %d 轮，每轮 50 份；当前概率 = 剩余份数 / 本轮总剩余份数。抽空自动换轮，第 11 轮起无限重复。", profile.BoxRound)}
	for _, entry := range profile.RewardPool {
		reward := entry.Reward
		name := ""
		switch reward.Type {
		case 4:
			name = "金币"
		case 10:
			name = "免费水晶"
		case 12:
			name = "BP"
		case 6:
			name = s.cardDefinitions[reward.RewardTypeID].Name
		case 8:
			name = s.itemDefinitions[reward.RewardTypeID].Name
		case 13:
			name = fmt.Sprintf("素材卡 %d", reward.RewardTypeID)
		case 15:
			name = s.sphereDefinitions[reward.RewardTypeID].Name
		case 19:
			name = s.buddyDefinitions[reward.RewardTypeID].Name
		}
		if name == "" {
			name = fmt.Sprintf("奖励 %d:%d", reward.Type, reward.RewardTypeID)
		}
		lines = append(lines, fmt.Sprintf("%s × %d：剩余 %d 份", name, reward.Num, entry.Weight))
	}
	return strings.Join(lines, "\n")
}

func (s *Account) gachaBoxLocked(profile gamestate.GachaProfile) gamestate.GachaBoxProgress {
	if progress, ok := s.gachaBoxes[profile.GroupID]; ok {
		return progress
	}
	return newGachaBox(profile, 1)
}

func newGachaBox(profile gamestate.GachaProfile, round uint64) gamestate.GachaBoxProgress {
	index := int(min(round, uint64(gamestate.GachaBoxTemplates))) - 1
	return gamestate.GachaBoxProgress{Round: round, Remaining: gamestate.CloneGachaBoxRewards(profile.BoxRounds[index].Rewards)}
}

func currentBoxGacha(profile gamestate.GachaProfile, progress gamestate.GachaBoxProgress) gamestate.GachaProfile {
	profile.BoxRound = progress.Round
	profile.RewardPool = make([]gamestate.WeightedReward, len(progress.Remaining))
	for i, entry := range progress.Remaining {
		profile.RewardPool[i] = gamestate.WeightedReward{Reward: entry.Reward, Weight: entry.Stock}
	}
	mode := ""
	if progress.Round >= 11 {
		mode = "（循环池）"
	}
	status := fmt.Sprintf("第 %d 轮%s，剩余 %d/50 份", progress.Round, mode, gamestate.GachaBoxStock(progress.Remaining))
	profile.Name += " · " + status
	payment := map[int]string{2: "友情点", 3: "水晶", 4: fmt.Sprintf("道具 %d", profile.PayTypeID), 6: "付费水晶"}[profile.PayType]
	profile.BuyMessage = fmt.Sprintf("%s。消耗 %d %s 抽取 1 份奖励吗？抽空自动进入下一轮。", status, profile.Price, payment)
	profile.SubMessage = "每轮 50 份，抽中扣库存；前 10 轮独立，第 11 轮起无限重复。"
	return profile
}

// Prepare on a deep copy, then commit only after payment and reward validation.
func (s *Account) drawGachaBoxLocked(profile gamestate.GachaProfile) (gamestate.Reward, gamestate.GachaBoxProgress, error) {
	progress := s.gachaBoxLocked(profile)
	progress.Remaining = gamestate.CloneGachaBoxRewards(progress.Remaining)
	indices, weights := make([]int, len(progress.Remaining)), make([]int, len(progress.Remaining))
	for i, entry := range progress.Remaining {
		indices[i], weights[i] = i, entry.Stock
	}
	index, err := weightedGachaCard(indices, weights)
	if err != nil {
		return gamestate.Reward{}, progress, err
	}
	reward := cloneReward(progress.Remaining[index].Reward)
	progress.Remaining[index].Stock--
	if progress.Remaining[index].Stock == 0 {
		progress.Remaining = append(progress.Remaining[:index], progress.Remaining[index+1:]...)
	}
	if len(progress.Remaining) == 0 {
		// Saturation at the machine counter limit never disables the repeating pool.
		round := progress.Round
		if round < math.MaxUint64 {
			round++
		}
		progress = newGachaBox(profile, round)
	}
	return reward, progress, nil
}
