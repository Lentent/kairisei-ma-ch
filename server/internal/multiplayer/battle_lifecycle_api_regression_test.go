package multiplayer

import (
	"reflect"
	"testing"
)

func TestCardStatePublishesDurationNotDamage(t *testing.T) {
	player := battlePlayer{MemberType: 1, Effects: []battleEffect{
		{Function: "CARD_TRAP_DAMAGE", CardType: 1, Value: 1001, Remaining: 2},
		{Function: "CARD_SEAL", CardType: 2, Remaining: 3},
	}}
	for i, args := range [][]int64{{1, 1, 2, 2}, {1, 2, 1, 3}, {1, 3, 0, 0}} {
		if row := playerCardStateResult(&player, i+1); !equalBattleArgs(row.Args, args) {
			t.Fatalf("card state = %+v; want %v", row, args)
		}
	}
	if player.Effects[0].Value != 1001 {
		t.Fatal("display projection overwrote actual trap damage")
	}
}

func TestCardTrapDamageIsNonlethalAndReportsCommittedHP(t *testing.T) {
	for _, hp := range []int{500, 1} {
		engine, _ := nextBattleFixture(t)
		p := &engine.players[0]
		p.HP = hp
		p.Effects = []battleEffect{{Function: "CARD_TRAP_DAMAGE", CardType: 1, Value: 1001, Remaining: 2, Source: 5, SourceSkillID: 44252016}}
		rows := engine.triggerCardTrap(1, 1)
		if p.HP != 1 || len(rows) != 5 || !equalBattleArgs(rows[1].Args, []int64{1, 0, int64(1 - hp), 1, 0, 0, 100, 0, 0, 0}) || len(p.Effects) != 0 {
			t.Fatalf("nonlethal trap at %d HP: rows=%+v, player=%+v", hp, rows, p)
		}
	}
}

func TestNoContinueRetirementKeepsEffectsAndClearsPendingPlay(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	engine.phase, engine.turn = battlePhaseUser, 1
	for i := range engine.players {
		p := &engine.players[i]
		p.HP, p.BurstBreak, p.BurstRecharge[0] = 0, 2, 3
		p.Effects = []battleEffect{{Function: "DEF_UP_FIXED", Parameter: "DEF", Delta: 100,
			Remaining: 2, SourceSkillID: 123, ListType: 0}}
		p.BlessHolds = []battleBlessHold{{Remaining: 2}}
		p.ReservedChalice = 1
		p.Hand = [5]int{1, 2, 3, 4, 5}
		engine.selectedPlays[p.MemberType] = cardPlaySubmission{CardTypes: [5]int{1}}
	}
	rows := engine.settleActionPhase(nil, battlePhaseUserAttack, true)
	want := []BattleResult{{Command: resultTurn, Args: []int64{1, 1}},
		battleGameOverResult(1), battleGameOverResult(2), battleGameOverResult(3), battleGameOverResult(4), battleEndResult(2)}
	if !reflect.DeepEqual(rows, want) || engine.phase != battlePhaseEnded || engine.endType != 2 {
		t.Fatalf("no-continue loss must end after explicit gameover rows, without victory cleanup: %+v", rows)
	}
	for _, p := range engine.players {
		if !p.GameOver || len(p.BlessHolds) != 0 || p.ReservedChalice != 0 || len(p.Effects) != 1 ||
			p.Effects[0].Remaining != 2 || p.BurstBreak != 2 || p.BurstRecharge[0] != 3 || p.Hand != [5]int{1, 2, 3, 4, 5} {
			t.Fatal("retirement changed retained state or failed to clear pending holds/selection")
		}
	}
	if len(engine.selectedPlays) != 0 || len(engine.appendNewPlayerGameOver(nil)) != 0 {
		t.Fatal("retirement left selections or emitted a duplicate gameover")
	}
	resume, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	retired, buffs, hands := 0, 0, 0
	for _, row := range resume {
		switch row.Command {
		case resultHoldMax, resultCost, resultCostBlock, resultBurstState:
			t.Fatalf("retired player received live-play resume metadata: %+v", row)
		case resultResumeGameOver:
			retired++
		case resultResumeBuff:
			buffs++
		case resultResumeCardHand:
			hands++
		}
	}
	if retired != 4 || buffs != 4 || hands != 4 {
		t.Fatal("resume lost retired identity, retained buff or hand state")
	}
}

