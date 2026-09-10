package multiplayer

import (
	"errors"
	"fmt"
)

// validateEnemyAttackLevelContracts binds ordinary-enemy ATTACK_AA to the
// managed level-one skill object and native producer FUN_000a45d2. Only two
// active enemy rows have a nonzero fixed-level coefficient, and integer
// division makes their level-zero and level-one output equal. The exact
// object graph and a larger structural probe therefore guard the level path
// without inventing a corrected live damage difference.
func validateEnemyAttackLevelContracts(catalog *CombatCatalog) error {
	count := 0
	targets := make(map[string]int)
	sources := make(map[string]int)
	attributes := make(map[string]int)
	physics := make(map[string]int)
	fixedGrowth := make(map[string]int)
	sourceGrowth := make(map[string]int)
	hits := make(map[string]int)
	criticalRates := make(map[string]int)
	chainRates := make(map[string]int)
	hateLimits := make(map[string]int)
	growthRoleIDs := make(map[int]bool)

	for _, roles := range catalog.EnemySkillRoles {
		for index := range roles {
			role := &roles[index]
			if role.Function != "ATTACK_AA" {
				continue
			}
			count++
			targets[role.Target]++
			sources[role.Parameters[5]]++
			attributes[role.Parameters[7]]++
			physics[role.Parameters[8]]++
			fixedGrowth[role.Parameters[1]]++
			sourceGrowth[role.Parameters[3]]++
			hits[role.Parameters[4]]++
			criticalRates[role.Parameters[6]]++
			chainRates[fmt.Sprint(role.ChainRate)]++
			hateLimits[fmt.Sprint(role.HateLimit)]++
			if combatParameterInt(role.Parameters[1]) != 0 || combatParameterInt(role.Parameters[3]) != 0 {
				growthRoleIDs[role.SkillID] = true
			}
			if role.ExcludeSelf || role.Parameters[9] != "" || combatParameterInt(role.Parameters[0]) < 0 ||
				combatParameterInt(role.Parameters[2]) < 0 || combatParameterInt(role.Parameters[4]) < 1 {
				return fmt.Errorf("official enemy ATTACK_AA shape changed: %+v", *role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy ATTACK_AA skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
		}
	}
	if count != 10765 ||
		!equalStringCountMap(targets, map[string]int{"FRIEND_ALL": 165, "SELECT": 10600}) ||
		!equalStringCountMap(sources, map[string]int{"ATK": 5560, "HP": 145, "INT": 5060}) ||
		!equalStringCountMap(attributes, map[string]int{"DARK": 1859, "FIRE": 2552, "ICE": 2127, "LIGHT": 2305, "WIND": 1922}) ||
		!equalStringCountMap(physics, map[string]int{"MAGIC": 5065, "PHYSICS": 5700}) ||
		!equalStringCountMap(fixedGrowth, map[string]int{"0": 10763, "5": 2}) ||
		!equalStringCountMap(sourceGrowth, map[string]int{"0": 10765}) ||
		!equalStringCountMap(hits, map[string]int{"1": 10252, "2": 340, "3": 159, "4": 14}) ||
		!equalStringCountMap(criticalRates, map[string]int{"": 6, "0": 10753, "50": 6}) ||
		!equalStringCountMap(chainRates, map[string]int{"0": 10765}) ||
		!equalStringCountMap(hateLimits, map[string]int{"0": 10435, "10000": 330}) {
		return fmt.Errorf("official enemy ATTACK_AA matrix changed: count=%d targets=%v sources=%v attributes=%v physics=%v fixed_growth=%v source_growth=%v hits=%v critical=%v chain=%v hate=%v", count, targets, sources, attributes, physics, fixedGrowth, sourceGrowth, hits, criticalRates, chainRates, hateLimits)
	}
	if !equalIntBoolMap(growthRoleIDs, map[int]bool{44202203: true, 44325402: true}) {
		return fmt.Errorf("official enemy ATTACK_AA growth roles changed: %v", growthRoleIDs)
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
	if variantCount != 2 || !equalIntBoolMap(growthSkillIDs, map[int]bool{44202203: true, 44325402: true}) ||
		actionRefs != 2 || !equalIntBoolMap(referencingLevels, map[int]bool{40021451: true, 40032551: true}) {
		return fmt.Errorf("official enemy ATTACK_AA reachability changed: variants=%d skills=%v refs=%d levels=%v", variantCount, growthSkillIDs, actionRefs, referencingLevels)
	}

	physicalRole, err := enemyAttackGrowthRole(catalog, 44202203, "ATK", "ICE", "PHYSICS")
	if err != nil {
		return err
	}
	magicRole, err := enemyAttackGrowthRole(catalog, 44325402, "INT", "LIGHT", "MAGIC")
	if err != nil {
		return err
	}
	if calibratedEnemySkillLevel(physicalRole.Function) != 1 || calibratedEnemySkillLevel(magicRole.Function) != 1 {
		return errors.New("official enemy ATTACK_AA is not bound to managed level one")
	}
	if zero, one, structural := attackRolePower(physicalRole, 0, 10000), attackRolePower(physicalRole, 1, 10000), attackRolePower(physicalRole, 200, 10000); zero != 33000 || one != 33000 || structural != 33001 {
		return fmt.Errorf("native physical ATTACK_AA level values are %d/%d/%d, want 33000/33000/33001", zero, one, structural)
	}
	sourceProbe := physicalRole
	sourceProbe.Parameters[3] = "1"
	if zero, one := attackRolePower(sourceProbe, 0, 10000), attackRolePower(sourceProbe, 1, 10000); zero != 33000 || one != 33010 {
		return fmt.Errorf("native ATTACK_AA source-growth structural probe is %d/%d, want 33000/33010", zero, one)
	}

	if err := validateEnemyAttackGrowthAction(catalog, 40021451, 44202203, 40200203, 7, physicalRole, 10000, 0, 1000, 2000, 32000, 4); err != nil {
		return err
	}
	if err := validateEnemyAttackGrowthAction(catalog, 40032551, 44325402, 40325402, 5, magicRole, 0, 10000, 1000, 2000, 31000, 4); err != nil {
		return err
	}
	return nil
}

func enemyAttackGrowthRole(catalog *CombatCatalog, skillID int, source string, attribute string, physics string) (CombatSkillRole, error) {
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 1 || roles[0].Function != "ATTACK_AA" {
		return CombatSkillRole{}, fmt.Errorf("official enemy ATTACK_AA growth role %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "23000" || role.Parameters[1] != "5" ||
		role.Parameters[2] != "1000" || role.Parameters[3] != "0" || role.Parameters[4] != "1" ||
		role.Parameters[5] != source || role.Parameters[6] != "0" || role.Parameters[7] != attribute || role.Parameters[8] != physics {
		return CombatSkillRole{}, fmt.Errorf("official enemy ATTACK_AA growth role %d parameters changed: %+v", skillID, role)
	}
	return role, nil
}

func validateEnemyAttackGrowthAction(catalog *CombatCatalog, levelID int, skillID int, conditionID int, actionCost int, role CombatSkillRole, attack int, magic int, defense int, magicDefense int, expectedDamage int, expectedTarget int) error {
	definition, ok := catalog.Enemies[levelID]
	if !ok {
		return fmt.Errorf("official enemy ATTACK_AA definition %d is missing", levelID)
	}
	if definition.Attack != attack || definition.Magic != magic {
		return fmt.Errorf("official enemy ATTACK_AA definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy ATTACK_AA level %d is missing", levelID)
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == skillID {
			action = candidate
			break
		}
	}
	if action.Slot != 2 || action.Category != "skill" || action.AIConditionID != conditionID || action.Priority != 2 ||
		action.Target != "RANDOM" || action.ActionCost != actionCost || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy ATTACK_AA action %d changed: level=%+v action=%+v", skillID, level, action)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1, HP: 100000, MaxHP: 100000, Attribute: "NEUTRAL",
			Defense: defense, MDefense: magicDefense,
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
	if len(results) != 9 || !equalBattleArgs(results[0].Args, []int64{5, int64(skillID), int64(expectedTarget), 1, 2, int64(skillID), 0}) {
		return fmt.Errorf("official enemy ATTACK_AA all-target header %d is %+v", skillID, results)
	}
	for index := range engine.players {
		target := &engine.players[index]
		damage, hp := results[1+index*2], results[2+index*2]
		if target.HP != 100000-expectedDamage || target.DamageTaken != expectedDamage ||
			damage.Command != 60 || damage.Args[0] != int64(index+1) || damage.Args[1] != int64(role.RoleIndex) ||
			damage.Args[2] != int64(-expectedDamage) || damage.Args[3] != 100000 || damage.Args[7] != 0 || damage.Args[9] != 5 ||
			!equalBattleArgs(hp.Args, []int64{int64(index + 1), 100000, int64(100000 - expectedDamage), 1}) {
			return fmt.Errorf("official enemy ATTACK_AA action %d is results=%+v target=%+v", skillID, results, *target)
		}
	}
	return nil
}
