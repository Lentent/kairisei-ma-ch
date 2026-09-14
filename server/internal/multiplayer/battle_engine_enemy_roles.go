package multiplayer

import (
	"fmt"
	"strings"
)

func enemyCombatFunctionRegistered(function string) bool {
	switch function {
	case "ATK_BREAK_BY_NOW_TURN_DAMAGE", "ATK_BREAK_BY_SELF_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "ATK_BREAK_FIXED",
		"ATK_OP_DAMAGE_INCREASE", "ATK_OP_DRAIN", "ATK_OP_NOW_TURN_REVENGE", "ATK_OP_PIERCING", "ATK_OP_REVENGE", "ATK_OP_REFLECTION_INVALID",
		"ATK_UP_BY_NOW_TURN_DAMAGE", "ATK_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_FIXED", "ATTACK_AA",
		"ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR", "ATTR_DEF_DOWN", "ATTR_DEF_UP", "ATTR_HIDE", "BLEED", "BLESS",
		"BLESS_TURN_DOWN", "BUFF_RELEASE", "BUFF_RELEASE_OLD", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM", "BURN",
		"BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN", "CARD_SEAL", "CARD_TRAP_DAMAGE", "COST_BLOCK", "CRITICAL_DOWN", "CRITICAL_UP", "CRITICAL_DAMAGE_BOOST", "STAN",
		"DARKNESS_APPOINT", "DARKNESS_RANDOM", "DEAL_BONUS", "DEAL_PENALTY", "DEAL_PENALTY_TURN_APPOINT",
		"DEBUFF_REGIST", "DEBUFF_RELEASE", "DEBUFF_RELEASE_OLD", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM", "DEF_UP_BY_SELF_PARAM", "DEF_UP_FIXED", "DESTRUCT",
		"DOT_VALUE_UP", "ELECTRIC", "ENCHANT", "ENDURE", "ENEMY_AI_TRIGGER_FLAG_SET", "ENEMY_AWAKE_FLAG_SET", "ENEMY_AI_VAR_ADD",
		"ENEMY_CURSE", "FORCE_BATTLE_END", "FREEZE", "GUARD_BREAK_BY_NOW_TURN_DAMAGE", "GUARD_BREAK_BY_SELF_PARAM",
		"GUARD_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_FIXED", "GUTS", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_MAXHP",
		"HEAL_FIXED", "HEAL_REVERSE", "HP_CUT", "NONE", "OUTPUT_TEXT", "PARAM_LIMIT_BREAK_FIXED", "POISON",
		"REFLECTION", "REGENERATE_BY_SELF_PARAM", "REGENERATE_FIXED", "REVIVE", "REWRITE", "WEAKNESS",
		"TRANCE_GAUGE_STATE_CHANGE", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return true
	default:
		return false
	}
}

func combatFunctionNeedsBuffCode(function string) bool {
	switch function {
	case "DEF_UP_FIXED", "DEF_UP_BY_SELF_PARAM", "ATK_UP_FIXED", "ATK_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_BY_NOW_TURN_DAMAGE",
		"ATK_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "ATK_BREAK_BY_NOW_TURN_DAMAGE",
		"GUARD_BREAK_FIXED", "GUARD_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_BY_NOW_TURN_DAMAGE",
		"REGENERATE_FIXED", "REGENERATE_BY_SELF_PARAM", "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC", "ENCHANT",
		"ATTR_DEF_DOWN", "ATTR_DEF_UP", "ATTR_HIDE", "CRITICAL_UP", "CRITICAL_DOWN", "PARAM_LIMIT_BREAK_FIXED", "WEAKNESS",
		"REFLECTION", "COVERING", "BLESS", "CARD_SEAL", "CARD_SEAL_REGIST", "DARKNESS_APPOINT", "DARKNESS_RANDOM",
		"DARKNESS_REGIST", "GUTS", "ENDURE", "STAN", "COST_BLOCK", "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR",
		"ATTR_SEE", "CARD_TRAP_DAMAGE", "HEAL_REVERSE", "DEBUFF_REGIST", "REWRITE", "DEAL_BONUS", "DEAL_PENALTY",
		"DEAL_PENALTY_TURN_APPOINT":
		return true
	default:
		return false
	}
}

func (engine *BattleEngine) executeEnemyRole(actor *battleEnemy, selected int, role CombatSkillRole, roles []CombatSkillRole) (results []BattleResult, err error) {
	defer func() {
		if err == nil {
			results = engine.projectRoleBuffParameters(results)
		}
	}()
	switch role.Function {
	case "", "NONE", "OUTPUT_TEXT":
		return nil, nil
	case "ATTACK_AA":
		return engine.executeEnemyAttack(actor, selected, role, roles)
	case "ATK_OP_DAMAGE_INCREASE", "ATK_OP_DRAIN", "ATK_OP_NOW_TURN_REVENGE", "ATK_OP_PIERCING", "ATK_OP_REVENGE", "ATK_OP_REFLECTION_INVALID":
		return nil, nil
	case "PARAM_LIMIT_BREAK_FIXED":
		return engine.executeEnemyParameterLimit(actor, selected, role)
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED",
		"ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM",
		"ATK_UP_BY_TARGET_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_BY_TARGET_PARAM",
		"ATK_UP_BY_NOW_TURN_DAMAGE", "ATK_BREAK_BY_NOW_TURN_DAMAGE", "GUARD_BREAK_BY_NOW_TURN_DAMAGE":
		return engine.executeEnemyParameterRole(actor, selected, role)
	case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_MAXHP":
		return engine.executeEnemyHeal(actor, selected, role)
	case "BUFF_RELEASE", "BUFF_RELEASE_OLD", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM",
		"DEBUFF_RELEASE", "DEBUFF_RELEASE_OLD", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM":
		return engine.executeEnemyRelease(actor, selected, role)
	case "BLESS":
		return engine.executeEnemyBless(actor, selected, role)
	case "ENEMY_CURSE":
		return engine.executeEnemyCurse(actor, selected, role)
	case "BLESS_TURN_DOWN":
		return engine.changeEnemyBlessTurns(actor, selected, role, -combatParameterInt(role.Parameters[0])), nil
	case "BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN":
		results := make([]BattleResult, 0, 8)
		level := calibratedEnemySkillLevel(role.Function)
		amount := burstGaugeQuickValue(role, level, 1)
		if role.Function == "BURST_GAUGE_QUICK_DOWN" {
			amount = -amount
		}
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			results = append(results, engine.addPlayerBurstGauge(memberType, role.RoleIndex, amount)...)
		}
		return results, nil
	case "DEAL_BONUS":
		return engine.executeEnemyDealChange(actor, selected, role, false)
	case "DEAL_PENALTY", "DEAL_PENALTY_TURN_APPOINT":
		return engine.executeEnemyDealChange(actor, selected, role, true)
	case "HP_CUT":
		return engine.executeEnemyHPCut(actor, selected, role)
	case "DESTRUCT":
		return engine.executeEnemyDestruct(actor, selected, role)
	case "REVIVE":
		return engine.executeEnemyRevive(actor, selected, role)
	case "FORCE_BATTLE_END":
		// Native FUN_00080a7b calls FUN_00062f03(1): it requests the normal
		// end-state evaluator instead of assigning DRAW or emitting a command.
		engine.forceEndCheck = true
		return nil, nil
	case "ENEMY_AI_VAR_ADD":
		return engine.executeEnemyAIVariable(actor, selected, role), nil
	case "TRANCE_GAUGE_STATE_CHANGE", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return engine.executeEnemyTranceRole(actor, selected, role), nil
	case "ENEMY_AI_TRIGGER_FLAG_SET":
		// Native action type 14/subtype 0 calls FUN_0005711b: p0 selects one
		// of 32 trigger bits and any positive p1 sets it; zero clears it. It
		// does not emit ResultCmd94 (that command belongs to subtype 2's AI
		// variable accumulator).
		flag := combatParameterInt(role.Parameters[0])
		if flag < 32 {
			// 80ec0 passes its resolved target to 5711b, not the source
			// or a shared party flag. Death actions may still target SELF.
			includeDead := role.Target != "ENEMY_ALL" && role.Target != "FRIEND_ALL"
			for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, includeDead) {
				mask := uint32(1) << (uint(flag) & 31)
				if combatParameterInt(role.Parameters[1]) > 0 {
					engine.enemies[index].AIFlags |= mask
				} else {
					engine.enemies[index].AIFlags &^= mask
				}
			}
		}
		return nil, nil
	case "ENEMY_AWAKE_FLAG_SET":
		// Subtype 1 calls FUN_0004cc73 on each resolved enemy target. Awake is
		// durable battle state but has no standalone ResultCmd projection.
		awake := combatParameterInt(role.Parameters[0])
		includeDead := role.Target != "ENEMY_ALL" && role.Target != "FRIEND_ALL"
		for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, includeDead) {
			engine.enemies[index].Awake = awake
		}
		return nil, nil
	case "REWRITE":
		return engine.executeEnemyRewrite(actor, selected, role), nil
	case "DOT_VALUE_UP":
		return engine.executeEnemyDOTValueUp(actor, selected, role), nil
	default:
		return engine.executeEnemyPersistentEffect(actor, selected, role)
	}
}

