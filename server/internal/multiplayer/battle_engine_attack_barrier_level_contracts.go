package multiplayer

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// validateEnemyAttackBarrierLevelContracts binds ordinary ATTACK_BARRIER to
// managed enemy skill level one. The barrier hit/release lifecycle is already
// shared and typed; this contract guards the producer matrix and proves that
// p1+p2*level reaches that existing consumer through a complete official
// outer action.
func validateEnemyAttackBarrierLevelContracts(catalog *CombatCatalog) error {
	rows := make([]CombatSkillRole, 0, 310)
	targets := make(map[string]int)
	durations := make(map[string]int)
	bases := make(map[string]int)
	growth := make(map[string]int)
	uses := make(map[string]int)
	physics := make(map[string]int)
	effect2D := make(map[string]int)
	effect3D := make(map[string]int)
	growthRoleIDs := make(map[int]bool)
	growthRows := 0
	for _, roleSet := range catalog.EnemySkillRoles {
		for _, role := range roleSet {
			if role.Function != "ATTACK_BARRIER" {
				continue
			}
			rows = append(rows, role)
			targets[role.Target]++
			durations[role.Parameters[0]]++
			bases[role.Parameters[1]]++
			growth[role.Parameters[2]]++
			uses[role.Parameters[3]]++
			physics[role.Parameters[4]]++
			effect2D[normalizedMatrixText(role.Effect2D)]++
			effect3D[normalizedMatrixText(role.Effect3D)]++
			if role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 || role.Parameters[5] != "" ||
				role.Parameters[6] != "" || role.Parameters[7] != "" || role.Parameters[8] != "" || role.Parameters[9] != "" {
				return fmt.Errorf("official enemy ATTACK_BARRIER tail changed: %+v", role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy ATTACK_BARRIER skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			if combatParameterInt(role.Parameters[2]) != 0 {
				growthRows++
				growthRoleIDs[role.SkillID] = true
			}
		}
	}
	if len(rows) != 310 || growthRows != 131 || len(growthRoleIDs) != 128 ||
		!equalStringCountMap(targets, map[string]int{"ENEMY_ALL": 2, "SELECT": 249, "SELF": 59}) ||
		!equalStringCountMap(durations, map[string]int{"2": 46, "3": 59, "4": 52, "5": 18, "7": 1, "8": 2, "9": 18, "10": 4, "99": 110}) ||
		!equalStringCountMap(growth, map[string]int{"0": 179, "1": 131}) ||
		!equalStringCountMap(uses, map[string]int{"1": 25, "2": 53, "3": 64, "4": 28, "5": 21, "6": 10, "7": 21, "8": 7, "9": 4, "10": 17, "12": 6, "14": 2, "15": 4, "16": 4, "17": 1, "20": 16, "22": 1, "24": 2, "25": 1, "30": 11, "40": 2, "42": 1, "47": 1, "50": 1, "99": 3, "999": 4}) ||
		!equalStringCountMap(physics, map[string]int{"ALL": 110, "MAGIC": 124, "PHYSICS": 76}) ||
		!equalStringCountMap(effect2D, map[string]int{"empty": 210, "enemy_skill_buff": 65, "enemy_skill_buff_noname": 35}) ||
		!equalStringCountMap(effect3D, map[string]int{"empty": 164, "enemy_a_skill_sp0": 10, "enemy_b_skill_sp0": 38, "enemy_b_skill_sp4": 29, "enemy_b_skill_sp5": 3, "enemy_skil_sp_1": 4, "enemy_skill_set_def": 8, "enemy_skill_sp0": 31, "enemy_skill_sp1": 17, "enemy_skill_sp2": 6}) {
		return fmt.Errorf("official enemy ATTACK_BARRIER matrix changed: rows=%d growth_rows=%d growth_ids=%d targets=%v durations=%v bases=%v growth=%v uses=%v physics=%v effect2d=%v effect3d=%v", len(rows), growthRows, len(growthRoleIDs), targets, durations, bases, growth, uses, physics, effect2D, effect3D)
	}
	if digest := combatRoleMatrixDigest(rows); digest != "cc7354c2790495d831d2c0599be23ae064691d8e65a362e5bc3a0b2f85177b6a" {
		return fmt.Errorf("official enemy ATTACK_BARRIER row digest is %s", digest)
	}
	if digest := combatIntSetDigest(growthRoleIDs); digest != "cdf0a5f348aca2605d98a991e496d4daf792d2a6efab94c0824d596d066a209a" {
		return fmt.Errorf("official enemy ATTACK_BARRIER growth-role digest is %s", digest)
	}

	variantCount := 0
	growthSkillIDs := make(map[int]bool)
	for skillID, variants := range catalog.EnemySkills {
		for _, variant := range variants {
			if growthRoleIDs[variant.FunctionID] {
				variantCount++
				growthSkillIDs[skillID] = true
			}
		}
	}
	actionRecords := make([]string, 0, 187)
	referencingLevels := make(map[int]bool)
	for levelID, level := range catalog.EnemyLevels {
		for _, action := range level.Actions {
			if !growthSkillIDs[action.SkillID] {
				continue
			}
			referencingLevels[levelID] = true
			actionRecords = append(actionRecords, fmt.Sprintf("%d|%d|%s|%d|%d|%d|%s|%s|%d|%d|%d|%t",
				levelID, action.Slot, action.Category, action.SkillID, action.AIConditionID, action.Priority,
				action.Target, strings.Join(action.TargetParams[:], ","), action.ActionCost, action.MaxUses, action.Rate, action.CountOnMiss))
		}
	}
	if variantCount != 128 || len(growthSkillIDs) != 128 || len(actionRecords) != 187 || len(referencingLevels) != 108 ||
		combatIntSetDigest(growthSkillIDs) != "cdf0a5f348aca2605d98a991e496d4daf792d2a6efab94c0824d596d066a209a" ||
		combatIntSetDigest(referencingLevels) != "c61f1ac92b263949efc4f45c1a4a12f4e80e4edd4d853566e3a31cc2c74cc2a3" {
		return fmt.Errorf("official enemy ATTACK_BARRIER reachability changed: variants=%d skills=%d refs=%d levels=%d skill_digest=%s level_digest=%s", variantCount, len(growthSkillIDs), len(actionRecords), len(referencingLevels), combatIntSetDigest(growthSkillIDs), combatIntSetDigest(referencingLevels))
	}
	if digest := combatStringRecordsDigest(actionRecords); digest != "01ea6159390b94e8cae31969c5f1f2ee67dbc07fc7f3945162c9c4796c2ef9b1" {
		return fmt.Errorf("official enemy ATTACK_BARRIER action digest is %s", digest)
	}
	return validateEnemyAttackBarrierGrowthAction(catalog)
}

func normalizedMatrixText(value string) string {
	if value == "" {
		return "empty"
	}
	return value
}

func combatRoleMatrixDigest(roles []CombatSkillRole) string {
	records := make([]string, 0, len(roles))
	for _, role := range roles {
		attributes := make([]string, len(role.Attributes))
		for index, enabled := range role.Attributes {
			if enabled {
				attributes[index] = "1"
			} else {
				attributes[index] = "0"
			}
		}
		records = append(records, fmt.Sprintf("%d|%d|%s|%s|%s|%s|%s|%s|%t|%s|%s|%d|%d",
			role.SkillID, role.RoleIndex, role.Effect2D, role.Effect3D, role.HitEffect, role.HitPosition,
			role.Function, role.Target, role.ExcludeSelf, strings.Join(attributes, ""),
			strings.Join(role.Parameters[:], ","), role.ChainRate, role.HateLimit))
	}
	return combatStringRecordsDigest(records)
}

func combatIntSetDigest(values map[int]bool) string {
	keys := make([]int, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Ints(keys)
	parts := make([]string, len(keys))
	for index, value := range keys {
		parts[index] = strconv.Itoa(value)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return fmt.Sprintf("%x", sum)
}

func combatStringRecordsDigest(records []string) string {
	sort.Strings(records)
	sum := sha256.Sum256([]byte(strings.Join(records, "\n")))
	return fmt.Sprintf("%x", sum)
}

func validateEnemyAttackBarrierGrowthAction(catalog *CombatCatalog) error {
	const levelID = 40029621
	const skillID = 44279009
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 2 || roles[0].Function != "ATK_UP_FIXED" || roles[1].Function != "ATTACK_BARRIER" {
		return fmt.Errorf("official enemy ATTACK_BARRIER role set %d changed: %+v", skillID, roles)
	}
	barrier := roles[1]
	if barrier.RoleIndex != 1 || barrier.Target != "SELF" || barrier.Parameters[0] != "3" ||
		barrier.Parameters[1] != "1" || barrier.Parameters[2] != "1" || barrier.Parameters[3] != "4" ||
		barrier.Parameters[4] != "ALL" || attackBarrierRoleValue(barrier, 0) != 1 ||
		attackBarrierRoleValue(barrier, 1) != 2 || calibratedEnemySkillLevel(barrier.Function) != 1 {
		return fmt.Errorf("official enemy ATTACK_BARRIER 44279009 changed: %+v", barrier)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 250000 || definition.Attack != 1200 || definition.Attribute != "WIND" {
		return fmt.Errorf("official enemy ATTACK_BARRIER definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy ATTACK_BARRIER level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 10 || action.Category != "skill" || action.AIConditionID != 40277008 || action.Priority != 13 ||
		action.Target != "RANDOM" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy ATTACK_BARRIER action changed: %+v", action)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1, HP: 10000, MaxHP: 10000, Attack: 1000, Magic: 900,
			Recovery: 800, Defense: 700, MDefense: 600, Attribute: "NEUTRAL",
		}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	effects := engine.enemies[0].Effects
	if len(results) != 5 || results[0].Command != resultSkill || results[1].Command != resultBuff ||
		results[2].Command != resultBattleParam || results[3].Command != resultBuff ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 4, skillID, 0}) ||
		results[1].Args[0] != 5 || engine.players[3].Attack != 1000 || len(engine.players[3].Effects) != 0 ||
		len(effects) != 2 || effects[0].Function != "ATK_UP_FIXED" || effects[0].Delta != 300 ||
		effects[1].Function != "ATTACK_BARRIER" || effects[1].Value != 2 ||
		effects[1].Uses != 4 || effects[1].Remaining != 3 || effects[1].DamageKind != "ALL" ||
		effects[1].Source != 5 || effects[1].RoleIndex != 1 ||
		!equalBattleArgs(results[3].Args, []int64{5, 1, 0, 205, 4, 0, 1, 2, 4, 0, 0}) ||
		results[4].Command != resultBattleParam ||
		!equalBattleArgs(results[4].Args, battleParameterArgs(5, 250000, 250000, 1500, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official enemy ATTACK_BARRIER level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	absorbed := engine.resolveIncomingDamageEffects(&engine.enemies[0].Effects, 2, engine.enemies[0].HP, "DARK", "MAGIC")
	over := engine.resolveIncomingDamageEffects(&engine.enemies[0].Effects, 3, engine.enemies[0].HP, "LIGHT", "PHYSICS")
	rowsAfterAbsorb := damageEffectResults(5, absorbed)
	rowsAfterOver := damageEffectResults(5, over)
	if absorbed.Damage != 0 || absorbed.BarrierRemainingUses != 3 || len(rowsAfterAbsorb) != 1 ||
		rowsAfterAbsorb[0].Command != 200 || rowsAfterAbsorb[0].Args[2] != 3 ||
		over.Damage != 3 || over.BarrierRemainingUses != 2 || len(rowsAfterOver) != 1 ||
		rowsAfterOver[0].Command != 200 || rowsAfterOver[0].Args[2] != 2 {
		return fmt.Errorf("official enemy ATTACK_BARRIER corrected consumer is absorbed=%+v rows=%+v over=%+v rows=%+v", absorbed, rowsAfterAbsorb, over, rowsAfterOver)
	}
	return nil
}
