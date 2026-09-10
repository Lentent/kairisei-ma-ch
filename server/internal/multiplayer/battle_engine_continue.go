package multiplayer

import "errors"

// settlePlayerDeaths is the native 65010/61d60 boundary. A KO remains a
// participant while continuation is offered; only GameOver retires it.
func (engine *BattleEngine) settlePlayerDeaths(results []BattleResult) []BattleResult {
	if engine.continueAllowed && (engine.endType == 0 || engine.endType == 2) {
		for _, player := range engine.players {
			if player.MemberType != 0 && player.HP <= 0 && !player.GameOver {
				engine.endType = 0
				engine.continuePending = true
				return append(results, BattleResult{Command: resultContinueWait})
			}
		}
	}
	results = engine.appendNewPlayerGameOver(results)
	if engine.allPlayersDead() && engine.endType == 0 {
		engine.endType = 2
	}
	return results
}

func (engine *BattleEngine) cancelContinue() []BattleResult {
	results := engine.appendNewPlayerGameOver(nil)
	engine.continuePending = false
	if engine.allPlayersDead() {
		engine.endType, engine.phase = 2, battlePhaseEnded
		results = append(results, battleEndResult(2))
	}
	return results
}

func (engine *BattleEngine) commitContinue(plan battleContinuePlan) {
	engine.players = plan.players
	engine.continuePending, engine.forceEndCheck, engine.endType = false, false, 0
	engine.continueCount++
}

// battleContinuePlan separates the native party mutation from the account
// transaction. The room must stay paused from preparation through commit;
// preparing a payment must not expose a revival or consume battle RNG.
type battleContinuePlan struct {
	players [maxRoomMembers]battlePlayer
	results []BattleResult
}

// prepareContinue mirrors x86 battle5_api_continue -> 65cec. Eligibility and
// payment belong to the room; this operation only prepares the same-side
// revival. Proof: cn602-d446-continue-static-20260907 (static original ELF).
func (engine *BattleEngine) prepareContinue(memberType int) (battleContinuePlan, error) {
	if engine == nil || memberType < 1 || memberType > maxRoomMembers ||
		engine.players[memberType-1].MemberType != memberType || engine.players[memberType-1].GameOver {
		return battleContinuePlan{}, errors.New("continue member is unavailable")
	}
	plan := battleContinuePlan{players: engine.players}
	for index := range plan.players {
		player := &plan.players[index]
		if player.MemberType == 0 || player.GameOver {
			continue
		}
		oldBlocked := engine.playerEffectValue(player, "COST_BLOCK")
		if player.HP <= 0 {
			plan.results = append(plan.results, BattleResult{Command: resultRevive, Args: []int64{int64(player.MemberType)}})
		}
		// 73302(2), 73302(5), 7339f(2), 7339f(5). Each nonempty
		// category has its own natural expiry and parameter projection.
		for _, listType := range [...]int{0, 2} {
			for _, category := range [...]int{2, 5} {
				plan.results = appendContinueRelease(plan.results, player, listType, category)
			}
		}
		newBlocked := engine.playerEffectValue(player, "COST_BLOCK")
		if engine.phase == battlePhaseTurn {
			// No selection has occurred in this turn, including a first-attack
			// wipe that returned before TurnPhase's ordinary cost projection.
			player.Cost = maxInt(0, engine.turnCost()-newBlocked)
		} else if oldBlocked != newBlocked {
			// COST_BLOCK is subtracted by the managed cost getter. Removing it
			// restores available cost while retaining already spent cost.
			player.Cost = maxInt(0, minInt(engine.turnCost(), player.Cost+oldBlocked-newBlocked))
		}
		// 42ae4(kind=0) removes CURSE while preserving BLESS, card holds,
		// submitted cards, costs and Sphere usage.
		kept := make([]battleBlessHold, 0, len(player.BlessHolds))
		for _, hold := range player.BlessHolds {
			if hold.CardType == 21 {
				plan.results = append(plan.results, blessHoldLostResult(player.MemberType, hold))
			} else {
				kept = append(kept, hold)
			}
		}
		player.BlessHolds = kept
		player.HP = player.MaxHP
		plan.results = append(plan.results, BattleResult{Command: resultHP, Args: []int64{
			int64(player.MemberType), int64(player.MaxHP), int64(player.HP), 3,
		}})
		// +59ab0 is consumed by 5cecc at the next draw: refill the hand,
		// overriding ordinary draw bonuses/penalties, without drawing now.
		player.ContinueDraw = true
	}
	plan.results = append(plan.results, BattleResult{Command: resultContinue, Args: []int64{int64(memberType)}})
	return plan, nil
}

func appendContinueRelease(results []BattleResult, player *battlePlayer, listType, category int) []BattleResult {
	var removed []battleEffect
	kept := make([]battleEffect, 0, len(player.Effects))
	for _, effect := range player.Effects {
		code, known := battleBuffCodes[effect.Function]
		if known && effect.ListType == listType && battleBuffKind(code) == category {
			removed = append(removed, effect)
		} else {
			kept = append(kept, effect)
		}
	}
	if len(removed) == 0 {
		return results
	}
	ensurePlayerBaseParameters(player)
	for _, effect := range removed {
		revertPlayerEffect(player, effect)
	}
	player.Effects = kept
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	results = appendNaturalExpiryRows(results, player.MemberType, removed, kept, player.Attribute)
	return append(results, BattleResult{Command: resultBattleParam, Args: battleParameterArgs(player.MemberType,
		player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense,
		player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
}
