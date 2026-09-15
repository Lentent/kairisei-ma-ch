package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"kairisei.local/server/internal/release"
)

type cardAcquisitionGroupsKey struct{}

// WithCardAcquisitionGroups uses the same current publication as the battle
// list adapter. nil means all activities; an empty map means none. Normal
// quests retain their account-scoped unlocks independently of operations.
func WithCardAcquisitionGroups(request *http.Request, groups map[int]struct{}) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), cardAcquisitionGroupsKey{}, groups))
}

func (s *store) battleCardSourcesLocked(profiles []release.TeamBattleRewardProfile, allowedGroups map[int]struct{}) (map[int][]howToGetCardEntry, error) {
	result := map[int][]howToGetCardEntry{}
	if len(profiles) == 0 || len(s.teamBattleSolo) == 0 {
		return result, nil
	}
	tutorialNormal := s.onboarding.ConfigVersion == cnOnboardingConfigVersion && s.onboarding.Step == 0
	tutorialActivity := s.onboarding.ConfigVersion == cnOnboardingConfigVersion && s.onboarding.Step == cnOnboardingStepCount-1
	projected, err := projectCNTeamBattlePublication(s.teamBattleSolo, s.stageQuests, s.teamBattleLimitedGroupIDs, tutorialNormal, tutorialActivity)
	if err != nil {
		return nil, err
	}
	var lists map[string]json.RawMessage
	if err := json.Unmarshal(projected, &lists); err != nil {
		return nil, err
	}
	type destination struct {
		entry howToGetCardEntry
		clear bool
		area  int
	}
	destinations := map[int][]destination{}
	for _, key := range []string{"9", "10", "11", "12"} {
		var groups []struct {
			ID     int    `json:"0"`
			Name   string `json:"4"`
			AreaID int    `json:"9"`
			Bosses []struct {
				ID         int    `json:"0"`
				Difficulty string `json:"4"`
				State      int    `json:"10"`
				Locked     int    `json:"26"`
			} `json:"10"`
		}
		if err := json.Unmarshal(lists[key], &groups); err != nil {
			return nil, err
		}
		for _, group := range groups {
			if key != "9" && allowedGroups != nil && !release.IsBurstQuestGroup(group.ID) {
				if _, allowed := allowedGroups[group.ID]; !allowed {
					continue
				}
			}
			for _, boss := range group.Bosses {
				if boss.Locked != 0 {
					continue
				}
				entry := howToGetCardEntry{Type: 10, ContentID: boss.ID, Text: strings.TrimSpace(group.Name + " " + boss.Difficulty)}
				if group.AreaID > 0 {
					// CONFIRMED: subtype 2 calls StageMgr.SetReturnParam(areaid).
					// Other battle entries pass bossid to TeamSlSt.InitializePage.
					entry.Subtype, entry.ContentID = 2, group.AreaID
				}
				destinations[boss.ID] = append(destinations[boss.ID], destination{entry, boss.State >= 2, group.AreaID})
			}
		}
	}
	seen := map[int]map[howToGetCardEntry]bool{}
	add := func(reward release.Reward, entry howToGetCardEntry) {
		if (reward.Type != 6 && reward.Type != 13) || reward.Num <= 0 {
			return
		}
		if seen[reward.RewardTypeID] == nil {
			seen[reward.RewardTypeID] = map[howToGetCardEntry]bool{}
		}
		if !seen[reward.RewardTypeID][entry] {
			seen[reward.RewardTypeID][entry] = true
			result[reward.RewardTypeID] = append(result[reward.RewardTypeID], entry)
		}
	}
	for _, profile := range profiles {
		for _, target := range destinations[profile.BossID] {
			if profile.TowerID != 0 || profile.StageQuestAreaID != target.area {
				continue
			}
			// Follow the settlement contract: explicit part drops replace the
			// aggregate tangible rewards, including an explicitly empty list.
			if profile.EnemyDrops == nil {
				for _, reward := range profile.ResultRewards {
					add(reward, target.entry)
				}
			} else {
				for _, drop := range profile.EnemyDrops {
					if drop.ChancePerMillion == nil || *drop.ChancePerMillion > 0 {
						add(drop.Reward, target.entry)
					}
				}
			}
			if s.teamBattleFameBonus.ConfigVersion != 0 {
				entry := target.entry
				entry.Text += "（名声奖励）"
				for _, reward := range teamBattleFamePool(profile, s.teamBattleFameBonus) {
					add(reward, entry)
				}
			}
			if !target.clear {
				entry := target.entry
				entry.Text += "（首次通关）"
				for _, reward := range profile.FirstClearRewards {
					add(reward, entry)
				}
			}
		}
	}
	return result, nil
}
