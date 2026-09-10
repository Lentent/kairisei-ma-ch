package httpapi

import (
	"errors"
	"fmt"
	"time"

	"kairisei.local/server/internal/release"
)

// GachaConfiguration is supplied by the local operations store, under the
// account request lock. Player counters and daily claims remain account-owned.
type GachaConfiguration struct {
	Disabled  bool
	Profile   release.GachaProfile
	StartUnix int64
	EndUnix   int64
}

type GachaConfigurator interface {
	ApplyGachaConfiguration(uint64, []GachaConfiguration)
}

func (h *accountBusinessHandler) ApplyGachaConfiguration(revision uint64, configs []GachaConfiguration) {
	s := h.api.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if h.gachaRevision == revision {
		return
	}
	for _, config := range configs {
		current := findGachaProfile(s.gachas, config.Profile.GachaID)
		if current == nil || isOnboardingGachaID(current.GachaID) {
			continue
		}
		profile := cloneGachaProfiles([]release.GachaProfile{config.Profile})[0]
		profile.PlayCount = current.PlayCount
		*current = profile
		if validateGachaSelection(profile, s.gachaSelections[profile.GachaID]) != nil {
			delete(s.gachaSelections, profile.GachaID)
		}
	}
	s.gachaWindows = make(map[int][2]int64, len(configs))
	for _, config := range configs {
		s.gachaWindows[config.Profile.GachaID] = [2]int64{config.StartUnix, config.EndUnix}
		if config.Disabled {
			s.gachaWindows[config.Profile.GachaID] = [2]int64{0, 1}
		}
	}
	h.gachaRevision = revision
}

func (s *store) gachaScheduledLocked(id int) bool {
	window, configured := s.gachaWindows[id]
	if !configured {
		return true
	}
	now := time.Now().Unix()
	return now >= window[0] && (window[1] == 0 || now < window[1])
}

// PreviewGachaWeights shares the integer rounding used by GachaOddsShow.
// Values are percentages multiplied by 100000 and always total 10000000.
func PreviewGachaWeights(weights []int) ([]int, error) { return scaledGachaOdds(weights) }

type GachaOddsStage struct {
	Name      string           `json:"name"`
	Rarity    int              `json:"rarity,omitempty"`
	DrawCount int              `json:"draw_count"`
	CardIDs   []int            `json:"card_ids"`
	Odds      []int            `json:"odds_scaled"`
	Rewards   []release.Reward `json:"rewards,omitempty"`
}

// PreviewGachaStages follows playGacha's two disjoint rarity pools. Both the
// client odds dialog and the operator preview consume these distributions.
func PreviewGachaStages(profile release.GachaProfile, rarities map[int]int) ([]GachaOddsStage, error) {
	profile = profile.CurrentStep()
	if len(profile.RewardPool) > 0 {
		weights := make([]int, len(profile.RewardPool))
		for i, entry := range profile.RewardPool {
			weights[i] = entry.Weight
		}
		odds, err := scaledGachaOdds(weights)
		if err != nil {
			return nil, err
		}
		return []GachaOddsStage{{Name: profile.Name, DrawCount: profile.CardNum, Rewards: gachaPoolRewards(profile), Odds: odds}}, nil
	}
	if len(profile.CardIDs) != len(profile.CardWeights) {
		return nil, errors.New("gacha card and weight lengths differ")
	}
	rules := [][2]int{{0, profile.CardNum}}
	if profile.GuaranteedCount > 0 {
		rules = [][2]int{{profile.GuaranteedRarityRank, profile.GuaranteedCount}, {profile.RemainderRarityRank, profile.CardNum - profile.GuaranteedCount}}
	}
	stages := make([]GachaOddsStage, 0, len(rules))
	for _, rule := range rules {
		stage := GachaOddsStage{Name: profile.Name, Rarity: rule[0], DrawCount: rule[1]}
		weights := []int{}
		for i, id := range profile.CardIDs {
			if rule[0] == 0 || rarities[id] == rule[0] {
				stage.CardIDs = append(stage.CardIDs, id)
				weights = append(weights, profile.CardWeights[i])
			}
		}
		if rule[0] != 0 {
			stage.Name = fmt.Sprintf("%s · 稀有度 %d · %d 抽", profile.Name, rule[0], rule[1])
		}
		var err error
		stage.Odds, err = scaledGachaOdds(weights)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}
	return stages, nil
}

func (s *store) gachaOddsStages(profile release.GachaProfile) ([]GachaOddsStage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rarities := make(map[int]int, len(profile.CardIDs))
	for _, id := range profile.CardIDs {
		rarities[id] = s.cardDefinitions[id].RarityRank
	}
	return PreviewGachaStages(profile, rarities)
}
