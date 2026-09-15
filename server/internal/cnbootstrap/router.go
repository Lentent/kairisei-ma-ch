package cnbootstrap

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

const maxCapturedBody = 1024 * 1024

// Original HOME2 artwork for the local 11801010 permanent event replay.
const cn602HomeEventBannerFile = "https___ma43_gdl_netease_com_web_netease_HOMEBANNER_20180409_banner_event_wang_home1_png"

const (
	cn602CatalogVersion = 790
	// The official catalog version remains 790.  Use a distinct local version
	// namespace to invalidate the client's cached cpk_file.csv after the CPK
	// registry or voice inventory changes; changing the catalog version would violate
	// the official version.dat contract.
	cn602VersionNamespace = "cpk-csv6-full7-umaru2"
	// Keep the previous cache namespace readable for a client which received a
	// login response immediately before a local server restart.  New logins use
	// cn602VersionNamespace and therefore invalidate the old catalog/CPK cache.
	cn602MenuBundle   = "main_c/scenes/scene_Menu.dat"
	cn602LocalSession = "local-cn-session-key"
	// CN 6.0.2 MODULE_SWITCH values confirmed from the client enum,
	// MenuItemsSwitch.Awake and DeckSl.openInitialize: EVERYDAY_TASK=2,
	// ACTIVITY=3, RECOMMAND_DECK=4, PVP_FUNCTION=28,
	// NEW_CARD_TO_SPIRIT=31, AVATAR_SHOP=34, CHANGE_MODEL=35,
	// COPY_DONOT_RETURN_HOME=37 (original result return to dungeon selection).
	// Only locally bounded Home functions are advertised as open.
	cn602LocalModuleSwitchState = int64(1<<2 | 1<<3 | 1<<4 | 1<<22 | 1<<28 | 1<<29 | 1<<31 | 1<<34 | 1<<35 | 1<<37)
)

var cn602LegacyVersionNamespaces = [...]string{"cpk-csv6-full1", "cpk-csv6-full2", "cpk-csv6-full3", "cpk-csv6-full4", "cpk-csv6-full5-eel1", "cpk-csv6-full6-cnunion1"}

var cn602ScrambleKey = [...]byte{0x01, 0xcd, 0x45, 0x89, 0x67, 0xab, 0x23, 0xef}

var cn602IntroPlaceholderPNG = func() []byte {
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		panic("decode embedded CN intro placeholder: " + err.Error())
	}
	return content
}()

// Local titles describe the configured pools; archived event artwork retains
// its source filename so it cannot silently serve an unrelated generic pool.
var cn602GachaBannerFiles = map[string]string{
	"historical_duozi":   "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_duozi_png",
	"historical_tianke":  "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_tianke_png",
	"historical_youmo":   "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_youmo_png",
	"historical_youmo10": "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_youmo10_png",

	"local_new_year":        "local_new_year.png",
	"local_standard":        "local_standard.png",
	"local_friend":          "local_friend.png",
	"local_first":           "local_first.png",
	"local_first_multi":     "local_first_multi.png",
	"element_fire":          "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdfire_png",
	"element_ice":           "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdice_png",
	"element_wind":          "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdwindy_png",
	"element_light":         "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdlight_png",
	"element_dark":          "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuixing_xddark_png",
	"rare_ticket_201805":    "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_jixiyou58_png",
	"unowned_ticket_201805": "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_weirushou58_png",
	"lucky_bag_opera":       "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaigeju_png",
	"lucky_bag_skuld":       "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaimo_png",
	"lucky_bag_constantine": "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaidai_png",
	"lucky_bag_merchant":    "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaifu_png",
	"lucky_bag_summer_duo":  "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaiyan_png",
	"lucky_bag_yalin":       "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaina_png",
}

