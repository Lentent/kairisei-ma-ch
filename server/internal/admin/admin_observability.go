package admin

import (
	"sort"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/multiplayer"
)

type activityOverview struct {
	HTTP        accounthttp.ActivitySnapshot `json:"http"`
	Rooms       []multiplayer.RoomActivity   `json:"rooms"`
	OnlineIDs   []int                        `json:"online_ids"`
	TeamPlayers int                          `json:"team_players"`
	SoloPlayers int                          `json:"solo_players"`
	BattleRooms int                          `json:"battle_rooms"`
	LobbyRooms  int                          `json:"lobby_rooms"`
	Available   bool                         `json:"available"`
}

func (a *API) activity() activityOverview {
	result := activityOverview{OnlineIDs: []int{}, Rooms: []multiplayer.RoomActivity{}}
	if source, ok := a.business.(interface {
		ActivitySnapshot() accounthttp.ActivitySnapshot
	}); ok {
		result.HTTP = source.ActivitySnapshot()
		result.Available = true
	}
	if a.multiplayerHub != nil {
		result.Rooms = a.multiplayerHub.ActivitySnapshot()
	}
	online, team, roomPlayers := map[int]bool{}, map[int]bool{}, map[int]bool{}
	for _, room := range result.Rooms {
		if room.State == multiplayer.RoomStateBattle {
			result.BattleRooms++
		} else {
			result.LobbyRooms++
		}
		for _, id := range room.Players {
			online[id], roomPlayers[id] = true, true
			if room.State == multiplayer.RoomStateBattle {
				team[id] = true
			}
		}
	}
	for _, player := range result.HTTP.Players {
		online[player.UserID] = true
		if player.UnsettledSolo && !roomPlayers[player.UserID] {
			result.SoloPlayers++
		}
	}
	result.TeamPlayers = len(team)
	for id := range online {
		result.OnlineIDs = append(result.OnlineIDs, id)
	}
	sort.Ints(result.OnlineIDs)
	return result
}
