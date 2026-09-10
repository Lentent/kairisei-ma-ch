package multiplayer

import (
	"errors"
	"fmt"
)

// validateEnemyReviveLevelContracts separates the full official role-57
// matrix and its ordinary-skill level from the broader death lifecycle audit.
// The 24 p2-growth rows are a strict subset of 30 p1-growth rows; all start at
// 1000 per mille, so the MaxHP clamp hides the old column/level mistake unless
// the producer segments are checked independently.
func validateEnemyReviveLevelContracts(catalog *CombatCatalog) error {
	count := 0
	targets := make(map[string]int)
	effects := make(map[string]int)
	rates := make(map[string]int)
	rateGrowth := make(map[string]int)
	fixedGrowth := make(map[string]int)
	legacyP4 := make(map[string]int)
	growthRoleIDs := make(map[int]bool)
	fixedGrowthRoleIDs := make(map[int]bool)
	for _, roles := range catalog.EnemySkillRoles {
		for index := range roles {
			role := &roles[index]
			if role.Function != "REVIVE" {
				continue
			}
			count++
			targets[role.Target]++
			effects[role.Effect2D]++
			rates[role.Parameters[0]]++
			rateGrowth[role.Parameters[1]]++
			fixedGrowth[role.Parameters[2]]++
			legacyP4[role.Parameters[3]]++
			if combatParameterInt(role.Parameters[1]) != 0 || combatParameterInt(role.Parameters[2]) != 0 {
				growthRoleIDs[role.SkillID] = true
			}
			if combatParameterInt(role.Parameters[2]) != 0 {
				fixedGrowthRoleIDs[role.SkillID] = true
			}
			if role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 {
				return fmt.Errorf("official enemy REVIVE shape changed: %+v", *role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy REVIVE skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			for parameter := 4; parameter < len(role.Parameters); parameter++ {
				if role.Parameters[parameter] != "" {
					return fmt.Errorf("official enemy REVIVE skill %d has unexpected p%d=%q", role.SkillID, parameter+1, role.Parameters[parameter])
				}
			}
		}
	}
	if count != 233 || !equalStringCountMap(targets, map[string]int{"SELECT": 233}) ||
		!equalStringCountMap(effects, map[string]int{"": 144, "enemy_skill_recovery_buff_0a": 18, "enemy_skill_revive": 67, "enemy_skill_revive_buff": 4}) ||
		!equalStringCountMap(rates, map[string]int{"600": 28, "800": 4, "1000": 201}) ||
		!equalStringCountMap(rateGrowth, map[string]int{"0": 203, "1000": 30}) ||
		!equalStringCountMap(fixedGrowth, map[string]int{"0": 209, "1": 24}) ||
		!equalStringCountMap(legacyP4, map[string]int{"": 135, "0": 98}) {
		return fmt.Errorf("official enemy REVIVE matrix changed: count=%d targets=%v effects=%v rates=%v rate_growth=%v fixed_growth=%v p4=%v", count, targets, effects, rates, rateGrowth, fixedGrowth, legacyP4)
	}
	if len(growthRoleIDs) != 30 || len(fixedGrowthRoleIDs) != 24 {
		return fmt.Errorf("official enemy REVIVE growth counts changed: rate_or_fixed=%d fixed=%d", len(growthRoleIDs), len(fixedGrowthRoleIDs))
	}

	type reachability struct {
		variants int
		skills   map[int]bool
		refs     int
		levels   map[int]bool
	}
	collectReachability := func(roleIDs map[int]bool) reachability {
		result := reachability{skills: make(map[int]bool), levels: make(map[int]bool)}
		for skillID, variants := range catalog.EnemySkills {
			for _, variant := range variants {
				if roleIDs[variant.FunctionID] {
					result.variants++
					result.skills[skillID] = true
				}
			}
		}
		for levelID, level := range catalog.EnemyLevels {
			for _, action := range level.Actions {
				if result.skills[action.SkillID] {
					result.refs++
					result.levels[levelID] = true
				}
			}
		}
		return result
	}
	growthReach := collectReachability(growthRoleIDs)
	fixedReach := collectReachability(fixedGrowthRoleIDs)
	if growthReach.variants != 30 || len(growthReach.skills) != 30 || growthReach.refs != 36 || len(growthReach.levels) != 31 ||
		fixedReach.variants != 24 || len(fixedReach.skills) != 24 || fixedReach.refs != 28 || len(fixedReach.levels) != 25 {
		return fmt.Errorf("official enemy REVIVE reachability changed: growth=%+v fixed=%+v", growthReach, fixedReach)
	}

	roles := catalog.EnemySkillRoles[44168001]
	if len(roles) != 1 || roles[0].Function != "REVIVE" {
		return fmt.Errorf("official enemy REVIVE representative roles changed: %+v", roles)
	}
	representative := roles[0]
	if representative.RoleIndex != 0 || representative.Target != "SELECT" || representative.Parameters[0] != "1000" ||
		representative.Parameters[1] != "1000" || representative.Parameters[2] != "1" ||
		calibratedEnemySkillLevel(representative.Function) != 1 {
		return fmt.Errorf("official enemy REVIVE representative changed: %+v", representative)
	}
	if value := reviveRoleValue(representative, 0, 9000); value != 9000 {
		return fmt.Errorf("official enemy REVIVE level-zero control is %d, want 9000", value)
	}
	if value := reviveRoleValue(representative, 1, 9000); value != 9000 {
		return fmt.Errorf("official enemy REVIVE level-one clamped value is %d, want 9000", value)
	}
	segmentProbe := representative
	segmentProbe.Parameters[0] = "600"
	segmentProbe.Parameters[1] = "25"
	segmentProbe.Parameters[2] = "1"
	if zero, one := reviveRoleValue(segmentProbe, 0, 1000), reviveRoleValue(segmentProbe, 1, 1000); zero != 600 || one != 626 {
		return fmt.Errorf("native enemy REVIVE segment probe is level0=%d level1=%d, want 600/626", zero, one)
	}

	actorDefinition, ok := catalog.Enemies[40017011]
	if !ok || actorDefinition.HP != 80000 || actorDefinition.Attack != 0 || actorDefinition.Magic != 1000 ||
		actorDefinition.Recovery != 100 || actorDefinition.Attribute != "WIND" {
		return fmt.Errorf("official enemy REVIVE actor definition changed: %+v", actorDefinition)
	}
	targetDefinition, ok := catalog.Enemies[40017012]
	if !ok || targetDefinition.HP != 9000 || targetDefinition.Recovery != 500 || targetDefinition.Attribute != "WIND" {
		return fmt.Errorf("official enemy REVIVE target definition changed: %+v", targetDefinition)
	}
	level, ok := catalog.EnemyLevels[40017011]
	if !ok {
		return errors.New("official enemy REVIVE level 40017011 is missing")
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == 44168001 {
			action = candidate
			break
		}
	}
	if level.HPBars != 2 || level.ActionsPerTurn != 4 || action.Slot != 1 || action.Category != "skill" ||
		action.AIConditionID != 40166001 || action.Priority != 1 || action.Target != "ENEMY2" ||
		action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy REVIVE action changed: level=%+v action=%+v", level, action)
	}

	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 2}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: actorDefinition.ID, HP: actorDefinition.HP, MaxHP: actorDefinition.HP,
		Attack: actorDefinition.Attack, Magic: actorDefinition.Magic, Recovery: actorDefinition.Recovery,
		Defense: actorDefinition.Defense, MDefense: actorDefinition.MagicDefense, Attribute: actorDefinition.Attribute,
		Level: level,
	}
	engine.enemies[1] = battleEnemy{BaseAttribute: targetDefinition.Attribute, Attribute: targetDefinition.Attribute,
		MemberType: 6, EnemyID: targetDefinition.ID, HP: 0, MaxHP: targetDefinition.HP,
		Broken: true, DeathActionTriggered: true, DiedTurn: 2, DeathCount: 1,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	target := &engine.enemies[1]
	if len(results) != 2 || target.HP != 9000 || target.Broken || target.DeathActionTriggered || target.DiedTurn != 0 || target.DeathCount != 1 ||
		!equalBattleArgs(results[0].Args, []int64{5, 44168001, 6, 1, 5, 44168001, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{6, 0, 9000, 9000}) {
		return fmt.Errorf("official enemy REVIVE level-one action is results=%+v target=%+v", results, *target)
	}
	return nil
}
