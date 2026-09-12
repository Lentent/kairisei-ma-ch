package cnbootstrap

import (
	_ "embed"
	"encoding/json"

	"kairisei.local/server/internal/release"
)

// Native CN 6.0.2 costume.csv identities, excluding commented-out rows.
// The public runtime avatar JSON omits this small collection catalog.
// Keeping it embedded requires no new game resources or CDN upload.
//
//go:embed costume_rewards.json
var cnCostumeRewardsJSON []byte

func loadCNCostumeRewards() ([]release.CollectionRewardDefinition, error) {
	var result []release.CollectionRewardDefinition
	err := json.Unmarshal(cnCostumeRewardsJSON, &result)
	return result, err
}
