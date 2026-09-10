package httpapi

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

const maxRequestBytes = 1 << 20

type API struct {
	release             *release.Release
	store               *store
	baseURL             string
	sessionKey          string
	logger              *slog.Logger
	persistState        StatePersister
	multiplayer         *multiplayer.Hub
	battleSV            multiplayer.Endpoint
	pvpAccounts         PVPAccountRepository
	pvpConfig           PVPConfig
	friendPointAccounts FriendPointAccountRepository
	teamBattleResultMu  sync.Mutex
	clientResultMu      sync.Mutex
}

func New(
	runtimeRelease *release.Release,
	baseURL string,
	logger *slog.Logger,
) (http.Handler, error) {
	return NewWithStatePersistence(runtimeRelease, baseURL, logger, nil)
}

func NewWithStatePersistence(
	runtimeRelease *release.Release,
	baseURL string,
	logger *slog.Logger,
	persistState StatePersister,
) (http.Handler, error) {
	return NewWithStatePersistenceAndMultiplayer(runtimeRelease, baseURL, logger, persistState, nil, multiplayer.Endpoint{})
}

func NewWithStatePersistenceAndMultiplayer(
	runtimeRelease *release.Release,
	baseURL string,
	logger *slog.Logger,
	persistState StatePersister,
	multiplayerHub *multiplayer.Hub,
	battleSV multiplayer.Endpoint,
) (http.Handler, error) {
	return NewWithStatePersistenceAndMultiplayerAndPVP(
		runtimeRelease, baseURL, logger, persistState,
		multiplayerHub, battleSV, nil, PVPConfig{}, nil,
	)
}

