package multiplayer

import (
	"errors"
	"fmt"
)

// ResumeResults projects the current authoritative Go state through the
// dedicated RESUME_* command family consumed by the CN 6.0.2 client. This is
// the server-side counterpart of libbattle5 battle5_api_resume_data_get: it is
// a state snapshot, not a replay of already applied damage and buff rows.
// Like 26050 -> 3d680/40392, publishing it commits only display caches.
func (engine *BattleEngine) ResumeResults(haveDrops ...BattleDrop) ([]BattleResult, error) {
	if engine == nil || engine.phase == battlePhaseCreated {
		return nil, errors.New("combat resume state is unavailable")
	}
	// Build atomically on a copy; gameplay/RNG must not change. Native commits
	// submitted-card and Sphere display caches, which affect later 314/315 rows.
	live := engine
	view := *engine
	engine = &view

	results := make([]BattleResult, 0, 160)
	// 1b3c0 restores identities before any RESUME_* rows. Unlike Start, the
	// comeback transport does not provide a separate CARD identity prefix.
	for index := range engine.players {
		player := &engine.players[index]
		for _, card := range player.Deck {
			if card.CardType != 0 && card.CardID != 0 {
				results = append(results, cardIdentityResult(player.MemberType, card))
			}
		}
		for _, card := range player.SupportDeck {
			if card.CardType != 0 && card.CardID != 0 {
				results = append(results, cardIdentityResult(player.MemberType, card))
			}
		}
	}
	results = append(results, engine.setupIdentityResults()...)
	// 26050 emits the caller's prior-wave reward vector after identity and
	// before turn state. It preserves order and duplicates without aggregating.
	for _, drop := range haveDrops {
		if !ValidBattleDrop(drop) {
			return nil, errors.New("invalid prior-wave resume reward")
		}
		results = append(results, BattleResult{Command: resultResumeHaveDrop, Args: []int64{
			int64(drop.RewardType), int64(drop.Num), int64(drop.RewardTypeID),
		}})
	}
	results = append(results, BattleResult{Command: resultResumeTurn, Args: []int64{
		int64(engine.turn), int64(engine.resumeBattleSide()),
	}})

	// a6078 follows the identity/turn prefix with cost and cost-block per user.
	for index := range engine.players {
		player := &engine.players[index]
		if player.GameOver {
			continue
		}
		results = append(results,
			BattleResult{Command: resultCost, Args: []int64{int64(player.MemberType), int64(engine.turnCost())}},
			BattleResult{Command: resultCostBlock, Args: []int64{int64(player.MemberType), int64(engine.playerEffectValue(player, "COST_BLOCK"))}},
		)
	}

	for index := range engine.players {
		player := &engine.players[index]
		hand, err := engine.resumePlayerHandResults(player)
		if err != nil {
			return nil, err
		}
		results = append(results,
			BattleResult{Command: resultResumeCardDeck, Args: []int64{
				int64(player.MemberType), int64(player.remainingDeckCount()),
			}},
			hand,
		)
		for _, hold := range player.CardHolds {
			selected := 0
			for _, cardType := range engine.selectedPlays[player.MemberType].CardTypes {
				if cardType != 0 && cardType == hold.Action.cardType {
					selected = 1
				}
			}
			results = append(results, BattleResult{Command: resultResumeHold, Args: []int64{
				int64(player.MemberType), int64(hold.Action.cardType), int64(hold.Action.sphereSlot), int64(selected),
				int64(hold.Remaining), int64(hold.Action.skill.ID), 0, int64(hold.Action.cardLevel),
			}})
		}
		for _, hold := range player.BlessHolds {
			results = append(results, resumeHoldResult(player.MemberType, hold))
		}
		for _, sphere := range player.Spheres {
			if sphere.SphereID != 0 && sphere.Type == sphereTypeChalice && sphere.ChalicePlayable {
				results = append(results,
					BattleResult{Command: resultChaliceSpherePlayable, Args: []int64{int64(player.MemberType), int64(sphere.Slot)}},
					BattleResult{Command: resultChaliceSphereReserve, Args: []int64{int64(player.MemberType), int64(player.ReservedChalice)}},
				)
				break
			}
		}
	}
	for index := range engine.players {
		player := &engine.players[index]
		results = append(results,
			playerBaseParameterResult(player),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(
				player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic,
				player.Recovery, player.Defense, player.MDefense,
				player.LimitAttack, player.LimitMagic, player.LimitRecovery,
			)},
		)
	}
	display, err := engine.selectionDisplayResults()
	if err != nil {
		return nil, err
	}
	results = append(results, display...)
	for index := range engine.players {
		player := &engine.players[index]
		if player.HP > 0 && player.BurstState != burstGaugeUnavailable {
			results = append(results, engine.burstStateResult(player))
		}
	}
	for index := range engine.players {
		player := &engine.players[index]
		if player.GameOver {
			results = append(results, BattleResult{Command: resultResumeGameOver, Args: []int64{int64(player.MemberType)}})
		}
	}

	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		dead := boolInt(enemy.HP <= 0)
		results = append(results,
			BattleResult{Command: resultResumeEnemy, Args: []int64{
				// 26050 reads 166d2 separately from current HP; revival keeps
				// this one-time break/drop marker set (6fab0), even without drops.
				int64(enemy.MemberType), int64(enemy.Parent), int64(dead), 0, 0, 0, int64(boolInt(enemy.DropResolved)),
			}},
		)
		results = append(results, resumeEnemyDropResults(enemy)...)
		results = append(results, resumeEnemyTrance(enemy)...)
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		results = append(results, BattleResult{Command: resultBattleParam, Args: battleParameterArgs(
			enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic,
			enemy.Recovery, enemy.Defense, enemy.MDefense,
			enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery,
		)},
		)
	}

	for index := range engine.players {
		results = appendResumeBuffs(results, engine.players[index].MemberType, engine.players[index].Effects)
	}
	for index := 0; index < engine.enemyCount; index++ {
		results = appendResumeBuffs(results, engine.enemies[index].MemberType, engine.enemies[index].Effects)
	}
	for index := range engine.players {
		live.players[index].CardDisplay = engine.players[index].CardDisplay
		for slot := range engine.players[index].Spheres {
			live.players[index].Spheres[slot].Display = engine.players[index].Spheres[slot].Display
		}
	}

	return results, nil
}

