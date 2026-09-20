package gamestate

import (
	"errors"
	"math"
)

type SphereExperienceState struct {
	Level               int
	Experience          int
	NowLevelExperience  int
	NextLevelExperience int
}

// SphereMaterialExperience derives the add_exp field sent in proto.SphrInfo.
// The managed client sums this field directly when previewing a fusion, so the
// server must publish the level-dependent value rather than only a rarity base.
func SphereMaterialExperience(base int, level int, bonusPermillePerLevel int) (int, error) {
	if base <= 0 || level < 1 || bonusPermillePerLevel < 0 {
		return 0, errors.New("sphere material experience inputs are invalid")
	}
	levels := level - 1
	if levels != 0 && bonusPermillePerLevel > math.MaxInt/levels {
		return 0, errors.New("sphere material experience multiplier overflows")
	}
	bonusPermille := levels * bonusPermillePerLevel
	if bonusPermille != 0 && base > math.MaxInt/bonusPermille {
		return 0, errors.New("sphere material experience overflows")
	}
	bonus := base * bonusPermille / 1000
	if base > math.MaxInt-bonus {
		return 0, errors.New("sphere material experience overflows")
	}
	return base + bonus, nil
}

// NormalizeSphereExperience consumes the official sphr_lvup_exp row as
// per-level requirements. The managed client uses the same progression shape
// for cards, spheres and buddies: exp is lifetime total, now_lv_exp is the
// amount earned inside the current level, and next_lv_exp is the remainder.
func NormalizeSphereExperience(levelMaximum int, experience int, table []int) (SphereExperienceState, error) {
	if levelMaximum < 1 || experience < 0 || len(table) < levelMaximum-1 {
		return SphereExperienceState{}, errors.New("sphere experience inputs are invalid")
	}
	maximum := 0
	for _, required := range table[:levelMaximum-1] {
		if required <= 0 || maximum > math.MaxInt-required {
			return SphereExperienceState{}, errors.New("sphere experience table is invalid")
		}
		maximum += required
	}
	if experience > maximum {
		experience = maximum
	}
	level := 1
	spent := 0
	for level < levelMaximum && experience >= spent+table[level-1] {
		spent += table[level-1]
		level++
	}
	state := SphereExperienceState{Level: level, Experience: experience}
	if level < levelMaximum {
		state.NowLevelExperience = experience - spent
		state.NextLevelExperience = table[level-1] - state.NowLevelExperience
	}
	return state, nil
}

func SphereExperienceAtLevelStart(level int, levelMaximum int, table []int) (int, error) {
	if level < 1 || level > levelMaximum || len(table) < levelMaximum-1 {
		return 0, errors.New("sphere level is outside its experience table")
	}
	total := 0
	for _, required := range table[:level-1] {
		if required <= 0 || total > math.MaxInt-required {
			return 0, errors.New("sphere experience table is invalid")
		}
		total += required
	}
	return total, nil
}
