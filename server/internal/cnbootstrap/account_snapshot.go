package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/internal/release"
)

// Bound the remaining account metadata separately from the 4 MiB seed limit.
// Inventory and presents live in individual SQL rows, not in this payload.
const maxCNAccountSnapshotBytes = 16 * 1024 * 1024

// cnAccountSnapshot is the durable account contract. Public definitions are
// loaded from the versioned seed/runtime catalog, never copied into these rows.
// Global operator changes remain in cn_global_operation.
type cnAccountSnapshot struct {
	InventorySequence              release.InventorySequenceState          `json:"inventory_sequence,omitempty"`
	BurstProgress                  [4]uint8                                `json:"burst_progress"`
	Navigation                     release.NavigationState                 `json:"navigation,omitempty"`
	SchemaVersion                  int                                     `json:"schema_version"`
	TeamBattleScores               map[int]release.TeamBattleScoreProgress `json:"team_battle_scores,omitempty"`
	LocalShop                      release.LocalShopState                  `json:"local_shop,omitempty"`
	User                           release.User                            `json:"user"`
	PlayerProgressionConfigVersion int                                     `json:"player_progression_config_version"`
	CardProgressionConfigVersion   int                                     `json:"card_progression_config_version"`
	FeatureUnlockConfigVersion     int                                     `json:"feature_unlock_config_version"`
	Onboarding                     release.OnboardingState                 `json:"onboarding"`
	ProfileConfigVersion           int                                     `json:"profile_config_version"`
	CurrencyConfigVersion          int                                     `json:"currency_config_version"`
	BattleLoadoutConfigVersion     int                                     `json:"battle_loadout_config_version"`
	BattleLoadoutCardUniqueIDs     []int64                                 `json:"battle_loadout_card_unique_ids,omitempty"`
	BattlePointConfigVersion       int                                     `json:"battle_point_config_version"`
	BattlePoint                    release.BattlePointState                `json:"battle_point"`
	PVPConfigVersion               int                                     `json:"pvp_config_version"`
	PVP                            release.PVPPlayerState                  `json:"pvp"`
	SphereConfigVersion            int                                     `json:"sphere_config_version"`
	Spheres                        []release.Sphere                        `json:"spheres"`
	Decks                          []release.Deck                          `json:"decks"`
	Avatars                        []release.Avatar                        `json:"avatars"`
	AvatarConfigVersion            int                                     `json:"avatar_config_version,omitempty"`
	AvatarParts                    []int                                   `json:"avatar_parts,omitempty"`
	Costume                        json.RawMessage                         `json:"costume,omitempty"`
	Buddy                          release.Buddy                           `json:"buddy"`
	BuddyConfigVersion             int                                     `json:"buddy_config_version,omitempty"`
	Buddies                        []release.Buddy                         `json:"buddies,omitempty"`
	Friends                        release.FriendCollectionState           `json:"friends"`
	Stamps                         release.StampCollectionState            `json:"stamps"`
	Honors                         release.HonorCollectionState            `json:"honors"`
	ExploreConfigVersion           int                                     `json:"explore_config_version"`
	Engagement                     release.EngagementState                 `json:"engagement"`
	Options                        release.OptionState                     `json:"options"`
	LoginBonus                     release.LoginBonusState                 `json:"login_bonus"`
	LocalAccountConfigVersion      int                                     `json:"local_account_config_version"`
	EventShopPurchases             []release.EventShopPurchase             `json:"event_shop_purchases,omitempty"`
	TradeShopPurchases             []release.TradeShopPurchase             `json:"trade_shop_purchases,omitempty"`
	GachaSelections                []release.GachaSelection                `json:"gacha_selections,omitempty"`
	GachaDailyClaims               []release.GachaDailyClaim               `json:"gacha_daily_claims,omitempty"`
	FriendPointInboxCursor         int64                                   `json:"friend_point_inbox_cursor,omitempty"`
	CardDevelopment                release.CardDevelopmentState            `json:"card_development"`
	SupportDeckConfigVersion       int                                     `json:"support_deck_config_version,omitempty"`
	SupportDeck                    release.SupportDeckState                `json:"support_deck"`
	TeamBattleSchedule             release.TeamBattleScheduleState         `json:"team_battle_schedule,omitempty"`
	TeamBattleResultReceipts       []release.TeamBattleResultReceipt       `json:"team_battle_result_receipts,omitempty"`
	TeamBattleStartReceipts        []release.TeamBattleStartReceipt        `json:"team_battle_start_receipts,omitempty"`
	TeamBattleContinueReceipts     []release.TeamBattleContinueReceipt     `json:"team_battle_continue_receipts,omitempty"`
	TeamBattleSoloResultReceipts   []release.TeamBattleSoloResultReceipt   `json:"team_battle_solo_result_receipts,omitempty"`
	ExploreResultReceipt           *release.ExploreResultReceipt           `json:"explore_result_receipt,omitempty"`
	PVPResultReceipts              []release.PVPResultReceipt              `json:"pvp_result_receipts,omitempty"`
	TowerQuestProgress             []release.TowerQuestProgress            `json:"tower_quest_progress,omitempty"`
	Progress                       cnCatalogProgress                       `json:"progress"`
	ExploreProgress                cnExploreProgress                       `json:"explore_progress"`
}

