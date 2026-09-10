package cnbootstrap

import (
	"errors"
	"fmt"
	"sort"

	"kairisei.local/server/internal/release"
)

const (
	cnOnboardingConfigVersion       = 2
	cnOnboardingStepCount           = 9
	cnOnboardingGachaID             = 90000200
	cnOnboardingMultiGachaID        = 90000100
	cnLegacyOnboardingGachaID       = 60209901
	cnLegacyOnboardingConfigVersion = 1
)

var cnStarterCardIDs = []int{
	10000010, 10000018, 10000022, 10000026, 10000030,
	10000034, 10000038, 10000042, 10000046, 10000050,
}

var cnStarterLeaderCardIDByArthur = map[int8]int{
	1: 10000010,
	2: 10000022,
	3: 10000030,
	4: 10000034,
}

var cnDefaultDeckNameByArthur = map[int8]string{
	1: "佣兵卡组1",
	2: "富豪卡组1",
	3: "盗贼卡组1",
	4: "歌姬卡组1",
}

var cnLegacyLocalDeckNameByArthur = map[int8]string{
	1: "佣兵本地卡组",
	2: "富豪本地卡组",
	3: "盗贼本地卡组",
	4: "歌姬本地卡组",
}

// initializeCNOnboardingSnapshot turns the broad QA seed into a clean account
// creation snapshot. The ten-card identity is INFERRED from the retained seed
// and the user-provided original-flow screenshot; the quest IDs, ticket 2001
// and first draw card 10002001 come from the official CN client/master contract.
func initializeCNOnboardingSnapshot(state *release.State, userID int) error {
	if state == nil || userID <= 0 {
		return errors.New("CN onboarding snapshot identity is invalid")
	}
	startersByCardID := make(map[int]release.Card, len(cnStarterCardIDs))
	for _, card := range state.Cards {
		for _, starterCardID := range cnStarterCardIDs {
			if card.CardID == starterCardID {
				if _, duplicate := startersByCardID[card.CardID]; !duplicate {
					startersByCardID[card.CardID] = card
				}
				break
			}
		}
	}
	if len(startersByCardID) != len(cnStarterCardIDs) {
		return errors.New("CN onboarding seed does not contain the ten starter cards")
	}
	state.Cards = make([]release.Card, 0, len(cnStarterCardIDs))
	starterUniqueIDByCardID := make(map[int]int64, len(cnStarterCardIDs))
	for _, cardID := range cnStarterCardIDs {
		card := startersByCardID[cardID]
		state.Cards = append(state.Cards, card)
		starterUniqueIDByCardID[cardID] = card.UniqueID
	}
	state.ContainerCards = []release.Card{}
	state.StackCards = []release.CardStack{}
	state.Spheres = []release.Sphere{}
	state.BattleLoadoutCardUniqueIDs = make([]int64, 0, len(state.Cards))
	for _, card := range state.Cards {
		state.BattleLoadoutCardUniqueIDs = append(state.BattleLoadoutCardUniqueIDs, card.UniqueID)
	}

	decksByArthur := make(map[int8]release.Deck, 4)
	for _, deck := range state.Decks {
		if deck.ArthurType >= 1 && deck.ArthurType <= 4 {
			if _, exists := decksByArthur[deck.ArthurType]; !exists || deck.IsActive != 0 {
				decksByArthur[deck.ArthurType] = deck
			}
		}
	}
	state.Decks = make([]release.Deck, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		deck, exists := decksByArthur[arthurType]
		if !exists {
			return fmt.Errorf("CN onboarding seed has no Arthur type %d deck", arthurType)
		}
		leaderCardID := cnStarterLeaderCardIDByArthur[arthurType]
		deck.ArthurType = arthurType
		deck.JobType = arthurType
		deck.Index = 0
		deck.LeaderCardIndex = 0
		deck.CardUniqueIDs = []int64{starterUniqueIDByCardID[leaderCardID]}
		for _, cardID := range cnStarterCardIDs {
			if cardID != leaderCardID {
				deck.CardUniqueIDs = append(deck.CardUniqueIDs, starterUniqueIDByCardID[cardID])
			}
		}
		deck.SupportCardUniqueIDs = make([]int64, len(deck.SupportCardUniqueIDs))
		deck.SphereUniqueIDs = []int64{}
		deck.BuddyUniqueIDs = make([]int64, 5)
		deck.Name = cnDefaultDeckNameByArthur[arthurType]
		deck.IsActive = 1
		deck.IsRental = 0
		state.Decks = append(state.Decks, deck)
	}

	state.User.UserID = userID
	state.User.InviteID = cnInviteID(userID)
	state.User.Name = ""
	state.User.Comment = "请多关照！"
	state.User.NaviID = 0 // Original CN navi.csv: 妖精乌莎哈 (uathach); 1 is 妮妙.
	state.User.NaviUnlockFlag = 1
	state.User.SelectableNaviIDs = []int8{0}
	state.User.ActiveArthurType = 1
	state.User.LeaderCardUniqueID = starterUniqueIDByCardID[cnStarterLeaderCardIDByArthur[1]]
	state.User.LeaderCardID = cnStarterLeaderCardIDByArthur[1]
	state.User.Gold = 0
	state.User.FriendPoint = 0
	state.User.Coin = 0
	state.User.CoinFree = 0
	state.User.PVPPoint = 0
	state.User.TutorialFlag = 0
	state.User.ArthurRank = 0
	state.User.LastHomeDeckRank = 0
	state.User.CardMax = release.CardCapacityLimit
	state.Buddy = release.Buddy{}
	state.Buddies = []release.Buddy{}
	// The profession bit is finalized by UserCreate after the player chooses one
	// Arthur.  Do not pre-open the other three professions on a clean account.
	state.User.UnlockedFeatureIDs = []uint{4, 5, 6, 7, 12}
	state.Onboarding = release.OnboardingState{ConfigVersion: cnOnboardingConfigVersion}

	state.Items = []release.Item{
		{ItemID: 1000}, {ItemID: 1200}, {ItemID: 2000}, {ItemID: 2001},
	}
	state.Engagement = release.EngagementState{
		Initialized:  true,
		Missions:     []release.Mission{},
		Presents:     []release.Present{},
		Histories:    []release.Present{},
		PopupReadIDs: []int{},
	}
	state.GachaSelections = []release.GachaSelection{}
	state.GachaDailyClaims = []release.GachaDailyClaim{}
	state.EventShopPurchases = []release.EventShopPurchase{}
	state.TradeShopPurchases = []release.TradeShopPurchase{}
	state.CardDevelopment = release.CardDevelopmentState{}
	state.SupportDeck.UnlockSlotNums = []int8{0, 0, 0, 0}
	state.SupportDeck.CardCollectionIDs = append([]int(nil), cnStarterCardIDs...)
	state.SupportDeck.CardCollectionLoveMaxIDs = nil
	state.TeamBattleResultReceipts = []release.TeamBattleResultReceipt{}
	state.TeamBattleStartReceipts = nil
	state.TeamBattleContinueReceipts = nil
	state.TeamBattleSoloResultReceipts = []release.TeamBattleSoloResultReceipt{}
	state.ExploreResultReceipt = nil
	state.PVPResultReceipts = []release.PVPResultReceipt{}
	state.ActiveTeamBattle = nil
	state.PVP.ActiveMatch = nil
	state.PVP.History = []release.PVPMatch{}
	state.PVP.DefenseDecks = []release.PVPDeckSelection{}
	state.FriendPointInboxCursor = 0
	state.TeamBattleScores = nil
	state.TowerQuestProgress = nil
	state.TowerQuestConfigVersion = 0
	progress, err := collectCNCatalogProgress(*state)
	if err != nil {
		return err
	}
	// QA seed clear flags and reward claims must not become new-player progress.
	for i := range progress.Areas {
		area := &progress.Areas[i]
		area.StageClear, area.NewClearStage = []byte("[]"), []byte("[]")
		for j := range area.StageQuest.Stages {
			stage := &area.StageQuest.Stages[j]
			stage.IsClearDone = 0
			for k := range stage.Raids {
				raid := &stage.Raids[k]
				for n := range raid.BossGroup.Bosses {
					raid.BossGroup.Bosses[n].State = 0
				}
				for n := range raid.ClearRewards {
					raid.ClearRewards[n].IsAlready = 0
				}
			}
		}
	}
	if err := (cnCatalogProgress{Areas: progress.Areas}).apply(state); err != nil {
		return err
	}

	if err := installCNOnboardingGacha(state); err != nil {
		return err
	}
	return nil
}

