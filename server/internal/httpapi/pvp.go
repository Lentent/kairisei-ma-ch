package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"kairisei.local/server/internal/release"
)

const pvpHistoryLimit = 50

const (
	pvpEngineServerReplay      = "server_replay"
	pvpEngineClientNativeLocal = "client_native_local"
)

type PVPFieldConfig struct {
	FieldID int `json:"fieldid"`
	MapID   int `json:"mapid"`
	BGMID   int `json:"bgmid"`
}

type PVPRankConfig struct {
	RankID   int    `json:"rank_id"`
	Name     string `json:"name"`
	PointMin int    `json:"point_min"`
	PointMax int    `json:"point_max"`
	WinCoin  int    `json:"win_coin"`
}

type PVPResultCommand struct {
	InputAPI  int    `json:"input_api"`
	ResultCmd string `json:"result_cmd"`
}

type PVPSourceFile struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// PVPConfig contains static local rules. Official client-owned field and rank
// rows are kept separate from mutable account state in SQLite.
type PVPConfig struct {
	SchemaVersion   int                `json:"schema_version"`
	ClientProfile   string             `json:"client_profile"`
	ConfigVersion   int                `json:"config_version"`
	OfficialSources []PVPSourceFile    `json:"official_sources"`
	PolicyEvidence  string             `json:"policy_evidence"`
	EngineMode      string             `json:"engine_mode"`
	ChallengeMax    int                `json:"challenge_max"`
	CostInitial     int                `json:"cost_initial"`
	TurnMax         int                `json:"turn_max"`
	HoldMax         int                `json:"hold_max"`
	GimmickID       int                `json:"gimmickid"`
	RankWinPoint    int                `json:"rank_win_point"`
	RankLosePoint   int                `json:"rank_lose_point"`
	ReplayResult    int                `json:"replay_result"`
	ResetStartTime  int                `json:"reset_start_time"`
	ResetEndTime    int                `json:"reset_end_time"`
	Fields          []PVPFieldConfig   `json:"fields"`
	Ranks           []PVPRankConfig    `json:"ranks"`
	ResultCommands  []PVPResultCommand `json:"result_cmds"`
}

type PVPAccountRepository interface {
	ListPVPOpponents(userID int) ([]release.State, error)
}

type pvpShowResponse struct {
	Result    int `json:"result"`
	PVPPoint  int `json:"pvp_point"`
	Challenge int `json:"challenge"`
	PVPReset  struct {
		StartTime int `json:"start_time"`
		EndTime   int `json:"end_time"`
	} `json:"pvp_reset"`
}

type pvpEnemyCardInfo struct {
	CardID int `json:"cardid"`
	Level  int `json:"lv"`
	Fame   int `json:"fame"`
	Love   int `json:"love"`
}

type pvpSphereInfo struct {
	SphereID int `json:"sphrid"`
	Level    int `json:"lv"`
}

type pvpBuddyInfo struct {
	BuddyID int `json:"buddyid"`
	Level   int `json:"lv"`
}

type pvpEnemyDeckInfo struct {
	ArthurType      int8               `json:"arthur_type"`
	JobType         int8               `json:"job_type"`
	IsBurst         int8               `json:"is_burst"`
	Level           int                `json:"lv"`
	HP              int                `json:"hp"`
	Attack          int                `json:"atkp"`
	Magic           int                `json:"intp"`
	Mind            int                `json:"mndp"`
	LeaderCardIndex int                `json:"leader_card_idx"`
	Cards           []pvpEnemyCardInfo `json:"cards"`
	SupportCards    []pvpEnemyCardInfo `json:"support_cards"`
	Spheres         []pvpSphereInfo    `json:"sphrs"`
	Avatar          release.Avatar     `json:"avatars"`
	Buddies         []pvpBuddyInfo     `json:"buddys"`
}

