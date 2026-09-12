package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/release"
)

func callContentAdmin(t *testing.T, h http.Handler, method, path string, body any, want int) []byte {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://localhost"+path, bytes.NewReader(data))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kairisei-Admin-Action", "apply")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: status %d: %s", method, path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func TestAdminContentAtomicConfigAndRestart(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	ops, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := release.State{TeamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":1,"4":"测试","10":[{"0":11,"4":"中级","12":[{"0":1,"1":0}]}]}],"11":[],"12":[]}`),
		TeamBattleRewards: []release.TeamBattleRewardProfile{{BossID: 11, ResultRewards: []release.Reward{{Type: 0, Num: 2}, {Type: 4, Num: 50}}, EnemyDrops: []release.TeamBattleEnemyDrop{{Reward: release.Reward{Type: 4, Num: 50, CardSkillLevels: []int16{}}}}}},
		TradeShopProfiles: []release.TradeShopProfile{{TradeShopID: 1, Name: "原兑换所", ShopType: 1, Lineups: []release.TradeShopLineupProfile{{LineupID: 2, LineupName: "道具", StockNum: 3, Prices: []release.TradeShopPointProfile{{Type: 4, ID: 4000, Num: 10, PointCardCondition: []release.TradeShopPointCardCondition{}}}, Rewards: []release.Reward{{Type: 8, RewardTypeID: 1000, Num: 1, CardSkillLevels: []int16{}}}}}}}}
	if err := ops.initializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	row := ops.content.bosses[11]
	row.Targets = []cnDropTarget{{BattleIndex: 0, EnemyIndex: 0, Name: "本体"}}
	ops.content.bosses[11] = row
	admin := &cnAdmin{operations: ops, catalogByKey: map[string]cnAdminCatalogEntry{"8:1000": {Name: "药水"}, "8:4000": {Name: "币"}}}
	router := chi.NewRouter()
	router.Put("/drops", admin.saveDropEditor)
	router.Put("/shops/{shopID}", admin.saveExchangeEditor)
	config := cnBossDrops{BossID: 11, Drops: []release.TeamBattleEnemyDrop{{Reward: release.Reward{Type: 8, RewardTypeID: 1000, Num: 3, CardSkillLevels: []int16{}}}}}
	config.FameRewards = []release.Reward{{Type: 8, RewardTypeID: 4000, Num: 600, CardSkillLevels: []int16{}}}
	callContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 0, "configs": []cnBossDrops{config}}, 200)
	p := ops.content.configuration.State.TeamBattleRewards[0]
	if len(p.EnemyDrops) != 2 || p.EnemyDrops[0].Reward.Type != 4 || p.EnemyDrops[0].Reward.Num != 50 || p.ResultRewards[0].Type != 0 {
		t.Fatal("fixed gold/EXP lost")
	}
	bad := config
	bad.BossID = 99
	callContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 1, "configs": []cnBossDrops{config, bad}}, 400)
	callContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 0, "configs": []cnBossDrops{config}}, 409)
	if ops.content.dropRevision != 1 {
		t.Fatal("failed batch advanced revision")
	}
	shop := base.TradeShopProfiles[0]
	shop.Disabled = true
	callContentAdmin(t, router, "PUT", "/shops/1", map[string]any{"expected_revision": 0, "shop": shop}, 200)
	shop.Lineups = nil
	callContentAdmin(t, router, "PUT", "/shops/1", map[string]any{"expected_revision": 1, "shop": shop}, 400)
	restarted, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.initializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	if restarted.content.dropRevision != 1 || restarted.content.shopRevision != 1 || !restarted.content.configuration.State.TradeShopProfiles[0].Disabled {
		t.Fatal("public config lost on restart")
	}
	if rewards := restarted.content.configuration.State.TeamBattleRewards[0].FameRewards; len(rewards) != 1 || rewards[0].Num != 600 {
		t.Fatal("fame pool lost on restart")
	}
	if base.TradeShopProfiles[0].Disabled || len(base.TeamBattleRewards[0].EnemyDrops) != 1 {
		t.Fatal("overrides mutated base")
	}
}

