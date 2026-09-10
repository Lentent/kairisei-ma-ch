package multiplayer

import (
	"errors"
	"fmt"
	"strings"
)

type battleAction struct {
	automatic    bool
	memberType   int
	cardID       int
	cardType     int
	sphereSlot   int
	cardLevel    int
	target       int
	branchIndex  int
	buffListType int
	// Scratch state shared only while executing one Burst/passive role set.
	// Native 7ed76 installs it and 7ea92 consumes the first matching segment.
	skillBonus *battleSkillBonus
	skill      CombatSkillDefinition
	roles      []CombatSkillRole
	// Card/SphrData call skills are the independent APPEND_CARD_BLESS payload.
	// Never store and replay the BLESS registration skill itself.
	callSkill CombatSkillDefinition
	callRoles []CombatSkillRole
}

func (engine *BattleEngine) UserAttack() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseUser {
		return nil, errors.New("combat user attack phase is unavailable")
	}
	for _, player := range engine.players {
		if player.HP > 0 {
			if _, submitted := engine.selectedPlays[player.MemberType]; !submitted {
				return nil, errors.New("combat user attack selection is incomplete")
			}
		}
	}
	actions, err := engine.selectedBattleActions()
	if err != nil {
		return nil, err
	}
	return engine.executeUserActions(actions)
}

// The selection snapshot is also consumed by native 3d680's CARD_UPDATE
// preview immediately after each commit, before UserAttack starts.
func (engine *BattleEngine) selectedBattleActions() ([]battleAction, error) {
	actions := make([]battleAction, 0, maxRoomMembers*2)
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		player := &engine.players[memberType-1]
		if player.HP <= 0 {
			continue
		}
		submission := engine.selectedPlays[memberType]
		for selectionIndex, cardType := range submission.CardTypes {
			if cardType == 0 {
				continue
			}
			card, found := player.cardInHand(cardType)
			if !found {
				return nil, fmt.Errorf("member %d selected card %d left its hand", memberType, cardType)
			}
			skill, roles, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
			if err != nil {
				return nil, err
			}
			callSkill, callRoles, err := engine.catalog.CardCallSkill(card.CardID)
			if err != nil {
				return nil, err
			}
			actions = append(actions, battleAction{automatic: submission.Automatic, memberType: memberType, cardID: card.CardID, cardType: cardType, cardLevel: card.Level, target: submission.Targets[selectionIndex], skill: skill, roles: roles, callSkill: callSkill, callRoles: callRoles})
		}
		if submission.SphereSlot != 0 {
			action, err := engine.validateSphereSubmission(memberType, submission)
			if err != nil {
				return nil, err
			}
			action.automatic = submission.Automatic
			actions = append(actions, action)
		}
	}
	return actions, nil
}

func (engine *BattleEngine) executeUserActions(actions []battleAction) ([]BattleResult, error) {
	chainCounts := battleActionChainCounts(actions)
	engine.turnActions = append([]battleAction(nil), actions...)
	for _, action := range actions {
		if action.cardType == 0 {
			continue
		}
		engine.turnStats.PlayedByUser[action.memberType-1]++
	}
	actions, held := engine.partitionPlayerActions(actions, chainCounts)
	engine.sortPlayerActions(actions)
	results, err := engine.chalicePlayableResults()
	if err != nil {
		return nil, err
	}
	results = append(results, engine.burstSelectionGaugeResults(engine.turnActions)...)
	for _, action := range actions {
		rows, err := engine.executePlayerAction(action, chainCounts)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
		if engine.endType != 0 {
			break
		}
	}
	// Native battle5_api_user_attack emits ATTACK_PARTITION after the ordered
	// card/sphere action list and before appended CURSE/BLESS execution. It is a
	// zero-argument managed single-row delimiter, not a burst-only effect.
	if engine.endType == 0 {
		results = appendAttackPartition(results)
	}
	results = append(results, engine.storeCardHolds(held)...)
	// Native battle5_api_user_attack calls the complete tail even when an
	// ordinary card already set the GameMaster end flag. Each callee owns its
	// own terminal gate: append-card execution is suppressed but cleaned up,
	// selected-card traps still run, and retained enemy DOT returns immediately.
	// The fixed order is BLESS -> card trap -> enemy DOT -> CURSE.
	emitTail := engine.endType == 0
	holdResults, err := engine.executeBlessHolds(22)
	if err != nil {
		return nil, err
	}
	if emitTail {
		results = append(results, holdResults...)
	}
	emitTail = engine.endType == 0
	trapResults := engine.triggerPlayedCardTraps()
	if emitTail {
		results = append(results, trapResults...)
	}
	emitTail = engine.endType == 0
	dotResults, err := engine.tickEnemyDOTEffects()
	if err != nil {
		return nil, err
	}
	if emitTail {
		results = append(results, dotResults...)
	}
	emitTail = engine.endType == 0
	curseResults, err := engine.executeBlessHolds(21)
	if err != nil {
		return nil, err
	}
	if emitTail {
		results = append(results, curseResults...)
	}
	// 64120 -> a7cd0 discards the registered selection only after BLESS,
	// card traps, enemy DOT and CURSE. Until then the cards remain visible to
	// display refreshes and hand/selection conditions, even after execution.
	for _, action := range engine.turnActions {
		if action.cardType > 0 && action.sphereSlot == 0 {
			engine.removeCardFromHand(action.memberType, action.cardType)
		}
	}
	if engine.endType == 0 && engine.enemyCount > 0 && engine.enemies[0].HP <= 0 {
		engine.endType = 1
	}
	return engine.settleActionPhase(results, battlePhaseUserAttack, true), nil
}

