package cnbootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

//go:embed admin_ui.html
var cnAdminHTML []byte

//go:embed admin_operations.js
var cnAdminOperationsJS []byte

//go:embed admin_ui.js
var cnAdminJS []byte

//go:embed admin_ui.css
var cnAdminCSS []byte

//go:embed admin_accounts.js
var cnAdminAccountsJS []byte

//go:embed admin_mail.js
var cnAdminMailJS []byte

//go:embed admin_settings.js
var cnAdminSettingsJS []byte

//go:embed admin_content.js
var cnAdminContentJS []byte

//go:embed admin_player_policy.js
var cnAdminPlayerPolicyJS []byte

// The hash-locked CN client DECK_RANK enum ends at SSSS (17). Admin setup may
// only advance this persisted high-water mark; normal gameplay remains the
// owner of calculated deck rank and no card or deck data is rewritten here.
const cnAdminMaximumArthurRank = 17

type cnDeploymentHandler struct {
	http.Handler
	admin http.Handler
}

func (handler *cnDeploymentHandler) AdminHandler() http.Handler {
	return handler.admin
}

type cnAdminBattleGroup struct {
	PastName      string   `json:"past_name,omitempty"`
	GroupID       int      `json:"group_id"`
	Name          string   `json:"name"`
	PictureID     int      `json:"picture_id"`
	BossCount     int      `json:"boss_count"`
	BossIDs       []int    `json:"boss_ids"`
	Category      string   `json:"category"`
	Difficulties  []string `json:"difficulties"`
	SegmentCounts []int    `json:"segment_counts"`
	MaxSegments   int      `json:"max_segments"`
	ImageURL      string   `json:"image_url"`
}

type cnAdminCatalogEntry struct {
	ResourceState string   `json:"resource_state,omitempty"`
	Kind          string   `json:"kind"`
	RewardType    int      `json:"reward_type"`
	RewardTypeID  int      `json:"reward_type_id"`
	Name          string   `json:"name"`
	Detail        string   `json:"detail"`
	ImageURL      string   `json:"image_url,omitempty"`
	Rarity        int      `json:"rarity,omitempty"`
	ArthurType    int8     `json:"arthur_type,omitempty"`
	LevelMax      int      `json:"level_max,omitempty"`
	FameMax       int      `json:"fame_max,omitempty"`
	LoveMax       int      `json:"love_max,omitempty"`
	PictID        int      `json:"pict_id,omitempty"`
	SourceTags    []string `json:"source_tags,omitempty"`
	GachaEligible bool     `json:"gacha_eligible"`
}

type cnAdminCatalogImageCoverage struct {
	EntryCount          int   `json:"entry_count"`
	ResolvedSourceCount int   `json:"resolved_source_count"`
	SourceGapCount      int   `json:"source_gap_count"`
	SourceGapIDs        []int `json:"source_gap_ids"`
	MissingOutputCount  int   `json:"missing_output_count"`
	MissingOutputs      []int `json:"missing_outputs"`
}

type cnAdminAssetManifest struct {
	SchemaVersion               int                                    `json:"schema_version"`
	ClientProfile               string                                 `json:"client_profile"`
	State                       string                                 `json:"state"`
	Purpose                     string                                 `json:"purpose"`
	CatalogImageCoverage        map[string]cnAdminCatalogImageCoverage `json:"catalog_image_coverage"`
	CatalogMissingOutputCount   int                                    `json:"catalog_missing_output_count"`
	BossGroupMissingOutputCount int                                    `json:"boss_group_missing_output_count"`
}

type cnAdmin struct {
	accounts         *cnAccountStore
	business         *cnAccountBusinessRouter
	operations       *cnOperationStore
	groups           []cnAdminBattleGroup
	catalog          []cnAdminCatalogEntry
	catalogByKey     map[string]cnAdminCatalogEntry
	assetURLs        map[string]struct{}
	assetsRoot       string
	knownGroups      map[int]struct{}
	pastGroups       []cnAdminBattleGroup
	bossCount        int
	multiplayerHub   *multiplayer.Hub
	advertiseHost    string
	gamePort         int
	logger           *slog.Logger
	progression      release.PlayerProgressionPolicy
	gachaPresets     []cnAdminGachaPreset
	knownGachaGroups map[int]struct{}
	gachaBannerPaths map[string]string
}

type cnAdminGachaPreset struct {
	PublicationKey string `json:"publication_key"`
	GroupID        int    `json:"group_id"`
	Name           string `json:"name"`
	BannerKey      string `json:"banner_key"`
	ImageURL       string `json:"image_url"`
	PaymentItemID  int    `json:"payment_item_id"`
	PaymentItem    string `json:"payment_item"`
	Price          int    `json:"price"`
	DrawCount      int    `json:"draw_count"`
	CardCount      int    `json:"card_count"`
	GachaIDs       []int  `json:"gacha_ids"`
}

type cnAdminAccount struct {
	TrainingStep     int    `json:"training_step"`
	ArthurRank       int    `json:"arthur_rank"`
	CardMax          int    `json:"card_max"`
	FeatureIDs       []uint `json:"unlocked_features"`
	UserID           int    `json:"user_id"`
	LoginUUID        string `json:"login_uuid"`
	Name             string `json:"name"`
	CreatedUTC       string `json:"created_utc"`
	LastLoginUTC     string `json:"last_login_utc"`
	Revision         int    `json:"revision"`
	UpdatedUTC       string `json:"updated_utc"`
	Level            int    `json:"level"`
	Gold             int    `json:"gold"`
	FriendPoint      int    `json:"friend_point"`
	PaidCrystal      int    `json:"paid_crystal"`
	FreeCrystal      int    `json:"free_crystal"`
	AP               int    `json:"ap"`
	APMax            int    `json:"ap_max"`
	BP               int    `json:"bp"`
	BPMax            int    `json:"bp_max"`
	CardCount        int    `json:"card_count"`
	StackCardKinds   int    `json:"stack_card_kinds"`
	PVPPoint         int    `json:"pvp_point"`
	ActiveArthurType int    `json:"active_arthur_type"`
}

// cnAdminAccountListItem is deliberately limited to relational metadata. The
// account snapshot can be several megabytes, so the list endpoint must not
// decode every player's cards, decks, items, and history just to draw a row.
type cnAdminAccountListItem struct {
	Name         string `json:"name"`
	Username     string `json:"username"`
	UserID       int    `json:"user_id"`
	LoginUUID    string `json:"login_uuid"`
	CreatedUTC   string `json:"created_utc"`
	LastLoginUTC string `json:"last_login_utc"`
	Revision     int    `json:"revision"`
	UpdatedUTC   string `json:"updated_utc"`
}

type cnAdminPublicationState struct {
	StartUnix  int64  `json:"start_unix"`
	EndUnix    int64  `json:"end_unix"`
	Mode       string `json:"mode"`
	GroupIDs   []int  `json:"group_ids"`
	Revision   int    `json:"revision"`
	UpdatedUTC string `json:"updated_utc,omitempty"`
}

type cnAdminGachaPublicationState struct {
	GroupIDs   []int  `json:"group_ids"`
	Revision   int    `json:"revision"`
	UpdatedUTC string `json:"updated_utc,omitempty"`
}

