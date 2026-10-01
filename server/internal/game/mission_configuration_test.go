package game

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func configuredMission(id int, kind string, daily bool, target int) MissionDefinition {
	return MissionDefinition{ID: id, Kind: kind, Daily: daily, Target: target, Enabled: true,
		Title: "自定义任务", Description: "后台配置的任务说明",
		Rewards: []gamestate.Reward{{Type: 10, Num: 7, CardSkillLevels: []int16{}}}}
}

func TestManagedMissionsReplaceLegacyCatalogAndEmptySurvivesReload(t *testing.T) {
	s := &Account{}
	s.MissionInfos()
	def := configuredMission(1000000, "login", true, 1)
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	infos := s.MissionInfos()
	if len(infos) != 1 || infos[0].MissionID != def.ID || infos[0].Description != def.Description {
		t.Fatalf("configured catalog not authoritative: %+v", infos)
	}
	if _, _, err := s.ReceiveMissionRewards([]int{910001}); err == nil {
		t.Fatal("removed legacy task remained claimable")
	}
	if _, _, err := s.ReceiveMissionRewards([]int{def.ID}); err != nil {
		t.Fatal(err)
	}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 2, Missions: []MissionDefinition{}})
	if infos := s.MissionInfos(); len(infos) != 0 {
		t.Fatalf("empty configuration restored defaults: %+v", infos)
	}
	data, err := json.Marshal(gamestate.EngagementState{Missions: s.missions, Presents: s.presents})
	if err != nil {
		t.Fatal(err)
	}
	var restored gamestate.EngagementState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	reloaded := &Account{missions: restored.Missions, presents: restored.Presents}
	reloaded.ApplyPlayerConfiguration(PlayerConfiguration{Revision: 10, Missions: DefaultMissions()})
	reloaded.ApplyMissionConfiguration(MissionConfiguration{Revision: 2, Missions: []MissionDefinition{}})
	reloaded.ApplyPlayerConfiguration(PlayerConfiguration{Revision: 11, Missions: DefaultMissions()})
	if infos := reloaded.MissionInfos(); len(infos) != 0 || len(reloaded.presents) != 1 {
		t.Fatalf("reload/legacy save changed empty catalog or issued mail: %+v", infos)
	}
	// Receipt records remain archived, even if an internal caller restores an ID.
	reloaded.ApplyMissionConfiguration(MissionConfiguration{Revision: 3, Missions: []MissionDefinition{def}})
	if _, _, err := reloaded.ReceiveMissionRewards([]int{def.ID}); err == nil {
		t.Fatal("configuration restored a claimed reward")
	}
}

func TestManagedExploreMissionsTrackEachConfiguredTarget(t *testing.T) {
	s := &Account{}
	daily := configuredMission(1000000, "explore", true, 2)
	growth := configuredMission(1000001, "explore", false, 3)
	disabled := configuredMission(1000002, "explore", false, 1)
	disabled.Enabled = false
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{daily, growth, disabled}})
	now := time.Date(2026, 10, 1, 15, 59, 59, 0, time.UTC)
	s.advanceDailyExploreMissionLocked(now)
	s.advanceDailyExploreMissionLocked(now)
	if m := missionByID(t, s, daily.ID); m.Info.State != 1 || m.Info.TabType != 2 {
		t.Fatalf("daily target/category: %+v", m.Info)
	}
	if m := missionByID(t, s, growth.ID); m.Info.State != 0 || m.Info.ProgressNow != 2 || m.Info.TabType != 0 {
		t.Fatalf("growth progress/category: %+v", m.Info)
	}
	if len(s.missions) != 2 {
		t.Fatal("disabled exploration task accrued progress")
	}
	s.advanceDailyExploreMissionLocked(now.Add(time.Second))
	if m := missionByID(t, s, daily.ID); m.Info.ProgressNow != 1 || m.Info.State != 0 {
		t.Fatalf("daily exploration did not reset: %+v", m.Info)
	}
	if m := missionByID(t, s, growth.ID); m.Info.ProgressNow != 3 || m.Info.State != 1 {
		t.Fatalf("growth exploration reset across midnight: %+v", m.Info)
	}
}

