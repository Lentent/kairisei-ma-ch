package multiplayer

import (
	"strconv"
	"strings"
)

func scaleHealChain(value int, chainRate int, chainCount int) int {
	if chainCount > 1 && chainRate != 0 {
		return value * (100 + chainRate*(chainCount-1)) / 100
	}
	return value
}

func fixedHealRoleValue(role CombatSkillRole, level int, chainCount int, source int) int {
	// a24ff keeps fixed/source terms separate in the producer, but 822a0
	// combines them before multiplying the complete heal by the Chain rate.
	// The producer's param7 is PvP scaling, not the Chain multiplier.
	fixed := combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level/1000
	coefficient := combatParameterInt(role.Parameters[2]) + combatParameterInt(role.Parameters[3])*level
	return scaleHealChain(maxInt(0, fixed+source*coefficient/1000), role.ChainRate, chainCount)
}

func selfScaledHealRoleValue(role CombatSkillRole, level int, chainCount int, source int) int {
	// a236e/a207d -> 822a0 subtype 1: Chain contributes percentage points
	// to the source coefficient (per-mille), not to the fixed p3*level term.
	if capValue := combatParameterInt(role.Parameters[4]); capValue > 0 {
		source = minInt(source, capValue)
	}
	coefficient := combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	if chainCount > 1 {
		coefficient += role.ChainRate * (chainCount - 1) * 10
	}
	fixed := combatParameterInt(role.Parameters[3]) * level
	return maxInt(0, source*coefficient/1000+fixed)
}

func targetMaxHPHealRoleValue(role CombatSkillRole, level int, targetMaxHP int) int {
	// Native role 11 FUN_000a23f3/FUN_000a207d stores p0+p1*level as the
	// per-mille coefficient, p2*level as the independent fixed segment and p3
	// as a positive cap on the selected target's MAX_HP source. The active CN
	// enemy matrix has no chain rate, so no unproven combo approximation enters
	// this helper.
	source := maxInt(0, targetMaxHP)
	if capValue := combatParameterInt(role.Parameters[3]); capValue > 0 {
		source = minInt(source, capValue)
	}
	coefficient := combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level
	fixed := combatParameterInt(role.Parameters[2]) * level
	return maxInt(0, int(int64(source)*int64(coefficient)/1000)+fixed)
}

func calibratedEnemySkillLevel(function string) int {
	// Managed EnemyLevelupDataContainer assigns level 1 to ordinary, passive
	// and call enemy skills. Keep the runtime switch limited to semantic
	// families whose native producer and a complete official action have been
	// independently calibrated; the remaining legacy level-zero callers are
	// audited by active-master intersection rather than changed speculatively.
	switch function {
	case "HEAL_FIXED", "HEAL_BY_TARGET_MAXHP", "ATTR_DEF_UP", "ATTR_DEF_DOWN",
		"ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM", "REGENERATE_FIXED", "REVIVE", "ATTACK_AA",
		"ATK_OP_DRAIN", "ATK_OP_REVENGE", "ATTACK_BARRIER",
		"BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC",
		"CRITICAL_UP", "WEAKNESS", "CARD_TRAP_DAMAGE", "STAN",
		"BUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM",
		"PARAM_LIMIT_BREAK_FIXED", "HEAL_BY_SELF_PARAM", "DOT_VALUE_UP", "BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN", "DESTRUCT":
		return 1
	default:
		return 0
	}
}

func releaseRoleChance(role CombatSkillRole, level int) int {
	// Native release producers keep p0 as the base percent and p1 as the
	// ordinary skill-level coefficient. Selection mode and per-kind limits are
	// independent consumers of the remaining columns.
	return combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level
}