type pvpStartResponse struct {
	BattleID       int                `json:"btluid"`
	FieldID        int                `json:"fieldid"`
	GimmickID      int                `json:"gimmickid"`
	CostInitial    int                `json:"cost_initial"`
	TurnMax        int                `json:"turn_max"`
	Seed           int                `json:"seed"`
	EnemyInfo      map[string]any     `json:"enemy_info"`
	EnemyDecks     []pvpEnemyDeckInfo `json:"pvp_enemy_deck"`
	MyCostumeIDs   []int              `json:"pvp_my_costumeid"`
	HoldMax        int                `json:"hold_max"`
	ResultCommands []PVPResultCommand `json:"result_cmds"`
	DeckHonorIDs   []int              `json:"deck_honorids"`
	EnemyHonorIDs  []int              `json:"enemy_deck_honorids"`
	Result         int                `json:"result"`
	PointBefore    int                `json:"bf_pvp_point"`
	PointAfter     int                `json:"af_pvp_point"`
	Challenge      int                `json:"challenge"`
}

func (a *API) refreshPVPChallenges(writer http.ResponseWriter, now time.Time) bool {
	if err := a.store.refreshPVPChallenges(a.pvpConfig, now, a.release.State, a.persistState); err != nil {
		writeError(writer, http.StatusInternalServerError, "save daily PVP challenges")
		return false
	}
	return true
}

func (a *API) pvpShow(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("{}")) {
		writeError(writer, http.StatusBadRequest, "request fields differ from the PVP show contract")
		return
	}
	if !a.refreshPVPChallenges(writer, time.Now()) {
		return
	}
	point, challenge := a.store.pvpStatus()
	response := pvpShowResponse{Result: 0, PVPPoint: point, Challenge: challenge}
	response.PVPReset.StartTime = a.pvpConfig.ResetStartTime
	response.PVPReset.EndTime = a.pvpConfig.ResetEndTime
	a.writeProtocol(writer, response)
}

