package admin

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func customGachaTestAPI(t *testing.T) (*API, *accountstore.Accounts, http.Handler) {
	t.Helper()
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	base := gamestate.GachaProfile{GachaID: 60201301, GroupID: 60201301, Name: "source", CategoryNum: 1, CategoryPictID: 1, BuyMessage: "draw", EndTime: 2147483647, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1, BannerKey: "local_standard", CardIDs: []int{1}, CardWeights: []int{1}, PlayCount: 9}
	ops, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{base})
	if err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops, gachaBannerPaths: map[string]string{"local_standard": "fixture"}, catalogByKey: map[string]AdminCatalogEntry{"6:1": {GachaEligible: true, Rarity: 5, LevelMax: 100, FameMax: 100, LoveMax: 100, ResourceState: "ready"}, "8:10": {Kind: "item", Name: "item", ResourceState: "ready"}}}
	router := chi.NewRouter()
	router.Post("/create", a.gachaCreate)
	router.Post("/editor/{action}", a.gachaEditorAction)
	router.Post("/banner", a.gachaBannerUpload)
	router.Get("/banners/{file}", ops.CustomGachaBanner)
	return a, accounts, router
}

func customGachaRequest(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	content, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://localhost"+path, bytes.NewReader(content))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kairisei-Admin-Action", "apply")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCustomGachaLifecycleAndPlayerProgress(t *testing.T) {
	a, accounts, h := customGachaTestAPI(t)
	w := customGachaRequest(t, h, "/create", map[string]any{"source_id": 60201301, "name": "new pool"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		ID int `json:"gacha_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.ID
	base := a.operations.gachaBases[id]
	if id == 60201301 || base.GroupID != id || base.PlayCount != 0 || a.operations.gachaBases[60201301].PlayCount != 9 {
		t.Fatal("copy did not isolate identity/progress")
	}
	revision := 0
	if _, err := a.operations.setGachaPublication(gachaPublication{GroupIDs: []int{id}, ExpectedRevision: &revision}); err == nil {
		t.Fatal("unpublished pool opened")
	}
	config := AdminGachaConfigFromProfile(base)
	w = customGachaRequest(t, h, "/editor/draft", map[string]any{"config": config, "expected_revision": 0})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var saved struct {
		Document accountstore.Document `json:"document"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(a.operations.gachaConfigurations) != 0 {
		t.Fatal("draft became live")
	}
	publish := map[string]any{"config": map[string]int{"gacha_id": id}, "expected_revision": 1, "expected_live_revision": 0, "sha256": saved.Document.SHA256}
	w = customGachaRequest(t, h, "/editor/publish", publish)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = customGachaRequest(t, h, "/editor/publish", publish); w.Code != 409 {
		t.Fatal("stale publish accepted")
	}
	active, err := a.operations.GachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	if a.operations.GachaIDPublished(id, active) {
		t.Fatal("new pool opened automatically")
	}
	if _, err := a.operations.setGachaPublication(gachaPublication{GroupIDs: []int{id}, ExpectedRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	active, err = a.operations.GachaPublication()
	if err != nil || !a.operations.GachaIDPublished(id, active) {
		t.Fatal("pool failed to open", err)
	}
	state, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range state.Gachas {
		if state.Gachas[i].GachaID == id {
			state.Gachas[i].PlayCount = 6
			found = true
		}
	}
	if !found {
		t.Fatal("custom pool absent from account catalog")
	}
	if err := accounts.PersistState(accountstore.PrimaryUserID, state); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{a.operations.gachaBases[60201301]})
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.customGachas[id] || len(restarted.gachaConfigurations) != 1 {
		t.Fatal("pool missing on restart")
	}
	restored, err := accounts.LoadPersistentState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, p := range restored.Gachas {
		if p.GachaID == id {
			found = true
			if p.PlayCount != 6 {
				t.Fatal("step/gift progress reset on reload")
			}
		}
	}
	if !found {
		t.Fatal("restored catalog lost custom pool")
	}
	// Returned catalogs must not let account mutations corrupt global rewards.
	catalog, _ := accounts.Database().CatalogState()
	for i := range catalog.Gachas {
		if catalog.Gachas[i].GachaID == id {
			catalog.Gachas[i].CardIDs[0] = 999
		}
	}
	catalog, _ = accounts.Database().CatalogState()
	for _, p := range catalog.Gachas {
		if p.GachaID == id && p.CardIDs[0] != 1 {
			t.Fatal("catalog mutation leaked")
		}
	}
}

func TestCustomMixedRewardsAndBannerValidation(t *testing.T) {
	a, _, h := customGachaTestAPI(t)
	w := customGachaRequest(t, h, "/create", map[string]any{"name": "mixed", "mode": "mixed"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		ID int `json:"gacha_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	c := AdminGachaConfigFromProfile(a.operations.gachaBases[created.ID])
	reward := gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}
	c.RewardPool = []gamestate.WeightedReward{{Reward: reward, Weight: 1}}
	c.Steps = []gamestate.GachaStep{{Price: c.Price, RewardPool: c.RewardPool}, {Price: 1, RewardPool: c.RewardPool}}
	c.Gifts = []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 1, Rewards: []gamestate.Reward{reward}}}
	if _, err := a.validateGachaConfig(c); err != nil {
		t.Fatal(err)
	}
	invalid := c
	invalid.Steps = append([]gamestate.GachaStep{}, c.Steps...)
	invalid.Steps[0].Price++
	if _, err := a.validateGachaConfig(invalid); err == nil {
		t.Fatal("inconsistent first stage accepted")
	}
	invalid = c
	invalid.CardNum = 12
	if _, err := a.validateGachaConfig(invalid); err == nil {
		t.Fatal("unsafe draw size accepted")
	}
	invalid = c
	invalid.Gifts = []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 1, Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 999, Num: 1, CardSkillLevels: []int16{}}}}}
	if _, err := a.validateGachaConfig(invalid); err == nil {
		t.Fatal("unknown gift accepted")
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	w = customGachaRequest(t, h, "/banner", map[string]any{"content": pngBytes.Bytes()})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var uploaded struct {
		Key string `json:"banner_key"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &uploaded)
	c.BannerKey = uploaded.Key
	if _, err := a.validateGachaConfig(c); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/banners/"+uploaded.Key+".png", nil))
	if get.Code != 200 || !bytes.Equal(get.Body.Bytes(), pngBytes.Bytes()) {
		t.Fatal("uploaded banner unavailable")
	}
	if w = customGachaRequest(t, h, "/banner", map[string]any{"content": []byte("not PNG")}); w.Code != 400 {
		t.Fatal("invalid image accepted")
	}
	c.BannerKey = "custom_" + string(bytes.Repeat([]byte("0"), 64))
	if _, err := a.validateGachaConfig(c); err == nil {
		t.Fatal("nonexistent banner published")
	}
}

func TestCustomGachaConcurrentIDsAndReaders(t *testing.T) {
	a, _, h := customGachaTestAPI(t)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := customGachaRequest(t, h, "/create", map[string]any{"name": "parallel"})
			codes <- w.Code
			_, _ = a.operations.GachaPublication()
			_ = a.operations.ManagedGroups()
			_ = a.operations.GachaIDPublished(70000001, map[int]struct{}{})
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("create: %d", code)
		}
	}
	if len(a.operations.customGachas) != 8 {
		t.Fatal("concurrent identity collision")
	}
}

func TestCopyHistorical60201301WithEditableStages(t *testing.T) {
	a, _, h := customGachaTestAPI(t)
	seed, err := accountstore.LoadSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source gamestate.GachaProfile
	for _, p := range seed.Gachas {
		if p.GachaID == 60201301 {
			source = p
		}
	}
	if len(source.Steps) != 3 {
		t.Fatal("historical source missing")
	}
	a.operations.gachaBases[source.GachaID] = source
	a.gachaBannerPaths[source.BannerKey] = "fixture"
	a.catalogByKey[adminCatalogKey(8, source.PayTypeID)] = AdminCatalogEntry{ResourceState: "ready"}
	for _, step := range source.Steps {
		for _, entry := range step.RewardPool {
			r := entry.Reward
			a.catalogByKey[adminCatalogKey(r.Type, r.RewardTypeID)] = AdminCatalogEntry{ResourceState: "ready", LevelMax: 100, FameMax: 100, LoveMax: 100}
		}
	}
	w := customGachaRequest(t, h, "/create", map[string]any{"source_id": 60201301, "name": "历史池副本"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		ID int `json:"gacha_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	c := AdminGachaConfigFromProfile(a.operations.gachaBases[created.ID])
	if _, err := a.validateGachaConfig(c); err != nil {
		t.Fatal("historical copy invalid", err)
	}
	c.Steps[1].Price = 7
	c.Steps[1].RewardPool = c.Steps[1].RewardPool[:1]
	c.Steps = append(c.Steps, c.Steps[1])
	if _, err := a.validateGachaConfig(c); err != nil {
		t.Fatal("custom stage edits rejected", err)
	}
	if len(source.Steps) != 3 || len(source.Steps[1].RewardPool) <= 1 || source.Steps[1].Price == 7 {
		t.Fatal("editing copy changed source")
	}
}

func TestCopy60200001WholeGroupAndPublication(t *testing.T) {
	a, accounts, h := customGachaTestAPI(t)
	seed, err := accountstore.LoadSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range seed.Gachas {
		if p.GroupID != 60200001 {
			continue
		}
		a.operations.gachaBases[p.GachaID] = p
		a.gachaBannerPaths[p.BannerKey] = "fixture"
		for _, id := range p.CardIDs {
			a.catalogByKey[adminCatalogKey(6, id)] = AdminCatalogEntry{ResourceState: "ready", GachaEligible: true, Rarity: 5}
		}
	}
	a.catalogByKey["8:2000"] = AdminCatalogEntry{ResourceState: "ready"}
	w := customGachaRequest(t, h, "/create", map[string]any{"name": "三入口新池", "mode": "standard_group"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		Group int   `json:"group_id"`
		IDs   []int `json:"gacha_ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.IDs) != 3 {
		t.Fatal("template did not copy three entries")
	}
	initial := 0
	for i, id := range created.IDs {
		p := a.operations.gachaBases[id]
		if p.GroupID != created.Group || p.PlayCount != 0 {
			t.Fatal("group/progress identity wrong")
		}
		expectedPay := []int{4, 3, 3}
		expectedPrice := []int{1, 50, 500}
		expectedCount := []int{1, 1, 10}
		if p.PayType != expectedPay[i] || p.Price != expectedPrice[i] || p.CardNum != expectedCount[i] {
			t.Fatalf("entry %d lost template rules: %+v", i, p)
		}
		if i == 0 && p.PayTypeID != 2000 {
			t.Fatal("ticket identity lost")
		}
		if _, err := a.operations.setGachaPublication(gachaPublication{GroupIDs: []int{created.Group}, ExpectedRevision: &initial}); err == nil {
			t.Fatal("partially published group opened")
		}
		c := AdminGachaConfigFromProfile(p)
		w = customGachaRequest(t, h, "/editor/draft", map[string]any{"config": c, "expected_revision": 0})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var draft struct {
			Document accountstore.Document `json:"document"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &draft)
		w = customGachaRequest(t, h, "/editor/publish", map[string]any{"config": map[string]int{"gacha_id": id}, "expected_revision": draft.Document.Revision, "expected_live_revision": 0, "sha256": draft.Document.SHA256})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if _, err := a.operations.setGachaPublication(gachaPublication{GroupIDs: []int{created.Group}, ExpectedRevision: &initial}); err != nil {
		t.Fatal(err)
	}
	if rows := a.customGachaPresets(); len(rows) != 1 || len(rows[0].GachaIDs) != 3 {
		t.Fatal("three entries were split into separate publication groups")
	}
	restarted, err := NewOperations(accounts.Database(), seed.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	active, err := restarted.GachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range created.IDs {
		if !restarted.GachaIDPublished(id, active) {
			t.Fatal("group entry unavailable after restart")
		}
	}
	if _, err := accounts.LoadPersistentState(accountstore.PrimaryUserID); err != nil {
		t.Fatal("group could not be restored into account", err)
	}
	// Copying any entry must retain the complete group, including custom groups.
	w = customGachaRequest(t, h, "/create", map[string]any{"name": "再次复制", "source_id": created.IDs[1]})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var second struct {
		IDs []int `json:"gacha_ids"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &second)
	if len(second.IDs) != 3 || second.IDs[0] == created.IDs[0] {
		t.Fatal("copying group member lost siblings")
	}
}
