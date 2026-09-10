package cnbootstrap

import (
	"kairisei.local/server/internal/release"
	"slices"
	"strings"
)

func cnAdminGachaCardEligible(base release.GachaProfile, entry cnAdminCatalogEntry, id int) bool {
	if base.UnownedOnly && entry.Rarity != 6 {
		return false
	}
	if base.PayType == 2 || entry.GachaEligible {
		return true
	}
	// These immutable presets were generated from exact original banner names
	// and unevolved IDs. Blank acquisition cells do not invalidate their own
	// featured cards, but do not authorize adding other unlabeled cards.
	return entry.Detail == "" && entry.Rarity == 6 && slices.Contains(base.CardIDs, id) &&
		base.PoolSourceState == "INFERRED_OFFICIAL_CN_CACHED_BANNER_FEATURED_IDENTITIES_LOCAL_RARITY5_REMAINDER_CHR10_CHR20_CLOSED"
}

func cnCardCrystalGachaSource(text string) bool {
	return strings.Contains(text, "扭蛋") && !strings.Contains(strings.ToLower(text), "boss币")
}

// Tags describe the original acquisition text, not resource readiness. A card
// with multiple acquisition paths belongs to every matching tab.
func cnCardSourceTags(text string) []string {
	tags := []string{}
	if cnCardCrystalGachaSource(text) {
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
