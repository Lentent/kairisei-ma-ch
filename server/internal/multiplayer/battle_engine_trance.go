package multiplayer

import "strings"

// Original EnemyData's four gauge fields. State and action counters are
// separate: 56efb clears only the reaction counters, never the retained gauge.
type battleEnemyTrance struct {
	State, Value, Remaining                          int
	NormalLimit, OverheatLimit, Duration, Resistance int
	SkipCountdown, Extended                          bool
	Damage, OtherDamage                              int64
	Debuff, Covering                                 bool
}

func newEnemyTrance(def CombatEnemyDefinition) battleEnemyTrance {
	t := battleEnemyTrance{NormalLimit: def.TranceLimit, OverheatLimit: def.OverheatLimit,
		Duration: def.OverheatTurns, Resistance: def.OverheatResist}
	if def.TranceLimit > 0 {
		t.State = 1
	}
	return t
}

func (t *battleEnemyTrance) limit() int {
	if t.State == 1 {
		return t.NormalLimit
	}
	if t.State == 2 {
		return t.OverheatLimit
	}
	return 0
}

func tranceState(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "0", "NONE", "NULL":
		return 0
	case "1", "NORMAL":
		return 1
	case "2", "TRANCE":
		return 2
	case "3", "OVER_HEAT":
		return 3
	default:
		return -1
	}
}

// 58606 resets both gauges and the duration. A disabled enemy cannot be
// enabled by a card; changing to the current state preserves its value.
func changeEnemyTrance(enemy *battleEnemy, state int) []BattleResult {
	t := &enemy.Trance
	if t.State == 0 || state < 1 || state > 3 || t.State == state {
		return nil
	}
	t.State, t.Value, t.Remaining = state, 0, 0
	if state == 3 {
		t.Remaining, t.SkipCountdown, t.Extended = t.Duration, true, false
	}
	return []BattleResult{{Command: 87, Args: []int64{int64(enemy.MemberType), int64(state), int64(t.limit()), int64(t.Remaining)}}}
}

// 5948e uses signed int32 arithmetic before division. In TRANCE, UP
// reduces the overheat gauge while DOWN increases it. Emit even a zero delta.
func controlEnemyTrance(enemy *battleEnemy, role CombatSkillRole, level int) []BattleResult {
	t := &enemy.Trance
	if role.Function == "TRANCE_GAUGE_STATE_CHANGE" {
		return changeEnemyTrance(enemy, tranceState(role.Parameters[0]))
	}
	filter := tranceState(role.Parameters[0])
	limit := int32(t.limit())
	if t.State != 1 && t.State != 2 || limit <= 0 || filter < 0 || filter != 0 && filter != t.State {
		return nil
	}
	percent := int32(combatParameterInt(role.Parameters[1])) + int32(level)*int32(combatParameterInt(role.Parameters[2]))
	if role.Function == "TRANCE_GAUGE_VALUE_DOWN" {
		percent = -percent
	}
	delta := limit * percent / 100
	if t.State == 2 {
		delta = -delta
	}
	t.Value = int(max(int32(0), min(limit, int32(t.Value)+delta)))
	return []BattleResult{{Command: 205, Args: []int64{int64(enemy.MemberType), int64(t.Value), int64(role.RoleIndex)}}}
}

func (engine *BattleEngine) executeEnemyTranceRole(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	var rows []BattleResult
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		rows = append(rows, engine.applyTranceRole(&engine.enemies[index], role, 1)...)
	}
	return rows
}

func (engine *BattleEngine) executePlayerTranceRole(action battleAction, role CombatSkillRole) []BattleResult {
	var rows []BattleResult
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		rows = append(rows, engine.applyTranceRole(&engine.enemies[index], role, action.cardLevel)...)
	}
	return rows
}

// 80810 rolls resistance before the producer's level-scaled success rate,
// even for an enemy outside OVER_HEAT. 59779 allows one extension per cycle.
func (engine *BattleEngine) applyTranceRole(enemy *battleEnemy, role CombatSkillRole, level int) []BattleResult {
	if role.Function != "TRANCE_GAUGE_OVER_HEAT_TURN_ADD" {
		return controlEnemyTrance(enemy, role, level)
	}
	t := &enemy.Trance
	chance := int(int32(combatParameterInt(role.Parameters[1])) + int32(level)*int32(combatParameterInt(role.Parameters[2])))
	args := []int64{int64(enemy.MemberType), int64(role.RoleIndex)}
	if engine.rollPercent10000(t.Resistance) || !engine.rollPercent10000(chance) || t.State != 3 || t.Extended {
		return []BattleResult{{Command: 204, Args: args}}
	}
	t.Extended = true
	t.Remaining = int(max(int32(0), int32(t.Remaining)+int32(combatParameterInt(role.Parameters[0]))))
	return []BattleResult{{Command: 203, Args: args},
		{Command: 89, Args: []int64{int64(enemy.MemberType), int64(t.Remaining)}}}
}