func newCNAdminHandler(
	accounts *cnAccountStore,
	business *cnAccountBusinessRouter,
	operations *cnOperationStore,
	battleMasterPath string,
	cardMasterPath string,
	itemMasterPath string,
	multiplayerHub *multiplayer.Hub,
	advertiseHost string,
	gamePort int,
	logger *slog.Logger,
	progression release.PlayerProgressionPolicy,
	gachaBannerPaths map[string]string,
	assetMapPaths ...string,
) (http.Handler, error) {
	if accounts == nil || business == nil || operations == nil || multiplayerHub == nil {
		return nil, errors.New("CN admin runtime is incomplete")
	}
	master, err := loadCNBattleRuntimeMaster(battleMasterPath)
	if err != nil {
		return nil, err
	}
	master, err = projectCNBattleOwnDeckVariants(master)
	if err != nil {
		return nil, err
	}
	groups := make([]cnAdminBattleGroup, 0, len(master.Groups)+4)
	knownGroups := make(map[int]struct{}, len(master.Groups)+4)
	knownBosses := make(map[int]struct{}, len(master.Replays)+4)
	replaySegments := make(map[int]int, len(master.Replays))
	for _, replay := range master.Replays {
		count := len(replay.Battles)
		if count == 0 {
			count = 1
		}
		replaySegments[replay.BossID] = count
	}
	appendGroup := func(raw json.RawMessage) error {
		var group struct {
			GroupID   int    `json:"0"`
			Name      string `json:"4"`
			PictureID int    `json:"7"`
			Bosses    []struct {
				BossID     int    `json:"0"`
				Difficulty string `json:"4"`
				IsModel    int    `json:"14"`
			} `json:"10"`
		}
		if err := json.Unmarshal(raw, &group); err != nil {
			return fmt.Errorf("decode CN admin battle group: %w", err)
		}
		if group.GroupID <= 0 || len(group.Bosses) == 0 {
			return errors.New("CN admin battle group has an invalid identity or no bosses")
		}
		if _, exists := knownGroups[group.GroupID]; exists {
			return nil
		}
		bossIDs := make([]int, len(group.Bosses))
		difficulties := make([]string, 0, len(group.Bosses))
		segmentCounts := make([]int, len(group.Bosses))
		maxSegments := 1
		category := "2d"
		for index, boss := range group.Bosses {
			if boss.BossID <= 0 {
				return errors.New("CN admin battle group has an invalid boss ID")
			}
			bossIDs[index] = boss.BossID
			if boss.Difficulty != "" && !slices.Contains(difficulties, boss.Difficulty) {
				difficulties = append(difficulties, boss.Difficulty)
			}
			segmentCounts[index] = max(1, replaySegments[boss.BossID])
			maxSegments = max(maxSegments, segmentCounts[index])
			if _, material := cnBattleMaterialGroupIcons[boss.BossID/100]; material {
				category = "material"
			} else if boss.IsModel == 1 && category != "material" {
				category = "3d"
			}
			knownBosses[boss.BossID] = struct{}{}
		}
		groups = append(groups, cnAdminBattleGroup{
			GroupID: group.GroupID, Name: group.Name, PictureID: group.PictureID,
			BossCount: len(bossIDs), BossIDs: bossIDs, Category: category,
			Difficulties: difficulties, SegmentCounts: segmentCounts, MaxSegments: maxSegments,
			ImageURL: fmt.Sprintf("/assets/boss/%d.webp", group.PictureID),
		})
		knownGroups[group.GroupID] = struct{}{}
		return nil
	}
	for _, raw := range master.Groups {
		if err := appendGroup(raw); err != nil {
			return nil, err
		}
	}
	primaryState, err := accounts.loadState(cnPrimaryUserID)
	if err != nil {
		return nil, fmt.Errorf("load CN admin baseline battle groups: %w", err)
	}
	cardMaster, err := loadCNCardRuntimeMaster(cardMasterPath)
	if err != nil {
		return nil, err
	}
	itemMaster, err := loadCNItemRuntimeMaster(itemMasterPath)
	if err != nil {
		return nil, err
	}
	itemNames := make(map[int]string, len(itemMaster.Items))
	for _, item := range itemMaster.Items {
		itemNames[item.ItemID] = item.Name
	}
	presetsByGroup := make(map[int]*cnAdminGachaPreset)
	knownGachaGroups := make(map[int]struct{}, len(operations.managedGachaGroups))
	for _, gacha := range primaryState.Gachas {
		if gacha.GachaID == 90000100 || gacha.GachaID == 90000200 {
			continue
		}
		bannerPath, exists := gachaBannerPaths[gacha.BannerKey]
		if !exists || bannerPath == "" {
			return nil, fmt.Errorf("CN managed gacha %d has no published banner", gacha.GachaID)
		}
		preset := presetsByGroup[gacha.GroupID]
		if preset == nil {
			payment := itemNames[gacha.PayTypeID]
			switch gacha.PayType {
			case 2:
				payment = "友情点"
			case 3:
				payment = "水晶"
			case 6:
				payment = "付费水晶"
			}
			preset = &cnAdminGachaPreset{
				PublicationKey: gacha.PublicationKey, GroupID: gacha.GroupID,
				Name: gacha.Name, BannerKey: gacha.BannerKey,
				ImageURL:      "/gacha-assets/" + gacha.BannerKey + ".png",
				PaymentItemID: gacha.PayTypeID, PaymentItem: payment,
				Price: gacha.Price, DrawCount: gacha.CardNum,
				CardCount: len(gacha.CardIDs) + len(gacha.RewardPool),
			}
			presetsByGroup[gacha.GroupID] = preset
		} else if preset.PublicationKey != gacha.PublicationKey {
			return nil, fmt.Errorf("CN managed gacha group %d has inconsistent metadata", gacha.GroupID)
		}
		preset.GachaIDs = append(preset.GachaIDs, gacha.GachaID)
		knownGachaGroups[gacha.GroupID] = struct{}{}
	}
	gachaPresets := make([]cnAdminGachaPreset, 0, len(presetsByGroup))
	for _, preset := range presetsByGroup {
		sort.Ints(preset.GachaIDs)
		gachaPresets = append(gachaPresets, *preset)
	}
	sort.Slice(gachaPresets, func(left, right int) bool {
		return gachaPresets[left].GroupID < gachaPresets[right].GroupID
	})
	catalog, _, err := buildCNAdminCatalog(cardMaster, itemMaster)
	if err != nil {
		return nil, err
	}
	assetsRoot, err := filepath.Abs(filepath.Join(filepath.Dir(cardMasterPath), "cn602-admin-assets"))
	if err != nil {
		return nil, fmt.Errorf("resolve CN admin asset root: %w", err)
	}
	var cardResources map[int]cnAdminCardResource
	if len(assetMapPaths) > 0 {
		cardResources, err = loadCNAdminCardResources(cardMaster, cardMasterPath, assetMapPaths[0])
		if err != nil {
			return nil, err
		}
	}
	catalog, err = applyCNAdminCatalogAssetCoverage(catalog, assetsRoot, cardResources)
	if err != nil {
		return nil, fmt.Errorf("validate CN admin catalog image coverage: %w", err)
	}
	catalogByKey := make(map[string]cnAdminCatalogEntry, len(catalog))
	for _, entry := range catalog {
		catalogByKey[cnAdminCatalogKey(entry.RewardType, entry.RewardTypeID)] = entry
	}
	for index := range groups {
		imageURL, err := cnAdminBossImageURL(assetsRoot, groups[index].ImageURL)
		if err != nil {
			return nil, fmt.Errorf("validate CN admin boss group %d image: %w", groups[index].GroupID, err)
		}
		groups[index].ImageURL = imageURL
	}
	pastGroups, err := buildCNAdminPastGroups(master.PastBossGroups, groups, catalogByKey)
	if err != nil {
		return nil, err
	}
	assetURLs := make(map[string]struct{}, len(catalog)+len(groups))
	for _, entry := range catalog {
		if entry.ImageURL != "" {
			assetURLs[entry.ImageURL] = struct{}{}
		}
	}
	for _, group := range groups {
		if group.ImageURL != "" {
			assetURLs[group.ImageURL] = struct{}{}
		}
	}
	admin := &cnAdmin{
		accounts: accounts, business: business, operations: operations,
		groups: groups, catalog: catalog, catalogByKey: catalogByKey,
		pastGroups: pastGroups,
		assetsRoot: assetsRoot, assetURLs: assetURLs,
		knownGroups: knownGroups, bossCount: len(knownBosses),
		multiplayerHub: multiplayerHub, advertiseHost: advertiseHost,
		gamePort: gamePort, logger: logger, progression: progression,
		gachaPresets: gachaPresets, knownGachaGroups: knownGachaGroups,
		gachaBannerPaths: gachaBannerPaths,
	}
	for _, config := range operations.gachaConfigurations {
		edit := cnAdminGachaConfigFromProfile(config.Profile)
		edit.StartUnix, edit.EndUnix = config.StartUnix, config.EndUnix
		preview, err := admin.validateGachaConfig(edit)
		if err != nil {
			return nil, err
		}
		if !preview["publishable"].(bool) {
			return nil, errors.New("published gacha card resources are unavailable")
		}
	}
	if operations.content != nil {
		for _, config := range operations.content.drops {
			if err := admin.validateDropConfig(config); err != nil {
				return nil, fmt.Errorf("saved drop configuration: %w", err)
			}
		}
		for _, shop := range operations.content.shops {
			if err := admin.validateExchange(shop, operations.content.configuration.State.TradeShopProfiles); err != nil {
				return nil, fmt.Errorf("saved exchange configuration: %w", err)
			}
		}
	}
	router := chi.NewRouter()
	router.Use(cnAdminSecurityHeaders)
	router.Get("/api/player-policy", admin.playerPolicy)
	router.Get("/api/player-policy/notice-preview", admin.operations.localNotice)
	router.Put("/api/player-policy", admin.savePlayerPolicy)
	router.Get("/player-policy.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(cnAdminPlayerPolicyJS)
	})
	router.Get("/", admin.index)
	for path, content := range map[string][]byte{"/admin.js": cnAdminJS, "/accounts.js": cnAdminAccountsJS, "/mail.js": cnAdminMailJS, "/settings.js": cnAdminSettingsJS, "/admin.css": cnAdminCSS} {
		router.Get(path, func(w http.ResponseWriter, _ *http.Request) {
			contentType := "text/javascript; charset=utf-8"
			if strings.HasSuffix(path, ".css") {
				contentType = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(content)
		})
	}
	router.Get("/favicon.ico", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	router.Get("/operations.js", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = writer.Write(cnAdminOperationsJS)
	})
	router.Post("/api/mail-preview", admin.mailPreview)
	router.Post("/api/mail-batches/preview", admin.previewMailBatch)
	router.Post("/api/mail-batches", admin.createMailBatch)
	router.Get("/api/mail-batches", admin.listMailBatches)
	router.Get("/api/mail-batches/{batchID}", admin.mailBatchStatus)
	router.Post("/api/mail-batches/{batchID}/run", admin.runMailBatch)
	router.Get("/api/health", admin.health)
	router.Get("/api/status", admin.status)
	router.Get("/api/settings", admin.runtimeSettings)
	router.Get("/content.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(cnAdminContentJS)
	})
	router.Get("/api/boss-drops", admin.dropEditor)
	router.Put("/api/boss-drops", admin.saveDropEditor)
	router.Get("/api/exchanges", admin.exchangeEditor)
	router.Put("/api/exchanges/{shopID}", admin.saveExchangeEditor)
	router.Put("/api/settings", admin.setRuntimeSettings)
	router.Get("/api/accounts", admin.accountList)
	router.Post("/api/accounts/resolve", admin.resolveAccounts)
	router.Get("/api/accounts/{userID}", admin.accountDetail)
	router.Post("/api/accounts/{userID}/grant", admin.grantAccountResources)
	router.Post("/api/accounts/{userID}/mail", admin.sendAccountMail)
	router.Post("/api/accounts/{userID}/credentials", admin.setAccountCredentials)
	router.Delete("/api/accounts/{userID}/credentials", admin.unbindAccount)
	router.Get("/api/catalog", admin.catalogEntries)
	router.Post("/api/catalog/resolve", admin.resolveCatalog)
	router.Get("/assets/{kind}/{file}", admin.catalogAsset)
	router.Get("/api/boss-groups", admin.bossGroups)
	router.Get("/api/boss-policy", admin.bossPolicy)
	router.Put("/api/boss-policy", admin.setBossPolicy)
	router.Get("/api/gacha-editor", admin.gachaEditorList)
	router.Post("/api/gacha-editor/{action}", admin.gachaEditorAction)
	router.Get("/api/gacha-presets", admin.gachaPresetList)
	router.Get("/api/gacha-policy", admin.gachaPolicy)
	router.Put("/api/gacha-policy", admin.setGachaPolicy)
	router.Get("/gacha-assets/{file}", admin.gachaAsset)
	router.Get("/api/audit", admin.audit)
	router.NotFound(func(writer http.ResponseWriter, _ *http.Request) {
		writeCNAdminError(writer, http.StatusNotFound, "admin route not found")
	})
	return router, nil
}

