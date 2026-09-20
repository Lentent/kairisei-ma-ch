package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

type teamBattleRoomIssue struct {
	BossID              int
	DeckArthurType      int8
	DeckArthurTypeIndex int8
	RoomType            int
	Password            string
	NeedDeckRank        int
	NeedHP              int
	NeedFame            int
	Comment             string
	Flag                int
	AllowToLeave        int
	GameStartMemberNum  int
	AutoStart           bool
}

func (a *API) teamBattleMultiShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ActiveArthurType int8 `json:"active_arthur_type"`
	}
	if err := decodeExact(request, []string{"active_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.ActiveArthurType < 1 || payload.ActiveArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid active Arthur type")
		return
	}
	groups, err := teamBattleMultiGroups(a.account.TeamBattleSoloState())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, groups)
}

func (a *API) teamBattleMultiRoomCreate(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil || a.battleSV.Host == "" || a.battleSV.Port == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		BossID               int    `json:"bossid"`
		DeckArthurType       int8   `json:"deck_arthur_type"`
		DeckArthurTypeIndex  int8   `json:"deck_arthur_type_idx"`
		RoomType             int8   `json:"room_type"`
		Password             string `json:"pass"`
		NeedDeckRank         int    `json:"deck_rank"`
		NeedHP               int    `json:"hp"`
		NeedFame             int    `json:"fame"`
		Comment              string `json:"comment"`
		Flag                 int    `json:"flag"`
		AllowToLeave         int8   `json:"allow_to_leave"`
		UsePunishedFreePoint int8   `json:"use_punished_free_piont"`
	}
	fields := []string{
		"bossid", "deck_arthur_type", "deck_arthur_type_idx", "room_type", "pass",
		"deck_rank", "hp", "fame", "comment", "flag", "allow_to_leave", "use_punished_free_piont",
	}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.BossID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || payload.RoomType < 0 || payload.RoomType > 1 ||
		payload.NeedDeckRank < 0 || payload.NeedHP < 0 || payload.NeedFame < 0 ||
		(payload.AllowToLeave != 0 && payload.AllowToLeave != 1) ||
		(payload.UsePunishedFreePoint != 0 && payload.UsePunishedFreePoint != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room configuration")
		return
	}
	credential, err := a.issueTeamBattleRoom(teamBattleRoomIssue{
		BossID: payload.BossID, DeckArthurType: payload.DeckArthurType,
		DeckArthurTypeIndex: payload.DeckArthurTypeIndex, RoomType: int(payload.RoomType),
		Password: payload.Password, NeedDeckRank: payload.NeedDeckRank, NeedHP: payload.NeedHP,
		NeedFame: payload.NeedFame, Comment: payload.Comment, Flag: payload.Flag,
		// Ordinary co-op requires two real players; the remaining jobs use
		// the owner's fallback decks. The client uses this same button gate.
		AllowToLeave: int(payload.AllowToLeave), GameStartMemberNum: 2,
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"battlesv":                   a.battleSVWire(credential),
		"is_unlimited_free_continue": 0,
		"unlimited_free_continue_lv": 0,
		"free_continue_remain":       0,
	})
}

func (a *API) teamBattleAIRoomCreate(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil || a.battleSV.Host == "" || a.battleSV.Port == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		BossID               int  `json:"bossid"`
		AIID                 int  `json:"ai_id"`
		DeckArthurType       int8 `json:"deck_arthur_type"`
		DeckArthurTypeIndex  int8 `json:"deck_arthur_type_idx"`
		UsePunishedFreePoint int8 `json:"use_punished_free_piont"`
	}
	fields := []string{"bossid", "ai_id", "deck_arthur_type", "deck_arthur_type_idx", "use_punished_free_piont"}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.BossID <= 0 || payload.AIID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || (payload.UsePunishedFreePoint != 0 && payload.UsePunishedFreePoint != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local AI room configuration")
		return
	}
	if payload.AIID != localAIUserID(a.initialState.User.UserID, payload.DeckArthurType) {
		writeError(writer, http.StatusBadRequest, "unknown local AI room owner")
		return
	}
	credential, err := a.issueTeamBattleRoom(teamBattleRoomIssue{
		BossID: payload.BossID, DeckArthurType: payload.DeckArthurType,
		DeckArthurTypeIndex: payload.DeckArthurTypeIndex, RoomType: 0,
		AllowToLeave: 1, GameStartMemberNum: 1, AutoStart: true,
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"battlesv":                   a.battleSVWire(credential),
		"is_unlimited_free_continue": 0,
		"unlimited_free_continue_lv": 0,
		"free_continue_remain":       0,
	})
}

