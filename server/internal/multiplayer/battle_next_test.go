package multiplayer

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

func nextBattleFixture(t *testing.T) (*BattleEngine, []Member) {
	t.Helper()
	catalog := &CombatCatalog{
		Cards:            map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
		PlayerSkills:     map[int][]CombatSkillDefinition{1: {{ID: 1, FunctionID: 1, Cost: 1, Target: "ENEMY_ONE", Attribute: "FIRE"}}},
		PlayerSkillRoles: map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA", Target: "ENEMY_ONE", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}},
		EnemyParties:     map[int]CombatEnemyParty{1: {ID: 1, Slots: [4]CombatEnemyPartySlot{{EnemyID: 1, HPRate: 1}}}},
		Enemies:          map[int]CombatEnemyDefinition{1: {ID: 1, HP: 100, Attribute: "FIRE"}},
		EnemyLevels:      map[int]CombatEnemyLevel{1: {ID: 1, HPBars: 1, AttributeRates: [9]int{100, 100, 100, 100, 100, 100, 100, 100, 100}}},
	}
	members := make([]Member, 4)
	for i := range members {
		members[i] = Member{MemberType: i + 1, ArthurType: i + 1, UserID: 1001 + i, HP: 1000, Attack: 100}
		for slot := 1; slot <= 10; slot++ {
			members[i].DeckCards = append(members[i].DeckCards, BattleCard{CardType: slot, CardID: 1, Level: 1})
		}
	}
	engine, err := newBattleEngine(catalog, RoomSpec{EnemyPartyID: 1, CostInitial: 3, HoldMax: 2, Seed: 602}, members)
	if err != nil {
		t.Fatal(err)
	}
	return engine, members
}

func TestAwakeInheritsUnreleasedBodyDrops(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	waves := []release.TeamBattleReplayBattle{{EnemyPartyID: 1, EnemyType: 1}, {EnemyPartyID: 1, EnemyType: 4}, {EnemyPartyID: 1, EnemyType: 4}}
	plan := []release.TeamBattleEnemyDrop{
		{Reward: release.Reward{Type: 4, Num: 900}},
		{Reward: release.Reward{Type: 8, RewardTypeID: 4000, Num: 600}},
		{EnemyIndex: 1, Reward: release.Reward{Type: 13, RewardTypeID: 20000003, Num: 1}},
	}
	current := &room{engine: engine, battles: waves, dropPlan: plan}
	engine.phase, engine.endType = battlePhaseEnded, 4
	recordRoomWaveDrops(current) // Living first-phase body did not release anything.
	drops := roomSpecWaveDrops(RoomSpec{DropLedgerVersion: 1, DropPlan: plan, Battles: waves}, 1)
	if len(drops) != 2 {
		t.Fatalf("awake drops: %+v", drops)
	}
	next, err := engine.NextBattle(1, drops)
	if err != nil {
		t.Fatal(err)
	}
	next.enemies[0].HP = 0
	if rows := enemyBreakDropResults(&next.enemies[0]); len(rows) == 0 {
		t.Fatal("missing body drop presentation")
	}
	current.engine, current.battleIndex = next, 1
	recordRoomWaveDrops(current)
	recordRoomWaveDrops(current)
	if len(current.releasedDrops) != 2 || current.releasedDrops[0].BattleIndex != 1 || !roomBodyRewardAlreadyReleased(current, 2) {
		t.Fatalf("bad awake settlement ledger: %+v", current.releasedDrops)
	}
	if len(priorWaveResumeDrops(current.releasedDrops, 1)) != 0 || len(priorWaveResumeDrops(current.releasedDrops, 2)) != 2 {
		t.Fatal("resume duplicated or lost current-wave body drops")
	}
}

