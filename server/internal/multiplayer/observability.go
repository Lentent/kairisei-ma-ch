package multiplayer

import "sort"

type RoomActivity struct {
	RoomID       int64     `json:"room_id"`
	BossID       int       `json:"boss_id"`
	State        RoomState `json:"state"`
	GameSpeed    int       `json:"game_speed"`
	Turn         int       `json:"turn"`
	Wave         int       `json:"wave"`
	Players      []int     `json:"players"`
	Disconnected int       `json:"disconnected"`
	AI           int       `json:"ai"`
}

// Never expose lobby passwords, comeback tokens or connection credentials.
func (h *Hub) ActivitySnapshot() []RoomActivity {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]RoomActivity, 0, len(h.rooms))
	for _, room := range h.rooms {
		if room.State == RoomStateClosed {
			continue
		}
		row := RoomActivity{RoomID: room.RoomID, BossID: room.BossID, State: room.State,
			GameSpeed: room.GameSpeed, Turn: room.turnNumber, Wave: room.battleIndex + 1, Players: []int{}}
		for _, member := range room.Members {
			if room.connections[member.MemberType] != nil && member.UserID > 0 && member.UserID < 1900000000 {
				row.Players = append(row.Players, member.UserID)
			} else if _, pending := room.disconnectedUntil[member.MemberType]; pending {
				row.Disconnected++
			} else {
				row.AI++
			}
		}
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RoomID < result[j].RoomID })
	return result
}
