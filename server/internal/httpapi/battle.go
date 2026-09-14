package httpapi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

// LOCAL_POLICY: the current published groups use searchid=0. Keep the menu
// labels in configuration; do not invent additional official categories.
//
//go:embed team_battle_search_options.json
var teamBattleSearchOptions json.RawMessage

type teamBattlePartnerDeckSelect struct {
	UserID     int  `json:"userid"`
	ArthurType int8 `json:"arthur_type"`
	DeckIndex  int8 `json:"deck_idx"`
}

type localTeamBattleDeckProjection struct {
	ArthurType  int8
	UserID      int
	PartnerDeck map[string]any
	FriendState int
}

func (a *API) teamBattleRecommendDeckShow(writer http.ResponseWriter, _ *http.Request) {
	recommendations := make([]release.TeamBattleRecommendation, len(a.release.State.TeamBattleRecommendations))
	for index, recommendation := range a.release.State.TeamBattleRecommendations {
		recommendation.RecommendIDs = append([]int(nil), recommendation.RecommendIDs...)
		recommendations[index] = recommendation
	}
	if len(recommendations) == 0 {
		writeError(writer, http.StatusInternalServerError, "team battle recommendations are unavailable")
		return
	}
	a.writeProtocol(writer, map[string]any{"recommends": recommendations})
}

func (a *API) teamBattlePastBossShow(writer http.ResponseWriter, _ *http.Request) {
	if len(a.release.State.TeamBattlePastBossGroups) == 0 {
		writeError(writer, http.StatusInternalServerError, "team battle past-boss archive is unavailable")
		return
	}
	partnerArthurs, err := a.localTeamBattlePartnerArthurs()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	numericArthurs, err := teamBattlePartnerArthursNumeric(partnerArthurs)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := pastBossProgress(a.release.State.TeamBattlePastBossGroups, a.store.teamBattleSoloState())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"0": groups,
		"1": numericArthurs,
		"2": 0,
		"3": 0,
	})
}

// Archive metadata is shared; clear state belongs to the requesting account.
func pastBossProgress(source []json.RawMessage, progress json.RawMessage) ([]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(progress, &top); err != nil {
		return nil, err
	}
	states := map[int]int{}
	for _, key := range []string{"10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		var groups []struct {
			Bosses []struct {
				ID    int `json:"0"`
				State int `json:"10"`
			} `json:"10"`
		}
		if err := json.Unmarshal(top[key], &groups); err != nil {
			return nil, err
		}
		for _, group := range groups {
			for _, boss := range group.Bosses {
				if boss.State > states[boss.ID] {
					states[boss.ID] = boss.State
				}
			}
		}
	}
	result := make([]json.RawMessage, len(source))
	for i, raw := range source {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, err
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group["13"], &bosses); err != nil {
			return nil, err
		}
		for _, boss := range bosses {
			var id int
			if err := json.Unmarshal(boss["0"], &id); err != nil {
				return nil, err
			}
			boss["10"], _ = json.Marshal(states[id])
		}
		group["13"], _ = json.Marshal(bosses)
		result[i], _ = json.Marshal(group)
	}
	return result, nil
}