func TestNextBattleNativeStateAndRotation(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	engine.phase, engine.endType, engine.turn = battlePhaseEnded, 1, 7
	player := &engine.players[0]
	player.HP, player.Cost, player.Burst = 123, 8, 42
	player.DamageTaken = 877
	player.Hand = [5]int{1, 2, 3}
	player.DeckOrder, player.DrawIndex, player.DrawCount = [10]int{3, 4, 5, 6}, 0, 4
	player.Discard = []int{7, 8, 9}
	player.Spheres[0] = battleSphere{SphereID: 123, Count: 0, Maximum: 1, Remaining: 3, ChalicePlayable: true}
	engine.players[1].HP = 0 // KO in the winning action, not formally retired.
	before := *engine
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*engine, before) {
		t.Fatal("next wave mutated its predecessor")
	}
	got := next.players[0]
	if got.Spheres[0].ChalicePlayable {
		t.Fatal("previous wave chalice eligibility survived the native handover reset")
	}
	if got.HP != 123 || got.Burst != 42 || got.Cost != 3 || got.DamageTaken != 0 || got.Hand != player.Hand || next.turn != 0 || next.endType != 0 || next.phase != battlePhaseCreated || next.players[1].HP != 1 || next.players[1].GameOver || got.Spheres[0].Count != 1 || got.Spheres[0].Remaining != 0 {
		t.Fatal("native wave handover lost player state or failed to reset wave state")
	}
	seen := map[int]bool{}
	for _, slot := range got.Hand {
		if slot != 0 {
			seen[slot] = true
		}
	}
	for _, slot := range got.DeckOrder[:got.DrawCount] {
		if slot < 0 {
			continue
		}
		if seen[slot+1] {
			t.Fatal("card duplicated across hand and rotation pool")
		}
		seen[slot+1] = true
	}
	if len(seen) != 10 || len(got.Discard) != 0 || next.rng == newXorShift128(602) || next.rng == engine.rng {
		t.Fatal("rotation lost cards or RNG was reseeded instead of advancing")
	}
}

func TestMultiplayerWavesFinishOnceAndShareAllDrops(t *testing.T) {
	engine, members := nextBattleFixture(t)
	hub := NewHub()
	server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, State: RoomStateBattle, Members: members},
		engine: engine, gameStarted: true, hostCostPaid: true, dropLedgerVersion: 1, connections: map[int]*clientConn{}, gameStartFinished: map[int]bool{},
		turnPhaseFinished: map[int]bool{}, cardPlaySubmissions: map[int]cardPlaySubmission{}, userAttackFinished: map[int]bool{},
	}
	hub.rooms[123] = current
	for i := 0; i < 6; i++ {
		current.battles = append(current.battles, release.TeamBattleReplayBattle{EnemyPartyID: 1, EnemyType: 0})
		current.dropPlan = append(current.dropPlan, release.TeamBattleEnemyDrop{BattleIndex: i, EnemyIndex: 0, Reward: release.Reward{Type: 4, Num: i + 1, CardSkillLevels: []int16{}}})
	}
	if fields := splitCSV(roomCountdownPayload(current)); len(fields) != 37 || fields[0] != "6" {
		t.Fatal("countdown truncated multi-wave enemies")
	}
	outputs := make([]*hubCheckingConn, 2)
	for i := range outputs {
		left, right := net.Pipe()
		t.Cleanup(func() { left.Close(); right.Close() })
		outputs[i] = &hubCheckingConn{Conn: left, hub: hub}
		current.connections[i+1] = &clientConn{conn: outputs[i], server: server, roomID: 123, memberType: i + 1, userID: 1001 + i}
	}
	engine.enemies[0].Drops = roomSpecWaveDrops(RoomSpec{DropLedgerVersion: 1, DropPlan: current.dropPlan}, 0)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	for wave := 0; wave < 6; wave++ {
		for _, client := range current.connections {
			if err := client.handleGameStartFinish(); err != nil {
				t.Fatal(err)
			}
		}
		for _, client := range current.connections {
			if err := client.handleTurnPhaseFinish(); err != nil {
				t.Fatal(err)
			}
		}
		current.cardPlaySubmissions[1] = cardPlaySubmission{CardTypes: [5]int{current.engine.players[0].Hand[0]}, Targets: [5]int{5}}
		current.cardPlaySubmissions[2], current.cardPlaySubmissions[3], current.cardPlaySubmissions[4] = cardPlaySubmission{}, cardPlaySubmission{}, cardPlaySubmission{}
		if err := server.tryAdvanceCardPlay(123); err != nil {
			t.Fatal(err)
		}
		if current.engineBattleEnd != 1 {
			t.Fatalf("wave %d did not win: %d hp %d", wave, current.engineBattleEnd, current.engine.enemies[0].HP)
		}
		for _, client := range current.connections {
			if err := client.handleUserAttackFinish(); err != nil {
				t.Fatal(err)
			}
		}
		if wave < 5 {
			if _, err := hub.SettlementFor(123, 1001); !errors.Is(err, ErrCompletedBattlePending) || !current.nextBattlePending {
				t.Fatal("intermediate wave settled as a completed quest")
			}
			// A repeated old direction ACK must not emit another transition.
			if err := current.connections[1].handleUserAttackFinish(); err != nil {
				t.Fatal(err)
			}
			if err := current.connections[1].handleGameNextFinish(""); err != nil {
				t.Fatal(err)
			}
			if current.battleIndex != wave {
				t.Fatal("next wave started before the second client was ready")
			}
			if err := current.connections[2].handleGameNextFinish(""); err != nil {
				t.Fatal(err)
			}
			if err := current.connections[2].handleGameNextFinish(""); err != nil {
				t.Fatal(err)
			}
			if current.battleIndex != wave+1 || current.nextBattlePending {
				t.Fatal("next wave transition failed")
			}
		}
	}
	for _, id := range []int{1001, 1002} {
		completed, err := hub.SettlementFor(123, id)
		if err != nil || len(completed.ReleasedDrops) != 6 || completed.BattleIndex != 5 || completed.Progress != 5 || !completed.HostCostPaid {
			t.Fatalf("incorrect completed multi-wave ledger: %+v %v", completed, err)
		}
		for i, drop := range completed.ReleasedDrops {
			if drop.BattleIndex != i || drop.Reward.Num != i+1 || drop.Reward.CardSkillLevels == nil {
				t.Fatal("drop repeated, reordered or lost")
			}
		}
	}
	for _, output := range outputs {
		frames := output.output.String()
		if output.locked || strings.Count(frames, "GameNextStart{") != 5 || strings.Count(frames, "ApiGameEnd{") != 1 || strings.Count(frames, "GameClose{") != 1 {
			t.Fatalf("invalid wave output: %s", frames)
		}
		for i := 1; i < 6; i++ {
			if !strings.Contains(frames, "GameNextStart{\n"+strconv.Itoa(i)+","+strconv.Itoa(i)+"\n}") {
				t.Fatal("incorrect wave index/progress")
			}
		}
	}
}