func resolveCN602GachaBannerPaths(defaultPath string, fiveStarPath string) (map[string]string, error) {
	result := map[string]string{"five_star_ticket": fiveStarPath}
	root := filepath.Dir(defaultPath)
	for key, fileName := range cn602GachaBannerFiles {
		candidate := filepath.Join(root, fileName)
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve CN gacha banner %s: %w", key, err)
		}
		if filepath.Dir(absolute) != root {
			return nil, fmt.Errorf("CN gacha banner %s escapes the banner root", key)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, fmt.Errorf("stat CN gacha banner %s: %w", key, err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4*1024*1024 {
			return nil, fmt.Errorf("CN gacha banner %s is not a valid bounded file", key)
		}
		result[key] = absolute
	}
	return result, nil
}

type capture struct {
	Timestamp string              `json:"timestamp"`
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     string              `json:"query,omitempty"`
	Remote    string              `json:"remote"`
	Headers   map[string][]string `json:"headers"`
	BodyBytes int                 `json:"body_bytes"`
	BodySHA   string              `json:"body_sha256"`
	Body      string              `json:"body"`
}

type recorder struct {
	path string
	mu   sync.Mutex
}

func New(requestLogPath string, savePath string, saveSeedPath string, assetMapPath string, cardMasterPath string, exploreMasterPath string, storyMasterPath string, battleMasterPath string, naviMasterPath string, itemMasterPath string, avatarMasterPath string, gachaBannerPath string, fiveStarGachaBannerPath string, homeBannerPath string, stampMasterPath string, honorMasterPath string, playerProgressionPath string, loginBonusPath string, advertiseHost string, port int, cpkRoot string, cpkAliasPath string, patchRoots []string, logger *slog.Logger) (http.Handler, error) {
	return NewWithMultiplayer(
		requestLogPath, savePath, saveSeedPath, assetMapPath, cardMasterPath,
		exploreMasterPath, storyMasterPath, battleMasterPath, naviMasterPath,
		itemMasterPath, avatarMasterPath, gachaBannerPath, fiveStarGachaBannerPath, homeBannerPath, stampMasterPath, honorMasterPath, playerProgressionPath, loginBonusPath,
		advertiseHost, port,
		multiplayer.Endpoint{Host: advertiseHost, Port: uint16(port + 1)},
		multiplayer.NewHub(),
		cpkRoot, cpkAliasPath, patchRoots, logger,
	)
}

func NewWithMultiplayer(requestLogPath string, savePath string, saveSeedPath string, assetMapPath string, cardMasterPath string, exploreMasterPath string, storyMasterPath string, battleMasterPath string, naviMasterPath string, itemMasterPath string, avatarMasterPath string, gachaBannerPath string, fiveStarGachaBannerPath string, homeBannerPath string, stampMasterPath string, honorMasterPath string, playerProgressionPath string, loginBonusPath string, advertiseHost string, port int, battleSV multiplayer.Endpoint, multiplayerHub *multiplayer.Hub, cpkRoot string, cpkAliasPath string, patchRoots []string, logger *slog.Logger) (http.Handler, error) {
	return NewWithMultiplayerAndPVP(
		requestLogPath, savePath, saveSeedPath, assetMapPath, cardMasterPath,
		exploreMasterPath, storyMasterPath, battleMasterPath, naviMasterPath,
		itemMasterPath, avatarMasterPath, gachaBannerPath, fiveStarGachaBannerPath, homeBannerPath, stampMasterPath, honorMasterPath, "", playerProgressionPath, loginBonusPath,
		advertiseHost, port, battleSV, multiplayerHub, cpkRoot, cpkAliasPath,
		patchRoots, CDNConfig{}, logger,
	)
}

func NewWithMultiplayerAndPVP(requestLogPath string, savePath string, saveSeedPath string, assetMapPath string, cardMasterPath string, exploreMasterPath string, storyMasterPath string, battleMasterPath string, naviMasterPath string, itemMasterPath string, avatarMasterPath string, gachaBannerPath string, fiveStarGachaBannerPath string, homeBannerPath string, stampMasterPath string, honorMasterPath string, pvpMasterPath string, playerProgressionPath string, loginBonusPath string, advertiseHost string, port int, battleSV multiplayer.Endpoint, multiplayerHub *multiplayer.Hub, cpkRoot string, cpkAliasPath string, patchRoots []string, cdn CDNConfig, logger *slog.Logger) (http.Handler, error) {
	var cdnErr error
	cdn, cdnErr = cdn.normalized()
	if cdnErr != nil {
		return nil, cdnErr
	}
	if multiplayerHub == nil || battleSV.Host == "" || battleSV.Port == 0 {
		return nil, errors.New("local multiplayer runtime is required")
	}
	if requestLogPath == "" {
		return nil, errors.New("request log path is required")
	}
	if advertiseHost == "" {
		return nil, errors.New("advertise host is required")
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("port must be 1 through 65535")
	}
	absoluteGachaBanner, err := filepath.Abs(gachaBannerPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN gacha banner: %w", err)
	}
	gachaBannerInfo, err := os.Stat(absoluteGachaBanner)
	if err != nil {
		return nil, fmt.Errorf("stat CN gacha banner: %w", err)
	}
	if !gachaBannerInfo.Mode().IsRegular() || gachaBannerInfo.Size() <= 0 || gachaBannerInfo.Size() > 4*1024*1024 {
		return nil, errors.New("CN gacha banner must be a non-empty regular file within four MiB")
	}
	absoluteFiveStarGachaBanner, err := filepath.Abs(fiveStarGachaBannerPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN five-star gacha banner: %w", err)
	}
	fiveStarGachaBannerInfo, err := os.Stat(absoluteFiveStarGachaBanner)
	if err != nil {
		return nil, fmt.Errorf("stat CN five-star gacha banner: %w", err)
	}
	if !fiveStarGachaBannerInfo.Mode().IsRegular() || fiveStarGachaBannerInfo.Size() <= 0 || fiveStarGachaBannerInfo.Size() > 4*1024*1024 {
		return nil, errors.New("CN five-star gacha banner must be a non-empty regular file within four MiB")
	}
	gachaBannerPaths, err := resolveCN602GachaBannerPaths(
		absoluteGachaBanner, absoluteFiveStarGachaBanner,
	)
	if err != nil {
		return nil, err
	}
	absoluteHomeBanner, err := filepath.Abs(homeBannerPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN Home banner: %w", err)
	}
	homeBannerInfo, err := os.Stat(absoluteHomeBanner)
	if err != nil {
		return nil, fmt.Errorf("stat CN Home banner: %w", err)
	}
	if !homeBannerInfo.Mode().IsRegular() || homeBannerInfo.Size() <= 0 || homeBannerInfo.Size() > 4*1024*1024 {
		return nil, errors.New("CN Home banner must be a non-empty regular file within four MiB")
	}
	homeEventBanner := filepath.Join(filepath.Dir(absoluteHomeBanner), cn602HomeEventBannerFile)
	homeEventBannerInfo, err := os.Stat(homeEventBanner)
	if err != nil {
		return nil, fmt.Errorf("stat CN Home event banner: %w", err)
	}
	if !homeEventBannerInfo.Mode().IsRegular() || homeEventBannerInfo.Size() <= 0 || homeEventBannerInfo.Size() > 4*1024*1024 {
		return nil, errors.New("CN Home event banner must be a non-empty regular file within four MiB")
	}
	cpkAliases, err := loadCPKAliases(cpkRoot, cpkAliasPath)
	if err != nil {
		return nil, err
	}
	cpkDelivery, err := loadCNCPKDelivery(cpkRoot, cpkAliases)
	if err != nil {
		return nil, err
	}
	cpkFileList := buildPreloadedCPKCSV(cpkDelivery)
	cpkPatchState := buildCPKPatchState(cpkDelivery)
	resolvedPatchRoots, err := resolveCNPatchRoots(patchRoots)
	if err != nil {
		return nil, err
	}
	catalogContent, err := buildCN602Catalog(resolvedPatchRoots, assetMapPath)
	if err != nil {
		return nil, err
	}
	patchDelivery, err := cnPatchDeliveryFromCatalog(catalogContent)
	if err != nil {
		return nil, err
	}
	availableBundles, err := loadCNAssetMapBundleSet(assetMapPath)
	if err != nil {
		return nil, err
	}
	saveDatabase, err := newCNSaveDatabase(savePath, saveSeedPath, logger)
	if err != nil {
		return nil, err
	}
	var pvpConfig httpapi.PVPConfig
	if pvpMasterPath != "" {
		pvpConfig, err = loadCNPVPRuntimeMaster(pvpMasterPath)
		if err != nil {
			return nil, err
		}
	}
	playerProgression, err := loadCNPlayerProgressionRuntimeMaster(playerProgressionPath)
	if err != nil {
		return nil, err
	}
	loginBonusPolicy, err := loadCNLoginBonusRuntimeMaster(loginBonusPath)
	if err != nil {
		return nil, err
	}
	catalog, err := loadCNSaveState(saveSeedPath)
	if err != nil {
		return nil, err
	}
	prepareRuntimeState, err := loadCNRuntimeStatePreparer(cardMasterPath, exploreMasterPath, storyMasterPath, battleMasterPath, naviMasterPath, itemMasterPath, avatarMasterPath, stampMasterPath, honorMasterPath, pvpConfig, playerProgression, loginBonusPolicy, availableBundles)
	if err != nil {
		return nil, err
	}
	catalog, err = prepareRuntimeState(catalog)
	if err != nil {
		return nil, err
	}
	saveDatabase.catalog = &catalog
	primaryState, err := saveDatabase.loadOrImport()
	if err != nil {
		return nil, err
	}
	accountStore, err := newCNAccountStore(saveDatabase)
	if err != nil {
		return nil, err
	}
	if err := multiplayerHub.AttachCompletionRepository(accountStore); err != nil {
		return nil, err
	}
	operationStore, err := newCNOperationStore(saveDatabase, primaryState.Gachas)
	if err != nil {
		return nil, err
	}
	if err := operationStore.initializeContent(catalog, battleMasterPath); err != nil {
		return nil, err
	}
	if err := operationStore.initializePlayerPolicy(catalog, naviMasterPath); err != nil {
		return nil, err
	}
	basePreparer := prepareRuntimeState
	prepareRuntimeState = func(state release.State) (release.State, error) {
		prepared, err := basePreparer(state)
		if err != nil {
			return prepared, err
		}
		return httpapi.ApplyContentState(prepared, operationStore.contentConfiguration()), nil
	}
	buildBusinessHandler := func(userID int, state release.State) (http.Handler, error) {
		return newCNBusinessHandler(
			advertiseHost, port, state,
			func(updated release.State) error {
				return accountStore.persistState(userID, updated)
			},
			prepareRuntimeState, pvpConfig, accountStore, accountStore, battleSV, multiplayerHub, logger,
		)
	}
	cacheLimit, err := configuredAccountCacheLimit()
	if err != nil {
		return nil, err
	}
	primaryBusinessHandler, err := buildBusinessHandler(cnPrimaryUserID, primaryState)
	if err != nil {
		return nil, err
	}
	businessHandler := newCNAccountBusinessRouter(
		primaryBusinessHandler,
		func(userID int) (http.Handler, error) {
			state, err := accountStore.loadState(userID)
			if err != nil {
				logger.Error("load CN local account snapshot", "user_id", userID, "error", err)
				return nil, err
			}
			handler, err := buildBusinessHandler(userID, state)
			if err != nil {
				logger.Error("build CN local account handler", "user_id", userID, "error", err)
				return nil, err
			}
			return handler, nil
		},
	)
	businessHandler.idleLimit = cacheLimit
	businessHandler.prepare = operationStore.prepareBusiness
	if err := multiplayerHub.AttachStartAuthorizer(businessHandler.chargeMultiplayerStart); err != nil {
		return nil, err
	}
	if err := multiplayerHub.AttachContinueAuthorizer(businessHandler.chargeMultiplayerContinue); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(requestLogPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("request log path is a directory")
	}
	r := &recorder{path: absolute}
	router := chi.NewRouter()
	router.Use(cnNetworkCompression(logger))
	router.Use(r.middleware(logger))
	router.Use(normalizeLeadingSlashes)
	router.Use(authenticateCNSessions(accountStore))
	router.Get("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"state":          "PASS",
			"client_profile": "cn602-bootstrap",
			"battle_rooms":   multiplayerHub.RoomCount(),
			"battle_port":    battleSV.Port,
		})
	})
	for _, name := range []string{"default", "apple-review", "qa"} {
		router.Get("/local/server/"+name+".list", cnBootstrapServerList(advertiseHost, port))
	}
	router.Get("/local/gacha/banner.png", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeFile(writer, request, absoluteGachaBanner)
	})
	router.Get("/local/gacha/five-star-banner.png", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeFile(writer, request, absoluteFiveStarGachaBanner)
	})
	for key, bannerPath := range gachaBannerPaths {
		if key == "five_star_ticket" {
			continue
		}
		key := key
		bannerPath := bannerPath
		router.Get("/local/gacha/"+key+".png", func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "image/png")
			writer.Header().Set("Cache-Control", "no-store")
			http.ServeFile(writer, request, bannerPath)
		})
	}
	router.Get("/local/home/banner.png", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeFile(writer, request, absoluteHomeBanner)
	})
	router.Get("/local/home/event-banner.png", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeFile(writer, request, homeEventBanner)
	})
	emptyJSON := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write([]byte("{}"))
	}
	emptyBody := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
	}
	router.Get("/xxxx/g0/apk.config", emptyJSON)
	router.Get("/xxxx/g0/dex.config", emptyJSON)
	router.Get("/xxxx/feature/*", emptyBody)
	router.Get("/xxxx/html/latest_default.json", emptyBody)
	router.Post("/disabled/envsdk/optsdk/*", emptyJSON)
	// Fixed-width DEX redirects for longer RFC1918 hosts omit the emulator-only
	// prefixes above. Keep both representations at the HTTP boundary.
	router.Get("/g0/apk.config", emptyJSON)
	router.Get("/g0/dex.config", emptyJSON)
	router.Get("/feature/*", emptyBody)
	router.Get("/html/latest_default.json", emptyBody)
	router.Post("/d/*", emptyJSON)
	router.Post("/log.php", acknowledgeClientLog)
	router.Post("/mods_switch.php", cnBootstrapModuleSwitches)
	router.Post("/loginSDK.php", cnBootstrapLogin(accountStore, advertiseHost, port, cdn))
	accountGateway := newCNAccountGateway(accountStore)
	for _, action := range []string{"status", "bind", "login"} {
		router.Post("/local/account/"+action, accountGateway.ServeHTTP)
	}
	router.Post("/subcribe_push.php", cnBootstrapPushRegistration)
	router.Post("/Ping", cnBootstrapPing)
	router.Post("/Connect", cnBootstrapConnect(businessHandler))
	router.Post("/HomeShow", cnBootstrapSessionOnlyBusiness(businessHandler, "HomeShow", http.MethodPost, "/HomeShow"))
	router.Post("/PopupExec", cnBootstrapExactBusiness(businessHandler, "PopupExec", "/PopupExec", "popupid", "is_system"))
	router.Post("/TutorialProgress", cnBootstrapTutorialProgress)
	router.Post("/TutorialFlag", cnBootstrapTutorialFlag(businessHandler))
	router.Post("/ClickLog", cnBootstrapClickLog)
	router.Post("/ClickNewVersionInfo", cnBootstrapClickNewVersionInfo)
	router.Post("/UserCreate", cnBootstrapExactBusiness(businessHandler, "UserCreate", "/UserCreate", "name", "arthur_type"))
	router.Post("/UserSetName", cnBootstrapExactBusiness(businessHandler, "UserSetName", "/UserSetName", "name"))
	router.Post("/UserSetComment", cnBootstrapExactBusiness(businessHandler, "UserSetComment", "/UserSetComment", "comment"))
	router.Post("/GetMobileServiceToken", cnBootstrapExactBusiness(businessHandler, "GetMobileServiceToken", "/__domain/MobileServiceInfo", "sprite"))
	router.Post("/ItemShow", cnBootstrapItemShow(businessHandler))
	router.Post("/ItemUse", cnBootstrapExactBusiness(businessHandler, "ItemUse", "/ItemUse", "itemid"))
	router.Post("/ItemExchange", cnPayloadAdapter(businessHandler, "ItemExchange", "/ItemExchange", []string{"itemid", "change_sets"}, adaptCNNamedRewardMethod))
	router.Post("/ItemLackTips", cnBootstrapExactBusiness(businessHandler, "ItemLackTips", "/ItemLackTips", "idx"))
	router.Post("/ItemShopShow", cnBootstrapSessionOnlyBusiness(businessHandler, "ItemShopShow", http.MethodPost, "/ItemShopShow"))
	router.Post("/ItemShopBuy", cnBootstrapExactBusiness(businessHandler, "ItemShopBuy", "/ItemShopBuy", "item_shop_lineupid", "buy_num", "popupid"))
	router.Post("/EventShopShow", cnBootstrapExactBusiness(businessHandler, "EventShopShow", "/EventShopShow", "eventid"))
	router.Post("/EventShopBuy", cnBootstrapExactBusiness(businessHandler, "EventShopBuy", "/EventShopBuy", "event_shop_lineupid"))
	router.Post("/GachaShow", cnBootstrapGachaShow(businessHandler, operationStore))
	router.Post("/GachaPlay2", cnBootstrapGachaPlay(businessHandler, operationStore))
	router.Post("/GachaItemPlay", cnPayloadAdapter(businessHandler, "GachaItemPlay", "/GachaItemPlay", []string{"itemid", "play_count", "gacha_hash"}, adaptCNGachaItemPlayMethod))
	router.Post("/GachaLineupShow2", cnBootstrapGachaLineupShow(businessHandler, operationStore))
	gachaPublicationBusiness := cnGachaPublicationBusiness(businessHandler, operationStore)
	router.Post("/GachaSelectLineupShow", cnBootstrapExactBusiness(gachaPublicationBusiness, "GachaSelectLineupShow", "/GachaSelectLineupShow", "gachaid"))
	router.Post("/GachaSelectedListShow", cnBootstrapExactBusiness(gachaPublicationBusiness, "GachaSelectedListShow", "/GachaSelectedListShow", "gachaid"))
	router.Post("/GachaOddsShow", cnBootstrapGachaOddsShow(businessHandler, operationStore))
	router.Post("/GetRecommendCardInfo", cnBootstrapSessionOnlyBusiness(businessHandler, "GetRecommendCardInfo", http.MethodPost, "/GetRecommendCardInfo"))
	router.Post("/GetURCardNoGetFromCurrentGaCha", cnBootstrapSessionOnlyBusiness(gachaPublicationBusiness, "GetURCardNoGetFromCurrentGaCha", http.MethodPost, "/GetURCardNoGetFromCurrentGaCha"))
	router.Post("/CoinUse", cnBootstrapExactBusiness(businessHandler, "CoinUse", "/CoinUse", "type", "param"))
	router.Post("/TradeShopShow2", cnBootstrapSessionOnlyBusiness(businessHandler, "TradeShopShow2", http.MethodPost, "/TradeShopShow2"))
	router.Post("/TradeShopLineupShow", cnBootstrapExactBusiness(businessHandler, "TradeShopLineupShow", "/TradeShopLineupShow", "trade_shopid"))
	router.Post("/TradeShopBuy2", cnBootstrapTradeShopBuy2(businessHandler))
	router.Post("/PurchaseBonusActivityShow", cnBootstrapSessionOnlyBusiness(businessHandler, "PurchaseBonusActivityShow", http.MethodPost, "/PurchaseBonusActivityShow"))
	router.Post("/QueryOrder", cnBootstrapExactBusiness(businessHandler, "QueryOrder", "/QueryOrder", "order"))
	router.Post("/QueryCardCoin", cnBootstrapSessionOnlyBusiness(businessHandler, "QueryCardCoin", http.MethodPost, "/QueryCardCoin"))
	router.Post("/GetCardCoin", cnBootstrapExactBusiness(businessHandler, "GetCardCoin", "/GetCardCoin", "cardtype"))
	router.Post("/HonorShow", cnBootstrapHonorShow(businessHandler))
	router.Post("/HonorDeckShow", cnBootstrapSessionOnlyBusiness(businessHandler, "HonorDeckShow", http.MethodPost, "/HonorDeckShow"))
	router.Post("/HonorDeckSet", cnBootstrapExactBusiness(businessHandler, "HonorDeckSet", "/HonorDeckSet", "deck_honorids"))
	router.Post("/CardCollectionShow", cnBootstrapCardCollectionShow(businessHandler))
	router.Post("/CardShow2", cnBootstrapCardShow2(businessHandler))
	router.Post("/CardContainerShow", cnBootstrapCardContainerShow(businessHandler))
	router.Post("/CardMove", cnBootstrapCardMove(businessHandler))
	router.Post("/CardContainerLock", cnBootstrapExactBusiness(businessHandler, "CardContainerLock", "/CardContainerLock", "uniqid"))
	router.Post("/CardContainerUnlock", cnBootstrapExactBusiness(businessHandler, "CardContainerUnlock", "/CardContainerUnlock", "uniqid"))
	router.Post("/CardContainerSell", cnBootstrapExactBusiness(businessHandler, "CardContainerSell", "/CardContainerSell", "uniqids"))
	router.Post("/CardLock", cnBootstrapExactBusiness(businessHandler, "CardLock", "/CardLock", "uniqid"))
	router.Post("/CardUnlock", cnBootstrapExactBusiness(businessHandler, "CardUnlock", "/CardUnlock", "uniqid"))
	router.Post("/CardLoveUp", cnBootstrapCardLoveUp(businessHandler))
	router.Post("/CardDecompose", cnBootstrapCardDecompose(businessHandler))
	router.Post("/CardFameTrainInfo", cnBootstrapSessionOnlyBusiness(businessHandler, "CardFameTrainInfo", http.MethodPost, "/CardFameTrainInfo"))
	router.Post("/CardFameStartTrain", cnBootstrapExactBusiness(businessHandler, "CardFameStartTrain", "/CardFameStartTrain", "base_uniqid", "base_fame"))
	router.Post("/CardFameCancelTrain", cnBootstrapExactBusiness(businessHandler, "CardFameCancelTrain", "/CardFameCancelTrain", "base_uniqid"))
	router.Post("/CardFameTrainFinish", cnBootstrapExactBusiness(businessHandler, "CardFameTrainFinish", "/CardFameTrainFinish", "base_uniqid"))
	router.Post("/HowToGetCardShow", cnBootstrapHowToGetCardShow(businessHandler, operationStore))
	router.Post("/CardCategoryGet", cnBootstrapCardCategoryGet(businessHandler))
	router.Post("/CardDeckSet", cnBootstrapCardDeckSet(businessHandler))
	router.Post("/SupportCardSlotUnlock", cnBootstrapExactBusiness(businessHandler, "SupportCardSlotUnlock", "/SupportCardSlotUnlock", "arthur_type"))
	router.Post("/CardFusion2", cnBootstrapCardFusion2(businessHandler))
	router.Post("/CardEvolution", cnBootstrapCardEvolution(businessHandler))
	router.Post("/CardSell", cnBootstrapCardSell(businessHandler))
	router.Post("/ExploreStart", cnBootstrapExploreStart(businessHandler))
	router.Post("/ExploreEnd", cnBootstrapExploreEnd(businessHandler))
	router.Post("/MissionShow", cnBootstrapMissionShow(businessHandler))
	router.Post("/MissionReward", cnBootstrapMissionReward(businessHandler))
	router.Post("/MissionURLOpen", cnBootstrapExactBusiness(businessHandler, "MissionURLOpen", "/MissionURLOpen", "open_url"))
	router.Post("/PresentBoxShow", cnBootstrapPresentBoxShow(businessHandler))
	router.Post("/PresentBoxRecv", cnBootstrapPresentBoxRecv(businessHandler))
	router.Post("/PresentBoxMultiRecv2", cnBootstrapPresentBoxMultiRecv(businessHandler))
	router.Post("/PresentBoxDelete", cnBootstrapExactBusiness(businessHandler, "PresentBoxDelete", "/PresentBoxDelete", "presentid"))
	router.Post("/UpdGameOption", cnBootstrapExactBusiness(businessHandler, "UpdGameOption", "/UpdGameOption", "game_option"))
	router.Post("/UpdPushOption", cnBootstrapExactBusiness(businessHandler, "UpdPushOption", "/UpdPushOption", "option"))
	router.Post("/GetNaviShow", cnBootstrapSessionOnlyBusiness(businessHandler, "GetNaviShow", http.MethodPost, "/GetNaviShow"))
	router.Post("/NaviSelect", cnBootstrapExactBusiness(businessHandler, "NaviSelect", "/NaviSelect", "navi_type"))
	router.Post("/BuyNavi", cnBootstrapExactBusiness(businessHandler, "BuyNavi", "/BuyNavi", "navi_id"))
	router.Post("/StampShow", cnBootstrapSessionOnlyBusiness(businessHandler, "StampShow", http.MethodPost, "/StampShow"))
	router.Post("/StampDeckSet", cnBootstrapExactBusiness(businessHandler, "StampDeckSet", "/StampDeckSet", "deck"))
	router.Post("/CostumeShow", cnBootstrapSessionOnlyBusiness(businessHandler, "CostumeShow", http.MethodPost, "/CostumeShow"))
	router.Post("/CostumeSet", cnBootstrapExactBusiness(businessHandler, "CostumeSet", "/CostumeSet", "arthur_type", "costumeid"))
	router.Post("/AvatarPartsShow", cnBootstrapSessionOnlyBusiness(businessHandler, "AvatarPartsShow", http.MethodPost, "/AvatarPartsShow"))
	router.Post("/AvatarPartsDeckSet", cnBootstrapExactBusiness(businessHandler, "AvatarPartsDeckSet", "/AvatarPartsDeckSet", "avatar_parts_decks"))
	router.Post("/AvatarShopShow", cnBootstrapSessionOnlyBusiness(businessHandler, "AvatarShopShow", http.MethodPost, "/AvatarShopShow"))
	router.Post("/AvatarShopBuy", cnBootstrapExactBusiness(businessHandler, "AvatarShopBuy", "/AvatarShopBuy", "avatar_shop_lineupid", "sales_index"))
	router.Post("/DeckLimitShow", cnBootstrapSessionOnlyBusiness(businessHandler, "DeckLimitShow", http.MethodPost, "/DeckLimitShow"))
	router.Post("/TeamBattleSoloShow", cnBootstrapTeamBattleSoloShow(businessHandler, operationStore))
	router.Post("/TowerQuestShow", cnBootstrapExactBusiness(businessHandler, "TowerQuestShow", "/TowerQuestShow", "towerid"))
	router.Post("/TowerRankingShow", cnBootstrapExactBusiness(businessHandler, "TowerRankingShow", "/TowerRankingShow", "towerid"))
	router.Post("/TeamBattleSoloPartnerShow", cnBootstrapExactBusiness(businessHandler, "TeamBattleSoloPartnerShow", "/TeamBattleSoloPartnerShow", "bossid"))
	router.Post("/TeamBattleSoloPartnerRentalDeck", cnBootstrapExactBusiness(businessHandler, "TeamBattleSoloPartnerRentalDeck", "/TeamBattleSoloPartnerRentalDeck", "userid"))
	router.Post("/TeamBattleRecommendDeckShow", cnBootstrapSessionOnlyBusiness(businessHandler, "TeamBattleRecommendDeckShow", http.MethodPost, "/TeamBattleRecommendDeckShow"))
	router.Post("/TeamBattlePastBossShow", cnBootstrapPastBossShow(businessHandler, operationStore))
	router.Post("/TeamBattleClearDeckShow", cnBootstrapExactBusiness(businessHandler, "TeamBattleClearDeckShow", "/TeamBattleClearDeckShow", "bossid"))
	router.Post("/TeamBattleScoreRewardLineup", cnBootstrapExactBusiness(businessHandler, "TeamBattleScoreRewardLineup", "/TeamBattleScoreRewardLineup", "bossid"))
	router.Post("/DailyClearRankShow", cnBootstrapExactBusiness(businessHandler, "DailyClearRankShow", "/DailyClearRankShow", "bossid", "is_multi"))
	router.Post("/ChallengeShow", cnBootstrapExactBusiness(businessHandler, "ChallengeShow", "/ChallengeShow", "bossid", "target_time"))
	router.Post("/TeamBattleScheduleShow", cnBootstrapExactBusiness(businessHandler, "TeamBattleScheduleShow", "/TeamBattleScheduleShow", "is_solo", "active_arthur_type"))
	router.Post("/TeamBattleScheduleUpdate", cnBootstrapExactBusiness(businessHandler, "TeamBattleScheduleUpdate", "/TeamBattleScheduleUpdate", "is_solo", "boss_groupid"))
	router.Post("/UserBuffExec", cnBootstrapExactBusiness(businessHandler, "UserBuffExec", "/UserBuffExec", "user_buff_id"))
	router.Post("/TeamBattleSoloStart", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleSoloStart",
		"/TeamBattleSoloStart",
		"bossid",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"partner_deck_selects",
		"starttime",
		"flag",
	))
	router.Post("/TeamBattleSoloContinue", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleSoloContinue",
		"/TeamBattleSoloContinue",
		"pay_type",
		"progress",
		"input_cmd",
		"enemy_dead_bit",
	))
	router.Post("/TeamBattleSoloEnd", cnBootstrapTeamBattleSoloEnd(businessHandler))
	router.Post("/TeamBattleMultiShow", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleMultiShow",
		"/TeamBattleMultiShow",
		"active_arthur_type",
	))
	router.Post("/TeamBattleMultiRoomSearch", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleMultiRoomSearch",
		"/TeamBattleMultiRoomSearch",
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
	))
	router.Post("/TeamBattleMultiRoomCreate", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleMultiRoomCreate",
		"/TeamBattleMultiRoomCreate",
		"bossid",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"room_type",
		"pass",
		"deck_rank",
		"hp",
		"fame",
		"comment",
		"flag",
		"allow_to_leave",
		"use_punished_free_piont",
	))
	router.Post("/TeamBattleAIRoomCreate", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleAIRoomCreate",
		"/TeamBattleAIRoomCreate",
		"bossid",
		"ai_id",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"use_punished_free_piont",
	))
	router.Post("/TeamBattleMultiRoomReserve", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleMultiRoomReserve",
		"/TeamBattleMultiRoomReserve",
		"roomid",
		"deck_arthur_type",
	))
	router.Post("/TeamBattleMultiRoomReserveCancel", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleMultiRoomReserveCancel",
		"/TeamBattleMultiRoomReserveCancel",
		"roomid",
		"deck_arthur_type",
	))
	router.Post("/TeamBattleMultiRoomEnter", cnBootstrapExactBusiness(
		businessHandler,
		"TeamBattleMultiRoomEnter",
		"/TeamBattleMultiRoomEnter",
		"roomid",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"use_punished_free_piont",
	))
	router.Post("/TeamBattleResult", cnBootstrapTeamBattleResult(businessHandler))
	router.Post("/StageQuestShow", cnBootstrapExactBusiness(
		businessHandler,
		"StageQuestShow",
		"/StageQuestShow",
		"areaid",
		"is_solo",
	))
	router.Post("/SphrShow", cnBootstrapSphereShow(businessHandler))
	router.Post("/SphrFusion2", cnBootstrapSphereFusion2(businessHandler))
	router.Post("/SphrEvolution", cnBootstrapSphereEvolution(businessHandler))
	router.Post("/SphrSell", cnBootstrapSphereSell(businessHandler))
	router.Post("/SphrLock", cnBootstrapSphereLock(businessHandler, false))
	router.Post("/SphrUnlock", cnBootstrapSphereLock(businessHandler, true))
	router.Post("/BuddyShow", cnBootstrapBuddyShow(businessHandler))
	router.Post("/BuddyFusion", cnBootstrapBuddyFusion(businessHandler))
	router.Post("/BuddyEvolution", cnBootstrapBuddyEvolution(businessHandler))
	router.Post("/BuddySell", cnBootstrapBuddySell(businessHandler))
	router.Post("/BuddyLock", cnBootstrapBuddyLock(businessHandler, false))
	router.Post("/BuddyUnlock", cnBootstrapBuddyLock(businessHandler, true))
	router.Post("/FollowoFollowShow", cnBootstrapFollowShow(businessHandler))
	router.Post("/FollowFollowerShow", cnBootstrapFollowerShow(businessHandler))
	router.Post("/FriendSearch", cnBootstrapFriendSearch(businessHandler))
	router.Post("/FollowAdd", cnBootstrapFollowAdd(businessHandler))
	router.Post("/FollowUnFollow", cnBootstrapFollowUnfollow(businessHandler))
	router.Post("/UserProfileShow", cnBootstrapUserProfileShow(businessHandler))
	router.Post("/StoryMainShow", cnBootstrapStoryMainShow(businessHandler))
	router.Post("/StoryMainStart", cnBootstrapStoryMainStart(businessHandler))
	router.Post("/StoryMainEnd", cnBootstrapStoryMainEnd(businessHandler))
	router.Post("/StorySubShow", cnBootstrapStorySubShow(businessHandler))
	router.Post("/StorySubStart", cnBootstrapStorySubStart(businessHandler))
	router.Post("/StorySubEnd", cnBootstrapStorySubEnd(businessHandler))
	router.Post("/StoryEventShow", cnBootstrapStoryEventShow(businessHandler))
	router.Post("/StoryEventUnlock", cnBootstrapStoryEventUnlock(businessHandler))
	router.Post("/StoryStart", cnBootstrapStoryStart(businessHandler))
	router.Post("/StoryTeamBattleStart", cnBootstrapExactBusiness(businessHandler,
		"StoryTeamBattleStart", "/StoryTeamBattleStart", "story_teambattleid"))
	router.Post("/StoryTeamBattleEnd", cnBootstrapStoryTeamBattleEnd(businessHandler))
	router.Post("/EventShow", cnBootstrapSessionOnlyBusiness(businessHandler, "EventShow", http.MethodPost, "/EventShow"))
	router.Post("/PvpShow", cnBootstrapSessionOnlyBusiness(businessHandler, "PvpShow", http.MethodPost, "/PvpShow"))
	router.Post("/PvpStart2", cnBootstrapExactBusiness(businessHandler, "PvpStart2", "/PvpStart2", "type", "select_arthur_type", "pvp_my_deck"))
	router.Post("/PvpEnd", cnBootstrapExactBusiness(businessHandler, "PvpEnd", "/PvpEnd", "btluid", "is_win", "is_retire", "input_cmd"))
	router.Get("/disabled/products", cnBootstrapProducts)
	router.Get("/disabled/web", operationStore.localNotice)
	router.Get("/disabled/web/deck-guide", cnBootstrapDeckGuide)
	router.Get("/disabled/web/information/2015/7/kechengbiao", cnBootstrapDungeonSchedule)
	introHandler, err := newCNIntroHandler(absoluteGachaBanner)
	if err != nil {
		logger.Warn("local download guide unavailable", "error", err)
		introHandler = cnBootstrapIntroPlaceholder
	}
	router.Get("/disabled/web/netease/fourplusone/20160626/Intro_{index}.png", introHandler)
	router.Get("/local/version/default/Android/patch/catalog.dat", cnBootstrapCatalog(catalogContent))
	router.Get("/local/version/default/Android/patch/*", cnBootstrapPatchFile(resolvedPatchRoots))
	router.Get("/local/version/"+cn602VersionNamespace+"/Android/patch/catalog.dat", cnBootstrapCatalog(catalogContent))
	router.Get("/local/version/"+cn602VersionNamespace+"/Android/patch/*", cnBootstrapPatchFile(resolvedPatchRoots))
	for _, namespace := range cn602LegacyVersionNamespaces {
		router.Get("/local/version/"+namespace+"/Android/patch/catalog.dat", cnBootstrapCatalog(catalogContent))
		router.Get("/local/version/"+namespace+"/Android/patch/*", cnBootstrapPatchFile(resolvedPatchRoots))
	}
	versionedPatch := cnBootstrapVersionedPatchFile(resolvedPatchRoots, patchDelivery)
	router.Get("/local/resources/patch/Android/patch/*", versionedPatch)
	router.Head("/local/resources/patch/Android/patch/*", versionedPatch)
	router.Get("/local/version/default/CPK/cpk_file.csv", cnBootstrapCPKFileList(cpkFileList))
	router.Get("/local/version/default/CPK/cpk_patch.txt", cnBootstrapCPKPatchState(cpkPatchState))
	router.Get("/local/version/"+cn602VersionNamespace+"/CPK/cpk_file.csv", cnBootstrapCPKFileList(cpkFileList))
	router.Get("/local/version/"+cn602VersionNamespace+"/CPK/cpk_patch.txt", cnBootstrapCPKPatchState(cpkPatchState))
	for _, namespace := range cn602LegacyVersionNamespaces {
		router.Get("/local/version/"+namespace+"/CPK/cpk_file.csv", cnBootstrapCPKFileList(cpkFileList))
		router.Get("/local/version/"+namespace+"/CPK/cpk_patch.txt", cnBootstrapCPKPatchState(cpkPatchState))
	}
	cpkResource := cnBootstrapCPKResource(cpkDelivery)
	router.Get("/local/resources/cpk/*", cpkResource)
	router.Head("/local/resources/cpk/*", cpkResource)
	router.NotFound(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "CN route adapter not implemented", http.StatusNotImplemented)
	})
	var adminHandler http.Handler
	if battleMasterPath != "" {
		adminHandler, err = newCNAdminHandler(
			accountStore,
			businessHandler,
			operationStore,
			battleMasterPath,
			cardMasterPath,
			itemMasterPath,
			multiplayerHub,
			advertiseHost,
			port,
			logger,
			playerProgression,
			gachaBannerPaths, assetMapPath,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize CN admin handler: %w", err)
		}
	}
	return &cnDeploymentHandler{Handler: router, admin: adminHandler}, nil
}

