package multiplayer

import "strings"

// battleParameterDeltas is the durable parameter projection owned by the
// native buff list. FUN_00071d48 aggregates retained effects and
// FUN_00071e60 rebuilds BATTLE_PARAM from the base member tuple plus those
// effects; it does not undo a clamped current value by adding the requested
// amount back later.
type battleParameterDeltas struct {
	maxHP    int
	attack   int
	magic    int
	recovery int
	defense  int
	mDefense int
}

// 71860 reads the base tuple plus PASSIVE(list1), not the full battle tuple.
// BURST_PASSIVE(list6) can survive a wave's end but must not leak into row5.
func playerBaseParameterResult(player *battlePlayer) BattleResult {
	view := *player
	ensurePlayerBaseParameters(&view)
	view.LimitAttack, view.LimitMagic, view.LimitRecovery = 99999, 99999, 99999
	view.Effects = nil
	for _, effect := range player.Effects {
		if effect.ListType == 1 {
			view.Effects = append(view.Effects, effect)
		}
	}
	refreshPlayerBattleParameters(&view)
	return BattleResult{Command: resultBaseParam, Args: baseParameterArgs(view.MemberType,
		view.MaxHP, view.Attack, view.Magic, view.Recovery, view.Defense, view.MDefense)}
}

func combatParameterSupported(parameter string) bool {
	switch strings.ToUpper(parameter) {
	case "MAX_HP", "ATK", "INT", "MND", "DEF", "MDEF":
		return true
	default:
		return false
	}
}

func retainedBattleParameterDeltas(effects []battleEffect) battleParameterDeltas {
	var deltas battleParameterDeltas
	for _, effect := range effects {
		if effect.Delta == 0 || effect.Function == "PARAM_LIMIT_BREAK_FIXED" {
			continue
		}
		switch strings.ToUpper(effect.Parameter) {
		case "MAX_HP":
			deltas.maxHP += effect.Delta
		case "ATK":
			deltas.attack += effect.Delta
		case "INT":
			deltas.magic += effect.Delta
		case "MND":
			deltas.recovery += effect.Delta
		case "DEF":
			deltas.defense += effect.Delta
		case "MDEF":
			deltas.mDefense += effect.Delta
		}
	}
	return deltas
}

func ensurePlayerBaseParameters(player *battlePlayer) {
	if player.BaseMaxHP != 0 {
		return
	}
	deltas := retainedBattleParameterDeltas(player.Effects)
	player.BaseMaxHP = maxInt(1, player.MaxHP-deltas.maxHP)
	player.BaseAttack = maxInt(0, player.Attack-deltas.attack)
	player.BaseMagic = maxInt(0, player.Magic-deltas.magic)
	player.BaseRecovery = maxInt(0, player.Recovery-deltas.recovery)
	player.BaseDefense = player.Defense - deltas.defense
	player.BaseMDefense = player.MDefense - deltas.mDefense
}

func ensureEnemyBaseParameters(enemy *battleEnemy) {
	if enemy.BaseMaxHP != 0 {
		return
	}
	deltas := retainedBattleParameterDeltas(enemy.Effects)
	enemy.BaseMaxHP = maxInt(1, enemy.MaxHP-deltas.maxHP)
	enemy.BaseAttack = maxInt(0, enemy.Attack-deltas.attack)
	enemy.BaseMagic = maxInt(0, enemy.Magic-deltas.magic)
	enemy.BaseRecovery = maxInt(0, enemy.Recovery-deltas.recovery)
	enemy.BaseDefense = enemy.Defense - deltas.defense
	enemy.BaseMDefense = enemy.MDefense - deltas.mDefense
}

func refreshPlayerBattleParameters(player *battlePlayer) {
	ensurePlayerBaseParameters(player)
	deltas := retainedBattleParameterDeltas(player.Effects)
	// 71860/71e60 clamp the projected player tuple, not its raw base.
	// Enemy HP has a separate minimum of one and no player ceiling.
	player.MaxHP = maxInt(500, minInt(999999, player.BaseMaxHP+deltas.maxHP))
	player.Attack = maxInt(0, minInt(activeParameterLimit(player.LimitAttack), player.BaseAttack+deltas.attack))
	player.Magic = maxInt(0, minInt(activeParameterLimit(player.LimitMagic), player.BaseMagic+deltas.magic))
	player.Recovery = maxInt(0, minInt(activeParameterLimit(player.LimitRecovery), player.BaseRecovery+deltas.recovery))
	// 74292/74344 retain negative defense. Only players have the symmetric
	// +/-999999 cap; attack consumers deliberately request signed values.
	player.Defense = maxInt(-999999, minInt(999999, player.BaseDefense+deltas.defense))
	player.MDefense = maxInt(-999999, minInt(999999, player.BaseMDefense+deltas.mDefense))
	// Projection/expiry only clamp HP. The application consumer separately
	// adds a positive MAX_HP delta once (88a20 -> 73e9f).
	player.HP = minInt(player.HP, player.MaxHP)
}

func refreshAppliedPlayerParameter(player *battlePlayer, parameter string, delta int) {
	hp := player.HP
	refreshPlayerBattleParameters(player)
	// 8f4e9 calls 73e9f/722ee only for a nonzero MAX_HP delta. Other
	// parameter buffs leave current HP intact, even above the visible cap.
	if !strings.EqualFold(parameter, "MAX_HP") || delta == 0 {
		player.HP = hp
	} else if delta > 0 {
		player.HP = nativeHPCommit(hp, player.MaxHP, delta, player.Effects)
	}
}

func refreshAppliedEnemyParameter(enemy *battleEnemy, parameter string, delta int) {
	hp := enemy.HP
	refreshEnemyBattleParameters(enemy)
	if !strings.EqualFold(parameter, "MAX_HP") || delta == 0 {
		enemy.HP = hp
	} else if delta > 0 {
		enemy.HP = nativeHPCommit(hp, enemy.MaxHP, delta, enemy.Effects)
	}
}

func refreshEnemyBattleParameters(enemy *battleEnemy) {
	ensureEnemyBaseParameters(enemy)
	deltas := retainedBattleParameterDeltas(enemy.Effects)
	enemy.MaxHP = maxInt(1, enemy.BaseMaxHP+deltas.maxHP)
	enemy.Attack = maxInt(0, enemy.BaseAttack+deltas.attack)
	enemy.Magic = maxInt(0, enemy.BaseMagic+deltas.magic)
	enemy.Recovery = maxInt(0, enemy.BaseRecovery+deltas.recovery)
	enemy.Defense = enemy.BaseDefense + deltas.defense
	enemy.MDefense = enemy.BaseMDefense + deltas.mDefense
	// The same native ceiling projection is shared by player and enemy members.
	enemy.HP = minInt(enemy.HP, enemy.MaxHP)
}