// 3d342 executes one ordinary card or sphere, also reused by 664ee holds.
func (engine *BattleEngine) executePlayerAction(action battleAction, chainCounts map[string]int) ([]BattleResult, error) {
	var results []BattleResult
	// FUN_0003d342 gates both ordinary cards and spheres on the actor's
	// current HP. A sphere selected before an earlier action kills its
	// owner must not execute or consume a use afterwards.
	if engine.players[action.memberType-1].HP <= 0 {
		return nil, nil
	}
	engine.retargetPlayerAction(&action)
	// Native sorts the base skills (FUN_0003ae10), then selects a branch
	// inside each execution (FUN_0007adb0 -> FUN_000d7578). In particular,
	// BUFF_EXEC/SELF_BUFF/HP conditions must observe earlier actions.
	branch, branchIndex := engine.selectCombatSkillBranchWithIndex(action, engine.turnActions, chainCounts)
	roles := engine.catalog.PlayerSkillRoles[branch.FunctionID]
	if len(roles) == 0 {
		return nil, fmt.Errorf("branched player skill role %d is unavailable", branch.FunctionID)
	}
	// SkillData owns the outer target; extend records contain roles and
	// conditions only. 7adb0 uses the base target for both 79ee0 and 51.
	branch.Target = action.skill.Target
	action.skill, action.branchIndex = branch, branchIndex
	// Burst modifiers are per play, never writes to the shared catalog.
	action.roles = append([]CombatSkillRole(nil), roles...)
	if err := engine.attachBurstCardModifiers(&action); err != nil {
		return nil, err
	}
	if action.sphereSlot != 0 {
		sphereResults, err := engine.executeSphereAction(action, 0)
		if err != nil {
			return nil, err
		}
		return sphereResults, nil
	}
	chainCount := maxCombatChain(action.skill.Attribute, chainCounts)
	cardSkillResult, err := engine.playerCardSkillResult(action, chainCount)
	if err != nil {
		return nil, err
	}
	results = append(results, cardSkillResult)
	engine.recordExecutedPlayerSkill(action, chainCount)
	skillResults, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill.Target, action.roles, func(role CombatSkillRole) ([]BattleResult, error) {
		return engine.executePlayerRole(action, role, chainCount)
	})
	if err != nil {
		return nil, err
	}
	results = append(results, skillResults...)
	engine.nativeSkillSerial++
	displayResults, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	// Native FUN_0003d4xx calls FUN_00057cf0 once after the complete card
	// role set. Value-derived attack/heal hate is added by the role paths.
	engine.addPlayerHate(action.memberType, int64(maxInt(0, action.skill.HateRatio)))
	if engine.enemies[0].HP <= 0 {
		engine.endType = 1
	}
	return results, nil
}

// 7adb0 modes 1 (card) and 5 (sphere) call 792cc only after a skill starts.
// 570ef keeps the highest actual Chain, while 57428 records the executed kind.
// Mode 7 (append/CALL) deliberately does not contribute to either AI trigger.
func (engine *BattleEngine) recordExecutedPlayerSkill(action battleAction, chain int) {
	engine.turnStats.MaxChain = maxInt(engine.turnStats.MaxChain, chain)
	kinds := engine.turnStats.KindsByUser[action.memberType-1]
	if kinds == nil {
		kinds = make(map[string]int)
		engine.turnStats.KindsByUser[action.memberType-1] = kinds
	}
	kinds[strings.ToUpper(action.skill.Kind)]++
}

// FUN_0003c8ea uses the right-to-left bubble pass in FUN_000c6e10, not a
// standard stable sort. Equal-priority actions put manual before automatic
// players (FUN_00040908 flag), then FUN_0003ae10 consumes a random low bit
// for each comparison of different actors. Changing the algorithm changes
// both the order and the RNG state used by subsequent damage/target rolls.
func (engine *BattleEngine) sortPlayerActions(actions []battleAction) {
	for boundary := 0; boundary < len(actions)-1; boundary++ {
		for index := len(actions) - 2; index >= boundary; index-- {
			left, right := actions[index], actions[index+1]
			swap := left.skill.PriorityPVE > right.skill.PriorityPVE
			if left.skill.PriorityPVE == right.skill.PriorityPVE {
				switch {
				case left.automatic != right.automatic:
					swap = left.automatic
				case left.memberType != right.memberType:
					swap = engine.rng.next()&1 != 0
				}
			}
			if swap {
				actions[index], actions[index+1] = right, left
			}
		}
	}
}

// FUN_0003d342 -> FUN_0003d038 reselects a dead target before branch selection
// and the direction header. Earlier cards can destroy the selected part;
// rejecting that later action would leave the room in a partially applied turn.
func (engine *BattleEngine) retargetPlayerAction(action *battleAction) {
	if action.target == 0 {
		return
	}
	hp, _, exists := engine.battleMemberHP(action.target)
	if exists && hp > 0 {
		return
	}
	var candidates []int
	switch action.skill.Target {
	case "USER_ONE":
		for index := range engine.players {
			if engine.players[index].HP > 0 {
				candidates = append(candidates, index+1)
			}
		}
	case "ENEMY_ONE":
		for index := 0; index < engine.enemyCount; index++ {
			if engine.enemies[index].HP > 0 {
				candidates = append(candidates, index+5)
			}
		}
	case "USER_ALL", "ENEMY_ALL":
		action.target = 0
		return
	default:
		return
	}
	action.target = 0
	if len(candidates) > 0 {
		// Native consumes a roll even when only one candidate remains.
		action.target = candidates[int(engine.rng.next()%uint32(len(candidates)))]
	}
}

func appendAttackPartition(results []BattleResult) []BattleResult {
	return append(results, BattleResult{Command: resultAttackPartition})
}

// settleActionPhase mirrors the shared terminal work performed after the
// native action API has completed its own ordered subcalls. UserAttack first
// projects FUN_00064120's ResultCmd1(turn, user-attack-complete=1); chalice
// user execution reaches FUN_00065010 directly and therefore omits that row.
// FUN_00065010 only writes a player-loss end type when no earlier win/timeout
// state exists, so an enemy DOT victory wins a simultaneous all-player KO.
func (engine *BattleEngine) settleActionPhase(results []BattleResult, nextPhase battlePhase, emitUserAttackTurn bool) []BattleResult {
	if nextPhase == battlePhaseUserAttack {
		engine.resumeSide = 1
	} else if nextPhase == battlePhaseEnemy {
		engine.resumeSide = 0
	}
	if emitUserAttackTurn && engine.endType == 0 {
		results = append(results, BattleResult{Command: resultTurn, Args: []int64{int64(engine.turn), 1}})
	}
	results = engine.settlePlayerDeaths(results)
	engine.phase = nextPhase
	if engine.endType != 0 {
		results = append(results, battleEndResult(engine.endType))
		engine.phase = battlePhaseEnded
		results = engine.appendTerminalBuffCleanup(results)
	}
	return results
}

