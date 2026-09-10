package multiplayer

import "fmt"

// CardCsvData's CARD_PASSIVE (column 33) is separate from the support-deck
// skill. The current CN main-card passive family only produces BEGINNING_DRAW
// (9974f -> buff 412). Resolve the actual support master; never infer this from
// rarity, character name or a hard-coded card-ID list.
func (catalog *CombatCatalog) cardBeginningDraw(card CombatCardDefinition) (bool, error) {
	if card.PassiveSkillID == 0 {
		return false, nil
	}
	variants := catalog.SupportSkills[card.PassiveSkillID]
	if len(variants) != 1 || variants[0].Target != "SELF" ||
		variants[0].BranchCondition != "" || variants[0].BranchCondition2 != "" {
		return false, fmt.Errorf("card %d passive %d has unsupported target/branches", card.ID, card.PassiveSkillID)
	}
	roles := catalog.SupportSkillRoles[variants[0].FunctionID]
	if len(roles) != 1 || roles[0].Function != "BEGINNING_DRAW" || roles[0].Target != "SELECT" || roles[0].ExcludeSelf {
		return false, fmt.Errorf("card %d passive %d has unsupported roles", card.ID, card.PassiveSkillID)
	}
	return true, nil
}

// 5ea1e runs after each user's EX/chalice passives. 9974f/88a20 records
// BEGINNING_DRAW in PASSIVE with duration zero and binds it to CARD_TYPE.
// It remains in state across turns; only the first-battle shuffle consumes
// its priority. Installing it in later waves must not reshuffle held cards.
func (engine *BattleEngine) executeMainCardPassives(owner *battlePlayer) ([]BattleResult, error) {
	var results []BattleResult
	for _, card := range owner.Deck {
		definition := engine.catalog.Cards[card.CardID]
		enabled, err := engine.catalog.cardBeginningDraw(definition)
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}
		skill := engine.catalog.SupportSkills[definition.PassiveSkillID][0]
		role := engine.catalog.SupportSkillRoles[skill.FunctionID][0]
		header, err := engine.playerCardSkillResult(battleAction{
			memberType: owner.MemberType, cardType: card.CardType, cardID: card.CardID,
			cardLevel: card.Level, target: owner.MemberType, skill: skill,
		}, 0)
		if err != nil {
			return nil, err
		}
		header.Args[6], header.Args[10] = 0, 1
		owner.Effects = append(owner.Effects, battleEffect{Function: "BEGINNING_DRAW", ListType: 1,
			Source: owner.MemberType, SourceSkillID: skill.ID, CardType: card.CardType, RoleIndex: role.RoleIndex, AppliedTurn: engine.turn})
		// 88a20's PASSIVE registration emits the base query before the
		// deferred 69/6 pair, even when BEGINNING_DRAW changes no parameter.
		results = append(results, engine.projectSkillStatusResults([]BattleResult{header, playerBaseParameterResult(owner), battleBuffResultWithListType(owner.MemberType, role.RoleIndex, 1,
			battleBuffCodes["BEGINNING_DRAW"], 0, 1, 0, 0, 0, 0)})...)
		engine.nativeSkillSerial++
		display, err := engine.refreshPassiveDisplayPowers()
		if err != nil {
			return nil, err
		}
		results = append(results, display...)
	}
	return results, nil
}

// FUN_0004a66e starts with deck/card-type order, pulls the first five eligible
// cards into a fixed prefix, then area_shuffle only shuffles the suffix.
// Unlike ordinary shuffle, area_shuffle consumes the final modulo-one draw.
// This entry is ONLY used for the first battle: 5db7a sets the 0x44 latch;
// subsequent waves and depleted-pool reshuffles use ordinary shuffle.
func (engine *BattleEngine) shuffleOpeningDeck(player *battlePlayer, guaranteed [10]bool) {
	count := 0
	for index := range player.DeckOrder {
		if guaranteed[player.DeckOrder[index]] {
			player.DeckOrder[index], player.DeckOrder[count] = player.DeckOrder[count], player.DeckOrder[index]
			count++
			if count == len(player.Hand) {
				break
			}
		}
	}
	if count == 0 {
		engine.shuffleDeck(player)
		return
	}
	for end := len(player.DeckOrder); count < end; end-- {
		swapIndex := count + int(engine.rng.next()%uint32(end-count))
		player.DeckOrder[end-1], player.DeckOrder[swapIndex] = player.DeckOrder[swapIndex], player.DeckOrder[end-1]
	}
}
