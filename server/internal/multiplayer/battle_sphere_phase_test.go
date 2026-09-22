package multiplayer

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestEnemyAnimationChaliceReservationBroadcast(t *testing.T) {
	hub := NewHub()
	s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ownerOut, guestOut := &hubCheckingConn{Conn: left, hub: hub}, &hubCheckingConn{Conn: left, hub: hub}
	owner := &clientConn{server: s, conn: ownerOut, roomID: 1, memberType: 1}
	guest := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 2}
	engine := newSphereContractEngine(&CombatCatalog{})
	engine.phase = battlePhaseEnemy
	engine.players[1].Spheres[0] = battleSphere{Slot: 1, SphereID: 16000030, Type: sphereTypeChalice, Count: 1, ChalicePlayable: true}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, engine: engine,
		enemyPhaseStarted: true, connections: map[int]*clientConn{1: owner, 2: guest}}
	hub.rooms[1] = current
	current.openEnemyChaliceInput()
	before := engine.rng
	for _, slot := range []string{"1", "0", "1"} {
		if err := guest.handle("ChaliceSphrReserve", slot); err != nil {
			t.Fatal("legal enemy-animation click must not reach connection rejection", err)
		}
	}
	for _, out := range []*hubCheckingConn{ownerOut, guestOut} {
		frames := out.output.String()
		if out.locked || strings.Count(frames, "ApiChaliceSphrReserve{") != 3 ||
			!strings.Contains(frames, "318,2,0") || strings.Contains(frames, "Disconnect") {
			t.Fatalf("reservation/cancellation did not reach both players outside the lock: %s", frames)
		}
	}
	if engine.phase != battlePhaseEnemy || engine.rng != before || engine.players[1].Spheres[0].Count != 1 || engine.players[1].ReservedChalice != 0 || current.chaliceInput.reserved[1] != 1 {
		t.Fatal("reservation prematurely executed or advanced the battle")
	}
	if rows := current.chaliceResumeResults(nil); len(rows) != 4 || !equalBattleArgs(rows[1].Args, []int64{2, 1}) {
		t.Fatal("comeback lost the broadcast reservation")
	}
	current.enemyPhaseFinished = map[int]bool{2: true}
	if err := guest.handle("ChaliceSphrReserve", "0"); err != nil || current.chaliceInput.reserved[1] != 1 {
		t.Fatal("input after the member's native Finish changed its closed reservation", err)
	}
	if err := current.commitChaliceInput(); err != nil || engine.players[1].ReservedChalice != 1 {
		t.Fatal("reservation was not committed at the execution boundary", err)
	}
	// The native cleanup rejects a new unavailable summon, but still accepts
	// slot zero. Preserve that distinction instead of bypassing eligibility.
	engine.phase = battlePhaseChaliceEnemy
	engine.players[1].Spheres[0].ChalicePlayable = false
	if _, err := engine.ReserveChaliceSphere(2, 1); err == nil {
		t.Fatal("unavailable summon accepted after enemy cleanup")
	}
	if err := guest.handle("ChaliceSphrReserve", "1"); err != nil || engine.players[1].ReservedChalice != 1 {
		t.Fatal("stale click disconnected or changed the existing reservation", err)
	}
	if err := guest.handle("ChaliceSphrReserve", "0"); err != nil {
		t.Fatal(err)
	}
}

func TestChaliceEnemyVictoryAdvancesWaveAfterAnimation(t *testing.T) {
	engine, members := nextBattleFixture(t)
	engine.phase, engine.endType = battlePhaseEnded, 1
	hub := NewHub()
	s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	out := &hubCheckingConn{Conn: left, hub: hub}
	client := &clientConn{server: s, conn: out, roomID: 1, memberType: 1}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle, Members: members},
		engine: engine, engineBattleEnd: 1, chaliceEnemyStarted: true,
		connections: map[int]*clientConn{1: client}, chaliceEnemyFinished: map[int]bool{},
		battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1}, {EnemyPartyID: 1}}}
	hub.rooms[1] = current
	if err := s.tryAdvanceChaliceEnemy(1); err != nil || current.nextBattlePending {
		t.Fatal("victory advanced before the animation finished", err)
	}
	current.chaliceEnemyFinished[1] = true
	if err := s.tryAdvanceChaliceEnemy(1); err != nil {
		t.Fatal(err)
	}
	if !current.nextBattlePending || !strings.Contains(out.output.String(), "GameNextStart{") || strings.Contains(out.output.String(), "ApiTurnPhase{") || out.locked {
		t.Fatal("summon victory attempted another turn instead of the existing wave-completion flow", out.output.String())
	}
}

