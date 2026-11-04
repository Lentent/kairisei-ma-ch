package admin

import (
	"fmt"
	"strings"
)

// A direction owns the entire result playback, not just the attack animation.
// Reuse a native complete direction when borrowing healing into an attack.
// Never change role parameters, target selection, or skill branch conditions.
func resolveCustomHealingPresentation(skills, roles [][]string, sources customCardSources) error {
	groups := map[string][][]string{}
	for _, r := range roles {
		groups[r[0]] = append(groups[r[0]], r)
	}
	native := map[string][]string{}
	for _, r := range sources.Roles {
		if _, ok := native[r[0]]; !ok {
			native[r[0]] = r
		}
	}
	for _, group := range groups {
		attack, heal, buff := false, false, false
		for _, r := range group {
			switch r[8] {
			case "ATTACK_AA":
				attack = true
			case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "ATK_OP_DRAIN", "ATK_OP_DRAIN_ALL":
				heal = true
			case "ATK_OP_PIERCING", customPowerBoost: // inert/attack operators do not need buff playback
			default:
				if !strings.HasPrefix(r[8], "ATK_OP_") {
					buff = true
				}
			}
		}
		if !attack || !heal {
			continue
		}
		first := group[0]
		if customDirectionSupportsHeal(first[1], buff) {
			continue
		}
		var targetSkill []string
		for _, s := range skills {
			if s[49] == first[0] {
				targetSkill = s
				break
			}
		}
		if targetSkill == nil {
			continue
		} // Unreferenced source groups are not played.
		var chosen []string
		for _, s := range sources.Skills {
			if s[10] != targetSkill[10] || s[19] != targetSkill[19] {
				continue
			}
			r := native[s[49]]
			if len(r) < 8 || r[3] == "" || !customDirectionSupportsHeal(r[1], buff) {
				continue
			}
			chosen = r
			// Prefer the same attribute, but a different native visual is still safe.
			if s[11] == targetSkill[11] {
				break
			}
		}
		if chosen == nil {
			return fmt.Errorf("技能%s包含攻击和治疗，但没有兼容的完整动作；请选择同时支持攻击%s及治疗的动作来源", targetSkill[0], map[bool]string{true: "、增益/减益", false: ""}[buff])
		}
		for _, r := range group {
			copy(r[1:8], chosen[1:8])
		}
	}
	return nil
}

// These native script families explicitly consume multi_damage/multi_heal
// (and multi_buff for compound actions). Do not infer support from role data:
// a user may have borrowed healing into a direction without that callback.
func customDirectionSupportsHeal(name string, buff bool) bool {
	if !strings.HasPrefix(name, "player_skill_") {
		return false
	}
	if buff {
		return strings.Contains(name, "_buff_heal_") || strings.Contains(name, "_drain_buff_") || strings.Contains(name, "_buff_burst_heal_")
	}
	return strings.Contains(name, "_heal_") || strings.Contains(name, "_drain_")
}
