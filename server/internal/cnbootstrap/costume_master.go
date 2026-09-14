package cnbootstrap

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"kairisei.local/server/internal/release"
)

// Native CN 6.0.2 costume.csv identities, excluding commented-out rows.
// The public runtime avatar JSON omits this small collection catalog.
// Imported costumes are supplied only by a matching resource-set avatar master.
// Older resource sets keep using this embedded native catalog unchanged.
//
//go:embed costume_rewards.json
var cnCostumeRewardsJSON []byte

func loadCNCostumeRewards(imported []release.CollectionRewardDefinition) ([]release.CollectionRewardDefinition, error) {
	var result []release.CollectionRewardDefinition
	if err := json.Unmarshal(cnCostumeRewardsJSON, &result); err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(result)+len(imported))
	for _, row := range result {
		seen[row.ID] = true
	}
	for _, row := range imported {
		if row.Type != 14 || row.ID <= 0 || row.PictID <= 0 || row.Name == "" || seen[row.ID] {
			return nil, fmt.Errorf("invalid or duplicate imported costume %d", row.ID)
		}
		seen[row.ID] = true
		result = append(result, row)
	}
	return result, nil
}
