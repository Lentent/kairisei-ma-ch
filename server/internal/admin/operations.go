package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const (
	teamBattlePublicationKey = "team_battle_publication"
	PastBattlePublicationKey = "past_battle_publication"
	gachaPublicationKey      = "gacha_publication"
)

type TeamBattlePublication struct {
	StartUnix        int64  `json:"start_unix,omitempty"`
	EndUnix          int64  `json:"end_unix,omitempty"`
	ExpectedRevision *int   `json:"expected_revision,omitempty"`
	Mode             string `json:"mode"`
	GroupIDs         []int  `json:"group_ids,omitempty"`
}

type gachaPublication struct {
	CatalogVersion   int   `json:"catalog_version,omitempty"`
	ExpectedRevision *int  `json:"expected_revision,omitempty"`
	GroupIDs         []int `json:"group_ids"`
}

func (operations *Operations) SetTeamBattlePublication(publication TeamBattlePublication) (accountstore.Document, error) {
	return operations.setBattlePublication(teamBattlePublicationKey, publication)
}

func (operations *Operations) setBattlePublication(key string, publication TeamBattlePublication) (accountstore.Document, error) {
	if publication.ExpectedRevision == nil {
		return accountstore.Document{}, errors.New("缺少配置版本，请重新载入后发布")
	}
	if publication.StartUnix < 0 || publication.EndUnix < 0 || (publication.EndUnix != 0 && publication.EndUnix <= publication.StartUnix) {
		return accountstore.Document{}, errors.New("活动结束时间须晚于开始时间")
	}
	switch publication.Mode {
	case "all":
		publication.GroupIDs = nil
	case "allowlist":
		sort.Ints(publication.GroupIDs)
		unique := publication.GroupIDs[:0]
		for _, groupID := range publication.GroupIDs {
			if groupID <= 0 {
				return accountstore.Document{}, errors.New("CN team battle publication contains an invalid group ID")
			}
			if len(unique) == 0 || unique[len(unique)-1] != groupID {
				unique = append(unique, groupID)
			}
		}
		publication.GroupIDs = unique
	default:
		return accountstore.Document{}, errors.New("CN team battle publication mode is invalid")
	}
	expected := *publication.ExpectedRevision
	publication.ExpectedRevision = nil
	return operations.writeDocument(key, expected, publication)
}

type Operations struct {
	playerPolicy            atomic.Pointer[playerPolicySnapshot]
	playerDefaults          *PlayerPolicy
	playerLoginBase         gamestate.LoginBonusPolicy
	playerNaviNames         map[int8]string
	content                 *contentStore
	runtimeSettings         game.RuntimeSettings
	runtimeSettingsRevision int
	itemShopBases           []gamestate.ItemShopLineup
	storage                 *accountstore.Database
	configMu                sync.RWMutex
	gachaBases              map[int]gamestate.GachaProfile
	gachaConfigurations     []game.GachaConfiguration
	gachaRevision           uint64
	managedGachaGroups      map[int]struct{}
	managedGachaGroupByID   map[int]int
	defaultGachaPublication map[int]struct{}
}

func NewOperations(storage *accountstore.Database, gachas []gamestate.GachaProfile) (*Operations, error) {
	if storage == nil {
		return nil, errors.New("CN operation database is required")
	}
	if err := storage.EnsureSchema(); err != nil {
		return nil, err
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
	bases := make(map[int]gamestate.GachaProfile)
	for _, profile := range gachas {
		if profile.GachaID != 90000200 && profile.GachaID != 90000100 {
			bases[profile.GachaID] = profile
		}
	}
	operations := &Operations{
		storage: storage, managedGachaGroups: managedGroups,
		managedGachaGroupByID: groupByID, defaultGachaPublication: defaults, gachaBases: bases,
	}
	catalog, err := storage.CatalogState()
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

func (operations *Operations) setGachaPublication(publication gachaPublication) (accountstore.Document, error) {
	if publication.ExpectedRevision == nil {
		return accountstore.Document{}, errors.New("缺少配置版本，请重新载入后发布")
	}
	sort.Ints(publication.GroupIDs)
	unique := publication.GroupIDs[:0]
	for _, groupID := range publication.GroupIDs {
		if _, exists := operations.managedGachaGroups[groupID]; !exists {
			return accountstore.Document{}, fmt.Errorf("unknown managed CN gacha group %d", groupID)
		}
		if len(unique) == 0 || unique[len(unique)-1] != groupID {
			unique = append(unique, groupID)
		}
	}
	publication.GroupIDs = unique
	expected := *publication.ExpectedRevision
	publication.ExpectedRevision = nil
	publication.CatalogVersion = 2
	return operations.writeDocument(gachaPublicationKey, expected, publication)
}

func (operations *Operations) GachaPublication() (map[int]struct{}, error) {
	doc, err := operations.storage.ReadDocument(gachaPublicationKey)
	if err != nil {
		return nil, err
	}
	return operations.gachaPublicationFromDocument(doc)
}

func (operations *Operations) gachaPublicationFromDocument(doc accountstore.Document) (map[int]struct{}, error) {
	if doc.Revision == 0 {
		return cloneIntSet(operations.defaultGachaPublication), nil
	}
	var publication gachaPublication
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

func cloneIntSet(source map[int]struct{}) map[int]struct{} {
	result := make(map[int]struct{}, len(source))
	for value := range source {
		result[value] = struct{}{}
	}
	return result
}

func (operations *Operations) GachaIDPublished(gachaID int, active map[int]struct{}) bool {
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
func (operations *Operations) TeamBattleGroupAllowlist() (map[int]struct{}, error) {
	return operations.BattleGroupAllowlist(teamBattlePublicationKey)
}

func (operations *Operations) BattleGroupAllowlist(key string) (map[int]struct{}, error) {
	doc, err := operations.storage.ReadDocument(key)
	if err != nil {
		return nil, err
	}
	if doc.Revision == 0 {
		return nil, nil
	}
	content := doc.Payload
	var publication TeamBattlePublication
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

func (operations *Operations) writeDocument(key string, expected int, value any) (accountstore.Document, error) {
	operation := strings.Split(key, ":")[0]
	if key == teamBattlePublicationKey || key == PastBattlePublicationKey {
		operation = "boss-policy"
	} else if key == gachaPublicationKey {
		operation = "gacha-policy"
	}
	return operations.storage.WriteDocument(key, expected, value, operation)
}

func (operations *Operations) ManagedGroups() map[int]struct{} {
	return cloneIntSet(operations.managedGachaGroups)
}
