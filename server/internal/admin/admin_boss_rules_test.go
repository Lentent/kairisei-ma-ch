package admin

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestDisableSoloPerDifficultyPersistsAndRestores(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{
		TeamBattleSolo:           json.RawMessage(`{"9":[],"10":[{"0":1,"4":"BOSS","10":[{"0":11,"1":0,"4":"上级","5":15,"6":8,"7":1,"10":0,"24":0},{"0":111,"1":1,"4":"上级","5":15,"6":8,"7":1,"10":0,"24":1},{"0":12,"1":0,"4":"超级","5":20,"6":10,"7":1,"10":0,"24":0}]}],"11":[],"12":[]}`),
		TeamBattlePastBossGroups: []json.RawMessage{json.RawMessage(`{"0":1,"13":[{"0":11,"1":0,"24":0},{"0":111,"1":1,"24":1},{"0":12,"1":0,"24":0}]}`)},
		TeamBattleOwnDeckSources: map[int]int{111: 11},
		TeamBattleRewards:        []gamestate.TeamBattleRewardProfile{{BossID: 11}, {BossID: 111}, {BossID: 12}},
	}
	if err := ops.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	router := chi.NewRouter()
	router.Put("/rules", a.saveBossRules)
	rule := BossRules{BossID: 11, Standard: true, SoloDisabled: true, OwnDeck: true, Continue: true}
	save := func(revision int, rules any, status int) {
		t.Helper()
		testfixture.CallContentAdmin(t, router, "PUT", "/rules", map[string]any{"expected_revision": revision, "rules": rules}, status)
	}
	check := func(state gamestate.State, disabled bool) {
		t.Helper()
		var top map[string][]map[string]json.RawMessage
		if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
			t.Fatal(err)
		}
		groups := []map[string]json.RawMessage{top["10"][0]}
		var past map[string]json.RawMessage
		if err := json.Unmarshal(state.TeamBattlePastBossGroups[0], &past); err != nil {
			t.Fatal(err)
		}
		past["10"] = past["13"]
		groups = append(groups, past)
		for _, group := range groups {
			for _, id := range []int{11, 111, 12} {
				r, ok := game.TeamBattleGroupEntryRules(group, id)
				wantSolo := !disabled || id == 12
				if !ok || r.AllowsSolo() != wantSolo || r.AllowsMultiplayer() != (id != 111) {
					t.Fatalf("boss %d rules = %+v, want solo %v with rooms preserved", id, r, wantSolo)
				}
				wantStart := 0
				if id == 111 {
					wantStart = 1
				}
				if disabled && id != 12 {
					wantStart = 3
				}
				if r.StartRule != wantStart {
					t.Fatalf("boss %d native start rule = %d, want %d", id, r.StartRule, wantStart)
				}
			}
		}
		if state.DisabledTeamBattleBossIDs[11] || state.DisabledTeamBattleBossIDs[12] || state.DisabledTeamBattleBossIDs[111] != disabled {
			t.Fatal("ordinary rooms closed or own-deck alias not gated")
		}
		if len(state.TeamBattleRewards) != 3 {
			t.Fatal("mode change removed rewards")
		}
	}
	check(ops.content.configuration.State, false)
	save(0, []BossRules{rule}, 200)
	check(ops.content.configuration.State, true)
	// Stale writes cannot reopen solo.
	rule.SoloDisabled = false
	save(0, []BossRules{rule}, 409)
	check(ops.content.configuration.State, true)
	// Older admin clients do not know the option; unrelated changes preserve it.
	save(1, []map[string]any{{"boss_id": 11, "standard": true, "own_deck": true, "continue": false}}, 200)
	check(ops.content.configuration.State, true)
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	check(restarted.content.configuration.State, true)
	if !restarted.content.rules[11].SoloDisabled || !restarted.content.rules[11].OwnDeck {
		t.Fatal("solo restriction or own-deck preference lost on restart")
	}
	save(2, []BossRules{rule}, 200)
	check(ops.content.configuration.State, false)
	check(base, false)
	// Legacy persisted JSON also defaults to allowing solo.
	var legacy BossRules
	if err := json.Unmarshal([]byte(`{"boss_id":11,"standard":true,"own_deck":true,"continue":true}`), &legacy); err != nil || legacy.SoloDisabled {
		t.Fatal("legacy rules changed solo behavior", err)
	}
}
