package httpapi

import (
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestRarityFiveTicketGachaUsesDedicatedLoopbackBanner(t *testing.T) {
	api := &API{baseURL: "http://10.0.2.2:26020"}
	infos := api.gachaInfos([]release.GachaProfile{
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

func TestRarityFiveTicketGachaConsumesOfficialItemAndAddsCard(t *testing.T) {
	const (
		gachaID = 60200301
		itemID  = 9010
		cardID  = 5001
	)
	cardDefinition := release.Card{
		CardID:            cardID,
		Name:              "五星测试卡",
		RarityRank:        5,
		LevelMax:          1,
		LoveMax:           0,
		FameMax:           100,
		ExperienceTableID: 1,
	}
	cardStore := &store{
		gachas: []release.GachaProfile{{
			GachaID: gachaID, PayType: 4, PayTypeID: itemID, Price: 1,
			CardNum: 1, CardNumMax: 1, CardIDs: []int{cardID}, CardWeights: []int{1},
		}},
		items: map[int]release.Item{itemID: {ItemID: itemID, Num: 2}},
		itemDefinitions: map[int]release.ItemDefinition{
			itemID: {ItemID: itemID, ItemType: "GACHA_TICKET", MaxOwned: 9999999},
		},
		cardDefinitions:   map[int]release.Card{cardID: cardDefinition},
		cardTemplates:     map[int]cardInfo{cardID: {CardID: cardID, LevelMax: 1, Fame: 1, SkillLevels: []int16{1}}},
		cardExperience:    map[int][]int{1: {}},
		cardCollectionIDs: map[int]struct{}{},
		cardMax:           10,
		nextUniqueID:      1,
		cardProgression: release.CardProgressionPolicy{
			ConfigVersion: 1, FusionGoldPerMaterialPerBaseLevel: 1,
		},
	}

	result, err := cardStore.playGacha(gachaID, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Item.ItemID != itemID || result.Item.Num != 1 || cardStore.items[itemID].Num != 1 {
		t.Fatalf("ticket state = result %+v / store %+v", result.Item, cardStore.items[itemID])
	}
	if len(cardStore.cards) != 1 || cardStore.cards[0].CardID != cardID ||
		cardStore.cardDefinitions[cardStore.cards[0].CardID].RarityRank != 5 {
		t.Fatalf("rarity-five reward was not added: %+v", cardStore.cards)
	}
	if cardStore.gachas[0].PlayCount != 1 {
		t.Fatalf("play count = %d, want 1", cardStore.gachas[0].PlayCount)
	}
	if result.Expectancy != 4 {
		t.Fatalf("five-star draw expectancy=%d, want ULTRARARE(4)", result.Expectancy)
	}

	cardStore.items[itemID] = release.Item{ItemID: itemID}
	beforeCards := len(cardStore.cards)
	if _, err := cardStore.playGacha(gachaID, 4, nil); err == nil {
		t.Fatal("expected insufficient ticket rejection")
	}
	if len(cardStore.cards) != beforeCards || cardStore.gachas[0].PlayCount != 1 {
		t.Fatal("failed draw mutated cards or play count")
	}
	// Selling the drawn card must not make its family "unowned" again.
	cardStore.cards = nil
	cardStore.items[itemID] = release.Item{ItemID: itemID, Num: 1}
	cardStore.gachas[0].UnownedOnly = true
	// A different acquired form must also exclude the base family.
	sameFamily := cardDefinition
	sameFamily.CardID = cardID + 1
	cardStore.cardDefinitions[cardID+1] = sameFamily
	delete(cardStore.cardCollectionIDs, cardID)
	cardStore.cardCollectionIDs[cardID+1] = struct{}{}
	if len(cardStore.visibleGachasLocked()) != 0 {
		t.Fatal("exhausted unowned pool remains published")
	}
	if _, err := cardStore.playGacha(gachaID, 4, nil); err == nil || cardStore.items[itemID].Num != 1 || cardStore.gachas[0].PlayCount != 1 {
		t.Fatal("unowned pool charged for a previously collected card")
	}
}

func TestUnavailableOnlyTicketSingleRemainsVisibleForItemLackTips(t *testing.T) {
	const (
		singleID = 60200301
		multiID  = 60200302
		itemID   = 9010
	)
	cardStore := &store{
		gachas: []release.GachaProfile{
			{GachaID: singleID, GroupID: singleID, CategoryNum: 5, PayType: 4, PayTypeID: itemID, Price: 1, CardNum: 1, CardNumMax: 1},
			{GachaID: multiID, GroupID: singleID, CategoryNum: 5, PayType: 4, PayTypeID: itemID, Price: 10, CardNum: 10, CardNumMax: 10},
		},
		items: map[int]release.Item{itemID: {ItemID: itemID, Num: 0}},
	}

	visible := cardStore.gachaState()
	if len(visible) != 2 || visible[0].GachaID != singleID || visible[1].GachaID != multiID {
		t.Fatalf("visible gachas = %+v, want unavailable single followed by multi", visible)
	}
}

func TestUnavailableTicketSingleYieldsToUsableCrystalSingle(t *testing.T) {
	const (
		ticketID  = 60200201
		crystalID = 60200211
		multiID   = 60200212
		itemID    = 2000
	)
	cardStore := &store{
		gachas: []release.GachaProfile{
			{GachaID: ticketID, GroupID: ticketID, CategoryNum: 4, PayType: 4, PayTypeID: itemID, Price: 1, CardNum: 1, CardNumMax: 1},
			{GachaID: crystalID, GroupID: ticketID, CategoryNum: 4, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1},
			{GachaID: multiID, GroupID: ticketID, CategoryNum: 4, PayType: 3, Price: 50, CardNum: 10, CardNumMax: 10},
		},
		items: map[int]release.Item{itemID: {ItemID: itemID, Num: 0}},
	}

	visible := cardStore.gachaState()
	if len(visible) != 2 || visible[0].GachaID != crystalID || visible[1].GachaID != multiID {
		t.Fatalf("visible gachas = %+v, want usable crystal single followed by multi", visible)
	}
}

func TestCrystalGachaTicketQuickBuyConsumesFreeCrystalAndStaysHidden(t *testing.T) {
	const (
		lineupID = 992001
		itemID   = 9010
	)
	cardStore := &store{
		coin:     3,
		coinFree: 2,
		itemShopTabs: []release.ItemShopTab{{
			TabType: 0,
			Lineup: []release.ItemShopLineup{{
				LineupID: lineupID, PictID: 90010, LineupName: "水晶扭蛋币快捷兑换",
				PayType: 3, Price: 1, BuyNumMax: 5000, Hidden: true,
				Interiors: []release.ItemShopInterior{{BuyType: 1, BuyTypeID: itemID, Num: 1}},
			}},
		}},
		items: map[int]release.Item{itemID: {ItemID: itemID}},
		itemDefinitions: map[int]release.ItemDefinition{
			itemID: {ItemID: itemID, ItemType: "GACHA_TICKET", MaxOwned: 9999999},
		},
	}

	updated, err := cardStore.buyItemShop(lineupID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ItemID != itemID || updated[0].Num != 2 ||
		cardStore.coin != 3 || cardStore.coinFree != 0 {
		t.Fatalf("quick buy result = %+v, paid/free = %d/%d", updated, cardStore.coin, cardStore.coinFree)
	}
	wire := itemShopTabsWire(cardStore.itemShopTabs, map[int]int{itemID: 2})
	lineups := wire[0].(map[string]any)["lineup"].([]any)
	if len(lineups) != 0 {
		t.Fatalf("hidden quick-buy lineup leaked into ItemShopShow: %+v", lineups)
	}
}
