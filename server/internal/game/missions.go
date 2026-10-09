package game

import (
	"fmt"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// IDs in this range belong to the local mission catalog and remain stable across releases.
const (
	dailyExploreMissionID = 910002
	// CN client MISSION_TAB_TYPE: PERMANENT=0, LIMITED=1, DAILY=2.
	permanentMissionTabType = 0
	dailyMissionTabType     = 2
)

var missionLocation = time.FixedZone("CST", 8*60*60)

type MissionDefinition struct {
	ID          int                `json:"id"`
	Target      int                `json:"target"`
	Crystals    int                `json:"crystals"`
	Title       string             `json:"title"`
	Kind        string             `json:"kind"`
	Daily       bool               `json:"daily"`
	Enabled     bool               `json:"enabled"`
	Description string             `json:"description,omitempty"`
	Rewards     []gamestate.Reward `json:"rewards,omitempty"`
}

func DefaultMissions() []MissionDefinition {
	return cloneMissionDefinitions(localMissions)
}

func cloneMissionDefinitions(definitions []MissionDefinition) []MissionDefinition {
	result := append([]MissionDefinition{}, definitions...)
	for i := range result {
		if result[i].Rewards != nil {
			result[i].Rewards = cloneRewards(result[i].Rewards)
		}
	}
	return result
}

func (s *Account) missionDefinitionsLocked() []MissionDefinition {
	if s.missionDefinitions == nil {
		return localMissions
	}
	return s.missionDefinitions
}

func (s *Account) missionEnabledLocked(id int) bool {
	for _, def := range s.missionDefinitionsLocked() {
		if def.ID == id {
			return def.Enabled
		}
	}
	return false // Removed definitions stay archived, but cannot be shown or claimed.
}

var localMissions = []MissionDefinition{
	{ID: 910001, Target: 1, Crystals: 1, Title: "每日登录", Kind: "login", Daily: true, Enabled: true},
	{ID: dailyExploreMissionID, Target: 1, Crystals: 2, Title: "每日探索", Kind: "explore", Daily: true, Enabled: true},
	{ID: 920001, Target: 10, Crystals: 5, Title: "成长之路：达到10级", Kind: "level", Enabled: true},
	{ID: 920002, Target: 30, Crystals: 10, Title: "成长之路：达到30级", Kind: "level", Enabled: true},
	{ID: 920003, Target: 50, Crystals: 15, Title: "成长之路：达到50级", Kind: "level", Enabled: true},
	{ID: 920101, Target: 20, Crystals: 5, Title: "骑士收藏：收集20种卡牌", Kind: "collection", Enabled: true},
	{ID: 920102, Target: 50, Crystals: 10, Title: "骑士收藏：收集50种卡牌", Kind: "collection", Enabled: true},
	{ID: 920201, Target: 7, Crystals: 5, Title: "日积月累：累计签到7天", Kind: "login", Enabled: true},
	{ID: 920202, Target: 30, Crystals: 15, Title: "日积月累：累计签到30天", Kind: "login", Enabled: true},
}

func missionTabType(def MissionDefinition) int {
	if def.Daily {
		return dailyMissionTabType
	}
	return permanentMissionTabType
}

func (s *Account) newMissionLocked(def MissionDefinition, period string, now time.Time) gamestate.Mission {
	empty := gamestate.Reward{CardSkillLevels: []int16{}}
	present := gamestate.Present{
		PresentID: s.nextPresentIDLocked(), Title: "任务奖励", Comment: def.Title,
		Reward: empty, Reward0: empty, Reward1: empty, Reward2: empty,
	}
	info := gamestate.MissionInfo{
		MissionID: def.ID, TabType: missionTabType(def), ViewPriority: def.ID,
		Title: def.Title, Description: missionDescription(def),
		ProgressShow: 1, ProgressMax: def.Target,
	}
	if def.Daily {
		local := now.In(missionLocation)
		info.ClearLimitTime = int(time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, missionLocation).Sub(now).Seconds())
	}
	mission := gamestate.Mission{Info: info, RewardPresent: present, Period: period}
	s.configureMissionRewardsLocked(&mission, def)
	return mission
}