// teamBattleClearDeckShow projects the single local account population into
// the original client's read-only clear-deck browser. The current Arthur is
// the player account; the other three Arthurs keep the same stable local-AI
// identities used by solo TeamBattle partner selection. This is deliberately
// not presented as an official ranking or a remote-player archive.
func (a *API) teamBattleClearDeckShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID int `json:"bossid"`
	}
	if err := decodeExact(request, []string{"bossid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if _, found := teamBattleReplayForBoss(a.release.State.TeamBattleReplays, payload.BossID); !found {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	entries, err := a.localTeamBattleDeckProjections()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	partnerDecks := make([]any, 0, 4)
	friendStates := make([]any, 0, 4)
	for _, entry := range entries {
		partnerDecks = append(partnerDecks, entry.PartnerDeck)
		friendStates = append(friendStates, map[string]any{
			"userid":       entry.UserID,
			"friend_state": entry.FriendState,
		})
	}

	a.writeProtocol(writer, map[string]any{
		"partner_deck":      partnerDecks,
		"how_to_list":       []any{},
		"friend_state_list": friendStates,
	})
}

func (a *API) localTeamBattleDeckProjections() ([]localTeamBattleDeckProjection, error) {
	cards, decks := a.store.show()
	cardByUniqueID := make(map[int64]cardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	sphereByUniqueID := make(map[int64]release.Sphere)
	for _, sphere := range a.store.sphereState() {
		sphereByUniqueID[sphere.UniqueID] = sphere
	}
	buddyByUniqueID := make(map[int64]release.Buddy)
	for _, buddy := range a.store.buddyState() {
		buddyByUniqueID[buddy.UniqueID] = buddy
	}
	avatars := a.store.avatarsState()
	_, supportUnlocks := a.store.supportDeckState()
	if len(avatars) != 4 || len(supportUnlocks) != 4 {
		return nil, errors.New("team battle deck projection state is unavailable")
	}
	activeArthurType := a.store.activeArthurType()
	if activeArthurType < 1 || activeArthurType > 4 {
		return nil, errors.New("active Arthur type is invalid")
	}

	entries := make([]localTeamBattleDeckProjection, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		deck, found := selectCompletePartnerDeck(decks, arthurType, cardByUniqueID)
		if !found {
			return nil, errors.New("team battle clear deck is incomplete")
		}
		userID := a.release.State.User.UserID*10 + int(arthurType)
		friendState := 0
		if arthurType == activeArthurType {
			userID = a.release.State.User.UserID
			friendState = 4 // FRIEND_STATE.MYSELF
		}
		partnerDeck, err := teamBattlePartnerDeckWire(
			userID,
			arthurType,
			deck,
			cardByUniqueID,
			a.store.jobParameter(deck.JobType),
			avatars[int(arthurType)-1],
			sphereByUniqueID,
			buddyByUniqueID,
			supportUnlocks[arthurType-1],
			a.store.arthurBurstUnlocked(arthurType),
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, localTeamBattleDeckProjection{
			ArthurType:  arthurType,
			UserID:      userID,
			PartnerDeck: partnerDeck,
			FriendState: friendState,
		})
	}
	return entries, nil
}

// dailyClearRankShow exposes one deterministic local-account rank row. The
// client expects four aligned deck lists: solo view merges the same row index
// into a four-Arthur clear deck, while multi view selects one list by job.
// There is no recovered public ranking population or retired daily score
// store, so rank 1 is an explicit single-account local projection.
func (a *API) dailyClearRankShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID  int `json:"bossid"`
		IsMulti int `json:"is_multi"`
	}
	if err := decodeExact(request, []string{"bossid", "is_multi"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsMulti != 0 && payload.IsMulti != 1 {
		writeError(writer, http.StatusBadRequest, "invalid daily-clear ranking mode")
		return
	}
	if _, found := teamBattleReplayForBoss(a.release.State.TeamBattleReplays, payload.BossID); !found {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}
	entries, err := a.localTeamBattleDeckProjections()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if payload.IsMulti == 0 {
		activeArthurType := a.store.activeArthurType()
		ordered := make([]localTeamBattleDeckProjection, 0, len(entries))
		for _, entry := range entries {
			if entry.ArthurType == activeArthurType {
				ordered = append(ordered, entry)
				break
			}
		}
		for _, entry := range entries {
			if entry.ArthurType != activeArthurType {
				ordered = append(ordered, entry)
			}
		}
		entries = ordered
	}

	deckHonorIDs, _ := a.store.honorState()
	playerName := a.store.userName()
	deckLists := make([]any, 0, len(entries))
	friendStates := make([]any, 0, len(entries))
	for _, entry := range entries {
		deckLists = append(deckLists, map[string]any{
			"ranks": []any{map[string]any{
				"userid":        entry.UserID,
				"name":          playerName,
				"rank":          1,
				"partner_deck":  entry.PartnerDeck,
				"deck_honorids": append([]int(nil), deckHonorIDs...),
			}},
		})
		friendStates = append(friendStates, map[string]any{
			"userid":       entry.UserID,
			"friend_state": entry.FriendState,
		})
	}
	a.writeProtocol(writer, map[string]any{
		"deck":              deckLists,
		"how_to_list":       []any{},
		"friend_state_list": friendStates,
	})
}

// challengeShow closes the original client's optional challenge-detail
// preflight without inventing retired live-service content. The immutable CN
// container has no challenge condition, reward, or schedule master, and every
// currently published boss carries an empty challenge progress list. ProtoGen
// deliberately leaves infos nil when the wire array is empty, so the original
// stage, room, and pause-window callbacks keep the challenge window closed.
func (a *API) challengeShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID     int `json:"bossid"`
		TargetTime int `json:"target_time"`
	}
	if err := decodeExact(request, []string{"bossid", "target_time"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.TargetTime < 0 {
		writeError(writer, http.StatusBadRequest, "invalid challenge target time")
		return
	}
	if _, found := teamBattleReplayForBoss(a.release.State.TeamBattleReplays, payload.BossID); !found ||
		!teamBattleSoloHasBoss(a.store.teamBattleSoloState(), payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}
	a.writeProtocol(writer, map[string]any{"infos": []any{}})
}

// All stages require this preflight; only configured score stages expose rewards.
func (a *API) teamBattleScoreRewardLineup(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID int `json:"bossid"`
	}
	if err := decodeExact(request, []string{"bossid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if _, found := teamBattleReplayForBoss(a.release.State.TeamBattleReplays, payload.BossID); !found ||
		!teamBattleSoloHasBoss(a.store.teamBattleSoloState(), payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	highScore, rewards := a.store.scoreLineup(payload.BossID, a.release.State.TeamBattleRewards)
	a.writeProtocol(writer, map[string]any{"high_score": highScore, "rewards": rewards})
}

func (a *API) userBuffExec(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserBuffID int `json:"user_buff_id"`
	}
	if err := decodeExact(request, []string{"user_buff_id"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.execUserBuff(payload.UserBuffID, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	unlockedBosses := make([]any, 0, len(result.Bosses))
	for _, raw := range result.Bosses {
		var numeric map[string]any
		if err := json.Unmarshal(raw, &numeric); err != nil {
			writeError(writer, http.StatusInternalServerError, "decode unlocked user-buff boss")
			return
		}
		named, err := teamBattleBossNamedDTO(numeric)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		unlockedBosses = append(unlockedBosses, named)
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"unlock_bosses": unlockedBosses,
		"update_items":  []release.Item{result.Item},
	})
}

func (a *API) teamBattleSoloStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID              int                           `json:"bossid"`
		DeckArthurType      int8                          `json:"deck_arthur_type"`
		DeckArthurTypeIndex int8                          `json:"deck_arthur_type_idx"`
		PartnerDeckSelects  []teamBattlePartnerDeckSelect `json:"partner_deck_selects"`
		StartTime           int                           `json:"starttime"`
		Flag                int                           `json:"flag"`
	}
	fields := []string{
		"bossid",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"partner_deck_selects",
		"starttime",
		"flag",
	}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || payload.StartTime <= 0 || payload.Flag < 0 || payload.Flag > 2 ||
		len(payload.PartnerDeckSelects) != 3 {
		writeError(writer, http.StatusBadRequest, "invalid local team battle start")
		return
	}
	replay, found := teamBattleReplayForBoss(a.release.State.TeamBattleReplays, payload.BossID)
	teamBattleSolo := a.store.teamBattleSoloState()
	isTowerBoss := a.store.towerQuestHasBoss(payload.BossID)
	isAccessible := isTowerBoss
	if payload.Flag == 0 {
		isAccessible = isAccessible || teamBattleSoloBossAccessible(teamBattleSolo, payload.BossID, time.Now().Unix())
	} else {
		isAccessible = teamBattlePastBossHasBoss(a.release.State.TeamBattlePastBossGroups, payload.BossID)
	}
	if !found || !isAccessible {
		writeError(writer, http.StatusBadRequest, "unknown team battle replay")
		return
	}
	rules, rulesFound := a.store.teamBattleEntryRulesForBoss(payload.BossID)
	if !rulesFound {
		writeError(writer, http.StatusInternalServerError, "team battle entry rules are unavailable")
		return
	}
	if !rules.allowsSolo() {
		writeError(writer, http.StatusBadRequest, "this battle requires multiplayer")
		return
	}
	battleSegments, err := teamBattleReplayBattles(replay)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}

	cards, decks := a.store.show()
	cardByUniqueID := make(map[int64]cardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	playerDeck, found := exactDeck(decks, payload.DeckArthurType, payload.DeckArthurTypeIndex)
	if !found {
		writeError(writer, http.StatusBadRequest, "selected player deck is unavailable")
		return
	}
	playerLeader, found := partnerLeaderCard(playerDeck, cardByUniqueID)
	if !found || playerLeader.Fame <= 0 || playerLeader.Fame > 100 {
		writeError(writer, http.StatusBadRequest, "selected player leader card is unavailable")
		return
	}
	avatars := a.store.avatarsState()
	if len(avatars) != 4 {
		writeError(writer, http.StatusInternalServerError, "team battle avatars are unavailable")
		return
	}
	var externalViews []friendPointPartnerView
	var ownState release.State
	for _, selection := range payload.PartnerDeckSelects {
		if selection.UserID == a.release.State.User.UserID {
			if ownState.User.UserID == 0 {
				ownState = a.store.snapshot(a.release.State)
			}
		} else {
			if rules.OnlyMyDeck != 0 {
				writeError(writer, http.StatusBadRequest, "this battle requires the player's own decks")
				return
			}
			if externalViews == nil {
				externalViews, err = a.friendPointPartnerViews()
				if err != nil {
					writeError(writer, http.StatusInternalServerError, "list local-account team battle partners")
					return
				}
			}
		}
	}

	seenArthurTypes := map[int8]struct{}{payload.DeckArthurType: {}}
	partnerDecks := make([]any, 0, 3)
	friendPointReward := 0
	rentalCredits := make([]FriendPointRentalCredit, 0, 3)
	selectedResultPartners := make([]teamBattleResultPartner, 0, 3)
	deckHonorIDs, _ := a.store.honorState()
	localHonorIDs := make([]int, 4)
	copy(localHonorIDs, deckHonorIDs)
	selections := append([]teamBattlePartnerDeckSelect(nil), payload.PartnerDeckSelects...)
	sort.Slice(selections, func(left, right int) bool {
		return selections[left].ArthurType < selections[right].ArthurType
	})
	for _, selection := range selections {
		if selection.ArthurType < 1 || selection.ArthurType > 4 || selection.DeckIndex < 0 {
			writeError(writer, http.StatusBadRequest, "invalid partner deck selection")
			return
		}
		if _, exists := seenArthurTypes[selection.ArthurType]; exists {
			writeError(writer, http.StatusBadRequest, "duplicate team battle Arthur type")
			return
		}
		seenArthurTypes[selection.ArthurType] = struct{}{}
		if selection.UserID == a.release.State.User.UserID {
			// The original SetDeckData sends the same account ID for each
			// selected own profession. Reuse the ordinary partner deck DTO,
			// with state SELF at result, without rental points or follow offers.
			view, available := partnerViewFromState(ownState, selection.ArthurType)
			if !available {
				writeError(writer, http.StatusBadRequest, "selected own partner profession is unavailable")
				return
			}
			deck, deckErr := view.deckWire(selection.DeckIndex)
			if deckErr != nil {
				writeError(writer, http.StatusBadRequest, deckErr.Error())
				return
			}
			partnerDecks = append(partnerDecks, deck)
			selectedDeck, _ := exactDeck(view.Decks, view.ArthurType, selection.DeckIndex)
			leader, _ := partnerLeaderCard(selectedDeck, view.Cards)
			selectedResultPartners = append(selectedResultPartners, teamBattleResultPartner{
				IsBurst:       view.IsBurst,
				LastLoginUnix: time.Now().Unix(),
				UserID:        view.UserID, IsSelf: true, Name: view.Name, ArthurType: view.ArthurType,
				Level: view.Level, DeckRank: selectedDeck.DeckRank, LeaderCardID: leader.CardID,
				LeaderLevel: leader.Level, LeaderFame: leader.Fame, Comment: view.Comment,
				PVPPoint: view.PVPPoint, HonorIDs: append([]int(nil), view.HonorIDs...),
			})
			continue
		}
		view, viewFound := findFriendPointPartnerView(externalViews, selection.UserID)
		if !viewFound || view.ArthurType != selection.ArthurType {
			writeError(writer, http.StatusBadRequest, "unknown persistent team battle partner")
			return
		}
		partnerDeck, deckErr := view.deckWire(selection.DeckIndex)
		if deckErr == nil {
			partnerReward := a.store.friendPointRewardForState(view.FriendState)
			friendPointReward += partnerReward
			deck, _ := exactDeck(view.Decks, view.ArthurType, selection.DeckIndex)
			leader, leaderFound := partnerLeaderCard(deck, view.Cards)
			if !leaderFound {
				deckErr = errors.New("selected persistent partner leader is unavailable")
			} else {
				selectedResultPartners = append(selectedResultPartners, teamBattleResultPartner{
					IsBurst:       view.IsBurst,
					LastLoginUnix: view.LastLoginUnix,
					UserID:        view.UserID, Name: view.Name, ArthurType: view.ArthurType,
					Level: view.Level, DeckRank: deck.DeckRank, LeaderCardID: leader.CardID,
					LeaderLevel: leader.Level, LeaderFame: leader.Fame, Comment: view.Comment,
					PVPPoint: view.PVPPoint, HonorIDs: append([]int(nil), view.HonorIDs...),
				})
			}
			rentalCredits = append(rentalCredits, FriendPointRentalCredit{
				OwnerUserID: view.UserID,
				FriendPoint: partnerReward,
			})
		}
		if deckErr != nil {
			writeError(writer, http.StatusInternalServerError, deckErr.Error())
			return
		}
		partnerDecks = append(partnerDecks, partnerDeck)
	}
	if len(seenArthurTypes) != 4 {
		writeError(writer, http.StatusBadRequest, "team battle requires all four Arthur types")
		return
	}
	standaloneBPUse := 0
	if !isTowerBoss {
		standaloneBPUse, found = teamBattleSoloBossBPUse(teamBattleSolo, payload.BossID)
		if !found {
			writeError(writer, http.StatusInternalServerError, "team battle BP cost is unavailable")
			return
		}
	}
	context, _, started, startErr := a.store.beginTeamBattle(
		payload.BossID,
		teamBattleEnemyTypes(battleSegments),
		standaloneBPUse,
		true,
		a.release.State.TeamBattleRewards,
		fmt.Sprintf(
			"solo:user=%d:boss=%d:start=%d",
			a.release.State.User.UserID,
			payload.BossID,
			payload.StartTime,
		),
		[]teamBattleFameSource{{
			ArthurType: int(payload.DeckArthurType),
			LeaderFame: playerLeader.Fame,
		}},
		0,
		rentalPartnerCount(selectedResultPartners),
		friendPointReward,
		rentalCredits,
		func() string {
			if len(rentalCredits) == 0 {
				return ""
			}
			return fmt.Sprintf(
				"solo-rental:user=%d:boss=%d:start=%d:nonce=%d",
				a.release.State.User.UserID,
				payload.BossID,
				payload.StartTime,
				time.Now().UnixNano(),
			)
		}(),
		selectedResultPartners,
	)
	if startErr != nil {
		writeError(writer, http.StatusBadRequest, startErr.Error())
		return
	} else if !started {
		writeError(writer, http.StatusBadRequest, "team battle points are insufficient")
		return
	}
	_, found = teamBattleRewardProfileForContext(a.release.State.TeamBattleRewards, context)
	if !found {
		writeError(writer, http.StatusInternalServerError, "team battle drop profile is unavailable")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	battlesWire := make([]any, len(battleSegments))
	for index, segment := range battleSegments {
		drop := teamBattleDropPlanWire(context.DropPlan, context.BattleEnemyTypes, index)
		battlesWire[index] = map[string]any{
			"enemy_partyid":        segment.EnemyPartyID,
			"enemy_type":           segment.EnemyType,
			"drop":                 drop,
			"event_special_reward": []any{},
			"talk_infos": []any{
				map[string]any{"talkid": 0, "script_name": ""},
				map[string]any{"talkid": 0, "script_name": ""},
			},
			"play_logs": []any{},
		}
	}

	a.writeProtocol(writer, map[string]any{
		"avatar": map[string]any{
			"costumeid":       avatars[int(payload.DeckArthurType)-1].CostumeID,
			"avatar_partsids": append([]int(nil), avatars[int(payload.DeckArthurType)-1].AvatarPartIDs...),
		},
		"partner_deck":                       partnerDecks,
		"seed":                               context.Seed,
		"battles":                            battlesWire,
		"is_unlimited_free_continue":         0,
		"unlimited_free_continue_lv":         0,
		"free_continue_remain":               0,
		"cost_initial":                       replay.CostInitial,
		"burst_gauge_initial":                replay.BurstGaugeInitial,
		"is_active_only_burst_gauge_initial": 0,
		"hold_max":                           replay.HoldMax,
		"end_turn":                           replay.EndTurn,
		"is_solo_online":                     0,
		"container":                          []any{},
		"start_time":                         payload.StartTime,
	})
}

type teamBattleContinueReport struct {
	PayType       int8     `json:"pay_type"`
	Progress      int      `json:"progress"`
	InputCommands []string `json:"input_cmd"`
	EnemyDeadBits []int    `json:"enemy_dead_bit"`
}

func (a *API) teamBattleSoloContinue(writer http.ResponseWriter, request *http.Request) {
	var payload teamBattleContinueReport
	fields := []string{"pay_type", "progress", "input_cmd", "enemy_dead_bit"}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	coin, coinFree, err := a.store.continueTeamBattle(payload, a.release.State, a.persistState)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"coin":                 coin,
		"coin_free":            coinFree,
		"free_continue_remain": 0,
	})
}

func (a *API) teamBattleSoloEnd(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Progress     int      `json:"progress"`
		IsClear      int8     `json:"is_clear"`
		InputCommand []string `json:"input_cmd"`
		EnemyDeadBit []int    `json:"enemy_dead_bit"`
		BossID       int      `json:"bossid"`
	}
	fields := []string{
		"progress",
		"is_clear",
		"input_cmd",
		"enemy_dead_bit",
		"bossid",
	}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	if payload.Progress < 0 || (payload.IsClear != 0 && payload.IsClear != 1) ||
		len(payload.InputCommand) == 0 || len(payload.EnemyDeadBit) == 0 || payload.BossID <= 0 {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, errors.New("invalid local team battle result"))
		return
	}
	canonicalRequest, err := json.Marshal(payload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode local team battle result identity")
		return
	}
	requestDigest := fmt.Sprintf("%x", sha256.Sum256(canonicalRequest))
	a.teamBattleResultMu.Lock()
	defer a.teamBattleResultMu.Unlock()
	if cached, exists := a.store.teamBattleSoloResultReceipt(requestDigest, payload.BossID); exists {
		a.writeProtocol(writer, cached)
		return
	}
	if !teamBattleSoloHasBoss(a.store.teamBattleSoloState(), payload.BossID) &&
		!a.store.towerQuestHasBoss(payload.BossID) {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, errors.New("local team battle boss is unavailable"))
		return
	}
	battleEnemyTypes, found := a.store.activeTeamBattleEnemyTypes(payload.BossID)
	if !found {
		if payload.IsClear == 0 {
			// After server restart there is no live run to retire. The original
			// onTeamBattleSoloEnd clears its local save then returns to home for
			// a non-win with an empty score. Acknowledge without any mutation.
			a.writeProtocol(writer, map[string]any{"is_clear": 0, "score": []any{}, "user": a.userPayload()})
		} else {
			// SV_ERR.TEAMBATTLE_REWARD_NOT_FOUND. The original callback clears
			// the pending local save before showing this business error.
			a.writeProtocolResult(writer, map[string]any{}, -3207, "战斗记录已失效，无法结算本次奖励，请重新进入副本。")
		}
		return
	}
	if err := validateNativeTeamBattleReport(
		payload.Progress,
		payload.IsClear != 0,
		payload.InputCommand,
		payload.EnemyDeadBit,
		battleEnemyTypes,
	); err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	settlement, err := a.store.completeTeamBattle(
		payload.BossID,
		payload.IsClear != 0,
		a.release.State.TeamBattleRewards,
		teamBattleDropReport{EnemyDeadBits: payload.EnemyDeadBit, Turns: nativeScoreTurns(payload.InputCommand)},
	)
	if err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	partnerUserIDs := make([]int, 0, len(settlement.Context.SelectedPartners))
	for _, partner := range settlement.Context.SelectedPartners {
		if !partner.IsSelf {
			partnerUserIDs = append(partnerUserIDs, partner.UserID)
		}
	}
	friendStates, err := a.friendPointAccountStates(partnerUserIDs)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "load result partner friend states")
		return
	}
	partners, err := teamBattleSoloResultPartners(settlement.Context.SelectedPartners, friendStates)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if payload.IsClear != 0 && len(settlement.Context.FriendPointRentalCredits) > 0 {
		if a.friendPointAccounts == nil {
			writeError(writer, http.StatusInternalServerError, "friend-point rental repository is unavailable")
			return
		}
		if err := a.friendPointAccounts.RecordFriendPointRentals(
			settlement.Context.FriendPointRentalEventKey,
			a.release.State.User.UserID,
			settlement.Context.FriendPointRentalCredits,
			settlement.Context.BossID,
		); err != nil {
			a.logger.Error("record friend-point rental events", "error", err)
			writeError(writer, http.StatusInternalServerError, "record friend-point rental events")
			return
		}
	}
	resultRewards := battleResultRewardsWire(settlement.Result.Rewards)
	clearRewards := battleResultRewardsWire(settlement.FirstClear.Rewards)
	newCards := append([]cardInfo(nil), settlement.Result.Cards...)
	newCards = append(newCards, settlement.FirstClear.Cards...)
	newStackCards := append([]release.CardStack(nil), settlement.Result.StackCards...)
	newStackCards = append(newStackCards, settlement.FirstClear.StackCards...)
	newItems := append([]release.Item(nil), settlement.Result.Items...)
	newItems = append(newItems, settlement.FirstClear.Items...)
	newSpheres := append([]release.Sphere(nil), settlement.Result.Spheres...)
	newSpheres = append(newSpheres, settlement.FirstClear.Spheres...)
	newBuddies := append([]release.Buddy(nil), settlement.Result.Buddies...)
	newBuddies = append(newBuddies, settlement.FirstClear.Buddies...)
	for _, fameAward := range settlement.Fame {
		newCards = append(newCards, fameAward.Result.Cards...)
		newStackCards = append(newStackCards, fameAward.Result.StackCards...)
		newItems = append(newItems, fameAward.Result.Items...)
		newSpheres = append(newSpheres, fameAward.Result.Spheres...)
		newBuddies = append(newBuddies, fameAward.Result.Buddies...)
	}
	fameRewards := battleFameRewardsWire(settlement.Fame)
	// StageQuestAreaID identifies the permanent quest route. It is not the
	// suppression/world-boss mode flag consumed by the result scene. Publishing
	// a zero-point object here makes every ordinary quest show a false
	// suppression-points popup, so keep the mode-specific result absent until a
	// real suppression battle contract is recovered.
	stageQuest := []any{}

	// Damage, skill order, enemy AI and the native command CRC remain owned by the
	// original CN libbattle engine. The HTTP boundary validates the recovered
	// native report envelope before it applies local rewards. The result DTO still
	// echoes all three selected partner projections, marking own decks as SELF.
	response := map[string]any{
		"is_clear":                        payload.IsClear,
		"user":                            a.userPayload(),
		"result_rewards":                  resultRewards,
		"clear_rewards":                   clearRewards,
		"fame_rewards":                    fameRewards,
		"score_rewards":                   battleResultRewardsWire(settlement.Score.Rewards),
		"challenge_rewards":               []any{},
		"deck_cards":                      []any{},
		"new_cards":                       toWireCards(newCards),
		"new_stack_cards":                 toWireStackCards(newStackCards),
		"new_items":                       a.itemInfosWire(newItems),
		"new_sphrs":                       toWireSpheres(newSpheres),
		"new_buddys":                      toWireBuddies(newBuddies),
		"partners":                        partners,
		"is_result_reward_in_present_box": boolInt(settlement.Result.InPresentBox),
		"is_clear_reward_in_present_box":  boolInt(settlement.FirstClear.InPresentBox),
		"is_fame_reward_in_present_box":   battleAwardsInPresentBox(settlement.Fame),
		"is_stage_reward_in_present_box":  0,
		"is_score_reward_in_present_box":  boolInt(settlement.Score.InPresentBox),
		"unlock_notice":                   []any{},
		"bonus_fame_add":                  a.release.State.TeamBattleFameBonusPolicy.BonusFameAdd,
		"rookie_type":                     0,
		"stage_quest":                     stageQuest,
		"show_boss_coin":                  0,
		"scene_transition":                []any{},
		"auto_fusion_result":              []any{},
		"auto_loveup_result":              []any{},
		"score":                           scoreInfoWire(settlement.ScoreInfo),
		"challenge_results":               []any{},
		"message":                         "",
	}
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode solo team battle settlement")
		return
	}
	if err := a.store.recordTeamBattleSoloResultReceipt(
		requestDigest,
		payload.BossID,
		encodedResponse,
		time.Now(),
	); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, json.RawMessage(encodedResponse))
}

