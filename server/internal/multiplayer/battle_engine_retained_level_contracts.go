package multiplayer

import (
	"fmt"
	"strings"
)

// validateEnemyRetainedLevelContracts guards three retained producers whose
// consumers already have independent official lifecycle contracts. The only
// runtime change here is the managed ordinary-enemy level reaching the native
// rate or fixed/source formula before the effect is stored.
func validateEnemyRetainedLevelContracts(catalog *CombatCatalog) error {
	wanted := map[string]int{"CRITICAL_UP": 10, "WEAKNESS": 110, "CARD_TRAP_DAMAGE": 253}
	wantedGrowth := map[string]int{"CRITICAL_UP": 7, "WEAKNESS": 8, "CARD_TRAP_DAMAGE": 9}
	rows := make([]CombatSkillRole, 0, 373)
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
			if role.Target != "SELECT" && role.Target != "FRIEND_ALL" || role.ExcludeSelf || role.ChainRate != 0 || role.HateLimit != 0 {
				return fmt.Errorf("official enemy retained level role shape changed: %+v", role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy %s skill %d disabled attribute %d", role.Function, role.SkillID, attribute)
				}
			}
			grows := combatParameterInt(role.Parameters[2]) != 0
			if role.Function == "CARD_TRAP_DAMAGE" {
				grows = combatParameterInt(role.Parameters[4]) != 0 || combatParameterInt(role.Parameters[6]) != 0
			}
			if grows {
				growthCounts[role.Function]++
				growthRoleIDs[role.SkillID] = true
			}
		}
	}
	if len(rows) != 373 || len(growthRoleIDs) != 24 || !equalStringCountMap(counts, wanted) || !equalStringCountMap(growthCounts, wantedGrowth) {
		return fmt.Errorf("official enemy retained level matrix changed: rows=%d growth=%d counts=%v growth_counts=%v", len(rows), len(growthRoleIDs), counts, growthCounts)
	}
	if digest := combatRoleMatrixDigest(rows); digest != "27d973a3e0d04e44f931d1352a9e15dcea358300c458cedf6b5a26b3bee24ebd" {
		return fmt.Errorf("official enemy retained level row digest is %s", digest)
	}
	if digest := combatIntSetDigest(growthRoleIDs); digest != "a626496779e36be40f9745c886c8cffe0c3d48b85ece8d6024e0029d93cbec66" {
		return fmt.Errorf("official enemy retained level growth-role digest is %s", digest)
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
	actionRecords := make([]string, 0, 42)
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
	if variantCount != 24 || len(growthSkillIDs) != 24 || len(actionRecords) != 42 || len(referencingLevels) != 27 {
		return fmt.Errorf("official enemy retained level reachability changed: variants=%d skills=%d refs=%d levels=%d", variantCount, len(growthSkillIDs), len(actionRecords), len(referencingLevels))
	}
	if digest := combatIntSetDigest(growthSkillIDs); digest != "a626496779e36be40f9745c886c8cffe0c3d48b85ece8d6024e0029d93cbec66" {
		return fmt.Errorf("official enemy retained level outer-skill digest is %s", digest)
	}
	if digest := combatIntSetDigest(referencingLevels); digest != "5a2e725b81b3d20591a7befd07acfbfa888ecad0ba37020460bde8ebaa5a2e57" {
		return fmt.Errorf("official enemy retained level enemy-level digest is %s", digest)
	}
	if digest := combatStringRecordsDigest(actionRecords); digest != "9734a3a5f0c1655ae017d0443885bd635ee8a3ad3481a5424c857fd8490e625b" {
		return fmt.Errorf("official enemy retained level action digest is %s", digest)
	}
	return validateEnemyRetainedGrowthActions(catalog)
}

func validateEnemyRetainedGrowthActions(catalog *CombatCatalog) error {
	if err := validateEnemyCriticalGrowthAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyWeaknessGrowthAction(catalog); err != nil {
		return err
	}
	return validateEnemyCardTrapGrowthAction(catalog)
}

