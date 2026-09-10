package multiplayer

import (
	"strings"
)

func (engine *BattleEngine) selectCombatSkillBranch(action battleAction, actions []battleAction, chainCounts map[string]int) CombatSkillDefinition {
	selected, _ := engine.selectCombatSkillBranchWithIndex(action, actions, chainCounts)
	return selected
}

func (engine *BattleEngine) selectCombatSkillBranchWithIndex(action battleAction, actions []battleAction, chainCounts map[string]int) (CombatSkillDefinition, int) {
	return engine.selectCombatSkillBranchMode(action, actions, chainCounts, true)
}

func (engine *BattleEngine) selectCombatSkillBranchMode(action battleAction, actions []battleAction, chainCounts map[string]int, allowRandom bool) (CombatSkillDefinition, int) {
	variants := engine.catalog.PlayerSkills[action.skill.ID]
	return engine.selectCombatSkillVariantsMode(variants, action, actions, chainCounts, allowRandom)
}

func (engine *BattleEngine) selectCombatSkillVariantsMode(variants []CombatSkillDefinition, action battleAction, actions []battleAction, chainCounts map[string]int, allowRandom bool) (CombatSkillDefinition, int) {
	if len(variants) == 0 {
		return CombatSkillDefinition{}, 0
	}
	var selected CombatSkillDefinition
	selectedIndex := 0
	matched := false
	for index, variant := range variants {
		// d3f50 rejects RANDOM in 7982a's mode0 lookup without a roll.
		// This switch is independent of 7982a's RNG save/restore argument.
		if !allowRandom && (variant.BranchCondition == "RANDOM" || variant.BranchCondition2 == "RANDOM") {
			continue
		}
		if variant.BranchCondition != "" && !engine.branchConditionSatisfied(variant.BranchCondition, variant.BranchParameters, action, actions, chainCounts) {
			continue
		}
		if variant.BranchCondition2 != "" && !engine.branchConditionSatisfied(variant.BranchCondition2, variant.BranchParameters2, action, actions, chainCounts) {
			continue
		}
		if !matched || variant.BranchPriority > selected.BranchPriority {
			selected = variant
			selectedIndex = index
			matched = true
		}
	}
	return selected, selectedIndex
}

func (engine *BattleEngine) branchConditionSatisfied(condition string, parameters [5]string, action battleAction, actions []battleAction, chainCounts map[string]int) bool {
	actor := &engine.players[action.memberType-1]
	switch condition {
	case "DECK_COMBO_COUNT":
		if action.sphereSlot != 0 {
			return combatValueInRange(0, parameters[0], parameters[1])
		}
		return combatValueInRange(maxCombatChain(action.skill.Attribute, chainCounts), parameters[0], parameters[1])
	case "TURN":
		return combatValueInRange(engine.turn, parameters[0], parameters[1])
	case "SELF_HP_PER":
		return combatValueInRange(actor.HP*100/maxInt(1, actor.MaxHP), parameters[0], parameters[1])
	case "FRIEND_HP_PER":
		for index := range engine.players {
			player := &engine.players[index]
			if player.HP > 0 && combatValueInRange(player.HP*100/maxInt(1, player.MaxHP), parameters[0], parameters[1]) {
				return true
			}
		}
		return false
	case "TARGET_ATTR":
		return engine.branchTargetAttributeMatches(action.target, parameters)
	case "SELF_BUFF":
		return branchHasStatus(actor.Effects, true, parameters)
	case "SELF_BLESS":
		// d2aab -> d2976 -> 43034 selects BLESS, not every append card.
		return combatValueInRange(playerAppendCardCount(actor, 22, parameters[0]), parameters[1], parameters[2])
	case "TARGET_BUFF", "TARGET_DEBUFF":
		return branchHasStatus(engine.battleMemberEffects(action.target), condition == "TARGET_BUFF", parameters)
	case "USER_SIDE_DEBUFF":
		return engine.branchSideHasStatus(true, false, parameters)
	case "ENEMY_SIDE_DEBUFF":
		return engine.branchSideHasStatus(false, false, parameters)
	case "SELF_OTHER_PLAY_ATTR", "SELF_OTHER_PLAY_RARITY", "SELF_OTHER_PLAY_SKILL_KIND":
		if combatNullValue(parameters[0]) {
			return false
		}
		return engine.countSelectedActions(actions, action, condition, parameters[0], false) >= combatParameterInt(parameters[1])
	case "SELF_OTHER_PLAY_NUM":
		return combatValueInRange(engine.countSelectedActions(actions, action, "", "", false), parameters[0], parameters[1])
	case "SELF_PLAY_MOST_LOW_COST":
		selected := selectedActionsByMember(actions, action.memberType)
		minimum := 2147483647 // d2866 retains INT_MAX when no card is selected.
		for _, other := range selected {
			minimum = minInt(minimum, other.skill.Cost)
		}
		return combatValueInRange(minimum, parameters[0], parameters[1])
	case "SELF_PLAY_COST_TOTAL":
		total := 0
		for _, other := range selectedActionsByMember(actions, action.memberType) {
			total += other.skill.Cost
		}
		return combatValueInRange(total, parameters[0], parameters[1])
	case "SELF_PLAY_COST_NUM":
		count := 0
		for _, other := range selectedActionsByMember(actions, action.memberType) {
			if combatValueInRange(other.skill.Cost, parameters[0], parameters[1]) {
				count++
			}
		}
		return combatValueInRange(count, parameters[2], parameters[3])
	case "SELF_NOT_PLAY_HAND_NUM":
		return combatValueInRange(engine.unplayedHandCount(actor, actions), parameters[0], parameters[1])
	case "BUFF_EXEC":
		for _, wanted := range parameters {
			kind := combatTriggerBuffKind(wanted)
			if kind > 0 && kind < len(actor.ExecutedBuffKinds) && int32(actor.ExecutedBuffKinds[kind]) > 0 {
				return true
			}
		}
		return false
	case "FRIEND_PLAY_NUM":
		count := 0
		for _, other := range actions {
			if other.cardType != 0 && other.sphereSlot == 0 {
				count++
			}
		}
		return combatValueInRange(count, parameters[0], parameters[1])
	case "FRIEND_PLAY_TAG":
		count := 0
		for _, other := range actions {
			// d3bcc counts selected cards across the whole user side,
			// including this card and retained selections of KO members.
			// 49e3e reads the card's eight profile tags. Skill.Groups are
			// unrelated target-hand filters; spheres have no card profile.
			if other.sphereSlot == 0 && combatCardHasProfileTag(engine.catalog.Cards[other.cardID],
				combatParameterInt(parameters[0]), combatParameterInt(parameters[1]), combatParameterInt(parameters[2])) {
				count++
			}
		}
		return count >= combatParameterInt(parameters[3])
	case "FRIEND_PLAY_MOST_LOW_COST":
		minimum := 2147483647
		for _, other := range actions {
			if other.cardType != 0 && other.sphereSlot == 0 {
				minimum = minInt(minimum, other.skill.Cost)
			}
		}
		return combatValueInRange(minimum, parameters[0], parameters[1])
	case "SELF_MAIN_DECK_ATTR", "SELF_MAIN_DECK_SKILL_KIND":
		count := 0
		for _, card := range actor.Deck {
			skill, _, err := engine.catalog.CardSkill(card.CardID, actor.ArthurType)
			if err != nil {
				continue
			}
			if condition == "SELF_MAIN_DECK_SKILL_KIND" && (strings.EqualFold(skill.Kind, parameters[0]) || strings.TrimSpace(parameters[1]) != "" && !strings.EqualFold(parameters[1], "NULL") && strings.EqualFold(skill.Kind, parameters[1])) {
				count++
			} else if condition == "SELF_MAIN_DECK_ATTR" && (combatAttributeMatches(skill.Attribute, parameters[0]) || strings.TrimSpace(parameters[1]) != "" && !strings.EqualFold(parameters[1], "NULL") && combatAttributeMatches(skill.Attribute, parameters[1])) {
				count++
			}
		}
		return count >= maxInt(1, combatParameterInt(parameters[2]))
	case "RANDOM":
		return int(engine.rng.next()%10000) < combatParameterInt(parameters[0])*100
	default:
		return false
	}
}

