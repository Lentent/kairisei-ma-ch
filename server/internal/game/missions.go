package game

import (
	"fmt"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// IDs in this range belong to the local mission catalog and remain stable across releases.
const dailyExploreMissionID = 910002

var missionLocation = time.FixedZone("CST", 8*60*60)

type MissionDefinition struct {
	ID       int    `json:"id"`
	Target   int    `json:"target"`
	Crystals int    `json:"crystals"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Daily    bool   `json:"daily"`
	Enabled  bool   `json:"enabled"`
}

func DefaultMissions() []MissionDefinition {
	return append([]MissionDefinition{}, localMissions...)
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
	return true // Preserve missions supplied by older save configurations.
}

var localMissions = []MissionDefinition{
	{910001, 1, 1, "每日登录", "login", true, true},
	{dailyExploreMissionID, 1, 2, "每日探索", "explore", true, true},
	{920001, 10, 5, "成长之路：达到10级", "level", false, true},
	{920002, 30, 10, "成长之路：达到30级", "level", false, true},
	{920003, 50, 15, "成长之路：达到50级", "level", false, true},
	{920101, 20, 5, "骑士收藏：收集20种卡牌", "collection", false, true},
	{920102, 50, 10, "骑士收藏：收集50种卡牌", "collection", false, true},
	{920201, 7, 5, "日积月累：累计签到7天", "login", false, true},
	{920202, 30, 15, "日积月累：累计签到30天", "login", false, true},
}

func (s *Account) newMissionLocked(def MissionDefinition, period string, now time.Time) gamestate.Mission {
	reward := gamestate.Reward{Type: 10, Num: def.Crystals, CardSkillLevels: []int16{}}
	empty := gamestate.Reward{CardSkillLevels: []int16{}}
	present := gamestate.Present{
		PresentID: s.nextPresentIDLocked(), Title: "任务奖励", Comment: def.Title,
		Reward: reward, Reward0: empty, Reward1: empty, Reward2: empty,
	}
	info := gamestate.MissionInfo{
		MissionID: def.ID, TabType: 1, ViewPriority: def.ID,
		Title: def.Title, Description: fmt.Sprintf("%s，完成后领取%d水晶（发送至礼物箱）。", def.Title, def.Crystals),
		Rewards: []gamestate.Reward{reward}, ProgressShow: 1, ProgressMax: def.Target,
	}
	if def.Daily {
		info.TabType = 0
		local := now.In(missionLocation)
		info.ClearLimitTime = int(time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, missionLocation).Sub(now).Seconds())
	}
	return gamestate.Mission{Info: info, RewardPresent: present, Period: period}
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
		if info.State != 2 {
			info.Title = def.Title
			info.Description = fmt.Sprintf("%s，目标%d，完成后领取%d水晶（发送至礼物箱）。", def.Title, def.Target, def.Crystals)
			info.ProgressMax = def.Target
			info.Rewards = []gamestate.Reward{{Type: 10, Num: def.Crystals, CardSkillLevels: []int16{}}}
			s.missions[index].RewardPresent.Reward = cloneReward(info.Rewards[0])
			s.missions[index].RewardPresent.Comment = def.Title
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
	for i := range s.missions {
		info := &s.missions[i].Info
		if info.MissionID == dailyExploreMissionID && info.State == 0 && s.missionEnabledLocked(info.MissionID) {
			info.ProgressNow = min(info.ProgressMax, info.ProgressNow+1)
			if info.ProgressNow == info.ProgressMax {
				info.State = 1
			}
		}
	}
}