func (a *API) issueTeamBattleRoom(issue teamBattleRoomIssue) (multiplayer.Credential, error) {
	teamBattleSolo := a.account.TeamBattleSoloState()
	var groupID int
	var bossGroup any
	var found bool
	switch issue.Flag {
	case 0:
		if !teamBattleSoloBossAccessible(teamBattleSolo, issue.BossID, time.Now().Unix()) {
			return multiplayer.Credential{}, errors.New("team battle boss is locked")
		}
		groupID, bossGroup, found = game.TeamBattleGroupForBoss(teamBattleSolo, issue.BossID)
	case 1, 2:
		groupID, bossGroup, found = teamBattlePastBossGroupForBoss(
			a.initialState.TeamBattlePastBossGroups, issue.BossID,
		)
	default:
		return multiplayer.Credential{}, errors.New("invalid team battle payment flag")
	}
	if !found {
		return multiplayer.Credential{}, errors.New("unknown team battle boss")
	}
	rules, found := game.TeamBattleGroupEntryRules(bossGroup, issue.BossID)
	if !found || !rules.AllowsMultiplayer() {
		return multiplayer.Credential{}, errors.New("this battle does not allow multiplayer rooms")
	}
	replay, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, issue.BossID)
	if !found {
		return multiplayer.Credential{}, errors.New("unknown team battle replay")
	}
	rewardProfile, found := game.TeamBattleRewardProfileForContext(
		a.initialState.TeamBattleRewards,
		game.TeamBattleContext{BossID: issue.BossID},
	)
	if !found {
		return multiplayer.Credential{}, errors.New("unknown team battle drop profile")
	}
	member, err := a.account.MultiplayerMember(a.initialState.User.UserID, issue.DeckArthurType, issue.DeckArthurTypeIndex)
	if err != nil {
		return multiplayer.Credential{}, err
	}
	fallbackParty := a.account.MultiplayerOwnerFallbackParty(a.initialState.User.UserID, issue.DeckArthurType)
	// The stock multiplayer picker uses bp_use_half; the request supplies no
	// price. Freeze the published cost in the room and debit only at start.
	bpUse := rules.BPUseHalf
	if bpUse <= 0 || bpUse > rules.BPUse {
		return multiplayer.Credential{}, errors.New("multiplayer battle point cost is unavailable")
	}
	if a.account.BattlePointState().Current < bpUse {
		return multiplayer.Credential{}, errors.New("multiplayer host battle points are insufficient")
	}
	battles, err := teamBattleReplayBattles(replay)
	if err != nil {
		return multiplayer.Credential{}, err
	}
	dropPlan, err := game.PlanTeamBattleDrops(rewardProfile, teamBattleEnemyTypes(battles), fmt.Sprintf("multi:issue:%d:%d", member.UserID, time.Now().UnixNano()))
	if err != nil {
		return multiplayer.Credential{}, err
	}
	return a.multiplayer.IssueCreate(multiplayer.RoomSpec{
		Battles:            battles,
		DropLedgerVersion:  1,
		DropPlan:           dropPlan,
		FameRewardsSet:     true,
		FameRewards:        game.TeamBattleFamePool(rewardProfile, a.initialState.TeamBattleFameBonusPolicy),
		BattlePointUse:     bpUse,
		ContinueAllowed:    rules.Continue != 0,
		BossID:             issue.BossID,
		BossGroupID:        groupID,
		EnemyPartyID:       replay.EnemyPartyID,
		EnemyType:          int(replay.EnemyType),
		RoomType:           issue.RoomType,
		Password:           issue.Password,
		NeedDeckRank:       issue.NeedDeckRank,
		NeedHP:             issue.NeedHP,
		NeedFame:           issue.NeedFame,
		Comment:            issue.Comment,
		Flag:               issue.Flag,
		AllowToLeave:       issue.AllowToLeave,
		GameStartMemberNum: issue.GameStartMemberNum,
		AutoStart:          issue.AutoStart,
		CostInitial:        replay.CostInitial,
		HoldMax:            replay.HoldMax,
		BurstGaugeInitial:  replay.BurstGaugeInitial,
		Seed:               game.NewBattleSeed(),
		Drops:              teamBattleDropPlanSpecs(dropPlan, 0),
		BossGroup:          bossGroup,
		Owner:              member,
		OwnerFallbackParty: fallbackParty,
	})
}

func (a *API) teamBattleMultiRoomEnter(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil || a.battleSV.Host == "" || a.battleSV.Port == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID               int64 `json:"roomid"`
		DeckArthurType       int8  `json:"deck_arthur_type"`
		DeckArthurTypeIndex  int8  `json:"deck_arthur_type_idx"`
		UsePunishedFreePoint int8  `json:"use_punished_free_piont"`
	}
	fields := []string{"roomid", "deck_arthur_type", "deck_arthur_type_idx", "use_punished_free_piont"}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || (payload.UsePunishedFreePoint != 0 && payload.UsePunishedFreePoint != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room entry")
		return
	}
	if err := a.authorizeMultiplayerRoom(payload.RoomID); err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, false, http.StatusForbidden)
		return
	}
	member, err := a.account.MultiplayerMember(a.initialState.User.UserID, payload.DeckArthurType, payload.DeckArthurTypeIndex)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	credential, err := a.multiplayer.IssueEnter(payload.RoomID, member)
	if err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, false, http.StatusBadRequest)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"battlesv":                   a.battleSVWire(credential),
		"is_unlimited_free_continue": 0,
		"unlimited_free_continue_lv": 0,
		"free_continue_remain":       0,
	})
}