func NewWithStatePersistenceAndMultiplayerAndPVP(
	runtimeRelease *release.Release,
	baseURL string,
	logger *slog.Logger,
	persistState StatePersister,
	multiplayerHub *multiplayer.Hub,
	battleSV multiplayer.Endpoint,
	pvpAccounts PVPAccountRepository,
	pvpConfig PVPConfig,
	friendPointAccounts FriendPointAccountRepository,
) (http.Handler, error) {
	if runtimeRelease == nil {
		return nil, errors.New("runtime release is required")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("advertised base URL is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	cardStore, err := newStore(runtimeRelease.State)
	if err != nil {
		return nil, err
	}
	api := &API{
		release: runtimeRelease,
		store:   cardStore,
		baseURL: baseURL,
		sessionKey: fmt.Sprintf(
			"LOCAL_V2_%d",
			runtimeRelease.State.CatalogVersion,
		),
		logger:              logger,
		persistState:        persistState,
		multiplayer:         multiplayerHub,
		battleSV:            battleSV,
		pvpAccounts:         pvpAccounts,
		pvpConfig:           pvpConfig,
		friendPointAccounts: friendPointAccounts,
	}
	if cardStore.initialStateRepair && persistState != nil {
		if err := persistState(cardStore.snapshot(runtimeRelease.State)); err != nil {
			return nil, fmt.Errorf("persist reconciled initial state: %w", err)
		}
	}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(api.logRequest)
	api.register(router)
	return &accountBusinessHandler{Handler: router, api: api}, nil
}

func (a *API) register(router chi.Router) {
	routes := a.release.State.Routes
	if routes.LocalShopBonus != "" {
		router.Post(routes.LocalShopBonus, func(w http.ResponseWriter, r *http.Request) {
			a.writeProtocol(w, map[string]any{"activities": []any{}})
		})
	}
	if routes.LocalShopQueryOrder != "" {
		router.Post(routes.LocalShopQueryOrder, a.localShopQueryOrder)
	}
	if routes.LocalShopQueryCard != "" {
		router.Post(routes.LocalShopQueryCard, a.localShopQueryCard)
	}
	if routes.LocalShopGetCard != "" {
		router.Post(routes.LocalShopGetCard, a.localShopGetCard)
	}
	router.Get(routes.Health, a.health)
	router.Post(routes.Login, a.login)
	router.Post(routes.AuthCheck, a.authCheck)
	router.Post(routes.Connect, a.connect)
	router.Get(routes.HonorShow, a.honorShow)
	router.Post(routes.HonorShow, a.honorShow)
	router.Get(routes.HonorDeckShow, a.honorDeckShow)
	router.Post(routes.HonorDeckShow, a.honorDeckShow)
	router.Post(routes.HonorDeckSet, a.honorDeckSet)
	router.Post(routes.UserCreate, a.userCreate)
	router.Post(routes.UserSetName, a.userSetName)
	router.Post(routes.UserSetComment, a.userSetComment)
	router.Get(routes.CardCollectionShow, a.cardCollectionShow)
	router.Post(routes.CardCollectionShow, a.cardCollectionShow)
	router.Post(routes.SupportCardSlotUnlock, a.supportCardSlotUnlock)
	router.Get(routes.HomeShow, a.homeShow)
	router.Post(routes.HomeShow, a.homeShow)
	if routes.PopupExec != "" {
		router.Post(routes.PopupExec, a.popupExec)
	}
	router.Get(routes.MainQuestShow, a.mainQuestShow)
	router.Post(routes.MainQuestShow, a.mainQuestShow)
	if routes.TeamBattleSoloShow != "" {
		router.Post(routes.TeamBattleSoloShow, a.teamBattleSoloShow)
	}
	if routes.TeamBattleSoloPartnerShow != "" {
		router.Post(routes.TeamBattleSoloPartnerShow, a.teamBattleSoloPartnerShow)
	}
	if routes.TeamBattleSoloPartnerRentalDeck != "" {
		router.Post(routes.TeamBattleSoloPartnerRentalDeck, a.teamBattleSoloPartnerRentalDeck)
	}
	if routes.TeamBattleRecommendDeckShow != "" {
		router.Post(routes.TeamBattleRecommendDeckShow, a.teamBattleRecommendDeckShow)
	}
	if routes.TeamBattlePastBossShow != "" {
		router.Post(routes.TeamBattlePastBossShow, a.teamBattlePastBossShow)
	}
	if routes.TeamBattleClearDeckShow != "" {
		router.Post(routes.TeamBattleClearDeckShow, a.teamBattleClearDeckShow)
	}
	if routes.TeamBattleScoreRewardLineup != "" {
		router.Post(routes.TeamBattleScoreRewardLineup, a.teamBattleScoreRewardLineup)
	}
	if routes.DailyClearRankShow != "" {
		router.Post(routes.DailyClearRankShow, a.dailyClearRankShow)
	}
	if routes.ChallengeShow != "" {
		router.Post(routes.ChallengeShow, a.challengeShow)
	}
	if routes.TeamBattleScheduleShow != "" {
		router.Post(routes.TeamBattleScheduleShow, a.teamBattleScheduleShow)
	}
	if routes.TeamBattleScheduleUpdate != "" {
		router.Post(routes.TeamBattleScheduleUpdate, a.teamBattleScheduleUpdate)
	}
	if routes.UserBuffExec != "" {
		router.Post(routes.UserBuffExec, a.userBuffExec)
	}
	if routes.TeamBattleSoloStart != "" {
		router.Post(routes.TeamBattleSoloStart, a.teamBattleSoloStart)
	}
	if routes.TeamBattleSoloContinue != "" {
		router.Post(routes.TeamBattleSoloContinue, a.teamBattleSoloContinue)
	}
	if routes.TeamBattleSoloEnd != "" {
		router.Post(routes.TeamBattleSoloEnd, a.teamBattleSoloEnd)
	}
	if routes.TowerQuestShow != "" {
		router.Post(routes.TowerQuestShow, a.towerQuestShow)
	}
	if routes.TowerRankingShow != "" {
		router.Post(routes.TowerRankingShow, a.towerRankingShow)
	}
	if routes.TeamBattleMultiShow != "" {
		router.Post(routes.TeamBattleMultiShow, a.teamBattleMultiShow)
	}
	if routes.TeamBattleMultiRoomSearch != "" {
		router.Post(routes.TeamBattleMultiRoomSearch, a.teamBattleMultiRoomSearch)
	}
	if routes.TeamBattleMultiRoomCreate != "" {
		router.Post(routes.TeamBattleMultiRoomCreate, a.teamBattleMultiRoomCreate)
	}
	if routes.TeamBattleAIRoomCreate != "" {
		router.Post(routes.TeamBattleAIRoomCreate, a.teamBattleAIRoomCreate)
	}
	if routes.TeamBattleMultiRoomReserve != "" {
		router.Post(routes.TeamBattleMultiRoomReserve, a.teamBattleMultiRoomReserve)
	}
	if routes.TeamBattleMultiRoomReserveCancel != "" {
		router.Post(routes.TeamBattleMultiRoomReserveCancel, a.teamBattleMultiRoomReserveCancel)
	}
	if routes.TeamBattleMultiRoomEnter != "" {
		router.Post(routes.TeamBattleMultiRoomEnter, a.teamBattleMultiRoomEnter)
	}
	if routes.TeamBattleResult != "" {
		router.Post(routes.TeamBattleResult, a.teamBattleResult)
	}
	router.Get(routes.DeckLimitShow, a.deckLimitShow)
	router.Post(routes.DeckLimitShow, a.deckLimitShow)
	router.Get(routes.CostumeShow, a.costumeShow)
	router.Post(routes.CostumeShow, a.costumeShow)
	router.Post(routes.CostumeSet, a.costumeSet)
	router.Post(routes.AvatarPartsShow, a.avatarPartsShow)
	router.Post(routes.AvatarPartsDeckSet, a.avatarPartsDeckSet)
	router.Post(routes.AvatarShopShow, a.avatarShopShow)
	router.Post(routes.AvatarShopBuy, a.avatarShopBuy)
	router.Post(routes.SetTutorialFlag, a.setTutorialFlag)
	router.Post(routes.CardShow, a.cardShow)
	if routes.CardContainerShow != "" {
		router.Post(routes.CardContainerShow, a.cardContainerShow)
	}
	router.Post(routes.CardMove, a.cardMove)
	router.Post(routes.CardContainerLock, a.cardContainerLock)
	router.Post(routes.CardContainerUnlock, a.cardContainerUnlock)
	router.Post(routes.CardContainerSell, a.cardContainerSell)
	router.Post(routes.CardLock, a.cardLock)
	router.Post(routes.CardUnlock, a.cardUnlock)
	router.Post(routes.CardLoveUp, a.cardLoveUp)
	router.Post(routes.CardDecompose, a.cardDecompose)
	router.Post(routes.CardFameTrainInfo, a.cardFameTrainInfo)
	router.Post(routes.CardFameStartTrain, a.cardFameStartTrain)
	router.Post(routes.CardFameCancelTrain, a.cardFameCancelTrain)
	router.Post(routes.CardFameTrainFinish, a.cardFameTrainFinish)
	router.Post(routes.HowToGetCardShow, a.howToGetCardShow)
	router.Get(routes.SphereShow, a.sphereShow)
	router.Post(routes.SphereShow, a.sphereShow)
	if routes.SphereFusion != "" {
		router.Post(routes.SphereFusion, a.sphereFusion)
	}
	if routes.SphereEvolution != "" {
		router.Post(routes.SphereEvolution, a.sphereEvolution)
	}
	if routes.SphereSell != "" {
		router.Post(routes.SphereSell, a.sphereSell)
	}
	if routes.SphereLock != "" {
		router.Post(routes.SphereLock, a.sphereLock)
	}
	if routes.SphereUnlock != "" {
		router.Post(routes.SphereUnlock, a.sphereUnlock)
	}
	router.Get(routes.BuddyShow, a.buddyShow)
	router.Post(routes.BuddyShow, a.buddyShow)
	if routes.BuddyFusion != "" {
		router.Post(routes.BuddyFusion, a.buddyFusion)
	}
	if routes.BuddyEvolution != "" {
		router.Post(routes.BuddyEvolution, a.buddyEvolution)
	}
	if routes.BuddySell != "" {
		router.Post(routes.BuddySell, a.buddySell)
	}
	if routes.BuddyLock != "" {
		router.Post(routes.BuddyLock, a.buddyLock)
	}
	if routes.BuddyUnlock != "" {
		router.Post(routes.BuddyUnlock, a.buddyUnlock)
	}
	router.Get(routes.CardCategoryGet, a.cardCategoryGet)
	router.Post(routes.CardCategoryGet, a.cardCategoryGet)
	router.Post(routes.CardDeckSet, a.cardDeckSet)
	router.Post(routes.ExploreStart, a.exploreStart)
	router.Get(routes.ExploreEnd, a.exploreEnd)
	router.Post(routes.ExploreEnd, a.exploreEnd)
	router.Post(routes.CardFusion, a.cardFusion)
	router.Post(routes.CardEvolution, a.cardEvolution)
	router.Post(routes.CardSell, a.cardSell)
	router.Post(routes.MissionShow, a.missionShow)
	router.Post(routes.MissionReward, a.missionReward)
	if routes.MissionURLOpen != "" {
		router.Post(routes.MissionURLOpen, a.missionURLOpen)
	}
	router.Get(routes.PresentBoxShow, a.presentBoxShow)
	router.Post(routes.PresentBoxShow, a.presentBoxShow)
	router.Post(routes.PresentBoxRecv, a.presentBoxRecv)
	router.Post(routes.PresentBoxMultiRecv, a.presentBoxMultiRecv)
	if routes.PresentBoxDelete != "" {
		router.Post(routes.PresentBoxDelete, a.presentBoxDelete)
	}
	router.Post(routes.UpdateGameOption, a.updateGameOption)
	router.Post(routes.UpdatePushOption, a.updatePushOption)
	if routes.GetNaviShow != "" {
		router.Post(routes.GetNaviShow, a.getNaviShow)
	}
	if routes.NaviSelect != "" {
		router.Post(routes.NaviSelect, a.naviSelect)
	}
	if routes.BuyNavi != "" {
		router.Post(routes.BuyNavi, a.buyNavi)
	}
	router.Post(routes.ItemShow, a.itemShow)
	router.Post(routes.ItemUse, a.itemUse)
	if routes.ItemExchange != "" {
		router.Post(routes.ItemExchange, a.itemExchange)
	}
	if routes.ItemLackTips != "" {
		router.Post(routes.ItemLackTips, a.itemLackTips)
	}
	router.Post(routes.ItemShopShow, a.itemShopShow)
	router.Post(routes.ItemShopBuy, a.itemShopBuy)
	if routes.EventShopShow != "" {
		router.Post(routes.EventShopShow, a.eventShopShow)
	}
	if routes.EventShopBuy != "" {
		router.Post(routes.EventShopBuy, a.eventShopBuy)
	}
	if routes.TradeShopShow != "" {
		router.Post(routes.TradeShopShow, a.tradeShopShow)
	}
	if routes.TradeShopLineupShow != "" {
		router.Post(routes.TradeShopLineupShow, a.tradeShopLineupShow)
	}
	if routes.TradeShopBuy != "" {
		router.Post(routes.TradeShopBuy, a.tradeShopBuy)
	}
	router.Post(routes.GachaShow, a.gachaShow)
	router.Post(routes.GachaPlay, a.gachaPlay)
	if routes.GachaItemPlay != "" {
		router.Post(routes.GachaItemPlay, a.gachaItemPlay)
	}
	router.Post(routes.GachaLineupShow, a.gachaLineupShow)
	if routes.GachaSelectLineupShow != "" {
		router.Post(routes.GachaSelectLineupShow, a.gachaSelectLineupShow)
	}
	if routes.GachaSelectedListShow != "" {
		router.Post(routes.GachaSelectedListShow, a.gachaSelectedListShow)
	}
	router.Post(routes.GachaOddsShow, a.gachaOddsShow)
	router.Post(routes.GetRecommendCardInfo, a.getRecommendCardInfo)
	router.Post(routes.GetURCardNoGetFromCurrentGaCha, a.getURCardNoGetFromCurrentGaCha)
	router.Post(routes.CoinUse, a.coinUse)
	router.Get(routes.StampShow, a.stampShow)
	router.Post(routes.StampShow, a.stampShow)
	router.Post(routes.StampDeckSet, a.stampDeckSet)
	router.Post(routes.FriendSearch, a.friendSearch)
	router.Get(routes.FollowShow, a.followShow)
	router.Post(routes.FollowShow, a.followShow)
	router.Post(routes.FollowerShow, a.followerShow)
	if routes.FollowAdd != "" {
		router.Post(routes.FollowAdd, a.followAdd)
	}
	if routes.FollowUnfollow != "" {
		router.Post(routes.FollowUnfollow, a.followUnfollow)
	}
	router.Post(routes.UserProfileShow, a.userProfileShow)
	router.Get(routes.StoryMainShow, a.storyMainShow)
	router.Post(routes.StoryMainShow, a.storyMainShow)
	router.Post(routes.StoryMainStart, a.storyMainStart)
	router.Post(routes.StoryMainEnd, a.storyMainEnd)
	router.Get(routes.StorySubShow, a.storySubShow)
	router.Post(routes.StorySubShow, a.storySubShow)
	router.Post(routes.StorySubStart, a.storySubStart)
	router.Post(routes.StorySubEnd, a.storySubEnd)
	router.Get(routes.StoryEventShow, a.storyEventShow)
	router.Post(routes.StoryEventShow, a.storyEventShow)
	router.Post(routes.StoryEventUnlock, a.storyEventUnlock)
	router.Post(routes.StoryStart, a.storyStart)
	router.Post("/StoryTeamBattleStart", a.storyTeamBattleStart)
	router.Post("/StoryTeamBattleEnd", a.storyTeamBattleEnd)
	if routes.EventShow != "" {
		router.Post(routes.EventShow, a.eventShow)
	}
	if routes.PVPShow != "" {
		router.Post(routes.PVPShow, a.pvpShow)
	}
	if routes.PVPStart != "" {
		router.Post(routes.PVPStart, a.pvpStart)
	}
	if routes.PVPEnd != "" {
		router.Post(routes.PVPEnd, a.pvpEnd)
	}
	router.Post(routes.Ping, a.ping)

	a.registerStatic(
		router,
		routes.ResourcePrefix,
		"resources/Android/patch",
	)
	a.registerStatic(router, routes.CPKPrefix, "cpk/CPK")
	a.registerStatic(router, routes.ImagePrefix, "images/chr51")
}

func (a *API) registerStatic(
	router chi.Router,
	prefix string,
	publicDirectory string,
) {
	pattern := prefix + "{filename}"
	handler := func(writer http.ResponseWriter, request *http.Request) {
		filename := chi.URLParam(request, "filename")
		if filename == "" ||
			path.Base(filename) != filename ||
			strings.ContainsAny(filename, `/\`) {
			writeError(writer, http.StatusBadRequest, "invalid release filename")
			return
		}
		a.servePublicFile(
			writer,
			request,
			path.Join(publicDirectory, filename),
		)
	}
	router.Get(pattern, handler)
	router.Head(pattern, handler)
}

func (a *API) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":          "ok",
		"release_id":      a.release.Manifest.ReleaseID,
		"source_build":    a.release.State.SourceBuild,
		"catalog_version": a.release.State.CatalogVersion,
	})
}

func (a *API) login(writer http.ResponseWriter, request *http.Request) {
	if _, err := readBody(request); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"res_code":      0,
		"sess_key":      a.sessionKey,
		"api_url":       a.baseURL + "/api/",
		"res_patch_url": a.baseURL + "/resources/",
		"res_img_url":   a.baseURL + "/images/",
		"res_cpk_url":   a.baseURL + "/cpk/",
		"smart_beat":    false,
		"web_url":       a.baseURL + "/",
		"userid":        a.release.State.User.UserID,
	})
}

func (a *API) authCheck(writer http.ResponseWriter, request *http.Request) {
	if _, err := readBody(request); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"res_code":     0,
		"res_str":      "",
		"check_result": 1,
		"user_name":    "",
		"lv":           0,
		"coin":         0,
		"refund_url":   "",
		"web_url":      a.baseURL + "/",
		"platforms":    []int{},
	})
}

func (a *API) connect(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(writer, http.StatusBadRequest, "session body is empty")
		return
	}
	gameOptionFlag, pushOptionFlag := a.store.optionState()
	isUser := 0
	if a.store.userCreated() {
		isUser = 1
	}
	a.writeProtocol(writer, map[string]any{
		"is_user":              isUser,
		"push_option":          map[string]int{"enable_flag": pushOptionFlag},
		"game_option":          map[string]int{"enable_flag": gameOptionFlag},
		"revision":             []any{},
		"navi_unlock_flag":     a.store.naviUnlockState(),
		"cl_behavior_flag":     0,
		"tutorial_flag":        a.store.tutorialState(),
		"is_multidevice_share": 0,
		"bonus": map[string]any{
			"bonus_end_time":   0,
			"present_end_time": 0,
			"info_url":         "",
		},
	})
}

func (a *API) honorDeckShow(writer http.ResponseWriter, _ *http.Request) {
	deckHonorIDs, _ := a.store.honorState()
	a.writeProtocol(writer, map[string]any{"deck_honorids": deckHonorIDs})
}

func (a *API) honorShow(writer http.ResponseWriter, _ *http.Request) {
	deckHonorIDs, honorIDs := a.store.honorState()
	a.writeProtocol(writer, map[string]any{
		"deck_honorids": deckHonorIDs,
		"honorids":      honorIDs,
		"new_honorids":  []int{},
	})
}

func (a *API) honorDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		DeckHonorIDs []int `json:"deck_honorids"`
	}
	if err := decodeExact(request, []string{"deck_honorids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.setHonorDeck(payload.DeckHonorIDs) {
		writeError(writer, http.StatusBadRequest, "invalid honor deck")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) userCreate(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Name       string `json:"name"`
		ArthurType int8   `json:"arthur_type"`
	}
	if err := decodeExact(request, []string{"name", "arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.createUser(payload.Name, payload.ArthurType) {
		writeError(writer, http.StatusBadRequest, "user is already created or creation fields are invalid")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	_, pushOptionFlag := a.store.optionState()
	a.writeProtocol(writer, map[string]any{
		"user":        a.userPayload(),
		"push_option": map[string]int{"enable_flag": pushOptionFlag},
	})
}

func (a *API) userSetName(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := decodeExact(request, []string{"name"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.setUserName(payload.Name) {
		writeError(writer, http.StatusBadRequest, "name must not be empty")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) userSetComment(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Comment string `json:"comment"`
	}
	if err := decodeExact(request, []string{"comment"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.setUserComment(payload.Comment) {
		writeError(writer, http.StatusBadRequest, "comment exceeds the official client byte limit")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) cardCollectionShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	pages, count, total := a.store.cardCollection()
	a.writeProtocol(writer, map[string]any{
		"pages":         pages,
		"find_card_num": count,
		"find_card_max": total,
	})
}

func (a *API) supportCardSlotUnlock(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ArthurType int8 `json:"arthur_type"`
	}
	if err := decodeExact(request, []string{"arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	unlocked, gold, err := a.store.unlockSupportCardSlot(payload.ArthurType)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	slots := make([]map[string]any, 4)
	for index := range slots {
		slots[index] = map[string]any{
			"arthur_type":     index + 1,
			"unlock_slot_num": unlocked[index],
		}
	}
	a.writeProtocol(writer, map[string]any{
		"support_card_slots": slots,
		"gold":               gold,
	})
}

func (a *API) userPayload() map[string]any {
	state := a.release.State
	user := state.User
	leaderUniqueID, leaderCardID, leaderFound := a.store.activeArthurLeaderState()
	if !leaderFound {
		leaderUniqueID = user.LeaderCardUniqueID
		leaderCardID = user.LeaderCardID
	}
	ap := a.store.apState()
	bp := a.store.battlePointState()
	progression := a.store.playerProgressionState()
	coin, coinFree := a.store.coinState()
	cardCapacity := a.store.cardCapacity()
	supportDeckSetCardNum, _ := a.store.supportDeckState()
	jobs := make([]map[string]int, len(progression.Jobs))
	for index, job := range progression.Jobs {
		jobs[index] = map[string]int{
			"hp":   job.HP,
			"atkp": job.Attack,
			"intp": job.Magic,
			"mndp": job.Mind,
		}
	}
	return map[string]any{
		"userid":                       user.UserID,
		"name":                         a.store.userName(),
		"active_arthur_type":           a.store.activeArthurType(),
		"arthur_rank":                  a.store.maximumDeckRank(),
		"lv":                           progression.Level,
		"exp":                          progression.Experience,
		"now_lv_exp":                   progression.NowLevelExperience,
		"next_lv_exp":                  progression.NextLevelExperience,
		"fame":                         0,
		"leader_card_uniqid":           leaderUniqueID,
		"leader_cardid":                leaderCardID,
		"comment":                      a.store.userComment(),
		"ap":                           ap.Current,
		"ap_max":                       ap.Max,
		"ap_next_sec":                  ap.NextSeconds,
		"ap_heal_sec":                  ap.HealSeconds,
		"bp":                           bp.Current,
		"bp_max":                       bp.Max,
		"bp_next_sec":                  bp.NextSeconds,
		"bp_heal_sec":                  bp.HealSeconds,
		"card_max_extend":              max(0, cardCapacity-cardCapacityBase),
		"card_num":                     a.store.cardCount(),
		"card_max":                     cardCapacity,
		"card_extend_limit":            cardCapacityLimit - cardCapacityBase,
		"card_container_num":           a.store.containerCardCount(),
		"card_container_max_extend":    0,
		"card_container_max":           user.CardContainerMax,
		"card_container_extend_limit":  0,
		"sphr_num":                     len(a.store.sphereState()),
		"sphr_max":                     user.SphereMax,
		"friend_max":                   progression.FriendMax,
		"friend_max_extend":            0,
		"friend_extend_limit":          0,
		"gold":                         a.store.goldState(),
		"fp":                           a.store.friendPointState(),
		"coin":                         coin,
		"coin_free":                    coinFree,
		"enter_state":                  0,
		"navi_type":                    a.store.naviID(),
		"inviteid":                     user.InviteID,
		"jobs":                         jobs,
		"deck_max_per_arthur":          user.DeckMaxPerArthur,
		"deck_max_alchemist":           user.DeckMaxAlchemist,
		"tag_name":                     "",
		"total_cost":                   0,
		"support_deck_set_card_num":    supportDeckSetCardNum,
		"rookie_type":                  0,
		"fresher_left_time":            0,
		"rookie_point":                 0,
		"punished_free_point":          0,
		"punished_free_point_max":      0,
		"punished_free_point_next_sec": 0,
		"punished_free_point_heal_sec": 0,
		"bridgeid":                     "",
		"buddy_num":                    len(a.store.buddyState()),
		"buddy_max":                    user.BuddyMax,
	}
}

func (a *API) homeShow(writer http.ResponseWriter, _ *http.Request) {
	// The original client submits its saved result before HomeShow. Reaching
	// home abandons any remaining solo context, without refunding the entry.
	a.store.abandonSoloBattle()
	a.store.clearBurstStory()
	now := time.Now()
	if !a.refreshPVPChallenges(writer, now) {
		return
	}
	homeBanners := []any{}
	if gachaID := a.store.homeBannerGachaID(now); gachaID > 0 &&
		a.store.featureUnlocked(20) && !a.store.onboardingInProgress() {
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
			a.release.State.User.UserID,
			a.store.friendPointRentalCursor(),
		)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "load friend-point rental inbox")
			return
		}
		claimed, err := a.store.claimFriendPointRentalEvents(events)
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
	loginClaims := loginBonusClaims{}
	if !a.store.onboardingInProgress() {
		var err error
		loginClaims, err = a.store.claimLoginBonuses(now)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "claim login bonuses")
			return
		}
	}
	onboardingQuests, err := a.store.homeOnboardingQuests()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "load onboarding quests")
		return
	}
	user := a.release.State.User
	stive, cardRemainTime := a.store.cardDevelopmentHomeState(now)
	maxRank := a.store.homeDeckRank()
	userPayload := a.userPayload()
	medalCount := a.store.itemCount(a.release.State.TeamBattleMedalItemID)
	bossCoinCount := a.store.itemCount(a.release.State.BossCoinItemID)
	pvpPoint, pvpChallenge := a.store.pvpStatus()
	appearTowerID := 0
	if towerIDs := a.store.towerQuestIDs(); len(towerIDs) > 0 {
		appearTowerID = towerIDs[0]
	}
	if !a.persistOrError(writer) {
		return
	}
	eventPage := a.release.State.EventPageProfile
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
		a.store.featureUnlocked(13) && !a.store.onboardingInProgress() {
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
	homePopups := []release.PopupProfile{}
	if !a.store.onboardingInProgress() {
		homePopups = a.store.unreadPopupState(a.release.State.PopupProfile)
	}
	cardFlags := a.store.localCardPayload(now)
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
		"firstpay":          localShopFirstPay,
		"monthcardflag":     cardFlags["monthcardflag"],
		"forevercardflag":   cardFlags["forevercardflag"],
		"monthcardtime":     cardFlags["monthcardtime"],
		"totalcoin":         0,
		"rookie_bonus":      nil,
		"home_event":        homeEvents,
		"max_rank":          maxRank,
		"medal_num":         medalCount,
		"expired_medal_num": 0,
		"bosscoin_itemid":   a.release.State.BossCoinItemID,
		"bosscoin_num":      bossCoinCount,
		"stive":             stive,
		"card_remain_time":  cardRemainTime,
		"tutorial_flag":     a.store.tutorialState(),
		"appear_towerid":    appearTowerID,
		"pvp_point":         pvpPoint,
		"challenge":         pvpChallenge,
		"score_ranking":     []any{},
	}, homePopups)
}

func (a *API) eventShow(writer http.ResponseWriter, _ *http.Request) {
	profile := a.release.State.EventPageProfile
	a.writeProtocol(writer, map[string]any{
		"eventid":      profile.EventID,
		"bg_pictid":    profile.BackgroundPictID,
		"info_url":     profile.InfoURL,
		"update_info":  profile.UpdateInfo,
		"itemid":       append([]int{}, profile.ItemIDs...),
		"buttons":      append([]release.EventPageButton{}, profile.Buttons...),
		"is_new_solo":  profile.IsNewSolo,
		"is_new_multi": profile.IsNewMulti,
	})
}

func projectLoginBonusResult(result presentReceiveResult) (any, error) {
	rewards := battleResultRewardsWire(result.Rewards)
	if len(rewards) != 1 {
		return nil, errors.New("login bonus result must contain exactly one reward")
	}
	return rewards[0], nil
}

func projectDailyLoginBonus(claim *loginBonusClaim) ([]any, error) {
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
	claim *loginBonusClaim,
	schedule []release.LoginBonusDay,
) ([]any, error) {
	if claim == nil {
		return nil, nil
	}
	rewards := make([]any, len(schedule))
	for index, day := range schedule {
		result := presentReceiveResult{Rewards: []receivedReward{{
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
	claim *loginBonusClaim,
	milestones []release.LoginBonusDay,
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
		preview, err := projectLoginBonusResult(presentReceiveResult{Rewards: []receivedReward{{
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
	payload, consumedNewClear, err := a.store.stageQuestPayload(areaID, selectArea)
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
	teamBattleSolo := a.store.teamBattleSoloState()
	if len(teamBattleSolo) == 0 {
		writeError(writer, http.StatusInternalServerError, "team battle configuration is empty")
		return
	}
	bp := a.store.battlePointState()
	medalCount := a.store.itemCount(a.release.State.TeamBattleMedalItemID)
	teamBattleSolo, err := teamBattleSoloWithBattlePoints(teamBattleSolo, bp, medalCount)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if a.store.clearPendingStageQuest() && !a.persistOrError(writer) {
		return
	}
	teamBattleSolo, err = a.store.withBurstQuest(teamBattleSolo, payload.ActiveArthurType)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, teamBattleSolo)
}

func (a *API) deckLimitShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, map[string]any{"infos": []any{}})
}

func (a *API) costumeShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, a.release.State.Costume)
}

func (a *API) costumeSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ArthurType int8 `json:"arthur_type"`
		CostumeID  int  `json:"costumeid"`
	}
	if err := decodeExact(
		request,
		[]string{"arthur_type", "costumeid"},
		&payload,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.selectCostume(payload.ArthurType, payload.CostumeID) {
		writeError(writer, http.StatusBadRequest, "unknown costume selection")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"arthur_type": payload.ArthurType,
		"costumeid":   payload.CostumeID,
	})
}

func (a *API) setTutorialFlag(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var fields map[string]json.RawMessage
	if err := decodeExact(request, []string{"flag"}, &fields); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var flag int64
	if err := json.Unmarshal(fields["flag"], &flag); err != nil {
		writeError(writer, http.StatusBadRequest, "flag must be an integer")
		return
	}
	if !a.store.mergeTutorialFlag(flag) {
		writeError(writer, http.StatusBadRequest, "invalid tutorial flag")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) cardShow(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || len(fields) != 1 {
		writeError(
			writer,
			http.StatusBadRequest,
			"CardShow2 requires one JSON field",
		)
		return
	}
	raw, exists := fields["0"]
	if !exists {
		raw, exists = fields["deck_info_type"]
	}
	var deckInfoType int
	if !exists ||
		json.Unmarshal(raw, &deckInfoType) != nil ||
		deckInfoType < 0 {
		writeError(
			writer,
			http.StatusBadRequest,
			"invalid deck_info_type",
		)
		return
	}
	cards, decks := a.store.show()
	stackCards := a.store.stackState()
	avatars := a.store.avatarsState()
	wireAvatars := make([]wireAvatarInfo, len(avatars))
	for index, avatar := range avatars {
		wireAvatars[index] = wireAvatarInfo{
			CostumeID:     avatar.CostumeID,
			AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	_, unlockedSupportSlots := a.store.supportDeckState()
	supportSlots := make([]wireSupportSlot, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		supportSlots = append(supportSlots, wireSupportSlot{
			ArthurType:    arthurType,
			UnlockSlotNum: unlockedSupportSlots[arthurType-1],
		})
	}
	a.writeProtocol(writer, wireCardShow{
		Cards:             toWireCards(cards),
		StackCards:        toWireStackCards(stackCards),
		Decks:             toWireDecks(decks),
		Avatars:           wireAvatars,
		SupportSlots:      supportSlots,
		CardCollectionNum: len(a.store.cardCollectionState()),
	})
}

func (a *API) sphereShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, wireSphereShow{Spheres: toWireSpheres(a.store.sphereState())})
}

func (a *API) cardContainerShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, wireCardContainerShow{
		Cards: toWireCards(a.store.containerShow()),
	})
}

func (a *API) cardMove(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ToSlot        int8    `json:"to_slot"`
		MoveUniqueIDs []int64 `json:"move_uniqids"`
	}
	if err := decodeExact(request, []string{"to_slot", "move_uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	cards, decks, err := a.store.moveCards(payload.ToSlot, payload.MoveUniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireCardMove{
		UpdatedCards: toWireCards(cards),
		UpdatedDecks: toWireDecks(decks),
	})
}

func (a *API) setCardLock(writer http.ResponseWriter, request *http.Request, slot int, locked bool) {
	var payload struct {
		UniqueID int64 `json:"uniqid"`
	}
	if err := decodeExact(request, []string{"uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.setCardLock(payload.UniqueID, slot, locked); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) cardLock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 0, true)
}

func (a *API) cardUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 0, false)
}

func (a *API) cardContainerLock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 1, true)
}

func (a *API) cardContainerUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 1, false)
}

func (a *API) cardContainerSell(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueIDs []int64 `json:"uniqids"`
	}
	if err := decodeExact(request, []string{"uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	getGold, gold, err := a.store.sellContainerCards(payload.UniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"card_container_num": a.store.containerCardCount(),
		"get_gold":           getGold,
		"gold":               gold,
	})
}

func (a *API) cardLoveUp(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID int64           `json:"base_uniqid"`
		UseItems     []loveUpItemUse `json:"use_items"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "use_items"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.loveUpCard(payload.BaseUniqueID, payload.UseItems)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	card := toWireCard(result.Card)
	a.writeProtocol(writer, map[string]any{
		"base_card": card,
		"item":      a.itemInfosWire(result.Items),
		"result_card": map[string]any{
			"card": card,
			"love_up": []any{map[string]any{
				"old_love": result.OldLove,
				"new_love": result.NewLove,
				"is_max":   result.IsMax,
			}},
			"rewards": []any{},
		},
		"gold": result.Gold,
	})
}

func (a *API) buddyShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, wireBuddyShow{Buddies: toWireBuddies(a.store.buddyState())})
}

func (a *API) cardCategoryGet(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	categories, groups := a.store.cardCategoryState()
	wireCategories := make([]map[string]any, len(categories))
	for index, category := range categories {
		wireCategories[index] = map[string]any{
			"categoryid": category.CategoryID,
			"name":       category.Name,
			"order":      category.Order,
			"view_type":  category.ViewType,
		}
	}
	wireGroups := make([]map[string]any, len(groups))
	for index, group := range groups {
		wireGroups[index] = map[string]any{
			"groupid":           group.GroupID,
			"categoryid":        group.CategoryID,
			"name":              group.Name,
			"order":             group.Order,
			"deck_limit_bossid": group.DeckLimitBossID,
		}
	}
	a.writeProtocol(writer, map[string]any{
		"categories": wireCategories,
		"groups":     wireGroups,
	})
}

func (a *API) cardDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Decks []deckInfo `json:"decks"`
	}
	if err := decodeExact(request, []string{"decks"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	cards, decks, err := a.store.setDecks(payload.Decks)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireDeckSet{
		Cards: toWireCards(cards),
		Decks: toWireDecks(decks),
	})
}

func (a *API) exploreStart(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{"arthur_type", "deck_idx"})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var arthurType, deckIndex int8
	if json.Unmarshal(fields["arthur_type"], &arthurType) != nil ||
		json.Unmarshal(fields["deck_idx"], &deckIndex) != nil ||
		arthurType < 1 || arthurType > 4 || deckIndex < 0 {
		writeError(writer, http.StatusBadRequest, "invalid Explore selection")
		return
	}
	a.clientResultMu.Lock()
	defer a.clientResultMu.Unlock()
	ap, leaderCardID, avatar, stage, started := a.store.beginExplore(arthurType, deckIndex)
	if !started {
		writeError(writer, http.StatusBadRequest, "Explore points are empty or an exploration is already active")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"ap":            ap.Current,
		"ap_max":        ap.Max,
		"ap_next_sec":   ap.NextSeconds,
		"arthur_type":   arthurType,
		"leader_cardid": leaderCardID,
		"stage":         stage,
		"floor_rarity":  a.release.State.Explore.FloorRarity,
		"events":        a.release.State.Explore.Events,
		"avatar":        avatar,
	})
}

