package admin

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

const variantTestSourceID = 60210001

type gachaVariantFixture struct {
	admin    *API
	router   http.Handler
	builtins []gamestate.GachaProfile
}

func ordinaryVariantTestProfile() gamestate.GachaProfile {
	return gamestate.GachaProfile{
		GachaID: variantTestSourceID, GroupID: variantTestSourceID, Name: "原单抽",
		PublicationKey: "water_coin", PayType: 3, Price: 50, ArthurType: 1,
		CardNum: 1, CardNumMax: 1, CardIDs: []int{1, 2}, CardWeights: []int{1, 3},
		EndTime: 2147483647,
	}
}

func newGachaVariantFixture(t *testing.T, profiles ...gamestate.GachaProfile) *gachaVariantFixture {
	t.Helper()
	if len(profiles) == 0 {
		profiles = []gamestate.GachaProfile{ordinaryVariantTestProfile()}
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), profiles)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o, catalogByKey: map[string]AdminCatalogEntry{
		"6:1":    {Rarity: 6, ResourceState: "ready", FameMax: 100},
		"6:2":    {Rarity: 6, ResourceState: "ready", FameMax: 100},
		"6:3":    {Rarity: 6, ResourceState: "ready", FameMax: 100},
		"8:4000": {Kind: "item", ResourceState: "ready"},
	}}
	router := chi.NewRouter()
	router.Post("/api/gacha-variants", a.createGachaVariant)
	router.Post("/api/gacha-pools", a.createCustomGacha)
	router.Post("/api/gacha-editor/group/{action:draft|publish|discard}", a.gachaGroupAction)
	router.Post("/api/gacha-pools/{gachaID}/{action:delete|restore}", a.changeCustomGachaDeletion)
	return &gachaVariantFixture{admin: a, router: router, builtins: profiles}
}

func (f *gachaVariantFixture) call(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kairisei-Admin-Action", "apply")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("response %d is not JSON: %s", w.Code, w.Body.String())
	}
	return w.Code, result
}

