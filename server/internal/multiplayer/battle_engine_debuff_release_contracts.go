package multiplayer

import "fmt"

// validateEnemyAllDebuffReleaseContracts closes the ordinary-enemy action
// boundary for native role 38 (DEBUFF_RELEASE). The selector is deliberately
// checked at three levels: the role target, the outer skill target, and the
// enemy-level action override. Conflating those levels breaks SELF cleanses,
// ENEMY_ALL cleanses whose action selects a player, and the revive-then-cleanse
// sequence used by old multipart bosses.
func validateEnemyAllDebuffReleaseContracts(catalog *CombatCatalog) error {
	roleCount := 0
	roleIDs := make(map[int]bool)
	targets := make(map[string]int)
	rates := make(map[[2]string]int)
	kinds := make(map[[2]string]int)
	for _, roles := range catalog.EnemySkillRoles {
		for _, role := range roles {
			if role.Function != "DEBUFF_RELEASE" {
				continue
			}
			roleCount++
			roleIDs[role.SkillID] = true
			targets[role.Target]++
			rates[[2]string{role.Parameters[0], role.Parameters[1]}]++
			kinds[[2]string{role.Parameters[2], role.Parameters[3]}]++
			if role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 {
				return fmt.Errorf("official enemy DEBUFF_RELEASE shape changed: %+v", role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy DEBUFF_RELEASE skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			for parameter := 4; parameter < len(role.Parameters); parameter++ {
				if role.Parameters[parameter] != "" {
					return fmt.Errorf("official enemy DEBUFF_RELEASE skill %d has unexpected p%d=%q", role.SkillID, parameter+1, role.Parameters[parameter])
				}
			}
			if releaseMode(role.Function) != "ALL" || releaseWantedKind(role.Function) != 2 ||
				releaseRoleChance(role, 0) != releaseRoleChance(role, 1) {
				return fmt.Errorf("official enemy DEBUFF_RELEASE producer changed: %+v", role)
			}
		}
	}
	if roleCount != 158 || len(roleIDs) != 155 ||
		!equalStringCountMap(targets, map[string]int{"SELECT": 110, "ENEMY_ALL": 34, "SELF": 14}) ||
		rates[[2]string{"100", ""}] != 11 || rates[[2]string{"100", "0"}] != 136 ||
		rates[[2]string{"1000", "0"}] != 11 || len(rates) != 3 ||
		kinds[[2]string{"", ""}] != 152 || kinds[[2]string{"ATK_BREAK", ""}] != 3 ||
		kinds[[2]string{"GUARD_BREAK", ""}] != 3 || len(kinds) != 3 {
		return fmt.Errorf("official enemy DEBUFF_RELEASE matrix changed: roles=%d ids=%d targets=%v rates=%v kinds=%v",
			roleCount, len(roleIDs), targets, rates, kinds)
	}

	// Plain DEBUFF_RELEASE ignores p2/p3 and projects the native all-debuff
	// sentinel. These duplicated rows prove that the two legacy kind labels do
	// not silently turn the role into DEBUFF_RELEASE_ONE.
	legacyRoles := catalog.EnemySkillRoles[30901113]
	if len(legacyRoles) != 2 || legacyRoles[0].RoleIndex != 0 || legacyRoles[1].RoleIndex != 1 ||
		legacyRoles[0].Function != "DEBUFF_RELEASE" || legacyRoles[1].Function != "DEBUFF_RELEASE" ||
		legacyRoles[0].Target != "SELECT" || legacyRoles[1].Target != "SELECT" ||
		legacyRoles[0].Parameters[2] != "ATK_BREAK" || legacyRoles[1].Parameters[2] != "GUARD_BREAK" {
		return fmt.Errorf("official legacy all-debuff release rows changed: %+v", legacyRoles)
	}

	outerSkills := make(map[int]bool)
	outerVariants := 0
	for skillID, variants := range catalog.EnemySkills {
		for _, variant := range variants {
			if !roleIDs[variant.FunctionID] {
				continue
			}
			outerVariants++
			outerSkills[skillID] = true
		}
	}
	actionRefs := 0
	reachableSkills := make(map[int]bool)
	actionTargets := make(map[string]int)
	for _, level := range catalog.EnemyLevels {
		for _, action := range level.Actions {
			if !outerSkills[action.SkillID] {
				continue
			}
			actionRefs++
			reachableSkills[action.SkillID] = true
			actionTargets[action.Target]++
		}
	}
	if outerVariants != 155 || len(outerSkills) != 155 || actionRefs != 361 || len(reachableSkills) != 141 ||
		!equalStringCountMap(actionTargets, map[string]int{"ENEMY1": 13, "ENEMY2": 58, "RANDOM": 290}) {
		return fmt.Errorf("official enemy DEBUFF_RELEASE reachability changed: variants=%d skills=%d refs=%d reachable=%d targets=%v",
			outerVariants, len(outerSkills), actionRefs, len(reachableSkills), actionTargets)
	}

	if err := validateEnemySelfDebuffReleaseAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyAllDebuffReleaseAction(catalog); err != nil {
		return err
	}
	return validateEnemyReviveDebuffReleaseAction(catalog)
}

func validateEnemySelfDebuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 40032421
	const skillID = 44324101
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "USER_ALL" || len(roles) != 3 ||
		roles[0].RoleIndex != 0 || roles[0].Function != "BUFF_RELEASE_ONE" || roles[0].Target != "SELECT" ||
		roles[1].RoleIndex != 1 || roles[1].Function != "BUFF_RELEASE_ONE" || roles[1].Target != "SELECT" ||
		roles[2].RoleIndex != 2 || roles[2].Function != "DEBUFF_RELEASE" || roles[2].Target != "SELF" ||
		action.Slot != 1 || action.Target != "RANDOM" || action.AIConditionID != 40324112 {
		return fmt.Errorf("official enemy SELF DEBUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 1},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 5},
	}}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "POISON", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 6, Source: 1},
	}}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{resultSkill,
		resultBuffRelease, 72, resultBuffLostOne, resultBattleParam,
		resultBuffReleaseFailed, resultBuffReleaseFailed, resultBuffReleaseFailed, resultBuffReleaseFailed,
		resultBuffReleaseFailed, resultBuffReleaseFailed, resultBuffReleaseFailed, resultBuffReleaseFailed}
	if len(results) != len(wantCommands) || len(engine.enemies[0].Effects) != 1 ||
		engine.enemies[0].Effects[0].Function != "ATK_UP_FIXED" || len(engine.enemies[1].Effects) != 1 ||
		engine.enemies[1].Effects[0].Function != "POISON" ||
		!equalBattleArgs(results[1].Args, []int64{5, 2, 0, 0, 0, 30, 0, 0}) {
		return fmt.Errorf("official enemy SELF DEBUFF_RELEASE action is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy SELF DEBUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	for role := 0; role < 2; role++ {
		for member := 1; member <= 4; member++ {
			// 7adb0 drains failed roles after immediate successful consumers;
			// D-329's mixed REWRITE/release API confirms this deferred ordering.
			if !equalBattleArgs(results[5+role*4+member-1].Args, []int64{int64(member), int64(role)}) {
				return fmt.Errorf("official USER_ALL release failure lost target/role: %+v", results)
			}
		}
	}
	return nil
}

func validateEnemyAllDebuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 40030521
	const skillID = 44305001
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "ENEMY_ALL" || len(roles) != 1 || roles[0].RoleIndex != 0 ||
		roles[0].Function != "DEBUFF_RELEASE" || roles[0].Target != "ENEMY_ALL" ||
		action.Slot != 1 || action.Target != "RANDOM" || action.AIConditionID != 40305001 {
		return fmt.Errorf("official enemy ENEMY_ALL DEBUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 1},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 5},
	}}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "WEAKNESS", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 6, Source: 1},
	}}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{resultSkill, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam}
	if len(results) != len(wantCommands) || len(engine.enemies[0].Effects) != 1 ||
		engine.enemies[0].Effects[0].Function != "ATK_UP_FIXED" || len(engine.enemies[1].Effects) != 0 ||
		!equalBattleArgs(results[1].Args, []int64{5, 0, 0, 0, 0, 30, 0, 0}) ||
		!equalBattleArgs(results[5].Args, []int64{6, 0, 0, 0, 0, 30, 0, 0}) {
		return fmt.Errorf("official enemy ENEMY_ALL DEBUFF_RELEASE action is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy ENEMY_ALL DEBUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}

func validateEnemyReviveDebuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 31210121
	const skillID = 38401112
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "DEAD_ENEMY_ONE" || len(roles) != 4 ||
		roles[0].Function != "REVIVE" || roles[1].Function != "DEBUFF_RELEASE" ||
		roles[2].Function != "DEF_UP_FIXED" || roles[3].Function != "DEF_UP_FIXED" ||
		action.Slot != 10 || action.Target != "ENEMY2" || action.AIConditionID != skillID {
		return fmt.Errorf("official revive/cleanse boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 6, HP: 0, MaxHP: 9000, Broken: true, DeathActionTriggered: true, DiedTurn: 2, DeathCount: 1,
		Effects: []battleEffect{
			{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 1},
			{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 5},
		},
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	target := &engine.enemies[1]
	wantCommands := []int{resultSkill, resultSkillRevive, resultBuffRelease, 72, resultBuffLostOne, resultBattleParam,
		resultBuff, resultBattleParam, resultBuff, resultBattleParam}
	if len(results) != len(wantCommands) || target.HP != 9000 || target.Broken || target.DeathActionTriggered || target.DiedTurn != 0 ||
		len(target.Effects) != 3 || target.Effects[0].Function != "ATK_UP_FIXED" ||
		target.Effects[1].Function != "DEF_UP_FIXED" || target.Effects[1].Parameter != "DEF" ||
		target.Effects[2].Function != "DEF_UP_FIXED" || target.Effects[2].Parameter != "MDEF" ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 6, 1, 5, skillID, 0}) ||
		!equalBattleArgs(results[2].Args, []int64{6, 1, 0, 0, 0, 30, 0, 0}) {
		return fmt.Errorf("official revive/cleanse action is results=%+v target=%+v", results, *target)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official revive/cleanse command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}