func (engine *BattleEngine) executeEnemyRewrite(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	return engine.executeEnemyRewriteWithListType(actor, selected, role, 0)
}

func (engine *BattleEngine) executeEnemyRewriteWithListType(actor *battleEnemy, selected int, role CombatSkillRole, listType int) []BattleResult {
	return engine.executeRewrite(actor.MemberType, role, listType,
		engine.enemyRolePlayerTargets(actor, selected, role), engine.enemyRoleEnemyTargets(actor, selected, role, false))
}

func (engine *BattleEngine) executePlayerRewrite(action battleAction, role CombatSkillRole) []BattleResult {
	var enemies []int
	if !combatTargetIsPlayer(role.Target) {
		enemies = engine.playerRoleEnemyTargets(action, role)
	}
	return engine.executeRewrite(action.memberType, role, 0, engine.playerRoleTargets(action, role), enemies)
}

func (engine *BattleEngine) executeRewrite(source int, role CombatSkillRole, listType int, players, enemies []int) []BattleResult {
	// Native role 43 (FUN_000974bd) creates action type 4/subtype 3. Its
	// FUN_0008d7e0 consumer owns BATTLE_BUFF.ATTR(202), replaces an older
	// attribute rewrite, emits immediate 32, and queues ATTR's 62/6 status.
	// The full API projection is confirmed by D-329's official 44099004/12.
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	attribute := role.Parameters[1]
	effect := battleEffect{
		Function: "REWRITE", ListType: listType, Attribute: attribute, Kind: 1, Remaining: duration,
		AppliedTurn: engine.turn, Source: source, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
	}
	results := make([]BattleResult, 0, 4)
	for _, memberType := range players {
		player := &engine.players[memberType-1]
		if player.BaseAttribute == "" {
			player.BaseAttribute = player.Attribute
			if player.BaseAttribute == "" {
				player.BaseAttribute = "NEUTRAL"
			}
		}
		var removed []battleEffect
		player.Effects, removed = replaceRewriteEffect(player.Effects, effect)
		results = appendRewriteReplacement(results, memberType, removed)
		player.Attribute = attribute
		results = append(results, rewriteResult(memberType, attribute),
			battleBuffResultWithListType(memberType, role.RoleIndex, listType, battleBuffCodes["REWRITE"], 0, 1<<combatAttributeCode(attribute), 0, 0, 0, 0))
	}
	for _, index := range enemies {
		enemy := &engine.enemies[index]
		if enemy.BaseAttribute == "" {
			enemy.BaseAttribute = enemy.Attribute
			if enemy.BaseAttribute == "" {
				enemy.BaseAttribute = "NULL"
			}
		}
		var removed []battleEffect
		enemy.Effects, removed = replaceRewriteEffect(enemy.Effects, effect)
		results = appendRewriteReplacement(results, enemy.MemberType, removed)
		enemy.Attribute = attribute
		results = append(results, rewriteResult(enemy.MemberType, attribute),
			battleBuffResultWithListType(enemy.MemberType, role.RoleIndex, listType, battleBuffCodes["REWRITE"], 0, 1<<combatAttributeCode(attribute), 0, 0, 0, 0))
	}
	return results
}

func (engine *BattleEngine) executeEnemyDestruct(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	// 8b660 invokes 8127b once per allowed role target, but 8127b consumes
	// its second argument (the caster), not the third (selected member).
	// Its 60 contains post-commit HP; it does not emit an additional 3.
	results := make([]BattleResult, 0, 8)
	count := len(engine.enemyRoleEnemyTargets(actor, selected, role, false))
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		count = len(engine.enemyRolePlayerTargets(actor, selected, role))
	}
	for i := 0; i < count; i++ {
		damage := nativeDestructDamage(actor.HP, role, calibratedEnemySkillLevel(role.Function))
		actor.HP = nativeHPCommit(actor.HP, actor.MaxHP, -damage, actor.Effects)
		recordEnemyDamage(actor, damage, "")
		engine.recordSpecialEnemyDamage(actor.MemberType, actor.MemberType, damage)
		results = append(results, battleDamageResult(actor.MemberType, role.RoleIndex, -damage, actor.HP, 0, "NULL", 100, 0, 0, actor.MemberType))
		if actor.HP > 0 {
			continue
		}
		actor.DeathActionTriggered = false
		if combatParameterInt(role.Parameters[2]) != 0 {
			actor.PendingBreak = true
			continue
		}
		// Immediate notification leaves 59948/drop ownership unchanged and
		// may repeat; a later ordinary kill can still release the reward.
		actor.DeathCount++
		actor.DiedTurn = engine.turn
		command := resultEnemyBreak
		if actor.Parent > 0 {
			command = resultPartsBreak
		}
		results = append(results, BattleResult{Command: command, Args: []int64{int64(actor.MemberType), 0, 0, 0, boolInt64(actor.DropResolved)}})
	}
	return results, nil
}

func nativeDestructDamage(currentHP int, role CombatSkillRole, level int) int {
	base := int32(int64(currentHP) * int64(int32(combatParameterInt(role.Parameters[0]))) / 100)
	levelSegment := int32(combatParameterInt(role.Parameters[1]) * maxInt(0, level))
	return maxInt(1, int(base+levelSegment))
}

func replaceRewriteEffect(effects []battleEffect, replacement battleEffect) ([]battleEffect, []battleEffect) {
	kept := make([]battleEffect, 0, len(effects)+1)
	removed := make([]battleEffect, 0, 1)
	for _, effect := range effects {
		if effect.Function == "REWRITE" && effect.ListType == replacement.ListType {
			removed = append(removed, effect)
			continue
		}
		kept = append(kept, effect)
	}
	return append(kept, replacement), removed
}