func cnAdminCatalogKey(rewardType int, rewardTypeID int) string {
	return strconv.Itoa(rewardType) + ":" + strconv.Itoa(rewardTypeID)
}

func requireCNAdminAsset(assetsRoot string, urlPath string) error {
	if !strings.HasPrefix(urlPath, "/assets/") {
		return fmt.Errorf("invalid asset URL %q", urlPath)
	}
	relativeURL := strings.TrimPrefix(urlPath, "/assets/")
	path := filepath.Join(assetsRoot, filepath.FromSlash(relativeURL))
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(assetsRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("asset URL escapes root: %q", urlPath)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("asset %q is absent: %w", urlPath, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("asset %q is not a non-empty regular file", urlPath)
	}
	return nil
}

func cnAdminBossImageURL(assetsRoot, urlPath string) (string, error) {
	err := requireCNAdminAsset(assetsRoot, urlPath)
	if errors.Is(err, os.ErrNotExist) {
		// An operator thumbnail is decorative. Packaging checks completeness;
		// runtime can show the group name without blocking the game server.
		return "", nil
	}
	return urlPath, err
}

func applyCNAdminCatalogAssetCoverage(catalog []cnAdminCatalogEntry, assetsRoot string, resources ...map[int]cnAdminCardResource) ([]cnAdminCatalogEntry, error) {
	content, err := os.ReadFile(filepath.Join(assetsRoot, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest cnAdminAssetManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != 2 || manifest.ClientProfile != "cn602-bootstrap" ||
		manifest.State != "PASS" || manifest.Purpose != "loopback_admin_web_thumbnail_only" ||
		manifest.CatalogMissingOutputCount != 0 || manifest.BossGroupMissingOutputCount != 0 {
		return nil, errors.New("manifest identity or output closure is invalid")
	}
	expectedKinds := map[string]struct{}{
		"card": {}, "item": {}, "material": {}, "sphere": {}, "buddy": {},
	}
	entryCounts := make(map[string]int, len(expectedKinds))
	for _, entry := range catalog {
		if entry.Kind != "currency" {
			entryCounts[entry.Kind]++
		}
	}
	gapIDs := make(map[string]map[int]struct{}, len(expectedKinds))
	for kind := range expectedKinds {
		coverage, exists := manifest.CatalogImageCoverage[kind]
		if !exists || coverage.EntryCount != entryCounts[kind] ||
			coverage.ResolvedSourceCount+coverage.SourceGapCount != coverage.EntryCount ||
			coverage.SourceGapCount != len(coverage.SourceGapIDs) ||
			coverage.MissingOutputCount != 0 || len(coverage.MissingOutputs) != 0 {
			return nil, fmt.Errorf("manifest %s catalog coverage is invalid", kind)
		}
		ids := make(map[int]struct{}, len(coverage.SourceGapIDs))
		for _, identity := range coverage.SourceGapIDs {
			if identity <= 0 {
				return nil, fmt.Errorf("manifest %s source gap contains invalid ID %d", kind, identity)
			}
			if _, duplicate := ids[identity]; duplicate {
				return nil, fmt.Errorf("manifest %s source gap contains duplicate ID %d", kind, identity)
			}
			ids[identity] = struct{}{}
		}
		gapIDs[kind] = ids
	}
	if len(manifest.CatalogImageCoverage) != len(expectedKinds) {
		return nil, errors.New("manifest contains an unexpected catalog image kind")
	}
	seenGaps := make(map[string]int, len(expectedKinds))
	resolved := make(map[string]int, len(expectedKinds))
	publishable := make([]cnAdminCatalogEntry, 0, len(catalog))
	for index := range catalog {
		entry := &catalog[index]
		if entry.Kind == "currency" {
			if entry.ImageURL != "" {
				return nil, fmt.Errorf("currency reward %d unexpectedly has an image URL", entry.RewardType)
			}
			publishable = append(publishable, *entry)
			continue
		}
		if _, exists := expectedKinds[entry.Kind]; !exists || entry.RewardTypeID <= 0 || entry.ImageURL == "" {
			return nil, fmt.Errorf("catalog reward %s/%d has an invalid image contract", entry.Kind, entry.RewardTypeID)
		}
		if _, sourceGap := gapIDs[entry.Kind][entry.RewardTypeID]; sourceGap {
			seenGaps[entry.Kind]++
			if entry.Kind == "card" && len(resources) > 0 && resources[0] != nil {
				copy := *entry
				copy.ImageURL = ""
				publishable = append(publishable, copy)
			}
			continue
		}
		if err := requireCNAdminAsset(assetsRoot, entry.ImageURL); err != nil {
			return nil, fmt.Errorf("catalog reward %s/%d: %w", entry.Kind, entry.RewardTypeID, err)
		}
		resolved[entry.Kind]++
		publishable = append(publishable, *entry)
	}
	for kind := range expectedKinds {
		coverage := manifest.CatalogImageCoverage[kind]
		if seenGaps[kind] != coverage.SourceGapCount || resolved[kind] != coverage.ResolvedSourceCount {
			return nil, fmt.Errorf("manifest %s source-gap identities do not match the active catalog", kind)
		}
	}
	if len(resources) > 0 && resources[0] != nil {
		filtered := publishable[:0]
		for _, entry := range publishable {
			if entry.Kind == "material" {
				if name := resources[0][entry.RewardTypeID].Name; name != "" {
					entry.Name = name
				}
			}
			if entry.Kind == "card" {
				resource := resources[0][entry.RewardTypeID]
				if resource.PictID < 10000000 || resource.Excluded {
					continue
				}
				entry.PictID = resource.PictID
				entry.ResourceState = "unavailable"
				if resource.Ready {
					entry.ResourceState = "ready"
				}
			}
			filtered = append(filtered, entry)
		}
		publishable = filtered
	}
	return publishable, nil
}

func buildCNAdminCatalog(cardMaster cnCardRuntimeMaster, itemMaster cnItemRuntimeMaster) ([]cnAdminCatalogEntry, map[string]cnAdminCatalogEntry, error) {
	evolved := make(map[int]bool)
	for _, transition := range cardMaster.EvolutionTransitions {
		evolved[transition.ToCardID] = true
	}
	entries := []cnAdminCatalogEntry{
		{Kind: "currency", RewardType: 0, Name: "亚瑟经验", Detail: "玩家等级经验"},
		{Kind: "currency", RewardType: 4, Name: "金币", Detail: "通用强化与商店货币"},
		{Kind: "currency", RewardType: 9, Name: "友情点", Detail: "友情扭蛋货币"},
		{Kind: "currency", RewardType: 10, Name: "免费水晶", Detail: "非付费水晶余额"},
		{Kind: "currency", RewardType: 12, Name: "体力", Detail: "领取时补充 BP，上限封顶"},
	}
	for _, card := range cardMaster.CardTemplates {
		entries = append(entries, cnAdminCatalogEntry{
			Kind: "card", RewardType: 6, RewardTypeID: card.CardID,
			Name: card.Name, Detail: card.AcquisitionText,
			SourceTags:    cnCardSourceTags(card.AcquisitionText),
			GachaEligible: cnCardCrystalGachaSource(card.AcquisitionText) && !evolved[card.CardID] && card.RarityRank >= 3,
			ImageURL:      fmt.Sprintf("/assets/card/%d.webp", card.CardID),
			Rarity:        card.RarityRank, LevelMax: card.LevelMax,
			ArthurType: cardMaster.DeckRankPolicy.Cards[card.CardID].ArthurType,
			FameMax:    card.FameMax, LoveMax: card.LoveMax,
		})
	}
	for _, item := range itemMaster.Items {
		entries = append(entries, cnAdminCatalogEntry{
			Kind: "item", RewardType: 8, RewardTypeID: item.ItemID,
			Name: item.Name, Detail: item.Description, PictID: item.PictID,
			ImageURL: fmt.Sprintf("/assets/item/%d.webp", item.ItemID),
		})
	}
	for _, stack := range cardMaster.StackCardTemplates {
		entries = append(entries, cnAdminCatalogEntry{
			Kind: "material", RewardType: 13, RewardTypeID: stack.CardID,
			Name:     fmt.Sprintf("素材卡 %d", stack.CardID),
			Detail:   fmt.Sprintf("素材类型 %d · 强化经验 %d", stack.MaterialType, stack.AddExperience),
			ImageURL: fmt.Sprintf("/assets/card/%d.webp", stack.CardID),
		})
	}
	for _, sphere := range cardMaster.SphereDefinitions {
		entries = append(entries, cnAdminCatalogEntry{
			Kind: "sphere", RewardType: 15, RewardTypeID: sphere.SphereID,
			Name: sphere.Name, Detail: sphere.Text, PictID: sphere.PictID,
			ImageURL: fmt.Sprintf("/assets/sphere/%d.webp", sphere.SphereID),
			LevelMax: sphere.MaxLevel,
		})
	}
	for _, buddy := range cardMaster.BuddyDefinitions {
		entries = append(entries, cnAdminCatalogEntry{
			Kind: "buddy", RewardType: 19, RewardTypeID: buddy.BuddyID,
			Name: buddy.Name, Detail: buddy.Rarity, PictID: buddy.PictID,
			ImageURL: fmt.Sprintf("/assets/buddy/%d.webp", buddy.BuddyID),
			LevelMax: buddy.MaxLevel,
		})
	}
	kindOrder := map[string]int{"currency": 0, "card": 1, "item": 2, "material": 3, "sphere": 4, "buddy": 5}
	sort.Slice(entries, func(left int, right int) bool {
		leftOrder, rightOrder := kindOrder[entries[left].Kind], kindOrder[entries[right].Kind]
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if entries[left].RewardTypeID != entries[right].RewardTypeID {
			return entries[left].RewardTypeID < entries[right].RewardTypeID
		}
		return entries[left].Name < entries[right].Name
	})
	byKey := make(map[string]cnAdminCatalogEntry, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || entry.RewardType < 0 || entry.RewardTypeID < 0 ||
			(entry.RewardTypeID == 0 && entry.Kind != "currency") {
			return nil, nil, errors.New("CN admin catalog contains an invalid entry")
		}
		key := cnAdminCatalogKey(entry.RewardType, entry.RewardTypeID)
		if _, duplicate := byKey[key]; duplicate {
			return nil, nil, fmt.Errorf("CN admin catalog contains duplicate reward %s", key)
		}
		byKey[key] = entry
	}
	return entries, byKey, nil
}

func cnAdminSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Cache-Control", "no-store")
		// Binding the listener to loopback alone does not prevent browser DNS
		// rebinding. Refuse non-loopback authorities even for read-only routes.
		host := request.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			writeCNAdminError(writer, http.StatusForbidden, "admin requires a localhost or loopback Host")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func requireCNAdminMutation(request *http.Request) error {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("admin mutations require a loopback client")
	}
	if request.Header.Get("X-Kairisei-Admin-Action") != "apply" {
		return errors.New("admin action confirmation header is missing")
	}
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		return errors.New("admin mutations require application/json")
	}
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return errors.New("cross-site admin mutation rejected")
	}
	return nil
}

