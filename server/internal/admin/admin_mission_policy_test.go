package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func dynamicMissionTestAPI(t *testing.T) (*API, http.Handler) {
	t.Helper()
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	o.playerDefaults = &PlayerPolicy{StoryCrystals: 1, Notice: noticePolicy{Title: "公告", Body: "欢迎"}}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	legacy := o.playerPolicy.Load().Value
	legacy.Missions[1].Target, legacy.Missions[1].Crystals = 3, 8
	legacy.Missions[2].Enabled = false
	if _, err := o.writeDocument(playerPolicyKey, 0, legacy); err != nil {
		t.Fatal(err)
	}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	if err := o.loadMissionPolicy(); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o, catalogByKey: map[string]AdminCatalogEntry{
		adminCatalogKey(10, 0):       {RewardType: 10, Name: "水晶"},
		adminCatalogKey(4, 0):        {RewardType: 4, Name: "金币"},
		adminCatalogKey(6, 10000010): {RewardType: 6, RewardTypeID: 10000010, Name: "骑士", LevelMax: 60, FameMax: 90, LoveMax: 100},
		adminCatalogKey(6, 10000020): {RewardType: 6, RewardTypeID: 10000020, Name: "缺少资源", ResourceState: "unavailable", LevelMax: 60, FameMax: 90, LoveMax: 100},
		adminCatalogKey(14, 1):       {RewardType: 14, RewardTypeID: 1, Name: "服装"},
	}}
	r := chi.NewRouter()
	r.Get("/api/missions", a.missions)
	r.Put("/api/missions", a.saveMissions)
	r.Put("/policy", a.savePlayerPolicy)
	return a, r
}

func cloneMissionPolicy(t *testing.T, value MissionPolicy) MissionPolicy {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result MissionPolicy
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func newTestMission() game.MissionDefinition {
	return game.MissionDefinition{Title: "新增探索任务", Description: "探索三次领取奖励", Kind: "explore", Target: 3, Daily: true, Enabled: true,
		Rewards: []gamestate.Reward{{Type: 10, Num: 20, CardSkillLevels: []int16{}}}}
}

func TestDynamicMissionMigrationRemovalAndRestart(t *testing.T) {
	a, r := dynamicMissionTestAPI(t)
	o := a.operations
	p := o.missionPolicy.Load()
	if p.Revision != 1 || len(p.Value.Config.Missions) != 9 || p.Value.Config.Missions[1].Target != 3 || p.Value.Config.Missions[1].Rewards[0].Num != 8 || p.Value.Config.Missions[2].Enabled {
		t.Fatal("legacy task settings were not migrated")
	}
	if err := a.validateSavedMissionRewards(); err != nil {
		t.Fatal(err)
	}
	response := testfixture.CallContentAdmin(t, r, "GET", "/api/missions", nil, 200)
	var metadata struct {
		RewardEntries []AdminCatalogEntry `json:"reward_entries"`
		Delivery      string              `json:"reward_delivery"`
		Limits        map[string]int      `json:"limits"`
	}
	if err := json.Unmarshal(response, &metadata); err != nil || len(metadata.RewardEntries) != 1 || metadata.Delivery != "mail_per_reward" || metadata.Limits["max_missions"] != 200 {
		t.Fatal("missing dynamic task editor metadata", err)
	}
	// A stale copy of the old page cannot change the separate task document.
	legacy := cloneMissionPolicy(t, MissionPolicy{Missions: o.playerPolicy.Load().Value.Missions})
	oldPage := o.playerPolicy.Load().Value
	oldPage.Missions = legacy.Missions
	oldPage.Missions[1].Target = 99
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": oldPage}, 200)
	if o.playerPolicy.Load().Value.Missions[1].Target != 3 || o.missionPolicy.Load() != p {
		t.Fatal("old page overwrote independently managed tasks")
	}
	config := cloneMissionPolicy(t, p.Value.Config)
	config.Missions = []game.MissionDefinition{config.Missions[1], newTestMission()}
	config.Missions[0].Title = "保留的旧探索"
	testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 1, "config": config}, 200)
	p = o.missionPolicy.Load()
	if p.Revision != 2 || p.Value.Config.Missions[0].ID != 910002 || p.Value.Config.Missions[1].ID < firstManagedMissionID || len(p.Value.RetiredIDs) != 8 {
		t.Fatal("add/delete did not preserve stable identities")
	}
	newID := p.Value.Config.Missions[1].ID
	testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 1, "config": config}, 409)
	testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 2, "config": MissionPolicy{Missions: []game.MissionDefinition{}}}, 200)
	p = o.missionPolicy.Load()
	before, err := o.storage.ReadDocument(missionPolicyKey)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewOperations(o.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.playerDefaults = o.playerDefaults
	if err := restarted.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	if err := restarted.loadMissionPolicy(); err != nil {
		t.Fatal(err)
	}
	if got := restarted.missionPolicy.Load(); got.Value.Config.Missions == nil || len(got.Value.Config.Missions) != 0 || got.Revision != 3 || !reflect.DeepEqual(got.Value, p.Value) {
		t.Fatal("restart resurrected removed missions or lost retired identities")
	}
	after, err := o.storage.ReadDocument(missionPolicyKey)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("restart performed a second migration")
	}
	b := &API{operations: restarted, catalogByKey: a.catalogByKey}
	r2 := chi.NewRouter()
	r2.Put("/api/missions", b.saveMissions)
	bad := newTestMission()
	bad.ID = newID
	testfixture.CallContentAdmin(t, r2, "PUT", "/api/missions", map[string]any{"expected_revision": 3, "config": MissionPolicy{Missions: []game.MissionDefinition{bad}}}, 400)
	bad.ID = 999999
	testfixture.CallContentAdmin(t, r2, "PUT", "/api/missions", map[string]any{"expected_revision": 3, "config": MissionPolicy{Missions: []game.MissionDefinition{bad}}}, 400)
	bad.ID = 0
	testfixture.CallContentAdmin(t, r2, "PUT", "/api/missions", map[string]any{"expected_revision": 3, "config": MissionPolicy{Missions: []game.MissionDefinition{bad}}}, 200)
	if got := restarted.missionPolicy.Load().Value.Config.Missions[0].ID; got <= newID {
		t.Fatal("deleted task ID was reused")
	}
}

