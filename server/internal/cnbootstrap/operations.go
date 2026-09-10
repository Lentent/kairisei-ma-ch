package cnbootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"kairisei.local/server/internal/httpapi"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"kairisei.local/server/internal/release"
)

const (
	cnTeamBattlePublicationKey = "team_battle_publication"
	cnPastBattlePublicationKey = "past_battle_publication"
	cnGachaPublicationKey      = "gacha_publication"
)

type cnTeamBattlePublication struct {
	StartUnix        int64  `json:"start_unix,omitempty"`
	EndUnix          int64  `json:"end_unix,omitempty"`
	ExpectedRevision *int   `json:"expected_revision,omitempty"`
	Mode             string `json:"mode"`
	GroupIDs         []int  `json:"group_ids,omitempty"`
}

type cnGachaPublication struct {
	CatalogVersion   int   `json:"catalog_version,omitempty"`
	ExpectedRevision *int  `json:"expected_revision,omitempty"`
	GroupIDs         []int `json:"group_ids"`
}

func (operations *cnOperationStore) setTeamBattlePublication(publication cnTeamBattlePublication) (cnAdminDocument, error) {
	return operations.setBattlePublication(cnTeamBattlePublicationKey, publication)
}

func (operations *cnOperationStore) setBattlePublication(key string, publication cnTeamBattlePublication) (cnAdminDocument, error) {
	if publication.ExpectedRevision == nil {
		return cnAdminDocument{}, errors.New("缺少配置版本，请重新载入后发布")
	}
	if publication.StartUnix < 0 || publication.EndUnix < 0 || (publication.EndUnix != 0 && publication.EndUnix <= publication.StartUnix) {
		return cnAdminDocument{}, errors.New("活动结束时间须晚于开始时间")
	}
	switch publication.Mode {
	case "all":
		publication.GroupIDs = nil
	case "allowlist":
		if len(publication.GroupIDs) == 0 {
			return cnAdminDocument{}, errors.New("CN team battle publication allowlist is empty")
		}
		sort.Ints(publication.GroupIDs)
		unique := publication.GroupIDs[:0]
		for _, groupID := range publication.GroupIDs {
			if groupID <= 0 {
				return cnAdminDocument{}, errors.New("CN team battle publication contains an invalid group ID")
			}
			if len(unique) == 0 || unique[len(unique)-1] != groupID {
				unique = append(unique, groupID)
			}
		}
		publication.GroupIDs = unique
	default:
		return cnAdminDocument{}, errors.New("CN team battle publication mode is invalid")
	}
	expected := *publication.ExpectedRevision
	publication.ExpectedRevision = nil
	return operations.writeDocument(key, expected, publication)
}

type cnAdminAudit struct {
	Operation string
	Target    string
	Payload   any
	Receipt   *cnAdminActionReceipt
}

// Account changes and their audit record share the snapshot transaction.
func (audit *cnAdminAudit) appendTo(transaction *sql.Tx, updatedUTC string) error {
	if audit == nil {
		return nil
	}
	if err := audit.Receipt.appendTo(transaction, updatedUTC); err != nil {
		return err
	}
	content, err := json.Marshal(audit.Payload)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	_, err = transaction.ExecContext(
		context.Background(),
		`INSERT INTO cn_admin_audit
		 (created_utc, operation, target, payload_json, payload_sha256)
		 VALUES (?, ?, ?, ?, ?)`,
		updatedUTC, audit.Operation, audit.Target, content, hex.EncodeToString(digest[:]),
	)
	if err != nil {
		return fmt.Errorf("append CN admin audit: %w", err)
	}
	return nil
}

