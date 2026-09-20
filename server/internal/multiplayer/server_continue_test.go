package multiplayer

import (
	"errors"
	"fmt"
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
		userAttackStarted: true, chaliceUserStarted: true, enemyPhaseStarted: true, enemyPhaseFinished: map[int]bool{1: true, 2: true},
		chaliceEnemyFinished: make(map[int]bool)}
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
	for _, peer := range []*clientConn{recovered, guest} {
		if err := peer.handleChaliceSphrExecEnemyPhaseFinish(); err != nil {
			t.Fatal(err)
		}
	}
	for _, peer := range []*clientConn{recovered, guest} {
		if err := peer.handleTurnPhaseFinish(); err != nil {
			t.Fatal(err)
		}
	}
	if _, submitted := current.cardPlaySubmissions[1]; submitted {
		t.Fatal("revived connected member was automatically skipped")
	}
	for _, peer := range []*clientConn{recovered, guest} {
		if strings.Contains(peer.conn.(*hubCheckingConn).output.String(), "28,1\n") {
			t.Fatal("revived member received a stale KO pass confirmation")
		}
	}
	if err := guest.handleCardPlay(strings.Repeat("0,", 11)+"0", false); err != nil {
		t.Fatal(err)
	}
	if current.userAttackStarted {
		t.Fatal("room did not wait for the revived member's cards")
	}
	card := current.engine.players[0].Hand[0]
	if card == 0 {
		t.Fatal("revived member has no selectable card")
	}
	if err := recovered.handleCardPlay(fmt.Sprintf("%d,5,0,0,0,0,0,0,0,0,0,0", card), false); err != nil {
		t.Fatal(err)
	}
	if !current.userAttackStarted {
		t.Fatal("revived member's submission did not release the input barrier")
	}
}

func TestContinueDeclineAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wipe    bool
		onlyAI  bool
		canPlay bool
	}{
		{name: "survivors", canPlay: true},
		{name: "wipe", wipe: true},
		{name: "only_ai_survivor", onlyAI: true, canPlay: true},
		{name: "only_ai_pass", onlyAI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, current := continueRoomFixture(t, func(BattleContinue) (ContinueBalance, error) {
				t.Fatal("decline charged currency")
				return ContinueBalance{}, nil
			}, tc.wipe)
			owner, guest := current.connections[1], current.connections[2]
			peers := []*clientConn{owner, guest}
			if tc.onlyAI {
				// Both humans and one CPU are KO; only CPU slot 4 is alive.
				current.engine.players[1].HP = 0
				current.engine.players[2].HP = 0
				if !tc.canPlay {
					current.engine.catalog.PlayerSkills[1][0].Cost = 99
				}
			}
			if err := owner.handleContinuePhaseFinish(""); err != nil {
				t.Fatal(err)
			}
			if current.engine.players[0].GameOver {
				t.Fatal("early decline retired party before others could revive it")
			}
			current.continuation.deadline = time.Time{}
			if err := s.advanceContinue(1); err != nil {
				t.Fatal(err)
			}
			if !current.engine.players[0].GameOver || current.engine.continuePending || current.engine.continueCount != 0 {
				t.Fatal("cancel did not retire KO members")
			}
			for _, peer := range peers {
				if err := peer.handleGameOverPhaseFinish(""); err != nil {
					t.Fatal(err)
				}
			}
			if tc.wipe {
				if current.engine.EndType() != 2 || !strings.Contains(guest.conn.(*hubCheckingConn).output.String(), "ApiGameEnd{") {
					t.Fatal("wipe cancellation did not end cleanly")
				}
				return
			}
			if !current.chaliceEnemyStarted {
				t.Fatal("survivors did not resume")
			}
			current.engine.enemies[0].HP = 1000000
			current.engine.enemies[0].MaxHP = 1000000
			current.engine.enemies[0].BaseMaxHP = 1000000
			for turn := 0; turn < 2; turn++ {
				for _, peer := range peers {
					peer.conn.(*hubCheckingConn).output.Reset()
				}
				beforeHP := current.engine.enemies[0].HP
				for _, peer := range peers {
					if err := peer.handleChaliceSphrExecEnemyPhaseFinish(); err != nil {
						t.Fatal(err)
					}
				}
				for _, peer := range peers {
					if err := peer.handleTurnPhaseFinish(); err != nil {
						t.Fatal(err)
					}
				}
				if _, submitted := current.cardPlaySubmissions[1]; submitted || current.userAttackStarted {
					t.Fatal("connected KO human bypassed original client input")
				}
				selection, submitted := current.engine.selectedPlays[4]
				if !submitted || (selectedCardCount(selection) > 0) != tc.canPlay || current.engine.enemies[0].HP != beforeHP {
					t.Fatal("CPU input was not confirmed while waiting for human input")
				}
				beforeRNG := current.engine.rng
				beforeFrames := guest.conn.(*hubCheckingConn).output.String()
				if err := s.submitAutomaticRoomCards(1); err != nil {
					t.Fatal(err)
				}
				if current.engine.rng != beforeRNG || guest.conn.(*hubCheckingConn).output.String() != beforeFrames {
					t.Fatal("automatic retry changed confirmed CPU input")
				}
				// Both KO clients skip manually on the first AI-only turn and
				// use their original CardPlayTimeup requests on the second.
				if err := guest.handleCardPlay(strings.Repeat("0,", 11)+"0", tc.onlyAI && turn == 1); err != nil {
					t.Fatal(err)
				}
				if current.userAttackStarted {
					t.Fatal("room bypassed the remaining connected KO client's input")
				}
				if err := owner.handleCardPlay(strings.Repeat("0,", 11)+"0", turn == 1); err != nil {
					t.Fatal(err)
				}
				if !current.userAttackStarted || (current.engine.enemies[0].HP < beforeHP) != tc.canPlay {
					t.Fatal("client skip/timeout did not release the CPU attack")
				}
				for _, peer := range peers {
					frames := peer.conn.(*hubCheckingConn).output.String()
					input, confirm, attack := strings.Index(frames, "ApiUserPhase{"), strings.Index(frames, "ApiCardPlayR{"), strings.Index(frames, "ApiUserAttack{")
					if input < 0 || confirm <= input || attack <= confirm || strings.Count(frames, "ApiUserAttack{") != 1 {
						t.Fatal("input, CPU confirmation and attack are missing or out of order", frames)
					}
					for slot := 1; slot <= maxRoomMembers; slot++ {
						if current.engine.players[slot-1].HP <= 0 && strings.Contains(frames, fmt.Sprintf("28,%d\n", slot)) {
							t.Fatal("KO member received a synthetic pass notification")
						}
					}
					beforeRNG = current.engine.rng
					if err := peer.handleCardPlay(strings.Repeat("0,", 11)+"0", true); err != nil {
						t.Fatal(err)
					}
					if err := s.tryAdvanceCardPlay(1); err != nil {
						t.Fatal(err)
					}
					if current.engine.rng != beforeRNG || peer.conn.(*hubCheckingConn).output.String() != frames {
						t.Fatal("late timeout replayed the attack")
					}
				}
				if turn == 0 {
					for _, finish := range []func(*clientConn) error{
						(*clientConn).handleUserAttackFinish,
						(*clientConn).handleChaliceSphrExecUserPhaseFinish,
						(*clientConn).handleEnemyPhaseFinish,
					} {
						for _, peer := range peers {
							if err := finish(peer); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		})
	}
}