func missionDescription(def MissionDefinition) string {
	if text := strings.TrimSpace(def.Description); text != "" {
		return text
	}
	return fmt.Sprintf("%s，目标%d，完成后领取奖励（发送至礼物箱）。", def.Title, def.Target)
}

func (s *Account) configureMissionRewardsLocked(mission *gamestate.Mission, def MissionDefinition) {
	rewards := def.Rewards
	if rewards == nil {
		rewards = []gamestate.Reward{{Type: 10, Num: def.Crystals, CardSkillLevels: []int16{}}}
	}
	mission.Info.Rewards = cloneRewards(rewards)
	if len(rewards) == 0 {
		return
	}
	mission.RewardPresent.Reward = cloneReward(rewards[0])
	mission.RewardPresent.Comment = def.Title
	additional := make([]gamestate.Present, max(0, len(rewards)-1))
	reserved := []int64{mission.RewardPresent.PresentID}
	for i := range additional {
		if i < len(mission.RewardPresents) {
			additional[i] = clonePresent(mission.RewardPresents[i])
		} else {
			additional[i] = clonePresent(mission.RewardPresent)
			additional[i].PresentID = s.nextPresentIDLocked(reserved...)
		}
		reserved = append(reserved, additional[i].PresentID)
		additional[i].Reward = cloneReward(rewards[i+1])
		additional[i].Comment = def.Title
	}
	mission.RewardPresents = additional
}

func (s *Account) refreshMissionsLocked(now time.Time) {
	day := now.In(missionLocation).Format("2006-01-02")
	for _, def := range s.missionDefinitionsLocked() {
		if !def.Enabled {
			continue
		}
		period := ""
		if def.Daily {
			period = day
		}
		index := -1
		for i := range s.missions {
			if s.missions[i].Info.MissionID == def.ID {
				index = i
				break
			}
		}
		if index < 0 {
			s.missions = append(s.missions, s.newMissionLocked(def, period, now))
			index = len(s.missions) - 1
		} else if def.Daily && s.missions[index].Period != period {
			s.missions[index] = s.newMissionLocked(def, period, now)
		}
		info := &s.missions[index].Info
		// Repair persisted tabs too, including completed/claimed missions, without
		// replacing their progress, receipt state or already-issued reward.
		info.TabType = missionTabType(def)
		if info.State != 2 {
			info.Title = def.Title
			info.Description = missionDescription(def)
			info.ProgressMax = def.Target
			s.configureMissionRewardsLocked(&s.missions[index], def)
		}
		if def.Daily {
			local := now.In(missionLocation)
			info.ClearLimitTime = int(time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, missionLocation).Sub(now).Seconds())
		}
		if info.State == 2 {
			continue
		}
		progress := info.ProgressNow
		switch def.Kind {
		case "level":
			progress = s.currentLevel
		case "collection":
			progress = len(s.cardCollectionIDs)
		case "login":
			progress = s.loginBonusState.TotalClaims
			if def.Daily {
				// Login bonuses may use a separately configured day boundary.
				// Viewing the authenticated mission list is itself today's login.
				progress = 1
			}
		}
		info.ProgressNow = min(def.Target, max(info.ProgressNow, progress))
		if info.ProgressNow >= def.Target {
			info.State = 1
		} else {
			info.State = 0
		}
	}
}

func (s *Account) advanceDailyExploreMissionLocked(now time.Time) {
	s.refreshMissionsLocked(now)
	for _, def := range s.missionDefinitionsLocked() {
		if !def.Enabled || def.Kind != "explore" {
			continue
		}
		for i := range s.missions {
			info := &s.missions[i].Info
			if info.MissionID == def.ID && info.State == 0 {
				info.ProgressNow = min(info.ProgressMax, info.ProgressNow+1)
				if info.ProgressNow == info.ProgressMax {
					info.State = 1
				}
				break
			}
		}
	}
}
