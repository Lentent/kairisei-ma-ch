package cnbootstrap

import (
	"encoding/json"
	"kairisei.local/server/internal/multiplayer"
	"os"
	"path/filepath"
	"testing"
)

func TestNamelessPublishedSequences(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("set CN602_RUNTIME_SET to the complete resource directory")
	}
	content, err := os.ReadFile(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	master, err := loadCNBattleRuntimeMaster(filepath.Join(root, manifest.Entrypoints["cn-battle-master"]))
	if err != nil {
		t.Fatal(err)
	}
	view, err := projectCNBattleRuntime(master)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := multiplayer.LoadCombatCatalog(filepath.Join(root, "_local/control/server/cn602-card-master/card.csv"), filepath.Join(root, "_local/control/server/cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	for _, replay := range view.Replays {
		for index, wave := range replay.Battles {
			party, ok := catalog.EnemyParties[wave.EnemyPartyID]
			if !ok {
				t.Fatalf("missing party %d", wave.EnemyPartyID)
			}
			for _, slot := range party.Slots {
				if slot.EnemyID == 0 {
					continue
				}
				level := catalog.EnemyLevels[slot.EnemyID]
				skills := []int{level.PassiveSkillID}
				skills = append(skills, level.CallSkillIDs[:]...)
				for _, action := range level.Actions {
					skills = append(skills, action.SkillID)
				}
				for _, skillID := range skills {
					for _, skill := range catalog.EnemySkills[skillID] {
						for _, role := range catalog.EnemySkillRoles[skill.FunctionID] {
							if role.Function == "ENEMY_AWAKE_FLAG_SET" && (index+1 >= len(replay.Battles) || replay.Battles[index+1].EnemyType != 4) {
								t.Fatalf("boss %d party %d skill %d has unbound awakening", replay.BossID, wave.EnemyPartyID, skillID)
							}
						}
					}
				}
			}
		}
	}
	found := 0
	for _, replay := range view.Replays {
		id := replay.BossID
		if id >= cnOwnDeckBossIDOffset {
			id -= cnOwnDeckBossIDOffset
		}
		if (id < 30920102 || id > 30920108) && id != 30910106 && id != 30910107 {
			continue
		}
		want := 1
		if id >= 30920106 || id == 30910106 || id == 30910107 {
			want = 2
		}
		if len(replay.Battles) != want || replay.Battles[0].EnemyPartyID != id {
			t.Fatalf("bad published phases: %+v", replay)
		}
		if want == 2 && (replay.Battles[1].EnemyPartyID != id+10 || replay.Battles[1].EnemyType != 4) {
			t.Fatalf("missing awake phase: %+v", replay)
		}
		found++
		t.Logf("boss=%d phases=%+v", replay.BossID, replay.Battles)
	}
	if found != 13 {
		t.Fatalf("expected Nameless and Constantine normal/own-deck phases, got %d", found)
	}
	for _, replay := range master.Replays {
		if replay.BossID >= 30920102 && replay.BossID <= 30920108 && len(replay.Battles) != 1 {
			t.Fatal("projection mutated source master")
		}
	}
}
