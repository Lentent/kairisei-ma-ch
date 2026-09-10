package multiplayer

// Default TEAMBATTLE has no AwakeTakeOver buff flag. The x86 EAX argument
// omitted by Ghidra's decompile selects 61d60 -> a7bac -> 7233e, NOT 5eb20.
// Clear player lists NORMAL/PASSIVE/FIELD/CARD (including KO users), retain
// EVENT and BURST lists, emit grouped 72 plus 5/6, and tick burst counters.
// Ordinary expiry (72a18) is a different path that also emits 70/71.
func (engine *BattleEngine) appendTerminalBuffCleanup(results []BattleResult) []BattleResult {
	// The no-continue profile retires KO members via 228c0 -> 661f3.
	// Its final 2,2 has no winning-wave cleanup or Burst countdown.
	if engine.endType == 2 {
		return results
	}
	for index := range engine.players {
		player := &engine.players[index]
		if player.MemberType == 0 {
			continue
		}
		results = appendDefaultWavePlayerCleanup(results, player)
	}
	engine.tickBurstCounters()
	// 61d60 -> a7c08 -> 422c0 removes remaining holds after all members'
	// buff cleanup; 42ae4 retains each BLESS/CURSE append index.
	for index := range engine.players {
		player := &engine.players[index]
		for _, hold := range player.CardHolds {
			player.releaseHeldCard(hold.Action, true)
			results = append(results, cardHoldLostResult(hold.Action))
		}
		player.CardHolds = nil
		// a7c08 calls 42ae4(kind=0) then kind=1: CURSE before BLESS,
		// retaining vector order within each kind regardless of insertion order.
		for _, cardType := range [...]int{21, 22} {
			for _, hold := range player.BlessHolds {
				if hold.CardType == cardType {
					results = append(results, blessHoldLostResult(player.MemberType, hold))
				}
			}
		}
		player.BlessHolds = nil
	}
	return results
}

func appendDefaultWavePlayerCleanup(results []BattleResult, player *battlePlayer) []BattleResult {
	ensurePlayerBaseParameters(player)
	kept := make([]battleEffect, 0, len(player.Effects))
	var removed []battleEffect
	eventCodes := make(map[int]bool)
	for _, effect := range player.Effects {
		if effect.ListType == 2 || effect.ListType == 5 || effect.ListType == 6 {
			kept = append(kept, effect)
			if effect.ListType == 2 {
				eventCodes[battleBuffCodes[effect.Function]] = true
			}
		} else {
			removed = append(removed, effect)
			revertPlayerEffect(player, effect)
		}
	}
	player.Effects = kept
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	released := make(map[int]bool)
	parameterMasks := make(map[int]int)
	for _, effect := range removed {
		code, ok := battleBuffCodes[effect.Function]
		if !ok || effect.ListType != 0 || eventCodes[code] {
			continue
		}
		key, row := naturalEffectReleaseResult(player.MemberType, effect, code)
		// 7233e groups by buff code and overlapping parameter bits. Unlike
		// ordinary expiry, a second attribute does not create another 72 row.
		parameterFlags := key[1]
		if !released[code] || parameterFlags != 0 && parameterMasks[code]&parameterFlags == 0 {
			results = append(results, row)
			released[code] = true
			parameterMasks[code] |= parameterFlags
		}
	}
	return append(results,
		playerBaseParameterResult(player),
		BattleResult{Command: resultBattleParam, Args: battleParameterArgs(player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
	)
}
