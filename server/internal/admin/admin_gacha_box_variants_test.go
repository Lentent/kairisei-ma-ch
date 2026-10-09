package admin

import (
	"encoding/json"
	"maps"
	"reflect"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func newBoxGachaVariantFixture(t *testing.T) (*gachaVariantFixture, *accountstore.Accounts, int, AdminGachaConfig) {
	t.Helper()
	a, accounts, handler := customGachaTestAPI(t)
	id := createCurrencyBox(t, a, handler)
	config := AdminGachaConfigFromProfile(a.operations.gachaBases[id])
	fillVariableBoxTestConfig(&config)
	draft := saveVariableBoxTestDraft(t, handler, config, 0)
	publishVariableBoxTestDraft(t, handler, id, draft, 0)
	router := chi.NewRouter()
	router.Post("/api/gacha-variants", a.createGachaVariant)
	router.Post("/api/gacha-pools", a.createCustomGacha)
	router.Post("/api/gacha-create", a.gachaCreate)
	router.Post("/api/gacha-editor/group/{action:draft|publish|discard}", a.gachaGroupAction)
	router.Post("/api/gacha-editor/{action:preview|draft|publish}", a.gachaEditorAction)
	return &gachaVariantFixture{admin: a, router: router, builtins: []gamestate.GachaProfile{a.operations.gachaBases[60201301]}}, accounts, id, config
}

func saveBoxVariantSourceDraft(t *testing.T, f *gachaVariantFixture, config AdminGachaConfig) {
	t.Helper()
	draft := f.document(t, "gacha-draft:"+strconv.Itoa(config.GachaID))
	if code, result := f.call(t, "/api/gacha-editor/draft", map[string]any{"config": config, "expected_revision": draft.Revision}); code != 200 {
		t.Fatalf("save box source draft: %d %v", code, result)
	}
}

func TestGachaBoxVariantSharedDraftPublishReloadAndGroupCopy(t *testing.T) {
	for _, count := range []int{10, 50} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			testGachaBoxVariantLifecycle(t, count)
		})
	}
}