func decodeCNAdminJSON(request *http.Request, target any) error {
	return decodeCNAdminJSONLimit(request, target, 64*1024)
}

func decodeCNAdminJSONLimit(request *http.Request, target any, maxBodyBytes int) error {
	body, err := io.ReadAll(io.LimitReader(request.Body, int64(maxBodyBytes)+1))
	if err != nil {
		return err
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("admin request exceeds %d KiB", maxBodyBytes/1024)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("admin request must contain exactly one JSON value")
	}
	return nil
}

func writeCNAdminJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeCNAdminError(writer http.ResponseWriter, status int, message string) {
	writeCNAdminJSON(writer, status, map[string]any{"state": "FAIL", "error": message})
}

func (admin *cnAdmin) index(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(cnAdminHTML)
}

func (admin *cnAdmin) health(writer http.ResponseWriter, _ *http.Request) {
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "schema_version": cnSaveDatabaseSchemaVersion,
	})
}

func (admin *cnAdmin) status(writer http.ResponseWriter, _ *http.Request) {
	accountCount, err := admin.accountCount()
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	policy, err := admin.publicationState()
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "client_profile": "cn602-bootstrap",
		"game_endpoint": fmt.Sprintf("http://%s:%d", admin.advertiseHost, admin.gamePort),
		"battle_port":   admin.gamePort + 1, "active_rooms": admin.multiplayerHub.RoomCount(),
		"max_player_level": admin.progression.MaxLevel, "account_count": accountCount, "boss_group_count": len(admin.groups),
		"boss_count": admin.bossCount, "publication": policy,
		"sqlite_schema_version": cnSaveDatabaseSchemaVersion,
		"server_time_utc":       time.Now().UTC().Format(time.RFC3339),
	})
}

