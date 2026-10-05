package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

var variableBoxTestTotals = []int{10, 20, 100, 10, 20, 100, 10, 20, 100, 20, 100}

func fillVariableBoxTestConfig(c *AdminGachaConfig) {
	c.Price = 1
	for i := range c.BoxRounds {
		c.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{
			{Stock: variableBoxTestTotals[i] - 1, Reward: gamestate.Reward{Type: 4, Num: 10000, CardSkillLevels: []int16{}}},
			{Stock: 1, Reward: gamestate.Reward{Type: 10, Num: 5, CardSkillLevels: []int16{}}},
		}
	}
}

func assertVariableBoxTestRounds(t *testing.T, rounds []gamestate.GachaBoxRound) {
	t.Helper()
	if len(rounds) != len(variableBoxTestTotals) {
		t.Fatalf("expected 11 templates, got %d", len(rounds))
	}
	for i, round := range rounds {
		if len(round.Rewards) != 2 || gamestate.GachaBoxStock(round.Rewards) != variableBoxTestTotals[i] {
			t.Fatalf("template %d lost its variable total: %+v", i+1, round)
		}
		coin, crystal := round.Rewards[0], round.Rewards[1]
		if coin.Stock != variableBoxTestTotals[i]-1 || coin.Reward.Type != 4 || coin.Reward.Num != 10000 || crystal.Stock != 1 || crystal.Reward.Type != 10 || crystal.Reward.Num != 5 {
			t.Fatalf("template %d changed rewards or stocks: %+v", i+1, round)
		}
	}
}

