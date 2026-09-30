package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestOperatorGachaPoolLifecycle(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	operations, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]AdminCatalogEntry{}
	for _, gacha := range catalog.Gachas {
		for _, id := range gacha.CardIDs {
			entries[adminCatalogKey(6, id)] = AdminCatalogEntry{Rarity: 3, ResourceState: "ready", FameMax: 10}
		}
		if gacha.PayType == 4 {
			entries[adminCatalogKey(8, gacha.PayTypeID)] = AdminCatalogEntry{Kind: "item", ResourceState: "ready"}
		}
	}
	admin := &API{operations: operations, catalogByKey: entries, gachaCoverDir: t.TempDir()}
	router := chi.NewRouter()
	router.Post("/api/gacha-pools", admin.createCustomGacha)
	router.Post("/api/gacha-pools/{gachaID}/{action:delete|restore}", admin.changeCustomGachaDeletion)
	router.Post("/api/gacha-covers", admin.uploadGachaCover)
	router.Get("/gacha-covers/{file}", ServeGachaCover(admin.gachaCoverDir))
	call := func(path string, body any) (int, map[string]any) {
		content, _ := json.Marshal(body)
		request := httptest.NewRequest(http.MethodPost, "http://localhost"+path, bytes.NewReader(content))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Kairisei-Admin-Action", "apply")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var result map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &result)
		return response.Code, result
	}
	// Inject a durable failure after an earlier member of a batch was written.
	// This verifies the SQL rollback and runtime snapshot, not only input validation.
	database, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	failAudit := func(key string) func() {
		t.Helper()
		if _, err := database.Exec(`CREATE TRIGGER fail_gacha_review BEFORE INSERT ON cn_admin_audit WHEN NEW.target='` + key + `' BEGIN SELECT RAISE(ABORT,'review failure'); END`); err != nil {
			t.Fatal(err)
		}
		return func() {
			if _, err := database.Exec(`DROP TRIGGER fail_gacha_review`); err != nil {
				t.Fatal(err)
			}
		}
	}
	stopFailure := failAudit("gacha-draft:60300001")
	if code, _ := call("/api/gacha-pools", map[string]any{"source_id": 60201206, "name": "x", "expected_revision": 0}); code != 500 {
		t.Fatalf("injected creation failure: %d", code)
	}
	stopFailure()
	if doc, err := operations.storage.ReadDocument(customGachaKey); err != nil || doc.Revision != 0 || operations.customGachaRevision != 0 || len(operations.customGachas) != 0 {
		t.Fatal("failed creation left a pool or changed runtime", err)
	}

	// Copying one variant copies its whole group (ticket, single and ten-draw) as unpublished drafts.
	if code, _ := call("/api/gacha-pools", map[string]any{"source_id": 60201206, "name": "x", "expected_revision": 0}); code != 200 {
		t.Fatalf("lucky-bag group copy status=%d", code)
	}
	if code, _ := call("/api/gacha-pools", map[string]any{"source_id": 60201301, "name": "x", "expected_revision": 1}); code != 400 {
		t.Fatalf("mixed reward pool copied: %d", code)
	}
	if code, _ := call("/api/gacha-pools", map[string]any{"source_id": 60200211, "name": "秋季限定", "expected_revision": 0}); code != 409 {
		t.Fatalf("stale pool list accepted: %d", code)
	}
	code, created := call("/api/gacha-pools", map[string]any{"source_id": 60200211, "name": "秋季限定", "expected_revision": 1})
	if code != 200 || created["group_id"] != float64(60300002) || len(created["gacha_ids"].([]any)) != 3 {
		t.Fatalf("create: %d %v", code, created)
	}
	draft, err := operations.storage.ReadDocument("gacha-draft:60300004")
	var draftConfig AdminGachaConfig
	if err != nil || json.Unmarshal(draft.Payload, &draftConfig) != nil || draftConfig.Name != "秋季限定" || draftConfig.Price != 500 {
		t.Fatalf("ten-draw variant draft: %+v %v", draftConfig, err)
	}
	active, _ := operations.GachaPublication()
	if operations.GachaIDPublished(60300002, active) {
		t.Fatal("a new pool is visible before its group is opened")
	}
	if operations.GachaIDPublished(60300002, map[int]struct{}{60300002: {}}) {
		t.Fatal("opening a group exposed an unpublished template")
	}

	// Accounts append operator pools (game tests); a saved account keeps their play counts and passes save
	// validation although the catalog lacks them.
	userID := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 1)
	state, err := accounts.LoadState(userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range operations.gachaConfigurations {
		if config.Operator && config.Profile.GroupID == 60300002 {
			profile := config.Profile
			if profile.GachaID == 60300003 {
				profile.PlayCount = 4
			}
			state.Gachas = append(state.Gachas, profile)
		}
	}
	if err := accounts.PersistState(userID, state); err != nil {
		t.Fatalf("account with an operator pool failed to save: %v", err)
	}
	if state, err = accounts.LoadState(userID); err != nil || state.OperatorGachaPlays[60300003] != 4 || findProfile(state.Gachas, 60300003) != nil {
		t.Fatalf("operator pool plays not restored: %v %v", state.OperatorGachaPlays, err)
	}

	// Deletion is soft, covers the group, blocks publication and survives restart; restore reverses it.
	if code, _ := call("/api/gacha-pools/60200211/delete", map[string]any{"expected_revision": 2}); code != 400 {
		t.Fatalf("built-in pool deleted: %d", code)
	}
	if code, _ := call("/api/gacha-pools/60300003/delete", map[string]any{"expected_revision": 2}); code != 200 {
		t.Fatalf("delete status=%d", code)
	}
	for _, config := range operations.gachaConfigurations {
		if config.Profile.GroupID == 60300002 && !config.Disabled {
			t.Fatal("deleted group variant still enabled")
		}
	}
	revision := 0
	if _, err := operations.setGachaPublication(gachaPublication{GroupIDs: []int{60300002}, ExpectedRevision: &revision}); err == nil {
		t.Fatal("deleted pool group published")
	}
	restarted, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil || !restarted.customGachas[60300004].Deleted || restarted.gachaBases[60300004].Name != "秋季限定" {
		t.Fatalf("operator pools after restart: %+v %v", restarted.customGachas, err)
	}
	restartedAPI := *admin
	restartedAPI.operations = restarted
	if err := restartedAPI.validateStoredGachas(); err != nil {
		t.Fatalf("deleted pool blocked server initialization: %v", err)
	}
	if code, _ := call("/api/gacha-pools/60300002/restore", map[string]any{"expected_revision": 3}); code != 200 || operations.customGachas[60300004].Deleted {
		t.Fatalf("restore status=%d", code)
	}
	if presets := admin.customGachaPresets(); len(presets) != 2 || len(presets[1].GachaIDs) != 3 {
		t.Fatalf("publication presets: %+v", presets)
	}
	// A pool deleted while open stays in the saved publication, so other publication edits still save.
	if _, err := operations.setGachaPublication(gachaPublication{GroupIDs: []int{60300002}, ExpectedRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("/api/gacha-pools/60300002/delete", map[string]any{"expected_revision": 4}); code != 200 {
		t.Fatalf("second delete status=%d", code)
	}
	published := 1
	if _, err := operations.setGachaPublication(gachaPublication{GroupIDs: []int{60200201, 60300002}, ExpectedRevision: &published}); err != nil {
		t.Fatalf("publication with a deleted but already open pool: %v", err)
	}
	if code, _ := call("/api/gacha-pools/60300002/restore", map[string]any{"expected_revision": 5}); code != 200 {
		t.Fatalf("second restore status=%d", code)
	}

	// Covers are content-addressed PNG/JPEG files and must exist before a pool can reference them.
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 64))); err != nil {
		t.Fatal(err)
	}
	upload := map[string]any{"data_base64": base64.StdEncoding.EncodeToString(encoded.Bytes())}
	code, uploaded := call("/api/gacha-covers", upload)
	coverPath, _ := uploaded["cover_path"].(string)
	if code != 200 || !gachaCoverPathPattern.MatchString(coverPath) {
		t.Fatalf("cover upload: %d %v", code, uploaded)
	}
	if _, again := call("/api/gacha-covers", upload); again["cover_path"] != coverPath {
		t.Fatal("identical cover stored under a different name")
	}
	var tiny bytes.Buffer
	_ = png.Encode(&tiny, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	for _, data := range [][]byte{tiny.Bytes(), []byte("not an image"), encoded.Bytes()[:33]} {
		if code, _ := call("/api/gacha-covers", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(data)}); code != 400 {
			t.Fatalf("invalid cover accepted: %d", code)
		}
	}
	var uploads sync.WaitGroup
	for range 8 {
		uploads.Add(1)
		go func() {
			defer uploads.Done()
			if code, result := call("/api/gacha-covers", upload); code != 200 || result["cover_path"] != coverPath {
				t.Errorf("concurrent upload: %d %v", code, result)
			}
		}()
	}
	uploads.Wait()
	draftConfig.CoverPath = coverPath
	if _, err := admin.validateGachaConfig(draftConfig); err != nil {
		t.Fatal(err)
	}
	draftConfig.CoverPath = "gacha-covers/" + filepath.Base(coverPath)[:10] + "0000000000000000000000000000000000000000000000000000000.png"
	if _, err := admin.validateGachaConfig(draftConfig); err == nil {
		t.Fatal("missing cover file accepted")
	}
	// Draw variants publish atomically, with independent content and one cover/quota.
	router.Post("/api/gacha-editor/group/{action:draft|publish|discard}", admin.gachaGroupAction)
	router.Post("/api/gacha-editor/{action:publish}", admin.gachaEditorAction)
	group := make([]AdminGachaConfig, 3)
	expected := map[int]gachaGroupExpectation{}
	for i := range group {
		doc, _ := operations.storage.ReadDocument("gacha-draft:" + strconv.Itoa(60300002+i))
		if err := json.Unmarshal(doc.Payload, &group[i]); err != nil {
			t.Fatal(err)
		}
		group[i].CoverPath = coverPath
		group[i].PlayCountMax = 2
		expected[60300002+i] = gachaGroupExpectation{Draft: doc.Revision}
	}
	group[2].CoverPath = ""
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected}); code != 400 {
		t.Fatalf("variants with different covers saved: %d", code)
	}
	group[2].CoverPath = coverPath
	group[2].PlayCountMax = 3
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected}); code != 400 {
		t.Fatalf("different group quotas accepted: %d", code)
	}
	group[2].PlayCountMax = 2
	group[2].StartUnix = 1893456000
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected}); code != 400 {
		t.Fatalf("different variant schedules accepted: %d", code)
	}
	for i := range group {
		group[i].StartUnix = 1893456000
	}
	group[2].CardIDs = group[2].CardIDs[:1]
	group[2].Weights = []int{9}
	for i := range group {
		group[i].Closed = true
	}
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected}); code != 400 {
		t.Fatalf("pool with every draw method closed saved: %d", code)
	}
	group[0].Closed, group[2].Closed = false, false
	stale := maps.Clone(expected)
	stale[60300003] = gachaGroupExpectation{}
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": stale}); code != 409 {
		t.Fatalf("stale variant draft accepted: %d", code)
	}
	stopFailure = failAudit("gacha-draft:60300003")
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected}); code != 500 {
		t.Fatalf("injected draft failure: %d", code)
	}
	stopFailure()
	if doc, _ := operations.storage.ReadDocument("gacha-draft:60300002"); doc.Revision != expected[60300002].Draft {
		t.Fatal("part of the draft committed")
	}
	code, saved := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected})
	if code != 200 {
		t.Fatalf("group draft: %d %v", code, saved)
	}
	for id := range expected {
		doc := saved["documents"].(map[string]any)[strconv.Itoa(id)].(map[string]any)
		expected[id] = gachaGroupExpectation{Draft: int(doc["revision"].(float64)), SHA256: doc["sha256"].(string)}
	}
	// A pre-upgrade browser must not bypass whole-pool publication.
	if code, _ := call("/api/gacha-editor/publish", map[string]any{"config": group[0], "expected_revision": expected[60300002].Draft, "expected_live_revision": 0, "sha256": expected[60300002].SHA256}); code != 400 {
		t.Fatalf("legacy per-variant publish bypassed group validation: %d", code)
	}
	stopFailure = failAudit("gacha-draft:60300003")
	if code, _ := call("/api/gacha-editor/group/publish", map[string]any{"group_id": 60300002, "expected": expected}); code != 500 {
		t.Fatalf("injected publish failure: %d", code)
	}
	stopFailure()
	if doc, _ := operations.storage.ReadDocument("gacha-live:60300002"); doc.Revision != 0 || operations.GachaIDPublished(60300002, map[int]struct{}{60300002: {}}) {
		t.Fatal("part of the publication committed to DB or memory")
	}
	if code, published := call("/api/gacha-editor/group/publish", map[string]any{"group_id": 60300002, "expected": expected}); code != 200 {
		t.Fatalf("group publish: %d %v", code, published)
	}
	for _, config := range operations.gachaConfigurations {
		if config.Profile.GroupID == 60300002 && (config.Profile.CoverPath != coverPath || config.Disabled != (config.Profile.GachaID == 60300003)) {
			t.Fatalf("variant %d published without the pool cover or with the wrong open state", config.Profile.GachaID)
		}
	}
	for id := range expected {
		draft, _ := operations.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
		live, _ := operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
		if gachaDraftExists(draft) {
			t.Fatal("published draft body retained", id)
		}
		expected[id] = gachaGroupExpectation{Draft: draft.Revision, Live: live.Revision}
	}
	group[0].Name = "pending edit"
	codes := make(chan int, 2)
	for range 2 {
		uploads.Add(1)
		go func() {
			defer uploads.Done()
			code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected})
			codes <- code
		}()
	}
	uploads.Wait()
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
	reloaded, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range reloaded.gachaConfigurations {
		if c.Profile.GachaID == group[2].GachaID && (len(c.Profile.CardIDs) != 1 || c.Profile.CardWeights[0] != 9 || c.StartUnix != group[2].StartUnix || c.Profile.PlayCountMax != 2) {
			t.Fatalf("independent variant or quota lost on reload: %+v", c)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("group concurrency: %d successes / %d conflicts", success, conflict)
	}
	for id, expectation := range expected {
		expectation.Draft++
		expected[id] = expectation
	}
	stopFailure = failAudit("gacha-draft:60300003")
	if code, _ := call("/api/gacha-editor/group/discard", map[string]any{"group_id": 60300002, "expected": expected}); code != 500 {
		t.Fatalf("injected discard failure: %d", code)
	}
	stopFailure()
	if code, result := call("/api/gacha-editor/group/discard", map[string]any{"group_id": 60300002, "expected": expected}); code != 200 {
		t.Fatalf("discard after rollback: %d %v", code, result)
	}
	for id := range expected {
		draft, _ := operations.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
		live, _ := operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
		if gachaDraftExists(draft) || live.Revision != expected[id].Live || draft.Revision != expected[id].Draft+1 {
			t.Fatal("discard retained draft content or changed the live config/version", id)
		}
	}
	if code, _ := call("/api/gacha-editor/group/draft", map[string]any{"group_id": 60300002, "configs": group, "expected": expected}); code != 409 {
		t.Fatal("old editor recreated discarded drafts", code)
	}
	served := httptest.NewRecorder()
	router.ServeHTTP(served, httptest.NewRequest(http.MethodGet, "/"+coverPath, nil))
	stored, _ := os.ReadFile(filepath.Join(admin.gachaCoverDir, filepath.Base(coverPath)))
	if served.Code != 200 || served.Header().Get("Content-Type") != "image/png" || !bytes.Equal(served.Body.Bytes(), stored) {
		t.Fatalf("serve cover: %d %q", served.Code, served.Header().Get("Content-Type"))
	}

	for input, want := range map[[2]string]string{
		{"server", ""}: "", {"storage", "https://cdn.example.com/game"}: "https://cdn.example.com/game/",
		{"storage", ""}: "!", {"storage", "ftp://cdn.example.com/"}: "!", {"storage", "https://u:p@cdn.example.com/"}: "!",
		{"storage", "https://cdn.example.com/?v=1"}: "!", {"s3", "https://cdn.example.com/"}: "!",
	} {
		got, err := validGachaCoverBase(input[0], input[1])
		if (want == "!") != (err != nil) || (err == nil && got != want) {
			t.Fatalf("cover base %v = %q, %v", input, got, err)
		}
	}
	// Permanent retirement shares the metadata transaction, preserves unrelated
	// counters and advances a durable allocation cursor instead of recycling IDs.
	state, err = accounts.LoadState(userID)
	if err != nil {
		t.Fatal(err)
	}
	state.OperatorGachaPlays[60300001] = 9
	if err = accounts.PersistState(userID, state); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("/api/gacha-pools/60300002/delete", map[string]any{"expected_revision": operations.customGachaRevision}); code != 200 {
		t.Fatal("recycle before purge", code)
	}
	preview, err := admin.purgeGacha(context.Background(), 60300002, "")
	if err != nil || preview.Accounts != 1 || len(preview.GachaIDs) != 3 {
		t.Fatalf("purge preview %+v %v", preview, err)
	}
	stopFailure = failAudit("60300002")
	if _, err = admin.purgeGacha(context.Background(), 60300002, preview.Digest); err == nil {
		t.Fatal("injected purge failure ignored")
	}
	stopFailure()
	rollback, err := accounts.LoadState(userID)
	if err != nil || rollback.OperatorGachaPlays[60300003] != 4 || !operations.customGachas[60300003].Deleted {
		t.Fatal("purge failed to roll back all owners", err)
	}
	result, err := admin.purgeGacha(context.Background(), 60300002, preview.Digest)
	if err != nil || !result.Applied || result.Backup == "" {
		t.Fatalf("purge %+v %v", result, err)
	}
	if _, err = os.Stat(result.Backup); err != nil {
		t.Fatal("purge backup missing", err)
	}
	for _, id := range preview.GachaIDs {
		for _, prefix := range []string{"gacha-live:", "gacha-draft:"} {
			doc, err := operations.storage.ReadDocument(prefix + strconv.Itoa(id))
			if err != nil || doc.Revision != 0 {
				t.Fatal("purged document retained", prefix, id, err)
			}
		}
	}
	after, err := accounts.LoadState(userID)
	if err != nil || after.OperatorGachaPlays[60300003] != 0 || after.OperatorGachaPlays[60300001] != 9 || !reflect.DeepEqual(after.User, state.User) || !reflect.DeepEqual(after.Cards, state.Cards) {
		t.Fatal("purge altered unrelated player data or retained its plays", err)
	}
	if operations.GachaIDPublished(60300003, map[int]struct{}{60300002: {}}) {
		t.Fatal("stale retired gacha accepted")
	}
	active, err = operations.GachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active[60300002]; ok {
		t.Fatal("retired publication reference retained")
	}
	reloaded, err = NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil || reloaded.nextCustomGachaID() <= 60300004 {
		t.Fatal("retired IDs reused after restart", err)
	}
}