func (admin *cnAdmin) accountCount() (int, error) {
	database, err := admin.accounts.storage.open()
	if err != nil {
		return 0, err
	}
	defer database.Close()
	var count int
	if err := database.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM cn_local_account`,
	).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// loadAccountState applies the same player progression migration used when a
// game handler is created. The admin list and grant paths otherwise expose and
// mutate dormant accounts before their next login using stale level-derived
// BP caps and job parameters.
func (admin *cnAdmin) loadAccountState(userID int) (release.State, error) {
	state, err := admin.accounts.loadState(userID)
	if err != nil {
		return release.State{}, err
	}
	changed, err := applyCNPlayerProgressionRuntimeMaster(&state, admin.progression)
	if err != nil {
		return release.State{}, err
	}
	if !changed {
		return state, nil
	}
	if err := admin.accounts.persistState(userID, state); err != nil {
		return release.State{}, fmt.Errorf("persist CN admin player progression migration: %w", err)
	}
	admin.business.invalidate(userID)
	return state, nil
}

func (admin *cnAdmin) accountSnapshotMetadata(userID int) (int, string, error) {
	database, err := admin.accounts.storage.open()
	if err != nil {
		return 0, "", err
	}
	defer database.Close()
	query := `SELECT revision, updated_utc FROM cn_account_snapshot WHERE user_id = ?`
	arguments := []any{userID}
	if userID == cnPrimaryUserID {
		query = `SELECT revision, updated_utc FROM cn_save_snapshot WHERE singleton = 1`
		arguments = nil
	}
	var revision int
	var updated string
	if err := database.QueryRow(query, arguments...).Scan(&revision, &updated); err != nil {
		return 0, "", err
	}
	return revision, updated, nil
}

func (admin *cnAdmin) accountDetail(writer http.ResponseWriter, request *http.Request) {
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < cnPrimaryUserID {
		writeCNAdminError(writer, http.StatusBadRequest, "invalid user ID")
		return
	}
	lock := admin.business.accountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	state, err := admin.loadAccountState(userID)
	if err != nil {
		writeCNAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	database, err := admin.accounts.storage.open()
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	defer database.Close()
	account := cnAdminAccount{
		UserID: userID, Name: state.User.Name, Level: state.User.Level,
		Gold: state.User.Gold, FriendPoint: state.User.FriendPoint,
		PaidCrystal: state.User.Coin, FreeCrystal: state.User.CoinFree,
		AP: state.User.AP, APMax: state.User.APMax, BP: state.User.BP, BPMax: state.User.BPMax,
		CardCount: len(state.Cards), StackCardKinds: len(state.StackCards),
		PVPPoint: state.User.PVPPoint, ActiveArthurType: state.User.ActiveArthurType,
		TrainingStep: state.Onboarding.Step, ArthurRank: state.User.ArthurRank, CardMax: state.User.CardMax, FeatureIDs: state.User.UnlockedFeatureIDs,
	}
	if err := database.QueryRowContext(
		context.Background(),
		`SELECT login_uuid, created_utc, last_login_utc FROM cn_local_account WHERE user_id = ?`,
		userID,
	).Scan(&account.LoginUUID, &account.CreatedUTC, &account.LastLoginUTC); err != nil {
		writeCNAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	account.Revision, account.UpdatedUTC, err = admin.accountSnapshotMetadata(userID)
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "account": account})
}

func (admin *cnAdmin) catalogEntries(writer http.ResponseWriter, request *http.Request) {
	kind := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind")))
	source := request.URL.Query().Get("source")
	arthurType := 0
	if raw := request.URL.Query().Get("arthur_type"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 4 {
			writeCNAdminError(writer, http.StatusBadRequest, "card arthur_type must be 0 through 4")
			return
		}
		arthurType = parsed
	}
	query := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("q")))
	limit := 80
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeCNAdminError(writer, http.StatusBadRequest, "catalog limit must be 1 through 200")
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := request.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeCNAdminError(writer, http.StatusBadRequest, "catalog offset must be non-negative")
			return
		}
		offset = parsed
	}
	if kind != "" {
		valid := map[string]bool{"currency": true, "card": true, "item": true, "material": true, "sphere": true, "buddy": true}
		if !valid[kind] {
			writeCNAdminError(writer, http.StatusBadRequest, "unknown catalog kind")
			return
		}
	}
	filtered := make([]cnAdminCatalogEntry, 0, limit)
	total := 0
	for _, entry := range admin.catalog {
		if kind != "" && entry.Kind != kind {
			continue
		}
		if source != "" && !slices.Contains(entry.SourceTags, source) {
			continue
		}
		if arthurType != 0 && (entry.Kind != "card" || int(entry.ArthurType) != arthurType) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(
			fmt.Sprintf("%s %s %d %d", entry.Name, entry.Detail, entry.RewardTypeID, entry.PictID),
		), query) {
			continue
		}
		if total >= offset && len(filtered) < limit {
			filtered = append(filtered, entry)
		}
		total++
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "entries": filtered, "total": total,
		"offset": offset, "limit": limit,
	})
}

func (admin *cnAdmin) catalogAsset(writer http.ResponseWriter, request *http.Request) {
	kind := chi.URLParam(request, "kind")
	file := chi.URLParam(request, "file")
	if kind == "" || file == "" || filepath.Base(file) != file || filepath.Ext(file) != ".webp" {
		writeCNAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	urlPath := "/assets/" + kind + "/" + file
	if _, allowed := admin.assetURLs[urlPath]; !allowed {
		writeCNAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	path := filepath.Join(admin.assetsRoot, kind, file)
	absolute, err := filepath.Abs(path)
	if err != nil {
		writeCNAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	relative, err := filepath.Rel(admin.assetsRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		writeCNAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		writeCNAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	writer.Header().Set("Content-Type", "image/webp")
	http.ServeFile(writer, request, absolute)
}

type cnAdminMailRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Discardable    bool   `json:"discardable"`
	RewardType     int    `json:"reward_type"`
	RewardTypeID   int    `json:"reward_type_id"`
	Quantity       int    `json:"quantity"`
	CardLevel      int    `json:"card_level"`
	CardFame       int    `json:"card_fame"`
	CardLove       int    `json:"card_love"`
}

const (
	cnAdminMaximumInstanceRewardQuantity = 100
	cnAdminMaximumStackedRewardQuantity  = 10000000
)

func cnAdminMailPresentID(userID int, idempotencyKey string) int64 {
	digest := sha256.Sum256([]byte(fmt.Sprintf("cn602-admin-mail\x00%d\x00%s", userID, idempotencyKey)))
	value := binary.BigEndian.Uint64(digest[:8]) & uint64(math.MaxInt64)
	if value == 0 {
		value = 1
	}
	return int64(value)
}

func (admin *cnAdmin) mailReward(request cnAdminMailRequest) (release.Reward, cnAdminCatalogEntry, error) {
	entry, exists := admin.catalogByKey[cnAdminCatalogKey(request.RewardType, request.RewardTypeID)]
	if !exists {
		return release.Reward{}, cnAdminCatalogEntry{}, errors.New("reward is not present in the active CN catalog")
	}
	if entry.ResourceState == "unavailable" {
		return release.Reward{}, cnAdminCatalogEntry{}, errors.New("该卡牌尚未进入当前运行资源清单")
	}
	maximum := cnAdminMaximumStackedRewardQuantity
	if request.RewardType == 6 || request.RewardType == 15 || request.RewardType == 19 {
		maximum = cnAdminMaximumInstanceRewardQuantity
	}
	if request.Quantity < 1 || request.Quantity > maximum {
		return release.Reward{}, cnAdminCatalogEntry{}, fmt.Errorf("reward quantity must be 1 through %d", maximum)
	}
	reward := release.Reward{
		Type: request.RewardType, Num: request.Quantity,
		RewardTypeID: request.RewardTypeID, CardSkillLevels: []int16{},
	}
	if request.RewardType != 6 {
		if request.CardLevel != 0 || request.CardFame != 0 || request.CardLove != 0 {
			return release.Reward{}, cnAdminCatalogEntry{}, errors.New("card progression fields require a card reward")
		}
		return reward, entry, nil
	}
	if request.CardLevel == 0 {
		request.CardLevel = 1
	}
	if request.CardFame == 0 {
		request.CardFame = 1
	}
	if request.CardLevel < 1 || request.CardLevel > entry.LevelMax ||
		request.CardFame < 1 || request.CardFame > entry.FameMax ||
		request.CardLove < 0 || request.CardLove > entry.LoveMax ||
		request.CardLevel > math.MaxInt16 || request.CardFame > math.MaxInt16 {
		return release.Reward{}, cnAdminCatalogEntry{}, errors.New("card progression values exceed the official card definition")
	}
	reward.CardLevel = int16(request.CardLevel)
	reward.CardFame = int16(request.CardFame)
	reward.CardLove = request.CardLove
	reward.CardSkillLevels = []int16{1}
	return reward, entry, nil
}

func (admin *cnAdmin) sendAccountMail(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNAdminMutation(request); err != nil {
		writeCNAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < cnPrimaryUserID {
		writeCNAdminError(writer, http.StatusBadRequest, "invalid user ID")
		return
	}
	var mail cnAdminMailRequest
	if err := decodeCNAdminJSON(request, &mail); err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	mail.IdempotencyKey = strings.TrimSpace(mail.IdempotencyKey)
	mail.Title = strings.TrimSpace(mail.Title)
	mail.Message = strings.TrimSpace(mail.Message)
	if len(mail.IdempotencyKey) < 8 || len(mail.IdempotencyKey) > 128 ||
		len(mail.Title) < 1 || len([]rune(mail.Title)) > 40 ||
		len(mail.Message) < 1 || len([]rune(mail.Message)) > 200 {
		writeCNAdminError(writer, http.StatusBadRequest, "mail key, title, or message length is invalid")
		return
	}
	reward, entry, err := admin.mailReward(mail)
	if err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	presentID := cnAdminMailPresentID(userID, mail.IdempotencyKey)
	lock := admin.business.accountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	state, err := admin.loadAccountState(userID)
	if err != nil {
		writeCNAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	for _, group := range [][]release.Present{state.Engagement.Presents, state.Engagement.Histories} {
		for _, existing := range group {
			if existing.AdminIdempotencyKey == mail.IdempotencyKey {
				if existing.PresentID != presentID {
					writeCNAdminError(writer, http.StatusConflict, "mail idempotency metadata is inconsistent")
					return
				}
				// A key identifies one immutable mail, not any subsequent request
				// with that key. Receipt changes State in-place; it is not part
				// of the immutable mail content, even before explicit deletion.
				if !cnAdminMailPayloadMatches(existing, mail, reward) {
					writeCNAdminError(writer, http.StatusConflict, "mail idempotency key was already used with different content")
					return
				}
				writeCNAdminJSON(writer, http.StatusOK, map[string]any{
					"state": "PASS", "user_id": userID, "present_id": presentID,
					"duplicate": true, "discardable": existing.State == 1,
					"reward": existing.Reward, "catalog": entry,
				})
				return
			}
			if existing.PresentID == presentID {
				writeCNAdminError(writer, http.StatusConflict, "mail present ID collision")
				return
			}
		}
	}
	emptyReward := release.Reward{CardSkillLevels: []int16{}}
	presentState := int8(0)
	if mail.Discardable {
		presentState = 1
	}
	present := release.Present{
		PresentID: presentID, IssuedAtUnix: time.Now().Unix(), Reward: reward,
		Reward0: emptyReward, Reward1: emptyReward, Reward2: emptyReward,
		Comment: mail.Message, Reason: 0, State: presentState, Title: mail.Title,
		URL: "", AdminIdempotencyKey: mail.IdempotencyKey,
	}
	state.Engagement.Presents = append(state.Engagement.Presents, present)
	auditPayload := map[string]any{
		"idempotency_key": mail.IdempotencyKey, "present_id": presentID,
		"title": mail.Title, "message": mail.Message, "discardable": mail.Discardable,
		"reward":  reward,
		"catalog": map[string]any{"kind": entry.Kind, "name": entry.Name},
	}
	if err := admin.accounts.persistStateWithAudit(userID, state, &cnAdminAudit{
		Operation: "account-mail", Target: strconv.Itoa(userID), Payload: auditPayload,
	}); err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	admin.business.invalidate(userID)
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "user_id": userID, "present_id": presentID,
		"duplicate": false, "discardable": mail.Discardable,
		"reward": reward, "catalog": entry,
		"audit_ok": true,
	})
}

func cnAdminMailPayloadMatches(existing release.Present, mail cnAdminMailRequest, reward release.Reward) bool {
	previous := existing.Reward
	return existing.Title == mail.Title && existing.Comment == mail.Message &&
		previous.Type == reward.Type && previous.Num == reward.Num && previous.RewardTypeID == reward.RewardTypeID &&
		previous.CardLevel == reward.CardLevel && previous.CardFame == reward.CardFame && previous.CardLove == reward.CardLove &&
		slices.Equal(previous.CardSkillLevels, reward.CardSkillLevels)
}

func (admin *cnAdmin) grantAccountResources(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNAdminMutation(request); err != nil {
		writeCNAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < cnPrimaryUserID {
		writeCNAdminError(writer, http.StatusBadRequest, "invalid user ID")
		return
	}
	var grant struct {
		IdempotencyKey   string `json:"idempotency_key"`
		Gold             int    `json:"gold"`
		FriendPoint      int    `json:"friend_point"`
		FreeCrystal      int    `json:"free_crystal"`
		PaidCrystal      int    `json:"paid_crystal"`
		TargetLevel      int    `json:"target_level"`
		TargetArthurRank int    `json:"target_arthur_rank"`
		FillAP           bool   `json:"fill_ap"`
		FillBP           bool   `json:"fill_bp"`
	}
	if err := decodeCNAdminJSON(request, &grant); err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	for _, value := range []int{grant.Gold, grant.FriendPoint, grant.FreeCrystal, grant.PaidCrystal} {
		if value < 0 || value > 10000000 {
			writeCNAdminError(writer, http.StatusBadRequest, "each resource grant must be 0 through 10,000,000")
			return
		}
	}
	if grant.TargetLevel < 0 || grant.TargetLevel > admin.progression.MaxLevel {
		writeCNAdminError(writer, http.StatusBadRequest, "target level is outside the active progression policy")
		return
	}
	if grant.TargetArthurRank < 0 || grant.TargetArthurRank > cnAdminMaximumArthurRank {
		writeCNAdminError(writer, http.StatusBadRequest, "target Arthur rank is outside the CN client deck-rank range")
		return
	}
	if grant.Gold == 0 && grant.FriendPoint == 0 && grant.FreeCrystal == 0 && grant.PaidCrystal == 0 && grant.TargetLevel == 0 && grant.TargetArthurRank == 0 && !grant.FillAP && !grant.FillBP {
		writeCNAdminError(writer, http.StatusBadRequest, "resource grant is empty")
		return
	}
	if grant.IdempotencyKey != "" && !validCNBatchID(grant.IdempotencyKey) {
		writeCNAdminError(writer, 400, "操作编号无效")
		return
	}
	requestJSON, _ := json.Marshal(grant)
	digest := sha256.Sum256(requestJSON)
	requestSHA := hex.EncodeToString(digest[:])
	operationKey := "account-grant:" + grant.IdempotencyKey
	lock := admin.business.accountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	if grant.IdempotencyKey != "" {
		receipts := &cnOperationStore{storage: admin.accounts.storage}
		previous, err := receipts.actionReceipt(operationKey, userID, requestSHA)
		if err != nil {
			writeCNAdminError(writer, 409, err.Error())
			return
		}
		if previous != nil {
			writeCNAdminJSON(writer, 200, previous)
			return
		}
	}
	state, err := admin.loadAccountState(userID)
	if err != nil {
		writeCNAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	if err := applyCNAdminTargetLevel(&state, admin.progression, grant.TargetLevel); err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyCNAdminTargetArthurRank(&state, grant.TargetArthurRank); err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	for _, pair := range [][2]int{{state.User.Gold, grant.Gold}, {state.User.FriendPoint, grant.FriendPoint}, {state.User.CoinFree, grant.FreeCrystal}, {state.User.Coin, grant.PaidCrystal}} {
		if pair[1] > math.MaxInt-pair[0] {
			writeCNAdminError(writer, http.StatusBadRequest, "resource total would overflow")
			return
		}
	}
	state.User.Gold += grant.Gold
	state.User.FriendPoint += grant.FriendPoint
	state.User.CoinFree += grant.FreeCrystal
	state.User.Coin += grant.PaidCrystal
	if grant.FillAP {
		state.User.AP = state.User.APMax
		state.Explore.APNextRecoveryUnix = 0
	}
	if grant.FillBP {
		state.User.BP = state.User.BPMax
		state.BattlePoint.NextRecoveryUnix = 0
	}
	response := map[string]any{
		"state": "PASS", "user_id": userID, "gold": state.User.Gold,
		"friend_point": state.User.FriendPoint,
		"free_crystal": state.User.CoinFree, "paid_crystal": state.User.Coin,
		"level": state.User.Level, "arthur_rank": state.User.ArthurRank, "ap": state.User.AP,
		"bp": state.User.BP, "bp_max": state.User.BPMax, "audit_ok": true,
	}
	var receipt *cnAdminActionReceipt
	if grant.IdempotencyKey != "" {
		receipt = &cnAdminActionReceipt{OperationKey: operationKey, UserID: userID, RequestSHA256: requestSHA, Result: response}
	}
	auditPayload := map[string]any{
		"request": grant, "result": map[string]int{
			"gold": state.User.Gold, "friend_point": state.User.FriendPoint, "free_crystal": state.User.CoinFree,
			"paid_crystal": state.User.Coin, "level": state.User.Level, "arthur_rank": state.User.ArthurRank, "ap": state.User.AP,
			"bp": state.User.BP, "bp_max": state.User.BPMax,
		},
	}
	if err := admin.accounts.persistStateWithAudit(userID, state, &cnAdminAudit{
		Operation: "account-grant", Target: strconv.Itoa(userID), Payload: auditPayload, Receipt: receipt,
	}); err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	admin.business.invalidate(userID)
	writeCNAdminJSON(writer, http.StatusOK, response)
}

func applyCNAdminTargetArthurRank(state *release.State, target int) error {
	if target == 0 {
		return nil
	}
	if target < state.User.ArthurRank {
		return errors.New("admin target Arthur rank cannot lower an account")
	}
	if target > cnAdminMaximumArthurRank {
		return errors.New("admin target Arthur rank is outside the CN client deck-rank range")
	}
	state.User.ArthurRank = target
	return nil
}

func applyCNAdminTargetLevel(state *release.State, policy release.PlayerProgressionPolicy, target int) error {
	if target == 0 {
		return nil
	}
	if target < state.User.Level {
		return errors.New("admin target level cannot lower an account")
	}
	if target > policy.MaxLevel || policy.ConfigVersion <= 0 {
		return errors.New("admin target level is outside the active progression policy")
	}
	if target == state.User.Level {
		return nil
	}
	state.User.Level = target
	state.User.Experience = policy.CumulativeExperience(target)
	state.User.NowLevelExperience = 0
	state.User.NextLevelExperience = 0
	if target < policy.MaxLevel {
		state.User.NextLevelExperience = policy.ExperienceRequired(target)
	}
	state.User.BPMax = policy.BattlePointMaximum(target)
	state.User.FriendMax = policy.FriendMaximum(target)
	state.User.Jobs = policy.JobsAtLevel(target)
	if policy.LevelUpRecovery.AP {
		state.User.AP = state.User.APMax
		state.Explore.APNextRecoveryUnix = 0
	}
	if policy.LevelUpRecovery.BP {
		state.User.BP = state.User.BPMax
		state.BattlePoint.NextRecoveryUnix = 0
	} else if state.User.BP > state.User.BPMax {
		state.User.BP = state.User.BPMax
	}
	return validateCNPlayerProgressionState(state.User, policy)
}

func (admin *cnAdmin) bossGroups(writer http.ResponseWriter, request *http.Request) {
	_, groups, _, err := admin.bossCatalog(request)
	if err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "groups": groups})
}

func (admin *cnAdmin) publicationState() (cnAdminPublicationState, error) {
	doc, err := admin.operations.readDocument(cnTeamBattlePublicationKey)
	if err != nil {
		return cnAdminPublicationState{}, err
	}
	return cnAdminPublicationFromDocument(doc)
}

func cnAdminPublicationFromDocument(doc cnAdminDocument) (cnAdminPublicationState, error) {
	publication := cnTeamBattlePublication{Mode: "all", GroupIDs: []int{}}
	if doc.Revision != 0 {
		if err := json.Unmarshal(doc.Payload, &publication); err != nil {
			return cnAdminPublicationState{}, err
		}
	}
	if publication.GroupIDs == nil {
		publication.GroupIDs = []int{}
	}
	return cnAdminPublicationState{
		Mode: publication.Mode, GroupIDs: publication.GroupIDs, StartUnix: publication.StartUnix, EndUnix: publication.EndUnix,
		Revision: doc.Revision, UpdatedUTC: doc.UpdatedUTC,
	}, nil
}

func (admin *cnAdmin) bossPolicy(writer http.ResponseWriter, request *http.Request) {
	key, _, _, err := admin.bossCatalog(request)
	if err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	doc, err := admin.operations.readDocument(key)
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	policy, err := cnAdminPublicationFromDocument(doc)
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "publication": policy})
}

func (admin *cnAdmin) setBossPolicy(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNAdminMutation(request); err != nil {
		writeCNAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	key, _, known, err := admin.bossCatalog(request)
	if err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	var publication cnTeamBattlePublication
	if err := decodeCNAdminJSON(request, &publication); err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if publication.Mode == "allowlist" {
		for _, groupID := range publication.GroupIDs {
			if _, exists := known[groupID]; !exists {
				writeCNAdminError(writer, http.StatusBadRequest, fmt.Sprintf("unknown group ID %d", groupID))
				return
			}
		}
	}
	doc, err := admin.operations.setBattlePublication(key, publication)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errCNAdminConflict) {
			status = http.StatusConflict
		}
		writeCNAdminError(writer, status, err.Error())
		return
	}
	policy, err := cnAdminPublicationFromDocument(doc)
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "publication": policy})
}

func (admin *cnAdmin) gachaPresetList(writer http.ResponseWriter, _ *http.Request) {
	admin.operations.configMu.RLock()
	defer admin.operations.configMu.RUnlock()
	presets := append([]cnAdminGachaPreset(nil), admin.gachaPresets...)
	for i := range presets {
		for _, config := range admin.operations.gachaConfigurations {
			if len(presets[i].GachaIDs) > 0 && presets[i].GachaIDs[0] == config.Profile.GachaID {
				presets[i].Name = config.Profile.Name
				presets[i].Price = config.Profile.Price
				presets[i].CardCount = len(config.Profile.CardIDs) + len(config.Profile.RewardPool)
			}
		}
	}

	writeCNAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "presets": presets,
	})
}

func (admin *cnAdmin) gachaAsset(writer http.ResponseWriter, request *http.Request) {
	fileName := chi.URLParam(request, "file")
	if filepath.Base(fileName) != fileName || filepath.Ext(fileName) != ".png" {
		writeCNAdminError(writer, http.StatusNotFound, "gacha asset not found")
		return
	}
	key := strings.TrimSuffix(fileName, ".png")
	assetPath, exists := admin.gachaBannerPaths[key]
	if !exists {
		writeCNAdminError(writer, http.StatusNotFound, "gacha asset not found")
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "no-store")
	http.ServeFile(writer, request, assetPath)
}

func (admin *cnAdmin) gachaPublicationState() (cnAdminGachaPublicationState, error) {
	doc, err := admin.operations.readDocument(cnGachaPublicationKey)
	if err != nil {
		return cnAdminGachaPublicationState{}, err
	}
	return admin.gachaPublicationFromDocument(doc)
}

func (admin *cnAdmin) gachaPublicationFromDocument(doc cnAdminDocument) (cnAdminGachaPublicationState, error) {
	active, err := admin.operations.gachaPublicationFromDocument(doc)
	if err != nil {
		return cnAdminGachaPublicationState{}, err
	}
	groupIDs := make([]int, 0, len(active))
	for groupID := range active {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Ints(groupIDs)
	return cnAdminGachaPublicationState{GroupIDs: groupIDs, Revision: doc.Revision, UpdatedUTC: doc.UpdatedUTC}, nil
}

func (admin *cnAdmin) gachaPolicy(writer http.ResponseWriter, _ *http.Request) {
	policy, err := admin.gachaPublicationState()
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "publication": policy})
}

func (admin *cnAdmin) setGachaPolicy(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNAdminMutation(request); err != nil {
		writeCNAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	var publication cnGachaPublication
	if err := decodeCNAdminJSON(request, &publication); err != nil {
		writeCNAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	for _, groupID := range publication.GroupIDs {
		if _, exists := admin.knownGachaGroups[groupID]; !exists {
			writeCNAdminError(writer, http.StatusBadRequest, fmt.Sprintf("unknown gacha group ID %d", groupID))
			return
		}
	}
	doc, err := admin.operations.setGachaPublication(publication)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errCNAdminConflict) {
			status = http.StatusConflict
		}
		writeCNAdminError(writer, status, err.Error())
		return
	}
	policy, err := admin.gachaPublicationFromDocument(doc)
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "publication": policy})
}

func (admin *cnAdmin) audit(writer http.ResponseWriter, request *http.Request) {
	limit, offset, err := cnAdminPage(request, 50, 200)
	if err != nil {
		writeCNAdminError(writer, 400, err.Error())
		return
	}
	q := strings.TrimSpace(request.URL.Query().Get("q"))
	op := strings.TrimSpace(request.URL.Query().Get("operation"))
	database, err := admin.accounts.storage.open()
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	defer database.Close()
	where := ` WHERE (?='' OR operation=?) AND (?='' OR instr(lower(target||' '||operation||' '||CAST(payload_json AS TEXT)),lower(?))>0)`
	var total int
	if err := database.QueryRow(`SELECT count(*) FROM cn_admin_audit`+where, op, op, q, q).Scan(&total); err != nil {
		writeCNAdminError(writer, 500, err.Error())
		return
	}
	rows, err := database.Query(
		`SELECT audit_id, created_utc, operation, target, payload_json, payload_sha256
		 FROM cn_admin_audit`+where+` ORDER BY audit_id DESC LIMIT ? OFFSET ?`, op, op, q, q, limit, offset,
	)
	if err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type record struct {
		AuditID    int             `json:"audit_id"`
		CreatedUTC string          `json:"created_utc"`
		Operation  string          `json:"operation"`
		Target     string          `json:"target"`
		Payload    json.RawMessage `json:"payload"`
		SHA256     string          `json:"sha256"`
	}
	var records []record
	for rows.Next() {
		var item record
		var content []byte
		if err := rows.Scan(&item.AuditID, &item.CreatedUTC, &item.Operation, &item.Target, &content, &item.SHA256); err != nil {
			writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != item.SHA256 {
			writeCNAdminError(writer, http.StatusInternalServerError, "admin audit digest mismatch")
			return
		}
		item.Payload = append(json.RawMessage(nil), content...)
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		writeCNAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []record{}
	}
	writeCNAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "records": records, "total": total, "limit": limit, "offset": offset})
}
