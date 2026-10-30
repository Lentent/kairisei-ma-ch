package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestBossStatsSaveRestartPackageUpgradeAndRestore(t *testing.T) {
	base, path := customBossFixture(t)
	// Another difficulty and an own-deck alias share the same enemy master.
	var top map[string]json.RawMessage
	_ = json.Unmarshal(base.TeamBattleSolo, &top)
	var groups []map[string]json.RawMessage
	_ = json.Unmarshal(top["10"], &groups)
	var bosses []map[string]json.RawMessage
	_ = json.Unmarshal(groups[0]["10"], &bosses)
	var other map[string]json.RawMessage
	raw, _ := json.Marshal(bosses[0])
	_ = json.Unmarshal(raw, &other)
	other["0"] = json.RawMessage("12")
	bosses = append(bosses, other)
	groups[0]["10"], _ = json.Marshal(bosses)
	top["10"], _ = json.Marshal(groups)
	base.TeamBattleSolo, _ = json.Marshal(top)
	for _, id := range []int{12, 1111} {
		r := base.TeamBattleReplays[0]
		r.BossID = id
		base.TeamBattleReplays = append(base.TeamBattleReplays, r)
		p := base.TeamBattleRewards[0]
		p.BossID = id
		base.TeamBattleRewards = append(base.TeamBattleRewards, p)
	}
	base.TeamBattleOwnDeckSources = map[int]int{1111: 11}
	base.TeamBattleReplays[2].EnemyOverrides = []gamestate.TeamBattleEnemyOverride{{Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 200, Attribute: "FIRE", Magic: 66}}}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	newOps := func() *Operations {
		t.Helper()
		o, err := NewOperations(accounts.Database(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = o.InitializeContent(base, path); err != nil {
			t.Fatal(err)
		}
		return o
	}
	o := newOps()
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Get("/stats", a.bossStats)
	router.Put("/stats", a.saveBossStats)
	router.Post("/copies", a.createCustomBoss)
	call := func(method, url string, body any, status int) []byte {
		t.Helper()
		return testfixture.CallContentAdmin(t, router, method, url, body, status)
	}
	var initial struct {
		Boss      DropBoss          `json:"boss"`
		Overrides []bossStatsTarget `json:"overrides"`
	}
	if err := json.Unmarshal(call("GET", "/stats?boss_id=11", nil, 200), &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Overrides) != 0 || initial.Boss.Targets[0].Stats.HP != 200 {
		t.Fatal("default must use package values including party multiplier")
	}
	patches := []bossStatsTarget{{BattleIndex: 0, EnemyIndex: 0, EnemyID: 1, Values: map[string]int{"hp": 999, "attack": 0, "attribute_rates:0": 50}}, {BattleIndex: 1, EnemyIndex: 0, EnemyID: 1, Values: map[string]int{"hp": 777}}}
	body := map[string]any{"boss_id": 11, "expected_revision": 0, "overrides": patches}
	call("PUT", "/stats", body, 200)
	snapshot := o.ContentConfiguration().State
	if snapshot.TeamBattleReplays[0].AutoStartBPUse != 0 {
		t.Fatal("stat edits changed original AI entry price")
	}
	if len(snapshot.TeamBattleReplays[0].EnemyOverrides) != 2 || snapshot.TeamBattleReplays[0].EnemyOverrides[0].Stats.HP != 999 || snapshot.TeamBattleReplays[0].EnemyOverrides[0].Stats.Attack != 0 || snapshot.TeamBattleReplays[0].EnemyOverrides[1].Stats.HP != 777 {
		t.Fatal("per-wave values or explicit zero lost")
	}
	if *snapshot.TeamBattleReplays[0].EnemyOverrides[0].Stats.AttributeRates != [9]int{50, 100, 100, 100, 100, 100, 100, 100, 100} {
		t.Fatal("individual attribute rate patch lost")
	}
	if len(snapshot.TeamBattleReplays[1].EnemyOverrides) != 0 || snapshot.TeamBattleReplays[2].EnemyOverrides[0].Stats.HP != 999 {
		t.Fatal("difficulty isolation or own-deck alias incorrect")
	}
	if snapshot.TeamBattleReplays[2].EnemyOverrides[0].Stats.Magic != 66 {
		t.Fatal("unmodified alias package defaults were replaced")
	}
	if len(base.TeamBattleReplays[0].EnemyOverrides) != 0 || o.content.bosses[11].Targets[0].Stats.HP != 200 || !reflect.DeepEqual(snapshot.TeamBattleRewards, base.TeamBattleRewards) || string(snapshot.TeamBattleSolo) != string(base.TeamBattleSolo) {
		t.Fatal("package, rewards, identities or catalog mutated")
	}
	listed := o.namedBossGroups([]AdminBattleGroup{{Bosses: []DropBoss{o.content.bosses[11], o.content.bosses[12]}}})[0].Bosses
	if !listed[0].StatsEdited || listed[0].Targets[0].Stats.HP != 999 || listed[1].StatsEdited {
		t.Fatal("catalog must mark edited difficulty and show its effective values")
	}
	// Conflict never changes stored or live values.
	call("PUT", "/stats", body, 409)
	if o.content.bossStatsRevision != 1 {
		t.Fatal("conflict advanced revision")
	}
	call("POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 200)
	if o.content.customBosses[0].Targets[0].Stats.HP != 200 {
		t.Fatal("native edit leaked into package copy template")
	}
	copyReplays := o.ContentConfiguration().State.TeamBattleReplays
	if copyReplays[len(copyReplays)-1].AutoStartBPUse != 20 {
		t.Fatal("custom copy must retain explicit solo AI entry price")
	}
	// Editing only HP/attack must allow other fields to follow a new package.
	enemyPath := filepath.Join(filepath.Dir(path), "cn602-battle-master", "enemy.csv")
	data, err := os.ReadFile(enemyPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	fields := strings.Split(lines[0], ",")
	fields[8], fields[10] = "150", "88"
	lines[0] = strings.Join(fields, ",")
	if err = os.WriteFile(enemyPath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := newOps()
	a.operations = restarted
	r := restarted.ContentConfiguration().State.TeamBattleReplays[0]
	if r.EnemyOverrides[0].Stats.HP != 999 || r.EnemyOverrides[0].Stats.Magic != 88 {
		t.Fatal("saved patch did not retain overridden field and inherit upgraded package field")
	}
	if snapshot.TeamBattleReplays[0].EnemyOverrides[0].Stats.Magic != 20 {
		t.Fatal("existing room/config snapshot was mutated")
	}
	var reloaded struct {
		Boss      DropBoss          `json:"boss"`
		Overrides []bossStatsTarget `json:"overrides"`
	}
	_ = json.Unmarshal(call("GET", "/stats?boss_id=11", nil, 200), &reloaded)
	if reloaded.Boss.Targets[0].Stats.HP != 300 || len(reloaded.Overrides) != 2 {
		t.Fatal("editor must show new package defaults separately from overrides")
	}
	call("PUT", "/stats", map[string]any{"expected_revision": 1, "boss_id": 11, "overrides": []bossStatsTarget{}}, 200)
	if len(restarted.ContentConfiguration().State.TeamBattleReplays[0].EnemyOverrides) != 0 || len(restarted.content.bossStats) != 0 {
		t.Fatal("restore did not remove overrides")
	}
	if len(newOps().content.bossStats) != 0 {
		t.Fatal("restore did not persist")
	}
}

func TestBossStatsRejectInvalidAndPreservePackagedOverrides(t *testing.T) {
	base, path := customBossFixture(t)
	base.TeamBattleReplays[0].EnemyOverrides = []gamestate.TeamBattleEnemyOverride{{BattleIndex: 0, EnemyIndex: 0, Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 600, Attribute: "FIRE", Attack: 20}, Actions: []gamestate.TeamBattleEnemyAction{}}}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = o.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Get("/stats", a.bossStats)
	router.Put("/stats", a.saveBossStats)
	call := func(body any, status int) {
		t.Helper()
		testfixture.CallContentAdmin(t, router, "PUT", "/stats", body, status)
	}
	valid := bossStatsTarget{BattleIndex: 0, EnemyIndex: 0, EnemyID: 1, Values: map[string]int{"hp": 999}}
	for _, bad := range []bossStatsTarget{
		{EnemyID: 99, Values: map[string]int{"hp": 999}},
		{BattleIndex: 2, EnemyID: 1, Values: map[string]int{"hp": 999}},
		{EnemyID: 1, Values: map[string]int{"enemy_id": 2}},
		{EnemyID: 1, Values: map[string]int{"hp": 0}},
		{EnemyID: 1, Values: map[string]int{"attribute_rates:9": 50}},
		{EnemyID: 1, Values: map[string]int{"status_resistances:1": 101}},
		{EnemyID: 1, Values: map[string]int{"attribute_rates:0": -1}},
		{EnemyID: 1, Attribute: customBossStatsString("FIRE_ICE")},
		{EnemyID: 1, OriginalSkills: []gamestate.TeamBattleEnemyOriginalSkill{{Slot: 1, SkillID: 1, FunctionID: 1, Power: customBossActionTestInt(100)}}},
	} {
		call(map[string]any{"boss_id": 11, "expected_revision": 0, "overrides": []bossStatsTarget{bad}}, 400)
	}
	call(map[string]any{"boss_id": 11, "expected_revision": 0, "overrides": []bossStatsTarget{valid, valid}}, 400)
	call(map[string]any{"boss_id": 11, "expected_revision": 0}, 400)
	call(map[string]any{"boss_id": 999, "expected_revision": 0, "overrides": []bossStatsTarget{}}, 400)
	req := httptest.NewRequest(http.MethodPut, "/stats", strings.NewReader(`{"boss_id":11,"expected_revision":0,"overrides":[]}`))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("missing mutation guard")
	}
	if o.content.bossStatsRevision != 0 {
		t.Fatal("invalid requests persisted")
	}
	call(map[string]any{"boss_id": 11, "expected_revision": 0, "overrides": []bossStatsTarget{valid}}, 200)
	r := o.ContentConfiguration().State.TeamBattleReplays[0].EnemyOverrides[0]
	if r.Stats.HP != 999 || r.Stats.Attack != 20 || r.Actions == nil {
		t.Fatal("override lost packaged stats/actions")
	}
	call(map[string]any{"boss_id": 11, "expected_revision": 1, "overrides": []bossStatsTarget{}}, 200)
	r = o.ContentConfiguration().State.TeamBattleReplays[0].EnemyOverrides[0]
	if r.Stats.HP != 600 || r.Actions == nil || base.TeamBattleReplays[0].EnemyOverrides[0].Stats.HP != 600 {
		t.Fatal("restore must retain packaged overrides without mutation")
	}
}

func TestBossStatsOriginalSkillsUseExistingBattleOverrides(t *testing.T) {
	base, path := customBossFixture(t)
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = o.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	o.content.customSkillCatalog = &multiplayer.CombatCatalog{
		EnemyLevels:     map[int]multiplayer.CombatEnemyLevel{1: {Actions: []multiplayer.CombatEnemyAction{{Slot: 1, Category: "skill", SkillID: 99}}}},
		EnemySkills:     map[int][]multiplayer.CombatSkillDefinition{99: {{ID: 99, FunctionID: 99, Target: "USER_ONE"}}},
		EnemySkillRoles: map[int][]multiplayer.CombatSkillRole{99: {{Function: "ATTACK_AA"}}},
	}
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Get("/stats", a.bossStats)
	router.Put("/stats", a.saveBossStats)
	body := map[string]any{"boss_id": 11, "expected_revision": 0, "overrides": []bossStatsTarget{{EnemyID: 1, OriginalSkills: []gamestate.TeamBattleEnemyOriginalSkill{{Slot: 1, SkillID: 99, FunctionID: 99, Power: customBossActionTestInt(4000)}}}}}
	raw := testfixture.CallContentAdmin(t, router, "PUT", "/stats", body, 200)
	if !strings.Contains(string(raw), `"editable":true`) {
		t.Fatal("native original skill editor metadata missing")
	}
	r := o.ContentConfiguration().State.TeamBattleReplays[0].EnemyOverrides[0]
	if r.Actions != nil || *r.OriginalSkills[0].Power != 4000 || r.Stats.HP != 200 {
		t.Fatal("editing original skill replaced native AI or lost package stats")
	}
}

func customBossStatsString(v string) *string { return &v }

func TestBossStatsCupPreservesScoreRulesAcrossSaveRestartAndRestore(t *testing.T) {
	base, path := customBossFixture(t)
	// Model a native 3-billion-HP cup body without changing the shared party.
	enemyPath := filepath.Join(filepath.Dir(path), "cn602-battle-master", "enemy.csv")
	data, err := os.ReadFile(enemyPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	fields := strings.Split(lines[0], ",")
	fields[8] = "1500000000" // The fixture party doubles the body's HP.
	lines[0] = strings.Join(fields, ",")
	if err = os.WriteFile(enemyPath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	policy := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", Rate: 150, EndTurn: 7, TeamCostInitial: 6, MemberDeadEnd: true}
	for i := range 9 {
		policy.Grades = append(policy.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	base.TeamBattleRewards[0].ScorePolicy = policy
	base.TeamBattleReplays[0].Battles = base.TeamBattleReplays[0].Battles[:1]
	base.TeamBattleOwnDeckSources = map[int]int{1111: 11}
	alias := base.TeamBattleReplays[0]
	alias.BossID = 1111
	base.TeamBattleReplays = append(base.TeamBattleReplays, alias)
	aliasReward := base.TeamBattleRewards[0]
	aliasReward.BossID = 1111
	base.TeamBattleRewards = append(base.TeamBattleRewards, aliasReward)
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	newOps := func() *Operations {
		t.Helper()
		o, err := NewOperations(accounts.Database(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = o.InitializeContent(base, path); err != nil {
			t.Fatal(err)
		}
		return o
	}
	o := newOps()
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Get("/stats", a.bossStats)
	router.Put("/stats", a.saveBossStats)
	router.Put("/activities", a.saveActivityRewards)
	router.Post("/copies", a.createCustomBoss)
	initial := o.ContentConfiguration().State
	var editor struct {
		Boss DropBoss `json:"boss"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/stats?boss_id=11", nil, 200), &editor); err != nil {
		t.Fatal(err)
	}
	if editor.Boss.Targets[0].Stats.HP != 3000000000 {
		t.Fatal("cup editor lost native HP or party multiplier")
	}
	patch := []bossStatsTarget{{EnemyID: 1, Values: map[string]int{"attack": 999}}, {EnemyID: 2, EnemyIndex: 1, Values: map[string]int{"hp": 3000000000}}}
	testfixture.CallContentAdmin(t, router, "PUT", "/stats", map[string]any{"boss_id": 11, "expected_revision": 0, "overrides": patch}, 200)
	assertCup := func(state gamestate.State, wantPolicy *gamestate.TeamBattleScorePolicy) {
		t.Helper()
		for i, r := range state.TeamBattleReplays {
			if len(r.EnemyOverrides) != 2 || r.EnemyOverrides[0].Stats.HP != 3000000000 || r.EnemyOverrides[0].Stats.Attack != 999 || r.EnemyOverrides[1].Stats.HP != 3000000000 {
				t.Fatalf("cup stats did not reach entry %d: %+v", r.BossID, r.EnemyOverrides)
			}
			if r.EnemyOverrides[0].Actions != nil || r.EndTurn != wantPolicy.EndTurn || r.CostInitial != 3 || !reflect.DeepEqual(state.TeamBattleRewards[i].ScorePolicy, wantPolicy) {
				t.Fatal("cup stat edit changed native AI, solo COST, or score rules")
			}
		}
	}
	saved := o.ContentConfiguration().State
	assertCup(saved, policy)
	if !reflect.DeepEqual(saved.TeamBattleRewards, initial.TeamBattleRewards) || !reflect.DeepEqual(initial.TeamBattleReplays[0].EnemyOverrides, base.TeamBattleReplays[0].EnemyOverrides) {
		t.Fatal("stat edit mutated rewards or an existing snapshot")
	}
	// The activity editor and the stats editor must compose in either order.
	config, err := o.content.activityDefaults()
	if err != nil {
		t.Fatal(err)
	}
	config.Cups[11].Rate, config.Cups[11].EndTurn = 200, 5
	testfixture.CallContentAdmin(t, router, "PUT", "/activities", map[string]any{"expected_revision": 0, "config": config}, 200)
	assertCup(o.ContentConfiguration().State, config.Cups[11])
	assertCup(saved, policy)
	a.operations = newOps()
	assertCup(a.operations.ContentConfiguration().State, config.Cups[11])
	testfixture.CallContentAdmin(t, router, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 400)
	patch[0].Values["hp"] = 3000000001
	testfixture.CallContentAdmin(t, router, "PUT", "/stats", map[string]any{"boss_id": 11, "expected_revision": 1, "overrides": patch}, 400)
	testfixture.CallContentAdmin(t, router, "PUT", "/stats", map[string]any{"boss_id": 11, "expected_revision": 1, "overrides": []bossStatsTarget{}}, 200)
	for _, r := range newOps().ContentConfiguration().State.TeamBattleReplays {
		if len(r.EnemyOverrides) != 0 {
			t.Fatal("cup restore did not persist")
		}
	}
}

func TestBossStatsCupRuntimeTemplates(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("requires full runtime resource set")
	}
	path := filepath.Join(root, "_local/control/server/cn602-battle-runtime-master.json")
	master, err := masterdata.LoadBattleRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{TeamBattleSolo: json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)}
	if err = masterdata.ApplyBattleRuntimeMaster(&base, master); err != nil {
		t.Fatal(err)
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = o.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	cups := 0
	for _, boss := range o.content.bosses {
		// Older package snapshots have native cup entries without score policy
		// metadata. Still exercise their real parties and enemy master values.
		if !strings.HasPrefix(boss.Name, "圣剑杯") || o.content.rewardBossID(boss.BossID) != boss.BossID {
			continue
		}
		b, err := o.content.originalBossSource(boss.BossID)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range b.Targets {
			// Editing attack alone must preserve all package defaults, including HP.
			if _, err := o.content.validateBossStats(b.BossID, []bossStatsTarget{{BattleIndex: target.BattleIndex, EnemyIndex: target.EnemyIndex, EnemyID: target.Stats.EnemyID, Values: map[string]int{"attack": target.Stats.Attack}}}); err != nil {
				t.Fatalf("cup %d part %d: %v", b.BossID, target.EnemyIndex, err)
			}
		}
		cups++
	}
	if cups == 0 {
		t.Fatal("runtime set did not exercise any cup templates")
	}
	t.Logf("validated %d native cup templates", cups)
}