func appendRewriteReplacement(results []BattleResult, memberType int, removed []battleEffect) []BattleResult {
	for _, effect := range removed {
		_, release := naturalEffectReleaseResult(memberType, effect, battleBuffCodes["REWRITE"])
		results = append(results, release)
	}
	return results
}

func rewriteResult(memberType int, attribute string) BattleResult {
	return BattleResult{Command: resultRewrite, Args: []int64{int64(memberType), int64(combatAttributeCode(attribute))}}
}

func (engine *BattleEngine) executeEnemyAttack(actor *battleEnemy, selected int, role CombatSkillRole, roles []CombatSkillRole) ([]BattleResult, error) {
	actorStats := playerFromEnemy(actor)
	level := calibratedEnemySkillLevel(role.Function)
	power := attackExecutionPower(role, level, combatStatValue(actorStats, role.Parameters[5]))
	modifiers := collectEnemyAttackModifiers(roles, actorStats, 1)
	power += modifiers.damageIncrease
	power += attackRevengeBonus(actor.DamageTaken, actorStats, modifiers.revengeRate, modifiers.revengeParameter, modifiers.revengeCapRate)
	power += attackRevengeBonus(actor.TurnDamage, actorStats, modifiers.nowTurnRevengeRate, modifiers.nowTurnRevengeParameter, modifiers.nowTurnRevengeCapRate)
	hits := maxInt(1, combatParameterInt(role.Parameters[4]))
	targets := engine.enemyRolePlayerTargets(actor, selected, role)
	if len(targets) == 0 {
		return nil, nil
	}
	results := make([]BattleResult, 0, len(targets)*hits*2)
	for _, memberType := range targets {
		player := &engine.players[memberType-1]
		targetHP := player.HP
		pendingDamage := 0
		for hit := 0; hit < hits; hit++ {
			defense := player.Defense
			if strings.EqualFold(role.Parameters[8], "MAGIC") {
				defense = player.MDefense
			} else if strings.EqualFold(role.Parameters[8], "ALL") {
				defense += player.MDefense
			}
			defense = nativePiercedDefense(defense, modifiers.piercingRate)
			rate := playerAttributeRateWithEffects(player, role.Parameters[7])
			if modifiers.attrRateDownInvalid {
				rate = maxInt(100, rate)
			}
			critical, criticalMultiplier := engine.nativeCriticalOutcome(actor.Effects, combatParameterInt(role.Parameters[6]))
			attrDefenseRate, attrDefenseFixed := attributeDefenseAdjustment(player.Effects, role.Parameters[7])
			attributeAdjustedPower := weaknessAdjustedPower(power, player.Effects) * (1000 + attrDefenseRate) / 1000
			damage, attributeDifference := nativeAttributeAttackDamage(attributeAdjustedPower, rate, criticalMultiplier, defense, 0)
			damage = playerCoveringDamage(player, damage, role.Parameters[7], role.Parameters[8])
			// 927b0 applies COVERING after ordinary defense, then adds the
			// fixed attribute adjustment. That fixed armor is not cut again.
			damage = maxInt(1, damage+attrDefenseFixed)
			resolution := engine.resolveIncomingDamageEffects(&player.Effects, damage, player.HP, role.Parameters[7], role.Parameters[8])
			damage = resolution.Damage
			if resolution.BarrierTriggered && damage == 0 {
				// Shared 47028 clears the flag only when the barrier absorbs the hit.
				critical = 0
			}
			pendingDamage += damage
			recordPlayerDamage(player, damage)
			results = append(results,
				battleDamageResult(player.MemberType, role.RoleIndex, -damage, targetHP, attributeDifference, role.Parameters[7], rate, critical, 0, actor.MemberType),
			)
			results = append(results, damageEffectResults(player.MemberType, resolution)...)
			if resolution.Reflected > 0 && !attackIgnoresReflection(roles, role) {
				results = append(results, engine.reflectedDamageResults(actor.MemberType, player.MemberType, resolution.Reflected)...)
			}
			// 838d0 commits drain after each normal hit, before enchant and
			// target HP publication. The 61 belongs to the attack role.
			if damage > 0 && modifiers.drainRate > 0 && actor.HP > 0 {
				heal := damage * modifiers.drainRate / 100
				if modifiers.drainCap > 0 {
					heal = minInt(heal, modifiers.drainCap)
				}
				reported, hp := nativeHealCommit(actor.HP, actor.MaxHP, heal, actor.Effects, false)
				actor.HP = hp
				results = append(results, battleHealResult(actor.MemberType, role.RoleIndex, reported, actor.HP))
			}
			if enchantValue, enchantAttribute, active := activeEnchantEffect(actor.Effects); active {
				enchantRate := playerAttributeRateWithEffects(player, enchantAttribute)
				enchantDefenseRate, enchantDefenseFixed := attributeDefenseAdjustment(player.Effects, enchantAttribute)
				enchantPower := enchantValue * (1000 + enchantDefenseRate) / 1000
				enchantDamage, enchantDifference := nativeAttributeEnchantDamage(enchantPower, enchantRate, enchantDefenseFixed)
				enchantResolution := engine.resolveEnchantDamageEffects(&player.Effects, enchantDamage, player.HP, enchantAttribute)
				enchantDamage = enchantResolution.Damage
				pendingDamage += enchantDamage
				recordPlayerDamage(player, enchantDamage)
				results = append(results, battleDamageResult(player.MemberType, role.RoleIndex, -enchantDamage, targetHP, enchantDifference, enchantAttribute, enchantRate, 0, 1, actor.MemberType))
				results = append(results, damageEffectResults(player.MemberType, enchantResolution)...)
				// 838d0 reflects only the normal hit, before the enchant branch.
			}
		}
		player.HP = engine.playerHPAfterDamage(player, pendingDamage)
		results = append(results, BattleResult{Command: resultHP, Args: []int64{int64(player.MemberType), int64(player.MaxHP), int64(player.HP), 1}})
		results = append(results, engine.burstDamageGaugeResults(player, pendingDamage)...)
	}
	return results, nil
}

func (engine *BattleEngine) playerHPAfterDamage(player *battlePlayer, damage int) int {
	if damage <= 0 {
		return player.HP
	}
	return nativeHPCommit(player.HP, player.MaxHP, -damage, player.Effects)
}

// 7adb0 -> 5feb6 sweeps members after every role and deferred status has
// finished. Reversed party drain can kill allies other than the caster;
// later attack roles must still observe them at zero HP until this sweep.
func (engine *BattleEngine) resolvePlayerSkillGuts() []BattleResult {
	var results []BattleResult
	for i := range engine.players {
		results = append(results, resolvePlayerGuts(&engine.players[i])...)
	}
	return results
}

func resolvePlayerGuts(player *battlePlayer) []BattleResult {
	// 5feb6 explicitly excludes formally retired users, independently of HP.
	if player.GameOver {
		return nil
	}
	var results []BattleResult
	player.HP, results = resolveGuts(player.MemberType, player.MaxHP, player.HP, &player.Effects)
	return results
}