// loadCNRuntimeStatePreparer reads immutable master data once per server.
func loadCNRuntimeStatePreparer(cardMasterPath, exploreMasterPath, storyMasterPath, battleMasterPath, naviMasterPath, itemMasterPath, avatarMasterPath, stampMasterPath, honorMasterPath string, pvpConfig httpapi.PVPConfig, playerProgression release.PlayerProgressionPolicy, loginBonusPolicy release.LoginBonusPolicy, availableBundles map[string]struct{}) (func(release.State) (release.State, error), error) {
	cardMaster, err := loadCNCardRuntimeMaster(cardMasterPath)
	if err != nil {
		return nil, err
	}
	exploreMaster, err := loadCNExploreRuntimeMaster(exploreMasterPath)
	if err != nil {
		return nil, err
	}
	storyMaster, err := loadCNStoryRuntimeMaster(storyMasterPath)
	if err != nil {
		return nil, err
	}
	naviMaster, err := loadCNNaviRuntimeMaster(naviMasterPath)
	if err != nil {
		return nil, err
	}
	itemMaster, err := loadCNItemRuntimeMaster(itemMasterPath)
	if err != nil {
		return nil, err
	}
	avatarMaster, err := loadCNAvatarRuntimeMaster(avatarMasterPath)
	if err != nil {
		return nil, err
	}
	costumes, err := loadCNCostumeRewards(avatarMaster.ImportedCostumeRewards)
	if err != nil {
		return nil, err
	}
	stampMaster, err := loadCNStampRuntimeMaster(stampMasterPath)
	if err != nil {
		return nil, err
	}
	honorMaster, err := loadCNHonorRuntimeMaster(honorMasterPath)
	if err != nil {
		return nil, err
	}
	var battleMaster cnBattleRuntimeMaster
	var normalQuestMaster cnNormalQuestRuntimeMaster
	if battleMasterPath != "" {
		battleMaster, err = loadCNBattleRuntimeMaster(battleMasterPath)
		if err != nil {
			return nil, err
		}
		normalQuestMasterPath := filepath.Join(
			filepath.Dir(battleMasterPath),
			"cn602-normal-quest-runtime.json",
		)
		normalQuestMaster, err = loadCNNormalQuestRuntimeMaster(normalQuestMasterPath)
		if err != nil {
			return nil, err
		}
	}
	return func(runtimeState release.State) (release.State, error) {
		var err error
		runtimeState.SetCollectionRewardDefinitions(14, costumes)
		if runtimeState.User.Comment == "LOCAL OFFLINE PROFILE" {
			runtimeState.User.Comment = "请多关照！"
		}
		runtimeState.User.InviteID = cnInviteID(runtimeState.User.UserID)
		runtimeState.User.SphereMax = max(runtimeState.User.SphereMax, release.SphereCapacityDefault)
		runtimeState.User.BuddyMax = max(runtimeState.User.BuddyMax, release.BuddyCapacityDefault)
		normalizeCNLegacyStaticFriends(&runtimeState)
		_, err = applyCNPlayerProgressionRuntimeMaster(&runtimeState, playerProgression)
		if err != nil {
			return release.State{}, err
		}
		_, err = applyCNLoginBonusRuntimeMaster(&runtimeState, loginBonusPolicy)
		if err != nil {
			return release.State{}, err
		}
		if pvpConfig.ConfigVersion > 0 {
			normalizeCNPVPState(&runtimeState, pvpConfig)
		}
		normalizeCN602StampDeck(&runtimeState)
		_, err = applyCNCardRuntimeMaster(&runtimeState, cardMaster)
		if err != nil {
			return release.State{}, err
		}
		if err := applyCNExploreRuntimeMaster(&runtimeState, exploreMaster); err != nil {
			return release.State{}, err
		}
		applyCNStoryRuntimeMaster(&runtimeState, storyMaster)
		if battleMasterPath != "" {
			if err := applyCNBattleRuntimeMaster(&runtimeState, battleMaster); err != nil {
				return release.State{}, err
			}
			if err := applyCNNormalQuestRuntimeMaster(&runtimeState, normalQuestMaster); err != nil {
				return release.State{}, err
			}
		}
		if err := applyCNNaviRuntimeMaster(&runtimeState, naviMaster); err != nil {
			return release.State{}, err
		}
		_, err = applyCNItemRuntimeMaster(&runtimeState, itemMaster)
		if err != nil {
			return release.State{}, err
		}
		_, err = applyCNAvatarRuntimeMaster(&runtimeState, avatarMaster)
		if err != nil {
			return release.State{}, err
		}
		if err := applyCNAvatarShopAssetClosure(&runtimeState, availableBundles); err != nil {
			return release.State{}, err
		}
		_, err = applyCNStampRuntimeMaster(&runtimeState, stampMaster)
		if err != nil {
			return release.State{}, err
		}
		_, err = applyCNHonorRuntimeMaster(&runtimeState, honorMaster)
		if err != nil {
			return release.State{}, err
		}
		return runtimeState, nil

	}, nil
}