func reviveRoleValue(role CombatSkillRole, level int, targetMaxHP int) int {
	// Native role 57 FUN_00094116 produces a per-mille MaxHP rate from
	// p0+p1*level and an independent fixed p2*level segment. All active enemy
	// rows have zero chain rate, so the ordinary action reduces exactly to this
	// expression before the common MaxHP clamp.
	rate := combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level
	fixed := combatParameterInt(role.Parameters[2]) * level
	return minInt(targetMaxHP, maxInt(1, targetMaxHP*rate/1000+fixed))
}

func fixedRegenerateRoleValue(role CombatSkillRole, level int, chainCount int, source int) int {
	// a1e00 -> 8d7e0 case 0 freezes the complete value after Chain scaling.
	// Actual application also applies HEAL_BOOST before this final multiplier.
	fixed := combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level/1000
	coefficient := combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level
	return scaleHealChain(maxInt(0, fixed+source*coefficient/1000), role.ChainRate, chainCount)
}

func scaledRoleValue(role CombatSkillRole, level int) int {
	return combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level/1000
}

func attackRolePower(role CombatSkillRole, level int, source int) int {
	// a45d2 keeps the fixed and source segments separate. Fixed growth and
	// both neutral PvE percentage steps still use signed int32 arithmetic:
	// 100000000 becomes 14100654 even with 100%/100% modifiers (D-329 API).
	// The source multiplication, unlike those producer steps, is int64.
	fixed := int32(combatParameterInt(role.Parameters[0])) + int32(combatParameterInt(role.Parameters[1]))*int32(level)/1000
	fixed = fixed * 100 / 100
	fixed = fixed * 100 / 100
	coefficient := int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(level)
	return int(fixed) + int(int64(source)*int64(coefficient)/1000)
}

func attackExecutionPower(role CombatSkillRole, level int, source int) int {
	// 927b0 rounds source*coefficient and its division through float32.
	// a45d2's display preview instead keeps the int64 division above.
	coefficient := int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(level)
	scaled := float32(int64(maxInt(0, source))*int64(coefficient)) / float32(1000)
	return attackRolePower(role, level, 0) + int(scaled)
}

// 927b0 pierces positive defense only and truncates the removed amount.
func nativePiercedDefense(defense int, rate int) int {
	if defense > 0 && rate > 0 {
		return maxInt(0, defense-int(int64(defense)*int64(rate)/100))
	}
	return defense
}

func nativeAttributeAttackDamage(power int, attributeRate int, criticalRate int, defense int, fixedAdjustment int) (damage int, attributeDifference int) {
	// FUN_000927b0 applies the target's attribute rate before the critical
	// multiplier, preserves the attribute-only delta in ResultCmd60 arg4, and
	// only then consumes ordinary defense and fixed attribute-defense changes.
	// Keeping this shared prevents the player and enemy ATTACK_AA paths from
	// drifting in both integer truncation order and wire projection.
	// A wrapped negative producer value remains signed in the attribute
	// difference field; only the eventual damage commit has a minimum of one.
	attributeDamage := int(int64(power) * int64(maxInt(0, attributeRate)) / 100)
	attributeDifference = attributeDamage - power
	criticalDamage := int(int64(attributeDamage) * int64(maxInt(0, criticalRate)) / 100)
	damage = maxInt(1, criticalDamage-defense)
	damage = maxInt(1, damage+fixedAdjustment)
	return damage, attributeDifference
}

func nativeAttributeEnchantDamage(power int, attributeRate int, fixedAdjustment int) (damage int, attributeDifference int) {
	// 93461 has no ordinary-defense/critical stage, and clamps only AFTER
	// applying fixed attribute defense (unlike 927b0's intermediate clamp).
	attributeDamage := int(int64(maxInt(0, power)) * int64(maxInt(0, attributeRate)) / 100)
	return maxInt(1, attributeDamage+fixedAdjustment), attributeDamage - maxInt(0, power)
}

func attackBarrierRoleValue(role CombatSkillRole, level int) int {
	// Native role 63 FUN_00095e9a stores p0 as duration, p1 as the base
	// absorbed-damage threshold and p2 as its ordinary-skill level growth.
	return combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
}