func resolveGuts(memberType int, maxHP int, hp int, effects *[]battleEffect) (int, []BattleResult) {
	if hp > 0 || effects == nil {
		return hp, nil
	}
	selected := -1
	for index, effect := range *effects {
		if effect.Function == "GUTS" && effect.Remaining > 0 && effect.Uses > 0 && effect.ListType >= 0 && effect.ListType <= 6 &&
			(selected < 0 || effect.ListType < (*effects)[selected].ListType) {
			selected = index
		}
	}
	if selected >= 0 {
		index := selected
		effect := &(*effects)[index]
		heal := maxInt(1, maxHP*maxInt(0, effect.Rate)/100)
		hp = minInt(maxHP, heal)
		effect.Uses--
		effect.Parameters[0] = effect.Uses
		results := []BattleResult{
			buffStatusEffectResult(memberType, battleBuffCodes["GUTS"]),
			battleHealResult(memberType, 0, heal, hp),
			{Command: 200, Args: []int64{int64(memberType), int64(battleBuffCodes["GUTS"]), int64(effect.Uses), 0}},
		}
		if effect.Uses == 0 {
			listType := effect.ListType
			results = append(results, BattleResult{Command: 72, Args: []int64{int64(memberType), int64(listType), int64(battleBuffCodes["GUTS"]), 0, 1}})
			kept := (*effects)[:0]
			for _, candidate := range *effects {
				if candidate.Function != "GUTS" || candidate.ListType != listType {
					kept = append(kept, candidate)
				}
			}
			*effects = kept
		}
		return hp, results
	}
	return hp, nil
}

type incomingDamageResolution struct {
	Damage                int
	Reflected             int
	BarrierTriggered      bool
	BarrierRemainingUses  int
	BarrierAttributeIndex int
	Released              []battleEffect
}

// resolveIncomingDamageEffects follows the CN native commit order: CHALICE
// DAMAGE_CUT2 modifies the incoming value before an attack barrier gets the
// first chance to consume the hit, and reflection only sees damage which the
// barrier did not absorb. Barrier Uses is independent from Remaining.
func (engine *BattleEngine) resolveIncomingDamageEffects(effects *[]battleEffect, damage int, currentHP int, attribute string, physics string) incomingDamageResolution {
	return engine.resolveDamageEffects(effects, damage, currentHP, attribute, physics, true)
}

func (engine *BattleEngine) resolveEnchantDamageEffects(effects *[]battleEffect, damage int, currentHP int, attribute string) incomingDamageResolution {
	// 82c85 processes enchant barriers without the ordinary hit's support cut.
	return engine.resolveDamageEffects(effects, damage, currentHP, attribute, "", false)
}

func (engine *BattleEngine) resolveDamageEffects(effects *[]battleEffect, damage int, currentHP int, attribute string, physics string, supportCut bool) incomingDamageResolution {
	resolution := incomingDamageResolution{Damage: maxInt(0, damage)}
	if resolution.Damage == 0 || effects == nil {
		return resolution
	}
	if supportCut {
		resolution.Damage = sphereDamageCut(*effects, attribute, physics, resolution.Damage)
	}

	for index := 0; index < len(*effects); index++ {
		effect := &(*effects)[index]
		if effect.Function != "ATTACK_BARRIER" && effect.Function != "ATTACK_BARRIER_APPOINT_ATTR" {
			continue
		}
		if effect.Remaining <= 0 || effect.Uses <= 0 || !damagePhysicsMatches(effect.DamageKind, physics) || !damageAttributeMatches(effect.Attribute, attribute) {
			continue
		}
		effect.Uses--
		resolution.BarrierTriggered = true
		resolution.BarrierRemainingUses = effect.Uses
		resolution.BarrierAttributeIndex = combatAttributeCode(effect.Attribute)
		if resolution.Damage <= effect.Value {
			resolution.Damage = 0
		}
		if effect.Uses == 0 {
			resolution.Released = append(resolution.Released, *effect)
			*effects = append((*effects)[:index], (*effects)[index+1:]...)
		}
		break
	}
	if resolution.Damage == 0 {
		return resolution
	}

	for index := 0; index < len(*effects); index++ {
		effect := &(*effects)[index]
		if effect.Function != "REFLECTION" || effect.Remaining <= 0 || effect.Rate <= 0 || !damagePhysicsMatches(effect.DamageKind, physics) {
			continue
		}
		// 838d0 adds reflected damage; it does not absorb the incoming hit or
		// consume duration. The retained coefficient is in ten-thousandths.
		resolution.Reflected = int(int64(resolution.Damage) * int64(effect.Rate) / 10000)
		break
	}
	return resolution
}

// 838d0 caps reflection at the attacker's pre-commit HP minus one. The 60
// carries that requested damage and post-commit HP, even when ENDURE clips it.
func (engine *BattleEngine) reflectedDamageResults(member int, source int, requested int) []BattleResult {
	var hp *int
	var maxHP int
	var effects []battleEffect
	if member >= 1 && member <= len(engine.players) {
		actor := &engine.players[member-1]
		hp, maxHP, effects = &actor.HP, actor.MaxHP, actor.Effects
	} else if member >= 5 && member < 5+engine.enemyCount {
		actor := &engine.enemies[member-5]
		hp, maxHP, effects = &actor.HP, actor.MaxHP, actor.Effects
	} else {
		return nil
	}
	damage := minInt(requested, maxInt(0, *hp-1))
	if damage <= 0 {
		return nil
	}
	*hp = nativeHPCommit(*hp, maxHP, -damage, effects)
	rows := []BattleResult{battleDamageResult(member, 0, -damage, *hp, 0, "NULL", 100, 0, 2, source)}
	if member < 5 {
		recordPlayerDamage(&engine.players[member-1], damage)
		return rows
	}
	actor := &engine.enemies[member-5]
	recordEnemyDamage(actor, damage, "MAGIC")
	recordEnemyAIDamage(actor, damage, 2)
	engine.recordSpecialEnemyDamage(member, source, damage)
	if actor.Parent > 0 && actor.Parent <= engine.enemyCount {
		parent := &engine.enemies[actor.Parent-1]
		if parent.HP > 0 {
			parentDamage := minInt(damage, parent.HP-1)
			parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, -parentDamage, parent.Effects)
			recordEnemyDamageTotals(parent, parentDamage)
			engine.recordSpecialEnemyDamage(parent.MemberType, source, damage)
			rows = append(rows, BattleResult{Command: resultHP, Args: []int64{int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 1}})
		}
	}
	return rows
}

func damagePhysicsMatches(filter string, physics string) bool {
	filter = strings.ToUpper(filter)
	physics = strings.ToUpper(physics)
	return filter == "" || filter == "ALL" || filter == "NULL" || physics == "" || physics == "ALL" || physics == "NULL" || filter == physics
}

func damageAttributeMatches(filter string, attribute string) bool {
	filter = strings.ToUpper(filter)
	if filter == "" || filter == "ALL" || filter == "NULL" {
		return true
	}
	if strings.EqualFold(filter, attribute) {
		return true
	}
	// Native FUN_000cec12 splits both decimal pair attributes and succeeds
	// when either component intersects. This covers LIGHT_DARK filters against
	// LIGHT, DARK and dual-attribute attacks without broadening to other attrs.
	for _, component := range splitCombatAttributes(filter) {
		if combatAttributeMatches(attribute, component) {
			return true
		}
	}
	return false
}