func TestManagedMissionMultipleRewardsAreAllDeliveredAndPersisted(t *testing.T) {
	def := configuredMission(1000000, "login", true, 1)
	def.Rewards = append(def.Rewards,
		gamestate.Reward{Type: 4, Num: 100, CardSkillLevels: []int16{}},
		gamestate.Reward{Type: 9, Num: 5, CardSkillLevels: []int16{}})
	s := &Account{}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	s.MissionInfos()
	data, err := json.Marshal(gamestate.EngagementState{Missions: s.missions})
	if err != nil {
		t.Fatal(err)
	}
	var restored gamestate.EngagementState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	reloaded := &Account{missions: restored.Missions}
	reloaded.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	if _, _, err := reloaded.ReceiveMissionRewards([]int{def.ID}); err != nil {
		t.Fatal(err)
	}
	presents, _ := reloaded.PresentState()
	if len(presents) != 3 || reloaded.coinFree != 0 || reloaded.gold != 0 || reloaded.friendPoint != 0 {
		t.Fatal("mission should issue a separate mail for each reward")
	}
	seen := map[int64]bool{}
	for i, present := range presents {
		if seen[present.PresentID] || !reflect.DeepEqual(present.Reward, def.Rewards[i]) {
			t.Fatalf("invalid reward mail: %+v", present)
		}
		seen[present.PresentID] = true
		if result, err := reloaded.ReceivePresent(present.PresentID); err != nil || len(result.FailedID) != 0 {
			t.Fatalf("mail receipt failed: %+v, %v", result, err)
		}
	}
	if reloaded.coinFree != 7 || reloaded.gold != 100 || reloaded.friendPoint != 5 {
		t.Fatalf("reward amounts: crystals=%d gold=%d friends=%d", reloaded.coinFree, reloaded.gold, reloaded.friendPoint)
	}
	if _, _, err := reloaded.ReceiveMissionRewards([]int{def.ID}); err == nil {
		t.Fatal("multiple rewards allowed duplicate mission claim")
	}
	if result, err := reloaded.ReceivePresent(presents[1].PresentID); err != nil || len(result.FailedID) != 1 || reloaded.gold != 100 {
		t.Fatal("mail replay duplicated a reward")
	}
}

func TestManagedMissionInvalidLaterRewardDoesNotPartiallyIssue(t *testing.T) {
	def := configuredMission(1000000, "login", true, 1)
	def.Rewards = append(def.Rewards, gamestate.Reward{Type: 999, Num: 1})
	s := &Account{}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	if _, _, err := s.ReceiveMissionRewards([]int{def.ID}); err == nil {
		t.Fatal("invalid additional reward accepted")
	}
	if len(s.presents) != 0 || missionByID(t, s, def.ID).Info.State != 1 {
		t.Fatal("invalid reward caused a partial mission receipt")
	}
}

func TestManagedMissionEditsPreserveClaimAndIssuedRewards(t *testing.T) {
	def := configuredMission(1000000, "login", false, 1)
	s := &Account{loginBonusState: gamestate.LoginBonusState{TotalClaims: 1}}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	if _, _, err := s.ReceiveMissionRewards([]int{def.ID}); err != nil {
		t.Fatal(err)
	}
	before := clonePresents(s.presents)
	def.Title, def.Target, def.Rewards[0].Num = "修改后的任务", 5, 99
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 2, Missions: []MissionDefinition{def}})
	s.MissionInfos()
	if !reflect.DeepEqual(s.presents, before) || missionByID(t, s, def.ID).Info.State != 2 {
		t.Fatal("editing modified issued mail or cleared receipt")
	}
}

func TestManagedMissionDailyResetKeepsAllIssuedMailIdentities(t *testing.T) {
	def := configuredMission(1000000, "explore", true, 1)
	def.Rewards = append(def.Rewards, gamestate.Reward{Type: 4, Num: 10}, gamestate.Reward{Type: 9, Num: 2})
	s := &Account{}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	now := time.Date(2026, 10, 1, 15, 59, 59, 0, time.UTC)
	s.advanceDailyExploreMissionLocked(now)
	old := missionByID(t, s, def.ID)
	s.missions[0].Info.State = 2
	s.presents = append([]gamestate.Present{old.RewardPresent}, old.RewardPresents...)
	before := clonePresents(s.presents)
	s.refreshMissionsLocked(now.Add(time.Second))
	fresh := missionByID(t, s, def.ID)
	if !reflect.DeepEqual(before, s.presents) || fresh.Info.State != 0 || fresh.Info.ProgressNow != 0 {
		t.Fatal("daily reset damaged issued mail or did not reset progress")
	}
	seen := map[int64]bool{}
	for _, present := range before {
		seen[present.PresentID] = true
	}
	for _, present := range append([]gamestate.Present{fresh.RewardPresent}, fresh.RewardPresents...) {
		if seen[present.PresentID] {
			t.Fatal("daily reset reused an issued or newly allocated mail identity")
		}
		seen[present.PresentID] = true
	}
}

func TestManagedMissionConfigurationDeepCopiesRewards(t *testing.T) {
	def := configuredMission(1000000, "login", true, 1)
	def.Rewards[0].CardSkillLevels = []int16{1}
	s := &Account{}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	def.Rewards[0].Num = 99
	def.Rewards[0].CardSkillLevels[0] = 9
	infos := s.MissionInfos()
	if infos[0].Rewards[0].Num != 7 || infos[0].Rewards[0].CardSkillLevels[0] != 1 {
		t.Fatal("caller mutated live mission configuration")
	}
	infos[0].Rewards[0].Num = 50
	infos[0].Rewards[0].CardSkillLevels[0] = 5
	if got := s.MissionInfos()[0].Rewards[0]; got.Num != 7 || got.CardSkillLevels[0] != 1 {
		t.Fatal("client view mutated live mission rewards")
	}
}
