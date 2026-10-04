package admin

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	StartUnix        int64                 `json:"start_unix,omitempty"`
	EndUnix          int64                 `json:"end_unix,omitempty"`
	ExpectedRevision *int                  `json:"expected_revision,omitempty"`
	Mode             string                `json:"mode"`
	GroupIDs         []int                 `json:"group_ids,omitempty"`
	GroupSchedules   []BattleGroupSchedule `json:"group_schedules,omitempty"`
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
	if err := normalizeBattleSchedules(publication.GroupSchedules); err != nil {
		return accountstore.Document{}, err
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
	battleGroupIDs          map[string][]int
	dungeonScheduleGroups   map[string][]AdminBattleGroup
	evolutionEdges          map[game.EvolutionPath]int
	evolutionClosed         []game.EvolutionPath
	evolutionRevision       int
	evolutionPolicy         *game.EvolutionRestrictions
	maintenance             maintenanceGate
	noticeSigningKey        [32]byte
	playerPolicy            atomic.Pointer[playerPolicySnapshot]
	missionPolicy           atomic.Pointer[missionPolicySnapshot]
	playerDefaults          *PlayerPolicy
	playerLoginBase         gamestate.LoginBonusPolicy
	playerNaviNames         map[int8]string
	content                 *contentStore
	runtimeSettings         game.RuntimeSettings
	runtimeSettingsRevision int
	itemShopBases           []gamestate.ItemShopLineup
	itemShopProducts        map[[2]int]gamestate.ItemDefinition
	storage                 *accountstore.Database
	configMu                sync.RWMutex
	legacyCustomGachas      map[int]bool
	gachaBases              map[int]gamestate.GachaProfile
	gachaCardJobs           map[int]int8
	gachaConfigurations     []game.GachaConfiguration
	gachaRevision           uint64
	managedGachaGroups      map[int]struct{}
	managedGachaGroupByID   map[int]int
	defaultGachaPublication map[int]struct{}
	customGachas            map[int]customGachaPool
	customGachaRevision     int
	customGachaNextID       int
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
	if _, err := rand.Read(operations.noticeSigningKey[:]); err != nil {
		return nil, fmt.Errorf("initialize notice signing key: %w", err)
	}
	catalog, err := storage.CatalogState()
	if err != nil {
		return nil, err
	}
	operations.battleGroupIDs, err = battlePublicationGroupIDs(catalog)
	if err != nil {
		return nil, err
	}
	operations.gachaCardJobs = map[int]int8{}
	operations.evolutionEdges = map[game.EvolutionPath]int{}
	for _, edge := range catalog.CardActions.EvolutionTransitions {
		operations.evolutionEdges[game.EvolutionPath{FromCardID: edge.FromCardID, ToCardID: edge.ToCardID}] = edge.Type
	}
	for id, card := range catalog.DeckRankPolicy.Cards {
		operations.gachaCardJobs[id] = card.ArthurType
	}
	for _, tab := range catalog.ItemShopTabs {
		for _, lineup := range tab.Lineup {
			if !lineup.Hidden {
				operations.itemShopBases = append(operations.itemShopBases, lineup)
			}
		}
	}
	operations.itemShopProducts = map[[2]int]gamestate.ItemDefinition{}
	for _, item := range catalog.ItemDefinitions {
		operations.itemShopProducts[[2]int{1, item.ItemID}] = item
	}
	for _, reward := range catalog.CollectionRewards {
		if reward.Type == 16 {
			operations.itemShopProducts[[2]int{2, reward.ID}] = gamestate.ItemDefinition{ItemID: reward.ID, Name: reward.Name, MaxOwned: 1}
		}
	}
	operations.itemShopProducts[[2]int{3, 0}] = gamestate.ItemDefinition{Name: "卡牌仓库扩容", MaxOwned: gamestate.CardCapacityLimit}
	operations.itemShopProducts[[2]int{4, 0}] = gamestate.ItemDefinition{Name: "卡牌持有上限扩容", MaxOwned: gamestate.CardCapacityLimit}
	if err := operations.loadCustomGachas(); err != nil {
		return nil, err
	}
	if err := operations.loadLegacyCustomGachas(); err != nil {
		return nil, err
	}
	if err := operations.reloadGachaConfigurations(); err != nil {
		return nil, err
	}
	if err := operations.loadRuntimeSettings(); err != nil {
		return nil, err
	}
	if err := operations.loadMaintenance(); err != nil {
		return nil, err
	}
	if err := operations.loadEvolutionRestrictions(); err != nil {
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
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	for _, groupID := range publication.GroupIDs {
		if _, exists := operations.managedGachaGroups[groupID]; !exists {
			return accountstore.Document{}, fmt.Errorf("unknown managed CN gacha group %d", groupID)
		}
		if len(unique) == 0 || unique[len(unique)-1] != groupID {
			unique = append(unique, groupID)
		}
	}
	for _, groupID := range unique {
		for id := range operations.legacyCustomGachas {
			if operations.gachaBases[id].GroupID != groupID {
				continue
			}
			live := false
			for _, c := range operations.gachaConfigurations {
				if c.Profile.GachaID == id {
					live = true
					break
				}
			}
			if !live {
				return accountstore.Document{}, errors.New("请先分别保存、预览并发布该组所有抽取入口，再统一开放")
			}
		}
	}

	publication.GroupIDs = unique
	// A deleted operator pool stays hidden by its configuration. Keeping it in an existing publication lets a
	// restore bring it back as before; only newly opening a deleted pool is refused.
	current, err := operations.storage.ReadDocument(gachaPublicationKey)
	if err != nil {
		return accountstore.Document{}, err
	}
	var saved gachaPublication
	if current.Revision > 0 {
		if err := json.Unmarshal(current.Payload, &saved); err != nil {
			return accountstore.Document{}, fmt.Errorf("decode CN gacha publication: %w", err)
		}
	}
	for _, groupID := range unique {
		if pool, custom := operations.customGachaByGroup(groupID); custom && pool.Deleted && !slices.Contains(saved.GroupIDs, groupID) {
			return accountstore.Document{}, fmt.Errorf("卡池 %d 已删除，恢复后才能开启", pool.GachaID)
		}
	}
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
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
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
	_, custom := operations.customGachas[gachaID]
	live := !custom
	for _, config := range operations.gachaConfigurations {
		if config.Profile.GachaID == gachaID {
			live = true
			now := time.Now().Unix()
			if config.Disabled || now < config.StartUnix || (config.EndUnix != 0 && now >= config.EndUnix) {
				return false
			}
		}
	}
	if !live {
		return false
	}
	groupID, managed := operations.managedGachaGroupByID[gachaID]
	if !managed {
		return gachaID == 90000100 || gachaID == 90000200
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
	return battlePublicationAllowlist(publication, operations.battleGroupIDs[key], time.Now())
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
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	return cloneIntSet(operations.managedGachaGroups)
}

func (operations *Operations) customGachaByGroup(groupID int) (customGachaPool, bool) {
	for _, pool := range operations.customGachas {
		if pool.GroupID == groupID {
			return pool, true
		}
	}
	return customGachaPool{}, false
}