func playerCoveringDamage(player *battlePlayer, damage int, attribute string, physics string) int {
	if player == nil || damage <= 0 {
		return maxInt(0, damage)
	}
	remainingRate := 1000
	for _, effect := range player.Effects {
		if effect.Function != "COVERING" || effect.Remaining <= 0 ||
			!damageAttributeMatches(effect.Attribute, attribute) || !damagePhysicsMatches(effect.DamageKind, physics) {
			continue
		}
		remainingRate = remainingRate * maxInt(0, 1000-effect.Rate) / 1000
	}
	return maxInt(1, damage*remainingRate/1000)
}

func damageEffectResults(memberType int, resolution incomingDamageResolution) []BattleResult {
	results := make([]BattleResult, 0, len(resolution.Released)+1)
	if resolution.BarrierTriggered {
		// Managed ResultCmd 200 calls MemberData.setBuffCount with
		// [member,buff,count,managed-attribute]. Native FUN_00082c85 emits it for
		// every matching barrier hit, even when the damage exceeds the threshold.
		results = append(results, BattleResult{Command: 200, Args: []int64{
			int64(memberType), int64(battleBuffCodes["ATTACK_BARRIER"]),
			int64(resolution.BarrierRemainingUses), int64(resolution.BarrierAttributeIndex),
		}})
	}
	for _, effect := range resolution.Released {
		if code, exists := battleBuffCodes[effect.Function]; exists {
			_, result := naturalEffectReleaseResult(memberType, effect, code)
			results = append(results, result)
		}
	}
	return results
}

func (engine *BattleEngine) executeEnemyParameterLimit(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	level := calibratedEnemySkillLevel(role.Function)
	value := fixedBuffRoleValue(role, level, 1)
	if value <= 0 {
		return nil, nil
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	code := battleBuffCodes[role.Function]
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			ensurePlayerBaseParameters(player)
			if !adjustPlayerParameterLimit(player, role.Parameters[1], value) {
				return nil, fmt.Errorf("unsupported player parameter limit %q", role.Parameters[1])
			}
			player.Effects = append(player.Effects, battleEffect{
				Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value,
				Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
			})
			engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
			refreshAppliedPlayerParameter(player, role.Parameters[1], 0)
			results = append(results,
				battleParameterBuffResult(memberType, role, code),
				BattleResult{Command: resultBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
			)
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		ensureEnemyBaseParameters(target)
		if !adjustEnemyParameterLimit(target, role.Parameters[1], value) {
			return nil, fmt.Errorf("unsupported enemy parameter limit %q", role.Parameters[1])
		}
		target.Effects = append(target.Effects, battleEffect{
			Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value,
			Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
		})
		engine.recordAIStatusApplied(target.MemberType, target.Effects[len(target.Effects)-1])
		refreshAppliedEnemyParameter(target, role.Parameters[1], 0)
		results = append(results,
			battleParameterBuffResult(target.MemberType, role, code),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(target.MemberType, target.HP, target.MaxHP, target.Attack, target.Magic, target.Recovery, target.Defense, target.MDefense, target.LimitAttack, target.LimitMagic, target.LimitRecovery)},
		)
	}
	return results, nil
}

func adjustEnemyParameterLimit(enemy *battleEnemy, name string, amount int) bool {
	switch strings.ToUpper(name) {
	case "ATK":
		enemy.LimitAttack = maxInt(0, activeParameterLimit(enemy.LimitAttack)+amount)
	case "INT":
		enemy.LimitMagic = maxInt(0, activeParameterLimit(enemy.LimitMagic)+amount)
	case "MND":
		enemy.LimitRecovery = maxInt(0, activeParameterLimit(enemy.LimitRecovery)+amount)
	default:
		return false
	}
	return true
}

func (engine *BattleEngine) executeEnemyParameterRole(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	level := calibratedEnemySkillLevel(role.Function)
	value := fixedBuffRoleValue(role, level, 1)
	if strings.Contains(role.Function, "BY_SELF_PARAM") {
		value = selfScaledParameterValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[2]))
	}
	debuff := strings.HasPrefix(role.Function, "ATK_BREAK") || strings.HasPrefix(role.Function, "GUARD_BREAK")
	code := battleBuffCodes[role.Function]
	if !combatParameterSupported(role.Parameters[1]) {
		return nil, fmt.Errorf("unsupported battle parameter %q", role.Parameters[1])
	}
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			amount := value
			if strings.Contains(role.Function, "BY_TARGET_PARAM") {
				amount = targetScaledParameterValue(role, combatStatValue(player, role.Parameters[2]))
			}
			if strings.Contains(role.Function, "BY_NOW_TURN_DAMAGE") {
				amount = nowTurnDamageParameterValue(role, actor.TurnDamage, 0, 1)
			}
			if debuff {
				amount = -amount
			}
			ensurePlayerBaseParameters(player)
			kind := 1
			if debuff {
				kind = 2
			}
			player.Effects = append(player.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: absInt(amount), Delta: amount, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
			engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
			refreshAppliedPlayerParameter(player, role.Parameters[1], amount)
			results = append(results,
				battleParameterBuffResult(memberType, role, code),
				BattleResult{Command: resultBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
			)
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		amount := value
		if strings.Contains(role.Function, "BY_TARGET_PARAM") {
			amount = targetScaledParameterValue(role, combatEnemyStatValue(target, role.Parameters[2]))
		}
		if strings.Contains(role.Function, "BY_NOW_TURN_DAMAGE") {
			amount = nowTurnDamageParameterValue(role, actor.TurnDamage, 0, 1)
		}
		if debuff {
			amount = -amount
		}
		ensureEnemyBaseParameters(target)
		kind := 1
		if debuff {
			kind = 2
		}
		target.Effects = append(target.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: absInt(amount), Delta: amount, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(target.MemberType, target.Effects[len(target.Effects)-1])
		refreshAppliedEnemyParameter(target, role.Parameters[1], amount)
		results = append(results,
			battleParameterBuffResult(target.MemberType, role, code),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(target.MemberType, target.HP, target.MaxHP, target.Attack, target.Magic, target.Recovery, target.Defense, target.MDefense, target.LimitAttack, target.LimitMagic, target.LimitRecovery)},
		)
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyHeal(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	level := calibratedEnemySkillLevel(role.Function)
	value := fixedHealRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[4]))
	if role.Function == "HEAL_BY_SELF_PARAM" {
		value = selfScaledHealRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[0]))
	}
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			target := &engine.players[memberType-1]
			amount := value
			if role.Function == "HEAL_BY_TARGET_MAXHP" {
				// EnemyLevelupDataContainer assigns every ordinary enemy skill
				// level 1. Native FUN_0007adb0 passes that same object to the role
				// producer after projecting level 1 in ResultCmd50.
				amount = targetMaxHPHealRoleValue(role, level, target.MaxHP)
			}
			reported, hp := nativeHealCommit(target.HP, target.MaxHP, amount, target.Effects, true)
			target.HP = hp
			results = append(results, battleHealResult(target.MemberType, role.RoleIndex, reported, target.HP))
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		amount := value
		if role.Function == "HEAL_BY_TARGET_MAXHP" {
			amount = targetMaxHPHealRoleValue(role, level, target.MaxHP)
		}
		results = append(results, engine.commitDirectEnemyHeal(target, role.RoleIndex, amount)...)
	}
	return results, nil
}

