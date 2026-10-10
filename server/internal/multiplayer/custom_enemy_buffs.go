package multiplayer

import (
	"fmt"
	"kairisei.local/server/internal/gamestate"
	"strconv"
	"strings"
)

type CustomEnemyBuffField struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Value *float64 `json:"value,omitempty"`
	Min   int      `json:"min"`
	Max   int      `json:"max"`
}
type CustomEnemyBuffEditor struct {
	RoleIndex int                    `json:"role_index"`
	Name      string                 `json:"name"`
	Fields    []CustomEnemyBuffField `json:"fields"`
}

func customRoleTargetsPlayer(skill CombatSkillDefinition, role CombatSkillRole) bool {
	target := role.Target
	if target == "SELECT" {
		target = skill.Target
	}
	return target != "SELF" && combatTargetIsPlayer(target)
}

func customPlayerSupportName(role CombatSkillRole) string {
	parameter := map[string]string{"ATK": "物攻", "INT": "魔攻", "MND": "回复量", "DEF": "物防", "MDEF": "魔防", "HP": "HP", "MAX_HP": "最大 HP"}[role.Parameters[1]]
	attribute := map[string]string{"FIRE": "火", "ICE": "冰", "WIND": "风", "LIGHT": "光", "DARK": "暗", "EARTH": "地", "THUNDER": "雷", "WATER": "水", "ALL": "全"}[role.Parameters[5]]
	if attribute == "" {
		attribute = role.Parameters[5]
	}
	switch role.Function {
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_BY_NOW_TURN_DAMAGE":
		return parameter + "提升"
	case "PARAM_LIMIT_BREAK_FIXED":
		return parameter + "上限提升"
	case "CRITICAL_UP":
		return "暴击率提升"
	case "ATTR_DEF_UP":
		return attribute + "属性抗性提升"
	case "REGENERATE_FIXED", "REGENERATE_BY_SELF_PARAM":
		return "每回合回复 HP"
	case "ENCHANT":
		return attribute + "属性追加伤害"
	case "DEAL_BONUS":
		return "额外抽卡"
	case "GUTS":
		return "致死后回复 HP"
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		return "伤害屏障"
	case "BURST_GAUGE_QUICK_UP":
		return "提升爆发槽"
	case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_MAXHP":
		return "立即回复 HP"
	case "DEBUFF_RELEASE", "DEBUFF_RELEASE_OLD", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM":
		return "解除玩家减益"
	}
	return ""
}

