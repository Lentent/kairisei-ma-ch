package multiplayer

import (
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
)

func (engine *BattleEngine) Start() ([]BattleResult, error) {
	if engine.phase != battlePhaseCreated {
		return nil, errors.New("combat start phase is unavailable")
	}
	results := make([]BattleResult, 0, 70)
	results = append(results, engine.waveRecovery...)
	// Start's 62af0 -> a50c4 also repairs a non-retired zero-HP initial user.
	// Recovery rows precede CARD identities in the transport packet.
	for index := range engine.players {
		player := &engine.players[index]
		if player.MemberType != 0 && !player.GameOver && player.HP <= 0 {
			player.HP = 1
			results = append(results, BattleResult{Command: resultHP,
				Args: []int64{int64(player.MemberType), int64(player.MaxHP), 1, 0}})
		}
	}
	results = append(results, engine.setupIdentityResults()...)
	for index := range engine.players {
		player := &engine.players[index]
		hp := player.HP
		refreshPlayerBattleParameters(player)
		// Start/getters expose raw current HP even if the supplied tuple
		// exceeds the projected maximum; only HP-mutating consumers clamp it.
		player.HP = hp
		results = append(results,
			playerBaseParameterResult(player),
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
			BattleResult{Command: resultBurstGaugeState, Args: []int64{int64(player.MemberType), int64(player.Burst)}},
		)
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		results = append(results,
			BattleResult{Command: resultBaseParam, Args: baseParameterArgs(enemy.MemberType, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense)},
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)},
		)
	}
	// Two internal gimmick members are present in the official solo oracle.
	for memberType := 10; memberType <= 11; memberType++ {
		results = append(results,
			BattleResult{Command: resultBaseParam, Args: baseParameterArgs(memberType, 1, 0, 0, 0, 0, 0)},
			BattleResult{Command: resultBattleParam, Args: battleParameterArgs(memberType, 0, 1, 0, 0, 0, 0, 0, 99999, 99999, 99999)},
		)
	}
	spherePassiveResults, err := engine.executePlayerSupportPassives()
	if err != nil {
		return nil, err
	}
	results = append(results, spherePassiveResults...)
	for index := 0; index < engine.enemyCount; index++ {
		passiveResults, err := engine.executeEnemyPassiveSkill(&engine.enemies[index])
		if err != nil {
			return nil, err
		}
		results = append(results, passiveResults...)
		if engine.enemies[index].Level.PassiveSkillID != 0 {
			displayResults, err := engine.refreshPassiveDisplayPowers()
			if err != nil {
				return nil, err
			}
			results = append(results, displayResults...)
		}
	}
	// Start calls 663a4 (all member passives) before 62d8a -> 5db7a
	// shuffles opening decks. Their shared RNG must be consumed in that order.
	if engine.openingDraw != nil {
		for index := range engine.players {
			engine.shuffleOpeningDeck(&engine.players[index], engine.openingDraw[index])
		}
		engine.openingDraw = nil
	}
	engine.phase = battlePhaseStarted
	engine.waveRecovery = nil
	return results, nil
}

// setupIdentityResults is 1b3c0's common Start/Resume identity tail. CARD
// identities are supplied by the start transport or the resume snapshot.
func (engine *BattleEngine) setupIdentityResults() []BattleResult {
	results := engine.sphereItemResults()
	for index := range engine.players {
		results = append(results, engine.buddyResults(&engine.players[index])...)
	}
	for index := range engine.players {
		results = append(results, sphereHandResult(engine.players[index].MemberType))
	}
	for index := range engine.players {
		if !engine.players[index].GameOver {
			results = append(results, BattleResult{Command: resultHoldMax, Args: []int64{int64(engine.players[index].MemberType), int64(engine.holdMax)}})
		}
	}
	for index := range engine.players {
		results = append(results, engine.buddySlotResult(&engine.players[index]))
	}
	return results
}

