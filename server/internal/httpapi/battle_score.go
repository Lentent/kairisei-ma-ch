package httpapi

import (
	"kairisei.local/server/internal/release"
	"strings"
)

func cloneTeamBattleScores(source map[int]release.TeamBattleScoreProgress) map[int]release.TeamBattleScoreProgress {
	result := make(map[int]release.TeamBattleScoreProgress, len(source))
	for id, progress := range source {
		result[id] = progress
	}
	return result
}

// Called only after the native report envelope was validated. This local policy
// credits a fixed clear score and a turn multiplier; it does not claim to replay damage.
func nativeScoreTurns(commands []string) int {
	turns := 0
	for _, wave := range commands {
		for _, line := range strings.Split(wave, "\n") {
			fields := strings.Split(strings.TrimSpace(line), ",")
			if len(fields) == 21 && strings.TrimSpace(fields[1]) == "10" {
				turns++
			}
		}
	}
	return max(1, turns)
}

func planTeamBattleScore(p *release.TeamBattleScorePolicy, progress release.TeamBattleScoreProgress, turns int) (release.TeamBattleScoreProgress, []release.Reward, []any) {
	if p == nil {
		return progress, nil, nil
	}
	// Missing legacy room turn metadata earns the lowest grade, not the maximum.
	if turns <= 0 {
		turns = p.BestTurn + 8
	}
	rate := max(100, min(900, 900-(turns-p.BestTurn)*100))
	score := p.BaseScore * int64(rate) / 100
	previousHighScore := progress.HighScore
	progress.HighScore = max(progress.HighScore, score)
	rewards := []release.Reward{}
	grade := 0
	for i, g := range p.Grades {
		if score < g.Score {
			break
		}
		grade = i
		mask := uint16(1) << i
		if progress.Claimed&mask == 0 {
			rewards = append(rewards, release.Reward{Type: 10, Num: g.Crystal, CardSkillLevels: []int16{}})
			progress.Claimed |= mask
		}
	}
	return progress, rewards, []any{map[string]any{"damage": p.BaseScore, "rate": rate, "grade": grade, "high_score": previousHighScore}}
}

func scoreInfoWire(info []any) []any {
	if info == nil {
		return []any{}
	}
	return info
}

func (s *store) scoreLineup(bossID int, profiles []release.TeamBattleRewardProfile) (int64, []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := s.teamBattleScores[bossID]
	var policy *release.TeamBattleScorePolicy
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
			rewards = append(rewards, map[string]any{"is_fixed": 0, "is_new": boolInt(progress.Claimed&(uint16(1)<<i) == 0), "reward": release.Reward{Type: 10, Num: g.Crystal, CardSkillLevels: []int16{}}})
		}
		rows[i] = map[string]any{"grade": i, "score": score, "reward": rewards}
	}
	return progress.HighScore, rows
}