// 58382 dispatches SELF_* against the actor and ENEMY_* against all live
// enemies. A disabled gauge is not NORMAL.
func (engine *BattleEngine) enemyTranceCondition(enemy *battleEnemy, value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "", "NULL", "0":
		return true
	case "SELF_NORMAL", "1":
		return enemy.Trance.State == 1
	case "SELF_TRANCE", "2":
		return enemy.Trance.State == 2
	case "SELF_OVER_HEAT", "3":
		return enemy.Trance.State == 3
	}
	state := map[string]int{"ENEMY_NORMAL": 1, "4": 1, "ENEMY_TRANCE": 2, "5": 2, "ENEMY_OVER_HEAT": 3, "6": 3}[value]
	for i := 0; state != 0 && i < engine.enemyCount; i++ {
		if engine.enemies[i].HP > 0 && engine.enemies[i].Trance.State == state {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) clearTranceReactions() {
	for i := 0; i < engine.enemyCount; i++ {
		t := &engine.enemies[i].Trance
		t.Damage, t.OtherDamage, t.Debuff, t.Covering = 0, 0, false, false
	}
}

// 4cdac -> 58c8e runs for every live enemy, including one not targeted by
// the action. The greatest reaction wins; damage and debuffs are not summed.
func (engine *BattleEngine) finishTranceReactions(cost int) []BattleResult {
	var rows []BattleResult
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		t := &enemy.Trance
		// 58c8e also emits an inert zero for a disabled gauge with a
		// nonzero overheat limit; 62f63 returns zero rates for state NONE.
		if enemy.HP > 0 && t.State == 0 && t.OverheatLimit > 0 {
			rows = append(rows, BattleResult{Command: 88, Args: []int64{int64(enemy.MemberType), 0}})
			continue
		}
		if enemy.HP <= 0 || t.State != 1 && t.State != 2 || t.limit() <= 0 {
			continue
		}
		limit := int32(t.limit())
		rate := func(kind int) int64 {
			return int64(int32(engine.catalog.TranceRates[kind][t.State-1][max(0, min(4, cost-1))]) * limit / 100)
		}
		amount := max(int64(0), min(t.Damage, rate(0)), min(t.OtherDamage, rate(1)))
		if t.Debuff {
			amount = max(amount, rate(2))
		}
		if t.Covering && t.State == 1 {
			amount = max(amount, rate(3))
		}
		t.Value = int(min(limit, int32(t.Value)+int32(amount)))
		rows = append(rows, BattleResult{Command: 88, Args: []int64{int64(enemy.MemberType), int64(t.Value)}})
	}
	engine.clearTranceReactions()
	return rows
}

// 588a0 runs at the user/enemy phase boundary, not after each card.
func (engine *BattleEngine) advanceTranceStates() []BattleResult {
	var rows []BattleResult
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		t := &enemy.Trance
		if enemy.HP <= 0 || t.State != 1 && t.State != 2 || t.limit() <= 0 || t.Value < t.limit() {
			continue
		}
		rows = append(rows, BattleResult{Command: 501, Args: []int64{int64(enemy.MemberType)}})
		rows = append(rows, changeEnemyTrance(enemy, t.State+1)...)
	}
	return rows
}

// 59098 skips the cleanup in which OVER_HEAT began, then counts full turns.
func tickEnemyTrance(enemy *battleEnemy) []BattleResult {
	t := &enemy.Trance
	if t.State != 3 || t.Duration <= 0 {
		return nil
	}
	if t.SkipCountdown {
		t.SkipCountdown = false
		return nil
	}
	t.Remaining = int(int32(t.Remaining) - 1)
	rows := []BattleResult{{Command: 89, Args: []int64{int64(enemy.MemberType), int64(t.Remaining)}}}
	if t.Remaining < 1 {
		rows = append(rows, BattleResult{Command: 501, Args: []int64{int64(enemy.MemberType)}})
		rows = append(rows, changeEnemyTrance(enemy, 1)...)
	}
	return rows
}

func resumeEnemyTrance(enemy *battleEnemy) []BattleResult {
	t := &enemy.Trance
	if t.State == 0 || enemy.HP <= 0 {
		return nil
	}
	return []BattleResult{{Command: 500, Args: []int64{int64(enemy.MemberType), int64(t.State), int64(t.Value), int64(t.limit()), int64(t.Remaining)}}}
}
