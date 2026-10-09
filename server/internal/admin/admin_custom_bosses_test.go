package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func customBossFixture(t *testing.T) (gamestate.State, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "cn602-battle-master")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, rows [][]string) {
		t.Helper()
		var text strings.Builder
		for _, r := range rows {
			text.WriteString(strings.Join(r, ","))
			text.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	party := make([]string, 17)
	party[0], party[2], party[3], party[5], party[6], party[7] = "101", "1", "2", "2", "1", "0"
	write("enemy_party.csv", [][]string{party})
	enemies, levels := [][]string{}, [][]string{}
	for id := 1; id <= 2; id++ {
		r := make([]string, 25)
		r[0], r[6], r[7], r[8], r[9], r[10], r[11], r[12], r[13] = strconv.Itoa(id), "部位"+strconv.Itoa(id), "FIRE", "100", "10", "20", "3", "4", "5"
		enemies = append(enemies, r)
		lv := make([]string, 313)
		lv[0], lv[1] = strconv.Itoa(id), "1"
		for j := 2; j < 11; j++ {
			lv[j] = "100"
		}
		levels = append(levels, lv)
	}
	write("enemy.csv", enemies)
	write("enemy_lvup.csv", levels)
	base := gamestate.State{
		TeamBattleSolo:    json.RawMessage(`{"9":[],"10":[{"0":700000010,"1":0,"4":"模板龙","7":100001,"9":0,"10":[{"0":11,"1":0,"4":"超级","5":20,"6":10,"7":1,"10":2,"14":1,"16":0,"24":0,"26":0,"12":[]}]}],"11":[],"12":[]}`),
		TeamBattleReplays: []gamestate.TeamBattleReplay{{BossID: 11, EnemyPartyID: 101, EnemyType: 1, CostInitial: 3, HoldMax: 5, Battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 101, EnemyType: 1}, {EnemyPartyID: 101, EnemyType: 1}}}},
		TeamBattleRewards: []gamestate.TeamBattleRewardProfile{{BossID: 11, ResultRewards: []gamestate.Reward{{Type: 4, Num: 50}}, FirstClearRewards: []gamestate.Reward{{Type: 10, Num: 1}}, EnemyDrops: []gamestate.TeamBattleEnemyDrop{{Reward: gamestate.Reward{Type: 8, RewardTypeID: 1000, Num: 1, CardSkillLevels: []int16{}}}}}},
	}
	return base, filepath.Join(root, "battle.json")
}