func (engine *BattleEngine) executePlayerRole(action battleAction, role CombatSkillRole, chainCount int) (results []BattleResult, err error) {
	role.SourceSkillID = action.skill.ID
	defer func() {
		if err == nil {
			results = engine.projectRoleBuffParameters(results)
		}
	}()
	// Successful status consumers record BUFF_EXEC on the actual caster.
	// ResultCmd presence is not evidence that a good-status registration won.
	role.Target = resolvedPlayerRoleTarget(action, role.Target)
	switch role.Function {
	case "ATTACK_AA":
		return engine.executePlayerAttack(action, role, chainCount)
	case "DEF_UP_FIXED":
		return engine.executeFixedDefense(action, role, chainCount)
	case "ATK_UP_FIXED":
		return engine.executeFixedParameter(action, role, chainCount)
	case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM":
		return engine.executeSelfScaledParameter(action, role, chainCount)
	case "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM":
		return engine.executeEnemyParameterDebuff(action, role, chainCount)
	case "HEAL_FIXED":
		return engine.executeFixedHeal(action, role, chainCount)
	case "HEAL_BY_SELF_PARAM":
		return engine.executeSelfScaledHeal(action, role, chainCount)
	case "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM", "DEBUFF_RELEASE", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_OLD",
		"BUFF_RELEASE", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_OLD":
		return engine.executeRelease(action, role)
	case "ATK_OP_DRAIN", "ATK_OP_DRAIN_ALL", "ATK_OP_REVENGE", "ATK_OP_PIERCING", "ATK_OP_DAMAGE_INCREASE", "ATK_OP_ATTR_RATE_DOWN_INVALID":
		// Attack operators are collected once by executePlayerAttack so order
		// inside a skill-role row set cannot change the arithmetic.
		return nil, nil
	case "DAMAGE_BOOST_ORDER_TRIBAL", "ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL":
		// Already attached to this selected card by BurstSkillExec. The
		// attack/parameter consumers read them; they are not new card actions.
		return nil, nil
	case "DEAL_BONUS":
		return engine.executeDealChange(action, role, false)
	case "PARAM_LIMIT_BREAK_FIXED":
		return engine.executePlayerParameterLimit(action, role, chainCount)
	case "REGENERATE_FIXED", "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC", "ENCHANT", "ATTR_DEF_DOWN", "ATTR_DEF_UP", "CRITICAL_UP", "CRITICAL_DAMAGE_BOOST", "WEAKNESS", "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN", "REFLECTION", "ENDURE", "COVERING", "CARD_SEAL_REGIST", "DARKNESS_REGIST", "GUTS", "STAN", "COST_BLOCK", "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR", "ATTR_SEE", "CARD_TRAP_DAMAGE", "DARKNESS_RANDOM":
		return engine.executePersistentEffect(action, role, action.cardLevel, chainCount)
	case "PARAM_UP_SKILL_BONUS", "LIMIT_BREAK_BONUS":
		if action.skillBonus != nil {
			*action.skillBonus = battleSkillBonus{active: true, parameter: role.Parameters[0],
				limit: role.Function == "LIMIT_BREAK_BONUS", attribute: role.Parameters[4],
				tag:     combatParameterInt(role.Parameters[5]),
				percent: int32(combatParameterInt(role.Parameters[1])),
				fixed:   int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(action.cardLevel)}
		}
		return nil, nil
	case "BLESS":
		if action.cardType == 22 {
			// Native FUN_000a275b suppresses recursive hold creation when the
			// currently executed card type is APPEND_CARD_BLESS.
			return nil, nil
		}
		return engine.executePlayerBless(action, role, chainCount)
	case "BLESS_TURN_UP":
		return engine.changePlayerBlessTurns(action, role, combatParameterInt(role.Parameters[0])), nil
	case "HP_CUT":
		return engine.executePlayerHPCut(action, role)
	case "REWRITE":
		return engine.executePlayerRewrite(action, role), nil
	case "BURST_GAUGE_QUICK_UP":
		var results []BattleResult
		for _, member := range engine.playerRoleTargets(action, role) {
			results = append(results, engine.addPlayerBurstGauge(member, role.RoleIndex, burstGaugeQuickValue(role, action.cardLevel, chainCount))...)
		}
		return results, nil
	case "DEAL_PENALTY":
		return engine.executeDealChange(action, role, true)
	case "DOT_VALUE_UP":
		return engine.executePlayerDOTValueUp(action, role, chainCount), nil
	default:
		return nil, fmt.Errorf("unsupported player combat function %q", role.Function)
	}
}

func resolvedPlayerRoleTarget(action battleAction, roleTarget string) string {
	if roleTarget != "SELECT" {
		return roleTarget
	}
	switch action.skill.Target {
	case "USER_ALL", "ENEMY_ALL", "SELF":
		return action.skill.Target
	}
	if action.target >= 1 && action.target <= maxRoomMembers {
		return "USER_ONE"
	}
	if action.target >= 5 {
		return "ENEMY_ONE"
	}
	return action.skill.Target
}

func playerCombatFunctionRegistered(function string) bool {
	switch function {
	case "ATTACK_AA", "DEF_UP_FIXED", "ATK_UP_FIXED", "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM",
		"ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM", "HEAL_FIXED", "HEAL_BY_SELF_PARAM",
		"DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM", "DEBUFF_RELEASE", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_OLD",
		"BUFF_RELEASE", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_OLD",
		"ATK_OP_DRAIN", "ATK_OP_DRAIN_ALL", "ATK_OP_REVENGE", "ATK_OP_PIERCING", "ATK_OP_DAMAGE_INCREASE",
		"ATK_OP_ATTR_RATE_DOWN_INVALID", "DEAL_BONUS", "REGENERATE_FIXED", "BURN", "POISON", "FREEZE",
		"BLEED", "ELECTRIC", "ENCHANT", "ATTR_DEF_DOWN", "ATTR_DEF_UP", "CRITICAL_UP", "CRITICAL_DAMAGE_BOOST", "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN",
		"PARAM_LIMIT_BREAK_FIXED", "WEAKNESS", "REFLECTION", "ENDURE", "COVERING", "BLESS", "CARD_SEAL_REGIST",
		"DARKNESS_REGIST", "GUTS", "STAN", "DEAL_PENALTY", "COST_BLOCK", "HP_CUT", "BURST_GAUGE_QUICK_UP", "DOT_VALUE_UP",
		"ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR", "ATTR_SEE", "CARD_TRAP_DAMAGE", "DARKNESS_RANDOM", "BLESS_TURN_UP":
		return true
	default:
		return false
	}
}

