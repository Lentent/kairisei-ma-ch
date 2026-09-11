package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kairisei.local/server/internal/release"
)

const (
	stampDeckSlots      = 12
	deckSphereSlots     = 3
	deckBuddySlots      = 5
	userCommentMaxBytes = 180
	maxBattleReceipts   = 32
	maxLoveUpItemKinds  = 10
)

type cardLoveRule struct {
	Max     int
	Premium bool
}

type cardDevelopmentRule struct {
	FameMax         int
	DevelopmentType int
	DecomposeRadix  int
	DevelopRadix    int
	AcquisitionText string
}

type store struct {
	teamBattleScores            map[int]release.TeamBattleScoreProgress
	localShop                   release.LocalShopState
	localCrystalPurchaseEnabled bool
	mu                          sync.RWMutex
	cards                       []cardInfo
	containerCards              []cardInfo
	stackCards                  []release.CardStack
	stackCardTemplates          map[int]release.CardStack
	spheres                     []release.Sphere
	sphereDefinitions           map[int]release.SphereDefinition
	sphereExperience            map[int][]int
	sphereEvoPrices             map[string][]int
	sphereProgression           release.SphereProgressionPolicy
	sphereMax                   int
	buddies                     []release.Buddy
	buddyDefinitions            map[int]release.BuddyDefinition
	buddyExperience             map[int][]int
	buddyEvoPrices              map[string][]int
	buddyProgression            release.BuddyProgressionPolicy
	buddyMax                    int
	decks                       []deckInfo
	avatars                     []release.Avatar
	avatarParts                 map[int]struct{}
	avatarDefinitions           map[int]release.AvatarPartDefinition
	avatarShopParts             map[int]struct{}
	avatarDefaultDecks          [][]int
	avatarCompletions           []release.AvatarSeriesCompletion
	avatarShopPolicy            release.AvatarShopPolicy
	cardTemplates               map[int]cardInfo
	cardDefinitions             map[int]release.Card
	cardCategoryProfiles        []release.CardCategoryProfile
	cardGroupProfiles           []release.CardGroupProfile
	cardExperience              map[int][]int
	cardProgression             release.CardProgressionPolicy
	deckRankPolicy              release.DeckRankPolicy
	highestDeckRank             int
	lastHomeDeckRank            int
	cardLoveRules               map[int]cardLoveRule
	cardDevelopmentRules        map[int]cardDevelopmentRule
	cardDevelopmentPolicy       release.CardDevelopmentPolicy
	stive                       int
	cardFameTraining            *release.CardFameTraining
	cardMax                     int
	cardContainerMax            int
	deckSlots                   int
	supportSlotCapacity         int
	supportDeckSetCardNum       int
	supportDeckRules            []release.SupportDeckSlotUnlockRule
	supportUnlockedSlots        []int8
	cardCollectionIDs           map[int]struct{}
	cardCollectionLoveMaxIDs    map[int]struct{}
	cardCollectionPages         [][10]int
	buddySlots                  int
	missions                    []release.Mission
	presents                    []release.Present
	presentHistories            []release.Present
	popupReadIDs                map[int]struct{}
	loginBonusPolicy            release.LoginBonusPolicy
	tutorialCompletionMail      TutorialCompletionMail
	loginBonusState             release.LoginBonusState
	nextUniqueID                int64
	nextSphereUniqueID          int64
	nextBuddyUniqueID           int64
	currentNaviID               int8
	naviUnlockFlag              int64
	selectableNaviIDs           map[int8]struct{}
	naviCatalogIDs              map[int8]struct{}
	naviPurchasePrice           int
	naviSettings                map[int8]NaviSetting
	stampIDs                    map[int]struct{}
	stampDeck                   []int
	costumeIDs                  map[int]struct{}
	currentActiveArthur         int8
	currentLevel                int
	currentExperience           int
	currentLevelExperience      int
	nextLevelExperience         int
	currentJobs                 []release.JobParameter
	currentFriendMax            int
	playerProgression           release.PlayerProgressionPolicy
	currentName                 string
	currentComment              string
	honorIDs                    map[int]struct{}
	deckHonorIDs                []int
	gold                        int
	friendPoint                 int
	friendPointInboxCursor      int64
	coin                        int
	coinFree                    int
	pvpPoint                    int
	pvp                         release.PVPPlayerState
	items                       map[int]release.Item
	itemDefinitions             map[int]release.ItemDefinition
	userBuffProfiles            map[int]release.UserBuffProfile
	itemGachaProfiles           map[int]release.ItemGachaProfile
	itemExchangeProfiles        map[int]release.ItemExchangeProfile
	itemLackTipProfiles         map[int]release.ItemLackTipProfile
	eventShopProfiles           map[int]release.EventShopProfile
	eventShopPurchases          map[int]int
	tradeShopProfiles           map[int]release.TradeShopProfile
	tradeShopPurchases          map[int]int
	itemShopTabs                []release.ItemShopTab
	itemShopSettings            map[int]ItemShopSetting
	gachas                      []release.GachaProfile
	gachaWindows                map[int][2]int64
	gachaSelections             map[int][]release.Reward
	gachaDailyClaims            map[int]string
	bp                          int
	bpMax                       int
	bpRecoveryInterval          time.Duration
	bpNextRecovery              time.Time
	gameOptionFlag              int
	pushOptionFlag              int
	tutorialFlag                int64
	unlockedFeatureIDs          map[uint]struct{}
	onboarding                  release.OnboardingState
	cardActions                 release.CardActionState
	ap                          int
	apMax                       int
	apRecoveryInterval          time.Duration
	apNextRecovery              time.Time
	exploreActive               bool
	exploreArthurType           int8
	exploreDeckIndex            int8
	exploreStartedAt            time.Time
	exploreStages               []release.ExploreStage
	exploreStageCursor          int
	exploreActiveStage          int
	friends                     []release.Friend
	followMax                   int
	storyMainParts              []release.StoryMainPart
	cnStoryMainParts            []release.StoryMainPart
	storySubCharacters          []release.StorySubCharacter
	storyEvents                 []release.StoryEvent
	storyRewardPolicy           release.StoryRewardPolicy
	storyBattleIDsByStory       map[string]map[int]struct{}
	activeMainStoryID           int
	activeMainStoryCN           bool
	activeSubStoryID            int
	burstProgress               [4]uint8
	storyTeamBattleSession      release.StoryTeamBattleSession
	mainQuest                   json.RawMessage
	stageQuests                 map[int]json.RawMessage
	defaultStageQuestAreaID     int
	teamBattleSolo              json.RawMessage
	teamBattleLimitedGroupIDs   []int
	teamBattleScheduleSoloPush  map[int]struct{}
	teamBattleScheduleMultiPush map[int]struct{}
	teamBattleReceipts          map[int64]release.TeamBattleResultReceipt
	teamBattleStartReceipts     []release.TeamBattleStartReceipt
	teamBattleContinueReceipts  []release.TeamBattleContinueReceipt
	teamBattleSoloReceipts      map[string]release.TeamBattleSoloResultReceipt
	lastExploreResultReceipt    *release.ExploreResultReceipt
	pvpResultReceipts           map[int]release.PVPResultReceipt
	teamBattleFameBonus         release.TeamBattleFameBonusPolicy
	teamBattleHostBonus         release.TeamBattleHostBonusPolicy
	towerQuestProfiles          map[int]release.TowerQuestProfile
	towerQuestProgress          map[int]release.TowerQuestProgress
	pendingStageAreaID          int
	activeBattle                *teamBattleContext
	initialStateRepair          bool
}

type teamBattleContext struct {
	Seed                      int
	DropPlanSet               bool
	DropPlan                  []release.TeamBattleEnemyDrop
	BossID                    int
	BattleEnemyTypes          []int8
	StageQuestAreaID          int
	StageQuestStageID         int
	TowerID                   int
	TowerFloor                int
	ItemID                    int
	ItemUse                   int
	BPUse                     int
	ConsumesBattlePoints      bool
	PrepaidRoomID             int64
	FameSeed                  string
	FameSources               []teamBattleFameSource
	HostBonusArthurType       int
	FriendPointPartners       int
	FriendPointReward         int
	FriendPointRentalCredits  []FriendPointRentalCredit
	FriendPointRentalEventKey string
	SelectedPartners          []teamBattleResultPartner
	ContinueAllowed           bool
	ContinueReceipts          []string
}

type teamBattleResultPartner struct {
	IsBurst       int8
	LastLoginUnix int64
	UserID        int
	IsSelf        bool
	Name          string
	ArthurType    int8
	Level         int
	DeckRank      int8
	LeaderCardID  int
	LeaderLevel   int
	LeaderFame    int
	Comment       string
	PVPPoint      int
	HonorIDs      []int
}

func teamBattleContextFromRelease(source *release.TeamBattleActiveState) *teamBattleContext {
	if source == nil {
		return nil
	}
	result := &teamBattleContext{
		Seed:                      source.Seed,
		DropPlanSet:               source.DropPlanSet,
		DropPlan:                  cloneTeamBattleDropPlan(source.DropPlan),
		BossID:                    source.BossID,
		BattleEnemyTypes:          append([]int8(nil), source.BattleEnemyTypes...),
		StageQuestAreaID:          source.StageQuestAreaID,
		StageQuestStageID:         source.StageQuestStageID,
		TowerID:                   source.TowerID,
		TowerFloor:                source.TowerFloor,
		ItemID:                    source.ItemID,
		ItemUse:                   source.ItemUse,
		BPUse:                     source.BPUse,
		ConsumesBattlePoints:      source.ConsumesBattlePoints,
		PrepaidRoomID:             source.PrepaidRoomID,
		FameSeed:                  source.FameSeed,
		HostBonusArthurType:       source.HostBonusArthurType,
		FriendPointPartners:       source.FriendPointPartners,
		FriendPointReward:         source.FriendPointReward,
		FriendPointRentalEventKey: source.FriendPointRentalEventKey,
		ContinueAllowed:           source.ContinueAllowed,
		ContinueReceipts:          append([]string(nil), source.ContinueReceipts...),
	}
	result.FameSources = make([]teamBattleFameSource, len(source.FameSources))
	for index, fame := range source.FameSources {
		result.FameSources[index] = teamBattleFameSource{
			ArthurType: fame.ArthurType, LeaderFame: fame.LeaderFame,
		}
	}
	result.FriendPointRentalCredits = make([]FriendPointRentalCredit, len(source.FriendPointRentalCredits))
	for index, credit := range source.FriendPointRentalCredits {
		result.FriendPointRentalCredits[index] = FriendPointRentalCredit{
			OwnerUserID: credit.OwnerUserID, FriendPoint: credit.FriendPoint,
		}
	}
	result.SelectedPartners = make([]teamBattleResultPartner, len(source.SelectedPartners))
	for index, partner := range source.SelectedPartners {
		result.SelectedPartners[index] = teamBattleResultPartner{
			IsBurst:       partner.IsBurst,
			LastLoginUnix: partner.LastLoginUnix,
			UserID:        partner.UserID, IsSelf: partner.IsSelf, Name: partner.Name, ArthurType: partner.ArthurType,
			Level: partner.Level, DeckRank: partner.DeckRank, LeaderCardID: partner.LeaderCardID,
			LeaderLevel: partner.LeaderLevel, LeaderFame: partner.LeaderFame,
			Comment: partner.Comment, PVPPoint: partner.PVPPoint,
			HonorIDs: append([]int(nil), partner.HonorIDs...),
		}
	}
	return result
}

func releaseTeamBattleContext(source *teamBattleContext) *release.TeamBattleActiveState {
	if source == nil {
		return nil
	}
	result := &release.TeamBattleActiveState{
		Seed:                      source.Seed,
		DropPlanSet:               source.DropPlanSet,
		DropPlan:                  cloneTeamBattleDropPlan(source.DropPlan),
		BossID:                    source.BossID,
		BattleEnemyTypes:          append([]int8(nil), source.BattleEnemyTypes...),
		StageQuestAreaID:          source.StageQuestAreaID,
		StageQuestStageID:         source.StageQuestStageID,
		TowerID:                   source.TowerID,
		TowerFloor:                source.TowerFloor,
		ItemID:                    source.ItemID,
		ItemUse:                   source.ItemUse,
		BPUse:                     source.BPUse,
		ConsumesBattlePoints:      source.ConsumesBattlePoints,
		PrepaidRoomID:             source.PrepaidRoomID,
		FameSeed:                  source.FameSeed,
		HostBonusArthurType:       source.HostBonusArthurType,
		FriendPointPartners:       source.FriendPointPartners,
		FriendPointReward:         source.FriendPointReward,
		FriendPointRentalEventKey: source.FriendPointRentalEventKey,
		ContinueAllowed:           source.ContinueAllowed,
		ContinueReceipts:          append([]string(nil), source.ContinueReceipts...),
	}
	result.FameSources = make([]release.TeamBattleFameSourceState, len(source.FameSources))
	for index, fame := range source.FameSources {
		result.FameSources[index] = release.TeamBattleFameSourceState{
			ArthurType: fame.ArthurType, LeaderFame: fame.LeaderFame,
		}
	}
	result.FriendPointRentalCredits = make(
		[]release.TeamBattleRentalCreditState, len(source.FriendPointRentalCredits),
	)
	for index, credit := range source.FriendPointRentalCredits {
		result.FriendPointRentalCredits[index] = release.TeamBattleRentalCreditState{
			OwnerUserID: credit.OwnerUserID, FriendPoint: credit.FriendPoint,
		}
	}
	result.SelectedPartners = make([]release.TeamBattleResultPartnerState, len(source.SelectedPartners))
	for index, partner := range source.SelectedPartners {
		result.SelectedPartners[index] = release.TeamBattleResultPartnerState{
			IsBurst:       partner.IsBurst,
			LastLoginUnix: partner.LastLoginUnix,
			UserID:        partner.UserID, IsSelf: partner.IsSelf, Name: partner.Name, ArthurType: partner.ArthurType,
			Level: partner.Level, DeckRank: partner.DeckRank, LeaderCardID: partner.LeaderCardID,
			LeaderLevel: partner.LeaderLevel, LeaderFame: partner.LeaderFame,
			Comment: partner.Comment, PVPPoint: partner.PVPPoint,
			HonorIDs: append([]int(nil), partner.HonorIDs...),
		}
	}
	return result
}

type teamBattleFameSource struct {
	ArthurType int
	LeaderFame int
}

type teamBattleFameAward struct {
	ArthurType int
	RewardKind int
	Result     presentReceiveResult
}

type teamBattleFameAwardPlan struct {
	ArthurType int
	RewardKind int
	Reward     release.Reward
}

type teamBattleSettlement struct {
	Score         presentReceiveResult
	ScoreInfo     []any
	Context       teamBattleContext
	Result        presentReceiveResult
	FirstClear    presentReceiveResult
	Fame          []teamBattleFameAward
	Host          []teamBattleFameAward
	WasFirstClear bool
}

func cardInfosFromRelease(cards []release.Card, slot int) []cardInfo {
	result := make([]cardInfo, len(cards))
	for index, card := range cards {
		result[index] = cardInfo{
			UniqueID:      card.UniqueID,
			CardID:        card.CardID,
			Level:         card.Level,
			LevelMax:      card.LevelMax,
			Experience:    card.Experience,
			NowLevelEXP:   card.NowLevelExperience,
			Love:          card.Love,
			LoveMax:       card.LoveMax,
			SkillLevels:   append([]int16(nil), card.SkillLevels...),
			HP:            card.HP,
			Attack:        card.Attack,
			Magic:         card.Magic,
			Mind:          card.Mind,
			NextLevelEXP:  card.NextLevelExperience,
			AddExperience: card.AddExperience,
			BaseAddPrice:  card.BaseAddPrice,
			IsLock:        card.IsLock,
			Fame:          card.Fame,
			Slot:          slot,
		}
	}
	return result
}

func cloneTeamBattleFameBonusPolicy(
	policy release.TeamBattleFameBonusPolicy,
) release.TeamBattleFameBonusPolicy {
	policy.EligibleRewardTypes = append([]int(nil), policy.EligibleRewardTypes...)
	clonedSourceState := make(map[string]string, len(policy.SourceState))
	for key, value := range policy.SourceState {
		clonedSourceState[key] = value
	}
	policy.SourceState = clonedSourceState
	return policy
}

func cloneTeamBattleHostBonusPolicy(
	policy release.TeamBattleHostBonusPolicy,
) release.TeamBattleHostBonusPolicy {
	policy.EligibleRewardTypes = append([]int(nil), policy.EligibleRewardTypes...)
	clonedSourceState := make(map[string]string, len(policy.SourceState))
	for key, value := range policy.SourceState {
		clonedSourceState[key] = value
	}
	policy.SourceState = clonedSourceState
	return policy
}

func cloneLoginBonusPolicy(policy release.LoginBonusPolicy) release.LoginBonusPolicy {
	policy.Cycle = cloneLoginBonusSchedule(policy.Cycle)
	policy.Beginner = cloneLoginBonusSchedule(policy.Beginner)
	policy.TotalMilestones = cloneLoginBonusSchedule(policy.TotalMilestones)
	clonedSourceState := make(map[string]string, len(policy.SourceState))
	for key, value := range policy.SourceState {
		clonedSourceState[key] = value
	}
	policy.SourceState = clonedSourceState
	return policy
}

func cloneStoryRewardPolicy(policy release.StoryRewardPolicy) release.StoryRewardPolicy {
	policy.MainFirstClear = cloneReward(policy.MainFirstClear)
	policy.SubFirstClear = cloneReward(policy.SubFirstClear)
	policy.EventFirstClear = cloneReward(policy.EventFirstClear)
	clonedSourceState := make(map[string]string, len(policy.SourceState))
	for key, value := range policy.SourceState {
		clonedSourceState[key] = value
	}
	policy.SourceState = clonedSourceState
	return policy
}

func cloneLoginBonusSchedule(schedule []release.LoginBonusDay) []release.LoginBonusDay {
	cloned := append([]release.LoginBonusDay(nil), schedule...)
	for index := range cloned {
		cloned[index].Reward = cloneReward(cloned[index].Reward)
	}
	return cloned
}

