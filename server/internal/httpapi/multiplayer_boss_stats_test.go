package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func TestOriginalBossStatsDoNotChangeAIRoomEntryCost(t *testing.T) {
	for _, tc := range []struct {
		name           string
		override, auto bool
		aiCost         int
		wantError      bool
	}{
		{"package", false, true, 0, false},
		{"edited-original", true, true, 0, false},
		{"custom-ai", true, true, 20, true},
		{"custom-multiplayer", true, false, 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testAccount(t, func(state *gamestate.State) {
				state.User.UserID = 1001
				state.User.BP, state.User.BPMax = 10, 10
				state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"1":0,"5":20,"6":10,"10":0,"16":0,"24":0}]}],"11":[],"12":[]}`)
			})
			replay := gamestate.TeamBattleReplay{BossID: 30010102, EnemyPartyID: 1, EnemyType: 1, CostInitial: 3, HoldMax: 5, AutoStartBPUse: tc.aiCost}
			if tc.override {
				replay.EnemyOverrides = []gamestate.TeamBattleEnemyOverride{{Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 200, Attribute: "FIRE"}}}
			}
			runtime := gamestate.State{TeamBattleReplays: []gamestate.TeamBattleReplay{replay}, TeamBattleRewards: []gamestate.TeamBattleRewardProfile{{BossID: 30010102}}}
			runtime.User.UserID = 1001
			a := &API{initialState: runtime, account: s, multiplayer: multiplayer.NewHub()}
			_, err := a.issueTeamBattleRoom(teamBattleRoomIssue{BossID: 30010102, DeckArthurType: 1, AutoStart: tc.auto})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "battle points are insufficient") {
					t.Fatalf("expected custom AI cost, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