func (a *API) rejectTeamBattleSoloReport(writer http.ResponseWriter, bossID int, err error) {
	a.logger.Warn("reject native solo battle report", "boss_id", bossID, "error", err)
	// The original result callback clears its pending local report on a
	// protocol response. HTTP 400 instead traps every subsequent login in
	// the same retry, before HomeShow can retire this run. Persistence errors
	// remain retryable and never pass through this business rejection path.
	a.writeProtocolResult(writer, map[string]any{}, -3207, "战斗报告无法结算，请重新进入副本。")
}

const (
	maxTeamBattleSegments            = 64
	nativeBattleCommandFieldCount    = 21
	nativeBattleCommandMaxOpcode     = 34
	nativeBattleCommandStartOpcode   = 22
	nativePVPCommandStartOpcode      = 23
	nativeBattleCommandTurnOpcode    = 10
	nativeBattleCommandMaxTurnPhases = 100
)

// validateNativeTeamBattleReport enforces the wire shape produced by the CN
// client's battle5_api_input_cmd_get. The native verifier replays these records
// against the same seed, decks and master data and additionally checks HP/CRC;
// the Go server deliberately does not duplicate that engine. The managed CN
// client allocates both report arrays from TeamBattleSoloStart.battles.Length,
// so validation is driven by the exact segment contract stored at start.
func validateNativeTeamBattleReport(
	progress int,
	isClear bool,
	inputCommands []string,
	enemyDeadBits []int,
	enemyTypes []int8,
) error {
	if len(enemyTypes) == 0 || len(enemyTypes) > maxTeamBattleSegments ||
		len(inputCommands) != len(enemyTypes) || len(enemyDeadBits) != len(enemyTypes) {
		return errors.New("local team battle report does not match the started battle count")
	}
	if isClear {
		if progress != len(enemyTypes) {
			return errors.New("clear result does not reach the final battle segment")
		}
		for index := range enemyTypes {
			if err := validateNativeTeamBattleCompletedSegment(
				inputCommands[index], enemyDeadBits[index], enemyTypes[index] == 4,
			); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		}
		return nil
	}
	if progress < 0 || progress > len(enemyTypes) {
		return errors.New("non-clear result advanced beyond the active battle segment")
	}
	// ResultMgr sends raw battle_idx. A normal defeat has already called
	// battleEnd and advanced it; manual retire bypasses battleEnd. The latter
	// may retain a nonempty report from a previous Continue in this same wave.
	// Accept either original envelope, keeping prior completed and future empty
	// segments checked. Neither non-clear form grants rewards.
	if progress > 0 {
		err := validateNativeTeamBattleUnfinishedSegments(progress-1, inputCommands, enemyDeadBits, enemyTypes)
		if err == nil || progress == len(enemyTypes) {
			return err
		}
	}
	return validateNativeTeamBattleUnfinishedSegments(progress, inputCommands, enemyDeadBits, enemyTypes)
}