func (engine *BattleEngine) TurnPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseStarted && engine.phase != battlePhaseChaliceEnemy {
		return nil, errors.New("combat turn phase is unavailable")
	}
	engine.turn++
	engine.resumeSide = 0
	copy(engine.damageHistory[1:], engine.damageHistory[:28])
	engine.damageHistory[0] = engine.turnStats.DamageEvents
	engine.turnStats = battleTurnStats{}
	for index := range engine.players {
		engine.players[index].TurnDamage = 0
		engine.players[index].ExecutedBuffKinds = [69]uint32{}
		copy(engine.players[index].HateHistory[1:], engine.players[index].HateHistory[:10])
		engine.players[index].HateHistory[0] = 0
	}
	for index := range engine.enemies {
		engine.enemies[index].TurnDamage = 0
		engine.enemies[index].TurnPhysical = 0
		engine.enemies[index].TurnMagic = 0
		engine.enemies[index].AITurn = battleEnemyAITurnStats{}
		engine.enemies[index].ExecutedBuffKinds = [69]uint32{}
		engine.enemies[index].ActionConsumed = 0
		engine.enemies[index].ChargedActions = [5]enemyActionCandidate{}
		engine.enemies[index].ChargedActionCount = 0
	}
	results := make([]BattleResult, 0, 25)
	burstResults, err := engine.transitionBurstStates()
	if err != nil {
		return nil, err
	}
	results = append(results, burstResults...)
	// 1d8f0 calls a86c0 before the first-attack dispatch at 65680.
	results = append(results, engine.burstPlayConditionResults()...)
	// Original TurnPhase calls 5dd72 before 65680. First-attack skills can
	// inspect hands or KO a member; drawing afterwards would lose that hand.
	engine.prepareTurnDraw()
	if engine.turn == 1 {
		firstAttack, err := engine.executeEnemyFirstAttack()
		if err != nil {
			return nil, err
		}
		results = append(results, firstAttack...)
	}
	// 1d8f0 has no unconditional 3fa8a/41a42 sweep. Actual Burst passive
	// executions already refresh after each 7adb0-equivalent action.
	results = engine.settlePlayerDeaths(results)
	if engine.continuePending {
		if engine.allPlayersDead() {
			engine.phase = battlePhaseTurn
			return results, nil
		}
		results = results[:len(results)-1] // 65010 follows the ordinary turn setup.
	}
	if engine.endType != 0 {
		engine.phase = battlePhaseEnded
		results = append(results, battleEndResult(engine.endType))
		results = engine.appendTerminalBuffCleanup(results)
		return results, nil
	}
	for index := range engine.players {
		player := &engine.players[index]
		if player.GameOver {
			continue
		}
		results = append(results, BattleResult{Command: resultHoldStore, Args: []int64{int64(player.MemberType)}})
		for _, hold := range player.CardHolds {
			results = append(results, BattleResult{Command: resultHoldRemainder, Args: []int64{
				int64(player.MemberType), int64(hold.Action.cardType), int64(hold.Action.sphereSlot), int64(hold.Remaining), 0,
			}})
		}
		// a72e2 projects BLESS before CURSE, preserving order within each kind.
		for _, cardType := range [...]int{22, 21} {
			for _, hold := range player.BlessHolds {
				if hold.CardType == cardType {
					results = append(results, BattleResult{Command: resultHoldRemainder, Args: []int64{
						int64(player.MemberType), int64(hold.CardType), 0, int64(hold.Remaining), int64(hold.AppendIndex),
					}})
				}
			}
		}
	}
	results = append(results, BattleResult{Command: resultTurn, Args: []int64{int64(engine.turn), 0}})
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP <= 0 {
			continue
		}
		for _, candidate := range engine.buildEnemyChargeStartPlan(enemy) {
			skill, _, matched := engine.selectEnemySkillBranch(enemy, candidate.action.SkillID, 0)
			if !matched {
				continue
			}
			results = append(results, BattleResult{Command: resultChargeStart, Args: []int64{
				int64(enemy.MemberType), int64(candidate.action.SkillID), int64(skill.FunctionID),
			}})
		}
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP <= 0 {
			continue
		}
		sign := engine.enemyAttackSign(enemy)
		results = append(results, BattleResult{Command: resultEnemyAttackSign, Args: []int64{int64(enemy.MemberType), int64(sign)}})
	}
	for index := range engine.players {
		player := &engine.players[index]
		if player.GameOver {
			continue
		}
		blocked := engine.playerEffectValue(player, "COST_BLOCK")
		player.Cost = maxInt(0, engine.turnCost()-blocked)
		results = append(results,
			BattleResult{Command: resultCost, Args: []int64{int64(player.MemberType), int64(engine.turnCost())}},
			BattleResult{Command: resultCostBlock, Args: []int64{int64(player.MemberType), int64(blocked)}},
		)
	}
	sphereResults, err := engine.sphereConditionResults()
	if err != nil {
		return nil, err
	}
	results = append(results, sphereResults...)
	for index := range engine.players {
		if !engine.players[index].GameOver {
			results = append(results, BattleResult{Command: resultPlayLogState, Args: []int64{int64(engine.players[index].MemberType), int64(engine.elapsedWaveTurns + engine.turn), 0}})
		}
	}
	if engine.continuePending {
		results = append(results, BattleResult{Command: resultContinueWait})
	}
	engine.phase = battlePhaseTurn
	return results, nil
}

