package cnbootstrap

import (
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestFirstAccountUsesOnboardingAndPreservesProgressOnReload(t *testing.T) {
	storage, err := newCNSaveDatabase(filepath.Join(t.TempDir(), "save.json"),
		filepath.Join("..", "..", "config", "cn602-save-template.json"),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	state, err := storage.loadOrImport()
	if err != nil {
		t.Fatal(err)
	}
	if state.User.Name != "" || state.User.TutorialFlag != 0 || len(state.Cards) != 10 ||
		state.Onboarding.ConfigVersion != cnOnboardingConfigVersion || state.Onboarding.Step != 0 ||
		state.User.CardMax != 6000 || state.User.NaviID != 0 || state.User.NaviUnlockFlag != 1 || !slices.Equal(state.User.SelectableNaviIDs, []int8{0}) || slices.Contains(state.User.UnlockedFeatureIDs, uint(10)) {
		t.Fatal("fresh primary account did not enter clean training")
	}
	state.User.Name = "保留玩家名字"
	state.User.Gold = 123
	state.User.NaviID = 1 // A player's later choice must survive reload.
	state.User.NaviUnlockFlag = 3
	state.User.SelectableNaviIDs = []int8{0, 1}
	state.Onboarding.Step = cnOnboardingStepCount
	state.User.UnlockedFeatureIDs = append(state.User.UnlockedFeatureIDs, 10)
	if err := storage.persist(state); err != nil {
		t.Fatal(err)
	}
	state, err = storage.loadOrImport()
	if err != nil {
		t.Fatal(err)
	}
	if state.User.Name != "保留玩家名字" || state.User.Gold != 123 || state.User.NaviID != 1 ||
		state.Onboarding.Step != cnOnboardingStepCount || !slices.Contains(state.User.UnlockedFeatureIDs, uint(10)) {
		t.Fatal("reload reset player progress or left encyclopedia locked")
	}
}

func TestInitializeCNOnboardingSnapshotRemovesQAAccountState(t *testing.T) {
	state, err := loadCNSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	if err := initializeCNOnboardingSnapshot(&state, 100000123); err != nil {
		t.Fatalf("initialize onboarding: %v", err)
	}
	if state.User.Name != "" || state.User.UserID != 100000123 ||
		state.User.TutorialFlag != 0 || state.User.Gold != 0 || state.User.CoinFree != 0 {
		t.Fatalf("clean user state was not installed: %+v", state.User)
	}
	if state.Onboarding.ConfigVersion != cnOnboardingConfigVersion || state.Onboarding.Step != 0 {
		t.Fatalf("onboarding state = %+v", state.Onboarding)
	}
	if slices.Contains(state.User.UnlockedFeatureIDs, uint(0)) ||
		slices.Contains(state.User.UnlockedFeatureIDs, uint(1)) ||
		slices.Contains(state.User.UnlockedFeatureIDs, uint(2)) ||
		slices.Contains(state.User.UnlockedFeatureIDs, uint(3)) {
		t.Fatalf("clean account pre-opened profession features: %v", state.User.UnlockedFeatureIDs)
	}
	if len(state.Cards) != 10 || len(state.ContainerCards) != 0 || len(state.StackCards) != 0 {
		t.Fatalf("inventory sizes = cards %d, container %d, stack %d", len(state.Cards), len(state.ContainerCards), len(state.StackCards))
	}
	actualCardIDs := make([]int, len(state.Cards))
	for index, card := range state.Cards {
		actualCardIDs[index] = card.CardID
	}
	if !slices.Equal(actualCardIDs, cnStarterCardIDs) {
		t.Fatalf("starter cards = %v, want %v", actualCardIDs, cnStarterCardIDs)
	}
	if len(state.Decks) != 4 {
		t.Fatalf("deck count = %d, want 4", len(state.Decks))
	}
	for _, deck := range state.Decks {
		if len(deck.CardUniqueIDs) != 10 || deck.LeaderCardIndex != 0 || deck.ArthurType != deck.JobType ||
			len(deck.BuddyUniqueIDs) != 5 || slices.ContainsFunc(deck.BuddyUniqueIDs, func(uniqueID int64) bool { return uniqueID != 0 }) ||
			deck.Name != cnDefaultDeckNameByArthur[deck.ArthurType] {
			t.Fatalf("invalid starter deck: %+v", deck)
		}
		leaderCardID := 0
		for _, card := range state.Cards {
			if card.UniqueID == deck.CardUniqueIDs[0] {
				leaderCardID = card.CardID
				break
			}
		}
		if leaderCardID != cnStarterLeaderCardIDByArthur[deck.ArthurType] {
			t.Fatalf("Arthur %d leader = %d", deck.ArthurType, leaderCardID)
		}
	}
	if len(state.Buddies) != 0 || state.Buddy.UniqueID != 0 {
		t.Fatalf("QA buddy state survived onboarding initialization: legacy=%+v buddies=%+v", state.Buddy, state.Buddies)
	}
	if len(state.Engagement.Missions) != 0 || len(state.Engagement.Presents) != 0 ||
		len(state.TeamBattleResultReceipts) != 0 || state.ActiveTeamBattle != nil {
		t.Fatal("QA engagement/battle state survived onboarding initialization")
	}
	foundTutorialGacha := false
	foundTutorialMultiGacha := false
	for _, gacha := range state.Gachas {
		switch gacha.GachaID {
		case cnOnboardingGachaID:
			foundTutorialGacha = true
			if gacha.PayTypeID != 2001 || !slices.Equal(gacha.CardIDs, []int{10002001}) {
				t.Fatalf("tutorial gacha = %+v", gacha)
			}
		case cnOnboardingMultiGachaID:
			foundTutorialMultiGacha = true
			if gacha.PayType != 3 || gacha.Price != 400 || gacha.CardNum != 11 ||
				gacha.GuaranteedRarityRank != 5 || gacha.GuaranteedCount != 1 ||
				gacha.RemainderRarityRank != 4 {
				t.Fatalf("tutorial multi gacha = %+v", gacha)
			}
		}
	}
	if !foundTutorialGacha || !foundTutorialMultiGacha {
		t.Fatal("one or more client-reserved tutorial gachas are missing")
	}
	if cnOnboardingGachaID != 90000200 {
		t.Fatalf("tutorial gacha ID = %d, want client-reserved first-draw ID", cnOnboardingGachaID)
	}
	encoded, err := encodeCNSaveState(state)
	if err != nil {
		t.Fatalf("encode clean onboarding snapshot: %v", err)
	}
	decoded, err := decodeCNSaveState(encoded)
	if err != nil {
		t.Fatalf("decode clean onboarding snapshot: %v", err)
	}
	if decoded.Onboarding != state.Onboarding || len(decoded.Cards) != 10 || decoded.Buddies == nil {
		t.Fatal("onboarding snapshot did not round-trip")
	}
}

func TestMigrateCNDefaultDeckNamesPreservesPlayerNames(t *testing.T) {
	state := release.State{Decks: []release.Deck{
		{ArthurType: 1, Name: "佣兵本地卡组"},
		{ArthurType: 2, Name: "我的富豪卡组"},
		{ArthurType: 3, Name: "盗贼本地卡组"},
	}}
	if !migrateCNDefaultDeckNames(&state) {
		t.Fatal("legacy generated deck names were not migrated")
	}
	if state.Decks[0].Name != "佣兵卡组1" || state.Decks[1].Name != "我的富豪卡组" ||
		state.Decks[2].Name != "盗贼卡组1" {
		t.Fatalf("migrated deck names = %+v", state.Decks)
	}
	if migrateCNDefaultDeckNames(&state) {
		t.Fatal("deck-name migration is not idempotent")
	}
}

func TestMigrateCNOnboardingGachaID(t *testing.T) {
	state, err := loadCNSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	if err := initializeCNOnboardingSnapshot(&state, 100000124); err != nil {
		t.Fatalf("initialize onboarding: %v", err)
	}
	for index := range state.Gachas {
		if state.Gachas[index].GachaID == cnOnboardingGachaID {
			state.Gachas[index].GachaID = cnLegacyOnboardingGachaID
			state.Gachas[index].GroupID = cnLegacyOnboardingGachaID
		}
	}
	changed, err := migrateCNOnboardingGachaID(&state)
	if err != nil || !changed {
		t.Fatalf("migrate onboarding gacha = (%t, %v)", changed, err)
	}
	found := false
	foundMulti := false
	for _, gacha := range state.Gachas {
		if gacha.GachaID == cnLegacyOnboardingGachaID {
			t.Fatal("legacy onboarding gacha survived migration")
		}
		if gacha.GachaID == cnOnboardingGachaID {
			found = true
		}
		if gacha.GachaID == cnOnboardingMultiGachaID {
			foundMulti = true
		}
	}
	if !found || !foundMulti {
		t.Fatal("one or more client-reserved onboarding gachas are missing after migration")
	}
}

func TestMigrateCNOnboardingStateRestoresMissingOriginalSteps(t *testing.T) {
	state, err := loadCNSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	state.Onboarding = release.OnboardingState{
		ConfigVersion:       cnLegacyOnboardingConfigVersion,
		Step:                5,
		CurrentAnnounced:    true,
		PendingClearQuestID: 1045,
	}
	changed, err := migrateCNOnboardingState(&state)
	if err != nil || !changed {
		t.Fatalf("migrate onboarding sequence = (%t, %v)", changed, err)
	}
	if state.Onboarding.ConfigVersion != cnOnboardingConfigVersion ||
		state.Onboarding.Step != 4 || state.Onboarding.CurrentAnnounced ||
		state.Onboarding.PendingClearQuestID != 0 {
		t.Fatalf("migrated onboarding state = %+v", state.Onboarding)
	}

	state.Onboarding = release.OnboardingState{
		ConfigVersion: cnLegacyOnboardingConfigVersion,
		Step:          7,
	}
	changed, err = migrateCNOnboardingState(&state)
	if err != nil || !changed || state.Onboarding.Step != cnOnboardingStepCount {
		t.Fatalf("migrate completed onboarding = (%+v, %t, %v)", state.Onboarding, changed, err)
	}
}

func TestCNOnboardingDoesNotReceiveLocalQAInitialItems(t *testing.T) {
	state := release.State{
		Onboarding: release.OnboardingState{ConfigVersion: cnOnboardingConfigVersion},
		Items:      []release.Item{{ItemID: 2001}},
	}
	master := cnItemRuntimeMaster{
		LocalAccountConfigVersion: 3,
		LocalAccountInitialItems:  []release.Item{{ItemID: 4000, Num: 10000}},
		Items: []release.ItemDefinition{
			{ItemID: 2001}, {ItemID: 4000},
		},
	}
	changed, err := applyCNItemRuntimeMaster(&state, master)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || state.LocalAccountConfigVersion != 3 {
		t.Fatalf("local item config migration = (%t, %d)", changed, state.LocalAccountConfigVersion)
	}
	if len(state.Items) != 1 || state.Items[0].ItemID != 2001 {
		t.Fatalf("onboarding items = %+v", state.Items)
	}
}