func validateEnemyCriticalGrowthAction(catalog *CombatCatalog) error {
	const levelID = 31120121
	const skillID = 37901107
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 5 || roles[4].Function != "CRITICAL_UP" {
		return fmt.Errorf("official enemy CRITICAL_UP role set %d changed: %+v", skillID, roles)
	}
	role := roles[4]
	if role.RoleIndex != 4 || role.Target != "SELECT" || role.Parameters[0] != "2" ||
		role.Parameters[1] != "100" || role.Parameters[2] != "1" || calibratedEnemySkillLevel(role.Function) != 1 {
		return fmt.Errorf("official enemy CRITICAL_UP growth row changed: %+v", role)
	}
	if zero, one := retainedRateRoleValue(role, 0), retainedRateRoleValue(role, 1); zero != 100 || one != 101 {
		return fmt.Errorf("native enemy CRITICAL_UP values are level0=%d level1=%d", zero, one)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 200000 || definition.Attack != 770 || definition.Magic != 180 ||
		definition.Recovery != 0 || definition.Defense != 0 || definition.MagicDefense != 300 || definition.Attribute != "DARK" {
		return fmt.Errorf("official enemy CRITICAL_UP definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok || level.HPBars != 3 || level.ActionsPerTurn != 1 {
		return fmt.Errorf("official enemy CRITICAL_UP level changed: %+v", level)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 6 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 17 ||
		action.Target != "RANDOM" || action.ActionCost != 0 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy CRITICAL_UP action changed: %+v", action)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 41 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 2, skillID, 0}) ||
		results[40].Command != resultBattleParam || !equalBattleArgs(results[40].Args, results[32].Args) {
		return fmt.Errorf("official enemy CRITICAL_UP action header is results=%+v", results)
	}
	for index := range engine.players {
		effects := engine.players[index].Effects
		if len(effects) != 5 {
			return fmt.Errorf("official enemy CRITICAL_UP companion role count is %d: %+v", len(effects), effects)
		}
		critical := effects[4]
		if critical.Function != "CRITICAL_UP" || critical.Value != 101 || critical.Rate != 101 ||
			critical.Remaining != 2 || critical.Source != 5 || critical.RoleIndex != 4 || critical.Parameters != [4]int{10, 0, 0, 0} ||
			!equalBattleArgs(results[33+2*index].Args, []int64{int64(index + 1), 4, 0, 10, 1, 0, 1, 0, 0, 0, 0}) {
			return fmt.Errorf("official enemy CRITICAL_UP target is result=%+v effect=%+v", results[33+2*index], critical)
		}
	}
	return nil
}

func validateEnemyWeaknessGrowthAction(catalog *CombatCatalog) error {
	const levelID = 30220151
	const skillID = 31701317
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 1 || roles[0].Function != "WEAKNESS" {
		return fmt.Errorf("official enemy WEAKNESS role set %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "4" ||
		role.Parameters[1] != "1300" || role.Parameters[2] != "1" || calibratedEnemySkillLevel(role.Function) != 1 {
		return fmt.Errorf("official enemy WEAKNESS growth row changed: %+v", role)
	}
	if zero, one := retainedRateRoleValue(role, 0), retainedRateRoleValue(role, 1); zero != 1300 || one != 1301 {
		return fmt.Errorf("native enemy WEAKNESS values are level0=%d level1=%d", zero, one)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 800000 || definition.Attack != 9000 || definition.Magic != 9000 ||
		definition.Recovery != 3000 || definition.Defense != 10000 || definition.MagicDefense != 10000 || definition.Attribute != "LIGHT" {
		return fmt.Errorf("official enemy WEAKNESS definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok || level.HPBars != 5 || level.ActionsPerTurn != 7 {
		return fmt.Errorf("official enemy WEAKNESS level changed: %+v", level)
	}
	var action CombatEnemyAction
	found := false
	for _, candidate := range level.Actions {
		if candidate.SkillID == skillID && candidate.Slot == 11 {
			action, found = candidate, true
			break
		}
	}
	if !found || action.Category != "skill" || action.AIConditionID != 31701319 || action.Priority != 9 ||
		action.Target != "SINGER" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy WEAKNESS deterministic action changed: %+v", action)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 3 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{4, 0, 0, 308, 5, 0, 1, 0, 0, 0, 0}) ||
		results[2].Command != resultBattleParam ||
		!equalBattleArgs(results[2].Args, battleParameterArgs(4, 10000, 10000, 0, 0, 0, 1000, 1000, 0, 0, 0)) {
		return fmt.Errorf("official enemy WEAKNESS level-one action is results=%+v", results)
	}
	for index := range engine.players {
		effects := engine.players[index].Effects
		if index != 3 {
			if len(effects) != 0 {
				return fmt.Errorf("official enemy WEAKNESS changed non-target member %d: %+v", index+1, effects)
			}
			continue
		}
		if len(effects) != 1 || effects[0].Function != "WEAKNESS" || effects[0].Value != 1301 ||
			effects[0].Rate != 1301 || effects[0].Remaining != 4 || effects[0].Source != 5 ||
			effects[0].Parameters != [4]int{130, 0, 0, 0} {
			return fmt.Errorf("official enemy WEAKNESS target is %+v", effects)
		}
	}
	return nil
}

func validateEnemyCardTrapGrowthAction(catalog *CombatCatalog) error {
	const levelID = 40026933
	const skillID = 44252016
	roles := catalog.EnemySkillRoles[skillID]
	if len(roles) != 2 || roles[0].Function != "CARD_TRAP_DAMAGE" || roles[1].Function != "GUARD_BREAK_FIXED" {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE role set %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "3" ||
		role.Parameters[1] != "1" || role.Parameters[2] != "1" || role.Parameters[3] != "1000" ||
		role.Parameters[4] != "1000" || role.Parameters[5] != "0" || role.Parameters[6] != "0" ||
		role.Parameters[7] != "MND" || calibratedEnemySkillLevel(role.Function) != 1 {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE growth row changed: %+v", role)
	}
	zero := cardTrapDamageRoleValue(role, 0, 1, 1000)
	one := cardTrapDamageRoleValue(role, 1, 1, 1000)
	if zero != 1000 || one != 1001 {
		return fmt.Errorf("native enemy CARD_TRAP_DAMAGE values are level0=%d level1=%d", zero, one)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 250000 || definition.Attack != 1000 || definition.Magic != 0 ||
		definition.Recovery != 1000 || definition.Defense != 0 || definition.MagicDefense != 0 || definition.Attribute != "LIGHT" {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok || level.HPBars != 3 || level.ActionsPerTurn != 6 {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE level changed: %+v", level)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 7 || action.Category != "skill" || action.AIConditionID != 40250016 || action.Priority != 9 ||
		action.Target != "RANDOM" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE action changed: %+v", action)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, true)
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 17 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 2, skillID, 0}) {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE level-one action is results=%+v", results)
	}
	// Official USER_ALL expands SELECT for both roles; the AI's concrete
	// member in row50 is not a restriction to that one player (D-327).
	for index := range engine.players {
		member := int64(index + 1)
		param := battleParameterArgs(index+1, 10000, 10000, 0, 0, 0, 500, 1000, 99999, 99999, 99999)
		if !equalBattleArgs(results[1+2*index].Args, []int64{member, 0, 0, 112, 2, 0, 1, 0, 0, 0, 0}) ||
			results[2+2*index].Command != resultBattleParam || !equalBattleArgs(results[2+2*index].Args, param) ||
			!equalBattleArgs(results[9+2*index].Args, []int64{member, 1, 0, 102, 2, 32, 1, 0, 0, 0, 0}) ||
			results[10+2*index].Command != resultBattleParam || !equalBattleArgs(results[10+2*index].Args, param) {
			return fmt.Errorf("official all-player trap/status projection is %+v", results)
		}
	}
	trappedCard := 0
	for _, effect := range engine.players[3].Effects {
		if effect.Function != "CARD_TRAP_DAMAGE" {
			continue
		}
		if trappedCard != 0 || effect.Value != 1001 || effect.Remaining != 3 || effect.Source != 5 ||
			effect.Parameters != [4]int{1, 1001, 0, 0} || effect.CardType < 1 || effect.CardType > 5 {
			return fmt.Errorf("official enemy CARD_TRAP_DAMAGE retained an invalid trap: %+v", effect)
		}
		trappedCard = effect.CardType
	}
	if trappedCard != 3 {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE did not bind a hand card: %+v", engine.players[3].Effects)
	}
	if next := engine.rng.next(); next != 3556250659 {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE consumed the wrong xor128 draws; next=%d", next)
	}
	hpBefore := engine.players[3].HP
	triggered := engine.triggerCardTrap(4, trappedCard)
	if len(triggered) != 5 || engine.players[3].HP != hpBefore-1001 ||
		triggered[0].Command != resultBuffEffect || triggered[1].Command != 60 || triggered[2].Command != 72 ||
		triggered[3].Command != resultBuffLostOne || triggered[4].Command != resultBattleParam ||
		!equalBattleArgs(triggered[1].Args, []int64{4, 0, -1001, int64(hpBefore - 1001), 0, 0, 100, 0, 0, 0}) {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE trigger is card=%d results=%+v player=%+v", trappedCard, triggered, engine.players[3])
	}
	if repeated := engine.triggerCardTrap(4, trappedCard); len(repeated) != 0 || engine.players[3].HP != hpBefore-1001 {
		return fmt.Errorf("official enemy CARD_TRAP_DAMAGE repeated after consumption: %+v", repeated)
	}
	return nil
}

func retainedGrowthContractEngine(catalog *CombatCatalog, definition CombatEnemyDefinition, level CombatEnemyLevel, withHand bool) *BattleEngine {
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1, ArthurType: 2, HP: 10000, MaxHP: 10000,
			Defense: 1000, BaseDefense: 1000, MDefense: 1000, BaseMDefense: 1000, Attribute: "NEUTRAL",
		}
		if withHand {
			for cardIndex := 0; cardIndex < 5; cardIndex++ {
				engine.players[index].Deck[cardIndex] = BattleCard{CardType: cardIndex + 1, CardID: 10131011, Level: 60}
				engine.players[index].Hand[cardIndex] = cardIndex + 1
			}
		}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		Level: level,
	}
	return engine
}
