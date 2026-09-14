package multiplayer

// Original CN 571d7/57e7d use five signed 32-bit counters on each enemy.
// The managed role enum 173 must be adapted to native ordinal 174 at the
// client DTO boundary; this does not change source CSV identities.
func (engine *BattleEngine) executeEnemyAIVariable(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	index := combatParameterInt(role.Parameters[0])
	if index < 0 || index >= 5 {
		return nil
	}
	var results []BattleResult
	includeDead := role.Target != "ENEMY_ALL" && role.Target != "FRIEND_ALL"
	for _, target := range engine.enemyRoleEnemyTargets(actor, selected, role, includeDead) {
		enemy := &engine.enemies[target]
		enemy.AIVariables[index] += int32(combatParameterInt(role.Parameters[1]))
		args := []int64{int64(enemy.MemberType)}
		for _, value := range enemy.AIVariables {
			args = append(args, int64(value))
		}
		results = append(results, BattleResult{Command: resultEnemyAIVariables, Args: args})
	}
	return results
}

func enemyAIVariableInRange(enemy *battleEnemy, indexText, lowText, highText string, branch bool) bool {
	index := combatParameterInt(indexText)
	value := int64(-1)
	if index >= 0 && index < len(enemy.AIVariables) {
		value = int64(enemy.AIVariables[index])
	} else if !branch {
		return false // AI trigger rejects invalid indices; branch getter returns -1.
	}
	low, high := int64(combatParameterInt(lowText)), int64(combatParameterInt(highText))
	// Original getExtendExecOrderParam IL_0564 and ParseTrigger IL_0942
	// default EMPTY bounds to int32 limits. Explicit zero is a real bound.
	if lowText == "" {
		low = -2147483648
	}
	if highText == "" {
		high = 2147483647
	}
	return low <= value && value <= high
}