func TestTerminalAnimationChaliceClickDoesNotDisconnect(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		endType     int
		phase       battlePhase
		nextPending bool
		battleIndex int
	}{
		{"ongoing-animation", 0, battlePhaseUserAttack, false, 0},
		{"win-animation", 1, battlePhaseEnded, false, 0},
		{"loss-animation", 2, battlePhaseEnded, false, 0},
		{"awake-animation", 4, battlePhaseEnded, false, 0},
		{"win-next-barrier", 1, battlePhaseEnded, true, 0},
		{"awake-next-barrier", 4, battlePhaseEnded, true, 0},
		{"awake-next-start", 0, battlePhaseStarted, false, 1},
		{"awake-next-turn", 0, battlePhaseTurn, false, 1},
		{"awake-next-user-stale-click", 0, battlePhaseUser, false, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			hub := NewHub()
			s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			ownerOut, guestOut := &hubCheckingConn{Conn: left, hub: hub}, &hubCheckingConn{Conn: left, hub: hub}
			owner := &clientConn{server: s, conn: ownerOut, roomID: 1, memberType: 1}
			guest := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 2}
			engine := newSphereContractEngine(&CombatCatalog{})
			engine.phase, engine.endType = scenario.phase, scenario.endType
			engine.players[0].Spheres[0] = battleSphere{Slot: 1, SphereID: 16000030, Type: sphereTypeChalice, Count: 1}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, engine: engine,
				engineBattleEnd: scenario.endType, nextBattlePending: scenario.nextPending, battleIndex: scenario.battleIndex,
				userAttackStarted: true, connections: map[int]*clientConn{1: owner, 2: guest}}
			hub.rooms[1] = current
			animating := !scenario.nextPending && scenario.battleIndex == 0
			if animating {
				current.openUserChaliceInput([]BattleResult{{Command: resultChaliceSpherePlayable, Args: []int64{1, 1}}})
			}
			before := engine.rng
			for _, slot := range []string{"1", "0", "1"} {
				if err := owner.handle("ChaliceSphrReserve", slot); err != nil {
					t.Fatal("click while terminal results are still animating rejected", err)
				}
			}
			for _, out := range []*hubCheckingConn{ownerOut, guestOut} {
				wantReserved := 0
				if animating {
					wantReserved = 2
				}
				if out.locked || strings.Count(out.output.String(), "318,1,1") != wantReserved || strings.Count(out.output.String(), "318,1,0") != 3-wantReserved || strings.Contains(out.output.String(), "Disconnect") {
					t.Fatal("published reservation feedback depended on an unanimated battle outcome", out.output.String())
				}
			}
			if err := owner.handle("Chat", "17"); err != nil || !strings.Contains(guestOut.output.String(), "MemberChat{\n1,17") {
				t.Fatal("battle outcome suppressed chat while the native connection was active", err)
			}
			if engine.rng != before || engine.phase != scenario.phase || engine.players[0].ReservedChalice != 0 || engine.players[0].Spheres[0].Count != 1 || current.nextBattlePending != scenario.nextPending {
				t.Fatal("late reservation mutated settled battle state or advanced the animation barrier")
			}
			owner.comebackPending = true
			if err := owner.handle("ChaliceSphrReserve", "1"); err == nil {
				t.Fatal("terminal handling bypassed member authentication")
			}
		})
	}
}