func TestOptionalAwakeWaveSelection(t *testing.T) {
	current := &room{battles: []release.TeamBattleReplayBattle{{EnemyType: 1}, {EnemyType: 4}, {EnemyType: 0}}, engineBattleEnd: 1}
	if index, ok := roomNextBattle(current); !ok || index != 2 || roomBattleProgress(current, index) != 1 {
		t.Fatal("ordinary win must skip optional awake wave")
	}
	current.engineBattleEnd = 4
	if index, ok := roomNextBattle(current); !ok || index != 1 || roomBattleProgress(current, index) != 0 {
		t.Fatal("awake must not increment visible progress")
	}
	current.engineBattleEnd = 2
	if _, ok := roomNextBattle(current); ok {
		t.Fatal("defeat started another wave")
	}
}

func TestComebackBetweenWavesUsesActualIndexAndWaitsForScene(t *testing.T) {
	engine, members := nextBattleFixture(t)
	engine.phase, engine.endType, engine.turn = battlePhaseEnded, 1, 1
	hub := NewHub()
	server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	output := &hubCheckingConn{Conn: left, hub: hub}
	client := &clientConn{server: server, conn: output}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 123, BossID: 1, State: RoomStateBattle, Members: members},
		engine: engine, engineBattleEnd: 1, gameStarted: true, dropLedgerVersion: 1,
		battleIndex: 3, progress: 3, nextBattlePending: true, nextBattleIndex: 4,
		connections: map[int]*clientConn{}, disconnectedUntil: map[int]time.Time{1: time.Now().Add(time.Minute)},
		comebackTokens: map[int]string{1: "resume-token"}, gameNextFinished: map[int]bool{},
	}
	for i := 0; i < 6; i++ {
		current.battles = append(current.battles, release.TeamBattleReplayBattle{EnemyPartyID: 1})
	}
	hub.rooms[123] = current
	if err := client.handleComeback("1001,123,resume-token,0"); err != nil {
		t.Fatal(err)
	}
	if err := client.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	frames := output.output.String()
	if output.locked || !strings.Contains(frames, "RoomComeback{\n3,3,") || !strings.Contains(frames, "GameNextStart{\n4,4\n}") || strings.Contains(frames, "ApiGameStart{") || current.battleIndex != 3 {
		t.Fatalf("incorrect mid-wave comeback %s", frames)
	}
	stale := &clientConn{server: server, roomID: 123, memberType: 1}
	if err := stale.handleGameNextFinish(""); err == nil {
		t.Fatal("stale socket acknowledged new connection's scene")
	}
	if err := client.handleGameNextFinish(""); err != nil {
		t.Fatal(err)
	}
	if current.battleIndex != 4 || current.nextBattlePending || !strings.Contains(output.output.String(), "ApiGameStart{") {
		t.Fatal("resumed room failed to start its pending wave")
	}
}