// Native FUN_0006edef caps the base at 10; FUN_0006ee0d subtracts
// COST_BLOCK afterwards. FUN_000a6078 sends the base, not the remainder:
// managed HandsData.COST.getEnableCost subtracts block_cost itself.
func (engine *BattleEngine) turnCost() int {
	return maxInt(0, minInt(10, engine.costInitial+engine.costTurnOffset+maxInt(0, engine.turn-1)))
}

func (engine *BattleEngine) UserPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseTurn {
		return nil, errors.New("combat user phase is unavailable")
	}
	results := make([]BattleResult, 0, 180)
	for index := range engine.players {
		engine.players[index].CardCostDown = [11]int{}
		engine.players[index].CardBurstSkills = [11][]int{}
	}
	type handCard struct {
		member  int
		slot    int
		card    BattleCard
		skill   CombatSkillDefinition
		display battleDisplayPower
	}
	handCards := make([]handCard, 0, maxRoomMembers*5)
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		if player.HP <= 0 {
			continue
		}
		player.CardDisplay = [11]battleDisplayPower{}
		for slotIndex, deckSlot := range player.Hand {
			if deckSlot == 0 {
				continue
			}
			card := player.Deck[deckSlot-1]
			skill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
			if err != nil {
				return nil, err
			}
			display, err := engine.cardDisplayStateMode(player, card, false, true)
			if err != nil {
				return nil, err
			}
			handCards = append(handCards, handCard{member: player.MemberType, slot: slotIndex + 1, card: card, skill: skill, display: display})
		}
	}
	for _, item := range handCards {
		results = append(results, fullCardUpdateResult(item.member, item.card.CardType,
			engine.catalog.cardUsesArthurSkill(item.card.CardID, engine.players[item.member-1].ArthurType), [4]bool{}, item.display, 0))
	}
	// 63cc0 runs 3c596's base display before the two 5fe48 regeneration
	// sweeps, then publishes darkness/draw/selection state.
	results = append(results, engine.regenerateMembers()...)
	for index := range engine.players {
		player := &engine.players[index]
		if player.HP <= 0 {
			continue
		}
		mask := playerDarknessMask(player)
		// a6e6c publishes all five hand slots, including empty ones. This
		// prevents a retained slot icon from surviving a multi-card play.
		for slot := 0; slot < len(player.Hand); slot++ {
			results = append(results, BattleResult{Command: 306, Args: []int64{
				int64(player.MemberType), int64(slot + 1), int64(mask >> (4 - slot) & 1)}})
		}
	}
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		for _, item := range handCards {
			if item.member == player.MemberType && engine.turnDrawn[playerIndex][item.slot-1] {
				results = append(results, BattleResult{Command: resultCardDeal, Args: []int64{int64(item.member), int64(item.card.CardType), int64(item.card.Level)}})
			}
		}
		// 5de62 always publishes the alive member's deck remainder, even
		// when a retained full hand needed no new card after Start.
		if player.HP > 0 {
			results = append(results, BattleResult{Command: resultCardDeck, Args: []int64{int64(player.MemberType), int64(player.remainingDeckCount()), 0}})
		}
		for _, item := range handCards {
			// 5de62 checks SkillData.kind (+8), independently of its roles.
			if item.member != player.MemberType || (item.skill.Kind != "ATTACK" && item.skill.Kind != "SORCERY") {
				continue
			}
			weakness := []int64{int64(item.member), int64(item.card.CardType)}
			// FUN_0005de62 reads SkillData.attribute (+0xc) for the hand
			// weakness preview. The ATTACK_AA role attribute controls damage
			// resolution, but can legitimately differ from this UI identity.
			attribute := item.skill.Attribute
			for enemyIndex := 0; enemyIndex < maxRoomMembers; enemyIndex++ {
				rate := 0
				if enemyIndex < engine.enemyCount {
					rate = enemyAttributeRate(&engine.enemies[enemyIndex], attribute)
				}
				weakness = append(weakness, int64(rate))
			}
			results = append(results, BattleResult{Command: resultCardWeak, Args: weakness})
		}
	}
	for _, item := range handCards {
		results = append(results, engine.playerCardCostResult(&engine.players[item.member-1], item.card.CardType, item.skill.Cost))
	}
	for _, item := range handCards {
		results = append(results, playerCardStateResult(&engine.players[item.member-1], item.card.CardType))
	}
	sphereResults, err := engine.sphereCountCostResults()
	if err != nil {
		return nil, err
	}
	results = append(results, sphereResults...)
	engine.selectedPlays = make(map[int]cardPlaySubmission, maxRoomMembers)
	// 63cc0 clears the selection table at UserPhase, not TurnPhase. Charge
	// conditions evaluated before that point can still read last turn's cards.
	engine.turnActions = nil
	displayResults, err := engine.selectionDisplayResults()
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		enabled := int64(0)
		if enemy.HP > 0 {
			enabled = 1
		}
		results = append(results, BattleResult{Command: resultEnableTarget, Args: []int64{int64(enemy.MemberType), enabled}})
	}
	engine.phase = battlePhaseUser
	return results, nil
}