func (a *API) pvpStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BattleType       int                        `json:"type"`
		SelectArthurType int                        `json:"select_arthur_type"`
		MyDecks          []release.PVPDeckSelection `json:"pvp_my_deck"`
	}
	if err := decodeExact(request, []string{"type", "select_arthur_type", "pvp_my_deck"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.BattleType < 0 || payload.BattleType > 1 || payload.SelectArthurType < 1 || payload.SelectArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid PVP mode or selected Arthur")
		return
	}
	if a.pvpAccounts == nil {
		writeError(writer, http.StatusServiceUnavailable, "local PVP account repository is unavailable")
		return
	}
	if a.pvpConfig.EngineMode == pvpEngineServerReplay && len(a.pvpConfig.ResultCommands) == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local PVP replay is not configured")
		return
	}
	a.clientResultMu.Lock()
	defer a.clientResultMu.Unlock()
	if !a.refreshPVPChallenges(writer, time.Now()) {
		return
	}
	opponents, err := a.pvpAccounts.ListPVPOpponents(a.release.State.User.UserID)
	if err != nil {
		a.logger.Error("list local PVP opponents", "error", err)
		writeError(writer, http.StatusInternalServerError, "list local PVP opponents")
		return
	}
	if len(opponents) == 0 {
		a.writeProtocolResult(writer, nil, -1200, "暂无可用的本地竞技场对手，请在其他玩家建立卡组后重试。")
		return
	}
	point, _, nextBattleID := a.store.pvpStatusWithBattleID()
	sort.SliceStable(opponents, func(left, right int) bool {
		leftDistance := absInt(opponents[left].User.PVPPoint - point)
		rightDistance := absInt(opponents[right].User.PVPPoint - point)
		if leftDistance != rightDistance {
			return leftDistance < rightDistance
		}
		return opponents[left].User.UserID < opponents[right].User.UserID
	})
	var opponent release.State
	var enemyDecks []pvpEnemyDeckInfo
	// A saved defense can reference inventory its owner has since moved or
	// consumed. Try the remaining accounts before rejecting the challenge.
	for offset := range opponents {
		candidate := opponents[(nextBattleID-1+offset)%len(opponents)]
		decks, err := buildPVPEnemyDecks(candidate)
		if err != nil {
			continue
		}
		opponent, enemyDecks = candidate, decks
		break
	}
	if len(enemyDecks) == 0 {
		a.writeProtocolResult(writer, nil, -1200, "暂无可用的竞技场防守卡组，请稍后重试。")
		return
	}
	match, challenge, err := a.store.beginPVP(payload.BattleType, opponent.User.UserID, payload.MyDecks, a.pvpConfig, time.Now())
	if err != nil {
		if errors.Is(err, errInvalidPVPDeck) {
			a.writeStoreError(writer, err)
		} else {
			// Stock PvPSt.checkProtoStartError handles -1200 by closing the
			// dialog and returning to the arena selection, without logging out.
			a.writeProtocolResult(writer, nil, -1200, err.Error())
		}
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	field := a.pvpConfig.Fields[(match.BattleID-1)%len(a.pvpConfig.Fields)]
	myAvatars := a.store.avatarsState()
	myCostumeIDs := make([]int, 4)
	for index := range myCostumeIDs {
		if index < len(myAvatars) {
			myCostumeIDs[index] = myAvatars[index].CostumeID
		}
	}
	myHonorIDs, _ := a.store.honorState()
	resultCommands := append([]PVPResultCommand{}, a.pvpConfig.ResultCommands...)
	if a.pvpConfig.EngineMode == pvpEngineClientNativeLocal && len(resultCommands) == 0 {
		// The CN 6.0.2 deserializer leaves result_cmds null when the JSON array
		// is empty, while PvPMgr.onPvpStart unconditionally reads its Length.
		// Native-local PVP does not consume playback commands; an unknown no-op
		// input_api keeps the original parser contract without driving playback.
		resultCommands = append(resultCommands, PVPResultCommand{InputAPI: 0, ResultCmd: ""})
	}
	a.writeProtocol(writer, pvpStartResponse{
		BattleID:    match.BattleID,
		FieldID:     field.FieldID,
		GimmickID:   a.pvpConfig.GimmickID,
		CostInitial: a.pvpConfig.CostInitial,
		TurnMax:     a.pvpConfig.TurnMax,
		Seed:        match.BattleID,
		EnemyInfo: map[string]any{
			"userid": opponent.User.UserID, "name": opponent.User.Name,
			"pvp_point": opponent.User.PVPPoint, "state": 0,
			"last_login_time": opponent.LastLoginUnix, "comment": opponent.User.Comment,
		},
		EnemyDecks:     enemyDecks,
		MyCostumeIDs:   myCostumeIDs,
		HoldMax:        a.pvpConfig.HoldMax,
		ResultCommands: resultCommands,
		DeckHonorIDs:   myHonorIDs,
		EnemyHonorIDs:  append([]int(nil), opponent.Honors.DeckHonorIDs...),
		Result:         boolInt(match.ExpectedWin),
		PointBefore:    match.PointBefore,
		PointAfter:     match.PointAfter,
		Challenge:      challenge,
	})
}

func (a *API) pvpEnd(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BattleID int      `json:"btluid"`
		IsWin    int8     `json:"is_win"`
		IsRetire int8     `json:"is_retire"`
		InputCmd []string `json:"input_cmd"`
	}
	if err := decodeExact(request, []string{"btluid", "is_win", "is_retire", "input_cmd"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if (payload.IsWin != 0 && payload.IsWin != 1) || (payload.IsRetire != 0 && payload.IsRetire != 1) {
		writeError(writer, http.StatusBadRequest, "invalid PVP settlement flags")
		return
	}
	if a.pvpConfig.EngineMode == pvpEngineClientNativeLocal {
		if len(payload.InputCmd) != 1 {
			writeError(writer, http.StatusBadRequest, "native PVP report does not contain exactly one battle command")
			return
		}
		commandRequired := payload.IsRetire == 0
		if err := validateNativeBattleCommandStart(payload.InputCmd[0], commandRequired, nativePVPCommandStartOpcode); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid native PVP report: "+err.Error())
			return
		}
	}
	canonicalRequest, err := json.Marshal(payload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode PVP result identity")
		return
	}
	requestDigest := fmt.Sprintf("%x", sha256.Sum256(canonicalRequest))
	a.clientResultMu.Lock()
	defer a.clientResultMu.Unlock()
	if receipt, exists := a.store.pvpResultReceipt(payload.BattleID); exists {
		if receipt.RequestSHA256 != requestDigest {
			writeError(writer, http.StatusConflict, "PVP result conflicts with the completed battle")
			return
		}
		a.writeProtocol(writer, receipt.Response)
		return
	}
	match, challenge, result, err := a.store.finishPVP(
		payload.BattleID, payload.IsWin == 1, payload.IsRetire == 1,
		a.pvpConfig, time.Now(),
	)
	if err != nil {
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	response := map[string]any{
		"result": result, "bf_pvp_point": match.PointBefore,
		"af_pvp_point": match.PointAfter, "challenge": challenge,
	}
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode PVP settlement")
		return
	}
	if err := a.store.recordPVPResultReceipt(payload.BattleID, requestDigest, encodedResponse, time.Now()); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, json.RawMessage(encodedResponse))
}

