package multiplayer

import (
	"errors"
	"fmt"
	"strings"
)

const (
	burstGaugeUnavailable = iota
	burstGaugeNormal
	burstGaugeBurst
	burstGaugeBreak
)

type burstSkillSubmission struct {
	Target    int
	CardTypes [5]int
	SkillSlot int
}

func (engine *BattleEngine) buddyResults(player *battlePlayer) []BattleResult {
	results := make([]BattleResult, 0, len(player.Buddies))
	for _, buddy := range player.Buddies {
		if buddy.BuddyID == 0 {
			continue
		}
		results = append(results, BattleResult{Command: resultBuddy, Args: []int64{
			int64(player.MemberType), int64(buddy.BuddyType), int64(buddy.BuddyID),
		}})
	}
	return results
}

func (engine *BattleEngine) burstStateResult(player *battlePlayer) BattleResult {
	maximum := 0
	if player.BurstState == burstGaugeNormal || player.BurstState == burstGaugeBurst {
		maximum = engine.catalog.BurstGauge.Maximum
	}
	return BattleResult{Command: resultBurstState, Args: []int64{
		int64(player.MemberType), int64(player.BurstState), int64(player.Burst), int64(maximum), int64(player.BurstBreak),
	}}
}

func (engine *BattleEngine) burstStateChangeResult(player *battlePlayer) BattleResult {
	maximum := 0
	if player.BurstState == burstGaugeNormal || player.BurstState == burstGaugeBurst {
		maximum = engine.catalog.BurstGauge.Maximum
	}
	return BattleResult{Command: resultBurstStateChange, Args: []int64{
		int64(player.MemberType), int64(player.BurstState), int64(maximum), int64(player.BurstBreak),
	}}
}

// transitionBurstStates mirrors native FUN_000a8500. Gauge changes do not
// transition immediately: the state boundary is evaluated by TurnPhase.
func (engine *BattleEngine) transitionBurstStates() ([]BattleResult, error) {
	results := make([]BattleResult, 0, len(engine.players))
	var entered []*battlePlayer
	for index := range engine.players {
		player := &engine.players[index]
		if player.MemberType == 0 || player.HP <= 0 {
			continue
		}
		switch player.BurstState {
		case burstGaugeBurst:
			if player.Burst < 1 {
				player.BurstState = burstGaugeBreak
				player.BurstBreak = engine.catalog.BurstGauge.BreakTurns
				results = append(results, engine.releaseBurstPassives(player)...)
				results = append(results, engine.burstStateChangeResult(player))
			}
		case burstGaugeBreak:
			if player.BurstBreak < 1 {
				player.BurstState = burstGaugeNormal
				results = append(results, engine.burstStateChangeResult(player))
			}
		case burstGaugeNormal:
			if player.Burst >= engine.catalog.BurstGauge.NormalThreshold {
				player.BurstState = burstGaugeBurst
				results = append(results, engine.burstStateChangeResult(player))
				entered = append(entered, player)
			}
		}
	}
	// FUN_000a8500 finishes all state transitions before FUN_0005f600 collects
	// and sorts the new entrants' buddy passives as one action list.
	passives, err := engine.executeBurstPassives(entered...)
	return append(results, passives...), err
}