func (a *API) teamBattleMultiRoomReserve(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID         int64 `json:"roomid"`
		DeckArthurType int8  `json:"deck_arthur_type"`
	}
	if err := decodeExact(request, []string{"roomid", "deck_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room reservation")
		return
	}
	if err := a.authorizeMultiplayerRoom(payload.RoomID); err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, true, http.StatusForbidden)
		return
	}
	limit, err := a.multiplayer.Reserve(
		payload.RoomID,
		a.initialState.User.UserID,
		int(payload.DeckArthurType),
	)
	if err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, true, http.StatusBadRequest)
		return
	}
	a.writeProtocol(writer, map[string]any{"reserve_limit_time": limit})
}

func (a *API) writeMultiplayerAvailabilityError(writer http.ResponseWriter, err error, reserving bool, fallbackStatus int) {
	code := -3208 // CN TEAMBATTLE_ROOM_NOT_FOUND.
	if errors.Is(err, multiplayer.ErrRoomArthurUnavailable) {
		// Both original reservation callbacks retry only NOT_FOUND. Entry also
		// handles ARTHUR_TYPE_FILL; retain that distinction after deck selection.
		if !reserving {
			code = -3202
		}
	} else if !errors.Is(err, multiplayer.ErrRoomUnavailable) {
		writeError(writer, fallbackStatus, err.Error())
		return
	}
	a.writeProtocolResult(writer, map[string]any{}, code, "房间或所选职业已不可用，请重新选择房间。")
}

func multiplayerRoomAllowsVisitor(roomType int, friendState int8) bool {
	return roomType != 1 || friendState == game.FriendStateFriend
}

func (a *API) authorizeMultiplayerRoom(roomID int64) error {
	room, exists := a.multiplayer.Snapshot(roomID)
	if !exists {
		return multiplayer.ErrRoomUnavailable
	}
	return a.authorizeMultiplayerRoomSnapshot(room)
}

func (a *API) authorizeMultiplayerRoomSnapshot(room multiplayer.RoomSnapshot) error {
	rules, found := a.account.TeamBattleEntryRulesForBoss(room.BossID)
	if !found || !rules.AllowsMultiplayer() {
		return multiplayer.ErrRoomUnavailable
	}
	if room.RoomType != 1 {
		return nil
	}
	ownerID := 0
	for _, member := range room.Members {
		if member.MemberType == room.OwnerMemberType {
			ownerID = member.UserID
			break
		}
	}
	states, err := a.friendPointAccountStates([]int{ownerID})
	if err != nil {
		return fmt.Errorf("load room owner friend state: %w", err)
	}
	if !multiplayerRoomAllowsVisitor(room.RoomType, states[ownerID]) {
		return errors.New("room only accepts the owner's friends")
	}
	return nil
}

