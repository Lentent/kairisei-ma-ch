package multiplayer

import "fmt"

// CARD_PASSIVE (column 33) is separate from EX. 5ea1e executes its own
// PASSIVE skill through 7adb0; the original table only used BEGINNING_DRAW,
// but D497 also confirms DAMAGE_BOOST at this same native entry point.
func (catalog *CombatCatalog) cardMainPassive(card CombatCardDefinition) (CombatSkillDefinition, []CombatSkillRole, error) {
	if card.PassiveSkillID == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	variants := catalog.SupportSkills[card.PassiveSkillID]
	if len(variants) != 1 || (variants[0].Target != "SELF" && variants[0].Target != "USER_ALL") ||
		variants[0].BranchCondition != "" || variants[0].BranchCondition2 != "" {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d passive %d has unsupported target/branches", card.ID, card.PassiveSkillID)
	}
	roles := catalog.SupportSkillRoles[variants[0].FunctionID]
	if len(roles) == 0 || len(roles) > 5 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d passive %d has unsupported roles", card.ID, card.PassiveSkillID)
	}
	for _, role := range roles {
		if !mainCardPassiveFunctionRegistered(role.Function) || role.Target != "SELECT" || role.ExcludeSelf ||
			(role.Function == "BEGINNING_DRAW" && variants[0].Target != "SELF") {
			return CombatSkillDefinition{}, nil, fmt.Errorf("card %d passive %d has unsupported roles", card.ID, card.PassiveSkillID)
		}
	}
	return variants[0], roles, nil
}

func mainCardPassiveFunctionRegistered(function string) bool {
	switch function {
	case "BEGINNING_DRAW", "DAMAGE_BOOST":
		return true
	default:
		return false
	}
}

func (catalog *CombatCatalog) cardBeginningDraw(card CombatCardDefinition) (bool, error) {
	_, roles, err := catalog.cardMainPassive(card)
	for _, role := range roles {
		if role.Function == "BEGINNING_DRAW" {
			return true, err
		}
	}
	return false, err
}

// 5ea1e runs after each user's EX/chalice passives. 9974f/88a20 records
// BEGINNING_DRAW in PASSIVE with duration zero and binds it to CARD_TYPE.
// It remains in state across turns; only the first-battle shuffle consumes
// its priority. Installing it in later waves must not reshuffle held cards.
func (engine *BattleEngine) executeMainCardPassives(owner *battlePlayer) ([]BattleResult, error) {
	var results []BattleResult
	for _, card := range owner.Deck {
		definition := engine.catalog.Cards[card.CardID]
		skill, roles, err := engine.catalog.cardMainPassive(definition)
		if err != nil {
			return nil, err
		}
		if len(roles) == 0 {
			continue
		}
		target := owner.MemberType
		if skill.Target == "USER_ALL" {
			target = 0
		}
		header, err := engine.playerCardSkillResult(battleAction{
			memberType: owner.MemberType, cardType: card.CardType, cardID: card.CardID,
			cardLevel: card.Level, target: target, skill: skill,
		}, 0)
		if err != nil {
			return nil, err
		}
		header.Args[6], header.Args[10] = 0, 1
		skillResults := []BattleResult{header}
		for _, role := range roles {
			role.SourceSkillID = skill.ID
			effect := battleEffect{Function: "BEGINNING_DRAW", ListType: 1,
				Source: owner.MemberType, SourceSkillID: skill.ID, CardType: card.CardType, RoleIndex: role.RoleIndex, AppliedTurn: engine.turn}
			flags := 1
			if role.Function == "DAMAGE_BOOST" {
				effect, err = sphereSupportEffect(role, card.Level, engine.turn, owner.MemberType)
				if err != nil {
					return nil, err
				}
				flags = sphereSupportAttributeFlags(effect.Attribute)
			}
			for i := range engine.players {
				target := &engine.players[i]
				if target.HP <= 0 || target.GameOver || (skill.Target == "SELF" && target.MemberType != owner.MemberType) ||
					!combatRoleAllowsTarget(role, owner.MemberType, target.MemberType, target.Attribute) {
					continue
				}
				target.Effects = append(target.Effects, effect)
				// Native queries each target's base parameters before draining
				// the passive 69/6 rows after the complete skill's role set.
				skillResults = append(skillResults, playerBaseParameterResult(target), battleBuffResultWithListType(target.MemberType, role.RoleIndex, 1,
					battleBuffCodes[role.Function], 0, flags, 0, 0, 0, 0))
			}
		}
		results = append(results, engine.projectSkillStatusResults(skillResults)...)
		results = append(results, engine.finishTranceReactions(skill.Cost)...)
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
