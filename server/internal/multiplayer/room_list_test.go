package multiplayer

import (
	"errors"
	"testing"
	"time"
)

func TestRoomListOnlyReturnsRealOpenNonFullMatchingRooms(t *testing.T) {
	h := NewHub()
	if got := h.List(RoomSearch{}); got == nil || len(got) != 0 {
		t.Fatalf("empty hub list: %v", got)
	}
	// Exercise the actual registry filter independently of battle transport.
	h.rooms[1] = &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, State: RoomStateOpen, Members: []Member{{UserID: 1}}}}
	h.rooms[2] = &room{RoomSnapshot: RoomSnapshot{RoomID: 2, BossID: 22, State: RoomStateOpen, Members: []Member{{UserID: 2}}}}
	h.rooms[3] = &room{RoomSnapshot: RoomSnapshot{RoomID: 3, BossID: 11, State: RoomStateBattle}}
	h.rooms[4] = &room{RoomSnapshot: RoomSnapshot{RoomID: 4, BossID: 11, State: RoomStateCountdown}}
	h.rooms[5] = &room{RoomSnapshot: RoomSnapshot{RoomID: 5, BossID: 11, State: RoomStateOpen, Members: make([]Member, 4)}}
	if got := h.List(RoomSearch{}); len(got) != 2 || got[0].RoomID != 1 || got[1].RoomID != 2 {
		t.Fatalf("all joinable: %v", got)
	}
	if got := h.List(RoomSearch{BossID: 11}); len(got) != 1 || got[0].RoomID != 1 {
		t.Fatalf("boss filter: %v", got)
	}
	h.rooms[6] = &room{RoomSnapshot: RoomSnapshot{RoomID: 6, BossID: 11, State: RoomStateOpen,
		HasPassword: true, BossGroup: roomPrivate{password: "1234"}, Members: []Member{{UserID: 6}}}}
	for _, query := range []struct {
		password string
		roomID   int64
	}{{"", 1}, {"1234", 6}, {"wrong", 0}} {
		got := h.List(RoomSearch{BossID: 11, Password: query.password})
		if query.roomID == 0 {
			if len(got) != 0 {
				t.Errorf("unknown password returned rooms: %v", got)
			}
		} else if len(got) != 1 || got[0].RoomID != query.roomID {
			t.Errorf("password %q returned %v, want room %d", query.password, got, query.roomID)
		}
	}
	session := h.lockRoomSession(1)
	session.room.State = RoomStateClosed
	session.Unlock()
	if got := h.List(RoomSearch{BossID: 11}); len(got) != 0 {
		t.Fatalf("closed room remains visible: %v", got)
	}
	delete(h.rooms, 2)
	if got := h.List(RoomSearch{}); len(got) != 0 {
		t.Fatalf("removed room remains visible: %v", got)
	}
}

func TestRoomSearchMatchesReservationAvailability(t *testing.T) {
	for _, tc := range []struct {
		name    string
		userID  int
		arthur  int
		groupID int
		expired bool
		want    bool
	}{
		{"free profession", 4, 3, 10, false, true},
		{"joined profession", 4, 1, 10, false, false},
		{"already in room", 1, 3, 10, false, false},
		{"other reservation", 4, 2, 10, false, false},
		{"own reservation", 3, 2, 10, false, true},
		{"expired reservation", 4, 2, 10, true, true},
		{"wrong group", 4, 3, 20, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHub()
			expires := time.Now().Add(time.Minute)
			if tc.expired {
				expires = time.Now().Add(-time.Minute)
			}
			h.rooms[1] = &room{RoomSnapshot: RoomSnapshot{RoomID: 1, BossID: 11, BossGroupID: 10,
				State: RoomStateOpen, NeedHP: 99999, NeedFame: 100,
				Members: []Member{{MemberType: 1, UserID: 1, ArthurType: 4}, {MemberType: 2, UserID: 2, ArthurType: 1}}},
				reservations: map[int]roomReservation{2: {UserID: 3, MemberType: 3, ExpiresAt: expires}}}
			got := h.List(RoomSearch{BossID: 11, BossGroupID: tc.groupID, UserID: tc.userID, ArthurType: tc.arthur})
			if (len(got) == 1) != tc.want {
				t.Errorf("search returned %d rooms, want visible=%v", len(got), tc.want)
			}
			// Searching must not consume or renew anyone's reservation.
			if len(h.rooms[1].reservations) != 1 || !h.rooms[1].reservations[2].ExpiresAt.Equal(expires) {
				t.Fatal("search changed the reservation")
			}
			if tc.groupID == 10 {
				_, err := h.Reserve(1, tc.userID, tc.arthur)
				if (err == nil) != tc.want {
					t.Errorf("reservation availability differs from search: %v", err)
				}
				if err != nil && !errors.Is(err, ErrRoomUnavailable) && !errors.Is(err, ErrRoomArthurUnavailable) {
					t.Errorf("unavailable search result lost its business rejection: %v", err)
				}
			}
		})
	}
}
