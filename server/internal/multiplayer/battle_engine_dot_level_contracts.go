package multiplayer

import (
	"fmt"
	"strings"
)

// validateEnemyDOTLevelContracts binds the five common retained-damage
// producers to managed enemy skill level one. Their resistance, replacement,
// projection and tick lifecycle is guarded elsewhere; this contract owns the
// complete active producer matrix and the growth-role reachability chain.
func validateEnemyDOTLevelContracts(catalog *CombatCatalog) error {
	wanted := map[string]int{
		"BURN": 69, "POISON": 245, "FREEZE": 45, "BLEED": 84, "ELECTRIC": 33,
	}
	wantedGrowth := map[string]int{
		"BURN": 0, "POISON": 3, "FREEZE": 6, "BLEED": 0, "ELECTRIC": 19,
	}
	rows := make([]CombatSkillRole, 0, 476)
	counts := make(map[string]int)
	growthCounts := make(map[string]int)
	for function := range wanted {
		counts[function] = 0
		growthCounts[function] = 0
	}
	growthRoleIDs := make(map[int]bool)
	for _, roleSet := range catalog.EnemySkillRoles {
		for _, role := range roleSet {
			if _, ok := wanted[role.Function]; !ok {
				continue
			}
			rows = append(rows, role)
			counts[role.Function]++
			if role.Target != "SELECT" && role.Target != "FRIEND_ALL" {
				return fmt.Errorf("official enemy %s target changed: %+v", role.Function, role)
			}
			if role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 {
				return fmt.Errorf("official enemy %s DOT tail changed: %+v", role.Function, role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy %s skill %d disabled attribute %d", role.Function, role.SkillID, attribute)
				}
			}
			if combatParameterInt(role.Parameters[2]) != 0 || combatParameterInt(role.Parameters[4]) != 0 || combatParameterInt(role.Parameters[6]) != 0 {
				growthCounts[role.Function]++
				growthRoleIDs[role.SkillID] = true
			}
		}
	}
	if len(rows) != 476 || len(growthRoleIDs) != 28 || !equalStringCountMap(counts, wanted) || !equalStringCountMap(growthCounts, wantedGrowth) {
		return fmt.Errorf("official enemy DOT matrix changed: rows=%d growth=%d counts=%v growth_counts=%v", len(rows), len(growthRoleIDs), counts, growthCounts)
	}
	if digest := combatRoleMatrixDigest(rows); digest != "2ff768541ffba5febd9f6e43f5c8f8807904a57d6b1663354835550e455ff217" {
		return fmt.Errorf("official enemy DOT row digest is %s", digest)
	}
	if digest := combatIntSetDigest(growthRoleIDs); digest != "48464fdc879d0876adc8ad55a023fc92734512910b756c11dc72e56ed5ab2ec0" {
		return fmt.Errorf("official enemy DOT growth-role digest is %s", digest)
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
	actionRecords := make([]string, 0, 41)
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
	if variantCount != 28 || len(growthSkillIDs) != 16 || len(actionRecords) != 41 || len(referencingLevels) != 35 {
		return fmt.Errorf("official enemy DOT reachability changed: variants=%d skills=%d refs=%d levels=%d", variantCount, len(growthSkillIDs), len(actionRecords), len(referencingLevels))
	}
	if digest := combatIntSetDigest(growthSkillIDs); digest != "390c5440ddd69bd8d740bfc55e38fdb4cb558d1ce641423cf3f274137d65d931" {
		return fmt.Errorf("official enemy DOT outer-skill digest is %s", digest)
	}
	if digest := combatIntSetDigest(referencingLevels); digest != "48214984c4a4f242bdb36e3ea8f2fee611054df08349306fe2a87d22be377374" {
		return fmt.Errorf("official enemy DOT level digest is %s", digest)
	}
	if digest := combatStringRecordsDigest(actionRecords); digest != "33e3911726beee9715d2fba728837375f24f9cacc0b86438827e4e5e8d51ad0e" {
		return fmt.Errorf("official enemy DOT action digest is %s", digest)
	}
	return validateEnemyDOTGrowthActions(catalog)
}

func validateEnemyDOTGrowthActions(catalog *CombatCatalog) error {
	if err := validateNativeDOTPhaseOrderContract(catalog); err != nil {
		return err
	}
	if err := validateDormantDOTExtensionReachabilityContract(catalog); err != nil {
		return err
	}
	if err := validateEnemyDOTFixedGrowthAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyDOTSourceGrowthAction(catalog); err != nil {
		return err
	}
	return validateEnemyPartDOTParentContract(catalog)
}

// validateDormantDOTExtensionReachabilityContract prevents enum/rule-table
// presence from being mistaken for an active CN producer. CARD_TRAP_DOT is a
// retained-DOT enum/rule identity distinct from the reachable card-bound
// CARD_TRAP_DAMAGE effect. ATTR_CUT_DOT and ATTR_DRAIN_DOT are native support
// consumer identities. None of the three is referenced by the official CN
// player, enemy or support role masters currently loaded by the local profile.
func validateDormantDOTExtensionReachabilityContract(catalog *CombatCatalog) error {
	wantRules := map[string]string{
		"CARD_TRAP_DOT":  "VALUE,VALUE,VALUE,VALUE,VALUE,VALUE,VALUE,SKILL_ROLE_BATTLE_PARAM,,",
		"ATTR_CUT_DOT":   "VALUE,VALUE,VALUE,VALUE,DOT,,,,,",
		"ATTR_DRAIN_DOT": "VALUE,VALUE,VALUE,VALUE,DOT,,,,,",
	}
	for function, want := range wantRules {
		rule, exists := catalog.RoleParamRules[function]
		if !exists {
			return fmt.Errorf("official dormant DOT extension %s lost its parameter rule", function)
		}
		if got := strings.Join(rule.Parameters[:], ","); got != want {
			return fmt.Errorf("official dormant DOT extension %s parameter rule is %q, want %q", function, got, want)
		}
	}

	roleMasters := map[string]map[int][]CombatSkillRole{
		"player":  catalog.PlayerSkillRoles,
		"enemy":   catalog.EnemySkillRoles,
		"support": catalog.SupportSkillRoles,
	}
	for master, roleSets := range roleMasters {
		for skillID, roles := range roleSets {
			for _, role := range roles {
				if _, dormant := wantRules[role.Function]; dormant {
					return fmt.Errorf("official %s skill %d unexpectedly activates dormant DOT extension %s", master, skillID, role.Function)
				}
			}
		}
	}
	return nil
}

// validateNativeDOTPhaseOrderContract binds three independent native facts:
// SKILL_ROLE_DOT is iterated before members, a GUTS sweep follows every DOT
// type, and the damage phase does not itself decrement retained duration.
func validateNativeDOTPhaseOrderContract(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000}
	engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, HP: 100, MaxHP: 100,
		Effects: []battleEffect{
			{Function: "BURN", Value: 100, Remaining: 2, AppliedTurn: 1, Source: 5},
			{Function: "POISON", Value: 100, Remaining: 2, AppliedTurn: 1, Source: 5},
			{Function: "GUTS", Rate: 100, Uses: 1, Remaining: 2, AppliedTurn: 1, Source: 1},
		},
	}
	for index := 1; index < len(engine.players); index++ {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 1, MaxHP: 1}
	}
	results := engine.tickPlayerDOTEffects()
	wantCommands := []int{resultBuffEffect, 60, resultBuffEffect, 61, 200, 72, resultBuffEffect, 60}
	if len(results) != len(wantCommands) {
		return fmt.Errorf("native DOT type-major/GUTS result count is %d, want %d: %+v", len(results), len(wantCommands), results)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("native DOT type-major/GUTS command %d is %d, want %d: %+v", index, results[index].Command, command, results)
		}
	}
	if !equalBattleArgs(results[0].Args, []int64{1, 1, 304}) ||
		!equalBattleArgs(results[1].Args, []int64{1, 0, -100, 0, 0, 0, 100, 0, 0, 0}) ||
		!equalBattleArgs(results[6].Args, []int64{1, 1, 305}) ||
		!equalBattleArgs(results[7].Args, []int64{1, 0, -100, 0, 0, 0, 100, 0, 0, 0}) ||
		engine.players[0].HP != 0 || engine.endType != 0 || len(engine.players[0].Effects) != 2 ||
		engine.players[0].Effects[0].Function != "BURN" || engine.players[0].Effects[0].Remaining != 2 ||
		engine.players[0].Effects[1].Function != "POISON" || engine.players[0].Effects[1].Remaining != 2 {
		return fmt.Errorf("native DOT type-major/GUTS state is player=%+v end=%d results=%+v", engine.players[0], engine.endType, results)
	}
	engine.turn = 2
	lifecycle, err := expireForEffectContract(engine)
	if err != nil {
		return err
	}
	if len(lifecycle) != 0 || engine.players[0].Effects[0].Remaining != 1 || engine.players[0].Effects[1].Remaining != 1 {
		return fmt.Errorf("native DOT next-turn lifetime is results=%+v effects=%+v", lifecycle, engine.players[0].Effects)
	}
	return nil
}