func (a *API) exploreEnd(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodPost {
		if _, err := decodeFlexibleFields(request, []string{}); err != nil {
			a.writeStoreError(writer, err)
			return
		}
	}
	rewards, err := decodeExploreRewards(a.release.State.Explore.Events)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.clientResultMu.Lock()
	defer a.clientResultMu.Unlock()
	if !a.store.exploreIsActive() {
		if cached, exists := a.store.exploreResultReceipt(); exists {
			a.writeProtocol(writer, cached)
			return
		}
	}
	result, deckCards, completed, startedAtUnix, err := a.store.endExplore(rewards)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	resultRewards := make([]any, 0, len(result.Rewards))
	if completed {
		for _, received := range result.Rewards {
			resultRewards = append(resultRewards, map[string]any{
				"reward":           received.Reward,
				"uniqid":           received.UniqueID,
				"is_new":           received.IsNew,
				"auto_fusion_used": 0,
				"auto_loveup_used": 0,
				"add":              []any{},
			})
		}
	}
	response := map[string]any{
		"user":                            a.userPayload(),
		"result_rewards":                  resultRewards,
		"deck_cards":                      toWireCards(deckCards),
		"new_cards":                       toWireCards(result.Cards),
		"new_stack_cards":                 toWireStackCards(result.StackCards),
		"new_items":                       a.itemInfosWire(result.Items),
		"new_sphrs":                       toWireSpheres(result.Spheres),
		"new_buddys":                      toWireBuddies(result.Buddies),
		"is_result_reward_in_present_box": boolInt(result.InPresentBox),
		"unlock_notice":                   []any{},
	}
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode Explore settlement")
		return
	}
	if completed {
		if err := a.store.recordExploreResultReceipt(startedAtUnix, encodedResponse, time.Now()); err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if completed && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, json.RawMessage(encodedResponse))
}