func (f *gachaVariantFixture) document(t *testing.T, key string) accountstore.Document {
	t.Helper()
	doc, err := f.admin.operations.storage.ReadDocument(key)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func (f *gachaVariantFixture) expectations(t *testing.T, groupID int) map[int]gachaGroupExpectation {
	t.Helper()
	expected := map[int]gachaGroupExpectation{}
	for _, id := range f.admin.operations.gachaGroupMembers(groupID) {
		key := strconv.Itoa(id)
		draft, live := f.document(t, "gacha-draft:"+key), f.document(t, "gacha-live:"+key)
		expected[id] = gachaGroupExpectation{Draft: draft.Revision, Live: live.Revision, SHA256: draft.SHA256}
	}
	return expected
}

func (f *gachaVariantFixture) request(t *testing.T, sourceID, count int) map[string]any {
	t.Helper()
	return map[string]any{
		"source_id": sourceID, "card_num": count, "name": "新增十连", "pay_type": 3, "pay_typeid": 0, "price": 500,
		"expected_revision": f.admin.operations.customGachaRevision,
		"expected":          f.expectations(t, f.admin.operations.gachaBases[sourceID].GroupID),
	}
}

func TestGachaVariantSavedDraftPublishAndRestart(t *testing.T) {
	f := newGachaVariantFixture(t)
	o := f.admin.operations
	original := gamestate.CloneGachas(f.builtins)[0]
	live := AdminGachaConfigFromProfile(original)
	live.Price = 61
	if _, err := o.storage.WriteDocument("gacha-live:60210001", 0, live, "test"); err != nil {
		t.Fatal(err)
	}
	if err := o.reloadGachaConfigurations(); err != nil {
		t.Fatal(err)
	}
	saved := AdminGachaConfigFromProfile(original)
	saved.CardIDs, saved.Weights = []int{2, 3}, []int{17, 2}
	saved.CardFames, saved.PlayCountMax = map[int]int{2: 7, 3: 9}, 4
	if _, err := o.storage.WriteDocument("gacha-draft:60210001", 0, saved, "test"); err != nil {
		t.Fatal(err)
	}
	beforeDraft, beforeLive := f.document(t, "gacha-draft:60210001"), f.document(t, "gacha-live:60210001")
	active, err := o.GachaPublication()
	if err != nil || !o.GachaIDPublished(variantTestSourceID, active) {
		t.Fatal("built-in source should start visible", err)
	}
	code, result := f.call(t, "/api/gacha-variants", f.request(t, variantTestSourceID, 10))
	if code != 200 {
		t.Fatalf("add ten-draw method: %d %v", code, result)
	}
	id := int(result["gacha_id"].(float64))
	if id < 70000001 || result["group_id"] != float64(variantTestSourceID) || len(result["gacha_ids"].([]any)) != 1 {
		t.Fatalf("new draw identity changed publication group: %v", result)
	}
	pool := o.customGachas[id]
	profile := o.gachaBases[id]
	if pool.RuleVersion != 3 || profile.GroupID != original.GroupID || profile.CardNum != 10 || profile.CardNumMax != 10 || !profile.FixedDrawCount || profile.ArthurType != original.ArthurType {
		t.Fatalf("draw rule snapshot: %+v %+v", pool, profile)
	}
	draft := f.document(t, "gacha-draft:"+strconv.Itoa(id))
	var copied AdminGachaConfig
	if err := json.Unmarshal(draft.Payload, &copied); err != nil {
		t.Fatal(err)
	}
	if copied.GachaID != id || copied.CardNum != 10 || copied.Price != 500 || copied.PlayCountMax != 4 || !reflect.DeepEqual(copied.CardIDs, saved.CardIDs) || !reflect.DeepEqual(copied.Weights, saved.Weights) || !reflect.DeepEqual(copied.CardFames, saved.CardFames) {
		t.Fatalf("new draft did not copy saved source contents: %+v", copied)
	}
	if !reflect.DeepEqual(f.document(t, "gacha-draft:60210001"), beforeDraft) || !reflect.DeepEqual(f.document(t, "gacha-live:60210001"), beforeLive) || !reflect.DeepEqual(o.gachaBases[variantTestSourceID], original) {
		t.Fatal("adding a draw method edited the source configuration")
	}
	if f.document(t, "gacha-live:"+strconv.Itoa(id)).Revision != 0 || o.GachaIDPublished(id, active) || !o.GachaIDPublished(variantTestSourceID, active) {
		t.Fatal("new draft exposed before publication or hid the original method")
	}
	restarted, err := NewOperations(o.storage, f.builtins)
	if err != nil {
		t.Fatal("restart with an attached draft", err)
	}
	f.admin.operations = restarted
	if !reflect.DeepEqual(restarted.gachaBases[id], profile) || len(restarted.gachaGroupMembers(original.GroupID)) != 2 || restarted.GachaIDPublished(id, active) || !restarted.GachaIDPublished(variantTestSourceID, active) {
		t.Fatal("draft rule/count/visibility changed on restart")
	}
	if err := f.admin.validateStoredGachas(); err != nil {
		t.Fatal("stored attached draft is invalid", err)
	}
	// Whole-group publication consumes both saved drafts and exposes the added method.
	if code, result := f.call(t, "/api/gacha-editor/group/publish", map[string]any{"group_id": original.GroupID, "expected": f.expectations(t, original.GroupID)}); code != 200 {
		t.Fatalf("whole-group publish: %d %v", code, result)
	}
	if !restarted.GachaIDPublished(id, active) || !restarted.GachaIDPublished(variantTestSourceID, active) || gachaDraftExists(f.document(t, "gacha-draft:"+strconv.Itoa(id))) {
		t.Fatal("publication did not expose the new draw or consume its draft")
	}
	loaded, err := NewOperations(o.storage, f.builtins)
	if err != nil || !loaded.GachaIDPublished(id, active) {
		t.Fatal("published attached method lost on restart", err)
	}
	for _, c := range loaded.gachaConfigurations {
		if c.Profile.GachaID == id && (c.Profile.CardNum != 10 || c.Profile.Price != 500 || !reflect.DeepEqual(c.Profile.CardWeights, saved.Weights) || !reflect.DeepEqual(c.Profile.CardFames, saved.CardFames)) {
			t.Fatalf("published config changed on restart: %+v", c.Profile)
		}
	}
}

func TestGachaVariantDuplicateCountIsPerProfession(t *testing.T) {
	single := ordinaryVariantTestProfile()
	other := gamestate.CloneGachas([]gamestate.GachaProfile{single})[0]
	other.GachaID, other.ArthurType, other.CardNum, other.CardNumMax = variantTestSourceID+1, 2, 10, 10
	f := newGachaVariantFixture(t, single, other)
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, single.GachaID, 10)); code != 200 {
		t.Fatalf("different profession prevented adding count: %d %v", code, result)
	}
	for _, id := range []int{single.GachaID, other.GachaID} {
		body := f.request(t, id, 10)
		body["pay_type"], body["pay_typeid"], body["price"] = 4, 4000, 1
		if code, result := f.call(t, "/api/gacha-variants", body); code != 400 {
			t.Fatalf("duplicate profession/count accepted with another payment: %d %v", code, result)
		}
	}
	if len(f.admin.operations.gachaGroupMembers(single.GroupID)) != 3 || f.admin.operations.customGachaRevision != 1 {
		t.Fatal("rejected duplicate changed registry")
	}
}

