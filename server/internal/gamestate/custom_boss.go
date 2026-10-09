package gamestate

import (
	"errors"
	"strings"
)

// Effective HP includes the template party's multiplier. These definitions are
// private to an encounter; native enemy IDs still select its art and actions.
type TeamBattleEnemyStats struct {
	EnemyID           int      `json:"enemy_id"`
	Attribute         string   `json:"attribute"`
	HP                int      `json:"hp"`
	Attack            int      `json:"attack"`
	Magic             int      `json:"magic"`
	Recovery          int      `json:"recovery"`
	Defense           int      `json:"defense"`
	MagicDefense      int      `json:"magic_defense"`
	DamageReduction   int      `json:"damage_reduction"`
	AttributeFixed    [5]int   `json:"attribute_fixed"`
	AttributeRates    *[9]int  `json:"attribute_rates,omitempty"`
	StatusResistances *[15]int `json:"status_resistances,omitempty"`
	DOTReductions     *[5]int  `json:"dot_reductions,omitempty"`
}

type TeamBattleEnemyOverride struct {
	BattleIndex int                  `json:"battle_index"`
	EnemyIndex  int                  `json:"enemy_index"`
	Stats       TeamBattleEnemyStats `json:"stats"`
}

func ValidateTeamBattleEnemyStats(s TeamBattleEnemyStats) error {
	if s.EnemyID <= 0 || s.HP < 1 || s.HP > 2000000000 {
		return errors.New("敌人ID无效或HP超出1至20亿")
	}
	for _, n := range []int{s.Attack, s.Magic, s.Recovery, s.DamageReduction} {
		if n < 0 || n > 2000000000 {
			return errors.New("攻击、回复和减伤须为0至20亿")
		}
	}
	for _, n := range append([]int{s.Defense, s.MagicDefense}, s.AttributeFixed[:]...) {
		if n < -2000000000 || n > 2000000000 {
			return errors.New("防御与属性固定减伤超出范围")
		}
	}
	allowed := map[string]bool{"FIRE": true, "ICE": true, "WIND": true, "LIGHT": true, "DARK": true, "EARTH": true, "THUNDER": true, "WATER": true, "NEUTRAL": true, "NONE": true, "NULL": true}
	for _, attr := range strings.Split(s.Attribute, "_") {
		if !allowed[attr] {
			return errors.New("敌人属性无效")
		}
	}
	for _, values := range []*[9]int{s.AttributeRates} {
		if values != nil {
			for _, n := range values {
				if n < 0 || n > 100000 {
					return errors.New("属性伤害倍率须为0至100000")
				}
			}
		}
	}
	if s.StatusResistances != nil {
		for _, n := range s.StatusResistances {
			if n < 0 || n > 100 {
				return errors.New("异常状态抗性须为0至100")
			}
		}
	}
	if s.DOTReductions != nil {
		for _, n := range s.DOTReductions {
			if n < 0 || n > 2000000000 {
				return errors.New("持续伤害减伤须为0至20亿")
			}
		}
	}
	return nil
}

func CloneTeamBattleEnemyOverrides(rows []TeamBattleEnemyOverride) []TeamBattleEnemyOverride {
	if rows == nil {
		return nil
	}
	result := append([]TeamBattleEnemyOverride{}, rows...)
	for i := range result {
		s := &result[i].Stats
		if s.AttributeRates != nil {
			v := *s.AttributeRates
			s.AttributeRates = &v
		}
		if s.StatusResistances != nil {
			v := *s.StatusResistances
			s.StatusResistances = &v
		}
		if s.DOTReductions != nil {
			v := *s.DOTReductions
			s.DOTReductions = &v
		}
	}
	return result
}
