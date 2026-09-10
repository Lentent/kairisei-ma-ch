package multiplayer

import (
	"fmt"
	"strings"
)

// validateEnemyAllBuffReleaseContracts owns the complete ordinary-enemy
// BUFF_RELEASE object graph. Native FUN_00096755 produces action type 8 and
// p0+p1*level chance; the role, outer skill and enemy action remain separate
// target layers. In particular, the old revive skill 37801102 revives ENEMY3
// but its following SELF release still targets the acting enemy.
func validateEnemyAllBuffReleaseContracts(catalog *CombatCatalog) error {
	rows := make([]CombatSkillRole, 0, 81)
	roleIDs := make(map[int]bool)
	targets := make(map[string]int)
	rates := make(map[[2]string]int)
	for _, roleSet := range catalog.EnemySkillRoles {
		for _, role := range roleSet {
			if role.Function != "BUFF_RELEASE" {
				continue
			}
			rows = append(rows, role)
			roleIDs[role.SkillID] = true
			targets[role.Target]++
			rates[[2]string{role.Parameters[0], role.Parameters[1]}]++
			if role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 ||
				role.Parameters[2] != "" || role.Parameters[3] != "" {
				return fmt.Errorf("official enemy BUFF_RELEASE shape changed: %+v", role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy BUFF_RELEASE skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			for parameter := 4; parameter < len(role.Parameters); parameter++ {
				if role.Parameters[parameter] != "" {
					return fmt.Errorf("official enemy BUFF_RELEASE skill %d has unexpected p%d=%q", role.SkillID, parameter+1, role.Parameters[parameter])
				}
			}
			if releaseMode(role.Function) != "ALL" || releaseWantedKind(role.Function) != 1 ||
				releaseRoleChance(role, 0) != releaseRoleChance(role, 1) {
				return fmt.Errorf("official enemy BUFF_RELEASE producer changed: %+v", role)
			}
		}
	}
	if len(rows) != 81 || len(roleIDs) != 81 ||
		!equalStringCountMap(targets, map[string]int{"SELECT": 75, "SELF": 6}) ||
		rates[[2]string{"100", ""}] != 5 || rates[[2]string{"100", "0"}] != 76 || len(rates) != 2 {
		return fmt.Errorf("official enemy BUFF_RELEASE matrix changed: rows=%d ids=%d targets=%v rates=%v", len(rows), len(roleIDs), targets, rates)
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
	actionRecords := make([]string, 0, 202)
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
	if outerVariants != 82 || len(outerSkills) != 82 || len(actionRecords) != 202 ||
		len(reachableSkills) != 66 || len(referencingLevels) != 135 ||
		!equalStringCountMap(outerTargets, map[string]int{"USER_ALL": 62, "USER_ONE": 14, "DEAD_ENEMY_ONE": 6}) ||
		!equalStringCountMap(actionTargets, map[string]int{"RANDOM": 154, "ENEMY3": 15, "": 13, "HIGH_ATK_USER": 12, "HIGH_INT_USER": 8}) {
		return fmt.Errorf("official enemy BUFF_RELEASE reachability changed: variants=%d skills=%d refs=%d reachable=%d levels=%d outer=%v actions=%v",
			outerVariants, len(outerSkills), len(actionRecords), len(reachableSkills), len(referencingLevels), outerTargets, actionTargets)
	}
	roleDigest := combatRoleMatrixDigest(rows)
	actionDigest := combatStringRecordsDigest(actionRecords)
	if roleDigest != "f14285f4134a69e8f27332767d14c0975ecea0098e3bb625b5843d1bfd6a195e" ||
		actionDigest != "3043e2fd6f48546b496276b00ca78fd3eac59e7ab496b66348d13927db17ded3" {
		return fmt.Errorf("official enemy BUFF_RELEASE digests are role=%s action=%s", roleDigest, actionDigest)
	}

	if err := validateEnemySelectedBuffReleaseAction(catalog); err != nil {
		return err
	}
	return validateEnemyReviveSelfBuffReleaseAction(catalog)
}

func validateEnemySelectedBuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 40018331
	const skillID = 44176005
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "USER_ONE" || len(roles) != 1 ||
		roles[0].RoleIndex != 0 || roles[0].Function != "BUFF_RELEASE" || roles[0].Target != "SELECT" ||
		action.Slot != 4 || action.Target != "HIGH_ATK_USER" {
		return fmt.Errorf("official selected BUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	attacks := [4]int{1000, 1300, 1500, 1200}
	for index := range engine.players {
		engine.players[index].Attack = attacks[index]
		engine.players[index].BaseAttack = attacks[index]
	}
	engine.players[1].BaseAttack = 1100
	engine.players[1].Effects = []battleEffect{{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Delta: 200, Remaining: 2, RoleIndex: 4, Source: 1}}
	engine.players[2].BaseAttack = 1000
	engine.players[2].Effects = []battleEffect{{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Delta: 500, Remaining: 2, RoleIndex: 5, Source: 1}}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{resultSkill, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam}
	if len(results) != len(wantCommands) || len(engine.players[2].Effects) != 0 || engine.players[2].Attack != 1000 ||
		len(engine.players[1].Effects) != 1 || engine.players[1].Attack != 1300 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 3, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{3, 0, 69, 0, 0, 0, 0, 0}) {
		return fmt.Errorf("official selected BUFF_RELEASE action is results=%+v players=%+v", results, engine.players)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official selected BUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}

func validateEnemyReviveSelfBuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 31100121
	const skillID = 37801102
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "DEAD_ENEMY_ONE" || len(roles) != 2 ||
		roles[0].RoleIndex != 0 || roles[0].Function != "REVIVE" || roles[0].Target != "SELECT" ||
		roles[1].RoleIndex != 1 || roles[1].Function != "BUFF_RELEASE" || roles[1].Target != "SELF" ||
		action.Slot != 3 || action.Target != "ENEMY3" {
		return fmt.Errorf("official revive/SELF BUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 3}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "CRITICAL_UP", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 5},
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 1},
	}}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 8000, MaxHP: 8000}
	engine.enemies[2] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 7, HP: 0, MaxHP: 9000, Broken: true, DeathActionTriggered: true, DiedTurn: 2, DeathCount: 1}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{resultSkill, resultSkillRevive, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam}
	if len(results) != len(wantCommands) || engine.enemies[2].HP != 9000 || engine.enemies[2].Broken ||
		len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].Function != "BURN" ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 7, 1, 5, skillID, 0}) ||
		!equalBattleArgs(results[2].Args, []int64{5, 1, 69, 0, 0, 0, 0, 0}) {
		return fmt.Errorf("official revive/SELF BUFF_RELEASE action is results=%+v enemies=%+v", results, engine.enemies[:3])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official revive/SELF BUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}