func validateNativeTeamBattleUnfinishedSegments(activeIndex int, inputCommands []string, enemyDeadBits []int, enemyTypes []int8) error {
	for index := range enemyTypes {
		command := strings.TrimSpace(inputCommands[index])
		if enemyDeadBits[index] < 0 {
			return fmt.Errorf("battle segment %d: local team battle enemy state is invalid", index)
		}
		switch {
		case index < activeIndex:
			if err := validateNativeTeamBattleCompletedSegment(
				command, enemyDeadBits[index], enemyTypes[index] == 4,
			); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		case index == activeIndex:
			if err := validateNativeBattleCommand(command, enemyDeadBits[index] != 0); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		default:
			if command != "" || enemyDeadBits[index] != 0 {
				return fmt.Errorf("battle segment %d contains future battle state", index)
			}
		}
	}
	return nil
}

func validateNativeTeamBattleContinueReport(
	progress int,
	inputCommands []string,
	enemyDeadBits []int,
	enemyTypes []int8,
) error {
	if len(enemyTypes) == 0 || len(enemyTypes) > maxTeamBattleSegments ||
		len(inputCommands) != len(enemyTypes) || len(enemyDeadBits) != len(enemyTypes) ||
		progress < 1 || progress > len(enemyTypes) {
		return errors.New("team battle continue report does not match the active battle")
	}
	activeIndex := progress - 1
	for index := range enemyTypes {
		command := strings.TrimSpace(inputCommands[index])
		if enemyDeadBits[index] < 0 {
			return fmt.Errorf("battle segment %d: team battle continue enemy state is invalid", index)
		}
		switch {
		case index < activeIndex:
			if err := validateNativeTeamBattleCompletedSegment(
				command, enemyDeadBits[index], enemyTypes[index] == 4,
			); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		case index == activeIndex:
			if err := validateNativeBattleCommand(command, true); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		default:
			if command != "" || enemyDeadBits[index] != 0 {
				return fmt.Errorf("battle segment %d contains future battle state", index)
			}
		}
	}
	return nil
}

func validateNativeTeamBattleCompletedSegment(command string, enemyDeadBits int, optional bool) error {
	command = strings.TrimSpace(command)
	if optional && command == "" && enemyDeadBits == 0 {
		return nil
	}
	if enemyDeadBits < 0 {
		return errors.New("local team battle enemy state is invalid")
	}
	// Native victory is independent of the death mask: Nameless's official
	// PARTS_ALL_BREAK action sets ENEMY_AWAKE_FLAG_SET and FORCE_BATTLE_END
	// while its body survives (captured clear report: mask 6). Keep this mask
	// intact for per-enemy drops; do not require or fabricate a body kill.
	return validateNativeBattleCommand(command, true)
}

func validateNativeBattleCommand(command string, required bool) error {
	return validateNativeBattleCommandStart(command, required, nativeBattleCommandStartOpcode)
}

