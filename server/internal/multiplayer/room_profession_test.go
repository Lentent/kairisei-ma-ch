package multiplayer

import (
	"testing"
	"time"
)

func TestRoomProfessionsStayInTheirWaitingSlots(t *testing.T) {
	for owner := 1; owner <= 4; owner++ {
		for firstGuest := 1; firstGuest <= 4; firstGuest++ {
			if firstGuest == owner {
				continue
			}
			r := &room{RoomSnapshot: RoomSnapshot{State: RoomStateOpen, Members: []Member{{MemberType: 1, UserID: 100, ArthurType: owner}}}, reservations: make(map[int]roomReservation)}
			waiting := roomProfessionSlots(r)
			for _, profession := range []int{firstGuest, 4, 3, 2, 1} {
				if profession == owner || (profession != firstGuest && roomHasArthur(r, profession)) {
					continue
				}
				seat, err := roomReservationSlot(r, 100+profession, profession, time.Now())
				if err != nil {
					if roomHasArthur(r, profession) {
						continue
					}
					t.Fatal(err)
				}
				if waiting[seat] != profession {
					t.Fatalf("owner=%d guest=%d replaced waiting Arthur %d in slot %d", owner, profession, waiting[seat], seat)
				}
				r.Members = append(r.Members, Member{MemberType: seat, UserID: 100 + profession, ArthurType: profession})
			}
			if roomProfessionSlots(r) != waiting {
				t.Fatal("joining reordered professions")
			}
			// Leaving, rejoining and CPU filling must use the same binding.
			removed := r.Members[1]
			removeMemberLocked(r, removed.MemberType)
			if roomProfessionSlots(r) != waiting {
				t.Fatal("departure swapped waiting portraits")
			}
			r.ownerFallbackParty = []Member{removed}
			added, err := fillOwnerFallbackPartyLocked(r)
			if err != nil || len(added) != 1 || added[0].MemberType != removed.MemberType || roomProfessionSlots(r) != waiting {
				t.Fatal("CPU changed the waiting profession", err)
			}
		}
	}
}

func roomHasArthur(r *room, arthur int) bool {
	for _, member := range r.Members {
		if member.ArthurType == arthur {
			return true
		}
	}
	return false
}
