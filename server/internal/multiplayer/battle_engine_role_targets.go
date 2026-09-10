package multiplayer

// 8b660 applies the role's attribute bitset and source exclusion AFTER target
// selection, before invoking a consumer. Rejection produces no failure row,
// mutation or extra target-selection/consumer RNG.
func combatRoleAllowsTarget(role CombatSkillRole, source, target int, attribute string) bool {
	if role.ExcludeSelf && source == target {
		return false
	}
	var mask uint32
	for i, allowed := range role.Attributes {
		if allowed {
			mask |= 1 << uint(i+1)
		}
	}
	if !role.HasTargetAttributes && mask == 0 {
		return true // A programmatic role did not specify an attribute rule.
	}
	code := combatAttributeCode(attribute)
	if parts := splitCombatAttributes(attribute); len(parts) == 2 {
		// The native consumer shifts by the raw ATTR enum (x86 low five bits),
		// not an OR of its components. Members normally have a single ATTR.
		code = 100*combatAttributeCode(parts[0]) + combatAttributeCode(parts[1])
	}
	return mask&(1<<uint(code&31)) != 0
}

func (engine *BattleEngine) filterRolePlayers(targets []int, source int, role CombatSkillRole) []int {
	kept := targets[:0]
	for _, member := range targets {
		if combatRoleAllowsTarget(role, source, member, engine.players[member-1].Attribute) {
			kept = append(kept, member)
		}
	}
	return kept
}

func (engine *BattleEngine) filterRoleEnemies(targets []int, source int, role CombatSkillRole) []int {
	kept := targets[:0]
	for _, index := range targets {
		enemy := &engine.enemies[index]
		if combatRoleAllowsTarget(role, source, enemy.MemberType, enemy.Attribute) {
			kept = append(kept, index)
		}
	}
	return kept
}

func (engine *BattleEngine) playerRoleTargets(action battleAction, role CombatSkillRole) []int {
	if targets, ok := engine.skillRoleTargets(action.memberType, role, true); ok {
		return engine.filterRolePlayers(targets, action.memberType, role)
	}
	return engine.filterRolePlayers(engine.playerTargets(role.Target, action.memberType, action.target), action.memberType, role)
}

func (engine *BattleEngine) playerRoleEnemyTargets(action battleAction, role CombatSkillRole) []int {
	if targets, ok := engine.skillRoleTargets(action.memberType, role, false); ok {
		return engine.filterRoleEnemies(targets, action.memberType, role)
	}
	return engine.filterRoleEnemies(engine.enemyTargets(role.Target, action.target), action.memberType, role)
}
