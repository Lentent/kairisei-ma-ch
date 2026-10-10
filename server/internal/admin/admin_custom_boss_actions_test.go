package admin

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomBossActionCatalogSaveRestartAndRejectInvalid(t *testing.T) {
	base, path := customBossFixture(t)
	questReward := base.TeamBattleRewards[0]
	questReward.StageQuestAreaID, questReward.StageQuestStageID = 7, 3
	base.TeamBattleRewards = append(base.TeamBattleRewards, questReward)
	dir := filepath.Join(filepath.Dir(path), "cn602-battle-master")
	skill := make([]string, 50)
	skill[0], skill[1], skill[10], skill[13], skill[19], skill[49] = "99", "借用攻击", "ATTACK", "PHYSICS", "USER_ONE", "99"
	role := make([]string, 32)
	role[0], role[8], role[9], role[20], role[24] = "99", "ATTACK_AA", "SELECT", "10000", "1"
	buffSkill, buffRole := append([]string(nil), skill...), append([]string(nil), role...)
	buffSkill[0], buffSkill[1], buffSkill[10], buffSkill[19], buffSkill[49] = "100", "玩家强化", "SUPPORT", "USER_ALL", "100"
	buffRole[0], buffRole[8], buffRole[20], buffRole[21], buffRole[22], buffRole[23], buffRole[24], buffRole[25] = "100", "ATK_UP_FIXED", "3", "ATK", "1000", "1000", "0", "0"
	for name, row := range map[string][]string{"skill_enemy.csv": skill, "skill_role_enemy.csv": role} {
		extra := buffSkill
		if name == "skill_role_enemy.csv" {
			extra = buffRole
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(row, ",")+"\n"+strings.Join(extra, ",")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "enemy_lvup.csv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i, line := range lines {
		fields := strings.Split(line, ",")
		fields[40] = "99"
		fields[53] = "100"
		lines[i] = strings.Join(fields, ",")
	}
	if err = os.WriteFile(filepath.Join(dir, "enemy_lvup.csv"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	r := chi.NewRouter()
	r.Get("/skills", a.customBossSkills)
	r.Get("/buffs", a.customBossBuffs)
	r.Post("/copies", a.createCustomBoss)
	r.Put("/copies", a.saveCustomBoss)
	catalog := testfixture.CallContentAdmin(t, r, "GET", "/skills?source_boss_id=11", nil, 200)
	if !strings.Contains(string(catalog), "借用攻击") || !strings.Contains(string(catalog), `"attack":true`) {
		t.Fatal("source skill catalogue missing", string(catalog))
	}
	if !strings.Contains(string(catalog), `"player_buff":true`) || !strings.Contains(string(catalog), "全体玩家：物理攻击增加 1000 点，持续 3 回合") {
		t.Fatal("player buff explanation missing from skill API", string(catalog))
	}
	if !strings.Contains(string(catalog), `"buff_editors"`) {
		t.Fatal("buff numeric editor missing")
	}
	quick := testfixture.CallContentAdmin(t, r, "GET", "/buffs", nil, 200)
	if !strings.Contains(string(quick), `"function_id":100`) || strings.Contains(string(quick), `"function_id":99`) {
		t.Fatal("quick picker must include pure player buffs and exclude attacks", string(quick))
	}
	var created struct {
		Boss customBoss `json:"boss"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, r, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 200), &created); err != nil {
		t.Fatal(err)
	}
	b := created.Boss
	if b.Reward.StageQuestAreaID != 0 || b.Reward.StageQuestStageID != 0 {
		t.Fatal("standalone copy retained quest reward context")
	}
	power := 5000
	b.Targets[0].Actions = []gamestate.TeamBattleEnemyAction{{SourceBossID: 11, Turn: 1, RepeatEvery: 2, SkillID: 99, FunctionID: 99, Target: "AUTO", Power: &power}}
	b.Targets[0].IncludeOriginalActions = true
	b.Targets[0].Actions = append(b.Targets[0].Actions, gamestate.TeamBattleEnemyAction{SourceBossID: 11, Turn: 1, SkillID: 100, FunctionID: 100, Target: "AUTO", Buffs: []gamestate.TeamBattleEnemyBuffOverride{{RoleIndex: 0, Value: customBossActionTestInt(2500), Duration: customBossActionTestInt(7)}}})
	b.Targets[1].Actions = []gamestate.TeamBattleEnemyAction{}
	input := map[string]any{"expected_revision": 1, "boss_id": b.BossID, "name": b.Name, "difficulty": b.Difficulty, "bp_use": b.BPUse, "bp_use_half": b.BPUseHalf, "enabled": false, "continue": b.Continue, "targets": b.Targets}
	testfixture.CallContentAdmin(t, r, "PUT", "/copies", input, 200)
	projected := ops.ContentConfiguration().State.TeamBattleReplays[1]
	if *projected.EnemyOverrides[0].Actions[1].Buffs[0].Value != 2500 || *projected.EnemyOverrides[0].Actions[1].Buffs[0].Duration != 7 || projected.EnemyOverrides[0].AnimationModel == "" {
		t.Fatal("buff overrides or animation mode lost in projection")
	}
	if !projected.EnemyOverrides[0].IncludeOriginalActions || projected.EnemyOverrides[1].IncludeOriginalActions || *projected.EnemyOverrides[0].Actions[0].Power != 5000 || projected.EnemyOverrides[1].Actions == nil || len(projected.EnemyOverrides[2].Actions) != 0 {
		t.Fatal("projection lost action/idle/inherited configuration")
	}
	b.Targets[0].Actions[0].FunctionID = 999
	input["targets"] = b.Targets
	input["expected_revision"] = 2
	testfixture.CallContentAdmin(t, r, "PUT", "/copies", input, 400)
	if ops.content.customBossRevision != 2 {
		t.Fatal("invalid action persisted")
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	saved := restarted.content.customBosses[0].Targets
	if *saved[0].Actions[1].Buffs[0].Value != 2500 || *saved[0].Actions[1].Buffs[0].Duration != 7 {
		t.Fatal("buff overrides lost after restart")
	}
	if !saved[0].IncludeOriginalActions || saved[1].IncludeOriginalActions || *saved[0].Actions[0].Power != 5000 || saved[0].Actions[0].FunctionID != 99 || saved[1].Actions == nil || saved[2].Actions != nil {
		t.Fatal("restart changed action configuration")
	}
	if len(restarted.content.configuration.State.TeamBattleReplays[0].EnemyOverrides) != 0 {
		t.Fatal("original boss was modified")
	}
}

func customBossActionTestInt(n int) *int { return &n }
