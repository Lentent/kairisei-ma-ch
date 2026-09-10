package cnbootstrap

import (
	"encoding/json"
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
	found := 0
	for _, replay := range view.Replays {
		id := replay.BossID
		if id >= cnOwnDeckBossIDOffset {
			id -= cnOwnDeckBossIDOffset
		}
		if id < 30920102 || id > 30920108 {
			continue
		}
		want := 1
		if id >= 30920106 {
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
	if found != 9 {
		t.Fatalf("expected six normal difficulties and three own-deck variants, got %d", found)
	}
	for _, replay := range master.Replays {
		if replay.BossID >= 30920102 && replay.BossID <= 30920108 && len(replay.Battles) != 1 {
			t.Fatal("projection mutated source master")
		}
	}
}