func CustomEnemyBuffEditors(skill CombatSkillDefinition, roles []CombatSkillRole) []CustomEnemyBuffEditor {
	out := []CustomEnemyBuffEditor{}
	for i, r := range roles {
		name := customPlayerSupportName(r)
		if name == "" || !customRoleTargetsPlayer(skill, r) {
			continue
		}
		fields := []CustomEnemyBuffField{}
		add := func(key, label string, value *float64, min, max int) {
			fields = append(fields, CustomEnemyBuffField{key, label, value, min, max})
		}
		number := func(n int) *float64 { value := float64(n); return &value }
		duration := true
		valueLabel := "增加点数"
		var value *float64
		maxValue := 100000000
		switch r.Function {
		case "ATK_UP_FIXED", "DEF_UP_FIXED", "PARAM_LIMIT_BREAK_FIXED":
			value = number(fixedBuffRoleValue(r, calibratedEnemySkillLevel(r.Function), 1))
		case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_BY_NOW_TURN_DAMAGE":
		case "REGENERATE_FIXED":
			valueLabel = "每回合回复点数"
			if combatParameterInt(r.Parameters[3]) == 0 && combatParameterInt(r.Parameters[4]) == 0 {
				value = number(fixedRegenerateRoleValue(r, calibratedEnemySkillLevel(r.Function), 1, 0))
			}
		case "REGENERATE_BY_SELF_PARAM":
			valueLabel = "每回合回复点数"
		case "ENCHANT":
			valueLabel = "每次命中的追加伤害"
			value = number(enemyPersistentRoleValue(r, calibratedEnemySkillLevel(r.Function), &battleEnemy{}))
		case "CRITICAL_UP":
			valueLabel = "暴击率增加（%）"
			maxValue = 100
			rate := float64(retainedRateRoleValue(r, calibratedEnemySkillLevel(r.Function))) / 10
			value = &rate
		case "ATTR_DEF_UP":
			valueLabel = "属性固定减伤"
			value = number(combatParameterInt(r.Parameters[3]) + combatParameterInt(r.Parameters[4])*calibratedEnemySkillLevel(r.Function)/1000)
			add("rate", "属性伤害减免（%）", number(combatParameterInt(r.Parameters[1])+combatParameterInt(r.Parameters[2])*calibratedEnemySkillLevel(r.Function)), 0, 100)
		case "GUTS":
			valueLabel = "致死后回复最大 HP（%）"
			maxValue = 100
			value = number(combatParameterInt(r.Parameters[2]))
			add("count", "可触发次数", number(combatParameterInt(r.Parameters[1])), 1, 20)
		case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
			valueLabel = "每次可抵挡伤害上限"
			value = number(attackBarrierRoleValue(r, calibratedEnemySkillLevel(r.Function)))
			add("count", "可抵挡次数", number(combatParameterInt(r.Parameters[3])), 1, 20)
		case "DEAL_BONUS":
			duration = false
			valueLabel = "额外抽卡张数（下次抽卡）"
			maxValue = 5
			value = number(combatParameterInt(r.Parameters[0]))
		case "BURST_GAUGE_QUICK_UP":
			duration = false
			valueLabel = "爆发槽增加（%）"
			maxValue = 100
			value = number(burstGaugeQuickValue(r, calibratedEnemySkillLevel(r.Function), 1))
		case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_MAXHP":
			duration = false
			valueLabel = "回复 HP 点数"
			if r.Function == "HEAL_FIXED" && combatParameterInt(r.Parameters[3]) == 0 {
				value = number(fixedHealRoleValue(r, calibratedEnemySkillLevel(r.Function), 1, 0))
			}
		default:
			continue // Release selectors/rates retain their original contract.
		}
		add("value", valueLabel, value, 0, maxValue)
		if duration {
			add("duration", "持续回合", number(maxInt(1, combatParameterInt(r.Parameters[0]))), 1, 999)
		}
		out = append(out, CustomEnemyBuffEditor{i, name, fields})
	}
	return out
}

func customBuffFieldValues(b gamestate.TeamBattleEnemyBuffOverride) map[string]*int {
	return map[string]*int{"value": b.Value, "duration": b.Duration, "count": b.Count, "rate": b.Rate}
}

