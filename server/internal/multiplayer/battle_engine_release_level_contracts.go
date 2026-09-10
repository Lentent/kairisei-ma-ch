package multiplayer

import (
	"fmt"
	"strings"
)

// validateEnemyReleaseLevelContracts owns the ordinary-enemy level boundary
// for the three active release producers with non-zero p1. The selection,
// kind/limit and ordered release-child consumers remain guarded by the common
// lifecycle contracts.
func validateEnemyReleaseLevelContracts(catalog *CombatCatalog) error {
	wanted := map[string]int{"BUFF_RELEASE_ONE": 349, "DEBUFF_RELEASE_ONE": 289, "BUFF_RELEASE_ONE_NUM": 28}
	wantedGrowth := map[string]int{"BUFF_RELEASE_ONE": 18, "DEBUFF_RELEASE_ONE": 6, "BUFF_RELEASE_ONE_NUM": 28}
	rows := make([]CombatSkillRole, 0, 666)
	counts := make(map[string]int)
	growthCounts := make(map[string]int)
	growthRoleIDs := make(map[int]bool)
	for function := range wanted {
		counts[function] = 0
		growthCounts[function] = 0
	}
	for _, roleSet := range catalog.EnemySkillRoles {
		for _, role := range roleSet {
			if _, ok := wanted[role.Function]; !ok {
				continue
			}
			rows = append(rows, role)
			counts[role.Function]++
			if combatParameterInt(role.Parameters[1]) != 0 {
				growthCounts[role.Function]++
				growthRoleIDs[role.SkillID] = true
			}
		}
	}
	if len(rows) != 666 || len(growthRoleIDs) != 20 || !equalStringCountMap(counts, wanted) || !equalStringCountMap(growthCounts, wantedGrowth) {
		return fmt.Errorf("official enemy release-level matrix changed: rows=%d growth_roles=%d counts=%v growth_counts=%v", len(rows), len(growthRoleIDs), counts, growthCounts)
	}
	if digest := combatRoleMatrixDigest(rows); digest != "d662402063dccbb0759bb8b9a4ec7cc9e43747759c04d324fc35354eff8dc272" {
		return fmt.Errorf("official enemy release-level row digest is %s", digest)
	}
	if digest := combatIntSetDigest(growthRoleIDs); digest != "d4ba58cc44f505667376ae871c02eaa967a2c8af155683e11bfcf831ad0dde1a" {
		return fmt.Errorf("official enemy release-level growth-role digest is %s", digest)
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
	actionRecords := make([]string, 0, 49)
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
	if variantCount != 20 || len(growthSkillIDs) != 20 || len(actionRecords) != 49 || len(referencingLevels) != 49 {
		return fmt.Errorf("official enemy release-level reachability changed: variants=%d skills=%d refs=%d levels=%d", variantCount, len(growthSkillIDs), len(actionRecords), len(referencingLevels))
	}
	if digest := combatIntSetDigest(growthSkillIDs); digest != "d4ba58cc44f505667376ae871c02eaa967a2c8af155683e11bfcf831ad0dde1a" {
		return fmt.Errorf("official enemy release-level outer-skill digest is %s", digest)
	}
	if digest := combatIntSetDigest(referencingLevels); digest != "53b5ef0be0eefc9091942ca3edfc6b080318216268f656de499f5b71d4b6d40b" {
		return fmt.Errorf("official enemy release-level enemy-level digest is %s", digest)
	}
	if digest := combatStringRecordsDigest(actionRecords); digest != "e93446082eab405ff1f774b8d84532c7fe0c25d6a4814b9bdab5886daa0253b7" {
		return fmt.Errorf("official enemy release-level action digest is %s", digest)
	}
	return validateEnemyReleaseGrowthActions(catalog)
}

func validateEnemyReleaseGrowthActions(catalog *CombatCatalog) error {
	if err := validateEnemyBuffReleaseOneGrowthAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyDebuffReleaseOneGrowthAction(catalog); err != nil {
		return err
	}
	return validateEnemyBuffReleaseOneNumGrowthAction(catalog)
}

func validateEnemyBuffReleaseOneGrowthAction(catalog *CombatCatalog) error {
	const levelID = 31150123
	const skillID = 38101109
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 2 || roles[0].Function != "BUFF_RELEASE_ONE" || roles[1].Function != "BUFF_RELEASE_ONE" {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE role set %d changed: %+v", skillID, roles)
	}
	for _, role := range roles {
		if role.Target != "SELECT" || role.Parameters[0] != "100" || role.Parameters[1] != "1" ||
			releaseRoleChance(role, 0) != 100 || releaseRoleChance(role, 1) != 101 ||
			calibratedEnemySkillLevel(role.Function) != 1 {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE growth row changed: %+v", role)
		}
	}
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	if definition.HP != 60000 || definition.Attack != 300 || definition.Magic != 300 || definition.Recovery != 1000 ||
		definition.Defense != 500 || definition.MagicDefense != 500 || definition.Attribute != "FIRE" ||
		level.HPBars != 2 || level.ActionsPerTurn != 1 || action.Slot != 1 || action.Category != "skill" ||
		action.AIConditionID != 38101110 || action.Priority != 12 || action.Target != "RANDOM" ||
		action.ActionCost != 0 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE action boundary changed: definition=%+v level=%+v action=%+v", definition, level, action)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.players[3].Effects = []battleEffect{
		{Function: "PARAM_LIMIT_BREAK_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, RoleIndex: 1, Source: 1},
		{Function: "PARAM_LIMIT_BREAK_FIXED", Parameter: "INT", Kind: 1, Remaining: 2, RoleIndex: 2, Source: 1},
		// Native kind32 is an actual MAX_HP delta, not a producer named ATK_UP_BY_MAX_HP.
		{Function: "ATK_UP_FIXED", Parameter: "MAX_HP", Delta: 100, Kind: 1, Remaining: 2, RoleIndex: 3, Source: 1},
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{50, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam, 72, resultBuffLostOne, resultBattleParam,
		resultBuffRelease, 72, resultBuffLostOne, resultBattleParam}
	if len(results) != len(wantCommands) || len(engine.players[3].Effects) != 0 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 2, skillID, 0}) {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE level-one action is results=%+v effects=%+v", results, engine.players[3].Effects)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	if !equalBattleArgs(results[1].Args, []int64{4, 0, 48, 49, 0, 0, 0, 0}) ||
		!equalBattleArgs(results[8].Args, []int64{4, 1, 32, 0, 0, 0, 0, 0}) {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE release rows changed: %+v", results)
	}
	// Two release roles now visit all four USER_ALL targets, including the
	// three without removable entries; their chance draws still advance RNG.
	if next := engine.rng.next(); next != 496576104 {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE consumed the wrong xor128 draws; next=%d", next)
	}
	return nil
}

func validateEnemyDebuffReleaseOneGrowthAction(catalog *CombatCatalog) error {
	const levelID = 31050121
	const skillID = 37601110
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 1 || roles[0].Function != "DEBUFF_RELEASE_ONE" {
		return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE role set %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "100" || role.Parameters[1] != "100" ||
		role.Parameters[2] != "ATK_BREAK_BY_INT" || role.Parameters[3] != "WEAKNESS" ||
		releaseRoleChance(role, 0) != 100 || releaseRoleChance(role, 1) != 200 ||
		calibratedEnemySkillLevel(role.Function) != 1 {
		return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE growth row changed: %+v", role)
	}
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	if definition.HP != 225000 || definition.Attack != 0 || definition.Magic != 220 || definition.Recovery != 0 ||
		definition.Defense != 0 || definition.MagicDefense != 0 || definition.Attribute != "WIND" ||
		level.HPBars != 3 || level.ActionsPerTurn != 1 || action.Slot != 9 || action.Category != "skill" ||
		action.AIConditionID != skillID || action.Priority != 13 || action.Target != "RANDOM" ||
		action.ActionCost != 0 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE action boundary changed: definition=%+v level=%+v action=%+v", definition, level, action)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.players[3].Effects = []battleEffect{
		{Function: "ATK_BREAK_FIXED", Parameter: "INT", Delta: -100, Kind: 2, Remaining: 2, RoleIndex: 1, Source: 5},
		{Function: "WEAKNESS", Kind: 2, Remaining: 2, RoleIndex: 2, Source: 5},
	}
	// This official skill targets ENEMY_ALL, not the AI's RANDOM player.
	// Keep the player decoy and put the same removable state on the enemy.
	engine.enemies[0].Effects = append([]battleEffect(nil), engine.players[3].Effects...)
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{50, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam, 72, resultBuffLostOne, resultBattleParam}
	if len(results) != len(wantCommands) || len(engine.enemies[0].Effects) != 0 || len(engine.players[3].Effects) != 2 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 4, skillID, 0}) {
		return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE level-one action is results=%+v effects=%+v", results, engine.players[3].Effects)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	if !equalBattleArgs(results[1].Args, []int64{5, 0, 0, 0, 0, 2, 19, 0}) {
		return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE release row changed: %+v", results[1])
	}
	if next := engine.rng.next(); next != 2618378627 {
		return fmt.Errorf("official enemy DEBUFF_RELEASE_ONE consumed the wrong xor128 draws; next=%d", next)
	}
	return nil
}