func (engine *BattleEngine) executeBurstPassives(players ...*battlePlayer) ([]BattleResult, error) {
	actions := make([]battleAction, 0, len(players)*5)
	for _, player := range players {
		for _, equipped := range player.Buddies {
			if equipped.BuddyID == 0 || equipped.Level <= 0 {
				continue
			}
			buddy, exists := engine.catalog.Buddies[equipped.BuddyID]
			if !exists || buddy.PassiveSkillID == 0 {
				continue
			}
			variants := engine.catalog.BurstSkills[buddy.PassiveSkillID]
			if len(variants) == 0 {
				return nil, fmt.Errorf("combat burst passive skill %d is unavailable", buddy.PassiveSkillID)
			}
			skill := variants[0]
			roles := engine.catalog.BurstSkillRoles[skill.FunctionID]
			if len(roles) == 0 {
				return nil, fmt.Errorf("combat burst passive role %d is unavailable", skill.FunctionID)
			}
			_, ok := combatSkillTargetCode(skill.Target)
			if !ok {
				return nil, fmt.Errorf("combat burst passive %d target %q is unsupported", skill.ID, skill.Target)
			}
			target := player.MemberType
			if skill.Target != "SELF" && skill.Target != "USER_ONE" {
				target = 0
			}
			actions = append(actions, battleAction{
				memberType: player.MemberType, cardLevel: equipped.Level, target: target,
				skill: skill, roles: roles,
			})
		}
	}
	engine.sortBurstPassiveActions(actions)
	results := make([]BattleResult, 0, len(actions)*4)
	for _, action := range actions {
		action.skillBonus = &battleSkillBonus{}
		targetCode, _ := combatSkillTargetCode(action.skill.Target)
		results = append(results, BattleResult{Command: resultBurstSkill, Args: []int64{
			int64(action.memberType), 0, int64(action.skill.ID), int64(action.target), int64(action.cardLevel),
			int64(targetCode), 0, 0, int64(action.skill.FunctionID), 0, 1,
		}})
		skillResults, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill, action.roles, func(role CombatSkillRole) ([]BattleResult, error) {
			return engine.executeBurstRoleWithListType(action, role, nil, 6)
		})
		if err != nil {
			return nil, err
		}
		results = append(results, skillResults...)
		engine.nativeSkillSerial++
		display, err := engine.refreshBattleDisplayPowers()
		if err != nil {
			return nil, err
		}
		results = append(results, display...)
	}
	return results, nil
}

// FUN_0005cf95 differs from ordinary card ordering: every equal-priority
// comparison consumes a random bit, including two buddies of the same actor.
func (engine *BattleEngine) sortBurstPassiveActions(actions []battleAction) {
	for boundary := 0; boundary < len(actions)-1; boundary++ {
		for index := len(actions) - 2; index >= boundary; index-- {
			left, right := actions[index], actions[index+1]
			swap := left.skill.PriorityPVE > right.skill.PriorityPVE
			if left.skill.PriorityPVE == right.skill.PriorityPVE {
				swap = engine.rng.next()&1 != 0
			}
			if swap {
				actions[index], actions[index+1] = right, left
			}
		}
	}
}

func (engine *BattleEngine) releaseBurstPassives(player *battlePlayer) []BattleResult {
	ensurePlayerBaseParameters(player)
	kept := player.Effects[:0]
	results := make([]BattleResult, 0, 8)
	released := make(map[[3]int]struct{})
	for _, effect := range player.Effects {
		if effect.ListType != 6 && effect.ListType != 7 {
			kept = append(kept, effect)
			continue
		}
		revertPlayerEffect(player, effect)
		if code, exists := battleBuffCodes[effect.Function]; exists && effect.ListType == 6 {
			key, result := naturalEffectReleaseResult(player.MemberType, effect, code)
			result.Args[1] = 6
			if _, duplicate := released[key]; !duplicate {
				results = append(results, result)
				released[key] = struct{}{}
			}
		}
	}
	player.Effects = kept
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	// FUN_000735e4 clears BURST_PASSIVE and CARD, with grouped 72 followed
	// unconditionally by 5/6. No per-entry 71; 504 comes afterwards.
	return append(results,
		playerBaseParameterResult(player),
		BattleResult{Command: resultBattleParam, Args: battleParameterArgs(
			player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
			player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
		)},
	)
}

func (engine *BattleEngine) tickBurstCounters() {
	for index := range engine.players {
		tickPlayerBurstCounters(&engine.players[index])
	}
}

func tickPlayerBurstCounters(player *battlePlayer) {
	if player.GameOver {
		return
	}
	for slot := range player.BurstRecharge {
		if player.BurstRecharge[slot] > 0 {
			player.BurstRecharge[slot]--
		}
	}
	if player.BurstBreak > 0 {
		player.BurstBreak--
	}
}