func combatValueInRange(value int, lowerText string, upperText string) bool {
	lower := combatParameterInt(lowerText)
	upper := combatParameterInt(upperText)
	if lowerText != "" && value < lower {
		return false
	}
	return upperText == "" || upper == 0 || value <= upper
}

func combatAttributeMatches(attributes string, wanted string) bool {
	for _, attribute := range splitCombatAttributes(attributes) {
		if strings.EqualFold(attribute, wanted) {
			return true
		}
	}
	return false
}

func combatEffectCount(effects []battleEffect, function string, attribute string) int {
	count := 0
	for _, effect := range effects {
		if (function == "" || strings.EqualFold(effect.Function, function)) && (attribute == "" || strings.EqualFold(effect.Attribute, attribute)) {
			count++
		}
	}
	return count
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) countSelectedActions(actions []battleAction, current battleAction, condition string, value string, friends bool) int {
	count := 0
	for _, other := range actions {
		// 40908 stores selected cards at +8/count+30, and spheres in a
		// separate +34/count+3c list. These predicates read only cards.
		if other.cardType == 0 || other.sphereSlot != 0 {
			continue
		}
		if other.cardType == current.cardType && other.memberType == current.memberType {
			continue
		}
		if friends && other.memberType == current.memberType || !friends && other.memberType != current.memberType {
			continue
		}
		matches := condition == ""
		switch condition {
		case "SELF_OTHER_PLAY_ATTR":
			for _, component := range splitCombatAttributes(value) {
				matches = matches || combatAttributeMatches(other.skill.Attribute, component)
			}
		case "SELF_OTHER_PLAY_RARITY":
			matches = combatRarityCode(engine.catalog.Cards[other.cardID].Rarity) >= combatRarityCode(value)
		case "SELF_OTHER_PLAY_SKILL_KIND":
			matches = strings.EqualFold(other.skill.Kind, value)
		}
		if matches {
			count++
		}
	}
	return count
}

func selectedActionsByMember(actions []battleAction, memberType int) []battleAction {
	selected := make([]battleAction, 0, len(actions))
	for _, action := range actions {
		if action.memberType == memberType && action.cardType != 0 && action.sphereSlot == 0 {
			selected = append(selected, action)
		}
	}
	return selected
}

func (engine *BattleEngine) unplayedHandCount(player *battlePlayer, actions []battleAction) int {
	played := make(map[int]struct{})
	for _, action := range actions {
		if action.memberType == player.MemberType {
			played[action.cardType] = struct{}{}
		}
	}
	count := 0
	for _, deckSlot := range player.Hand {
		if deckSlot <= 0 || deckSlot > len(player.Deck) {
			continue
		}
		cardType := player.Deck[deckSlot-1].CardType
		if _, exists := played[cardType]; !exists {
			count++
		}
	}
	return count
}
