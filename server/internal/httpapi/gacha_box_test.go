package httpapi

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
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
	if !strings.Contains(info["buymsg"].(string), "4/50") || !strings.Contains(info["gacha_name"].(string), "1000") || info["play_count_max"] != 0 || info["is_stepup_price"] != 0 || info["play_count"].(int) > math.MaxInt32 {
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