func (engine *BattleEngine) burstPlayConditionResults() []BattleResult {
	results := make([]BattleResult, 0, len(engine.players)*3)
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		leader := player.Buddies[0]
		definition, exists := engine.catalog.Buddies[leader.BuddyID]
		if !exists {
			continue
		}
		for skillIndex, skill := range definition.Skills {
			if skill.SkillID == 0 {
				continue
			}
			playable := skill.RequiredLevel <= leader.Level && player.BurstState == burstGaugeBurst &&
				player.Burst >= skill.GaugeCost && player.BurstRecharge[skillIndex] < 1
			playableValue := int64(0)
			if playable {
				playableValue = 1
			}
			results = append(results, BattleResult{Command: resultBurstCondition, Args: []int64{
				int64(player.MemberType), int64(skillIndex + 1), playableValue,
				int64(player.BurstRecharge[skillIndex]), int64(skill.GaugeCost),
			}})
		}
	}
	return results
}

func (engine *BattleEngine) effectiveCardCost(player *battlePlayer, cardType int, baseCost int) int {
	if cardType < 0 || cardType >= len(player.CardCostDown) {
		return maxInt(0, baseCost)
	}
	return maxInt(0, baseCost-player.CardCostDown[cardType])
}

func (engine *BattleEngine) addBurstGauge(player *battlePlayer, amount int) bool {
	if player.GameOver || player.BurstState == burstGaugeUnavailable || player.BurstBreak > 0 ||
		(player.BurstState == burstGaugeBurst && player.Burst < 1) {
		return false
	}
	player.Burst = maxInt(0, player.Burst+amount)
	if player.BurstState == burstGaugeNormal || player.BurstState == burstGaugeBurst {
		player.Burst = minInt(engine.catalog.BurstGauge.Maximum, player.Burst)
	}
	return true
}

// 5ee36 visits the registered selection before action sorting/execution.
// 49c83 is the original card cost, before the independent Buddy cost discount.
func (engine *BattleEngine) burstSelectionGaugeResults(actions []battleAction) []BattleResult {
	var rows []BattleResult
	for i := range engine.players {
		player := &engine.players[i]
		if player.HP <= 0 || player.GameOver {
			continue
		}
		for _, action := range actions {
			if action.memberType != player.MemberType {
				continue
			}
			bucket := minInt(4, maxInt(0, action.skill.Cost-1))
			if engine.addBurstGauge(player, engine.catalog.BurstGauge.ReactionRates[bucket]) {
				rows = append(rows, BattleResult{Command: resultBurstGaugeState, Args: []int64{int64(player.MemberType), int64(player.Burst)}})
			}
		}
	}
	return rows
}

// 5ef2e is called once per incoming attack role (all normal/enchant hits),
// and separately by opponent HP-cut, DOT and card traps. It does not run for
// every HP mutation or for reflected damage back to a player attacker.
func (engine *BattleEngine) burstDamageGaugeResults(player *battlePlayer, damage int) []BattleResult {
	if damage <= 0 || player.MaxHP <= 0 || player.BurstState != burstGaugeBurst {
		return nil
	}
	percent := int64(damage) * 100 / int64(player.MaxHP)
	decrease := maxInt(1, int(percent*int64(engine.catalog.BurstGauge.DamageReduction)/100))
	if !engine.addBurstGauge(player, -decrease) {
		return nil
	}
	return []BattleResult{{Command: resultBurstGaugeState, Args: []int64{int64(player.MemberType), int64(player.Burst)}}}
}

func burstRoleRegistersCard(function string) bool {
	switch function {
	case "NEED_COST_DOWN_BURST", "ATTACK_MULTISTAGE", "ADD_ATK_OP_PIERCING",
		"DAMAGE_BOOST_ORDER_TRIBAL", "ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL":
		return true
	default:
		return false
	}
}

func (engine *BattleEngine) registerBurstCardSkill(player *battlePlayer, cardType int, skillID int) error {
	if cardType < 1 || cardType >= len(player.CardBurstSkills) {
		return fmt.Errorf("combat burst card type %d is invalid", cardType)
	}
	player.CardBurstSkills[cardType] = append(player.CardBurstSkills[cardType], skillID)
	return nil
}