func decodeExploreRewards(events []json.RawMessage) ([]release.Reward, error) {
	type treasureBox struct {
		Rewards []release.Reward `json:"reward"`
	}
	type symbol struct {
		Rewards []release.Reward `json:"reward"`
	}
	type exploreEvent struct {
		TreasureBoxes []treasureBox `json:"treasureboxes"`
		Symbols       []symbol      `json:"symbols"`
	}
	results := make([]release.Reward, 0)
	for _, raw := range events {
		var event exploreEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("decode Explore result reward: %w", err)
		}
		rewardGroups := make([][]release.Reward, 0, len(event.TreasureBoxes)+len(event.Symbols))
		for _, box := range event.TreasureBoxes {
			rewardGroups = append(rewardGroups, box.Rewards)
		}
		for _, eventSymbol := range event.Symbols {
			rewardGroups = append(rewardGroups, eventSymbol.Rewards)
		}
		for _, group := range rewardGroups {
			for _, reward := range group {
				if reward.Num <= 0 || reward.Type < 0 || reward.Type >= 20 ||
					reward.CardSkillLevels == nil {
					return nil, errors.New("Explore result reward is incomplete")
				}
				results = append(results, reward)
			}
		}
	}
	return results, nil
}

func (a *API) cardFusion(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{
		"base_uniqid", "add_uniqids", "add_container_uniqids", "add_cardids", "add_stackcards",
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var baseUniqueID int64
	var materialUniqueIDs, containerUniqueIDs []int64
	if json.Unmarshal(fields["base_uniqid"], &baseUniqueID) != nil ||
		json.Unmarshal(fields["add_uniqids"], &materialUniqueIDs) != nil ||
		json.Unmarshal(fields["add_container_uniqids"], &containerUniqueIDs) != nil {
		writeError(writer, http.StatusBadRequest, "invalid fusion card selection")
		return
	}
	cardUses, err := decodeStackUses(fields["add_cardids"])
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	stackUses, err := decodeStackUses(fields["add_stackcards"])
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	stackUses = append(cardUses, stackUses...)
	oldCard, resultCard, successType, decks, err := a.store.fuseCard(
		baseUniqueID, materialUniqueIDs, containerUniqueIDs, stackUses,
	)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	isLevelMax := 0
	if resultCard.Level >= resultCard.LevelMax {
		isLevelMax = 1
	}
	experienceUp := []any{}
	if resultCard.Experience != oldCard.Experience {
		experienceUp = append(experienceUp, map[string]any{
			"old_exp":   oldCard.Experience,
			"new_exp":   resultCard.Experience,
			"old_lv":    oldCard.Level,
			"new_lv":    resultCard.Level,
			"is_lv_max": isLevelMax,
		})
	}
	fameUp := []any{}
	if resultCard.Fame != oldCard.Fame {
		isFameMax := 0
		if definition, exists := a.store.cardDefinitions[resultCard.CardID]; exists &&
			resultCard.Fame >= definition.FameMax {
			isFameMax = 1
		}
		fameUp = append(fameUp, map[string]any{
			"old_fame":    oldCard.Fame,
			"new_fame":    resultCard.Fame,
			"is_fame_max": isFameMax,
		})
	}
	a.writeProtocol(writer, map[string]any{
		"result_card": map[string]any{
			"card":         toWireCard(resultCard),
			"old_card":     toWireCard(oldCard),
			"success_type": successType,
			"exp_up":       experienceUp,
			"fame_up":      fameUp,
			"rewards":      []any{},
		},
		"gold":                   a.store.goldState(),
		"card_num":               a.store.cardCount(),
		"decks":                  toWireDecks(decks),
		"back_uniqids":           []int64{},
		"back_container_uniqids": []int64{},
		"back_stack_cards":       []any{},
	})
}

func (a *API) cardEvolution(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{
		"base_uniqid", "to_cardid", "add_uniqids", "add_container_uniqids", "add_cardids", "add_itemids",
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var baseUniqueID int64
	var toCardID int
	var materialUniqueIDs, containerUniqueIDs []int64
	if json.Unmarshal(fields["base_uniqid"], &baseUniqueID) != nil ||
		json.Unmarshal(fields["to_cardid"], &toCardID) != nil ||
		json.Unmarshal(fields["add_uniqids"], &materialUniqueIDs) != nil ||
		json.Unmarshal(fields["add_container_uniqids"], &containerUniqueIDs) != nil {
		writeError(writer, http.StatusBadRequest, "invalid evolution card selection")
		return
	}
	var cardIDs, itemIDs []int
	if json.Unmarshal(fields["add_cardids"], &cardIDs) != nil || json.Unmarshal(fields["add_itemids"], &itemIDs) != nil {
		writeError(writer, http.StatusBadRequest, "evolution material IDs must be arrays of integers")
		return
	}
	result, gold, decks, err := a.store.evolveCard(
		baseUniqueID, toCardID, materialUniqueIDs, containerUniqueIDs, append(cardIDs, itemIDs...),
	)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"base_card": toWireCard(result),
		"gold":      gold,
		"card_num":  a.store.cardCount(),
		"decks":     toWireDecks(decks),
		"reward":    nil,
	})
}

func (a *API) cardSell(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{"uniqids", "cardids"})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var uniqueIDs []int64
	if json.Unmarshal(fields["uniqids"], &uniqueIDs) != nil {
		writeError(writer, http.StatusBadRequest, "invalid sell card selection")
		return
	}
	stackUses, err := decodeStackUses(fields["cardids"])
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	getGold, gold, decks, err := a.store.sellCards(uniqueIDs, stackUses)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"card_num":                         a.store.cardCount(),
		"get_gold":                         getGold,
		"gold":                             gold,
		"extract_reward":                   []any{},
		"is_extract_reward_in_present_box": 0,
		"decks":                            toWireDecks(decks),
	})
}

func (a *API) missionShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		IsReward int `json:"is_reward"`
	}
	if err := decodeExact(request, []string{"is_reward"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"missions": a.store.missionInfos(),
	})
}

func (a *API) missionReward(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		MissionIDs []int `json:"missionids"`
	}
	if err := decodeExact(request, []string{"missionids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	rewardMissions, receiveMissions, err := a.store.receiveMissionRewards(payload.MissionIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"reward_missions":  rewardMissions,
		"receive_missions": receiveMissions,
	})
}

func (a *API) missionURLOpen(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		OpenURL string `json:"open_url"`
	}
	if err := decodeExact(request, []string{"open_url"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	updatedMissions, err := a.store.checkMissionOpenURL(payload.OpenURL)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"upd_missions": updatedMissions,
	})
}