func newStore(state release.State) (*store, error) {
	if state.PlayerProgressionPolicy.ConfigVersion > 0 {
		policy := state.PlayerProgressionPolicy
		if state.PlayerProgressionConfigVersion != policy.ConfigVersion ||
			state.User.Level < 1 || state.User.Level > policy.MaxLevel ||
			state.User.Experience < 0 || state.User.NowLevelExperience < 0 ||
			state.User.BPMax != policy.BattlePointMaximum(state.User.Level) ||
			state.User.FriendMax != policy.FriendMaximum(state.User.Level) ||
			!slices.Equal(state.User.Jobs, policy.JobsAtLevel(state.User.Level)) {
			return nil, errors.New("card store player progression state is invalid")
		}
		cumulative := policy.CumulativeExperience(state.User.Level)
		if state.User.Level == policy.MaxLevel {
			if state.User.Experience != cumulative || state.User.NowLevelExperience != 0 ||
				state.User.NextLevelExperience != 0 {
				return nil, errors.New("card store maximum-level progression state is invalid")
			}
		} else {
			required := policy.ExperienceRequired(state.User.Level)
			if required <= 0 || state.User.NowLevelExperience >= required ||
				state.User.Experience != cumulative+state.User.NowLevelExperience ||
				state.User.NextLevelExperience != required-state.User.NowLevelExperience {
				return nil, errors.New("card store player EXP state is invalid")
			}
		}
	}
	if state.CardProgressionConfigVersion <= 0 ||
		state.CardProgressionPolicy.ConfigVersion != state.CardProgressionConfigVersion ||
		state.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel <= 0 ||
		len(state.CardExperienceTables) == 0 {
		return nil, errors.New("card store card progression state is invalid")
	}
	if err := validateCardFusionSuccessPolicy(state.CardProgressionPolicy); err != nil {
		return nil, err
	}
	cards := cardInfosFromRelease(state.Cards, 0)
	containerCards := cardInfosFromRelease(state.ContainerCards, 1)
	decks := deckInfosFromRelease(state.Decks)
	if len(cards) == 0 || len(decks) == 0 {
		return nil, errors.New("card store requires cards and decks")
	}
	if state.User.CardMax <= 0 || state.User.CardContainerMax <= 0 ||
		len(cards) > state.User.CardMax || len(containerCards) > state.User.CardContainerMax {
		return nil, errors.New("card store inventory capacity is invalid")
	}
	cardUniqueIDs := make(map[int64]struct{}, len(cards)+len(containerCards))
	for _, inventory := range [][]cardInfo{cards, containerCards} {
		for _, card := range inventory {
			if card.UniqueID <= 0 || card.CardID <= 0 || card.IsLock < 0 || card.IsLock > 1 {
				return nil, errors.New("card store contains an invalid card")
			}
			if _, duplicate := cardUniqueIDs[card.UniqueID]; duplicate {
				return nil, fmt.Errorf("card store repeats unique ID %d", card.UniqueID)
			}
			cardUniqueIDs[card.UniqueID] = struct{}{}
		}
	}
	deckSlots := len(decks[0].CardUniqueIDs)
	supportSlotCapacity := len(decks[0].SupportCardUniqueIDs)
	buddySlots := len(decks[0].BuddyUniqueIDs)
	if deckSlots == 0 || supportSlotCapacity == 0 || buddySlots == 0 {
		return nil, errors.New("card store deck shape is empty")
	}
	if state.SupportDeckSetCardNum <= 0 ||
		state.SupportDeckSetCardNum > supportSlotCapacity ||
		len(state.SupportDeckSlotUnlockRules) != state.SupportDeckSetCardNum ||
		len(state.SupportDeck.UnlockSlotNums) != 4 {
		return nil, errors.New("card store support-deck configuration is incomplete")
	}
	for index, rule := range state.SupportDeckSlotUnlockRules {
		if rule.SlotIndex != index+1 || rule.Level <= 0 ||
			rule.CardCollectionNum <= 0 || rule.Gold <= 0 {
			return nil, errors.New("card store support-deck slot rule is invalid")
		}
	}
	cardCollectionIDs := make(map[int]struct{}, len(state.SupportDeck.CardCollectionIDs))
	for _, progress := range state.BurstProgress {
		if progress > 3 {
			return nil, errors.New("card store sword-release progress is invalid")
		}
	}
	for _, cardID := range state.SupportDeck.CardCollectionIDs {
		if cardID <= 0 {
			return nil, errors.New("card store collection contains an invalid card ID")
		}
		cardCollectionIDs[cardID] = struct{}{}
	}
	var costume struct {
		CostumeIDs []int `json:"costumeids"`
	}
	if err := json.Unmarshal(state.Costume, &costume); err != nil ||
		len(costume.CostumeIDs) == 0 {
		return nil, errors.New("card store requires costume IDs")
	}
	exploreStages := state.Explore.Stages
	if len(exploreStages) == 0 {
		exploreStages = []release.ExploreStage{state.Explore.Stage}
	}
	stageQuests, defaultStageQuestAreaID, err := stageQuestConfigurations(
		state.MainQuest,
		state.StageQuestAreas,
	)
	if err != nil {
		return nil, err
	}
	result := &store{
		cards:                       cards,
		containerCards:              containerCards,
		stackCards:                  append([]release.CardStack(nil), state.StackCards...),
		stackCardTemplates:          make(map[int]release.CardStack, len(state.StackCardTemplates)+len(state.StackCards)),
		spheres:                     append([]release.Sphere{}, state.Spheres...),
		sphereDefinitions:           make(map[int]release.SphereDefinition, len(state.SphereDefinitions)),
		sphereExperience:            cloneSphereExperienceTables(state.SphereExperienceTables),
		sphereEvoPrices:             cloneSphereEvolutionPrices(state.SphereEvolutionPrices),
		sphereProgression:           state.SphereProgressionPolicy,
		sphereMax:                   state.User.SphereMax,
		buddies:                     append([]release.Buddy{}, state.Buddies...),
		buddyDefinitions:            make(map[int]release.BuddyDefinition, len(state.BuddyDefinitions)),
		buddyExperience:             cloneBuddyExperienceTables(state.BuddyExperienceTables),
		buddyEvoPrices:              cloneBuddyEvolutionPrices(state.BuddyEvolutionPrices),
		buddyProgression:            state.BuddyProgressionPolicy,
		buddyMax:                    state.User.BuddyMax,
		decks:                       decks,
		avatars:                     cloneAvatars(state.Avatars),
		avatarParts:                 make(map[int]struct{}, len(state.AvatarParts)),
		avatarDefinitions:           make(map[int]release.AvatarPartDefinition, len(state.AvatarPartDefinitions)),
		avatarShopParts:             make(map[int]struct{}, len(state.AvatarShopPartIDs)),
		avatarDefaultDecks:          cloneIntMatrix(state.AvatarDefaultDecks),
		avatarCompletions:           cloneAvatarCompletions(state.AvatarSeriesCompletions),
		avatarShopPolicy:            state.AvatarShopPolicy,
		cardTemplates:               make(map[int]cardInfo, len(cards)+len(containerCards)+len(state.CardTemplates)),
		cardDefinitions:             make(map[int]release.Card, len(state.CardTemplates)),
		deckRankPolicy:              state.DeckRankPolicy,
		highestDeckRank:             state.User.ArthurRank,
		lastHomeDeckRank:            state.User.LastHomeDeckRank,
		cardCategoryProfiles:        append([]release.CardCategoryProfile(nil), state.CardCategoryProfiles...),
		cardGroupProfiles:           cloneCardGroupProfiles(state.CardGroupProfiles),
		cardExperience:              cloneCardExperienceTables(state.CardExperienceTables),
		cardProgression:             state.CardProgressionPolicy,
		cardLoveRules:               make(map[int]cardLoveRule, len(state.CardTemplates)),
		cardDevelopmentRules:        make(map[int]cardDevelopmentRule, len(state.CardTemplates)),
		cardDevelopmentPolicy:       state.CardDevelopmentPolicy,
		teamBattleFameBonus:         cloneTeamBattleFameBonusPolicy(state.TeamBattleFameBonusPolicy),
		teamBattleHostBonus:         cloneTeamBattleHostBonusPolicy(state.TeamBattleHostBonusPolicy),
		stive:                       state.CardDevelopment.Stive,
		cardMax:                     state.User.CardMax,
		cardContainerMax:            state.User.CardContainerMax,
		deckSlots:                   deckSlots,
		supportSlotCapacity:         supportSlotCapacity,
		supportDeckSetCardNum:       state.SupportDeckSetCardNum,
		supportDeckRules:            append([]release.SupportDeckSlotUnlockRule(nil), state.SupportDeckSlotUnlockRules...),
		supportUnlockedSlots:        append([]int8(nil), state.SupportDeck.UnlockSlotNums...),
		cardCollectionIDs:           cardCollectionIDs,
		cardCollectionPages:         append([][10]int(nil), state.CardCollectionPages...),
		cardCollectionLoveMaxIDs:    make(map[int]struct{}),
		buddySlots:                  buddySlots,
		missions:                    cloneMissions(state.Engagement.Missions),
		presents:                    clonePresents(state.Engagement.Presents),
		presentHistories:            clonePresents(state.Engagement.Histories),
		popupReadIDs:                make(map[int]struct{}, len(state.Engagement.PopupReadIDs)),
		loginBonusPolicy:            cloneLoginBonusPolicy(state.LoginBonusPolicy),
		loginBonusState:             state.LoginBonus,
		currentNaviID:               state.User.NaviID,
		naviUnlockFlag:              state.User.NaviUnlockFlag,
		selectableNaviIDs:           make(map[int8]struct{}),
		naviCatalogIDs:              make(map[int8]struct{}, len(state.User.NaviCatalogIDs)),
		naviPurchasePrice:           state.User.NaviPurchasePrice,
		stampIDs:                    make(map[int]struct{}, len(state.Stamps.StampIDs)),
		stampDeck:                   normalizeStampDeck(state.Stamps.DeckStampIDs),
		costumeIDs:                  make(map[int]struct{}, len(costume.CostumeIDs)),
		currentActiveArthur:         int8(state.User.ActiveArthurType),
		currentLevel:                state.User.Level,
		currentExperience:           state.User.Experience,
		currentLevelExperience:      state.User.NowLevelExperience,
		nextLevelExperience:         state.User.NextLevelExperience,
		currentJobs:                 append([]release.JobParameter(nil), state.User.Jobs...),
		currentFriendMax:            state.User.FriendMax,
		playerProgression:           state.PlayerProgressionPolicy,
		currentName:                 state.User.Name,
		currentComment:              state.User.Comment,
		honorIDs:                    make(map[int]struct{}, len(state.Honors.HonorIDs)),
		deckHonorIDs:                append([]int(nil), state.Honors.DeckHonorIDs...),
		gold:                        state.User.Gold,
		friendPoint:                 state.User.FriendPoint,
		friendPointInboxCursor:      state.FriendPointInboxCursor,
		coin:                        state.User.Coin,
		coinFree:                    state.User.CoinFree,
		pvpPoint:                    state.User.PVPPoint,
		pvp:                         clonePVPState(state.PVP),
		items:                       make(map[int]release.Item, len(state.Items)),
		itemDefinitions:             make(map[int]release.ItemDefinition, len(state.ItemDefinitions)),
		userBuffProfiles:            make(map[int]release.UserBuffProfile, len(state.UserBuffProfiles)),
		itemGachaProfiles:           make(map[int]release.ItemGachaProfile, len(state.ItemGachaProfiles)),
		itemExchangeProfiles:        make(map[int]release.ItemExchangeProfile, len(state.ItemExchangeProfiles)),
		itemLackTipProfiles:         make(map[int]release.ItemLackTipProfile, len(state.ItemLackTipProfiles)),
		eventShopProfiles:           make(map[int]release.EventShopProfile, len(state.EventShopProfiles)),
		eventShopPurchases:          make(map[int]int, len(state.EventShopPurchases)),
		tradeShopProfiles:           make(map[int]release.TradeShopProfile, len(state.TradeShopProfiles)),
		tradeShopPurchases:          make(map[int]int, len(state.TradeShopPurchases)),
		teamBattleScores:            cloneTeamBattleScores(state.TeamBattleScores),
		localShop:                   cloneLocalShop(state.LocalShop),
		itemShopTabs:                cloneItemShopTabs(state.ItemShopTabs),
		gachas:                      cloneGachaProfiles(state.Gachas),
		gachaSelections:             make(map[int][]release.Reward, len(state.GachaSelections)),
		gachaDailyClaims:            make(map[int]string, len(state.GachaDailyClaims)),
		bp:                          state.User.BP,
		bpMax:                       state.User.BPMax,
		bpRecoveryInterval:          time.Duration(state.BattlePoint.RecoverySeconds) * time.Second,
		gameOptionFlag:              state.Options.GameEnableFlag,
		pushOptionFlag:              state.Options.PushEnableFlag,
		tutorialFlag:                state.User.TutorialFlag,
		unlockedFeatureIDs:          make(map[uint]struct{}, len(state.User.UnlockedFeatureIDs)),
		onboarding:                  state.Onboarding,
		cardActions:                 state.CardActions,
		activeMainStoryID:           state.Navigation.MainStoryID,
		activeMainStoryCN:           state.Navigation.MainStoryCN,
		activeSubStoryID:            state.Navigation.SubStoryID,
		pendingStageAreaID:          state.Navigation.StageAreaID,
		ap:                          state.User.AP,
		apMax:                       state.User.APMax,
		apRecoveryInterval:          time.Duration(state.Explore.APRecoverySeconds) * time.Second,
		exploreActive:               state.Explore.Active,
		exploreArthurType:           state.Explore.ArthurType,
		exploreDeckIndex:            state.Explore.DeckIndex,
		exploreStages:               cloneExploreStages(exploreStages),
		exploreStageCursor:          state.Explore.StageCursor,
		exploreActiveStage:          state.Explore.ActiveStageID,
		friends:                     cloneFriends(state.Friends.Users),
		followMax:                   state.Friends.FollowMax,
		storyMainParts:              cloneStoryMainParts(state.Story.MainParts),
		cnStoryMainParts:            cloneStoryMainParts(state.Story.CNMainParts),
		storySubCharacters:          cloneStorySubCharacters(state.Story.SubCharacters),
		storyEvents:                 cloneStoryEvents(state.Story.Events),
		storyRewardPolicy:           cloneStoryRewardPolicy(state.StoryRewardPolicy),
		storyBattleIDsByStory:       make(map[string]map[int]struct{}, len(state.Story.StoryBattleReferences)),
		mainQuest:                   append(json.RawMessage(nil), state.MainQuest...),
		stageQuests:                 stageQuests,
		defaultStageQuestAreaID:     defaultStageQuestAreaID,
		teamBattleSolo:              append(json.RawMessage(nil), state.TeamBattleSolo...),
		teamBattleScheduleSoloPush:  make(map[int]struct{}, len(state.TeamBattleSchedule.SoloPushGroupIDs)),
		teamBattleScheduleMultiPush: make(map[int]struct{}, len(state.TeamBattleSchedule.MultiPushGroupIDs)),
		teamBattleReceipts:          make(map[int64]release.TeamBattleResultReceipt, len(state.TeamBattleResultReceipts)),
		teamBattleSoloReceipts:      make(map[string]release.TeamBattleSoloResultReceipt, len(state.TeamBattleSoloResultReceipts)),
		pvpResultReceipts:           make(map[int]release.PVPResultReceipt, len(state.PVPResultReceipts)),
		towerQuestProfiles:          make(map[int]release.TowerQuestProfile, len(state.TowerQuestProfiles)),
		towerQuestProgress:          make(map[int]release.TowerQuestProgress, len(state.TowerQuestProgress)),
	}
	for _, featureID := range state.User.UnlockedFeatureIDs {
		result.unlockedFeatureIDs[featureID] = struct{}{}
	}
	result.burstProgress = state.BurstProgress
	result.storyTeamBattleSession = state.StoryTeamBattleSession
	result.reconcileOnboardingFeatureUnlocksLocked()
	for _, template := range state.StackCardTemplates {
		if template.CardID <= 0 || template.Num != 0 || template.AddExperience <= 0 || template.BaseAddPrice < 0 {
			return nil, fmt.Errorf("stack-card template %d is invalid", template.CardID)
		}
		if _, duplicate := result.stackCardTemplates[template.CardID]; duplicate {
			return nil, fmt.Errorf("duplicate stack-card template %d", template.CardID)
		}
		result.stackCardTemplates[template.CardID] = template
	}
	for _, inventory := range state.StackCards {
		template, exists := result.stackCardTemplates[inventory.CardID]
		if !exists {
			// Legacy save snapshots predate the separate static template layer.
			template = inventory
			template.Num = 0
			result.stackCardTemplates[inventory.CardID] = template
			continue
		}
		if inventory.AddExperience != template.AddExperience || inventory.BaseAddPrice != template.BaseAddPrice ||
			inventory.MaterialType != template.MaterialType {
			return nil, fmt.Errorf("stack-card inventory %d differs from its template", inventory.CardID)
		}
	}
	for _, popupID := range state.Engagement.PopupReadIDs {
		if popupID <= 0 {
			return nil, errors.New("popup read state contains an invalid ID")
		}
		if _, duplicate := result.popupReadIDs[popupID]; duplicate {
			return nil, fmt.Errorf("popup read state repeats ID %d", popupID)
		}
		result.popupReadIDs[popupID] = struct{}{}
	}
	for _, profile := range state.TowerQuestProfiles {
		if _, duplicate := result.towerQuestProfiles[profile.TowerID]; duplicate {
			return nil, fmt.Errorf("duplicate tower profile %d", profile.TowerID)
		}
		result.towerQuestProfiles[profile.TowerID] = cloneTowerQuestProfile(profile)
	}
	for _, progress := range state.TowerQuestProgress {
		if _, duplicate := result.towerQuestProgress[progress.TowerID]; duplicate {
			return nil, fmt.Errorf("duplicate tower progress %d", progress.TowerID)
		}
		result.towerQuestProgress[progress.TowerID] = cloneTowerQuestProgress(progress)
	}
	for _, reference := range state.Story.StoryBattleReferences {
		key := fmt.Sprintf("%s:%d", reference.StoryKind, reference.StoryID)
		battleIDs := make(map[int]struct{}, len(reference.StoryBattleIDs))
		for _, battleID := range reference.StoryBattleIDs {
			battleIDs[battleID] = struct{}{}
		}
		result.storyBattleIDsByStory[key] = battleIDs
	}
	allowedScheduleGroups := make(map[int]struct{}, len(state.TeamBattleScheduleGroupIDs))
	for _, groupID := range state.TeamBattleScheduleGroupIDs {
		allowedScheduleGroups[groupID] = struct{}{}
	}
	result.teamBattleLimitedGroupIDs = append(
		[]int(nil), state.TeamBattleScheduleGroupIDs...,
	)
	for _, groupID := range state.TeamBattleSchedule.SoloPushGroupIDs {
		if _, allowed := allowedScheduleGroups[groupID]; allowed {
			result.teamBattleScheduleSoloPush[groupID] = struct{}{}
		}
	}
	for _, groupID := range state.TeamBattleSchedule.MultiPushGroupIDs {
		if _, allowed := allowedScheduleGroups[groupID]; allowed {
			result.teamBattleScheduleMultiPush[groupID] = struct{}{}
		}
	}
	if state.CardDevelopment.Training != nil {
		training := *state.CardDevelopment.Training
		result.cardFameTraining = &training
	}
	if err := release.ValidateTeamBattleContinueReceipts(state.TeamBattleContinueReceipts); err != nil {
		return nil, err
	}
	result.teamBattleContinueReceipts = append([]release.TeamBattleContinueReceipt(nil), state.TeamBattleContinueReceipts...)
	seenStarts := make(map[int64]struct{})
	for _, receipt := range state.TeamBattleStartReceipts {
		if receipt.RoomID <= 0 || receipt.BossID <= 0 || receipt.BPUse < 0 {
			return nil, errors.New("card store multiplayer start receipt is invalid")
		}
		if _, duplicate := seenStarts[receipt.RoomID]; duplicate {
			return nil, errors.New("card store multiplayer start receipt is duplicated")
		}
		seenStarts[receipt.RoomID] = struct{}{}
		result.teamBattleStartReceipts = append(result.teamBattleStartReceipts, receipt)
	}
	for _, receipt := range state.TeamBattleResultReceipts {
		if receipt.RoomID <= 0 || receipt.ClaimedAtUnix <= 0 || len(receipt.Response) == 0 || !json.Valid(receipt.Response) {
			return nil, errors.New("card store team battle result receipt is invalid")
		}
		if _, duplicate := result.teamBattleReceipts[receipt.RoomID]; duplicate {
			return nil, errors.New("card store team battle result receipt room is duplicated")
		}
		receipt.Response = append(json.RawMessage(nil), receipt.Response...)
		result.teamBattleReceipts[receipt.RoomID] = receipt
	}
	for _, receipt := range state.TeamBattleSoloResultReceipts {
		if !isLowerSHA256Digest(receipt.RequestSHA256) || receipt.BossID <= 0 || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || !json.Valid(receipt.Response) {
			return nil, errors.New("card store solo team battle result receipt is invalid")
		}
		if _, duplicate := result.teamBattleSoloReceipts[receipt.RequestSHA256]; duplicate {
			return nil, errors.New("card store solo team battle result receipt digest is duplicated")
		}
		receipt.Response = append(json.RawMessage(nil), receipt.Response...)
		result.teamBattleSoloReceipts[receipt.RequestSHA256] = receipt
	}
	if receipt := state.ExploreResultReceipt; receipt != nil {
		if receipt.StartedAtUnix <= 0 || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || !json.Valid(receipt.Response) {
			return nil, errors.New("card store Explore result receipt is invalid")
		}
		copy := *receipt
		copy.Response = append(json.RawMessage(nil), receipt.Response...)
		result.lastExploreResultReceipt = &copy
	}
	for _, receipt := range state.PVPResultReceipts {
		if receipt.BattleID <= 0 || !isLowerSHA256Digest(receipt.RequestSHA256) || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || !json.Valid(receipt.Response) {
			return nil, errors.New("card store PVP result receipt is invalid")
		}
		if _, duplicate := result.pvpResultReceipts[receipt.BattleID]; duplicate {
			return nil, errors.New("card store PVP result receipt battle is duplicated")
		}
		receipt.Response = append(json.RawMessage(nil), receipt.Response...)
		result.pvpResultReceipts[receipt.BattleID] = receipt
	}
	if result.pvp.NextBattleID <= 0 {
		result.pvp.NextBattleID = 1
	}
	if result.pvp.Challenge < 0 || result.pvp.ChallengeDay < 0 {
		return nil, errors.New("persisted PVP challenge count is invalid")
	}
	for _, definition := range state.ItemDefinitions {
		result.itemDefinitions[definition.ItemID] = definition
	}
	for _, profile := range state.UserBuffProfiles {
		if _, duplicate := result.userBuffProfiles[profile.UserBuffID]; duplicate {
			return nil, fmt.Errorf("duplicate user-buff profile %d", profile.UserBuffID)
		}
		if _, exists := result.itemDefinitions[profile.ItemID]; !exists ||
			profile.RequiredNum <= 0 || profile.DurationSeconds <= 0 {
			return nil, fmt.Errorf("invalid user-buff profile %d", profile.UserBuffID)
		}
		result.userBuffProfiles[profile.UserBuffID] = profile
	}
	for _, profile := range state.ItemGachaProfiles {
		if _, duplicate := result.itemGachaProfiles[profile.ItemID]; duplicate {
			return nil, fmt.Errorf("duplicate item gacha profile %d", profile.ItemID)
		}
		result.itemGachaProfiles[profile.ItemID] = profile
	}
	for _, profile := range state.ItemExchangeProfiles {
		if _, duplicate := result.itemExchangeProfiles[profile.ItemID]; duplicate {
			return nil, fmt.Errorf("duplicate item exchange profile %d", profile.ItemID)
		}
		profile.Reward = cloneReward(profile.Reward)
		result.itemExchangeProfiles[profile.ItemID] = profile
	}
	for _, profile := range state.ItemLackTipProfiles {
		if _, duplicate := result.itemLackTipProfiles[profile.Index]; duplicate {
			return nil, fmt.Errorf("duplicate item lack-tip profile %d", profile.Index)
		}
		links := make([]release.ItemLackTipLink, len(profile.TextURLs))
		copy(links, profile.TextURLs)
		profile.TextURLs = links
		result.itemLackTipProfiles[profile.Index] = profile
	}
	for _, profile := range state.EventShopProfiles {
		if _, duplicate := result.eventShopProfiles[profile.EventID]; duplicate {
			return nil, fmt.Errorf("duplicate event shop profile %d", profile.EventID)
		}
		result.eventShopProfiles[profile.EventID] = cloneEventShopProfile(profile)
	}
	for _, profile := range state.TradeShopProfiles {
		if profile.TradeShopID <= 0 {
			return nil, errors.New("trade shop profile has an invalid ID")
		}
		if _, duplicate := result.tradeShopProfiles[profile.TradeShopID]; duplicate {
			return nil, fmt.Errorf("duplicate trade shop profile %d", profile.TradeShopID)
		}
		result.tradeShopProfiles[profile.TradeShopID] = cloneTradeShopProfile(profile)
	}
	for _, selection := range state.GachaSelections {
		if _, duplicate := result.gachaSelections[selection.GachaID]; duplicate {
			return nil, fmt.Errorf("card store repeats gacha selection %d", selection.GachaID)
		}
		profile := findGachaProfile(result.gachas, selection.GachaID)
		if profile == nil {
			return nil, fmt.Errorf("card store gacha selection %d has no profile", selection.GachaID)
		}
		if err := validateGachaSelection(*profile, selection.Rewards); err != nil {
			return nil, err
		}
		result.gachaSelections[selection.GachaID] = cloneRewards(selection.Rewards)
	}
	for _, claim := range state.GachaDailyClaims {
		if _, duplicate := result.gachaDailyClaims[claim.GachaID]; duplicate {
			return nil, fmt.Errorf("card store repeats gacha daily claim %d", claim.GachaID)
		}
		profile := findGachaProfile(result.gachas, claim.GachaID)
		if profile == nil || !profile.DailyFirstFree {
			return nil, fmt.Errorf("card store gacha daily claim %d has no daily profile", claim.GachaID)
		}
		if _, err := time.Parse("2006-01-02", claim.Day); err != nil {
			return nil, fmt.Errorf("card store gacha daily claim %d has an invalid day", claim.GachaID)
		}
		result.gachaDailyClaims[claim.GachaID] = claim.Day
	}
	for _, definition := range state.SphereDefinitions {
		if _, duplicate := result.sphereDefinitions[definition.SphereID]; duplicate {
			return nil, fmt.Errorf("duplicate sphere definition %d", definition.SphereID)
		}
		result.sphereDefinitions[definition.SphereID] = definition
	}
	// Capacity limits new grants, not loading or reducing existing inventory.
	if result.sphereMax <= 0 ||
		result.sphereProgression.ConfigVersion <= 0 ||
		result.sphereProgression.MaterialLevelBonusPermillePerLevel <= 0 ||
		len(result.sphereDefinitions) == 0 || len(result.sphereExperience) == 0 || len(result.sphereEvoPrices) == 0 {
		return nil, errors.New("card store requires sphere master and valid inventory capacity")
	}
	sphereUniqueIDs := make(map[int64]struct{}, len(result.spheres))
	for _, sphere := range result.spheres {
		definition, exists := result.sphereDefinitions[sphere.SphereID]
		if !exists || sphere.UniqueID <= 0 || sphere.Level < 1 || sphere.Level > definition.MaxLevel ||
			sphere.Experience < 0 || sphere.IsLock < 0 || sphere.IsLock > 1 {
			return nil, fmt.Errorf("invalid persisted sphere %d", sphere.UniqueID)
		}
		if _, duplicate := sphereUniqueIDs[sphere.UniqueID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted sphere unique ID %d", sphere.UniqueID)
		}
		sphereUniqueIDs[sphere.UniqueID] = struct{}{}
		if sphere.UniqueID >= result.nextSphereUniqueID {
			result.nextSphereUniqueID = sphere.UniqueID + 1
		}
	}
	if result.nextSphereUniqueID == 0 {
		result.nextSphereUniqueID = 1
	}
	for _, definition := range state.BuddyDefinitions {
		result.buddyDefinitions[definition.BuddyID] = definition
	}
	if len(result.buddyDefinitions) == 0 || len(result.buddyExperience) == 0 || len(result.buddyEvoPrices) == 0 ||
		result.buddyProgression.ConfigVersion <= 0 || result.buddyProgression.MaximumMaterialCount <= 0 ||
		result.buddyMax <= 0 || len(result.buddies) > result.buddyMax {
		// An initialized but empty Buddy inventory is valid for a clean account.
		// Master data and capacity are still mandatory, and every non-empty entry
		// is validated below.
		return nil, errors.New("card store requires official buddy master and valid inventory capacity")
	}
	buddyUniqueIDs := make(map[int64]struct{}, len(result.buddies))
	for _, buddy := range result.buddies {
		definition, exists := result.buddyDefinitions[buddy.BuddyID]
		if !exists || buddy.UniqueID <= 0 || buddy.Level < 1 || buddy.Level > definition.MaxLevel ||
			buddy.Experience < 0 || buddy.IsLock < 0 || buddy.IsLock > 1 ||
			buddy.AddExperience != definition.MaterialAddExperience ||
			buddy.BaseAddPrice != definition.FusionBaseAddPrice {
			return nil, fmt.Errorf("invalid persisted buddy %d", buddy.UniqueID)
		}
		if _, duplicate := buddyUniqueIDs[buddy.UniqueID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted buddy unique ID %d", buddy.UniqueID)
		}
		buddyUniqueIDs[buddy.UniqueID] = struct{}{}
		if buddy.UniqueID >= result.nextBuddyUniqueID {
			result.nextBuddyUniqueID = buddy.UniqueID + 1
		}
	}
	if result.nextBuddyUniqueID == 0 {
		result.nextBuddyUniqueID = 1
	}
	for _, definition := range state.AvatarPartDefinitions {
		if definition.PartID <= 0 {
			return nil, errors.New("card store contains an invalid Avatar part definition")
		}
		if _, duplicate := result.avatarDefinitions[definition.PartID]; duplicate {
			return nil, fmt.Errorf("duplicate Avatar part definition %d", definition.PartID)
		}
		result.avatarDefinitions[definition.PartID] = definition
	}
	if state.AvatarShopPartIDs == nil {
		for partID := range result.avatarDefinitions {
			result.avatarShopParts[partID] = struct{}{}
		}
	} else {
		for _, partID := range state.AvatarShopPartIDs {
			if _, exists := result.avatarDefinitions[partID]; !exists {
				return nil, fmt.Errorf("Avatar shop part %d is absent from the official master", partID)
			}
			if _, duplicate := result.avatarShopParts[partID]; duplicate {
				return nil, fmt.Errorf("duplicate Avatar shop part %d", partID)
			}
			result.avatarShopParts[partID] = struct{}{}
		}
	}
	if len(result.avatarDefinitions) == 0 || len(result.avatarDefaultDecks) != 4 || len(result.avatarCompletions) == 0 ||
		len(result.avatarShopParts) == 0 ||
		result.avatarShopPolicy.PayType < 1 || result.avatarShopPolicy.PayType > 7 ||
		result.avatarShopPolicy.Price <= 0 {
		return nil, errors.New("card store requires Avatar master and local shop policy")
	}
	for _, partID := range state.AvatarParts {
		if _, exists := result.avatarDefinitions[partID]; !exists {
			return nil, fmt.Errorf("persisted Avatar part %d is absent from the official master", partID)
		}
		if _, duplicate := result.avatarParts[partID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted Avatar part %d", partID)
		}
		result.avatarParts[partID] = struct{}{}
	}
	if len(result.avatarParts) == 0 {
		return nil, errors.New("card store requires owned Avatar parts")
	}
	for _, item := range state.Items {
		definition, exists := result.itemDefinitions[item.ItemID]
		if !exists || item.Num < 0 || item.Num > definition.MaxOwned {
			return nil, fmt.Errorf("invalid persisted item %d", item.ItemID)
		}
		result.items[item.ItemID] = item
	}
	if len(result.itemDefinitions) == 0 || len(result.itemShopTabs) != 5 {
		return nil, errors.New("card store requires official item master and five shop tabs")
	}
	for towerID, profile := range result.towerQuestProfiles {
		progress, exists := result.towerQuestProgress[towerID]
		_, itemExists := result.itemDefinitions[profile.ItemID]
		if !exists || !itemExists || progress.Floor < 0 || progress.Floor > len(profile.Floors) ||
			progress.LoseCount < 0 || progress.LoseCount >= profile.LoseCountMax {
			return nil, fmt.Errorf("card store tower profile %d is incomplete", towerID)
		}
	}
	if result.followMax < 1 || result.followMax < len(result.friends) {
		return nil, errors.New("friend store capacity is invalid")
	}
	if state.Explore.StartedAtUnix > 0 {
		result.exploreStartedAt = time.Unix(state.Explore.StartedAtUnix, 0)
	}
	if result.ap < result.apMax {
		if state.Explore.APNextRecoveryUnix > 0 {
			result.apNextRecovery = time.Unix(state.Explore.APNextRecoveryUnix, 0)
		} else {
			result.apNextRecovery = time.Now().Add(result.apRecoveryInterval)
		}
	}
	if result.bpRecoveryInterval <= 0 {
		return nil, errors.New("card store requires a positive BP recovery interval")
	}
	if result.bp < result.bpMax {
		if state.BattlePoint.NextRecoveryUnix > 0 {
			result.bpNextRecovery = time.Unix(state.BattlePoint.NextRecoveryUnix, 0)
		} else {
			result.bpNextRecovery = time.Now().Add(result.bpRecoveryInterval)
		}
	}
	for _, persisted := range state.CardTemplates {
		if persisted.LoveMax < 0 {
			return nil, fmt.Errorf("card template %d has an invalid love maximum", persisted.CardID)
		}
		card := cardInfo{
			UniqueID:      persisted.UniqueID,
			CardID:        persisted.CardID,
			Level:         persisted.Level,
			LevelMax:      persisted.LevelMax,
			Experience:    persisted.Experience,
			NowLevelEXP:   persisted.NowLevelExperience,
			Love:          persisted.Love,
			LoveMax:       persisted.LoveMax,
			SkillLevels:   append([]int16(nil), persisted.SkillLevels...),
			HP:            persisted.HP,
			Attack:        persisted.Attack,
			Magic:         persisted.Magic,
			Mind:          persisted.Mind,
			NextLevelEXP:  persisted.NextLevelExperience,
			AddExperience: persisted.AddExperience,
			BaseAddPrice:  persisted.BaseAddPrice,
			IsLock:        0,
			Fame:          persisted.Fame,
			Slot:          0,
		}
		result.cardTemplates[card.CardID] = card
		result.cardDefinitions[card.CardID] = persisted
		result.cardLoveRules[card.CardID] = cardLoveRule{
			Max: persisted.LoveMax, Premium: persisted.PremiumRarity,
		}
		result.cardDevelopmentRules[card.CardID] = cardDevelopmentRule{
			FameMax: persisted.FameMax, DevelopmentType: persisted.DevelopmentType,
			DecomposeRadix: persisted.DecomposeRadix, DevelopRadix: persisted.DevelopRadix,
			AcquisitionText: persisted.AcquisitionText,
		}
	}
	if result.cardDevelopmentPolicy.TimeEveryFameSeconds <= 0 ||
		result.cardDevelopmentPolicy.CoinEveryHour <= 0 ||
		result.cardDevelopmentPolicy.HelpPath == "" || result.cardDevelopmentPolicy.HelpPath[0] != '/' ||
		result.cardDevelopmentPolicy.Evidence != "PLACEHOLDER" || result.stive < 0 {
		return nil, errors.New("card store requires a valid card development policy and state")
	}
	if result.cardFameTraining != nil {
		index := cardIndexByUniqueID(result.cards, result.cardFameTraining.UniqueID)
		if index < 0 || result.cards[index].IsLock != 1 || result.cardFameTraining.BeginAtUnix <= 0 ||
			result.cardFameTraining.Fame <= 0 {
			return nil, errors.New("card store card fame training state is invalid")
		}
		rule, exists := result.cardDevelopmentRules[result.cards[index].CardID]
		if !exists || rule.DevelopmentType != 2 || rule.DevelopRadix <= 0 ||
			result.cards[index].Fame+result.cardFameTraining.Fame > rule.FameMax {
			return nil, errors.New("card store card fame training exceeds the official card rule")
		}
	}
	for _, inventory := range [][]cardInfo{cards, containerCards} {
		for _, card := range inventory {
			loveRule, exists := result.cardLoveRules[card.CardID]
			if card.Love < 0 || !exists || card.Love > loveRule.Max {
				return nil, fmt.Errorf("card %d love is outside its official master range", card.UniqueID)
			}
			developmentRule, exists := result.cardDevelopmentRules[card.CardID]
			if !exists || card.Fame < 1 || card.Fame > developmentRule.FameMax {
				return nil, fmt.Errorf("card %d fame is outside its official master range", card.UniqueID)
			}
			normalized, err := result.normalizeCardLocked(card)
			if err != nil || !equalCardInfo(normalized, card) {
				return nil, fmt.Errorf("card %d progression fields are invalid", card.UniqueID)
			}
		}
	}
	for cardID := range result.cardCollectionIDs {
		if _, exists := result.cardTemplates[cardID]; !exists {
			if _, stack := result.stackCardTemplates[cardID]; !stack {
				return nil, fmt.Errorf("card store collection references unknown card %d", cardID)
			}
		}
	}
	for _, id := range state.SupportDeck.CardCollectionLoveMaxIDs {
		if _, exists := result.cardCollectionIDs[id]; !exists || result.cardDefinitions[id].LoveMax <= 0 {
			return nil, fmt.Errorf("card collection love maximum references invalid card %d", id)
		}
		result.cardCollectionLoveMaxIDs[id] = struct{}{}
	}
	for _, inventory := range [][]cardInfo{result.cards, result.containerCards} {
		for _, card := range inventory {
			result.recordCollectedCardLocked(card)
		}
	}
	if len(result.gachas) == 0 {
		return nil, errors.New("card store requires a configured CN gacha")
	}
	for _, gacha := range result.gachas {
		if err := result.validateGachaRulesLocked(gacha); err != nil {
			return nil, err
		}
		if gacha.PayType == 4 {
			if definition, exists := result.itemDefinitions[gacha.PayTypeID]; !exists || definition.ItemType != "GACHA_TICKET" {
				return nil, fmt.Errorf("gacha %d references an invalid ticket item", gacha.GachaID)
			}
		}
		for _, cardID := range gacha.CardIDs {
			if _, exists := result.cardTemplates[cardID]; !exists {
				return nil, fmt.Errorf("gacha %d references unknown card %d", gacha.GachaID, cardID)
			}
		}
	}
	for itemID, profile := range result.itemGachaProfiles {
		definition, exists := result.itemDefinitions[itemID]
		if !exists || definition.ItemType != "GACHA" || definition.Function != "GACHA_EXEC" ||
			definition.FunctionValue != profile.FunctionValue || (len(profile.Rewards) == 0 && len(profile.RewardPool) == 0) ||
			(profile.Evidence != "INFERRED_OFFICIAL_DESCRIPTION_EXACT_CARD_BASE" && profile.Evidence != "PLACEHOLDER_LOCAL_POLICY_OFFICIAL_CN_ITEM_DESCRIPTION") {
			return nil, fmt.Errorf("item gacha profile %d is invalid", itemID)
		}
		for _, reward := range profile.Rewards {
			if err := result.validateRewardLocked(reward); err != nil {
				return nil, fmt.Errorf("item gacha profile %d: %w", itemID, err)
			}
		}
		if len(profile.RewardPool) > 0 {
			if err := release.ValidateRewardPool(profile.RewardPool); err != nil {
				return nil, err
			}
			for _, entry := range profile.RewardPool {
				if err := result.validateRewardLocked(entry.Reward); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, profile := range state.TeamBattleRewards {
		if err := release.ValidateTeamBattleScorePolicy(profile.ScorePolicy); err != nil {
			return nil, err
		}
	}
	for itemID, profile := range result.itemExchangeProfiles {
		if itemID != profile.ItemID || profile.EventID <= 0 || profile.NeedNum <= 0 ||
			(profile.IsAppearEvent != 0 && profile.IsAppearEvent != 1) ||
			(profile.Evidence != "INFERRED_OFFICIAL_CN_ITEM_NAME_EXACT_CARD_FAMILY_LOCAL_POLICY" &&
				profile.Evidence != "PLACEHOLDER_LOCAL_POLICY_OFFICIAL_CN_ITEM_DESCRIPTION") {
			return nil, fmt.Errorf("item exchange profile %d is invalid", itemID)
		}
		if _, exists := result.itemDefinitions[itemID]; !exists {
			return nil, fmt.Errorf("item exchange profile %d has no item definition", itemID)
		}
		if err := result.validateLocalTradeRewardLocked(profile.Reward); err != nil {
			return nil, fmt.Errorf("item exchange profile %d: %w", itemID, err)
		}
	}
	lineupStocks := make(map[int]int)
	for eventID, profile := range result.eventShopProfiles {
		if eventID != profile.EventID || eventID <= 0 || profile.PointItemID <= 0 || len(profile.Lineups) == 0 {
			return nil, fmt.Errorf("event shop profile %d is invalid", eventID)
		}
		if _, exists := result.itemDefinitions[profile.PointItemID]; !exists {
			return nil, fmt.Errorf("event shop %d has no point item definition", eventID)
		}
		for _, lineup := range profile.Lineups {
			if lineup.LineupID <= 0 || lineup.Name == "" || lineup.ImageURL == "" || lineup.Price <= 0 ||
				lineup.StockNum == 0 || lineup.StockType < 0 ||
				(lineup.IsFeature != 0 && lineup.IsFeature != 1) ||
				lineup.Evidence != "CONFIRMED_CN_OFFICIAL_EVENT_SHOP_MASTER" {
				return nil, fmt.Errorf("event shop lineup %d is invalid", lineup.LineupID)
			}
			if _, duplicate := lineupStocks[lineup.LineupID]; duplicate {
				return nil, fmt.Errorf("duplicate event shop lineup %d", lineup.LineupID)
			}
			if err := result.validateLocalTradeRewardLocked(lineup.Reward); err != nil {
				return nil, fmt.Errorf("event shop lineup %d: %w", lineup.LineupID, err)
			}
			lineupStocks[lineup.LineupID] = lineup.StockNum
		}
	}
	for _, purchase := range state.EventShopPurchases {
		stock, exists := lineupStocks[purchase.LineupID]
		if !exists || purchase.Count <= 0 || (stock > 0 && purchase.Count > stock) {
			return nil, fmt.Errorf("event shop purchase %d is invalid", purchase.LineupID)
		}
		if _, duplicate := result.eventShopPurchases[purchase.LineupID]; duplicate {
			return nil, fmt.Errorf("duplicate event shop purchase %d", purchase.LineupID)
		}
		result.eventShopPurchases[purchase.LineupID] = purchase.Count
	}
	tradeLineupStocks := make(map[int]int)
	for shopID, profile := range result.tradeShopProfiles {
		if shopID != profile.TradeShopID || profile.Name == "" || len(profile.Lineups) == 0 {
			return nil, fmt.Errorf("trade shop profile %d is invalid", shopID)
		}
		for _, lineup := range profile.Lineups {
			if lineup.LineupID <= 0 || lineup.LineupName == "" || lineup.StockNum == 0 ||
				len(lineup.Prices) != 1 || len(lineup.Rewards) != 1 {
				return nil, fmt.Errorf("trade shop lineup %d is invalid", lineup.LineupID)
			}
			if _, duplicate := tradeLineupStocks[lineup.LineupID]; duplicate {
				return nil, fmt.Errorf("duplicate trade shop lineup %d", lineup.LineupID)
			}
			price := lineup.Prices[0]
			if price.Type != 4 || price.ID <= 0 || price.Num <= 0 || price.PointCardCondition == nil {
				return nil, fmt.Errorf("trade shop lineup %d price is invalid", lineup.LineupID)
			}
			if _, exists := result.itemDefinitions[price.ID]; !exists {
				return nil, fmt.Errorf("trade shop lineup %d price item is unknown", lineup.LineupID)
			}
			if err := result.validateLocalTradeRewardLocked(lineup.Rewards[0]); err != nil {
				return nil, fmt.Errorf("trade shop lineup %d: %w", lineup.LineupID, err)
			}
			tradeLineupStocks[lineup.LineupID] = lineup.StockNum
		}
	}
	for _, purchase := range state.TradeShopPurchases {
		// Counts are durable receipts. A retired offer or a reduced limit must
		// not prevent login or reset what the player has already exchanged.
		if purchase.LineupID <= 0 || purchase.Count <= 0 {
			return nil, fmt.Errorf("persisted trade shop purchase %d is invalid", purchase.LineupID)
		}
		if _, duplicate := result.tradeShopPurchases[purchase.LineupID]; duplicate {
			return nil, fmt.Errorf("duplicate persisted trade shop purchase %d", purchase.LineupID)
		}
		result.tradeShopPurchases[purchase.LineupID] = purchase.Count
	}
	for _, naviID := range state.User.SelectableNaviIDs {
		result.selectableNaviIDs[naviID] = struct{}{}
	}
	for _, naviID := range state.User.NaviCatalogIDs {
		result.naviCatalogIDs[naviID] = struct{}{}
	}
	for _, stampID := range state.Stamps.StampIDs {
		result.stampIDs[stampID] = struct{}{}
	}
	for _, costumeID := range costume.CostumeIDs {
		result.costumeIDs[costumeID] = struct{}{}
	}
	if len(result.avatars) != 4 {
		return nil, errors.New("card store requires one avatar for each Arthur type")
	}
	for index, avatar := range result.avatars {
		if len(avatar.AvatarPartIDs) != avatarDeckSlots {
			return nil, fmt.Errorf("Avatar deck %d has an invalid shape", index+1)
		}
		for slot, partID := range avatar.AvatarPartIDs {
			if partID == 0 {
				continue
			}
			definition, exists := result.avatarDefinitions[partID]
			if !exists {
				return nil, fmt.Errorf("Avatar deck %d references unknown part %d", index+1, partID)
			}
			if _, owned := result.avatarParts[partID]; !owned || !avatarEquipAllowed(definition, index+1, slot, result.avatarDefaultDecks) {
				return nil, fmt.Errorf("Avatar deck %d has an invalid part %d in slot %d", index+1, partID, slot)
			}
		}
	}
	for _, honorID := range state.Honors.HonorIDs {
		result.honorIDs[honorID] = struct{}{}
	}
	if _, exists := result.selectableNaviIDs[result.currentNaviID]; !exists {
		return nil, errors.New("card store default navigator is not selectable")
	}
	for _, inventory := range [][]cardInfo{cards, containerCards} {
		for _, card := range inventory {
			if card.UniqueID >= result.nextUniqueID {
				result.nextUniqueID = card.UniqueID + 1
			}
		}
	}
	repairedDecks, err := result.repairIncompleteMainDecks()
	if err != nil {
		return nil, fmt.Errorf("repair persisted main deck: %w", err)
	}
	featureCount := len(result.unlockedFeatureIDs)
	if err := result.completeTrainingBurstLocked(); err != nil {
		return nil, fmt.Errorf("complete training sword unlocks: %w", err)
	}
	result.initialStateRepair = repairedDecks || len(result.unlockedFeatureIDs) != featureCount
	for _, deck := range result.decks {
		if err := result.validateDeck(deck); err != nil {
			return nil, fmt.Errorf("validate initial deck: %w", err)
		}
	}
	if err := result.validateEngagementLocked(); err != nil {
		return nil, fmt.Errorf("validate mission and present state: %w", err)
	}
	if err := result.validateLoginBonusLocked(); err != nil {
		return nil, err
	}
	if result.exploreActive {
		if _, _, _, ok := result.exploreSelectionLocked(result.exploreArthurType, result.exploreDeckIndex); !ok ||
			result.exploreStartedAt.IsZero() || !result.hasExploreStageLocked(result.exploreActiveStage) {
			return nil, errors.New("persisted Explore selection is invalid")
		}
	}
	if activeBattle := teamBattleContextFromRelease(state.ActiveTeamBattle); activeBattle != nil {
		if err := result.validateActiveTeamBattleContextLocked(*activeBattle, state.TeamBattleRewards); err != nil {
			return nil, fmt.Errorf("validate persisted active team battle: %w", err)
		}
		result.activeBattle = activeBattle
	}
	result.refreshDeckRanksLocked()
	return result, nil
}

func (s *store) userName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentName
}

type playerProgressionState struct {
	Level               int
	Experience          int
	NowLevelExperience  int
	NextLevelExperience int
	FriendMax           int
	Jobs                []release.JobParameter
}

func (s *store) playerProgressionState() playerProgressionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return playerProgressionState{
		Level:               s.currentLevel,
		Experience:          s.currentExperience,
		NowLevelExperience:  s.currentLevelExperience,
		NextLevelExperience: s.nextLevelExperience,
		FriendMax:           s.currentFriendMax,
		Jobs:                append([]release.JobParameter(nil), s.currentJobs...),
	}
}

func (s *store) userLevel() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentLevel
}