func (a *API) teamBattleMultiRoomReserveCancel(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID         int64 `json:"roomid"`
		DeckArthurType int8  `json:"deck_arthur_type"`
	}
	if err := decodeExact(request, []string{"roomid", "deck_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room reservation cancellation")
		return
	}
	if err := a.multiplayer.CancelReservation(
		payload.RoomID,
		a.initialState.User.UserID,
		int(payload.DeckArthurType),
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func (a *API) teamBattleResult(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID int64 `json:"roomid"`
	}
	if err := decodeExact(request, []string{"roomid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid completed multiplayer room")
		return
	}
	a.teamBattleResultMu.Lock()
	defer a.teamBattleResultMu.Unlock()
	if cached, exists := a.account.TeamBattleResultReceipt(payload.RoomID); exists {
		a.writeProtocol(writer, cached)
		return
	}
	completed, err := a.multiplayer.SettlementFor(payload.RoomID, a.initialState.User.UserID)
	if err != nil {
		if errors.Is(err, multiplayer.ErrCompletedBattleIneligible) ||
			errors.Is(err, multiplayer.ErrCompletedBattleUnavailable) {
			// ResultMgr.onTeamBattleResult clears the pending battle report
			// before handling the native error. HTTP 409 never reaches it and
			// would make every subsequent login resubmit the same lost room.
			// A live, unfinished room or a storage failure remains retryable.
			a.writeProtocolResult(writer, map[string]any{}, -3207, "战斗记录已失效，无法结算本次奖励，请重新进入副本。")
			return
		}
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	memberUserIDs := make([]int, 0, len(completed.Members))
	for _, member := range completed.Members {
		memberUserIDs = append(memberUserIDs, member.UserID)
	}
	friendStates, err := a.friendPointAccountStates(memberUserIDs)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "load multiplayer friend states")
		return
	}
	members, membersOnline, err := multiplayerResultMembersWire(
		completed,
		a.initialState.User.UserID,
		friendStates,
	)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	fameSources, claimantFameSource, err := multiplayerFameSources(
		completed,
		a.initialState.User.UserID,
	)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	hostBonusArthurType, isRoomOwner, err := multiplayerHostBonusArthurType(
		completed,
		a.initialState.User.UserID,
		claimantFameSource,
	)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if isRoomOwner && !completed.HostCostPaid {
		writeError(writer, http.StatusConflict, "multiplayer start payment is unavailable")
		return
	}
	multiplayerReplay, found := teamBattleReplayForBoss(
		a.initialState.TeamBattleReplays,
		completed.BossID,
	)
	if !found {
		writeError(writer, http.StatusInternalServerError, "multiplayer battle replay is unavailable")
		return
	}
	battles, err := teamBattleReplayBattles(multiplayerReplay)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	friendPointReward := 0
	for _, member := range completed.Members {
		if member.UserID == a.initialState.User.UserID {
			continue
		}
		friendPointReward += a.account.FriendPointRewardForState(friendStates[member.UserID])
	}
	if _, _, started, startErr := a.account.BeginTeamBattle(
		completed.BossID,
		teamBattleEnemyTypes(battles),
		0, // The host's exact paid cost comes from its durable start receipt.
		isRoomOwner,
		a.initialState.TeamBattleRewards,
		fmt.Sprintf("multi:room=%d", payload.RoomID),
		fameSources,
		hostBonusArthurType,
		len(completed.Members)-1,
		friendPointReward,
		nil,
		"",
		nil,
		func() int64 {
			if isRoomOwner && completed.HostCostPaid {
				return completed.RoomID
			}
			return 0
		}(),
	); startErr != nil {
		writeError(writer, http.StatusConflict, startErr.Error())
		return
	} else if !started {
		writeError(writer, http.StatusConflict, "team battle points are insufficient")
		return
	}
	settlement, err := a.account.CompleteTeamBattle(
		completed.BossID,
		true,
		a.initialState.TeamBattleRewards,
		multiplayerDropReport(completed),
	)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	resultRewards := battleResultRewardsWire(settlement.Result.Rewards)
	clearRewards := battleResultRewardsWire(settlement.FirstClear.Rewards)
	newCards := append([]game.CardInfo(nil), settlement.Result.Cards...)
	newCards = append(newCards, settlement.FirstClear.Cards...)
	newStackCards := append([]gamestate.CardStack(nil), settlement.Result.StackCards...)
	newStackCards = append(newStackCards, settlement.FirstClear.StackCards...)
	newItems := append([]gamestate.Item(nil), settlement.Result.Items...)
	newItems = append(newItems, settlement.FirstClear.Items...)
	newSpheres := append([]gamestate.Sphere(nil), settlement.Result.Spheres...)
	newSpheres = append(newSpheres, settlement.FirstClear.Spheres...)
	newBuddies := append([]gamestate.Buddy(nil), settlement.Result.Buddies...)
	newBuddies = append(newBuddies, settlement.FirstClear.Buddies...)
	for _, fameAward := range settlement.Fame {
		newCards = append(newCards, fameAward.Result.Cards...)
		newStackCards = append(newStackCards, fameAward.Result.StackCards...)
		newItems = append(newItems, fameAward.Result.Items...)
		newSpheres = append(newSpheres, fameAward.Result.Spheres...)
		newBuddies = append(newBuddies, fameAward.Result.Buddies...)
	}
	for _, hostAward := range settlement.Host {
		newCards = append(newCards, hostAward.Result.Cards...)
		newStackCards = append(newStackCards, hostAward.Result.StackCards...)
		newItems = append(newItems, hostAward.Result.Items...)
		newSpheres = append(newSpheres, hostAward.Result.Spheres...)
		newBuddies = append(newBuddies, hostAward.Result.Buddies...)
	}
	fameRewards := battleFameRewardsWire(settlement.Fame)
	hostRewards := battleHostRewardsWire(settlement.Host)
	// A permanent StageQuest route is not a suppression/world-boss settlement.
	// The original result scene interprets any element here as suppression
	// points, including a zero-valued placeholder.
	stageQuest := []any{}
	response := map[string]any{
		"user":                            a.userPayload(),
		"result_rewards":                  resultRewards,
		"clear_rewards":                   clearRewards,
		"fame_rewards":                    fameRewards,
		"host_rewards":                    hostRewards,
		"score_rewards":                   battleResultRewardsWire(settlement.Score.Rewards),
		"challenge_rewards":               []any{},
		"deck_cards":                      []any{},
		"new_cards":                       toWireCards(newCards),
		"new_stack_cards":                 toWireStackCards(newStackCards),
		"new_items":                       a.itemInfosWire(newItems),
		"new_sphrs":                       toWireSpheres(newSpheres),
		"new_buddys":                      toWireBuddies(newBuddies),
		"members":                         members,
		"members_is_online":               membersOnline,
		"is_result_reward_in_present_box": game.BoolInt(settlement.Result.InPresentBox),
		"is_clear_reward_in_present_box":  game.BoolInt(settlement.FirstClear.InPresentBox),
		"is_fame_reward_in_present_box":   battleAwardsInPresentBox(settlement.Fame),
		"is_stage_reward_in_present_box":  0,
		"is_score_reward_in_present_box":  game.BoolInt(settlement.Score.InPresentBox),
		"rookie_type":                     0,
		"unlock_notice":                   []any{},
		"bonus_fame_add":                  a.initialState.TeamBattleFameBonusPolicy.BonusFameAdd,
		"stage_quest":                     stageQuest,
		"scene_transition":                []any{},
		"auto_fusion_result":              []any{},
		"auto_loveup_result":              []any{},
		"score":                           scoreInfoWire(settlement.ScoreInfo),
		"challenge_results":               []any{},
	}
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode multiplayer settlement")
		return
	}
	if err := a.account.RecordTeamBattleResultReceipt(payload.RoomID, encodedResponse, time.Now()); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	if err := a.multiplayer.MarkSettlementClaimed(payload.RoomID, a.initialState.User.UserID); err != nil {
		a.logger.Warn(
			"persisted multiplayer settlement but could not mark in-memory claim",
			"room_id", payload.RoomID,
			"user_id", a.initialState.User.UserID,
			"error", err,
		)
	}
	a.writeProtocol(writer, json.RawMessage(encodedResponse))
}

