package cnbootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

const (
	cnPrimaryUserID            = 1000001
	cnSystemPartnerUserIDBase  = 1900000000
	cnSystemPartnerArthurCount = 4
	cnAccountUserHeader        = "X-Kairisei-Local-User-ID"
)

var cnSystemPartnerNameByArthur = map[int8]string{
	1: "新手伙伴·佣兵",
	2: "新手伙伴·富豪",
	3: "新手伙伴·盗贼",
	4: "新手伙伴·歌姬",
}

type cnAccountIdentity struct {
	UserID     int
	LoginUUID  string
	SessionKey string
}

type cnAccountStore struct {
	storage  *cnSaveDatabase
	mu       sync.Mutex
	battleMu sync.Mutex
	battles  map[int]*cnAccountBattleSession
}

// A solo battle needs only a small runtime context, not a retained account
// handler or a durable participation lock. Abandoned sessions expire after a
// hour without a successful account mutation; process exit drops them all.
const cnAccountBattleLifetime = time.Hour

type cnAccountBattleSession struct {
	state *release.TeamBattleActiveState
	story release.StoryTeamBattleSession
	timer *time.Timer
}

func (accounts *cnAccountStore) rememberBattle(userID int, active *release.TeamBattleActiveState, story release.StoryTeamBattleSession) {
	accounts.battleMu.Lock()
	defer accounts.battleMu.Unlock()
	if previous := accounts.battles[userID]; previous != nil {
		previous.timer.Stop()
		delete(accounts.battles, userID)
	}
	if active == nil && story.StoryID == 0 {
		return
	}
	if accounts.battles == nil {
		accounts.battles = make(map[int]*cnAccountBattleSession)
	}
	session := &cnAccountBattleSession{state: cloneActiveTeamBattleState(active), story: story}
	accounts.battles[userID] = session
	session.timer = time.AfterFunc(cnAccountBattleLifetime, func() {
		accounts.battleMu.Lock()
		defer accounts.battleMu.Unlock()
		if accounts.battles[userID] == session {
			delete(accounts.battles, userID)
		}
	})
}

