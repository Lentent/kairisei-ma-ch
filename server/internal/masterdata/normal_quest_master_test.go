package masterdata

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestMergeNormalQuestAreasPromotesCanonicalFirstArea(t *testing.T) {
	legacy := json.RawMessage(`{"stage_quest":{"areaid":200001,"stage_object":[]},"stage_clear":[],"new_clear_stage":[]}`)
	canonical := json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":0,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":0}]},"clear_reward":[]}]}]},"stage_clear":[],"new_clear_stage":[]}`)
	persistedCanonical := json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":1,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":0}]},"clear_reward":[]}]}]},"stage_clear":[10000101],"new_clear_stage":[]}`)
	state := gamestate.State{
		MainQuest:       legacy,
		StageQuestAreas: []json.RawMessage{legacy, persistedCanonical},
	}
	if err := mergeNormalQuestAreas(&state, []json.RawMessage{canonical}); err != nil {
		t.Fatalf("merge normal quest areas: %v", err)
	}
	defaultAreaID, err := StageQuestAreaID(state.MainQuest)
	if err != nil || defaultAreaID != 100001 {
		t.Fatalf("default normal quest area = %d, err = %v", defaultAreaID, err)
	}
	if len(state.StageQuestAreas) != 1 {
		t.Fatalf("normal quest area count = %d, want 1", len(state.StageQuestAreas))
	}
	var progress NormalQuestProgress
	if err := json.Unmarshal(state.MainQuest, &progress); err != nil {
		t.Fatal(err)
	}
	if len(progress.StageQuest.Stages) != 1 || progress.StageQuest.Stages[0].IsClearDone != 1 {
		t.Fatalf("canonical first-area progress was not preserved: %+v", progress.StageQuest.Stages)
	}
	if progress.StageQuest.Stages[0].Raids[0].BossGroup.Bosses[0].State != 2 {
		t.Fatalf("cleared canonical StageQuest boss state was not migrated: %+v", progress.StageQuest.Stages[0].Raids)
	}
}

func TestCollectNormalQuestClearedBossStates(t *testing.T) {
	area := json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":1,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":0}]},"clear_reward":[]}]},{"stageid":10000102,"is_clear_done":0,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000102,"state":2}]},"clear_reward":[]}]}]},"stage_clear":[],"new_clear_stage":[]}`)
	states, err := collectNormalQuestClearedBossStates([]json.RawMessage{area})
	if err != nil {
		t.Fatal(err)
	}
	if states[[2]int{10000101, 100001}] != 2 {
		t.Fatal("cleared StageQuest boss did not migrate to CLEAR")
	}
	if _, exists := states[[2]int{10000102, 100001}]; exists {
		t.Fatal("uncleared StageQuest boss migrated to CLEAR")
	}
}