func (engine *BattleEngine) Submit(memberType int, submission cardPlaySubmission) ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseUser {
		return nil, errors.New("combat card submission phase is unavailable")
	}
	if memberType < 1 || memberType > maxRoomMembers {
		return nil, errors.New("combat card submission member is invalid")
	}
	if _, duplicate := engine.selectedPlays[memberType]; duplicate {
		return nil, errors.New("combat card submission is duplicated")
	}
	player := &engine.players[memberType-1]
	if player.HP <= 0 {
		if selectedActionCount(submission) != 0 {
			return nil, errors.New("combat KO member cannot select cards")
		}
		return nil, nil
	}
	remainingCost := player.Cost
	results := make([]BattleResult, 0, 6)
	selectedCards := make(map[int]struct{}, len(submission.CardTypes))
	sphereAction, err := engine.validateSphereSubmission(memberType, submission)
	if err != nil {
		return nil, err
	}
	for index, cardType := range submission.CardTypes {
		if cardType == 0 {
			continue
		}
		if _, duplicate := selectedCards[cardType]; duplicate {
			return nil, errors.New("combat card submission repeats a hand card")
		}
		selectedCards[cardType] = struct{}{}
		card, found := player.cardInHand(cardType)
		if !found {
			return nil, fmt.Errorf("member %d card type %d is not in hand", memberType, cardType)
		}
		if _, sealed := playerCardSealEffect(player, cardType); sealed || engine.playerHasEffect(player, "STAN") {
			return nil, fmt.Errorf("member %d cannot play cards while sealed or stunned", memberType)
		}
		skill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
		if err != nil {
			return nil, err
		}
		cost := engine.effectiveCardCost(player, cardType, skill.Cost)
		if cost > remainingCost {
			return nil, fmt.Errorf("member %d card cost exceeds remaining cost", memberType)
		}
		if skill.Target == "SELF" && submission.Targets[index] == 0 {
			// Original UserCardPlay canonicalizes SELF's zero alias before
			// publishing 22 and retaining the selected action (D-332 receipt).
			submission.Targets[index] = memberType
		}
		if err := engine.validateTarget(skill.Target, memberType, submission.Targets[index]); err != nil {
			return nil, err
		}
		remainingCost -= cost
		state := playerCardStateResult(player, cardType).Args[2]
		results = append(results, BattleResult{Command: resultCardPlay, Args: []int64{int64(memberType), int64(cardType), int64(submission.Targets[index]), int64(engine.playerCardDarkness(memberType, cardType)), state}})
	}
	if sphereAction.sphereSlot != 0 {
		results = append(results, BattleResult{Command: resultSpherePlay, Args: []int64{
			int64(memberType), int64(sphereAction.sphereSlot), int64(sphereAction.target),
		}})
	}
	if len(results) == 0 {
		// Native FUN_00040908 emits CARD_PASS when one member commits no card
		// and no sphere. The result belongs to ApiCardPlayR, not ApiUserAttack.
		results = append(results, BattleResult{Command: resultCardPass, Args: []int64{int64(memberType)}})
	}
	// Full preview can fail on an unavailable referenced skill. Keep the
	// accepted selection, cost and display cache atomic in that case too.
	preview := *engine
	preview.selectedPlays = maps.Clone(engine.selectedPlays)
	if preview.selectedPlays == nil {
		preview.selectedPlays = make(map[int]cardPlaySubmission, maxRoomMembers)
	}
	preview.players[memberType-1].Cost = remainingCost
	preview.selectedPlays[memberType] = submission
	preview.turnActions, err = preview.selectedBattleActions()
	if err != nil {
		return nil, err
	}
	displayResults, err := preview.selectionDisplayResults()
	if err != nil {
		return nil, err
	}
	engine.players, engine.selectedPlays, engine.turnActions = preview.players, preview.selectedPlays, preview.turnActions
	results = append(results, displayResults...)
	return results, nil
}

