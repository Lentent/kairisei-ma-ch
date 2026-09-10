package cnbootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"kairisei.local/server/internal/release"
)

const (
	cnSaveDatabaseSchemaVersion = 3
	cnSaveSnapshotSchemaVersion = 3
)

type cnSaveDatabase struct {
	catalog            *release.State // immutable public configuration, initialized before serving accounts
	databasePath       string
	legacyDatabasePath string
	jsonPath           string
	seedPath           string
	logger             *slog.Logger
}

func newCNSaveDatabase(jsonPath string, seedPath string, logger *slog.Logger) (*cnSaveDatabase, error) {
	if jsonPath == "" {
		return nil, errors.New("CN save path is required")
	}
	if seedPath == "" {
		return nil, errors.New("CN save migration seed path is required")
	}
	absoluteJSONPath, err := filepath.Abs(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN save path: %w", err)
	}
	extension := filepath.Ext(absoluteJSONPath)
	basePath := absoluteJSONPath[:len(absoluteJSONPath)-len(extension)]
	databasePath := basePath + "-state.sqlite3"
	legacyDatabasePath := basePath + ".sqlite3"
	if databasePath == absoluteJSONPath {
		return nil, errors.New("CN save database path must differ from the JSON path")
	}
	absoluteSeedPath, err := filepath.Abs(seedPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN save migration seed path: %w", err)
	}
	return &cnSaveDatabase{
		databasePath:       databasePath,
		legacyDatabasePath: legacyDatabasePath,
		jsonPath:           absoluteJSONPath,
		seedPath:           absoluteSeedPath,
		logger:             logger,
	}, nil
}

