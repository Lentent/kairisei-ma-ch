package httpapi

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestGachaBoxUsesExistingClientFieldsAndCurrentOdds(t *testing.T) {
	a := &API{account: testAccount(t, func(state *gamestate.State) {
		p := gamestate.GachaProfile{GachaID: 70000001, GroupID: 70000001, Name: "箱池", PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1}
		for i := 0; i < 11; i++ {
			p.BoxRounds = append(p.BoxRounds, gamestate.GachaBoxRound{Rewards: []gamestate.GachaBoxReward{{Stock: 50, Reward: gamestate.Reward{Type: 4, Num: 10, CardSkillLevels: []int16{}}}}})
		}
		state.Gachas = []gamestate.GachaProfile{p}
		state.GachaSelections, state.GachaDailyClaims = nil, nil
		state.GachaBoxes = map[int]gamestate.GachaBoxProgress{p.GroupID: {Round: 1000, Remaining: []gamestate.GachaBoxReward{
			{Stock: 1, Reward: gamestate.Reward{Type: 4, Num: 10, CardSkillLevels: []int16{}}},
			{Stock: 3, Reward: gamestate.Reward{Type: 10, Num: 2, CardSkillLevels: []int16{}}},
		}}}
	})}
	profile := a.account.GachaState()[0]
	profile.PlayCount = math.MaxInt
	info := a.gachaInfos([]gamestate.GachaProfile{profile})[0].(map[string]any)
	if !strings.Contains(info["buymsg"].(string), "剩余 4 份") || !strings.Contains(info["gacha_name"].(string), "1000") || info["play_count_max"] != 0 || info["is_stepup_price"] != 0 || info["play_count"].(int) > math.MaxInt32 {
		t.Fatal("box wire info breaks existing client fields", info)
	}
	if _, ok := info["box_rounds"]; ok {
		t.Fatal("server-only templates leaked to APK")
	}
	w := httptest.NewRecorder()
	a.gachaOddsShow(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"gachaid":70000001,"popupid":0}`)))
	lines := bytes.Split(bytes.TrimSpace(w.Body.Bytes()), []byte{'\n'})
	if len(lines) != 3 {
		t.Fatal(w.Body.String())
	}
	var body struct {
		Message string `json:"odds_msg"`
		Lineups []struct {
			Prizes []struct {
				Reward gamestate.Reward `json:"prize"`
				Odds   int              `json:"odds"`
			} `json:"prize_list"`
		} `json:"lineup_infos"`
	}
	if err := json.Unmarshal(lines[1], &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Message, "剩余 3 份") || len(body.Lineups) != 1 || len(body.Lineups[0].Prizes) != 2 || body.Lineups[0].Prizes[0].Odds != 2500000 || body.Lineups[0].Prizes[1].Odds != 7500000 || body.Lineups[0].Prizes[1].Reward.Num != 2 {
		t.Fatal("remaining stock/probability/grant quantity confused", w.Body.String())
	}
}

func TestGachaBoxTenDrawWireReturnsAllSequentialRewards(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.Onboarding.Step, state.User.Coin, state.User.CoinFree = 9, 0, 100
	single := gamestate.GachaProfile{GachaID: 70000001, GroupID: 70000001, Name: "箱池", PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1}
	for i := 0; i < gamestate.GachaBoxTemplates; i++ {
		single.BoxRounds = append(single.BoxRounds, gamestate.GachaBoxRound{Rewards: []gamestate.GachaBoxReward{{Stock: 20, Reward: gamestate.Reward{Type: 4, Num: i + 1, CardSkillLevels: []int16{}}}}})
	}
	multi := gamestate.CloneGachas([]gamestate.GachaProfile{single})[0]
	multi.GachaID, multi.CardNum, multi.CardNumMax, multi.Price = 70000002, 10, 10, 5
	state.Gachas = []gamestate.GachaProfile{single, multi}
	state.GachaSelections, state.GachaDailyClaims = nil, nil
	state.GachaBoxes = map[int]gamestate.GachaBoxProgress{single.GroupID: {Round: 1, Remaining: []gamestate.GachaBoxReward{{Stock: 2, Reward: gamestate.Reward{Type: 4, Num: 1, CardSkillLevels: []int16{}}}}}}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{account: account, initialState: state}
	before := a.gachaInfos(account.GachaState())
	if len(before) != 2 || before[1].(map[string]any)["card_num"] != 10 || before[1].(map[string]any)["card_num_max"] != 10 || !strings.Contains(before[1].(map[string]any)["buymsg"].(string), "抽取 10 份奖励") {
		t.Fatal("client draw count or purchase message mismatches ten draw", before)
	}
	w := httptest.NewRecorder()
	a.gachaPlay(w, httptest.NewRequest(http.MethodPost, "/GachaPlay2", strings.NewReader(`{"gachaid":70000002,"pay_type":3,"gacha_hash":"box-ten","select_lineup_list":[],"popupid":0}`)))
	lines := bytes.Split(bytes.TrimSpace(w.Body.Bytes()), []byte{'\n'})
	if w.Code != 200 || len(lines) != 3 {
		t.Fatal("batch wire response failed", w.Body.String())
	}
	var body struct {
		Rewards []struct {
			Reward gamestate.Reward `json:"reward"`
			IDs    []int64          `json:"uniqid"`
		} `json:"rewards"`
		Adds   []int `json:"reward_adds"`
		Gachas []struct {
			Name  string `json:"gacha_name"`
			Count int    `json:"card_num"`
		} `json:"gacha_list"`
	}
	if err := json.Unmarshal(lines[1], &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rewards) != 10 || len(body.Adds) != 10 || len(body.Gachas) != 2 {
		t.Fatal("client did not receive all batch results and shared variants", w.Body.String())
	}
	for i, received := range body.Rewards {
		want := 2
		if i < 2 {
			want = 1
		}
		if received.Reward.Num != want || len(received.IDs) != 1 || received.IDs[0] != 0 || body.Adds[i] != 0 {
			t.Fatalf("draw %d wire reward/grant ID mismatch: %+v", i+1, received)
		}
	}
	for _, variant := range body.Gachas {
		if !strings.Contains(variant.Name, "第 2 轮") || !strings.Contains(variant.Name, "剩余 12 份") {
			t.Fatal("client variants have different inventory after the batch", body.Gachas)
		}
	}
	_, free := account.CoinState()
	if free != 95 || gamestate.GachaBoxStock(account.Snapshot(state).GachaBoxes[single.GroupID].Remaining) != 12 {
		t.Fatal("batch charged per draw or failed to consume ten actual slots")
	}
}
