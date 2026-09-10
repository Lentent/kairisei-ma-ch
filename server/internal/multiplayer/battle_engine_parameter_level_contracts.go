package multiplayer

import (
	"errors"
	"fmt"
)

type enemySelfParameterMatrix struct {
	count             int
	targets           map[string]int
	targetParameters  map[string]int
	sources           map[string]int
	coefficientGrowth map[string]int
	fixedGrowth       map[string]int
	caps              map[string]int
	effects           map[string]int
	hate              map[int]int
}

// validateEnemySelfParameterLevelContracts owns the ordinary-enemy level-one
// proof for native roles 17/23/30. FUN_000a1618/FUN_000a12e9/FUN_000a0c8b
// all cap the acting member's p2 source by positive p6, multiply it by
// p3+p4*level per mille, and add the independent p5*level segment. Managed
// EnemyLevelupDataContainer supplies level 1 to the same skill object consumed
// by these producers.
func validateEnemySelfParameterLevelContracts(catalog *CombatCatalog) error {
	expected := map[string]enemySelfParameterMatrix{
		"ATK_UP_BY_SELF_PARAM": {
			count:             1875,
			targets:           map[string]int{"ENEMY_ALL": 175, "SELECT": 1282, "SELF": 418},
			targetParameters:  map[string]int{"ATK": 893, "DEF": 1, "INT": 974, "MAX_HP": 3, "MDEF": 1, "MND": 3},
			sources:           map[string]int{"MND": 1875},
			coefficientGrowth: map[string]int{"": 102, "0": 1753, "1": 20},
			fixedGrowth:       map[string]int{"": 103, "0": 1768, "INT": 4},
			caps:              map[string]int{"": 1847, "0": 28},
			effects: map[string]int{
				"": 973, "enemy_skill_attack_0a": 3, "enemy_skill_attack_buff_0a": 18,
				"enemy_skill_attack_buff_0a_noname": 1, "enemy_skill_buff": 799,
				"enemy_skill_buff_debuff": 17, "enemy_skill_buff_noname": 11,
				"enemy_skill_debuff": 6, "enemy_skill_magic_buff_0a": 14,
				"enemy_skill_player_buff2_buff2": 32, "enemy_skill_text": 1,
			},
			hate: map[int]int{0: 1763, 10000: 112},
		},
		"DEF_UP_BY_SELF_PARAM": {
			count:             555,
			targets:           map[string]int{"ENEMY_ALL": 30, "SELECT": 494, "SELF": 31},
			targetParameters:  map[string]int{"DEF": 294, "MDEF": 261},
			sources:           map[string]int{"MND": 555},
			coefficientGrowth: map[string]int{"": 46, "0": 503, "1": 6},
			fixedGrowth:       map[string]int{"": 46, "0": 509},
			caps:              map[string]int{"": 531, "0": 24},
			effects: map[string]int{
				"": 274, "enemy_skill_attack_buff_0a": 1, "enemy_skill_buff": 255,
				"enemy_skill_buff_noname": 3, "enemy_skill_magic_buff_0a": 7,
				"enemy_skill_player_buff2_buff2": 15,
			},
			hate: map[int]int{0: 511, 10000: 44},
		},
		"GUARD_BREAK_BY_SELF_PARAM": {
			count:             1185,
			targets:           map[string]int{"FRIEND_ALL": 59, "SELECT": 1118, "SELF": 8},
			targetParameters:  map[string]int{"DEF": 585, "MDEF": 600},
			sources:           map[string]int{"MND": 1185},
			coefficientGrowth: map[string]int{"": 7, "0": 1175, "1": 3},
			fixedGrowth:       map[string]int{"": 7, "0": 1178},
			caps:              map[string]int{"": 1179, "0": 6},
			effects: map[string]int{
				"": 927, "enemy_skill_attack_debuff_0a": 14,
				"enemy_skill_attack_debuff_0a_noname": 4, "enemy_skill_debuff": 189,
				"enemy_skill_debuff_noname": 27, "enemy_skill_magic_debuff_0a": 22,
				"enemy_skill_magic_debuff_0a_noname": 2,
			},
			hate: map[int]int{0: 1185},
		},
	}

	type observedMatrix struct {
		count             int
		targets           map[string]int
		targetParameters  map[string]int
		sources           map[string]int
		coefficientGrowth map[string]int
		fixedGrowth       map[string]int
		caps              map[string]int
		effects           map[string]int
		hate              map[int]int
	}
	observed := make(map[string]*observedMatrix, len(expected))
	for function := range expected {
		observed[function] = &observedMatrix{
			targets: make(map[string]int), targetParameters: make(map[string]int), sources: make(map[string]int),
			coefficientGrowth: make(map[string]int), fixedGrowth: make(map[string]int), caps: make(map[string]int),
			effects: make(map[string]int), hate: make(map[int]int),
		}
	}

	growthRoleIDs := make(map[int]bool)
	legacyTailIDs := make(map[int]bool)
	for _, roles := range catalog.EnemySkillRoles {
		for index := range roles {
			role := &roles[index]
			matrix := observed[role.Function]
			if matrix == nil {
				continue
			}
			matrix.count++
			matrix.targets[role.Target]++
			matrix.targetParameters[role.Parameters[1]]++
			matrix.sources[role.Parameters[2]]++
			matrix.coefficientGrowth[role.Parameters[4]]++
			matrix.fixedGrowth[role.Parameters[5]]++
			matrix.caps[role.Parameters[6]]++
			matrix.effects[role.Effect2D]++
			matrix.hate[role.HateLimit]++
			if combatParameterInt(role.Parameters[4]) != 0 || combatParameterInt(role.Parameters[5]) != 0 {
				growthRoleIDs[role.SkillID] = true
			}
			if role.ExcludeSelf || role.ChainRate != 0 || role.Parameters[9] != "" {
				return fmt.Errorf("official enemy self-parameter shape changed: %+v", *role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy self-parameter skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			if role.Parameters[7] != "" || role.Parameters[8] != "" {
				if role.Function != "ATK_UP_BY_SELF_PARAM" || role.Parameters[5] != "INT" ||
					role.Parameters[7] != "ICE" || role.Parameters[8] != "MAGIC" {
					return fmt.Errorf("official enemy self-parameter legacy tail changed: %+v", *role)
				}
				legacyTailIDs[role.SkillID] = true
			}
		}
	}

	for function, want := range expected {
		got := observed[function]
		if got.count != want.count || !equalStringCountMap(got.targets, want.targets) ||
			!equalStringCountMap(got.targetParameters, want.targetParameters) || !equalStringCountMap(got.sources, want.sources) ||
			!equalStringCountMap(got.coefficientGrowth, want.coefficientGrowth) || !equalStringCountMap(got.fixedGrowth, want.fixedGrowth) ||
			!equalStringCountMap(got.caps, want.caps) || !equalStringCountMap(got.effects, want.effects) || !equalIntCountMap(got.hate, want.hate) {
			return fmt.Errorf("official enemy %s matrix changed: got=%+v want=%+v", function, *got, want)
		}
	}
	wantLegacy := map[int]bool{30701115: true, 30701120: true, 30801115: true, 30801120: true}
	if !equalIntBoolMap(legacyTailIDs, wantLegacy) {
		return fmt.Errorf("official enemy self-parameter legacy tails changed: %v", legacyTailIDs)
	}
	if len(growthRoleIDs) != 29 {
		return fmt.Errorf("official enemy self-parameter growth-role count is %d, want 29", len(growthRoleIDs))
	}

	growthVariantCount := 0
	growthSkillIDs := make(map[int]bool)
	for skillID, variants := range catalog.EnemySkills {
		for _, variant := range variants {
			if growthRoleIDs[variant.FunctionID] {
				growthVariantCount++
				growthSkillIDs[skillID] = true
			}
		}
	}
	actionReferenceCount := 0
	referencingLevels := make(map[int]bool)
	for levelID, level := range catalog.EnemyLevels {
		for _, action := range level.Actions {
			if growthSkillIDs[action.SkillID] {
				actionReferenceCount++
				referencingLevels[levelID] = true
			}
		}
	}
	if growthVariantCount != 29 || len(growthSkillIDs) != 29 || actionReferenceCount != 88 || len(referencingLevels) != 52 {
		return fmt.Errorf("official enemy self-parameter reachability changed: variants=%d skills=%d action_refs=%d levels=%d", growthVariantCount, len(growthSkillIDs), actionReferenceCount, len(referencingLevels))
	}

	roles := catalog.EnemySkillRoles[44169204]
	if len(roles) != 2 || roles[0].Function != "ATTACK_AA" || roles[1].Function != "ATK_UP_BY_SELF_PARAM" {
		return fmt.Errorf("official enemy self-parameter representative roles changed: %+v", roles)
	}
	representative := roles[1]
	if representative.RoleIndex != 1 || representative.Target != "SELF" || representative.Parameters[0] != "99" ||
		representative.Parameters[1] != "ATK" || representative.Parameters[2] != "MND" ||
		representative.Parameters[3] != "1000" || representative.Parameters[4] != "1" ||
		representative.Parameters[5] != "0" || representative.Parameters[6] != "0" ||
		calibratedEnemySkillLevel(representative.Function) != 1 {
		return fmt.Errorf("official enemy self-parameter representative changed: %+v", representative)
	}
	if value := selfScaledParameterValue(representative, 0, 1, 1500); value != 1500 {
		return fmt.Errorf("official enemy self-parameter level-zero control is %d, want 1500", value)
	}
	if value := selfScaledParameterValue(representative, 1, 1, 1500); value != 1501 {
		return fmt.Errorf("official enemy self-parameter level-one value is %d, want 1501", value)
	}

	definition, ok := catalog.Enemies[40017131]
	if !ok || definition.HP != 420000 || definition.Attack != 2000 || definition.Magic != 0 ||
		definition.Recovery != 1500 || definition.Defense != 0 || definition.MagicDefense != 0 || definition.Attribute != "LIGHT" {
		return fmt.Errorf("official enemy self-parameter definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[40017131]
	if !ok {
		return errors.New("official enemy self-parameter level 40017131 is missing")
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == 44169204 {
			action = candidate
			break
		}
	}
	if level.HPBars != 4 || level.ActionsPerTurn != 3 || action.Slot != 4 || action.Category != "skill" ||
		action.AIConditionID != 40167202 || action.Priority != 4 || action.Target != "LOW_HP_USER" ||
		action.ActionCost != 3 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy self-parameter action changed: level=%+v action=%+v", level, action)
	}

	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 20000, MaxHP: 20000}
	}
	engine.players[0].HP = 10000
	engine.players[0].MaxHP = 10000
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute, Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 5 || engine.players[0].HP != 8000 || engine.enemies[0].Attack != 3501 ||
		!equalBattleArgs(results[0].Args, []int64{5, 44169204, 1, 1, 1, 44169204, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{1, 0, -2000, 10000, 0, 4, 100, 0, 0, 5}) ||
		!equalBattleArgs(results[2].Args, []int64{1, 10000, 8000, 1}) ||
		!equalBattleArgs(results[3].Args, []int64{5, 1, 0, 1, 1, 4, 1, 0, 0, 0, 0}) ||
		!equalBattleArgs(results[4].Args, []int64{5, 420000, 420000, 3501, 0, 1500, 0, 0, 99999, 99999, 99999}) {
		return fmt.Errorf("official enemy self-parameter level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	return nil
}

func equalStringCountMap(left map[string]int, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalIntCountMap(left map[int]int, right map[int]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalIntBoolMap(left map[int]bool, right map[int]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