func validateNativeBattleCommandStart(command string, required bool, expectedStartOpcode int) error {
	if command == "" {
		if required {
			return errors.New("clear result has no native battle command")
		}
		return nil
	}

	lineCount := 0
	firstOpcode := -1
	startCount := 0
	turnCount := 0
	for _, line := range strings.Split(strings.ReplaceAll(command, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != nativeBattleCommandFieldCount {
			return errors.New("native battle command row has an invalid field count")
		}
		values := make([]int64, nativeBattleCommandFieldCount)
		for index, field := range fields {
			value, err := strconv.ParseInt(strings.TrimSpace(field), 10, 32)
			if err != nil {
				return errors.New("native battle command row contains a non-int32 field")
			}
			values[index] = value
		}
		if values[0] < 0 {
			return errors.New("native battle command row contains an invalid HP checksum")
		}
		opcode := values[1]
		if opcode < 0 || opcode > nativeBattleCommandMaxOpcode {
			return errors.New("native battle command row contains an unknown opcode")
		}
		if opcode == int64(expectedStartOpcode) {
			startCount++
		}
		if opcode == nativeBattleCommandTurnOpcode {
			turnCount++
			if turnCount > nativeBattleCommandMaxTurnPhases {
				return errors.New("native battle command exceeds the verifier turn limit")
			}
		}
		if lineCount == 0 {
			firstOpcode = int(opcode)
		}
		lineCount++
	}
	if lineCount == 0 {
		return errors.New("native battle command has no rows")
	}
	// Each native battle API records its own START opcode before any phases.
	// This also rejects a syntactically valid hand-written row as a clear report.
	if firstOpcode != expectedStartOpcode || startCount != 1 {
		return errors.New("native battle command does not begin with START")
	}
	return nil
}

func rentalPartnerCount(selected []teamBattleResultPartner) int {
	count := 0
	for _, partner := range selected {
		if !partner.IsSelf {
			count++
		}
	}
	return count
}

func teamBattleSoloResultPartners(
	selected []teamBattleResultPartner,
	friendStates map[int]int8,
) ([]any, error) {
	if len(selected) != 3 {
		return nil, errors.New("team battle result requires the three selected partners")
	}
	partners := make([]any, 0, len(selected))
	for _, partner := range selected {
		friendState := friendStates[partner.UserID]
		if partner.IsSelf {
			friendState = 4 // Original TeamBattleMemberWindow's all-self branch.
		}
		partners = append(partners, map[string]any{
			"userid":            partner.UserID,
			"name":              partner.Name,
			"arthur_type":       partner.ArthurType,
			"is_burst":          partner.IsBurst,
			"lv":                partner.Level,
			"deck_rank":         partner.DeckRank,
			"state":             friendState,
			"leader_cardid":     partner.LeaderCardID,
			"leader_card_lv":    partner.LeaderLevel,
			"leader_card_fame":  partner.LeaderFame,
			"last_login_time":   partner.LastLoginUnix,
			"comment":           partner.Comment,
			"pvp_point":         partner.PVPPoint,
			"is_first_matching": 0,
			"rookie_type":       0,
			"deck_honorids":     append([]int(nil), partner.HonorIDs...),
		})
	}
	return partners, nil
}

func (a *API) teamBattleMultiRoomSearch(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		DeckArthurType      int8   `json:"deck_arthur_type"`
		DeckArthurTypeIndex int8   `json:"deck_arthur_type_idx"`
		Password            string `json:"pass"`
		IsRookie            int8   `json:"is_rookie"`
		BossID              int    `json:"bossid"`
		BossGroupID         int    `json:"boss_groupid"`
		RookieType          int8   `json:"rookie_type"`
		SearchID            int    `json:"searchid"`
		QuestGetTime        int    `json:"quest_get_time"`
		IsAuto              int8   `json:"is_auto"`
		EmptyTime           int    `json:"empty_time"`
	}
	fields := []string{
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"pass",
		"is_rookie",
		"bossid",
		"boss_groupid",
		"rookie_type",
		"searchid",
		"quest_get_time",
		"is_auto",
		"empty_time",
	}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || payload.BossGroupID < 0 ||
		payload.SearchID < 0 || payload.QuestGetTime < 0 || payload.EmptyTime < 0 ||
		(payload.IsRookie != 0 && payload.IsRookie != 1) ||
		(payload.IsAuto != 0 && payload.IsAuto != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local team battle room query")
		return
	}
	// The native quick-room entry sends bossid=0 to request every open room.
	// Positive values remain an exact official boss filter.
	if payload.BossID < 0 || payload.BossID > 0 && !teamBattleSoloHasBoss(a.store.teamBattleSoloState(), payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	rooms, err := a.teamBattleMultiRoomWires(multiplayer.RoomSearch{
		BossID: payload.BossID, BossGroupID: payload.BossGroupID, Password: payload.Password,
		UserID: a.release.State.User.UserID, ArthurType: int(payload.DeckArthurType),
	}, payload.SearchID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := teamBattleMultiGroups(a.store.teamBattleSoloState())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	reserveRoomID := int64(0)
	reserveLimitTime := int64(0)
	if a.multiplayer != nil {
		if roomID, limit, found := a.multiplayer.ReservationFor(a.release.State.User.UserID); found {
			reserveRoomID = roomID
			reserveLimitTime = limit
		}
	}
	a.writeProtocol(writer, map[string]any{
		"rooms":                  rooms,
		"search_datas":           teamBattleSearchOptions,
		"normal_groups":          groups["normal_groups"],
		"special_groups":         groups["special_groups"],
		"key_groups":             groups["key_groups"],
		"event_groups":           groups["event_groups"],
		"reserve_roomid":         reserveRoomID,
		"reserve_limit_time":     reserveLimitTime,
		"retry_interval":         0,
		"auto_search_time":       0,
		"punished_left_sec":      0,
		"punished_host_left_sec": 0,
	})
}

func (a *API) teamBattleMultiRoomWires(
	query multiplayer.RoomSearch,
	searchID int,
) ([]any, error) {
	rooms := make([]any, 0)
	if a.multiplayer == nil {
		return rooms, nil
	}
	snapshots := a.multiplayer.List(query)
	ownerUserIDs := make([]int, 0, len(snapshots))
	for _, room := range snapshots {
		for _, member := range room.Members {
			if member.MemberType == room.OwnerMemberType {
				ownerUserIDs = append(ownerUserIDs, member.UserID)
				break
			}
		}
	}
	friendStates, err := a.friendPointAccountStates(ownerUserIDs)
	if err != nil {
		return nil, fmt.Errorf("load room owner friend states: %w", err)
	}
	for _, room := range snapshots {
		ownerUserID := 0
		for _, member := range room.Members {
			if member.MemberType == room.OwnerMemberType {
				ownerUserID = member.UserID
				break
			}
		}
		friendState := friendStates[ownerUserID]
		if !multiplayerRoomAllowsVisitor(room.RoomType, friendState) {
			continue
		}
		wire, err := multiplayerRoomWire(
			room,
			a.store.friendPointRewardForState(friendState),
			friendState,
		)
		if err != nil {
			return nil, err
		}
		if searchID > 0 && wire["boss_group"].(map[string]any)["searchid"] != float64(searchID) {
			continue
		}
		rooms = append(rooms, wire)
	}
	return rooms, nil
}

func (a *API) teamBattleSoloPartnerShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID int `json:"bossid"`
	}
	if err := decodeExact(request, []string{"bossid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !teamBattleSoloHasBoss(a.store.teamBattleSoloState(), payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	arthurs, err := a.localTeamBattlePartnerArthurs()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{"arthurs": arthurs})
}

func (a *API) localTeamBattlePartnerArthurs() ([]any, error) {
	partnerBuckets := make(map[int8][]any, 4)
	preferredBuckets := make(map[int8][]any, 4)
	systemFallbacks := make(map[int8]any, 4)
	externalViews, err := a.friendPointPartnerViews()
	if err != nil {
		return nil, fmt.Errorf("list local-account team battle partners: %w", err)
	}
	for _, view := range externalViews {
		partnerReward := a.store.friendPointRewardForState(view.FriendState)
		partner, err := view.listWire(partnerReward, view.FriendState)
		if err != nil {
			return nil, err
		}
		if view.System {
			systemFallbacks[view.ArthurType] = partner
		} else if view.FriendState == friendStateFriend || view.FriendState == friendStateFollow {
			preferredBuckets[view.ArthurType] = append(preferredBuckets[view.ArthurType], partner)
		} else {
			partnerBuckets[view.ArthurType] = append(partnerBuckets[view.ArthurType], partner)
		}
	}
	arthurs := make([]any, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		// Preserve variety within each tier, but never shuffle strangers ahead
		// of a usable friend or followed player's deck.
		for _, bucket := range [][]any{preferredBuckets[arthurType], partnerBuckets[arthurType]} {
			rand.Shuffle(len(bucket), func(left, right int) {
				bucket[left], bucket[right] = bucket[right], bucket[left]
			})
		}
		partnerBuckets[arthurType] = append(preferredBuckets[arthurType], partnerBuckets[arthurType]...)
		// Keep the system deck selectable after real players, even when this
		// profession has a usable local account.
		if fallback, found := systemFallbacks[arthurType]; found {
			partnerBuckets[arthurType] = append(partnerBuckets[arthurType], fallback)
		}
		if len(partnerBuckets[arthurType]) == 0 {
			return nil, fmt.Errorf("team battle partner profession %d has no persistent candidate", arthurType)
		}
		arthurs = append(arthurs, map[string]any{
			"partners":    partnerBuckets[arthurType],
			"arthur_type": arthurType,
		})
	}
	return arthurs, nil
}

func teamBattlePartnerArthursNumeric(arthurs []any) ([]any, error) {
	numeric := make([]any, 0, len(arthurs))
	for _, arthurValue := range arthurs {
		arthur, ok := arthurValue.(map[string]any)
		if !ok {
			return nil, errors.New("team battle partner Arthur projection is invalid")
		}
		partners, ok := arthur["partners"].([]any)
		if !ok || len(partners) == 0 {
			return nil, errors.New("team battle partner projection is invalid")
		}
		numericPartners := make([]any, 0, len(partners))
		for _, partnerValue := range partners {
			partner, ok := partnerValue.(map[string]any)
			if !ok {
				return nil, errors.New("team battle partner projection is invalid")
			}
			leader, ok := partner["leader_card"].(map[string]any)
			if !ok {
				return nil, errors.New("team battle partner leader projection is invalid")
			}
			numericLeader := map[string]any{
				"0": leader["cardid"], "1": leader["lv"], "2": leader["skill_lv"],
				"3": leader["love"], "4": leader["hp"], "5": leader["atkp"],
				"6": leader["intp"], "7": leader["mndp"], "8": leader["fame"],
			}
			numericPartners = append(numericPartners, map[string]any{
				"0": partner["userid"], "1": partner["name"], "2": partner["lv"],
				"3": partner["arthur_type"], "4": partner["is_burst"], "5": partner["job_type"],
				"6": partner["hp"], "7": partner["atkp"], "8": partner["intp"],
				"9": partner["mndp"], "10": partner["deck_rank"], "11": numericLeader,
				"12": partner["get_fp"], "13": partner["comment"], "14": partner["friend_state"],
				"15": partner["rookie_type"], "16": partner["deck_name"], "17": partner["rental_idx"],
				"18": partner["play_log_turn"], "19": partner["attr_nums"], "20": partner["kind_nums"],
				"21": partner["deck_honorids"],
			})
		}
		numeric = append(numeric, map[string]any{
			"0": numericPartners,
			"1": arthur["arthur_type"],
		})
	}
	if len(numeric) != 4 {
		return nil, errors.New("team battle partner Arthur coverage is incomplete")
	}
	return numeric, nil
}

func (a *API) teamBattleSoloPartnerRentalDeck(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserID int `json:"userid"`
	}
	if err := decodeExact(request, []string{"userid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	views, err := a.friendPointPartnerViews()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "list persistent team battle partners")
		return
	}
	view, found := findFriendPointPartnerView(views, payload.UserID)
	if !found {
		writeError(writer, http.StatusBadRequest, "unknown persistent team battle partner")
		return
	}
	partnerDecks, err := view.rentalDeckWires()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{"partner_deck": partnerDecks})
}

func teamBattleSoloHasBoss(configuration json.RawMessage, bossID int) bool {
	if bossID <= 0 {
		return false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(configuration, &top) != nil {
		return false
	}
	for _, groupKey := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if json.Unmarshal(top[groupKey], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var bosses []map[string]json.RawMessage
			if json.Unmarshal(group["10"], &bosses) != nil {
				continue
			}
			for _, boss := range bosses {
				var current int
				if json.Unmarshal(boss["0"], &current) == nil && current == bossID {
					return true
				}
			}
		}
	}
	return false
}

func teamBattleSoloBossAccessible(configuration json.RawMessage, bossID int, nowUnix int64) bool {
	if bossID <= 0 || nowUnix <= 0 {
		return false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(configuration, &top) != nil {
		return false
	}
	for _, groupKey := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if json.Unmarshal(top[groupKey], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var bosses []map[string]json.RawMessage
			if json.Unmarshal(group["10"], &bosses) != nil {
				continue
			}
			for _, boss := range bosses {
				var current, state int
				var expiration int64
				if json.Unmarshal(boss["0"], &current) != nil || current != bossID ||
					json.Unmarshal(boss["10"], &state) != nil ||
					json.Unmarshal(boss["16"], &expiration) != nil {
					continue
				}
				// TeamBattleBossInfo field "10" is the client presentation
				// state, not an availability flag: the CN client renders 0 as
				// NEW and 2 as CLEAR. Reject malformed states, but keep a new
				// battle selectable while the publication has not expired.
				return state >= 0 && state <= 2 && (expiration == 0 || expiration > nowUnix)
			}
		}
	}
	return false
}

func teamBattleSoloBossBPUse(configuration json.RawMessage, bossID int) (int, bool) {
	if bossID <= 0 {
		return 0, false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(configuration, &top) != nil {
		return 0, false
	}
	for _, groupKey := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if json.Unmarshal(top[groupKey], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var bosses []map[string]json.RawMessage
			if json.Unmarshal(group["10"], &bosses) != nil {
				continue
			}
			for _, boss := range bosses {
				var current, bpUse int
				if json.Unmarshal(boss["0"], &current) == nil && current == bossID &&
					json.Unmarshal(boss["5"], &bpUse) == nil && bpUse > 0 {
					return bpUse, true
				}
			}
		}
	}
	return 0, false
}

func teamBattleSoloWithBattlePoints(
	configuration json.RawMessage,
	status battlePointStatus,
	medalCount int,
) (json.RawMessage, error) {
	if status.Current < 0 || status.Max <= 0 || status.Current > status.Max {
		return nil, errors.New("local team battle point state is invalid")
	}
	if medalCount < 0 {
		return nil, errors.New("local team battle medal state is invalid")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, fmt.Errorf("decode local team battle point response: %w", err)
	}
	current, err := json.Marshal(status.Current)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle current points: %w", err)
	}
	maximum, err := json.Marshal(status.Max)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle maximum points: %w", err)
	}
	top["0"] = current
	top["1"] = maximum
	medals, err := json.Marshal(medalCount)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle medal count: %w", err)
	}
	top["8"] = medals
	updated, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle point response: %w", err)
	}
	return updated, nil
}