// migrateCNDefaultDeckNames removes an early local-server marker only from
// untouched generated deck names. Player-renamed decks are never rewritten.
func migrateCNDefaultDeckNames(state *release.State) bool {
	if state == nil {
		return false
	}
	changed := false
	for index := range state.Decks {
		deck := &state.Decks[index]
		if deck.Name != cnLegacyLocalDeckNameByArthur[deck.ArthurType] {
			continue
		}
		deck.Name = cnDefaultDeckNameByArthur[deck.ArthurType]
		changed = true
	}
	return changed
}

func installCNOnboardingGacha(state *release.State) error {
	var singleSource *release.GachaProfile
	var multiSource *release.GachaProfile
	playCounts := make(map[int]int, 3)
	for _, gacha := range state.Gachas {
		playCounts[gacha.GachaID] = gacha.PlayCount
	}
	if legacyCount, exists := playCounts[cnLegacyOnboardingGachaID]; exists {
		playCounts[cnOnboardingGachaID] = legacyCount
	}
	gachas := make([]release.GachaProfile, 0, len(state.Gachas)+2)
	for index := range state.Gachas {
		gacha := state.Gachas[index]
		if gacha.GachaID == 60200201 {
			copy := gacha
			singleSource = &copy
		}
		if gacha.GachaID == 60200212 {
			copy := gacha
			multiSource = &copy
		}
		if gacha.GachaID != cnOnboardingGachaID &&
			gacha.GachaID != cnOnboardingMultiGachaID &&
			gacha.GachaID != cnLegacyOnboardingGachaID {
			gachas = append(gachas, gacha)
		}
	}
	if singleSource == nil || multiSource == nil {
		return errors.New("CN onboarding source gachas are unavailable")
	}
	tutorial := *singleSource
	tutorial.GachaID = cnOnboardingGachaID
	tutorial.BannerKey = "local_first"
	tutorial.GroupID = cnOnboardingGachaID
	tutorial.Name = "首次扭蛋（仅限一次）"
	tutorial.BuyMessage = "使用新手扭蛋券抽取1张骑士卡牌吗？"
	tutorial.SubMessage = "完成『最初的一步』后开放"
	tutorial.OrderNum = 0
	tutorial.PayType = 4
	tutorial.PayTypeID = 2001
	tutorial.Price = 1
	tutorial.CardNum = 1
	tutorial.CardNumMax = 1
	tutorial.UserSelectMax = 0
	tutorial.DailyFirstFree = false
	tutorial.DailyFirstFreeSourceState = ""
	tutorial.PlayCount = playCounts[cnOnboardingGachaID]
	tutorial.GuaranteedRarityRank = 0
	tutorial.GuaranteedCount = 0
	tutorial.RemainderRarityRank = 0
	tutorial.ResultPolicySourceState = ""
	tutorial.CardIDs = []int{10002001}
	tutorial.CardWeights = []int{1}
	tutorial.PoolSourceState = "CONFIRMED_ORIGINAL_CN_TUTORIAL_FIXED_CARD"
	tutorial.WeightSourceState = "CONFIRMED_SINGLE_FIXED_RESULT"
	gachas = append(gachas, tutorial)

	// CONFIRMED: the official CN client reserves 90000100..90000103 for its
	// post-training first multi-draw scene and only records completion when the
	// profile publishes at least eleven results.  The exact historical pool is
	// absent from the retained service data, so the official CN general pool is
	// reused and the screenshot-backed one-UR/remainder-SR split is explicit.
	multi := *multiSource
	multi.GachaID = cnOnboardingMultiGachaID
	multi.BannerKey = "local_first_multi"
	multi.GroupID = cnOnboardingMultiGachaID
	multi.Name = "新手11连扭蛋（仅限一次）"
	multi.BuyMessage = "使用400个水晶进行新手11连扭蛋吗？"
	multi.SubMessage = "通关『大地起源』后开放"
	multi.OrderNum = 0
	multi.PayType = 3
	multi.PayTypeID = 0
	multi.Price = 400
	multi.CardNum = 11
	multi.CardNumMax = 11
	multi.UserSelectMax = 0
	multi.DailyFirstFree = false
	multi.DailyFirstFreeSourceState = ""
	multi.PlayCount = playCounts[cnOnboardingMultiGachaID]
	multi.GuaranteedRarityRank = 5
	multi.GuaranteedCount = 1
	multi.RemainderRarityRank = 4
	multi.ResultPolicySourceState = "INFERRED_USER_SCREENSHOT_AND_CONFIRMED_CN_CLIENT_FAMILY"
	multi.PoolSourceState = "INFERRED_OFFICIAL_CN_ACQUISITION_IDENTITY"
	multi.WeightSourceState = "INFERRED_ARCHIVED_CN_40_57_3_BUCKETS_LOCAL_POLICY"
	gachas = append(gachas, multi)
	sort.SliceStable(gachas, func(left, right int) bool {
		if gachas[left].CategoryNum != gachas[right].CategoryNum {
			return gachas[left].CategoryNum < gachas[right].CategoryNum
		}
		if gachas[left].OrderNum != gachas[right].OrderNum {
			return gachas[left].OrderNum < gachas[right].OrderNum
		}
		return gachas[left].GachaID < gachas[right].GachaID
	})
	state.Gachas = gachas
	return nil
}

