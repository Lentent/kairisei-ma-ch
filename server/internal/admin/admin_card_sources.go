package admin

import (
	"encoding/json"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

// Region comes from the import receipt, never from names or card ID ranges.
// It is a catalog shortcut, not evidence of a crystal-gacha acquisition path.
func adminJPCardIDs(source json.RawMessage) (map[int]bool, error) {
	var provenance struct {
		Imports []struct {
			Region string `json:"region"`
			IDs    struct {
				Cards []int `json:"card"`
			} `json:"ids"`
		} `json:"imported_inventory"`
	}
	ids := make(map[int]bool)
	if len(source) == 0 {
		return ids, nil
	}
	if err := json.Unmarshal(source, &provenance); err != nil {
		return nil, err
	}
	for _, imported := range provenance.Imports {
		if imported.Region == "JP" {
			for _, id := range imported.IDs.Cards {
				ids[id] = true
			}
		}
	}
	return ids, nil
}

// Operator pools may offer any existing card (user decision 2026-09-27, LOCAL_POLICY); the
// unowned-six-star pool keeps its six-star identity because its draw excludes owned card families.
func adminGachaCardEligible(base gamestate.GachaProfile, entry AdminCatalogEntry) bool {
	if base.ArthurType >= 1 && base.ArthurType <= 4 && entry.ArthurType != 0 && entry.ArthurType != base.ArthurType {
		return false
	}
	return !base.UnownedOnly || entry.Rarity == 6
}

func cardCrystalGachaSource(text string) bool {
	return strings.Contains(text, "扭蛋") && !strings.Contains(strings.ToLower(text), "boss币")
}

// Tags describe the original acquisition text, not resource readiness. A card
// with multiple acquisition paths belongs to every matching tab.
func cardSourceTags(text string) []string {
	tags := []string{}
	if cardCrystalGachaSource(text) {
		tags = append(tags, "gacha")
	}
	if strings.Contains(text, "副本") {
		tags = append(tags, "dungeon")
	}
	if strings.Contains(strings.ToLower(text), "boss币") {
		tags = append(tags, "boss_coin")
	}
	if strings.Contains(text, "进化") || strings.Contains(text, "進化") || strings.Contains(text, "乖离") {
		tags = append(tags, "evolution")
	}
	if strings.Contains(text, "覚醒") || strings.Contains(text, "觉醒") {
		tags = append(tags, "awakening")
	}
	if strings.Contains(text, "限界") {
		tags = append(tags, "limit")
	}
	if strings.Contains(text, "特典") || strings.Contains(text, "活动") || strings.Contains(text, "任务") || strings.Contains(text, "礼包") || text == "PVP" {
		tags = append(tags, "event")
	}
	if len(tags) == 0 {
		tags = append(tags, "other")
	}
	return tags
}