func newCNBusinessHandler(advertiseHost string, port int, runtimeState release.State, persistState httpapi.StatePersister, prepareRuntimeState func(release.State) (release.State, error), pvpConfig httpapi.PVPConfig, pvpAccounts httpapi.PVPAccountRepository, friendPointAccounts httpapi.FriendPointAccountRepository, battleSV multiplayer.Endpoint, multiplayerHub *multiplayer.Hub, logger *slog.Logger) (http.Handler, error) {
	stateBefore, err := fingerprintCNAccountState(runtimeState)
	if err != nil {
		return nil, fmt.Errorf("encode CN runtime state before master application: %w", err)
	}
	runtimeState, err = prepareRuntimeState(runtimeState)
	if err != nil {
		return nil, err
	}
	contentGate, err := validateCNRunnableContent(runtimeState)
	if err != nil {
		return nil, fmt.Errorf("validate CN runtime content gate: %w", err)
	}
	stateAfter, err := fingerprintCNAccountState(runtimeState)
	if err != nil {
		return nil, fmt.Errorf("encode CN runtime state after master application: %w", err)
	}
	if stateBefore != stateAfter {
		if err := persistState(runtimeState); err != nil {
			return nil, fmt.Errorf("persist validated CN runtime state migration: %w", err)
		}
	}
	logger.Info(
		"validated CN runtime content gate",
		"team_battle_groups", contentGate.TeamBattleGroups,
		"team_battle_boss_entries", contentGate.TeamBattleBossEntries,
		"user_buff_profiles", contentGate.UserBuffProfiles,
		"user_buff_references", contentGate.UserBuffReferences,
		"stage_quest_stages", contentGate.StageQuestStages,
		"explore_stages", contentGate.ExploreStages,
		"explore_floors", contentGate.ExploreFloors,
		"main_stories", contentGate.MainStories,
		"cn_main_stories", contentGate.CNMainStories,
		"sub_story_sections", contentGate.SubStorySections,
		"sub_stories", contentGate.SubStories,
		"story_events", contentGate.StoryEvents,
		"event_stories", contentGate.EventStories,
		"story_battle_ids", contentGate.StoryBattleIDs,
		"story_battle_references", contentGate.StoryBattleReferences,
		"popup_profiles", contentGate.PopupProfiles,
	)
	runtimeState.SchemaVersion = 1
	runtimeState.ReleaseID = "cn602-bootstrap"
	runtimeState.SourceBuild = "cn-official-6.0.2"
	runtimeState.CatalogVersion = cn602CatalogVersion
	runtimeState.State = "LOCAL"
	runtimeState.Routes = release.Routes{
		Login:                            "/__domain/Login",
		AuthCheck:                        "/__domain/AuthCheck",
		Health:                           "/__domain/healthz",
		Catalog:                          "/__domain/catalog.dat",
		ResourcePrefix:                   "/__domain/resources/",
		CPKIndex:                         "/__domain/cpk_file.csv",
		CPKPatch:                         "/__domain/cpk_patch.txt",
		CPKPrefix:                        "/__domain/cpk/",
		ImagePrefix:                      "/__domain/images/",
		Connect:                          "/__domain/Connect",
		HomeShow:                         "/HomeShow",
		MainQuestShow:                    "/StageQuestShow",
		TeamBattleSoloShow:               "/TeamBattleSoloShow",
		TowerQuestShow:                   "/TowerQuestShow",
		TowerRankingShow:                 "/TowerRankingShow",
		TeamBattleSoloPartnerShow:        "/TeamBattleSoloPartnerShow",
		TeamBattleSoloPartnerRentalDeck:  "/TeamBattleSoloPartnerRentalDeck",
		TeamBattleRecommendDeckShow:      "/TeamBattleRecommendDeckShow",
		TeamBattlePastBossShow:           "/TeamBattlePastBossShow",
		TeamBattleClearDeckShow:          "/TeamBattleClearDeckShow",
		TeamBattleScoreRewardLineup:      "/TeamBattleScoreRewardLineup",
		DailyClearRankShow:               "/DailyClearRankShow",
		ChallengeShow:                    "/ChallengeShow",
		TeamBattleScheduleShow:           "/TeamBattleScheduleShow",
		TeamBattleScheduleUpdate:         "/TeamBattleScheduleUpdate",
		UserBuffExec:                     "/UserBuffExec",
		TeamBattleSoloStart:              "/TeamBattleSoloStart",
		TeamBattleSoloContinue:           "/TeamBattleSoloContinue",
		TeamBattleSoloEnd:                "/TeamBattleSoloEnd",
		TeamBattleMultiShow:              "/TeamBattleMultiShow",
		TeamBattleMultiRoomSearch:        "/TeamBattleMultiRoomSearch",
		TeamBattleMultiRoomCreate:        "/TeamBattleMultiRoomCreate",
		TeamBattleAIRoomCreate:           "/TeamBattleAIRoomCreate",
		TeamBattleMultiRoomReserve:       "/TeamBattleMultiRoomReserve",
		TeamBattleMultiRoomReserveCancel: "/TeamBattleMultiRoomReserveCancel",
		TeamBattleMultiRoomEnter:         "/TeamBattleMultiRoomEnter",
		TeamBattleResult:                 "/TeamBattleResult",
		DeckLimitShow:                    "/DeckLimitShow",
		CostumeShow:                      "/CostumeShow",
		CostumeSet:                       "/CostumeSet",
		AvatarPartsShow:                  "/AvatarPartsShow",
		AvatarPartsDeckSet:               "/AvatarPartsDeckSet",
		AvatarShopShow:                   "/AvatarShopShow",
		AvatarShopBuy:                    "/AvatarShopBuy",
		HonorShow:                        "/HonorShow",
		HonorDeckShow:                    "/HonorDeckShow",
		HonorDeckSet:                     "/HonorDeckSet",
		UserCreate:                       "/UserCreate",
		UserSetName:                      "/UserSetName",
		UserSetComment:                   "/UserSetComment",
		CardCollectionShow:               "/CardCollectionShow",
		SetTutorialFlag:                  "/SetTutorialFlag",
		CardShow:                         "/CardShow2",
		CardContainerShow:                "/CardContainerShow",
		CardMove:                         "/CardMove",
		CardContainerLock:                "/CardContainerLock",
		CardContainerUnlock:              "/CardContainerUnlock",
		CardContainerSell:                "/CardContainerSell",
		CardLock:                         "/CardLock",
		CardUnlock:                       "/CardUnlock",
		CardLoveUp:                       "/CardLoveUp",
		CardDecompose:                    "/CardDecompose",
		CardFameTrainInfo:                "/CardFameTrainInfo",
		CardFameStartTrain:               "/CardFameStartTrain",
		CardFameCancelTrain:              "/CardFameCancelTrain",
		CardFameTrainFinish:              "/CardFameTrainFinish",
		HowToGetCardShow:                 "/HowToGetCardShow",
		SphereShow:                       "/SphrShow",
		SphereFusion:                     "/SphrFusion2",
		SphereEvolution:                  "/SphrEvolution",
		SphereSell:                       "/SphrSell",
		SphereLock:                       "/SphrLock",
		SphereUnlock:                     "/SphrUnlock",
		BuddyShow:                        "/BuddyShow",
		BuddyFusion:                      "/BuddyFusion",
		BuddyEvolution:                   "/BuddyEvolution",
		BuddySell:                        "/BuddySell",
		BuddyLock:                        "/BuddyLock",
		BuddyUnlock:                      "/BuddyUnlock",
		CardCategoryGet:                  "/CardCategoryGet",
		CardDeckSet:                      "/CardDeckSet",
		SupportCardSlotUnlock:            "/SupportCardSlotUnlock",
		ExploreStart:                     "/ExploreStart",
		ExploreEnd:                       "/ExploreEnd",
		CardFusion:                       "/CardFusion2",
		CardEvolution:                    "/CardEvolution",
		CardSell:                         "/CardSell",
		MissionShow:                      "/MissionShow",
		MissionReward:                    "/MissionReward",
		MissionURLOpen:                   "/MissionURLOpen",
		PresentBoxShow:                   "/PresentBoxShow",
		PresentBoxRecv:                   "/PresentBoxRecv",
		PresentBoxMultiRecv:              "/PresentBoxMultiRecv2",
		PresentBoxDelete:                 "/PresentBoxDelete",
		UpdateGameOption:                 "/UpdGameOption",
		UpdatePushOption:                 "/UpdPushOption",
		GetNaviShow:                      "/GetNaviShow",
		NaviSelect:                       "/NaviSelect",
		BuyNavi:                          "/BuyNavi",
		ItemShow:                         "/ItemShow",
		ItemUse:                          "/ItemUse",
		ItemExchange:                     "/ItemExchange",
		ItemLackTips:                     "/ItemLackTips",
		ItemShopShow:                     "/ItemShopShow",
		ItemShopBuy:                      "/ItemShopBuy",
		EventShopShow:                    "/EventShopShow",
		EventShopBuy:                     "/EventShopBuy",
		TradeShopShow:                    "/TradeShopShow2",
		TradeShopLineupShow:              "/TradeShopLineupShow",
		TradeShopBuy:                     "/TradeShopBuy2",
		LocalShopBonus:                   "/PurchaseBonusActivityShow",
		LocalShopQueryOrder:              "/QueryOrder",
		LocalShopQueryCard:               "/QueryCardCoin",
		LocalShopGetCard:                 "/GetCardCoin",
		GachaShow:                        "/GachaShow",
		GachaPlay:                        "/GachaPlay2",
		GachaItemPlay:                    "/GachaItemPlay",
		GachaLineupShow:                  "/GachaLineupShow2",
		GachaSelectLineupShow:            "/GachaSelectLineupShow",
		GachaSelectedListShow:            "/GachaSelectedListShow",
		GachaOddsShow:                    "/GachaOddsShow",
		GetRecommendCardInfo:             "/GetRecommendCardInfo",
		GetURCardNoGetFromCurrentGaCha:   "/GetURCardNoGetFromCurrentGaCha",
		CoinUse:                          "/CoinUse",
		StampShow:                        "/StampShow",
		StampDeckSet:                     "/StampDeckSet",
		FriendSearch:                     "/FriendSearch",
		FollowShow:                       "/FollowoFollowShow",
		FollowerShow:                     "/FollowFollowerShow",
		FollowAdd:                        "/FollowAdd",
		FollowUnfollow:                   "/FollowUnFollow",
		UserProfileShow:                  "/UserProfileShow",
		StoryMainShow:                    "/StoryMainShow",
		StoryMainStart:                   "/StoryMainStart",
		StoryMainEnd:                     "/StoryMainEnd",
		StorySubShow:                     "/StorySubShow",
		StorySubStart:                    "/StorySubStart",
		StorySubEnd:                      "/StorySubEnd",
		StoryEventShow:                   "/StoryEventShow",
		StoryEventUnlock:                 "/StoryEventUnlock",
		StoryStart:                       "/StoryStart",
		EventShow:                        "/EventShow",
		PopupExec:                        "/PopupExec",
		Ping:                             "/__domain/Ping",
	}
	if pvpConfig.ConfigVersion > 0 {
		runtimeState.Routes.PVPShow = "/PvpShow"
		runtimeState.Routes.PVPStart = "/PvpStart2"
		runtimeState.Routes.PVPEnd = "/PvpEnd"
	}
	runtimeRelease := &release.Release{
		Manifest: release.Manifest{ReleaseID: "cn602-bootstrap"},
		State:    runtimeState,
	}
	baseURL := "http://" + advertiseHost + ":" + strconv.Itoa(port)
	return httpapi.NewWithStatePersistenceAndMultiplayerAndPVP(runtimeRelease, baseURL, logger, func(state release.State) error {
		return persistState(state)
	}, multiplayerHub, battleSV, pvpAccounts, pvpConfig, friendPointAccounts)
}

