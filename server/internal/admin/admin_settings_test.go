package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestEvolutionPolicyUsesDirectedEdgesAndSurvivesReload(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	catalog.CardActions.EvolutionTransitions = []gamestate.EvolutionTransition{{FromCardID: 10, ToCardID: 11, Type: 1}, {FromCardID: 11, ToCardID: 10, Type: 0}}
	accounts.Database().SetCatalog(catalog)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.evolutionClosed) != 0 {
		t.Fatal("unconfigured evolutions are closed")
	}
	a := &API{operations: o}
	call := func(body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/evolution-policy", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		a.saveEvolutionPolicy(w, r)
		if w.Code != want {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	call(`{"closed_paths":[{"from_cardid":10,"to_cardid":99}],"expected_revision":0}`, 400)
	call(`{"closed_paths":[{"from_cardid":10,"to_cardid":11}],"expected_revision":0}`, 200)
	call(`{"closed_paths":[],"expected_revision":0}`, 409)
	loaded, err := NewOperations(accounts.Database(), nil)
	if err != nil || len(loaded.evolutionClosed) != 1 || loaded.evolutionClosed[0] != (game.EvolutionPath{FromCardID: 10, ToCardID: 11}) {
		t.Fatal("directed policy did not reload", err)
	}
	call(`{"expected_revision":1}`, 400) // An incomplete request must not accidentally reopen everything.
	call(`{"closed_paths":[],"expected_revision":1}`, 200)
	loaded, err = NewOperations(accounts.Database(), nil)
	if err != nil || len(loaded.evolutionClosed) != 0 {
		t.Fatal("all-open reset did not persist", err)
	}
}

func TestAdminItemShopSettingsPersistOutsidePlayerSave(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	catalog.ItemDefinitions = append(catalog.ItemDefinitions, gamestate.ItemDefinition{ItemID: 1000, Name: "恢复药", PictID: 10080, MaxOwned: 999})
	catalog.CollectionRewards = append(catalog.CollectionRewards, gamestate.CollectionRewardDefinition{Type: 16, ID: 123, Name: "表情"})
	accounts.Database().SetCatalog(catalog)
	operations, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{operations: operations}
	put := func(handler http.HandlerFunc, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("settings HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	call := func(body string, want int) { t.Helper(); put(admin.setRuntimeSettings, body, want) }
	call(`{"crystal_purchase_enabled":false,"expected_revision":0,"item_shop":[{"lineup_id":602003,"enabled":true,"price":2000},{"lineup_id":602004,"enabled":false,"price":12000}]}`, 200)
	call(`{"crystal_purchase_enabled":true,"expected_revision":0}`, 409)
	call(`{"crystal_purchase_enabled":false,"expected_revision":1,"item_shop":[{"lineup_id":992001,"enabled":false,"price":2}]}`, 400)
	if operations.runtimeSettings.TeamBattleSpeed != 150 {
		t.Fatal("default team speed is not 1.5x")
	}
	call(`{"crystal_purchase_enabled":false,"expected_revision":1,"team_battle_speed":125}`, 400)
	for i, speed := range []int{100, 150, 200} {
		payload, _ := json.Marshal(map[string]any{"crystal_purchase_enabled": false, "expected_revision": i + 1, "team_battle_speed": speed})
		call(string(payload), 200)
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	admin.operations = restarted
	w := httptest.NewRecorder()
	admin.runtimeSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	var response struct {
		Settings game.RuntimeSettings `json:"settings"`
		Revision int                  `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Revision != 4 || response.Settings.TeamBattleSpeed != 200 {
		t.Fatalf("settings reload: %s %v", w.Body.String(), err)
	}
	got := make(map[int]game.ItemShopSetting)
	for _, setting := range response.Settings.ItemShop {
		got[setting.LineupID] = setting
	}
	if !got[602003].Enabled || got[602003].Price != 2000 || got[602004].Enabled || got[602004].Price != 12000 {
		t.Fatalf("operator settings lost after restart: %+v", got)
	}
	call(`{"crystal_purchase_enabled":false,"expected_revision":4,"item_shop":[{"lineup_id":70000001,"enabled":true,"price":10,"item_id":1000,"buy_type":1,"name":"恢复药","quantity":2,"pay_type":1,"buy_num_max":5,"total_limit":20,"period":"week","period_limit":3},{"lineup_id":70000002,"enabled":true,"price":50,"item_id":123,"buy_type":2,"name":"表情","tab_type":2,"quantity":1,"pay_type":3,"buy_num_max":1}]}`, 200)
	call(`{"crystal_purchase_enabled":false,"expected_revision":5,"item_shop":[{"lineup_id":70000001,"enabled":true,"price":10,"item_id":123,"buy_type":2,"name":"替换","tab_type":2,"quantity":1,"pay_type":1,"buy_num_max":1}]}`, 400)
	// An older admin page omitting the new products cannot erase their IDs.
	call(`{"crystal_purchase_enabled":false,"expected_revision":5,"item_shop":[]}`, 200)
	restarted, err = NewOperations(accounts.Database(), nil)
	if err != nil || len(restarted.runtimeSettings.ItemShop) != 2 || restarted.runtimeSettings.ItemShop[0].Period != "week" {
		t.Fatal("new products did not survive restart", err)
	}
	// The shop page saves only the shop; a missing list must not read as "remove everything".
	put(admin.setItemShop, `{"expected_revision":6}`, 400)
	put(admin.setItemShop, `{"expected_revision":6,"item_shop":[{"lineup_id":602003,"enabled":false,"price":3000}]}`, 200)
	if saved := admin.operations.runtimeSettings; saved.TeamBattleSpeed != 200 || len(saved.ItemShop) != 3 || saved.ItemShop[0].Price != 3000 {
		t.Fatalf("item shop save changed other settings or dropped products: %+v", saved)
	}
	uid := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 77)
	state, err := accounts.LoadState(uid)
	if err != nil {
		t.Fatal(err)
	}
	state.ItemShopPurchases = map[int]int{70000001: 7}
	state.ItemShopPeriods = map[int]gamestate.ItemShopPeriodCounts{70000001: {Day: "2026-09-30", DayCount: 2, Week: "2026-09-28", WeekCount: 3, Month: "2026-09", MonthCount: 7}}
	state.User.BP = state.User.BPMax + 30
	state.BattlePoint.NextRecoveryUnix = 0
	if err := accounts.PersistState(uid, state); err != nil {
		t.Fatal("persist shop counters and overflow BP", err)
	}
	loaded, err := accounts.LoadState(uid)
	if err != nil || loaded.ItemShopPurchases[70000001] != 7 || loaded.ItemShopPeriods[70000001].WeekCount != 3 || loaded.User.BP != state.User.BP {
		t.Fatal("player progress lost on reload", err)
	}
}