func (engine *BattleEngine) attachBurstCardModifiers(action *battleAction) error {
	if action == nil || action.memberType < 1 || action.memberType > len(engine.players) || action.cardType < 1 {
		return nil
	}
	player := &engine.players[action.memberType-1]
	if len(player.CardBurstSkills[action.cardType]) > 0 {
		// Callers may hold a catalog slice. Modifiers must remain local to this
		// action, including in display previews and simultaneous rooms.
		action.roles = append([]CombatSkillRole(nil), action.roles...)
	}
	for _, burstSkillID := range player.CardBurstSkills[action.cardType] {
		variants := engine.catalog.BurstSkills[burstSkillID]
		if len(variants) == 0 {
			return fmt.Errorf("combat burst card skill %d is unavailable", burstSkillID)
		}
		roles := engine.catalog.BurstSkillRoles[variants[0].FunctionID]
		if len(roles) == 0 {
			return fmt.Errorf("combat burst card role %d is unavailable", variants[0].FunctionID)
		}
		for _, burstRole := range roles {
			switch burstRole.Function {
			case "NEED_COST_DOWN_BURST":
				// Cost was committed at BurstSkillExec and is consumed by Submit.
			case "ATTACK_MULTISTAGE":
				for index := range action.roles {
					if action.roles[index].Function != "ATTACK_AA" ||
						!damagePhysicsMatches(burstRole.Parameters[1], action.skill.DamageKind) {
						continue
					}
					action.roles[index].Parameters[4] = fmt.Sprintf("%d", maxInt(
						combatParameterInt(action.roles[index].Parameters[4]), combatParameterInt(burstRole.Parameters[2]),
					))
				}
			case "ADD_ATK_OP_PIERCING":
				operator := burstRole
				operator.Function = "ATK_OP_PIERCING"
				operator.Parameters = [10]string{}
				operator.Parameters[0] = burstRole.Parameters[1]
				action.roles = append(action.roles, operator)
			case "DAMAGE_BOOST_ORDER_TRIBAL", "ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL":
				action.roles = append(action.roles, burstRole)
			default:
				return fmt.Errorf("combat burst card modifier %q is unsupported", burstRole.Function)
			}
		}
	}
	return nil
}