func TestDynamicMissionShapeChangesAndCanonicalRewards(t *testing.T) {
	a, r := dynamicMissionTestAPI(t)
	o := a.operations
	config := cloneMissionPolicy(t, o.missionPolicy.Load().Value.Config)
	oldID := config.Missions[1].ID
	config.Missions[1].Title, config.Missions[1].Target = "新标题", 7
	config.Missions[1].Rewards = []gamestate.Reward{{Type: 4, Num: 25}, {Type: 6, RewardTypeID: 10000010, Num: 2}}
	config.Missions[1].Crystals = 8888
	testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 1, "config": config}, 200)
	got := o.missionPolicy.Load().Value.Config.Missions[1]
	if got.ID != oldID || got.Crystals != 0 || got.Rewards[1].CardLevel != 1 || got.Rewards[1].CardFame != 1 || !slices.Equal(got.Rewards[1].CardSkillLevels, []int16{1}) {
		t.Fatal("ordinary edits reset identity or rewards were not canonicalized")
	}
	config = cloneMissionPolicy(t, o.missionPolicy.Load().Value.Config)
	config.Missions[1].Daily = false
	testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 2, "config": config}, 200)
	changedID := o.missionPolicy.Load().Value.Config.Missions[1].ID
	if changedID == oldID || !slices.Contains(o.missionPolicy.Load().Value.RetiredIDs, oldID) {
		t.Fatal("category change reused a task receipt identity")
	}
	config = cloneMissionPolicy(t, o.missionPolicy.Load().Value.Config)
	config.Missions[1].Kind = "level"
	testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 3, "config": config}, 200)
	if o.missionPolicy.Load().Value.Config.Missions[1].ID == changedID || !slices.Contains(o.missionPolicy.Load().Value.RetiredIDs, changedID) {
		t.Fatal("condition change reused a task receipt identity")
	}
}