type cnOperationStore struct {
	playerPolicy            atomic.Pointer[cnPlayerPolicySnapshot]
	playerDefaults          *cnPlayerPolicy
	playerLoginBase         release.LoginBonusPolicy
	playerNaviNames         map[int8]string
	content                 *cnContentStore
	runtimeSettings         httpapi.RuntimeSettings
	runtimeSettingsRevision int
	itemShopBases           []release.ItemShopLineup
	storage                 *cnSaveDatabase
	configMu                sync.RWMutex
	gachaBases              map[int]release.GachaProfile
	gachaConfigurations     []httpapi.GachaConfiguration
	gachaRevision           uint64
	managedGachaGroups      map[int]struct{}
	managedGachaGroupByID   map[int]int
	defaultGachaPublication map[int]struct{}
}

func newCNOperationStore(storage *cnSaveDatabase, gachas []release.GachaProfile) (*cnOperationStore, error) {
	if storage == nil {
		return nil, errors.New("CN operation database is required")
	}
	database, err := storage.open()
	if err != nil {
		return nil, err
	}
	defer database.Close()
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin CN operation schema initialization: %w", err)
	}
	if err := initializeCNSaveSchema(transaction); err != nil {
		_ = transaction.Rollback()
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit CN operation schema initialization: %w", err)
	}
	managedGroups := make(map[int]struct{})
	groupByID := make(map[int]int)
	defaults := make(map[int]struct{})
	publicationKeys := make(map[int]string)
	for _, gacha := range gachas {
		if gacha.GachaID == 90000100 || gacha.GachaID == 90000200 || gacha.GroupID <= 0 {
			continue
		}
		if previous, exists := publicationKeys[gacha.GroupID]; exists && previous != gacha.PublicationKey {
			return nil, fmt.Errorf("CN managed gacha group %d mixes publication keys", gacha.GroupID)
		}
		publicationKeys[gacha.GroupID] = gacha.PublicationKey
		managedGroups[gacha.GroupID] = struct{}{}
		groupByID[gacha.GachaID] = gacha.GroupID
		if gacha.PublicationKey == "water_coin" || gacha.PublicationKey == "" {
			defaults[gacha.GroupID] = struct{}{}
		}
	}
	bases := make(map[int]release.GachaProfile)
	for _, profile := range gachas {
		if profile.GachaID != 90000200 && profile.GachaID != 90000100 {
			bases[profile.GachaID] = profile
		}
	}
	operations := &cnOperationStore{
		storage: storage, managedGachaGroups: managedGroups,
		managedGachaGroupByID: groupByID, defaultGachaPublication: defaults, gachaBases: bases,
	}
	catalog, err := storage.catalogState()
	if err != nil {
		return nil, err
	}
	for _, tab := range catalog.ItemShopTabs {
		for _, lineup := range tab.Lineup {
			if !lineup.Hidden {
				operations.itemShopBases = append(operations.itemShopBases, lineup)
			}
		}
	}
	if err := operations.reloadGachaConfigurations(); err != nil {
		return nil, err
	}
	if err := operations.loadRuntimeSettings(); err != nil {
		return nil, err
	}
	return operations, nil
}

func (operations *cnOperationStore) setGachaPublication(publication cnGachaPublication) (cnAdminDocument, error) {
	if publication.ExpectedRevision == nil {
		return cnAdminDocument{}, errors.New("缺少配置版本，请重新载入后发布")
	}
	sort.Ints(publication.GroupIDs)
	unique := publication.GroupIDs[:0]
	for _, groupID := range publication.GroupIDs {
		if _, exists := operations.managedGachaGroups[groupID]; !exists {
			return cnAdminDocument{}, fmt.Errorf("unknown managed CN gacha group %d", groupID)
		}
		if len(unique) == 0 || unique[len(unique)-1] != groupID {
			unique = append(unique, groupID)
		}
	}
	publication.GroupIDs = unique
	expected := *publication.ExpectedRevision
	publication.ExpectedRevision = nil
	publication.CatalogVersion = 2
	return operations.writeDocument(cnGachaPublicationKey, expected, publication)
}

