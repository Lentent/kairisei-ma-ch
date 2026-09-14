package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/release"
)

func TestCNAdminGachaDraftPublicationAndRestart(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	base := release.GachaProfile{GachaID: 60200301, Name: "local", Price: 10, CardNum: 2, CardNumMax: 2, GroupID: 60200301, PublicationKey: "water_coin", CardIDs: []int{1, 2}, CardWeights: []int{1, 1}, GuaranteedCount: 1, GuaranteedRarityRank: 6, RemainderRarityRank: 5}
	operations, err := newCNOperationStore(accounts.storage, []release.GachaProfile{base, {GachaID: 90000100}})
	if err != nil {
		t.Fatal(err)
	}
	admin := &cnAdmin{operations: operations, catalogByKey: map[string]cnAdminCatalogEntry{"6:1": {GachaEligible: true, Rarity: 6, ResourceState: "ready"}, "6:2": {GachaEligible: true, Rarity: 5, ResourceState: "ready"}, "6:3": {GachaEligible: true, Rarity: 6, ResourceState: "unavailable"}}}
	router := chi.NewRouter()
	router.Post("/{action}", admin.gachaEditorAction)
	call := func(action string, body any) *httptest.ResponseRecorder {
		content, _ := json.Marshal(body)
		request := httptest.NewRequest(http.MethodPost, "http://localhost/"+action, bytes.NewReader(content))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Kairisei-Admin-Action", "apply")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	config := cnAdminGachaConfigFromProfile(base)
	config.Price = 25
	config.Weights = []int{1, 9}
	result := call("draft", map[string]any{"config": config, "expected_revision": 0})
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	var saved struct {
		Document cnAdminDocument `json:"document"`
		Preview  struct {
			Stages []struct {
				Odds []int `json:"odds_scaled"`
			} `json:"stages"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Preview.Stages) != 2 || saved.Preview.Stages[0].Odds[0] != 10000000 || saved.Preview.Stages[1].Odds[0] != 10000000 {
		t.Fatalf("guaranteed draws preview wrong distribution: %s", result.Body.String())
	}
	if operations.gachaRevision != 0 {
		t.Fatal("saving a draft changed the active pool")
	}
	publish := map[string]any{"config": map[string]int{"gacha_id": base.GachaID}, "expected_revision": 1, "expected_live_revision": 0, "sha256": saved.Document.SHA256}
	if response := call("publish", publish); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if response := call("publish", publish); response.Code != 409 {
		t.Fatalf("stale publication status=%d", response.Code)
	}
	restarted, err := newCNOperationStore(accounts.storage, []release.GachaProfile{base, {GachaID: 90000100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.gachaBases) != 1 || len(restarted.gachaConfigurations) != 1 || restarted.gachaConfigurations[0].Profile.Price != 25 {
		t.Fatal("publication did not survive restart or exposed onboarding pool")
	}
	config.CardIDs = []int{3, 2}
	result = call("draft", map[string]any{"config": config, "expected_revision": 1})
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	if err := json.Unmarshal(result.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	publish["expected_revision"], publish["expected_live_revision"], publish["sha256"] = 2, 1, saved.Document.SHA256
	if response := call("publish", publish); response.Code != 400 {
		t.Fatalf("unavailable card published: %d", response.Code)
	}
	config.CardIDs = []int{1, 2}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- call("draft", map[string]any{"config": config, "expected_revision": 2}).Code
		}()
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent editors: success=%d conflict=%d", success, conflict)
	}
	entry := admin.catalogByKey["6:1"]
	entry.GachaEligible = false
	admin.catalogByKey["6:1"] = entry
	if _, err := admin.validateGachaConfig(config); err == nil {
		t.Fatal("non-gacha acquisition card accepted by the pool editor")
	}
	entry.GachaEligible = true
	admin.catalogByKey["6:1"] = entry
	unowned := release.GachaProfile{GachaID: 60201102, UnownedOnly: true, CardNum: 1, CardNumMax: 1, Price: 1, CardIDs: []int{1}, CardWeights: []int{1}}
	operations.gachaBases[unowned.GachaID] = unowned
	unownedConfig := cnAdminGachaConfigFromProfile(unowned)
	unownedConfig.Name = "未入手六星"
	if _, err := admin.validateGachaConfig(unownedConfig); err != nil {
		t.Fatal(err)
	}
	unownedConfig.CardIDs = []int{2}
	if _, err := admin.validateGachaConfig(unownedConfig); err == nil {
		t.Fatal("unowned six-star guarantee accepted a five-star card")
	}
	initialRevision := 0
	future := time.Now().Add(time.Hour).Unix()
	if _, err := operations.setTeamBattlePublication(cnTeamBattlePublication{Mode: "all", StartUnix: future, ExpectedRevision: &initialRevision}); err != nil {
		t.Fatal(err)
	}
	allowed, err := operations.teamBattleGroupAllowlist()
	if err != nil || allowed == nil || len(allowed) != 0 {
		t.Fatalf("future event directory is visible: %v %v", allowed, err)
	}
}

func TestCardSourceTabsKeepMultiSourceCardsAndExcludeEvolvedGachaCandidates(t *testing.T) {
	catalog, _, err := buildCNAdminCatalog(cnCardRuntimeMaster{
		CardTemplates: []release.Card{
			{CardID: 1, Name: "初始", RarityRank: 3, AcquisitionText: "普通副本，扭蛋"},
			{CardID: 2, Name: "进化", RarityRank: 4, AcquisitionText: "普通副本，进化，扭蛋"},
			{CardID: 3, Name: "奖章", RarityRank: 5, AcquisitionText: "BOSS币扭蛋"}},
		EvolutionTransitions: []release.EvolutionTransition{{FromCardID: 1, ToCardID: 2}},
		DeckRankPolicy: release.DeckRankPolicy{Cards: map[int]release.CardRankRule{
			1: {ArthurType: 1}, 2: {ArthurType: 4}, 3: {ArthurType: 4},
		}},
	}, cnItemRuntimeMaster{})
	if err != nil {
		t.Fatal(err)
	}
	admin := &cnAdmin{catalog: catalog}
	for _, source := range []string{"gacha", "dungeon"} {
		response := httptest.NewRecorder()
		admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&source="+source+"&limit=1&offset=1", nil))
		var page struct {
			Total   int
			Entries []cnAdminCatalogEntry
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Entries) != 1 || page.Entries[0].RewardTypeID != 2 || page.Entries[0].GachaEligible {
			t.Fatalf("incorrect source page: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&source=gacha&arthur_type=4&limit=1", nil))
	var page struct {
		Total   int
		Entries []cnAdminCatalogEntry
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].RewardTypeID != 2 || page.Entries[0].ArthurType != 4 {
		t.Fatalf("profession filter did not intersect source before pagination: %s", response.Body.String())
	}
}

func TestImportedCardCatalogFiltersIntersectBeforePagination(t *testing.T) {
	master := cnCardRuntimeMaster{
		Source: json.RawMessage(`{"imported_inventory":[{"region":"JP","ids":{"card":[10214045,10214046,10214048,10214050,10214053]}}]}`),
		CardTemplates: []release.Card{
			{CardID: 10214045, Name: "絢爛型オルトリート", RarityRank: 6},
			{CardID: 10214046, Name: "蹴球型ドモヴォーイ", RarityRank: 6},
			{CardID: 10214048, Name: "奏楽型ダカーポ", RarityRank: 6},
			{CardID: 10214050, Name: "聖夜型ルー", RarityRank: 6},
			{CardID: 10214053, Name: "新春型モードレッド", RarityRank: 6},
			// A Japanese name without an import receipt is not a JP import.
			{CardID: 10214099, Name: "既存カード", RarityRank: 6},
		},
		DeckRankPolicy: release.DeckRankPolicy{Cards: map[int]release.CardRankRule{
			10214045: {ArthurType: 3}, 10214046: {ArthurType: 4}, 10214048: {ArthurType: 2},
			10214050: {ArthurType: 1}, 10214053: {ArthurType: 2}, 10214099: {ArthurType: 2},
		}},
	}
	catalog, _, err := buildCNAdminCatalog(master, cnItemRuntimeMaster{})
	if err != nil {
		t.Fatal(err)
	}
	admin := &cnAdmin{catalog: catalog}
	for _, tc := range []struct {
		query string
		total int
		first int
	}{
		{"source=jp_import&arthur_type=0", 5, 10214045},
		{"source=jp_import&arthur_type=1", 1, 10214050},
		{"source=jp_import&arthur_type=2", 2, 10214048},
		{"source=jp_import&arthur_type=3", 1, 10214045},
		{"source=jp_import&arthur_type=4", 1, 10214046},
		{"source=jp_import&arthur_type=2&offset=1", 2, 10214053},
		{"source=jp_import&arthur_type=2&q=10214053", 1, 10214053},
		{"source=jp_import&arthur_type=1&q=10214053", 0, 0},
		{"source=other&arthur_type=2", 3, 10214048},
		{"source=gacha&arthur_type=2", 0, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			response := httptest.NewRecorder()
			admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&limit=1&"+tc.query, nil))
			var page struct {
				Total   int
				Entries []cnAdminCatalogEntry
			}
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || page.Total != tc.total ||
				(tc.first == 0 && len(page.Entries) != 0) ||
				(tc.first != 0 && (len(page.Entries) != 1 || page.Entries[0].RewardTypeID != tc.first || page.Entries[0].GachaEligible)) {
				t.Fatalf("incorrect imported card page: %s", response.Body.String())
			}
		})
	}
}
