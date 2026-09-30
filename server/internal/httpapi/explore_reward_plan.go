package httpapi

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

// Roll each configured reward independently at entry, like boss drops. Only
// selected rewards reach the native event DTO and the persisted settlement plan.
func planExploreRewards(source []json.RawMessage, seed []byte) ([]json.RawMessage, []gamestate.Reward, error) {
	if _, err := decodeExploreRewards(source); err != nil {
		return nil, nil, err
	}
	events := make([]json.RawMessage, len(source))
	for eventIndex, raw := range source {
		var event map[string]json.RawMessage
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, nil, err
		}
		for _, kind := range []string{"symbols", "treasureboxes"} {
			if len(event[kind]) == 0 {
				continue
			}
			var rows []map[string]json.RawMessage
			if err := json.Unmarshal(event[kind], &rows); err != nil {
				return nil, nil, err
			}
			for rowIndex, row := range rows {
				if len(row["local_reward_chances"]) == 0 {
					continue
				}
				var chances []int
				var rewards []gamestate.Reward
				if err := json.Unmarshal(row["local_reward_chances"], &chances); err != nil {
					return nil, nil, err
				}
				if err := json.Unmarshal(row["reward"], &rewards); err != nil {
					return nil, nil, err
				}
				if len(chances) != len(rewards) {
					return nil, nil, errors.New("Explore reward probabilities do not match rewards")
				}
				selected := []gamestate.Reward{}
				for i, reward := range rewards {
					chance := chances[i]
					if chance < 0 || chance > 1000000 {
						return nil, nil, errors.New("Explore reward probability is out of range")
					}
					hash := sha256.New()
					hash.Write(seed)
					fmt.Fprintf(hash, "|explore=%d|kind=%s|slot=%d|reward=%d", eventIndex, kind, rowIndex, i)
					if int(binary.BigEndian.Uint64(hash.Sum(nil))%1000000) < chance {
						selected = append(selected, reward)
					}
				}
				row["reward"], _ = json.Marshal(selected)
				delete(row, "local_reward_chances")
			}
			event[kind], _ = json.Marshal(rows)
		}
		events[eventIndex], _ = json.Marshal(event)
	}
	rewards, err := decodeExploreRewards(events)
	return events, rewards, err
}
