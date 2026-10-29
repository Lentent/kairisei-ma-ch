package multiplayer

import (
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func createExpiryTestRoom(t *testing.T, autoStart bool) (*Server, *room) {
	t.Helper()
	_, members := nextBattleFixture(t)
	for i := range members {
		members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
		members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
		members[i].IsRoomLoading = 1
	}
	h := NewHub()
	s, err := NewServer(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[0],
		OwnerFallbackParty: members[1:], AutoStart: autoStart}
	credential, err := h.IssueCreate(spec)
	if err != nil {
		t.Fatal(err)
	}
	owner := &clientConn{server: s, conn: &sessionTestConn{}}
	method := "RoomCreateRequestResult"
	if autoStart {
		method = "AIRoomCreateRequestResult"
	}
	if err := owner.handleCreateRequest(joinCSV(strconv.Itoa(spec.Owner.UserID), "1", "0", "", "1", "0", "0", "0", "0",
		credential.AuthToken, credential.Signature, "android"), method, autoStart); err != nil {
		t.Fatal(err)
	}
	current := h.rooms[owner.roomID]
	t.Cleanup(func() {
		session := h.lockRoomSession(current.RoomID)
		if session.room != nil {
			h.removeRoom(session.room)
		}
		session.Unlock()
		_ = s.Close()
	})
	if !autoStart {
		guestMember := members[1]
		if _, err := h.Reserve(current.RoomID, guestMember.UserID, guestMember.ArthurType); err != nil {
			t.Fatal(err)
		}
		credential, err := h.IssueEnter(current.RoomID, guestMember)
		if err != nil {
			t.Fatal(err)
		}
		guest := &clientConn{server: s, conn: &sessionTestConn{}}
		if err := guest.handleEnter(joinCSV(strconv.Itoa(guestMember.UserID), strconv.FormatInt(current.RoomID, 10), "", "2",
			credential.AuthToken, credential.Signature, "android")); err != nil {
			t.Fatal(err)
		}
	}
	return s, current
}

func TestWaitingRoomExpiresAtNineMinutes(t *testing.T) {
	for _, countdown := range []bool{false, true} {
		t.Run(strconv.FormatBool(countdown), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, current := createExpiryTestRoom(t, false)
				peers := roomConnections(current)
				if countdown {
					// A countdown still checking account eligibility is also waiting.
					current.State = RoomStateCountdown
				}
				time.Sleep(9*time.Minute - time.Nanosecond)
				synctest.Wait()
				if _, exists := s.hub.Snapshot(current.RoomID); !exists {
					t.Fatal("room expired before nine minutes")
				}
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				if _, exists := s.hub.Snapshot(current.RoomID); exists || len(s.hub.List(RoomSearch{})) != 0 {
					t.Fatal("expired room remains available")
				}
				if _, err := s.hub.Reserve(current.RoomID, 9003, 3); !errors.Is(err, ErrRoomUnavailable) {
					t.Fatal("expired room accepted a reservation", err)
				}
				for _, peer := range peers {
					if !peer.closed || strings.Count(peer.conn.(*sessionTestConn).output.String(), "RoomDelete{\n}\n") != 1 {
						t.Fatal("owner or guest missed the dissolution callback or stayed connected")
					}
				}
				if current.waitingTimer != nil || !current.waitingExpiresAt.IsZero() || len(s.hub.completed) != 0 {
					t.Fatal("expired lobby retained a timer or published a battle reward")
				}
			})
		})
	}
}

func TestCancelledCountdownKeepsOriginalWaitingDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, current := createExpiryTestRoom(t, false)
		deadline := current.waitingExpiresAt
		time.Sleep(8 * time.Minute)
		owner := current.connections[current.OwnerMemberType]
		if err := owner.handleCountdownStart(); err != nil {
			t.Fatal(err)
		}
		if current.State != RoomStateCountdown {
			t.Fatal("countdown did not start")
		}
		if err := owner.handleCountdownCancel(); err != nil {
			t.Fatal(err)
		}
		if current.State != RoomStateOpen || !current.waitingExpiresAt.Equal(deadline) {
			t.Fatal("cancelled countdown reset the waiting deadline")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if _, exists := s.hub.Snapshot(current.RoomID); exists {
			t.Fatal("cancelled countdown escaped expiry")
		}
	})
}

func TestWaitingRoomOwnerLeaveStopsExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, current := createExpiryTestRoom(t, false)
		peers := roomConnections(current)
		if err := current.connections[current.OwnerMemberType].close(true); err != nil {
			t.Fatal(err)
		}
		if current.waitingTimer != nil || !current.waitingExpiresAt.IsZero() {
			t.Fatal("owner departure left a waiting timer")
		}
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if _, exists := s.hub.Snapshot(current.RoomID); exists {
			t.Fatal("owner departure retained the room")
		}
		for _, peer := range peers {
			want := 1
			if peer.memberType == current.OwnerMemberType {
				want = 0
			}
			if got := strings.Count(peer.conn.(*sessionTestConn).output.String(), "RoomDelete{\n}\n"); got != want {
				t.Fatalf("timer repeated the owner departure notification: got %d, want %d", got, want)
			}
		}
	})
}

func TestBattleAndAIRoomSurviveWaitingDeadline(t *testing.T) {
	for _, autoStart := range []bool{false, true} {
		t.Run(strconv.FormatBool(autoStart), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, current := createExpiryTestRoom(t, autoStart)
				if !autoStart {
					if err := current.connections[current.OwnerMemberType].handleCountdownStart(); err != nil {
						t.Fatal(err)
					}
					time.Sleep(roomCountdownSeconds * time.Second)
					synctest.Wait()
				}
				if current.State != RoomStateBattle || current.waitingTimer != nil || !current.waitingExpiresAt.IsZero() {
					t.Fatal("battle retained a waiting timer")
				}
				time.Sleep(10 * time.Minute)
				synctest.Wait()
				if _, exists := s.hub.Snapshot(current.RoomID); !exists {
					t.Fatal("waiting deadline interrupted a battle")
				}
			})
		})
	}
}

func TestWaitingExpiryWaitsForBattleStartTransaction(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(strconv.FormatBool(denied), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, current := createExpiryTestRoom(t, false)
				release := make(chan struct{})
				// Startup normally installs this before room creation.
				s.hub.startAuthorizer = func(start BattleStart) error {
					if !start.CheckOnly {
						<-release
						if denied {
							return &BattleStartDenied{Message: "insufficient BP"}
						}
					}
					return nil
				}
				time.Sleep(9*time.Minute - 4*time.Second)
				if err := current.connections[current.OwnerMemberType].handleCountdownStart(); err != nil {
					t.Fatal(err)
				}
				time.Sleep(4 * time.Second)
				synctest.Wait()
				if s.hub.rooms[current.RoomID] != current || current.startDone == nil {
					t.Fatal("expiry discarded the in-flight start transaction")
				}
				close(release)
				synctest.Wait()
				_, exists := s.hub.Snapshot(current.RoomID)
				if denied {
					if exists || current.hostCostPaid {
						t.Fatal("rejected battle start kept an expired lobby")
					}
				} else if !exists || current.State != RoomStateBattle || !current.hostCostPaid {
					t.Fatal("expiry interrupted the paid battle")
				}
			})
		})
	}
}