// commitDirectEnemyHeal mirrors native FUN_000822a0. ResultCmd61 already
// carries the selected target's authoritative HP. When that target is a living
// enemy part, the same positive reported heal is also committed to its living
// parent, without evaluating the parent's HEAL_REVERSE effects a second time.
// Native exposes only that parent change as a ResultCmd3 row, with update flag 2.
func (engine *BattleEngine) commitDirectEnemyHeal(target *battleEnemy, roleIndex int, amount int) []BattleResult {
	reported, hp := nativeHealCommit(target.HP, target.MaxHP, amount, target.Effects, true)
	target.HP = hp
	results := []BattleResult{battleHealResult(target.MemberType, roleIndex, reported, target.HP)}
	if reported <= 0 || target.Parent <= 0 || target.Parent > engine.enemyCount {
		return results
	}
	parent := &engine.enemies[target.Parent-1]
	if parent.HP <= 0 {
		return results
	}
	parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, reported, parent.Effects)
	return append(results, BattleResult{Command: resultHP, Args: []int64{
		int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 2,
	}})
}

func (engine *BattleEngine) executeEnemyHPCut(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			delta, _ := nativeHPCut(player.HP, combatParameterInt(role.Parameters[0]))
			player.HP = nativeHPCommit(player.HP, player.MaxHP, delta, player.Effects)
			recordPlayerDamage(player, -delta)
			results = append(results, battleHPCutResult(memberType, role.RoleIndex, delta, player.HP))
			results = append(results, engine.burstDamageGaugeResults(player, -delta)...)
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		delta, _ := nativeHPCut(target.HP, combatParameterInt(role.Parameters[0]))
		target.HP = nativeHPCommit(target.HP, target.MaxHP, delta, target.Effects)
		recordEnemyDamageTotals(target, -delta)
		engine.recordSpecialEnemyDamage(target.MemberType, actor.MemberType, -delta)
		results = append(results, battleHPCutResult(target.MemberType, role.RoleIndex, delta, target.HP))
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyRevive(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 8)
	level := calibratedEnemySkillLevel(role.Function)
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, true) {
		target := &engine.enemies[index]
		if target.HP > 0 {
			continue
		}
		target.HP = reviveRoleValue(role, level, target.MaxHP)
		target.Broken = false
		target.DeathActionTriggered = false
		target.DiedTurn = 0
		results = append(results,
			BattleResult{Command: resultSkillRevive, Args: []int64{int64(target.MemberType), int64(role.RoleIndex), int64(target.HP), int64(target.HP)}},
		)
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyDOTValueUp(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers)
	level := calibratedEnemySkillLevel(role.Function)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			var projected []BattleResult
			engine.players[memberType-1].Effects, projected = applyDOTValueUp(engine.players[memberType-1].Effects, memberType, role, level, 1)
			results = append(results, projected...)
		}
		return results
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		var projected []BattleResult
		engine.enemies[index].Effects, projected = applyDOTValueUp(engine.enemies[index].Effects, engine.enemies[index].MemberType, role, level, 1)
		results = append(results, projected...)
	}
	return results
}

func (engine *BattleEngine) executeEnemyPersistentEffect(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	return engine.executeEnemyPersistentEffectWithListType(actor, selected, role, 0)
}