func (engine *BattleEngine) AutoSubmission(memberType int) (cardPlaySubmission, error) {
	if engine.phase != battlePhaseUser || memberType < 1 || memberType > maxRoomMembers {
		return cardPlaySubmission{}, errors.New("combat automatic card phase is unavailable")
	}
	player := &engine.players[memberType-1]
	if player.HP <= 0 || engine.playerHasEffect(player, "STAN") {
		return cardPlaySubmission{TimedOut: true, Automatic: true}, nil
	}
	remainingCost := player.Cost
	type candidate struct {
		card   BattleCard
		skill  CombatSkillDefinition
		target int
	}
	candidates := make([]candidate, 0, len(player.Hand))
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		card := player.Deck[deckSlot-1]
		if _, sealed := playerCardSealEffect(player, card.CardType); sealed {
			continue
		}
		skill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
		if err != nil || engine.effectiveCardCost(player, card.CardType, skill.Cost) > remainingCost {
			continue
		}
		target, err := engine.defaultTarget(skill.Target, memberType)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{card: card, skill: skill, target: target})
	}
	if len(candidates) == 0 {
		return cardPlaySubmission{TimedOut: true, Automatic: true}, nil
	}
	sort.SliceStable(candidates, func(left int, right int) bool {
		if candidates[left].skill.PriorityPVE == candidates[right].skill.PriorityPVE {
			return candidates[left].card.CardType < candidates[right].card.CardType
		}
		return candidates[left].skill.PriorityPVE < candidates[right].skill.PriorityPVE
	})
	selected := cardPlaySubmission{TimedOut: true, Automatic: true}
	selectedCount := 0
	for _, candidate := range candidates {
		cost := engine.effectiveCardCost(player, candidate.card.CardType, candidate.skill.Cost)
		if selectedCount >= len(selected.CardTypes) || cost > remainingCost {
			continue
		}
		selected.CardTypes[selectedCount] = candidate.card.CardType
		selected.Targets[selectedCount] = candidate.target
		remainingCost -= cost
		selectedCount++
	}
	return selected, nil
}

