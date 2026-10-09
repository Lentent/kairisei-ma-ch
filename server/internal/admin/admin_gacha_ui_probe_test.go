package admin

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

// Manual browser fixture: every write goes to NewFriendCapacityTestAccounts'
// temporary database. It never opens a deployment's player database.
func TestGachaUIProbe(t *testing.T) {
	if os.Getenv("KAIRI_GACHA_UI_PROBE") != "1" {
		t.Skip("manual isolated gacha UI probe; opt in with KAIRI_GACHA_UI_PROBE=1")
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoot := t.TempDir()
	writeImage := func(path string, width, height int, tint color.RGBA) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		canvas := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(tint), image.Point{}, draw.Src)
		if err := png.Encode(file, canvas); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	assetsRoot := filepath.Join(fixtureRoot, "assets")
	bannerPath := filepath.Join(fixtureRoot, "local_standard.png")
	writeImage(bannerPath, 934, 372, color.RGBA{R: 45, G: 83, B: 114, A: 255})
	entries := []AdminCatalogEntry{
		{Kind: "currency", RewardType: 4, Name: "金币", Detail: "隔离测试奖励", ResourceState: "ready"},
		{Kind: "currency", RewardType: 10, Name: "免费水晶", Detail: "隔离测试奖励", ResourceState: "ready"},
		{Kind: "currency", RewardType: 12, Name: "体力", Detail: "隔离测试奖励", ResourceState: "ready"},
		{Kind: "item", RewardType: 8, RewardTypeID: 4000, Name: "测试 Boss 币", Detail: "隔离测试消耗道具", ResourceState: "ready", ImageURL: "/assets/item/4000.png"},
	}
	cardDefinitions := catalog.CardTemplates
	if len(cardDefinitions) == 0 {
		cardDefinitions = catalog.Cards
	}
	cardIDs, weights := []int{}, []int{}
	jobs := map[int8]bool{}
	selected := map[int]bool{}
	addCard := func(card gamestate.Card) {
		job := catalog.DeckRankPolicy.Cards[card.CardID].ArthurType
		selected[card.CardID], jobs[job] = true, true
		cardIDs, weights = append(cardIDs, card.CardID), append(weights, 1)
		entries = append(entries, AdminCatalogEntry{Kind: "card", RewardType: 6, RewardTypeID: card.CardID, Name: card.Name, Rarity: card.RarityRank, ArthurType: job, LevelMax: max(1, card.LevelMax), FameMax: max(1, card.FameMax), LoveMax: max(0, card.LoveMax), GachaEligible: true, ResourceState: "ready", SourceTags: []string{"gacha"}, ImageURL: fmt.Sprintf("/assets/card/%d.png", card.CardID)})
	}
	for _, card := range cardDefinitions {
		job := catalog.DeckRankPolicy.Cards[card.CardID].ArthurType
		if card.CardID > 0 && card.RarityRank >= 3 && !jobs[job] {
			addCard(card)
		}
	}
	for _, card := range cardDefinitions {
		if len(cardIDs) >= 12 {
			break
		}
		if card.CardID > 0 && card.RarityRank >= 3 && !selected[card.CardID] {
			addCard(card)
		}
	}
	// The lightweight friend fixture omits external card runtime masters. Reuse
	// the seeded pool identities with explicit preview-only definitions instead.
	if len(cardIDs) == 0 {
		for _, profile := range catalog.Gachas {
			for _, id := range profile.CardIDs {
				if len(cardIDs) >= 12 || selected[id] {
					continue
				}
				addCard(gamestate.Card{CardID: id, Name: fmt.Sprintf("测试卡牌 %d", id), RarityRank: 5, LevelMax: 100, FameMax: 100, LoveMax: 100})
				entries[len(entries)-1].ArthurType = int8((len(cardIDs)-1)%4 + 1)
			}
		}
	}
	if len(cardIDs) == 0 {
		addCard(gamestate.Card{CardID: 1, Name: "测试卡牌 1", RarityRank: 5, LevelMax: 100, FameMax: 100, LoveMax: 100})
	}
	materials := catalog.StackCardTemplates
	if len(materials) == 0 {
		materials = catalog.StackCards
	}
	for i, material := range materials {
		if i >= 2 {
			break
		}
		entries = append(entries, AdminCatalogEntry{Kind: "material", RewardType: 13, RewardTypeID: material.CardID, Name: fmt.Sprintf("测试素材 %d", material.CardID), Detail: "隔离测试素材奖励", ResourceState: "ready", ImageURL: fmt.Sprintf("/assets/card/%d.png", material.CardID)})
	}
	if len(materials) == 0 {
		entries = append(entries, AdminCatalogEntry{Kind: "material", RewardType: 13, RewardTypeID: 20000014, Name: "测试素材卡", Detail: "隔离预览奖励", ResourceState: "ready", ImageURL: "/assets/card/20000014.png"})
	}
	base := gamestate.GachaProfile{GachaID: 60200301, GroupID: 60200301, Name: "隔离测试普通池（单抽）", CategoryNum: 1, CategoryPictID: 1, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1, EndTime: 2147483647, BuyMessage: "隔离测试", BannerKey: "local_standard", CardIDs: cardIDs, CardWeights: weights}
	eleven := gamestate.CloneGachas([]gamestate.GachaProfile{base})[0]
	eleven.GachaID, eleven.GroupID, eleven.CardNum, eleven.CardNumMax, eleven.Price = 60200401, 60200401, 11, 11, 50
	eleven.Name = "隔离测试普通模板（11 抽）"
	operations, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{base, eleven})
	if err != nil {
		t.Fatal(err)
	}
	a := &API{accounts: accounts, operations: operations, catalog: entries, catalogByKey: map[string]AdminCatalogEntry{}, assetURLs: map[string]struct{}{}, assetsRoot: assetsRoot, gachaCoverDir: filepath.Join(fixtureRoot, "gacha-covers"), gachaBannerPaths: map[string]string{"local_standard": bannerPath}}
	for _, entry := range entries {
		a.catalogByKey[adminCatalogKey(entry.RewardType, entry.RewardTypeID)] = entry
		if entry.Kind == "card" {
			operations.gachaCardJobs[entry.RewardTypeID] = entry.ArthurType
		}
		if entry.ImageURL != "" {
			a.assetURLs[entry.ImageURL] = struct{}{}
			writeImage(filepath.Join(assetsRoot, filepath.FromSlash(strings.TrimPrefix(entry.ImageURL, "/assets/"))), 200, 276, color.RGBA{R: 61, G: 103, B: 131, A: 255})
		}
	}
	for _, profile := range []gamestate.GachaProfile{base, eleven} {
		a.gachaPresets = append(a.gachaPresets, AdminGachaPreset{GroupID: profile.GroupID, Name: profile.Name, BannerKey: profile.BannerKey, ImageURL: "/gacha-assets/local_standard.png", PaymentItem: "水晶", Price: profile.Price, DrawCount: profile.CardNum, CardCount: len(cardIDs), GachaIDs: []int{profile.GachaID}})
	}
	router := chi.NewRouter()
	router.Use(adminSecurityHeaders)
	router.Get("/", a.index)
	router.Get("/api/gacha-editor", a.gachaEditorList)
	router.Post("/api/gacha-pools", a.createCustomGacha)
	router.Post("/api/gacha-pools/{gachaID}/{action:delete|restore}", a.changeCustomGachaDeletion)
	router.Post("/api/gacha-variants", a.createGachaVariant)
	router.Post("/api/gacha-create", a.gachaCreate)
	router.Post("/api/gacha-editor/{action}", a.gachaEditorAction)
	router.Post("/api/gacha-editor/group/{action:draft|publish|discard}", a.gachaGroupAction)
	router.Post("/api/gacha-covers", a.uploadGachaCover)
	router.Get("/gacha-covers/{file}", ServeGachaCover(a.gachaCoverDir))
	router.Post("/api/gacha-banner", a.gachaBannerUpload)
	router.Get("/api/gacha-presets", a.gachaPresetList)
	router.Get("/api/gacha-policy", a.gachaPolicy)
	router.Put("/api/gacha-policy", a.setGachaPolicy)
	router.Get("/api/catalog", a.catalogEntries)
	router.Post("/api/catalog/resolve", a.resolveCatalog)
	router.Get("/assets/{kind}/{file}", a.catalogAsset)
	router.Get("/gacha-assets/{file}", a.gachaAsset)
	router.Get("/api/status", func(w http.ResponseWriter, r *http.Request) {
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "maintenance": map[string]any{"enabled": false}, "publication": map[string]any{"mode": "all", "group_ids": []int{}, "start_unix": 0, "end_unix": 0}, "client_profile": "isolated UI fixture", "game_endpoint": "no player server", "server_time_utc": time.Now().UTC().Format(time.RFC3339), "account_count": 1, "boss_count": 0, "boss_group_count": 0, "active_rooms": 0})
	})
	router.Get("/api/mail-batches", func(w http.ResponseWriter, r *http.Request) {
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "batches": []any{}, "total": 0})
	})
	router.Get("/api/exchanges", func(w http.ResponseWriter, r *http.Request) {
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "shops": []any{}})
	})
	router.Get("/api/player-policy", func(w http.ResponseWriter, r *http.Request) {
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "config": map[string]any{"notice": map[string]any{"enabled": false}}})
	})
	for path, content := range map[string][]byte{
		"/admin.js": adminJS, "/operations.js": adminOperationsJS, "/gacha-legacy.js": adminGachaLegacyJS,
		"/accounts.js": adminAccountsJS, "/mail.js": adminMailJS, "/settings.js": adminSettingsJS,
		"/evolution.js": adminEvolutionJS, "/content.js": adminContentJS, "/cdk-admin.js": adminCDKJS,
		"/collections.js": adminCollectionsJS, "/custom-cards.js": adminCustomCardsJS,
		"/player-policy.js": adminPlayerPolicyJS, "/dungeon-schedule.js": adminDungeonScheduleJS,
		"/missions.js": adminMissionsJS, "/insights.js": adminInsightsJS,
	} {
		router.Get(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(content)
		})
	}
	router.Get("/admin.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(adminCSS)
	})
	stop := make(chan struct{})
	var once sync.Once
	router.Get("/__probe_stop", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			http.Error(w, "local only", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("stopping isolated gacha UI probe"))
		once.Do(func() { close(stop) })
	})
	listener, err := net.Listen("tcp", "127.0.0.1:28932")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("READY isolated admin http://127.0.0.1:28932/#pool-editor; all writes use a disposable t.TempDir database")
	select {
	case <-stop:
	case <-time.After(15 * time.Minute):
		t.Fatal("UI probe timeout")
	}
}