func multiplayerResultMembersWire(
	completed multiplayer.CompletedBattle,
	userID int,
	friendStates map[int]int8,
) ([]any, []int, error) {
	if len(completed.Members) != 4 || len(completed.OnlineUserIDs) == 0 {
		return nil, nil, errors.New("completed multiplayer member projection is incomplete")
	}
	online := make(map[int]struct{}, len(completed.OnlineUserIDs))
	for _, onlineUserID := range completed.OnlineUserIDs {
		online[onlineUserID] = struct{}{}
	}
	byArthurType := make(map[int]multiplayer.Member, len(completed.Members))
	containsUser := false
	for _, member := range completed.Members {
		if member.ArthurType < 1 || member.ArthurType > 4 || len(member.DeckHonorIDs) != 4 {
			return nil, nil, errors.New("completed multiplayer member is invalid")
		}
		if _, duplicate := byArthurType[member.ArthurType]; duplicate {
			return nil, nil, errors.New("completed multiplayer Arthur type is duplicated")
		}
		byArthurType[member.ArthurType] = member
		if member.UserID == userID {
			containsUser = true
		}
	}
	if !containsUser || len(byArthurType) != 4 {
		return nil, nil, errors.New("completed multiplayer account member is unavailable")
	}
	members := make([]any, 0, 3)
	membersOnline := make([]int, 0, 3)
	for arthurType := 1; arthurType <= 4; arthurType++ {
		member := byArthurType[arthurType]
		if member.UserID == userID {
			continue
		}
		isOnline := 0
		if _, exists := online[member.UserID]; exists {
			isOnline = 1
		}
		members = append(members, map[string]any{
			"userid":            member.UserID,
			"name":              member.Name,
			"arthur_type":       member.ArthurType,
			"is_burst":          member.IsBurst,
			"lv":                member.Level,
			"deck_rank":         member.DeckRank,
			"state":             friendStates[member.UserID],
			"leader_cardid":     member.LeaderCardID,
			"leader_card_lv":    member.LeaderLevel,
			"leader_card_fame":  member.LeaderFame,
			"last_login_time":   completed.CompletedAtUnix,
			"comment":           "",
			"pvp_point":         0,
			"is_first_matching": member.IsFirstMatch,
			"rookie_type":       member.RookieType,
			"deck_honorids":     append([]int(nil), member.DeckHonorIDs...),
		})
		membersOnline = append(membersOnline, isOnline)
	}
	if len(members) != 3 || len(membersOnline) != 3 {
		return nil, nil, errors.New("completed multiplayer result requires three partner members")
	}
	return members, membersOnline, nil
}