func testGachaBoxVariantLifecycle(t *testing.T, count int) {
	f, accounts, sourceID, sourceConfig := newBoxGachaVariantFixture(t)
	// The source's saved draft must take precedence over its currently live rewards.
	sourceConfig.BoxRounds[0].Rewards[0].Stock, sourceConfig.BoxRounds[0].Rewards[1].Stock = 6, 4
	saveBoxVariantSourceDraft(t, f, sourceConfig)
	draftKey, liveKey := "gacha-draft:"+strconv.Itoa(sourceID), "gacha-live:"+strconv.Itoa(sourceID)
	beforeDraft, beforeLive := f.document(t, draftKey), f.document(t, liveKey)
	request := f.request(t, sourceID, count)
	request["name"], request["pay_type"], request["pay_typeid"], request["price"] = "箱池连抽", 4, 10, 2
	code, result := f.call(t, "/api/gacha-variants", request)
	if code != 200 {
		t.Fatalf("create box ten-draw method: %d %v", code, result)
	}
	id := int(result["gacha_id"].(float64))
	base := f.admin.operations.gachaBases[id]
	if result["group_id"] != float64(sourceID) || base.GroupID != sourceID || base.CardNum != count || base.CardNumMax != count || base.PayType != 4 || base.PayTypeID != 10 || base.Price != 2 || !base.FixedDrawCount || f.admin.operations.customGachas[id].RuleVersion != 3 {
		t.Fatalf("box added method changed family/group/payment: %+v %v", base, result)
	}
	var addedDraft AdminGachaConfig
	if err := json.Unmarshal(f.document(t, "gacha-draft:"+strconv.Itoa(id)).Payload, &addedDraft); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(addedDraft.BoxRounds, sourceConfig.BoxRounds) || !reflect.DeepEqual(f.document(t, draftKey), beforeDraft) || !reflect.DeepEqual(f.document(t, liveKey), beforeLive) {
		t.Fatal("box method did not copy saved templates or edited the source")
	}
	active := map[int]struct{}{sourceID: {}}
	if f.admin.operations.GachaIDPublished(id, active) || !f.admin.operations.GachaIDPublished(sourceID, active) {
		t.Fatal("new box draft exposed before publication or hid the original")
	}
	// Until shared drafts are published, copying live A plus an unpublished snapshot B is unsafe.
	beforeCopyRevision := f.admin.operations.customGachaRevision
	if code, result := f.call(t, "/api/gacha-pools", map[string]any{"source_id": id, "name": "不一致箱池复制", "expected_revision": beforeCopyRevision}); code != 400 {
		t.Fatalf("group copy accepted inconsistent live/pending box templates: %d %v", code, result)
	}
	if len(f.admin.operations.customGachas) != 2 || f.admin.operations.customGachaRevision != beforeCopyRevision {
		t.Fatal("rejected inconsistent group copy leaked registry entries")
	}
	if code, result := f.call(t, "/api/gacha-create", map[string]any{"source_id": id, "name": "旧入口不一致箱池复制"}); code != 400 {
		t.Fatalf("legacy group copy accepted inconsistent live/pending box templates: %d %v", code, result)
	}
	restarted, err := NewOperations(accounts.Database(), f.builtins)
	if err != nil {
		t.Fatal("restart added box draft", err)
	}
	f.admin.operations = restarted
	if err := f.admin.validateStoredGachas(); err != nil {
		t.Fatal("stored box method is invalid", err)
	}
	if restarted.gachaBases[id].CardNum != count || !reflect.DeepEqual(restarted.gachaBases[id].BoxRounds, base.BoxRounds) || restarted.GachaIDPublished(id, active) {
		t.Fatal("unpublished box snapshot lost after restart")
	}
	// Preserve one group-keyed player inventory through adding and publishing draw methods.
	state, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	oldBox := gamestate.GachaBoxProgress{Round: 1, Remaining: []gamestate.GachaBoxReward{{Stock: 3, Reward: gamestate.Reward{Type: 4, Num: 7, CardSkillLevels: []int16{}}}}}
	state.GachaBoxes = map[int]gamestate.GachaBoxProgress{sourceID: oldBox}
	if err := accounts.PersistState(accountstore.PrimaryUserID, state); err != nil {
		t.Fatal(err)
	}
	// The old per-method API cannot publish just the legacy single-draw root.
	if code, result := f.call(t, "/api/gacha-editor/publish", map[string]any{
		"config": map[string]int{"gacha_id": sourceID}, "expected_revision": beforeDraft.Revision,
		"expected_live_revision": beforeLive.Revision, "sha256": beforeDraft.SHA256,
	}); code != 400 {
		t.Fatalf("single-method API bypassed shared box publication: %d %v", code, result)
	}
	if code, result := f.call(t, "/api/gacha-editor/group/publish", map[string]any{"group_id": sourceID, "expected": f.expectations(t, sourceID)}); code != 200 {
		t.Fatalf("publish box group: %d %v", code, result)
	}
	restarted, err = NewOperations(accounts.Database(), f.builtins)
	if err != nil {
		t.Fatal("restart published box group", err)
	}
	f.admin.operations = restarted
	if !restarted.GachaIDPublished(sourceID, active) || !restarted.GachaIDPublished(id, active) {
		t.Fatal("whole-group publication omitted a box method")
	}
	stored, err := accounts.LoadPersistentState(accountstore.PrimaryUserID)
	if err != nil || !reflect.DeepEqual(stored.GachaBoxes[sourceID], oldBox) || len(stored.GachaBoxes) != 1 {
		t.Fatal("adding/publishing a method changed or duplicated group inventory", err)
	}
	runtime := testfixture.RuntimeState(t)
	runtime.Onboarding.Step, runtime.User.CoinFree = 9, 100
	runtime.Items = []gamestate.Item{{ItemID: 10, Num: 10}}
	runtime.ItemDefinitions = append(runtime.ItemDefinitions, gamestate.ItemDefinition{ItemID: 10, Name: "测试扭蛋券", ItemType: "GACHA_TICKET", MaxOwned: 100})
	runtime.GachaBoxes = gamestate.CloneGachaBoxes(stored.GachaBoxes)
	account, err := game.New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	account.ApplyGachaConfiguration(restarted.gachaRevision, restarted.gachaConfigurations)
	played, err := account.PlayGacha(id, 4, nil)
	if err != nil || len(played.Reward.Rewards) != count {
		t.Fatal("play added box batch method", err)
	}
	progress := account.Snapshot(runtime).GachaBoxes[sourceID]
	wantRound, wantStock := uint64(2), 23-count
	if count == 50 {
		wantRound, wantStock = 3, 73
	}
	if progress.Round != wantRound || gamestate.GachaBoxStock(progress.Remaining) != wantStock || len(account.Snapshot(runtime).GachaBoxes) != 1 {
		t.Fatalf("batch method did not consume the shared old inventory across rounds: %+v", progress)
	}
	if _, err := account.PlayGacha(sourceID, 3, nil); err != nil || gamestate.GachaBoxStock(account.Snapshot(runtime).GachaBoxes[sourceID].Remaining) != wantStock-1 {
		t.Fatal("single-draw method did not continue the same group inventory", err)
	}
	// The old copy API converts snapshots into legacy entries, retaining the box family and counts.
	code, legacyCopied := f.call(t, "/api/gacha-create", map[string]any{"source_id": id, "name": "旧入口复制箱池"})
	if code != 200 || len(legacyCopied["gacha_ids"].([]any)) != 2 {
		t.Fatalf("legacy box group copy: %d %v", code, legacyCopied)
	}
	legacyGroup := int(legacyCopied["group_id"].(float64))
	legacyIDs := legacyCopied["gacha_ids"].([]any)
	// Copy the mixed registry ownership (legacy root + v3 added method) as one new box group.
	code, copied := f.call(t, "/api/gacha-pools", map[string]any{"source_id": id, "name": "复制箱池", "expected_revision": restarted.customGachaRevision})
	if code != 200 || len(copied["gacha_ids"].([]any)) != 2 {
		t.Fatalf("copy box group containing a snapshot variant: %d %v", code, copied)
	}
	copyGroup := int(copied["group_id"].(float64))
	copyIDs := copied["gacha_ids"].([]any)
	restarted, err = NewOperations(accounts.Database(), f.builtins)
	if err != nil {
		t.Fatal("restart copied box group", err)
	}
	f.admin.operations = restarted
	for i, raw := range legacyIDs {
		profile := restarted.gachaBases[int(raw.(float64))]
		if profile.GroupID != legacyGroup || profile.CardNum != []int{1, count}[i] || !reflect.DeepEqual(profile.BoxRounds, sourceConfig.BoxRounds) {
			t.Fatalf("legacy copy lost snapshot box rules on restart: %+v", profile)
		}
	}
	for i, raw := range copyIDs {
		copyID := int(raw.(float64))
		profile := restarted.gachaBases[copyID]
		if profile.GroupID != copyGroup || profile.CardNum != []int{1, count}[i] || !reflect.DeepEqual(profile.BoxRounds, sourceConfig.BoxRounds) {
			t.Fatalf("copied box lost draw count or reward family: %+v", profile)
		}
		if restarted.GachaIDPublished(copyID, map[int]struct{}{copyGroup: {}}) {
			t.Fatal("copied box method exposed before publication")
		}
	}
}