func (engine *BattleEngine) executeEnemyPersistentEffectWithListType(actor *battleEnemy, selected int, role CombatSkillRole, listType int) ([]BattleResult, error) {
	code, exists := battleBuffCodes[role.Function]
	if !exists {
		return nil, fmt.Errorf("persistent enemy combat function %q has no official buff code", role.Function)
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	kind := persistentEffectKind(role.Function)
	level := calibratedEnemySkillLevel(role.Function)
	value := enemyPersistentRoleValue(role, level, actor)
	effect := persistentBattleEffect(role, value, duration, kind, engine.turn, actor.MemberType, level)
	effect.ListType = listType
	engine.prepareRandomHandRole(role, &effect)
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			targetEffect := effect
			if role.Function == "COST_BLOCK" {
				results = append(results, engine.registerPlayerCostBlock(player, role, targetEffect)...)
				continue
			}
			if applied, handled := engine.registerGoodStatus(memberType, &player.Effects, role, targetEffect); handled {
				results = append(results, applied...)
				continue
			}
			if role.Function == "WEAKNESS" {
				results = append(results, engine.registerWeakness(memberType, role, targetEffect)...)
				continue
			}
			if role.Function == "STAN" && !engine.stunHits(player.Effects, 0, 0, role, level) {
				results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
				continue
			}
			if isCombatDOTFunction(role.Function) {
				if hasActiveCombatEffect(player.Effects, role.Function) {
					results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
					continue
				}
				// Player bad-status base resistance is currently zero, but the
				// native target consumer still consumes this independent draw.
				if engine.rollPercent10000(0) ||
					!engine.persistentEffectHits(role, level) ||
					engine.effectsResistDOT(player.Effects, role.Function) {
					results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
					continue
				}
				targetEffect.Value = registeredDOTValue(targetEffect.Value, 100, 0)
			}
			if engine.applyPlayerPersistentEffect(player, role, &targetEffect) {
				results = append(results, battlePersistentResult(memberType, role, code, targetEffect))
			} else {
				// Retain the consumer failure; the complete skill projects it
				// only if this role has no successful status targets.
				results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
			}
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		enemy := &engine.enemies[index]
		targetEffect := effect
		if applied, handled := engine.registerGoodStatus(enemy.MemberType, &enemy.Effects, role, targetEffect); handled {
			results = append(results, applied...)
			continue
		}
		if role.Function == "WEAKNESS" {
			results = append(results, engine.registerWeakness(enemy.MemberType, role, targetEffect)...)
			continue
		}
		if role.Function == "STAN" && !engine.stunHits(enemy.Effects, enemy.StatusCooldown[1], enemy.Level.StatusResistances[1], role, level) {
			results = append(results, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
			continue
		}
		if isCombatDOTFunction(role.Function) {
			if hasActiveCombatEffect(enemy.Effects, role.Function) ||
				engine.enemyResistsDOTStatus(enemy, role.Function) ||
				!engine.persistentEffectHits(role, level) ||
				engine.effectsResistDOT(enemy.Effects, role.Function) {
				results = append(results, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
				continue
			}
			targetEffect = engine.dotEffectForEnemy(targetEffect, enemy)
		}
		enemy.Effects = append(enemy.Effects, targetEffect)
		engine.recordAIStatusApplied(enemy.MemberType, targetEffect)
		results = append(results, battlePersistentResult(enemy.MemberType, role, code, targetEffect))
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyDealChange(actor *battleEnemy, selected int, role CombatSkillRole, penalty bool) ([]BattleResult, error) {
	code := battleBuffCodes[role.Function]
	value := absInt(combatParameterInt(role.Parameters[0]))
	duration := 2
	if role.Function == "DEAL_PENALTY_TURN_APPOINT" {
		duration = maxInt(1, value)
		value = absInt(combatParameterInt(role.Parameters[1]))
	}
	if value == 0 {
		return nil, nil
	}
	kind := 1
	if penalty {
		kind = 2
	}
	results := make([]BattleResult, 0, maxRoomMembers)
	for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
		player := &engine.players[memberType-1]
		effect := battleEffect{Function: role.Function, Value: value, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex}
		if !applyPlayerDrawEffect(player, effect, code) {
			continue
		}
		results = append(results, battleBuffResultWithParameters(memberType, role.RoleIndex, code, 0, 1, value, 0, 0, 0))
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyRelease(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	level := calibratedEnemySkillLevel(role.Function)
	results := make([]BattleResult, 0, 8)
	failed := make([]BattleResult, 0, 8)
	for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
		player := &engine.players[memberType-1]
		_, childRows := engine.releasePlayerEffectsWithRows(player, role, level)
		if len(childRows) > 0 {
			results = append(results, childRows...)
		} else {
			failed = append(failed, battleBuffReleaseFailedResult(memberType, role.RoleIndex))
		}
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		_, childRows := engine.releaseEnemyEffectsWithRows(target, role, level)
		if len(childRows) > 0 {
			results = append(results, childRows...)
		} else {
			failed = append(failed, battleBuffReleaseFailedResult(target.MemberType, role.RoleIndex))
		}
	}
	if len(results) == 0 {
		return failed, nil
	}
	return results, nil
}

func (engine *BattleEngine) enemyRolePlayerTargets(actor *battleEnemy, selected int, role CombatSkillRole) (targets []int) {
	defer func() { targets = engine.filterRolePlayers(targets, actor.MemberType, role) }()
	if targets, ok := engine.skillRoleTargets(actor.MemberType, role, true); ok {
		return targets
	}
	kind := role.Target
	if !engine.enemyRoleTargetsPlayers(kind, selected) {
		return nil
	}
	if kind == "USER_ALL" || kind == "FRIEND_ALL" {
		result := make([]int, 0, maxRoomMembers)
		for index := range engine.players {
			if engine.players[index].HP > 0 {
				result = append(result, engine.players[index].MemberType)
			}
		}
		return result
	}
	if combatArthurType(kind) != 0 {
		selected = engine.memberForJob(kind)
	}
	if selected >= 1 && selected <= maxRoomMembers {
		player := &engine.players[selected-1]
		// 79ee0 preserves the action's explicit target after a lethal earlier
		// role. Both subsequent attacks and parameter debuffs still consume it.
		if player.HP > 0 || player.MemberType != 0 && (kind == "SELECT" || kind == "USER_ONE" || kind == "FRIEND_ONE") {
			return []int{selected}
		}
	}
	return nil
}

func (engine *BattleEngine) enemyRoleTargetsPlayers(kind string, selected int) bool {
	// Enemy skill-role SELF is the acting enemy. It cannot reuse the player
	// skill target classifier, where SELF means the acting Arthur.
	if kind == "SELF" {
		return false
	}
	// SELECT inherits the concrete target chosen for the enemy action. Resolve
	// it before combatTargetIsPlayer: that shared helper treats SELECT as a
	// player-side selectable target, which previously discarded every enemy
	// member/part selection (member types 5+).
	if kind == "SELECT" {
		return selected >= 1 && selected <= maxRoomMembers
	}
	if combatTargetIsPlayer(kind) || kind == "USER_ALL" || kind == "USER_ONE" {
		return true
	}
	return false
}

func (engine *BattleEngine) enemyRoleEnemyTargets(actor *battleEnemy, selected int, role CombatSkillRole, includeDead bool) (targets []int) {
	// 8b660 filters source identity AFTER target resolution. Do not reroll or
	// substitute another target when the chosen member is excluded.
	defer func() { targets = engine.filterRoleEnemies(targets, actor.MemberType, role) }()
	if targets, ok := engine.skillRoleTargets(actor.MemberType, role, false); ok {
		return targets
	}
	kind := role.Target
	valid := func(index int) bool {
		return index >= 0 && index < engine.enemyCount && (includeDead || engine.enemies[index].HP > 0)
	}
	if kind == "ENEMY_ALL" || kind == "FRIEND_ALL" || kind == "DEAD_ENEMY_ALL" {
		result := make([]int, 0, engine.enemyCount)
		for index := 0; index < engine.enemyCount; index++ {
			if valid(index) && (kind != "DEAD_ENEMY_ALL" || engine.enemies[index].HP <= 0) {
				result = append(result, index)
			}
		}
		return result
	}
	if kind == "SELF" {
		selected = actor.MemberType
	}
	if selected >= 5 && valid(selected-5) {
		return []int{selected - 5}
	}
	if kind == "DEAD_ENEMY_ONE" {
		for index := 0; index < engine.enemyCount; index++ {
			if engine.enemies[index].HP <= 0 {
				return []int{index}
			}
		}
	}
	return nil
}

func enemyPersistentRoleValue(role CombatSkillRole, level int, actor *battleEnemy) int {
	switch role.Function {
	case "REGENERATE_FIXED":
		return fixedRegenerateRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[5]))
	case "REGENERATE_BY_SELF_PARAM":
		return combatEnemyStatValue(actor, role.Parameters[1])*combatParameterInt(role.Parameters[2])/1000 + combatParameterInt(role.Parameters[3])
	case "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC":
		_, _, _, value := dotRoleValues(role, level, 1, combatEnemyStatValue(actor, role.Parameters[7]))
		return value
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		_, fixed := attributeDefenseRoleValues(role, 0, 1)
		return fixed
	case "CARD_TRAP_DAMAGE":
		return cardTrapDamageRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[7]))
	case "CRITICAL_UP", "CRITICAL_DOWN", "WEAKNESS":
		return retainedRateRoleValue(role, level)
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		return attackBarrierRoleValue(role, level)
	case "ENCHANT":
		return combatParameterInt(role.Parameters[2]) * combatParameterInt(role.Parameters[1]) / 1000
	default:
		return combatParameterInt(role.Parameters[1])
	}
}

