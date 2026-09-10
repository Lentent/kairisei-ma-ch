package multiplayer

import "sort"

// A single role consumer supplies a 6 snapshot after every 62/69. A complete
// 7adb0 skill defers these records until all consumers finish; see below.
func (engine *BattleEngine) projectRoleBuffParameters(rows []BattleResult) []BattleResult {
	var out []BattleResult
	for index, row := range rows {
		out = append(out, row)
		if (row.Command != resultBuff && row.Command != resultPassiveBuff) || len(row.Args) == 0 {
			continue
		}
		member := int(row.Args[0])
		if index+1 < len(rows) && rows[index+1].Command == resultBattleParam && len(rows[index+1].Args) > 0 && rows[index+1].Args[0] == row.Args[0] {
			continue
		}
		var args []int64
		if member >= 1 && member <= len(engine.players) {
			p := &engine.players[member-1]
			args = battleParameterArgs(member, p.HP, p.MaxHP, p.Attack, p.Magic, p.Recovery, p.Defense, p.MDefense, p.LimitAttack, p.LimitMagic, p.LimitRecovery)
		} else if member >= 5 && member < 5+engine.enemyCount {
			e := &engine.enemies[member-5]
			args = battleParameterArgs(member, e.HP, e.MaxHP, e.Attack, e.Magic, e.Recovery, e.Defense, e.MDefense, e.LimitAttack, e.LimitMagic, e.LimitRecovery)
		}
		if args != nil {
			out = append(out, BattleResult{Command: resultBattleParam, Args: args})
		}
	}
	return out
}

// 7adb0 first executes the complete role set through 8b660 (which may emit
// immediate attack/HOLD_SET rows), then drains status buffers in role order,
// NORMAL/BURST_NORMAL/PASSIVE/BURST_PASSIVE order. Each status's 6 is a query
// of the final skill state, not the state immediately after its own consumer.
// This only changes projection: later consumers still observe earlier effects.
func (engine *BattleEngine) projectSkillStatusResults(rows []BattleResult) []BattleResult {
	var immediate, statuses, dotUpdates []BattleResult
	statusSucceeded := make(map[int64]bool)
	for i := 0; i < len(rows); i++ {
		row := rows[i]
		if row.Command == 201 && len(row.Args) == 7 || row.Command == 202 && len(row.Args) == 2 {
			dotUpdates = append(dotUpdates, row)
			continue
		}
		// 7adb0 drains failed statuses/releases by role after all consumers.
		failed := row.Command == resultBuffReleaseFailed && len(row.Args) == 2 ||
			row.Command == resultDebuffFailed && len(row.Args) == 4
		if !failed && ((row.Command != resultBuff && row.Command != resultPassiveBuff) || len(row.Args) < 3) {
			immediate = append(immediate, row)
			continue
		}
		statuses = append(statuses, row)
		if !failed {
			statusSucceeded[row.Args[1]] = true
		}
		if i+1 < len(rows) && rows[i+1].Command == resultBattleParam && len(rows[i+1].Args) > 0 && rows[i+1].Args[0] == row.Args[0] {
			i++
		}
	}
	// 7adb0 emits 65/67 only when this role produced no 62/69 in any
	// status list. A successful sibling target suppresses pending failures.
	filtered := statuses[:0]
	for _, row := range statuses {
		if (row.Command == resultDebuffFailed || row.Command == resultBuffReleaseFailed) && statusSucceeded[row.Args[1]] {
			continue
		}
		filtered = append(filtered, row)
	}
	statuses = filtered
	listOrder := func(row BattleResult) int {
		if row.Command == resultDebuffFailed {
			return 4
		}
		if row.Command == resultBuffReleaseFailed {
			return 5
		}
		switch row.Args[2] {
		case 0:
			return 0
		case 5:
			return 1
		case 1:
			return 2
		case 6:
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if statuses[i].Args[1] != statuses[j].Args[1] {
			return statuses[i].Args[1] < statuses[j].Args[1]
		}
		return listOrder(statuses[i]) < listOrder(statuses[j])
	})
	immediate = append(immediate, engine.projectRoleBuffParameters(statuses)...)
	// 7adb0 drains DOT updates in a separate role loop after ordinary
	// statuses. Any successful target suppresses that role's missing-DOT rows.
	sort.SliceStable(dotUpdates, func(i, j int) bool { return dotUpdates[i].Args[1] < dotUpdates[j].Args[1] })
	succeeded := make(map[int64]bool)
	for _, row := range dotUpdates {
		if row.Command == 201 {
			succeeded[row.Args[1]] = true
		}
	}
	for _, row := range dotUpdates {
		if row.Command == 201 || !succeeded[row.Args[1]] {
			immediate = append(immediate, row)
		}
	}
	return immediate
}