func (storage *cnSaveDatabase) loadOrImport() (release.State, error) {
	if err := storage.ensureCurrentDatabase(); err != nil {
		return release.State{}, err
	}
	database, err := storage.open()
	if err != nil {
		return release.State{}, err
	}
	defer database.Close()

	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return release.State{}, fmt.Errorf("begin CN save database initialization: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	if err := initializeCNSaveSchema(transaction); err != nil {
		return release.State{}, err
	}

	var schemaVersion int
	var content []byte
	var expectedDigest string
	err = transaction.QueryRowContext(
		context.Background(),
		`SELECT schema_version, payload_json, payload_sha256
		 FROM cn_save_snapshot WHERE singleton = 1`,
	).Scan(&schemaVersion, &content, &expectedDigest)
	imported := false
	if errors.Is(err, sql.ErrNoRows) {
		state, loadErr := loadCNSaveState(storage.seedPath)
		if loadErr == nil {
			loadErr = initializeCNOnboardingSnapshot(&state, cnPrimaryUserID)
		}
		if loadErr != nil {
			return release.State{}, fmt.Errorf("load CN JSON seed: %w", loadErr)
		}
		content, loadErr = encodeCNAccountMetadata(state)
		if loadErr != nil {
			return release.State{}, loadErr
		}
		if err := writeCNAccountRows(transaction, state); err != nil {
			return release.State{}, err
		}
		digest := sha256.Sum256(content)
		expectedDigest = hex.EncodeToString(digest[:])
		_, err = transaction.ExecContext(
			context.Background(),
			`INSERT INTO cn_save_snapshot
			 (singleton, schema_version, revision, updated_utc, payload_json, payload_sha256)
			 VALUES (1, ?, 1, ?, ?, ?)`,
			cnSaveSnapshotSchemaVersion,
			time.Now().UTC().Format(time.RFC3339Nano),
			content,
			expectedDigest,
		)
		if err != nil {
			return release.State{}, fmt.Errorf("import CN JSON seed into SQLite: %w", err)
		}
		schemaVersion = cnSaveSnapshotSchemaVersion
		imported = true
	} else if err != nil {
		return release.State{}, fmt.Errorf("read CN SQLite snapshot: %w", err)
	}
	if schemaVersion != cnSaveSnapshotSchemaVersion {
		return release.State{}, fmt.Errorf("CN SQLite snapshot schema_version is %d, want %d", schemaVersion, cnSaveSnapshotSchemaVersion)
	}
	if len(content) == 0 || len(content) > maxCNAccountSnapshotBytes {
		return release.State{}, errors.New("CN SQLite snapshot is empty or exceeds the size limit")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return release.State{}, errors.New("CN SQLite snapshot digest mismatch")
	}
	state, err := storage.decodeAccount(transaction, content)
	if err != nil {
		return release.State{}, fmt.Errorf("decode CN SQLite account: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return release.State{}, fmt.Errorf("commit CN database initialization: %w", err)
	}
	committed = true
	if imported {
		storage.logger.Info("initialized CN account in SQLite", "database_path", storage.databasePath)
	}
	return state, nil
}

func applyBattleLoadoutMigration(state *release.State, seed release.State) error {
	seedCards := make(map[int64]release.Card, len(seed.BattleLoadoutCardUniqueIDs))
	requested := make(map[int64]struct{}, len(seed.BattleLoadoutCardUniqueIDs))
	for _, uniqueID := range seed.BattleLoadoutCardUniqueIDs {
		if uniqueID <= 0 {
			return errors.New("CN battle loadout migration has an invalid card unique ID")
		}
		if _, exists := requested[uniqueID]; exists {
			return fmt.Errorf("CN battle loadout migration repeats card unique ID %d", uniqueID)
		}
		requested[uniqueID] = struct{}{}
	}
	for _, card := range seed.Cards {
		if _, wanted := requested[card.UniqueID]; !wanted {
			continue
		}
		if card.CardID <= 0 || card.Level <= 0 || card.Level != card.LevelMax ||
			card.NextLevelExperience != 0 || len(card.SkillLevels) == 0 {
			return fmt.Errorf("CN battle loadout migration card %d is not a complete max-level card", card.UniqueID)
		}
		seedCards[card.UniqueID] = card
	}
	if len(seedCards) != len(requested) {
		return errors.New("CN battle loadout migration seed does not contain every configured card")
	}

	for uniqueID, seeded := range seedCards {
		found := false
		for index := range state.Cards {
			if state.Cards[index].UniqueID != uniqueID {
				continue
			}
			if state.Cards[index].CardID != seeded.CardID {
				return fmt.Errorf("CN battle loadout unique ID %d collides with card %d", uniqueID, state.Cards[index].CardID)
			}
			state.Cards[index] = seeded
			found = true
			break
		}
		if !found {
			for index := range state.ContainerCards {
				if state.ContainerCards[index].UniqueID != uniqueID {
					continue
				}
				if state.ContainerCards[index].CardID != seeded.CardID {
					return fmt.Errorf("CN battle loadout unique ID %d collides with container card %d", uniqueID, state.ContainerCards[index].CardID)
				}
				state.ContainerCards = append(
					state.ContainerCards[:index],
					state.ContainerCards[index+1:]...,
				)
				state.Cards = append(state.Cards, seeded)
				found = true
				break
			}
		}
		if !found {
			state.Cards = append(state.Cards, seeded)
		}
	}
	if len(state.Cards) > state.User.CardMax {
		return errors.New("CN battle loadout migration exceeds card inventory capacity")
	}

	seedDecks := make(map[int8]release.Deck, 4)
	for _, deck := range seed.Decks {
		if deck.ArthurType < 1 || deck.ArthurType > 4 || deck.IsActive == 0 {
			continue
		}
		if _, exists := seedDecks[deck.ArthurType]; exists {
			return fmt.Errorf("CN battle loadout migration has multiple active Arthur type %d decks", deck.ArthurType)
		}
		if len(deck.CardUniqueIDs) == 0 || deck.LeaderCardIndex < 0 ||
			int(deck.LeaderCardIndex) >= len(deck.CardUniqueIDs) {
			return fmt.Errorf("CN battle loadout migration has an invalid Arthur type %d deck", deck.ArthurType)
		}
		seedDecks[deck.ArthurType] = cloneReleaseDeck(deck)
	}
	if len(seedDecks) != 4 {
		return errors.New("CN battle loadout migration requires four active Arthur decks")
	}
	for uniqueID := range requested {
		ownerCount := 0
		for _, deck := range seedDecks {
			for _, deckUniqueID := range deck.CardUniqueIDs {
				if deckUniqueID == uniqueID {
					ownerCount++
				}
			}
		}
		if ownerCount != 1 {
			return fmt.Errorf("CN battle loadout card %d must belong to exactly one seeded Arthur deck", uniqueID)
		}
	}

	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		seedDeck := seedDecks[arthurType]
		activeIndex := -1
		for index := range state.Decks {
			if state.Decks[index].ArthurType == arthurType && state.Decks[index].IsActive != 0 {
				activeIndex = index
				break
			}
		}
		if activeIndex < 0 {
			state.Decks = append(state.Decks, cloneReleaseDeck(seedDeck))
			continue
		}
		if len(state.Decks[activeIndex].CardUniqueIDs) != len(seedDeck.CardUniqueIDs) {
			return fmt.Errorf("CN battle loadout deck shape differs for Arthur type %d", arthurType)
		}
		state.Decks[activeIndex].CardUniqueIDs = append([]int64(nil), seedDeck.CardUniqueIDs...)
		state.Decks[activeIndex].LeaderCardIndex = seedDeck.LeaderCardIndex
	}

	activeArthurType := int8(state.User.ActiveArthurType)
	activeDeck, exists := seedDecks[activeArthurType]
	if !exists {
		return errors.New("CN battle loadout active Arthur deck is unavailable")
	}
	leaderUniqueID := activeDeck.CardUniqueIDs[activeDeck.LeaderCardIndex]
	leader, exists := seedCards[leaderUniqueID]
	if !exists {
		return errors.New("CN battle loadout leader is not one of the configured max-level cards")
	}
	state.User.LeaderCardUniqueID = leader.UniqueID
	state.User.LeaderCardID = leader.CardID
	return nil
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

func applyCNItemShopConfigMigration(state *release.State, seed release.State) (bool, error) {
	if state.ItemShopConfigVersion >= seed.ItemShopConfigVersion {
		return false, nil
	}
	if seed.ItemShopConfigVersion < cnItemShopConfigVersion || len(seed.ItemShopTabs) != 5 {
		return false, errors.New("CN save item shop migration seed is incomplete")
	}
	owned := make(map[int]struct{}, len(state.Items))
	for _, item := range state.Items {
		if item.ItemID <= 0 || item.Num < 0 {
			return false, errors.New("CN save persisted item is invalid during item shop migration")
		}
		owned[item.ItemID] = struct{}{}
	}
	for _, item := range seed.Items {
		if _, exists := owned[item.ItemID]; exists {
			continue
		}
		state.Items = append(state.Items, item)
		owned[item.ItemID] = struct{}{}
	}
	state.ItemShopConfigVersion = seed.ItemShopConfigVersion
	state.ItemShopTabs = cloneItemShopTabs(seed.ItemShopTabs)
	return true, nil
}

func cloneGachas(source []release.GachaProfile) []release.GachaProfile {
	return release.CloneGachas(source)
}

func applyCNGachaConfigMigration(state *release.State, seed release.State) (bool, error) {
	if state.GachaConfigVersion >= seed.GachaConfigVersion {
		return false, nil
	}
	if seed.GachaConfigVersion < cnGachaConfigVersion || len(seed.Gachas) == 0 {
		return false, errors.New("CN save gacha migration seed is incomplete")
	}
	playCounts := make(map[int]int, len(state.Gachas))
	for _, gacha := range state.Gachas {
		playCounts[gacha.GachaID] = gacha.PlayCount
	}
	state.GachaConfigVersion = seed.GachaConfigVersion
	state.Gachas = cloneGachas(seed.Gachas)
	dailyProfiles := make(map[int]struct{}, len(state.Gachas))
	for index := range state.Gachas {
		if playCount, exists := playCounts[state.Gachas[index].GachaID]; exists {
			state.Gachas[index].PlayCount = playCount
		}
		if state.Gachas[index].DailyFirstFree {
			dailyProfiles[state.Gachas[index].GachaID] = struct{}{}
		}
	}
	claims := state.GachaDailyClaims[:0]
	for _, claim := range state.GachaDailyClaims {
		if _, exists := dailyProfiles[claim.GachaID]; exists {
			claims = append(claims, claim)
		}
	}
	state.GachaDailyClaims = claims
	return true, nil
}

func mergeMissingTeamBattleDecks(
	current []release.Deck,
	seed []release.Deck,
) ([]release.Deck, bool, error) {
	result := make([]release.Deck, len(current))
	for index, deck := range current {
		result[index] = cloneReleaseDeck(deck)
	}

	present := make(map[int8]struct{}, 4)
	for _, deck := range result {
		if deck.ArthurType >= 1 && deck.ArthurType <= 4 {
			present[deck.ArthurType] = struct{}{}
		}
	}
	changed := false
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		if _, exists := present[arthurType]; exists {
			continue
		}
		var selected *release.Deck
		for index := range seed {
			if seed[index].ArthurType != arthurType {
				continue
			}
			if selected == nil || seed[index].IsActive != 0 {
				selected = &seed[index]
			}
			if seed[index].IsActive != 0 {
				break
			}
		}
		if selected == nil {
			return nil, false, fmt.Errorf(
				"CN team battle migration seed has no Arthur type %d deck",
				arthurType,
			)
		}
		result = append(result, cloneReleaseDeck(*selected))
		changed = true
	}
	return result, changed, nil
}

