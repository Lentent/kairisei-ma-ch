package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type boxCurrencyEditorRow struct {
	ID     int                   `json:"gacha_id"`
	Config json.RawMessage       `json:"config"`
	Draft  accountstore.Document `json:"draft"`
	Live   accountstore.Document `json:"live"`
}

func readBoxCurrencyEditorRow(t *testing.T, a *API, id int) boxCurrencyEditorRow {
	t.Helper()
	w := httptest.NewRecorder()
	a.gachaEditorList(w, httptest.NewRequest(http.MethodGet, "/api/gacha-editor", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("load editor: %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Pools []boxCurrencyEditorRow `json:"pools"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	for _, row := range listed.Pools {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("created box %d missing from editor", id)
	return boxCurrencyEditorRow{}
}

func createCurrencyBox(t *testing.T, a *API, handler http.Handler) int {
	t.Helper()
	a.catalogByKey["4:0"] = AdminCatalogEntry{Kind: "currency", Name: "金币", ResourceState: "ready"}
	a.catalogByKey["10:0"] = AdminCatalogEntry{Kind: "currency", Name: "免费水晶", ResourceState: "ready"}
	w := customGachaRequest(t, handler, "/create", map[string]any{"name": "金币水晶无限箱池", "mode": "box"})
	if w.Code != http.StatusOK {
		t.Fatalf("create box: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID int `json:"gacha_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func assertEmptyBoxEditorArrays(t *testing.T, config json.RawMessage) {
	t.Helper()
	var decoded struct {
		Rounds []struct {
			Rewards json.RawMessage `json:"rewards"`
		} `json:"box_rounds"`
	}
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Rounds) != gamestate.GachaBoxTemplates {
		t.Fatalf("empty box should expose 11 editable templates: %s", config)
	}
	for i, round := range decoded.Rounds {
		if !bytes.Equal(bytes.TrimSpace(round.Rewards), []byte("[]")) {
			t.Fatalf("empty template %d rewards must be a JSON array: %s", i+1, round.Rewards)
		}
	}
}

func assertBoxCurrencyRounds(t *testing.T, rounds []gamestate.GachaBoxRound) {
	t.Helper()
	if len(rounds) != gamestate.GachaBoxTemplates {
		t.Fatalf("expected 11 reward templates, got %d", len(rounds))
	}
	for i, round := range rounds {
		if len(round.Rewards) != 2 || gamestate.GachaBoxStock(round.Rewards) != 50 {
			t.Fatalf("template %d must contain two rewards totaling 50 slots: %+v", i+1, round)
		}
		coin, crystal := round.Rewards[0], round.Rewards[1]
		if coin.Stock != 25 || coin.Reward.Type != 4 || coin.Reward.RewardTypeID != 0 || coin.Reward.Num != 10000 || crystal.Stock != 25 || crystal.Reward.Type != 10 || crystal.Reward.RewardTypeID != 0 || crystal.Reward.Num != 5 {
			t.Fatalf("template %d mixed stock with granted quantity: %+v", i+1, round)
		}
	}
}

func assertBoxCurrencyPreview(t *testing.T, body []byte) {
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
	assertBoxCurrencyRounds(t, response.Preview.Config.BoxRounds)
	if len(response.Preview.Stages) != gamestate.GachaBoxTemplates {
		t.Fatalf("preview has %d templates", len(response.Preview.Stages))
	}
	for i, stage := range response.Preview.Stages {
		if stage.DrawCount != 1 || len(stage.Rewards) != 2 || !reflect.DeepEqual(stage.Stocks, []int{25, 25}) || len(stage.Odds) != 2 || stage.Odds[0] != stage.Odds[1] || stage.Rewards[0].Type != 4 || stage.Rewards[0].Num != 10000 || stage.Rewards[1].Type != 10 || stage.Rewards[1].Num != 5 {
			t.Fatalf("template %d preview confused quantity and stock: %+v", i+1, stage)
		}
	}
}

func TestCustomGachaBoxEmptyEditorRewardsAreArrays(t *testing.T) {
	a, accounts, handler := customGachaTestAPI(t)
	id := createCurrencyBox(t, a, handler)
	assertEmptyBoxEditorArrays(t, readBoxCurrencyEditorRow(t, a, id).Config)
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{a.operations.gachaBases[60201301]})
	if err != nil {
		t.Fatal(err)
	}
	a.operations = restarted
	assertEmptyBoxEditorArrays(t, readBoxCurrencyEditorRow(t, a, id).Config)
}

func TestCustomGachaBoxCurrencyEditorLifecycle(t *testing.T) {
	a, accounts, handler := customGachaTestAPI(t)
	id := createCurrencyBox(t, a, handler)
	row := readBoxCurrencyEditorRow(t, a, id)
	var config AdminGachaConfig
	if err := json.Unmarshal(row.Config, &config); err != nil {
		t.Fatal(err)
	}
	for i := range config.BoxRounds {
		config.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{
			{Stock: 25, Reward: gamestate.Reward{Type: 4, Num: 10000, CardSkillLevels: []int16{}}},
			{Stock: 25, Reward: gamestate.Reward{Type: 10, Num: 5, CardSkillLevels: []int16{}}},
		}
	}
	preview := customGachaRequest(t, handler, "/editor/preview", map[string]any{"config": config})
	if preview.Code != http.StatusOK {
		t.Fatalf("preview box currencies: %d %s", preview.Code, preview.Body.String())
	}
	assertBoxCurrencyPreview(t, preview.Body.Bytes())
	saved := customGachaRequest(t, handler, "/editor/draft", map[string]any{"config": config, "expected_revision": row.Draft.Revision})
	if saved.Code != http.StatusOK {
		t.Fatalf("save box currency draft: %d %s", saved.Code, saved.Body.String())
	}
	assertBoxCurrencyPreview(t, saved.Body.Bytes())
	// Reload through the editor endpoint: its saved draft must retain reward quantities and stocks.
	row = readBoxCurrencyEditorRow(t, a, id)
	if row.Draft.Revision != 1 || row.Live.Revision != 0 {
		t.Fatalf("draft unexpectedly published or failed to persist: %+v", row)
	}
	var draft AdminGachaConfig
	if err := json.Unmarshal(row.Draft.Payload, &draft); err != nil {
		t.Fatal(err)
	}
	assertBoxCurrencyRounds(t, draft.BoxRounds)
	published := customGachaRequest(t, handler, "/editor/publish", map[string]any{
		"config": map[string]int{"gacha_id": id}, "expected_revision": row.Draft.Revision,
		"expected_live_revision": row.Live.Revision, "sha256": row.Draft.SHA256,
	})
	if published.Code != http.StatusOK {
		t.Fatalf("publish box currencies: %d %s", published.Code, published.Body.String())
	}
	assertBoxCurrencyPreview(t, published.Body.Bytes())
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{a.operations.gachaBases[60201301]})
	if err != nil {
		t.Fatal("reload published box currency configuration", err)
	}
	a.operations = restarted
	if err := a.validateStoredGachas(); err != nil {
		t.Fatal("published box currency config is invalid after restart", err)
	}
	row = readBoxCurrencyEditorRow(t, a, id)
	if row.Live.Revision != 1 || gachaDraftExists(row.Draft) {
		t.Fatal("restart lost publication or restored a consumed draft")
	}
	var live AdminGachaConfig
	if err := json.Unmarshal(row.Config, &live); err != nil {
		t.Fatal(err)
	}
	assertBoxCurrencyRounds(t, live.BoxRounds)
	preview = customGachaRequest(t, handler, "/editor/preview", map[string]any{"config": live})
	if preview.Code != http.StatusOK {
		t.Fatalf("preview restarted box currencies: %d %s", preview.Code, preview.Body.String())
	}
	assertBoxCurrencyPreview(t, preview.Body.Bytes())
}