func markStandaloneTeamBattleClear(
	configuration json.RawMessage,
	bossID int,
) (json.RawMessage, bool, error) {
	return markTeamBattleClearForArea(configuration, bossID, 0)
}

func markTeamBattleClearForArea(
	configuration json.RawMessage,
	bossID int,
	stageQuestAreaID int,
) (json.RawMessage, bool, error) {
	if bossID <= 0 {
		return nil, false, errors.New("local team battle boss is invalid")
	}
	if stageQuestAreaID < 0 {
		return nil, false, errors.New("local team battle area is invalid")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, false, fmt.Errorf("decode local team battle state: %w", err)
	}
	found := false
	firstClear := false
	for _, groupKey := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(top[groupKey], &groups); err != nil {
			return nil, false, fmt.Errorf("decode local team battle groups %s: %w", groupKey, err)
		}
		categoryChanged := false
		for groupIndex := range groups {
			var groupAreaID int
			if err := json.Unmarshal(groups[groupIndex]["9"], &groupAreaID); err != nil {
				return nil, false, errors.New("local team battle group area is invalid")
			}
			if groupAreaID != stageQuestAreaID {
				continue
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(groups[groupIndex]["10"], &bosses); err != nil {
				return nil, false, errors.New("local team battle boss list is invalid")
			}
			for bossIndex := range bosses {
				var currentBossID, state int
				if json.Unmarshal(bosses[bossIndex]["0"], &currentBossID) != nil ||
					currentBossID != bossID {
					continue
				}
				if json.Unmarshal(bosses[bossIndex]["10"], &state) != nil || state < 0 || state > 2 {
					return nil, false, errors.New("local team battle clear state is invalid")
				}
				found = true
				firstClear = state != 2
				bosses[bossIndex]["10"] = json.RawMessage("2")
				encodedBosses, err := json.Marshal(bosses)
				if err != nil {
					return nil, false, fmt.Errorf("encode local team battle bosses: %w", err)
				}
				groups[groupIndex]["10"] = encodedBosses
				categoryChanged = true
				break
			}
			if found {
				break
			}
		}
		if categoryChanged {
			encodedGroups, err := json.Marshal(groups)
			if err != nil {
				return nil, false, fmt.Errorf("encode local team battle groups: %w", err)
			}
			top[groupKey] = encodedGroups
			break
		}
	}
	if !found {
		return nil, false, errors.New("local team battle clear boss is unavailable")
	}
	updated, err := json.Marshal(top)
	if err != nil {
		return nil, false, fmt.Errorf("encode local team battle state: %w", err)
	}
	return updated, firstClear, nil
}

func stageQuestAreaID(configuration json.RawMessage) (int, error) {
	var configured struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(configuration, &configured); err != nil {
		return 0, fmt.Errorf("decode local StageQuest configuration: %w", err)
	}
	if configured.StageQuest.AreaID <= 0 {
		return 0, fmt.Errorf("local StageQuest area is unavailable")
	}
	return configured.StageQuest.AreaID, nil
}

func stageQuestBattleForBoss(
	configuration json.RawMessage,
	areaID int,
	bossID int,
) (int, int, bool, error) {
	var configured struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
			Stages []struct {
				StageID  int `json:"stageid"`
				RaidBoss []struct {
					BossGroup struct {
						Bosses []struct {
							BossID int `json:"bossid"`
							BPUse  int `json:"bp_use"`
						} `json:"bosses"`
					} `json:"boss_group"`
				} `json:"raid_boss"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(configuration, &configured); err != nil {
		return 0, 0, false, fmt.Errorf("decode local StageQuest battle: %w", err)
	}
	if configured.StageQuest.AreaID != areaID {
		return 0, 0, false, nil
	}
	for _, stage := range configured.StageQuest.Stages {
		for _, raid := range stage.RaidBoss {
			for _, boss := range raid.BossGroup.Bosses {
				if boss.BossID == bossID && stage.StageID > 0 && boss.BPUse > 0 {
					return stage.StageID, boss.BPUse, true, nil
				}
			}
		}
	}
	return 0, 0, false, nil
}

func releasedStageQuestBattleForBoss(
	configuration json.RawMessage,
	areaID int,
	bossID int,
) (int, int, bool, error) {
	var configured struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
			Stages []struct {
				StageID    int `json:"stageid"`
				IsReleased int `json:"is_release_now"`
				RaidBoss   []struct {
					BossGroup struct {
						Bosses []struct {
							BossID int `json:"bossid"`
							BPUse  int `json:"bp_use"`
						} `json:"bosses"`
					} `json:"boss_group"`
				} `json:"raid_boss"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(configuration, &configured); err != nil {
		return 0, 0, false, fmt.Errorf("decode released local StageQuest battle: %w", err)
	}
	if configured.StageQuest.AreaID != areaID {
		return 0, 0, false, nil
	}
	for _, stage := range configured.StageQuest.Stages {
		if stage.IsReleased != 1 {
			continue
		}
		for _, raid := range stage.RaidBoss {
			for _, boss := range raid.BossGroup.Bosses {
				if boss.BossID == bossID && stage.StageID > 0 && boss.BPUse > 0 {
					return stage.StageID, boss.BPUse, true, nil
				}
			}
		}
	}
	return 0, 0, false, nil
}

func consumeStageQuestNewClear(
	configuration json.RawMessage,
) (json.RawMessage, bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest response: %w", err)
	}
	var stageIDs []int
	if err := json.Unmarshal(top["new_clear_stage"], &stageIDs); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest new-clear state: %w", err)
	}
	if len(stageIDs) == 0 {
		return append(json.RawMessage(nil), configuration...), false, nil
	}
	top["new_clear_stage"] = json.RawMessage("[]")
	updated, err := json.Marshal(top)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest response: %w", err)
	}
	return updated, true, nil
}