// nativeCriticalOutcome mirrors the common critical branch in CN libbattle5
// FUN_000838d0/FUN_000927b0. Rates use tenths of one percent: 150 is 15%,
// positive rolls use the normal 150% multiplier, and a negative critical roll
// (from CRITICAL_DOWN exceeding the base/up rate) halves the pre-defense value.
func (engine *BattleEngine) nativeCriticalOutcome(effects []battleEffect, baseRate int, skills ...CombatSkillDefinition) (flag int, multiplier int) {
	rate := nativeCriticalRate(effects, baseRate, skills...)
	criticalDamageRate := 150
	for _, effect := range effects {
		if effect.Remaining <= 0 {
			continue
		}
		if effect.Function == "CRITICAL_DAMAGE_BOOST" {
			criticalDamageRate = maxInt(criticalDamageRate, 150+effect.Rate)
		}
	}
	rate = maxInt(-1000, minInt(1000, rate))
	roll := int(engine.rng.next() % 10000)
	if rate > 0 && roll < rate*10 {
		return 1, criticalDamageRate
	}
	if rate < 0 && roll < -rate*10 {
		return -1, 50
	}
	return 0, 100
}

func nativeCriticalRate(effects []battleEffect, baseRate int, skills ...CombatSkillDefinition) int {
	rate := baseRate
	if baseRate >= 1000 {
		return rate
	}
	// 504b0/505f0 evaluate HIGH/LOW_CRITICAL_USER using an empty skill and
	// physics ALL, so their metric includes unrestricted EX critical boosts.
	skill := CombatSkillDefinition{DamageKind: "ALL"}
	if len(skills) > 0 {
		skill = skills[0]
	}
	for _, effect := range effects {
		if effect.Remaining <= 0 {
			continue
		}
		switch effect.Function {
		case "CRITICAL_UP":
			rate += effect.Rate
		case "CRITICAL_DOWN":
			rate -= effect.Rate
		case "CRITICAL_BOOST":
			if effect.ListType == 1 && supportAttributeMatches(effect.Attribute, skill.Attribute) &&
				damagePhysicsMatches(effect.DamageKind, skill.DamageKind) && supportCostMatches(effect, skill.Cost) {
				rate += effect.Rate
			}
		}
	}
	return rate
}

func activeEnchantEffect(effects []battleEffect) (value int, attribute string, active bool) {
	for _, effect := range effects {
		if effect.Function != "ENCHANT" || effect.Remaining <= 0 {
			continue
		}
		active = true // 45b88 counts entries; a zero value still reaches 93461.
		value += effect.Value
		if effect.Attribute != "" {
			attribute = effect.Attribute
		}
	}
	return value, attribute, active
}

