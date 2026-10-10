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
	// nil inherits AI; a non-nil empty list deliberately disables turn actions.
	Actions []TeamBattleEnemyAction `json:"actions"`
	// With a custom list, retain template AI and append scheduled actions to it.
	IncludeOriginalActions bool   `json:"include_original_actions,omitempty"`
	AnimationModel         string `json:"animation_model,omitempty"`
}

// Actions run in list order. Turn zero is the opening attack; RepeatEvery=0
// executes once. Damage changes apply to this invocation, never shared masters.
type TeamBattleEnemyAction struct {
	SourceBossID        int                           `json:"source_boss_id,omitempty"`
	Turn                int                           `json:"turn"`
	RepeatEvery         int                           `json:"repeat_every"`
	SkillID             int                           `json:"skill_id"`
	FunctionID          int                           `json:"function_id"`
	Target              string                        `json:"target"`
	Power               *int                          `json:"power,omitempty"`
	PowerRate           *int                          `json:"power_rate,omitempty"`
	Hits                *int                          `json:"hits,omitempty"`
	Buffs               []TeamBattleEnemyBuffOverride `json:"buffs,omitempty"`
	AnimationFunctionID int                           `json:"animation_function_id,omitempty"`
	AnimationSource     bool                          `json:"animation_source,omitempty"`
}

type TeamBattleEnemyBuffOverride struct {
	RoleIndex int  `json:"role_index"`
	Value     *int `json:"value,omitempty"`
	Duration  *int `json:"duration,omitempty"`
	Count     *int `json:"count,omitempty"`
	Rate      *int `json:"rate,omitempty"`
}

func ValidateTeamBattleEnemyActions(rows []TeamBattleEnemyAction) error {
	if len(rows) > 100 {
		return errors.New("每个部位最多配置100条行动")
	}
	for _, a := range rows {
		if a.AnimationFunctionID < 0 || a.AnimationSource && a.AnimationFunctionID != 0 || len(a.Buffs) > 5 {
			return errors.New("演出或 Buff 配置无效")
		}
		seen := map[int]bool{}
		for _, b := range a.Buffs {
			if b.RoleIndex < 0 || b.RoleIndex > 4 || seen[b.RoleIndex] {
				return errors.New("Buff 效果序号无效或重复")
			}
			seen[b.RoleIndex] = true
			if b.Value != nil && (*b.Value < 0 || *b.Value > 100000000) || b.Duration != nil && (*b.Duration < 1 || *b.Duration > 999) || b.Count != nil && (*b.Count < 1 || *b.Count > 20) || b.Rate != nil && (*b.Rate < 0 || *b.Rate > 100) {
				return errors.New("Buff 数值、持续回合或次数超出范围")
			}
		}
		if a.Turn < 0 || a.Turn > 999 || a.RepeatEvery < 0 || a.RepeatEvery > 999 || a.SkillID <= 0 || a.FunctionID <= 0 {
			return errors.New("行动回合须为0至999、循环间隔为0至999，且须选择有效招式")
		}
		switch a.Target {
		case "AUTO", "RANDOM", "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER":
		default:
			return errors.New("行动目标无效")
		}
		if a.Power != nil && (*a.Power < 0 || *a.Power > 100000000) || a.PowerRate != nil && (*a.PowerRate < 0 || *a.PowerRate > 1000) || a.Hits != nil && (*a.Hits < 1 || *a.Hits > 20) {
			return errors.New("基础伤害须为0至1亿，伤害倍率为0至1000%，攻击次数为1至20")
		}
	}
	// Every schedule repeats within these bounded turns; reject queues rather
	// than silently dropping actions at the native 20-action limit.
	for turn := 0; turn <= 999; turn++ {
		count := 0
		for _, a := range rows {
			if a.MatchesTurn(turn) {
				count++
			}
		}
		if count > 20 {
			return errors.New("同一部位每回合最多执行20条行动")
		}
	}
	return nil
}

func (a TeamBattleEnemyAction) MatchesTurn(turn int) bool {
	return turn == a.Turn || turn > a.Turn && a.RepeatEvery > 0 && (turn-a.Turn)%a.RepeatEvery == 0
}

func CloneTeamBattleEnemyActions(rows []TeamBattleEnemyAction) []TeamBattleEnemyAction {
	if rows == nil {
		return nil
	}
	next := append([]TeamBattleEnemyAction{}, rows...)
	for i := range next {
		next[i].Buffs = append([]TeamBattleEnemyBuffOverride(nil), rows[i].Buffs...)
		for j := range next[i].Buffs {
			b := &next[i].Buffs[j]
			for _, field := range []**int{&b.Value, &b.Duration, &b.Count, &b.Rate} {
				if *field != nil {
					v := **field
					*field = &v
				}
			}
		}
		for _, field := range []**int{&next[i].Power, &next[i].PowerRate, &next[i].Hits} {
			if *field != nil {
				v := **field
				*field = &v
			}
		}
	}
	return next
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
		result[i].Actions = CloneTeamBattleEnemyActions(result[i].Actions)
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
