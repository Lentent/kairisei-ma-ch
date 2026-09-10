package release

// ExperienceRequired returns the EXP needed to advance from level to level+1.
// A maximum-level account has no next threshold.
func (policy PlayerProgressionPolicy) ExperienceRequired(level int) int {
	if level < 1 || level >= policy.MaxLevel {
		return 0
	}
	return policy.Experience.Base + (level-1)*policy.Experience.PerLevel
}

// CumulativeExperience returns the total EXP at the start of level.
func (policy PlayerProgressionPolicy) CumulativeExperience(level int) int {
	if level <= 1 {
		return 0
	}
	if level > policy.MaxLevel {
		level = policy.MaxLevel
	}
	levels := level - 1
	return levels*policy.Experience.Base +
		levels*(levels-1)/2*policy.Experience.PerLevel
}

func (policy PlayerProgressionPolicy) BattlePointMaximum(level int) int {
	value := policy.BattlePoints.Base
	if level > 0 {
		value += level / policy.BattlePoints.LevelsPerPoint
	}
	if value > policy.BattlePoints.Maximum {
		return policy.BattlePoints.Maximum
	}
	return value
}

func (policy PlayerProgressionPolicy) FriendMaximum(level int) int {
	value := policy.Friends.Offset
	if level > 0 {
		value += level * policy.Friends.Numerator / policy.Friends.Denominator
	}
	if value < policy.Friends.Minimum {
		value = policy.Friends.Minimum
	}
	if value > policy.Friends.Maximum {
		value = policy.Friends.Maximum
	}
	return value
}

func (policy PlayerProgressionPolicy) JobsAtLevel(level int) []JobParameter {
	result := make([]JobParameter, len(policy.JobParameters.Maximum))
	if level < 1 {
		level = 1
	}
	if level > policy.JobParameters.MaxStatusLevel {
		level = policy.JobParameters.MaxStatusLevel
	}
	for index, maximum := range policy.JobParameters.Maximum {
		result[index] = JobParameter{
			HP:     maximum.HP * level / policy.JobParameters.MaxStatusLevel,
			Attack: maximum.Attack * level / policy.JobParameters.MaxStatusLevel,
			Magic:  maximum.Magic * level / policy.JobParameters.MaxStatusLevel,
			Mind:   maximum.Mind * level / policy.JobParameters.MaxStatusLevel,
		}
	}
	return result
}