// 466e2/464e4 pass 6eeb6's enemy-party vector to 43534. It checks presence
// and tribal identity, not the selected target or current HP. A destroyed
// part can therefore still satisfy this condition for the current wave.
func (engine *BattleEngine) enemyPartyHasRace(raceID int) bool {
	if raceID <= 0 {
		return true
	}
	if engine.catalog == nil {
		return false
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.MemberType == 0 {
			continue
		}
		if definition, exists := engine.catalog.Enemies[enemy.EnemyID]; exists && definition.RaceID == raceID {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) deckSkillBonusCount(player *battlePlayer, attribute string, tag int) int {
	if player == nil || engine.catalog == nil {
		return 0
	}
	count := 0
	for _, card := range player.Deck {
		definition, exists := engine.catalog.Cards[card.CardID]
		if !exists {
			continue
		}
		// 7ea92 checks both raw skills, regardless of the actor's job;
		// either skill's attribute OR a positive profile tag is enough.
		matches := tag > 0 && combatCardHasProfileTag(definition, tag)
		for _, skillID := range [...]int{definition.NormalSkillID, definition.ArthurSkillID} {
			if variants := engine.catalog.PlayerSkills[skillID]; len(variants) > 0 {
				for _, component := range splitCombatAttributes(attribute) {
					matches = matches || combatAttributeMatches(variants[0].Attribute, component)
				}
			}
		}
		if matches {
			count++
		}
	}
	return count
}

type battleSkillBonus struct {
	active    bool
	limit     bool
	parameter string
	attribute string
	tag       int
	percent   int32
	fixed     int32
}

func (engine *BattleEngine) fixedParameterActionValue(action battleAction, role CombatSkillRole, chainCount int) int {
	first, second := fixedBuffRoleSegments(role, action.cardLevel)
	bonus := action.skillBonus
	if bonus != nil && bonus.active && bonus.limit == (role.Function == "PARAM_LIMIT_BREAK_FIXED") &&
		strings.EqualFold(bonus.parameter, role.Parameters[1]) {
		// 7ea92 modifies ONLY first, with per-card truncation. p1=0 uses
		// p2+p3*level as the flat amount per matching card. Consume even
		// when no card matches, but do not consume on the wrong parameter.
		count := engine.deckSkillBonusCount(&engine.players[action.memberType-1], bonus.attribute, bonus.tag)
		first = deckBonusSegment(first, bonus.percent, bonus.fixed, count)
		bonus.active = false
	}
	value := first + second
	if chainCount > 1 {
		value += int32(role.ChainRate) * int32(chainCount-1)
	}
	return int(value)
}

func deckBonusSegment(value, percent, fixed int32, count int) int32 {
	perCard := fixed
	if percent != 0 {
		perCard = (percent * value) / 100
	}
	return value + perCard*int32(count)
}

func (engine *BattleEngine) parameterBoostedValue(action battleAction, role CombatSkillRole, function string, value int) int {
	actor := &engine.players[action.memberType-1]
	fixed, rate := sphereSupportBoostTerms(actor.Effects, function, action.skill.Attribute, role.Parameters[1], action.skill.Cost, action.cardType)
	for _, modifier := range action.roles {
		if modifier.Function != function+"_ORDER_TRIBAL" ||
			!strings.EqualFold(role.Parameters[1], modifier.Parameters[6]) ||
			!supportAttributeMatches(modifier.Parameters[5], action.skill.Attribute) ||
			!engine.enemyPartyHasRace(combatParameterInt(modifier.Parameters[9])) {
			continue
		}
		costMin, costMax := combatParameterInt(modifier.Parameters[7]), combatParameterInt(modifier.Parameters[8])
		if action.skill.Cost < costMin || (costMax != 0 && action.skill.Cost > costMax) {
			continue
		}
		// 9b908: p1+p2*L/1000 is fixed; p3+p4*L is the rate. All
		// active registered CN parameter tribal rows have zero p2/p4 growth.
		fixed += combatParameterInt(modifier.Parameters[1]) + combatParameterInt(modifier.Parameters[2])*action.cardLevel/1000
		rate += combatParameterInt(modifier.Parameters[3]) + combatParameterInt(modifier.Parameters[4])*action.cardLevel
	}
	// 4641c + 464e4 accumulate into the same fixed/rate pair before 45070.
	return int((int32(value+fixed) * int32(1000+rate)) / 1000)
}

func (engine *BattleEngine) ExecuteBurst(memberType int, submission burstSkillSubmission) ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseUser {
		return nil, errors.New("combat burst skill phase is unavailable")
	}
	if memberType < 1 || memberType > maxRoomMembers {
		return nil, errors.New("combat burst skill member is invalid")
	}
	if submission.SkillSlot < 1 || submission.SkillSlot > 3 {
		return nil, errors.New("combat burst skill slot is invalid")
	}
	player := &engine.players[memberType-1]
	if player.HP <= 0 || player.BurstState != burstGaugeBurst {
		return nil, errors.New("combat burst skill gauge state is unavailable")
	}
	leader := player.Buddies[0]
	definition, exists := engine.catalog.Buddies[leader.BuddyID]
	if !exists {
		return nil, errors.New("combat burst skill leader buddy is unavailable")
	}
	buddySkill := definition.Skills[submission.SkillSlot-1]
	if buddySkill.SkillID == 0 || buddySkill.RequiredLevel > leader.Level {
		return nil, errors.New("combat burst skill is not released")
	}
	if player.BurstRecharge[submission.SkillSlot-1] > 0 || player.Burst < 1 || player.Burst < buddySkill.GaugeCost {
		return nil, errors.New("combat burst skill gauge or recharge is unavailable")
	}
	variants := engine.catalog.BurstSkills[buddySkill.SkillID]
	if len(variants) == 0 {
		return nil, fmt.Errorf("combat burst skill %d is unavailable", buddySkill.SkillID)
	}
	skill, selectedCards, err := engine.selectBurstSkillVariant(player, variants, submission)
	if err != nil {
		return nil, err
	}
	roles := engine.catalog.BurstSkillRoles[skill.FunctionID]
	if len(roles) == 0 {
		return nil, fmt.Errorf("combat burst skill role %d is unavailable", skill.FunctionID)
	}
	target := submission.Target
	if skill.Target == "HAND_SELECT" || skill.Target == "HAND_ALL" {
		target = 0
	} else if err := engine.validateTarget(skill.Target, memberType, target); err != nil {
		return nil, err
	}
	targetCode, ok := combatSkillTargetCode(skill.Target)
	if !ok {
		return nil, fmt.Errorf("combat burst skill %d has unsupported target %q", skill.ID, skill.Target)
	}

	player.Burst = maxInt(0, player.Burst-buddySkill.GaugeCost)
	results := []BattleResult{
		{Command: resultBurstGaugeState, Args: []int64{int64(memberType), int64(player.Burst)}},
		{Command: resultBurstSkill, Args: []int64{
			int64(memberType), int64(submission.SkillSlot), int64(skill.ID), int64(target), int64(leader.Level),
			int64(targetCode), 0, 0, int64(skill.FunctionID), 0, 0,
		}},
	}
	for _, cardType := range selectedCards {
		results = append(results, BattleResult{Command: resultCardBuff, Args: []int64{int64(memberType), 0, int64(cardType)}})
	}
	registerCardSkill := false
	for _, role := range roles {
		registerCardSkill = registerCardSkill || burstRoleRegistersCard(role.Function)
	}
	if registerCardSkill {
		for _, cardType := range selectedCards {
			if registerErr := engine.registerBurstCardSkill(player, cardType, skill.ID); registerErr != nil {
				return nil, registerErr
			}
		}
	}
	action := battleAction{memberType: memberType, cardLevel: leader.Level, target: target, skill: skill, roles: roles, skillBonus: &battleSkillBonus{}}
	skillRows, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill, roles, func(role CombatSkillRole) ([]BattleResult, error) {
		return engine.executeBurstRole(action, role, selectedCards)
	})
	if err != nil {
		return nil, err
	}
	results = append(results, skillRows...)
	engine.nativeSkillSerial++
	displayResults, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	player.BurstRecharge[submission.SkillSlot-1] = buddySkill.RechargeTurn
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		card := player.Deck[deckSlot-1]
		cardSkill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
		if err != nil {
			return nil, err
		}
		results = append(results, engine.playerCardCostResult(player, card.CardType, cardSkill.Cost))
	}
	for _, deckSlot := range player.Hand {
		if deckSlot != 0 {
			results = append(results, playerCardStateResult(player, player.Deck[deckSlot-1].CardType))
		}
	}
	// 67320 -> 3ef0c includes ordinary and BLESS/CURSE holds after the hand.
	handDisplay, err := engine.selectionCardDisplayResults(memberType)
	if err != nil {
		return nil, err
	}
	results = append(results, handDisplay...)
	results = append(results, BattleResult{Command: resultAttackPartition})
	results = append(results, engine.burstPlayConditionResults()...)
	return results, nil
}