func buildPVPEnemyDecks(state release.State) ([]pvpEnemyDeckInfo, error) {
	selections := clonePVPDeckSelections(state.PVP.DefenseDecks)
	if len(selections) != 4 {
		selections = activePVPDeckSelections(state.Decks)
	}
	if len(selections) != 4 {
		return nil, errors.New("opponent does not have four PVP decks")
	}
	cards := make(map[int64]cardInfo, len(state.Cards))
	for _, card := range cardInfosFromRelease(state.Cards, 0) {
		cards[card.UniqueID] = card
	}
	spheres := make(map[int64]release.Sphere, len(state.Spheres))
	for _, sphere := range state.Spheres {
		spheres[sphere.UniqueID] = sphere
	}
	buddies := make(map[int64]release.Buddy, len(state.Buddies))
	for _, buddy := range state.Buddies {
		buddies[buddy.UniqueID] = buddy
	}
	if len(buddies) == 0 && state.Buddy.UniqueID > 0 && state.Buddy.BuddyID > 0 {
		buddies[state.Buddy.UniqueID] = state.Buddy
	}
	result := make([]pvpEnemyDeckInfo, 0, 4)
	for _, selection := range selections {
		if selection.ArthurType < 1 || selection.ArthurType > 4 {
			return nil, errors.New("opponent PVP defense has an invalid Arthur type")
		}
		deck := pvpEnemyDeckInfo{
			IsBurst:    release.ArthurBurstUnlocked(state.User.UnlockedFeatureIDs, selection.ArthurType),
			ArthurType: selection.ArthurType, JobType: selection.JobType,
			Level: state.User.Level, LeaderCardIndex: selection.LeaderCardIndex,
			Cards: []pvpEnemyCardInfo{}, SupportCards: []pvpEnemyCardInfo{},
			Spheres: []pvpSphereInfo{}, Buddies: []pvpBuddyInfo{},
		}
		jobIndex := int(selection.JobType)
		var job release.JobParameter
		if jobIndex >= 0 && jobIndex < len(state.User.Jobs) {
			job = state.User.Jobs[jobIndex]
		}
		if len(state.Avatars) >= int(selection.ArthurType) {
			deck.Avatar = cloneAvatar(state.Avatars[int(selection.ArthurType)-1])
		} else {
			deck.Avatar = release.Avatar{AvatarPartIDs: []int{0, 0, 0, 0, 0, 0, 0}}
		}
		appendCard := func(uniqueID int64, target *[]pvpEnemyCardInfo) error {
			if uniqueID == 0 {
				return nil
			}
			card, exists := cards[uniqueID]
			if !exists {
				return fmt.Errorf("opponent PVP defense references unknown card %d", uniqueID)
			}
			*target = append(*target, pvpEnemyCardInfo{CardID: card.CardID, Level: card.Level, Fame: card.Fame, Love: card.Love})
			return nil
		}
		for _, uniqueID := range selection.CardUniqueIDs {
			if err := appendCard(uniqueID, &deck.Cards); err != nil {
				return nil, err
			}
		}
		for _, uniqueID := range selection.SupportCardUniqueIDs {
			if err := appendCard(uniqueID, &deck.SupportCards); err != nil {
				return nil, err
			}
		}
		for _, uniqueID := range selection.SphereUniqueIDs {
			if uniqueID == 0 {
				continue
			}
			sphere, exists := spheres[uniqueID]
			if !exists {
				return nil, fmt.Errorf("opponent PVP defense references unknown sphere %d", uniqueID)
			}
			deck.Spheres = append(deck.Spheres, pvpSphereInfo{SphereID: sphere.SphereID, Level: sphere.Level})
		}
		for _, uniqueID := range selection.BuddyUniqueIDs {
			if uniqueID == 0 {
				continue
			}
			buddy, exists := buddies[uniqueID]
			if !exists {
				return nil, fmt.Errorf("opponent PVP defense references unknown buddy %d", uniqueID)
			}
			deck.Buddies = append(deck.Buddies, pvpBuddyInfo{BuddyID: buddy.BuddyID, Level: buddy.Level})
		}
		deck.HP, deck.Attack, deck.Magic, deck.Mind = partnerDeckStats(deckInfo{
			LeaderCardIndex:      int8(selection.LeaderCardIndex),
			CardUniqueIDs:        selection.CardUniqueIDs,
			SupportCardUniqueIDs: selection.SupportCardUniqueIDs,
		}, cards, job)
		if deck.HP <= 0 || len(deck.Cards) == 0 {
			return nil, errors.New("opponent PVP defense has no usable cards")
		}
		result = append(result, deck)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ArthurType < result[right].ArthurType })
	return result, nil
}