// migrateCNOnboardingState restores the two client-native steps that the
// first local profile skipped after deck editing.  Completed legacy accounts
// stay complete; an account inside the old Explore/Story tail returns to the
// first missing original objective so it cannot silently skip the sequence.
func migrateCNOnboardingState(state *release.State) (bool, error) {
	if state == nil || state.Onboarding.ConfigVersion == 0 ||
		state.Onboarding.ConfigVersion == cnOnboardingConfigVersion {
		return false, nil
	}
	if state.Onboarding.ConfigVersion != cnLegacyOnboardingConfigVersion ||
		state.Onboarding.Step < 0 || state.Onboarding.Step > 7 {
		return false, errors.New("CN legacy onboarding state is invalid")
	}
	switch {
	case state.Onboarding.Step == 7:
		state.Onboarding.Step = cnOnboardingStepCount
		state.Onboarding.CurrentAnnounced = false
		state.Onboarding.PendingClearQuestID = 0
	case state.Onboarding.Step >= 4:
		state.Onboarding.Step = 4
		state.Onboarding.CurrentAnnounced = false
		if state.Onboarding.PendingClearQuestID < 1041 ||
			state.Onboarding.PendingClearQuestID > 1044 {
			state.Onboarding.PendingClearQuestID = 0
		}
	}
	state.Onboarding.ConfigVersion = cnOnboardingConfigVersion
	if err := installCNOnboardingGacha(state); err != nil {
		return false, err
	}
	return true, nil
}

func migrateCNOnboardingGachaID(state *release.State) (bool, error) {
	if state == nil {
		return false, nil
	}
	foundSingle := false
	foundMulti := false
	for _, gacha := range state.Gachas {
		switch gacha.GachaID {
		case cnLegacyOnboardingGachaID:
			if err := installCNOnboardingGacha(state); err != nil {
				return false, err
			}
			return true, nil
		case cnOnboardingGachaID:
			foundSingle = true
		case cnOnboardingMultiGachaID:
			foundMulti = true
		}
	}
	if state.Onboarding.ConfigVersion != cnOnboardingConfigVersion {
		return false, nil
	}
	if !foundSingle || !foundMulti {
		if err := installCNOnboardingGacha(state); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
