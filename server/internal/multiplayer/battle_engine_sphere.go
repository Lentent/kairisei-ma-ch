package multiplayer

import (
	"errors"
	"fmt"
	"strings"
)

const (
	sphereTypeNormal  = "NORMAL"
	sphereTypeChalice = "CHALICE"
)

// sphereIdentityResults mirrors FUN_000a553e followed by FUN_000a5776. The
// native producer registers every concrete sphere before it publishes the
// fixed three-slot hands; grouping by player would change the managed setup
// order on a reconnect.
func (engine *BattleEngine) sphereIdentityResults() []BattleResult {
	results := engine.sphereItemResults()
	for playerIndex := range engine.players {
		results = append(results, sphereHandResult(engine.players[playerIndex].MemberType))
	}
	return results
}

func (engine *BattleEngine) sphereItemResults() []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers*(deckSphereSlots+1))
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			results = append(results, BattleResult{Command: resultSphere, Args: []int64{
				int64(player.MemberType), int64(sphere.Slot), int64(sphere.SphereID),
			}})
		}
	}
	return results
}

// sphereStateResults projects the count, cost and display-power families that
// native refreshes after the identities have been installed. Play conditions
// are intentionally excluded: FUN_000cef63 publishes ResultCmd309 at the turn
// input boundary, which is sphereConditionResults below.
func (engine *BattleEngine) sphereStateResults() ([]BattleResult, error) {
	results, err := engine.sphereCountCostResults()
	if err != nil {
		return nil, err
	}
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			display, err := engine.sphereDisplayState(player, sphere)
			if err != nil {
				return nil, err
			}
			sphere.Display = display
			results = append(results, sphereUpdateResult(player.MemberType, sphere.Slot, display.Power, display.Marker))
		}
	}
	return results, nil
}

// 63cc0 calls a59f6 then a5c9e: all sphere counts, then all costs. Full
// selection power (307) is emitted later by the same projection as hand cards.
func (engine *BattleEngine) sphereCountCostResults() ([]BattleResult, error) {
	var counts, costs []BattleResult
	for i := range engine.players {
		player := &engine.players[i]
		for j := range player.Spheres {
			sphere := &player.Spheres[j]
			if sphere.SphereID == 0 {
				continue
			}
			skill, _, err := engine.catalog.SphereSkill(sphere.SphereID)
			if err != nil {
				return nil, err
			}
			counts = append(counts, BattleResult{Command: resultSphereCount, Args: []int64{int64(player.MemberType), int64(sphere.Slot), int64(sphere.Count), int64(sphere.Maximum)}})
			costs = append(costs, BattleResult{Command: resultSphereCost, Args: []int64{int64(player.MemberType), int64(sphere.Slot), int64(skill.Cost)}})
		}
	}
	return append(counts, costs...), nil
}

func (engine *BattleEngine) sphereConditionResults() ([]BattleResult, error) {
	results := make([]BattleResult, 0, maxRoomMembers*deckSphereSlots)
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			definition := engine.catalog.Spheres[sphere.SphereID]
			engine.refreshSphereCondition(sphere, definition)
			condition, err := spherePlayConditionCode(definition.PlayCondition)
			if err != nil {
				return nil, err
			}
			results = append(results, BattleResult{Command: resultSpherePlayCondition, Args: []int64{
				int64(player.MemberType), int64(sphere.Slot), int64(condition), boolInt64(sphere.Playable), int64(sphere.Remaining),
			}})
		}
	}
	return results, nil
}

func (engine *BattleEngine) refreshSphereCondition(sphere *battleSphere, definition CombatSphereDefinition) {
	sphere.Remaining = 0
	switch definition.PlayCondition {
	case "", "NONE":
		sphere.Playable = true
	case "TURN_AFTER":
		sphere.Remaining = maxInt(0, definition.PlayParam+1-engine.turn)
		sphere.Playable = sphere.Remaining == 0
	default:
		sphere.Playable = false
	}
}