func (engine *BattleEngine) selectBurstSkillVariant(player *battlePlayer, variants []CombatSkillDefinition, submission burstSkillSubmission) (CombatSkillDefinition, []int, error) {
	selected := make([]int, 0, len(submission.CardTypes))
	seen := make(map[int]struct{}, len(submission.CardTypes))
	for _, cardType := range submission.CardTypes {
		if cardType == 0 {
			continue
		}
		if _, duplicate := seen[cardType]; duplicate {
			return CombatSkillDefinition{}, nil, errors.New("combat burst skill card is duplicated")
		}
		seen[cardType] = struct{}{}
		_, found := player.cardInHand(cardType)
		if !found {
			return CombatSkillDefinition{}, nil, fmt.Errorf("combat burst skill card type %d is not in hand", cardType)
		}
		selected = append(selected, cardType)
	}
	for _, skill := range variants {
		if skill.Target == "HAND_SELECT" && len(selected) > skill.HandSelectCount {
			continue
		}
		if skill.Target == "HAND_ALL" {
			selected = nil
			for _, deckSlot := range player.Hand {
				if deckSlot == 0 {
					continue
				}
				card := player.Deck[deckSlot-1]
				matches, err := engine.burstHandCardMatches(player, card, skill)
				if err != nil {
					return CombatSkillDefinition{}, nil, err
				}
				if matches {
					selected = append(selected, card.CardType)
				}
			}
			return skill, selected, nil
		}
		matched := true
		for _, cardType := range selected {
			card, _ := player.cardInHand(cardType)
			matches, err := engine.burstHandCardMatches(player, card, skill)
			if err != nil {
				return CombatSkillDefinition{}, nil, err
			}
			matched = matched && matches
		}
		if matched {
			return skill, selected, nil
		}
	}
	return CombatSkillDefinition{}, nil, errors.New("combat burst skill hand condition is not satisfied")
}

