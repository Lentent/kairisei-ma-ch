package multiplayer

// a4ac0 executes every producer, including a275b's CALL lookup even when
// another role owns the displayed number. The outer card/sphere preview owns
// RNG rollback; each CALL lookup uses preview=0 and card type22, preventing
// recursive BLESS display. All7982a lookups disable RANDOM independently of
// RNG rollback. Actual registration performs the same lookup once.
func (engine *BattleEngine) actionDisplayPower(player *battlePlayer, action battleAction) (int, error) {
	power := engine.cardDisplayPower(player, action.cardLevel, action.skill, action.roles)
	if action.cardType == 22 || action.callSkill.ID == 0 {
		return power, nil
	}
	for index, role := range action.roles {
		if role.Function != "BLESS" {
			continue
		}
		hold := battleBlessHold{CardType: 22, SourceMember: player.MemberType,
			Skill: action.callSkill, Roles: action.callRoles, CardLevel: action.cardLevel}
		state, err := engine.resolveHoldDisplayState(player, hold, engine.appendChainCounts(player.MemberType, action.callSkill), false)
		if err != nil {
			return 0, err
		}
		if action.skill.DisplayRole == index+1 {
			power = state.Power
		}
	}
	return power, nil
}

// 7982a selects extend.skill_value_role_no (CSV column5), then the chosen
// producer's info+0x50. It never searches for the first recognizable effect.
// Previews use 743f6(...,0,0): its third argument clips negative values, not
// parameter limits. Player limits still apply; native enemy sources are uncapped.
// Target armor/critical/EX damage consumers are not previews.
func (engine *BattleEngine) cardDisplayPower(player *battlePlayer, level int, skill CombatSkillDefinition, roles []CombatSkillRole) int {
	if skill.DisplayRole < 1 || skill.DisplayRole > 5 || skill.DisplayRole > len(roles) {
		return 0
	}
	role := roles[skill.DisplayRole-1]
	view := *player
	stat := func(parameter string) int { return combatStatValue(&view, parameter) }
	switch role.Function {
	case "ATTACK_AA":
		value := attackRolePower(role, level, stat(role.Parameters[5]))
		// 7982a adds the ordinary REVENGE operator and all DAMAGE_INCREASE
		// operators only when the selected role is an attack (not any buff).
		modifiers := collectAttackModifiers(roles, &view, level, 1)
		return value + modifiers.damageIncrease + attackRevengeBonus(view.DamageTaken, &view, modifiers.revengeRate, modifiers.revengeParameter, modifiers.revengeCapRate)
	case "DEF_UP_FIXED", "ATK_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "PARAM_LIMIT_BREAK_FIXED":
		return fixedBuffRoleValue(role, level, 1)
	case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM":
		return selfScaledParameterValue(role, level, 1, stat(role.Parameters[2]))
	case "HEAL_FIXED":
		return fixedHealRoleValue(role, level, 1, stat(role.Parameters[4]))
	case "HEAL_BY_SELF_PARAM":
		return selfScaledHealRoleValue(role, level, 1, stat(role.Parameters[0]))
	case "REGENERATE_FIXED":
		return fixedRegenerateRoleValue(role, level, 1, stat(role.Parameters[5]))
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		_, fixed := attributeDefenseRoleValues(role, level, 1)
		return absInt(fixed)
	case "ENCHANT":
		return persistentRoleValue(role, level, &view, 1)
	case "POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC":
		_, _, _, value := dotRoleValues(role, level, 1, stat(role.Parameters[7]))
		return value
	case "CARD_TRAP_DAMAGE":
		// a354a publishes fixed damage plus the selected source coefficient.
		return cardTrapDamageRoleValue(role, level, 1, stat(role.Parameters[7]))
	case "COVERING", "WEAKNESS", "CRITICAL_UP", "CRITICAL_DOWN":
		return retainedRateRoleValue(role, level) / 10
	case "REFLECTION":
		return retainedRateRoleValue(role, level) / 100
	case "DOT_VALUE_UP":
		return retainedRateRoleValue(role, level)
	case "ENDURE", "DEAL_BONUS":
		return combatParameterInt(role.Parameters[0])
	case "GUTS":
		// 95fe9 writes the restored HP percentage to info+0x50.
		return combatParameterInt(role.Parameters[2])
	case "COST_BLOCK":
		return combatParameterInt(role.Parameters[1])
	case "BURST_GAUGE_QUICK_UP", "DEBUFF_RELEASE_ONE", "BUFF_RELEASE", "BUFF_RELEASE_ONE", "HP_CUT":
		return combatParameterInt(role.Parameters[0]) + combatParameterInt(role.Parameters[1])*level
	case "ATTACK_BARRIER_APPOINT_ATTR":
		// 9acb7 reports p3 (uses), not the p1+p2*level damage threshold.
		return combatParameterInt(role.Parameters[3])
	case "ATK_OP_PIERCING", "ATK_OP_DAMAGE_INCREASE":
		// Their info+0x50 is explicitly zero; modifiers only raise ATTACK display.
		return 0
	default:
		return 0
	}
}