func (s *store) jobParameter(jobType int8) release.JobParameter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := int(jobType)
	if index < 0 || index >= len(s.currentJobs) {
		return release.JobParameter{}
	}
	return s.currentJobs[index]
}

func (s *store) userCreated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.currentName) != ""
}

func (s *store) activeArthurType() int8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentActiveArthur
}

func (s *store) activeArthurLeaderState() (int64, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeArthurLeaderLocked()
}

func (s *store) activeArthurLeaderLocked() (int64, int, bool) {
	deck, found := selectPartnerDeck(s.decks, s.currentActiveArthur)
	if !found {
		return 0, 0, false
	}
	cardByUniqueID := make(map[int64]cardInfo, len(s.cards))
	for _, card := range s.cards {
		cardByUniqueID[card.UniqueID] = card
	}
	leader, found := partnerLeaderCard(deck, cardByUniqueID)
	if !found {
		return 0, 0, false
	}
	return leader.UniqueID, leader.CardID, true
}

func (s *store) supportDeckState() (int, []int8) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.supportDeckSetCardNum, append([]int8(nil), s.supportUnlockedSlots...)
}

func (s *store) cardCollectionState() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]int, 0, len(s.cardCollectionIDs))
	for _, page := range s.cardCollectionPages {
		for _, cardID := range page {
			if _, found := s.cardCollectionIDs[cardID]; cardID != 0 && found {
				result = append(result, cardID)
			}
		}
	}
	sort.Ints(result)
	return result
}

func (s *store) unlockSupportCardSlot(arthurType int8) ([]int8, int, error) {
	if arthurType < 1 || arthurType > 4 {
		return nil, 0, errors.New("arthur_type must be 1 through 4")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	arthurIndex := int(arthurType) - 1
	unlocked := int(s.supportUnlockedSlots[arthurIndex])
	if unlocked >= s.supportDeckSetCardNum {
		return nil, 0, errors.New("all active support-card slots are already unlocked")
	}
	rule := s.supportDeckRules[unlocked]
	if rule.SlotIndex != unlocked+1 {
		return nil, 0, errors.New("support-card slot rule order is invalid")
	}
	if s.currentLevel < rule.Level {
		return nil, 0, errors.New("Arthur level does not meet the support-card slot requirement")
	}
	if s.cardCollectionCountLocked() < rule.CardCollectionNum {
		return nil, 0, errors.New("card collection does not meet the support-card slot requirement")
	}
	if s.gold < rule.Gold {
		return nil, 0, errInsufficientGold
	}
	s.gold -= rule.Gold
	s.supportUnlockedSlots[arthurIndex]++
	return append([]int8(nil), s.supportUnlockedSlots...), s.gold, nil
}

func (s *store) createUser(name string, arthurType int8) bool {
	name = strings.TrimSpace(name)
	if name == "" || arthurType < 1 || arthurType > 4 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.currentName) != "" {
		return false
	}
	s.currentName = name
	s.currentActiveArthur = arthurType
	if s.unlockedFeatureIDs == nil {
		s.unlockedFeatureIDs = make(map[uint]struct{})
	}
	for featureID := uint(0); featureID <= 3; featureID++ {
		delete(s.unlockedFeatureIDs, featureID)
	}
	s.unlockedFeatureIDs[uint(arthurType-1)] = struct{}{}
	return true
}

func (s *store) userComment() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentComment
}

func (s *store) tutorialState() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tutorialFlag
}

func (s *store) mergeTutorialFlag(flag int64) bool {
	// CONFIRMED: the CN 6.0.2 TUTORIAL_FLAG enum has 26 real bit positions.
	if flag < 0 || flag >= int64(1)<<26 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tutorialFlag |= flag
	return true
}

func (s *store) setUserName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentName = name
	return true
}

func (s *store) setUserComment(comment string) bool {
	if len([]byte(comment)) > userCommentMaxBytes {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentComment = comment
	return true
}

func (s *store) honorState() ([]int, []int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	honorIDs := make([]int, 0, len(s.honorIDs))
	for honorID := range s.honorIDs {
		honorIDs = append(honorIDs, honorID)
	}
	sort.Ints(honorIDs)
	return append([]int(nil), s.deckHonorIDs...), honorIDs
}

func (s *store) setHonorDeck(honorIDs []int) bool {
	if len(honorIDs) != 4 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, honorID := range honorIDs {
		if honorID == 0 {
			continue
		}
		if _, exists := s.honorIDs[honorID]; !exists {
			return false
		}
	}
	s.deckHonorIDs = append([]int(nil), honorIDs...)
	return true
}

func (s *store) selectCostume(arthurType int8, costumeID int) bool {
	if arthurType < 1 || arthurType > 4 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if costumeID != 0 {
		if _, exists := s.costumeIDs[costumeID]; !exists {
			return false
		}
	}
	s.avatars[int(arthurType)-1].CostumeID = costumeID
	return true
}

func (s *store) avatarsState() []release.Avatar {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAvatars(s.avatars)
}

func cloneAvatars(avatars []release.Avatar) []release.Avatar {
	cloned := make([]release.Avatar, len(avatars))
	for index, avatar := range avatars {
		cloned[index] = release.Avatar{
			CostumeID:     avatar.CostumeID,
			AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	return cloned
}

func cloneExploreStages(stages []release.ExploreStage) []release.ExploreStage {
	cloned := make([]release.ExploreStage, len(stages))
	for index, stage := range stages {
		cloned[index] = stage
		cloned[index].Floors = append([]release.ExploreFloor(nil), stage.Floors...)
	}
	return cloned
}

func cloneFriends(friends []release.Friend) []release.Friend {
	cloned := make([]release.Friend, len(friends))
	for index, friend := range friends {
		cloned[index] = friend
		cloned[index].DeckHonorIDs = append([]int(nil), friend.DeckHonorIDs...)
	}
	return cloned
}

func cloneStoryMainParts(parts []release.StoryMainPart) []release.StoryMainPart {
	cloned := make([]release.StoryMainPart, len(parts))
	for partIndex, part := range parts {
		cloned[partIndex] = part
		cloned[partIndex].Sections = make([]release.StoryMainSection, len(part.Sections))
		for sectionIndex, section := range part.Sections {
			cloned[partIndex].Sections[sectionIndex] = section
			cloned[partIndex].Sections[sectionIndex].Stories = append(
				[]release.StoryMain(nil),
				section.Stories...,
			)
		}
	}
	return cloned
}

func cloneStorySubCharacters(characters []release.StorySubCharacter) []release.StorySubCharacter {
	cloned := make([]release.StorySubCharacter, len(characters))
	for characterIndex, character := range characters {
		cloned[characterIndex] = character
		cloned[characterIndex].Sections = make([]release.StorySubSection, len(character.Sections))
		for sectionIndex, section := range character.Sections {
			cloned[characterIndex].Sections[sectionIndex] = section
			cloned[characterIndex].Sections[sectionIndex].Stories = make([]release.StorySub, len(section.Stories))
			for storyIndex, story := range section.Stories {
				cloned[characterIndex].Sections[sectionIndex].Stories[storyIndex] = story
				cloned[characterIndex].Sections[sectionIndex].Stories[storyIndex].FeatureReward.CardSkillLevels = append(
					[]int16{},
					story.FeatureReward.CardSkillLevels...,
				)
			}
		}
	}
	return cloned
}

func cloneStoryEvents(events []release.StoryEvent) []release.StoryEvent {
	cloned := make([]release.StoryEvent, len(events))
	for eventIndex, event := range events {
		cloned[eventIndex] = event
		cloned[eventIndex].Stories = make([]release.StorySubEvent, len(event.Stories))
		for storyIndex, wrapper := range event.Stories {
			cloned[eventIndex].Stories[storyIndex] = wrapper
			cloned[eventIndex].Stories[storyIndex].Sub.FeatureReward.CardSkillLevels = append(
				[]int16{},
				wrapper.Sub.FeatureReward.CardSkillLevels...,
			)
			cloned[eventIndex].Stories[storyIndex].Materials = append(
				[]release.StoryUnlockMaterial{},
				wrapper.Materials...,
			)
		}
	}
	return cloned
}

func (s *store) storyMainState(cnStory bool) []release.StoryMainPart {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cnStory {
		return cloneStoryMainParts(s.cnStoryMainParts)
	}
	return cloneStoryMainParts(s.storyMainParts)
}

func (s *store) beginMainStory(storyMainID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for cnStory, parts := range map[bool][]release.StoryMainPart{false: s.storyMainParts, true: s.cnStoryMainParts} {
		for _, part := range parts {
			for _, section := range part.Sections {
				for _, story := range section.Stories {
					if story.StoryMainID == storyMainID {
						if story.StateFlag&(1<<2) != 0 {
							return false
						}
						s.activeMainStoryID = storyMainID
						s.storyTeamBattleSession = release.StoryTeamBattleSession{}
						s.activeMainStoryCN = cnStory
						s.activeSubStoryID = 0
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *store) applyStoryFirstClearRewardLocked(
	reward release.Reward,
) (presentReceiveResult, error) {
	result := presentReceiveResult{}
	if err := s.validateSettlementRewardsLocked([]release.Reward{reward}); err != nil {
		return presentReceiveResult{}, err
	}
	if err := s.applySettlementRewardLocked(reward, &result); err != nil {
		return presentReceiveResult{}, err
	}
	return result, nil
}

func (s *store) endMainStory(isClear bool) (presentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeMainStoryID == 0 {
		return presentReceiveResult{}, errors.New("no active main story")
	}
	activeID := s.activeMainStoryID
	activeCN := s.activeMainStoryCN
	parts := &s.storyMainParts
	if activeCN {
		parts = &s.cnStoryMainParts
	}
	result := presentReceiveResult{}
	found := false
	for partIndex := range *parts {
		for sectionIndex := range (*parts)[partIndex].Sections {
			stories := (*parts)[partIndex].Sections[sectionIndex].Stories
			for storyIndex := range stories {
				if !found && stories[storyIndex].StoryMainID != activeID {
					continue
				}
				if !found {
					if isClear {
						if stories[storyIndex].StateFlag&2 == 0 {
							var err error
							result, err = s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.MainFirstClear)
							if err != nil {
								return presentReceiveResult{}, fmt.Errorf("apply main story first-clear reward: %w", err)
							}
						}
						stories[storyIndex].StateFlag = stories[storyIndex].StateFlag&8 | 1<<1
						stories[storyIndex].UnlockText = ""
						if err := s.advanceOnboardingLocked(onboardingEvent{kind: "story"}); err != nil {
							return presentReceiveResult{}, err
						}
					}
					found = true
					if !isClear {
						s.activeMainStoryID = 0
						s.activeMainStoryCN = false
						return result, nil
					}
					continue
				}
				if stories[storyIndex].StateFlag&2 == 0 {
					stories[storyIndex].StateFlag = stories[storyIndex].StateFlag&8 | 1
					stories[storyIndex].UnlockText = ""
				}
				s.activeMainStoryID = 0
				s.activeMainStoryCN = false
				return result, nil
			}
		}
	}
	if !found {
		return presentReceiveResult{}, errors.New("active main story is absent from the catalog")
	}
	s.activeMainStoryID = 0
	s.activeMainStoryCN = false
	return result, nil
}

func (s *store) storySubState() []release.StorySubCharacter {
	s.mu.Lock()
	defer s.mu.Unlock()
	release.UnlockCollectedCharacterStories(s.storySubCharacters, s.cardCollectionIDs)
	characters := cloneStorySubCharacters(s.storySubCharacters)
	for i := range characters {
		sections := characters[i].Sections[:0]
		for _, section := range characters[i].Sections {
			if len(section.Stories) > 0 {
				if _, _, learning := release.FindBurstStory(section.Stories[0].StorySubID); learning {
					continue
				}
			}
			sections = append(sections, section)
		}
		characters[i].Sections = sections
	}
	return characters
}

func (s *store) storyEventState() []release.StoryEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneStoryEvents(s.storyEvents)
}

func (s *store) authorizeStoryBattle(
	storyBattleID int,
	deckArthurType int8,
	deckArthurTypeIndexes []int8,
) error {
	if storyBattleID <= 0 || deckArthurType < 1 || deckArthurType > 4 || len(deckArthurTypeIndexes) != 4 {
		return errors.New("invalid story fixed-battle request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	storyKey := ""
	if s.storyTeamBattleSession.StoryID != 0 {
		quest, step, ok := release.FindBurstStory(s.storyTeamBattleSession.StoryID)
		if !ok || step != 2 || storyBattleID != quest.StoryIDs[2] {
			return errors.New("story fixed battle differs from active learning script")
		}
		storyKey = fmt.Sprintf("sub:%d", quest.StoryIDs[2])
	}
	if s.activeMainStoryID != 0 {
		storyKind := "main"
		if s.activeMainStoryCN {
			storyKind = "cn_main"
		}
		storyKey = fmt.Sprintf("%s:%d", storyKind, s.activeMainStoryID)
	} else if s.activeSubStoryID != 0 {
		storyKey = fmt.Sprintf("sub:%d", s.activeSubStoryID)
	}
	allowed, active := s.storyBattleIDsByStory[storyKey]
	if !active {
		return errors.New("story fixed battle has no active script owner")
	}
	if _, exists := allowed[storyBattleID]; !exists {
		return errors.New("story fixed battle differs from active script")
	}
	for index, deckIndex := range deckArthurTypeIndexes {
		if deckIndex < -1 {
			return errors.New("story fixed-battle deck index is invalid")
		}
		if deckIndex < 0 {
			if int8(index+1) == deckArthurType {
				return errors.New("selected Arthur has no active story deck")
			}
			continue
		}
		found := false
		for _, deck := range s.decks {
			if deck.ArthurType == int8(index+1) && deck.Index == deckIndex {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("story fixed-battle deck %d/%d is unknown", index+1, deckIndex)
		}
	}
	return nil
}

func (s *store) unlockEventStory(storySubID int) ([]release.Item, []release.StoryEvent, error) {
	if storySubID <= 0 {
		return nil, nil, errors.New("invalid event story ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	eventIndex := -1
	storyIndex := -1
	var materials []release.StoryUnlockMaterial
	for candidateEventIndex := range s.storyEvents {
		for candidateStoryIndex := range s.storyEvents[candidateEventIndex].Stories {
			wrapper := s.storyEvents[candidateEventIndex].Stories[candidateStoryIndex]
			if wrapper.Sub.StorySubID != storySubID {
				continue
			}
			if wrapper.Sub.StateFlag&16 == 0 || len(wrapper.Materials) == 0 {
				return nil, nil, errors.New("event story has no published material unlock")
			}
			eventIndex = candidateEventIndex
			storyIndex = candidateStoryIndex
			materials = append([]release.StoryUnlockMaterial(nil), wrapper.Materials...)
			break
		}
	}
	if eventIndex < 0 || storyIndex < 0 {
		return nil, nil, errors.New("unknown event story ID")
	}
	maximum := int(^uint(0) >> 1)
	goldCost := 0
	coinCost := 0
	secureCoinCost := 0
	itemCosts := make(map[int]int)
	addCost := func(current int, added int) (int, error) {
		if added <= 0 || current > maximum-added {
			return 0, errors.New("event story unlock cost is invalid")
		}
		return current + added, nil
	}
	for _, material := range materials {
		var err error
		switch material.PayType {
		case 1:
			goldCost, err = addCost(goldCost, material.Price)
		case 3:
			coinCost, err = addCost(coinCost, material.Price)
		case 4:
			if material.PayTypeID <= 0 {
				return nil, nil, errors.New("event story unlock item is invalid")
			}
			itemCosts[material.PayTypeID], err = addCost(itemCosts[material.PayTypeID], material.Price)
		case 6:
			secureCoinCost, err = addCost(secureCoinCost, material.Price)
		default:
			return nil, nil, fmt.Errorf("unsupported event story pay type %d", material.PayType)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if s.gold < goldCost {
		return nil, nil, errInsufficientGold
	}
	if s.coin < secureCoinCost {
		return nil, nil, errInsufficientPaidCrystals
	}
	if coinCost > s.coinFree && s.coin-secureCoinCost < coinCost-s.coinFree {
		return nil, nil, errInsufficientCrystals
	}
	now := time.Now().Unix()
	for itemID, cost := range itemCosts {
		item, exists := s.items[itemID]
		if !exists || item.Num < cost || item.LimitTime > 0 && now >= int64(item.LimitTime) {
			return nil, nil, errInsufficientMaterials
		}
	}

	s.gold -= goldCost
	s.coin -= secureCoinCost
	freeUse := coinCost
	if freeUse > s.coinFree {
		freeUse = s.coinFree
	}
	s.coinFree -= freeUse
	s.coin -= coinCost - freeUse
	changedItems := make([]release.Item, 0, len(itemCosts))
	for itemID, cost := range itemCosts {
		item := s.items[itemID]
		item.Num -= cost
		s.items[itemID] = item
		changedItems = append(changedItems, item)
	}
	sort.Slice(changedItems, func(left, right int) bool {
		return changedItems[left].ItemID < changedItems[right].ItemID
	})
	story := &s.storyEvents[eventIndex].Stories[storyIndex].Sub
	story.StateFlag = story.StateFlag&8 | 1
	story.UnlockText = ""
	return changedItems, cloneStoryEvents(s.storyEvents), nil
}

func (s *store) beginSubStory(storySubID int) bool {
	if _, _, learning := release.FindBurstStory(storySubID); learning {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release.UnlockCollectedCharacterStories(s.storySubCharacters, s.cardCollectionIDs)
	for _, character := range s.storySubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				if story.StorySubID != storySubID {
					continue
				}
				if story.StateFlag&4 != 0 {
					return false
				}
				s.activeSubStoryID = storySubID
				s.storyTeamBattleSession = release.StoryTeamBattleSession{}
				s.activeMainStoryID = 0
				s.activeMainStoryCN = false
				return true
			}
		}
	}
	for _, event := range s.storyEvents {
		for _, wrapper := range event.Stories {
			if wrapper.Sub.StorySubID != storySubID {
				continue
			}
			if wrapper.Sub.StateFlag&(4|16) != 0 {
				return false
			}
			s.activeSubStoryID = storySubID
			s.storyTeamBattleSession = release.StoryTeamBattleSession{}
			s.activeMainStoryID = 0
			s.activeMainStoryCN = false
			return true
		}
	}
	return false
}

func (s *store) endSubStory(isClear bool) (presentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeSubStoryID == 0 {
		return presentReceiveResult{}, errors.New("no active sub story")
	}
	activeID := s.activeSubStoryID
	for characterIndex := range s.storySubCharacters {
		for sectionIndex := range s.storySubCharacters[characterIndex].Sections {
			stories := s.storySubCharacters[characterIndex].Sections[sectionIndex].Stories
			for storyIndex := range stories {
				if stories[storyIndex].StorySubID != activeID {
					continue
				}
				if !isClear {
					s.activeSubStoryID = 0
					return presentReceiveResult{}, nil
				}
				result := presentReceiveResult{}
				if stories[storyIndex].StateFlag&2 == 0 {
					var err error
					result, err = s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.SubFirstClear)
					if err != nil {
						return presentReceiveResult{}, fmt.Errorf("apply character story first-clear reward: %w", err)
					}
				}
				stories[storyIndex].StateFlag = stories[storyIndex].StateFlag&8 | 2
				stories[storyIndex].UnlockText = ""
				if storyIndex+1 < len(stories) && stories[storyIndex+1].StateFlag&2 == 0 {
					stories[storyIndex+1].StateFlag = stories[storyIndex+1].StateFlag&8 | 1
					stories[storyIndex+1].UnlockText = ""
				}
				s.activeSubStoryID = 0
				return result, nil
			}
		}
	}
	for eventIndex := range s.storyEvents {
		stories := s.storyEvents[eventIndex].Stories
		for storyIndex := range stories {
			if stories[storyIndex].Sub.StorySubID != activeID {
				continue
			}
			if !isClear {
				s.activeSubStoryID = 0
				return presentReceiveResult{}, nil
			}
			result := presentReceiveResult{}
			if stories[storyIndex].Sub.StateFlag&2 == 0 {
				var err error
				result, err = s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.EventFirstClear)
				if err != nil {
					return presentReceiveResult{}, fmt.Errorf("apply event story first-clear reward: %w", err)
				}
			}
			stories[storyIndex].Sub.StateFlag = stories[storyIndex].Sub.StateFlag&8 | 2
			stories[storyIndex].Sub.UnlockText = ""
			if storyIndex+1 < len(stories) && stories[storyIndex+1].Sub.StateFlag&2 == 0 {
				nextStatic := stories[storyIndex+1].Sub.StateFlag & (8 | 16)
				if nextStatic&16 != 0 {
					stories[storyIndex+1].Sub.StateFlag = nextStatic
					stories[storyIndex+1].Sub.UnlockText = "需要活动材料解锁"
				} else {
					stories[storyIndex+1].Sub.StateFlag = nextStatic | 1
					stories[storyIndex+1].Sub.UnlockText = ""
				}
			}
			s.activeSubStoryID = 0
			return result, nil
		}
	}
	return presentReceiveResult{}, errors.New("active sub story is absent from the catalog")
}

const (
	friendStateOther    int8 = 0
	friendStateFriend   int8 = 3
	friendStateFollow   int8 = 5
	friendStateFollower int8 = 6
)

func (s *store) followMaximum() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followMax
}

func (s *store) friendMaximum() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentFriendMax
}

func (s *store) stampState() ([]int, []int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	owned := make([]int, 0, len(s.stampIDs))
	for stampID := range s.stampIDs {
		owned = append(owned, stampID)
	}
	sort.Ints(owned)
	return owned, append([]int{}, s.stampDeck...)
}

func (s *store) setStampDeck(stampIDs []int) error {
	if len(stampIDs) != stampDeckSlots {
		return fmt.Errorf("stamp deck must contain exactly %d slots", stampDeckSlots)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stampID := range stampIDs {
		if stampID == 0 {
			continue
		}
		if _, exists := s.stampIDs[stampID]; !exists {
			return fmt.Errorf("unknown stamp ID %d", stampID)
		}
	}
	s.stampDeck = append([]int(nil), stampIDs...)
	return nil
}

func normalizeStampDeck(stampIDs []int) []int {
	deck := make([]int, stampDeckSlots)
	copy(deck, stampIDs)
	return deck
}

func (s *store) naviID() int8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentNaviID
}

func (s *store) naviUnlockState() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.naviUnlockFlag
}

func (s *store) naviOwnershipState() map[int8]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[int8]struct{}, len(s.selectableNaviIDs))
	for id := range s.selectableNaviIDs {
		result[id] = struct{}{}
	}
	return result
}

func (s *store) selectNavi(id int8) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.selectableNaviIDs[id]; !exists {
		return false
	}
	s.currentNaviID = id
	return true
}

func (s *store) purchaseNavi(id int8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 0 {
		return errors.New("invalid navigator ID")
	}
	if _, exists := s.naviCatalogIDs[id]; !exists {
		return errors.New("navigator is absent from the local catalog")
	}
	if _, exists := s.selectableNaviIDs[id]; exists {
		return &businessError{-1, "已拥有这个看板。"}
	}
	price, enabled := s.naviPriceLocked(id)
	if !enabled {
		return &businessError{-1, "该看板暂未开放购买。"}
	}
	if s.coin+s.coinFree < price {
		return errInsufficientCrystals
	}
	freeSpend := min(s.coinFree, price)
	s.coinFree -= freeSpend
	s.coin -= price - freeSpend
	s.selectableNaviIDs[id] = struct{}{}
	s.naviUnlockFlag |= int64(1) << uint(id)
	return nil
}

func (s *store) presentState() ([]release.Present, []release.Present) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePresents(s.presents), clonePresents(s.presentHistories)
}

type receivedReward struct {
	Reward       release.Reward
	UniqueID     []int64
	IsNew        int8
	InPresentBox bool `json:",omitempty"`
}

type presentReceiveResult struct {
	InPresentBox bool
	Rewards      []receivedReward
	Cards        []cardInfo
	StackCards   []release.CardStack
	Items        []release.Item
	Spheres      []release.Sphere
	Buddies      []release.Buddy
	PresentID    []int64
	FailedID     []int64
}

type loginBonusClaim struct {
	Day    release.LoginBonusDay
	Result presentReceiveResult
}

type loginBonusClaims struct {
	Daily            *loginBonusClaim
	Beginner         *loginBonusClaim
	Total            *loginBonusClaim
	BeginnerSchedule []release.LoginBonusDay
	TotalMilestones  []release.LoginBonusDay
}

func (s *store) validateLoginBonusLocked() error {
	policy := s.loginBonusPolicy
	state := s.loginBonusState
	if policy.ConfigVersion <= 0 || state.ConfigVersion != policy.ConfigVersion ||
		len(policy.Cycle) == 0 || state.CycleDay < 0 || state.CycleDay > len(policy.Cycle) ||
		len(policy.Beginner) == 0 || state.BeginnerDay < 0 || state.BeginnerDay > len(policy.Beginner) ||
		len(policy.TotalMilestones) == 0 || state.TotalClaims < state.CycleDay ||
		state.TotalClaims < state.BeginnerDay ||
		(state.LastClaimDay == "" && (state.CycleDay != 0 || state.BeginnerDay != 0 || state.TotalClaims != 0)) {
		return errors.New("card store login bonus state is invalid")
	}
	for _, schedule := range []struct {
		Name        string
		Days        []release.LoginBonusDay
		Consecutive bool
	}{
		{Name: "daily", Days: policy.Cycle, Consecutive: true},
		{Name: "beginner", Days: policy.Beginner, Consecutive: true},
		{Name: "total", Days: policy.TotalMilestones},
	} {
		previousDay := 0
		for index, day := range schedule.Days {
			if day.Day <= previousDay || (schedule.Consecutive && day.Day != index+1) ||
				strings.TrimSpace(day.Comment) == "" {
				return fmt.Errorf("card store %s login bonus entry %d is invalid", schedule.Name, index+1)
			}
			if err := s.validateRewardLocked(day.Reward); err != nil {
				return fmt.Errorf("card store %s login bonus entry %d reward: %w", schedule.Name, index+1, err)
			}
			previousDay = day.Day
		}
	}
	return nil
}

func (s *store) claimLoginBonuses(now time.Time) (loginBonusClaims, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.currentName) == "" {
		return loginBonusClaims{}, nil
	}
	policy := s.loginBonusPolicy
	if err := s.validateLoginBonusLocked(); err != nil {
		return loginBonusClaims{}, err
	}
	location := time.FixedZone(
		"CN login bonus",
		policy.DayBoundaryOffsetMinutes*60,
	)
	dayKey := now.In(location).Format("2006-01-02")
	if s.loginBonusState.LastClaimDay != "" && dayKey <= s.loginBonusState.LastClaimDay {
		return loginBonusClaims{}, nil
	}
	if s.loginBonusState.TotalClaims == math.MaxInt {
		return loginBonusClaims{}, errors.New("login bonus total claim counter overflow")
	}
	nextDailyDay := s.loginBonusState.CycleDay%len(policy.Cycle) + 1
	nextTotalClaims := s.loginBonusState.TotalClaims + 1
	dailyDay := policy.Cycle[nextDailyDay-1]
	rewards := []release.Reward{dailyDay.Reward}
	var beginnerDay *release.LoginBonusDay
	if s.loginBonusState.BeginnerDay < len(policy.Beginner) {
		day := policy.Beginner[s.loginBonusState.BeginnerDay]
		beginnerDay = &day
		rewards = append(rewards, day.Reward)
	}
	var totalDay *release.LoginBonusDay
	for index := range policy.TotalMilestones {
		if policy.TotalMilestones[index].Day == nextTotalClaims {
			day := policy.TotalMilestones[index]
			totalDay = &day
			rewards = append(rewards, day.Reward)
			break
		}
	}
	if err := s.validateRewardBatchCapacityLocked(rewards); err != nil {
		return loginBonusClaims{}, err
	}
	claims := loginBonusClaims{
		BeginnerSchedule: cloneLoginBonusSchedule(policy.Beginner),
		TotalMilestones:  cloneLoginBonusSchedule(policy.TotalMilestones),
	}
	dailyResult := presentReceiveResult{}
	if err := s.applyRewardLocked(dailyDay.Reward, &dailyResult); err != nil {
		return loginBonusClaims{}, err
	}
	claims.Daily = &loginBonusClaim{Day: dailyDay, Result: dailyResult}
	if beginnerDay != nil {
		beginnerResult := presentReceiveResult{}
		if err := s.applyRewardLocked(beginnerDay.Reward, &beginnerResult); err != nil {
			return loginBonusClaims{}, err
		}
		claims.Beginner = &loginBonusClaim{Day: *beginnerDay, Result: beginnerResult}
		s.loginBonusState.BeginnerDay++
	}
	if totalDay != nil {
		totalResult := presentReceiveResult{}
		if err := s.applyRewardLocked(totalDay.Reward, &totalResult); err != nil {
			return loginBonusClaims{}, err
		}
		claims.Total = &loginBonusClaim{Day: *totalDay, Result: totalResult}
	}
	s.loginBonusState.LastClaimDay = dayKey
	s.loginBonusState.CycleDay = nextDailyDay
	s.loginBonusState.TotalClaims = nextTotalClaims
	return claims, nil
}

func (s *store) receivePresent(presentID int64) (presentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for current := range s.presents {
		if s.presents[current].PresentID == presentID {
			index = current
			break
		}
	}
	if index < 0 || s.presents[index].State != 0 {
		return presentReceiveResult{FailedID: []int64{presentID}}, nil
	}
	if err := s.validateRewardLocked(s.presents[index].Reward); err != nil {
		return presentReceiveResult{}, err
	}
	if err := s.validateRewardBatchCapacityLocked([]release.Reward{s.presents[index].Reward}); err != nil {
		return presentReceiveResult{FailedID: []int64{presentID}}, nil
	}
	result := presentReceiveResult{PresentID: []int64{presentID}}
	if err := s.applyRewardLocked(s.presents[index].Reward, &result); err != nil {
		return presentReceiveResult{}, err
	}
	// Native MovePresent2Received keeps the entry visible until Delete.
	s.presents[index].State = 1
	return result, nil
}

// Original CN Const.PRESENT_BOX_MULTI_RECV_MAX bounds the client confirmation.
const presentMultiReceiveMax = 20

func (s *store) receivePresents(receiveTypes []int, receiveCoin bool) (presentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	typeSet := make(map[int]struct{}, len(receiveTypes))
	for _, receiveType := range receiveTypes {
		if receiveType < 0 || receiveType > 2 {
			return presentReceiveResult{}, errors.New("unknown present receive type")
		}
		typeSet[receiveType] = struct{}{}
	}
	selected := make([]int, 0, presentMultiReceiveMax)
	rewards := make([]release.Reward, 0, presentMultiReceiveMax)
	result := presentReceiveResult{}
	candidates := 0
	// Match PresentMgr.ResortPresentList before its filtered Take(20).
	order := make([]int, len(s.presents))
	for i := range order {
		order[i] = i
	}
	now := time.Now().Unix()
	age := func(p release.Present) uint32 {
		if p.IssuedAtUnix > 0 {
			return uint32(min(max(0, now-p.IssuedAtUnix), math.MaxUint32))
		}
		return p.AddElapsedSec
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := s.presents[order[i]], s.presents[order[j]]
		if a.State != b.State {
			return a.State < b.State
		}
		if age(a) != age(b) {
			return age(a) < age(b)
		}
		return int32(b.PresentID-a.PresentID) < 0
	})
	for _, index := range order {
		present := s.presents[index]
		if _, ok := typeSet[s.presentReceiveTypeLocked(present)]; !ok {
			continue
		}
		if candidates == presentMultiReceiveMax {
			break
		}
		candidates++
		// The native list disables individual receipt for nonzero state, but
		// its batch request still selects the first twenty filtered entries.
		if present.State != 0 {
			result.FailedID = append(result.FailedID, present.PresentID)
			continue
		}
		// The client takes twenty filtered entries before asking about crystals.
		if present.Reward.Type == 10 && !receiveCoin {
			continue
		}
		if err := s.validateRewardLocked(present.Reward); err != nil {
			return presentReceiveResult{}, err
		}
		prospective := append(rewards, present.Reward)
		if err := s.validateRewardBatchCapacityLocked(prospective); err != nil {
			// Leave only this gift in the inbox. The native callback supports
			// successful and failed IDs together, including a fully blocked batch.
			result.FailedID = append(result.FailedID, present.PresentID)
			continue
		}
		selected = append(selected, index)
		rewards = prospective
	}
	for _, index := range selected {
		present := s.presents[index]
		if err := s.applyRewardLocked(present.Reward, &result); err != nil {
			return presentReceiveResult{}, err
		}
		result.PresentID = append(result.PresentID, present.PresentID)
		s.presents[index].State = 1
	}
	return result, nil
}