func (engine *BattleEngine) defaultTarget(targetKind string, memberType int) (int, error) {
	switch targetKind {
	case "SELF":
		return memberType, nil
	case "USER_ONE", "SELECT", "FRIEND_ONE":
		living := make([]int, 0, maxRoomMembers)
		for index := range engine.players {
			if engine.players[index].HP > 0 {
				living = append(living, engine.players[index].MemberType)
			}
		}
		if len(living) == 0 {
			return 0, errors.New("no living ally target")
		}
		return living[int(engine.rng.next()%uint32(len(living)))], nil
	case "USER_ALL", "FRIEND_ALL", "ENEMY_ALL", "ALL":
		return 0, nil
	case "ENEMY_ONE", "PARENT", "HIGH_HP_ENEMY", "HIGH_DEF_ENEMY", "HIGH_MDEF_ENEMY", "LOW_HP_ENEMY", "LOW_DEF_ENEMY", "LOW_MDEF_ENEMY":
		living := make([]int, 0, engine.enemyCount)
		for index := 0; index < engine.enemyCount; index++ {
			if engine.enemies[index].HP > 0 {
				living = append(living, engine.enemies[index].MemberType)
			}
		}
		if len(living) == 0 {
			return 0, errors.New("no living enemy target")
		}
		return living[int(engine.rng.next()%uint32(len(living)))], nil
	case "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER":
		target := engine.memberForJob(targetKind)
		if target == 0 || engine.players[target-1].HP <= 0 {
			return 0, errors.New("no living ally of the target profession")
		}
		return target, nil
	default:
		return 0, fmt.Errorf("unsupported combat target kind %q", targetKind)
	}
}

func (engine *BattleEngine) battleMemberHP(memberType int) (int, int, bool) {
	if memberType >= 1 && memberType <= maxRoomMembers {
		member := &engine.players[memberType-1]
		return member.HP, member.MaxHP, true
	}
	if memberType >= 5 && memberType < 5+engine.enemyCount {
		member := &engine.enemies[memberType-5]
		return member.HP, member.MaxHP, true
	}
	return 0, 0, false
}

func (engine *BattleEngine) battleMemberEffects(memberType int) []battleEffect {
	if memberType >= 1 && memberType <= maxRoomMembers {
		return engine.players[memberType-1].Effects
	}
	if memberType >= 5 && memberType < 5+engine.enemyCount {
		return engine.enemies[memberType-5].Effects
	}
	return nil
}

func combatEnemyMemberType(target string) int {
	target = strings.ToUpper(strings.TrimSpace(target))
	if !strings.HasPrefix(target, "ENEMY") {
		return 0
	}
	index := combatParameterInt(strings.TrimPrefix(target, "ENEMY"))
	if index < 1 || index > 4 {
		return 0
	}
	return index + 4
}

