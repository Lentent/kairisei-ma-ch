package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Uses an isolated resource snapshot; never reads or writes a player's database.
func TestGoldenDragonRestoredTemplates(t *testing.T) {
	root := os.Getenv("CN602_GOLDEN_DRAGON_SET")
	if root == "" {
		t.Skip("set CN602_GOLDEN_DRAGON_SET to the restored resource set")
	}
	path := filepath.Join(root, "_local/control/server/cn602-battle-runtime-master.json")
	master, err := masterdata.LoadBattleRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{TeamBattleSolo: json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)}
	if err := masterdata.ApplyBattleRuntimeMaster(&base, master); err != nil {
		t.Fatal(err)
	}
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	router := chi.NewRouter()
	router.Get("/copies", a.customBossCatalog)
	router.Post("/copies", a.createCustomBoss)
	var catalog struct {
		Sources []DropBoss `json:"sources"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/copies", nil, 200), &catalog); err != nil {
		t.Fatal(err)
	}
	want := map[int]int{30110202: 30110201, 30110203: 30110202, 30110205: 30110203, 30110206: 30110204}
	seen := map[int]bool{}
	for _, source := range catalog.Sources {
		party, ok := want[source.BossID]
		if !ok {
			continue
		}
		seen[source.BossID] = true
		if source.Name != "金闪闪龙" || source.Category != "boss" {
			t.Fatalf("unexpected restored template: %+v", source)
		}
		var result struct {
			Boss customBoss `json:"boss"`
		}
		body := map[string]any{"expected_revision": ops.content.customBossRevision, "source_boss_id": source.BossID}
		if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/copies", body, 200), &result); err != nil {
			t.Fatal(err)
		}
		b := result.Boss
		if b.Replay.EnemyPartyID != party || b.Enabled || len(b.Replay.Battles) != 1 || b.Replay.Battles[0].EnemyPartyID != party {
			t.Fatalf("copy lost party mapping or was enabled: %+v", b)
		}
		for _, target := range b.Targets {
			if target.Actions != nil {
				t.Fatal("copy replaced original AI")
			}
		}
		if source.BossID == 30110206 {
			if len(b.Targets) != 3 || b.Targets[0].Stats.HP != 2000000 || b.Targets[1].Stats.HP != 600000 || b.Targets[2].Stats.HP != 400000 || b.Targets[0].Stats.Magic != 12000 || b.Targets[1].Stats.Attack != 12000 {
				t.Fatalf("super-bow stats differ from original CN rows: %+v", b.Targets)
			}
			skills := ops.content.customBossSkillEntries(source)
			seal := false
			for _, skill := range skills {
				seal = seal || skill.SkillID == 31102421
			}
			if !seal {
				t.Fatal("restored template has no native cost-3 seal skill")
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("restored templates = %v, want all four difficulties", seen)
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, path); err != nil {
		t.Fatal(err)
	}
	if len(restarted.content.customBosses) != 4 {
		t.Fatal("restored template copies were lost on restart")
	}
	t.Log("all four golden-dragon templates listed, copied with original AI, and restored after restart")
}
