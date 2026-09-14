package multiplayer

// Original 9b076 -> 80070 rolls once per selected user, even if no curse
// matches. 42b52 then releases all matching CURSE holds in vector order;
// ordinary held cards and blessings are independent inventories.
func (engine *BattleEngine) releasePlayerCurses(action battleAction, role CombatSkillRole) []BattleResult {
	rate := int32(combatParameterInt(role.Parameters[0])) + int32(action.cardLevel)*int32(combatParameterInt(role.Parameters[1]))
	attribute := role.Parameters[2]
	codes, _ := sphereSupportAttributeCodes(attribute)
	flags := 0
	for _, code := range codes {
		flags |= 1 << uint(code&31)
	}
	var results []BattleResult
	for _, member := range engine.playerRoleTargets(action, role) {
		if int32(engine.rng.next()%10000) >= rate*100 {
			continue
		}
		player := &engine.players[member-1]
		kept := player.BlessHolds[:0]
		announced := false
		for _, hold := range player.BlessHolds {
			if hold.CardType != 21 || !damageAttributeMatches(attribute, hold.Skill.Attribute) {
				kept = append(kept, hold)
				continue
			}
			if !announced {
				results = append(results, BattleResult{Command: resultAppendRelease, Args: []int64{int64(member), int64(role.RoleIndex), 21, int64(flags)}})
				announced = true
			}
			results = append(results, blessHoldLostResult(member, hold))
		}
		player.BlessHolds = kept
	}
	return results
}