func TestGachaVariantRejectsStaleVersions(t *testing.T) {
	for _, kind := range []string{"registry", "membership", "draft", "live", "hash"} {
		t.Run(kind, func(t *testing.T) {
			f := newGachaVariantFixture(t)
			config := AdminGachaConfigFromProfile(f.builtins[0])
			for _, key := range []string{"gacha-draft:60210001", "gacha-live:60210001"} {
				if _, err := f.admin.operations.storage.WriteDocument(key, 0, config, "test"); err != nil {
					t.Fatal(err)
				}
			}
			body := f.request(t, variantTestSourceID, 10)
			expected := body["expected"].(map[int]gachaGroupExpectation)
			entry := expected[variantTestSourceID]
			switch kind {
			case "registry":
				body["expected_revision"] = 1
			case "membership":
				delete(expected, variantTestSourceID)
			case "draft":
				entry.Draft = 0
			case "live":
				entry.Live = 0
			case "hash":
				entry.SHA256 = "stale"
			}
			if kind != "membership" {
				expected[variantTestSourceID] = entry
			}
			if code, result := f.call(t, "/api/gacha-variants", body); code != 409 {
				t.Fatalf("stale %s accepted: %d %v", kind, code, result)
			}
			if len(f.admin.operations.customGachas) != 0 || f.document(t, customGachaKey).Revision != 0 {
				t.Fatal("stale request persisted a new method")
			}
		})
	}
	// A browser that refreshes only the registry revision still has stale group membership.
	f := newGachaVariantFixture(t)
	old := f.request(t, variantTestSourceID, 2)
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, variantTestSourceID, 10)); code != 200 {
		t.Fatalf("setup add failed: %d %v", code, result)
	}
	old["expected_revision"] = f.admin.operations.customGachaRevision
	if code, result := f.call(t, "/api/gacha-variants", old); code != 409 {
		t.Fatalf("stale group accepted after another method was added: %d %v", code, result)
	}
}

func TestGachaVariantRejectsSpecialRuleFamilies(t *testing.T) {
	mutations := map[string]func(*gamestate.GachaProfile){
		"box":        func(p *gamestate.GachaProfile) { p.BoxRounds = []gamestate.GachaBoxRound{{}} },
		"mixed":      func(p *gamestate.GachaProfile) { p.RewardPool = []gamestate.WeightedReward{{Weight: 1}} },
		"stages":     func(p *gamestate.GachaProfile) { p.Steps = []gamestate.GachaStep{{Price: 1}} },
		"daily":      func(p *gamestate.GachaProfile) { p.DailyFirstFree = true },
		"unowned":    func(p *gamestate.GachaProfile) { p.UnownedOnly = true },
		"selfselect": func(p *gamestate.GachaProfile) { p.UserSelectMax = 1 },
		"variable":   func(p *gamestate.GachaProfile) { p.CardNumMax = 10 },
		"reserved":   func(p *gamestate.GachaProfile) { p.CategoryNum = 10000 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			profile := ordinaryVariantTestProfile()
			mutate(&profile)
			f := newGachaVariantFixture(t, profile)
			if code, result := f.call(t, "/api/gacha-variants", f.request(t, profile.GachaID, 10)); code != 400 {
				t.Fatalf("special %s rule accepted: %d %v", name, code, result)
			}
			if len(f.admin.operations.customGachas) != 0 {
				t.Fatal("rejected special pool changed registry")
			}
		})
	}
}