// 79358 is shared by HAND_SELECT validation and HAND_ALL filtering. Both
// include-tag slots are required independently; the third is an exclusion.
func (engine *BattleEngine) burstHandCardMatches(player *battlePlayer, card BattleCard, skill CombatSkillDefinition) (bool, error) {
	cardSkill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
	if err != nil {
		return false, err
	}
	if !combatNullValue(skill.HandAttribute) && !combatAttributeMatches(cardSkill.Attribute, skill.HandAttribute) ||
		!combatNullValue(skill.HandKind) && !strings.EqualFold(cardSkill.Kind, skill.HandKind) ||
		skill.HandMinCost > 0 && cardSkill.Cost < skill.HandMinCost || skill.HandMaxCost > 0 && cardSkill.Cost > skill.HandMaxCost {
		return false, nil
	}
	definition := engine.catalog.Cards[card.CardID]
	for _, tag := range skill.Groups[:2] {
		if tag > 0 && !combatCardHasProfileTag(definition, tag) {
			return false, nil
		}
	}
	return skill.Groups[2] <= 0 || !combatCardHasProfileTag(definition, skill.Groups[2]), nil
}

func (engine *BattleEngine) executeBurstRole(action battleAction, role CombatSkillRole, selectedCards []int) ([]BattleResult, error) {
	// Official active burst skills are NORMAL. BURST_PASSIVE alone uses 6;
	// the activation API does not convert every active effect to list 5.
	return engine.executeBurstRoleWithListType(action, role, selectedCards, 0)
}

func (engine *BattleEngine) executeBurstRoleWithListType(action battleAction, role CombatSkillRole, selectedCards []int, listType int) (results []BattleResult, err error) {
	action.buffListType = listType
	// Keep the skill's list identity on both durable state and projected rows.
	var playerLengths, enemyLengths [4]int
	for i := range engine.players {
		playerLengths[i] = len(engine.players[i].Effects)
		enemyLengths[i] = len(engine.enemies[i].Effects)
	}
	defer func() {
		for i := range engine.players {
			for j := playerLengths[i]; j < len(engine.players[i].Effects); j++ {
				effect := &engine.players[i].Effects[j]
				effect.ListType = listType
				if listType == 6 {
					effect.BurstPassive = true
				}
			}
			for j := enemyLengths[i]; j < len(engine.enemies[i].Effects); j++ {
				engine.enemies[i].Effects[j].ListType = listType
			}
		}
		for i := range results {
			if (results[i].Command == resultBuff || results[i].Command == resultPassiveBuff) && len(results[i].Args) == 11 {
				results[i].Args[2] = int64(listType)
				if listType == 6 {
					results[i].Command = resultPassiveBuff
				}
			}
		}
	}()
	if burstRoleRegistersCard(role.Function) {
		// Each native modifier role emits 324; the whole skill's roles are
		// retained once per activation for subsequent card execution.
		for _, cardType := range selectedCards {
			results = append(results, BattleResult{Command: resultAddCardBuff, Args: []int64{
				int64(action.memberType), int64(cardType), int64(action.skill.ID), int64(engine.nativeSkillSerial),
			}})
		}
	}
	if role.Function == "NEED_COST_DOWN_BURST" {
		amount := maxInt(0, combatParameterInt(role.Parameters[1]))
		for _, cardType := range selectedCards {
			actionPlayer := &engine.players[action.memberType-1]
			actionPlayer.CardCostDown[cardType] += amount
		}
		return results, nil
	}
	if burstRoleRegistersCard(role.Function) {
		return results, nil
	}
	if role.Function == "DISCARD_DRAW" {
		return engine.executeBurstDiscardDraw(action.memberType, selectedCards, maxInt(0, combatParameterInt(role.Parameters[0])))
	}
	return engine.executePlayerRole(action, role, 1)
}

