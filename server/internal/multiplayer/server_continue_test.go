package multiplayer

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func continueRoomFixture(t *testing.T, authorize func(BattleContinue) (ContinueBalance, error), allDead bool) (*Server, *room) {
	t.Helper()
	h := NewHub()
	if err := h.AttachContinueAuthorizer(authorize); err != nil {
		t.Fatal(err)
	}
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	engine, members := nextBattleFixture(t)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	engine.continueAllowed, engine.turn, engine.phase = true, 2, battlePhaseChaliceUser
	for i := range engine.players {
		engine.players[i].HP = 100
		if i == 0 || allDead {
			engine.players[i].HP = 0
		}
	}
	if _, err := engine.EnemyPhase(); err != nil {
		t.Fatal(err)
	}
	current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, State: RoomStateBattle, OwnerMemberType: 1, Members: members},
		continueAllowed: true, engine: engine, connections: make(map[int]*clientConn), comebackTokens: map[int]string{1: "token1", 2: "token2"},
		disconnectedUntil: make(map[int]time.Time), gameStarted: true, turnPhaseStarted: true, userPhaseStarted: true,
		userAttackStarted: true, chaliceUserStarted: true, enemyPhaseStarted: true, enemyPhaseFinished: map[int]bool{1: true, 2: true}}
	for slot := 1; slot <= 2; slot++ {
		current.connections[slot] = continueTestPeer(t, s, slot)
	}
	h.rooms[1] = current
	if err := s.tryAdvanceEnemyPhase(1); err != nil {
		t.Fatal(err)
	}
	if !engine.continuePending || current.continuation == nil || engine.EndType() != 0 || current.chaliceEnemyStarted {
		t.Fatal("KO did not pause the room before the next phase")
	}
	t.Cleanup(func() { current.continuationTimerStopForTest() })
	return s, current
}

func (current *room) continuationTimerStopForTest() {
	if current.continuation != nil {
		current.continuation.timer.Stop()
	}
}

func continueTestPeer(t *testing.T, s *Server, slot int) *clientConn {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	return &clientConn{server: s, conn: &hubCheckingConn{Conn: left, hub: s.hub}, roomID: 1, memberType: slot, userID: 1000 + slot}
}

func TestContinuePaymentQuorumRetryAndReconnect(t *testing.T) {
	var calls atomic.Int32
	var reject atomic.Bool
	reject.Store(true)
	entered, releasePayment := make(chan struct{}), make(chan struct{})
	var s *Server
	s, current := continueRoomFixture(t, func(request BattleContinue) (ContinueBalance, error) {
		if _, ok := s.hub.Snapshot(request.RoomID); !ok {
			return ContinueBalance{}, errors.New("room vanished")
		}
		calls.Add(1)
		if reject.Load() {
			return ContinueBalance{}, errors.New("disk unavailable")
		}
		close(entered)
		<-releasePayment
		return ContinueBalance{Coin: 5, CoinFree: 7}, nil
	}, false)
	owner, guest := current.connections[1], current.connections[2]
	random := current.engine.rng
	if err := owner.handleContinue(""); err != nil {
		t.Fatal(err)
	}
	if current.engine.players[0].HP != 0 || current.engine.continueCount != 0 || current.engine.rng != random {
		t.Fatal("rejected debit changed the battle")
	}
	reject.Store(false)
	done := make(chan error, 1)
	go func() { done <- owner.handleContinue("") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("debit called under Hub lock")
	}
	if err := guest.handleContinue(""); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || current.engine.players[0].HP != 0 {
		t.Fatal("concurrent payer charged or revived before commit")
	}
	close(releasePayment)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if current.engine.continueCount != 1 || current.engine.rng != random || current.chaliceEnemyStarted {
		t.Fatal("revival advanced the phase/RNG before animation ACK")
	}
	for _, p := range current.engine.players {
		if p.HP != p.MaxHP {
			t.Fatal("party not revived")
		}
	}
	if strings.Contains(guest.conn.(*hubCheckingConn).output.String(), "ContinueResult{") {
		t.Fatal("payer currency leaked to a teammate")
	}
	if err := owner.close(true); err != nil {
		t.Fatal(err)
	}
	recovered := continueTestPeer(t, s, 1)
	recovered.roomID, recovered.memberType, recovered.userID = 0, 0, 0
	if err := recovered.handleComeback("1001,1,token1,0"); err != nil {
		t.Fatal(err)
	}
	if err := recovered.handleReadyToComeback(""); err != nil {
		t.Fatal(err)
	}
	if current.chaliceEnemyStarted || current.engine.rng != random {
		t.Fatal("reconnect bypassed continuation ACK or replayed gameplay")
	}
	if err := recovered.handleGameOverPhaseFinish(""); err != nil {
		t.Fatal(err)
	}
	if current.chaliceEnemyStarted {
		t.Fatal("one animation ACK advanced two-player room")
	}
	if err := guest.handleGameOverPhaseFinish(""); err != nil {
		t.Fatal(err)
	}
	if current.continuation != nil || !current.chaliceEnemyStarted || current.engine.turn != 2 || current.engine.rng != random {
		t.Fatalf("continuation resume: pending=%v chalice=%v turn=%d phase=%d rng=%+v want=%+v", current.continuation != nil, current.chaliceEnemyStarted, current.engine.turn, current.engine.phase, current.engine.rng, random)
	}
	output := guest.conn.(*hubCheckingConn).output.String()
	if !(strings.Index(output, "ApiContinue{") < strings.Index(output, "RoomComeback{") && strings.Index(output, "RoomComeback{") < strings.Index(output, "ApiChaliceSphrExecEnemyPhase{")) {
		t.Fatal("revival, reset snapshot and next phase are out of order")
	}
}

func TestContinueDeclineAndTimeout(t *testing.T) {
	for _, allDead := range []bool{false, true} {
		t.Run(map[bool]string{false: "survivors", true: "wipe"}[allDead], func(t *testing.T) {
			_, current := continueRoomFixture(t, func(BattleContinue) (ContinueBalance, error) {
				t.Fatal("decline charged currency")
				return ContinueBalance{}, nil
			}, allDead)
			owner, guest := current.connections[1], current.connections[2]
			if err := owner.handleContinuePhaseFinish(""); err != nil {
				t.Fatal(err)
			}
			if current.engine.players[0].GameOver {
				t.Fatal("early decline retired party before others could revive it")
			}
			current.continuation.deadline = time.Time{}
			if err := owner.server.advanceContinue(1); err != nil {
				t.Fatal(err)
			}
			if !current.engine.players[0].GameOver || current.engine.continuePending || current.engine.continueCount != 0 {
				t.Fatal("cancel did not retire KO members")
			}
			for _, peer := range []*clientConn{owner, guest} {
				if err := peer.handleGameOverPhaseFinish(""); err != nil {
					t.Fatal(err)
				}
			}
			if allDead {
				if current.engine.EndType() != 2 || !strings.Contains(guest.conn.(*hubCheckingConn).output.String(), "ApiGameEnd{") {
					t.Fatal("wipe cancellation did not end cleanly")
				}
			} else if !current.chaliceEnemyStarted {
				t.Fatal("survivors did not resume")
			}
		})
	}
}
