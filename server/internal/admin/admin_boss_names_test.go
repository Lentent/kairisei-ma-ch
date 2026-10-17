package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestBossNameRenameRestartRestoreAndPreserveIdentity(t *testing.T) {
	base, path := customBossFixture(t)
	base.TeamBattlePastBossGroups = []json.RawMessage{json.RawMessage(`{"0":700000010,"4":"模板龙","5":1,"6":"第一期","7":[100],"8":[0],"13":[{"0":11,"4":"超级","7":1}]}`)}
	baseSolo := string(base.TeamBattleSolo)
	basePast := string(base.TeamBattlePastBossGroups[0])
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	newOps := func() *Operations {
		t.Helper()
		o, err := NewOperations(accounts.Database(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := o.InitializeContent(base, path); err != nil {
			t.Fatal(err)
		}
		return o
	}
	o := newOps()
	group := AdminBattleGroup{GroupID: 700000010, Name: "模板龙", BossIDs: []int{11}, Bosses: []DropBoss{o.content.bosses[11]}}
	a := &API{operations: o, groups: []AdminBattleGroup{group}, pastGroups: []AdminBattleGroup{group}}
	o.dungeonScheduleGroups = map[string][]AdminBattleGroup{"activity": {group}, "past": {group}}
	router := chi.NewRouter()
	router.Get("/names", a.bossName)
	router.Put("/names", a.saveBossName)
	router.Get("/groups", a.bossGroups)
	router.Get("/drops", a.dropEditor)
	router.Get("/copies", a.customBossCatalog)
	router.Post("/copies", a.createCustomBoss)
	call := func(method, url string, body any, status int) []byte {
		t.Helper()
		return testfixture.CallContentAdmin(t, router, method, url, body, status)
	}
	assertName := func(raw []byte, key, want string) {
		t.Helper()
		var result map[string]json.RawMessage
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := json.Unmarshal(result[key], &got); err != nil || got != want {
			t.Fatalf("%s: %s, want %q", key, raw, want)
		}
	}
	assertName(call("GET", "/names?group_id=700000010", nil, 200), "name", "模板龙")
	assertName(call("PUT", "/names", map[string]any{"expected_revision": 0, "group_id": group.GroupID, "name": "  已有龙改名  "}, 200), "name", "已有龙改名")
	config := o.ContentConfiguration()
	var want, got any
	_ = json.Unmarshal([]byte(strings.Replace(baseSolo, "模板龙", "已有龙改名", 1)), &want)
	_ = json.Unmarshal(config.State.TeamBattleSolo, &got)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("rename changed native catalog fields other than group title")
	}
	_ = json.Unmarshal([]byte(strings.Replace(basePast, "模板龙", "已有龙改名", 1)), &want)
	_ = json.Unmarshal(config.State.TeamBattlePastBossGroups[0], &got)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("rename changed archive identities, period or difficulty")
	}
	if !reflect.DeepEqual(config.State.TeamBattleRewards, base.TeamBattleRewards) || !reflect.DeepEqual(config.State.TeamBattleReplays, base.TeamBattleReplays) {
		t.Fatal("rename changed rewards or battle definitions")
	}
	if string(base.TeamBattleSolo) != baseSolo || string(base.TeamBattlePastBossGroups[0]) != basePast {
		t.Fatal("rename mutated base catalogs")
	}
	for _, url := range []string{"/groups", "/groups?catalog=past"} {
		var result struct{ Groups []AdminBattleGroup }
		_ = json.Unmarshal(call("GET", url, nil, 200), &result)
		if result.Groups[0].Name != "已有龙改名" || result.Groups[0].Bosses[0].Name != "已有龙改名" {
			t.Fatal("admin catalog did not show the new name", url)
		}
	}
	var drop struct{ Boss DropBoss }
	_ = json.Unmarshal(call("GET", "/drops?boss_id=11", nil, 200), &drop)
	if drop.Boss.Name != "已有龙改名" {
		t.Fatal("drop editor did not inherit the name")
	}
	entries, err := o.dungeonScheduleEntries(defaultDungeonScheduleConfig(), time.Now())
	if err != nil || len(entries) != 2 || entries[0].DisplayName != "已有龙改名" || entries[1].DisplayName != "已有龙改名" {
		t.Fatal("schedule did not inherit the name", entries, err)
	}
	if a.groups[0].Name != "模板龙" || a.groups[0].Bosses[0].Name != "模板龙" {
		t.Fatal("admin projection mutated its original metadata")
	}
	account := base
	account.TeamBattleBossStates = map[int]int{11: 2}
	account.TeamBattleSolo = json.RawMessage(strings.Replace(baseSolo, `"10":2`, `"10":1`, 1))
	updated := game.ApplyContentState(account, config)
	var top map[string][]struct {
		Name   string `json:"4"`
		Bosses []struct {
			ID       int `json:"0"`
			Progress int `json:"10"`
		} `json:"10"`
	}
	_ = json.Unmarshal(updated.TeamBattleSolo, &top)
	if top["10"][0].Name != "已有龙改名" || top["10"][0].Bosses[0].ID != 11 || top["10"][0].Bosses[0].Progress != 2 || updated.TeamBattleBossStates[11] != 2 {
		t.Fatal("refresh lost the name or account clear progress")
	}
	// A fresh operations store reads the persisted override without changing
	// existing copies, then a separate content save must retain that override.
	o = newOps()
	a.operations = o
	assertName(call("GET", "/names?group_id=700000010", nil, 200), "name", "已有龙改名")
	call("POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 200)
	if !strings.Contains(string(o.ContentConfiguration().State.TeamBattleSolo), "已有龙改名") {
		t.Fatal("saving another content document removed the rename")
	}
	copyName := o.content.customBosses[0].Name
	assertName(call("PUT", "/names", map[string]any{"expected_revision": 1, "group_id": group.GroupID, "name": ""}, 200), "name", "模板龙")
	if len(o.content.bossNames) != 0 || o.content.customBosses[0].Name != copyName {
		t.Fatal("restore did not clear the override or changed an independent copy")
	}
	o = newOps()
	a.operations = o
	assertName(call("GET", "/names?group_id=700000010", nil, 200), "name", "模板龙")
	_ = json.Unmarshal(o.ContentConfiguration().State.TeamBattlePastBossGroups[0], &got)
	_ = json.Unmarshal([]byte(basePast), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("restoring and restarting did not recover original archive name")
	}
}

func TestBossNameValidationConflictAndAtomicity(t *testing.T) {
	base, path := customBossFixture(t)
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, _ := NewOperations(accounts.Database(), nil)
	if err := o.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Get("/names", a.bossName)
	router.Put("/names", a.saveBossName)
	for _, body := range []map[string]any{
		{"expected_revision": 0, "group_id": 700000010},
		{"expected_revision": 0, "group_id": 700000010, "name": nil},
		{"expected_revision": 0, "group_id": 999, "name": "不存在"},
		{"expected_revision": 0, "group_id": 790000001, "name": "复制品须使用原编辑器"},
		{"expected_revision": 0, "group_id": 700000010, "name": strings.Repeat("字", 81)},
		{"expected_revision": 0, "group_id": 700000010, "name": "第一行\n第二行"},
		{"expected_revision": 0, "group_id": 700000010, "name": "控\x00制"},
	} {
		testfixture.CallContentAdmin(t, router, "PUT", "/names", body, 400)
	}
	testfixture.CallContentAdmin(t, router, "GET", "/names?group_id=999", nil, 404)
	req := httptest.NewRequest(http.MethodPut, "/names", strings.NewReader(`{"expected_revision":0,"group_id":700000010,"name":"无请求保护"}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != 403 {
		t.Fatalf("unguarded write accepted: %d", recorder.Code)
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/names", map[string]any{"expected_revision": 0, "group_id": 700000010, "name": strings.Repeat("字", 80)}, 200)
	before := o.ContentConfiguration()
	testfixture.CallContentAdmin(t, router, "PUT", "/names", map[string]any{"expected_revision": 0, "group_id": 700000010, "name": "过期修改"}, 409)
	if !reflect.DeepEqual(o.ContentConfiguration(), before) || o.content.bossNameRevision != 1 {
		t.Fatal("rejected write changed active content")
	}
	doc, err := o.storage.ReadDocument(bossNamesKey)
	if err != nil || doc.Revision != 1 || strings.Contains(string(doc.Payload), "过期修改") {
		t.Fatal("rejected write changed persisted names", doc, err)
	}
}

func TestBossNameProjectionAllNativeCategories(t *testing.T) {
	for _, key := range []string{"9", "10", "11", "12"} {
		t.Run(key, func(t *testing.T) {
			raw := json.RawMessage(`{"` + key + `":[{"0":42,"4":"原名","10":[{"0":11,"4":"难度","10":2}]}]}`)
			state := gamestate.State{TeamBattleSolo: raw}
			c := &contentStore{bossNames: map[int]string{42: "新名"}}
			if err := c.projectBossNames(&state); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(state.TeamBattleSolo), "新名") || strings.Contains(string(raw), "新名") {
				t.Fatal("category title not projected independently", string(state.TeamBattleSolo))
			}
		})
	}
}
