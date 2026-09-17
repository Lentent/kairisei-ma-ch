package multiplayer

import (
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
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
	if engine.phase != battlePhaseEnemy || engine.rng != before || engine.players[1].Spheres[0].Count != 1 || engine.players[1].ReservedChalice != 1 {
		t.Fatal("reservation prematurely executed or advanced the battle")
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
		battles: []release.TeamBattleReplayBattle{{EnemyPartyID: 1}, {EnemyPartyID: 1}}}
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
			before := engine.rng
			for _, slot := range []string{"1", "0", "1"} {
				if err := owner.handle("ChaliceSphrReserve", slot); err != nil {
					t.Fatal("click while terminal results are still animating rejected", err)
				}
			}
			for _, out := range []*hubCheckingConn{ownerOut, guestOut} {
				if out.locked || strings.Count(out.output.String(), "318,1,0") != 3 || strings.Contains(out.output.String(), "Disconnect") {
					t.Fatal("late click did not acknowledge empty reservation to both clients", out.output.String())
				}
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
