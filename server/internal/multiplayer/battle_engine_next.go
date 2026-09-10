package multiplayer

import (
	"errors"
	"fmt"
)

// NextBattle implements the CN TEAMBATTLE (mode 1) default handover. Managed
// AwakeTakeOverSet only supplies configurable flags in EXPLORE (mode 0).
// Native 62af0 resets turn/cost/sphere use; 5d160/49830 recycles the trash
// without taking held cards away. a50c4 restores KO users to ONE HP only
// while they have not been formally retired by battle5_api_gameover.
// UserSet and RNG initialization are not repeated at this boundary.
// Return a separate engine so failure cannot partially advance a live room.
func (engine *BattleEngine) NextBattle(partyID int, drops []BattleDrop) (*BattleEngine, error) {
	if engine.phase != battlePhaseEnded || (engine.endType != 1 && engine.endType != 4) {
		return nil, errors.New("next battle requires a completed winning/awake wave")
	}
	party, exists := engine.catalog.EnemyParties[partyID]
	if !exists {
		return nil, fmt.Errorf("next enemy party %d is unavailable", partyID)
	}
	next := *engine
	if err := next.loadEnemyParty(party, drops); err != nil {
		return nil, err
	}
	next.elapsedWaveTurns += next.turn
	next.turn, next.endType = 0, 0
	next.resumeSide, next.turnDrawn = 0, [4][5]bool{}
	next.phase = battlePhaseCreated
	next.selectedPlays = make(map[int]cardPlaySubmission)
	next.enemyUses = make(map[int]int)
	next.enemyTriggerTarget, next.turnStats, next.turnActions = 0, battleTurnStats{}, nil
	next.damageHistory = [29][]battleDamageEvent{}
	next.forceEndCheck = false
	next.waveRecovery = nil
	for index := range next.players {
		player := &next.players[index]
		// End cleanup normally removed these. Also clean a KO member, whose
		// stale effects must not revive alongside it in the next wave.
		if len(player.Effects) != 0 {
			player.Effects = append([]battleEffect(nil), player.Effects...)
			_ = appendDefaultWavePlayerCleanup(nil, player)
		}
		if !player.GameOver && player.HP <= 0 {
			player.HP = 1
			next.waveRecovery = append(next.waveRecovery, BattleResult{Command: resultHP,
				Args: []int64{int64(player.MemberType), int64(player.MaxHP), 1, 0}})
		}
		player.Cost = next.costInitial
		player.DamageTaken, player.TurnDamage = 0, 0
		player.ExecutedBuffKinds = [69]uint32{}
		player.BlessHolds, player.ReservedChalice = nil, 0
		player.CardDisplay, player.CardCostDown = [11]battleDisplayPower{}, [11]int{}
		player.CardBurstSkills = [11][]int{}
		for slot := range player.Spheres {
			player.Spheres[slot].Count = player.Spheres[slot].Maximum
			player.Spheres[slot].Playable = false
			player.Spheres[slot].Remaining = 0
		}
		next.recycleDrawPool(player)
	}
	return &next, nil
}