func multiplayerFameSources(
	completed multiplayer.CompletedBattle,
	claimantUserID int,
) ([]game.TeamBattleFameSource, game.TeamBattleFameSource, error) {
	if claimantUserID <= 0 || len(completed.Members) != 4 || len(completed.OnlineUserIDs) == 0 {
		return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame projection is incomplete")
	}
	eligibleUserIDs := make(map[int]struct{}, len(completed.OnlineUserIDs))
	for _, userID := range completed.OnlineUserIDs {
		if userID <= 0 {
			return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame claimant is invalid")
		}
		eligibleUserIDs[userID] = struct{}{}
	}
	sources := make([]game.TeamBattleFameSource, 0, len(eligibleUserIDs))
	seenArthurTypes := make(map[int]struct{}, len(completed.Members))
	claimant := game.TeamBattleFameSource{}
	for _, member := range completed.Members {
		if member.ArthurType < 1 || member.ArthurType > 4 || member.LeaderFame <= 0 ||
			member.LeaderFame > 100 {
			return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame member is invalid")
		}
		if _, duplicate := seenArthurTypes[member.ArthurType]; duplicate {
			return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame Arthur type is duplicated")
		}
		seenArthurTypes[member.ArthurType] = struct{}{}
		if _, eligible := eligibleUserIDs[member.UserID]; !eligible {
			continue
		}
		source := game.TeamBattleFameSource{
			ArthurType: member.ArthurType,
			LeaderFame: member.LeaderFame,
		}
		sources = append(sources, source)
		if member.UserID == claimantUserID {
			claimant = source
		}
	}
	if len(sources) == 0 || claimant.ArthurType == 0 {
		return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame claimant is unavailable")
	}
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].ArthurType < sources[right].ArthurType
	})
	return sources, claimant, nil
}

func multiplayerHostBonusArthurType(
	completed multiplayer.CompletedBattle,
	claimantUserID int,
	claimant game.TeamBattleFameSource,
) (int, bool, error) {
	if claimantUserID <= 0 || claimant.ArthurType < 1 || claimant.ArthurType > 4 ||
		completed.OwnerMemberType < 1 || completed.OwnerMemberType > 4 || len(completed.Members) != 4 {
		return 0, false, errors.New("completed multiplayer host projection is incomplete")
	}
	for _, member := range completed.Members {
		if member.MemberType != completed.OwnerMemberType {
			continue
		}
		if member.UserID <= 0 || member.ArthurType < 1 || member.ArthurType > 4 {
			return 0, false, errors.New("completed multiplayer room owner is invalid")
		}
		if member.UserID != claimantUserID {
			return 0, false, nil
		}
		if member.ArthurType != claimant.ArthurType {
			return 0, false, errors.New("completed multiplayer room owner identity is inconsistent")
		}
		return member.ArthurType, true, nil
	}
	return 0, false, errors.New("completed multiplayer room owner is unavailable")
}

func (a *API) battleSVWire(credential multiplayer.Credential) map[string]any {
	return map[string]any{
		"host":       a.battleSV.Host,
		"port":       a.battleSV.Port,
		"nginx_host": a.battleSV.Host,
		"nginx_port": a.battleSV.Port,
		"auth_token": credential.AuthToken,
		"signature":  credential.Signature,
	}
}

func teamBattleMultiGroups(configuration json.RawMessage) (map[string]any, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, fmt.Errorf("decode team battle groups: %w", err)
	}
	result := map[string]any{"event_bg_pictid": 0}
	for key, name := range map[string]string{
		"9": "normal_groups", "10": "special_groups", "11": "key_groups", "12": "event_groups",
	} {
		var sourceGroups []any
		if err := json.Unmarshal(top[key], &sourceGroups); err != nil {
			return nil, fmt.Errorf("decode team battle %s: %w", name, err)
		}
		groups := make([]any, 0, len(sourceGroups))
		for _, sourceGroup := range sourceGroups {
			group, err := teamBattleBossGroupNamedDTO(sourceGroup)
			if err != nil {
				return nil, fmt.Errorf("adapt team battle %s: %w", name, err)
			}
			groups = append(groups, group)
		}
		result[name] = groups
	}
	return result, nil
}

func nextAIArthurType(selectedArthurType int8) int8 {
	if selectedArthurType < 1 || selectedArthurType > 4 {
		return 1
	}
	return selectedArthurType%4 + 1
}

func localAIUserID(accountUserID int, selectedArthurType int8) int {
	return accountUserID*10 + int(nextAIArthurType(selectedArthurType))
}

func teamBattlePastBossHasBoss(groups []json.RawMessage, bossID int) bool {
	_, _, found := teamBattlePastBossGroupForBoss(groups, bossID)
	return found
}

