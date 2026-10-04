package admin

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestCustomGachaBoxPublishRestartAndInventory(t *testing.T) {
	a, accounts, handler := customGachaTestAPI(t)
	a.catalogByKey["4:0"] = AdminCatalogEntry{Kind: "currency", Name: "金币", ResourceState: "ready"}
	w := customGachaRequest(t, handler, "/create", map[string]any{"name": "无限箱池", "mode": "box"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var created struct {
		ID int `json:"gacha_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	config := AdminGachaConfigFromProfile(a.operations.gachaBases[created.ID])
	if _, err := a.validateGachaConfig(config); err == nil {
		t.Fatal("empty box can be published")
	}
	for i := range config.BoxRounds {
		config.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{{Stock: 50, Reward: gamestate.Reward{Type: 4, Num: i + 1, CardSkillLevels: []int16{}}}}
	}
	preview, err := a.validateGachaConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview["stages"].([]game.GachaOddsStage)) != 11 {
		t.Fatal("missing template previews")
	}
	bad := config
	bad.PlayCountMax = 999
	if _, err := a.validateGachaConfig(bad); err == nil {
		t.Fatal("box round limit accepted")
	}
	publish := func(c AdminGachaConfig, draftRev, liveRev int) {
		t.Helper()
		w := customGachaRequest(t, handler, "/editor/draft", map[string]any{"config": c, "expected_revision": draftRev})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var saved struct {
			Document accountstore.Document `json:"document"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatal(err)
		}
		w = customGachaRequest(t, handler, "/editor/publish", map[string]any{"config": map[string]int{"gacha_id": created.ID}, "expected_revision": saved.Document.Revision, "expected_live_revision": liveRev, "sha256": saved.Document.SHA256})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	publish(config, 0, 0)
	state, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	state.GachaBoxes = map[int]gamestate.GachaBoxProgress{created.ID: {Round: 1000, Remaining: []gamestate.GachaBoxReward{{Stock: 17, Reward: gamestate.Reward{Type: 4, Num: 11, CardSkillLevels: []int16{}}}}}}
	if err := accounts.PersistState(accountstore.PrimaryUserID, state); err != nil {
		t.Fatal(err)
	}
	config.BoxRounds[10].Rewards[0].Reward.Num = 99
	publish(config, 2, 1)
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{a.operations.gachaBases[60201301]})
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.gachaBases[created.ID].BoxRounds) != 11 {
		t.Fatal("box rule family lost on restart")
	}
	restored, err := accounts.LoadPersistentState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	box := restored.GachaBoxes[created.ID]
	if box.Round != 1000 || box.Remaining[0].Stock != 17 || box.Remaining[0].Reward.Num != 11 {
		t.Fatal("restart/republish changed active box")
	}
	found := false
	for _, p := range restored.Gachas {
		if p.GachaID == created.ID {
			found = true
			if p.BoxRounds[10].Rewards[0].Reward.Num != 99 {
				t.Fatal("future box config did not refresh")
			}
		}
	}
	if !found {
		t.Fatal("published box missing from catalog")
	}
}
