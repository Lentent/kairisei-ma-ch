package game

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

type TeamBattleContext struct {
	Seed                      int
	DropPlanSet               bool
	DropPlan                  []gamestate.TeamBattleEnemyDrop
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
	FameRewardsSet            bool
	FameRewards               []gamestate.Reward
	FameSources               []TeamBattleFameSource
	HostBonusArthurType       int
	FriendPointPartners       int
	FriendPointReward         int
	FriendPointRentalCredits  []FriendPointRentalCredit
	FriendPointRentalEventKey string
	SelectedPartners          []TeamBattleResultPartner
	ContinueAllowed           bool
	ContinueReceipts          []string
}

// A single-player battle has no continuously connected transport. This is the
// last known unsettled state, not proof that the client is still running.
func (s *Account) HasUnsettledSoloBattle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (s.activeBattle != nil && s.activeBattle.PrepaidRoomID == 0) ||
		(s.pvp.ActiveMatch != nil && !s.pvp.ActiveMatch.Retired && s.pvp.ActiveMatch.CompletedAtUnix == 0)
}

type TeamBattleResultPartner struct {
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

func teamBattleContextFromRelease(source *gamestate.TeamBattleActiveState) *TeamBattleContext {
	if source == nil {
		return nil
	}
	result := &TeamBattleContext{
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
		FameRewardsSet:            source.FameRewardsSet,
		FameRewards:               cloneRewards(source.FameRewards),
		HostBonusArthurType:       source.HostBonusArthurType,
		FriendPointPartners:       source.FriendPointPartners,
		FriendPointReward:         source.FriendPointReward,
		FriendPointRentalEventKey: source.FriendPointRentalEventKey,
		ContinueAllowed:           source.ContinueAllowed,
		ContinueReceipts:          append([]string(nil), source.ContinueReceipts...),
	}
	result.FameSources = make([]TeamBattleFameSource, len(source.FameSources))
	for index, fame := range source.FameSources {
		result.FameSources[index] = TeamBattleFameSource{
			ArthurType: fame.ArthurType, LeaderFame: fame.LeaderFame,
		}
	}
	result.FriendPointRentalCredits = make([]FriendPointRentalCredit, len(source.FriendPointRentalCredits))
	for index, credit := range source.FriendPointRentalCredits {
		result.FriendPointRentalCredits[index] = FriendPointRentalCredit{
			OwnerUserID: credit.OwnerUserID, FriendPoint: credit.FriendPoint,
		}
	}
	result.SelectedPartners = make([]TeamBattleResultPartner, len(source.SelectedPartners))
	for index, partner := range source.SelectedPartners {
		result.SelectedPartners[index] = TeamBattleResultPartner{
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

func snapshotTeamBattleContext(source *TeamBattleContext) *gamestate.TeamBattleActiveState {
	if source == nil {
		return nil
	}
	result := &gamestate.TeamBattleActiveState{
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
		FameRewardsSet:            source.FameRewardsSet,
		FameRewards:               cloneRewards(source.FameRewards),
		HostBonusArthurType:       source.HostBonusArthurType,
		FriendPointPartners:       source.FriendPointPartners,
		FriendPointReward:         source.FriendPointReward,
		FriendPointRentalEventKey: source.FriendPointRentalEventKey,
		ContinueAllowed:           source.ContinueAllowed,
		ContinueReceipts:          append([]string(nil), source.ContinueReceipts...),
	}
	result.FameSources = make([]gamestate.TeamBattleFameSourceState, len(source.FameSources))
	for index, fame := range source.FameSources {
		result.FameSources[index] = gamestate.TeamBattleFameSourceState{
			ArthurType: fame.ArthurType, LeaderFame: fame.LeaderFame,
		}
	}
	result.FriendPointRentalCredits = make(
		[]gamestate.TeamBattleRentalCreditState, len(source.FriendPointRentalCredits),
	)
	for index, credit := range source.FriendPointRentalCredits {
		result.FriendPointRentalCredits[index] = gamestate.TeamBattleRentalCreditState{
			OwnerUserID: credit.OwnerUserID, FriendPoint: credit.FriendPoint,
		}
	}
	result.SelectedPartners = make([]gamestate.TeamBattleResultPartnerState, len(source.SelectedPartners))
	for index, partner := range source.SelectedPartners {
		result.SelectedPartners[index] = gamestate.TeamBattleResultPartnerState{
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

type TeamBattleFameSource struct {
	ArthurType int
	LeaderFame int
}

type TeamBattleFameAward struct {
	ArthurType int
	RewardKind int
	Result     PresentReceiveResult
}

type teamBattleFameAwardPlan struct {
	ArthurType int
	RewardKind int
	Reward     gamestate.Reward
}

type teamBattleSettlement struct {
	Score         PresentReceiveResult
	ScoreInfo     []any
	Context       TeamBattleContext
	Result        PresentReceiveResult
	FirstClear    PresentReceiveResult
	Fame          []TeamBattleFameAward
	Host          []TeamBattleFameAward
	WasFirstClear bool
}

func (s *Account) StageQuestPayload(
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

func (s *Account) ClearPendingStageQuest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.pendingStageAreaID != 0
	s.pendingStageAreaID = 0
	return changed
}

func (s *Account) validateActiveTeamBattleContextLocked(
	context TeamBattleContext,
	profiles []gamestate.TeamBattleRewardProfile,
) error {
	if context.BossID <= 0 || len(context.BattleEnemyTypes) == 0 ||
		len(context.BattleEnemyTypes) > MaxTeamBattleSegments {
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
	if friendPointPolicy.OtherPerPartner <= 0 ||
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
		(len(context.SelectedPartners) != 3 || RentalPartnerCount(context.SelectedPartners) != context.FriendPointPartners) {
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
			bpUse, found := TeamBattleSoloBossBPUse(s.teamBattleSolo, context.BossID)
			if !found || bpUse != context.BPUse || context.BPUse > s.bpMax {
				return errors.New("local team battle BP cost is invalid")
			}
		} else if !context.ConsumesBattlePoints && context.BPUse != 0 {
			return errors.New("local non-host team battle must not consume battle points")
		}
	}
	if _, found := TeamBattleRewardProfileForContext(profiles, context); !found {
		return errors.New("local team battle reward profile is unavailable")
	}
	return nil
}

func (s *Account) BeginTeamBattle(
	bossID int,
	battleEnemyTypes []int8,
	standaloneBPUse int,
	consumeBattlePoints bool,
	profiles []gamestate.TeamBattleRewardProfile,
	fameSeed string,
	fameSources []TeamBattleFameSource,
	hostBonusArthurType int,
	friendPointPartners int,
	friendPointReward int,
	friendPointRentalCredits []FriendPointRentalCredit,
	friendPointRentalEventKey string,
	selectedPartners []TeamBattleResultPartner,
	prepaidRoomID ...int64,
) (TeamBattleContext, BattlePointStatus, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.refreshBattlePointsLocked(now)
	if s.activeBattle != nil {
		// Retrying the same start or completed room must not debit again.
		if s.activeBattle.FameSeed == fameSeed && s.activeBattle.BossID == bossID {
			return *teamBattleContextFromRelease(snapshotTeamBattleContext(s.activeBattle)), s.battlePointStatusLocked(now), true, nil
		}
		// An explicit new solo start replaces an abandoned solo run only after
		// all validation/payment below succeeds. Keep multiplayer settlement
		// retries separate from this single-client lifecycle.
		if !strings.HasPrefix(fameSeed, "solo:") || !strings.HasPrefix(s.activeBattle.FameSeed, "solo:") {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("a local team battle is already active")
		}
	}
	if len(battleEnemyTypes) == 0 || len(battleEnemyTypes) > MaxTeamBattleSegments {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle segment contract is unavailable")
	}
	for _, enemyType := range battleEnemyTypes {
		if enemyType < 0 || enemyType > 4 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle segment enemy type is invalid")
		}
	}
	if strings.TrimSpace(fameSeed) == "" || len(fameSources) == 0 {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle fame source is unavailable")
	}
	seenFameArthurTypes := make(map[int]struct{}, len(fameSources))
	for _, source := range fameSources {
		if source.ArthurType < 1 || source.ArthurType > 4 || source.LeaderFame <= 0 ||
			source.LeaderFame > 100 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle fame source is invalid")
		}
		if _, duplicate := seenFameArthurTypes[source.ArthurType]; duplicate {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle fame source is duplicated")
		}
		seenFameArthurTypes[source.ArthurType] = struct{}{}
	}
	if hostBonusArthurType < 0 || hostBonusArthurType > 4 {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle host-bonus Arthur type is invalid")
	}
	if hostBonusArthurType != 0 {
		if _, exists := seenFameArthurTypes[hostBonusArthurType]; !exists {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle host-bonus Arthur is unavailable")
		}
		if !consumeBattlePoints {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle host must consume battle points")
		}
	}
	friendPointPolicy := s.playerProgression.Friends.HelperReward
	if friendPointPolicy.OtherPerPartner <= 0 ||
		friendPointPolicy.FriendPerPartner <= friendPointPolicy.OtherPerPartner ||
		friendPointPolicy.MaximumPartners != 3 ||
		friendPointPartners < 0 || friendPointPartners > friendPointPolicy.MaximumPartners {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local friend-point helper policy is invalid")
	}
	minimumReward := friendPointPolicy.OtherPerPartner * friendPointPartners
	maximumReward := friendPointPolicy.FriendPerPartner * friendPointPartners
	if friendPointReward < minimumReward || friendPointReward > maximumReward {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local friend-point helper reward is invalid")
	}
	seenRentalOwners := make(map[int]struct{}, len(friendPointRentalCredits))
	rentalReward := 0
	for _, credit := range friendPointRentalCredits {
		if credit.OwnerUserID <= 0 || len(friendPointRentalCredits) > friendPointPartners ||
			(credit.FriendPoint != friendPointPolicy.OtherPerPartner &&
				credit.FriendPoint != friendPointPolicy.FriendPerPartner) {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local friend-point rental owner is invalid")
		}
		if _, duplicate := seenRentalOwners[credit.OwnerUserID]; duplicate {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local friend-point rental owner is duplicated")
		}
		seenRentalOwners[credit.OwnerUserID] = struct{}{}
		rentalReward += credit.FriendPoint
	}
	if rentalReward > friendPointReward ||
		(len(friendPointRentalCredits) == 0) != (strings.TrimSpace(friendPointRentalEventKey) == "") {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local friend-point rental event key is invalid")
	}
	if len(selectedPartners) != 0 && (len(selectedPartners) != 3 || RentalPartnerCount(selectedPartners) != friendPointPartners) {
		return TeamBattleContext{}, BattlePointStatus{}, false,
			errors.New("local team battle selected-partner projection is invalid")
	}
	selectedPartnerCopies := make([]TeamBattleResultPartner, len(selectedPartners))
	for index, partner := range selectedPartners {
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 ||
			partner.Level <= 0 || partner.LeaderCardID <= 0 || partner.LeaderLevel <= 0 || partner.LeaderFame <= 0 {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("local team battle selected partner is invalid")
		}
		partner.HonorIDs = append([]int(nil), partner.HonorIDs...)
		selectedPartnerCopies[index] = partner
	}
	context := TeamBattleContext{
		Seed:   NewBattleSeed(),
		BossID: bossID, BattleEnemyTypes: append([]int8(nil), battleEnemyTypes...),
		BPUse: standaloneBPUse, ConsumesBattlePoints: consumeBattlePoints,
		FameSeed: fameSeed, FameSources: append([]TeamBattleFameSource(nil), fameSources...),
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
		if !found || !rules.AllowsSolo() {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("solo battle entry is unavailable")
		}
		if rules.OnlyMyDeck != 0 && (len(selectedPartners) != 3 || RentalPartnerCount(selectedPartners) != 0) {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("this battle requires the player's own decks")
		}
		// Keep the accepted rule with the entry debit and in-memory battle state.
		// Publishing a new schedule or rule must not change an ongoing battle.
		context.ContinueAllowed = rules.Continue != 0
	}
	if s.pendingStageAreaID != 0 {
		stageQuest, exists := s.stageQuests[s.pendingStageAreaID]
		if !exists {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected StageQuest area is unavailable")
		}
		published, err := projectCNStageQuestPublication(stageQuest)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		stageID, bpUse, found, err := releasedStageQuestBattleForBoss(
			published,
			s.pendingStageAreaID,
			bossID,
		)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		if !found {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected StageQuest boss is unavailable")
		}
		context.StageQuestAreaID = s.pendingStageAreaID
		context.StageQuestStageID = stageID
		context.BPUse = bpUse
	} else if areaID := bossID / 100; areaID >= cnNormalQuestAreaMin && areaID <= cnNormalQuestAreaMax {
		stageQuest, exists := s.stageQuests[areaID]
		if !exists {
			return TeamBattleContext{}, BattlePointStatus{}, false,
				errors.New("selected normal quest area is unavailable")
		}
		published, err := projectCNStageQuestPublication(stageQuest)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		stageID, bpUse, found, err := releasedStageQuestBattleForBoss(
			published, areaID, bossID,
		)
		if err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
		if !found {
			return TeamBattleContext{}, BattlePointStatus{}, false,
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
			return TeamBattleContext{}, BattlePointStatus{}, false, errors.New("multiplayer prepaid start receipt is unavailable")
		}
		context.BPUse = bpUse
	}
	if err := s.validateActiveTeamBattleContextLocked(context, profiles); err != nil {
		return TeamBattleContext{}, BattlePointStatus{}, false, err
	}
	profile, _ := TeamBattleRewardProfileForContext(profiles, context)
	plan, err := PlanTeamBattleDrops(profile, context.BattleEnemyTypes, context.FameSeed)
	if err != nil {
		return TeamBattleContext{}, BattlePointStatus{}, false, err
	}
	context.DropPlanSet, context.DropPlan = true, plan
	context.FameRewardsSet, context.FameRewards = true, TeamBattleFamePool(profile, s.teamBattleFameBonus)
	for _, reward := range context.FameRewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
		}
	}
	for _, drop := range plan {
		if err := s.validateRewardLocked(drop.Reward); err != nil {
			return TeamBattleContext{}, BattlePointStatus{}, false, err
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

func (s *Account) AbandonSoloBattle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle != nil && strings.HasPrefix(s.activeBattle.FameSeed, "solo:") {
		s.activeBattle = nil
	}
}

func (s *Account) ActiveTeamBattleEnemyTypes(bossID int) ([]int8, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID || len(s.activeBattle.BattleEnemyTypes) == 0 {
		return nil, false
	}
	return append([]int8(nil), s.activeBattle.BattleEnemyTypes...), true
}

func (s *Account) towerQuestBattleForBossLocked(
	bossID int,
) (gamestate.TowerQuestProfile, gamestate.TowerQuestFloorProfile, bool) {
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
	return gamestate.TowerQuestProfile{}, gamestate.TowerQuestFloorProfile{}, false
}

func (s *Account) TowerQuestHasBoss(bossID int) bool {
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
func (s *Account) ContinueTeamBattle(report TeamBattleContinueReport, base gamestate.State, persist StatePersister) (int, int, error) {
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

func (s *Account) planTeamBattleFameAwardsLocked(
	profile gamestate.TeamBattleRewardProfile,
	context TeamBattleContext,
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
	pool := context.FameRewards
	if !context.FameRewardsSet {
		pool = TeamBattleFamePool(profile, policy)
	}
	if len(pool) == 0 {
		// The local onboarding/training profile has no inventory-bearing drop.
		// Keeping that one route fame-empty is safer than inventing a second pool.
		return nil, nil
	}
	sources := append([]TeamBattleFameSource(nil), context.FameSources...)
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
				Reward:     teamBattleFameReward(pool, context.FameSeed, source.ArthurType, 0),
			})
		}
		if effectiveFame >= policy.FullFameThreshold {
			for rewardIndex := 1; rewardIndex < policy.FullFameRewardCount; rewardIndex++ {
				plans = append(plans, teamBattleFameAwardPlan{
					ArthurType: source.ArthurType,
					RewardKind: 1,
					Reward:     teamBattleFameReward(pool, context.FameSeed, source.ArthurType, rewardIndex),
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

func (s *Account) planTeamBattleHostAwardsLocked(
	profile gamestate.TeamBattleRewardProfile,
	context TeamBattleContext,
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

func (s *Account) CompleteTeamBattle(
	bossID int,
	isClear bool,
	profiles []gamestate.TeamBattleRewardProfile,
	dropReports ...TeamBattleDropReport,
) (teamBattleSettlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID {
		return teamBattleSettlement{}, errors.New("local team battle result has no matching start")
	}
	context := *s.activeBattle
	profile, found := TeamBattleRewardProfileForContext(profiles, context)
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
	report := TeamBattleDropReport{}
	if len(dropReports) > 0 {
		report = dropReports[0]
	}
	if context.DropPlanSet && !report.Authoritative && len(report.EnemyDeadBits) != len(context.BattleEnemyTypes) {
		return teamBattleSettlement{}, errors.New("team battle drop report does not match its started plan")
	}
	profile.ResultRewards = SettledTeamBattleRewards(profile, context, report)
	if report.FameRewardsSet {
		context.FameRewardsSet, context.FameRewards = true, report.FameRewards
	}
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
	rewards := append([]gamestate.Reward(nil), profile.ResultRewards...)
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
	helperReward := gamestate.Reward{}
	if context.FriendPointReward > 0 {
		helperReward = gamestate.Reward{
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
		award := TeamBattleFameAward{
			ArthurType: plan.ArthurType,
			RewardKind: plan.RewardKind,
		}
		if err := s.applySettlementRewardLocked(plan.Reward, &award.Result); err != nil {
			return teamBattleSettlement{}, err
		}
		settlement.Fame = append(settlement.Fame, award)
	}
	for _, plan := range hostPlans {
		award := TeamBattleFameAward{
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

func (s *Account) settleTowerQuestLocked(context TeamBattleContext, isClear bool) error {
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

func (s *Account) TeamBattleSoloState() json.RawMessage {
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
		return filterBattleCatalog(append(json.RawMessage(nil), s.teamBattleSolo...), s.disabledTeamBattleBossIDs)
	}
	return filterBattleCatalog(projected, s.disabledTeamBattleBossIDs)
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
	Item   gamestate.Item
}

func (s *Account) ExecUserBuff(userBuffID int, now time.Time) (userBuffExecResult, error) {
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
		return userBuffExecResult{}, ErrInsufficientMaterials
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

func (s *Account) TeamBattleSchedulePushState(isSolo int) []int {
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

func (s *Account) ToggleTeamBattleSchedulePush(isSolo int, groupID int) bool {
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

func (s *Account) TeamBattleResultReceipt(roomID int64) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.teamBattleReceipts[roomID]
	if !exists {
		return nil, false
	}
	return append(json.RawMessage(nil), receipt.Response...), true
}

func (s *Account) RecordTeamBattleResultReceipt(roomID int64, response json.RawMessage, claimedAt time.Time) error {
	if roomID <= 0 || claimedAt.IsZero() || len(response) == 0 || len(response) > MaxRequestBytes || !json.Valid(response) {
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
	s.teamBattleReceipts[roomID] = gamestate.TeamBattleResultReceipt{
		RoomID:        roomID,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}

func (s *Account) TeamBattleSoloResultReceipt(requestSHA256 string, bossID int) (json.RawMessage, bool) {
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

func (s *Account) RecordTeamBattleSoloResultReceipt(
	requestSHA256 string,
	bossID int,
	response json.RawMessage,
	claimedAt time.Time,
) error {
	if !isLowerSHA256Digest(requestSHA256) || bossID <= 0 || claimedAt.IsZero() ||
		len(response) == 0 || len(response) > MaxRequestBytes || !json.Valid(response) {
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
	s.teamBattleSoloReceipts[requestSHA256] = gamestate.TeamBattleSoloResultReceipt{
		RequestSHA256: requestSHA256,
		BossID:        bossID,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}