func normalizeCN602LegacyStoryClear(state *release.State) {
	// Earlier CN bootstrap builds wrote enum ordinal 1 instead of the CLEAR bit
	// value 1<<1 for the only persisted story. Migrate that known local-save
	// shape without changing future NEW-only rows in a full story master.
	for partIndex := range state.Story.MainParts {
		for sectionIndex := range state.Story.MainParts[partIndex].Sections {
			stories := state.Story.MainParts[partIndex].Sections[sectionIndex].Stories
			for storyIndex := range stories {
				if stories[storyIndex].StoryMainID == 10001 && stories[storyIndex].StateFlag == 1 {
					stories[storyIndex].StateFlag = 1 << 1
				}
			}
		}
	}
}

func normalizeCN602StampDeck(state *release.State) bool {
	const stampDeckSlots = 12
	if len(state.Stamps.DeckStampIDs) == stampDeckSlots {
		return false
	}
	deck := make([]int, stampDeckSlots)
	copy(deck, state.Stamps.DeckStampIDs)
	state.Stamps.DeckStampIDs = deck
	return true
}

func writeCNProtocolResponse(writer http.ResponseWriter, common map[string]any, payload map[string]any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	commonJSON, err := json.Marshal(common)
	if err != nil {
		http.Error(writer, "encode common response", http.StatusInternalServerError)
		return
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		http.Error(writer, "encode protocol response", http.StatusInternalServerError)
		return
	}
	_, _ = writer.Write(append(append(commonJSON, '\n'), append(payloadJSON, '\n')...))
}

