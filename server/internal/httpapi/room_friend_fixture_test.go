package httpapi

import "kairisei.local/server/internal/game"

type RoomFriendAccounts struct {
	game.FriendPointAccountRepository
	state     int8
	err       error
	requester int
	targets   []int
}

func (r *RoomFriendAccounts) FriendPointAccountStates(userID int, targets []int) (map[int]int8, error) {
	r.requester, r.targets = userID, append([]int(nil), targets...)
	return map[int]int8{202: r.state}, r.err
}