type cnExploreProgress struct {
	StageCursor        int   `json:"stage_cursor"`
	ActiveStageID      int   `json:"active_stage_id"`
	Active             bool  `json:"active"`
	ArthurType         int8  `json:"arthur_type"`
	DeckIndex          int8  `json:"deck_index"`
	StartedAtUnix      int64 `json:"started_at_unix"`
	APNextRecoveryUnix int64 `json:"ap_next_recovery_unix"`
}

func accountSnapshotFromState(state release.State) (cnAccountSnapshot, error) {
	progress, err := collectCNCatalogProgress(state)
	if err != nil {
		return cnAccountSnapshot{}, err
	}
	return cnAccountSnapshot{
		InventorySequence:              state.InventorySequence,
		Navigation:                     state.Navigation,
		BurstProgress:                  state.BurstProgress,
		SchemaVersion:                  cnSaveSnapshotSchemaVersion,
		TeamBattleScores:               state.TeamBattleScores,
		LocalShop:                      state.LocalShop,
		User:                           state.User,
		PlayerProgressionConfigVersion: state.PlayerProgressionConfigVersion,
		CardProgressionConfigVersion:   state.CardProgressionConfigVersion,
		FeatureUnlockConfigVersion:     state.FeatureUnlockConfigVersion,
		Onboarding:                     state.Onboarding,
		ProfileConfigVersion:           state.ProfileConfigVersion,
		CurrencyConfigVersion:          state.CurrencyConfigVersion,
		BattleLoadoutConfigVersion:     state.BattleLoadoutConfigVersion,
		BattleLoadoutCardUniqueIDs:     state.BattleLoadoutCardUniqueIDs,
		BattlePointConfigVersion:       state.BattlePointConfigVersion,
		BattlePoint:                    state.BattlePoint,
		PVPConfigVersion:               state.PVPConfigVersion,
		PVP:                            state.PVP,
		SphereConfigVersion:            state.SphereConfigVersion,
		Spheres:                        state.Spheres,
		Decks:                          state.Decks,
		Avatars:                        state.Avatars,
		AvatarConfigVersion:            state.AvatarConfigVersion,
		AvatarParts:                    state.AvatarParts,
		Costume:                        state.Costume,
		Buddy:                          state.Buddy,
		BuddyConfigVersion:             state.BuddyConfigVersion,
		Buddies:                        state.Buddies,
		Friends:                        release.FriendCollectionState{FollowMax: state.Friends.FollowMax, Users: []release.Friend{}},
		Stamps:                         state.Stamps,
		Honors:                         state.Honors,
		ExploreConfigVersion:           state.ExploreConfigVersion,
		Engagement:                     state.Engagement,
		Options:                        state.Options,
		LoginBonus:                     state.LoginBonus,
		LocalAccountConfigVersion:      state.LocalAccountConfigVersion,
		EventShopPurchases:             state.EventShopPurchases,
		TradeShopPurchases:             state.TradeShopPurchases,
		GachaSelections:                state.GachaSelections,
		GachaDailyClaims:               state.GachaDailyClaims,
		FriendPointInboxCursor:         state.FriendPointInboxCursor,
		CardDevelopment:                state.CardDevelopment,
		SupportDeckConfigVersion:       state.SupportDeckConfigVersion,
		SupportDeck:                    state.SupportDeck,
		TeamBattleSchedule:             state.TeamBattleSchedule,
		TeamBattleResultReceipts:       state.TeamBattleResultReceipts,
		TeamBattleStartReceipts:        state.TeamBattleStartReceipts,
		TeamBattleContinueReceipts:     state.TeamBattleContinueReceipts,
		TeamBattleSoloResultReceipts:   state.TeamBattleSoloResultReceipts,
		ExploreResultReceipt:           state.ExploreResultReceipt,
		PVPResultReceipts:              state.PVPResultReceipts,
		TowerQuestProgress:             state.TowerQuestProgress,
		Progress:                       progress,
		ExploreProgress: cnExploreProgress{
			StageCursor: state.Explore.StageCursor, ActiveStageID: state.Explore.ActiveStageID,
			Active: state.Explore.Active, ArthurType: state.Explore.ArthurType, DeckIndex: state.Explore.DeckIndex,
			StartedAtUnix: state.Explore.StartedAtUnix, APNextRecoveryUnix: state.Explore.APNextRecoveryUnix,
		},
	}, nil
}