func writeCNProtocolResponseWithPopup(writer http.ResponseWriter, common map[string]any, payload map[string]any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	segments := make([][]byte, 0, 3)
	for _, value := range []any{
		common,
		payload,
		map[string]any{"popup": []any{}},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			http.Error(writer, "encode CN protocol response", http.StatusInternalServerError)
			return
		}
		segments = append(segments, encoded)
	}
	_, _ = writer.Write(bytes.Join(segments, []byte{'\n'}))
}

func cnBootstrapCommon() map[string]any {
	return map[string]any{
		"res_code":            0,
		"res_str":             "",
		"notification":        []any{},
		"revision":            cn602CatalogVersion,
		"is_appupdate":        0,
		"res_err_action":      0,
		"res_is_del_savedata": 0,
	}
}

func cnBootstrapPing(writer http.ResponseWriter, _ *http.Request) {
	writeCNProtocolResponse(writer, cnBootstrapCommon(), map[string]any{
		"stamp": 0,
	})
}

func cnBootstrapConnect(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "Connect")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if len(payload) != 0 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(payload, &fields) != nil || len(fields) != 0 {
				http.Error(writer, "invalid CN Connect payload", http.StatusBadRequest)
				return
			}
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(
				request,
				http.MethodPost,
				"/__domain/Connect",
				[]byte(cn602LocalSession),
			),
			businessHandler,
			adaptCNConnectResponse,
		)
	}
}

func adaptCNConnectResponse(content []byte) ([]byte, error) {
	segments := bytes.Split(content, []byte{'\n'})
	if len(segments) != 3 {
		return nil, errors.New("unexpected CN Connect protocol segment count")
	}
	return bytes.Join(segments[:2], []byte{'\n'}), nil
}

func cnBootstrapClickLog(writer http.ResponseWriter, _ *http.Request) {
	// Statistics are outside the local server's scope. The CN 6.0.2 client
	// nevertheless requires the ordinary three-segment protocol envelope before
	// it can continue its business state machine, so acknowledge without parsing
	// or persisting the reported click data.
	writeCNProtocolResponseWithPopup(writer, cnBootstrapCommon(), map[string]any{})
}

func cnBootstrapTutorialProgress(writer http.ResponseWriter, request *http.Request) {
	payload, err := readCNSessionPayload(request, "TutorialProgress")
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnauthorized)
		return
	}
	if err := requireCNExactFields(payload, "TutorialProgress", "stepid", "model"); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	var progress struct {
		StepID int8   `json:"stepid"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(payload, &progress); err != nil {
		http.Error(writer, "invalid CN TutorialProgress payload", http.StatusBadRequest)
		return
	}
	// The hash-locked CN client uses this request only as a KPI transport and
	// consumes no response fields. A transport failure nevertheless blocks the
	// first-install state machine, so acknowledge the exact DTO without storing
	// or interpreting the statistic. TutorialFlag remains the only persisted
	// tutorial-progress owner.
	writeCNProtocolResponseWithPopup(writer, cnBootstrapCommon(), map[string]any{})
}

func cnBootstrapClickNewVersionInfo(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNSessionOnly(request, "ClickNewVersionInfo"); err != nil {
		http.Error(writer, err.Error(), http.StatusUnauthorized)
		return
	}
	writeCNProtocolResponseWithPopup(writer, cnBootstrapCommon(), map[string]any{
		"code": 0,
	})
}

func cnBootstrapTradeShopBuy2(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TradeShopBuy2")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(payload, &raw) != nil || (len(raw) != 2 && len(raw) != 3) {
			http.Error(writer, "invalid CN TradeShopBuy2 payload", http.StatusBadRequest)
			return
		}
		if _, exists := raw["lineupid"]; !exists {
			http.Error(writer, "missing CN TradeShopBuy2 field: lineupid", http.StatusBadRequest)
			return
		}
		if _, exists := raw["num"]; !exists {
			http.Error(writer, "missing CN TradeShopBuy2 field: num", http.StatusBadRequest)
			return
		}
		if len(raw) == 3 {
			if _, exists := raw["uniqids"]; !exists {
				http.Error(writer, "invalid CN TradeShopBuy2 payload", http.StatusBadRequest)
				return
			}
		}
		var lineupID int
		var num int
		if json.Unmarshal(raw["lineupid"], &lineupID) != nil ||
			json.Unmarshal(raw["num"], &num) != nil || lineupID <= 0 || num <= 0 {
			http.Error(writer, "invalid CN TradeShopBuy2 fields", http.StatusBadRequest)
			return
		}
		if uniqidsRaw, exists := raw["uniqids"]; exists {
			var uniqids []int64
			if json.Unmarshal(uniqidsRaw, &uniqids) != nil || len(uniqids) == 0 {
				http.Error(writer, "invalid CN TradeShopBuy2 uniqids", http.StatusBadRequest)
				return
			}
			seen := make(map[int64]struct{}, len(uniqids))
			for _, uniqid := range uniqids {
				if uniqid <= 0 {
					http.Error(writer, "invalid CN TradeShopBuy2 uniqids", http.StatusBadRequest)
					return
				}
				if _, duplicate := seen[uniqid]; duplicate {
					http.Error(writer, "duplicate CN TradeShopBuy2 uniqid", http.StatusBadRequest)
					return
				}
				seen[uniqid] = struct{}{}
			}
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/TradeShopBuy2", payload),
		)
	}
}

func cnBootstrapTutorialFlag(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TutorialFlag")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 2 {
			http.Error(writer, "invalid CN TutorialFlag payload", http.StatusBadRequest)
			return
		}
		flagRaw, hasFlag := fields["flag"]
		stepRaw, hasStep := fields["step"]
		var flag int64
		var step int
		if !hasFlag || !hasStep || json.Unmarshal(flagRaw, &flag) != nil || json.Unmarshal(stepRaw, &step) != nil {
			http.Error(writer, "invalid CN TutorialFlag fields", http.StatusBadRequest)
			return
		}
		domainBody, err := json.Marshal(map[string]int64{"flag": flag})
		if err != nil {
			http.Error(writer, "encode TutorialFlag adapter", http.StatusInternalServerError)
			return
		}
		adapted := request.Clone(request.Context())
		adaptedURL := *request.URL
		adaptedURL.Path = "/SetTutorialFlag"
		adapted.URL = &adaptedURL
		adapted.Body = io.NopCloser(bytes.NewReader(domainBody))
		adapted.ContentLength = int64(len(domainBody))
		businessHandler.ServeHTTP(writer, adapted)
	}
}

func cnBootstrapItemShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "ItemShow")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 1 {
			http.Error(writer, "invalid CN ItemShow payload", http.StatusBadRequest)
			return
		}
		itemTypeRaw, ok := fields["item_type"]
		var itemType int
		if !ok || json.Unmarshal(itemTypeRaw, &itemType) != nil {
			http.Error(writer, "invalid CN ItemShow fields", http.StatusBadRequest)
			return
		}
		adapted := request.Clone(request.Context())
		adapted.Body = io.NopCloser(bytes.NewReader(payload))
		adapted.ContentLength = int64(len(payload))
		businessHandler.ServeHTTP(writer, adapted)
	}
}

func cnBootstrapPushRegistration(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write([]byte("{}"))
}

type cpkAliasManifest struct {
	SchemaVersion         int        `json:"schema_version"`
	ClientProfile         string     `json:"client_profile"`
	GeneratedUTC          string     `json:"generated_utc"`
	Evidence              string     `json:"evidence"`
	SourceInventory       string     `json:"source_inventory"`
	SourceInventorySchema int        `json:"source_inventory_schema,omitempty"`
	Aliases               []cpkAlias `json:"aliases"`
}

type cpkAlias struct {
	AliasCPKName  string `json:"alias_cpk_name"`
	SourceCPKName string `json:"source_cpk_name"`
	NaviIDs       []int  `json:"navi_ids"`
	Evidence      string `json:"evidence"`
}

type resolvedCPKAlias struct {
	AliasName  string
	SourceName string
	SourcePath string
}

func safeCPKFileName(name string) bool {
	return name != "" &&
		name == filepath.Base(name) && name == path.Base(name) &&
		strings.EqualFold(filepath.Ext(name), ".cpk") &&
		!strings.ContainsAny(name, "/\\,\r\n")
}

func loadCPKAliases(cpkRoot string, manifestPath string) ([]resolvedCPKAlias, error) {
	if manifestPath == "" {
		return nil, errors.New("generated CN CPK alias manifest is required")
	}
	absoluteRoot, err := filepath.Abs(cpkRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve official CN CPK root: %w", err)
	}
	absoluteManifest, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CN CPK alias manifest: %w", err)
	}
	content, err := os.ReadFile(absoluteManifest)
	if err != nil {
		return nil, fmt.Errorf("read CN CPK alias manifest: %w", err)
	}
	if len(content) == 0 || len(content) > 1024*1024 {
		return nil, errors.New("CN CPK alias manifest must be non-empty and at most one MiB")
	}
	var manifest cpkAliasManifest
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode CN CPK alias manifest: %w", err)
	}
	if (manifest.SchemaVersion != 1 && manifest.SchemaVersion != 2) || manifest.ClientProfile != "cn602-bootstrap" {
		return nil, errors.New("CN CPK alias manifest identity is invalid")
	}
	if manifest.SchemaVersion == 2 && manifest.SourceInventorySchema != 4 && manifest.SourceInventorySchema != 5 {
		return nil, errors.New("CN CPK alias manifest source inventory schema is invalid")
	}
	seen := make(map[string]struct{}, len(manifest.Aliases))
	resolved := make([]resolvedCPKAlias, 0, len(manifest.Aliases))
	for _, alias := range manifest.Aliases {
		if !safeCPKFileName(alias.AliasCPKName) || !safeCPKFileName(alias.SourceCPKName) {
			return nil, fmt.Errorf("unsafe CN CPK alias %q -> %q", alias.AliasCPKName, alias.SourceCPKName)
		}
		if strings.EqualFold(alias.AliasCPKName, alias.SourceCPKName) {
			return nil, fmt.Errorf("CN CPK alias %q points to itself", alias.AliasCPKName)
		}
		key := strings.ToLower(alias.AliasCPKName)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate CN CPK alias %q", alias.AliasCPKName)
		}
		seen[key] = struct{}{}
		aliasPath := filepath.Join(absoluteRoot, alias.AliasCPKName)
		if _, err := os.Stat(aliasPath); err == nil {
			return nil, fmt.Errorf("CN CPK alias %q collides with an official file", alias.AliasCPKName)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat CN CPK alias target %q: %w", alias.AliasCPKName, err)
		}
		sourcePath := filepath.Join(absoluteRoot, alias.SourceCPKName)
		info, err := os.Stat(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("stat CN CPK alias source %q: %w", alias.SourceCPKName, err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return nil, fmt.Errorf("CN CPK alias source %q is not a non-empty regular file", alias.SourceCPKName)
		}
		resolved = append(resolved, resolvedCPKAlias{
			AliasName: alias.AliasCPKName, SourceName: alias.SourceCPKName, SourcePath: sourcePath,
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		return strings.ToLower(resolved[i].AliasName) < strings.ToLower(resolved[j].AliasName)
	})
	return resolved, nil
}

func appendPreloadedCPKCSVRow(manifest *strings.Builder, name string, size int64, downloadType string) {
	cpkType, cpkID := classifyPreloadedCPK(name)
	manifest.WriteString(name)
	manifest.WriteByte(',')
	manifest.WriteString(cpkType)
	manifest.WriteByte(',')
	manifest.WriteString(strconv.Itoa(cpkID))
	manifest.WriteByte(',')
	manifest.WriteString(strconv.FormatFloat(float64(size)/(1024*1024), 'f', 6, 64))
	manifest.WriteByte(',')
	// Omitting this column shifts size_bytes into DOWNLOAD_TYPE and prevents
	// startup audio binding. ALL makes both EASY and FULL sound modes bootstrap
	// a missing file from the local server; already valid device files remain
	// reusable by the original CPK version/size checks.
	manifest.WriteString(downloadType)
	manifest.WriteByte(',')
	manifest.WriteString(strconv.FormatInt(size, 10))
	manifest.WriteByte('\n')
}

func buildPreloadedCPKCSV(delivery []cnCPKDelivery) []byte {
	var manifest strings.Builder
	manifest.WriteString("# cpk_name,type,id,file_size_mb,download_type,size_bytes\n")
	for _, file := range delivery {
		appendPreloadedCPKCSVRow(&manifest, file.Name, file.Size, "ALL")
	}
	return []byte(manifest.String())
}

func cnBootstrapCPKResource(delivery []cnCPKDelivery) http.HandlerFunc {
	files := make(map[string]cnCPKDelivery, len(delivery))
	for _, file := range delivery {
		files[strings.ToLower(file.Name)] = file
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		// The CN client appends its own CPK directory to res_cpk_url.
		name := strings.TrimPrefix(chi.URLParam(request, "*"), "CPK/")
		versionText := ""
		if suffixAt := strings.LastIndex(name, ".v"); suffixAt > 0 {
			versionText, name = name[suffixAt+2:], name[:suffixAt]
			if versionText == "" {
				http.Error(writer, "invalid CPK version", http.StatusBadRequest)
				return
			}
		}
		if !safeCPKFileName(name) {
			http.Error(writer, "invalid CPK file name", http.StatusBadRequest)
			return
		}
		file, ok := files[strings.ToLower(name)]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		if versionText != "" {
			version, err := strconv.ParseUint(versionText, 10, 31)
			if err != nil || version != file.Version {
				http.Error(writer, "invalid CPK version", http.StatusBadRequest)
				return
			}
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeFile(writer, request, file.SourcePath)
	}
}

func cnCPKVersion(name string) uint64 {
	// D-452 encrypts the same-size UMARU payload. The original client compares
	// per-file versions, so a changed download URL alone cannot invalidate v1.
	if strings.EqualFold(name, "cv_navi_5.cpk") {
		return 2
	}
	return 1
}

func classifyPreloadedCPK(name string) (string, int) {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	lower := strings.ToLower(stem)
	switch {
	case lower == "cuesheet_bgm_event":
		return "EVENT_BGM", 0
	case lower == "cuesheet_se_event":
		return "EVENT_SE", 0
	case strings.HasPrefix(lower, "cuesheet_bgm"):
		return "BGM", trailingCPKID(lower, "cuesheet_bgm")
	case strings.HasPrefix(lower, "cuesheet_se"):
		return "SE", trailingCPKID(lower, "cuesheet_se")
	case strings.HasPrefix(lower, "cuesheet_card_"):
		return "CARD_VOICE", trailingCPKID(lower, "cuesheet_card")
	case strings.HasPrefix(lower, "cuesheet_legend_"):
		return "CARD_VOICE", trailingCPKID(lower, "cuesheet_legend")
	case strings.HasPrefix(lower, "cv_arthur_"):
		return "BATTLE_VOICE", trailingCPKID(lower, "cv_arthur")
	case strings.HasPrefix(lower, "cv_tb_"):
		return "TEAMBATTLE_VOICE", trailingCPKID(lower, "cv_tb")
	case strings.HasPrefix(lower, "cv_navi_"):
		return "NAVI_VOICE", trailingCPKID(lower, "cv_navi")
	case strings.HasPrefix(lower, "cv_ep"):
		return "STORY_VOICE", trailingCPKID(lower, "cv_ep")
	case lower == "mov_prologue":
		return "MOVIE", 0
	case lower == "mov_op":
		return "MOVIE", 1
	case strings.HasPrefix(lower, "mov_"):
		return "MOVIE", trailingCPKID(lower, "mov")
	default:
		return "NONE", 0
	}
}

func trailingCPKID(stem string, prefix string) int {
	suffix := strings.TrimPrefix(stem, prefix)
	suffix = strings.TrimPrefix(suffix, "_")
	value, err := strconv.Atoi(suffix)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func cnBootstrapCPKFileList(content []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(content)
	}
}

func buildCPKPatchState(delivery []cnCPKDelivery) []byte {
	var state strings.Builder
	for _, file := range delivery {
		fmt.Fprintf(&state, "%s,%d\n", file.Name, file.Version)
	}
	return []byte(state.String())
}

func cnBootstrapCPKPatchState(content []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(content)
	}
}

func cnBootstrapProducts(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"code":         200,
		"product_list": cnLocalShopProducts(),
	})
}

func cnBootstrapIntroPlaceholder(writer http.ResponseWriter, request *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(request, "index"))
	if err != nil || index < 0 || index > 4 {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	writer.Header().Set("X-Kairisei-Source-State", "PLACEHOLDER")
	_, _ = writer.Write(cn602IntroPlaceholderPNG)
}

func cnBootstrapDungeonSchedule(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>副本日程表</title><style>
body{margin:0;background:#f2ead7;color:#392d1d;font-family:sans-serif}main{max-width:920px;margin:auto;padding:24px}
h1{margin:0 0 18px;color:#74531e;border-bottom:3px solid #c69b44;padding-bottom:12px}.status{background:#fff8df;border:2px solid #c69b44;border-radius:12px;padding:18px;line-height:1.7}
.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:18px}.card{background:#fff;border:1px solid #b8934e;border-radius:10px;padding:16px}.card h2{margin:0 0 8px;color:#8a5c10;font-size:1.15rem}.open{color:#277234;font-weight:bold}
@media(max-width:700px){.grid{grid-template-columns:1fr}}
</style></head><body><main><h1>副本日程表</h1><section class="status">
<strong>本地离线副本开放规则</strong><br>活动副本不再受原运营时间窗限制，当前均为<span class="open">全天开放</span>；常驻普通副本仍按账号通关进度依次解锁。
</section><section class="grid">
<article class="card"><h2>普通活动副本</h2><span class="open">全天开放</span></article>
<article class="card"><h2>3D 活动副本</h2><span class="open">全天开放</span></article>
<article class="card"><h2>限时素材副本</h2><span class="open">全天开放</span></article>
</section></main></body></html>`)
}