func friendPayload(friend release.Friend) map[string]any {
	return map[string]any{
		"userid":            friend.UserID,
		"name":              friend.Name,
		"arthur_type":       friend.ArthurType,
		"is_burst":          friend.IsBurst,
		"lv":                friend.Level,
		"deck_rank":         friend.DeckRank,
		"state":             friend.FriendState,
		"leader_cardid":     friend.LeaderCardID,
		"leader_card_lv":    friend.LeaderCardLevel,
		"leader_card_fame":  friend.LeaderCardFame,
		"last_login_time":   friend.LastLoginTime,
		"comment":           friend.Comment,
		"pvp_point":         friend.PVPPoint,
		"is_first_matching": friend.IsFirstMatching,
		"is_rookie_bonus":   friend.IsRookieBonus,
		"deck_honorids":     friend.DeckHonorIDs,
	}
}

func friendPayloads(friends []release.Friend) []any {
	result := make([]any, 0, len(friends))
	for _, friend := range friends {
		result = append(result, friendPayload(friend))
	}
	return result
}

func (a *API) friendSearch(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		InviteID string `json:"inviteid"`
	}
	if err := decodeExact(request, []string{"inviteid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	relations, err := a.localAccountFriendRelations()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	friends := make([]release.Friend, 0, 1)
	for _, relation := range relations {
		if relation.State.User.InviteID != payload.InviteID {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			friends = append(friends, friend)
		}
		break
	}
	a.writeProtocol(writer, map[string]any{"user": friendPayloads(friends)})
}

func (a *API) followShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	followMax := a.store.followMaximum()
	relations, err := a.localAccountFriendRelations()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	friends := make([]release.Friend, 0)
	for _, relation := range relations {
		if relation.FriendState != friendStateFriend && relation.FriendState != friendStateFollow {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			friends = append(friends, friend)
		}
	}
	a.writeProtocol(writer, map[string]any{
		"follow_max": followMax,
		"friends":    friendPayloads(friends),
	})
}

