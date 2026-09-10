package multiplayer

import (
	"fmt"
	"strings"
)

// validateEnemyRewriteContracts owns the complete ordinary-enemy REWRITE
// object graph. Native FUN_000974bd only produces action type 4/subtype 3 with
// duration and attribute; the concrete member still comes from the separate
// role, outer-skill and enemy-action target layers.
func validateEnemyRewriteContracts(catalog *CombatCatalog) error {
	rows := make([]CombatSkillRole, 0, 207)
	roleIDs := make(map[int]bool)
	targets := make(map[string]int)
	values := make(map[[2]string]int)
	for _, roleSet := range catalog.EnemySkillRoles {
		for _, role := range roleSet {
			if role.Function != "REWRITE" {
				continue
			}
			rows = append(rows, role)
			roleIDs[role.SkillID] = true
			targets[role.Target]++
			values[[2]string{role.Parameters[0], role.Parameters[1]}]++
			if role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 || role.Target != "SELECT" {
				return fmt.Errorf("official enemy REWRITE shape changed: %+v", role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy REWRITE skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			for parameter := 2; parameter < len(role.Parameters); parameter++ {
				if role.Parameters[parameter] != "" {
					return fmt.Errorf("official enemy REWRITE skill %d has unexpected p%d=%q", role.SkillID, parameter+1, role.Parameters[parameter])
				}
			}
			if combatParameterInt(role.Parameters[0]) <= 0 || combatAttributeCode(role.Parameters[1]) == 0 {
				return fmt.Errorf("official enemy REWRITE value changed: %+v", role)
			}
		}
	}
	wantedValues := map[[2]string]int{
		{"99", "LIGHT"}: 53, {"99", "DARK"}: 44, {"99", "FIRE"}: 41,
		{"99", "ICE"}: 32, {"99", "WIND"}: 32,
		{"999", "LIGHT"}: 1, {"999", "DARK"}: 1, {"999", "FIRE"}: 1,
		{"999", "ICE"}: 1, {"999", "WIND"}: 1,
	}
	if len(rows) != 207 || len(roleIDs) != 207 ||
		!equalStringCountMap(targets, map[string]int{"SELECT": 207}) || len(values) != len(wantedValues) {
		return fmt.Errorf("official enemy REWRITE matrix changed: rows=%d ids=%d targets=%v values=%v", len(rows), len(roleIDs), targets, values)
	}
	for key, count := range wantedValues {
		if values[key] != count {
			return fmt.Errorf("official enemy REWRITE value %v count is %d, want %d", key, values[key], count)
		}
	}

	outerSkills := make(map[int]bool)
	outerTargets := make(map[string]int)
	outerVariants := 0
	for skillID, variants := range catalog.EnemySkills {
		for _, variant := range variants {
			if !roleIDs[variant.FunctionID] {
				continue
			}
			outerVariants++
			outerSkills[skillID] = true
			outerTargets[variant.Target]++
		}
	}
	actionRecords := make([]string, 0, 150)
	reachableSkills := make(map[int]bool)
	referencingLevels := make(map[int]bool)
	actionTargets := make(map[string]int)
	for levelID, level := range catalog.EnemyLevels {
		for _, action := range level.Actions {
			if !outerSkills[action.SkillID] {
				continue
			}
			reachableSkills[action.SkillID] = true
			referencingLevels[levelID] = true
			actionTargets[action.Target]++
			actionRecords = append(actionRecords, fmt.Sprintf("%d|%d|%s|%d|%d|%d|%s|%s|%d|%d|%d|%t",
				levelID, action.Slot, action.Category, action.SkillID, action.AIConditionID, action.Priority,
				action.Target, strings.Join(action.TargetParams[:], ","), action.ActionCost, action.MaxUses, action.Rate, action.CountOnMiss))
		}
	}
	if outerVariants != 207 || len(outerSkills) != 79 || len(actionRecords) != 150 ||
		len(reachableSkills) != 40 || len(referencingLevels) != 52 ||
		!equalStringCountMap(outerTargets, map[string]int{"SELF": 140, "ENEMY_ONE": 63, "": 4}) ||
		!equalStringCountMap(actionTargets, map[string]int{"RANDOM": 82, "ENEMY3": 30, "ENEMY2": 18, "": 12, "ENEMY4": 8}) {
		return fmt.Errorf("official enemy REWRITE reachability changed: variants=%d skills=%d refs=%d reachable=%d levels=%d outer=%v actions=%v",
			outerVariants, len(outerSkills), len(actionRecords), len(reachableSkills), len(referencingLevels), outerTargets, actionTargets)
	}
	roleDigest := combatRoleMatrixDigest(rows)
	actionDigest := combatStringRecordsDigest(actionRecords)
	if roleDigest != "1e06bc13337d7fb8c2732eee2829c32dd93814bcaf17905017b09735552dd7b8" ||
		actionDigest != "adbe3501dbe8d39f9c922a57fc889eac79c52ca41ccd8e538f689cf10b1e0dd8" {
		return fmt.Errorf("official enemy REWRITE digests are role=%s action=%s", roleDigest, actionDigest)
	}

	if err := validateEnemyRewriteNullSelfAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyRewriteRandomDisplaySelfAction(catalog); err != nil {
		return err
	}
	return validateEnemyRewriteEnemyPartAction(catalog)
}

func validateEnemyRewriteNullSelfAction(catalog *CombatCatalog) error {
	const levelID = 40009632
	const skillID = 44099012
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "SELF" || len(roles) != 2 || roles[0].Function != "ATTR_HIDE" ||
		roles[1].Function != "REWRITE" || roles[1].Target != "SELECT" || roles[1].Parameters[1] != "LIGHT" ||
		action.Slot != 1 || action.Target != "" {
		return fmt.Errorf("official NULL/SELF REWRITE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.enemies[0].BaseAttribute = "DARK"
	engine.enemies[0].Attribute = "DARK"
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{resultSkill, resultRewrite, resultBuff, resultBattleParam, resultBuff, resultBattleParam}
	// D-377 party 40096003: NULL AI retains display target zero while
	// the outer SELF skill still applies both roles to the enemy caster.
	if len(results) != len(wantCommands) || engine.enemies[0].Attribute != "LIGHT" || len(engine.enemies[0].Effects) != 2 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 0, 1, 0, skillID, 0}) ||
		!equalBattleArgs(results[3].Args, battleParameterArgs(5, 125000, 125000, 3000, 3000, 1000, 0, 0, 0, 0, 0)) ||
		!equalBattleArgs(results[1].Args, []int64{5, int64(combatAttributeCode("LIGHT"))}) {
		return fmt.Errorf("official NULL/SELF REWRITE action is results=%+v enemy=%+v", results, engine.enemies[0])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official NULL/SELF REWRITE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}

func validateEnemyRewriteRandomDisplaySelfAction(catalog *CombatCatalog) error {
	const levelID = 40009632
	const skillID = 44099004
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "SELF" || len(roles) != 4 || roles[2].Function != "REWRITE" ||
		roles[2].Target != "SELECT" || roles[2].Parameters[1] != "FIRE" || action.Slot != 4 || action.Target != "RANDOM" {
		return fmt.Errorf("official random-display SELF REWRITE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	for index := range engine.players {
		engine.players[index].BaseAttribute = "WIND"
		engine.players[index].Attribute = "WIND"
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	selected := int(results[0].Args[2])
	// Original D-329 API: 50 retains a random player, while SELF applies to
	// the enemy caster. Immediate 32 precedes deferred 67 and both 62/6 pairs.
	if selected < 1 || selected > 4 || engine.enemies[0].Attribute != "FIRE" || len(engine.enemies[0].Effects) != 2 ||
		len(results) != 7 || results[1].Command != resultRewrite || !equalBattleArgs(results[1].Args, []int64{5, 1}) ||
		results[2].Command != resultBuffReleaseFailed || results[3].Command != resultBuff || results[4].Command != resultBattleParam ||
		results[5].Command != resultBuff || results[6].Command != resultBattleParam ||
		!equalBattleArgs(results[5].Args, []int64{5, 2, 0, 202, 4, 0, 2, 0, 0, 0, 0}) ||
		!equalBattleArgs(results[4].Args, battleParameterArgs(5, 125000, 125000, 3000, 3000, 1000, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official random-display SELF REWRITE action is results=%+v enemy=%+v", results, engine.enemies[0])
	}
	for index := range engine.players {
		if engine.players[index].Attribute != "WIND" || len(engine.players[index].Effects) != 0 {
			return fmt.Errorf("official SELF REWRITE changed player %d: %+v", index+1, engine.players[index])
		}
	}
	return nil
}

func validateEnemyRewriteEnemyPartAction(catalog *CombatCatalog) error {
	const levelID = 40009311
	const skillID = 44096005
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "ENEMY_ONE" || len(roles) != 2 || roles[0].Function != "ATTR_HIDE" ||
		roles[1].Function != "REWRITE" || roles[1].Target != "SELECT" || roles[1].Parameters[1] != "FIRE" ||
		action.Slot != 1 || action.Target != "ENEMY2" {
		return fmt.Errorf("official enemy-part REWRITE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.enemyCount = 2
	engine.enemies[1] = battleEnemy{MemberType: 6, HP: 10000, MaxHP: 10000, BaseAttribute: "DARK", Attribute: "DARK"}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{resultSkill, resultRewrite, resultBuff, resultBattleParam, resultBuff, resultBattleParam}
	if len(results) != len(wantCommands) || engine.enemies[1].Attribute != "FIRE" || len(engine.enemies[1].Effects) != 2 ||
		len(engine.enemies[0].Effects) != 0 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 6, 1, 3, skillID, 0}) ||
		!equalBattleArgs(results[3].Args, battleParameterArgs(6, 10000, 10000, 0, 0, 0, 0, 0, 0, 0, 0)) ||
		!equalBattleArgs(results[1].Args, []int64{6, int64(combatAttributeCode("FIRE"))}) {
		return fmt.Errorf("official enemy-part REWRITE action is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy-part REWRITE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}
