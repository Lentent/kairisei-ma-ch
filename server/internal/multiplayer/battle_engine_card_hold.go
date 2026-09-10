package multiplayer

// 3d621 keeps ordinary card/sphere holds separately from CURSE/BLESS.
// A held card stays outside the draw/trash pools until 422c0 releases it.
type battleCardHold struct {
	Action    battleAction
	Remaining int
}

func (player *battlePlayer) displayCardSlots() []int {
	slots := append([]int(nil), player.Hand[:]...)
	for _, hold := range player.CardHolds {
		if hold.Action.cardType == 0 {
			continue
		}
		for index, card := range player.Deck {
			if card.CardType != hold.Action.cardType {
				continue
			}
			present := false
			for _, slot := range slots {
				present = present || slot == index+1
			}
			if !present {
				slots = append(slots, index+1)
			}
			break
		}
	}
	return slots
}

func (engine *BattleEngine) cardActionReady(action battleAction, timing string, chains map[string]int) bool {
	trigger := action.skill.AppendTrigger
	if trigger == "NULL" {
		trigger = ""
	}
	condition := action.skill.AppendCondition
	return trigger == timing && (condition == "" || condition == "NULL" || condition == "NONE" ||
		engine.branchConditionSatisfied(condition, action.skill.AppendParameters, action, engine.turnActions, chains))
}

func (engine *BattleEngine) partitionPlayerActions(actions []battleAction, chains map[string]int) (ready, held []battleAction) {
	// 63d60 -> 3cb1c checks timing/hold conditions before sorting any action.
	for _, action := range actions {
		if engine.cardActionReady(action, "", chains) {
			ready = append(ready, action)
		} else if action.skill.AppendDuration > 0 {
			held = append(held, action)
		}
	}
	return ready, held
}

func (engine *BattleEngine) storeCardHolds(actions []battleAction) []BattleResult {
	var rows []BattleResult
	for _, action := range actions {
		player := &engine.players[action.memberType-1]
		// 4200e has a thirteen-entry physical bound; 4340a enforces the
		// configured combined hold limit at the turn tail.
		if player.GameOver || len(player.CardHolds) >= 13 {
			continue
		}
		player.CardHolds = append(player.CardHolds, battleCardHold{Action: action, Remaining: action.skill.AppendDuration})
		if engine.endType == 0 {
			rows = append(rows, BattleResult{Command: resultHoldSet, Args: []int64{
				int64(action.memberType), -1, int64(action.cardType), int64(action.sphereSlot),
				int64(action.skill.ID), 0, int64(action.skill.AppendDuration), int64(action.cardLevel), 0,
			}})
		}
	}
	return rows
}

func cardHoldLostResult(action battleAction) BattleResult {
	return BattleResult{Command: resultHoldLost, Args: []int64{
		int64(action.memberType), int64(action.cardType), int64(action.sphereSlot), 0,
	}}
}

func (player *battlePlayer) releaseHeldCard(action battleAction, clearModifiers bool) {
	if action.cardType == 0 {
		return
	}
	for index, card := range player.Deck {
		if card.CardType == action.cardType {
			// 422c0 -> 49120 clears CARD modifiers on expiry/removal. The
			// execution queue defers that same 45726 cleanup until after use.
			if clearModifiers {
				player.clearCardModifiers(action.cardType)
			}
			player.Discard = append(player.Discard, index)
			return
		}
	}
}

func (engine *BattleEngine) executeCardHolds(timing string) ([]BattleResult, error) {
	var actions []battleAction
	chains := engine.currentChainCounts()
	// 664ee snapshots and silently releases every eligible entry before
	// sorting. Conditions therefore cannot observe an earlier held skill.
	for index := range engine.players {
		player := &engine.players[index]
		if player.HP <= 0 {
			continue
		}
		kept := make([]battleCardHold, 0, len(player.CardHolds))
		for _, hold := range player.CardHolds {
			if !engine.cardActionReady(hold.Action, timing, chains) {
				kept = append(kept, hold)
				continue
			}
			actions = append(actions, hold.Action)
			player.releaseHeldCard(hold.Action, false)
		}
		player.CardHolds = kept
	}
	engine.sortPlayerActions(actions)
	var rows []BattleResult
	for _, action := range actions {
		if engine.endType == 0 {
			// 664ee -> 3d342 -> 796a8 includes the held card's owner even
			// when that member selected a different attribute this turn.
			skillRows, err := engine.executePlayerAction(action, engine.appendChainCounts(action.memberType, action.skill))
			if err != nil {
				return nil, err
			}
			rows = append(rows, skillRows...)
		}
		// 664ee calls 45726 even if an earlier queued action ended battle.
		engine.players[action.memberType-1].clearCardModifiers(action.cardType)
		if engine.endType == 0 {
			rows = append(rows, cardHoldLostResult(action))
		}
	}
	return rows, nil
}

func (engine *BattleEngine) tickPlayerCardHolds(player *battlePlayer) []BattleResult {
	var rows []BattleResult
	kept := make([]battleCardHold, 0, len(player.CardHolds))
	for _, hold := range player.CardHolds {
		hold.Remaining--
		if hold.Remaining > 0 {
			kept = append(kept, hold)
		} else {
			player.releaseHeldCard(hold.Action, true)
			rows = append(rows, cardHoldLostResult(hold.Action))
		}
	}
	for len(kept) > 0 && len(kept)+len(player.BlessHolds) > engine.holdMax {
		action := kept[len(kept)-1].Action
		kept = kept[:len(kept)-1]
		player.releaseHeldCard(action, true)
		rows = append(rows, cardHoldLostResult(action))
	}
	player.CardHolds = kept
	return rows
}