func appendResumeBuffs(results []BattleResult, member int, effects []battleEffect) []BattleResult {
	for _, list := range []int{0, 3, 5, 6} {
		for _, effect := range effects {
			if effect.ListType != list {
				continue
			}
			if row, ok := resumeBuffResult(member, effect); ok {
				results = append(results, row)
			}
		}
	}
	return results
}

func (engine *BattleEngine) resumeBattleSide() int {
	return engine.resumeSide
}

func (engine *BattleEngine) resumePlayerHandResults(player *battlePlayer) (BattleResult, error) {
	args := make([]int64, 1, 11)
	args[0] = int64(player.MemberType)
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			args = append(args, 0, 0)
			continue
		}
		if deckSlot < 1 || deckSlot > len(player.Deck) {
			return BattleResult{}, fmt.Errorf("combat resume member %d has invalid hand deck slot %d", player.MemberType, deckSlot)
		}
		card := player.Deck[deckSlot-1]
		// 26050 writes zero here; 3d680 later publishes actual CARD_UPDATE.
		args = append(args, int64(card.CardType), 0)
	}
	return BattleResult{Command: resultResumeCardHand, Args: args}, nil
}

func resumeHoldResult(memberType int, hold battleBlessHold) BattleResult {
	// The second (appended-skill) list in 26050 fixes the fourth value to
	// zero; it is not the repeat flag used by the BLESS execution producer.
	return BattleResult{Command: resultResumeHold, Args: []int64{
		int64(memberType), int64(hold.CardType), 0, 0,
		int64(hold.Remaining), int64(hold.Skill.ID), int64(hold.AppendIndex),
		int64(hold.CardLevel),
	}}
}

func resumeBuffResult(memberType int, effect battleEffect) (BattleResult, bool) {
	// Native 26050 serializes only lists 0/3/5/6 with positive duration.
	// PASSIVE stays in the engine, not a temporary UI buff replayed on resume.
	if effect.ListType != 0 && effect.ListType != 3 && effect.ListType != 5 && effect.ListType != 6 {
		return BattleResult{}, false
	}
	if effect.Remaining <= 0 {
		return BattleResult{}, false
	}
	code, exists := battleBuffCodes[effect.Function]
	if !exists {
		return BattleResult{}, false
	}
	// 26050 initializes four zeros, then 453cc fills only these five buff
	// codes. This is a UI restore record, not the producer's calculation
	// parameters or even a replay of the initial ResultCmd62 notification.
	parameters := nativeStatusUIParameters(code, effect)
	attributeFlags := 1 << combatAttributeCode(effect.Attribute)
	if code == 206 {
		attributeFlags = 1
	} // Original resume omits COVERING's initial attribute mask.
	return BattleResult{Command: resultResumeBuff, Args: []int64{
		int64(memberType), int64(effect.ListType), int64(code), int64(effect.Remaining), int64(battleBuffKind(code)),
		int64(retainedEffectParameterFlags(effect)), int64(attributeFlags),
		int64(parameters[0]), int64(parameters[1]), int64(parameters[2]), int64(parameters[3]),
	}}, true
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