func TestContinuePreparesPartyWithoutMutatingLiveBattle(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	engine.phase, engine.turn = battlePhaseEnemy, 2
	for i := range engine.players {
		p := &engine.players[i]
		p.HP, p.MaxHP, p.BaseMaxHP = 100, 1000, 1000
		p.Hand, p.Cost = [5]int{1, 2}, 2
		p.Effects = []battleEffect{
			{Function: "POISON", ListType: 0, Value: 10, Remaining: 2, SourceSkillID: 102},
			{Function: "ATK_BREAK_FIXED", ListType: 0, Parameter: "ATK", Delta: -10, Remaining: 2, SourceSkillID: 101},
			{Function: "POISON", ListType: 2, Value: 20, Remaining: 2, SourceSkillID: 202},
			{Function: "ATK_BREAK_FIXED", ListType: 2, Parameter: "ATK", Delta: -20, Remaining: 2, SourceSkillID: 201},
			{Function: "POISON", ListType: 1, Value: 30, Remaining: 2},
			{Function: "DEF_UP_FIXED", ListType: 0, Parameter: "DEF", Delta: 10, Remaining: 2},
		}
		p.BlessHolds = []battleBlessHold{{CardType: 22, Remaining: 2}, {CardType: 21, Remaining: 2, AppendIndex: 7}}
	}
	engine.players[0].HP = 0
	engine.players[2].HP, engine.players[2].GameOver = 0, true
	before, random := engine.players, engine.rng
	plan, err := engine.prepareContinue(1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engine.players, before) || engine.rng != random {
		t.Fatal("preparing payment changed live members or RNG")
	}
	if !reflect.DeepEqual(plan.players[2], before[2]) {
		t.Fatal("continuation revived or cleared a retired member")
	}
	for _, i := range []int{0, 1, 3} {
		p := plan.players[i]
		if p.HP != 1000 || !p.ContinueDraw || p.Cost != before[i].Cost || p.Hand != before[i].Hand ||
			len(p.Effects) != 2 || p.Effects[0].ListType != 1 || p.Effects[1].Function != "DEF_UP_FIXED" ||
			len(p.BlessHolds) != 1 || p.BlessHolds[0].CardType != 22 {
			t.Fatalf("continue party state for member %d is incorrect", i+1)
		}
	}
	var groups [][2]int64
	for _, row := range plan.results {
		if row.Command == resultBuffRelease {
			t.Fatal("continuation emitted a skill-release announcement")
		}
		if row.Command == resultBuffLostOne && row.Args[0] == 1 {
			groups = append(groups, [2]int64{row.Args[1], row.Args[2]})
		}
	}
	// Native category order: NORMAL parameter/status, then EVENT parameter/status.
	if !reflect.DeepEqual(groups, [][2]int64{{100, 101}, {304, 102}, {100, 201}, {304, 202}}) {
		t.Fatalf("continue release order = %v", groups)
	}
	if !reflect.DeepEqual(plan.results[0], BattleResult{Command: resultRevive, Args: []int64{1}}) ||
		!reflect.DeepEqual(plan.results[len(plan.results)-1], BattleResult{Command: resultContinue, Args: []int64{1}}) {
		t.Fatal("continue revival/direction boundary is incorrect")
	}
	if _, err := encodeBattleResults(plan.results); err != nil {
		t.Fatal(err)
	}
	for _, member := range []int{0, 3, 5} {
		if _, err := engine.prepareContinue(member); err == nil {
			t.Fatalf("invalid continue member %d accepted", member)
		}
	}
}

func TestContinueRefillsHandOnNextDrawOnly(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	engine.turn = 2
	p := &engine.players[0]
	p.Hand, p.HP = [5]int{}, 1
	p.Effects = []battleEffect{{Function: "DEAL_PENALTY", ListType: 1, Value: 99, Remaining: 3}}
	random := engine.rng
	plan, err := engine.prepareContinue(1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.players[0].Hand != [5]int{} || engine.rng != random {
		t.Fatal("continue drew cards before the next turn")
	}
	engine.players = plan.players
	engine.prepareTurnDraw()
	if engine.players[0].ContinueDraw {
		t.Fatal("one-time refill was not consumed")
	}
	for _, slot := range engine.players[0].Hand {
		if slot == 0 {
			t.Fatal("continue refill did not override ordinary draw penalty")
		}
	}
}

func TestPlayerProjectionBoundsPreserveRawStartHP(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	for i, maximum := range []int{100, 500, 2000000, 1000} {
		p := &engine.players[i]
		p.HP, p.MaxHP, p.BaseMaxHP = maximum, maximum, maximum
		p.Attack, p.BaseAttack = 200000, 200000
		p.Magic, p.BaseMagic = 300000, 300000
		p.Recovery, p.BaseRecovery = 400000, 400000
	}
	rows, err := engine.Start()
	if err != nil {
		t.Fatal(err)
	}
	for i, maximum := range []int{500, 500, 999999, 1000} {
		p := &engine.players[i]
		if p.MaxHP != maximum || p.Attack != 99999 || p.Magic != 99999 || p.Recovery != 99999 || p.HP != p.BaseMaxHP {
			t.Fatalf("raw tuple and player projection were conflated: %+v", p)
		}
		found := false
		for _, row := range rows {
			if row.Command == resultBattleParam && row.Args[0] == int64(i+1) {
				found = equalBattleArgs(row.Args, battleParameterArgs(i+1, p.BaseMaxHP, maximum, 99999, 99999, 99999, 0, 0, 99999, 99999, 99999))
			}
		}
		if !found {
			t.Fatal("Start did not publish native bounded player parameters")
		}
	}
	view := &engine.players[2]
	view.LimitAttack = 299999
	refreshPlayerBattleParameters(view)
	if view.BaseAttack != 200000 || view.Attack != 200000 || view.HP != 999999 {
		t.Fatal("parameter refresh lost raw base or failed to apply an increased limit/current HP clamp")
	}
}

func TestResumeEnemyProjectsOfficialParentIndex(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	engine.phase = battlePhaseStarted
	engine.enemyCount = 2
	engine.enemies[1] = battleEnemy{MemberType: 6, Parent: 1, HP: 100, MaxHP: 100}
	rows, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]int64{5: 0, 6: 1}
	for _, row := range rows {
		if row.Command != resultResumeEnemy {
			continue
		}
		if len(row.Args) != 7 || row.Args[1] != want[int(row.Args[0])] {
			t.Fatalf("resume enemy lost parent identity: %+v", row)
		}
		delete(want, int(row.Args[0]))
	}
	if len(want) != 0 {
		t.Fatal("resume omitted an enemy")
	}
}
