package multiplayer

const CombatFunctionTurnSingleToAll = "TURN_SINGLE_TO_ALL"

// Resolve recipients before rewriting this action's targets. An earlier
// all-target effect must not broaden the recipients of another activator.
func (engine *BattleEngine) singleToAllRecipients(action battleAction) uint8 {
	if action.sphereSlot != 0 || action.cardType < 1 || action.cardType > 10 {
		return 0
	}
	if engine.catalog.Cards[action.cardID].TurnSingleToAll {
		return 15 // Legacy card flags applied to the entire team.
	}
	var recipients uint8
	for _, role := range action.roles {
		if role.Function != CombatFunctionTurnSingleToAll {
			continue
		}
		// Old marker rows used SELF as an inert placeholder, with no parameters.
		if role.Parameters[0] == "" {
			return 15
		}
		if role.Parameters[0] != "1" {
			continue
		}
		switch role.Target {
		case "SELF", "USER_ONE", "USER_ALL":
			for _, member := range engine.playerTargets(role.Target, action.memberType, action.target) {
				if member >= 1 && member <= 4 {
					recipients |= 1 << uint(member-1)
				}
			}
		}
	}
	return recipients
}

func singleToAllTarget(target string) string {
	switch target {
	case "SELF", "USER_ONE", "FRIEND_ONE":
		return "USER_ALL"
	case "ENEMY_ONE":
		return "ENEMY_ALL"
	default:
		return target // All-target and special targeting retain their rules.
	}
}

func (engine *BattleEngine) applySingleToAll(action battleAction) battleAction {
	if action.memberType < 1 || action.memberType > 4 || engine.singleToAllMembers&(1<<uint(action.memberType-1)) == 0 || action.sphereSlot != 0 || action.cardType < 1 || action.cardType > 10 {
		return action
	}
	target := singleToAllTarget(action.skill.Target)
	if target != action.skill.Target {
		action.skill.Target, action.target = target, 0
	}
	// Actions can share master roles and held-card slices. Rewrite only a copy.
	action.roles = append([]CombatSkillRole(nil), action.roles...)
	for i := range action.roles {
		if action.roles[i].Function == CombatFunctionTurnSingleToAll {
			continue
		}
		action.roles[i].Target = singleToAllTarget(action.roles[i].Target)
	}
	return action
}