func findProfile(gachas []gamestate.GachaProfile, id int) *gamestate.GachaProfile {
	for i := range gachas {
		if gachas[i].GachaID == id {
			return &gachas[i]
		}
	}
	return nil
}

func TestRuleTemplatesCreatePublishReloadAndKeepDailyClaim(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	// Give fixture cards explicit jobs, including common cards, before loading operations.
	catalog.DeckRankPolicy.Cards = map[int]gamestate.CardRankRule{}
	for _, p := range catalog.Gachas {
		for i, id := range p.CardIDs {
			catalog.DeckRankPolicy.Cards[id] = gamestate.CardRankRule{ArthurType: int8(i % 5)}
		}
	}
	accounts.Database().SetCatalog(catalog)
	o, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]AdminCatalogEntry{}
	for _, base := range catalog.Gachas {
		for _, id := range base.CardIDs {
			entries[adminCatalogKey(6, id)] = AdminCatalogEntry{Rarity: 6, ResourceState: "ready", FameMax: 100, ArthurType: catalog.DeckRankPolicy.Cards[id].ArthurType}
		}
		entries[adminCatalogKey(8, base.PayTypeID)] = AdminCatalogEntry{ResourceState: "ready"}
		pools := [][]gamestate.WeightedReward{base.RewardPool}
		for _, step := range base.Steps {
			pools = append(pools, step.RewardPool)
		}
		for _, pool := range pools {
			for _, item := range pool {
				entries[adminCatalogKey(item.Reward.Type, item.Reward.RewardTypeID)] = AdminCatalogEntry{Rarity: 6, ResourceState: "ready", FameMax: 100}
			}
		}
	}
	admin := &API{operations: o, catalogByKey: entries}
	router := chi.NewRouter()
	router.Get("/api/gacha-editor", admin.gachaEditorList)
	router.Post("/api/gacha-pools", admin.createCustomGacha)
	router.Post("/api/gacha-editor/group/{action:publish}", admin.gachaGroupAction)
	call := func(path string, body any) (int, map[string]any) {
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var result map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return w.Code, result
	}
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/gacha-editor", nil))
	var list struct {
		RuleTemplates []gachaRuleTemplate `json:"rule_templates"`
	}
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &list) != nil || len(list.RuleTemplates) == 0 {
		t.Fatal("missing rule templates")
	}
	for _, body := range []map[string]any{
		{"rule_template_id": 90000200}, {"rule_template_id": -1},
		{"rule_template_id": 60201302, "source_id": 60200211},
	} {
		body["name"], body["expected_revision"] = "invalid", 0
		if code, _ := call("/api/gacha-pools", body); code != 400 {
			t.Fatal("invalid template accepted", code, body)
		}
	}
	// A template must not inherit mutable published contents from its example pool.
	changed := AdminGachaConfigFromProfile(o.gachaBases[60200211])
	changed.Price = 37
	changed.Closed = true
	if _, err := o.storage.WriteDocument("gacha-live:60200211", 0, changed, "test"); err != nil {
		t.Fatal(err)
	}
	created := map[int]int{}
	for _, template := range list.RuleTemplates {
		if len(template.Summary) == 0 {
			t.Fatal("rule picker has no summary")
		}
		code, result := call("/api/gacha-pools", map[string]any{"rule_template_id": template.ID, "name": "模板测试", "expected_revision": o.customGachaRevision})
		if code != 200 {
			t.Fatalf("template %d: %d %v", template.ID, code, result)
		}
		group := int(result["group_id"].(float64))
		created[template.ID] = group
		ids := result["gacha_ids"].([]any)
		if len(ids) != len(template.GachaIDs) {
			t.Fatal("missing template variant")
		}
		expected := map[int]gachaGroupExpectation{}
		for i, raw := range ids {
			id := int(raw.(float64))
			base := o.gachaBases[id]
			source := o.gachaBases[template.GachaIDs[i]]
			if base.CardNum != source.CardNum || base.CardNumMax != source.CardNumMax || base.UnownedOnly != source.UnownedOnly ||
				base.DailyFirstFree != source.DailyFirstFree || base.GuaranteedCount != source.GuaranteedCount ||
				!reflect.DeepEqual(base.Steps, source.Steps) || !reflect.DeepEqual(base.Gifts, source.Gifts) {
				t.Fatal("template lost its rules", template.ID)
			}
			doc, err := o.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
			var config AdminGachaConfig
			if err != nil || json.Unmarshal(doc.Payload, &config) != nil || config.Closed || config.Price != source.Price {
				t.Fatal("template inherited live edits", err)
			}
			expected[id] = gachaGroupExpectation{Draft: doc.Revision, SHA256: doc.SHA256}
			if o.GachaIDPublished(id, map[int]struct{}{group: {}}) {
				t.Fatal("unpublished template exposed")
			}
		}
		if code, result := call("/api/gacha-editor/group/publish", map[string]any{"group_id": group, "expected": expected}); code != 200 {
			t.Fatalf("template publish: %d %v", code, result)
		}
	}
	for _, group := range []int{60200101, 60200201, 60201102, 60201206, 60201302, 60201401} {
		if created[group] == 0 {
			t.Fatal("supported rule family missing", group)
		}
	}
	if code, _ := call("/api/gacha-pools", map[string]any{"rule_template_id": created[60200201], "name": "invalid", "expected_revision": o.customGachaRevision}); code != 400 {
		t.Fatal("operator pool offered as immutable template")
	}
	// The stock picker needs one row per profession/draw count, and sends that exact ID.
	if code, _ := call("/api/gacha-pools", map[string]any{"rule_template_id": 60200201, "professions": []int{1, 5}, "name": "ambiguous", "expected_revision": o.customGachaRevision}); code != 400 {
		t.Fatal("ambiguous payment alternatives accepted for the native picker")
	}
	code, professional := call("/api/gacha-pools", map[string]any{"rule_template_id": 60200301, "professions": []int{1, 2, 3, 4, 5}, "name": "职业测试", "expected_revision": o.customGachaRevision})
	if code != 200 || len(professional["gacha_ids"].([]any)) != 10 {
		t.Fatalf("profession creation: %d %v", code, professional)
	}
	professionGroup := int(professional["group_id"].(float64))
	expected := map[int]gachaGroupExpectation{}
	for _, raw := range professional["gacha_ids"].([]any) {
		id := int(raw.(float64))
		base := o.gachaBases[id]
		if base.ArthurType < 1 || base.ArthurType > 5 || base.GroupID != professionGroup || len(base.CardIDs) == 0 {
			t.Fatalf("invalid profession variant: %+v", base)
		}
		for _, card := range base.CardIDs {
			job := catalog.DeckRankPolicy.Cards[card].ArthurType
			if base.ArthurType != 5 && job != 0 && job != base.ArthurType {
				t.Fatal("other profession leaked into pool", base.ArthurType, job)
			}
		}
		doc, _ := o.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
		expected[id] = gachaGroupExpectation{Draft: doc.Revision, SHA256: doc.SHA256}
	}
	if code, result := call("/api/gacha-editor/group/publish", map[string]any{"group_id": professionGroup, "expected": expected}); code != 200 {
		t.Fatalf("profession publish: %d %v", code, result)
	}
	restarted, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	admin.operations = restarted
	if err := admin.validateStoredGachas(); err != nil {
		t.Fatal("template restart", err)
	}
	for id := range expected {
		if !reflect.DeepEqual(restarted.gachaBases[id], o.gachaBases[id]) {
			t.Fatal("profession or card filtering lost after restart", id)
		}
	}
	if len(admin.gachaRuleTemplates()) != len(list.RuleTemplates) {
		t.Fatal("templates grow after creating pools")
	}
	// First free draw, SQLite reload and a payment round trip must not reset the daily claim.
	uid := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 1)
	state, err := accounts.LoadState(uid)
	if err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = 9
	state.User.FriendPoint = 0
	master, err := masterdata.LoadCardRuntimeMaster(testfixture.WriteTestCardMaster(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := masterdata.ApplyCardRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	avatar, err := masterdata.LoadAvatarRuntimeMaster(testfixture.WriteTestAvatarMaster(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := masterdata.ApplyAvatarRuntimeMaster(&state, avatar); err != nil {
		t.Fatal(err)
	}
	item, err := masterdata.LoadItemRuntimeMaster(testfixture.WriteTestItemMaster(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := masterdata.ApplyItemRuntimeMaster(&state, item); err != nil {
		t.Fatal(err)
	}
	login, err := masterdata.LoadLoginBonusRuntimeMaster("../../config/cn602-login-bonus-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := masterdata.ApplyLoginBonusRuntimeMaster(&state, login); err != nil {
		t.Fatal(err)
	}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	accounts.Database().SetCatalog(state)
	// Published templates must also survive the player-save boundary, including
	// the mixed-profession row (ArthurType 5), before any draw is performed.
	account.ApplyGachaConfiguration(restarted.gachaRevision, restarted.gachaConfigurations)
	if err := accounts.PersistState(uid, account.Snapshot(state)); err != nil {
		t.Fatal("published templates cannot persist in a player account", err)
	}
	id := created[60200101]
	profile := restarted.gachaBases[id]
	configs := []game.GachaConfiguration{{Operator: true, Profile: profile}}
	account.ApplyGachaConfiguration(1, configs)
	if _, err := account.PlayGacha(id, 2, nil); err != nil {
		t.Fatal("first free draw failed", err)
	}
	if err := accounts.PersistState(uid, account.Snapshot(state)); err != nil {
		t.Fatal("save daily claim", err)
	}
	state, err = accounts.LoadState(uid)
	if err != nil {
		t.Fatal("reload operator daily claim", err)
	}
	account, err = game.New(state)
	if err != nil {
		t.Fatal("load account before operator profiles", err)
	}
	account.ApplyGachaConfiguration(2, configs)
	if _, err := account.PlayGacha(id, 2, nil); err == nil {
		t.Fatal("restart granted another free draw")
	}
	pay := AdminGachaConfigFromProfile(profile)
	pay.PayType = 3
	paid := adminConfiguredGacha(profile, pay)
	paid.Operator = true
	account.ApplyGachaConfiguration(3, []game.GachaConfiguration{paid})
	if err := accounts.PersistState(uid, account.Snapshot(state)); err != nil {
		t.Fatal("payment change lost daily claim", err)
	}
	account.ApplyGachaConfiguration(4, configs)
	if _, err := account.PlayGacha(id, 2, nil); err == nil {
		t.Fatal("payment round trip granted another free draw")
	}
}
