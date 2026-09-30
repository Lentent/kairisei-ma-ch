package game

import "kairisei.local/server/internal/gamestate"

func cloneTeamBattleScores(source map[int]gamestate.TeamBattleScoreProgress) map[int]gamestate.TeamBattleScoreProgress {
	result := make(map[int]gamestate.TeamBattleScoreProgress, len(source))
	for id, progress := range source {
		result[id] = progress
	}
	return result
}

func planTeamBattleScore(p *gamestate.TeamBattleScorePolicy, progress gamestate.TeamBattleScoreProgress, report TeamBattleDropReport) (gamestate.TeamBattleScoreProgress, []gamestate.Reward, []any) {
	if p == nil {
		return progress, nil, nil
	}
	turns := report.Turns
	// Missing legacy room turn metadata earns the lowest grade, not the maximum.
	if turns <= 0 {
		turns = p.BestTurn + 8
	}
	rate := max(100, min(900, 900-(turns-p.BestTurn)*100))
	damage := p.BaseScore
	if p.SourceState == "LOCAL_POLICY_DAMAGE_SCORE" {
		if report.SoloChallenge {
			// Native result UI calculates damage*rate/100. The highest threshold
			// is a local clear score, not a claim about the player's damage.
			damage, rate = p.Grades[len(p.Grades)-1].Score, 100
		} else if report.ScoreVerified {
			damage, rate = report.ScoreDamage, p.Rate
		} else {
			return progress, nil, nil
		}
	}
	score := damage * int64(rate) / 100
	previousHighScore := progress.HighScore
	progress.HighScore = max(progress.HighScore, score)
	rewards := []gamestate.Reward{}
	grade := 0
	for i, g := range p.Grades {
		if score < g.Score {
			break
		}
		grade = i
		mask := uint16(1) << i
		if progress.Claimed&mask == 0 {
			rewards = append(rewards, g.RewardList()...)
			progress.Claimed |= mask
		}
	}
	return progress, rewards, []any{map[string]any{"damage": damage, "rate": rate, "grade": grade, "high_score": previousHighScore}}
}

func (s *Account) ScoreLineup(bossID int, profiles []gamestate.TeamBattleRewardProfile) (int64, []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := s.teamBattleScores[bossID]
	var policy *gamestate.TeamBattleScorePolicy
	for _, profile := range profiles {
		if profile.BossID == bossID && profile.StageQuestAreaID == 0 && profile.TowerID == 0 {
			policy = profile.ScorePolicy
			break
		}
	}
	rows := make([]any, 9)
	for i := range rows {
		score := int64(0)
		rewards := []any{}
		if policy != nil {
			g := policy.Grades[i]
			score = g.Score
			for _, reward := range g.RewardList() {
				rewards = append(rewards, map[string]any{"is_fixed": 0, "is_new": BoolInt(progress.Claimed&(uint16(1)<<i) == 0), "reward": reward})
			}
		}
		rows[i] = map[string]any{"grade": i, "score": score, "reward": rewards}
	}
	return progress.HighScore, rows
}