func (s *store) deletePresents(presentID int64) ([]int64, error) {
	if presentID < 0 {
		return nil, errors.New("invalid present ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	selected := make(map[int]struct{})
	if presentID == 0 {
		for index, present := range s.presents {
			if present.State == 1 && present.Reason != 19 {
				selected[index] = struct{}{}
			}
		}
	} else {
		index := -1
		for current, present := range s.presents {
			if present.PresentID == presentID {
				index = current
				break
			}
		}
		if index < 0 {
			for _, archived := range s.presentHistories {
				if archived.PresentID == presentID {
					// A delayed/retried native Delete must still remove its cached row.
					return []int64{presentID}, nil
				}
			}
			return nil, &businessError{-1, "这封礼物已不存在，请重新打开礼物箱。"}
		}
		if s.presents[index].State != 1 {
			return nil, &businessError{-1, "请先领取这封礼物，再删除。"}
		}
		if s.presents[index].Reason == 19 {
			return nil, &businessError{-1, "这封礼物暂时不能删除。"}
		}
		selected[index] = struct{}{}
	}

	deleted := make([]int64, 0, len(selected))
	remaining := make([]release.Present, 0, len(s.presents)-len(selected))
	for index, present := range s.presents {
		if _, exists := selected[index]; !exists {
			remaining = append(remaining, present)
			continue
		}
		deleted = append(deleted, present.PresentID)
		removed := clonePresent(present)
		removed.State = 1
		s.presentHistories = append(s.presentHistories, removed)
	}
	s.presents = remaining
	return deleted, nil
}

func (s *store) missionState() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, mission := range s.missions {
		if mission.Info.State == 1 {
			return true
		}
	}
	return false
}

func (s *store) missionInfos() []release.MissionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]release.MissionInfo, len(s.missions))
	for index, mission := range s.missions {
		result[index] = cloneMissionInfo(mission.Info)
	}
	return result
}

func (s *store) checkMissionOpenURL(openURL string) ([]release.MissionInfo, error) {
	if openURL == "" {
		return nil, errors.New("mission URL is empty")
	}
	if !isLocalMissionCommand(openURL) {
		return nil, errors.New("mission URL is outside the local client boundary")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, mission := range s.missions {
		if mission.Info.OpenURL == openURL {
			// The client opens the already-published command after this check.
			// This route has no evidenced progress rule, so it returns no
			// synthetic mission mutations.
			return []release.MissionInfo{}, nil
		}
	}
	return nil, errors.New("mission URL is not published")
}

func isLocalMissionCommand(command string) bool {
	if strings.Contains(command, "external") ||
		strings.Contains(command, "http") ||
		strings.Contains(command, "GetUrlToken") ||
		strings.Contains(command, "netease_sprite") ||
		strings.Contains(command, "eventpage") ||
		strings.Contains(command, "trade") ||
		strings.Contains(command, "review") ||
		strings.Contains(command, "monthcard") ||
		strings.Contains(command, "firstpay") ||
		strings.Contains(command, "shop_crystal") {
		return false
	}
	for _, marker := range []string{
		"explore",
		"teambattleone",
		"teambattle_join",
		"teambattle",
		"gacha",
		"arena",
		"mainstory",
		"substory",
		"eventstory",
		"exp_fusion",
		"evo_fusion",
		"card_collection",
		"createroom",
		"uplove",
		"sellcard",
		"enterroom",
		"gotoscene",
		"eventboss",
		"pastboss",
		"exchange",
		"mission",
		"towerquest",
	} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

func (s *store) receiveMissionRewards(missionIDs []int) ([]release.MissionInfo, []release.MissionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(missionIDs) == 0 {
		return nil, nil, errors.New("mission selection is empty")
	}
	indices := make([]int, len(missionIDs))
	seen := make(map[int]struct{}, len(missionIDs))
	for resultIndex, missionID := range missionIDs {
		if _, duplicate := seen[missionID]; duplicate {
			return nil, nil, errors.New("duplicate mission ID")
		}
		seen[missionID] = struct{}{}
		index := -1
		for current := range s.missions {
			if s.missions[current].Info.MissionID == missionID {
				index = current
				break
			}
		}
		if index < 0 || s.missions[index].Info.State != 1 {
			return nil, nil, errors.New("mission is not claimable")
		}
		if err := s.validateRewardLocked(s.missions[index].RewardPresent.Reward); err != nil {
			return nil, nil, err
		}
		for _, present := range s.presents {
			if present.PresentID == s.missions[index].RewardPresent.PresentID {
				return nil, nil, errors.New("mission reward present already exists")
			}
		}
		indices[resultIndex] = index
	}
	rewardMissions := make([]release.MissionInfo, 0, len(indices))
	for _, index := range indices {
		s.missions[index].Info.State = 2
		rewardMissions = append(rewardMissions, cloneMissionInfo(s.missions[index].Info))
		present := clonePresent(s.missions[index].RewardPresent)
		present.IssuedAtUnix = time.Now().Unix()
		s.presents = append(s.presents, present)
	}
	receiveMissions := make([]release.MissionInfo, 0, len(s.missions)-len(indices))
	for _, mission := range s.missions {
		if mission.Info.State != 2 {
			receiveMissions = append(receiveMissions, cloneMissionInfo(mission.Info))
		}
	}
	return rewardMissions, receiveMissions, nil
}

func (s *store) presentReceiveTypeLocked(present release.Present) int {
	// PresentBoxInfoEx.OnThatDayOnly reads item.csv's limit flag. A gift's
	// own expiry does not make gold, crystals or ordinary items ITEM_TODAY.
	if present.Reward.Type == 8 && s.itemDefinitions[present.Reward.RewardTypeID].DailyLimited != 0 {
		return 1
	}
	if present.Reward.Type == 6 {
		return 0
	}
	return 2
}

func (s *store) validateRewardLocked(reward release.Reward) error {
	if reward.Num <= 0 {
		return errors.New("reward quantity must be positive")
	}
	switch reward.Type {
	case 0:
		if s.playerProgression.ConfigVersion <= 0 {
			return errors.New("player EXP reward requires a progression policy")
		}
		return nil
	case 4, 9, 10, 12:
		if reward.RewardTypeID != 0 {
			return errors.New("scalar reward has an unexpected ID")
		}
		return nil
	case 6:
		definition, ok := s.cardDefinitions[reward.RewardTypeID]
		if !ok {
			return errors.New("reward references an unknown card")
		}
		level := int(reward.CardLevel)
		if level == 0 {
			level = 1
		}
		fame := int(reward.CardFame)
		if fame == 0 {
			fame = 1
		}
		if level < 1 || level > definition.LevelMax || fame < 1 || fame > definition.FameMax ||
			reward.CardLove < 0 || reward.CardLove > definition.LoveMax || len(reward.CardSkillLevels) == 0 {
			return errors.New("card reward progression fields are invalid")
		}
		return nil
	case 8:
		if _, exists := s.itemDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("reward references an unknown item")
		}
		return nil
	case 13:
		if _, exists := s.stackCardTemplates[reward.RewardTypeID]; exists {
			return nil
		}
		return errors.New("reward references an unknown stack card")
	case 15:
		if _, exists := s.sphereDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("reward references an unknown sphere")
		}
		return nil
	case 19:
		if _, exists := s.buddyDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("reward references an unknown buddy")
		}
		return nil
	default:
		return fmt.Errorf("unsupported local reward type %d", reward.Type)
	}
}

func (s *store) validateRewardBatchCapacityLocked(rewards []release.Reward) error {
	gold := s.gold
	friendPoint := s.friendPoint
	coinFree := s.coinFree
	cardCount := len(s.cards)
	sphereCount := len(s.spheres)
	buddyCount := len(s.buddies)
	nextCardUniqueID := s.nextUniqueID
	nextSphereUniqueID := s.nextSphereUniqueID
	nextBuddyUniqueID := s.nextBuddyUniqueID
	itemCounts := make(map[int]int)
	stackCounts := make(map[int]int)
	for itemID, item := range s.items {
		itemCounts[itemID] = item.Num
	}
	for _, stack := range s.stackCards {
		stackCounts[stack.CardID] = stack.Num
	}
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
		switch reward.Type {
		case 4:
			if reward.Num > math.MaxInt-gold {
				return errors.New("gold reward overflows")
			}
			gold += reward.Num
		case 9:
			if reward.Num > math.MaxInt-friendPoint {
				return errors.New("friend-point reward overflows")
			}
			friendPoint += reward.Num
		case 6:
			if reward.Num > s.cardMax-cardCount || int64(reward.Num) > math.MaxInt64-nextCardUniqueID {
				return errCardCapacity
			}
			cardCount += reward.Num
			nextCardUniqueID += int64(reward.Num)
		case 8:
			definition := s.itemDefinitions[reward.RewardTypeID]
			owned := itemCounts[reward.RewardTypeID]
			if reward.Num > definition.MaxOwned-owned {
				return errItemCapacity
			}
			itemCounts[reward.RewardTypeID] = owned + reward.Num
		case 10:
			if reward.Num > math.MaxInt-coinFree {
				return errors.New("crystal reward overflows")
			}
			coinFree += reward.Num
		case 13:
			owned := stackCounts[reward.RewardTypeID]
			if reward.Num > math.MaxInt-owned {
				return errors.New("stack-card reward overflows")
			}
			stackCounts[reward.RewardTypeID] = owned + reward.Num
		case 15:
			if reward.Num > s.sphereMax-sphereCount || int64(reward.Num) > math.MaxInt64-nextSphereUniqueID {
				return errSphereCapacity
			}
			sphereCount += reward.Num
			nextSphereUniqueID += int64(reward.Num)
		case 19:
			if reward.Num > s.buddyMax-buddyCount || int64(reward.Num) > math.MaxInt64-nextBuddyUniqueID {
				return errBuddyCapacity
			}
			buddyCount += reward.Num
			nextBuddyUniqueID += int64(reward.Num)
		}
	}
	return nil
}

func (s *store) validateEngagementLocked() error {
	missionIDs := make(map[int]struct{}, len(s.missions))
	for _, mission := range s.missions {
		if mission.Info.MissionID <= 0 || mission.Info.Title == "" ||
			mission.Info.State < 0 || mission.Info.State > 2 ||
			mission.RewardPresent.PresentID <= 0 {
			return errors.New("mission configuration is incomplete")
		}
		if _, duplicate := missionIDs[mission.Info.MissionID]; duplicate {
			return errors.New("mission IDs must be unique")
		}
		missionIDs[mission.Info.MissionID] = struct{}{}
		if mission.Info.State == 1 {
			if err := s.validateRewardLocked(mission.RewardPresent.Reward); err != nil {
				return err
			}
		}
	}
	presentIDs := make(map[int64]struct{}, len(s.presents)+len(s.presentHistories))
	for _, group := range [][]release.Present{s.presents, s.presentHistories} {
		for _, present := range group {
			if present.PresentID <= 0 || present.Title == "" {
				return errors.New("present configuration is incomplete")
			}
			if _, duplicate := presentIDs[present.PresentID]; duplicate {
				return errors.New("active and received present IDs must be unique")
			}
			presentIDs[present.PresentID] = struct{}{}
		}
	}
	for _, present := range s.presents {
		if err := s.validateRewardLocked(present.Reward); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) applyRewardLocked(reward release.Reward, result *presentReceiveResult) error {
	received := receivedReward{Reward: cloneReward(reward), UniqueID: []int64{}}
	switch reward.Type {
	case 0:
		s.applyPlayerExperienceLocked(reward.Num)
	case 4:
		s.gold += reward.Num
	case 9:
		s.friendPoint += reward.Num
	case 6:
		owned := false
		for _, inventory := range [][]cardInfo{s.cards, s.containerCards} {
			for _, current := range inventory {
				if current.CardID == reward.RewardTypeID {
					owned = true
					break
				}
			}
			if owned {
				break
			}
		}
		if !owned {
			received.IsNew = 1
		}
		template := s.cardTemplates[reward.RewardTypeID]
		prepared := make([]cardInfo, 0, reward.Num)
		for count := 0; count < reward.Num; count++ {
			card := cloneCard(template)
			card.UniqueID = s.nextUniqueID + int64(count)
			level := int(reward.CardLevel)
			if level == 0 {
				level = 1
			}
			card.Level = level
			experience, err := release.CardExperienceAtLevelStart(
				level, card.LevelMax, s.cardExperience[s.cardDefinitions[card.CardID].ExperienceTableID],
			)
			if err != nil {
				return err
			}
			card.Experience = experience
			card.Love = reward.CardLove
			card.Fame = int(reward.CardFame)
			if card.Fame == 0 {
				card.Fame = 1
			}
			if len(reward.CardSkillLevels) > 0 {
				card.SkillLevels = append([]int16(nil), reward.CardSkillLevels...)
			}
			card, err = s.normalizeCardLocked(card)
			if err != nil {
				return err
			}
			prepared = append(prepared, card)
			received.UniqueID = append(received.UniqueID, card.UniqueID)
		}
		s.nextUniqueID += int64(len(prepared))
		s.cards = append(s.cards, prepared...)
		result.Cards = append(result.Cards, prepared...)
		for _, card := range prepared {
			s.recordCollectedCardLocked(card)
		}
	case 8:
		item := s.items[reward.RewardTypeID]
		item.ItemID = reward.RewardTypeID
		item.Num += reward.Num
		s.items[item.ItemID] = item
		result.Items = append(result.Items, item)
	case 10:
		s.coinFree += reward.Num
	case 12:
		if reward.Num >= s.bpMax-s.bp {
			s.bp = s.bpMax
		} else {
			s.bp += reward.Num
		}
		if s.bp == s.bpMax {
			s.bpNextRecovery = time.Time{}
		}
	case 13:
		owned := false
		for index := range s.stackCards {
			if s.stackCards[index].CardID != reward.RewardTypeID {
				continue
			}
			s.stackCards[index].Num += reward.Num
			owned = true
			break
		}
		delta := s.stackCardTemplates[reward.RewardTypeID]
		delta.Num = reward.Num
		result.StackCards = append(result.StackCards, delta)
		s.recordCollectedCardLocked(cardInfo{CardID: reward.RewardTypeID})
		if !owned {
			s.stackCards = append(s.stackCards, delta)
			// CN 6.0.2 consumes type-13 rewards through new_stack_cards. Marking
			// the result row as new instead routes the stack material through the
			// normal-card detail animation, where CardInfoWindow dereferences a
			// CardInfo shape that stack materials do not provide. Keep the local
			// inventory delta and suppress the normal-card UI flag.
		}
	case 15:
		isNew := true
		for _, sphere := range s.spheres {
			if sphere.SphereID == reward.RewardTypeID {
				isNew = false
				break
			}
		}
		if isNew {
			received.IsNew = 1
		}
		definition := s.sphereDefinitions[reward.RewardTypeID]
		prepared := make([]release.Sphere, reward.Num)
		now := int(time.Now().Unix())
		for count := 0; count < reward.Num; count++ {
			sphere, err := s.normalizeSphereLocked(release.Sphere{
				UniqueID: s.nextSphereUniqueID + int64(count), SphereID: reward.RewardTypeID,
				Level: 1, CreateTime: now,
			}, definition)
			if err != nil {
				return err
			}
			prepared[count] = sphere
			received.UniqueID = append(received.UniqueID, sphere.UniqueID)
		}
		s.nextSphereUniqueID += int64(len(prepared))
		s.spheres = append(s.spheres, prepared...)
		result.Spheres = append(result.Spheres, prepared...)
	case 19:
		isNew := true
		for _, buddy := range s.buddies {
			if buddy.BuddyID == reward.RewardTypeID {
				isNew = false
				break
			}
		}
		if isNew {
			received.IsNew = 1
		}
		definition := s.buddyDefinitions[reward.RewardTypeID]
		prepared := make([]release.Buddy, reward.Num)
		now := int(time.Now().Unix())
		for count := 0; count < reward.Num; count++ {
			buddy, err := s.normalizeBuddyLocked(release.Buddy{
				UniqueID: s.nextBuddyUniqueID + int64(count), BuddyID: reward.RewardTypeID,
				Level: 1, CreateTime: now,
			}, definition)
			if err != nil {
				return err
			}
			prepared[count] = buddy
			received.UniqueID = append(received.UniqueID, buddy.UniqueID)
		}
		s.nextBuddyUniqueID += int64(len(prepared))
		s.buddies = append(s.buddies, prepared...)
		result.Buddies = append(result.Buddies, prepared...)
	default:
		return fmt.Errorf("unsupported local reward type %d", reward.Type)
	}
	result.Rewards = append(result.Rewards, received)
	return nil
}

func (s *store) applyPlayerExperienceLocked(amount int) {
	policy := s.playerProgression
	if amount <= 0 || policy.ConfigVersion <= 0 || s.currentLevel >= policy.MaxLevel {
		return
	}
	remaining := amount
	leveledUp := false
	for remaining > 0 && s.currentLevel < policy.MaxLevel {
		required := policy.ExperienceRequired(s.currentLevel)
		needed := required - s.currentLevelExperience
		if remaining < needed {
			s.currentLevelExperience += remaining
			remaining = 0
			break
		}
		remaining -= needed
		s.currentLevel++
		s.currentLevelExperience = 0
		leveledUp = true
	}
	if s.currentLevel >= policy.MaxLevel {
		s.currentLevel = policy.MaxLevel
		s.currentExperience = policy.CumulativeExperience(policy.MaxLevel)
		s.currentLevelExperience = 0
		s.nextLevelExperience = 0
	} else {
		s.currentExperience = policy.CumulativeExperience(s.currentLevel) + s.currentLevelExperience
		s.nextLevelExperience = policy.ExperienceRequired(s.currentLevel) - s.currentLevelExperience
	}
	if !leveledUp {
		return
	}
	s.bpMax = policy.BattlePointMaximum(s.currentLevel)
	s.currentFriendMax = policy.FriendMaximum(s.currentLevel)
	s.currentJobs = policy.JobsAtLevel(s.currentLevel)
	if policy.LevelUpRecovery.AP {
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
	}
	if policy.LevelUpRecovery.BP {
		s.bp = s.bpMax
		s.bpNextRecovery = time.Time{}
	}
}

func (s *store) show() ([]cardInfo, []deckInfo) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCards(s.cards), s.rankedDecksLocked(s.decks)
}

func (s *store) containerShow() []cardInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCards(s.containerCards)
}

func (s *store) cardCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cards)
}

func (s *store) containerCardCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.containerCards)
}

func (s *store) stackState() []release.CardStack {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]release.CardStack(nil), s.stackCards...)
}

func (s *store) goldState() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gold
}

func (s *store) friendPointState() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.friendPoint
}

func (s *store) friendPointHelperReward() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.playerProgression.Friends.HelperReward.OtherPerPartner
}

func (s *store) friendPointRewardForState(state int8) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state == friendStateFriend {
		return s.playerProgression.Friends.HelperReward.FriendPerPartner
	}
	return s.playerProgression.Friends.HelperReward.OtherPerPartner
}

func (s *store) coinState() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.coin, s.coinFree
}

type battlePointStatus struct {
	Current     int
	Max         int
	NextSeconds int
	HealSeconds int
}

func (s *store) battlePointState() battlePointStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.battlePointStatusLocked(time.Now())
}

func (s *store) refreshBattlePointsLocked(now time.Time) {
	if s.bp >= s.bpMax {
		s.bp = s.bpMax
		s.bpNextRecovery = time.Time{}
		return
	}
	if s.bpNextRecovery.IsZero() {
		s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
		return
	}
	if now.Before(s.bpNextRecovery) {
		return
	}
	elapsed := now.Sub(s.bpNextRecovery)
	recovered := 1 + int(elapsed/s.bpRecoveryInterval)
	s.bp += recovered
	if s.bp >= s.bpMax {
		s.bp = s.bpMax
		s.bpNextRecovery = time.Time{}
		return
	}
	s.bpNextRecovery = s.bpNextRecovery.Add(
		time.Duration(recovered) * s.bpRecoveryInterval,
	)
}

func (s *store) battlePointStatusLocked(now time.Time) battlePointStatus {
	s.refreshBattlePointsLocked(now)
	status := battlePointStatus{Current: s.bp, Max: s.bpMax}
	if s.bp >= s.bpMax {
		return status
	}
	remaining := s.bpNextRecovery.Sub(now)
	status.NextSeconds = int((remaining + time.Second - 1) / time.Second)
	missingAfterNext := s.bpMax - s.bp - 1
	status.HealSeconds = status.NextSeconds +
		missingAfterNext*int(s.bpRecoveryInterval/time.Second)
	return status
}

func (s *store) stageQuestPayload(
	areaID int,
	selectArea bool,
) (json.RawMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selectedAreaID := s.defaultStageQuestAreaID
	if selectArea {
		if _, exists := s.stageQuests[areaID]; !exists {
			return nil, false, errors.New("unknown local stage quest area")
		}
		unlocked, hasNormalQuest, err := unlockedCNNormalQuestAreas(s.stageQuests)
		if err != nil {
			return nil, false, err
		}
		if hasNormalQuest && areaID >= cnNormalQuestAreaMin && areaID <= cnNormalQuestAreaMax {
			if _, available := unlocked[areaID]; !available {
				return nil, false, errors.New("local stage quest area is not unlocked")
			}
		}
		selectedAreaID = areaID
		s.pendingStageAreaID = areaID
	}
	configuration, exists := s.stageQuests[selectedAreaID]
	if !exists {
		return nil, false, errors.New("local stage quest default area is unavailable")
	}
	response, err := projectCNStageQuestPublication(configuration)
	if err != nil {
		return nil, false, err
	}
	updated, consumed, err := consumeStageQuestNewClear(configuration)
	if err != nil {
		return nil, false, err
	}
	if consumed {
		s.stageQuests[selectedAreaID] = updated
		if selectedAreaID == s.defaultStageQuestAreaID {
			s.mainQuest = append(json.RawMessage(nil), updated...)
		}
	}
	return response, consumed, nil
}

func stageQuestConfigurations(
	mainQuest json.RawMessage,
	areas []json.RawMessage,
) (map[int]json.RawMessage, int, error) {
	defaultAreaID, err := stageQuestAreaID(mainQuest)
	if err != nil {
		return nil, 0, err
	}
	result := make(map[int]json.RawMessage, max(1, len(areas)))
	for _, area := range areas {
		areaID, areaErr := stageQuestAreaID(area)
		if areaErr != nil {
			return nil, 0, areaErr
		}
		if _, duplicate := result[areaID]; duplicate {
			return nil, 0, fmt.Errorf("duplicate local stage quest area %d", areaID)
		}
		result[areaID] = append(json.RawMessage(nil), area...)
	}
	if len(areas) > 0 {
		if _, exists := result[defaultAreaID]; !exists {
			return nil, 0, errors.New("local default stage quest is absent from the area catalog")
		}
	}
	result[defaultAreaID] = append(json.RawMessage(nil), mainQuest...)
	return result, defaultAreaID, nil
}

func (s *store) clearPendingStageQuest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.pendingStageAreaID != 0
	s.pendingStageAreaID = 0
	return changed
}

func (s *store) validateActiveTeamBattleContextLocked(
	context teamBattleContext,
	profiles []release.TeamBattleRewardProfile,
) error {
	if context.BossID <= 0 || len(context.BattleEnemyTypes) == 0 ||
		len(context.BattleEnemyTypes) > maxTeamBattleSegments {
		return errors.New("local team battle segment contract is unavailable")
	}
	for _, enemyType := range context.BattleEnemyTypes {
		if enemyType < 0 || enemyType > 4 {
			return errors.New("local team battle segment enemy type is invalid")
		}
	}
	if strings.TrimSpace(context.FameSeed) == "" || len(context.FameSources) == 0 ||
		len(context.FameSources) > 4 {
		return errors.New("local team battle fame source is unavailable")
	}
	seenFameArthurTypes := make(map[int]struct{}, len(context.FameSources))
	for _, source := range context.FameSources {
		if source.ArthurType < 1 || source.ArthurType > 4 || source.LeaderFame <= 0 ||
			source.LeaderFame > 100 {
			return errors.New("local team battle fame source is invalid")
		}
		if _, duplicate := seenFameArthurTypes[source.ArthurType]; duplicate {
			return errors.New("local team battle fame source is duplicated")
		}
		seenFameArthurTypes[source.ArthurType] = struct{}{}
	}
	if context.HostBonusArthurType < 0 || context.HostBonusArthurType > 4 {
		return errors.New("local team battle host-bonus Arthur type is invalid")
	}
	if context.HostBonusArthurType != 0 {
		if _, exists := seenFameArthurTypes[context.HostBonusArthurType]; !exists {
			return errors.New("local team battle host-bonus Arthur is unavailable")
		}
		if !context.ConsumesBattlePoints {
			return errors.New("local team battle host must consume battle points")
		}
	}
	friendPointPolicy := s.playerProgression.Friends.HelperReward
	if friendPointPolicy.SourceState != "PLACEHOLDER" ||
		friendPointPolicy.OtherPerPartner <= 0 ||
		friendPointPolicy.FriendPerPartner <= friendPointPolicy.OtherPerPartner ||
		friendPointPolicy.MaximumPartners != 3 || context.FriendPointPartners < 0 ||
		context.FriendPointPartners > friendPointPolicy.MaximumPartners {
		return errors.New("local friend-point helper policy is invalid")
	}
	minimumReward := friendPointPolicy.OtherPerPartner * context.FriendPointPartners
	maximumReward := friendPointPolicy.FriendPerPartner * context.FriendPointPartners
	if context.FriendPointReward < minimumReward || context.FriendPointReward > maximumReward {
		return errors.New("local friend-point helper reward is invalid")
	}
	seenRentalOwners := make(map[int]struct{}, len(context.FriendPointRentalCredits))
	rentalReward := 0
	for _, credit := range context.FriendPointRentalCredits {
		if credit.OwnerUserID <= 0 || len(context.FriendPointRentalCredits) > context.FriendPointPartners ||
			(credit.FriendPoint != friendPointPolicy.OtherPerPartner &&
				credit.FriendPoint != friendPointPolicy.FriendPerPartner) {
			return errors.New("local friend-point rental owner is invalid")
		}
		if _, duplicate := seenRentalOwners[credit.OwnerUserID]; duplicate {
			return errors.New("local friend-point rental owner is duplicated")
		}
		seenRentalOwners[credit.OwnerUserID] = struct{}{}
		rentalReward += credit.FriendPoint
	}
	if rentalReward > context.FriendPointReward ||
		(len(context.FriendPointRentalCredits) == 0) !=
			(strings.TrimSpace(context.FriendPointRentalEventKey) == "") {
		return errors.New("local friend-point rental event key is invalid")
	}
	if len(context.SelectedPartners) != 0 &&
		(len(context.SelectedPartners) != 3 || rentalPartnerCount(context.SelectedPartners) != context.FriendPointPartners) {
		return errors.New("local team battle selected-partner projection is invalid")
	}
	seenPartnerUsers := make(map[int]struct{}, len(context.SelectedPartners))
	seenPartnerTypes := make(map[int8]struct{}, len(context.SelectedPartners))
	for _, partner := range context.SelectedPartners {
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 ||
			partner.Level <= 0 || partner.LeaderCardID <= 0 || partner.LeaderLevel <= 0 ||
			partner.LeaderFame <= 0 || partner.LeaderFame > 100 || partner.PVPPoint < 0 {
			return errors.New("local team battle selected partner is invalid")
		}
		if _, duplicate := seenPartnerUsers[partner.UserID]; duplicate && !partner.IsSelf {
			return errors.New("local team battle selected partner user is duplicated")
		}
		if _, duplicate := seenPartnerTypes[partner.ArthurType]; duplicate {
			return errors.New("local team battle selected partner Arthur type is duplicated")
		}
		seenPartnerUsers[partner.UserID] = struct{}{}
		seenPartnerTypes[partner.ArthurType] = struct{}{}
	}
	stageBattle := context.StageQuestAreaID != 0 || context.StageQuestStageID != 0
	towerBattle := context.TowerID != 0 || context.TowerFloor != 0 ||
		context.ItemID != 0 || context.ItemUse != 0
	if (context.StageQuestAreaID == 0) != (context.StageQuestStageID == 0) ||
		stageBattle && towerBattle || context.BPUse < 0 {
		return errors.New("local team battle context locator is invalid")
	}
	if context.PrepaidRoomID != 0 {
		bpUse, found := s.prepaidTeamBattleCostLocked(context.PrepaidRoomID, context.BossID)
		if !found || !context.ConsumesBattlePoints || bpUse != context.BPUse || towerBattle {
			return errors.New("multiplayer prepaid start receipt is unavailable")
		}
	}
	switch {
	case stageBattle:
		stageQuest, exists := s.stageQuests[context.StageQuestAreaID]
		if !exists {
			return errors.New("local StageQuest battle context is unavailable")
		}
		stageID, bpUse, found, err := stageQuestBattleForBoss(
			stageQuest, context.StageQuestAreaID, context.BossID,
		)
		if err != nil {
			return err
		}
		if !found || stageID != context.StageQuestStageID || (context.ConsumesBattlePoints && context.PrepaidRoomID == 0 && bpUse != context.BPUse) || (!context.ConsumesBattlePoints && context.BPUse != 0) {
			return errors.New("local StageQuest battle context differs from the published stage")
		}
	case towerBattle:
		tower, floor, found := s.towerQuestBattleForBossLocked(context.BossID)
		if !found || tower.TowerID != context.TowerID || floor.Floor != context.TowerFloor ||
			tower.ItemID != context.ItemID || tower.ItemUse != context.ItemUse || context.BPUse != 0 {
			return errors.New("local tower battle context differs from the published floor")
		}
	default:
		if context.ConsumesBattlePoints && context.PrepaidRoomID == 0 {
			bpUse, found := teamBattleSoloBossBPUse(s.teamBattleSolo, context.BossID)
			if !found || bpUse != context.BPUse || context.BPUse > s.bpMax {
				return errors.New("local team battle BP cost is invalid")
			}
		} else if !context.ConsumesBattlePoints && context.BPUse != 0 {
			return errors.New("local non-host team battle must not consume battle points")
		}
	}
	if _, found := teamBattleRewardProfileForContext(profiles, context); !found {
		return errors.New("local team battle reward profile is unavailable")
	}
	return nil
}

