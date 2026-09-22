package multiplayer

import (
	"slices"
	"sort"
)

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
	views := h.roomViews()
	result := make([]RoomActivity, 0, len(views))
	for _, view := range views {
		row := view.activity
		row.Players = slices.Clone(row.Players)
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RoomID < result[j].RoomID })
	return result
}