func assertVariableBoxTestPreview(t *testing.T, body []byte) {
	t.Helper()
	var response struct {
		Preview struct {
			Config AdminGachaConfig      `json:"config"`
			Stages []game.GachaOddsStage `json:"stages"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	assertVariableBoxTestRounds(t, response.Preview.Config.BoxRounds)
	if len(response.Preview.Stages) != len(variableBoxTestTotals) {
		t.Fatalf("preview has %d templates", len(response.Preview.Stages))
	}
	for i, stage := range response.Preview.Stages {
		total := variableBoxTestTotals[i]
		if !strings.Contains(stage.Name, fmt.Sprintf("· %d 份", total)) || stage.DrawCount != 1 || !reflect.DeepEqual(stage.Stocks, []int{total - 1, 1}) || len(stage.Rewards) != 2 || stage.Rewards[0].Num != 10000 || stage.Rewards[1].Num != 5 || len(stage.Odds) != 2 || stage.Odds[0] <= stage.Odds[1] {
			t.Fatalf("template %d preview did not use its actual total/weights: %+v", i+1, stage)
		}
	}
}

func saveVariableBoxTestDraft(t *testing.T, handler http.Handler, c AdminGachaConfig, revision int) accountstore.Document {
	t.Helper()
	w := customGachaRequest(t, handler, "/editor/draft", map[string]any{"config": c, "expected_revision": revision})
	if w.Code != http.StatusOK {
		t.Fatalf("save variable box: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Document accountstore.Document `json:"document"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Document
}

func publishVariableBoxTestDraft(t *testing.T, handler http.Handler, id int, draft accountstore.Document, liveRevision int) []byte {
	t.Helper()
	w := customGachaRequest(t, handler, "/editor/publish", map[string]any{
		"config": map[string]int{"gacha_id": id}, "expected_revision": draft.Revision,
		"expected_live_revision": liveRevision, "sha256": draft.SHA256,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("publish variable box: %d %s", w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func TestCustomGachaBoxVariableStocksEditorLifecycle(t *testing.T) {
	a, accounts, handler := customGachaTestAPI(t)
	id := createCurrencyBox(t, a, handler)
	row := readBoxCurrencyEditorRow(t, a, id)
	var config AdminGachaConfig
	if err := json.Unmarshal(row.Config, &config); err != nil {
		t.Fatal(err)
	}
	fillVariableBoxTestConfig(&config)
	preview := customGachaRequest(t, handler, "/editor/preview", map[string]any{"config": config})
	if preview.Code != http.StatusOK {
		t.Fatalf("preview variable boxes: %d %s", preview.Code, preview.Body.String())
	}
	assertVariableBoxTestPreview(t, preview.Body.Bytes())
	saved := saveVariableBoxTestDraft(t, handler, config, row.Draft.Revision)
	row = readBoxCurrencyEditorRow(t, a, id)
	if row.Draft.Revision != saved.Revision || row.Draft.SHA256 != saved.SHA256 || row.Live.Revision != 0 {
		t.Fatal("editor reload lost the saved draft or published it early")
	}
	var reloadedDraft AdminGachaConfig
	if err := json.Unmarshal(row.Draft.Payload, &reloadedDraft); err != nil {
		t.Fatal(err)
	}
	assertVariableBoxTestRounds(t, reloadedDraft.BoxRounds)
	assertVariableBoxTestPreview(t, publishVariableBoxTestDraft(t, handler, id, saved, row.Live.Revision))
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{a.operations.gachaBases[60201301]})
	if err != nil {
		t.Fatal("restart variable stock templates", err)
	}
	a.operations = restarted
	if err := a.validateStoredGachas(); err != nil {
		t.Fatal("restarted variable templates are invalid", err)
	}
	row = readBoxCurrencyEditorRow(t, a, id)
	if row.Live.Revision != 1 || gachaDraftExists(row.Draft) {
		t.Fatal("restart lost variable-template publication")
	}
	var live AdminGachaConfig
	if err := json.Unmarshal(row.Config, &live); err != nil {
		t.Fatal(err)
	}
	assertVariableBoxTestRounds(t, live.BoxRounds)
	preview = customGachaRequest(t, handler, "/editor/preview", map[string]any{"config": live})
	if preview.Code != http.StatusOK {
		t.Fatalf("preview restarted variable boxes: %d %s", preview.Code, preview.Body.String())
	}
	assertVariableBoxTestPreview(t, preview.Body.Bytes())
}

func TestCustomGachaBoxVariableStocksPreserveActiveFiftySlotBox(t *testing.T) {
	a, accounts, handler := customGachaTestAPI(t)
	id := createCurrencyBox(t, a, handler)
	config := AdminGachaConfigFromProfile(a.operations.gachaBases[id])
	config.Price = 1
	for i := range config.BoxRounds {
		config.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{{Stock: 50, Reward: gamestate.Reward{Type: 4, Num: 7, CardSkillLevels: []int16{}}}}
	}
	draft := saveVariableBoxTestDraft(t, handler, config, 0)
	publishVariableBoxTestDraft(t, handler, id, draft, 0)
	state, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	// This player has already used 48 of the old 50 slots.
	oldBox := gamestate.GachaBoxProgress{Round: 1, Remaining: []gamestate.GachaBoxReward{{Stock: 2, Reward: gamestate.Reward{Type: 4, Num: 7, CardSkillLevels: []int16{}}}}}
	state.GachaBoxes = map[int]gamestate.GachaBoxProgress{id: oldBox}
	if err := accounts.PersistState(accountstore.PrimaryUserID, state); err != nil {
		t.Fatal(err)
	}
	fillVariableBoxTestConfig(&config)
	row := readBoxCurrencyEditorRow(t, a, id)
	draft = saveVariableBoxTestDraft(t, handler, config, row.Draft.Revision)
	assertVariableBoxTestPreview(t, publishVariableBoxTestDraft(t, handler, id, draft, row.Live.Revision))
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{a.operations.gachaBases[60201301]})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := accounts.LoadPersistentState(accountstore.PrimaryUserID)
	if err != nil || !reflect.DeepEqual(stored.GachaBoxes[id], oldBox) {
		t.Fatal("republish/restart refilled the active box or changed its rewards", err)
	}
	// Apply the new publication to a real game account carrying the persisted old inventory.
	runtime := testfixture.RuntimeState(t)
	runtime.Onboarding.Step, runtime.User.CoinFree = 9, 100
	runtime.GachaBoxes = gamestate.CloneGachaBoxes(stored.GachaBoxes)
	account, err := game.New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	account.ApplyGachaConfiguration(restarted.gachaRevision, restarted.gachaConfigurations)
	if got := account.Snapshot(runtime).GachaBoxes[id]; !reflect.DeepEqual(got, oldBox) {
		t.Fatal("applying the new publication changed the active old box")
	}
	if _, err := account.PlayGacha(id, 3, nil); err != nil {
		t.Fatal(err)
	}
	remaining := account.Snapshot(runtime).GachaBoxes[id]
	if remaining.Round != 1 || len(remaining.Remaining) != 1 || remaining.Remaining[0].Stock != 1 || remaining.Remaining[0].Reward.Num != 7 {
		t.Fatalf("old box did not keep its remaining reward: %+v", remaining)
	}
	if _, err := account.PlayGacha(id, 3, nil); err != nil {
		t.Fatal(err)
	}
	next := account.Snapshot(runtime).GachaBoxes[id]
	if next.Round != 2 || gamestate.GachaBoxStock(next.Remaining) != 20 || !reflect.DeepEqual(next.Remaining, config.BoxRounds[1].Rewards) {
		t.Fatalf("depleted old box did not refill from the new 20-slot template: %+v", next)
	}
	for range 20 {
		if _, err := account.PlayGacha(id, 3, nil); err != nil {
			t.Fatal(err)
		}
	}
	next = account.Snapshot(runtime).GachaBoxes[id]
	if next.Round != 3 || gamestate.GachaBoxStock(next.Remaining) != 100 || !reflect.DeepEqual(next.Remaining, config.BoxRounds[2].Rewards) {
		t.Fatalf("20-slot box did not advance after its actual stock was consumed: %+v", next)
	}
}