func (engine *BattleEngine) executeBurstDiscardDraw(memberType int, retainedCardTypes []int, handMaximum int) ([]BattleResult, error) {
	player := &engine.players[memberType-1]
	// 7ee5e removes the member's selection before publishing its empty plans.
	delete(engine.selectedPlays, memberType)
	keptActions := engine.turnActions[:0]
	for _, action := range engine.turnActions {
		if action.memberType != memberType {
			keptActions = append(keptActions, action)
		}
	}
	engine.turnActions = keptActions
	retained := make(map[int]struct{}, len(retainedCardTypes))
	for _, cardType := range retainedCardTypes {
		retained[cardType] = struct{}{}
	}
	discarded := make([]int, 0, len(player.Hand))
	discardArgs := []int64{int64(memberType), 0, 0, 0, 0, 0}
	for slotIndex, deckSlot := range player.Hand {
		if deckSlot <= 0 || deckSlot > len(player.Deck) {
			continue
		}
		cardType := player.Deck[deckSlot-1].CardType
		if _, keep := retained[cardType]; !keep {
			discarded = append(discarded, cardType)
			discardArgs[slotIndex+1] = int64(cardType)
		}
	}
	results := make([]BattleResult, 0, 20)
	for range player.Hand {
		results = append(results, BattleResult{Command: resultCardPlayPlan, Args: []int64{int64(memberType), 0, 0, 0, 0, 0, 0}})
	}
	results = append(results,
		BattleResult{Command: resultSpherePlayPlan, Args: []int64{int64(memberType), 0, 0}},
		BattleResult{Command: resultBurstDiscard, Args: discardArgs},
		BattleResult{Command: resultCardPass, Args: []int64{int64(memberType)}},
	)
	for _, cardType := range discarded {
		// FUN_0007ee5e removes both good- and bad-list entries owned by each
		// discarded card before it removes that card from the hand. Every
		// non-empty removal list goes through FUN_00072a18 independently, so a
		// later draw of the same card type cannot inherit an obsolete trap or
		// seal. Each entry gets 71/6; 72 waits until its status kind is gone.
		buffs, debuffs := removePlayerCardBoundEffects(player, cardType)
		for _, removed := range [][]battleEffect{buffs, debuffs} {
			if len(removed) == 0 {
				continue
			}
			ensurePlayerBaseParameters(player)
			refreshPlayerBattleParameters(player)
			refreshPlayerAttribute(player)
			results = appendNaturalExpiryRows(results, memberType, removed, player.Effects, player.Attribute)
			results = append(results, BattleResult{Command: resultBattleParam, Args: battleParameterArgs(
				player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
				player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
			)})
		}
		engine.removeCardFromHand(memberType, cardType)
	}
	handMaximum = minInt(len(player.Hand), maxInt(0, handMaximum))
	for slotIndex := range player.Hand {
		if player.Hand[slotIndex] != 0 || playerHandCount(player) >= handMaximum {
			continue
		}
		deckSlot := engine.drawCard(player)
		if deckSlot == 0 {
			break
		}
		player.Hand[slotIndex] = deckSlot
		card := player.Deck[deckSlot-1]
		skill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
		if err != nil {
			return nil, err
		}
		results = append(results,
			BattleResult{Command: resultCardDeal, Args: []int64{int64(memberType), int64(card.CardType), int64(card.Level)}},
			engine.playerCardCostResult(player, card.CardType, skill.Cost),
			playerCardStateResult(player, card.CardType),
		)
	}
	// 7ee5e refreshes every member's cards through 3d680 before cost/deck rows.
	display, err := engine.selectionCardDisplayResults(0)
	if err != nil {
		return nil, err
	}
	results = append(results, display...)
	results = append(results,
		BattleResult{Command: resultCost, Args: []int64{int64(memberType), int64(player.Cost + engine.playerEffectValue(player, "COST_BLOCK"))}},
		BattleResult{Command: resultCardDeck, Args: []int64{int64(memberType), int64(player.remainingDeckCount()), 0}},
	)
	return results, nil
}

func playerHandCount(player *battlePlayer) int {
	count := 0
	for _, deckSlot := range player.Hand {
		if deckSlot != 0 {
			count++
		}
	}
	return count
}