// Full-resource HTTP contracts for editors; all requests use an isolated
// database and recorder. No listener, actual player or external network.
func auditCompleteAdminContent(t *testing.T, h http.Handler) {
	t.Helper()
	admin := h.(interface{ AdminHandler() http.Handler }).AdminHandler()
	for _, kind := range []string{"costume", "stamp", "honor"} {
		var catalog struct {
			Total   int                   `json:"total"`
			Entries []cnAdminCatalogEntry `json:"entries"`
		}
		if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/catalog?kind="+kind+"&limit=200", nil, 200), &catalog); err != nil {
			t.Fatal(err)
		}
		if catalog.Total == 0 {
			t.Fatalf("missing %s catalog", kind)
		}
		requests := make([]cnAdminMailRequest, len(catalog.Entries))
		for i, entry := range catalog.Entries {
			requests[i] = cnAdminMailRequest{RewardType: entry.RewardType, RewardTypeID: entry.RewardTypeID, Quantity: 1}
		}
		for i := 0; i < len(requests); i += 100 {
			callContentAdmin(t, admin, "POST", "/api/catalog/resolve", map[string]any{"rewards": requests[i:min(i+100, len(requests))]}, 200)
		}
		t.Logf("%s catalog: %d entries", kind, catalog.Total)
	}
	var list struct {
		Bosses []cnDropBoss `json:"bosses"`
	}
	if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/boss-drops", nil, 200), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Bosses) < 100 {
		t.Fatal("missing boss editor catalog")
	}
	var detail struct {
		Config cnBossDrops `json:"config"`
		Boss   cnDropBoss  `json:"boss"`
	}
	// Verify every default can actually be edited; this catches reward/slot
	// schema mismatches that a page-only test would miss.
	for _, boss := range list.Bosses {
		if len(boss.Targets) == 0 {
			t.Fatalf("boss %d has no real targets", boss.BossID)
		}
	}
	if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/boss-drops?boss_id=30100102", nil, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Config.Drops) == 0 {
		t.Fatal("missing drop config")
	}
	for i := range detail.Config.Drops {
		if detail.Config.Drops[i].Reward.Type == 13 {
			detail.Config.Drops[i].Reward.Num++
			break
		}
	}
	callContentAdmin(t, admin, "PUT", "/api/boss-drops", map[string]any{"expected_revision": 0, "configs": []cnBossDrops{detail.Config}}, 200)
	var shops struct {
		Shops      []release.TradeShopProfile       `json:"shops"`
		NextShop   int                              `json:"next_shop_id"`
		NextLineup int                              `json:"next_lineup_id"`
		Templates  map[string][]cnAdminCatalogEntry `json:"templates"`
	}
	if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/exchanges", nil, 200), &shops); err != nil {
		t.Fatal(err)
	}
	if len(shops.Templates["evolution"]) < 20 || len(shops.Templates["fusion"]) < 2 {
		t.Fatal("incomplete material shortcuts")
	}
	shop := shops.Shops[0]
	shop.TradeShopID = shops.NextShop
	shop.Name = "测试复制兑换所"
	for i := range shop.Lineups {
		shop.Lineups[i].LineupID = shops.NextLineup + i
	}
	callContentAdmin(t, admin, "PUT", "/api/exchanges/"+jsonNumber(shop.TradeShopID), map[string]any{"expected_revision": 0, "shop": shop}, 200)
	var presets struct {
		Presets []cnAdminGachaPreset `json:"presets"`
	}
	_ = json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/gacha-presets", nil, 200), &presets)
	baseFound := map[int]bool{}
	for _, p := range presets.Presets {
		baseFound[p.GroupID] = true
		for _, id := range p.GachaIDs {
			if id == 90000100 || id == 90000200 {
				t.Fatal("tutorial exposed")
			}
		}
	}
	for _, id := range []int{60200001, 60200101, 60200201} {
		if !baseFound[id] {
			t.Fatalf("base pool %d missing", id)
		}
	}
	var pools struct {
		Pools []struct {
			Config cnAdminGachaConfig `json:"config"`
		} `json:"pools"`
	}
	_ = json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/gacha-editor", nil, 200), &pools)
	for _, pool := range pools.Pools {
		callContentAdmin(t, admin, "POST", "/api/gacha-editor/preview", map[string]any{"config": pool.Config}, 200)
	}
	t.Logf("operations: %d boss difficulties, %d exchange shops; %d fusion/%d evolution material options; %d gacha groups include permanent pools", len(list.Bosses), len(shops.Shops), len(shops.Templates["fusion"]), len(shops.Templates["evolution"]), len(presets.Presets))
}

func jsonNumber(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestBuiltinGachaPublicationAndIndividualSwitch(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	base := release.GachaProfile{GachaID: 60200011, GroupID: 60200001, Name: "常驻", Price: 50}
	profiles := []release.GachaProfile{base, {GachaID: 90000100, GroupID: 90000100}, {GachaID: 90000200, GroupID: 90000200}}
	o, err := newCNOperationStore(accounts.storage, profiles)
	if err != nil {
		t.Fatal(err)
	}
	active, err := o.gachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	if !o.gachaIDPublished(base.GachaID, active) || len(o.gachaBases) != 1 || len(o.managedGachaGroups) != 1 {
		t.Fatal("built-in pools or onboarding boundary incorrect")
	}
	zero := 0
	if _, err := o.setGachaPublication(cnGachaPublication{ExpectedRevision: &zero, GroupIDs: []int{}}); err != nil {
		t.Fatal(err)
	}
	active, err = o.gachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	if o.gachaIDPublished(base.GachaID, active) || !o.gachaIDPublished(90000100, active) || !o.gachaIDPublished(90000200, active) {
		t.Fatal("group switch did not respect onboarding exemption")
	}
	c := cnAdminGachaConfigFromProfile(base)
	c.Disabled = true
	if _, err := o.writeDocument("gacha-live:60200011", 0, c); err != nil {
		t.Fatal(err)
	}
	restarted, err := newCNOperationStore(accounts.storage, profiles)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.gachaIDPublished(base.GachaID, map[int]struct{}{base.GroupID: {}}) {
		t.Fatal("individual disable lost after restart")
	}
}