func (a *API) followerShow(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		Page int `json:"page"`
	}
	if err := decodeExact(request, []string{"page"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	relations, err := a.localAccountFriendRelations()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	followerFriends := make([]release.Friend, 0)
	for _, relation := range relations {
		if relation.FriendState != friendStateFriend && relation.FriendState != friendStateFollower {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			followerFriends = append(followerFriends, friend)
		}
	}
	friends := friendPayloads(followerFriends)
	a.writeProtocol(writer, map[string]any{
		"friends":      friends,
		"follower_num": len(friends),
		"page_max":     1,
	})
}

func (a *API) followAdd(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserIDs []int `json:"userids"`
	}
	if err := decodeExact(request, []string{"userids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if a.friendPointAccounts == nil {
		writeError(writer, http.StatusInternalServerError, "local-account friend repository is unavailable")
		return
	}
	followMax := a.store.followMaximum()
	result, err := a.friendPointAccounts.FollowFriendPointAccounts(
		a.release.State.User.UserID,
		payload.UserIDs,
		followMax,
		a.store.friendMaximum(),
	)
	if err != nil {
		var followErr *FollowAddError
		if errors.As(err, &followErr) {
			a.writeProtocolResult(writer, map[string]any{
				"request_userids": []int{},
				"is_friend_full":  0,
			}, followErr.ResultCode, followErr.ResultString)
			return
		}
		a.writeStoreError(writer, err)
		return
	}
	friendFull := 0
	if result.IsFriendFull {
		friendFull = 1
	}
	a.writeProtocol(writer, map[string]any{
		"request_userids": result.RequestUserIDs,
		"is_friend_full":  friendFull,
	})
}

func (a *API) followUnfollow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserID int `json:"userid"`
	}
	if err := decodeExact(request, []string{"userid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if a.friendPointAccounts == nil {
		writeError(writer, http.StatusInternalServerError, "local-account friend repository is unavailable")
		return
	}
	if err := a.friendPointAccounts.UnfollowFriendPointAccount(
		a.release.State.User.UserID,
		payload.UserID,
	); err != nil {
		var followErr *FollowAddError
		if errors.As(err, &followErr) {
			a.writeProtocolResult(writer, map[string]any{}, followErr.ResultCode, followErr.ResultString)
			return
		}
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func profilePayload(friend release.Friend) map[string]any {
	return map[string]any{
		"name": friend.Name,
		"lv":   friend.Level,
		"hp":   friend.HP,
		"atkp": friend.Attack,
		"intp": friend.Magic,
		"mndp": friend.Mind,
		"deck": map[string]any{
			"arthur_type":      friend.ArthurType,
			"is_burst":         friend.IsBurst,
			"job_type":         friend.JobType,
			"deck_rank":        friend.DeckRank,
			"leader_cardid":    friend.LeaderCardID,
			"leader_card_lv":   friend.LeaderCardLevel,
			"leader_card_fame": friend.LeaderCardFame,
		},
		"comment":         friend.Comment,
		"last_login_time": friend.LastLoginTime,
		"friend_state":    friend.FriendState,
		"pvp_point":       friend.PVPPoint,
		"deck_honorids":   friend.DeckHonorIDs,
	}
}

func (a *API) localProfilePayload() map[string]any {
	user := a.release.State.User
	activeArthurType := a.store.activeArthurType()
	deck, leader, hp, attack, magic, mind, found := a.activeDeckProfile()
	leaderLevel := 1
	leaderFame := 1
	leaderCardID := user.LeaderCardID
	jobType := activeArthurType
	deckRank := user.DeckRank
	if found {
		leaderCardID = leader.CardID
		leaderLevel = leader.Level
		leaderFame = leader.Fame
		jobType = deck.JobType
		deckRank = deck.DeckRank
	}
	pvpPoint, _ := a.store.pvpStatus()
	return map[string]any{
		"name": a.store.userName(),
		"lv":   a.store.userLevel(),
		"hp":   hp,
		"atkp": attack,
		"intp": magic,
		"mndp": mind,
		"deck": map[string]any{
			"arthur_type":      activeArthurType,
			"is_burst":         a.store.arthurBurstUnlocked(activeArthurType),
			"job_type":         jobType,
			"deck_rank":        deckRank,
			"leader_cardid":    leaderCardID,
			"leader_card_lv":   leaderLevel,
			"leader_card_fame": leaderFame,
		},
		"comment":         a.store.userComment(),
		"last_login_time": time.Now().Unix(),
		"friend_state":    0,
		"pvp_point":       pvpPoint,
		"deck_honorids": func() []int {
			deckHonorIDs, _ := a.store.honorState()
			return deckHonorIDs
		}(),
	}
}

func (a *API) activeDeckProfile() (
	deckInfo,
	cardInfo,
	int,
	int,
	int,
	int,
	bool,
) {
	cards, decks := a.store.show()
	deck, found := selectPartnerDeck(decks, a.store.activeArthurType())
	if !found {
		return deckInfo{}, cardInfo{}, 0, 0, 0, 0, false
	}
	cardByUniqueID := make(map[int64]cardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	leader, found := partnerLeaderCard(deck, cardByUniqueID)
	if !found {
		return deckInfo{}, cardInfo{}, 0, 0, 0, 0, false
	}
	hp, attack, magic, mind := partnerDeckStats(
		deck,
		cardByUniqueID,
		a.store.jobParameter(deck.JobType),
	)
	return deck, leader, hp, attack, magic, mind, true
}

func (a *API) userProfileShow(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		UserID int `json:"userid"`
	}
	if err := decodeExact(request, []string{"userid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.UserID == a.release.State.User.UserID {
		a.writeProtocol(
			writer,
			map[string]any{"profile": a.localProfilePayload()},
		)
		return
	}
	relations, err := a.localAccountFriendRelations()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	for _, relation := range relations {
		if relation.State.User.UserID != payload.UserID {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			a.writeProtocol(writer, map[string]any{"profile": profilePayload(friend)})
			return
		}
		break
	}
	writeError(writer, http.StatusBadRequest, "unknown profile user ID")
}

func (a *API) storyMainShow(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		CNStory int `json:"cn_story"`
	}
	if err := decodeExact(request, []string{"cn_story"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.CNStory != 0 && payload.CNStory != 1 {
		writeError(writer, http.StatusBadRequest, "cn_story must be zero or one")
		return
	}
	a.writeProtocol(writer, map[string]any{
		"parts": a.store.storyMainState(payload.CNStory == 1),
	})
}

func (a *API) storyMainStart(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		StoryMainID int `json:"story_mainid"`
	}
	if err := decodeExact(request, []string{"story_mainid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.beginMainStory(payload.StoryMainID) {
		writeError(writer, http.StatusBadRequest, "unknown main story ID")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	_, _, hp, _, _, _, _ := a.activeDeckProfile()
	if hp < 1 {
		hp = 1
	}
	a.writeProtocol(writer, map[string]any{
		"seed":                0,
		"hp":                  hp,
		"hp_max":              hp,
		"cost_initial":        3,
		"burst_gauge_initial": 0,
		"hold_max":            5,
	})
}

func (a *API) storyMainEnd(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		IsClear int `json:"is_clear"`
	}
	if err := decodeExact(request, []string{"is_clear"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsClear != 0 && payload.IsClear != 1 {
		writeError(writer, http.StatusBadRequest, "is_clear must be zero or one")
		return
	}
	result, err := a.store.endMainStory(payload.IsClear == 1)
	if err != nil {
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyEndPayload(result))
}

func (a *API) storySubShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	a.writeProtocol(writer, map[string]any{
		"characters": a.store.storySubState(),
	})
}

func (a *API) storySubStart(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		StorySubID int `json:"story_subid"`
	}
	if err := decodeExact(request, []string{"story_subid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.beginSubStory(payload.StorySubID) {
		writeError(writer, http.StatusBadRequest, "unknown or locked sub story ID")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	_, _, hp, _, _, _, _ := a.activeDeckProfile()
	if hp < 1 {
		hp = 1
	}
	a.writeProtocol(writer, map[string]any{
		"seed":                0,
		"hp":                  hp,
		"hp_max":              hp,
		"cost_initial":        3,
		"burst_gauge_initial": 0,
		"hold_max":            5,
	})
}

func (a *API) storySubEnd(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		IsClear int `json:"is_clear"`
	}
	if err := decodeExact(request, []string{"is_clear"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsClear != 0 && payload.IsClear != 1 {
		writeError(writer, http.StatusBadRequest, "is_clear must be zero or one")
		return
	}
	result, err := a.store.endSubStory(payload.IsClear == 1)
	if err != nil {
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyEndPayload(result))
}

func (a *API) storyEndPayload(result presentReceiveResult) map[string]any {
	return map[string]any{
		"user":                           a.userPayload(),
		"clear_rewards":                  battleResultRewardsWire(result.Rewards),
		"new_cards":                      toWireCards(result.Cards),
		"new_stack_cards":                toWireStackCards(result.StackCards),
		"new_items":                      a.itemInfosWire(result.Items),
		"new_sphrs":                      toWireSpheres(result.Spheres),
		"new_buddys":                     toWireBuddies(result.Buddies),
		"is_clear_reward_in_present_box": boolInt(result.InPresentBox),
		"unlock_notice":                  []any{},
	}
}

func (a *API) storyEventShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	a.writeProtocol(writer, map[string]any{
		"events": a.store.storyEventState(),
	})
}

func (a *API) storyEventUnlock(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StorySubID int `json:"story_subid"`
	}
	if err := decodeExact(request, []string{"story_subid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	items, events, err := a.store.unlockEventStory(payload.StorySubID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"user":   a.userPayload(),
		"items":  items,
		"events": events,
	})
}

func (a *API) storyStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StoryBattleID       int    `json:"story_battleid"`
		DeckArthurType      int8   `json:"deck_arthur_type"`
		DeckArthurTypeIndex []int8 `json:"deck_arthur_type_idxs"`
	}
	if err := decodeExact(
		request,
		[]string{"story_battleid", "deck_arthur_type", "deck_arthur_type_idxs"},
		&payload,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.authorizeStoryBattle(
		payload.StoryBattleID,
		payload.DeckArthurType,
		payload.DeckArthurTypeIndex,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func (a *API) presentBoxShow(writer http.ResponseWriter, _ *http.Request) {
	presents, histories := a.store.presentState()
	projectPresentTimes(presents, time.Now().Unix())
	projectPresentTimes(histories, time.Now().Unix())
	a.writeProtocol(writer, map[string]any{
		"presents":  presents,
		"histories": histories,
	})
}

func projectPresentTimes(presents []release.Present, now int64) {
	for i := range presents {
		present := &presents[i]
		if present.IssuedAtUnix > 0 {
			present.AddElapsedSec = uint32(min(max(0, now-present.IssuedAtUnix), math.MaxUint32))
		}
		present.AdminIdempotencyKey = ""
		present.IssuedAtUnix = 0
	}
}

func (a *API) presentReceivePayload(
	result presentReceiveResult,
) map[string]any {
	presentIDs := append([]int64{}, result.PresentID...)
	failedIDs := append([]int64{}, result.FailedID...)
	rewards := make([]any, 0, len(result.Rewards))
	for _, received := range result.Rewards {
		rewards = append(rewards, map[string]any{
			"reward":           received.Reward,
			"uniqid":           received.UniqueID,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		})
	}
	newCards := make([]wireCardInfo, 0, len(result.Cards))
	for _, card := range result.Cards {
		newCards = append(newCards, toWireCard(card))
	}
	return map[string]any{
		"user":               a.userPayload(),
		"result_rewards":     rewards,
		"new_cards":          newCards,
		"new_stack_cards":    toWireStackCards(result.StackCards),
		"new_items":          a.itemInfosWire(result.Items),
		"new_sphrs":          toWireSpheres(result.Spheres),
		"new_buddys":         toWireBuddies(result.Buddies),
		"new_stampids":       []int{},
		"presentid":          presentIDs,
		"failed_presentid":   failedIDs,
		"auto_fusion_result": []any{},
		"auto_loveup_result": []any{},
	}
}

func (a *API) presentBoxRecv(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PresentID int64 `json:"presentid"`
	}
	if err := decodeExact(request, []string{"presentid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.receivePresent(payload.PresentID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(result.PresentID) > 0 && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.presentReceivePayload(result))
}

func (a *API) presentBoxMultiRecv(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		IsCoinReceive int8  `json:"is_coin_recv"`
		ReceiveTypes  []int `json:"receive_types"`
	}
	if err := decodeExact(
		request,
		[]string{"is_coin_recv", "receive_types"},
		&payload,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsCoinReceive != 0 && payload.IsCoinReceive != 1 {
		writeError(writer, http.StatusBadRequest, "invalid coin receive flag")
		return
	}
	result, err := a.store.receivePresents(payload.ReceiveTypes, payload.IsCoinReceive == 1)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(result.PresentID) > 0 && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.presentReceivePayload(result))
}

func (a *API) presentBoxDelete(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PresentID int64 `json:"presentid"`
	}
	if err := decodeExact(request, []string{"presentid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	deleted, err := a.store.deletePresents(payload.PresentID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(deleted) > 0 && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"presentid":        deleted,
		"failed_presentid": []int64{},
	})
}

func (a *API) updateGameOption(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		GameOption struct {
			EnableFlag int `json:"enable_flag"`
		} `json:"game_option"`
	}
	if err := decodeExact(request, []string{"game_option"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.setGameOption(payload.GameOption.EnableFlag) {
		writeError(writer, http.StatusBadRequest, "invalid game option flag")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) updatePushOption(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		Option struct {
			EnableFlag int `json:"enable_flag"`
		} `json:"option"`
	}
	if err := decodeExact(request, []string{"option"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.setPushOption(payload.Option.EnableFlag) {
		writeError(writer, http.StatusBadRequest, "invalid push option flag")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) naviSelect(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		NaviType int8 `json:"navi_type"`
	}
	if err := decodeExact(request, []string{"navi_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.store.selectNavi(payload.NaviType) {
		writeError(writer, http.StatusBadRequest, "unknown navigator ID")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) getNaviShow(writer http.ResponseWriter, _ *http.Request) {
	// Navi names, models and presentation remain client-local in navi.csv. The
	// server supplies the official catalog IDs plus acquisition state from the
	// player save. Historical service prices are absent, so the replaceable local
	// runtime profile owns the crystal price used for unowned entries.
	catalogIDs := a.release.State.User.NaviCatalogIDs
	if len(catalogIDs) == 0 {
		catalogIDs = a.release.State.User.SelectableNaviIDs
	}
	acquired := a.store.naviOwnershipState()
	prices := a.store.naviPrices()
	navigators := make([]map[string]any, len(catalogIDs))
	for index, naviID := range catalogIDs {
		setting := prices[naviID]
		isGet := 0
		getType := 4
		notice := "暂未开放购买"
		orgValue := 0
		getValue := 0
		if _, exists := acquired[naviID]; exists {
			isGet = 1
			getType = 0
			notice = ""
		} else if setting.Enabled {
			getType = 0
			orgValue = setting.Price
			getValue = setting.Price
			notice = ""
		}
		navigators[index] = map[string]any{
			"navi_id":      naviID,
			"order":        len(catalogIDs) - index,
			"is_hot":       0,
			"get_type":     getType,
			"org_value":    orgValue,
			"get_value":    getValue,
			"begin_time":   0,
			"end_time":     0,
			"exchangeitem": 0,
			"now":          0,
			"is_get":       isGet,
			"notice":       notice,
		}
	}
	a.writeProtocol(writer, map[string]any{"NaviList": navigators})
}

func (a *API) itemShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemType int `json:"item_type"`
	}
	if err := decodeExact(request, []string{"item_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"items":  a.itemInfosWire(a.store.itemState()),
		"gachas": []any{},
	})
}

func (a *API) itemUse(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemID int `json:"itemid"`
	}
	if err := decodeExact(request, []string{"itemid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.useItem(payload.ItemID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	item := a.itemInfosWire([]release.Item{result.Item})[0]
	a.writeProtocol(writer, map[string]any{
		"item":        item,
		"ap":          result.AP.Current,
		"ap_max":      result.AP.Max,
		"ap_next_sec": result.AP.NextSeconds,
		"bp":          result.BP.Current,
		"bp_max":      result.BP.Max,
		"bp_next_sec": result.BP.NextSeconds,
	})
}

func (a *API) itemExchange(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemID     int `json:"itemid"`
		ChangeSets int `json:"change_sets"`
	}
	if err := decodeExact(request, []string{"itemid", "change_sets"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.exchangeItem(payload.ItemID, payload.ChangeSets)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	rewards := make([]any, len(result.Reward.Rewards))
	for index, received := range result.Reward.Rewards {
		rewards[index] = map[string]any{
			"reward":           received.Reward,
			"uniqid":           received.UniqueID,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		}
	}
	a.writeProtocol(writer, map[string]any{
		"result_rewards":           rewards,
		"user":                     a.userPayload(),
		"new_cards":                toWireCards(result.Reward.Cards),
		"new_stack_cards":          toWireStackCards(result.Reward.StackCards),
		"new_items":                a.itemInfosWire(result.Reward.Items),
		"new_sphrs":                toWireSpheres(result.Reward.Spheres),
		"new_buddys":               toWireBuddies(result.Reward.Buddies),
		"use_item":                 a.itemInfosWire([]release.Item{result.Item})[0],
		"is_reward_in_present_box": 0,
	})
}

func (a *API) itemLackTips(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Index int `json:"idx"`
	}
	if err := decodeExact(request, []string{"idx"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, err := a.store.itemLackTipState(payload.Index)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"title":        profile.Title,
		"desc":         profile.Description,
		"way":          profile.Way,
		"txt_url_info": profile.TextURLs,
	})
}

func (a *API) itemShopShow(writer http.ResponseWriter, _ *http.Request) {
	tabs, owned := a.store.itemShopState()
	a.writeProtocol(writer, map[string]any{
		"tabs": itemShopTabsWire(tabs, owned),
		"user": a.userPayload(),
	})
}

func (a *API) itemShopBuy(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LineupID int `json:"item_shop_lineupid"`
		BuyNum   int `json:"buy_num"`
		PopupID  int `json:"popupid"`
	}
	if err := decodeExact(request, []string{"item_shop_lineupid", "buy_num", "popupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	updated, err := a.store.buyItemShop(payload.LineupID, payload.BuyNum)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	tabs, owned := a.store.itemShopState()
	coin, coinFree := a.store.coinState()
	a.writeProtocol(writer, map[string]any{
		"items":                  a.itemInfosWire(updated),
		"new_stampids":           []int{},
		"pay_item":               []any{},
		"is_item_in_present_box": 0,
		"user":                   a.userPayload(),
		"tabs":                   itemShopTabsWire(tabs, owned),
		"coin":                   coin,
		"coin_free":              coinFree,
	})
}

func (a *API) eventShopShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		EventID int `json:"eventid"`
	}
	if err := decodeExact(request, []string{"eventid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	shop, err := a.store.eventShopState(payload.EventID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{"shops": eventShopsWire(shop)})
}

func (a *API) eventShopBuy(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LineupID int `json:"event_shop_lineupid"`
	}
	if err := decodeExact(request, []string{"event_shop_lineupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.buyEventShop(payload.LineupID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"res_code":        0,
		"res_str":         "",
		"use_item":        a.itemInfosWire([]release.Item{result.UseItem})[0],
		"buy_lineup_name": result.LineupName,
		"buy_price":       result.Price,
		"shops":           eventShopsWire(result.Shop),
	})
}

func (a *API) tradeShopShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, map[string]any{
		"shops": tradeShopsWire(a.store.tradeShopState()),
	})
}

func (a *API) tradeShopLineupShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TradeShopID int `json:"trade_shopid"`
	}
	if err := decodeExact(request, []string{"trade_shopid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	found := false
	for _, shop := range a.store.tradeShopState() {
		if shop.Profile.TradeShopID == payload.TradeShopID {
			found = true
			break
		}
	}
	if !found {
		writeError(writer, http.StatusBadRequest, "trade shop is unavailable")
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) tradeShopBuy(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || (len(fields) != 2 && len(fields) != 3) {
		writeError(writer, http.StatusBadRequest, "request fields differ from the endpoint contract")
		return
	}
	if _, exists := fields["lineupid"]; !exists {
		writeError(writer, http.StatusBadRequest, "request is missing lineupid")
		return
	}
	if _, exists := fields["num"]; !exists {
		writeError(writer, http.StatusBadRequest, "request is missing num")
		return
	}
	if len(fields) == 3 {
		if _, exists := fields["uniqids"]; !exists {
			writeError(writer, http.StatusBadRequest, "request fields differ from the endpoint contract")
			return
		}
	}
	var payload struct {
		LineupID int     `json:"lineupid"`
		UniqueID []int64 `json:"uniqids"`
		Num      int     `json:"num"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "decode trade shop purchase")
		return
	}
	result, err := a.store.buyTradeShop(payload.LineupID, payload.Num, payload.UniqueID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"buy_lineup_name": result.LineupName,
		"shops":           tradeShopsWire(result.Shops),
		"decks":           toWireDecks(result.Decks),
		"user":            a.userPayload(),
	})
}

func (a *API) gachaShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ShowType int `json:"show_type"`
	}
	if err := decodeExact(request, []string{"show_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.ShowType != 0 && payload.ShowType != 1 {
		writeError(writer, http.StatusBadRequest, "invalid gacha show type")
		return
	}
	gachas := a.store.gachaState()
	a.writeProtocol(writer, map[string]any{
		"gacha_category_list":   gachaCategories(gachas),
		"gacha_list":            a.gachaInfos(gachas),
		"is_valid_lineup_cache": 0,
		"user":                  a.userPayload(),
	})
}

func (a *API) gachaPlay(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID      int              `json:"gachaid"`
		PayType      int              `json:"pay_type"`
		GachaHash    string           `json:"gacha_hash"`
		SelectLineup []release.Reward `json:"select_lineup_list"`
		PopupID      int              `json:"popupid"`
	}
	if err := decodeExact(request, []string{
		"gachaid", "pay_type", "gacha_hash", "select_lineup_list", "popupid",
	}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.GachaHash == "" {
		writeError(writer, http.StatusBadRequest, "gacha hash is empty")
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, errGachaUnavailable)
		return
	}
	result, err := a.store.playGacha(payload.GachaID, payload.PayType, payload.SelectLineup)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	rewards := make([]any, len(result.Reward.Rewards))
	rewardAdds := make([]int, len(result.Reward.Rewards))
	for index, received := range result.Reward.Rewards {
		if received.InPresentBox {
			rewardAdds[index] = 1
		}
		uniqueIDs := received.UniqueID
		if len(uniqueIDs) == 0 && !received.InPresentBox {
			// GachaResult reads uniqid[0] for direct rewards, including items
			// without an instance ID. Its native null-array fallback is ID 0.
			uniqueIDs = []int64{0}
		}
		rewards[index] = map[string]any{
			"reward":           received.Reward,
			"uniqid":           uniqueIDs,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		}
	}
	a.writeProtocol(writer, map[string]any{
		"cards":              toWireCards(result.Reward.Cards),
		"stack_cards":        toWireStackCards(result.Reward.StackCards),
		"sphrs":              toWireSpheres(result.Reward.Spheres),
		"new_items":          a.itemInfosWire(gachaItemDeltas(result.Reward)),
		"buddys":             toWireBuddies(result.Reward.Buddies),
		"user":               a.userPayload(),
		"item":               a.itemInfosWire([]release.Item{result.Item})[0],
		"gacha_list":         a.gachaInfos(result.Gachas),
		"gift_direct":        result.Gifts,
		"gift_present":       result.PresentGifts,
		"rewards":            rewards,
		"reward_adds":        rewardAdds,
		"expectancy":         result.Expectancy,
		"cutin":              []any{},
		"auto_fusion_result": []any{},
		"auto_loveup_result": []any{},
	})
}

const cnGachaItemHashSalt = "R-.m(NjG8!-v"

func (a *API) gachaItemPlay(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemID    int    `json:"itemid"`
		PlayCount int    `json:"play_count"`
		GachaHash string `json:"gacha_hash"`
	}
	if err := decodeExact(request, []string{"itemid", "play_count", "gacha_hash"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	hashInput := strconv.Itoa(a.release.State.User.UserID) + cnGachaItemHashSalt
	hash := sha1.Sum([]byte(hashInput))
	if payload.GachaHash != base64.StdEncoding.EncodeToString(hash[:]) {
		writeError(writer, http.StatusBadRequest, "invalid item gacha hash")
		return
	}
	result, err := a.store.playItemGacha(payload.ItemID, payload.PlayCount)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	rewards, presentBoxCards := []any{}, []any{}
	for _, received := range result.Reward.Rewards {
		entry := map[string]any{
			"reward":           received.Reward,
			"uniqid":           received.UniqueID,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		}
		if received.InPresentBox {
			presentBoxCards = append(presentBoxCards, entry)
		} else {
			rewards = append(rewards, entry)
		}
	}
	a.writeProtocol(writer, map[string]any{
		"cards":             toWireCards(result.Reward.Cards),
		"stack_cards":       toWireStackCards(result.Reward.StackCards),
		"sphrs":             toWireSpheres(result.Reward.Spheres),
		"new_items":         a.itemInfosWire(gachaItemDeltas(result.Reward)),
		"buddys":            toWireBuddies(result.Reward.Buddies),
		"user":              a.userPayload(),
		"item":              a.itemInfosWire([]release.Item{result.Item})[0],
		"gacha_list":        []any{},
		"gift_present":      []any{},
		"rewards":           rewards,
		"present_box_cards": presentBoxCards,
	})
}

func (a *API) gachaLineupShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaIDs []int `json:"gachaids"`
		PopupID  int   `json:"popupid"`
	}
	if err := decodeExact(request, []string{"gachaids", "popupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	selected := make(map[int]struct{}, len(payload.GachaIDs))
	for _, gachaID := range payload.GachaIDs {
		if !gachaPublished(request, gachaID) {
			a.writeStoreError(writer, errGachaUnavailable)
			return
		}
		selected[gachaID] = struct{}{}
	}
	gachas, ownedCardIDs := a.store.gachaStateWithOwnership()
	lineups := make([]any, 0, len(selected))
	for _, gacha := range gachas {
		if _, exists := selected[gacha.GachaID]; !exists {
			continue
		}
		pool := gachaPoolRewards(gacha)
		cards := make([]any, len(pool))
		for index, reward := range pool {
			isNew := int8(0)
			if reward.Type == 6 {
				isNew = cardNewFlag(ownedCardIDs, reward.RewardTypeID)
			}
			cards[index] = map[string]any{
				"prize":  reward,
				"is_new": isNew,
			}
		}
		lineups = append(lineups, map[string]any{
			"gachaid":           gacha.GachaID,
			"gacha_lineup_list": cards,
		})
	}
	a.writeProtocol(writer, map[string]any{"gachas": lineups})
}

func (a *API) gachaSelectLineupShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID int `json:"gachaid"`
	}
	if err := decodeExact(request, []string{"gachaid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, errGachaUnavailable)
		return
	}
	lineup, err := a.store.gachaSelectLineup(payload.GachaID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"gachaid":           payload.GachaID,
		"gacha_lineup_list": gachaLineupEntriesWire(lineup),
	})
}

func (a *API) gachaSelectedListShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID int `json:"gachaid"`
	}
	if err := decodeExact(request, []string{"gachaid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, errGachaUnavailable)
		return
	}
	lineup, err := a.store.gachaSelectedLineup(payload.GachaID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"gachaid":           payload.GachaID,
		"gacha_lineup_list": gachaLineupEntriesWire(lineup),
	})
}

func gachaLineupEntriesWire(lineup []gachaLineupEntry) []any {
	result := make([]any, len(lineup))
	for index, entry := range lineup {
		result[index] = map[string]any{
			"prize":  entry.Prize,
			"is_new": entry.IsNew,
		}
	}
	return result
}

const gachaOddsScale = 100 * 100000

func (a *API) gachaOddsShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID int `json:"gachaid"`
		PopupID int `json:"popupid"`
	}
	if err := decodeExact(request, []string{"gachaid", "popupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.GachaID <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid gacha ID")
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, errGachaUnavailable)
		return
	}
	gachas, ownedCardIDs := a.store.gachaStateWithOwnership()
	var selected *release.GachaProfile
	for index := range gachas {
		if gachas[index].GachaID == payload.GachaID {
			selected = &gachas[index]
			break
		}
	}
	if selected == nil {
		writeError(writer, http.StatusBadRequest, "unknown or unavailable gacha")
		return
	}
	stages, err := a.store.gachaOddsStages(*selected)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	lineups := make([]any, 0, len(stages))
	for _, stage := range stages {
		prizes := make([]any, len(stage.CardIDs))
		for i, id := range stage.CardIDs {
			prizes[i] = map[string]any{"prize": gachaCardReward(id), "is_new": cardNewFlag(ownedCardIDs, id), "odds": stage.Odds[i]}
		}
		if len(stage.Rewards) > 0 {
			prizes = make([]any, len(stage.Rewards))
			for i, reward := range stage.Rewards {
				isNew := int8(0)
				if reward.Type == 6 {
					isNew = cardNewFlag(ownedCardIDs, reward.RewardTypeID)
				}
				prizes[i] = map[string]any{"prize": reward, "is_new": isNew, "odds": stage.Odds[i]}
			}
		}
		lineups = append(lineups, map[string]any{"lineup_name": stage.Name, "select_num": stage.DrawCount, "prize_list": prizes})
	}
	message := "当前概率按本地配置权重计算。"
	if selected.UserSelectMax > 0 {
		message += "自选后的实际概率按选中卡牌权重重新归一化。"
	}
	a.writeProtocol(writer, map[string]any{"odds_msg": message, "lineup_infos": lineups})
}

func cardNewFlag(ownedCardIDs map[int]struct{}, cardID int) int8 {
	if _, owned := ownedCardIDs[cardID]; owned {
		return 0
	}
	return 1
}

func scaledGachaOdds(weights []int) ([]int, error) {
	if len(weights) == 0 {
		return nil, errors.New("gacha odds have no weights")
	}
	var total int64
	for _, weight := range weights {
		if weight <= 0 || total > int64(^uint64(0)>>1)-int64(weight) {
			return nil, errors.New("invalid gacha odds weight")
		}
		total += int64(weight)
	}
	if total > int64(^uint64(0)>>1)/int64(gachaOddsScale) {
		return nil, errors.New("gacha odds weight total is too large")
	}
	result := make([]int, len(weights))
	var cumulative int64
	var previousScaled int64
	for index, weight := range weights {
		cumulative += int64(weight)
		scaled := cumulative * int64(gachaOddsScale) / total
		result[index] = int(scaled - previousScaled)
		previousScaled = scaled
	}
	return result, nil
}

func gachaCategories(gachas []release.GachaProfile) []any {
	seen := make(map[int]struct{}, len(gachas))
	result := make([]any, 0, len(gachas))
	for _, gacha := range gachas {
		if _, exists := seen[gacha.CategoryNum]; exists {
			continue
		}
		seen[gacha.CategoryNum] = struct{}{}
		result = append(result, map[string]any{
			"category_num": gacha.CategoryNum,
			"pictid":       gacha.CategoryPictID,
		})
	}
	return result
}

func (a *API) gachaInfos(gachas []release.GachaProfile) []any {
	result := make([]any, len(gachas))
	for index, gacha := range gachas {
		gacha = gacha.CurrentStep()
		bannerPath := "/local/gacha/banner.png"
		if gacha.BannerKey == "five_star_ticket" {
			bannerPath = "/local/gacha/five-star-banner.png"
		} else if gacha.BannerKey != "" {
			bannerPath = "/local/gacha/" + gacha.BannerKey + ".png"
		}
		price := gacha.Price
		buyMessage := gacha.BuyMessage
		dailyFirst := int8(0)
		playCountOneDay := 0
		if gacha.DailyFirstAvailable {
			price = 0
			buyMessage = "今日首次友情点扭蛋免费，抽取1张骑士卡牌吗？"
			dailyFirst = 1
		} else if gacha.DailyFirstFree {
			playCountOneDay = 1
		}
		stepCount, isStep, isLast := 0, 0, 0
		if len(gacha.Steps) > 0 {
			stepCount, isStep = min(gacha.PlayCount+1, len(gacha.Steps)), 1
			if stepCount == len(gacha.Steps) {
				isLast = 1
			}
			buyMessage = fmt.Sprintf("第%d阶段，消耗%d，抽取%d次吗？", stepCount, price, gacha.CardNum)
		}
		result[index] = map[string]any{
			"gachaid":                   gacha.GachaID,
			"gacha_name":                gacha.Name,
			"buymsg":                    buyMessage,
			"submsg":                    gacha.SubMessage,
			"category_num":              gacha.CategoryNum,
			"order_num":                 gacha.OrderNum,
			"groupid":                   gacha.GroupID,
			"gacha_type":                gacha.GachaType,
			"arthur_type":               gacha.ArthurType,
			"pay_type":                  gacha.PayType,
			"pay_typeid":                gacha.PayTypeID,
			"price":                     price,
			"card_num":                  gacha.CardNum,
			"card_num_max":              gacha.CardNumMax,
			"play_count":                gacha.PlayCount,
			"play_count_priority_price": 0,
			"play_count_1day":           playCountOneDay,
			"play_count_max":            0,
			"play_count_1day_max":       0,
			"user_select_max":           gacha.UserSelectMax,
			"is_stepup_price":           isStep,
			"is_daily_first":            dailyFirst,
			"image_l_url":               a.baseURL + bannerPath,
			"image_s_url":               a.baseURL + bannerPath,
			"info_url":                  "",
			"end_time":                  gacha.EndTime,
			"gifts":                     gachaGiftsWire(gacha),
			"fate_player_number":        0,
			"expensive_price":           0,
			"is_odds_view":              1,
			"stepup_count":              stepCount,
			"is_last_step":              isLast,
		}
	}
	return result
}

func (a *API) itemInfosWire(items []release.Item) []any {
	profiles := a.store.itemExchangeProfileState()
	result := make([]any, len(items))
	for index, item := range items {
		exchange := map[string]any{
			"is_appear_event": 0,
			"eventid":         0,
			"need_num":        0,
			"reward": release.Reward{
				CardSkillLevels: []int16{},
			},
		}
		if profile, exists := profiles[item.ItemID]; exists {
			exchange = map[string]any{
				"is_appear_event": profile.IsAppearEvent,
				"eventid":         profile.EventID,
				"need_num":        profile.NeedNum,
				"reward":          profile.Reward,
			}
		}
		result[index] = map[string]any{
			"itemid":     item.ItemID,
			"num":        item.Num,
			"limit_time": item.LimitTime,
			"exchange":   exchange,
		}
	}
	return result
}

func itemShopTabsWire(tabs []release.ItemShopTab, owned map[int]int) []any {
	result := make([]any, len(tabs))
	for tabIndex, tab := range tabs {
		lineups := make([]any, 0, len(tab.Lineup))
		for _, lineup := range tab.Lineup {
			if lineup.Hidden || lineup.Disabled {
				continue
			}
			interiors := make([]any, len(lineup.Interiors))
			for interiorIndex, interior := range lineup.Interiors {
				interiors[interiorIndex] = map[string]any{
					"buy_type":   interior.BuyType,
					"buy_typeid": interior.BuyTypeID,
					"num":        interior.Num,
					"own_num":    owned[interior.BuyTypeID],
				}
			}
			lineups = append(lineups, map[string]any{
				"item_shop_lineupid": lineup.LineupID,
				"pictid":             lineup.PictID,
				"lineup_name":        lineup.LineupName,
				"pay_type":           lineup.PayType,
				"pay_typeid":         lineup.PayTypeID,
				"price":              lineup.Price,
				"stock_num":          lineup.StockNum,
				"stock_remain_num":   lineup.StockRemain,
				"stock_type":         lineup.StockType,
				"appear_end":         lineup.AppearEnd,
				"note":               lineup.Note,
				"buy_num_max":        lineup.BuyNumMax,
				"interiors":          interiors,
			})
		}
		result[tabIndex] = map[string]any{
			"tab_type": tab.TabType,
			"lineup":   lineups,
		}
	}
	return result
}

func eventShopsWire(shop eventShopState) []any {
	lineups := make([]any, len(shop.Lineups))
	for index, state := range shop.Lineups {
		lineup := state.Profile
		lineups[index] = map[string]any{
			"event_shop_lineupid": lineup.LineupID,
			"lineup_name":         lineup.Name,
			"image_url":           lineup.ImageURL,
			"price":               lineup.Price,
			"stock_num":           lineup.StockNum,
			"stock_remain_num":    state.StockRemain,
			"stock_type":          lineup.StockType,
			"is_feature":          lineup.IsFeature,
		}
	}
	return []any{map[string]any{
		"eventid":        shop.EventID,
		"point_itemid":   shop.PointItemID,
		"point_item_num": shop.PointItemNum,
		"lineups":        lineups,
	}}
}

func tradeShopsWire(shops []tradeShopState) []any {
	result := make([]any, len(shops))
	for shopIndex, state := range shops {
		profile := state.Profile
		lineups := make([]any, len(state.Lineups))
		for lineupIndex, lineupState := range state.Lineups {
			lineup := lineupState.Profile
			lineups[lineupIndex] = map[string]any{
				"lineupid":         lineup.LineupID,
				"lineup_name":      lineup.LineupName,
				"stock_num":        lineup.StockNum,
				"stock_remain_num": lineupState.StockRemain,
				"is_lineup_new":    lineup.IsLineupNew,
				"is_lineup_old":    lineup.IsLineupOld,
				"is_new":           lineupState.IsNew,
				"pictid":           lineup.PictID,
				"prices":           lineup.Prices,
				"reward":           lineup.Rewards,
			}
		}
		result[shopIndex] = map[string]any{
			"trade_shopid": profile.TradeShopID,
			"name":         profile.Name,
			"text":         profile.Text,
			"shop_type":    profile.ShopType,
			"tab_type":     profile.TabType,
			"end_time":     profile.EndTime,
			"is_new":       profile.IsNew,
			"pictid":       profile.PictID,
			"lineups":      lineups,
			"owns":         state.Owns,
		}
	}
	return result
}

func (a *API) stampShow(writer http.ResponseWriter, _ *http.Request) {
	stampIDs, deckStampIDs := a.store.stampState()
	a.writeProtocol(writer, map[string]any{
		"stampids": stampIDs,
		"deck": map[string]any{
			"stampids": deckStampIDs,
		},
	})
}

func (a *API) stampDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Deck struct {
			StampIDs []int `json:"stampids"`
		} `json:"deck"`
	}
	if err := decodeExact(request, []string{"deck"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.setStampDeck(payload.Deck.StampIDs); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) ping(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Stamp int `json:"stamp"`
	}
	if err := decodeExact(request, []string{"stamp"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]int{"stamp": payload.Stamp})
}

func (a *API) writeProtocol(writer http.ResponseWriter, method any) {
	a.writeProtocolWithPopups(writer, method, []release.PopupProfile{})
}

func (a *API) writeProtocolResult(
	writer http.ResponseWriter,
	method any,
	resultCode int,
	resultString string,
) {
	a.writeProtocolResultWithPopups(
		writer,
		method,
		[]release.PopupProfile{},
		resultCode,
		resultString,
	)
}

func (a *API) writeProtocolWithPopups(
	writer http.ResponseWriter,
	method any,
	popups []release.PopupProfile,
) {
	a.writeProtocolResultWithPopups(writer, method, popups, 0, "")
}

func (a *API) writeProtocolResultWithPopups(
	writer http.ResponseWriter,
	method any,
	popups []release.PopupProfile,
	resultCode int,
	resultString string,
) {
	a.writeProtocolResponse(writer, method, popups, resultCode, resultString, 0)
}

func (a *API) writeProtocolResponse(
	writer http.ResponseWriter, method any, popups []release.PopupProfile,
	resultCode int, resultString string, resultAction int,
) {
	common := newCommonResponse(a.store.unlockedFeatureState())
	common.ResultCode = resultCode
	common.ResultString = resultString
	common.ResultErrorAction = resultAction
	common.Revision = a.release.State.CatalogVersion
	if len(common.Notifications) == 1 {
		presents, _ := a.store.presentState()
		common.Notifications[0].PresentNum =
			int16(len(presents))
		_, pvpChallenge := a.store.pvpStatus()
		common.Notifications[0].Challenge = pvpChallenge
		common.Notifications[0].PVPReset.StartTime = a.pvpConfig.ResetStartTime
		common.Notifications[0].PVPReset.EndTime = a.pvpConfig.ResetEndTime
		if a.store.missionState() {
			common.Notifications[0].MissionClearReceive = 1
		}
	}
	body, err := marshalProtocolWithPopups(common, method, popups)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode response")
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (a *API) servePublicFile(
	writer http.ResponseWriter,
	request *http.Request,
	relative string,
) {
	filename, err := a.release.PublicFile(relative)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "unsafe release path")
		return
	}
	file, err := os.Open(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(writer, http.StatusNotFound, "release file not found")
			return
		}
		writeError(writer, http.StatusInternalServerError, "open release file")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(writer, http.StatusInternalServerError, "stat release file")
		return
	}
	contentType := mime.TypeByExtension(path.Ext(relative))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", contentType)
	http.ServeContent(
		writer,
		request,
		path.Base(relative),
		info.ModTime(),
		file,
	)
}

func decodeFlexibleFields(
	request *http.Request,
	expected []string,
) (map[string]json.RawMessage, error) {
	body, err := readBody(request)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	if len(fields) != len(expected) {
		return nil, errors.New("request fields differ from the endpoint contract")
	}
	result := make(map[string]json.RawMessage, len(expected))
	for index, name := range expected {
		raw, exists := fields[name]
		if !exists {
			raw, exists = fields[strconv.Itoa(index)]
		}
		if !exists {
			return nil, fmt.Errorf("request is missing %s", name)
		}
		result[name] = raw
	}
	return result, nil
}

func decodeStackUses(raw json.RawMessage) ([]release.CardStackUse, error) {
	if string(raw) == "null" {
		return []release.CardStackUse{}, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, errors.New("stack-card selection must be an array")
	}
	counts := make(map[int]int, len(entries))
	order := make([]int, 0, len(entries))
	for _, entry := range entries {
		use := release.CardStackUse{Num: 1}
		if json.Unmarshal(entry, &use.CardID) != nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(entry, &fields) != nil {
				return nil, errors.New("stack-card entry must be an object")
			}
			cardRaw, cardExists := fields["cardid"]
			if !cardExists {
				cardRaw, cardExists = fields["0"]
			}
			numRaw, numExists := fields["num"]
			if !numExists {
				numRaw, numExists = fields["1"]
			}
			use.Num = 0
			if !cardExists || !numExists ||
				json.Unmarshal(cardRaw, &use.CardID) != nil ||
				json.Unmarshal(numRaw, &use.Num) != nil {
				return nil, errors.New("stack-card entry fields differ")
			}
		}
		// Validate each quantity before merging; negative entries or overflow
		// must not turn an invalid selection into a small, sellable total.
		if use.CardID <= 0 || use.Num <= 0 || counts[use.CardID] > math.MaxInt-use.Num {
			return nil, errors.New("invalid stack-card quantity")
		}
		if _, exists := counts[use.CardID]; !exists {
			order = append(order, use.CardID)
		}
		counts[use.CardID] += use.Num
	}
	result := make([]release.CardStackUse, 0, len(order))
	for _, cardID := range order {
		result = append(result, release.CardStackUse{CardID: cardID, Num: counts[cardID]})
	}
	return result, nil
}

func decodeExact(
	request *http.Request,
	expected []string,
	target any,
) error {
	body, err := readBody(request)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return fmt.Errorf("decode JSON object: %w", err)
	}
	if len(fields) != len(expected) {
		return errors.New("request fields differ from the endpoint contract")
	}
	for _, name := range expected {
		if _, exists := fields[name]; !exists {
			return fmt.Errorf("request is missing %s", name)
		}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

func readBody(request *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, maxRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request: %w", err)
	}
	if len(body) > maxRequestBytes {
		return nil, errors.New("request exceeds one MiB")
	}
	return body, nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, "encode response", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]any{
		"res_code": status,
		"res_str":  message,
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (a *API) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		capture := &statusWriter{
			ResponseWriter: writer,
			status:         http.StatusOK,
		}
		next.ServeHTTP(capture, request)
		a.logger.Info(
			"http request",
			"request_id", middleware.GetReqID(request.Context()),
			"method", request.Method,
			"path", request.URL.Path,
			"status", capture.status,
			"elapsed_ms", time.Since(started).Milliseconds(),
		)
	})
}