// applyAccountData replaces only account-owned fields. Catalog maps/slices
// may be shared with other accounts and must not be mutated here.
func (snapshot cnAccountSnapshot) applyAccountData(state *release.State) {
	state.InventorySequence = snapshot.InventorySequence
	state.TeamBattleScores = snapshot.TeamBattleScores
	state.LocalShop = snapshot.LocalShop
	state.User = snapshot.User
	state.PlayerProgressionConfigVersion = snapshot.PlayerProgressionConfigVersion
	state.CardProgressionConfigVersion = snapshot.CardProgressionConfigVersion
	state.FeatureUnlockConfigVersion = snapshot.FeatureUnlockConfigVersion
	state.Onboarding = snapshot.Onboarding
	state.ProfileConfigVersion = snapshot.ProfileConfigVersion
	state.CurrencyConfigVersion = snapshot.CurrencyConfigVersion
	state.BattleLoadoutConfigVersion = snapshot.BattleLoadoutConfigVersion
	state.BattleLoadoutCardUniqueIDs = snapshot.BattleLoadoutCardUniqueIDs
	state.BattlePointConfigVersion = snapshot.BattlePointConfigVersion
	state.BattlePoint = snapshot.BattlePoint
	state.PVPConfigVersion = snapshot.PVPConfigVersion
	state.PVP = snapshot.PVP
	state.SphereConfigVersion = snapshot.SphereConfigVersion
	state.Spheres = snapshot.Spheres
	state.Decks = snapshot.Decks
	state.Avatars = snapshot.Avatars
	state.AvatarConfigVersion = snapshot.AvatarConfigVersion
	state.AvatarParts = snapshot.AvatarParts
	if len(snapshot.Costume) > 0 {
		state.Costume = snapshot.Costume
	}
	state.Buddy = snapshot.Buddy
	state.BuddyConfigVersion = snapshot.BuddyConfigVersion
	state.Buddies = snapshot.Buddies
	state.Friends = snapshot.Friends
	state.Stamps = snapshot.Stamps
	state.Honors = snapshot.Honors
	state.ExploreConfigVersion = snapshot.ExploreConfigVersion
	state.Engagement = snapshot.Engagement
	state.Options = snapshot.Options
	state.LoginBonus = snapshot.LoginBonus
	state.LocalAccountConfigVersion = snapshot.LocalAccountConfigVersion
	state.EventShopPurchases = snapshot.EventShopPurchases
	state.TradeShopPurchases = snapshot.TradeShopPurchases
	state.GachaSelections = snapshot.GachaSelections
	state.GachaDailyClaims = snapshot.GachaDailyClaims
	state.FriendPointInboxCursor = snapshot.FriendPointInboxCursor
	state.CardDevelopment = snapshot.CardDevelopment
	state.SupportDeckConfigVersion = snapshot.SupportDeckConfigVersion
	state.SupportDeck = snapshot.SupportDeck
	state.TeamBattleSchedule = snapshot.TeamBattleSchedule
	state.TeamBattleResultReceipts = snapshot.TeamBattleResultReceipts
	state.TeamBattleStartReceipts = snapshot.TeamBattleStartReceipts
	state.TeamBattleContinueReceipts = snapshot.TeamBattleContinueReceipts
	state.TeamBattleSoloResultReceipts = snapshot.TeamBattleSoloResultReceipts
	state.ExploreResultReceipt = snapshot.ExploreResultReceipt
	state.PVPResultReceipts = snapshot.PVPResultReceipts
	state.ActiveTeamBattle = nil
	state.Navigation = snapshot.Navigation
	state.BurstProgress = snapshot.BurstProgress
	state.TowerQuestProgress = snapshot.TowerQuestProgress
	state.SchemaVersion = 1
	state.Explore.StageCursor = snapshot.ExploreProgress.StageCursor
	state.Explore.ActiveStageID = snapshot.ExploreProgress.ActiveStageID
	state.Explore.Active = snapshot.ExploreProgress.Active
	state.Explore.ArthurType = snapshot.ExploreProgress.ArthurType
	state.Explore.DeckIndex = snapshot.ExploreProgress.DeckIndex
	state.Explore.StartedAtUnix = snapshot.ExploreProgress.StartedAtUnix
	state.Explore.APNextRecoveryUnix = snapshot.ExploreProgress.APNextRecoveryUnix
}

