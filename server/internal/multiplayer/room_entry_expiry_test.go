package multiplayer

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestRoomEntryAvailabilityAfterCredential(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"room removed", "-3208"}, {"room started", "-3208"},
		{"reservation expired", "-3208"}, {"profession occupied", "-3202"},
		{"wrong signature", "-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, members := nextBattleFixture(t)
			member := members[1]
			member.Level, member.LeaderCardID, member.LeaderLevel, member.LeaderFame = 1, 1, 1, 1
			member.PartsIDs, member.DeckHonorIDs = make([]int, 7), make([]int, 4)
			h := NewHub()
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateOpen, Members: members[:1]},
				reservations: make(map[int]roomReservation)}
			h.rooms[1] = current
			if _, err := h.Reserve(1, member.UserID, member.ArthurType); err != nil {
				t.Fatal(err)
			}
			credential, err := h.IssueEnter(1, member)
			if err != nil {
				t.Fatal(err)
			}
			signature := credential.Signature
			switch tc.name {
			case "room removed":
				delete(h.rooms, 1)
			case "room started":
				current.State = RoomStateBattle
			case "reservation expired":
				r := current.reservations[member.ArthurType]
				r.ExpiresAt = time.Now().Add(-time.Second)
				current.reservations[member.ArthurType] = r
			case "profession occupied":
				current.Members = append(current.Members, Member{MemberType: 2, UserID: 5005, ArthurType: member.ArthurType})
				delete(current.reservations, member.ArthurType)
			case "wrong signature":
				signature = "wrong"
			}
			count := len(current.Members)
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			c := &clientConn{conn: left, server: &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
			done := make(chan error, 1)
			go func() {
				done <- c.handleEnter(joinCSV(strconv.Itoa(member.UserID), "1", "", "2", credential.AuthToken, signature, "Android"))
			}()
			_ = right.SetReadDeadline(time.Now().Add(time.Second))
			method, payload, err := readFrame(bufio.NewReader(right))
			if err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			fields := splitCSV(payload)
			if method != "RoomEnterRequestResult" || len(fields) != 2 || fields[0] != tc.code {
				t.Fatalf("wrong original-client entry result: %s %q, want %s", method, payload, tc.code)
			}
			if c.roomID != 0 || len(current.Members) != count {
				t.Fatal("rejected entry mutated membership")
			}
			_, pending := h.pending[credential.AuthToken]
			if pending != (tc.name == "wrong signature") {
				t.Fatal("credential consumption changed")
			}
		})
	}
}
