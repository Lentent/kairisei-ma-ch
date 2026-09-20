package gamestate

import "errors"

// This is a local clear/turn score, not a reconstruction of official damage scoring.
type TeamBattleScorePolicy struct {
	SourceState string                 `json:"source_state"`
	BaseScore   int64                  `json:"base_score"`
	BestTurn    int                    `json:"best_turn"`
	Grades      []TeamBattleScoreGrade `json:"grades"`
}

type TeamBattleScoreGrade struct {
	Score   int64 `json:"score"`
	Crystal int   `json:"crystal"`
}

type TeamBattleScoreProgress struct {
	HighScore int64  `json:"high_score"`
	Claimed   uint16 `json:"claimed"`
}

func ValidateTeamBattleScorePolicy(p *TeamBattleScorePolicy) error {
	if p == nil {
		return nil
	}
	if p.SourceState != "PLACEHOLDER_LOCAL_CLEAR_TURN_SCORE" || p.BaseScore < 1 || p.BaseScore > 1000000000 || p.BestTurn < 1 || p.BestTurn > 100 || len(p.Grades) != 9 {
		return errors.New("invalid local battle score policy")
	}
	for i, g := range p.Grades {
		if g.Score != p.BaseScore*int64(i+1) || g.Crystal < 1 || g.Crystal > 10000 {
			return errors.New("invalid local score grade")
		}
	}
	return nil
}