func TestDynamicMissionValidationDoesNotMutateDocument(t *testing.T) {
	a, r := dynamicMissionTestAPI(t)
	o := a.operations
	before, err := o.storage.ReadDocument(missionPolicyKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*MissionPolicy){
		"negative ID":       func(c *MissionPolicy) { c.Missions[0].ID = -1 },
		"duplicate ID":      func(c *MissionPolicy) { c.Missions[1].ID = c.Missions[0].ID },
		"unknown condition": func(c *MissionPolicy) { c.Missions[1].Kind = "battle" },
		"daily level":       func(c *MissionPolicy) { c.Missions[1].Kind = "level" },
		"daily collection":  func(c *MissionPolicy) { c.Missions[1].Kind = "collection" },
		"login count":       func(c *MissionPolicy) { c.Missions[0].Target = 2 },
		"target zero":       func(c *MissionPolicy) { c.Missions[1].Target = 0 },
		"blank title":       func(c *MissionPolicy) { c.Missions[1].Title = " " },
		"long description":  func(c *MissionPolicy) { c.Missions[1].Description = strings.Repeat("字", 201) },
		"zero rewards":      func(c *MissionPolicy) { c.Missions[1].Rewards = []gamestate.Reward{} },
		"many rewards":      func(c *MissionPolicy) { c.Missions[1].Rewards = make([]gamestate.Reward, 5) },
		"unknown reward":    func(c *MissionPolicy) { c.Missions[1].Rewards[0].Type = 999 },
		"unavailable card": func(c *MissionPolicy) {
			c.Missions[1].Rewards[0] = gamestate.Reward{Type: 6, RewardTypeID: 10000020, Num: 1}
		},
		"card quantity": func(c *MissionPolicy) {
			c.Missions[1].Rewards[0] = gamestate.Reward{Type: 6, RewardTypeID: 10000010, Num: 101}
		},
		"card stats": func(c *MissionPolicy) {
			c.Missions[1].Rewards[0] = gamestate.Reward{Type: 6, RewardTypeID: 10000010, Num: 1, CardLevel: 61}
		},
		"collection quantity": func(c *MissionPolicy) { c.Missions[1].Rewards[0] = gamestate.Reward{Type: 14, RewardTypeID: 1, Num: 2} },
		"duplicate rewards": func(c *MissionPolicy) {
			c.Missions[1].Rewards = append(c.Missions[1].Rewards, c.Missions[1].Rewards[0])
		},
		"missing array":  func(c *MissionPolicy) { c.Missions = nil },
		"too many tasks": func(c *MissionPolicy) { c.Missions = make([]game.MissionDefinition, maxManagedMissions+1) },
	} {
		t.Run(name, func(t *testing.T) {
			config := cloneMissionPolicy(t, o.missionPolicy.Load().Value.Config)
			mutate(&config)
			testfixture.CallContentAdmin(t, r, "PUT", "/api/missions", map[string]any{"expected_revision": 1, "config": config}, 400)
		})
	}
	request := httptest.NewRequest("PUT", "/api/missions", strings.NewReader(`{"expected_revision":1,"config":{"missions":[]}}`))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("mutation accepted without action header")
	}
	request.Header.Set("X-Kairisei-Admin-Action", "apply")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("cross-site mutation accepted")
	}
	after, err := o.storage.ReadDocument(missionPolicyKey)
	if err != nil || !reflect.DeepEqual(before, after) || o.missionPolicy.Load().Revision != 1 {
		t.Fatal("rejected save modified the document")
	}
}

func TestDynamicMissionConcurrentWritersAllocateOnce(t *testing.T) {
	a, r := dynamicMissionTestAPI(t)
	config := MissionPolicy{Missions: []game.MissionDefinition{newTestMission()}}
	payload, _ := json.Marshal(map[string]any{"expected_revision": 1, "config": config})
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := httptest.NewRequest("PUT", "/api/missions", strings.NewReader(string(payload)))
			request.RemoteAddr = "127.0.0.1:1234"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Kairisei-Admin-Action", "apply")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for code := range statuses {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent stale page won twice", counts)
	}
	if got := a.operations.missionPolicy.Load(); got.Revision != 2 || got.Value.NextID != firstManagedMissionID+1 || got.Value.Config.Missions[0].ID != firstManagedMissionID {
		t.Fatal("failed save consumed an identity")
	}
}