func resolveCNPatchRoots(patchRoots []string) ([]string, error) {
	if len(patchRoots) == 0 {
		return nil, errors.New("at least one official CN patch root is required")
	}
	resolved := make([]string, 0, len(patchRoots))
	seen := make(map[string]struct{}, len(patchRoots))
	var versionNamespace string
	for _, patchRoot := range patchRoots {
		if patchRoot == "" {
			return nil, errors.New("official CN patch root is required")
		}
		absolute, err := filepath.Abs(patchRoot)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate official CN patch root %q", absolute)
		}
		seen[key] = struct{}{}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, fmt.Errorf("stat official CN patch root: %w", err)
		}
		if !info.IsDir() {
			return nil, errors.New("official CN patch root is not a directory")
		}
		versionBytes, err := os.ReadFile(filepath.Join(absolute, "version.dat"))
		if err != nil {
			return nil, fmt.Errorf("read official CN version.dat: %w", err)
		}
		namespace, err := cnPatchVersionNamespace(versionBytes)
		if err != nil {
			return nil, fmt.Errorf("official CN patch root %q: %w", absolute, err)
		}
		if versionNamespace == "" {
			versionNamespace = namespace
		} else if versionNamespace != namespace {
			return nil, fmt.Errorf("official CN patch root %q has a different ordered version.dat bundle namespace", absolute)
		}
		resolved = append(resolved, absolute)
	}
	return resolved, nil
}

// Parallel official captures have the same version and ordered bundle namespace
// but different historical CRCs. The first root stays the catalog authority;
// the generated asset map supplies the CRC of the selected delivery bytes.
// Comparing whole version.dat files incorrectly rejects the verified CN variant.
func cnPatchVersionNamespace(encoded []byte) (string, error) {
	decoded := bytes.Clone(encoded)
	for index := range decoded {
		decoded[index] -= cn602ScrambleKey[index%len(cn602ScrambleKey)]
	}
	reader := csv.NewReader(bytes.NewReader(decoded))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse official CN version.dat: %w", err)
	}
	foundVersion := false
	names := make([]string, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		switch strings.TrimSpace(row[0]) {
		case "<version>":
			if len(row) != 2 || foundVersion || strings.TrimSpace(row[1]) != strconv.Itoa(cn602CatalogVersion) {
				return "", errors.New("official CN version.dat has an invalid or duplicate version row")
			}
			foundVersion = true
		case "<bundle_ver>":
			if len(row) != 4 {
				return "", errors.New("official CN version.dat has an invalid bundle row")
			}
			name := strings.TrimSpace(row[1])
			if name != path.Clean(name) || name == "." || strings.HasPrefix(name, "../") || path.IsAbs(name) || strings.ContainsAny(name, "\\:,\r\n\x00") {
				return "", fmt.Errorf("unsafe official CN bundle path %q", name)
			}
			if _, exists := seen[strings.ToLower(name)]; exists {
				return "", fmt.Errorf("duplicate official CN bundle path %q", name)
			}
			if _, err := strconv.ParseUint(strings.TrimSpace(row[3]), 16, 32); err != nil {
				return "", fmt.Errorf("invalid official CN bundle CRC for %q", name)
			}
			seen[strings.ToLower(name)] = struct{}{}
			names = append(names, name)
		}
	}
	if !foundVersion || len(names) == 0 {
		return "", errors.New("official CN version.dat has no version or bundles")
	}
	return strings.Join(names, "\x00"), nil
}

// BuildCN602CatalogForAudit runs the same patch-root and catalog construction
// path used by the server without starting a listener. Operational tooling
// uses it to validate staged overlays before they are eligible for a runtime
// profile.
func BuildCN602CatalogForAudit(patchRoots []string, assetMapPath string) ([]byte, error) {
	resolved, err := resolveCNPatchRoots(patchRoots)
	if err != nil {
		return nil, err
	}
	return buildCN602Catalog(resolved, assetMapPath)
}