func spherePlayConditionCode(condition string) (int, error) {
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "", "NONE":
		return 0, nil
	case "TURN_AFTER":
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported sphere play condition %q", condition)
	}
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func (engine *BattleEngine) validateSphereSubmission(memberType int, submission cardPlaySubmission) (battleAction, error) {
	if submission.SphereSlot == 0 {
		return battleAction{}, nil
	}
	if submission.SphereSlot < 1 || submission.SphereSlot > deckSphereSlots {
		return battleAction{}, errors.New("combat sphere selection slot is invalid")
	}
	player := &engine.players[memberType-1]
	sphere := &player.Spheres[submission.SphereSlot-1]
	if sphere.SphereID == 0 || sphere.Type != sphereTypeNormal || sphere.Count <= 0 || !sphere.Playable {
		return battleAction{}, errors.New("combat normal sphere selection is unavailable")
	}
	if engine.playerHasEffect(player, "STAN") {
		return battleAction{}, errors.New("combat sphere cannot be played while stunned")
	}
	skill, roles, err := engine.catalog.SphereSkill(sphere.SphereID)
	if err != nil {
		return battleAction{}, err
	}
	if err := engine.validateTarget(skill.Target, memberType, submission.SphereTarget); err != nil {
		return battleAction{}, err
	}
	action := battleAction{
		memberType: memberType, sphereSlot: sphere.Slot, cardLevel: sphere.Level,
		target: submission.SphereTarget, skill: skill, roles: roles,
	}
	if err := engine.attachSphereCallSkill(&action, sphere.SphereID); err != nil {
		return battleAction{}, err
	}
	return action, nil
}

func (engine *BattleEngine) attachSphereCallSkill(action *battleAction, sphereID int) error {
	callSkill, callRoles, err := engine.catalog.SphereCallSkill(sphereID)
	if err != nil {
		return err
	}
	action.callSkill = callSkill
	action.callRoles = callRoles
	return nil
}

func (engine *BattleEngine) chalicePlayableResults() ([]BattleResult, error) {
	results := make([]BattleResult, 0, maxRoomMembers)
	chainCounts := engine.currentChainCounts()
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		if player.HP <= 0 {
			continue
		}
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID != 0 && sphere.Type == sphereTypeChalice && sphere.Count > 0 && sphere.Playable {
				skill, _, err := engine.catalog.SphereSkill(sphere.SphereID)
				if err != nil {
					return nil, err
				}
				// 796a8/79d3e uses the owner's selected-card Chain for the
				// hold condition; this is not a zero-Chain sphere attack.
				action := battleAction{memberType: player.MemberType, cardLevel: sphere.Level, skill: skill}
				if !engine.branchConditionSatisfied(skill.AppendCondition, skill.AppendParameters, action, engine.turnActions, chainCounts) {
					continue
				}
				sphere.ChalicePlayable = true
				results = append(results, BattleResult{Command: resultChaliceSpherePlayable, Args: []int64{int64(player.MemberType), int64(sphere.Slot)}})
			}
		}
	}
	return results, nil
}

func (engine *BattleEngine) ReserveChaliceSphere(memberType int, slot int) ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseUser && engine.phase != battlePhaseUserAttack {
		return nil, errors.New("combat chalice sphere reserve phase is unavailable")
	}
	if memberType < 1 || memberType > maxRoomMembers || slot < 0 || slot > deckSphereSlots {
		return nil, errors.New("combat chalice sphere reserve is invalid")
	}
	player := &engine.players[memberType-1]
	if slot != 0 {
		sphere := &player.Spheres[slot-1]
		if sphere.SphereID == 0 || sphere.Type != sphereTypeChalice || sphere.Count <= 0 || !sphere.ChalicePlayable {
			return nil, errors.New("combat chalice sphere is unavailable")
		}
	}
	player.ReservedChalice = slot
	return []BattleResult{{Command: resultChaliceSphereReserve, Args: []int64{int64(memberType), int64(slot)}}}, nil
}