func (s *store) beginTeamBattle(
	bossID int,
	battleEnemyTypes []int8,
	standaloneBPUse int,
	consumeBattlePoints bool,
	profiles []release.TeamBattleRewardProfile,
	fameSeed string,
	fameSources []teamBattleFameSource,
	hostBonusArthurType int,
	friendPointPartners int,
	friendPointReward int,
	friendPointRentalCredits []FriendPointRentalCredit,
	friendPointRentalEventKey string,
	selectedPartners []teamBattleResultPartner,
	prepaidRoomID ...int64,
) (teamBattleContext, battlePointStatus, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.refreshBattlePointsLocked(now)
	if s.activeBattle != nil {
		// Retrying the same start or completed room must not debit again.
		if s.activeBattle.FameSeed == fameSeed && s.activeBattle.BossID == bossID {
			return *teamBattleContextFromRelease(releaseTeamBattleContext(s.activeBattle)), s.battlePointStatusLocked(now), true, nil
		}
		// An explicit new solo start replaces an abandoned solo run only after
		// all validation/payment below succeeds. Keep multiplayer settlement
		// retries separate from this single-client lifecycle.
		if !strings.HasPrefix(fameSeed, "solo:") || !strings.HasPrefix(s.activeBattle.FameSeed, "solo:") {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("a local team battle is already active")
		}
	}
	if len(battleEnemyTypes) == 0 || len(battleEnemyTypes) > maxTeamBattleSegments {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local team battle segment contract is unavailable")
	}
	for _, enemyType := range battleEnemyTypes {
		if enemyType < 0 || enemyType > 4 {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local team battle segment enemy type is invalid")
		}
	}
	if strings.TrimSpace(fameSeed) == "" || len(fameSources) == 0 {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local team battle fame source is unavailable")
	}
	seenFameArthurTypes := make(map[int]struct{}, len(fameSources))
	for _, source := range fameSources {
		if source.ArthurType < 1 || source.ArthurType > 4 || source.LeaderFame <= 0 ||
			source.LeaderFame > 100 {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local team battle fame source is invalid")
		}
		if _, duplicate := seenFameArthurTypes[source.ArthurType]; duplicate {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local team battle fame source is duplicated")
		}
		seenFameArthurTypes[source.ArthurType] = struct{}{}
	}
	if hostBonusArthurType < 0 || hostBonusArthurType > 4 {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local team battle host-bonus Arthur type is invalid")
	}
	if hostBonusArthurType != 0 {
		if _, exists := seenFameArthurTypes[hostBonusArthurType]; !exists {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local team battle host-bonus Arthur is unavailable")
		}
		if !consumeBattlePoints {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local team battle host must consume battle points")
		}
	}
	friendPointPolicy := s.playerProgression.Friends.HelperReward
	if friendPointPolicy.SourceState != "PLACEHOLDER" ||
		friendPointPolicy.OtherPerPartner <= 0 ||
		friendPointPolicy.FriendPerPartner <= friendPointPolicy.OtherPerPartner ||
		friendPointPolicy.MaximumPartners != 3 ||
		friendPointPartners < 0 || friendPointPartners > friendPointPolicy.MaximumPartners {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local friend-point helper policy is invalid")
	}
	minimumReward := friendPointPolicy.OtherPerPartner * friendPointPartners
	maximumReward := friendPointPolicy.FriendPerPartner * friendPointPartners
	if friendPointReward < minimumReward || friendPointReward > maximumReward {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local friend-point helper reward is invalid")
	}
	seenRentalOwners := make(map[int]struct{}, len(friendPointRentalCredits))
	rentalReward := 0
	for _, credit := range friendPointRentalCredits {
		if credit.OwnerUserID <= 0 || len(friendPointRentalCredits) > friendPointPartners ||
			(credit.FriendPoint != friendPointPolicy.OtherPerPartner &&
				credit.FriendPoint != friendPointPolicy.FriendPerPartner) {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local friend-point rental owner is invalid")
		}
		if _, duplicate := seenRentalOwners[credit.OwnerUserID]; duplicate {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local friend-point rental owner is duplicated")
		}
		seenRentalOwners[credit.OwnerUserID] = struct{}{}
		rentalReward += credit.FriendPoint
	}
	if rentalReward > friendPointReward ||
		(len(friendPointRentalCredits) == 0) != (strings.TrimSpace(friendPointRentalEventKey) == "") {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local friend-point rental event key is invalid")
	}
	if len(selectedPartners) != 0 && (len(selectedPartners) != 3 || rentalPartnerCount(selectedPartners) != friendPointPartners) {
		return teamBattleContext{}, battlePointStatus{}, false,
			errors.New("local team battle selected-partner projection is invalid")
	}
	selectedPartnerCopies := make([]teamBattleResultPartner, len(selectedPartners))
	for index, partner := range selectedPartners {
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 ||
			partner.Level <= 0 || partner.LeaderCardID <= 0 || partner.LeaderLevel <= 0 || partner.LeaderFame <= 0 {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("local team battle selected partner is invalid")
		}
		partner.HonorIDs = append([]int(nil), partner.HonorIDs...)
		selectedPartnerCopies[index] = partner
	}
	context := teamBattleContext{
		Seed:   newBattleSeed(),
		BossID: bossID, BattleEnemyTypes: append([]int8(nil), battleEnemyTypes...),
		BPUse: standaloneBPUse, ConsumesBattlePoints: consumeBattlePoints,
		FameSeed: fameSeed, FameSources: append([]teamBattleFameSource(nil), fameSources...),
		HostBonusArthurType: hostBonusArthurType, FriendPointPartners: friendPointPartners,
		FriendPointReward:         friendPointReward,
		FriendPointRentalCredits:  append([]FriendPointRentalCredit(nil), friendPointRentalCredits...),
		FriendPointRentalEventKey: friendPointRentalEventKey,
		SelectedPartners:          selectedPartnerCopies,
	}
	if len(prepaidRoomID) > 0 {
		context.PrepaidRoomID = prepaidRoomID[0]
	}
	if strings.HasPrefix(fameSeed, "solo:") {
		rules, found := s.teamBattleEntryRulesForBossLocked(bossID)
		if !found || !rules.allowsSolo() {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("solo battle entry is unavailable")
		}
		if rules.OnlyMyDeck != 0 && (len(selectedPartners) != 3 || rentalPartnerCount(selectedPartners) != 0) {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("this battle requires the player's own decks")
		}
		// Keep the accepted rule with the entry debit and in-memory battle state.
		// Publishing a new schedule or rule must not change an ongoing battle.
		context.ContinueAllowed = rules.Continue != 0
	}
	if s.pendingStageAreaID != 0 {
		stageQuest, exists := s.stageQuests[s.pendingStageAreaID]
		if !exists {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("selected StageQuest area is unavailable")
		}
		published, err := projectCNStageQuestPublication(stageQuest)
		if err != nil {
			return teamBattleContext{}, battlePointStatus{}, false, err
		}
		stageID, bpUse, found, err := releasedStageQuestBattleForBoss(
			published,
			s.pendingStageAreaID,
			bossID,
		)
		if err != nil {
			return teamBattleContext{}, battlePointStatus{}, false, err
		}
		if !found {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("selected StageQuest boss is unavailable")
		}
		context.StageQuestAreaID = s.pendingStageAreaID
		context.StageQuestStageID = stageID
		context.BPUse = bpUse
	} else if areaID := bossID / 100; areaID >= cnNormalQuestAreaMin && areaID <= cnNormalQuestAreaMax {
		stageQuest, exists := s.stageQuests[areaID]
		if !exists {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("selected normal quest area is unavailable")
		}
		published, err := projectCNStageQuestPublication(stageQuest)
		if err != nil {
			return teamBattleContext{}, battlePointStatus{}, false, err
		}
		stageID, bpUse, found, err := releasedStageQuestBattleForBoss(
			published, areaID, bossID,
		)
		if err != nil {
			return teamBattleContext{}, battlePointStatus{}, false, err
		}
		if !found {
			return teamBattleContext{}, battlePointStatus{}, false,
				errors.New("selected normal quest boss is locked or unavailable")
		}
		context.StageQuestAreaID = areaID
		context.StageQuestStageID = stageID
		context.BPUse = bpUse
	} else if tower, floor, found := s.towerQuestBattleForBossLocked(bossID); found {
		context.TowerID = tower.TowerID
		context.TowerFloor = floor.Floor
		context.ItemID = tower.ItemID
		context.ItemUse = tower.ItemUse
		context.BPUse = 0
	}
	if !context.ConsumesBattlePoints {
		context.BPUse = 0
	}
	if context.PrepaidRoomID != 0 {
		bpUse, found := s.prepaidTeamBattleCostLocked(context.PrepaidRoomID, context.BossID)
		if !found || !context.ConsumesBattlePoints {
			return teamBattleContext{}, battlePointStatus{}, false, errors.New("multiplayer prepaid start receipt is unavailable")
		}
		context.BPUse = bpUse
	}
	if err := s.validateActiveTeamBattleContextLocked(context, profiles); err != nil {
		return teamBattleContext{}, battlePointStatus{}, false, err
	}
	profile, _ := teamBattleRewardProfileForContext(profiles, context)
	plan, err := planTeamBattleDrops(profile, context.BattleEnemyTypes, context.FameSeed)
	if err != nil {
		return teamBattleContext{}, battlePointStatus{}, false, err
	}
	context.DropPlanSet, context.DropPlan = true, plan
	for _, drop := range plan {
		if err := s.validateRewardLocked(drop.Reward); err != nil {
			return teamBattleContext{}, battlePointStatus{}, false, err
		}
	}
	if context.TowerID != 0 {
		item, exists := s.items[context.ItemID]
		if !exists || item.Num < context.ItemUse {
			return context, s.battlePointStatusLocked(now), false,
				errors.New("tower entry item is insufficient")
		}
		item.Num -= context.ItemUse
		s.items[item.ItemID] = item
	} else if context.ConsumesBattlePoints && context.PrepaidRoomID == 0 {
		if s.bp < context.BPUse {
			return context, s.battlePointStatusLocked(now), false, nil
		}
		s.bp -= context.BPUse
		if s.bpNextRecovery.IsZero() {
			s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
		}
	} else if !context.ConsumesBattlePoints {
		context.BPUse = 0
	}
	s.pendingStageAreaID = 0
	if strings.HasPrefix(fameSeed, "solo:") {
		// The original result request has no start ID. Receipts belong to the
		// most recent run; two immediate retreats produce identical reports.
		clear(s.teamBattleSoloReceipts)
	}
	active := context
	s.activeBattle = &active
	return context, s.battlePointStatusLocked(now), true, nil
}

func (s *store) abandonSoloBattle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle != nil && strings.HasPrefix(s.activeBattle.FameSeed, "solo:") {
		s.activeBattle = nil
	}
}

func (s *store) activeTeamBattleEnemyTypes(bossID int) ([]int8, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID || len(s.activeBattle.BattleEnemyTypes) == 0 {
		return nil, false
	}
	return append([]int8(nil), s.activeBattle.BattleEnemyTypes...), true
}

func (s *store) towerQuestBattleForBossLocked(
	bossID int,
) (release.TowerQuestProfile, release.TowerQuestFloorProfile, bool) {
	for _, profile := range s.towerQuestProfiles {
		if !profile.ClientEntryPublished {
			continue
		}
		for _, floor := range profile.Floors {
			var identity struct {
				BossID int `json:"0"`
			}
			if json.Unmarshal(floor.Boss, &identity) == nil && identity.BossID == bossID {
				return profile, floor, true
			}
		}
	}
	return release.TowerQuestProfile{}, release.TowerQuestFloorProfile{}, false
}

func (s *store) towerQuestHasBoss(bossID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, _, found := s.towerQuestBattleForBossLocked(bossID)
	return found
}

// continueTeamBattle applies the active local paid solo-continue policy.
// The managed client contract handles CONTINUE_MODE.FREE entirely on-device;
// both COIN and ROOKIE send pay_type 3. The current SoloStart profile advertises
// no rookie or unlimited-free continues. The fifty-crystal price is therefore an
// explicit local policy, consumed from the free balance before the paid balance.
func (s *store) continueTeamBattle(report teamBattleContinueReport, base release.State, persist StatePersister) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil {
		return s.coin, s.coinFree, errors.New("team battle continue has no active battle")
	}
	if report.PayType != 3 {
		return s.coin, s.coinFree, errors.New("unsupported team battle continue payment type")
	}
	if !strings.HasPrefix(s.activeBattle.FameSeed, "solo:") || s.activeBattle.PrepaidRoomID != 0 {
		return s.coin, s.coinFree, errors.New("team battle continue requires an active solo battle")
	}
	if !s.activeBattle.ContinueAllowed {
		return s.coin, s.coinFree, errors.New("this battle does not allow continues")
	}
	if err := validateNativeTeamBattleContinueReport(report.Progress, report.InputCommands, report.EnemyDeadBits, s.activeBattle.BattleEnemyTypes); err != nil {
		return s.coin, s.coinFree, err
	}
	// A retry carries the same accumulated native report. Commit its receipt
	// in this battle's durable context together with the currency debit.
	encoded, err := json.Marshal(report)
	if err != nil {
		return s.coin, s.coinFree, err
	}
	receipt := fmt.Sprintf("%x", sha256.Sum256(encoded))
	for _, previous := range s.activeBattle.ContinueReceipts {
		if previous == receipt {
			return s.coin, s.coinFree, nil
		}
	}
	if s.coin+s.coinFree < continueCrystalCost {
		return s.coin, s.coinFree, errors.New("insufficient crystals for team battle continue")
	}
	previousCoin, previousFree := s.coin, s.coinFree
	previousReceipts := s.activeBattle.ContinueReceipts
	freeSpend := min(s.coinFree, continueCrystalCost)
	s.coinFree -= freeSpend
	s.coin -= continueCrystalCost - freeSpend
	s.activeBattle.ContinueReceipts = append(append([]string(nil), previousReceipts...), receipt)
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.coin, s.coinFree = previousCoin, previousFree
			s.activeBattle.ContinueReceipts = previousReceipts
			return s.coin, s.coinFree, fmt.Errorf("persist team battle continue: %w", err)
		}
	}
	return s.coin, s.coinFree, nil
}

func (s *store) planTeamBattleFameAwardsLocked(
	profile release.TeamBattleRewardProfile,
	context teamBattleContext,
) ([]teamBattleFameAwardPlan, error) {
	policy := s.teamBattleFameBonus
	if policy.ConfigVersion == 0 {
		return nil, nil
	}
	if policy.ConfigVersion != 1 || policy.ChanceMaximum != 100 ||
		policy.FullFameThreshold != 100 || policy.FullFameRewardCount != 2 ||
		policy.BonusFameAdd < 0 || policy.RewardSource != "first_inventory_result_reward" ||
		policy.RollPolicy != "sha256_seed_modulo_chance_maximum_plus_one" ||
		strings.TrimSpace(context.FameSeed) == "" || len(context.FameSources) == 0 {
		return nil, errors.New("local team battle fame-bonus policy is invalid")
	}
	eligibleTypes := make(map[int]struct{}, len(policy.EligibleRewardTypes))
	for _, rewardType := range policy.EligibleRewardTypes {
		if rewardType <= 0 {
			return nil, errors.New("local team battle fame-bonus reward type is invalid")
		}
		eligibleTypes[rewardType] = struct{}{}
	}
	var bonusReward release.Reward
	foundReward := false
	for _, reward := range profile.ResultRewards {
		if _, eligible := eligibleTypes[reward.Type]; eligible {
			bonusReward = reward
			foundReward = true
			break
		}
	}
	if !foundReward {
		// The local onboarding/training profile has no inventory-bearing drop.
		// Keeping that one route fame-empty is safer than inventing a second pool.
		return nil, nil
	}
	sources := append([]teamBattleFameSource(nil), context.FameSources...)
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].ArthurType < sources[right].ArthurType
	})
	plans := make([]teamBattleFameAwardPlan, 0, len(sources)+1)
	for _, source := range sources {
		effectiveFame := min(policy.ChanceMaximum, source.LeaderFame+policy.BonusFameAdd)
		if effectiveFame <= 0 {
			continue
		}
		if teamBattleFameRoll(context.FameSeed, source.ArthurType, policy.ChanceMaximum) <= effectiveFame {
			plans = append(plans, teamBattleFameAwardPlan{
				ArthurType: source.ArthurType,
				RewardKind: 0,
				Reward:     bonusReward,
			})
		}
		if effectiveFame >= policy.FullFameThreshold {
			for rewardIndex := 1; rewardIndex < policy.FullFameRewardCount; rewardIndex++ {
				plans = append(plans, teamBattleFameAwardPlan{
					ArthurType: source.ArthurType,
					RewardKind: 1,
					Reward:     bonusReward,
				})
			}
		}
	}
	return plans, nil
}

func teamBattleFameRoll(seed string, arthurType int, chanceMaximum int) int {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|arthur=%d|fame-normal", seed, arthurType)))
	return int(binary.BigEndian.Uint64(sum[:8])%uint64(chanceMaximum)) + 1
}

func (s *store) planTeamBattleHostAwardsLocked(
	profile release.TeamBattleRewardProfile,
	context teamBattleContext,
) ([]teamBattleFameAwardPlan, error) {
	policy := s.teamBattleHostBonus
	if policy.ConfigVersion == 0 {
		return nil, nil
	}
	if policy.ConfigVersion != 1 || policy.RewardCount != 1 || policy.RewardKind != 5 ||
		policy.RewardSource != "first_inventory_result_reward" ||
		policy.RecipientPolicy != "multiplayer_room_owner_only" ||
		policy.BattlePointPayerPolicy != "multiplayer_room_owner_only" {
		return nil, errors.New("local team battle host-bonus policy is invalid")
	}
	if context.HostBonusArthurType == 0 {
		return nil, nil
	}
	if !context.ConsumesBattlePoints || context.HostBonusArthurType < 1 || context.HostBonusArthurType > 4 {
		return nil, errors.New("local team battle host-bonus context is invalid")
	}
	eligibleTypes := make(map[int]struct{}, len(policy.EligibleRewardTypes))
	for _, rewardType := range policy.EligibleRewardTypes {
		if rewardType <= 0 {
			return nil, errors.New("local team battle host-bonus reward type is invalid")
		}
		eligibleTypes[rewardType] = struct{}{}
	}
	for _, reward := range profile.ResultRewards {
		if _, eligible := eligibleTypes[reward.Type]; eligible {
			return []teamBattleFameAwardPlan{{
				ArthurType: context.HostBonusArthurType,
				RewardKind: policy.RewardKind,
				Reward:     reward,
			}}, nil
		}
	}
	// The local onboarding/training profile has no inventory-bearing drop.
	return nil, nil
}

func (s *store) completeTeamBattle(
	bossID int,
	isClear bool,
	profiles []release.TeamBattleRewardProfile,
	dropReports ...teamBattleDropReport,
) (teamBattleSettlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID {
		return teamBattleSettlement{}, errors.New("local team battle result has no matching start")
	}
	context := *s.activeBattle
	profile, found := teamBattleRewardProfileForContext(profiles, context)
	if !found {
		return teamBattleSettlement{}, errors.New("local team battle reward profile is unavailable")
	}
	settlement := teamBattleSettlement{Context: context}
	if !isClear {
		if context.TowerID != 0 {
			if err := s.settleTowerQuestLocked(context, false); err != nil {
				return teamBattleSettlement{}, err
			}
		}
		s.activeBattle = nil
		return settlement, nil
	}
	report := teamBattleDropReport{}
	if len(dropReports) > 0 {
		report = dropReports[0]
	}
	if context.DropPlanSet && !report.Authoritative && len(report.EnemyDeadBits) != len(context.BattleEnemyTypes) {
		return teamBattleSettlement{}, errors.New("team battle drop report does not match its started plan")
	}
	profile.ResultRewards = settledTeamBattleRewards(profile, context, report)
	scoreProgress, scoreRewards, scoreInfo := planTeamBattleScore(profile.ScorePolicy, s.teamBattleScores[bossID], report.Turns)
	settlement.ScoreInfo = scoreInfo

	updatedMainQuest := append(json.RawMessage(nil), s.mainQuest...)
	updatedStageQuestAreaID := 0
	completedStageQuestArea := false
	updatedTeamBattleSolo := append(json.RawMessage(nil), s.teamBattleSolo...)
	if context.StageQuestAreaID != 0 {
		stageQuest, exists := s.stageQuests[context.StageQuestAreaID]
		if !exists {
			return teamBattleSettlement{}, errors.New("local StageQuest area is unavailable")
		}
		var err error
		updatedMainQuest, settlement.WasFirstClear, err = markStageQuestClear(
			stageQuest,
			context.StageQuestAreaID,
			context.StageQuestStageID,
		)
		if err != nil {
			return teamBattleSettlement{}, err
		}
		updatedTeamBattleSolo, _, err = markTeamBattleClearForArea(
			s.teamBattleSolo,
			context.BossID,
			context.StageQuestAreaID,
		)
		if err != nil {
			return teamBattleSettlement{}, err
		}
		completedStageQuestArea, err = stageQuestAllStagesCleared(updatedMainQuest)
		if err != nil {
			return teamBattleSettlement{}, err
		}
		updatedStageQuestAreaID = context.StageQuestAreaID
	} else if context.TowerID != 0 {
		progress, exists := s.towerQuestProgress[context.TowerID]
		if !exists {
			return teamBattleSettlement{}, errors.New("local tower progress is unavailable")
		}
		settlement.WasFirstClear = !slices.Contains(progress.ClearedFloors, context.TowerFloor)
	} else {
		var err error
		updatedTeamBattleSolo, settlement.WasFirstClear, err = markStandaloneTeamBattleClear(
			s.teamBattleSolo,
			context.BossID,
		)
		if err != nil {
			return teamBattleSettlement{}, err
		}
	}
	rewards := append([]release.Reward(nil), profile.ResultRewards...)
	rewards = append(rewards, scoreRewards...)
	if settlement.WasFirstClear {
		rewards = append(rewards, profile.FirstClearRewards...)
	}
	famePlans, err := s.planTeamBattleFameAwardsLocked(profile, context)
	if err != nil {
		return teamBattleSettlement{}, err
	}
	hostPlans, err := s.planTeamBattleHostAwardsLocked(profile, context)
	if err != nil {
		return teamBattleSettlement{}, err
	}
	for _, plan := range famePlans {
		rewards = append(rewards, plan.Reward)
	}
	for _, plan := range hostPlans {
		rewards = append(rewards, plan.Reward)
	}
	helperReward := release.Reward{}
	if context.FriendPointReward > 0 {
		helperReward = release.Reward{
			Type: 9, Num: context.FriendPointReward,
			CardSkillLevels: []int16{},
		}
		rewards = append(rewards, helperReward)
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return teamBattleSettlement{}, err
	}
	for _, reward := range scoreRewards {
		if err := s.applySettlementRewardLocked(reward, &settlement.Score); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	for _, reward := range profile.ResultRewards {
		if err := s.applySettlementRewardLocked(reward, &settlement.Result); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	if helperReward.Num > 0 {
		if err := s.applySettlementRewardLocked(helperReward, &settlement.Result); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	if settlement.WasFirstClear {
		for _, reward := range profile.FirstClearRewards {
			if err := s.applySettlementRewardLocked(reward, &settlement.FirstClear); err != nil {
				return teamBattleSettlement{}, err
			}
		}
	}
	for _, plan := range famePlans {
		award := teamBattleFameAward{
			ArthurType: plan.ArthurType,
			RewardKind: plan.RewardKind,
		}
		if err := s.applySettlementRewardLocked(plan.Reward, &award.Result); err != nil {
			return teamBattleSettlement{}, err
		}
		settlement.Fame = append(settlement.Fame, award)
	}
	for _, plan := range hostPlans {
		award := teamBattleFameAward{
			ArthurType: plan.ArthurType,
			RewardKind: plan.RewardKind,
		}
		if err := s.applySettlementRewardLocked(plan.Reward, &award.Result); err != nil {
			return teamBattleSettlement{}, err
		}
		settlement.Host = append(settlement.Host, award)
	}
	if updatedStageQuestAreaID != 0 {
		s.stageQuests[updatedStageQuestAreaID] = append(json.RawMessage(nil), updatedMainQuest...)
		if updatedStageQuestAreaID == s.defaultStageQuestAreaID {
			s.mainQuest = append(json.RawMessage(nil), updatedMainQuest...)
		}
	}
	if profile.ScorePolicy != nil {
		s.teamBattleScores[bossID] = scoreProgress
	}
	s.teamBattleSolo = updatedTeamBattleSolo
	if context.TowerID != 0 {
		if err := s.settleTowerQuestLocked(context, true); err != nil {
			return teamBattleSettlement{}, err
		}
	}
	onboardingEventKind := "activity"
	if context.StageQuestAreaID != 0 {
		onboardingEventKind = "battle"
		if completedStageQuestArea {
			onboardingEventKind = "battle_area"
		}
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{
		kind: onboardingEventKind, areaID: context.StageQuestAreaID,
		stageID:      context.StageQuestStageID,
		activityBoss: context.StageQuestAreaID == 0 && context.TowerID == 0,
	}); err != nil {
		return teamBattleSettlement{}, err
	}
	s.activeBattle = nil
	return settlement, nil
}

func (s *store) settleTowerQuestLocked(context teamBattleContext, isClear bool) error {
	profile, exists := s.towerQuestProfiles[context.TowerID]
	if !exists || context.TowerFloor <= 0 || context.TowerFloor > len(profile.Floors) ||
		profile.Floors[context.TowerFloor-1].Floor != context.TowerFloor {
		return errors.New("local tower battle context is invalid")
	}
	progress, exists := s.towerQuestProgress[context.TowerID]
	if !exists {
		return errors.New("local tower progress is unavailable")
	}
	progress.LastBattleFloor = context.TowerFloor
	if isClear {
		previousLoseCount := progress.LoseCount
		previousRank := profile.Floors[context.TowerFloor-1].Rank
		progress.LastResult = "win"
		progress.LastResultLoseCount = previousLoseCount
		progress.LoseCount = 0
		if !slices.Contains(progress.ClearedFloors, context.TowerFloor) {
			progress.ClearedFloors = append(progress.ClearedFloors, context.TowerFloor)
			sort.Ints(progress.ClearedFloors)
		}
		if context.TowerFloor == len(profile.Floors) {
			progress.Floor = 0
			progress.LastRankUp = false
		} else {
			progress.Floor = context.TowerFloor + 1
			progress.LastRankUp = profile.Floors[progress.Floor-1].Rank > previousRank
		}
	} else {
		progress.LastResult = "lose"
		progress.LastRankUp = false
		progress.LoseCount++
		progress.LastResultLoseCount = progress.LoseCount
		if progress.LoseCount >= profile.LoseCountMax {
			rank := profile.Floors[context.TowerFloor-1].Rank
			progress.Floor = context.TowerFloor
			for _, floor := range profile.Floors {
				if floor.Rank == rank {
					progress.Floor = floor.Floor
					break
				}
			}
			progress.LoseCount = 0
		}
	}
	s.towerQuestProgress[context.TowerID] = progress
	return nil
}

func (s *store) teamBattleSoloState() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if refreshed, changed, err := expireTeamBattleUserBuffs(s.teamBattleSolo, time.Now().Unix()); err == nil && changed {
		s.teamBattleSolo = refreshed
	}
	tutorialNormalQuest := s.onboarding.ConfigVersion == cnOnboardingConfigVersion &&
		s.onboarding.Step == 0
	tutorialActivity := s.onboarding.ConfigVersion == cnOnboardingConfigVersion &&
		s.onboarding.Step == cnOnboardingStepCount-1
	projected, err := projectCNTeamBattlePublication(
		s.teamBattleSolo, s.stageQuests, s.teamBattleLimitedGroupIDs,
		tutorialNormalQuest, tutorialActivity,
	)
	if err != nil {
		return append(json.RawMessage(nil), s.teamBattleSolo...)
	}
	return projected
}

func expireTeamBattleUserBuffs(
	configuration json.RawMessage,
	nowUnix int64,
) (json.RawMessage, bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, false, err
	}
	changed := false
	for _, groupKey := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(top[groupKey], &groups); err != nil {
			return nil, false, err
		}
		groupChanged := false
		for groupIndex := range groups {
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(groups[groupIndex]["10"], &bosses); err != nil {
				return nil, false, err
			}
			bossesChanged := false
			for bossIndex := range bosses {
				var expiration int64
				if err := json.Unmarshal(bosses[bossIndex]["16"], &expiration); err != nil {
					return nil, false, err
				}
				if expiration <= 0 || expiration > nowUnix {
					continue
				}
				bosses[bossIndex]["10"] = json.RawMessage("0")
				bosses[bossIndex]["16"] = json.RawMessage("0")
				bossesChanged = true
			}
			if bossesChanged {
				encoded, err := json.Marshal(bosses)
				if err != nil {
					return nil, false, err
				}
				groups[groupIndex]["10"] = encoded
				groupChanged = true
			}
		}
		if groupChanged {
			encoded, err := json.Marshal(groups)
			if err != nil {
				return nil, false, err
			}
			top[groupKey] = encoded
			changed = true
		}
	}
	if !changed {
		return append(json.RawMessage(nil), configuration...), false, nil
	}
	encoded, err := json.Marshal(top)
	return encoded, true, err
}

type userBuffExecResult struct {
	Bosses []json.RawMessage
	Item   release.Item
}

func (s *store) execUserBuff(userBuffID int, now time.Time) (userBuffExecResult, error) {
	if userBuffID <= 0 {
		return userBuffExecResult{}, errors.New("invalid user-buff ID")
	}
	nowUnix := now.Unix()
	if nowUnix <= 0 {
		return userBuffExecResult{}, errors.New("invalid user-buff execution time")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, exists := s.userBuffProfiles[userBuffID]
	if !exists {
		return userBuffExecResult{}, errors.New("unknown user-buff ID")
	}
	expiresUnix := nowUnix + int64(profile.DurationSeconds)
	if expiresUnix <= nowUnix || expiresUnix > int64(^uint32(0)>>1) {
		return userBuffExecResult{}, errors.New("invalid user-buff expiration")
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(s.teamBattleSolo, &top); err != nil {
		return userBuffExecResult{}, fmt.Errorf("decode user-buff TeamBattle state: %w", err)
	}
	changedBosses := make([]json.RawMessage, 0)
	seenBossIDs := make(map[int]struct{})
	referenced := false
	alreadyActive := false
	for _, groupKey := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if raw, present := top[groupKey]; !present || json.Unmarshal(raw, &groups) != nil {
			return userBuffExecResult{}, fmt.Errorf("decode user-buff TeamBattle group %s", groupKey)
		}
		groupChanged := false
		for groupIndex := range groups {
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(groups[groupIndex]["10"], &bosses); err != nil {
				return userBuffExecResult{}, fmt.Errorf("decode user-buff boss list: %w", err)
			}
			bossListChanged := false
			for bossIndex := range bosses {
				var userBuffIDs []int
				if err := json.Unmarshal(bosses[bossIndex]["15"], &userBuffIDs); err != nil {
					return userBuffExecResult{}, fmt.Errorf("decode boss user-buff IDs: %w", err)
				}
				usesProfile := false
				for _, candidate := range userBuffIDs {
					if candidate == userBuffID {
						usesProfile = true
						break
					}
				}
				if !usesProfile {
					continue
				}
				referenced = true
				var state int
				var currentExpiration int64
				if err := json.Unmarshal(bosses[bossIndex]["10"], &state); err != nil ||
					json.Unmarshal(bosses[bossIndex]["16"], &currentExpiration) != nil {
					return userBuffExecResult{}, errors.New("decode user-buff boss state")
				}
				if state != 0 && (currentExpiration == 0 || currentExpiration > nowUnix) {
					alreadyActive = true
					continue
				}
				bosses[bossIndex]["10"] = json.RawMessage("1")
				bosses[bossIndex]["16"] = json.RawMessage(strconv.FormatInt(expiresUnix, 10))
				bossListChanged = true
				var bossID int
				if err := json.Unmarshal(bosses[bossIndex]["0"], &bossID); err != nil || bossID <= 0 {
					return userBuffExecResult{}, errors.New("decode user-buff boss ID")
				}
				if _, duplicate := seenBossIDs[bossID]; !duplicate {
					encoded, err := json.Marshal(bosses[bossIndex])
					if err != nil {
						return userBuffExecResult{}, fmt.Errorf("encode unlocked boss: %w", err)
					}
					changedBosses = append(changedBosses, encoded)
					seenBossIDs[bossID] = struct{}{}
				}
			}
			if bossListChanged {
				encoded, err := json.Marshal(bosses)
				if err != nil {
					return userBuffExecResult{}, fmt.Errorf("encode user-buff boss list: %w", err)
				}
				groups[groupIndex]["10"] = encoded
				groupChanged = true
			}
		}
		if groupChanged {
			encoded, err := json.Marshal(groups)
			if err != nil {
				return userBuffExecResult{}, fmt.Errorf("encode user-buff TeamBattle group: %w", err)
			}
			top[groupKey] = encoded
		}
	}
	if !referenced {
		return userBuffExecResult{}, errors.New("user-buff ID is not referenced by a published boss")
	}
	if alreadyActive {
		return userBuffExecResult{}, errors.New("user-buff boss is already unlocked")
	}
	if len(changedBosses) == 0 {
		return userBuffExecResult{}, errors.New("user-buff unlock has no target boss")
	}
	item, exists := s.items[profile.ItemID]
	if !exists || item.Num < profile.RequiredNum ||
		item.LimitTime > 0 && nowUnix >= int64(item.LimitTime) {
		return userBuffExecResult{}, errInsufficientMaterials
	}
	encoded, err := json.Marshal(top)
	if err != nil {
		return userBuffExecResult{}, fmt.Errorf("encode user-buff TeamBattle state: %w", err)
	}
	item.Num -= profile.RequiredNum
	s.items[item.ItemID] = item
	s.teamBattleSolo = encoded
	sort.Slice(changedBosses, func(left, right int) bool {
		var leftID, rightID struct {
			BossID int `json:"0"`
		}
		_ = json.Unmarshal(changedBosses[left], &leftID)
		_ = json.Unmarshal(changedBosses[right], &rightID)
		return leftID.BossID < rightID.BossID
	})
	return userBuffExecResult{Bosses: changedBosses, Item: item}, nil
}

func (s *store) teamBattleSchedulePushState(isSolo int) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.teamBattleScheduleMultiPush
	if isSolo == 1 {
		values = s.teamBattleScheduleSoloPush
	}
	result := make([]int, 0, len(values))
	for groupID := range values {
		result = append(result, groupID)
	}
	sort.Ints(result)
	return result
}