func (engine *BattleEngine) executePlayerAttack(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := &engine.players[action.memberType-1]
	attackPower := attackExecutionPower(role, action.cardLevel, combatStatValue(actor, role.Parameters[5]))
	modifiers := collectAttackModifiers(action.roles, actor, action.cardLevel, chainCount)
	attackPower += modifiers.damageIncrease
	attackPower += attackRevengeBonus(actor.DamageTaken, actor, modifiers.revengeRate, modifiers.revengeParameter, modifiers.revengeCapRate)
	attackPower += attackRevengeBonus(actor.TurnDamage, actor, modifiers.nowTurnRevengeRate, modifiers.nowTurnRevengeParameter, modifiers.nowTurnRevengeCapRate)
	// 927b0 first combines fixed additions and the attacker's boost rates.
	// Conditional tribal boosts (466e2) add AFTER the ordinary 71d48 cap,
	// not as another multiplier. Chain belongs after target WEAKNESS/ATTR_DEF.
	boostFixed, boostRate := sphereDamageBoostTerms(actor.Effects, action.skill.Attribute, role.Parameters[8], action.skill.Cost)
	for _, tribal := range modifiers.tribalDamage {
		if engine.enemyPartyHasRace(tribal.raceID) && damagePhysicsMatches(tribal.damageKind, role.Parameters[8]) &&
			supportAttributeMatches(tribal.attribute, action.skill.Attribute) &&
			action.skill.Cost >= tribal.costMin && (tribal.costMax == 0 || action.skill.Cost <= tribal.costMax) {
			boostFixed += tribal.fixed
			boostRate += tribal.rate
		}
	}
	attackPower = int(int64(attackPower+boostFixed) * int64(1000+boostRate) / 1000)
	targets := engine.playerRoleEnemyTargets(action, role)
	if len(targets) == 0 {
		return nil, nil // 8b660 silently skips every attribute-filtered target.
	}
	hits := maxInt(1, combatParameterInt(role.Parameters[4]))
	results := make([]BattleResult, 0, len(targets)*hits*3)
	for _, targetIndex := range targets {
		enemy := &engine.enemies[targetIndex]
		// FUN_000838d0 computes every hit against the same pre-commit HP and
		// commits the accumulated normal + enchant damage once after the hit
		// loop. This is observable both in ResultCmd60 arg3 and in multi-hit
		// overkill: a lethal first hit does not suppress later hit commands.
		targetHP := enemy.HP
		pendingDamage := 0
		parentSignedDamage := 0
		var parent *battleEnemy
		if enemy.Parent > 0 && enemy.Parent <= engine.enemyCount {
			parent = &engine.enemies[enemy.Parent-1]
		}
		for hit := 0; hit < hits; hit++ {
			rate, damageAttribute := engine.enemyAttackAttribute(enemy, role.Parameters[7])
			if modifiers.attrRateDownInvalid {
				rate = maxInt(100, rate)
			}
			criticalSkill := action.skill
			criticalSkill.DamageKind = role.Parameters[8]
			critical, criticalMultiplier := engine.nativeCriticalOutcome(actor.Effects, combatParameterInt(role.Parameters[6]), criticalSkill)
			attrDefenseRate, attrDefenseFixed := attributeDefenseAdjustment(enemy.Effects, damageAttribute)
			attrDefenseFixed -= enemyFixedAttributeDefense(enemy, damageAttribute)
			attributeAdjustedPower := weaknessAdjustedPower(attackPower, enemy.Effects) * (1000 + attrDefenseRate) / 1000
			if chainCount > 1 {
				attributeAdjustedPower = int(int64(attributeAdjustedPower) * int64(100+role.ChainRate*(chainCount-1)) / 100)
			}
			defense := engine.enemyEffectiveDefense(enemy, role.Parameters[8])
			defense = nativePiercedDefense(defense, modifiers.piercingRate)
			damage, attributeDifference := nativeAttributeAttackDamage(attributeAdjustedPower, rate, criticalMultiplier, defense, attrDefenseFixed)
			resolution := engine.resolveIncomingDamageEffects(&enemy.Effects, damage, enemy.HP, damageAttribute, role.Parameters[8])
			damage = resolution.Damage
			if resolution.BarrierTriggered && damage == 0 {
				// 47028 clears the critical flag after an absorbed hit, retaining its RNG draw.
				critical = 0
			}
			if damage > 0 && parent != nil {
				// FUN_000838d0 keeps the attribute-only ResultCmd field separate
				// from its damage-consumer adjustment slot. Their final aggregate
				// is exactly the signed post-consumer damage propagated to parent.
				parentSignedDamage -= damage
				recordEnemyDamageKind(parent, damage, role.Parameters[8])
			}
			pendingDamage += damage
			recordEnemyDamage(enemy, damage, role.Parameters[8])
			recordEnemyAIDamage(enemy, damage, combatPhysicsIndex(role.Parameters[8]))
			engine.turnStats.Damage += damage
			engine.turnStats.DamageHits++
			engine.turnStats.DamageHitsByPhysics[combatPhysicsIndex(role.Parameters[8])]++
			engine.turnStats.DamageByUser[action.memberType-1] += damage
			engine.recordDamageEvent(enemy.MemberType, action.memberType, damage, damageAttribute, role.Parameters[8], "", false)
			if strings.EqualFold(role.Parameters[8], "MAGIC") {
				engine.turnStats.Magic += damage
			} else {
				engine.turnStats.Physical += damage
			}
			results = append(results,
				battleDamageResult(enemy.MemberType, role.RoleIndex, -damage, targetHP, attributeDifference, damageAttribute, rate, critical, 0, action.memberType),
			)
			results = append(results, damageEffectResults(enemy.MemberType, resolution)...)
			if resolution.Reflected > 0 {
				results = append(results, engine.reflectedDamageResults(actor.MemberType, enemy.MemberType, resolution.Reflected)...)
			}
			results = append(results, engine.playerAttackDrainResults(actor, role.RoleIndex, damage, modifiers)...)
			if enchantValue, enchantAttribute, active := activeEnchantEffect(actor.Effects); active {
				enchantRate, enchantAttribute := engine.enemyAttackAttribute(enemy, enchantAttribute)
				enchantDefenseRate, enchantDefenseFixed := attributeDefenseAdjustment(enemy.Effects, enchantAttribute)
				enchantDefenseFixed -= enemyFixedAttributeDefense(enemy, enchantAttribute)
				enchantPower := enchantValue * (1000 + enchantDefenseRate) / 1000
				enchantDamage, enchantDifference := nativeAttributeEnchantDamage(enchantPower, enchantRate, enchantDefenseFixed)
				enchantResolution := engine.resolveEnchantDamageEffects(&enemy.Effects, enchantDamage, enemy.HP, enchantAttribute)
				enchantDamage = enchantResolution.Damage
				if enchantDamage > 0 && parent != nil {
					parentSignedDamage -= enchantDamage
					recordEnemyDamageKind(parent, enchantDamage, "MAGIC")
				}
				pendingDamage += enchantDamage
				recordEnemyDamage(enemy, enchantDamage, "MAGIC")
				recordEnemyAIDamage(enemy, enchantDamage, 2)
				engine.turnStats.Damage += enchantDamage
				engine.turnStats.Enchant += enchantDamage
				engine.turnStats.DamageByUser[action.memberType-1] += enchantDamage
				engine.recordDamageEvent(enemy.MemberType, action.memberType, enchantDamage, enchantAttribute, "", "", true)
				results = append(results, battleDamageResult(enemy.MemberType, role.RoleIndex, -enchantDamage, targetHP, enchantDifference, enchantAttribute, enchantRate, 0, 1, action.memberType))
				results = append(results, damageEffectResults(enemy.MemberType, enchantResolution)...)
				// 838d0 reflects only the normal hit, before the enchant branch.
			}
		}
		enemy.HP = nativeHPCommit(targetHP, enemy.MaxHP, -pendingDamage, enemy.Effects)
		markEnemyPendingBreak(enemy)
		engine.addPlayerRoleHate(action.memberType, pendingDamage, action.skill.HateRatio, role.HateLimit)
		results = append(results, BattleResult{Command: resultHP, Args: []int64{int64(enemy.MemberType), int64(enemy.MaxHP), int64(enemy.HP), 1}})
		if parent != nil && parentSignedDamage < 0 && parent.HP > 0 && !parent.Broken {
			// Native commits the parent only once, after every normal and enchant
			// hit and after the selected part's HP row. It does not run the
			// parent's barriers or reflection a second time; 73e9f applies its HP floor.
			parentDamage := -parentSignedDamage
			parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, -parentDamage, parent.Effects)
			markEnemyPendingBreak(parent)
			recordEnemyDamageTotals(parent, parentDamage)
			results = append(results, BattleResult{Command: resultHP, Args: []int64{int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 1}})
		}
	}
	return results, nil
}