func TestGachaBoxVariantConflictsDuplicateCountAndTemplateMismatch(t *testing.T) {
	f, _, sourceID, config := newBoxGachaVariantFixture(t)
	saveBoxVariantSourceDraft(t, f, config)
	for _, kind := range []string{"registry", "draft", "live", "hash", "membership"} {
		body := f.request(t, sourceID, 10)
		expected := body["expected"].(map[int]gachaGroupExpectation)
		entry := expected[sourceID]
		switch kind {
		case "registry":
			body["expected_revision"] = 999
		case "draft":
			entry.Draft = 0
		case "live":
			entry.Live = 0
		case "hash":
			entry.SHA256 = "stale"
		case "membership":
			delete(expected, sourceID)
		}
		if kind != "membership" {
			expected[sourceID] = entry
		}
		if code, result := f.call(t, "/api/gacha-variants", body); code != 409 {
			t.Fatalf("stale box %s accepted: %d %v", kind, code, result)
		}
	}
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, sourceID, 1)); code != 400 {
		t.Fatalf("duplicate single-draw box method accepted: %d %v", code, result)
	}
	code, result := f.call(t, "/api/gacha-variants", f.request(t, sourceID, 10))
	if code != 200 {
		t.Fatalf("add box method: %d %v", code, result)
	}
	id := int(result["gacha_id"].(float64))
	duplicate := f.request(t, sourceID, 10)
	duplicate["pay_type"], duplicate["price"] = 6, 123
	if code, result := f.call(t, "/api/gacha-variants", duplicate); code != 400 {
		t.Fatalf("different payment allowed duplicate box draw count: %d %v", code, result)
	}
	var added AdminGachaConfig
	if err := json.Unmarshal(f.document(t, "gacha-draft:"+strconv.Itoa(id)).Payload, &added); err != nil {
		t.Fatal(err)
	}
	if code, result := f.call(t, "/api/gacha-editor/preview", map[string]any{"config": added}); code != 200 {
		t.Fatalf("v3 box method did not route to box preview validation: %d %v", code, result)
	}
	if code, result := f.call(t, "/api/gacha-editor/draft", map[string]any{
		"config": added, "expected_revision": f.document(t, "gacha-draft:"+strconv.Itoa(id)).Revision,
	}); code != 400 {
		t.Fatalf("single-method API bypassed shared box draft saving: %d %v", code, result)
	}
	rootChanged := config
	rootChanged.CardNum = 2
	if code, result := f.call(t, "/api/gacha-editor/group/draft", map[string]any{
		"group_id": sourceID, "configs": []AdminGachaConfig{rootChanged, added}, "expected": f.expectations(t, sourceID),
	}); code != 400 {
		t.Fatalf("old box root allowed changing its fixed draw count: %d %v", code, result)
	}
	// Saved templates must remain shared even when an older caller writes a divergent draft.
	added.BoxRounds[0].Rewards[0].Reward.Num++
	doc := f.document(t, "gacha-draft:"+strconv.Itoa(id))
	if _, err := f.admin.operations.storage.WriteDocument("gacha-draft:"+strconv.Itoa(id), doc.Revision, added, "test"); err != nil {
		t.Fatal(err)
	}
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, sourceID, 2)); code != 400 {
		t.Fatalf("new method copied an already-divergent box group: %d %v", code, result)
	}
	if code, result := f.call(t, "/api/gacha-editor/group/draft", map[string]any{
		"group_id": sourceID, "configs": []AdminGachaConfig{config, added}, "expected": f.expectations(t, sourceID),
	}); code != 400 {
		t.Fatalf("group draft accepted divergent box templates: %d %v", code, result)
	}
	if code, result := f.call(t, "/api/gacha-editor/group/publish", map[string]any{
		"group_id": sourceID, "expected": f.expectations(t, sourceID),
	}); code != 400 {
		t.Fatalf("group publish accepted divergent box templates: %d %v", code, result)
	}
}

