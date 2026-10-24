package admin

import (
	"encoding/json"
	"slices"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/testfixture"
)

func TestBossMissionCatalogValidationAndTargetIdentity(t *testing.T) {
	a, handler := dynamicMissionTestAPI(t)
	a.groups = []AdminBattleGroup{{Name: "指定BOSS", BossIDs: []int{123, 124}, Difficulties: []string{"超级", "超弩级"}}}
	a.pastGroups = []AdminBattleGroup{{Name: "往期指定BOSS", BossIDs: []int{124, 125}, Difficulties: []string{"超弩级", "地狱级"}}}
	response := testfixture.CallContentAdmin(t, handler, "GET", "/api/missions", nil, 200)
	var data struct {
		Targets []missionBossTarget     `json:"boss_targets"`
		Kinds   []struct{ Kind string } `json:"supported_kinds"`
	}
	if err := json.Unmarshal(response, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Targets) != 3 || data.Targets[1].BossID != 124 || data.Targets[1].Difficulty != "超弩级" || !slices.ContainsFunc(data.Kinds, func(k struct{ Kind string }) bool { return k.Kind == "boss_clear" }) {
		t.Fatalf("boss mission metadata lost difficulty or duplicated catalog entries: %+v", data)
	}
	def := newTestMission()
	def.Kind, def.BossID = "boss_clear", 124
	for _, id := range []int{0, -1, 999, 2147483648} {
		bad := def
		bad.BossID = id
		testfixture.CallContentAdmin(t, handler, "PUT", "/api/missions", map[string]any{"expected_revision": 1, "config": MissionPolicy{Missions: []game.MissionDefinition{bad}}}, 400)
	}
	bad := def
	bad.Kind = "explore"
	testfixture.CallContentAdmin(t, handler, "PUT", "/api/missions", map[string]any{"expected_revision": 1, "config": MissionPolicy{Missions: []game.MissionDefinition{bad}}}, 400)
	save := func(revision int, m game.MissionDefinition) game.MissionDefinition {
		t.Helper()
		testfixture.CallContentAdmin(t, handler, "PUT", "/api/missions", map[string]any{"expected_revision": revision, "config": MissionPolicy{Missions: []game.MissionDefinition{m}}}, 200)
		return a.operations.missionPolicy.Load().Value.Config.Missions[0]
	}
	def = save(1, def)
	firstID := def.ID
	def.Title, def.Target = "击败指定超弩级BOSS", 5
	def = save(2, def)
	if def.ID != firstID || def.BossID != 124 {
		t.Fatal("ordinary edits changed task identity or lost boss target")
	}
	def.BossID = 123
	def = save(3, def)
	if def.ID == firstID || !slices.Contains(a.operations.missionPolicy.Load().Value.RetiredIDs, firstID) {
		t.Fatal("changing difficulty inherited the old target's progress/claim identity")
	}
	def.Daily = false
	def = save(4, def)
	if err := a.operations.loadMissionPolicy(); err != nil {
		t.Fatal(err)
	}
	got := a.operations.missionPolicy.Load().Value.Config.Missions[0]
	if got.ID != def.ID || got.BossID != 123 || got.Daily {
		t.Fatal("reload lost boss mission configuration", got)
	}
}