func TestCustomBossSkipsOccupiedGroupIdentity(t *testing.T) {
	base, path := customBossFixture(t)
	base.TeamBattleSolo = json.RawMessage(strings.ReplaceAll(string(base.TeamBattleSolo), "700000010", "790000001"))
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	router := chi.NewRouter()
	router.Post("/copies", a.createCustomBoss)
	var result struct {
		Boss customBoss `json:"boss"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 200), &result); err != nil {
		t.Fatal(err)
	}
	if result.Boss.BossID != customBossFirstID+1 || result.Boss.GroupID != 790000002 {
		t.Fatal("copy reused an existing group identity")
	}
}

func TestCustomBossCompleteRuntimeTemplates(t *testing.T) {
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
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	router := chi.NewRouter()
	router.Get("/copies", a.customBossCatalog)
	router.Post("/copies", a.createCustomBoss)
	var catalog struct {
		Sources []DropBoss `json:"sources"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/copies", nil, 200), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Sources) == 0 {
		t.Fatal("full catalog has no templates")
	}
	for _, source := range catalog.Sources {
		b := customBoss{BossID: customBossFirstID, GroupID: customBossFirstID + customBossGroupOffset, Name: "测试", Difficulty: source.Difficulty, BPUse: ops.content.ruleBases[source.BossID].BPUse, BPUseHalf: ops.content.ruleBases[source.BossID].BPUseHalf, Targets: source.Targets}
		if err = validateCustomBoss(b, source.Targets); err != nil {
			t.Fatalf("template %d (%s): %v", source.BossID, source.Name, err)
		}
	}
	var result struct {
		Boss customBoss `json:"boss"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": catalog.Sources[0].BossID}, 200), &result); err != nil {
		t.Fatal(err)
	}
	t.Logf("validated %d complete runtime templates; copied Boss %d", len(catalog.Sources), result.Boss.SourceBossID)
}

func TestCustomBossCopyEditRestartAndDropIsolation(t *testing.T) {
	base, path := customBossFixture(t)
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops, groups: []AdminBattleGroup{{GroupID: 700000010, BossIDs: []int{11}}}, knownGroups: map[int]struct{}{700000010: {}}}
	router := chi.NewRouter()
	router.Get("/copies", a.customBossCatalog)
	router.Post("/copies", a.createCustomBoss)
	router.Put("/copies", a.saveCustomBoss)
	router.Get("/groups", a.bossGroups)
	router.Get("/drops", a.dropEditor)
	router.Put("/drops", a.saveDropEditor)
	result := testfixture.CallContentAdmin(t, router, "POST", "/copies", map[string]any{"expected_revision": 0, "source_boss_id": 11}, 200)
	var created struct {
		Boss customBoss `json:"boss"`
	}
	if err = json.Unmarshal(result, &created); err != nil {
		t.Fatal(err)
	}
	b := created.Boss
	if b.Enabled || b.BossID != customBossFirstID || len(b.Targets) != 4 || b.Targets[0].Stats.HP != 200 {
		t.Fatalf("bad independent copy: %+v", b)
	}
	if !ops.ContentConfiguration().State.DisabledTeamBattleBossIDs[b.BossID] {
		t.Fatal("new copy was published immediately")
	}
	if !strings.Contains(string(testfixture.CallContentAdmin(t, router, "GET", "/groups", nil, 200)), strconv.Itoa(b.GroupID)) {
		t.Fatal("copy missing from publication catalog")
	}
	var drop struct {
		Config BossDrops `json:"config"`
	}
	_ = json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/drops?boss_id="+strconv.Itoa(b.BossID), nil, 200), &drop)
	if len(drop.Config.Drops) != 1 || drop.Config.Drops[0].Reward.RewardTypeID != 1000 {
		t.Fatal("copy editor lost inherited drops")
	}
	b.Targets[0].Stats.HP = 12345
	b.Targets[0].Stats.Attack = 345
	b.Targets[0].Stats.Attribute = "ICE"
	b.Targets[2].Stats.HP = 67890
	b.Name = "自定义龙"
	b.Enabled = true
	input := map[string]any{"expected_revision": 1, "boss_id": b.BossID, "name": b.Name, "difficulty": b.Difficulty, "bp_use": b.BPUse, "bp_use_half": b.BPUseHalf, "enabled": true, "continue": b.Continue, "targets": b.Targets}
	testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 200)
	testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 409)
	input["expected_revision"] = 2
	bad := cloneBossTargets(b.Targets)
	bad[0].Stats.EnemyID = 99
	input["targets"] = bad
	testfixture.CallContentAdmin(t, router, "PUT", "/copies", input, 400)
	if ops.content.customBossRevision != 2 || ops.content.bosses[11].Targets[0].Stats.HP != 200 {
		t.Fatal("invalid save changed copy or template")
	}
	config := ops.ContentConfiguration()
	if config.State.DisabledTeamBattleBossIDs[b.BossID] {
		t.Fatal("enabled copy remained disabled")
	}
	copyReplay := config.State.TeamBattleReplays[1]
	if copyReplay.EnemyPartyID != 101 || len(copyReplay.EnemyOverrides) != 4 || copyReplay.EnemyOverrides[2].Stats.HP != 67890 {
		t.Fatal("copy lost actions or independent wave stats")
	}
	account := game.ApplyContentState(gamestate.State{TeamBattleSolo: base.TeamBattleSolo}, config)
	if !strings.Contains(string(account.TeamBattleSolo), "自定义龙") {
		t.Fatal("account catalog did not receive copy")
	}
	var catalog map[string][]json.RawMessage
	_ = json.Unmarshal(config.State.TeamBattleSolo, &catalog)
	var group struct {
		Bosses []struct {
			ID   int `json:"0"`
			Rule int `json:"24"`
		} `json:"10"`
	}
	_ = json.Unmarshal(catalog["9"][0], &group)
	if group.Bosses[0].Rule != 3 {
		t.Fatal("copy exposed native offline solo with template stats")
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 0, "configs": []BossDrops{{BossID: b.BossID, Drops: []gamestate.TeamBattleEnemyDrop{}, FameRewards: []gamestate.Reward{}}}}, 200)
	profiles := ops.ContentConfiguration().State.TeamBattleRewards
	if len(profiles[0].EnemyDrops) != 1 || len(profiles[1].EnemyDrops) != 0 {
		t.Fatal("copy drop changes affected original or did not apply")
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	if restarted.content.customBossRevision != 2 || restarted.content.customBosses[0].Targets[0].Stats.HP != 12345 || len(restarted.content.configuration.State.TeamBattleRewards[1].EnemyDrops) != 0 {
		t.Fatal("copy or drop configuration lost on restart")
	}
}