func activePVPDeckSelections(decks []release.Deck) []release.PVPDeckSelection {
	byArthur := make(map[int8]release.Deck, 4)
	for _, deck := range decks {
		current, exists := byArthur[deck.ArthurType]
		if !exists || (deck.IsActive != 0 && current.IsActive == 0) ||
			(deck.IsActive == current.IsActive && deck.Index < current.Index) {
			byArthur[deck.ArthurType] = deck
		}
	}
	result := make([]release.PVPDeckSelection, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		deck, exists := byArthur[arthurType]
		if !exists {
			return nil
		}
		result = append(result, release.PVPDeckSelection{
			ArthurType: deck.ArthurType, JobType: deck.JobType, DeckIndex: deck.Index,
			LeaderCardIndex:      int(deck.LeaderCardIndex),
			CardUniqueIDs:        append([]int64(nil), deck.CardUniqueIDs...),
			SupportCardUniqueIDs: append([]int64(nil), deck.SupportCardUniqueIDs...),
			SphereUniqueIDs:      append([]int64(nil), deck.SphereUniqueIDs...),
			BuddyUniqueIDs:       append([]int64(nil), deck.BuddyUniqueIDs...),
		})
	}
	return result
}

func clonePVPDeckSelections(source []release.PVPDeckSelection) []release.PVPDeckSelection {
	result := make([]release.PVPDeckSelection, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].CardUniqueIDs = append([]int64(nil), source[index].CardUniqueIDs...)
		result[index].SupportCardUniqueIDs = append([]int64(nil), source[index].SupportCardUniqueIDs...)
		result[index].SphereUniqueIDs = append([]int64(nil), source[index].SphereUniqueIDs...)
		result[index].BuddyUniqueIDs = append([]int64(nil), source[index].BuddyUniqueIDs...)
	}
	return result
}

func clonePVPState(source release.PVPPlayerState) release.PVPPlayerState {
	result := source
	result.DefenseDecks = clonePVPDeckSelections(source.DefenseDecks)
	result.History = make([]release.PVPMatch, len(source.History))
	copy(result.History, source.History)
	if source.ActiveMatch != nil {
		active := *source.ActiveMatch
		result.ActiveMatch = &active
	}
	return result
}

func cloneAvatar(source release.Avatar) release.Avatar {
	return release.Avatar{CostumeID: source.CostumeID, AvatarPartIDs: append([]int(nil), source.AvatarPartIDs...)}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