// teamBattlePastBossGroupForBoss converts the archive-only PastBoss DTO back
// into the ordinary boss-group shape embedded in room information. The stock
// client sends flag 1 (gold) or 2 (key) when entering this archive; the local
// profile currently publishes both costs as zero, but the flag is still part
// of the original request contract.
func teamBattlePastBossGroupForBoss(groups []json.RawMessage, bossID int) (int, any, bool) {
	if bossID <= 0 {
		return 0, nil, false
	}
	for _, raw := range groups {
		var group struct {
			GroupID   int    `json:"0"`
			StageType int    `json:"1"`
			SearchID  int    `json:"2"`
			IconIndex int    `json:"3"`
			Name      string `json:"4"`
			Bosses    []struct {
				BossID int `json:"0"`
				PictID int `json:"9"`
			} `json:"13"`
		}
		if json.Unmarshal(raw, &group) != nil || group.GroupID <= 0 || len(group.Bosses) == 0 {
			continue
		}
		pictID := 0
		found := false
		for _, boss := range group.Bosses {
			if boss.BossID == bossID {
				pictID = boss.PictID
				found = true
				break
			}
		}
		if !found {
			continue
		}
		var source map[string]any
		if json.Unmarshal(raw, &source) != nil {
			return 0, nil, false
		}
		bosses, ok := source["13"].([]any)
		if !ok || len(bosses) == 0 {
			return 0, nil, false
		}
		return group.GroupID, map[string]any{
			"0": group.GroupID, "1": group.StageType, "2": group.SearchID,
			"3": group.IconIndex, "4": group.Name, "5": 0, "6": 0,
			"7": pictID, "8": 0, "9": 0, "10": bosses,
			"11": []any{}, "12": []any{}, "13": []any{}, "14": 0, "15": 0,
		}, true
	}
	return 0, nil, false
}

func teamBattleGroupForID(configuration json.RawMessage, groupID int) (any, bool) {
	if groupID <= 0 {
		return nil, false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(configuration, &top) != nil {
		return nil, false
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if json.Unmarshal(top[key], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var current int
			if json.Unmarshal(group["0"], &current) != nil || current != groupID {
				continue
			}
			encoded, err := json.Marshal(group)
			if err != nil {
				return nil, false
			}
			var wire any
			if json.Unmarshal(encoded, &wire) != nil {
				return nil, false
			}
			return wire, true
		}
	}
	return nil, false
}

func multiplayerRoomWire(
	snapshot multiplayer.RoomSnapshot,
	friendPointReward int,
	friendState int8,
) (map[string]any, error) {
	owner := snapshot.Members[0]
	for _, member := range snapshot.Members {
		if member.MemberType == snapshot.OwnerMemberType {
			owner = member
			break
		}
	}
	entryUsers := make([]any, 4)
	for index := range entryUsers {
		entryUsers[index] = map[string]any{"userid": 0, "is_burst": 0}
	}
	for _, member := range snapshot.Members {
		if member.ArthurType < 1 || member.ArthurType > len(entryUsers) {
			continue
		}
		entryUsers[member.ArthurType-1] = map[string]any{
			"userid":   member.UserID,
			"is_burst": member.IsBurst,
		}
	}
	bossGroup, err := multiplayerRoomBossGroupWire(snapshot.BossGroup, snapshot.BossID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"roomid":                 snapshot.RoomID,
		"bossid":                 snapshot.BossID,
		"owner":                  multiplayerPartnerWire(owner, friendPointReward, friendState),
		"owner_leader_card_fame": owner.LeaderFame,
		"need_deck_rank":         snapshot.NeedDeckRank,
		"need_hp":                snapshot.NeedHP,
		"need_fame":              snapshot.NeedFame,
		"comment":                snapshot.Comment,
		"boss_group":             bossGroup,
		"entry_users":            entryUsers,
		"attr_str":               "",
		"is_punished":            0,
	}, nil
}

func multiplayerRoomBossGroupWire(group any, bossID int) (any, error) {
	encoded, err := json.Marshal(group)
	if err != nil {
		return nil, fmt.Errorf("encode local multiplayer boss group: %w", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return nil, fmt.Errorf("decode local multiplayer boss group: %w", err)
	}
	named, err := teamBattleBossGroupNamedDTO(wire)
	if err != nil {
		return nil, err
	}
	bosses, ok := named["bosses"].([]any)
	if !ok || len(bosses) == 0 {
		return nil, errors.New("local multiplayer boss group has no boss")
	}
	// Both the lobby card and SetRoomIdx read bosses[0], not the room's
	// separate bossid. Reorder this detached DTO only, preserving the group.
	for index, value := range bosses {
		boss := value.(map[string]any)
		if boss["bossid"] != float64(bossID) {
			continue
		}
		copy(bosses[1:index+1], bosses[:index])
		bosses[0] = boss
		return named, nil
	}
	return nil, errors.New("local multiplayer room boss is absent from its group")
}

func teamBattleBossGroupNamedDTO(value any) (map[string]any, error) {
	source, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("team battle boss group is not an object")
	}
	result := remapNumericObject(source, []string{
		"boss_groupid", "stage_type", "searchid", "iconindex", "name",
		"appear_begin", "appear_end", "pictid", "recommended_arthur", "stage_quest_areaid",
	})
	bossValues, ok := source["10"].([]any)
	if !ok {
		return nil, errors.New("team battle boss group has no boss list")
	}
	bosses := make([]any, 0, len(bossValues))
	for _, bossValue := range bossValues {
		boss, err := teamBattleBossNamedDTO(bossValue)
		if err != nil {
			return nil, err
		}
		bosses = append(bosses, boss)
	}
	result["bosses"] = bosses

	storyValues, _ := source["11"].([]any)
	stories := make([]any, 0, len(storyValues))
	for _, storyValue := range storyValues {
		story, err := teamBattleStoryNamedDTO(storyValue)
		if err != nil {
			return nil, err
		}
		stories = append(stories, story)
	}
	result["stories"] = stories
	result["button_str1"] = arrayOrEmpty(source["12"])
	result["button_str2"] = arrayOrEmpty(source["13"])
	result["bonus_flag"] = source["14"]
	result["bonus_end_time"] = source["15"]
	return result, nil
}

func teamBattleBossNamedDTO(value any) (map[string]any, error) {
	source, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("team battle boss is not an object")
	}
	result := remapNumericObject(source, []string{
		"bossid", "is_only_my_deck", "arthur_limit", "end_turn", "difficulty",
		"bp_use", "bp_use_half", "is_continue", "bg_num", "pictid", "state", "hint",
	})
	result["reward_cardids"] = remapNumericObjectArray(source["12"], []string{"cardid", "is_new"})
	result["reward_sphrids"] = remapNumericObjectArray(source["13"], []string{"sphrid", "is_new"})
	for index, name := range []string{
		"is_model", "user_buff_id", "unlock_expire_time", "is_daily_ranking",
	} {
		result[name] = source[fmt.Sprintf("%d", index+14)]
	}
	result["awake"] = remapNumericObjectValue(source["18"], []string{
		"is_turn", "is_cost", "is_buff", "is_sphr_count", "is_deck_trash", "is_deal",
	})
	for index, name := range []string{
		"needrank", "autofight", "need_hp", "mission_related", "is_punished", "start_rule",
	} {
		result[name] = source[fmt.Sprintf("%d", index+19)]
	}
	challengeValues, _ := source["25"].([]any)
	if len(challengeValues) == 0 {
		result["challenge"] = nil
	} else {
		result["challenge"] = remapNumericObjectArray(
			challengeValues,
			[]string{"num", "state_bit", "is_new"},
		)
	}
	result["is_lock"] = source["26"]
	result["unlock"] = remapNumericObjectValue(source["27"], []string{
		"name", "difficulty", "story_tab_name", "story_sectionid",
		"story_section_title", "story_title", "story_teambattle_group_name", "story_teambattle_title",
	})
	return result, nil
}