func TestGachaVariantTransactionRollsBackAndCannotDeleteBuiltInGroup(t *testing.T) {
	f := newGachaVariantFixture(t)
	o := f.admin.operations
	bases, groups, byID := maps.Clone(o.gachaBases), maps.Clone(o.managedGachaGroups), maps.Clone(o.managedGachaGroupByID)
	beforeConfigs, beforeRevision, beforeNext := o.gachaConfigurations, o.gachaRevision, o.nextCustomGachaID()
	db, err := o.storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	// Abort the second document's audit insert after the registry was written in the transaction.
	if _, err := db.Exec(`CREATE TRIGGER fail_gacha_variant BEFORE INSERT ON cn_admin_audit WHEN NEW.target='gacha-draft:70000001' BEGIN SELECT RAISE(ABORT,'variant failure'); END`); err != nil {
		t.Fatal(err)
	}
	code, result := f.call(t, "/api/gacha-variants", f.request(t, variantTestSourceID, 10))
	if _, err := db.Exec(`DROP TRIGGER fail_gacha_variant`); err != nil {
		t.Fatal(err)
	}
	if code != 500 {
		t.Fatalf("transaction failure not surfaced: %d %v", code, result)
	}
	if f.document(t, customGachaKey).Revision != 0 || f.document(t, "gacha-draft:70000001").Revision != 0 || len(o.customGachas) != 0 || o.customGachaRevision != 0 || o.gachaRevision != beforeRevision || o.nextCustomGachaID() != beforeNext || !reflect.DeepEqual(o.gachaBases, bases) || !reflect.DeepEqual(o.managedGachaGroups, groups) || !reflect.DeepEqual(o.managedGachaGroupByID, byID) || !reflect.DeepEqual(o.gachaConfigurations, beforeConfigs) {
		t.Fatal("failed persistence leaked a registry entry, document, cursor or runtime config")
	}
	if code, result = f.call(t, "/api/gacha-variants", f.request(t, variantTestSourceID, 10)); code != 200 || result["gacha_id"] != float64(70000001) {
		t.Fatalf("add after rollback failed or consumed an ID: %d %v", code, result)
	}
	if code, result := f.call(t, "/api/gacha-pools/70000001/delete", map[string]any{"expected_revision": o.customGachaRevision}); code != 400 {
		t.Fatalf("attachment allowed deletion of its built-in parent: %d %v", code, result)
	}
	active, err := o.GachaPublication()
	if err != nil || o.customGachas[70000001].Deleted || !o.GachaIDPublished(variantTestSourceID, active) || o.customGachaRevision != 1 {
		t.Fatal("rejected attached-method deletion changed the built-in group", err)
	}
}

