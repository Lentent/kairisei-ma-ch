package httpapi

import (
	"encoding/json"
	"kairisei.local/server/internal/release"
	"testing"
)

func TestScoreSettlementKeepsBestAndDoesNotRepeatAwards(t *testing.T) {
	p := &release.TeamBattleScorePolicy{SourceState: "PLACEHOLDER_LOCAL_CLEAR_TURN_SCORE", BaseScore: 1000000, BestTurn: 4}
	for i := 0; i < 9; i++ {
		p.Grades = append(p.Grades, release.TeamBattleScoreGrade{Score: 1000000 * int64(i+1), Crystal: 50})
	}
	s := onboardingTestStore()
	s.teamBattleScores = map[int]release.TeamBattleScoreProgress{}
	s.teamBattleSolo = json.RawMessage(`{"9":[{"0":1,"9":0,"10":[{"0":123,"10":0}]}],"10":[],"11":[],"12":[]}`)
	profiles := []release.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: p}}
	play := func(turns int) teamBattleSettlement {
		t.Helper()
		s.activeBattle = &teamBattleContext{BossID: 123}
		r, err := s.completeTeamBattle(123, true, profiles, teamBattleDropReport{Turns: turns})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := play(12)
	if s.coinFree != 50 || first.ScoreInfo[0].(map[string]any)["high_score"] != int64(0) {
		t.Fatal("first award or previous-record display is wrong")
	}
	if _, err := s.completeTeamBattle(123, true, profiles); err == nil {
		t.Fatal("duplicate result allowed without a start")
	}
	// Persist/reload the same field carried by the account save, then improve the score.
	raw, err := json.Marshal(release.State{TeamBattleScores: cloneTeamBattleScores(s.teamBattleScores)})
	if err != nil {
		t.Fatal(err)
	}
	var restored release.State
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
	high, rows := s.scoreLineup(123, profiles)
	if high != 9000000 || len(rows) != 9 {
		t.Fatal("lineup lost saved best")
	}
}