func markStageQuestClear(
	configuration json.RawMessage,
	areaID int,
	stageID int,
) (json.RawMessage, bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest state: %w", err)
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest area: %w", err)
	}
	var configuredAreaID int
	if json.Unmarshal(stageQuest["areaid"], &configuredAreaID) != nil || configuredAreaID != areaID {
		return nil, false, errors.New("local StageQuest clear area does not match")
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest stages: %w", err)
	}
	found := false
	firstClear := false
	for index := range stages {
		var currentStageID, clearDone int
		if json.Unmarshal(stages[index]["stageid"], &currentStageID) != nil || currentStageID != stageID {
			continue
		}
		if json.Unmarshal(stages[index]["is_clear_done"], &clearDone) != nil {
			return nil, false, errors.New("local StageQuest clear state is invalid")
		}
		found = true
		firstClear = clearDone == 0
		stages[index]["is_clear_done"] = json.RawMessage("1")

		var raids []map[string]json.RawMessage
		if err := json.Unmarshal(stages[index]["raid_boss"], &raids); err != nil {
			return nil, false, fmt.Errorf("decode local StageQuest raid rewards: %w", err)
		}
		for raidIndex := range raids {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raids[raidIndex]["boss_group"], &group); err != nil {
				return nil, false, fmt.Errorf("decode local StageQuest raid boss group: %w", err)
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group["bosses"], &bosses); err != nil {
				return nil, false, fmt.Errorf("decode local StageQuest raid bosses: %w", err)
			}
			for bossIndex := range bosses {
				bosses[bossIndex]["state"] = json.RawMessage("2")
			}
			encodedBosses, err := json.Marshal(bosses)
			if err != nil {
				return nil, false, fmt.Errorf("encode local StageQuest raid bosses: %w", err)
			}
			group["bosses"] = encodedBosses
			encodedGroup, err := json.Marshal(group)
			if err != nil {
				return nil, false, fmt.Errorf("encode local StageQuest raid boss group: %w", err)
			}
			raids[raidIndex]["boss_group"] = encodedGroup

			var rewards []map[string]json.RawMessage
			if err := json.Unmarshal(raids[raidIndex]["clear_reward"], &rewards); err != nil {
				return nil, false, fmt.Errorf("decode local StageQuest clear rewards: %w", err)
			}
			for rewardIndex := range rewards {
				rewards[rewardIndex]["is_already"] = json.RawMessage("1")
			}
			encodedRewards, err := json.Marshal(rewards)
			if err != nil {
				return nil, false, fmt.Errorf("encode local StageQuest clear rewards: %w", err)
			}
			raids[raidIndex]["clear_reward"] = encodedRewards
		}
		encodedRaids, err := json.Marshal(raids)
		if err != nil {
			return nil, false, fmt.Errorf("encode local StageQuest raids: %w", err)
		}
		stages[index]["raid_boss"] = encodedRaids
		break
	}
	if !found {
		return nil, false, errors.New("local StageQuest clear stage is unavailable")
	}
	if firstClear {
		var newClear []int
		if err := json.Unmarshal(top["new_clear_stage"], &newClear); err != nil {
			return nil, false, fmt.Errorf("decode local StageQuest new-clear state: %w", err)
		}
		alreadyQueued := false
		for _, queued := range newClear {
			if queued == stageID {
				alreadyQueued = true
				break
			}
		}
		if !alreadyQueued {
			newClear = append(newClear, stageID)
		}
		encodedNewClear, err := json.Marshal(newClear)
		if err != nil {
			return nil, false, fmt.Errorf("encode local StageQuest new-clear state: %w", err)
		}
		top["new_clear_stage"] = encodedNewClear
	}
	encodedStages, err := json.Marshal(stages)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest stages: %w", err)
	}
	stageQuest["stage_object"] = encodedStages
	encodedStageQuest, err := json.Marshal(stageQuest)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest area: %w", err)
	}
	top["stage_quest"] = encodedStageQuest
	updated, err := json.Marshal(top)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest state: %w", err)
	}
	return updated, firstClear, nil
}

func stageQuestAllStagesCleared(configuration json.RawMessage) (bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return false, fmt.Errorf("decode local StageQuest completion state: %w", err)
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return false, fmt.Errorf("decode local StageQuest completion area: %w", err)
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil || len(stages) == 0 {
		return false, errors.New("local StageQuest completion stages are invalid")
	}
	for _, stage := range stages {
		var clearDone int
		if err := json.Unmarshal(stage["is_clear_done"], &clearDone); err != nil {
			return false, errors.New("local StageQuest completion flag is invalid")
		}
		if clearDone == 0 {
			return false, nil
		}
	}
	return true, nil
}

func teamBattleRewardProfileForContext(
	profiles []release.TeamBattleRewardProfile,
	context teamBattleContext,
) (release.TeamBattleRewardProfile, bool) {
	for _, profile := range profiles {
		if profile.BossID == context.BossID &&
			profile.StageQuestAreaID == context.StageQuestAreaID &&
			profile.StageQuestStageID == context.StageQuestStageID &&
			profile.TowerID == context.TowerID &&
			profile.TowerFloor == context.TowerFloor {
			return profile, true
		}
	}
	return release.TeamBattleRewardProfile{}, false
}

func battleResultRewardsWire(rewards []receivedReward) []any {
	result := make([]any, 0, len(rewards))
	for _, received := range rewards {
		result = append(result, map[string]any{
			"reward":           received.Reward,
			"uniqid":           append([]int64(nil), received.UniqueID...),
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		})
	}
	return result
}

func battleFameRewardsWire(awards []teamBattleFameAward) []any {
	result := make([]any, 0, len(awards))
	for _, award := range awards {
		if len(award.Result.Rewards) != 1 || award.ArthurType < 1 || award.ArthurType > 4 ||
			(award.RewardKind != 0 && award.RewardKind != 1) {
			continue
		}
		reward := battleResultRewardsWire(award.Result.Rewards)
		result = append(result, map[string]any{
			"result_reward":             reward[0],
			"arthur_type":               award.ArthurType,
			"ex_teambattle_reward_type": award.RewardKind,
		})
	}
	return result
}

func battleHostRewardsWire(awards []teamBattleFameAward) []any {
	result := make([]any, 0, len(awards))
	for _, award := range awards {
		if len(award.Result.Rewards) != 1 || award.ArthurType < 1 || award.ArthurType > 4 ||
			award.RewardKind != 5 {
			continue
		}
		reward := battleResultRewardsWire(award.Result.Rewards)
		result = append(result, map[string]any{
			"result_reward":             reward[0],
			"arthur_type":               award.ArthurType,
			"ex_teambattle_reward_type": award.RewardKind,
		})
	}
	return result
}

func teamBattleReplayForBoss(
	replays []release.TeamBattleReplay,
	bossID int,
) (release.TeamBattleReplay, bool) {
	for _, replay := range replays {
		if replay.BossID == bossID {
			return replay, true
		}
	}
	return release.TeamBattleReplay{}, false
}

func teamBattleReplayBattles(replay release.TeamBattleReplay) ([]release.TeamBattleReplayBattle, error) {
	if replay.EnemyPartyID <= 0 || replay.EnemyType < 0 || replay.EnemyType > 4 {
		return nil, errors.New("team battle replay legacy segment is invalid")
	}
	if len(replay.Battles) == 0 {
		return []release.TeamBattleReplayBattle{{
			EnemyPartyID: replay.EnemyPartyID,
			EnemyType:    replay.EnemyType,
		}}, nil
	}
	if len(replay.Battles) > maxTeamBattleSegments {
		return nil, errors.New("team battle replay has too many battle segments")
	}
	if replay.Battles[0].EnemyPartyID != replay.EnemyPartyID ||
		replay.Battles[0].EnemyType != replay.EnemyType {
		return nil, errors.New("team battle replay first segment does not mirror its legacy projection")
	}
	segments := append([]release.TeamBattleReplayBattle(nil), replay.Battles...)
	for _, segment := range segments {
		if segment.EnemyPartyID <= 0 || segment.EnemyType < 0 || segment.EnemyType > 4 {
			return nil, errors.New("team battle replay segment is invalid")
		}
	}
	return segments, nil
}