func TestGachaBoxVariantRejectsMixedCardAndBoxFamilies(t *testing.T) {
	ordinary := ordinaryVariantTestProfile()
	box := gamestate.CloneGachas([]gamestate.GachaProfile{ordinary})[0]
	box.GachaID, box.CardIDs, box.CardWeights = ordinary.GachaID+1, nil, nil
	box.BoxRounds = make([]gamestate.GachaBoxRound, gamestate.GachaBoxTemplates)
	for i := range box.BoxRounds {
		box.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{{Stock: 10, Reward: gamestate.Reward{Type: 4, Num: 7, CardSkillLevels: []int16{}}}}
	}
	f := newGachaVariantFixture(t, ordinary, box)
	for _, source := range []int{ordinary.GachaID, box.GachaID} {
		if code, result := f.call(t, "/api/gacha-variants", f.request(t, source, 10)); code != 400 {
			t.Fatalf("mixed card/box group accepted a method: %d %v", code, result)
		}
	}
	if f.document(t, customGachaKey).Revision != 0 || len(f.admin.operations.customGachas) != 0 {
		t.Fatal("mixed-family rejection changed the registry")
	}
}

func TestGachaBoxVariantPersistenceRollback(t *testing.T) {
	f, accounts, sourceID, _ := newBoxGachaVariantFixture(t)
	o := f.admin.operations
	bases, groups, custom, byID := maps.Clone(o.gachaBases), maps.Clone(o.managedGachaGroups), maps.Clone(o.customGachas), maps.Clone(o.managedGachaGroupByID)
	beforeRevision, beforeGachaRevision, beforeConfigs := o.customGachaRevision, o.gachaRevision, o.gachaConfigurations
	newID := o.nextCustomGachaID()
	db, err := o.storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_box_variant BEFORE INSERT ON cn_admin_audit WHEN NEW.target='gacha-draft:` + strconv.Itoa(newID) + `' BEGIN SELECT RAISE(ABORT,'box variant failure'); END`); err != nil {
		t.Fatal(err)
	}
	code, result := f.call(t, "/api/gacha-variants", f.request(t, sourceID, 10))
	if _, err := db.Exec(`DROP TRIGGER fail_box_variant`); err != nil {
		t.Fatal(err)
	}
	if code != 500 {
		t.Fatalf("box creation transaction failure not reported: %d %v", code, result)
	}
	if f.document(t, customGachaKey).Revision != beforeRevision || f.document(t, "gacha-draft:"+strconv.Itoa(newID)).Revision != 0 || o.customGachaRevision != beforeRevision || o.gachaRevision != beforeGachaRevision || !reflect.DeepEqual(o.gachaBases, bases) || !reflect.DeepEqual(o.managedGachaGroups, groups) || !reflect.DeepEqual(o.customGachas, custom) || !reflect.DeepEqual(o.managedGachaGroupByID, byID) || !reflect.DeepEqual(o.gachaConfigurations, beforeConfigs) {
		t.Fatal("failed box creation leaked persisted or runtime state")
	}
	restarted, err := NewOperations(accounts.Database(), f.builtins)
	if err != nil || len(restarted.gachaGroupMembers(sourceID)) != 1 {
		t.Fatal("failed box method persisted through restart", err)
	}
}

func TestGachaBoxFiftyDrawVariantChoices(t *testing.T) {
	f, _, sourceID, _ := newBoxGachaVariantFixture(t)
	for _, count := range []int{0, 12, 49, 51} {
		if code, result := f.call(t, "/api/gacha-variants", f.request(t, sourceID, count)); code != 400 {
			t.Fatalf("invalid count %d accepted: %d %v", count, code, result)
		}
	}
	code, result := f.call(t, "/api/gacha-variants", f.request(t, sourceID, 50))
	if code != 200 {
		t.Fatalf("add fifty draws: %d %v", code, result)
	}
	id := int(result["gacha_id"].(float64))
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, id, 50)); code != 400 {
		t.Fatalf("duplicate fifty draws accepted: %d %v", code, result)
	}
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, id, 10)); code != 200 {
		t.Fatalf("fifty-draw source cannot add ten draws: %d %v", code, result)
	}
	ordinary := newGachaVariantFixture(t)
	if code, result := ordinary.call(t, "/api/gacha-variants", ordinary.request(t, variantTestSourceID, 50)); code != 400 {
		t.Fatalf("ordinary pool accepted fifty-draw variant: %d %v", code, result)
	}
}