// combatRolesDefined distinguishes a valid no-effect role from an incomplete
// skill. CN native roles 45 (NONE) and 51 (OUTPUT_TEXT) return success with
// zero effect output; the outer enemy skill still consumes its action and
// projects ResultCmd50. An empty function, in contrast, is not a usable role
// definition.
func combatRolesDefined(roles []CombatSkillRole) bool {
	for _, role := range roles {
		if strings.TrimSpace(role.Function) != "" {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) EndType() int { return engine.endType }

func (engine *BattleEngine) appendNewPlayerGameOver(results []BattleResult) []BattleResult {
	// 65010/61d60 finish a winning/awake wave before any continue/gameover
	// exchange. A KO in that same action remains eligible for a50c4's 1 HP
	// recovery. The local no-continue policy must not retire it prematurely.
	if engine.endType == 1 || engine.endType == 4 {
		return results
	}
	for index := range engine.players {
		player := &engine.players[index]
		if player.MemberType == 0 || player.HP > 0 || player.GameOver {
			continue
		}
		player.GameOver = true
		// 228c0 -> 425cc clears the pending holds/selection. It does not
		// restore HP or clear the retirement flag at the next wave.
		player.BlessHolds, player.ReservedChalice = nil, 0
		player.CardHolds = nil
		delete(engine.selectedPlays, player.MemberType)
		results = append(results, battleGameOverResult(player.MemberType))
	}
	return results
}

func (engine *BattleEngine) allPlayersDead() bool {
	for index := range engine.players {
		if engine.players[index].HP > 0 {
			return false
		}
	}
	return true
}

func (engine *BattleEngine) removeCardFromHand(memberType int, cardType int) {
	player := &engine.players[memberType-1]
	for index, deckSlot := range player.Hand {
		if deckSlot != 0 && player.Deck[deckSlot-1].CardType == cardType {
			player.Hand[index] = 0
			for _, hold := range player.CardHolds {
				if hold.Action.cardType == cardType {
					return
				}
			}
			if cardType > 0 && cardType < len(player.CardDisplay) {
				player.CardDisplay[cardType] = battleDisplayPower{}
			}
			player.clearCardModifiers(cardType)
			player.Discard = append(player.Discard, deckSlot-1)
			return
		}
	}
}

// 45726 removes CARD-list modifiers when a card leaves the hand or hold.
// A later draw of the same physical card must not inherit them.
func (player *battlePlayer) clearCardModifiers(cardType int) {
	if cardType > 0 && cardType < len(player.CardCostDown) {
		player.CardCostDown[cardType] = 0
		player.CardBurstSkills[cardType] = nil
	}
}

func playerCardTrapEffect(player *battlePlayer, cardType int) (battleEffect, bool) {
	for _, effect := range player.Effects {
		if effect.Function == "CARD_TRAP_DAMAGE" && effect.CardType == cardType && effect.Remaining > 0 {
			return effect, true
		}
	}
	return battleEffect{}, false
}

func playerCardSealEffect(player *battlePlayer, cardType int) (battleEffect, bool) {
	for _, effect := range player.Effects {
		if effect.Function == "CARD_SEAL" && effect.CardType == cardType && effect.Remaining > 0 {
			return effect, true
		}
	}
	return battleEffect{}, false
}

func removePlayerCardEffect(player *battlePlayer, function string, cardType int) {
	kept := player.Effects[:0]
	for _, effect := range player.Effects {
		if effect.Function == function && effect.CardType == cardType && effect.Remaining > 0 {
			continue
		}
		kept = append(kept, effect)
	}
	player.Effects = kept
}

func removePlayerCardBoundEffects(player *battlePlayer, cardType int) (buffs []battleEffect, debuffs []battleEffect) {
	kept := player.Effects[:0]
	for _, effect := range player.Effects {
		if effect.CardType == cardType && effect.Remaining > 0 {
			switch effect.Kind {
			case 1:
				buffs = append(buffs, effect)
				continue
			case 2:
				debuffs = append(debuffs, effect)
				continue
			}
		}
		kept = append(kept, effect)
	}
	player.Effects = kept
	return buffs, debuffs
}

func (engine *BattleEngine) triggerCardTrap(memberType int, cardType int) []BattleResult {
	player := &engine.players[memberType-1]
	// FUN_0005f044 filters dead members before it looks up/removes the selected
	// card's trap. A dead holder therefore retains an unconsumed trap entry.
	if player.HP <= 0 {
		return nil
	}
	kept := player.Effects[:0]
	var trap *battleEffect
	for index := range player.Effects {
		effect := player.Effects[index]
		if trap == nil && effect.Function == "CARD_TRAP_DAMAGE" && effect.CardType == cardType && effect.Remaining > 0 {
			copy := effect
			trap = &copy
			continue
		}
		kept = append(kept, effect)
	}
	player.Effects = kept
	if trap == nil {
		return nil
	}
	// 5f044 caps the request at current HP minus one before 73e9f. A
	// card trap is nonlethal, independently of ENDURE or GUTS effects.
	damage := maxInt(0, minInt(trap.Value, player.HP-1))
	player.HP = engine.playerHPAfterDamage(player, damage)
	recordPlayerDamage(player, damage)
	results := []BattleResult{
		buffStatusEffectResult(memberType, battleBuffCodes["CARD_TRAP_DAMAGE"]),
		// 5f044 keeps the capped request even when ENDURE raises committed HP.
		{Command: 60, Args: []int64{int64(memberType), 0, -int64(damage), int64(player.HP), 0, 0, 100, 0, 0, 0}},
	}
	if trap.Source >= 5 && trap.Source < 5+engine.enemyCount {
		results = append(engine.burstDamageGaugeResults(player, damage), results...)
	}
	// 5f044 -> 449a0 removes this card's trap. The shared 72a18 -> 47190
	// release path keeps the grouped status while other traps remain, but
	// still emits this entry's 71 before the parameter snapshot.
	ensurePlayerBaseParameters(player)
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	results = appendNaturalExpiryRows(results, memberType, []battleEffect{*trap}, player.Effects, player.Attribute)
	results = append(results, BattleResult{Command: resultBattleParam, Args: battleParameterArgs(
		player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
		player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
	)})
	var guts []BattleResult
	guts = resolvePlayerGuts(player)
	return append(results, guts...)
}

// triggerPlayedCardTraps mirrors FUN_0005f044 after the complete ordered card
// action list. Native walks members and their selected hand slots, so use the
// unsorted turnActions snapshot rather than skill-priority execution order.
func (engine *BattleEngine) triggerPlayedCardTraps() []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers*5)
	for _, action := range engine.turnActions {
		if action.cardType == 0 {
			continue
		}
		results = append(results, engine.triggerCardTrap(action.memberType, action.cardType)...)
	}
	return results
}

func (engine *BattleEngine) playerTargets(kind string, actor int, selected int) []int {
	switch kind {
	case "SELF":
		return []int{actor}
	case "USER_ONE", "SELECT":
		if selected >= 1 && selected <= 4 {
			return []int{selected}
		}
	case "USER_ALL", "FRIEND_ALL":
		result := make([]int, 0, maxRoomMembers)
		for index := range engine.players {
			if engine.players[index].HP > 0 {
				result = append(result, engine.players[index].MemberType)
			}
		}
		return result
	case "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER":
		if memberType := engine.memberForJob(kind); memberType != 0 && engine.players[memberType-1].HP > 0 {
			return []int{memberType}
		}
	}
	return nil
}

func (engine *BattleEngine) enemyTargets(kind string, selected int) []int {
	if kind == "ENEMY_ALL" {
		result := make([]int, 0, engine.enemyCount)
		for index := 0; index < engine.enemyCount; index++ {
			if engine.enemies[index].HP > 0 {
				result = append(result, index)
			}
		}
		return result
	}
	if selected == 0 {
		selected = 5
	}
	if selected >= 5 && selected < 5+engine.enemyCount && engine.enemies[selected-5].HP > 0 {
		return []int{selected - 5}
	}
	return nil
}