func (s *store) toggleTeamBattleSchedulePush(isSolo int, groupID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.teamBattleScheduleMultiPush
	if isSolo == 1 {
		values = s.teamBattleScheduleSoloPush
	}
	if _, enabled := values[groupID]; enabled {
		delete(values, groupID)
		return false
	}
	values[groupID] = struct{}{}
	return true
}

func (s *store) teamBattleResultReceipt(roomID int64) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.teamBattleReceipts[roomID]
	if !exists {
		return nil, false
	}
	return append(json.RawMessage(nil), receipt.Response...), true
}

func (s *store) recordTeamBattleResultReceipt(roomID int64, response json.RawMessage, claimedAt time.Time) error {
	if roomID <= 0 || claimedAt.IsZero() || len(response) == 0 || len(response) > maxRequestBytes || !json.Valid(response) {
		return errors.New("team battle result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.teamBattleReceipts[roomID]; exists {
		if string(existing.Response) != string(response) {
			return errors.New("team battle result receipt conflicts with the completed room")
		}
		return nil
	}
	if len(s.teamBattleReceipts) >= maxBattleReceipts {
		var oldestRoomID int64
		var oldestClaimedAt int64
		for existingRoomID, receipt := range s.teamBattleReceipts {
			if oldestRoomID == 0 || receipt.ClaimedAtUnix < oldestClaimedAt ||
				(receipt.ClaimedAtUnix == oldestClaimedAt && existingRoomID < oldestRoomID) {
				oldestRoomID = existingRoomID
				oldestClaimedAt = receipt.ClaimedAtUnix
			}
		}
		delete(s.teamBattleReceipts, oldestRoomID)
	}
	s.teamBattleReceipts[roomID] = release.TeamBattleResultReceipt{
		RoomID:        roomID,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}

func (s *store) teamBattleSoloResultReceipt(requestSHA256 string, bossID int) (json.RawMessage, bool) {
	if !isLowerSHA256Digest(requestSHA256) || bossID <= 0 {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.teamBattleSoloReceipts[requestSHA256]
	if !exists || receipt.BossID != bossID {
		return nil, false
	}
	return append(json.RawMessage(nil), receipt.Response...), true
}

func (s *store) recordTeamBattleSoloResultReceipt(
	requestSHA256 string,
	bossID int,
	response json.RawMessage,
	claimedAt time.Time,
) error {
	if !isLowerSHA256Digest(requestSHA256) || bossID <= 0 || claimedAt.IsZero() ||
		len(response) == 0 || len(response) > maxRequestBytes || !json.Valid(response) {
		return errors.New("solo team battle result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.teamBattleSoloReceipts[requestSHA256]; exists {
		if existing.BossID != bossID || string(existing.Response) != string(response) {
			return errors.New("solo team battle result receipt conflicts with the completed report")
		}
		return nil
	}
	if len(s.teamBattleSoloReceipts) >= maxBattleReceipts {
		var oldestDigest string
		var oldestClaimedAt int64
		for digest, receipt := range s.teamBattleSoloReceipts {
			if oldestDigest == "" || receipt.ClaimedAtUnix < oldestClaimedAt ||
				(receipt.ClaimedAtUnix == oldestClaimedAt && digest < oldestDigest) {
				oldestDigest = digest
				oldestClaimedAt = receipt.ClaimedAtUnix
			}
		}
		delete(s.teamBattleSoloReceipts, oldestDigest)
	}
	s.teamBattleSoloReceipts[requestSHA256] = release.TeamBattleSoloResultReceipt{
		RequestSHA256: requestSHA256,
		BossID:        bossID,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}

func (s *store) exploreIsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exploreActive
}

func (s *store) exploreResultReceipt() (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastExploreResultReceipt == nil {
		return nil, false
	}
	return append(json.RawMessage(nil), s.lastExploreResultReceipt.Response...), true
}

func (s *store) recordExploreResultReceipt(
	startedAtUnix int64,
	response json.RawMessage,
	claimedAt time.Time,
) error {
	if startedAtUnix <= 0 || claimedAt.IsZero() || claimedAt.Unix() <= 0 ||
		len(response) == 0 || len(response) > maxRequestBytes || !json.Valid(response) {
		return errors.New("Explore result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastExploreResultReceipt = &release.ExploreResultReceipt{
		StartedAtUnix: startedAtUnix,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}

func (s *store) pvpResultReceipt(battleID int) (release.PVPResultReceipt, bool) {
	if battleID <= 0 {
		return release.PVPResultReceipt{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.pvpResultReceipts[battleID]
	if !exists {
		return release.PVPResultReceipt{}, false
	}
	receipt.Response = append(json.RawMessage(nil), receipt.Response...)
	return receipt, true
}

func (s *store) recordPVPResultReceipt(
	battleID int,
	requestSHA256 string,
	response json.RawMessage,
	claimedAt time.Time,
) error {
	if battleID <= 0 || !isLowerSHA256Digest(requestSHA256) || claimedAt.IsZero() ||
		len(response) == 0 || len(response) > maxRequestBytes || !json.Valid(response) {
		return errors.New("PVP result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.pvpResultReceipts[battleID]; exists {
		if existing.RequestSHA256 != requestSHA256 || string(existing.Response) != string(response) {
			return errors.New("PVP result receipt conflicts with the completed battle")
		}
		return nil
	}
	if len(s.pvpResultReceipts) >= maxBattleReceipts {
		oldestBattleID := 0
		oldestClaimedAt := int64(0)
		for existingBattleID, receipt := range s.pvpResultReceipts {
			if oldestBattleID == 0 || receipt.ClaimedAtUnix < oldestClaimedAt ||
				(receipt.ClaimedAtUnix == oldestClaimedAt && existingBattleID < oldestBattleID) {
				oldestBattleID = existingBattleID
				oldestClaimedAt = receipt.ClaimedAtUnix
			}
		}
		delete(s.pvpResultReceipts, oldestBattleID)
	}
	s.pvpResultReceipts[battleID] = release.PVPResultReceipt{
		BattleID:      battleID,
		RequestSHA256: requestSHA256,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}

func isLowerSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (s *store) optionState() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gameOptionFlag, s.pushOptionFlag
}

func (s *store) setGameOption(flag int) bool {
	if flag < 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gameOptionFlag = flag
	return true
}

func (s *store) setPushOption(flag int) bool {
	if flag < 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushOptionFlag = flag
	return true
}

type apStatus struct {
	Current     int
	Max         int
	NextSeconds int
	HealSeconds int
}

func (s *store) refreshAPLocked(now time.Time) {
	if s.ap >= s.apMax {
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
		return
	}
	if s.apNextRecovery.IsZero() {
		s.apNextRecovery = now.Add(s.apRecoveryInterval)
		return
	}
	if now.Before(s.apNextRecovery) {
		return
	}
	elapsed := now.Sub(s.apNextRecovery)
	recovered := 1 + int(elapsed/s.apRecoveryInterval)
	s.ap += recovered
	if s.ap >= s.apMax {
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
		return
	}
	s.apNextRecovery = s.apNextRecovery.Add(
		time.Duration(recovered) * s.apRecoveryInterval,
	)
}

func (s *store) apStatusLocked(now time.Time) apStatus {
	s.refreshAPLocked(now)
	status := apStatus{Current: s.ap, Max: s.apMax}
	if s.ap >= s.apMax {
		return status
	}
	remaining := s.apNextRecovery.Sub(now)
	status.NextSeconds = int((remaining + time.Second - 1) / time.Second)
	missingAfterNext := s.apMax - s.ap - 1
	status.HealSeconds = status.NextSeconds +
		missingAfterNext*int(s.apRecoveryInterval/time.Second)
	return status
}

func (s *store) apState() apStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apStatusLocked(time.Now())
}

func (s *store) beginExplore(arthurType, deckIndex int8) (apStatus, int, release.Avatar, release.ExploreStage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.refreshAPLocked(now)
	leaderCardID, avatar, _, selectionOK := s.exploreSelectionLocked(arthurType, deckIndex)
	if s.exploreActive || s.ap <= 0 {
		return s.apStatusLocked(now), 0, release.Avatar{}, release.ExploreStage{}, false
	}
	if !selectionOK || len(s.exploreStages) == 0 || s.exploreStageCursor < 0 || s.exploreStageCursor >= len(s.exploreStages) {
		return s.apStatusLocked(now), 0, release.Avatar{}, release.ExploreStage{}, false
	}
	stage := cloneExploreStages(s.exploreStages[s.exploreStageCursor : s.exploreStageCursor+1])[0]
	s.exploreStageCursor = (s.exploreStageCursor + 1) % len(s.exploreStages)
	s.exploreActiveStage = stage.ExploreStageID
	s.ap--
	if s.apNextRecovery.IsZero() {
		s.apNextRecovery = now.Add(s.apRecoveryInterval)
	}
	s.exploreActive = true
	s.exploreArthurType = arthurType
	s.exploreDeckIndex = deckIndex
	s.exploreStartedAt = now
	return s.apStatusLocked(now), leaderCardID, avatar, stage, true
}

func (s *store) hasExploreStageLocked(stageID int) bool {
	for _, stage := range s.exploreStages {
		if stage.ExploreStageID == stageID {
			return true
		}
	}
	return false
}

func (s *store) exploreSelectionLocked(arthurType, deckIndex int8) (int, release.Avatar, []int64, bool) {
	if arthurType < 1 || arthurType > 4 || int(arthurType) > len(s.avatars) {
		return 0, release.Avatar{}, nil, false
	}
	for _, deck := range s.decks {
		if deck.ArthurType != arthurType || deck.Index != deckIndex ||
			deck.LeaderCardIndex < 0 || int(deck.LeaderCardIndex) >= len(deck.CardUniqueIDs) {
			continue
		}
		leaderUniqueID := deck.CardUniqueIDs[deck.LeaderCardIndex]
		cardIndex := cardIndexByUniqueID(s.cards, leaderUniqueID)
		if cardIndex < 0 {
			return 0, release.Avatar{}, nil, false
		}
		avatar := s.avatars[int(arthurType)-1]
		avatar.AvatarPartIDs = append([]int(nil), avatar.AvatarPartIDs...)
		return s.cards[cardIndex].CardID, avatar, append([]int64(nil), deck.CardUniqueIDs...), true
	}
	return 0, release.Avatar{}, nil, false
}

func (s *store) endExplore(rewards []release.Reward) (presentReceiveResult, []cardInfo, bool, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	completed := s.exploreActive
	if !completed {
		return presentReceiveResult{}, nil, false, 0, nil
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return presentReceiveResult{}, nil, false, 0, err
	}
	startedAtUnix := s.exploreStartedAt.Unix()
	result := presentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applySettlementRewardLocked(reward, &result); err != nil {
			return presentReceiveResult{}, nil, false, 0, err
		}
	}
	s.exploreActive = false
	arthurType := s.exploreArthurType
	deckIndex := s.exploreDeckIndex
	s.exploreArthurType = 0
	s.exploreDeckIndex = 0
	s.exploreStartedAt = time.Time{}
	s.exploreActiveStage = 0
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "explore"}); err != nil {
		return presentReceiveResult{}, nil, false, 0, err
	}
	if arthurType == 0 {
		arthurType = 1
	}
	var cardIDs []int64
	for _, deck := range s.decks {
		if deck.ArthurType == arthurType && deck.Index == deckIndex {
			cardIDs = deck.CardUniqueIDs
			break
		}
	}
	if cardIDs == nil {
		cardIDs = s.decks[0].CardUniqueIDs
	}
	cards := make([]cardInfo, 0, len(cardIDs))
	for _, uniqueID := range cardIDs {
		index := cardIndexByUniqueID(s.cards, uniqueID)
		if index >= 0 {
			cards = append(cards, cloneCard(s.cards[index]))
		}
	}
	return result, cards, true, startedAtUnix, nil
}

func (s *store) moveCards(toSlot int8, uniqueIDs []int64) ([]cardInfo, []deckInfo, error) {
	if toSlot != 0 && toSlot != 1 {
		return nil, nil, errors.New("card destination slot must be inventory or container")
	}
	if len(uniqueIDs) == 0 {
		return nil, nil, errors.New("card move selection is empty")
	}
	requested := make(map[int64]struct{}, len(uniqueIDs))
	for _, uniqueID := range uniqueIDs {
		if uniqueID <= 0 {
			return nil, nil, errors.New("invalid card unique ID")
		}
		if _, duplicate := requested[uniqueID]; duplicate {
			return nil, nil, errors.New("duplicate card move selection")
		}
		requested[uniqueID] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	source := &s.containerCards
	destination := &s.cards
	destinationMax := s.cardMax
	if toSlot == 1 {
		source = &s.cards
		destination = &s.containerCards
		destinationMax = s.cardContainerMax
	}
	if len(*destination)+len(uniqueIDs) > destinationMax {
		if toSlot == 1 {
			return nil, nil, &businessError{-2901, "仓库卡牌容量不足，请整理后再试。"}
		}
		return nil, nil, errCardCapacity
	}

	selected := make(map[int]struct{}, len(uniqueIDs))
	moved := make([]cardInfo, len(uniqueIDs))
	for moveIndex, uniqueID := range uniqueIDs {
		index := cardIndexByUniqueID(*source, uniqueID)
		if index < 0 {
			return nil, nil, errors.New("card move source does not contain unique ID")
		}
		selected[index] = struct{}{}
		moved[moveIndex] = cloneCard((*source)[index])
		moved[moveIndex].Slot = int(toSlot)
	}
	kept := make([]cardInfo, 0, len(*source)-len(selected))
	for index, card := range *source {
		if _, remove := selected[index]; !remove {
			kept = append(kept, card)
		}
	}
	proposedDestination := append(cloneCards(*destination), moved...)
	updatedDecks := make([]deckInfo, 0)
	proposedDecks := s.rankedDecksLocked(s.decks)
	if toSlot == 1 {
		for deckIndex := range proposedDecks {
			changed := false
			vacatedMainSlots := make([]int, 0)
			vacatedSupportSlots := make([]int, 0)
			for cardIndex, uniqueID := range proposedDecks[deckIndex].CardUniqueIDs {
				if _, remove := requested[uniqueID]; remove {
					proposedDecks[deckIndex].CardUniqueIDs[cardIndex] = 0
					vacatedMainSlots = append(vacatedMainSlots, cardIndex)
					changed = true
				}
			}
			for cardIndex, uniqueID := range proposedDecks[deckIndex].SupportCardUniqueIDs {
				if _, remove := requested[uniqueID]; remove {
					proposedDecks[deckIndex].SupportCardUniqueIDs[cardIndex] = 0
					vacatedSupportSlots = append(vacatedSupportSlots, cardIndex)
					changed = true
				}
			}
			if !changed {
				continue
			}
			if err := s.fillVacatedDeckSlots(
				&proposedDecks[deckIndex], kept, vacatedMainSlots, false,
			); err != nil {
				return nil, nil, err
			}
			if err := s.fillVacatedDeckSlots(
				&proposedDecks[deckIndex], kept, vacatedSupportSlots, true,
			); err != nil {
				return nil, nil, err
			}
			updatedDecks = append(updatedDecks, cloneDeck(proposedDecks[deckIndex]))
		}
	}
	*source = kept
	*destination = proposedDestination
	s.decks = proposedDecks
	return cloneCards(moved), s.rankedDecksLocked(updatedDecks), nil
}

func (s *store) fillVacatedDeckSlots(
	deck *deckInfo,
	candidates []cardInfo,
	vacatedSlots []int,
	support bool,
) error {
	for _, slot := range vacatedSlots {
		filled := false
		for _, candidate := range candidates {
			if deckContainsCardUniqueID(*deck, candidate.UniqueID) {
				continue
			}
			if support {
				deck.SupportCardUniqueIDs[slot] = candidate.UniqueID
			} else {
				deck.CardUniqueIDs[slot] = candidate.UniqueID
			}
			if err := s.validateDeckCardFamilies(*deck, 0, 0); err == nil {
				filled = true
				break
			}
			if support {
				deck.SupportCardUniqueIDs[slot] = 0
			} else {
				deck.CardUniqueIDs[slot] = 0
			}
		}
		if !filled {
			kind := "main"
			if support {
				kind = "support"
			}
			return fmt.Errorf(
				"cannot auto-fill %s deck slot %d for Arthur %d deck %d",
				kind, slot+1, deck.ArthurType, deck.Index,
			)
		}
	}
	return nil
}

// repairIncompleteMainDecks migrates state written before CardMove replaced
// cards removed from an equipped deck. The CN client treats a main deck with
// any zero slot as invalid; at low rank CardMgr.isUseCard then dereferences a
// missing selected deck while rendering every inventory item.
func (s *store) repairIncompleteMainDecks() (bool, error) {
	proposed := s.rankedDecksLocked(s.decks)
	mainInventory := make(map[int64]struct{}, len(s.cards))
	for _, card := range s.cards {
		mainInventory[card.UniqueID] = struct{}{}
	}
	containerInventory := make(map[int64]struct{}, len(s.containerCards))
	for _, card := range s.containerCards {
		containerInventory[card.UniqueID] = struct{}{}
	}

	changed := false
	for deckIndex := range proposed {
		vacated := make([]int, 0)
		for slot, uniqueID := range proposed[deckIndex].CardUniqueIDs {
			if uniqueID == 0 {
				vacated = append(vacated, slot)
				continue
			}
			if _, exists := mainInventory[uniqueID]; exists {
				continue
			}
			if _, stored := containerInventory[uniqueID]; !stored {
				return false, fmt.Errorf(
					"Arthur %d deck %d references unknown card unique ID %d",
					proposed[deckIndex].ArthurType,
					proposed[deckIndex].Index,
					uniqueID,
				)
			}
			proposed[deckIndex].CardUniqueIDs[slot] = 0
			vacated = append(vacated, slot)
		}
		if len(vacated) == 0 {
			continue
		}
		if err := s.fillVacatedDeckSlots(
			&proposed[deckIndex], s.cards, vacated, false,
		); err != nil {
			return false, err
		}
		changed = true
	}
	if changed {
		s.decks = proposed
	}
	return changed, nil
}

func (s *store) setCardLock(uniqueID int64, slot int, locked bool) error {
	if uniqueID <= 0 {
		return errors.New("invalid card unique ID")
	}
	if slot != 0 && slot != 1 {
		return errors.New("invalid card slot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inventory := &s.cards
	if slot == 1 {
		inventory = &s.containerCards
	}
	index := cardIndexByUniqueID(*inventory, uniqueID)
	if index < 0 {
		return errors.New("unknown card unique ID in requested slot")
	}
	if !locked && s.cardFameTraining != nil && s.cardFameTraining.UniqueID == uniqueID {
		return errors.New("card is locked by active fame training")
	}
	if locked {
		(*inventory)[index].IsLock = 1
	} else {
		(*inventory)[index].IsLock = 0
	}
	return nil
}

type loveUpItemUse struct {
	ItemID int `json:"itemid"`
	Num    int `json:"num"`
}

type cardLoveUpResult struct {
	Card    cardInfo
	Items   []release.Item
	Gold    int
	OldLove int
	NewLove int
	IsMax   int8
}

func (s *store) loveUpCard(baseUniqueID int64, uses []loveUpItemUse) (cardLoveUpResult, error) {
	if baseUniqueID <= 0 || len(uses) == 0 || len(uses) > maxLoveUpItemKinds {
		return cardLoveUpResult{}, errors.New("invalid card love-up selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cardIndex := cardIndexByUniqueID(s.cards, baseUniqueID)
	if cardIndex < 0 {
		return cardLoveUpResult{}, errors.New("love-up card is not in the inventory")
	}
	card := s.cards[cardIndex]
	rule, exists := s.cardLoveRules[card.CardID]
	if !exists || rule.Max <= 0 || card.Love < 0 || card.Love >= rule.Max {
		return cardLoveUpResult{}, errors.New("card cannot increase love")
	}

	seen := make(map[int]struct{}, len(uses))
	updatedItems := make([]release.Item, len(uses))
	var addLove int64
	var totalPrice int64
	for index, use := range uses {
		if use.ItemID <= 0 || use.Num <= 0 {
			return cardLoveUpResult{}, errors.New("invalid love-up item selection")
		}
		if _, duplicate := seen[use.ItemID]; duplicate {
			return cardLoveUpResult{}, errors.New("duplicate love-up item selection")
		}
		seen[use.ItemID] = struct{}{}
		definition, known := s.itemDefinitions[use.ItemID]
		item, owned := s.items[use.ItemID]
		if !owned || use.Num > item.Num {
			return cardLoveUpResult{}, errInsufficientMaterials
		}
		if !known || definition.ItemType != "LOVE_UP" ||
			(definition.Function != "LOVEUP_NORMAL" && definition.Function != "LOVEUP_ALL") ||
			definition.FunctionValue <= 0 || definition.LoveUpPrice <= 0 {
			return cardLoveUpResult{}, errors.New("love-up item is unavailable")
		}
		if rule.Premium && definition.Function != "LOVEUP_ALL" {
			return cardLoveUpResult{}, errors.New("love-up item does not support premium rarity")
		}
		addLove += int64(definition.FunctionValue) * int64(use.Num)
		totalPrice += int64(definition.LoveUpPrice) * int64(use.Num)
		item.Num -= use.Num
		updatedItems[index] = item
	}
	if addLove <= 0 || totalPrice <= 0 || totalPrice > int64(s.gold) {
		return cardLoveUpResult{}, errInsufficientGold
	}

	oldLove := card.Love
	newLove := int64(oldLove) + addLove
	if newLove > int64(rule.Max) {
		newLove = int64(rule.Max)
	}
	card.Love = int(newLove)
	card, err := s.normalizeCardLocked(card)
	if err != nil {
		return cardLoveUpResult{}, err
	}
	for _, item := range updatedItems {
		s.items[item.ItemID] = item
	}
	s.gold -= int(totalPrice)
	s.cards[cardIndex] = card
	isMax := int8(0)
	s.recordCollectedCardLocked(card)
	if card.Love == rule.Max {
		isMax = 1
	}
	return cardLoveUpResult{
		Card: card, Items: updatedItems, Gold: s.gold,
		OldLove: oldLove, NewLove: card.Love, IsMax: isMax,
	}, nil
}

func (s *store) sellContainerCards(uniqueIDs []int64) (int, int, error) {
	if len(uniqueIDs) == 0 {
		return 0, 0, errors.New("container sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := make(map[int]struct{}, len(uniqueIDs))
	getGold := 0
	for _, uniqueID := range uniqueIDs {
		index := cardIndexByUniqueID(s.containerCards, uniqueID)
		if index < 0 {
			return 0, 0, errCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.containerCards[index]); err != nil {
			return 0, 0, err
		}
		definition, exists := s.cardDefinitions[s.containerCards[index].CardID]
		if !exists || definition.SellGold < 0 {
			return 0, 0, errors.New("container sell price is unavailable")
		}
		remove[index] = struct{}{}
		var err error
		getGold, err = checkedCardSaleGold(s.gold, getGold, definition.SellGold, 1)
		if err != nil {
			return 0, 0, err
		}
	}
	if len(remove) != len(uniqueIDs) {
		return 0, 0, errors.New("duplicate container sell card")
	}
	kept := make([]cardInfo, 0, len(s.containerCards)-len(remove))
	for index, card := range s.containerCards {
		if _, exists := remove[index]; !exists {
			kept = append(kept, card)
		}
	}
	s.containerCards = kept
	s.gold += getGold
	return getGold, s.gold, nil
}

func (s *store) fuseCard(
	baseUniqueID int64,
	materialUniqueIDs []int64,
	containerMaterialUniqueIDs []int64,
	stackUses []release.CardStackUse,
) (cardInfo, cardInfo, int, []deckInfo, error) {
	if baseUniqueID <= 0 ||
		(len(materialUniqueIDs) == 0 && len(containerMaterialUniqueIDs) == 0 && len(stackUses) == 0) {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("fusion selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := cardIndexByUniqueID(s.cards, baseUniqueID)
	if baseIndex < 0 {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("unknown fusion base card")
	}
	oldCard := s.cards[baseIndex]
	if s.cardFameTraining != nil && s.cardFameTraining.UniqueID == baseUniqueID {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("fusion base is in active fame training")
	}
	baseDefinition, exists := s.cardDefinitions[oldCard.CardID]
	if !exists {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("fusion base card definition is unavailable")
	}
	materialIndexes := make(map[int]struct{}, len(materialUniqueIDs))
	for _, uniqueID := range materialUniqueIDs {
		index := cardIndexByUniqueID(s.cards, uniqueID)
		if index < 0 {
			return cardInfo{}, cardInfo{}, 0, nil, errCardUnavailable
		}
		if index == baseIndex {
			return cardInfo{}, cardInfo{}, 0, nil, errors.New("invalid fusion material card")
		}
		if err := s.checkCardConsumptionLocked(s.cards[index]); err != nil {
			return cardInfo{}, cardInfo{}, 0, nil, err
		}
		materialIndexes[index] = struct{}{}
	}
	if len(materialIndexes) != len(materialUniqueIDs) {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("duplicate fusion material card")
	}
	containerMaterialIndexes := make(map[int]struct{}, len(containerMaterialUniqueIDs))
	for _, uniqueID := range containerMaterialUniqueIDs {
		index := cardIndexByUniqueID(s.containerCards, uniqueID)
		if index < 0 {
			return cardInfo{}, cardInfo{}, 0, nil, errCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.containerCards[index]); err != nil {
			return cardInfo{}, cardInfo{}, 0, nil, err
		}
		containerMaterialIndexes[index] = struct{}{}
	}
	if len(containerMaterialIndexes) != len(containerMaterialUniqueIDs) {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("duplicate container fusion material card")
	}
	if err := validateStackUses(s.stackCards, stackUses); err != nil {
		return cardInfo{}, cardInfo{}, 0, nil, err
	}
	materialCount := len(materialUniqueIDs) + len(containerMaterialUniqueIDs)
	for _, use := range stackUses {
		if use.Num > s.cardProgression.MaximumCardMaterialCount-materialCount {
			return cardInfo{}, cardInfo{}, 0, nil, errors.New("too many card fusion materials")
		}
		materialCount += use.Num
	}
	if materialCount <= 0 || materialCount > s.cardProgression.MaximumCardMaterialCount {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("too many card fusion materials")
	}
	cost64 := int64(oldCard.BaseAddPrice) * int64(materialCount)
	if cost64 <= 0 {
		return cardInfo{}, cardInfo{}, 0, nil, errors.New("invalid card fusion cost")
	}
	if cost64 > int64(s.gold) {
		return cardInfo{}, cardInfo{}, 0, nil, errInsufficientGold
	}
	cost := int(cost64)
	expUp := 0
	fameUp := 0
	addMaterial := func(material cardInfo) error {
		if material.AddExperience <= 0 || expUp > math.MaxInt-material.AddExperience {
			return errors.New("card fusion experience overflows")
		}
		expUp += material.AddExperience
		definition, available := s.cardDefinitions[material.CardID]
		if !available {
			return errors.New("card fusion material definition is unavailable")
		}
		// CalcAddFame uses family identity; level/rarity restrictions belong to
		// the client's convenient same-card selector, not manual fusion results.
		if definition.SameCardID > 0 && definition.SameCardID == baseDefinition.SameCardID {
			if material.Fame <= 0 || fameUp > math.MaxInt-material.Fame {
				return errors.New("card fusion material fame is invalid")
			}
			fameUp += material.Fame
		}
		return nil
	}
	for index := range materialIndexes {
		if err := addMaterial(s.cards[index]); err != nil {
			return cardInfo{}, cardInfo{}, 0, nil, err
		}
	}
	for index := range containerMaterialIndexes {
		if err := addMaterial(s.containerCards[index]); err != nil {
			return cardInfo{}, cardInfo{}, 0, nil, err
		}
	}
	for _, use := range stackUses {
		bonus := 0
		for attribute, value := range s.cardProgression.FameMaterials[use.CardID] {
			if baseDefinition.FusionAttributes&(1<<attribute) != 0 {
				bonus = max(bonus, value) // Dual attributes take the maximum, not the sum.
			}
		}
		if bonus > 0 {
			addition := int64(bonus) * int64(use.Num)
			if addition > int64(math.MaxInt-fameUp) {
				return cardInfo{}, cardInfo{}, 0, nil, errors.New("card fusion fame overflows")
			}
			fameUp += int(addition)
		}
		for _, stack := range s.stackCards {
			if stack.CardID == use.CardID {
				addition := int64(stack.AddExperience) * int64(use.Num)
				if stack.AddExperience <= 0 || addition > math.MaxInt || int64(expUp)+addition > math.MaxInt {
					return cardInfo{}, cardInfo{}, 0, nil, errors.New("card fusion experience overflows")
				}
				expUp += int(addition)
				break
			}
		}
	}
	updated := oldCard
	success := s.cardProgression.FusionSuccessTypes[0]
	if oldCard.Level < oldCard.LevelMax {
		rolled, rollErr := rollCardFusionSuccess(s.cardProgression)
		if rollErr != nil {
			return cardInfo{}, cardInfo{}, 0, nil, rollErr
		}
		success = rolled
		boosted, boostErr := boostedFusionExperience(expUp, success)
		if boostErr != nil || updated.Experience > math.MaxInt-boosted {
			return cardInfo{}, cardInfo{}, 0, nil, errors.New("card fusion experience overflows")
		}
		updated.Experience += boosted
	}
	if fameUp > 0 && updated.Fame < baseDefinition.FameMax {
		remaining := baseDefinition.FameMax - updated.Fame
		if fameUp > remaining {
			fameUp = remaining
		}
		updated.Fame += fameUp
	} else {
		fameUp = 0
	}
	if oldCard.Level >= oldCard.LevelMax && fameUp == 0 {
		return cardInfo{}, cardInfo{}, 0, nil, &businessError{-1, "这张卡牌的等级和名声已无法通过所选素材提升。"}
	}
	updated, err := s.normalizeCardLocked(updated)
	if err != nil {
		return cardInfo{}, cardInfo{}, 0, nil, err
	}
	s.cards[baseIndex] = updated
	kept := make([]cardInfo, 0, len(s.cards)-len(materialIndexes))
	for index, card := range s.cards {
		if _, remove := materialIndexes[index]; !remove {
			kept = append(kept, card)
		} else {
			s.clearCardFromDecksLocked(card.UniqueID)
		}
	}
	s.cards = kept
	if len(containerMaterialIndexes) != 0 {
		keptContainer := make([]cardInfo, 0, len(s.containerCards)-len(containerMaterialIndexes))
		for index, card := range s.containerCards {
			if _, remove := containerMaterialIndexes[index]; !remove {
				keptContainer = append(keptContainer, card)
			}
		}
		s.containerCards = keptContainer
	}
	consumeStackUses(s.stackCards, stackUses)
	s.gold -= cost
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "fusion"}); err != nil {
		return cardInfo{}, cardInfo{}, 0, nil, err
	}
	return oldCard, updated, success.SuccessType, s.rankedDecksLocked(s.decks), nil
}

func (s *store) evolveCard(
	baseUniqueID int64,
	toCardID int,
	materialUniqueIDs []int64,
	containerMaterialUniqueIDs []int64,
	materialCardIDs []int,
) (cardInfo, int, []deckInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := cardIndexByUniqueID(s.cards, baseUniqueID)
	if baseIndex < 0 {
		return cardInfo{}, 0, nil, errors.New("unknown evolution base card")
	}
	base := s.cards[baseIndex]
	if s.cardFameTraining != nil && s.cardFameTraining.UniqueID == baseUniqueID {
		return cardInfo{}, 0, nil, errors.New("evolution base is in active fame training")
	}
	var transition *release.EvolutionTransition
	for index := range s.cardActions.EvolutionTransitions {
		candidate := &s.cardActions.EvolutionTransitions[index]
		if candidate.FromCardID == base.CardID && candidate.ToCardID == toCardID {
			transition = candidate
			break
		}
	}
	if transition == nil || base.Level != base.LevelMax {
		return cardInfo{}, 0, nil, errors.New("card is not ready for evolution")
	}
	materialIndexes := make(map[int]struct{}, len(materialUniqueIDs))
	selectedMaterials := make(map[int]int, len(materialUniqueIDs)+len(containerMaterialUniqueIDs)+len(materialCardIDs))
	for _, uniqueID := range materialUniqueIDs {
		index := cardIndexByUniqueID(s.cards, uniqueID)
		if index < 0 {
			return cardInfo{}, 0, nil, errCardUnavailable
		}
		if index == baseIndex {
			return cardInfo{}, 0, nil, errors.New("invalid evolution material card")
		}
		if err := s.checkCardConsumptionLocked(s.cards[index]); err != nil {
			return cardInfo{}, 0, nil, err
		}
		materialIndexes[index] = struct{}{}
		selectedMaterials[s.cards[index].CardID]++
	}
	if len(materialIndexes) != len(materialUniqueIDs) {
		return cardInfo{}, 0, nil, errors.New("duplicate evolution material card")
	}
	containerMaterialIndexes := make(map[int]struct{}, len(containerMaterialUniqueIDs))
	for _, uniqueID := range containerMaterialUniqueIDs {
		index := cardIndexByUniqueID(s.containerCards, uniqueID)
		if index < 0 {
			return cardInfo{}, 0, nil, errCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.containerCards[index]); err != nil {
			return cardInfo{}, 0, nil, err
		}
		containerMaterialIndexes[index] = struct{}{}
		selectedMaterials[s.containerCards[index].CardID]++
	}
	if len(containerMaterialIndexes) != len(containerMaterialUniqueIDs) {
		return cardInfo{}, 0, nil, errors.New("duplicate container evolution material card")
	}
	stackUses, err := evolutionStackUses(materialCardIDs, selectedMaterials, transition.Materials)
	if err != nil {
		return cardInfo{}, 0, nil, err
	}
	if err := validateStackUses(s.stackCards, stackUses); err != nil {
		return cardInfo{}, 0, nil, err
	}
	for _, use := range stackUses {
		selectedMaterials[use.CardID] += use.Num
	}
	if !sameEvolutionMaterials(selectedMaterials, transition.Materials) {
		return cardInfo{}, 0, nil, errors.New("evolution materials differ")
	}
	// GOD evolution checks each material card's fame, not the sum of a family.
	if transition.Type == 1 {
		for _, required := range transition.Materials {
			for index := range materialIndexes {
				card := s.cards[index]
				if card.CardID == required.CardID && card.Fame < required.Fame {
					return cardInfo{}, 0, nil, &businessError{-1, "进化素材名声不足。"}
				}
			}
			for index := range containerMaterialIndexes {
				card := s.containerCards[index]
				if card.CardID == required.CardID && card.Fame < required.Fame {
					return cardInfo{}, 0, nil, &businessError{-1, "进化素材名声不足。"}
				}
			}
		}
	}
	if s.gold < transition.Gold {
		return cardInfo{}, 0, nil, errInsufficientGold
	}
	template, exists := s.cardTemplates[toCardID]
	if !exists {
		return cardInfo{}, 0, nil, errors.New("evolution target template is missing")
	}
	result := template
	result.UniqueID = base.UniqueID
	result.Love = base.Love
	result.Fame = base.Fame
	result.IsLock = base.IsLock
	if transition.KeepLevel {
		definition, exists := s.cardDefinitions[toCardID]
		if !exists {
			return cardInfo{}, 0, nil, errors.New("evolution target definition is missing")
		}
		experience, err := release.CardExperienceAtLevelStart(min(base.Level, definition.LevelMax), definition.LevelMax, s.cardExperience[definition.ExperienceTableID])
		if err != nil {
			return cardInfo{}, 0, nil, err
		}
		result.Experience = experience
	}
	result, err = s.normalizeCardLocked(result)
	if err != nil {
		return cardInfo{}, 0, nil, err
	}
	for _, deck := range s.decks {
		if !deckContainsCardUniqueID(deck, base.UniqueID) {
			continue
		}
		if err := s.validateDeckCardFamilies(deck, base.UniqueID, toCardID); err != nil {
			return cardInfo{}, 0, nil, fmt.Errorf("evolution target invalidates deck: %w", err)
		}
	}
	s.cards[baseIndex] = result
	s.recordCollectedCardLocked(result)
	if len(materialIndexes) != 0 {
		kept := make([]cardInfo, 0, len(s.cards)-len(materialIndexes))
		for index, card := range s.cards {
			if _, remove := materialIndexes[index]; !remove {
				kept = append(kept, card)
			} else {
				s.clearCardFromDecksLocked(card.UniqueID)
			}
		}
		s.cards = kept
	}
	if len(containerMaterialIndexes) != 0 {
		keptContainer := make([]cardInfo, 0, len(s.containerCards)-len(containerMaterialIndexes))
		for index, card := range s.containerCards {
			if _, remove := containerMaterialIndexes[index]; !remove {
				keptContainer = append(keptContainer, card)
			}
		}
		s.containerCards = keptContainer
	}
	consumeStackUses(s.stackCards, stackUses)
	s.gold -= transition.Gold
	return result, s.gold, s.rankedDecksLocked(s.decks), nil
}

func (s *store) sellCards(
	uniqueIDs []int64,
	stackUses []release.CardStackUse,
) (int, int, []deckInfo, error) {
	if len(uniqueIDs) == 0 && len(stackUses) == 0 {
		return 0, 0, nil, errors.New("sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := make(map[int]struct{}, len(uniqueIDs))
	getGold := 0
	for _, uniqueID := range uniqueIDs {
		index := cardIndexByUniqueID(s.cards, uniqueID)
		if index < 0 {
			return 0, 0, nil, errCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.cards[index]); err != nil {
			return 0, 0, nil, err
		}
		definition, exists := s.cardDefinitions[s.cards[index].CardID]
		if !exists || definition.SellGold < 0 {
			return 0, 0, nil, errors.New("card sell price is unavailable")
		}
		remove[index] = struct{}{}
		var err error
		getGold, err = checkedCardSaleGold(s.gold, getGold, definition.SellGold, 1)
		if err != nil {
			return 0, 0, nil, err
		}
	}
	if len(remove) != len(uniqueIDs) {
		return 0, 0, nil, errors.New("duplicate sell card")
	}
	if err := validateStackUses(s.stackCards, stackUses); err != nil {
		return 0, 0, nil, err
	}
	for _, use := range stackUses {
		for _, stack := range s.stackCards {
			if stack.CardID == use.CardID {
				var err error
				getGold, err = checkedCardSaleGold(s.gold, getGold, stack.BaseAddPrice, use.Num)
				if err != nil {
					return 0, 0, nil, err
				}
				break
			}
		}
	}
	kept := make([]cardInfo, 0, len(s.cards)-len(remove))
	for index, card := range s.cards {
		if _, exists := remove[index]; !exists {
			kept = append(kept, card)
		} else {
			s.clearCardFromDecksLocked(card.UniqueID)
		}
	}
	s.cards = kept
	consumeStackUses(s.stackCards, stackUses)
	s.gold += getGold
	return getGold, s.gold, s.rankedDecksLocked(s.decks), nil
}

func (s *store) itemState() []release.Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]release.Item, 0, len(s.items))
	for _, item := range s.items {
		if item.Num > 0 {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].ItemID < items[right].ItemID
	})
	return items
}

func (s *store) cardCategoryState() ([]release.CardCategoryProfile, []release.CardGroupProfile) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]release.CardCategoryProfile(nil), s.cardCategoryProfiles...),
		cloneCardGroupProfiles(s.cardGroupProfiles)
}

func cloneCardGroupProfiles(source []release.CardGroupProfile) []release.CardGroupProfile {
	result := make([]release.CardGroupProfile, len(source))
	for index, group := range source {
		result[index] = group
		result[index].MemberCardIDs = append([]int(nil), group.MemberCardIDs...)
	}
	return result
}

func (s *store) itemExchangeProfileState() map[int]release.ItemExchangeProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[int]release.ItemExchangeProfile, len(s.itemExchangeProfiles))
	for itemID, profile := range s.itemExchangeProfiles {
		profile.Reward = cloneReward(profile.Reward)
		result[itemID] = profile
	}
	return result
}

func (s *store) itemLackTipState(index int) (release.ItemLackTipProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, exists := s.itemLackTipProfiles[index]
	if !exists {
		return release.ItemLackTipProfile{}, errors.New("item lack-tip profile is unavailable")
	}
	links := make([]release.ItemLackTipLink, len(profile.TextURLs))
	copy(links, profile.TextURLs)
	profile.TextURLs = links
	return profile, nil
}

func (s *store) itemCount(itemID int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if itemID <= 0 {
		return 0
	}
	return s.items[itemID].Num
}

func (s *store) itemShopState() ([]release.ItemShopTab, map[int]int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	owned := make(map[int]int, len(s.items))
	for itemID, item := range s.items {
		owned[itemID] = item.Num
	}
	tabs := cloneItemShopTabs(s.itemShopTabs)
	for ti := range tabs {
		for li := range tabs[ti].Lineup {
			tabs[ti].Lineup[li] = s.configuredItemShopLineup(tabs[ti].Lineup[li])
		}
	}
	return tabs, owned
}

type eventShopLineupState struct {
	Profile     release.EventShopLineupProfile
	StockRemain int
}

type eventShopState struct {
	EventID      int
	PointItemID  int
	PointItemNum int
	Lineups      []eventShopLineupState
}

func (s *store) eventShopState(eventID int) (eventShopState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eventShopStateLocked(eventID)
}

func (s *store) eventShopStateLocked(eventID int) (eventShopState, error) {
	if eventID <= 0 {
		return eventShopState{}, errors.New("invalid event shop ID")
	}
	profile, published := s.eventShopProfiles[eventID]
	if !published {
		return eventShopState{}, errors.New("event shop is unavailable from official local data")
	}
	result := eventShopState{
		EventID:      profile.EventID,
		PointItemID:  profile.PointItemID,
		PointItemNum: s.items[profile.PointItemID].Num,
		Lineups:      make([]eventShopLineupState, len(profile.Lineups)),
	}
	for index, lineup := range profile.Lineups {
		remain := lineup.StockNum
		if remain > 0 {
			remain -= s.eventShopPurchases[lineup.LineupID]
			if remain < 0 {
				remain = 0
			}
		}
		lineup.Reward = cloneReward(lineup.Reward)
		result.Lineups[index] = eventShopLineupState{Profile: lineup, StockRemain: remain}
	}
	return result, nil
}

type tradeShopLineupState struct {
	Profile     release.TradeShopLineupProfile
	StockRemain int
	IsNew       int8
}

type tradeShopState struct {
	Profile release.TradeShopProfile
	Lineups []tradeShopLineupState
	Owns    []release.TradeShopPointProfile
}

func (s *store) tradeShopState() []tradeShopState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tradeShopStateLocked()
}

func (s *store) tradeShopStateLocked() []tradeShopState {
	shopIDs := make([]int, 0, len(s.tradeShopProfiles))
	for shopID := range s.tradeShopProfiles {
		shopIDs = append(shopIDs, shopID)
	}
	sort.Ints(shopIDs)
	ownedCards := s.ownedCardIDsLocked()
	result := make([]tradeShopState, 0, len(shopIDs))
	ownedStacks := make(map[int]bool, len(s.stackCards))
	for _, stack := range s.stackCards {
		if stack.Num > 0 {
			ownedStacks[stack.CardID] = true
		}
	}
	for _, shopID := range shopIDs {
		profile := cloneTradeShopProfile(s.tradeShopProfiles[shopID])
		if profile.Disabled || (profile.EndTime > 0 && time.Now().Unix() >= int64(profile.EndTime)) {
			continue
		}
		lineups := profile.Lineups[:0]
		for _, lineup := range profile.Lineups {
			if !lineup.Disabled {
				lineups = append(lineups, lineup)
			}
		}
		profile.Lineups = lineups
		if len(lineups) == 0 {
			continue
		}
		state := tradeShopState{
			Profile: profile,
			Lineups: make([]tradeShopLineupState, len(profile.Lineups)),
		}
		pointKeys := make(map[[2]int]struct{})
		for index, lineup := range profile.Lineups {
			remain := lineup.StockNum
			if remain > 0 {
				remain -= s.tradeShopPurchases[lineup.LineupID]
				if remain < 0 {
					remain = 0
				}
			}
			isNew := int8(0)
			if len(lineup.Rewards) == 1 && lineup.Rewards[0].Type == 6 {
				if _, owned := ownedCards[lineup.Rewards[0].RewardTypeID]; !owned {
					isNew = 1
				}
			}
			if len(lineup.Rewards) == 1 && lineup.Rewards[0].Type == 13 && !ownedStacks[lineup.Rewards[0].RewardTypeID] {
				isNew = 1
			}
			state.Lineups[index] = tradeShopLineupState{
				Profile:     lineup,
				StockRemain: remain,
				IsNew:       isNew,
			}
			for _, price := range lineup.Prices {
				key := [2]int{price.Type, price.ID}
				if _, exists := pointKeys[key]; exists {
					continue
				}
				pointKeys[key] = struct{}{}
				owned := price
				owned.Num = s.items[price.ID].Num
				state.Owns = append(state.Owns, owned)
			}
		}
		sort.Slice(state.Owns, func(left, right int) bool {
			if state.Owns[left].Type != state.Owns[right].Type {
				return state.Owns[left].Type < state.Owns[right].Type
			}
			return state.Owns[left].ID < state.Owns[right].ID
		})
		result = append(result, state)
	}
	return result
}

func (s *store) gachaState() []release.GachaProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.visibleGachasLocked()
}

// homeBannerGachaID selects a crystal multi-draw profile because those rows
// remain visible whether or not the account owns a single-draw ticket. The CN
// client resolves the exact gacha ID to its group before displaying the scene.
func (s *store) homeBannerGachaID(now time.Time) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selected := release.GachaProfile{}
	for _, gacha := range s.gachas {
		if !s.gachaScheduledLocked(gacha.GachaID) || gacha.GachaType != 0 || gacha.PayType != 3 ||
			gacha.CardNum <= 1 || gacha.CardNumMax <= 1 ||
			int64(gacha.EndTime) <= now.Unix() {
			continue
		}
		if selected.GachaID == 0 ||
			gacha.CategoryNum < selected.CategoryNum ||
			(gacha.CategoryNum == selected.CategoryNum && gacha.OrderNum < selected.OrderNum) ||
			(gacha.CategoryNum == selected.CategoryNum && gacha.OrderNum == selected.OrderNum &&
				gacha.GachaID < selected.GachaID) {
			selected = gacha
		}
	}
	return selected.GachaID
}

func (s *store) gachaStateWithOwnership() ([]release.GachaProfile, map[int]struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ownedCardIDs := make(map[int]struct{}, len(s.cards)+len(s.containerCards))
	for _, inventory := range [][]cardInfo{s.cards, s.containerCards} {
		for _, card := range inventory {
			ownedCardIDs[card.CardID] = struct{}{}
		}
	}
	return s.visibleGachasLocked(), ownedCardIDs
}

func (s *store) visibleGachasLocked() []release.GachaProfile {
	// The original guide selects the first category. The local client's selector
	// keeps a sole gacha category ahead of exchange, so no normal pool is needed
	// to make the reserved tutorial pool the landing page.
	tutorialGachaID := s.onboardingVisibleGachaForStepLocked()
	if tutorialGachaID != 0 {
		for _, gacha := range s.gachas {
			if gacha.GachaID == tutorialGachaID && gacha.PlayCount == 0 {
				return cloneGachaProfiles([]release.GachaProfile{gacha})
			}
		}
		return []release.GachaProfile{}
	}

	preferredSingles := make(map[int]int)
	for _, payType := range []int{4, 2, 3, 6} {
		for _, gacha := range s.gachas {
			if isOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) {
				continue
			}
			if gacha.CardNumMax != 1 || gacha.PayType != payType {
				continue
			}
			if _, exists := preferredSingles[gacha.GroupID]; exists {
				continue
			}
			if payType == 4 && s.items[gacha.PayTypeID].Num < gacha.Price {
				continue
			}
			preferredSingles[gacha.GroupID] = gacha.GachaID
		}
	}
	// A group whose only single-draw payment is an item must remain visible
	// even when the account does not own that item. The stock client uses that
	// row to open ItemLackTips (notably item 9010 / idx 8). The preference pass
	// above may still replace an unavailable ticket row when the same group has
	// an actually usable crystal or FP single-draw alternative.
	for _, gacha := range s.gachas {
		if isOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) || gacha.CardNumMax != 1 {
			continue
		}
		if _, exists := preferredSingles[gacha.GroupID]; exists {
			continue
		}
		preferredSingles[gacha.GroupID] = gacha.GachaID
	}

	visible := make([]release.GachaProfile, 0, len(s.gachas))
	for _, gacha := range s.gachas {
		if isOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) {
			continue
		}
		gacha = s.currentGachaLocked(gacha)
		if gacha.UnownedOnly && len(gacha.CardIDs) == 0 {
			continue
		}
		if gacha.CardNumMax == 1 && preferredSingles[gacha.GroupID] != gacha.GachaID {
			continue
		}
		visible = append(visible, gacha)
	}
	result := cloneGachaProfiles(visible)
	dayKey := gachaLocalDayKey(time.Now())
	for index := range result {
		result[index].DailyFirstAvailable = result[index].DailyFirstFree &&
			s.gachaDailyClaims[result[index].GachaID] != dayKey
	}
	return result
}