func newCNAccountStore(storage *cnSaveDatabase) (*cnAccountStore, error) {
	if storage == nil {
		return nil, errors.New("CN save database is required")
	}
	accounts := &cnAccountStore{storage: storage}
	database, err := storage.open()
	if err != nil {
		return nil, err
	}
	defer database.Close()
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin CN account schema initialization: %w", err)
	}
	if err := initializeCNSaveSchema(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := initializeCNAccountCredentials(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := accounts.ensureSystemPartnerAccounts(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := storage.syncCNAccountProjections(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit CN account schema initialization: %w", err)
	}
	return accounts, nil
}

func cnSystemPartnerUserID(arthurType int8) int {
	return cnSystemPartnerUserIDBase + int(arthurType)
}

func isCNSystemPartnerUserID(userID int) bool {
	return userID > cnSystemPartnerUserIDBase &&
		userID <= cnSystemPartnerUserIDBase+cnSystemPartnerArthurCount
}

func cnInviteID(userID int) string {
	if isCNSystemPartnerUserID(userID) {
		return fmt.Sprintf("%09d", 900000000+userID-cnSystemPartnerUserIDBase)
	}
	return fmt.Sprintf("%09d", userID)
}

// cnSystemPartnerLoadoutFromSeed keeps the versioned, profession-specific
// battle loadout separate from the deliberately small new-player inventory.
// These decks remain an INFERRED local fallback rather than a recovered
// retired-service partner composition.
func cnSystemPartnerLoadoutFromSeed(
	seed release.State,
	arthurType int8,
) ([]release.Card, release.Deck, release.Card, error) {
	if arthurType < 1 || arthurType > cnSystemPartnerArthurCount {
		return nil, release.Deck{}, release.Card{}, errors.New("CN system-partner Arthur type is invalid")
	}

	var sourceDeck *release.Deck
	for index := range seed.Decks {
		deck := &seed.Decks[index]
		if deck.ArthurType != arthurType || deck.JobType != arthurType || deck.Index != 0 {
			continue
		}
		if sourceDeck == nil || deck.IsActive != 0 {
			sourceDeck = deck
		}
	}
	if sourceDeck == nil || len(sourceDeck.CardUniqueIDs) != 10 {
		return nil, release.Deck{}, release.Card{}, fmt.Errorf(
			"CN system-partner seed has no complete Arthur type %d deck",
			arthurType,
		)
	}
	leaderIndex := int(sourceDeck.LeaderCardIndex)
	if leaderIndex < 0 || leaderIndex >= len(sourceDeck.CardUniqueIDs) {
		return nil, release.Deck{}, release.Card{}, fmt.Errorf(
			"CN system-partner Arthur type %d leader index is invalid",
			arthurType,
		)
	}

	seedCards := make(map[int64]release.Card, len(seed.Cards))
	for _, card := range seed.Cards {
		if card.UniqueID <= 0 {
			continue
		}
		if _, duplicate := seedCards[card.UniqueID]; duplicate {
			return nil, release.Deck{}, release.Card{}, fmt.Errorf(
				"CN system-partner seed card unique ID %d is duplicated",
				card.UniqueID,
			)
		}
		seedCards[card.UniqueID] = card
	}

	cards := make([]release.Card, 0, len(sourceDeck.CardUniqueIDs))
	seen := make(map[int64]struct{}, len(sourceDeck.CardUniqueIDs))
	for _, uniqueID := range sourceDeck.CardUniqueIDs {
		if uniqueID <= 0 {
			return nil, release.Deck{}, release.Card{}, fmt.Errorf(
				"CN system-partner Arthur type %d deck contains an empty card slot",
				arthurType,
			)
		}
		if _, duplicate := seen[uniqueID]; duplicate {
			return nil, release.Deck{}, release.Card{}, fmt.Errorf(
				"CN system-partner Arthur type %d deck repeats card unique ID %d",
				arthurType,
				uniqueID,
			)
		}
		card, exists := seedCards[uniqueID]
		if !exists {
			return nil, release.Deck{}, release.Card{}, fmt.Errorf(
				"CN system-partner Arthur type %d deck card %d is unavailable",
				arthurType,
				uniqueID,
			)
		}
		card.SkillLevels = append([]int16(nil), card.SkillLevels...)
		cards = append(cards, card)
		seen[uniqueID] = struct{}{}
	}

	deck := cloneReleaseDeck(*sourceDeck)
	deck.SupportCardUniqueIDs = make([]int64, len(deck.SupportCardUniqueIDs))
	deck.SphereUniqueIDs = []int64{}
	deck.BuddyUniqueIDs = make([]int64, 5)
	deck.Name = cnDefaultDeckNameByArthur[arthurType]
	deck.IsActive = 1
	deck.IsRental = 0
	return cards, deck, cards[leaderIndex], nil
}

func cnSystemPartnerPopulationFromSeed(
	seed release.State,
) ([]release.Card, []release.Deck, map[int8]release.Card, error) {
	cards := make([]release.Card, 0, len(seed.Cards))
	decks := make([]release.Deck, 0, cnSystemPartnerArthurCount)
	leaders := make(map[int8]release.Card, cnSystemPartnerArthurCount)
	seenCards := make(map[int64]struct{}, len(seed.Cards))
	for arthurType := int8(1); arthurType <= cnSystemPartnerArthurCount; arthurType++ {
		loadoutCards, deck, leader, err := cnSystemPartnerLoadoutFromSeed(seed, arthurType)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, card := range loadoutCards {
			if _, exists := seenCards[card.UniqueID]; exists {
				continue
			}
			cards = append(cards, card)
			seenCards[card.UniqueID] = struct{}{}
		}
		decks = append(decks, deck)
		leaders[arthurType] = leader
	}
	return cards, decks, leaders, nil
}

func (accounts *cnAccountStore) ensureSystemPartnerAccounts(transaction *sql.Tx) error {
	seed, err := loadCNSaveState(accounts.storage.seedPath)
	if err != nil {
		return fmt.Errorf("load CN system-partner seed: %w", err)
	}
	partnerCards, partnerDecks, partnerLeaders, err := cnSystemPartnerPopulationFromSeed(seed)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for arthurType := int8(1); arthurType <= cnSystemPartnerArthurCount; arthurType++ {
		userID := cnSystemPartnerUserID(arthurType)
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_local_account
			 (user_id, login_uuid, session_key, created_utc, last_login_utc)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(user_id) DO NOTHING`,
			userID,
			fmt.Sprintf("system-partner-%d", arthurType),
			fmt.Sprintf("local-cn-system-partner-%d", arthurType),
			now,
			now,
		); err != nil {
			return fmt.Errorf("create CN system-partner account %d: %w", userID, err)
		}
		state := seed
		if err := initializeCNOnboardingSnapshot(&state, userID); err != nil {
			return fmt.Errorf("initialize CN system-partner account %d: %w", userID, err)
		}
		state.Cards = make([]release.Card, len(partnerCards))
		for index, card := range partnerCards {
			card.SkillLevels = append([]int16(nil), card.SkillLevels...)
			state.Cards[index] = card
		}
		state.Decks = make([]release.Deck, len(partnerDecks))
		for index, deck := range partnerDecks {
			state.Decks[index] = cloneReleaseDeck(deck)
		}
		state.BattleLoadoutCardUniqueIDs = make([]int64, 0, len(state.Cards))
		state.SupportDeck.CardCollectionIDs = make([]int, 0, len(state.Cards))
		for _, card := range state.Cards {
			state.BattleLoadoutCardUniqueIDs = append(state.BattleLoadoutCardUniqueIDs, card.UniqueID)
			state.SupportDeck.CardCollectionIDs = append(state.SupportDeck.CardCollectionIDs, card.CardID)
		}
		leader := partnerLeaders[arthurType]
		state.User.Name = cnSystemPartnerNameByArthur[arthurType]
		state.User.Comment = "一起完成最初的一步吧！"
		state.User.ActiveArthurType = int(arthurType)
		state.User.LeaderCardID = leader.CardID
		state.User.LeaderCardUniqueID = leader.UniqueID
		state.User.UnlockedFeatureIDs = append(
			[]uint{uint(arthurType - 1)},
			state.User.UnlockedFeatureIDs...,
		)
		state.User.TutorialFlag = (int64(1) << 26) - 1
		state.Onboarding = release.OnboardingState{
			ConfigVersion: cnOnboardingConfigVersion,
			Step:          cnOnboardingStepCount,
		}
		content, err := encodeCNAccountMetadata(state)
		if err != nil {
			return fmt.Errorf("encode CN system-partner account %d: %w", userID, err)
		}
		if err := writeCNAccountRows(transaction, state); err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_account_snapshot
			 (user_id, schema_version, revision, updated_utc, payload_json, payload_sha256)
			 VALUES (?, ?, 1, ?, ?, ?)
			 ON CONFLICT(user_id) DO UPDATE SET
			  schema_version = excluded.schema_version,
			  revision = cn_account_snapshot.revision + 1,
			  updated_utc = excluded.updated_utc,
			  payload_json = excluded.payload_json,
			  payload_sha256 = excluded.payload_sha256
			 WHERE cn_account_snapshot.payload_sha256 <> excluded.payload_sha256`,
			userID,
			cnSaveSnapshotSchemaVersion,
			now,
			content,
			hex.EncodeToString(digest[:]),
		); err != nil {
			return fmt.Errorf("create CN system-partner snapshot %d: %w", userID, err)
		}
		var snapshotRevision int
		var snapshotUpdatedUTC string
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT revision, updated_utc FROM cn_account_snapshot WHERE user_id = ?`,
			userID,
		).Scan(&snapshotRevision, &snapshotUpdatedUTC); err != nil {
			return fmt.Errorf("read CN system-partner snapshot metadata %d: %w", userID, err)
		}
		if err := upsertCNAccountProjection(
			transaction,
			userID,
			snapshotRevision,
			snapshotUpdatedUTC,
			state,
		); err != nil {
			return fmt.Errorf("project CN system-partner account %d: %w", userID, err)
		}
	}
	return nil
}

func (accounts *cnAccountStore) resolveLogin(loginUUID string) (cnAccountIdentity, error) {
	loginUUID = strings.ToLower(strings.TrimSpace(loginUUID))
	if !validCNLoginUUID(loginUUID) {
		return cnAccountIdentity{}, errors.New("CN login UUID is invalid")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()

	database, err := accounts.storage.open()
	if err != nil {
		return cnAccountIdentity{}, err
	}
	defer database.Close()
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return cnAccountIdentity{}, fmt.Errorf("begin CN local login: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	if err := initializeCNSaveSchema(transaction); err != nil {
		return cnAccountIdentity{}, err
	}

	identity, err := accounts.resolveLoginTransaction(transaction, loginUUID, true)
	if err != nil {
		return cnAccountIdentity{}, err
	}
	if err := transaction.Commit(); err != nil {
		return cnAccountIdentity{}, fmt.Errorf("commit CN local login: %w", err)
	}
	committed = true
	return identity, nil
}

// Caller owns accounts.mu and the transaction. Binding does not rotate an active game session.
func (accounts *cnAccountStore) resolveLoginTransaction(transaction *sql.Tx, loginUUID string, rotate bool) (cnAccountIdentity, error) {
	var identity cnAccountIdentity
	err := transaction.QueryRowContext(
		context.Background(),
		`SELECT user_id, login_uuid, session_key
		 FROM cn_local_account WHERE login_uuid = ?`,
		loginUUID,
	).Scan(&identity.UserID, &identity.LoginUUID, &identity.SessionKey)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err == nil && !rotate {
		return identity, nil
	}
	if err == nil {
		sessionKey, err := newCNSessionKey()
		if err != nil {
			return cnAccountIdentity{}, err
		}
		if _, err := transaction.ExecContext(
			context.Background(),
			`UPDATE cn_local_account SET last_login_utc = ?, session_key = ? WHERE user_id = ?`,
			now,
			sessionKey,
			identity.UserID,
		); err != nil {
			return cnAccountIdentity{}, fmt.Errorf("update CN local login: %w", err)
		}
		identity.SessionKey = sessionKey
	} else if !errors.Is(err, sql.ErrNoRows) {
		return cnAccountIdentity{}, fmt.Errorf("read CN local account: %w", err)
	} else {
		var nextUserID int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT COALESCE(MAX(user_id), ?) + 1
			 FROM cn_local_account WHERE user_id < ?`,
			cnPrimaryUserID-1,
			cnSystemPartnerUserIDBase,
		).Scan(&nextUserID); err != nil {
			return cnAccountIdentity{}, fmt.Errorf("allocate CN local user ID: %w", err)
		}
		sessionKey, err := newCNSessionKey()
		if err != nil {
			return cnAccountIdentity{}, err
		}
		identity = cnAccountIdentity{
			UserID:     nextUserID,
			LoginUUID:  loginUUID,
			SessionKey: sessionKey,
		}
		if _, err := transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_local_account
			 (user_id, login_uuid, session_key, created_utc, last_login_utc)
			 VALUES (?, ?, ?, ?, ?)`,
			identity.UserID,
			identity.LoginUUID,
			identity.SessionKey,
			now,
			now,
		); err != nil {
			return cnAccountIdentity{}, fmt.Errorf("create CN local account: %w", err)
		}
		if identity.UserID != cnPrimaryUserID {
			if err := accounts.insertSeedSnapshot(transaction, identity.UserID, now); err != nil {
				return cnAccountIdentity{}, err
			}
		} else {
			var revision int
			var updatedUTC string
			var content []byte
			var expectedDigest string
			if err := transaction.QueryRowContext(
				context.Background(),
				`SELECT revision, updated_utc, payload_json, payload_sha256
				 FROM cn_save_snapshot WHERE singleton = 1`,
			).Scan(&revision, &updatedUTC, &content, &expectedDigest); err != nil {
				return cnAccountIdentity{}, fmt.Errorf("read primary CN account snapshot: %w", err)
			}
			digest := sha256.Sum256(content)
			if hex.EncodeToString(digest[:]) != expectedDigest {
				return cnAccountIdentity{}, errors.New("primary CN account snapshot digest mismatch")
			}
			state, err := accounts.storage.decodeAccount(transaction, content)
			if err != nil {
				return cnAccountIdentity{}, err
			}
			if err := upsertCNAccountProjection(transaction, identity.UserID, revision, updatedUTC, state); err != nil {
				return cnAccountIdentity{}, err
			}
		}
	}
	return identity, nil
}

func (accounts *cnAccountStore) insertSeedSnapshot(transaction *sql.Tx, userID int, now string) error {
	state, err := loadCNSaveState(accounts.storage.seedPath)
	if err != nil {
		return fmt.Errorf("load CN account seed: %w", err)
	}
	if err := initializeCNOnboardingSnapshot(&state, userID); err != nil {
		return err
	}
	content, err := encodeCNAccountMetadata(state)
	if err != nil {
		return err
	}
	if err := writeCNAccountRows(transaction, state); err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	if _, err := transaction.ExecContext(context.Background(),
		`INSERT INTO cn_account_snapshot (user_id, schema_version, revision, updated_utc, payload_json, payload_sha256)
         VALUES (?, ?, 1, ?, ?, ?)`, userID, cnSaveSnapshotSchemaVersion, now, content, hex.EncodeToString(digest[:])); err != nil {
		return fmt.Errorf("create CN account snapshot: %w", err)
	}
	return upsertCNAccountProjection(transaction, userID, 1, now, state)
}

func (accounts *cnAccountStore) loadState(userID int) (release.State, error) {
	state, err := accounts.loadPersistentState(userID)
	if err != nil {
		return state, err
	}
	accounts.battleMu.Lock()
	if session := accounts.battles[userID]; session != nil {
		state.ActiveTeamBattle = cloneActiveTeamBattleState(session.state)
		state.StoryTeamBattleSession = session.story
	}
	accounts.battleMu.Unlock()
	return state, nil
}

func (accounts *cnAccountStore) loadPersistentState(userID int) (release.State, error) {
	if userID == cnPrimaryUserID {
		return accounts.storage.loadOrImport()
	}
	if userID < cnPrimaryUserID {
		return release.State{}, errors.New("CN local user ID is invalid")
	}
	database, err := accounts.storage.open()
	if err != nil {
		return release.State{}, err
	}
	defer database.Close()
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return release.State{}, err
	}
	defer transaction.Rollback()
	var schemaVersion int
	var content []byte
	var expectedDigest string
	if err := transaction.QueryRowContext(context.Background(),
		`SELECT schema_version, payload_json, payload_sha256 FROM cn_account_snapshot WHERE user_id = ?`,
		userID).Scan(&schemaVersion, &content, &expectedDigest); err != nil {
		return release.State{}, fmt.Errorf("read CN account snapshot: %w", err)
	}
	if schemaVersion != cnSaveSnapshotSchemaVersion {
		return release.State{}, fmt.Errorf("unsupported CN account snapshot schema %d; use a fresh database", schemaVersion)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return release.State{}, errors.New("CN account snapshot digest mismatch")
	}
	state, err := accounts.storage.decodeAccount(transaction, content)
	if err != nil {
		return release.State{}, err
	}
	if state.User.UserID != userID {
		return release.State{}, errors.New("CN account snapshot user ID mismatch")
	}
	if err := transaction.Commit(); err != nil {
		return release.State{}, err
	}
	return state, nil
}

func (accounts *cnAccountStore) persistState(userID int, state release.State) error {
	return accounts.persistStateWithAudit(userID, state, nil)
}

func (accounts *cnAccountStore) persistStateWithAudit(userID int, state release.State, audit *cnAdminAudit) error {
	if err := accounts.persistAccountData(userID, state, audit); err != nil {
		return err
	}
	// Commit currency/reward changes first. Failed persistence must retain the
	// prior runtime context when the account handler is discarded and reloaded.
	accounts.rememberBattle(userID, state.ActiveTeamBattle, state.StoryTeamBattleSession)
	return nil
}

func (accounts *cnAccountStore) persistAccountData(userID int, state release.State, audit *cnAdminAudit) error {
	if state.User.UserID != userID {
		return errors.New("refusing to persist a CN account under a different user ID")
	}
	if userID == cnPrimaryUserID {
		return accounts.storage.persistWithAudit(state, audit)
	}
	content, err := encodeCNAccountMetadata(state)
	if err != nil {
		return err
	}
	database, err := accounts.storage.open()
	if err != nil {
		return err
	}
	defer database.Close()
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin CN account snapshot update: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	digest := sha256.Sum256(content)
	updatedUTC := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeCNAccountRows(transaction, state); err != nil {
		return err
	}
	result, err := transaction.ExecContext(
		context.Background(),
		`UPDATE cn_account_snapshot
		 SET schema_version = ?, revision = revision + 1, updated_utc = ?,
		     payload_json = ?, payload_sha256 = ?
		 WHERE user_id = ?`,
		cnSaveSnapshotSchemaVersion,
		updatedUTC,
		content,
		hex.EncodeToString(digest[:]),
		userID,
	)
	if err != nil {
		return fmt.Errorf("write CN account snapshot: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("CN account snapshot update did not affect exactly one row")
	}
	var revision int
	if err := transaction.QueryRowContext(
		context.Background(),
		`SELECT revision FROM cn_account_snapshot WHERE user_id = ?`,
		userID,
	).Scan(&revision); err != nil {
		return fmt.Errorf("read updated CN account snapshot revision: %w", err)
	}
	if err := upsertCNAccountProjection(transaction, userID, revision, updatedUTC, state); err != nil {
		return err
	}
	if err := audit.appendTo(transaction, updatedUTC); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit CN account snapshot update: %w", err)
	}
	committed = true
	return nil
}

// ListPVPOpponents returns persisted local accounts other than the challenger.
// PVP is asynchronous: the selected account does not need an active process or
// an in-memory multiplayer room.
func (accounts *cnAccountStore) ListPVPOpponents(userID int) ([]release.State, error) {
	return accounts.listLocalAccountStates(userID, false)
}

func (accounts *cnAccountStore) listFriendPointPartnerAccounts(userID int) ([]release.State, error) {
	return accounts.listLocalAccountStates(userID, true)
}

func (accounts *cnAccountStore) listLocalAccountStates(
	userID int,
	includeSystemPartners bool,
) ([]release.State, error) {
	database, err := accounts.storage.open()
	if err != nil {
		return nil, err
	}
	defer database.Close()
	query := `SELECT account.user_id, projection.payload_json, projection.payload_sha256, account.last_login_utc
		FROM cn_local_account AS account
		JOIN cn_account_projection AS projection ON projection.user_id = account.user_id
		WHERE account.user_id <> ? AND account.user_id < ? ORDER BY account.user_id`
	arguments := []any{userID, cnSystemPartnerUserIDBase}
	if includeSystemPartners {
		query = `SELECT account.user_id, projection.payload_json, projection.payload_sha256, account.last_login_utc
			FROM cn_local_account AS account
			JOIN cn_account_projection AS projection ON projection.user_id = account.user_id
			WHERE account.user_id <> ? ORDER BY account.user_id`
		arguments = []any{userID}
	}
	rows, err := database.QueryContext(context.Background(), query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list CN local PVP opponents: %w", err)
	}
	defer rows.Close()
	result := make([]release.State, 0)
	for rows.Next() {
		var opponentUserID int
		var content []byte
		var expectedDigest string
		var lastLoginUTC string
		if err := rows.Scan(&opponentUserID, &content, &expectedDigest, &lastLoginUTC); err != nil {
			return nil, fmt.Errorf("scan CN local PVP opponent: %w", err)
		}
		state, err := decodeCNAccountProjection(content, expectedDigest)
		if err != nil {
			return nil, fmt.Errorf("decode CN local account projection %d: %w", opponentUserID, err)
		}
		if state.User.UserID != opponentUserID {
			return nil, fmt.Errorf("CN local account projection %d has a mismatched user ID", opponentUserID)
		}
		lastLogin, err := time.Parse(time.RFC3339Nano, lastLoginUTC)
		if err != nil {
			return nil, fmt.Errorf("decode CN account %d login time: %w", opponentUserID, err)
		}
		state.LastLoginUnix = lastLogin.Unix()
		// System partners are always available local helpers, without player sessions.
		if isCNSystemPartnerUserID(opponentUserID) {
			state.LastLoginUnix = time.Now().Unix()
		}
		result = append(result, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CN local PVP opponents: %w", err)
	}
	return result, nil
}

func validCNLoginUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func cnSessionForUserID(userID int) string {
	if userID == cnPrimaryUserID {
		return cn602LocalSession
	}
	return cn602LocalSession + "-" + strconv.Itoa(userID)
}

func newCNSessionKey() (string, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate CN account session: %w", err)
	}
	return "local-cn-" + hex.EncodeToString(entropy[:]), nil
}

func (accounts *cnAccountStore) resolveSession(sessionKey string) (int, error) {
	if len(sessionKey) < 32 || len(sessionKey) > 128 {
		return 0, errors.New("CN account session is invalid")
	}
	database, err := accounts.storage.open()
	if err != nil {
		return 0, err
	}
	defer database.Close()
	var userID int
	if err := database.QueryRowContext(
		context.Background(),
		`SELECT user_id FROM cn_local_account WHERE session_key = ?`,
		sessionKey,
	).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("CN account session is invalid")
		}
		return 0, fmt.Errorf("resolve CN account session: %w", err)
	}
	if userID < cnPrimaryUserID || isCNSystemPartnerUserID(userID) {
		return 0, errors.New("CN account session is invalid")
	}
	return userID, nil
}

const cnIdleAccountCacheLimit = 8
const cnIdleAccountCacheTTL = 5 * time.Minute

type cnAccountHandlerEntry struct {
	handler   http.Handler
	active    bool
	lastUsed  time.Time
	lastOrder uint64
	timer     *time.Timer
}

type cnAccountBusinessRouter struct {
	mu       sync.Mutex
	handlers map[int]*cnAccountHandlerEntry
	sequence uint64
	// Stable bounded locks also serialize Admin and BattleSv against HTTP.
	locks   [256]sync.Mutex
	build   func(int) (http.Handler, error)
	prepare func(http.Handler)
}

func newCNAccountBusinessRouter(primary http.Handler, build func(int) (http.Handler, error)) *cnAccountBusinessRouter {
	router := &cnAccountBusinessRouter{handlers: make(map[int]*cnAccountHandlerEntry), build: build}
	if primary != nil {
		entry := &cnAccountHandlerEntry{handler: primary, active: true}
		router.handlers[cnPrimaryUserID] = entry
		router.releaseHandler(cnPrimaryUserID, entry, false)
	}
	return router
}

func (router *cnAccountBusinessRouter) accountLock(userID int) *sync.Mutex {
	return &router.locks[uint(userID)%uint(len(router.locks))]
}

// Caller holds router.mu. Active requests retain their local handler until completion.
func (router *cnAccountBusinessRouter) removeHandlerLocked(userID int) {
	if entry := router.handlers[userID]; entry != nil && entry.timer != nil {
		entry.timer.Stop()
	}
	delete(router.handlers, userID)
}

func (router *cnAccountBusinessRouter) invalidate(userID int) {
	router.mu.Lock()
	router.removeHandlerLocked(userID)
	router.mu.Unlock()
}

// Caller holds the stable account lock throughout acquire, use, and release.
func (router *cnAccountBusinessRouter) acquireHandler(userID int) (*cnAccountHandlerEntry, error) {
	router.mu.Lock()
	entry := router.handlers[userID]
	if entry != nil {
		entry.active = true
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	router.mu.Unlock()
	if entry != nil {
		return entry, nil
	}
	handler, err := router.build(userID)
	if err != nil {
		return nil, err
	}
	entry = &cnAccountHandlerEntry{handler: handler, active: true}
	router.mu.Lock()
	router.handlers[userID] = entry
	router.mu.Unlock()
	return entry, nil
}

func (router *cnAccountBusinessRouter) releaseHandler(userID int, entry *cnAccountHandlerEntry, discard bool) {
	router.mu.Lock()
	defer router.mu.Unlock()
	if router.handlers[userID] != entry {
		return
	}
	if discard {
		router.removeHandlerLocked(userID)
		return
	}
	entry.active = false
	entry.lastUsed = time.Now()
	router.sequence++
	entry.lastOrder = router.sequence
	// The callback checks both identity and the deadline: a timer racing with
	// a new request cannot evict that request or its refreshed cache entry.
	entry.timer = time.AfterFunc(cnIdleAccountCacheTTL, func() {
		router.expireHandler(userID, entry)
	})
	for {
		idle, oldestID := 0, 0
		var oldest uint64
		for id, candidate := range router.handlers {
			if candidate.active {
				continue
			}
			idle++
			if oldest == 0 || candidate.lastOrder < oldest {
				oldest, oldestID = candidate.lastOrder, id
			}
		}
		if idle <= cnIdleAccountCacheLimit {
			break
		}
		router.removeHandlerLocked(oldestID)
	}
}

func (router *cnAccountBusinessRouter) expireHandler(userID int, entry *cnAccountHandlerEntry) {
	router.mu.Lock()
	defer router.mu.Unlock()
	if router.handlers[userID] == entry && !entry.active && time.Since(entry.lastUsed) >= cnIdleAccountCacheTTL {
		router.removeHandlerLocked(userID)
	}
}

func (router *cnAccountBusinessRouter) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	userID := cnPrimaryUserID
	if text := request.Header.Get(cnAccountUserHeader); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < cnPrimaryUserID {
			http.Error(writer, "invalid CN local account context", http.StatusUnauthorized)
			return
		}
		userID = parsed
	}
	accountLock := router.accountLock(userID)
	accountLock.Lock()
	defer accountLock.Unlock()

	entry, err := router.acquireHandler(userID)
	if err != nil {
		http.Error(writer, "load CN local account", http.StatusInternalServerError)
		return
	}
	handler := entry.handler
	// Handlers mutate an account-local cache before committing its snapshot.
	// An unsuccessful request must never leave that cache available to the next
	// request: reload the last durable state while retaining this account lock.
	response := &cnAccountResponseWriter{ResponseWriter: writer}
	completed := false
	defer func() {
		router.releaseHandler(userID, entry, !completed || response.status >= http.StatusBadRequest)
	}()
	if router.prepare != nil {
		router.prepare(handler)
	}
	handler.ServeHTTP(response, request)
	completed = true
}

type cnAccountResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *cnAccountResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *cnAccountResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	if status >= 200 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *cnAccountResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

// BattleSv never calls this while holding the Hub lock. The same account lock
// serializes HTTP/Admin mutations and the durable start debit.
func (router *cnAccountBusinessRouter) chargeMultiplayerStart(start multiplayer.BattleStart) error {
	lock := router.accountLock(start.OwnerUserID)
	lock.Lock()
	defer lock.Unlock()
	entry, err := router.acquireHandler(start.OwnerUserID)
	if err != nil {
		return err
	}
	completed := false
	defer func() { router.releaseHandler(start.OwnerUserID, entry, !completed) }()
	handler := entry.handler
	starter, ok := handler.(httpapi.MultiplayerBattleStarter)
	if !ok {
		return errors.New("CN account handler has no multiplayer start transaction")
	}
	err = starter.ChargeMultiplayerStart(start)
	completed = err == nil
	return err
}

// Called outside hub.mu, under the same per-account transaction lock as HTTP.
func (router *cnAccountBusinessRouter) chargeMultiplayerContinue(request multiplayer.BattleContinue) (multiplayer.ContinueBalance, error) {
	lock := router.accountLock(request.UserID)
	lock.Lock()
	defer lock.Unlock()
	entry, err := router.acquireHandler(request.UserID)
	if err != nil {
		return multiplayer.ContinueBalance{}, err
	}
	completed := false
	defer func() { router.releaseHandler(request.UserID, entry, !completed) }()
	handler := entry.handler
	starter, ok := handler.(httpapi.MultiplayerBattleContinuer)
	if !ok {
		return multiplayer.ContinueBalance{}, errors.New("CN account handler has no multiplayer continue transaction")
	}
	balance, err := starter.ChargeMultiplayerContinue(request)
	completed = err == nil
	return balance, err
}
