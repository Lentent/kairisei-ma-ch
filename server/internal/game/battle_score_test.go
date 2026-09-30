package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestScoreSettlementKeepsBestAndDoesNotRepeatAwards(t *testing.T) {
	p := &gamestate.TeamBattleScorePolicy{SourceState: "PLACEHOLDER_LOCAL_CLEAR_TURN_SCORE", BaseScore: 1000000, BestTurn: 4}
	for i := 0; i < 9; i++ {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: 1000000 * int64(i+1), Crystal: 50})
	}
	s := onboardingTestStore()
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{}
	s.teamBattleSolo = json.RawMessage(`{"9":[{"0":1,"9":0,"10":[{"0":123,"10":0}]}],"10":[],"11":[],"12":[]}`)
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: p}}
	play := func(turns int) teamBattleSettlement {
		t.Helper()
		s.activeBattle = &TeamBattleContext{BossID: 123}
		r, err := s.CompleteTeamBattle(123, true, profiles, TeamBattleDropReport{Turns: turns})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := play(12)
	if s.coinFree != 50 || first.ScoreInfo[0].(map[string]any)["high_score"] != int64(0) {
		t.Fatal("first award or previous-record display is wrong")
	}
	if _, err := s.CompleteTeamBattle(123, true, profiles); err == nil {
		t.Fatal("duplicate result allowed without a start")
	}
	// Persist/reload the same field carried by the account save, then improve the score.
	raw, err := json.Marshal(gamestate.State{TeamBattleScores: cloneTeamBattleScores(s.teamBattleScores)})
	if err != nil {
		t.Fatal(err)
	}
	var restored gamestate.State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	s.teamBattleScores = cloneTeamBattleScores(restored.TeamBattleScores)
	second := play(4)
	if s.coinFree != 450 || len(second.Score.Rewards) != 8 || s.teamBattleScores[123].HighScore != 9000000 {
		t.Fatal("improved score did not grant only newly reached grades")
	}
	third := play(20)
	if s.coinFree != 450 || len(third.Score.Rewards) != 0 || s.teamBattleScores[123].HighScore != 9000000 {
		t.Fatal("lower score repeated rewards or erased best")
	}
	high, rows := s.ScoreLineup(123, profiles)
	if high != 9000000 || len(rows) != 9 {
		t.Fatal("lineup lost saved best")
	}
}

func TestDamageScoreTimeoutFreezesPolicyAndAwardsOnlyNewGrades(t *testing.T) {
	p := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", EndTurn: 5, Rate: 100}
	for i := range 9 {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	s := onboardingTestStore()
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{}
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: gamestate.CloneTeamBattleScorePolicy(p), ResultRewards: []gamestate.Reward{{Type: 4, Num: 100}}}}
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: gamestate.CloneTeamBattleScorePolicy(p)}
	// A policy change during the fight must not multiply the accepted score.
	profiles[0].ScorePolicy.Rate = 1000
	result, err := s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 250})
	if err != nil || s.coinFree != 20 || s.gold != 0 || len(result.ScoreInfo) != 1 || s.teamBattleScores[123].HighScore != 250 {
		t.Fatalf("timeout settlement: coin=%d gold=%d result=%+v err=%v", s.coinFree, s.gold, result, err)
	}
	if _, err := s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 900}); err == nil {
		t.Fatal("timeout retry awarded without a new entry")
	}
	encoded, err := json.Marshal(s.teamBattleScores)
	if err != nil {
		t.Fatal(err)
	}
	s.teamBattleScores = nil
	if err := json.Unmarshal(encoded, &s.teamBattleScores); err != nil {
		t.Fatal(err)
	}
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
	result, err = s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 450})
	if err != nil || s.coinFree != 40 || len(result.Score.Rewards) != 2 || s.teamBattleScores[123].Claimed != 15 {
		t.Fatal("reloaded progress repeated or lost grade rewards", result, err)
	}
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
	_, err = s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 150})
	if err != nil || s.coinFree != 40 || s.teamBattleScores[123].HighScore != 450 {
		t.Fatal("lower result erased progress", err)
	}
}

func TestSoloCupClearAwardsAllGradesAndFailureAwardsNothing(t *testing.T) {
	p := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", EndTurn: 5, Rate: 150}
	for i := range 9 {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	s := onboardingTestStore()
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{}
	s.teamBattleSolo = json.RawMessage(`{"9":[{"0":1,"9":0,"10":[{"0":123,"10":0}]}],"10":[],"11":[],"12":[]}`)
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: gamestate.CloneTeamBattleScorePolicy(p), ResultRewards: []gamestate.Reward{{Type: 4, Num: 100}}}}
	// Later changes to the top threshold and multiplier do not change this run.
	profiles[0].ScorePolicy.Grades[8].Score = 10000
	profiles[0].ScorePolicy.Rate = 1000
	for _, turns := range []int{1, 5, 8} {
		s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
		r, err := s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{SoloChallenge: true, Turns: turns})
		if err != nil || len(r.ScoreInfo) != 0 || s.coinFree != 0 || s.gold != 0 || s.teamBattleScores[123].Claimed != 0 {
			t.Fatalf("failed/retired/timeout solo earned rewards: %+v %v", r, err)
		}
	}
	for attempt := range 2 {
		s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
		r, err := s.CompleteTeamBattle(123, true, profiles, TeamBattleDropReport{SoloChallenge: true, Turns: 4})
		if err != nil || len(r.ScoreInfo) != 1 || s.coinFree != 90 || s.teamBattleScores[123].Claimed != 511 || s.teamBattleScores[123].HighScore != 900 {
			t.Fatalf("clear must earn only unclaimed grades: %+v %v", r, err)
		}
		info := r.ScoreInfo[0].(map[string]any)
		if info["damage"] != int64(900) || info["rate"] != 100 || info["grade"] != 8 || (attempt == 1 && len(r.Score.Rewards) != 0) {
			t.Fatal("clear score multiplied or rewards repeated", r)
		}
		if _, err := s.CompleteTeamBattle(123, true, profiles, TeamBattleDropReport{SoloChallenge: true}); err == nil {
			t.Fatal("duplicate result accepted without entry")
		}
	}
}
