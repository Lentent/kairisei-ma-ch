package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"kairisei.local/server/internal/gamestate"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSoloStartPublishedBossAppearance(t *testing.T) {
	for _, published := range []int{0, 900000000} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			var initial gamestate.State
			account := testAccount(t, func(s *gamestate.State) {
				s.User.Name = "外观测试"
				s.User.BP = 100
				helper := &s.PlayerProgressionPolicy.Friends.HelperReward
				helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
				s.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"1":1,"5":1,"10":0,"16":0}]}],"11":[],"12":[]}`)
				initial = *s
			})
			state := account.Snapshot(initial)
			state.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: 30010102, EnemyPartyID: 1, Battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 1, ClientEnemyPartyID: published}, {EnemyPartyID: 2}}}}
			state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: 30010102}}
			api := &API{initialState: state, account: account, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			body := fmt.Sprintf(`{"bossid":30010102,"deck_arthur_type":3,"deck_arthur_type_idx":0,"partner_deck_selects":[{"userid":%d,"arthur_type":1,"deck_idx":0},{"userid":%d,"arthur_type":2,"deck_idx":0},{"userid":%d,"arthur_type":4,"deck_idx":0}],"starttime":1,"flag":0}`, state.User.UserID, state.User.UserID, state.User.UserID)
			w := httptest.NewRecorder()
			api.teamBattleSoloStart(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
			lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
			if w.Code != 200 || len(lines) != 3 {
				t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
			}
			var response struct {
				Battles []struct {
					Party int `json:"enemy_partyid"`
				} `json:"battles"`
			}
			if err := json.Unmarshal([]byte(lines[1]), &response); err != nil {
				t.Fatal(err)
			}
			expected := published
			if expected == 0 {
				expected = 1
			}
			if len(response.Battles) != 2 || response.Battles[0].Party != expected || response.Battles[1].Party != 2 {
				t.Fatalf("wrong client parties: %+v", response)
			}
			if api.initialState.TeamBattleReplays[0].Battles[0].EnemyPartyID != 1 {
				t.Fatal("appearance changed combat identity")
			}
		})
	}
}