func TestDefaultNativeTerminalRetainsBurstListsAndCleansKO(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	player := &engine.players[0]
	player.HP, player.BurstBreak, player.BurstRecharge[0] = 0, 2, 3
	for _, list := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		player.Effects = append(player.Effects, battleEffect{Function: "BURN", ListType: list, Remaining: 3})
	}
	rows := engine.appendTerminalBuffCleanup([]BattleResult{battleEndResult(1)})
	if len(player.Effects) != 3 || player.Effects[0].ListType != 2 || player.Effects[1].ListType != 5 || player.Effects[2].ListType != 6 || player.BurstBreak != 1 || player.BurstRecharge[0] != 2 {
		t.Fatal("default wave cleanup lost preserved lists/counters or retained KO debuffs")
	}
	if battleResultsContainCommand(rows, resultBuffPartition) || battleResultsContainCommand(rows, resultBuffLostOne) {
		t.Fatal("default terminal took the optional expiry branch")
	}
	engine.players[0].HP = 100
	engine.players[0].BurstState = burstGaugeBurst
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "ATK", "10"}}
	rows, err := engine.executeBurstRoleWithListType(battleAction{memberType: 1, target: 1, cardLevel: 1}, role, nil, 6)
	if err != nil {
		t.Fatal(err)
	}
	last := player.Effects[len(player.Effects)-1]
	if last.ListType != 6 || !last.BurstPassive {
		t.Fatal("burst passive lost its durable list identity")
	}
	found := false
	for _, row := range rows {
		if row.Command == resultPassiveBuff && row.Args[2] == 6 {
			found = true
		}
	}
	if !found {
		t.Fatal("burst passive projected as a normal buff")
	}
}

func TestAwakeningCarriesCurrentTurnCost(t *testing.T) {
	for _, turn := range []int{1, 4, 7, 12} {
		engine, _ := nextBattleFixture(t)
		engine.phase, engine.endType, engine.turn = battlePhaseEnded, 4, turn
		next, err := engine.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = next.Start(); err != nil {
			t.Fatal(err)
		}
		if _, err = next.TurnPhase(); err != nil {
			t.Fatal(err)
		}
		want := minInt(10, 3+turn-1)
		if next.turnCost() != want || next.players[0].Cost != want {
			t.Fatalf("turn %d: cost %d, want %d", turn, next.players[0].Cost, want)
		}
		// Starting the second post-awakening turn advances the cost normally.
		next.turn++
		if next.turnCost() != minInt(10, want+1) {
			t.Fatal("post-awakening cost did not resume normal growth")
		}
		// A further awakening at that turn must also inherit its current cost.
		next.phase, next.endType = battlePhaseEnded, 4
		again, err := next.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if again.turnCost() != next.turnCost() {
			t.Fatal("repeated awakening changed current cost")
		}
	}
}

func TestAwakeningPreservesFirstDrawCycle(t *testing.T) {
	for _, endType := range []int{1, 4} {
		engine, _ := nextBattleFixture(t)
		if _, err := engine.Start(); err != nil {
			t.Fatal(err)
		}
		player := &engine.players[0]
		seen := map[int]bool{}
		// Five cards reached the hand, and two were used before the transition.
		for i := 0; i < 5; i++ {
			slot := engine.drawCard(player)
			seen[slot] = true
			if i < 2 {
				player.Discard = append(player.Discard, slot-1)
			} else {
				player.Hand[i-2] = slot
			}
		}
		engine.phase, engine.endType, engine.turn = battlePhaseEnded, endType, 2
		before := *player
		rng := engine.rng
		next, err := engine.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := &next.players[0]
		if p.Hand != before.Hand {
			t.Fatal("transition replaced retained hand")
		}
		if endType == 1 {
			if len(p.Discard) != 0 || p.remainingDeckCount() != 7 {
				t.Fatal("ordinary wave must still recycle used cards")
			}
			continue
		}
		if p.DeckOrder != before.DeckOrder || p.DrawIndex != before.DrawIndex || p.DrawCount != before.DrawCount || next.rng != rng {
			t.Fatal("awakening reset the draw order or consumed shuffle RNG")
		}
		for i := 0; i < 5; i++ {
			slot := next.drawCard(p)
			if slot == 0 || seen[slot] {
				t.Fatalf("awakening repeated slot %d before all ten cards appeared", slot)
			}
			seen[slot] = true
		}
		if len(seen) != 10 || len(p.Discard) != 2 {
			t.Fatal("awakening lost unused cards or discarded cards")
		}
		if slot := next.drawCard(p); slot != before.Discard[0]+1 && slot != before.Discard[1]+1 {
			t.Fatal("depleted draw pool did not recycle the discarded cards")
		}
		if player.DeckOrder != before.DeckOrder || len(player.Discard) != 2 {
			t.Fatal("advancing the next stage mutated the preceding engine")
		}
	}
}