func (engine *BattleEngine) ExecuteChaliceUserPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseUserAttack {
		return nil, errors.New("combat chalice user phase is unavailable")
	}
	actions := make([]battleAction, 0, maxRoomMembers)
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		if player.ReservedChalice == 0 || player.HP <= 0 {
			continue
		}
		sphere := &player.Spheres[player.ReservedChalice-1]
		if sphere.SphereID == 0 || sphere.Type != sphereTypeChalice || sphere.Count <= 0 || !sphere.ChalicePlayable {
			return nil, fmt.Errorf("member %d reserved chalice sphere became unavailable", player.MemberType)
		}
		skill, roles, err := engine.catalog.SphereSkill(sphere.SphereID)
		if err != nil {
			return nil, err
		}
		// 65b9a builds the reserved action with target zero; 7adb0 resolves
		// SELF/whole-side effects without changing the 56 display target.
		action := battleAction{
			memberType: player.MemberType, sphereSlot: sphere.Slot, cardLevel: sphere.Level,
			skill: skill, roles: roles,
		}
		if err := engine.attachSphereCallSkill(&action, sphere.SphereID); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	// FUN_00065b9a builds manual-flag actions and uses the same native sort.
	engine.sortPlayerActions(actions)
	results := make([]BattleResult, 0, len(actions)*12)
	for _, action := range actions {
		if engine.players[action.memberType-1].HP <= 0 {
			continue
		}
		engine.retargetPlayerAction(&action)
		// Like normal spheres, branch at execution, not before earlier actions.
		// 65b9a reuses 3d342: branch conditions read the ordinary selections.
		branch, branchIndex := engine.selectCombatSkillBranchWithIndex(action, engine.turnActions, engine.currentChainCounts())
		roles := engine.catalog.PlayerSkillRoles[branch.FunctionID]
		if branch.ID == 0 || len(roles) == 0 {
			return nil, fmt.Errorf("chalice sphere skill role %d is unavailable", branch.FunctionID)
		}
		branch.Target = action.skill.Target
		action.skill, action.branchIndex, action.roles = branch, branchIndex, roles
		actionResults, err := engine.executeSphereAction(action, 0)
		if err != nil {
			return nil, err
		}
		results = append(results, actionResults...)
		if engine.endType != 0 {
			break
		}
	}
	for playerIndex := range engine.players {
		player := &engine.players[playerIndex]
		if player.ReservedChalice == 0 {
			continue
		}
		player.ReservedChalice = 0
		for sphereIndex := range player.Spheres {
			player.Spheres[sphereIndex].ChalicePlayable = false
		}
	}
	return engine.settleActionPhase(results, battlePhaseChaliceUser, false), nil
}

func (engine *BattleEngine) ExecuteChaliceEnemyPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != battlePhaseEnemy {
		return nil, errors.New("combat chalice enemy phase is unavailable")
	}
	for playerIndex := range engine.players {
		for sphereIndex := range engine.players[playerIndex].Spheres {
			engine.players[playerIndex].Spheres[sphereIndex].ChalicePlayable = false
		}
	}
	engine.phase = battlePhaseChaliceEnemy
	// Native battle5_api_chalice_sphr_exec_enemy_phase calls FUN_0005eb20
	// whenever its terminal check remains clear. FUN_0005eb20 always starts
	// the resulting direction group with zero-argument BUFF_PARTITION(70),
	// even when it has no following cleanup rows.
	rows, err := engine.tickPersistentEffects()
	if err != nil {
		return nil, err
	}
	display, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	rows = append(rows, display...)
	return append([]BattleResult{{Command: resultBuffPartition}}, rows...), nil
}

func (engine *BattleEngine) executeSphereAction(action battleAction, chainCount int) ([]BattleResult, error) {
	if action.memberType < 1 || action.memberType > maxRoomMembers || action.sphereSlot < 1 || action.sphereSlot > deckSphereSlots {
		return nil, errors.New("combat sphere action is invalid")
	}
	sphere := &engine.players[action.memberType-1].Spheres[action.sphereSlot-1]
	if sphere.SphereID == 0 || sphere.Count <= 0 {
		return nil, errors.New("combat sphere action is unavailable")
	}
	header, err := playerSphereSkillResult(action, chainCount)
	if err != nil {
		return nil, err
	}
	results := []BattleResult{header}
	engine.recordExecutedPlayerSkill(action, chainCount)
	skillRows, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill.Target, action.roles, func(role CombatSkillRole) ([]BattleResult, error) {
		return engine.executePlayerRole(action, role, chainCount)
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
	engine.addPlayerHate(action.memberType, int64(maxInt(0, action.skill.HateRatio)))
	sphere.Count--
	if engine.enemies[0].HP <= 0 && engine.endType == 0 {
		engine.endType = 1
	}
	return results, nil
}