// 838d0 applies self drain then party drain after each ordinary hit. Both use
// the attack role index and the shared cap written by 81db0. Party drain runs
// 452d0 on the caster once, then commits that signed delta to each living ally.
func (engine *BattleEngine) playerAttackDrainResults(actor *battlePlayer, roleIndex, damage int, modifiers attackModifiers) []BattleResult {
	var results []BattleResult
	for i, rate := range [...]int{modifiers.drainRate, modifiers.drainAllRate} {
		heal := damage * rate / 100
		if modifiers.drainCap > 0 {
			heal = minInt(heal, modifiers.drainCap)
		}
		if heal <= 0 {
			continue
		}
		delta, hp := nativeHealCommit(actor.HP, actor.MaxHP, heal, actor.Effects, false)
		if i == 0 {
			actor.HP = hp
			results = append(results, battleHealResult(actor.MemberType, roleIndex, delta, hp))
			continue
		}
		for index := range engine.players {
			player := &engine.players[index]
			if player.HP <= 0 || player.GameOver {
				continue
			}
			player.HP = nativeHPCommit(player.HP, player.MaxHP, delta, player.Effects)
			results = append(results, battleHealResult(player.MemberType, roleIndex, delta, player.HP))
		}
	}
	return results
}

type attackModifiers struct {
	piercingRate            int
	drainRate               int
	drainCap                int
	drainRoleIndex          int
	drainAllRate            int
	drainAllCap             int
	drainAllRoleIndex       int
	damageIncrease          int
	revengeRate             int
	revengeParameter        string
	revengeCapRate          int
	nowTurnRevengeRate      int
	nowTurnRevengeParameter string
	nowTurnRevengeCapRate   int
	attrRateDownInvalid     bool
	tribalDamage            []tribalDamageModifier
}

type tribalDamageModifier struct {
	rate       int
	fixed      int
	damageKind string
	attribute  string
	costMin    int
	costMax    int
	raceID     int
}

func collectAttackModifiers(roles []CombatSkillRole, actor *battlePlayer, level int, chainCount int) attackModifiers {
	return collectAttackModifiersByRoleLevel(roles, actor, chainCount, func(CombatSkillRole) int { return level })
}

func collectEnemyAttackModifiers(roles []CombatSkillRole, actor *battlePlayer, chainCount int) attackModifiers {
	return collectAttackModifiersByRoleLevel(roles, actor, chainCount, func(role CombatSkillRole) int {
		return calibratedEnemySkillLevel(role.Function)
	})
}

func collectAttackModifiersByRoleLevel(roles []CombatSkillRole, actor *battlePlayer, chainCount int, roleLevel func(CombatSkillRole) int) attackModifiers {
	var result attackModifiers
	var seenDrain, seenDrainAll, lastDrainAll bool
	for _, role := range roles {
		level := roleLevel(role)
		switch role.Function {
		case "ATK_OP_PIERCING":
			result.piercingRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_DRAIN":
			if !seenDrain {
				seenDrain, lastDrainAll = true, false
			}
			if result.drainRate == 0 {
				result.drainRoleIndex = role.RoleIndex
			}
			result.drainRate += attackOperatorLevelValue(role, level, chainCount)
			result.drainCap = mergePositiveAttackCap(result.drainCap, combatParameterInt(role.Parameters[2]))
		case "ATK_OP_DRAIN_ALL":
			if !seenDrainAll {
				seenDrainAll, lastDrainAll = true, true
			}
			if result.drainAllRate == 0 {
				result.drainAllRoleIndex = role.RoleIndex
			}
			result.drainAllRate += attackOperatorLevelValue(role, level, chainCount)
			result.drainAllCap = mergePositiveAttackCap(result.drainAllCap, combatParameterInt(role.Parameters[2]))
		case "ATK_OP_DAMAGE_INCREASE":
			stat := combatStatValue(actor, role.Parameters[4])
			if cap := combatParameterInt(role.Parameters[5]); cap > 0 {
				stat = minInt(stat, cap)
			}
			base := combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level/1000
			coefficient := combatParameterInt(role.Parameters[2]) + combatParameterInt(role.Parameters[3])*level
			result.damageIncrease += base + stat*coefficient/1000
		case "ATK_OP_REVENGE":
			if result.revengeRate == 0 {
				result.revengeParameter = role.Parameters[2]
				result.revengeCapRate = combatParameterInt(role.Parameters[3])
			}
			result.revengeRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_NOW_TURN_REVENGE":
			if result.nowTurnRevengeRate == 0 {
				result.nowTurnRevengeParameter = role.Parameters[2]
				result.nowTurnRevengeCapRate = combatParameterInt(role.Parameters[3])
			}
			result.nowTurnRevengeRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_ATTR_RATE_DOWN_INVALID":
			result.attrRateDownInvalid = true
		case "DAMAGE_BOOST_ORDER_TRIBAL":
			result.tribalDamage = append(result.tribalDamage, tribalDamageModifier{
				rate:       combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level,
				fixed:      combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level/1000,
				damageKind: role.Parameters[6], attribute: role.Parameters[5],
				costMin: combatParameterInt(role.Parameters[7]), costMax: combatParameterInt(role.Parameters[8]),
				raceID: combatParameterInt(role.Parameters[9]),
			})
		}
	}
	result.piercingRate = minInt(100, maxInt(0, result.piercingRate))
	// 93790/939a0 merge repeated roles into their first producer entry.
	// 81db0 then overwrites the same cap field in producer-entry order.
	if lastDrainAll {
		result.drainCap = result.drainAllCap
	} else {
		result.drainAllCap = result.drainCap
	}
	return result
}

func attackOperatorLevelValue(role CombatSkillRole, level int, chainCount int) int {
	value := combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level
	if chainCount > 1 && role.ChainRate != 0 {
		value = value * (100 + role.ChainRate*(chainCount-1)) / 100
	}
	return value
}

func mergePositiveAttackCap(current int, incoming int) int {
	if incoming <= 0 {
		return current
	}
	if current <= 0 {
		return incoming
	}
	return minInt(current, incoming)
}

func attackRevengeBonus(damageTaken int, actor *battlePlayer, rate int, parameter string, capRate int) int {
	if damageTaken <= 0 || rate <= 0 {
		return 0
	}
	value := damageTaken * rate / 100
	cap := combatStatValue(actor, parameter) * capRate / 100
	return maxInt(0, minInt(value, cap))
}

func recordPlayerDamage(player *battlePlayer, damage int) {
	if damage <= 0 {
		return
	}
	player.DamageTaken += damage
	player.TurnDamage += damage
}