func TestGachaVariantLegacyCustomRootRestartAndDrawCountCollision(t *testing.T) {
	f := newGachaVariantFixture(t)
	root := ordinaryVariantTestProfile()
	root.GachaID, root.GroupID, root.Name, root.PublicationKey = 70001000, 70001000, "自建单抽", "custom"
	root.BannerKey = "local_standard"
	f.admin.gachaBannerPaths = map[string]string{"local_standard": "fixture"}
	if _, err := f.admin.operations.storage.WriteDocument(customGachaCatalogKey, 0, []gamestate.GachaProfile{root}, "test"); err != nil {
		t.Fatal(err)
	}
	rootLive := AdminGachaConfigFromProfile(root)
	if _, err := f.admin.operations.storage.WriteDocument("gacha-live:70001000", 0, rootLive, "test"); err != nil {
		t.Fatal(err)
	}
	o, err := NewOperations(f.admin.operations.storage, f.builtins)
	if err != nil {
		t.Fatal(err)
	}
	f.admin.operations = o
	code, result := f.call(t, "/api/gacha-variants", f.request(t, root.GachaID, 10))
	if code != 200 || result["gacha_id"] != float64(70001001) || result["group_id"] != float64(root.GroupID) {
		t.Fatalf("add to legacy custom root: %d %v", code, result)
	}
	restarted, err := NewOperations(o.storage, f.builtins)
	if err != nil {
		t.Fatal("restart lost a root that originated in custom-gacha-catalog", err)
	}
	f.admin.operations = restarted
	if restarted.customGachas[root.GachaID].RuleVersion != 2 || restarted.customGachas[70001001].RuleVersion != 3 || restarted.gachaBases[70001001].CardNum != 10 || len(restarted.gachaGroupMembers(root.GroupID)) != 2 || restarted.gachaBases[root.GachaID].FixedDrawCount {
		t.Fatal("legacy root/attached count ownership changed after restart")
	}
	if err := f.admin.validateStoredGachas(); err != nil {
		t.Fatal("reloaded legacy attachment is invalid", err)
	}
	newDraft := f.document(t, "gacha-draft:70001001")
	var newConfig AdminGachaConfig
	if err := json.Unmarshal(newDraft.Payload, &newConfig); err != nil {
		t.Fatal(err)
	}
	duplicate := rootLive
	duplicate.CardNum = 10
	if code, result := f.call(t, "/api/gacha-editor/group/draft", map[string]any{
		"group_id": root.GroupID, "configs": []AdminGachaConfig{duplicate, newConfig}, "expected": f.expectations(t, root.GroupID),
	}); code != 400 {
		t.Fatalf("editable legacy root created duplicate draw count: %d %v", code, result)
	}
	if f.document(t, "gacha-draft:70001000").Revision != 0 || !reflect.DeepEqual(f.document(t, "gacha-draft:70001001"), newDraft) {
		t.Fatal("rejected group save changed a draft")
	}
	// A conflicting draft saved by an earlier version must also be rejected at publication.
	if _, err := o.storage.WriteDocument("gacha-draft:70001000", 0, duplicate, "test"); err != nil {
		t.Fatal(err)
	}
	if code, result := f.call(t, "/api/gacha-editor/group/publish", map[string]any{"group_id": root.GroupID, "expected": f.expectations(t, root.GroupID)}); code != 400 {
		t.Fatalf("conflicting older draft was published: %d %v", code, result)
	}
	if f.document(t, "gacha-live:70001001").Revision != 0 || f.document(t, "gacha-live:70001000").Revision != 1 {
		t.Fatal("rejected duplicate publication changed live documents")
	}
	newConfig.CardNum = 2
	if code, result := f.call(t, "/api/gacha-editor/group/draft", map[string]any{
		"group_id": root.GroupID, "configs": []AdminGachaConfig{rootLive, newConfig}, "expected": f.expectations(t, root.GroupID),
	}); code != 400 {
		t.Fatalf("attached draw count was editable through a draft: %d %v", code, result)
	}
}

func TestGachaVariantRuleTemplatePoolPublishAndRestart(t *testing.T) {
	profile := ordinaryVariantTestProfile()
	profile.ArthurType = 0
	f := newGachaVariantFixture(t, profile)
	code, created := f.call(t, "/api/gacha-pools", map[string]any{
		"rule_template_id": profile.GroupID, "name": "模板单抽", "expected_revision": 0,
	})
	if code != 200 {
		t.Fatalf("create ordinary rule template: %d %v", code, created)
	}
	group := int(created["group_id"].(float64))
	source := int(created["gacha_ids"].([]any)[0].(float64))
	if f.admin.operations.customGachas[source].RuleVersion != 1 {
		t.Fatal("fixture did not create a rule-template pool")
	}
	code, added := f.call(t, "/api/gacha-variants", f.request(t, source, 10))
	if code != 200 || added["group_id"] != float64(group) {
		t.Fatalf("add to template-created pool: %d %v", code, added)
	}
	id := int(added["gacha_id"].(float64))
	restarted, err := NewOperations(f.admin.operations.storage, f.builtins)
	if err != nil || len(restarted.gachaGroupMembers(group)) != 2 || restarted.gachaBases[source].CardNum != 1 || restarted.gachaBases[id].CardNum != 10 {
		t.Fatal("rule-template attachment lost on restart", err)
	}
	f.admin.operations = restarted
	if code, result := f.call(t, "/api/gacha-editor/group/publish", map[string]any{"group_id": group, "expected": f.expectations(t, group)}); code != 200 {
		t.Fatalf("publish rule-template attachment: %d %v", code, result)
	}
	active := map[int]struct{}{group: {}}
	if !restarted.GachaIDPublished(source, active) || !restarted.GachaIDPublished(id, active) {
		t.Fatal("published template group omitted one draw method")
	}
	// Adding to the original group must not mutate the immutable rule-template picker.
	if code, result := f.call(t, "/api/gacha-variants", f.request(t, profile.GachaID, 10)); code != 200 {
		t.Fatalf("add to built-in template source: %d %v", code, result)
	}
	templates := f.admin.gachaRuleTemplates()
	if len(templates) != 1 || len(templates[0].GachaIDs) != 1 || templates[0].GachaIDs[0] != profile.GachaID {
		t.Fatalf("attached operator method leaked into immutable rule template: %+v", templates)
	}
}