func fixedSourceRoleValues(role CombatSkillRole, level int, source int) (fixed int, coefficient int, value int) {
	fixed = combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level/1000
	coefficient = combatParameterInt(role.Parameters[5]) + combatParameterInt(role.Parameters[6])*level
	value = fixed + int(int64(source)*int64(coefficient)/1000)
	return fixed, coefficient, value
}

func dotRoleValues(role CombatSkillRole, level int, _ int, source int) (rate int, fixed int, coefficient int, value int) {
	// a077a produces the unchained fixed/source value. 8bb40 first applies
	// target attribute, then Chain to the whole value, then enemy DOT reduction.
	// Keep preview/producer values separate from this target-specific commit.
	rate = combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	fixed, coefficient, value = fixedSourceRoleValues(role, level, source)
	return rate, fixed, coefficient, value
}

func retainedRateRoleValue(role CombatSkillRole, level int) int {
	return combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
}

func cardTrapDamageRoleValue(role CombatSkillRole, level int, _ int, source int) int {
	// Native role 92 FUN_000a354a uses the same fixed/source columns as the
	// common DOT producer after p1/p2 have independently selected the number
	// of trapped cards. Unlike a077a, a354a does not pass any Chain to its
	// consumer (action+0x54 stays zero).
	_, _, value := fixedSourceRoleValues(role, level, source)
	return value
}

func fixedBuffRoleValue(role CombatSkillRole, level int, chainCount int) int {
	first, second := fixedBuffRoleSegments(role, level)
	value := first + second
	if chainCount > 1 && role.ChainRate != 0 {
		// 795e1 creates an additive integer, not a percentage over the value.
		value += int32(role.ChainRate) * int32(chainCount-1)
	}
	return int(value)
}

func fixedBuffRoleSegments(role CombatSkillRole, level int) (int32, int32) {
	// Original roles 19/25/29/32/142 use numeric p2/1000, not a source enum.
	// Both the APK's x86 and ARM producers wrap EACH int32 multiplication.
	// Keep the two PvE 100/100 stages: cancelling them algebraically changes
	// overflow behavior. Actual CN Boss rows contain 3M/4M/5M/8.5M inputs.
	p := func(index int) int32 { return int32(combatParameterInt(role.Parameters[index])) }
	n := int32(level)
	first := ((p(3) + p(4)*n) * p(2)) / 1000
	first = (first * 100) / 100
	first = (first * 100) / 100
	second := (p(5) * n * 100) / 100
	second = (second * 100) / 100
	return first, second
}

func selfScaledParameterValue(role CombatSkillRole, level int, chainCount int, sourceValue int) int {
	// Native roles 17/23/28/30 cap the selected p2 source by positive p6,
	// multiply it by p3+p4*level plus ten times FUN_000795e1's additive chain
	// bonus per mille (a1618/a12e9/a0fba/a0c8b), then add p5*level.
	if capValue := combatParameterInt(role.Parameters[6]); capValue > 0 {
		sourceValue = minInt(sourceValue, capValue)
	}
	coefficient := combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level
	if chainCount > 1 && role.ChainRate != 0 {
		coefficient += 10 * role.ChainRate * (chainCount - 1)
	}
	return sourceValue*coefficient/1000 + combatParameterInt(role.Parameters[5])*level
}

func targetScaledParameterValue(role CombatSkillRole, sourceValue int) int {
	// Native roles 18/31/147 carry p7 as the positive source cap for the
	// selected target's p3 battle parameter. Every active CN enemy row has zero
	// p5/p6 growth, so its reachable value is min(target[p3], p7) * p4 / 1000.
	// Keeping this separate from selfScaledParameterValue prevents accidentally
	// reading the acting enemy when SELECT points at a user or another part.
	if capValue := combatParameterInt(role.Parameters[6]); capValue > 0 {
		sourceValue = minInt(sourceValue, capValue)
	}
	return sourceValue * combatParameterInt(role.Parameters[3]) / 1000
}