func recordEnemyDamage(enemy *battleEnemy, damage int, damageKind string) {
	if damage <= 0 {
		return
	}
	recordEnemyDamageTotals(enemy, damage)
	recordEnemyDamageKind(enemy, damage, damageKind)
}

func recordEnemyDamageTotals(enemy *battleEnemy, damage int) {
	if damage <= 0 {
		return
	}
	enemy.DamageTaken += damage
	enemy.TurnDamage += damage
}

func recordEnemyDamageKind(enemy *battleEnemy, damage int, damageKind string) {
	if damage <= 0 {
		return
	}
	switch strings.ToUpper(damageKind) {
	case "PHYSICS":
		enemy.TurnPhysical += damage
	case "MAGIC":
		enemy.TurnMagic += damage
	}
}

func (engine *BattleEngine) recordDamageEvent(target int, source int, damage int, attribute string, physics string, dot string, enchant bool) {
	if damage <= 0 {
		return
	}
	engine.turnStats.DamageEvents = append(engine.turnStats.DamageEvents, battleDamageEvent{
		Target: target, Source: source, Value: damage, Attribute: strings.ToUpper(attribute),
		Physics: strings.ToUpper(physics), DOT: strings.ToUpper(dot), Enchant: enchant,
	})
}

func combatPhysicsIndex(value string) int {
	switch strings.ToUpper(value) {
	case "MAGIC":
		return 1
	case "ALL":
		return 2
	default:
		return 0
	}
}

func (engine *BattleEngine) resolveEnemyDeath(results []BattleResult, enemy *battleEnemy) ([]BattleResult, error) {
	if enemy.HP > 0 || enemy.Broken {
		return results, nil
	}
	var err error
	results, err = engine.triggerEnemyDeathAction(results, enemy)
	if err != nil || enemy.HP > 0 {
		return results, err
	}
	return engine.commitEnemyBreak(results, enemy), nil
}

func (engine *BattleEngine) triggerEnemyDeathAction(results []BattleResult, enemy *battleEnemy) ([]BattleResult, error) {
	if enemy.HP > 0 || enemy.DeathActionTriggered {
		return results, nil
	}
	if !enemy.DeathActionTriggered {
		// Native FUN_0005b9d8 clears the per-enemy death trigger before it
		// evaluates slot 20. A failed condition/rate/budget therefore does not
		// retry on another HP consumer while the member remains dead.
		enemy.DeathActionTriggered = true
		for _, action := range enemy.Level.Actions {
			if action.Category != "death" {
				continue
			}
			consumed := enemy.ActionConsumed
			candidate, scheduled := engine.tryScheduleEnemyAction(enemy, action, &consumed)
			enemy.ActionConsumed = consumed
			if scheduled {
				projected, err := engine.executeEnemyActionCandidate(enemy, candidate)
				if err != nil {
					return nil, fmt.Errorf("enemy %d death action %d: %w", enemy.MemberType, action.SkillID, err)
				}
				results = append(results, projected...)
			}
			break
		}
	}
	return results, nil
}

func (engine *BattleEngine) commitEnemyBreak(results []BattleResult, enemy *battleEnemy) []BattleResult {
	enemy.Broken = true
	enemy.DiedTurn = engine.turn
	enemy.DeathCount++
	command := resultEnemyBreak
	if enemy.Parent > 0 {
		command = resultPartsBreak
	}
	results = append(results, BattleResult{Command: command, Args: []int64{int64(enemy.MemberType), 0, 0, 0, 1}})
	return append(results, enemyBreakDropResults(enemy)...)
}

func (engine *BattleEngine) executeFixedDefense(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	value := engine.fixedParameterActionValue(action, role, chainCount)
	value = engine.parameterBoostedValue(action, role, "DEF_UP_BOOST", value)
	return engine.applyPlayerParameter(action, role, value)
}

func (engine *BattleEngine) executeFixedParameter(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	value := engine.fixedParameterActionValue(action, role, chainCount)
	value = engine.parameterBoostedValue(action, role, "ATK_UP_BOOST", value)
	return engine.applyPlayerParameter(action, role, value)
}

