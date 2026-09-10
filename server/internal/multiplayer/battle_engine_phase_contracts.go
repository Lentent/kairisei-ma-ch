package multiplayer

import "fmt"

func validateBattlePhaseControlContracts() error {
	for _, row := range []BattleResult{
		{Command: resultWaitAndSee},
		{Command: resultBuffPartition},
		{Command: resultBuffLostOne, Args: []int64{1, 17, 0}},
	} {
		if _, err := row.CSV(); err != nil {
			return fmt.Errorf("phase-control ResultCmd%d boundary: %w", row.Command, err)
		}
	}

	waitEngine := &BattleEngine{phase: battlePhaseUserAttack, enemyCount: 1}
	for index := range waitEngine.players {
		waitEngine.players[index] = battlePlayer{MemberType: index + 1, HP: 1, MaxHP: 1}
	}
	waitEngine.enemies[0] = battleEnemy{MemberType: 5, HP: 1, MaxHP: 1}
	waitResults, err := waitEngine.EnemyPhase()
	if err != nil || len(waitResults) != 1 || waitResults[0].Command != resultWaitAndSee {
		return fmt.Errorf("native empty enemy phase is results=%+v err=%v", waitResults, err)
	}

	stunEngine := &BattleEngine{phase: battlePhaseUserAttack, enemyCount: 1}
	for index := range stunEngine.players {
		stunEngine.players[index] = battlePlayer{MemberType: index + 1, HP: 1, MaxHP: 1}
	}
	stunEngine.enemies[0] = battleEnemy{
		MemberType: 5, HP: 1, MaxHP: 1,
		Effects: []battleEffect{{Function: "STAN", Remaining: 1}},
	}
	stunResults, err := stunEngine.EnemyPhase()
	if err != nil || len(stunResults) != 1 || stunResults[0].Command != resultStun {
		return fmt.Errorf("native interrupted enemy phase is results=%+v err=%v", stunResults, err)
	}

	chaliceEngine := &BattleEngine{phase: battlePhaseEnemy}
	chaliceResults, err := chaliceEngine.ExecuteChaliceEnemyPhase()
	if err != nil || len(chaliceResults) != 5 || chaliceResults[0].Command != resultBuffPartition ||
		chaliceEngine.phase != battlePhaseChaliceEnemy {
		return fmt.Errorf("native chalice enemy partition is results=%+v phase=%d err=%v", chaliceResults, chaliceEngine.phase, err)
	}
	// Original API transcripts retain both NORMAL/EVENT snapshots for the
	// two internal field members, even with no real actors in this fixture.
	for index, row := range chaliceResults[1:] {
		if row.Command != resultBattleParam || !equalBattleArgs(row.Args,
			battleParameterArgs(10+index/2, 0, 1, 0, 0, 0, 0, 0, 99999, 99999, 99999)) {
			return fmt.Errorf("native field-member expiry snapshot is %+v", row)
		}
	}

	terminalEngine := &BattleEngine{phase: battlePhaseUserAttack, endType: 1}
	terminalEngine.players[0] = battlePlayer{
		MemberType: 1, HP: 100, MaxHP: 100,
		Effects: []battleEffect{{Function: "BURN", Kind: 2, Remaining: 2, RoleIndex: 4}},
	}
	terminalResults := terminalEngine.appendTerminalBuffCleanup([]BattleResult{battleEndResult(1)})
	wantTerminal := []int{resultEnd, 72, resultBaseParam, resultBattleParam}
	if len(terminalResults) != len(wantTerminal) {
		return fmt.Errorf("native terminal buff cleanup is %+v", terminalResults)
	}
	for index, command := range wantTerminal {
		if terminalResults[index].Command != command {
			return fmt.Errorf("native terminal buff cleanup order is %+v", terminalResults)
		}
	}
	if len(terminalResults[0].Args) != 1 || terminalResults[0].Args[0] != 1 {
		return fmt.Errorf("native END projection is %+v", terminalResults[0])
	}
	if terminalResults[1].Args[0] != 1 || terminalResults[1].Args[2] != int64(battleBuffCodes["BURN"]) || len(terminalEngine.players[0].Effects) != 0 {
		return fmt.Errorf("native default terminal release is result=%+v player=%+v", terminalResults[1], terminalEngine.players[0])
	}
	return nil
}