func cloneReleaseDeck(deck release.Deck) release.Deck {
	deck.CardUniqueIDs = append([]int64(nil), deck.CardUniqueIDs...)
	deck.SupportCardUniqueIDs = append([]int64(nil), deck.SupportCardUniqueIDs...)
	deck.SphereUniqueIDs = append([]int64(nil), deck.SphereUniqueIDs...)
	deck.BuddyUniqueIDs = append([]int64(nil), deck.BuddyUniqueIDs...)
	return deck
}

func (storage *cnSaveDatabase) persist(state release.State) error {
	return storage.persistWithAudit(state, nil)
}

func (storage *cnSaveDatabase) persistWithAudit(state release.State, audit *cnAdminAudit) error {
	content, err := encodeCNAccountMetadata(state)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	database, err := storage.open()
	if err != nil {
		return err
	}
	defer database.Close()

	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin CN SQLite save: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	updatedUTC := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeCNAccountRows(transaction, state); err != nil {
		return err
	}
	result, err := transaction.ExecContext(
		context.Background(),
		`UPDATE cn_save_snapshot
		 SET schema_version = ?, revision = revision + 1, updated_utc = ?,
		     payload_json = ?, payload_sha256 = ?
		 WHERE singleton = 1`,
		cnSaveSnapshotSchemaVersion,
		updatedUTC,
		content,
		digestText,
	)
	if err != nil {
		return fmt.Errorf("write CN SQLite snapshot: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm CN SQLite snapshot update: %w", err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("CN SQLite snapshot update affected %d rows, want 1", rowsAffected)
	}
	var accountExists int
	if err := transaction.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM cn_local_account WHERE user_id = ?`,
		state.User.UserID,
	).Scan(&accountExists); err != nil {
		return fmt.Errorf("check primary CN account projection owner: %w", err)
	}
	if accountExists == 1 {
		var revision int
		if err := transaction.QueryRowContext(
			context.Background(),
			`SELECT revision FROM cn_save_snapshot WHERE singleton = 1`,
		).Scan(&revision); err != nil {
			return fmt.Errorf("read primary CN snapshot revision: %w", err)
		}
		if err := upsertCNAccountProjection(
			transaction,
			state.User.UserID,
			revision,
			updatedUTC,
			state,
		); err != nil {
			return err
		}
	}
	if err := audit.appendTo(transaction, updatedUTC); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit CN SQLite save: %w", err)
	}
	committed = true

	// The JSON path is an optional first-import input, never a save output.
	// SQLite and its account projection are committed together above.
	return nil
}

func (storage *cnSaveDatabase) open() (*sql.DB, error) {
	// Acquire the writer reservation before taking a read snapshot. A deferred
	// read-then-write transaction can fail with SQLITE_BUSY_SNAPSHOT immediately,
	// even with busy_timeout, when another account commits between the two.
	// modernc keeps explicitly read-only transactions deferred.
	path := filepath.ToSlash(storage.databasePath)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_txlock=immediate"}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open CN SQLite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = FULL`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := database.Exec(statement); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("configure CN SQLite database: %w", err)
		}
	}
	if err := protectLocalDatabase(storage.databasePath); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func initializeCNSaveSchema(transaction *sql.Tx) error {
	var userVersion int
	if err := transaction.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("read CN SQLite schema version: %w", err)
	}
	if userVersion != 0 && userVersion != cnSaveDatabaseSchemaVersion {
		return fmt.Errorf(
			"unsupported CN state database schema version %d, want %d",
			userVersion,
			cnSaveDatabaseSchemaVersion,
		)
	}
	if userVersion == cnSaveDatabaseSchemaVersion {
		return validateFixedCNStateSchema(transaction)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_save_snapshot (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		schema_version INTEGER NOT NULL,
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN SQLite save schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_local_account (
		user_id INTEGER PRIMARY KEY CHECK (user_id > 0),
		login_uuid TEXT NOT NULL UNIQUE,
		session_key TEXT NOT NULL UNIQUE,
		created_utc TEXT NOT NULL,
		last_login_utc TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create CN local account schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_account_snapshot (
		user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		schema_version INTEGER NOT NULL,
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN account snapshot schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_account_projection (
		user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		schema_version INTEGER NOT NULL,
		snapshot_revision INTEGER NOT NULL CHECK (snapshot_revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN account projection schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_multiplayer_runtime (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		next_room_id INTEGER NOT NULL CHECK (next_room_id > 0)
	)`); err != nil {
		return fmt.Errorf("create CN multiplayer runtime schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_multiplayer_completion (
		room_id INTEGER PRIMARY KEY CHECK (room_id > 0),
		expires_unix INTEGER NOT NULL CHECK (expires_unix > 0),
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN multiplayer completion schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_global_operation (
		operation_key TEXT PRIMARY KEY,
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN global operation schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_admin_audit (
		audit_id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_utc TEXT NOT NULL,
		operation TEXT NOT NULL,
		target TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN admin audit schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_admin_action_receipt (
		operation_key TEXT NOT NULL,
		user_id INTEGER NOT NULL,
		request_sha256 TEXT NOT NULL CHECK (length(request_sha256) = 64),
		result_json BLOB NOT NULL,
		result_sha256 TEXT NOT NULL CHECK (length(result_sha256) = 64),
		created_utc TEXT NOT NULL,
		PRIMARY KEY (operation_key, user_id)
	)`); err != nil {
		return fmt.Errorf("create CN admin action receipts: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_friend_point_rental (
		event_id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_key TEXT NOT NULL,
		owner_user_id INTEGER NOT NULL CHECK (owner_user_id > 0),
		renter_user_id INTEGER NOT NULL CHECK (renter_user_id > 0),
		boss_id INTEGER NOT NULL CHECK (boss_id > 0),
		friend_point INTEGER NOT NULL CHECK (friend_point > 0),
		created_utc TEXT NOT NULL,
		UNIQUE (event_key, owner_user_id)
	)`); err != nil {
		return fmt.Errorf("create CN friend-point rental schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE INDEX IF NOT EXISTS cn_friend_point_rental_owner_event
		ON cn_friend_point_rental (owner_user_id, event_id)`); err != nil {
		return fmt.Errorf("create CN friend-point rental owner index: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS cn_local_account_follow (
		follower_user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		followed_user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		created_utc TEXT NOT NULL,
		PRIMARY KEY (follower_user_id, followed_user_id),
		CHECK (follower_user_id <> followed_user_id)
	)`); err != nil {
		return fmt.Errorf("create CN local-account follow schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE INDEX IF NOT EXISTS cn_local_account_follow_inbound
		ON cn_local_account_follow (followed_user_id, follower_user_id)`); err != nil {
		return fmt.Errorf("create CN local-account follow inbound index: %w", err)
	}
	if userVersion == 0 {
		if _, err := transaction.Exec(cnAccountRowsSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, cnSaveDatabaseSchemaVersion)); err != nil {
			return fmt.Errorf("set CN SQLite schema version: %w", err)
		}
	}
	return validateFixedCNStateSchema(transaction)
}

func protectLocalDatabase(databasePath string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return fmt.Errorf("stat CN SQLite database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("CN SQLite database is not a regular file")
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		return fmt.Errorf("protect CN SQLite database: %w", err)
	}
	return nil
}
