package httpapi

import (
	"encoding/json"
	"sort"

	"kairisei.local/server/internal/release"
)

func (s *store) ownsCollectionRewardLocked(reward release.Reward) bool {
	var owned map[int]struct{}
	switch reward.Type {
	case 14:
		owned = s.costumeIDs
	case 16:
		owned = s.stampIDs
	case 18:
		owned = s.honorIDs
	default:
		return false
	}
	_, found := owned[reward.RewardTypeID]
	return found
}

func (s *store) costumeStateLocked() json.RawMessage {
	ids := make([]int, 0, len(s.costumeIDs))
	for id := range s.costumeIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	raw, _ := json.Marshal(struct {
		IDs []int `json:"costumeids"`
	}{ids})
	return raw
}

func (s *store) costumeState() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.costumeStateLocked()
}