// validateEnemyPartDOTParentContract binds the retained-damage parent lookup
// in native FUN_00060730 to the official 40084001 object graph: 40008411 is
// the body and 40008412 is ParentIndex=1. DOT damage propagates unchanged;
// ENDURE constrains the HP commits independently on the part and body.
func validateEnemyPartDOTParentContract(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, turn: 2, enemyCount: 2}
	engine.enemies[0] = battleEnemy{BaseAttribute: catalog.Enemies[40008411].Attribute, Attribute: catalog.Enemies[40008411].Attribute,
		MemberType: 5, EnemyID: 40008411, HP: 1000, MaxHP: 1000,
		Effects: []battleEffect{{Function: "ENDURE", Value: 1, Remaining: 2, AppliedTurn: 1}},
	}
	engine.enemies[1] = battleEnemy{
		MemberType: 6, EnemyID: 40008412, Parent: 1, HP: 1000, MaxHP: 1000,
		Effects: []battleEffect{
			{Function: "ENDURE", Value: 50, Remaining: 2, AppliedTurn: 1},
			{Function: "BURN", Value: 400, Remaining: 2, AppliedTurn: 1, Source: 1, RoleIndex: 9, Attribute: "FIRE"},
		},
	}
	results, err := engine.tickEnemyDOTEffects()
	if err != nil {
		return err
	}
	if engine.enemies[1].HP != 600 || engine.enemies[0].HP != 600 ||
		engine.enemies[1].DamageTaken != 400 || engine.enemies[0].DamageTaken != 400 ||
		len(results) != 3 || results[0].Command != resultBuffEffect ||
		results[1].Command != 60 || !equalBattleArgs(results[1].Args, []int64{6, 0, -400, 600, 0, 0, 100, 0, 0, 0}) ||
		results[2].Command != resultHP || !equalBattleArgs(results[2].Args, []int64{5, 1000, 600, 1}) {
		return fmt.Errorf("official enemy-part DOT parent propagation is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	return nil
}

func validateEnemyDOTFixedGrowthAction(catalog *CombatCatalog) error {
	const levelID = 31020123
	const skillID = 37301116
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 1 || roles[0].Function != "FREEZE" {
		return fmt.Errorf("official enemy fixed-growth DOT role set %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "2" ||
		role.Parameters[1] != "100" || role.Parameters[2] != "0" || role.Parameters[3] != "150" ||
		role.Parameters[4] != "1000" || role.Parameters[5] != "1200" || role.Parameters[6] != "0" ||
		role.Parameters[7] != "INT" || calibratedEnemySkillLevel(role.Function) != 1 {
		return fmt.Errorf("official enemy fixed-growth FREEZE changed: %+v", role)
	}
	zeroRate, zeroFixed, _, zeroValue := dotRoleValues(role, 0, 1, 550)
	zeroRate, oneFixed, oneCoefficient, oneValue := dotRoleValues(role, 1, 1, 550)
	if zeroRate != 100 || zeroFixed != 150 || zeroValue != 810 || oneFixed != 151 || oneCoefficient != 1200 || oneValue != 811 {
		return fmt.Errorf("native fixed-growth FREEZE values are rate=%d level0=%d/%d level1=%d/%d/%d", zeroRate, zeroFixed, zeroValue, oneFixed, oneCoefficient, oneValue)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 109600 || definition.Attack != 550 || definition.Magic != 550 ||
		definition.Recovery != 0 || definition.Defense != 50 || definition.MagicDefense != 50 || definition.Attribute != "ICE" {
		return fmt.Errorf("official enemy fixed-growth FREEZE definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok || level.HPBars != 2 || level.ActionsPerTurn != 1 {
		return fmt.Errorf("official enemy fixed-growth FREEZE level changed: %+v", level)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 4 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 16 ||
		action.Target != "RANDOM" || action.ActionCost != 0 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy fixed-growth FREEZE action changed: %+v", action)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, HP: 10000, MaxHP: 10000, Attribute: "NEUTRAL"}
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
	if len(results) != 9 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 2, skillID, 0}) ||
		results[8].Command != resultBattleParam ||
		!equalBattleArgs(results[8].Args, battleParameterArgs(4, 10000, 10000, 0, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official enemy fixed-growth FREEZE action header is results=%+v", results)
	}
	for index := range engine.players {
		effects := engine.players[index].Effects
		if len(effects) != 1 || effects[0].Function != "FREEZE" || effects[0].Value != 811 ||
			effects[0].Remaining != 2 || effects[0].Rate != 100 || effects[0].Source != 5 ||
			effects[0].Parameters != [4]int{100, 151, 120, 0} ||
			!equalBattleArgs(results[1+2*index].Args, []int64{int64(index + 1), 0, 0, 306, 5, 0, 1, 0, 0, 0, 0}) {
			return fmt.Errorf("official enemy fixed-growth FREEZE target is result=%+v effects=%+v", results[1+2*index], effects)
		}
	}
	tick := engine.tickPlayerDOTEffects()
	engine.turn = 2
	lifecycle, err := expireForEffectContract(engine)
	if err != nil {
		return err
	}
	if len(lifecycle) != 0 {
		return fmt.Errorf("official enemy fixed-growth FREEZE next-turn lifecycle projected rows: %+v", lifecycle)
	}
	if len(tick) != 8 {
		return fmt.Errorf("official enemy fixed-growth FREEZE tick has %d results: %+v", len(tick), tick)
	}
	for index := range engine.players {
		if engine.players[index].HP != 9189 || engine.players[index].Effects[0].Remaining != 1 ||
			tick[2*index].Command != resultBuffEffect || tick[1+2*index].Command != 60 ||
			!equalBattleArgs(tick[1+2*index].Args, []int64{int64(index + 1), 0, -811, 9189, 0, 0, 100, 0, 0, 0}) {
			return fmt.Errorf("official enemy fixed-growth FREEZE tick target is player=%+v rows=%+v", engine.players[index], tick)
		}
	}
	return nil
}

func validateEnemyDOTSourceGrowthAction(catalog *CombatCatalog) error {
	const levelID = 37100112
	const skillID = 237101102
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 1 || roles[0].Function != "ELECTRIC" {
		return fmt.Errorf("official enemy source-growth DOT role set %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "1" ||
		role.Parameters[1] != "100" || role.Parameters[2] != "0" || role.Parameters[3] != "0" ||
		role.Parameters[4] != "0" || role.Parameters[5] != "0" || role.Parameters[6] != "100" ||
		role.Parameters[7] != "INT" || calibratedEnemySkillLevel(role.Function) != 1 {
		return fmt.Errorf("official enemy source-growth ELECTRIC changed: %+v", role)
	}
	_, zeroFixed, zeroCoefficient, zeroValue := dotRoleValues(role, 0, 1, 5000)
	oneRate, oneFixed, oneCoefficient, oneValue := dotRoleValues(role, 1, 1, 5000)
	if zeroFixed != 0 || zeroCoefficient != 0 || zeroValue != 0 || oneRate != 100 || oneFixed != 0 || oneCoefficient != 100 || oneValue != 500 {
		return fmt.Errorf("native source-growth ELECTRIC values are level0=%d/%d/%d level1=%d/%d/%d/%d", zeroFixed, zeroCoefficient, zeroValue, oneRate, oneFixed, oneCoefficient, oneValue)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 140000000 || definition.Attack != 10000 || definition.Magic != 5000 ||
		definition.Recovery != 0 || definition.Defense != 800000 || definition.MagicDefense != 20000 || definition.Attribute != "DARK" {
		return fmt.Errorf("official enemy source-growth ELECTRIC definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok || level.HPBars != 4 || level.ActionsPerTurn != 1 {
		return fmt.Errorf("official enemy source-growth ELECTRIC level changed: %+v", level)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 2 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 2 ||
		action.Target != "RANDOM" || action.ActionCost != 0 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy source-growth ELECTRIC action changed: %+v", action)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, HP: 10000, MaxHP: 10000, Attribute: "NEUTRAL"}
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
	if len(results) != 9 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 2, skillID, 0}) ||
		results[8].Command != resultBattleParam ||
		!equalBattleArgs(results[8].Args, battleParameterArgs(4, 10000, 10000, 0, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official enemy source-growth ELECTRIC action header is results=%+v", results)
	}
	for index := range engine.players {
		effects := engine.players[index].Effects
		if len(effects) != 1 || effects[0].Function != "ELECTRIC" || effects[0].Value != 500 ||
			effects[0].Remaining != 1 || effects[0].Rate != 100 || effects[0].Source != 5 ||
			effects[0].Parameters != [4]int{100, 0, 10, 0} ||
			!equalBattleArgs(results[1+2*index].Args, []int64{int64(index + 1), 0, 0, 309, 5, 0, 1, 0, 0, 0, 0}) {
			return fmt.Errorf("official enemy source-growth ELECTRIC target is result=%+v effects=%+v", results[1+2*index], effects)
		}
	}
	return nil
}