func (operations *cnOperationStore) gachaPublication() (map[int]struct{}, error) {
	doc, err := operations.readDocument(cnGachaPublicationKey)
	if err != nil {
		return nil, err
	}
	return operations.gachaPublicationFromDocument(doc)
}

func (operations *cnOperationStore) gachaPublicationFromDocument(doc cnAdminDocument) (map[int]struct{}, error) {
	if doc.Revision == 0 {
		return cloneCNIntSet(operations.defaultGachaPublication), nil
	}
	var publication cnGachaPublication
	if err := json.Unmarshal(doc.Payload, &publication); err != nil {
		return nil, fmt.Errorf("decode CN gacha publication: %w", err)
	}
	active := make(map[int]struct{}, len(publication.GroupIDs))
	// Older operator documents only covered event presets. Do not silently
	// close previously permanent pools when loading such a document.
	if publication.CatalogVersion < 2 {
		for _, base := range operations.gachaBases {
			if base.PublicationKey == "" && base.GroupID > 0 {
				active[base.GroupID] = struct{}{}
			}
		}
	}
	for _, groupID := range publication.GroupIDs {
		if _, exists := operations.managedGachaGroups[groupID]; !exists {
			return nil, fmt.Errorf("CN gacha publication contains unknown group %d", groupID)
		}
		active[groupID] = struct{}{}
	}
	return active, nil
}

func cloneCNIntSet(source map[int]struct{}) map[int]struct{} {
	result := make(map[int]struct{}, len(source))
	for value := range source {
		result[value] = struct{}{}
	}
	return result
}

func (operations *cnOperationStore) gachaIDPublished(gachaID int, active map[int]struct{}) bool {
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	for _, config := range operations.gachaConfigurations {
		if config.Profile.GachaID == gachaID {
			now := time.Now().Unix()
			if config.Disabled || now < config.StartUnix || (config.EndUnix != 0 && now >= config.EndUnix) {
				return false
			}
		}
	}
	groupID, managed := operations.managedGachaGroupByID[gachaID]
	if !managed {
		return true
	}
	_, published := active[groupID]
	return published
}

// teamBattleGroupAllowlist returns nil for the default all-published policy.
// It is read at the HTTP presentation boundary so an operations change does
// not rebuild immutable master data and does not require a server restart.
func (operations *cnOperationStore) teamBattleGroupAllowlist() (map[int]struct{}, error) {
	return operations.battleGroupAllowlist(cnTeamBattlePublicationKey)
}

func (operations *cnOperationStore) battleGroupAllowlist(key string) (map[int]struct{}, error) {
	database, err := operations.storage.open()
	if err != nil {
		return nil, err
	}
	defer database.Close()
	var content []byte
	var expectedDigest string
	err = database.QueryRowContext(
		context.Background(),
		`SELECT payload_json, payload_sha256 FROM cn_global_operation WHERE operation_key = ?`,
		key,
	).Scan(&content, &expectedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read CN team battle publication: %w", err)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return nil, errors.New("CN team battle publication digest mismatch")
	}
	var publication cnTeamBattlePublication
	if err := json.Unmarshal(content, &publication); err != nil {
		return nil, fmt.Errorf("decode CN team battle publication: %w", err)
	}
	now := time.Now().Unix()
	if now < publication.StartUnix || (publication.EndUnix != 0 && now >= publication.EndUnix) {
		return map[int]struct{}{}, nil
	}
	switch publication.Mode {
	case "all":
		return nil, nil
	case "allowlist":
		if len(publication.GroupIDs) == 0 {
			return nil, errors.New("CN team battle publication allowlist is empty")
		}
		allowed := make(map[int]struct{}, len(publication.GroupIDs))
		for _, groupID := range publication.GroupIDs {
			if groupID <= 0 {
				return nil, errors.New("CN team battle publication contains an invalid group ID")
			}
			allowed[groupID] = struct{}{}
		}
		return allowed, nil
	default:
		return nil, errors.New("CN team battle publication mode is invalid")
	}
}