func buildCN602Catalog(patchRoots []string, assetMapPath string) ([]byte, error) {
	versionBytes, err := os.ReadFile(filepath.Join(patchRoots[0], "version.dat"))
	if err != nil {
		return nil, fmt.Errorf("read official CN version.dat: %w", err)
	}
	versionDATDigest := sha256.Sum256(versionBytes)
	versionDATSHA256 := hex.EncodeToString(versionDATDigest[:])
	for index := range versionBytes {
		versionBytes[index] -= cn602ScrambleKey[index%len(cn602ScrambleKey)]
	}
	versionReader := csv.NewReader(bytes.NewReader(versionBytes))
	versionReader.FieldsPerRecord = -1
	rows, err := versionReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse official CN version.dat: %w", err)
	}

	var catalog strings.Builder
	fmt.Fprintf(&catalog, "<v>,%d\n", cn602CatalogVersion)
	foundVersion := false
	foundMenu := false
	bundleCount := 0
	availableBundles := make(map[string]struct{})
	bundleRows := make([]cnCatalogBundleRow, 0)
	for _, row := range rows {
		if len(row) == 2 && strings.TrimSpace(row[0]) == "<version>" {
			version, parseErr := strconv.Atoi(strings.TrimSpace(row[1]))
			if parseErr != nil || version != cn602CatalogVersion {
				return nil, fmt.Errorf("official CN version.dat has unexpected version %q", row[1])
			}
			foundVersion = true
			continue
		}
		if len(row) != 4 || strings.TrimSpace(row[0]) != "<bundle_ver>" {
			continue
		}
		bundleName := strings.TrimSpace(row[1])
		cleanName := path.Clean(bundleName)
		if cleanName != bundleName || cleanName == "." || strings.HasPrefix(cleanName, "../") || path.IsAbs(cleanName) || strings.ContainsAny(cleanName, ",\r\n") {
			return nil, fmt.Errorf("unsafe official CN bundle path %q", bundleName)
		}
		patchCRC := strings.ToUpper(strings.TrimSpace(row[3]))
		if _, parseErr := strconv.ParseUint(patchCRC, 16, 32); parseErr != nil {
			return nil, fmt.Errorf("invalid official CN bundle CRC %q for %q", patchCRC, bundleName)
		}
		bundleInfo, statErr := findCNPatchFile(patchRoots, bundleName)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf("stat official CN bundle %q: %w", bundleName, statErr)
		}
		if !bundleInfo.Mode().IsRegular() || bundleInfo.Size() <= 0 {
			return nil, fmt.Errorf("official CN bundle %q is not a non-empty file", bundleName)
		}
		bundleRows = append(bundleRows, cnCatalogBundleRow{
			name:     bundleName,
			patchCRC: patchCRC,
			size:     bundleInfo.Size(),
		})
		bundleCount++
		availableBundles[bundleName] = struct{}{}
		if bundleName == cn602MenuBundle {
			foundMenu = true
		}
	}
	if !foundVersion {
		return nil, errors.New("official CN version.dat has no version row")
	}
	if bundleCount == 0 {
		return nil, errors.New("official CN patch root has no versioned bundle files")
	}
	if !foundMenu {
		return nil, errors.New("official CN patch root is missing the Menu bundle")
	}
	assetMap, bundleMetadata, err := loadCNAssetMap(assetMapPath, versionDATSHA256, availableBundles)
	if err != nil {
		return nil, err
	}
	for _, bundle := range bundleRows {
		metadata := bundleMetadata[bundle.name]
		deliveryCRC32 := bundle.patchCRC
		if metadata.deliveryCRC32 != "" {
			deliveryCRC32 = metadata.deliveryCRC32
		}
		storageFlag := 0
		if metadata.scrambled {
			storageFlag = 1
		}
		// BundleInfo.bundle_crc is parsed but never consumed by the CN 6.0.2
		// managed client. The delivery CRC is authoritative for local-file identity
		// and update URLs. Official bundles inherit it from version.dat; a validated
		// overlay may declare the CRC of its replacement delivery bytes in the asset
		// map. Field 8 selects the original client's storage
		// path: scrambled files are decoded and loaded from memory, while ordinary
		// UnityFS/UnityRaw/UnityWeb files must be loaded directly from disk.
		//
		// Field 6 is BundleSettings.category.  Category 0 is synchronously fetched
		// by Title.onRecvPatchList before Connect and has no DownloadFirst UI;
		// category 1 is handed to IntroMgr.downloadC0, where the original Intro
		// scene owns the sound choice, total-size prompt and both progress bars.
		// The CN source version.dat does not preserve the retired service's category
		// table (its third column is 0 for every row), so the local full-data profile
		// deliberately publishes all official bundles as initial-download category
		// 1.  This preserves the original clean-install state machine instead of
		// silently transferring the whole data set behind the Title waiting layer.
		fmt.Fprintf(
			&catalog,
			"<b>,%s,%s,00000000,%d,N,1,0,%d",
			bundle.name,
			deliveryCRC32,
			bundle.size,
			storageFlag,
		)
		for _, dependency := range metadata.dependencies {
			fmt.Fprintf(&catalog, ",%s", dependency)
		}
		catalog.WriteByte('\n')
	}
	// The generated official container table is authoritative for extension and
	// bundle ownership. Its logical names are lowercased by the CN bundles; the
	// targeted FileCatalog compatibility patch handles caller casing. There are
	// no handwritten catalog rows: every asset owner comes from this manifest.
	for _, asset := range assetMap {
		fmt.Fprintf(
			&catalog,
			"<a>,%s,%s,%s,%s,%s\n",
			asset.directory,
			asset.name,
			asset.baseDir,
			asset.extension,
			asset.bundle,
		)
	}

	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	_, writeErr := zipper.Write([]byte(catalog.String()))
	closeErr := zipper.Close()
	if writeErr != nil || closeErr != nil {
		return nil, errors.New("compress CN catalog")
	}
	return compressed.Bytes(), nil
}

type cnCatalogBundleAsset struct {
	directory string
	name      string
	baseDir   string
	extension string
	bundle    string
}

type cnCatalogBundleRow struct {
	name     string
	patchCRC string
	size     int64
}

func findCNPatchFile(patchRoots []string, relative string) (os.FileInfo, error) {
	for _, root := range patchRoots {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return nil, fmt.Errorf("official CN bundle %q is not a non-empty file", relative)
		}
		// Match both select_bundle in the generator and serveCNPatchFile:
		// earlier roots own duplicate paths, including different-sized official
		// variants. A shadowed copy must not change the catalog's selected size.
		return info, nil
	}
	return nil, os.ErrNotExist
}

func cnBootstrapPatchFile(patchRoots []string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		serveCNPatchFile(writer, request, patchRoots, chi.URLParam(request, "*"))
	}
}

func cnBootstrapVersionedPatchFile(patchRoots []string, delivery []cnPatchDelivery) http.HandlerFunc {
	versions := make(map[string]string, len(delivery))
	for _, file := range delivery {
		versions[file.Name] = file.CRC
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		versioned := chi.URLParam(request, "*")
		suffixAt := strings.LastIndex(versioned, ".v")
		if suffixAt <= 0 || len(versioned[suffixAt+2:]) != 8 {
			http.Error(writer, "invalid CN patch path", http.StatusBadRequest)
			return
		}
		if _, err := strconv.ParseUint(versioned[suffixAt+2:], 16, 32); err != nil {
			http.Error(writer, "invalid CN patch version", http.StatusBadRequest)
			return
		}
		if !strings.EqualFold(versions[versioned[:suffixAt]], versioned[suffixAt+2:]) {
			http.NotFound(writer, request)
			return
		}
		serveCNPatchFile(writer, request, patchRoots, versioned[:suffixAt])
	}
}

func serveCNPatchFile(writer http.ResponseWriter, request *http.Request, patchRoots []string, relative string) {
	clean := path.Clean(relative)
	if relative == "" || clean != relative || clean == "." || path.IsAbs(clean) ||
		strings.HasPrefix(clean, "../") || strings.Contains(clean, `\`) {
		http.Error(writer, "invalid CN patch path", http.StatusBadRequest)
		return
	}
	for _, root := range patchRoots {
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(clean)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			http.Error(writer, "read CN patch file", http.StatusInternalServerError)
			return
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			http.Error(writer, "read CN patch file", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeContent(writer, request, path.Base(clean), info.ModTime(), file)
		_ = file.Close()
		return
	}
	http.NotFound(writer, request)
}

func cnBootstrapCatalog(content []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(content)
	}
}

func cnBootstrapModuleSwitches(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"res_code":   0,
		"mods_state": cn602LocalModuleSwitchState,
	})
}

func cnBootstrapLogin(accounts *cnAccountStore, advertiseHost string, port int, cdn CDNConfig) http.HandlerFunc {
	baseURL := "http://" + advertiseHost + ":" + strconv.Itoa(port)
	patchURL, cpkURL := cdn.resourceURLs(baseURL)
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
		if err != nil || len(body) == 0 || len(body) > maxCapturedBody {
			http.Error(writer, "read CN local login", http.StatusBadRequest)
			return
		}
		var payload struct {
			UUID          string `json:"uuid"`
			ClientVersion string `json:"clver"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(writer, "decode CN local login", http.StatusBadRequest)
			return
		}
		if payload.UUID == "" {
			http.Error(writer, "CN local login UUID is required", http.StatusBadRequest)
			return
		}
		if !cnClientVersionAllowed(payload.ClientVersion) {
			cnRejectOldClient(writer)
			return
		}
		identity, err := accounts.resolveLogin(payload.UUID)
		if err != nil {
			http.Error(writer, "resolve CN local account", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		uid := fmt.Sprintf("local-cn-user-%d", identity.UserID)
		session := identity.SessionKey
		if identity.UserID == cnPrimaryUserID {
			uid = "local-cn-user"
			session = "local-cn-session"
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"res_code":         0,
			"res_str":          "",
			"sess_key":         identity.SessionKey,
			"api_url":          baseURL + "/",
			"res_patch_url":    patchURL,
			"version_url":      baseURL + "/local/version/" + cn602VersionNamespace + "/",
			"res_img_url":      baseURL + "/local/resources/image/",
			"res_cpk_url":      cpkURL,
			"web_url":          baseURL + "/disabled/web",
			"charge_url":       baseURL + "/disabled/charge",
			"products_url":     baseURL + "/disabled/products",
			"update_url":       cnClientReleaseURL,
			"gid":              1,
			"userid":           identity.UserID,
			"uid":              uid,
			"session":          session,
			"realname_status":  1,
			"room_config":      nil,
			"comment_url":      nil,
			"display_pictures": map[string]any{},
			"sp_resource_flag": 1,
		})
	}
}

func normalizeLeadingSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "//") {
			request.URL.Path = "/" + strings.TrimLeft(request.URL.Path, "/")
		}
		next.ServeHTTP(writer, request)
	})
}

func acknowledgeClientLog(writer http.ResponseWriter, request *http.Request) {
	response := make(map[string]string)
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err == nil {
		if category, ok := payload["log_cat"].(string); ok && category != "" {
			response["log_cat"] = category
		}
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(response)
}

func (r *recorder) middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/healthz" && !isBulkResourceRequest(request) {
				body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
				if err != nil {
					http.Error(writer, "request body read failed", http.StatusBadRequest)
					return
				}
				if len(body) > maxCapturedBody {
					http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				request.Body = io.NopCloser(bytes.NewReader(body))
				digest := sha256.Sum256(body)
				headers := request.Header.Clone()
				for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization", cnAccountUserHeader} {
					headers.Del(name)
				}
				entry := capture{
					Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					Method:    request.Method,
					Path:      request.URL.Path,
					Query:     request.URL.RawQuery,
					Remote:    request.RemoteAddr,
					Headers:   headers,
					BodyBytes: len(body),
					BodySHA:   hex.EncodeToString(digest[:]),
					Body:      redactCNRequestBody(request.URL.Path, body),
				}
				if strings.HasPrefix(request.URL.Path, "/local/account/") {
					// Even a raw SHA-256/length would expose a fast password
					// guessing oracle. Account captures contain routing only.
					entry.BodySHA, entry.Query, entry.BodyBytes = "", "", 0
					entry.Headers = nil
				}
				if err := r.append(entry); err != nil {
					logger.Warn("skip failed request capture", "error", err)
				} else {
					logger.Info("captured CN request", "method", request.Method, "path", request.URL.Path)
				}
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func redactCNSessionBody(body []byte) string {
	_, payload, ok := splitCNSessionPayload(body)
	if !ok {
		return string(body)
	}
	return "<session-redacted>" + string(payload)
}

func redactCNRequestBody(path string, body []byte) string {
	path = "/" + strings.TrimLeft(path, "/")
	if path == "/loginSDK.php" || strings.HasPrefix(path, "/local/account/") {
		return "<login-redacted>"
	}
	return redactCNSessionBody(body)
}

func (r *recorder) append(entry capture) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	// The capture is diagnostic runtime evidence, not transactional state. Closing
	// the handle preserves normal-process durability without blocking every client
	// request on a physical-disk flush.
	return file.Close()
}

func isBulkResourceRequest(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	path := "/" + strings.TrimLeft(request.URL.Path, "/")
	return strings.HasPrefix(path, "/local/resources/") ||
		strings.HasPrefix(path, "/local/version/")
}
