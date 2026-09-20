package httpapi

import (
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestRarityFiveTicketGachaUsesDedicatedLoopbackBanner(t *testing.T) {
	api := &API{baseURL: "http://10.0.2.2:26020"}
	infos := api.gachaInfos([]gamestate.GachaProfile{
		{GachaID: 1},
		{GachaID: 2, BannerKey: "five_star_ticket"},
		{GachaID: 3, BannerKey: "element_fire"},
	})
	defaultBanner := infos[0].(map[string]any)["image_l_url"].(string)
	fiveStarBanner := infos[1].(map[string]any)["image_l_url"].(string)
	elementBanner := infos[2].(map[string]any)["image_l_url"].(string)
	if !strings.HasSuffix(defaultBanner, "/local/gacha/banner.png") {
		t.Fatalf("default banner = %q", defaultBanner)
	}
	if !strings.HasSuffix(fiveStarBanner, "/local/gacha/five-star-banner.png") ||
		fiveStarBanner == defaultBanner {
		t.Fatalf("five-star banner = %q, default = %q", fiveStarBanner, defaultBanner)
	}
	if !strings.HasSuffix(elementBanner, "/local/gacha/element_fire.png") ||
		elementBanner == defaultBanner {
		t.Fatalf("element banner = %q, default = %q", elementBanner, defaultBanner)
	}
}

func TestHiddenAndDisabledItemShopOffersAreOmitted(t *testing.T) {
	tabs := []gamestate.ItemShopTab{{Lineup: []gamestate.ItemShopLineup{{Hidden: true}, {Disabled: true}, {LineupID: 3, Price: 12000}}}}
	lineups := itemShopTabsWire(tabs, nil)[0].(map[string]any)["lineup"].([]any)
	if len(lineups) != 1 || lineups[0].(map[string]any)["price"] != 12000 {
		t.Fatalf("visible offers: %+v", lineups)
	}
}
