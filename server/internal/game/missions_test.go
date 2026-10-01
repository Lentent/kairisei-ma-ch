package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func missionByID(t *testing.T, s *Account, id int) gamestate.Mission {
	t.Helper()
	for _, mission := range s.missions {
		if mission.Info.MissionID == id {
			return mission
		}
	}
	t.Fatalf("missing mission %d", id)
	return gamestate.Mission{}
}

func TestMissionClaimIsAtomicAndCannotRepeat(t *testing.T) {
	s := &Account{currentLevel: 10}
	s.MissionInfos()
	if _, _, err := s.ReceiveMissionRewards([]int{910001, 920002}); err == nil {
		t.Fatal("claimed unfinished mission")
	}
	if len(s.presents) != 0 || missionByID(t, s, 910001).Info.State != 1 {
		t.Fatal("partial claim committed")
	}
	if _, _, err := s.ReceiveMissionRewards([]int{910001, 910001}); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if _, _, err := s.ReceiveMissionRewards([]int{910001, 920001}); err != nil {
		t.Fatal(err)
	}
	if len(s.presents) != 2 || s.coinFree != 0 {
		t.Fatal("rewards must go to present box")
	}
	if _, _, err := s.ReceiveMissionRewards([]int{910001}); err == nil {
		t.Fatal("repeat claim accepted")
	}
	if s.presents[0].PresentID == s.presents[1].PresentID {
		t.Fatal("present ID collision")
	}
}

func TestDailyMissionsResetAtBeijingMidnight(t *testing.T) {
	s := &Account{}
	before := time.Date(2026, 10, 1, 15, 59, 59, 0, time.UTC)
	s.advanceDailyExploreMissionLocked(before)
	old := missionByID(t, s, dailyExploreMissionID)
	if old.Info.State != 1 || old.Period != "2026-10-01" || old.Info.ClearLimitTime != 1 {
		t.Fatalf("before midnight: %+v", old)
	}
	// Simulate an already-issued reward: reset must preserve it.
	s.presents = append(s.presents, old.RewardPresent)
	s.refreshMissionsLocked(before.Add(time.Second))
	fresh := missionByID(t, s, dailyExploreMissionID)
	if fresh.Info.State != 0 || fresh.Info.ProgressNow != 0 || fresh.Period != "2026-10-02" {
		t.Fatalf("after midnight: %+v", fresh)
	}
	if fresh.RewardPresent.PresentID == old.RewardPresent.PresentID || len(s.presents) != 1 {
		t.Fatal("reset damaged issued reward")
	}
	count := len(s.missions)
	s.refreshMissionsLocked(before.Add(2 * time.Second))
	if len(s.missions) != count {
		t.Fatal("refresh duplicates missions")
	}
}

func TestMissionProgressAndClaimsSurviveSerialization(t *testing.T) {
	s := &Account{currentLevel: 50, loginBonusState: gamestate.LoginBonusState{TotalClaims: 30}, cardCollectionIDs: map[int]struct{}{}}
	for i := 1; i <= 50; i++ {
		s.cardCollectionIDs[i] = struct{}{}
	}
	s.advanceDailyExploreMissionLocked(time.Now())
	for _, mission := range s.missions {
		if mission.Info.State != 1 {
			t.Fatalf("progress not derived: %+v", mission.Info)
		}
	}
	if _, _, err := s.ReceiveMissionRewards([]int{920001, dailyExploreMissionID}); err != nil {
		t.Fatal(err)
	}
	state := gamestate.EngagementState{Missions: cloneMissions(s.missions), Presents: clonePresents(s.presents)}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var restored gamestate.EngagementState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	reloaded := &Account{missions: cloneMissions(restored.Missions), presents: clonePresents(restored.Presents)}
	reloaded.MissionInfos()
	if _, _, err := reloaded.ReceiveMissionRewards([]int{920001}); err == nil {
		t.Fatal("reload allowed second claim")
	}
	if missionByID(t, reloaded, dailyExploreMissionID).Info.State != 2 {
		t.Fatal("daily claim lost on reload")
	}
	if missionByID(t, reloaded, 920003).Info.ProgressNow != 50 {
		t.Fatal("completed progress regressed")
	}
}

func TestExploreWithoutActiveSessionDoesNotAdvanceMission(t *testing.T) {
	s := &Account{}
	if _, _, completed, _, err := s.EndExplore(nil); err != nil || completed {
		t.Fatal("invalid exploration accepted")
	}
	s.MissionInfos()
	if missionByID(t, s, dailyExploreMissionID).Info.ProgressNow != 0 {
		t.Fatal("invalid exploration advanced mission")
	}
}

func TestSuccessfulExplorationAdvancesMissionOnlyOnce(t *testing.T) {
	s := &Account{exploreActive: true, exploreStartedAt: time.Now(),
		decks: []DeckInfo{{ArthurType: 1, CardUniqueIDs: []int64{}}}}
	if _, _, completed, _, err := s.EndExplore(nil); err != nil || !completed {
		t.Fatalf("exploration failed: %v", err)
	}
	if missionByID(t, s, dailyExploreMissionID).Info.State != 1 {
		t.Fatal("successful exploration not counted")
	}
	if _, _, err := s.ReceiveMissionRewards([]int{dailyExploreMissionID}); err != nil {
		t.Fatal(err)
	}
	if _, _, completed, _, err := s.EndExplore(nil); err != nil || completed {
		t.Fatal("replay accepted")
	}
	if missionByID(t, s, dailyExploreMissionID).Info.State != 2 || len(s.presents) != 1 {
		t.Fatal("replay reset reward receipt")
	}
}

func TestMissionConfigurationAppliesWithoutResettingClaims(t *testing.T) {
	s := &Account{currentLevel: 10}
	s.MissionInfos()
	if _, _, err := s.ReceiveMissionRewards([]int{920001}); err != nil {
		t.Fatal(err)
	}
	defs := DefaultMissions()
	defs[0].Enabled = false
	defs[1].Target, defs[1].Crystals = 3, 8
	s.ApplyPlayerConfiguration(PlayerConfiguration{Revision: 1, Missions: defs})
	for _, m := range s.MissionInfos() {
		if m.MissionID == 910001 {
			t.Fatal("disabled task visible")
		}
	}
	if _, _, err := s.ReceiveMissionRewards([]int{910001}); err == nil {
		t.Fatal("disabled task claimable")
	}
	s.advanceDailyExploreMissionLocked(time.Now())
	if m := missionByID(t, s, dailyExploreMissionID); m.Info.State != 0 || m.Info.ProgressMax != 3 {
		t.Fatal("target not applied")
	}
	s.advanceDailyExploreMissionLocked(time.Now())
	s.advanceDailyExploreMissionLocked(time.Now())
	if _, _, err := s.ReceiveMissionRewards([]int{dailyExploreMissionID}); err != nil {
		t.Fatal(err)
	}
	if s.presents[len(s.presents)-1].Reward.Num != 8 {
		t.Fatal("reward not applied")
	}
	defs[0].Enabled = true
	defs[2].Crystals = 99
	s.ApplyPlayerConfiguration(PlayerConfiguration{Revision: 2, Missions: defs})
	if _, _, err := s.ReceiveMissionRewards([]int{920001}); err == nil {
		t.Fatal("configuration reset claim")
	}
	if s.presents[0].Reward.Num != 5 {
		t.Fatal("configuration modified issued reward")
	}
}