func (engine *BattleEngine) executePlayerParameterLimit(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	// Native role 142 creates action type 2/subtype 32. Its value is accumulated
	// into the selected ATK/INT/MND ceiling; it never mutates that battle
	// parameter directly. ResultCmd6 exposes the three current ceilings.
	value := engine.fixedParameterActionValue(action, role, chainCount)
	if value <= 0 {
		return nil, nil
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	results := make([]BattleResult, 0, 8)
	for _, memberType := range engine.playerBuffTargets(action, role) {
		player := &engine.players[memberType-1]
		ensurePlayerBaseParameters(player)
		if !adjustPlayerParameterLimit(player, role.Parameters[1], value) {
			return nil, fmt.Errorf("unsupported player parameter limit %q", role.Parameters[1])
		}
		player.Effects = append(player.Effects, battleEffect{
			Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value,
			Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
		})
		engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
		refreshAppliedPlayerParameter(player, role.Parameters[1], 0)
		results = append(results,
			battleParameterBuffResult(memberType, role, battleBuffCodes[role.Function]),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
		)
	}
	return results, nil
}

func adjustPlayerParameterLimit(player *battlePlayer, name string, amount int) bool {
	switch strings.ToUpper(name) {
	case "ATK":
		player.LimitAttack = maxInt(0, activeParameterLimit(player.LimitAttack)+amount)
	case "INT":
		player.LimitMagic = maxInt(0, activeParameterLimit(player.LimitMagic)+amount)
	case "MND":
		player.LimitRecovery = maxInt(0, activeParameterLimit(player.LimitRecovery)+amount)
	default:
		return false
	}
	return true
}

func (engine *BattleEngine) executeFixedHeal(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := &engine.players[action.memberType-1]
	value := fixedHealRoleValue(role, action.cardLevel, 1, combatStatValue(actor, role.Parameters[4]))
	value = supportHealValue(actor.Effects, action.skill, value)
	value = scaleHealChain(value, role.ChainRate, chainCount)
	return engine.healPlayerTargets(action, role, value)
}

func (engine *BattleEngine) executeSelfScaledParameter(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := &engine.players[action.memberType-1]
	boostFunction := "ATK_UP_BOOST"
	if strings.HasPrefix(role.Function, "DEF_UP") {
		boostFunction = "DEF_UP_BOOST"
	}
	// 88a20 reads the source again for each recipient. A source that receives
	// its own ATK-based ATK buff changes the value for subsequent recipients.
	return engine.applyPlayerParameterValue(action, role, func() int {
		value := selfScaledParameterValue(role, action.cardLevel, chainCount, combatStatValue(actor, role.Parameters[2]))
		return engine.parameterBoostedValue(action, role, boostFunction, value)
	})
}

func (engine *BattleEngine) applyPlayerParameter(action battleAction, role CombatSkillRole, value int) ([]BattleResult, error) {
	return engine.applyPlayerParameterValue(action, role, func() int { return value })
}

func (engine *BattleEngine) applyPlayerParameterValue(action battleAction, role CombatSkillRole, currentValue func() int) ([]BattleResult, error) {
	buffCode, exists := battleBuffCodes[role.Function]
	if !exists {
		return nil, fmt.Errorf("player parameter function %q has no official buff code", role.Function)
	}
	if !combatParameterSupported(role.Parameters[1]) {
		return nil, fmt.Errorf("unsupported player parameter %q", role.Parameters[1])
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	results := make([]BattleResult, 0, 8)
	for _, memberType := range engine.playerBuffTargets(action, role) {
		value := currentValue()
		player := &engine.players[memberType-1]
		ensurePlayerBaseParameters(player)
		player.Effects = append(player.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value, Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
		refreshAppliedPlayerParameter(player, role.Parameters[1], value)
		results = append(results,
			battleParameterBuffResult(memberType, role, buffCode),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
		)
	}
	if combatTargetIsPlayer(role.Target) {
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		value := currentValue()
		enemy := &engine.enemies[index]
		ensureEnemyBaseParameters(enemy)
		effect := battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value, Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex}
		enemy.Effects = append(enemy.Effects, effect)
		engine.recordAIStatusApplied(enemy.MemberType, effect)
		refreshAppliedEnemyParameter(enemy, role.Parameters[1], value)
		results = append(results, battleParameterBuffResult(enemy.MemberType, role, buffCode),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)})
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyParameterDebuff(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	role.Target = resolvedPlayerRoleTarget(action, role.Target)
	value := fixedBuffRoleValue(role, action.cardLevel, chainCount)
	if strings.HasSuffix(role.Function, "BY_SELF_PARAM") {
		actor := &engine.players[action.memberType-1]
		value = selfScaledParameterValue(role, action.cardLevel, chainCount, combatStatValue(actor, role.Parameters[2]))
	}
	boostFunction := "ATK_BREAK_BOOST"
	if strings.HasPrefix(role.Function, "GUARD_BREAK") {
		boostFunction = "GUARD_BREAK_BOOST"
	}
	value = engine.parameterBoostedValue(action, role, boostFunction, value)
	if !combatParameterSupported(role.Parameters[1]) {
		return nil, fmt.Errorf("unsupported debuff parameter %q", role.Parameters[1])
	}
	results := make([]BattleResult, 0, engine.enemyCount*2)
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	for _, memberType := range engine.playerBuffTargets(action, role) {
		player := &engine.players[memberType-1]
		ensurePlayerBaseParameters(player)
		player.Effects = append(player.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: -value, Kind: 2, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
		refreshAppliedPlayerParameter(player, role.Parameters[1], -value)
		results = append(results,
			battleParameterBuffResult(memberType, role, battleBuffCodes[role.Function]),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
		)
	}
	if combatTargetIsPlayer(role.Target) {
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		enemy := &engine.enemies[index]
		ensureEnemyBaseParameters(enemy)
		code := battleBuffCodes[role.Function]
		enemy.Effects = append(enemy.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: -value, Kind: 2, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(enemy.MemberType, enemy.Effects[len(enemy.Effects)-1])
		refreshAppliedEnemyParameter(enemy, role.Parameters[1], -value)
		results = append(results,
			battleParameterBuffResult(enemy.MemberType, role, code),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)},
		)
	}
	return results, nil
}

func (engine *BattleEngine) executeSelfScaledHeal(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := &engine.players[action.memberType-1]
	value := selfScaledHealRoleValue(role, action.cardLevel, chainCount, combatStatValue(actor, role.Parameters[0]))
	value = supportHealValue(actor.Effects, action.skill, value)
	return engine.healPlayerTargets(action, role, value)
}

func (engine *BattleEngine) healPlayerTargets(action battleAction, role CombatSkillRole, value int) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 8)
	for _, memberType := range engine.playerRoleTargets(action, role) {
		player := &engine.players[memberType-1]
		oldHP := player.HP
		reported, hp := nativeHealCommit(player.HP, player.MaxHP, value, player.Effects, true)
		player.HP = hp
		engine.turnStats.Heal += maxInt(0, reported)
		engine.recordPlayerHealCommit(player, action.memberType, oldHP, reported, action.skill.HateRatio, role.HateLimit)
		results = append(results, battleHealResult(memberType, role.RoleIndex, reported, player.HP))
	}
	// 822a0 also accepts enemy targets for a player skill. Only player-to-player
	// healing generates hate; positive reported healing always enters the
	// caster's skill counter, and enemy parts share it with their living parent.
	if combatTargetIsPlayer(role.Target) {
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		rows := engine.commitDirectEnemyHeal(&engine.enemies[index], role.RoleIndex, value)
		engine.turnStats.Heal += maxInt(0, int(rows[0].Args[2]))
		results = append(results, rows...)
	}
	return results, nil
}

func (engine *BattleEngine) recordPlayerHealCommit(player *battlePlayer, source int, oldHP, reported, hateRate, hateLimit int) {
	// 822a0/5f808 retain the requested positive amount for the result/skill
	// counters, but 57c40 receives only HP actually restored. 73e9f records
	// a reversed heal as damage; it never generates healing hate.
	if reported < 0 {
		recordPlayerDamage(player, -reported)
	} else if reported > 0 {
		engine.addPlayerRoleHate(source, maxInt(0, player.HP-oldHP), hateRate, hateLimit)
	}
}