func TestCompletedBattleInteraction(t *testing.T) {
	engine, members := nextBattleFixture(t)
	engine.phase, engine.endType = battlePhaseEnded, 1
	hub := NewHub()
	repository := &terminalCompletionRepository{}
	hub.repository = repository
	s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	ownerOut, guestOut := &hubCheckingConn{Conn: left, hub: hub}, &hubCheckingConn{Conn: left, hub: hub}
	owner := &clientConn{server: s, conn: ownerOut, roomID: 1, memberType: 1, userID: 1001}
	guest := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 2, userID: 1002}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle, OwnerMemberType: 1, Members: members},
		engine: engine, engineBattleEnd: 1, connections: map[int]*clientConn{1: owner, 2: guest}}
	hub.rooms[1] = current
	session := hub.lockRoomSession(current.RoomID)
	if err := s.completeGoBattleLocked(session, current); err != nil {
		t.Fatal(err)
	}
	before, err := hub.SettlementFor(1, 1001)
	if err != nil {
		t.Fatal(err)
	}
	rng := engine.rng
	for _, request := range []battleFrame{{"Chat", "17"}, {"ChatNew", "18"}, {"ChaliceSphrReserve", "1"}, {"ChaliceSphrReserve", "0"}, {"ChaliceSphrSkip", ""}} {
		if err := owner.handle(request.method, request.payload); err != nil {
			t.Fatal("legitimate final-direction interaction rejected", request.method, err)
		}
	}
	frames := guestOut.output.String()
	if !strings.Contains(frames, "MemberChat{\n1,17") || !strings.Contains(frames, "MemberChatNew{\n1,18") ||
		strings.Index(frames, "GameClose{") > strings.Index(frames, "MemberChat{") ||
		strings.Contains(ownerOut.output.String(), "MemberChat{") || strings.Contains(frames, "ChaliceSphrSkipExec{") ||
		strings.Contains(frames, "ApiChaliceSphrReserve{") || guestOut.locked || ownerOut.locked {
		t.Fatal("final-direction chat did not reach the peer in order", frames)
	}
	if err := guest.handle("ChaliceSphrSkip", ""); err == nil {
		t.Fatal("guest skipped the owner's movie")
	}
	impostor := &clientConn{server: s, conn: guestOut, roomID: 1, memberType: 1, userID: 1001}
	if err := impostor.handle("Chat", "17"); err == nil {
		t.Fatal("stale socket impersonated a completed member")
	}
	if err := owner.handle("ChaliceSphrReserve", "100"); err == nil {
		t.Fatal("completion accepted malformed input")
	}
	if err := hub.MarkSettlementClaimed(1, 1001); err != nil {
		t.Fatal(err)
	}
	if err := guest.handle("Chat", "19"); err != nil {
		t.Fatal("one player's settlement closed the other player's direction", err)
	}
	after, err := hub.SettlementFor(1, 1002)
	if err != nil || !reflect.DeepEqual(before, after) || repository.saves != 1 || engine.rng != rng || hub.rooms[1] != nil {
		t.Fatal("interaction reopened combat or modified immutable settlement", err)
	}
	deadline := owner.finishingDeadline()
	if !owner.readDeadline().Equal(deadline) {
		t.Fatal("ping could extend the fixed finishing lifetime")
	}
	owner.setFinishingDeadline(time.Now().Add(-time.Second))
	if err := owner.handle("Chat", "20"); err == nil {
		t.Fatal("expired interaction identity was accepted")
	}
	if err := guest.close(true); err != nil || hub.completed[1].comebackConnections[2] != nil {
		t.Fatal("final direction close retained a live identity", err)
	}
}

func TestChaliceReservationExecutionBoundary(t *testing.T) {
	for _, phase := range []battlePhase{battlePhaseUserAttack, battlePhaseEnemy} {
		for _, action := range []string{"reserve", "cancel", "unavailable", "dead"} {
			t.Run(fmt.Sprintf("%d/%s", phase, action), func(t *testing.T) {
				engine, _ := nextBattleFixture(t)
				engine.phase = phase
				engine.catalog.Spheres = map[int]CombatSphereDefinition{16000030: {ID: 16000030, SkillID: 1, Type: sphereTypeChalice}}
				engine.players[0].Spheres[0] = battleSphere{Slot: 1, SphereID: 16000030, Type: sphereTypeChalice, Level: 1, Count: 1, ChalicePlayable: true}
				hub := NewHub()
				s := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
				left, right := net.Pipe()
				t.Cleanup(func() { left.Close(); right.Close() })
				out := &hubCheckingConn{Conn: left, hub: hub}
				owner := &clientConn{server: s, conn: out, roomID: 1, memberType: 1}
				current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, engine: engine,
					userAttackStarted: true, enemyPhaseStarted: phase == battlePhaseEnemy,
					connections: map[int]*clientConn{1: owner}, userAttackFinished: map[int]bool{}, enemyPhaseFinished: map[int]bool{}}
				hub.rooms[1] = current
				advance, finish, method := s.tryAdvanceUserAttack, "UserAttackFinish", "ApiChaliceSphrExecUserPhase{"
				if phase == battlePhaseEnemy {
					current.openEnemyChaliceInput()
					advance, finish, method = s.tryAdvanceEnemyPhase, "EnemyPhaseFinish", "ApiChaliceSphrExecEnemyPhase{"
				} else {
					current.openUserChaliceInput([]BattleResult{{Command: resultChaliceSpherePlayable, Args: []int64{1, 1}}})
				}
				if action == "unavailable" {
					engine.players[0].Spheres[0].ChalicePlayable = false
				} else if action == "dead" {
					engine.players[0].HP = 0
				}
				if err := owner.handle("ChaliceSphrReserve", "1"); err != nil {
					t.Fatal(err)
				}
				if action == "cancel" {
					if err := owner.handle("ChaliceSphrReserve", "0"); err != nil {
						t.Fatal(err)
					}
				}
				if err := advance(1); err != nil || engine.players[0].Spheres[0].Count != 1 || strings.Contains(out.output.String(), method) {
					t.Fatal("reservation crossed an unfinished direction barrier", err)
				}
				if err := owner.handle(finish, ""); err != nil {
					t.Fatal(err)
				}
				if err := advance(1); err != nil {
					t.Fatal(err)
				}
				wantCount := 1
				if action == "reserve" {
					wantCount = 0
				}
				if engine.players[0].Spheres[0].Count != wantCount || engine.players[0].ReservedChalice != 0 || strings.Count(out.output.String(), method) != 1 {
					t.Fatal("reservation executed incorrectly or more than once", out.output.String())
				}
			})
		}
	}
}
