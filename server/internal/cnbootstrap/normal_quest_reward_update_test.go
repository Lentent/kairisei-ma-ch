package cnbootstrap

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFirstClearRewardUpdatePreservesAlreadyClaimedProgress(t *testing.T) {
	oldArea := json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":1,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":2}]},"clear_reward":[{"is_already":1,"rewards":[{"type":10,"num":1}]}]}]}]},"stage_clear":[10000101],"new_clear_stage":[]}`)
	newArea := json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":0,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":0}]},"clear_reward":[{"is_already":0,"rewards":[{"type":10,"num":50}]}]}]}]},"stage_clear":[],"new_clear_stage":[]}`)
	merged, err := mergeCNNormalQuestAreaProgress(newArea, oldArea)
	if err != nil {
		t.Fatal(err)
	}
	var progress cnNormalQuestProgress
	if err := json.Unmarshal(merged, &progress); err != nil {
		t.Fatal(err)
	}
	stage := progress.StageQuest.Stages[0]
	if stage.IsClearDone != 1 || stage.Raids[0].BossGroup.Bosses[0].State != 2 || stage.Raids[0].ClearRewards[0].IsAlready != 1 {
		t.Fatalf("reward update reset cleared/claimed flags: %s", merged)
	}
	if !strings.Contains(string(merged), `"num":50`) || string(progress.StageClear) != "[10000101]" {
		t.Fatalf("reward update did not replace static quantity while preserving progress: %s", merged)
	}
}
