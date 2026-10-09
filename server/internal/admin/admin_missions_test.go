package admin

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/testfixture"
)

func TestMissionPolicySaveReloadAndValidation(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	o.playerDefaults = &PlayerPolicy{StoryCrystals: 1, Notice: noticePolicy{Title: "公告", Body: "欢迎"}}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	config := o.playerPolicy.Load().Value
	if len(config.Missions) != 9 {
		t.Fatal("legacy policy missing default missions")
	}
	config.Missions[1].Target, config.Missions[1].Crystals = 3, 8
	config.Missions[2].Enabled = false
	a := &API{operations: o}
	r := chi.NewRouter()
	r.Put("/policy", a.savePlayerPolicy)
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 200)
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 409)
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	p := o.playerPolicy.Load().Runtime.Missions
	if p[1].Target != 3 || p[1].Crystals != 8 || p[2].Enabled {
		t.Fatal("mission policy not restored")
	}
	// An older admin client must not erase configured missions.
	config.Missions = nil
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": config}, 200)
	if o.playerPolicy.Load().Value.Missions[1].Target != 3 {
		t.Fatal("legacy client reset mission policy")
	}
	for _, mutate := range []func([]game.MissionDefinition){
		func(m []game.MissionDefinition) { m[1].Target = 0 },
		func(m []game.MissionDefinition) { m[1].Crystals = -1 },
		func(m []game.MissionDefinition) { m[1].ID = m[0].ID },
		func(m []game.MissionDefinition) { m[1].Kind = "level" },
		func(m []game.MissionDefinition) { m[0].Target = 2 },
		func(m []game.MissionDefinition) { m[1].Title = " " },
	} {
		config.Missions = game.DefaultMissions()
		mutate(config.Missions)
		testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 2, "config": config}, 400)
	}
	doc, err := accounts.Database().ReadDocument(playerPolicyKey)
	if err != nil {
		t.Fatal(err)
	}
	var stored PlayerPolicy
	if err := json.Unmarshal(doc.Payload, &stored); err != nil {
		t.Fatal(err)
	}
	if doc.Revision != 2 || stored.Missions[1].Target != 3 {
		t.Fatal("invalid save mutated document")
	}
}
