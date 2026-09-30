package multiplayer

import "testing"

func TestMaintenanceRevokesRoomAdmissionWithoutInterruptingRooms(t *testing.T) {
	h := NewHub()
	h.creatingRooms = 1
	if h.SetMaintenance(true) == nil {
		t.Fatal("maintenance interrupted room creation")
	}
	h.creatingRooms = 0
	h.rooms[1] = &room{}
	if h.SetMaintenance(true) == nil {
		t.Fatal("maintenance interrupted active room")
	}
	delete(h.rooms, 1)
	h.pending["before-maintenance"] = pendingRequest{Kind: pendingCreate}
	if err := h.SetMaintenance(true); err != nil {
		t.Fatal(err)
	}
	if len(h.pending) != 0 || !h.maintenance {
		t.Fatal("old authorization can bypass maintenance")
	}
	if err := h.SetMaintenance(false); err != nil || h.maintenance {
		t.Fatal("maintenance did not end", err)
	}
}