// Large inventories and balances have relational owners. Keep only the small
// configuration/progress fragments here; no duplicate zero-valued wallet fields.
func encodeCNAccountMetadata(state release.State) ([]byte, error) {
	if err := validateCNSave(cnSaveFromState(state)); err != nil {
		return nil, err
	}
	snapshot, err := accountSnapshotFromState(state)
	if err != nil {
		return nil, err
	}
	type metadataUser struct {
		release.User
		Gold        *int `json:"gold,omitempty"`
		Coin        *int `json:"coin,omitempty"`
		CoinFree    *int `json:"coin_free,omitempty"`
		FriendPoint *int `json:"friend_point,omitempty"`
		PVPPoint    *int `json:"pvp_point,omitempty"`
	}
	type metadataEngagement struct {
		Initialized  bool              `json:"initialized"`
		Missions     []release.Mission `json:"missions"`
		PopupReadIDs []int             `json:"popup_read_ids,omitempty"`
	}
	content, err := json.Marshal(struct {
		cnAccountSnapshot
		User       metadataUser       `json:"user"`
		Engagement metadataEngagement `json:"engagement"`
	}{snapshot, metadataUser{User: snapshot.User}, metadataEngagement{snapshot.Engagement.Initialized, snapshot.Engagement.Missions, snapshot.Engagement.PopupReadIDs}})
	if err != nil {
		return nil, err
	}
	if len(content) > maxCNAccountSnapshotBytes {
		return nil, errors.New("CN account metadata exceeds the size limit")
	}
	return content, nil
}