func teamBattleStoryNamedDTO(value any) (map[string]any, error) {
	source, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("team battle story is not an object")
	}
	result := remapNumericObject(source, []string{
		"story_teambattleid", "talkid", "state_flag", "title", "is_lock",
	})
	result["unlock"] = remapNumericObjectValue(source["5"], []string{
		"name", "difficulty", "story_tab_name", "story_sectionid",
		"story_section_title", "story_title", "story_sub_title",
	})
	return result, nil
}

func remapNumericObject(source map[string]any, names []string) map[string]any {
	result := make(map[string]any, len(names))
	for index, name := range names {
		result[name] = source[fmt.Sprintf("%d", index)]
	}
	return result
}

func remapNumericObjectValue(value any, names []string) any {
	source, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return remapNumericObject(source, names)
}

func remapNumericObjectArray(value any, names []string) []any {
	values, _ := value.([]any)
	result := make([]any, 0, len(values))
	for _, item := range values {
		if source, ok := item.(map[string]any); ok {
			result = append(result, remapNumericObject(source, names))
		}
	}
	return result
}

func arrayOrEmpty(value any) []any {
	values, _ := value.([]any)
	if values == nil {
		return []any{}
	}
	return values
}

func multiplayerPartnerWire(member multiplayer.Member, friendPointReward int, friendState int8) map[string]any {
	return map[string]any{
		"userid":        member.UserID,
		"name":          member.Name,
		"lv":            member.Level,
		"arthur_type":   member.ArthurType,
		"is_burst":      member.IsBurst,
		"job_type":      member.JobType,
		"hp":            member.HP,
		"atkp":          member.Attack,
		"intp":          member.Magic,
		"mndp":          member.Mind,
		"deck_rank":     member.DeckRank,
		"leader_card":   map[string]any{"cardid": member.LeaderCardID, "lv": member.LeaderLevel, "skill_lv": []int{1, 1}, "love": 0, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0, "fame": member.LeaderFame},
		"get_fp":        friendPointReward,
		"comment":       "",
		"friend_state":  friendState,
		"rookie_type":   member.RookieType,
		"deck_name":     member.DeckName,
		"rental_idx":    0,
		"play_log_turn": 0,
		"attr_nums":     []int{0, 0, 0, 0, 0, 0},
		"kind_nums":     []int{},
		"deck_honorids": append([]int(nil), member.DeckHonorIDs...),
	}
}
