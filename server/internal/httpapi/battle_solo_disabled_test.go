package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func TestSoloDisabledRejectsStaleStartButAllowsRooms(t *testing.T) {
	const boss = 30010102
	s := testAccount(t, func(state *gamestate.State) {
		state.User.UserID = 1001
		state.User.BP, state.User.BPMax = 100, 100
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"1":0,"5":20,"6":10,"10":0,"16":0,"24":3}]}],"11":[],"12":[]}`)
	})
	runtime := gamestate.State{
		TeamBattleReplays:        []gamestate.TeamBattleReplay{{BossID: boss, EnemyPartyID: 1, EnemyType: 1, CostInitial: 3, HoldMax: 5}},
		TeamBattleRewards:        []gamestate.TeamBattleRewardProfile{{BossID: boss}},
		TeamBattlePastBossGroups: []json.RawMessage{json.RawMessage(`{"0":300101,"13":[{"0":30010102,"24":3}]}`)},
	}
	runtime.User.UserID = 1001
	a := &API{initialState: runtime, account: s, multiplayer: multiplayer.NewHub()}
	for _, flag := range []int{0, 1, 2} {
		w := httptest.NewRecorder()
		body := fmt.Sprintf(`{"bossid":30010102,"deck_arthur_type":1,"deck_arthur_type_idx":0,"partner_deck_selects":[{},{},{}],"starttime":1,"flag":%d}`, flag)
		a.teamBattleSoloStart(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "this battle requires multiplayer") {
			t.Fatalf("solo flag %d: HTTP %d %s", flag, w.Code, w.Body.String())
		}
		if got := s.Snapshot(runtime).User.BP; got != 100 {
			t.Fatalf("rejected solo entry charged BP: %d", got)
		}
	}
	for _, auto := range []bool{false, true} {
		if _, err := a.issueTeamBattleRoom(teamBattleRoomIssue{BossID: boss, DeckArthurType: 1, AutoStart: auto}); err != nil {
			t.Fatalf("room auto=%v rejected after disabling solo: %v", auto, err)
		}
	}
}
