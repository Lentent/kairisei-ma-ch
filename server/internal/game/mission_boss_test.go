package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestBossMissionCountsAcceptedVictoriesAndSurvivesReload(t *testing.T) {
	const boss, other = 30010103, 30010102
	def := configuredMission(1000000, "boss_clear", false, 2)
	def.BossID = boss
	s := &Account{teamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"10":0},{"0":30010103,"10":2}]}],"11":[],"12":[]}`)}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	if got := s.MissionInfos()[0].ProgressNow; got != 0 {
		t.Fatal("historical clear credited a newly created task", got)
	}
	play := func(id int, win bool, rewards []gamestate.Reward) error {
		t.Helper()
		s.activeBattle = &TeamBattleContext{BossID: id}
		_, err := s.CompleteTeamBattle(id, win, []gamestate.TeamBattleRewardProfile{{BossID: id, ResultRewards: rewards}})
		return err
	}
	if err := play(other, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := play(boss, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := play(boss, true, []gamestate.Reward{{Type: 999, Num: 1}}); err == nil {
		t.Fatal("invalid reward settlement accepted")
	}
	if got := s.MissionInfos()[0].ProgressNow; got != 0 {
		t.Fatal("wrong difficulty, defeat or rejected settlement counted", got)
	}
	if err := play(boss, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteTeamBattle(boss, true, []gamestate.TeamBattleRewardProfile{{BossID: boss}}); err == nil {
		t.Fatal("duplicate settlement accepted")
	}
	if got := s.MissionInfos()[0]; got.ProgressNow != 1 || got.State != 0 {
		t.Fatalf("first victory/retry: %+v", got)
	}
	// The same engagement field is serialized in account saves.
	raw, err := json.Marshal(gamestate.EngagementState{Missions: s.missions})
	if err != nil {
		t.Fatal(err)
	}
	var restored gamestate.EngagementState
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	s = &Account{missions: restored.Missions, teamBattleSolo: s.teamBattleSolo}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: []MissionDefinition{def}})
	if err := play(boss, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := s.MissionInfos()[0]; got.ProgressNow != 2 || got.State != 1 {
		t.Fatalf("second victory after reload: %+v", got)
	}
	if _, _, err := s.ReceiveMissionRewards([]int{def.ID}); err != nil {
		t.Fatal(err)
	}
	if err := play(boss, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReceiveMissionRewards([]int{def.ID}); err == nil || len(s.presents) != 1 {
		t.Fatal("further victories duplicated mission mail")
	}
}

func TestBossMissionsDailyBoundaryAndDisabledProgress(t *testing.T) {
	defs := []MissionDefinition{configuredMission(1000000, "boss_clear", true, 2), configuredMission(1000001, "boss_clear", false, 5), configuredMission(1000002, "boss_clear", false, 5)}
	for i := range defs {
		defs[i].BossID = 123
	}
	defs[2].Enabled = false
	s := &Account{}
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 1, Missions: defs})
	before := time.Date(2026, 10, 24, 15, 59, 59, 0, time.UTC)
	s.advanceBossClearMissionsLocked(123, before)
	s.advanceBossClearMissionsLocked(123, before)
	if got := missionByID(t, s, defs[0].ID).Info; got.ProgressNow != 2 || got.State != 1 {
		t.Fatalf("daily mission did not complete: %+v", got)
	}
	s.advanceBossClearMissionsLocked(123, before.Add(time.Second))
	if got := missionByID(t, s, defs[0].ID).Info; got.ProgressNow != 1 || got.State != 0 {
		t.Fatalf("daily mission did not reset at Beijing midnight: %+v", got)
	}
	if got := missionByID(t, s, defs[1].ID).Info.ProgressNow; got != 3 || len(s.missions) != 2 {
		t.Fatal("permanent/disabled task progress is wrong", got, len(s.missions))
	}
	defs[0].Enabled, defs[2].Enabled = false, true
	s.ApplyMissionConfiguration(MissionConfiguration{Revision: 2, Missions: defs})
	s.advanceBossClearMissionsLocked(123, before.Add(2*time.Second))
	if missionByID(t, s, defs[0].ID).Info.ProgressNow != 1 || missionByID(t, s, defs[2].ID).Info.ProgressNow != 1 {
		t.Fatal("disabled period credited progress or erased existing progress")
	}
}
