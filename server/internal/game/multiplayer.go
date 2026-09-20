package game

import (
	"encoding/json"
)

func TeamBattleGroupForBoss(configuration json.RawMessage, bossID int) (int, any, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(configuration, &top) != nil {
		return 0, nil, false
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if json.Unmarshal(top[key], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var groupID int
			var bosses []map[string]json.RawMessage
			if json.Unmarshal(group["0"], &groupID) != nil || json.Unmarshal(group["10"], &bosses) != nil {
				continue
			}
			for _, boss := range bosses {
				var current int
				if json.Unmarshal(boss["0"], &current) == nil && current == bossID {
					var wire any
					encoded, err := json.Marshal(group)
					if err != nil || json.Unmarshal(encoded, &wire) != nil {
						return 0, nil, false
					}
					return groupID, wire, true
				}
			}
		}
	}
	return 0, nil, false
}