func combatEnemyStatValue(enemy *battleEnemy, name string) int {
	switch strings.ToUpper(name) {
	case "HP":
		return enemy.HP
	case "MAX_HP":
		return enemy.MaxHP
	case "ATK":
		return enemy.Attack
	case "INT":
		return enemy.Magic
	case "MND":
		return enemy.Recovery
	case "DEF":
		return enemy.Defense
	case "MDEF":
		return enemy.MDefense
	default:
		return 0
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (engine *BattleEngine) playerHasEffect(player *battlePlayer, function string) bool {
	return combatEffectCount(player.Effects, function, "") > 0
}

func (engine *BattleEngine) playerEffectValue(player *battlePlayer, function string) int {
	value := 0
	for _, effect := range player.Effects {
		if effect.Function == function {
			value += maxInt(0, effect.Value)
		}
	}
	return value
}

func (engine *BattleEngine) enemyActionHasDefinedRole(skillID int) bool {
	if skillID <= 0 {
		return false
	}
	for _, skill := range engine.catalog.EnemySkills[skillID] {
		if combatRolesDefined(engine.catalog.EnemySkillRoles[skill.FunctionID]) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) enemySkillBase(skillID int) (CombatSkillDefinition, []CombatSkillRole, bool) {
	variants := engine.catalog.EnemySkills[skillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, false
	}
	// The first row is the same fallback used by the managed SkillDataContainer
	// before an extension condition selects a higher-priority variant. Role rows
	// are owned by FunctionID, not necessarily by the outer skill ID.
	skill := variants[0]
	roles := engine.catalog.EnemySkillRoles[skill.FunctionID]
	return skill, roles, true
}

func (engine *BattleEngine) selectEnemySkillBranch(actor *battleEnemy, skillID int, selectedTarget int) (CombatSkillDefinition, []CombatSkillRole, bool) {
	skill, roles, _, matched := engine.selectEnemySkillBranchWithIndex(actor, skillID, selectedTarget)
	return skill, roles, matched
}

func (engine *BattleEngine) selectEnemySkillBranchWithIndex(actor *battleEnemy, skillID int, selectedTarget int) (CombatSkillDefinition, []CombatSkillRole, int, bool) {
	return engine.selectEnemySkillBranchMode(actor, skillID, selectedTarget, true)
}

func (engine *BattleEngine) selectEnemySkillBranchMode(actor *battleEnemy, skillID int, selectedTarget int, allowRandom bool) (CombatSkillDefinition, []CombatSkillRole, int, bool) {
	variants := engine.catalog.EnemySkills[skillID]
	var selected CombatSkillDefinition
	selectedIndex := 0
	matched := false
	for index, variant := range variants {
		if !allowRandom && (variant.BranchCondition == "RANDOM" || variant.BranchCondition2 == "RANDOM") {
			continue
		}
		if !engine.enemyBranchConditionSatisfied(actor, selectedTarget, variant.Target, variant.BranchCondition, variant.BranchParameters) ||
			!engine.enemyBranchConditionSatisfied(actor, selectedTarget, variant.Target, variant.BranchCondition2, variant.BranchParameters2) {
			continue
		}
		// Native FUN_000d7578 replaces the selected slot only on a strictly
		// greater priority. Equal-priority rows therefore keep CSV order.
		if !matched || variant.BranchPriority > selected.BranchPriority {
			selected = variant
			selectedIndex = index
			matched = true
		}
	}
	if !matched {
		return CombatSkillDefinition{}, nil, 0, false
	}
	return selected, engine.catalog.EnemySkillRoles[selected.FunctionID], selectedIndex, true
}

func (engine *BattleEngine) enemyBranchConditionSatisfied(actor *battleEnemy, selectedTarget int, _ string, condition string, parameters [5]string) bool {
	condition = strings.ToUpper(strings.TrimSpace(condition))
	if condition == "" || condition == "NONE" {
		return true
	}
	parameter := func(index int) string { return strings.ToUpper(strings.TrimSpace(parameters[index])) }
	switch condition {
	case "RANDOM":
		// FUN_000d3f50 uses xor128()%10000 against percent*100.
		return int(engine.rng.next()%10000) < combatParameterInt(parameter(0))*100
	case "TURN":
		return combatValueInRange(engine.turn, parameter(0), parameter(1))
	case "SELF_HP_PER":
		return combatValueInRange(actor.HP*100/maxInt(1, actor.MaxHP), parameter(0), parameter(1))
	case "SELF_HP_FIXED":
		return combatValueInRange(actor.HP, parameter(0), parameter(1))
	case "TARGET_HP_PER":
		hp, maxHP, ok := engine.battleMemberHP(selectedTarget)
		return ok && combatValueInRange(maxInt(0, hp)*100/maxInt(1, maxHP), parameter(0), parameter(1))
	case "TARGET_ATTR":
		return engine.branchTargetAttributeMatches(selectedTarget, parameters)
	case "SELF_ENEMY_AI_FLAG":
		first := combatParameterInt(parameter(0))
		second := combatParameterInt(parameter(1))
		return actor.hasAIFlag(first) || actor.hasAIFlag(second)
	case "SELF_ENEMY_AI_FLAG_AND":
		first := combatParameterInt(parameter(0))
		second := combatParameterInt(parameter(1))
		return actor.hasAIFlag(first) && actor.hasAIFlag(second)
	case "SELF_ENEMY_AI_VAR":
		return enemyAIVariableInRange(actor, parameters[0], parameters[1], parameters[2], true)
	case "FRIEND_PLAY_NUM":
		count := 0
		for _, played := range engine.turnStats.PlayedByUser {
			count += played
		}
		return combatValueInRange(count, parameter(0), parameter(1))
	case "SELF_BUFF", "SELF_DEBUFF":
		return branchHasStatus(actor.Effects, condition == "SELF_BUFF", parameters)
	case "TARGET_BUFF", "TARGET_DEBUFF":
		return branchHasStatus(engine.battleMemberEffects(selectedTarget), condition == "TARGET_BUFF", parameters)
	case "USER_SIDE_BUFF", "USER_SIDE_DEBUFF":
		return engine.branchSideHasStatus(true, condition == "USER_SIDE_BUFF", parameters)
	case "ENEMY_SIDE_DEBUFF":
		return engine.branchSideHasStatus(false, false, parameters)
	case "TARGET_DEBUFF_KIND_NUM":
		_, _, ok := engine.battleMemberHP(selectedTarget)
		return ok && combatValueInRange(branchDebuffKindCount(engine.battleMemberEffects(selectedTarget)), parameter(0), parameter(1))
	case "USER_SIDE_BLESS":
		// d2a0a tests d2976 for each living user. It is ANY user's filtered
		// BLESS count in range, not a party-wide sum including CURSE/KO.
		for index := range engine.players {
			player := &engine.players[index]
			if player.HP > 0 && combatValueInRange(playerAppendCardCount(player, 22, parameter(0)), parameter(1), parameter(2)) {
				return true
			}
		}
		return false
	case "SELF_DAMAGE":
		damage := actor.TurnDamage
		switch parameter(0) {
		case "PHYSICS":
			damage = actor.TurnPhysical
		case "MAGIC":
			damage = actor.TurnMagic
		}
		return combatValueInRange(damage, parameter(2), parameter(3))
	case "ENEMY_DEAD_NOW_TURN":
		memberType := combatEnemyMemberType(parameter(0))
		return memberType >= 5 && memberType < 5+engine.enemyCount && engine.enemies[memberType-5].DiedTurn == engine.turn
	default:
		return false
	}
}

func enemyBranchConditionSupported(condition string) bool {
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "", "NONE", "RANDOM", "TURN", "SELF_HP_PER", "SELF_HP_FIXED", "TARGET_HP_PER", "TARGET_ATTR",
		"SELF_ENEMY_AI_FLAG", "SELF_ENEMY_AI_FLAG_AND", "SELF_ENEMY_AI_VAR", "FRIEND_PLAY_NUM", "SELF_BUFF", "SELF_DEBUFF",
		"TARGET_BUFF", "TARGET_DEBUFF", "USER_SIDE_BUFF", "USER_SIDE_DEBUFF", "ENEMY_SIDE_DEBUFF",
		"TARGET_DEBUFF_KIND_NUM", "USER_SIDE_BLESS", "SELF_DAMAGE", "ENEMY_DEAD_NOW_TURN":
		return true
	default:
		return false
	}
}

func playerBranchConditionSupported(condition string) bool {
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "", "NONE", "DECK_COMBO_COUNT", "TARGET_ATTR", "TARGET_DEBUFF", "RANDOM",
		"SELF_OTHER_PLAY_ATTR", "SELF_OTHER_PLAY_SKILL_KIND", "SELF_OTHER_PLAY_RARITY", "SELF_HP_PER",
		"TURN", "FRIEND_PLAY_NUM", "FRIEND_PLAY_MOST_LOW_COST", "USER_SIDE_DEBUFF", "ENEMY_SIDE_DEBUFF",
		"FRIEND_PLAY_TAG", "SELF_MAIN_DECK_ATTR", "SELF_MAIN_DECK_SKILL_KIND", "SELF_BUFF", "TARGET_BUFF",
		"FRIEND_HP_PER", "SELF_BLESS":
		return true
	default:
		return false
	}
}
