package multiplayer

import "fmt"

// validateEnemyAttackOptionLevelContracts binds the two attack-option
// consumers with active enemy level growth to the managed level-one skill
// object. Native DRAIN and REVENGE both evaluate p0+p1*level, but their
// downstream consumers differ: DRAIN heals from actual committed damage while
// REVENGE adds a capped value derived from cumulative damage taken.
func validateEnemyAttackOptionLevelContracts(catalog *CombatCatalog) error {
	counts := make(map[string]int)
	targets := map[string]map[string]int{
		"ATK_OP_DRAIN":   {},
		"ATK_OP_REVENGE": {},
	}
	primaryValues := map[string]map[string]int{
		"ATK_OP_DRAIN":   {},
		"ATK_OP_REVENGE": {},
	}
	growthValues := map[string]map[string]int{
		"ATK_OP_DRAIN":   {},
		"ATK_OP_REVENGE": {},
	}
	sourceValues := make(map[string]int)
	capValues := map[string]map[string]int{
		"ATK_OP_DRAIN":   {},
		"ATK_OP_REVENGE": {},
	}
	chainRates := map[string]map[string]int{
		"ATK_OP_DRAIN":   {},
		"ATK_OP_REVENGE": {},
	}
	hateLimits := map[string]map[string]int{
		"ATK_OP_DRAIN":   {},
		"ATK_OP_REVENGE": {},
	}
	growthRoleIDs := make(map[int]bool)
	zeroGrowthCounts := make(map[string]int)

	for _, roles := range catalog.EnemySkillRoles {
		for _, role := range roles {
			switch role.Function {
			case "ATK_OP_PIERCING", "ATK_OP_DAMAGE_INCREASE", "ATK_OP_NOW_TURN_REVENGE":
				zeroGrowthCounts[role.Function]++
				if combatParameterInt(role.Parameters[1]) != 0 {
					return fmt.Errorf("official enemy %s unexpectedly has level growth: %+v", role.Function, role)
				}
			case "ATK_OP_DRAIN", "ATK_OP_REVENGE":
				counts[role.Function]++
				targets[role.Function][role.Target]++
				primaryValues[role.Function][role.Parameters[0]]++
				growthValues[role.Function][role.Parameters[1]]++
				capValues[role.Function][role.Parameters[3]]++
				chainRates[role.Function][fmt.Sprint(role.ChainRate)]++
				hateLimits[role.Function][fmt.Sprint(role.HateLimit)]++
				if role.Function == "ATK_OP_DRAIN" {
					capValues[role.Function][role.Parameters[2]]++
				} else {
					sourceValues[role.Parameters[2]]++
				}
				if combatParameterInt(role.Parameters[1]) != 0 {
					growthRoleIDs[role.SkillID] = true
				}
			}
		}
	}
	if !equalStringCountMap(zeroGrowthCounts, map[string]int{
		"ATK_OP_PIERCING":         2565,
		"ATK_OP_DAMAGE_INCREASE":  36,
		"ATK_OP_NOW_TURN_REVENGE": 61,
	}) {
		return fmt.Errorf("official zero-growth enemy attack-option matrix changed: %v", zeroGrowthCounts)
	}
	if counts["ATK_OP_DRAIN"] != 207 ||
		!equalStringCountMap(targets["ATK_OP_DRAIN"], map[string]int{"ENEMY_ALL": 1, "SELECT": 122, "SELF": 84}) ||
		!equalStringCountMap(growthValues["ATK_OP_DRAIN"], map[string]int{"0": 205, "10000": 2}) ||
		!equalStringCountMap(chainRates["ATK_OP_DRAIN"], map[string]int{"0": 207}) ||
		!equalStringCountMap(hateLimits["ATK_OP_DRAIN"], map[string]int{"0": 202, "10000": 5}) {
		return fmt.Errorf("official enemy DRAIN matrix changed: count=%d targets=%v primary=%v growth=%v caps=%v chain=%v hate=%v", counts["ATK_OP_DRAIN"], targets["ATK_OP_DRAIN"], primaryValues["ATK_OP_DRAIN"], growthValues["ATK_OP_DRAIN"], capValues["ATK_OP_DRAIN"], chainRates["ATK_OP_DRAIN"], hateLimits["ATK_OP_DRAIN"])
	}
	if counts["ATK_OP_REVENGE"] != 19 ||
		!equalStringCountMap(targets["ATK_OP_REVENGE"], map[string]int{"SELECT": 6, "SELF": 13}) ||
		!equalStringCountMap(primaryValues["ATK_OP_REVENGE"], map[string]int{"1": 3, "30": 9, "100": 7}) ||
		!equalStringCountMap(growthValues["ATK_OP_REVENGE"], map[string]int{"0": 12, "1": 7}) ||
		!equalStringCountMap(sourceValues, map[string]int{"ATK": 6, "HP": 12, "MAX_HP": 1}) ||
		!equalStringCountMap(capValues["ATK_OP_REVENGE"], map[string]int{"1": 6, "10": 1, "30": 3, "50": 2, "100": 6, "200": 1}) ||
		!equalStringCountMap(chainRates["ATK_OP_REVENGE"], map[string]int{"0": 19}) ||
		!equalStringCountMap(hateLimits["ATK_OP_REVENGE"], map[string]int{"0": 19}) {
		return fmt.Errorf("official enemy REVENGE matrix changed: count=%d targets=%v primary=%v growth=%v sources=%v caps=%v chain=%v hate=%v", counts["ATK_OP_REVENGE"], targets["ATK_OP_REVENGE"], primaryValues["ATK_OP_REVENGE"], growthValues["ATK_OP_REVENGE"], sourceValues, capValues["ATK_OP_REVENGE"], chainRates["ATK_OP_REVENGE"], hateLimits["ATK_OP_REVENGE"])
	}
	wantGrowthRoles := map[int]bool{
		35601113: true, 35601213: true, 35601317: true,
		36601603: true, 36601604: true,
		44254015: true, 44254116: true, 44254216: true, 44254222: true,
	}
	if !equalIntBoolMap(growthRoleIDs, wantGrowthRoles) {
		return fmt.Errorf("official enemy attack-option growth roles changed: %v", growthRoleIDs)
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
	actionRefs := 0
	referencingLevels := make(map[int]bool)
	for levelID, level := range catalog.EnemyLevels {
		for _, action := range level.Actions {
			if growthSkillIDs[action.SkillID] {
				actionRefs++
				referencingLevels[levelID] = true
			}
		}
	}
	wantLevels := map[int]bool{
		30880123: true, 30880133: true, 30880153: true, 30880253: true,
		40027132: true, 40027142: true, 40027152: true, 40027154: true,
		83005081: true, 83005181: true, 83005281: true,
	}
	if variantCount != 9 || !equalIntBoolMap(growthSkillIDs, wantGrowthRoles) ||
		actionRefs != 14 || !equalIntBoolMap(referencingLevels, wantLevels) {
		return fmt.Errorf("official enemy attack-option reachability changed: variants=%d skills=%v refs=%d levels=%v", variantCount, growthSkillIDs, actionRefs, referencingLevels)
	}

	if err := validateEnemyDrainGrowthAction(catalog); err != nil {
		return err
	}
	return validateEnemyRevengeGrowthAction(catalog)
}

func enemyAttackOptionRole(catalog *CombatCatalog, skillID int, function string) (CombatSkillRole, error) {
	for _, role := range catalog.EnemySkillRoles[skillID] {
		if role.Function == function {
			return role, nil
		}
	}
	return CombatSkillRole{}, fmt.Errorf("official enemy skill role %d/%s is missing", skillID, function)
}

func enemyAttackOptionAction(level CombatEnemyLevel, skillID int) (CombatEnemyAction, error) {
	for _, action := range level.Actions {
		if action.SkillID == skillID {
			return action, nil
		}
	}
	return CombatEnemyAction{}, fmt.Errorf("official enemy level %d action %d is missing", level.ID, skillID)
}

func validateEnemyDrainGrowthAction(catalog *CombatCatalog) error {
	const levelID = 83005081
	const skillID = 36601603
	drain, err := enemyAttackOptionRole(catalog, skillID, "ATK_OP_DRAIN")
	if err != nil {
		return err
	}
	attack, err := enemyAttackOptionRole(catalog, skillID, "ATTACK_AA")
	if err != nil {
		return err
	}
	piercing, err := enemyAttackOptionRole(catalog, skillID, "ATK_OP_PIERCING")
	if err != nil {
		return err
	}
	if drain.RoleIndex != 1 || drain.Target != "SELECT" || drain.Parameters[0] != "1000" ||
		drain.Parameters[1] != "10000" || drain.Parameters[2] != "0" ||
		attack.RoleIndex != 2 || attack.Target != "SELECT" || attack.Parameters[0] != "40000" ||
		attack.Parameters[1] != "0" || attack.Parameters[2] != "500" || attack.Parameters[3] != "0" ||
		attack.Parameters[4] != "1" || attack.Parameters[5] != "ATK" || attack.Parameters[6] != "0" ||
		attack.Parameters[7] != "LIGHT" || attack.Parameters[8] != "PHYSICS" ||
		piercing.RoleIndex != 0 || piercing.Parameters[0] != "80" || piercing.Parameters[1] != "0" {
		return fmt.Errorf("official enemy DRAIN 36601603 roles changed: piercing=%+v drain=%+v attack=%+v", piercing, drain, attack)
	}
	if zero, one := attackOperatorLevelValue(drain, 0, 1), attackOperatorLevelValue(drain, 1, 1); zero != 1000 || one != 11000 || calibratedEnemySkillLevel(drain.Function) != 1 {
		return fmt.Errorf("native enemy DRAIN level values are %d/%d with calibrated level %d, want 1000/11000/1", zero, one, calibratedEnemySkillLevel(drain.Function))
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 55000000 || definition.Attack != 10000 || definition.Magic != 5000 ||
		definition.Recovery != 5000 || definition.Defense != 200000 || definition.MagicDefense != 200000 || definition.Attribute != "LIGHT" {
		return fmt.Errorf("official enemy DRAIN definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy DRAIN level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 3 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 12 ||
		action.Target != "HIGH_HP_USER" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy DRAIN action changed: %+v", action)
	}
	actorProbe := playerFromEnemy(&battleEnemy{Attack: definition.Attack, HP: 1, MaxHP: definition.HP})
	oldModifiers := collectAttackModifiers([]CombatSkillRole{drain}, actorProbe, 0, 1)
	newModifiers := collectEnemyAttackModifiers([]CombatSkillRole{drain}, actorProbe, 1)
	if oldModifiers.drainRate != 1000 || newModifiers.drainRate != 11000 || newModifiers.drainRoleIndex != 1 {
		return fmt.Errorf("official enemy DRAIN modifier boundary is old=%+v managed=%+v", oldModifiers, newModifiers)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	playerHP := [4]int{100001, 100002, 100004, 100003}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, HP: playerHP[index], MaxHP: playerHP[index], Attribute: "NEUTRAL"}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: 1, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 4 || engine.players[2].HP != 55004 || engine.players[2].DamageTaken != 45000 ||
		engine.enemies[0].HP != 4950001 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 3, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{3, 2, -45000, 100004, 0, 4, 100, 0, 0, 5}) ||
		!equalBattleArgs(results[2].Args, []int64{5, 2, 4950000, 4950001}) ||
		!equalBattleArgs(results[3].Args, []int64{3, 100004, 55004, 1}) {
		return fmt.Errorf("official enemy DRAIN level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	return nil
}

func validateEnemyRevengeGrowthAction(catalog *CombatCatalog) error {
	const levelID = 30880123
	const skillID = 35601113
	revenge, err := enemyAttackOptionRole(catalog, skillID, "ATK_OP_REVENGE")
	if err != nil {
		return err
	}
	attack, err := enemyAttackOptionRole(catalog, skillID, "ATTACK_AA")
	if err != nil {
		return err
	}
	if revenge.RoleIndex != 0 || revenge.Target != "SELF" || revenge.Parameters[0] != "30" ||
		revenge.Parameters[1] != "1" || revenge.Parameters[2] != "ATK" || revenge.Parameters[3] != "30" ||
		attack.RoleIndex != 1 || attack.Target != "SELECT" || attack.Parameters[0] != "1000" ||
		attack.Parameters[1] != "0" || attack.Parameters[2] != "1000" || attack.Parameters[3] != "0" ||
		attack.Parameters[4] != "1" || attack.Parameters[5] != "ATK" || attack.Parameters[6] != "0" ||
		attack.Parameters[7] != "WIND" || attack.Parameters[8] != "PHYSICS" {
		return fmt.Errorf("official enemy REVENGE 35601113 roles changed: revenge=%+v attack=%+v", revenge, attack)
	}
	if zero, one := attackOperatorLevelValue(revenge, 0, 1), attackOperatorLevelValue(revenge, 1, 1); zero != 30 || one != 31 || calibratedEnemySkillLevel(revenge.Function) != 1 {
		return fmt.Errorf("native enemy REVENGE level values are %d/%d with calibrated level %d, want 30/31/1", zero, one, calibratedEnemySkillLevel(revenge.Function))
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 50000 || definition.Attack != 1000 || definition.Magic != 750 ||
		definition.Recovery != 0 || definition.Defense != 0 || definition.MagicDefense != 0 || definition.Attribute != "WIND" {
		return fmt.Errorf("official enemy REVENGE definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy REVENGE level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 1 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 13 ||
		action.Target != "RANDOM" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy REVENGE action changed: %+v", action)
	}
	actorProbe := playerFromEnemy(&battleEnemy{Attack: definition.Attack, HP: definition.HP, MaxHP: definition.HP})
	oldModifiers := collectAttackModifiers([]CombatSkillRole{revenge}, actorProbe, 0, 1)
	newModifiers := collectEnemyAttackModifiers([]CombatSkillRole{revenge}, actorProbe, 1)
	if oldModifiers.revengeRate != 30 || newModifiers.revengeRate != 31 ||
		newModifiers.revengeParameter != "ATK" || newModifiers.revengeCapRate != 30 ||
		attackRevengeBonus(100, actorProbe, oldModifiers.revengeRate, oldModifiers.revengeParameter, oldModifiers.revengeCapRate) != 30 ||
		attackRevengeBonus(100, actorProbe, newModifiers.revengeRate, newModifiers.revengeParameter, newModifiers.revengeCapRate) != 31 {
		return fmt.Errorf("official enemy REVENGE modifier boundary is old=%+v managed=%+v", oldModifiers, newModifiers)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, HP: 100000, MaxHP: 100000, Attribute: "NEUTRAL"}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		DamageTaken: 100, Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 3 || engine.players[3].HP != 97969 || engine.players[3].DamageTaken != 2031 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{4, 1, -2031, 100000, 0, 3, 100, 0, 0, 5}) ||
		!equalBattleArgs(results[2].Args, []int64{4, 100000, 97969, 1}) {
		return fmt.Errorf("official enemy REVENGE level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	return nil
}
