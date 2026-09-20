package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) homeShow(writer http.ResponseWriter, _ *http.Request) {
	// The original client submits its saved result before HomeShow. Reaching
	// home abandons any remaining solo context, without refunding the entry.
	a.account.AbandonSoloBattle()
	a.account.ClearBurstStory()
	now := time.Now()
	if !a.refreshPVPChallenges(writer, now) {
		return
	}
	homeBanners := []any{}
	if gachaID := a.account.HomeBannerGachaID(now); gachaID > 0 &&
		a.account.FeatureUnlocked(20) && !a.account.OnboardingInProgress() {
		homeBanners = append(homeBanners, map[string]any{
			"home_banner_type": 12,
			"image_url":        a.baseURL + "/local/home/banner.png",
			"open_url":         "gacha:" + strconv.Itoa(gachaID),
			"infomation":       []string{},
			"eventid":          0,
		})
	}
	var friendPointReward any
	if a.friendPointAccounts != nil {
		events, err := a.friendPointAccounts.ListFriendPointRentalEvents(
			a.initialState.User.UserID,
			a.account.FriendPointRentalCursor(),
		)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "load friend-point rental inbox")
			return
		}
		claimed, err := a.account.ClaimFriendPointRentalEvents(events)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "claim friend-point rental inbox")
			return
		}
		if claimed != nil {
			friendPointReward = []any{map[string]any{
				"from_user_num": claimed.FromUserNum,
				"get_fp":        claimed.FriendPoint,
			}}
		}
	}
	loginClaims := game.LoginBonusClaims{}
	if !a.account.OnboardingInProgress() {
		var err error
		loginClaims, err = a.account.ClaimLoginBonuses(now)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "claim login bonuses")
			return
		}
	}
	onboardingQuests, err := a.account.HomeOnboardingQuests()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "load onboarding quests")
		return
	}
	user := a.initialState.User
	stive, cardRemainTime := a.account.CardDevelopmentHomeState(now)
	maxRank := a.account.HomeDeckRank()
	userPayload := a.userPayload()
	medalCount := a.account.ItemCount(a.initialState.TeamBattleMedalItemID)
	bossCoinCount := a.account.ItemCount(a.initialState.BossCoinItemID)
	pvpPoint, pvpChallenge := a.account.PvpStatus()
	appearTowerID := 0
	if towerIDs := a.account.TowerQuestIDs(); len(towerIDs) > 0 {
		appearTowerID = towerIDs[0]
	}
	if !a.persistOrError(writer) {
		return
	}
	eventPage := a.initialState.EventPageProfile
	homeEvents := []any{}
	// Event 0 is the generic official art template, not a configured event.
	// Publishing it renders a blank green target banner in every open menu.
	if eventPage.EventID > 0 && int64(eventPage.EndTime) > now.Unix() {
		homeEvents = append(homeEvents, map[string]any{
			"eventid":       eventPage.EventID,
			"button_pictid": eventPage.HomeButtonPictID,
			"end_time":      eventPage.EndTime,
		})
	}
	// HOME2 is the lower native carousel. This retained event artwork belongs
	// to the configured New Year replay, not to every future event profile.
	if eventPage.EventID == 11801010 && int64(eventPage.EndTime) > now.Unix() &&
		a.account.FeatureUnlocked(13) && !a.account.OnboardingInProgress() {
		homeBanners = append(homeBanners, map[string]any{
			"home_banner_type": 13,
			"image_url":        a.baseURL + "/local/home/event-banner.png",
			"open_url":         "eventpage",
			"infomation":       []string{},
			"eventid":          0,
		})
	}
	dailyBonuses, err := projectDailyLoginBonus(loginClaims.Daily)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "project daily login bonus")
		return
	}
	beginnerBonuses, err := projectBeginnerLoginBonus(loginClaims.Beginner, loginClaims.BeginnerSchedule)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "project beginner login bonus")
		return
	}
	totalBonuses, err := projectTotalLoginBonus(loginClaims.Total, loginClaims.TotalMilestones)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "project total login bonus")
		return
	}
	homePopups := []gamestate.PopupProfile{}
	if !a.account.OnboardingInProgress() {
		homePopups = a.account.UnreadPopupState(a.initialState.PopupProfile)
	}
	cardFlags := a.account.LocalCardPayload(now)
	a.writeProtocolWithPopups(writer, map[string]any{
		"user":                      userPayload,
		"login_bonus_daily":         dailyBonuses,
		"login_bonus_beginner":      beginnerBonuses,
		"login_bonus_total":         totalBonuses,
		"login_bonus_event":         nil,
		"quests":                    onboardingQuests,
		"banners":                   homeBanners,
		"fp_reward":                 friendPointReward,
		"function_flag":             []any{},
		"function_show_state":       []any{},
		"server_time":               now.Unix(),
		"background":                user.HomeBackgroundID,
		"purchase_bonus_end_time":   0,
		"purchase_present_end_time": 0,
		// This local profile has no payment service. A non-zero flag keeps the
		// original shortage dialog on its ordinary acquisition body instead of
		// entering the packaged first-purchase promotion flow.
		"firstpay":          game.LocalShopFirstPay,
		"monthcardflag":     cardFlags["monthcardflag"],
		"forevercardflag":   cardFlags["forevercardflag"],
		"monthcardtime":     cardFlags["monthcardtime"],
		"totalcoin":         0,
		"rookie_bonus":      nil,
		"home_event":        homeEvents,
		"max_rank":          maxRank,
		"medal_num":         medalCount,
		"expired_medal_num": 0,
		"bosscoin_itemid":   a.initialState.BossCoinItemID,
		"bosscoin_num":      bossCoinCount,
		"stive":             stive,
		"card_remain_time":  cardRemainTime,
		"tutorial_flag":     a.account.TutorialState(),
		"appear_towerid":    appearTowerID,
		"pvp_point":         pvpPoint,
		"challenge":         pvpChallenge,
		"score_ranking":     []any{},
	}, homePopups)
}