func nowTurnDamageParameterValue(role CombatSkillRole, damage int, level int, chainCount int) int {
	// Native roles 81/83/84 (FUN_0009d6b4/FUN_0009d2d8/FUN_0009d0ea)
	// use the source member's current-turn received-damage accumulator in
	// consumers 88a20/8fb80 (param_2+0x5a534), before applying to the target. The
	// p3+p4*level coefficient is percentage based, unlike the per-mille
	// self/target-parameter families. Active CN enemy rows have p4=0, but keep
	// the level term so the producer contract remains complete.
	value := maxInt(0, damage) * (combatParameterInt(role.Parameters[2]) + combatParameterInt(role.Parameters[3])*level) / 100
	if chainCount > 1 && role.ChainRate != 0 {
		value = value * (100 + role.ChainRate*(chainCount-1)) / 100
	}
	// The native buff/debuff consumers clamp this magnitude to at least one,
	// including when the source member has received no damage (D-357/D-373).
	return maxInt(1, value)
}

func combatParameterInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func combatStatValue(player *battlePlayer, name string) int {
	switch strings.ToUpper(name) {
	case "HP":
		return player.HP
	case "ATK":
		if player.MemberType >= 5 {
			return player.Attack
		}
		return minInt(player.Attack, activeParameterLimit(player.LimitAttack))
	case "INT":
		if player.MemberType >= 5 {
			return player.Magic
		}
		return minInt(player.Magic, activeParameterLimit(player.LimitMagic))
	case "MND":
		if player.MemberType >= 5 {
			return player.Recovery
		}
		return minInt(player.Recovery, activeParameterLimit(player.LimitRecovery))
	case "DEF":
		return player.Defense
	case "MDEF":
		return player.MDefense
	case "MAX_HP":
		return player.MaxHP
	default:
		return 0
	}
}

func activeParameterLimit(limit int) int {
	if limit <= 0 {
		return 99999
	}
	return minInt(999999, limit)
}

func playerFromEnemy(enemy *battleEnemy) *battlePlayer {
	return &battlePlayer{
		MemberType: enemy.MemberType,
		Attack:     enemy.Attack, Magic: enemy.Magic, Recovery: enemy.Recovery,
		Defense: enemy.Defense, MDefense: enemy.MDefense, HP: enemy.HP, MaxHP: enemy.MaxHP,
		LimitAttack: enemy.LimitAttack, LimitMagic: enemy.LimitMagic, LimitRecovery: enemy.LimitRecovery,
	}
}

func (engine *BattleEngine) enemyAttributeRateWithEffects(enemy *battleEnemy, attribute string) int {
	rate, _ := engine.enemyAttackAttribute(enemy, attribute)
	return rate
}

func (engine *BattleEngine) enemyAttackAttribute(enemy *battleEnemy, attribute string) (int, string) {
	return enemyBaseAttackAttribute(enemy, attribute)
}

func playerAttributeRateWithEffects(player *battlePlayer, attribute string) int {
	// 927b0/93461 use 100 for PvE players. WEAKNESS is a separate ordinary
	// damage multiplier, not an attribute rate and not an enchant/DOT bonus.
	return 100
}

// attributeDefenseAdjustment mirrors the two ATTR_DEF aggregate fields in
// FUN_00043a26/FUN_00071d48. For the per-mille field, only the strongest UP
// and strongest DOWN survive before their sum is clamped to +/-3000. The
// signed fixed field is accumulated across every matching attribute effect.
func attributeDefenseAdjustment(effects []battleEffect, attribute string) (int, int) {
	strongestUp := 0
	strongestDown := 0
	fixed := 0
	for _, effect := range effects {
		if effect.Remaining <= 0 || (effect.Function != "ATTR_DEF_UP" && effect.Function != "ATTR_DEF_DOWN") {
			continue
		}
		if effect.Attribute != "" && !strings.EqualFold(effect.Attribute, attribute) {
			continue
		}
		primary := effect.Parameters[0]
		fixed += effect.Parameters[1]
		if effect.Function == "ATTR_DEF_UP" {
			strongestUp = minInt(strongestUp, primary)
		} else {
			strongestDown = maxInt(strongestDown, primary)
		}
	}
	return minInt(3000, maxInt(-3000, strongestUp+strongestDown)), fixed
}