func isOnboardingGachaID(gachaID int) bool {
	return gachaID == cnOnboardingGachaID || gachaID == cnOnboardingMultiGachaID
}

// onboardingVisibleGachaForStepLocked follows the stock client's two reserved
// first-gacha families.  The single-draw profile is replaced by the multi-draw
// profile immediately after the fixed single draw; the multi remains visible
// while fusion, deck editing, and the first area are still guiding the player.
// Playback is gated separately by gachaAvailableForPlayLocked.
func (s *store) onboardingVisibleGachaForStepLocked() int {
	if s.onboarding.ConfigVersion != cnOnboardingConfigVersion {
		return 0
	}
	switch s.onboarding.Step {
	case 1:
		return cnOnboardingGachaID
	case 2, 3, 4, 5:
		return cnOnboardingMultiGachaID
	default:
		return 0
	}
}

func (s *store) gachaAvailableForPlayLocked(gachaID int) bool {
	if !s.gachaScheduledLocked(gachaID) {
		return false
	}
	if s.onboarding.ConfigVersion == cnOnboardingConfigVersion {
		switch s.onboarding.Step {
		case 0, 2, 3, 4:
			return false
		case 1:
			return gachaID == cnOnboardingGachaID
		case 5:
			return gachaID == cnOnboardingMultiGachaID
		}
	}
	return !isOnboardingGachaID(gachaID)
}

func gachaLocalDayKey(now time.Time) string {
	return now.In(time.FixedZone("CN local service day", 8*60*60)).Format("2006-01-02")
}

type gachaLineupEntry struct {
	Prize release.Reward
	IsNew int8
}

func (s *store) gachaSelectLineup(gachaID int) ([]gachaLineupEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil {
		return nil, errGachaUnavailable
	}
	if profile.UserSelectMax <= 0 {
		return nil, errors.New("gacha has no user-select lineup")
	}
	owned := s.ownedCardIDsLocked()
	result := make([]gachaLineupEntry, len(profile.CardIDs))
	for index, cardID := range profile.CardIDs {
		result[index] = gachaLineupEntry{
			Prize: gachaCardReward(cardID),
			IsNew: cardNewFlag(owned, cardID),
		}
	}
	return result, nil
}

func (s *store) gachaSelectedLineup(gachaID int) ([]gachaLineupEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil {
		return nil, errGachaUnavailable
	}
	if profile.UserSelectMax <= 0 {
		return nil, errors.New("gacha has no user-select lineup")
	}
	selected := s.gachaSelections[gachaID]
	owned := s.ownedCardIDsLocked()
	result := make([]gachaLineupEntry, len(selected))
	for index, reward := range selected {
		result[index] = gachaLineupEntry{
			Prize: cloneReward(reward),
			IsNew: cardNewFlag(owned, reward.RewardTypeID),
		}
	}
	return result, nil
}

func (s *store) ownedCardIDsLocked() map[int]struct{} {
	owned := make(map[int]struct{}, len(s.cards)+len(s.containerCards))
	for _, inventory := range [][]cardInfo{s.cards, s.containerCards} {
		for _, card := range inventory {
			owned[card.CardID] = struct{}{}
		}
	}
	return owned
}

func findGachaProfile(gachas []release.GachaProfile, gachaID int) *release.GachaProfile {
	for index := range gachas {
		if gachas[index].GachaID == gachaID {
			return &gachas[index]
		}
	}
	return nil
}

func gachaCardReward(cardID int) release.Reward {
	return release.Reward{
		Type: 6, Num: 1, RewardTypeID: cardID,
		CardLevel: 1, CardFame: 1, CardSkillLevels: []int16{1},
	}
}

func validateGachaSelection(gacha release.GachaProfile, rewards []release.Reward) error {
	if gacha.UserSelectMax < 0 || gacha.UserSelectMax > len(gacha.CardIDs) {
		return errors.New("gacha user-select configuration is invalid")
	}
	if len(rewards) != gacha.UserSelectMax {
		return errors.New("gacha user-select lineup has an invalid length")
	}
	allowed := make(map[int]struct{}, len(gacha.CardIDs))
	for _, cardID := range gacha.CardIDs {
		allowed[cardID] = struct{}{}
	}
	seen := make(map[int]struct{}, len(rewards))
	for _, reward := range rewards {
		if reward.Type != 6 || reward.Num != 1 || reward.CardLevel != 1 ||
			reward.CardFame != 1 || reward.CardLove != 0 ||
			len(reward.CardSkillLevels) != 1 || reward.CardSkillLevels[0] != 1 {
			return errors.New("gacha user-select lineup contains an invalid reward")
		}
		if _, exists := allowed[reward.RewardTypeID]; !exists {
			return errors.New("gacha user-select lineup contains an unavailable card")
		}
		if _, duplicate := seen[reward.RewardTypeID]; duplicate {
			return errors.New("gacha user-select lineup repeats a card")
		}
		seen[reward.RewardTypeID] = struct{}{}
	}
	return nil
}

func cloneRewards(source []release.Reward) []release.Reward {
	result := make([]release.Reward, len(source))
	for index, reward := range source {
		result[index] = cloneReward(reward)
	}
	return result
}

func cloneTowerQuestProfile(source release.TowerQuestProfile) release.TowerQuestProfile {
	result := source
	result.Ranks = append([]release.TowerQuestRankProfile(nil), source.Ranks...)
	result.Floors = make([]release.TowerQuestFloorProfile, len(source.Floors))
	for index, floor := range source.Floors {
		floor.Boss = append(json.RawMessage(nil), floor.Boss...)
		floor.ClearRewards = cloneRewards(floor.ClearRewards)
		result.Floors[index] = floor
	}
	return result
}

func cloneTowerQuestProgress(source release.TowerQuestProgress) release.TowerQuestProgress {
	result := source
	result.ClearedFloors = append([]int(nil), source.ClearedFloors...)
	return result
}

type gachaPlayResult struct {
	Gachas       []release.GachaProfile
	Item         release.Item
	Reward       presentReceiveResult
	Expectancy   int
	Gifts        []release.Reward
	PresentGifts []release.Reward
}

// CN GACHA_EXPECTANCY is zero-based (RARE=2, SUPERRARE=3, ULTRARARE=4),
// whereas the official card master projects rarity_rank as 1..8. The whole
// draw uses its highest actual result, not the pool's maximum or an evolved card.
func gachaResultExpectancy(rewards []release.Reward, definitions map[int]release.Card) int {
	maxRank := 1
	for _, reward := range rewards {
		if reward.Type == 6 && reward.Num > 0 {
			maxRank = max(maxRank, definitions[reward.RewardTypeID].RarityRank)
		}
	}
	return min(maxRank, 8) - 1
}

type itemGachaPlayResult struct {
	Item   release.Item
	Reward presentReceiveResult
}

type itemExchangeResult struct {
	Item   release.Item
	Reward presentReceiveResult
}

func (s *store) playItemGacha(itemID int, playCount int) (itemGachaPlayResult, error) {
	if itemID <= 0 || playCount < 1 || playCount > 10 {
		return itemGachaPlayResult{}, errors.New("invalid item gacha selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	item, owned := s.items[itemID]
	if !owned || item.Num < playCount {
		return itemGachaPlayResult{}, errInsufficientMaterials
	}
	if item.LimitTime > 0 && time.Now().Unix() >= int64(item.LimitTime) {
		return itemGachaPlayResult{}, errItemExpired
	}
	definition, exists := s.itemDefinitions[itemID]
	if !exists || definition.ItemType != "GACHA" || definition.Function != "GACHA_EXEC" {
		return itemGachaPlayResult{}, errors.New("item cannot execute a gacha")
	}
	profile, exists := s.itemGachaProfiles[itemID]
	if !exists || profile.FunctionValue != definition.FunctionValue {
		return itemGachaPlayResult{}, errors.New("item gacha pool unavailable from official local data")
	}
	rewards := make([]release.Reward, 0, len(profile.Rewards)*playCount)
	for play := 0; play < playCount; play++ {
		rewards = append(rewards, profile.Rewards...)
		if len(profile.RewardPool) > 0 {
			reward, err := drawWeightedReward(profile.RewardPool)
			if err != nil {
				return itemGachaPlayResult{}, err
			}
			rewards = append(rewards, reward)
		}
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return itemGachaPlayResult{}, err
	}

	item.Num -= playCount
	s.items[itemID] = item
	result := presentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &result, "扭蛋奖励"); err != nil {
			return itemGachaPlayResult{}, err
		}
	}
	return itemGachaPlayResult{Item: s.items[itemID], Reward: result}, nil
}

func (s *store) exchangeItem(itemID int, changeSets int) (itemExchangeResult, error) {
	if itemID <= 0 || changeSets <= 0 {
		return itemExchangeResult{}, errors.New("invalid item exchange selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	profile, published := s.itemExchangeProfiles[itemID]
	if !published || profile.IsAppearEvent != 0 {
		return itemExchangeResult{}, errors.New("item exchange is unavailable from official local data")
	}
	item, owned := s.items[itemID]
	if !owned || item.Num <= 0 {
		return itemExchangeResult{}, errInsufficientMaterials
	}
	if item.LimitTime > 0 && time.Now().Unix() >= int64(item.LimitTime) {
		return itemExchangeResult{}, errItemExpired
	}
	maximum := int(^uint(0) >> 1)
	if profile.NeedNum > maximum/changeSets {
		return itemExchangeResult{}, errors.New("item exchange cost overflows")
	}
	cost := profile.NeedNum * changeSets
	if item.Num < cost {
		return itemExchangeResult{}, errInsufficientMaterials
	}
	reward := cloneReward(profile.Reward)
	if reward.Num <= 0 || reward.Num > maximum/changeSets {
		return itemExchangeResult{}, errors.New("item exchange reward overflows")
	}
	reward.Num *= changeSets
	if err := s.validateLocalTradeRewardLocked(reward); err != nil {
		return itemExchangeResult{}, err
	}
	if err := s.validateLocalTradeCapacityLocked(itemID, cost, reward); err != nil {
		return itemExchangeResult{}, err
	}

	previousItem := item
	item.Num -= cost
	s.items[itemID] = item
	result := presentReceiveResult{}
	if err := s.applyLocalTradeRewardLocked(reward, &result); err != nil {
		s.items[itemID] = previousItem
		return itemExchangeResult{}, err
	}
	return itemExchangeResult{Item: s.items[itemID], Reward: result}, nil
}

func (s *store) validateLocalTradeRewardLocked(reward release.Reward) error {
	if reward.Num <= 0 || reward.CardSkillLevels == nil {
		return errors.New("local trade reward is incomplete")
	}
	switch reward.Type {
	case 4, 10, 12:
		if reward.RewardTypeID != 0 {
			return errors.New("local trade scalar reward has an unexpected ID")
		}
	case 6:
		if _, exists := s.cardTemplates[reward.RewardTypeID]; !exists ||
			reward.CardLevel < 1 || reward.CardFame < 1 || reward.CardLove < 0 ||
			len(reward.CardSkillLevels) == 0 {
			return errors.New("local trade references an invalid card reward")
		}
	case 8:
		if _, exists := s.itemDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("local trade references an unknown item reward")
		}
	case 13:
		if _, exists := s.stackCardTemplates[reward.RewardTypeID]; exists {
			return nil
		}
		return errors.New("local trade references an unknown stack-card reward")
	case 15:
		if _, exists := s.sphereDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("local trade references an unknown sphere reward")
		}
	case 19:
		if _, exists := s.buddyDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("local trade references an unknown buddy reward")
		}
	default:
		return fmt.Errorf("unsupported local trade reward type %d", reward.Type)
	}
	return nil
}

func (s *store) validateLocalTradeCapacityLocked(itemID int, cost int, reward release.Reward) error {
	maximum := int(^uint(0) >> 1)
	switch reward.Type {
	case 4:
		if s.gold > maximum-reward.Num {
			return errors.New("local trade gold reward overflows")
		}
	case 6:
		if reward.Num > s.cardMax-len(s.cards) {
			return errCardCapacity
		}
	case 8:
		owned := s.items[reward.RewardTypeID].Num
		if reward.RewardTypeID == itemID {
			owned -= cost
		}
		definition := s.itemDefinitions[reward.RewardTypeID]
		if owned < 0 || reward.Num > definition.MaxOwned-owned {
			return errItemCapacity
		}
	case 10:
		if s.coinFree > maximum-reward.Num {
			return errors.New("local trade crystal reward overflows")
		}
	case 13:
		owned := 0
		for _, stack := range s.stackCards {
			if stack.CardID == reward.RewardTypeID {
				owned = stack.Num
				break
			}
		}
		if owned < 0 || reward.Num > maximum-owned {
			return errors.New("local trade stack-card reward overflows")
		}
	case 15:
		if reward.Num > s.sphereMax-len(s.spheres) {
			return errSphereCapacity
		}
		if s.nextSphereUniqueID <= 0 || int64(reward.Num) > int64(^uint64(0)>>1)-s.nextSphereUniqueID {
			return errors.New("sphere unique ID space is exhausted")
		}
	case 19:
		if reward.Num > s.buddyMax-len(s.buddies) {
			return errBuddyCapacity
		}
		if s.nextBuddyUniqueID <= 0 || int64(reward.Num) > int64(^uint64(0)>>1)-s.nextBuddyUniqueID {
			return errors.New("buddy unique ID space is exhausted")
		}
	}
	return nil
}

func (s *store) applyLocalTradeRewardLocked(reward release.Reward, result *presentReceiveResult) error {
	return s.applyRewardLocked(reward, result)
}