func (a *API) eventShow(writer http.ResponseWriter, _ *http.Request) {
	profile := a.initialState.EventPageProfile
	a.writeProtocol(writer, map[string]any{
		"eventid":      profile.EventID,
		"bg_pictid":    profile.BackgroundPictID,
		"info_url":     profile.InfoURL,
		"update_info":  profile.UpdateInfo,
		"itemid":       append([]int{}, profile.ItemIDs...),
		"buttons":      append([]gamestate.EventPageButton{}, profile.Buttons...),
		"is_new_solo":  profile.IsNewSolo,
		"is_new_multi": profile.IsNewMulti,
	})
}

func projectLoginBonusResult(result game.PresentReceiveResult) (any, error) {
	rewards := battleResultRewardsWire(result.Rewards)
	if len(rewards) != 1 {
		return nil, errors.New("login bonus result must contain exactly one reward")
	}
	return rewards[0], nil
}

func projectDailyLoginBonus(claim *game.LoginBonusClaim) ([]any, error) {
	if claim == nil {
		return nil, nil
	}
	reward, err := projectLoginBonusResult(claim.Result)
	if err != nil {
		return nil, err
	}
	return []any{map[string]any{
		"reward":                          reward,
		"day_num":                         claim.Day.Day,
		"comment":                         claim.Day.Comment,
		"is_result_reward_in_present_box": 0,
	}}, nil
}

func projectBeginnerLoginBonus(
	claim *game.LoginBonusClaim,
	schedule []gamestate.LoginBonusDay,
) ([]any, error) {
	if claim == nil {
		return nil, nil
	}
	rewards := make([]any, len(schedule))
	for index, day := range schedule {
		result := game.PresentReceiveResult{Rewards: []game.ReceivedReward{{
			Reward: day.Reward, UniqueID: []int64{},
		}}}
		if day.Day == claim.Day.Day {
			result = claim.Result
		}
		projected, err := projectLoginBonusResult(result)
		if err != nil {
			return nil, err
		}
		rewards[index] = projected
	}
	return []any{map[string]any{
		"reward":                          rewards,
		"day_num":                         claim.Day.Day,
		"comment":                         claim.Day.Comment,
		"is_result_reward_in_present_box": 0,
	}}, nil
}

func projectTotalLoginBonus(
	claim *game.LoginBonusClaim,
	milestones []gamestate.LoginBonusDay,
) ([]any, error) {
	if claim == nil {
		return nil, nil
	}
	actual, err := projectLoginBonusResult(claim.Result)
	if err != nil {
		return nil, err
	}
	result := []any{map[string]any{
		"reward":                          actual,
		"day_num":                         claim.Day.Day,
		"comment":                         claim.Day.Comment,
		"is_result_reward_in_present_box": 0,
	}}
	for _, day := range milestones {
		preview, err := projectLoginBonusResult(game.PresentReceiveResult{Rewards: []game.ReceivedReward{{
			Reward: day.Reward, UniqueID: []int64{},
		}}})
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"reward":                          preview,
			"day_num":                         day.Day,
			"comment":                         day.Comment,
			"is_result_reward_in_present_box": 0,
		})
	}
	return result, nil
}

func (a *API) mainQuestShow(writer http.ResponseWriter, request *http.Request) {
	areaID := 0
	selectArea := request.Method == http.MethodPost
	if request.Method == http.MethodPost {
		var payload struct {
			AreaID int `json:"areaid"`
			IsSolo int `json:"is_solo"`
		}
		if err := decodeExact(request, []string{"areaid", "is_solo"}, &payload); err != nil {
			a.writeStoreError(writer, err)
			return
		}
		if payload.AreaID <= 0 || (payload.IsSolo != 0 && payload.IsSolo != 1) {
			writeError(writer, http.StatusBadRequest, "invalid local stage quest query")
			return
		}
		areaID = payload.AreaID
	}
	payload, consumedNewClear, err := a.account.StageQuestPayload(areaID, selectArea)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if (consumedNewClear || selectArea) && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, payload)
}

func (a *API) teamBattleSoloShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ActiveArthurType int8 `json:"active_arthur_type"`
	}
	if err := decodeExact(request, []string{"active_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.ActiveArthurType < 1 || payload.ActiveArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "active Arthur type must be 1 through 4")
		return
	}
	teamBattleSolo := a.account.TeamBattleSoloState()
	if len(teamBattleSolo) == 0 {
		writeError(writer, http.StatusInternalServerError, "team battle configuration is empty")
		return
	}
	bp := a.account.BattlePointState()
	medalCount := a.account.ItemCount(a.initialState.TeamBattleMedalItemID)
	teamBattleSolo, err := teamBattleSoloWithBattlePoints(teamBattleSolo, bp, medalCount)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if a.account.ClearPendingStageQuest() && !a.persistOrError(writer) {
		return
	}
	teamBattleSolo, err = a.account.WithBurstQuest(teamBattleSolo, payload.ActiveArthurType)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, teamBattleSolo)
}