func (engine *BattleEngine) enemyEffectiveDefense(enemy *battleEnemy, damageKind string) int {
	value := enemy.Defense
	if strings.EqualFold(damageKind, "MAGIC") {
		value = enemy.MDefense
	} else if strings.EqualFold(damageKind, "ALL") {
		value += enemy.MDefense
	}
	// 743f6 already includes retained DEF/MDEF deltas. Re-applying their
	// effects here doubled only the FIXED families. 927b0 also adds the base
	// enemy damage_down field before piercing, independently of attribute armor.
	return value + enemy.DamageReduction
}

func combatAttributeIndex(attribute string) int {
	switch strings.ToUpper(attribute) {
	case "FIRE":
		return 0
	case "ICE":
		return 1
	case "WIND":
		return 2
	case "LIGHT":
		return 3
	case "DARK":
		return 4
	case "EARTH":
		return 5
	case "THUNDER":
		return 6
	case "WATER":
		return 7
	case "NEUTRAL", "NONE", "NULL", "":
		return 8
	default:
		return -1
	}
}

func combatAttributeCode(attribute string) int {
	// Managed ATTR is NULL=0, FIRE..WATER=1..8 and NEUTRAL=9. The master
	// string NULL is therefore not the ninth attribute-rate column.
	switch strings.ToUpper(strings.TrimSpace(attribute)) {
	case "", "NULL", "NONE":
		return 0
	case "NEUTRAL":
		return 9
	}
	index := combatAttributeIndex(attribute)
	if index < 0 {
		return 0
	}
	return index + 1
}

func splitCombatAttributes(attribute string) []string {
	parts := strings.FieldsFunc(strings.ToUpper(attribute), func(character rune) bool { return character == '_' || character == '/' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if combatAttributeIndex(part) >= 0 && part != "NONE" && part != "NULL" {
			result = append(result, part)
		}
	}
	return result
}

func maxCombatChain(attribute string, counts map[string]int) int {
	if count, exists := counts[strings.ToUpper(attribute)]; exists {
		return count
	}
	maximum := 0
	for _, item := range splitCombatAttributes(attribute) {
		if counts[item] > maximum {
			maximum = counts[item]
		}
	}
	return maximum
}

// FUN_000796a8 counts each member at most once among selected CARDS;
// FUN_000cec12 matches either component of a dual attribute. The sphere
// branch of FUN_0003d342 supplies chain zero and is not a contributor.
// Native represents one/no participant as zero, not a displayed "1 Chain".
func battleActionChainCounts(actions []battleAction) map[string]int {
	counts := make(map[string]int)
	for _, action := range actions {
		if action.cardType == 0 || action.sphereSlot != 0 {
			continue
		}
		counts[strings.ToUpper(action.skill.Attribute)] = 0
		for _, attribute := range splitCombatAttributes(action.skill.Attribute) {
			counts[attribute] = 0
		}
	}
	for attribute := range counts {
		members := make(map[int]bool, maxRoomMembers)
		for _, action := range actions {
			if action.cardType == 0 || action.sphereSlot != 0 {
				continue
			}
			for _, component := range splitCombatAttributes(attribute) {
				if combatAttributeMatches(action.skill.Attribute, component) {
					members[action.memberType] = true
					break
				}
			}
		}
		if len(members) > 1 {
			counts[attribute] = len(members)
		}
	}
	return counts
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