func (s *store) playGacha(
	gachaID int,
	payType int,
	selectedRewards []release.Reward,
) (gachaPlayResult, error) {
	if gachaID <= 0 {
		return gachaPlayResult{}, errors.New("invalid gacha ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for current := range s.gachas {
		if s.gachas[current].GachaID == gachaID {
			index = current
			break
		}
	}
	if index < 0 || payType != s.gachas[index].PayType {
		return gachaPlayResult{}, errGachaUnavailable
	}
	profile := s.currentGachaLocked(s.gachas[index])
	gacha := &profile
	if gacha.PlayCount == math.MaxInt || (gacha.UnownedOnly && len(gacha.CardIDs) == 0) {
		return gachaPlayResult{}, errGachaUnavailable
	}
	if !s.gachaAvailableForPlayLocked(gachaID) ||
		(isOnboardingGachaID(gachaID) && gacha.PlayCount != 0) {
		return gachaPlayResult{}, errGachaUnavailable
	}
	if err := validateGachaSelection(*gacha, selectedRewards); err != nil {
		return gachaPlayResult{}, err
	}
	dayKey := gachaLocalDayKey(time.Now())
	dailyFree := gacha.DailyFirstFree && s.gachaDailyClaims[gachaID] != dayKey
	drawCount := gacha.CardNum
	paymentCost := gacha.Price
	if dailyFree {
		paymentCost = 0
	} else if gacha.PayType == 2 && gacha.CardNumMax > gacha.CardNum {
		drawCount = min(gacha.CardNumMax, s.friendPoint/gacha.Price)
		if drawCount < gacha.CardNum {
			return gachaPlayResult{}, errInsufficientFriendPoints
		}
		if gacha.Price > math.MaxInt/drawCount {
			return gachaPlayResult{}, errors.New("friend-point gacha cost overflows")
		}
		paymentCost = gacha.Price * drawCount
	}
	paidItem := release.Item{}
	switch gacha.PayType {
	case 2:
		if s.friendPoint < paymentCost {
			return gachaPlayResult{}, errInsufficientFriendPoints
		}
	case 3:
		if int64(s.coin)+int64(s.coinFree) < int64(paymentCost) {
			return gachaPlayResult{}, errInsufficientCrystals
		}
	case 4:
		paidItem = s.items[gacha.PayTypeID]
		if paidItem.Num < paymentCost || (paidItem.LimitTime > 0 && time.Now().Unix() >= int64(paidItem.LimitTime)) {
			return gachaPlayResult{}, errInsufficientMaterials
		}
	case 6:
		if s.coin < paymentCost {
			return gachaPlayResult{}, errInsufficientPaidCrystals
		}
	default:
		return gachaPlayResult{}, errors.New("unsupported gacha payment type")
	}

	drawCardIDs := gacha.CardIDs
	drawCardWeights := gacha.CardWeights
	if len(selectedRewards) > 0 {
		drawCardIDs = make([]int, len(selectedRewards))
		drawCardWeights = make([]int, len(selectedRewards))
		weightsByCardID := make(map[int]int, len(gacha.CardIDs))
		for cardIndex, cardID := range gacha.CardIDs {
			weightsByCardID[cardID] = gacha.CardWeights[cardIndex]
		}
		for selectedIndex, reward := range selectedRewards {
			drawCardIDs[selectedIndex] = reward.RewardTypeID
			drawCardWeights[selectedIndex] = weightsByCardID[reward.RewardTypeID]
		}
	}
	guaranteedCardIDs := []int(nil)
	guaranteedCardWeights := []int(nil)
	remainderCardIDs := drawCardIDs
	remainderCardWeights := drawCardWeights
	if gacha.GuaranteedCount > 0 {
		if len(selectedRewards) != 0 || gacha.GuaranteedCount >= drawCount ||
			gacha.GuaranteedRarityRank <= 0 || gacha.RemainderRarityRank <= 0 {
			return gachaPlayResult{}, errors.New("gacha guaranteed-result policy is invalid")
		}
		var err error
		guaranteedCardIDs, guaranteedCardWeights, err = s.gachaPoolForRarityLocked(
			drawCardIDs, drawCardWeights, gacha.GuaranteedRarityRank,
		)
		if err != nil {
			return gachaPlayResult{}, err
		}
		remainderCardIDs, remainderCardWeights, err = s.gachaPoolForRarityLocked(
			drawCardIDs, drawCardWeights, gacha.RemainderRarityRank,
		)
		if err != nil {
			return gachaPlayResult{}, err
		}
	}
	rewards := make([]release.Reward, drawCount)
	for draw := range rewards {
		if len(gacha.RewardPool) > 0 {
			reward, err := drawWeightedReward(gacha.RewardPool)
			if err != nil {
				return gachaPlayResult{}, err
			}
			rewards[draw] = reward
			continue
		}
		poolCardIDs := remainderCardIDs
		poolCardWeights := remainderCardWeights
		if draw < gacha.GuaranteedCount {
			poolCardIDs = guaranteedCardIDs
			poolCardWeights = guaranteedCardWeights
		}
		cardID, err := weightedGachaCard(poolCardIDs, poolCardWeights)
		if err != nil {
			return gachaPlayResult{}, err
		}
		rewards[draw] = gachaCardReward(cardID)
		if err := s.validateRewardLocked(rewards[draw]); err != nil {
			return gachaPlayResult{}, err
		}
	}
	gifts := gacha.CurrentGifts()
	allRewards := append(cloneRewards(rewards), gifts...)
	if err := s.validateSettlementRewardsLocked(allRewards); err != nil {
		return gachaPlayResult{}, err
	}

	switch gacha.PayType {
	case 2:
		s.friendPoint -= paymentCost
	case 3:
		freeSpend := min(s.coinFree, paymentCost)
		s.coinFree -= freeSpend
		s.coin -= paymentCost - freeSpend
	case 4:
		paidItem.Num -= paymentCost
		s.items[paidItem.ItemID] = paidItem
	case 6:
		s.coin -= paymentCost
	}
	s.gachas[index].PlayCount++
	if dailyFree {
		s.gachaDailyClaims[gachaID] = dayKey
	}
	if len(selectedRewards) > 0 {
		s.gachaSelections[gachaID] = cloneRewards(selectedRewards)
	}
	result := presentReceiveResult{}
	for _, reward := range allRewards {
		if err := s.applyRewardOrPresentLocked(reward, &result, "扭蛋奖励"); err != nil {
			return gachaPlayResult{}, err
		}
	}
	directGifts, presentGifts := []release.Reward{}, []release.Reward{}
	for _, gift := range result.Rewards[len(rewards):] {
		if gift.InPresentBox {
			presentGifts = append(presentGifts, gift.Reward)
		} else {
			directGifts = append(directGifts, gift.Reward)
		}
	}
	// Gift inventory updates share the transaction but have their own original
	// client display; do not also animate them as ordinary draw results.
	result.Rewards = result.Rewards[:len(rewards)]
	if paidItem.ItemID != 0 {
		paidItem = s.items[paidItem.ItemID]
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "gacha", gachaID: gachaID}); err != nil {
		return gachaPlayResult{}, err
	}
	return gachaPlayResult{
		Gachas:       s.visibleGachasLocked(),
		Item:         paidItem,
		Reward:       result,
		Expectancy:   s.gachaMixedResultExpectancy(rewards),
		Gifts:        directGifts,
		PresentGifts: presentGifts,
	}, nil
}

func (s *store) gachaPoolForRarityLocked(
	cardIDs []int,
	weights []int,
	rarityRank int,
) ([]int, []int, error) {
	if len(cardIDs) == 0 || len(cardIDs) != len(weights) || rarityRank <= 0 {
		return nil, nil, errors.New("invalid gacha rarity pool request")
	}
	filteredIDs := make([]int, 0, len(cardIDs))
	filteredWeights := make([]int, 0, len(cardIDs))
	for index, cardID := range cardIDs {
		definition, exists := s.cardDefinitions[cardID]
		if !exists {
			return nil, nil, fmt.Errorf("gacha card definition %d is unavailable", cardID)
		}
		if definition.RarityRank != rarityRank {
			continue
		}
		filteredIDs = append(filteredIDs, cardID)
		filteredWeights = append(filteredWeights, weights[index])
	}
	if len(filteredIDs) == 0 {
		return nil, nil, fmt.Errorf("gacha rarity %d pool is empty", rarityRank)
	}
	return filteredIDs, filteredWeights, nil
}

func weightedGachaCard(cardIDs []int, weights []int) (int, error) {
	if len(cardIDs) == 0 || len(cardIDs) != len(weights) {
		return 0, errors.New("invalid gacha card weights")
	}
	total := 0
	for _, weight := range weights {
		if weight <= 0 || total > int(^uint(0)>>1)-weight {
			return 0, errors.New("invalid gacha card weight")
		}
		total += weight
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0, fmt.Errorf("select gacha card: %w", err)
	}
	selected := int(value.Int64())
	for index, weight := range weights {
		if selected < weight {
			return cardIDs[index], nil
		}
		selected -= weight
	}
	return 0, errors.New("select gacha card outside configured weights")
}

type itemUseResult struct {
	Item release.Item
	AP   apStatus
	BP   battlePointStatus
}

func (s *store) useItem(itemID int) (itemUseResult, error) {
	if itemID <= 0 {
		return itemUseResult{}, errors.New("invalid item ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.items[itemID]
	if !exists || item.Num <= 0 {
		return itemUseResult{}, errInsufficientMaterials
	}
	definition, exists := s.itemDefinitions[itemID]
	if !exists {
		return itemUseResult{}, errors.New("unknown item definition")
	}
	now := time.Now()
	s.refreshAPLocked(now)
	s.refreshBattlePointsLocked(now)
	switch definition.Function {
	case "BP_HEAL_FULL":
		s.bp = s.bpMax
		s.bpNextRecovery = time.Time{}
	case "AP_HEAL_FULL":
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
	default:
		return itemUseResult{}, errors.New("item cannot be used directly")
	}
	item.Num--
	s.items[itemID] = item
	return itemUseResult{
		Item: item,
		AP:   s.apStatusLocked(now),
		BP:   s.battlePointStatusLocked(now),
	}, nil
}

func (s *store) buyItemShop(lineupID int, buyNum int) ([]release.Item, error) {
	if lineupID <= 0 || buyNum <= 0 {
		return nil, errors.New("invalid item shop purchase")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var selected *release.ItemShopLineup
	for tabIndex := range s.itemShopTabs {
		for lineupIndex := range s.itemShopTabs[tabIndex].Lineup {
			lineup := &s.itemShopTabs[tabIndex].Lineup[lineupIndex]
			if lineup.LineupID == lineupID {
				configured := s.configuredItemShopLineup(*lineup)
				selected = &configured
				break
			}
		}
	}
	if selected == nil || selected.Disabled {
		return nil, &businessError{-3600, "该商品已下架，请重新选择。"}
	}
	if buyNum > selected.BuyNumMax {
		return nil, &businessError{-1, "超过单次购买数量上限。"}
	}
	if selected.PayType != 1 && selected.PayType != 3 {
		return nil, errors.New("unsupported item shop payment type")
	}
	cost := int64(selected.Price) * int64(buyNum)
	if cost <= 0 || cost > int64(math.MaxInt) {
		return nil, errors.New("item shop purchase cost is invalid")
	}
	switch selected.PayType {
	case 1:
		if cost > int64(s.gold) {
			return nil, errInsufficientGold
		}
	case 3:
		if cost > int64(s.coin)+int64(s.coinFree) {
			return nil, errInsufficientCrystals
		}
	}
	updates := make([]release.Item, 0, len(selected.Interiors))
	for _, interior := range selected.Interiors {
		if interior.BuyType != 1 {
			return nil, errors.New("unsupported item shop interior")
		}
		definition, exists := s.itemDefinitions[interior.BuyTypeID]
		if !exists {
			return nil, errors.New("unknown item shop item")
		}
		addition := int64(interior.Num) * int64(buyNum)
		current := s.items[interior.BuyTypeID]
		if addition <= 0 || int64(current.Num)+addition > int64(definition.MaxOwned) {
			return nil, errItemCapacity
		}
	}
	switch selected.PayType {
	case 1:
		s.gold -= int(cost)
	case 3:
		freeSpend := min(s.coinFree, int(cost))
		s.coinFree -= freeSpend
		s.coin -= int(cost) - freeSpend
	}
	for _, interior := range selected.Interiors {
		current := s.items[interior.BuyTypeID]
		current.ItemID = interior.BuyTypeID
		current.Num += interior.Num * buyNum
		s.items[current.ItemID] = current
		updates = append(updates, current)
	}
	sort.Slice(updates, func(left, right int) bool {
		return updates[left].ItemID < updates[right].ItemID
	})
	return updates, nil
}

type eventShopBuyResult struct {
	UseItem    release.Item
	LineupName string
	Price      int
	Shop       eventShopState
}

func (s *store) buyEventShop(lineupID int) (eventShopBuyResult, error) {
	if lineupID <= 0 {
		return eventShopBuyResult{}, errors.New("invalid event shop lineup")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var eventID int
	var pointItemID int
	var selected *release.EventShopLineupProfile
	for _, profile := range s.eventShopProfiles {
		for index := range profile.Lineups {
			if profile.Lineups[index].LineupID != lineupID {
				continue
			}
			lineup := profile.Lineups[index]
			selected = &lineup
			eventID = profile.EventID
			pointItemID = profile.PointItemID
			break
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		return eventShopBuyResult{}, errors.New("event shop lineup is unavailable from official local data")
	}
	if selected.StockNum > 0 && s.eventShopPurchases[lineupID] >= selected.StockNum {
		return eventShopBuyResult{}, &businessError{-3702, "该商品兑换次数已用完。"}
	}
	pointItem, owned := s.items[pointItemID]
	if !owned || pointItem.Num < selected.Price {
		return eventShopBuyResult{}, errInsufficientMaterials
	}
	if pointItem.LimitTime > 0 && time.Now().Unix() >= int64(pointItem.LimitTime) {
		return eventShopBuyResult{}, errItemExpired
	}
	reward := cloneReward(selected.Reward)
	if err := s.validateLocalTradeRewardLocked(reward); err != nil {
		return eventShopBuyResult{}, err
	}
	if err := s.validateLocalTradeCapacityLocked(pointItemID, selected.Price, reward); err != nil {
		return eventShopBuyResult{}, err
	}

	previousPointItem := pointItem
	pointItem.Num -= selected.Price
	s.items[pointItemID] = pointItem
	rewardResult := presentReceiveResult{}
	if err := s.applyLocalTradeRewardLocked(reward, &rewardResult); err != nil {
		s.items[pointItemID] = previousPointItem
		return eventShopBuyResult{}, err
	}
	s.eventShopPurchases[lineupID]++
	shop, err := s.eventShopStateLocked(eventID)
	if err != nil {
		return eventShopBuyResult{}, err
	}
	return eventShopBuyResult{
		UseItem:    s.items[pointItemID],
		LineupName: selected.Name,
		Price:      selected.Price,
		Shop:       shop,
	}, nil
}

type tradeShopBuyResult struct {
	LineupName string
	Shops      []tradeShopState
	Decks      []deckInfo
}

func (s *store) buyTradeShop(lineupID int, num int, uniqueIDs []int64) (tradeShopBuyResult, error) {
	if lineupID <= 0 || num <= 0 || len(uniqueIDs) != 0 {
		return tradeShopBuyResult{}, errors.New("invalid trade shop purchase")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var selected *release.TradeShopLineupProfile
	for _, shop := range s.tradeShopProfiles {
		if shop.Disabled || (shop.EndTime > 0 && time.Now().Unix() >= int64(shop.EndTime)) {
			continue
		}
		for index := range shop.Lineups {
			if shop.Lineups[index].LineupID != lineupID || shop.Lineups[index].Disabled {
				continue
			}
			lineup := shop.Lineups[index]
			selected = &lineup
			break
		}
		if selected != nil {
			break
		}
	}
	if selected == nil || len(selected.Prices) != 1 || len(selected.Rewards) == 0 {
		return tradeShopBuyResult{}, &businessError{-6301, "该兑换商品已下架，请重新打开兑换所。"}
	}
	if selected.StockNum > 0 && s.tradeShopPurchases[lineupID]+num > selected.StockNum {
		return tradeShopBuyResult{}, &businessError{-6301, "该商品兑换次数已用完。"}
	}
	price := selected.Prices[0]
	if price.Type != 4 || price.ID <= 0 || price.Num <= 0 {
		return tradeShopBuyResult{}, errors.New("unsupported trade shop price")
	}
	cost64 := int64(price.Num) * int64(num)
	if cost64 <= 0 || cost64 > int64(^uint(0)>>1) {
		return tradeShopBuyResult{}, errors.New("trade shop cost overflows")
	}
	pointItem, owned := s.items[price.ID]
	if !owned || pointItem.Num < int(cost64) {
		return tradeShopBuyResult{}, errInsufficientMaterials
	}
	if pointItem.LimitTime > 0 && time.Now().Unix() >= int64(pointItem.LimitTime) {
		return tradeShopBuyResult{}, errItemExpired
	}
	rewards := make([]release.Reward, 0, len(selected.Rewards)*num)
	for count := 0; count < num; count++ {
		for _, reward := range selected.Rewards {
			cloned := cloneReward(reward)
			if err := s.validateLocalTradeRewardLocked(cloned); err != nil {
				return tradeShopBuyResult{}, err
			}
			rewards = append(rewards, cloned)
		}
	}
	if err := s.validateRewardBatchCapacityLocked(rewards); err != nil {
		return tradeShopBuyResult{}, err
	}

	pointItem.Num -= int(cost64)
	s.items[price.ID] = pointItem
	rewardResult := presentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applyLocalTradeRewardLocked(reward, &rewardResult); err != nil {
			return tradeShopBuyResult{}, err
		}
	}
	s.tradeShopPurchases[lineupID] += num
	return tradeShopBuyResult{
		LineupName: selected.LineupName,
		Shops:      s.tradeShopStateLocked(),
		Decks:      s.rankedDecksLocked(s.decks),
	}, nil
}

// Selling and using a card as material share these ordinary availability
// rules. They do not apply to the base card being strengthened or evolved.
func (s *store) checkCardConsumptionLocked(card cardInfo) error {
	if s.uniqueIDInDeck(card.UniqueID) {
		return errCardInDeck
	}
	if card.IsLock != 0 {
		return errCardLocked
	}
	return nil
}

func (s *store) uniqueIDInDeck(uniqueID int64) bool {
	decks := s.decks
	// CN CardMgr.isUseCard considers only the selected deck before rank A.
	if s.deckRankPolicy.ConfigVersion > 0 && s.highestDeckRank < 10 {
		selected, found := selectPartnerDeck(decks, s.currentActiveArthur)
		if !found {
			return false
		}
		decks = []deckInfo{selected}
	}
	for _, deck := range decks {
		for _, candidate := range deck.CardUniqueIDs {
			if candidate == uniqueID {
				return true
			}
		}
		for _, candidate := range deck.SupportCardUniqueIDs {
			if candidate == uniqueID {
				return true
			}
		}
	}
	return false
}

func cardIndexByUniqueID(cards []cardInfo, uniqueID int64) int {
	for index, card := range cards {
		if card.UniqueID == uniqueID {
			return index
		}
	}
	return -1
}

func stackUseCount(uses []release.CardStackUse) int {
	result := 0
	for _, use := range uses {
		result += use.Num
	}
	return result
}

func validateStackUses(cards []release.CardStack, uses []release.CardStackUse) error {
	available := make(map[int]int, len(cards))
	for _, card := range cards {
		available[card.CardID] = card.Num
	}
	seen := make(map[int]struct{}, len(uses))
	for _, use := range uses {
		if use.CardID <= 0 || use.Num <= 0 {
			return errors.New("invalid stack-card selection")
		}
		if _, exists := seen[use.CardID]; exists {
			return errors.New("duplicate stack-card selection")
		}
		seen[use.CardID] = struct{}{}
		if available[use.CardID] < use.Num {
			return errInsufficientMaterials
		}
	}
	return nil
}

func consumeStackUses(cards []release.CardStack, uses []release.CardStackUse) {
	for _, use := range uses {
		for index := range cards {
			if cards[index].CardID == use.CardID {
				cards[index].Num -= use.Num
				break
			}
		}
	}
}

func sameEvolutionMaterials(selected map[int]int, expected []release.EvolutionMaterial) bool {
	if len(selected) != len(expected) {
		return false
	}
	for _, material := range expected {
		if selected[material.CardID] != material.Num {
			return false
		}
	}
	return true
}

// EvoFusionUI.SendFusionInfo sends one card ID per selected material slot,
// even when that slot requires 25 or 800 copies. The authoritative recipe
// supplies quantities; unlike exp fusion, repeated IDs are not unit counts.
func evolutionStackUses(cardIDs []int, instanceCounts map[int]int, materials []release.EvolutionMaterial) ([]release.CardStackUse, error) {
	selectedSlots := make(map[int]int, len(cardIDs))
	for _, id := range cardIDs {
		if id <= 0 {
			return nil, errors.New("invalid evolution stack material ID")
		}
		selectedSlots[id]++
	}
	uses := make([]release.CardStackUse, 0, len(selectedSlots))
	for _, material := range materials {
		slots := selectedSlots[material.CardID]
		if slots == 0 {
			continue
		}
		count := material.Num - instanceCounts[material.CardID]
		if count <= 0 || slots > count {
			return nil, errors.New("invalid evolution material slots")
		}
		uses = append(uses, release.CardStackUse{CardID: material.CardID, Num: count})
		delete(selectedSlots, material.CardID)
	}
	if len(selectedSlots) != 0 {
		return nil, errors.New("evolution stack material is not in the recipe")
	}
	return uses, nil
}

func (s *store) setDecks(incoming []deckInfo) ([]cardInfo, []deckInfo, error) {
	if len(incoming) == 0 {
		return nil, nil, errors.New("decks must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlockedRank := s.highestDeckRank
	inventory := s.rankInventoryLocked()
	for _, deck := range incoming {
		if err := s.validateDeck(deck); err != nil {
			return nil, nil, err
		}
		// The client batches the edited deck and any newly opened empty slot.
		// Compute its earned rank before checking the other slots, without
		// committing rank or decks until the whole request has been validated.
		if s.deckRankPolicy.ConfigVersion > 0 && deck.ArthurType <= 4 && s.highestDeckRank >= requiredDeckRank(deck.Index) {
			unlockedRank = max(unlockedRank, int(s.deckRankLocked(deck, inventory)))
		}
	}
	for _, deck := range incoming {
		if s.deckRankPolicy.ConfigVersion > 0 && deck.ArthurType <= 4 && unlockedRank < requiredDeckRank(deck.Index) {
			return nil, nil, errors.New("deck slot is locked by highest deck rank")
		}
	}
	for _, deck := range incoming {
		replaced := false
		for index := range s.decks {
			if s.decks[index].ArthurType == deck.ArthurType &&
				s.decks[index].Index == deck.Index {
				s.decks[index] = cloneDeck(deck)
				replaced = true
				break
			}
		}
		if !replaced {
			s.decks = append(s.decks, cloneDeck(deck))
		}
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "deck"}); err != nil {
		return nil, nil, err
	}
	s.refreshDeckRanksLocked()
	return cloneCards(s.cards), s.rankedDecksLocked(s.decks), nil
}

func (s *store) validateDeck(deck deckInfo) error {
	if deck.ArthurType < 1 || deck.ArthurType > 5 {
		return errors.New("arthur_type must be 1 through 5")
	}
	if deck.Index < 0 {
		return errors.New("idx must be non-negative")
	}
	if deck.LeaderCardIndex < 0 ||
		int(deck.LeaderCardIndex) >= s.deckSlots {
		return errors.New("leader_card_idx is outside the deck")
	}
	if len(deck.CardUniqueIDs) != s.deckSlots {
		return fmt.Errorf("card_uniqid must contain %d entries", s.deckSlots)
	}
	if len(deck.SupportCardUniqueIDs) != s.supportSlotCapacity {
		return fmt.Errorf(
			"support_card_uniqid must contain %d entries",
			s.supportSlotCapacity,
		)
	}
	if deck.ArthurType >= 1 && deck.ArthurType <= 4 {
		unlocked := int(s.supportUnlockedSlots[deck.ArthurType-1])
		for index := unlocked; index < len(deck.SupportCardUniqueIDs); index++ {
			if deck.SupportCardUniqueIDs[index] != 0 {
				return fmt.Errorf("support card slot %d is locked for Arthur %d", index+1, deck.ArthurType)
			}
		}
	}
	if len(deck.BuddyUniqueIDs) != s.buddySlots {
		return fmt.Errorf("buddy_uniqid must contain %d entries", s.buddySlots)
	}
	if deck.BuddyUniqueIDs[0] == 0 {
		for _, uniqueID := range deck.BuddyUniqueIDs[1:] {
			if uniqueID != 0 {
				return errors.New("non-zero buddy entries require a non-zero leader")
			}
		}
	}
	if err := s.validateDeckSpheres(deck); err != nil {
		return err
	}
	if err := s.validateDeckBuddies(deck); err != nil {
		return err
	}
	known := make(map[int64]struct{}, len(s.cards))
	for _, card := range s.cards {
		known[card.UniqueID] = struct{}{}
	}
	for _, uniqueID := range deck.CardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		if _, exists := known[uniqueID]; !exists {
			return fmt.Errorf("unknown card unique ID %d", uniqueID)
		}
	}
	for _, uniqueID := range deck.SupportCardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		if _, exists := known[uniqueID]; !exists {
			return fmt.Errorf("unknown support card unique ID %d", uniqueID)
		}
	}
	if err := s.validateDeckCardFamilies(deck, 0, 0); err != nil {
		return err
	}
	return nil
}

func deckContainsCardUniqueID(deck deckInfo, uniqueID int64) bool {
	for _, candidate := range deck.CardUniqueIDs {
		if candidate == uniqueID {
			return true
		}
	}
	for _, candidate := range deck.SupportCardUniqueIDs {
		if candidate == uniqueID {
			return true
		}
	}
	return false
}

// validateDeckCardFamilies mirrors the CN client's DeckUtility.IsValidDeck
// family checks. Main slots compare same_cardid, support slots compare
// same_supportcardid, and a support/main pair conflicts only when both family
// IDs match. replacementUniqueID/replacementCardID lets evolution validate
// the target definition before any inventory, gold, or material mutation.
func (s *store) validateDeckCardFamilies(
	deck deckInfo,
	replacementUniqueID int64,
	replacementCardID int,
) error {
	cardIDsByUniqueID := make(map[int64]int, len(s.cards))
	for _, card := range s.cards {
		cardIDsByUniqueID[card.UniqueID] = card.CardID
	}
	definitionFor := func(uniqueID int64) (release.Card, error) {
		cardID, exists := cardIDsByUniqueID[uniqueID]
		if !exists {
			return release.Card{}, fmt.Errorf("unknown card unique ID %d", uniqueID)
		}
		if uniqueID == replacementUniqueID {
			cardID = replacementCardID
		}
		definition, exists := s.cardDefinitions[cardID]
		if !exists {
			return release.Card{}, fmt.Errorf("card definition %d is unavailable", cardID)
		}
		return definition, nil
	}
	collect := func(uniqueIDs []int64) ([]release.Card, error) {
		definitions := make([]release.Card, 0, len(uniqueIDs))
		for _, uniqueID := range uniqueIDs {
			if uniqueID == 0 {
				continue
			}
			definition, err := definitionFor(uniqueID)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
		return definitions, nil
	}
	mainCards, err := collect(deck.CardUniqueIDs)
	if err != nil {
		return err
	}
	supportCards, err := collect(deck.SupportCardUniqueIDs)
	if err != nil {
		return err
	}
	for left := range mainCards {
		for right := left + 1; right < len(mainCards); right++ {
			if mainCards[left].SameCardID == mainCards[right].SameCardID {
				return fmt.Errorf("main cards %d and %d share family %d",
					mainCards[left].CardID, mainCards[right].CardID, mainCards[left].SameCardID)
			}
		}
	}
	for left := range supportCards {
		for right := left + 1; right < len(supportCards); right++ {
			if supportCards[left].SameSupportCardID == supportCards[right].SameSupportCardID {
				return fmt.Errorf("support cards %d and %d share family %d",
					supportCards[left].CardID, supportCards[right].CardID,
					supportCards[left].SameSupportCardID)
			}
		}
	}
	for _, support := range supportCards {
		for _, main := range mainCards {
			if support.SameCardID == main.SameCardID &&
				support.SameSupportCardID == main.SameSupportCardID {
				return fmt.Errorf("support card %d conflicts with main card %d in family %d/%d",
					support.CardID, main.CardID, support.SameCardID, support.SameSupportCardID)
			}
		}
	}
	return nil
}

func cloneCards(cards []cardInfo) []cardInfo {
	result := append([]cardInfo(nil), cards...)
	for index := range result {
		result[index] = cloneCard(result[index])
	}
	return result
}

func cloneCard(card cardInfo) cardInfo {
	card.SkillLevels = append([]int16(nil), card.SkillLevels...)
	return card
}

func cloneReward(reward release.Reward) release.Reward {
	reward.CardSkillLevels = slices.Clone(reward.CardSkillLevels)
	return reward
}

func clonePresent(present release.Present) release.Present {
	present.Reward = cloneReward(present.Reward)
	present.Reward0 = cloneReward(present.Reward0)
	present.Reward1 = cloneReward(present.Reward1)
	present.Reward2 = cloneReward(present.Reward2)
	return present
}

func clonePresents(presents []release.Present) []release.Present {
	result := make([]release.Present, len(presents))
	for index, present := range presents {
		result[index] = clonePresent(present)
	}
	return result
}

func cloneMissionInfo(info release.MissionInfo) release.MissionInfo {
	info.Rewards = append([]release.Reward(nil), info.Rewards...)
	for index := range info.Rewards {
		info.Rewards[index] = cloneReward(info.Rewards[index])
	}
	return info
}

func cloneMissions(missions []release.Mission) []release.Mission {
	result := make([]release.Mission, len(missions))
	for index, mission := range missions {
		result[index] = release.Mission{
			Info:          cloneMissionInfo(mission.Info),
			RewardPresent: clonePresent(mission.RewardPresent),
		}
	}
	return result
}

func cloneItemShopTabs(source []release.ItemShopTab) []release.ItemShopTab {
	result := make([]release.ItemShopTab, len(source))
	for tabIndex, tab := range source {
		result[tabIndex].TabType = tab.TabType
		result[tabIndex].Lineup = make([]release.ItemShopLineup, len(tab.Lineup))
		for lineupIndex, lineup := range tab.Lineup {
			result[tabIndex].Lineup[lineupIndex] = lineup
			result[tabIndex].Lineup[lineupIndex].Interiors = append(
				[]release.ItemShopInterior(nil),
				lineup.Interiors...,
			)
		}
	}
	return result
}

func cloneEventShopProfile(source release.EventShopProfile) release.EventShopProfile {
	result := source
	result.Lineups = make([]release.EventShopLineupProfile, len(source.Lineups))
	for index, lineup := range source.Lineups {
		result.Lineups[index] = lineup
		result.Lineups[index].Reward = cloneReward(lineup.Reward)
	}
	return result
}

func cloneTradeShopProfile(source release.TradeShopProfile) release.TradeShopProfile {
	result := source
	result.Lineups = make([]release.TradeShopLineupProfile, len(source.Lineups))
	for lineupIndex, lineup := range source.Lineups {
		result.Lineups[lineupIndex] = lineup
		result.Lineups[lineupIndex].Prices = make([]release.TradeShopPointProfile, len(lineup.Prices))
		for priceIndex, price := range lineup.Prices {
			result.Lineups[lineupIndex].Prices[priceIndex] = price
			result.Lineups[lineupIndex].Prices[priceIndex].PointCardCondition = make(
				[]release.TradeShopPointCardCondition,
				len(price.PointCardCondition),
			)
			copy(result.Lineups[lineupIndex].Prices[priceIndex].PointCardCondition, price.PointCardCondition)
		}
		result.Lineups[lineupIndex].Rewards = cloneRewards(lineup.Rewards)
	}
	return result
}

func cloneGachaProfiles(source []release.GachaProfile) []release.GachaProfile {
	return release.CloneGachas(source)
}

func cloneDecks(decks []deckInfo) []deckInfo {
	result := make([]deckInfo, len(decks))
	for index, deck := range decks {
		result[index] = cloneDeck(deck)
	}
	return result
}

func cloneDeck(deck deckInfo) deckInfo {
	deck.CardUniqueIDs = append([]int64(nil), deck.CardUniqueIDs...)
	deck.SupportCardUniqueIDs = append(
		[]int64(nil),
		deck.SupportCardUniqueIDs...,
	)
	deck.SphereUniqueIDs = fixedInt64Slots(deck.SphereUniqueIDs, deckSphereSlots)
	deck.BuddyUniqueIDs = fixedInt64Slots(deck.BuddyUniqueIDs, deckBuddySlots)
	return deck
}

func fixedInt64Slots(values []int64, size int) []int64 {
	result := make([]int64, size)
	copy(result, values)
	return result
}