// Master application can mutate slices in place. Capture a digest beforehand,
// streaming inventory one record at a time instead of cloning/encoding one
// full account document. This is only used when constructing a handler.
func fingerprintCNAccountState(state release.State) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	metadata, err := encodeCNAccountMetadata(state)
	if err != nil {
		return result, err
	}
	digest := sha256.New()
	digest.Write(metadata)
	encoder := json.NewEncoder(digest)
	u := state.User
	if err := encoder.Encode([5]int{u.Gold, u.Coin, u.CoinFree, u.FriendPoint, u.PVPPoint}); err != nil {
		return result, err
	}
	for _, cards := range [][]release.Card{state.Cards, state.ContainerCards} {
		if err := fingerprintCNRecords(encoder, cards); err != nil {
			return result, err
		}
	}
	if err := fingerprintCNRecords(encoder, state.Items); err != nil {
		return result, err
	}
	if err := fingerprintCNRecords(encoder, state.StackCards); err != nil {
		return result, err
	}
	for _, presents := range [][]release.Present{state.Engagement.Presents, state.Engagement.Histories} {
		if err := fingerprintCNRecords(encoder, presents); err != nil {
			return result, err
		}
	}
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func fingerprintCNRecords[T any](encoder *json.Encoder, records []T) error {
	if err := encoder.Encode(len(records)); err != nil {
		return err
	}
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return nil
}

func decodeCNAccountSnapshot(content []byte) (cnAccountSnapshot, error) {
	var snapshot cnAccountSnapshot
	if len(content) == 0 || len(content) > maxCNAccountSnapshotBytes {
		return snapshot, errors.New("CN account data is empty or exceeds the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return snapshot, fmt.Errorf("decode CN account data: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return snapshot, err
	}
	if snapshot.SchemaVersion != cnSaveSnapshotSchemaVersion || snapshot.User.UserID < cnPrimaryUserID {
		return snapshot, errors.New("unsupported CN account data schema or identity; use a fresh database")
	}
	return snapshot, nil
}

func (storage *cnSaveDatabase) decodeAccount(tx *sql.Tx, content []byte) (release.State, error) {
	snapshot, err := decodeCNAccountSnapshot(content)
	if err != nil {
		return release.State{}, err
	}
	state, err := storage.catalogState()
	if err != nil {
		return release.State{}, err
	}
	// Minimal test/seed configurations keep templates in their initial cards;
	// production always supplies the complete immutable CardTemplates master.
	if len(state.CardTemplates) == 0 {
		state.CardTemplates = state.Cards
	}
	snapshot.applyAccountData(&state)
	if err := readCNAccountRows(tx, &state); err != nil {
		return release.State{}, err
	}
	// A fresh seed has no tower progress yet; definitions are public, while
	// the initial floor belongs to the newly created account.
	for _, profile := range state.TowerQuestProfiles {
		found := false
		for _, progress := range state.TowerQuestProgress {
			found = found || progress.TowerID == profile.TowerID
		}
		if !found {
			state.TowerQuestProgress = append(state.TowerQuestProgress, release.TowerQuestProgress{TowerID: profile.TowerID, Floor: 1})
		}
	}
	if snapshot.Onboarding.ConfigVersion == cnOnboardingConfigVersion {
		if err := installCNOnboardingGacha(&state); err != nil {
			return release.State{}, err
		}
	}
	if err := snapshot.Progress.apply(&state); err != nil {
		return release.State{}, err
	}
	if err := validateCNSave(cnSaveFromState(state)); err != nil {
		return release.State{}, fmt.Errorf("validate CN account data: %w", err)
	}
	return state, nil
}

func (storage *cnSaveDatabase) catalogState() (release.State, error) {
	if storage.catalog != nil {
		return *storage.catalog, nil
	}
	return loadCNSaveState(storage.seedPath)
}