func applyCustomEnemyBuffs(skill CombatSkillDefinition, roles []CombatSkillRole, overrides []gamestate.TeamBattleEnemyBuffOverride) ([]CombatSkillRole, error) {
	if len(overrides) == 0 {
		return roles, nil
	}
	specs := map[int]CustomEnemyBuffEditor{}
	for _, s := range CustomEnemyBuffEditors(skill, roles) {
		specs[s.RoleIndex] = s
	}
	next := append([]CombatSkillRole(nil), roles...)
	seen := map[int]bool{}
	for _, b := range overrides {
		spec, ok := specs[b.RoleIndex]
		if !ok || seen[b.RoleIndex] {
			return nil, fmt.Errorf("效果 %d 不是可编辑的玩家增益", b.RoleIndex+1)
		}
		seen[b.RoleIndex] = true
		for key, v := range customBuffFieldValues(b) {
			if v == nil {
				continue
			}
			valid := false
			for _, f := range spec.Fields {
				if f.Key == key && *v >= f.Min && *v <= f.Max {
					valid = true
				}
			}
			if !valid {
				return nil, fmt.Errorf("效果 %d 的 %s 参数无效或超出范围", b.RoleIndex+1, key)
			}
		}
		r := &next[b.RoleIndex]
		p := func(i, n int) { r.Parameters[i] = strconv.Itoa(n) }
		if b.Duration != nil {
			p(0, *b.Duration)
		}
		if b.Count != nil {
			if r.Function == "GUTS" {
				p(1, *b.Count)
			} else {
				p(3, *b.Count)
			}
		}
		if b.Rate != nil {
			p(1, *b.Rate)
			p(2, 0)
		}
		if b.Value != nil {
			switch r.Function {
			case "CRITICAL_UP":
				p(1, *b.Value*10)
				p(2, 0)
			case "ATTR_DEF_UP":
				p(3, *b.Value)
				p(4, 0)
			case "GUTS":
				p(2, *b.Value)
			case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
				p(1, *b.Value)
				p(2, 0)
			case "DEAL_BONUS":
				p(0, *b.Value)
			case "BURST_GAUGE_QUICK_UP":
				p(0, *b.Value)
				p(1, 0)
			default:
				r.CustomBuffValue = b.Value
			}
		}
	}
	return next, nil
}

func CustomEnemyAnimationFamily(function string) string {
	if strings.HasPrefix(function, "ATK_UP") || strings.HasPrefix(function, "DEF_UP") {
		return "parameter"
	}
	if strings.HasPrefix(function, "HEAL_") {
		return "heal"
	}
	return function
}

// The client reads its immutable role CSV for animation, while effect rows
// carry the authoritative results. Preserve role count/order and effect family.
func CompatibleCustomEnemyAnimation(roles, visual []CombatSkillRole) bool {
	if len(roles) == 0 || len(roles) != len(visual) {
		return false
	}
	for i := range roles {
		if CustomEnemyAnimationFamily(roles[i].Function) != CustomEnemyAnimationFamily(visual[i].Function) {
			return false
		}
	}
	return true
}

func (engine *BattleEngine) customEnemyPresentation(enemy *battleEnemy, a gamestate.TeamBattleEnemyAction, skill CombatSkillDefinition, roles []CombatSkillRole) CombatSkillDefinition {
	if a.AnimationFunctionID > 0 {
		skill.FunctionID = a.AnimationFunctionID
		return skill
	}
	if a.AnimationSource || enemy.CustomAnimationModel == "" {
		return skill
	}
	hasVisual := func(rs []CombatSkillRole) bool {
		if len(rs) == 0 {
			return false
		}
		if enemy.CustomAnimationModel == "3d" {
			return rs[0].Effect3D != ""
		}
		return rs[0].Effect2D != ""
	}
	if hasVisual(roles) {
		return skill
	}
	owners := []*battleEnemy{enemy}
	for i := 0; i < engine.enemyCount; i++ {
		if &engine.enemies[i] != enemy {
			owners = append(owners, &engine.enemies[i])
		}
	}
	for _, owner := range owners {
		for _, action := range owner.Level.Actions {
			if action.Category == "death" {
				continue
			}
			for _, s := range engine.catalog.EnemySkills[action.SkillID] {
				visual := engine.catalog.EnemySkillRoles[s.FunctionID]
				if hasVisual(visual) && CompatibleCustomEnemyAnimation(roles, visual) {
					skill.FunctionID = s.FunctionID
					return skill
				}
			}
		}
	}
	return skill
}

func CustomEnemyPlayerSupportOnly(skills []CombatSkillDefinition, functionID int, roles []CombatSkillRole) bool {
	for _, s := range skills {
		if s.FunctionID == functionID {
			for _, r := range roles {
				if r.Function == "OUTPUT_TEXT" {
					continue
				}
				if !customRoleTargetsPlayer(s, r) || customPlayerSupportName(r) == "" {
					return false
				}
			}
			return len(roles) > 0
		}
	}
	return false
}