func teamBattleEnemyTypes(segments []release.TeamBattleReplayBattle) []int8 {
	enemyTypes := make([]int8, len(segments))
	for index, segment := range segments {
		enemyTypes[index] = segment.EnemyType
	}
	return enemyTypes
}

func lastRequiredTeamBattleSegment(segments []release.TeamBattleReplayBattle) int {
	for index := len(segments) - 1; index >= 0; index-- {
		if segments[index].EnemyType != 4 {
			return index
		}
	}
	return len(segments) - 1
}

func exactDeck(decks []deckInfo, arthurType int8, deckIndex int8) (deckInfo, bool) {
	for _, deck := range decks {
		if deck.ArthurType == arthurType && deck.Index == deckIndex {
			return deck, true
		}
	}
	return deckInfo{}, false
}

func teamBattlePartnerDeckWire(
	userID int,
	arthurType int8,
	deck deckInfo,
	cards map[int64]cardInfo,
	job release.JobParameter,
	avatar release.Avatar,
	spheres map[int64]release.Sphere,
	buddies map[int64]release.Buddy,
	supportUnlocked int8,
	isBurst int8,
) (map[string]any, error) {
	cardDeck := make([]any, 0, len(deck.CardUniqueIDs))
	for _, uniqueID := range deck.CardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			return nil, fmt.Errorf("team battle deck card %d is unavailable", uniqueID)
		}
		cardDeck = append(cardDeck, partnerCardWire(card))
	}
	if len(cardDeck) == 0 {
		return nil, fmt.Errorf("team battle deck is empty")
	}
	if supportUnlocked < 0 || int(supportUnlocked) > len(deck.SupportCardUniqueIDs) {
		return nil, fmt.Errorf("team battle support-card slot count is invalid")
	}
	// ProtoGen only allocates PartnerDeckInfo.support_deck when the wire list is
	// non-empty. ClearDeck then unconditionally unions deck and support_deck, so
	// sending [] for a profession with zero unlocked slots becomes a client-side
	// null and aborts the view. Keep the complete fixed slot vector on the wire;
	// support_card_unlock_slot_num remains the independent lock-state owner.
	supportDeck := make([]any, len(deck.SupportCardUniqueIDs))
	for index := range supportDeck {
		uniqueID := deck.SupportCardUniqueIDs[index]
		if uniqueID == 0 {
			supportDeck[index] = partnerCardWire(cardInfo{SkillLevels: []int16{}})
			continue
		}
		card, exists := cards[uniqueID]
		if !exists {
			return nil, fmt.Errorf("team battle support card %d is unavailable", uniqueID)
		}
		supportDeck[index] = partnerCardWire(card)
	}
	hp, attack, magic, mind := partnerDeckStats(deck, cards, job)
	if len(deck.SphereUniqueIDs) != deckSphereSlots {
		return nil, fmt.Errorf("team battle deck must contain %d sphere slots", deckSphereSlots)
	}
	sphereDeck := make([]any, deckSphereSlots)
	for index, uniqueID := range deck.SphereUniqueIDs {
		if uniqueID == 0 {
			sphereDeck[index] = map[string]any{"sphrid": 0, "lv": 0}
			continue
		}
		sphere, exists := spheres[uniqueID]
		if !exists || sphere.SphereID <= 0 || sphere.Level <= 0 {
			return nil, fmt.Errorf("team battle deck sphere %d is unavailable", uniqueID)
		}
		sphereDeck[index] = map[string]any{
			"sphrid": sphere.SphereID,
			"lv":     sphere.Level,
		}
	}
	const battleBuddySlots = 5
	buddyDeck := make([]any, battleBuddySlots)
	for index := range buddyDeck {
		buddyDeck[index] = map[string]any{"buddyid": 0, "lv": 0}
	}
	buddyIndex := 0
	for _, uniqueID := range deck.BuddyUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		buddy, exists := buddies[uniqueID]
		if !exists {
			return nil, fmt.Errorf("team battle deck buddy %d is unavailable", uniqueID)
		}
		if buddyIndex >= len(buddyDeck) {
			return nil, fmt.Errorf("team battle deck has more than %d buddies", battleBuddySlots)
		}
		buddyDeck[buddyIndex] = map[string]any{
			"buddyid": buddy.BuddyID,
			"lv":      buddy.Level,
		}
		buddyIndex++
	}
	return map[string]any{
		"userid":                       userID,
		"arthur_type":                  arthurType,
		"job_type":                     deck.JobType,
		"is_burst":                     isBurst,
		"hp":                           hp,
		"atkp":                         attack,
		"intp":                         magic,
		"mndp":                         mind,
		"deck":                         cardDeck,
		"support_deck":                 supportDeck,
		"support_card_unlock_slot_num": supportUnlocked,
		"sphrs":                        sphereDeck,
		"buddys":                       buddyDeck,
		"avatar": map[string]any{
			"costumeid":       avatar.CostumeID,
			"avatar_partsids": append([]int(nil), avatar.AvatarPartIDs...),
		},
		"deck_rank":       deck.DeckRank,
		"leader_card_idx": deck.LeaderCardIndex,
		"name":            deck.Name,
		"rental_idx":      deck.Index,
		"play_log_turn":   0,
	}, nil
}

func selectPartnerDeck(decks []deckInfo, arthurType int8) (deckInfo, bool) {
	for _, deck := range decks {
		if deck.ArthurType == arthurType && deck.IsActive != 0 {
			return deck, true
		}
	}
	for _, deck := range decks {
		if deck.ArthurType == arthurType {
			return deck, true
		}
	}
	return deckInfo{}, false
}

func selectCompletePartnerDeck(
	decks []deckInfo,
	arthurType int8,
	cards map[int64]cardInfo,
) (deckInfo, bool) {
	const battleDeckCardCount = 10
	complete := func(deck deckInfo) bool {
		if deck.ArthurType != arthurType || len(deck.CardUniqueIDs) != battleDeckCardCount {
			return false
		}
		for _, uniqueID := range deck.CardUniqueIDs {
			if uniqueID <= 0 {
				return false
			}
			if _, found := cards[uniqueID]; !found {
				return false
			}
		}
		return true
	}
	for _, deck := range decks {
		if deck.IsActive != 0 && complete(deck) {
			return deck, true
		}
	}
	for _, deck := range decks {
		if complete(deck) {
			return deck, true
		}
	}
	return deckInfo{}, false
}

func partnerLeaderCard(deck deckInfo, cards map[int64]cardInfo) (cardInfo, bool) {
	leaderIndex := int(deck.LeaderCardIndex)
	if leaderIndex >= 0 && leaderIndex < len(deck.CardUniqueIDs) {
		if card, exists := cards[deck.CardUniqueIDs[leaderIndex]]; exists {
			return card, true
		}
	}
	for _, uniqueID := range deck.CardUniqueIDs {
		if card, exists := cards[uniqueID]; exists {
			return card, true
		}
	}
	return cardInfo{}, false
}

func partnerDeckStats(
	deck deckInfo,
	cards map[int64]cardInfo,
	job release.JobParameter,
) (int, int, int, int) {
	hp, attack, magic, mind := job.HP, job.Attack, job.Magic, job.Mind
	for index, uniqueID := range deck.CardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			continue
		}
		if index == int(deck.LeaderCardIndex) {
			// CN battle5_api_leader_card_param_calc: each card parameter *150/100,
			// truncated separately. Job stats, support cards and skill power are
			// not leader parameters.
			card.HP = card.HP * 150 / 100
			card.Attack = card.Attack * 150 / 100
			card.Magic = card.Magic * 150 / 100
			card.Mind = card.Mind * 150 / 100
		}
		hp += card.HP
		attack += card.Attack
		magic += card.Magic
		mind += card.Mind
	}
	for _, uniqueID := range deck.SupportCardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			continue
		}
		// CN CardUtility truncates getLovePer before calling native
		// battle5_api_support_card_param_calc. card_param_bonus.csv SUPPORT
		// supplies HP/ATK/MAG/MIND rates 60/200/200/100; native scales them
		// by (10 + 90*lovePercent/100) percent, then truncates each stat.
		lovePercent := 100 // CardCsvData.getLovePer: cards without a love cap use 100%.
		if card.LoveMax > 0 {
			lovePercent = max(0, min(100, card.Love*100/card.LoveMax))
		}
		scale := float64(lovePercent)*90/100 + 10
		hp += int((60 * scale / 100) * float64(card.HP) / 100)
		attack += int((200 * scale / 100) * float64(card.Attack) / 100)
		magic += int((200 * scale / 100) * float64(card.Magic) / 100)
		mind += int((100 * scale / 100) * float64(card.Mind) / 100)
	}
	return hp, attack, magic, mind
}

func partnerCardWire(card cardInfo) map[string]any {
	return map[string]any{
		"cardid":   card.CardID,
		"lv":       card.Level,
		"skill_lv": append([]int16(nil), card.SkillLevels...),
		"love":     card.Love,
		"hp":       card.HP,
		"atkp":     card.Attack,
		"intp":     card.Magic,
		"mndp":     card.Mind,
		"fame":     card.Fame,
	}
}