func validateEnemyBuffReleaseOneNumGrowthAction(catalog *CombatCatalog) error {
	const levelID = 31080121
	const skillID = 37701113
	allRoles := catalog.EnemySkillRoles[skillID]
	if len(allRoles) != 5 || allRoles[4].RoleIndex != 4 || allRoles[4].Function != "HP_CUT" || allRoles[4].Parameters[0] != "20" {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM role set %d changed: %+v", skillID, allRoles)
	}
	roles := allRoles[:4]
	for index, role := range roles {
		if role.RoleIndex != index || role.Function != "BUFF_RELEASE_ONE_NUM" || role.Target != "SELECT" ||
			role.Parameters[0] != "100" || role.Parameters[1] != "1" || role.Parameters[4] != "100" ||
			releaseRoleChance(role, 0) != 100 || releaseRoleChance(role, 1) != 101 ||
			calibratedEnemySkillLevel(role.Function) != 1 {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM growth row %d changed: %+v", index, role)
		}
	}
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	if definition.HP != 250000 || definition.Attack != 500 || definition.Magic != 500 || definition.Recovery != 0 ||
		definition.Defense != 0 || definition.MagicDefense != 10000 || definition.Attribute != "LIGHT" ||
		level.HPBars != 3 || level.ActionsPerTurn != 2 || action.Slot != 5 || action.Category != "skill" ||
		action.AIConditionID != skillID || action.Priority != 14 || action.Target != "RANDOM" ||
		action.ActionCost != 0 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM action boundary changed: definition=%+v level=%+v action=%+v", definition, level, action)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.players[3].Effects = []battleEffect{
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Delta: 100, Kind: 1, Remaining: 2, RoleIndex: 1, Source: 1},
		{Function: "ATK_UP_FIXED", Parameter: "INT", Delta: 100, Kind: 1, Remaining: 2, RoleIndex: 2, Source: 1},
		{Function: "DEF_UP_FIXED", Parameter: "DEF", Delta: 100, Kind: 1, Remaining: 2, RoleIndex: 3, Source: 1},
		{Function: "DEF_UP_FIXED", Parameter: "MDEF", Delta: 100, Kind: 1, Remaining: 2, RoleIndex: 4, Source: 1},
		{Function: "CRITICAL_UP", Kind: 1, Remaining: 2, RoleIndex: 5, Source: 1},
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{
		50,
		resultBuffRelease, 72, resultBuffLostOne, resultBattleParam,
		resultBuffRelease, 72, resultBuffLostOne, resultBattleParam,
		resultBuffRelease, 72, resultBuffLostOne, resultBattleParam, 72, resultBuffLostOne, resultBattleParam,
		resultBuffRelease, 72, resultBuffLostOne, resultBattleParam,
		resultHPCut, resultHPCut, resultHPCut, resultHPCut,
	}
	if len(results) != len(wantCommands) || len(engine.players[3].Effects) != 0 || engine.players[3].HP != 8000 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 2, skillID, 0}) {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM level-one action is results=%+v effects=%+v", results, engine.players[3].Effects)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	wantReleaseArgs := [][]int64{
		{4, 0, 1, 0, 0, 0, 0, 0},
		{4, 1, 2, 0, 0, 0, 0, 0},
		{4, 2, 4, 5, 0, 0, 0, 0},
		{4, 3, 23, 0, 0, 0, 0, 0},
	}
	for index, resultIndex := range []int{1, 5, 9, 16} {
		if !equalBattleArgs(results[resultIndex].Args, wantReleaseArgs[index]) {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM release row %d changed: %+v", index, results[resultIndex])
		}
	}
	for index := range engine.players {
		member := int64(index + 1)
		if engine.players[index].HP != 8000 || !equalBattleArgs(results[20+index].Args, []int64{member, 4, -2000, 8000}) {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM all-player HP cut changed: %+v", results[20:])
		}
	}
	if next := engine.rng.next(); next != 2477440007 {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE_NUM consumed the wrong xor128 draws; next=%d", next)
	}
	return nil
}

func releaseGrowthActionBoundary(catalog *CombatCatalog, levelID int, skillID int) (CombatEnemyDefinition, CombatEnemyLevel, CombatEnemyAction, error) {
	definition, ok := catalog.Enemies[levelID]
	if !ok {
		return CombatEnemyDefinition{}, CombatEnemyLevel{}, CombatEnemyAction{}, fmt.Errorf("official enemy release definition %d is missing", levelID)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return CombatEnemyDefinition{}, CombatEnemyLevel{}, CombatEnemyAction{}, fmt.Errorf("official enemy release level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	return definition, level, action, err
}
