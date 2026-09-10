package httpapi

import (
	"encoding/json"
	"testing"
)

func TestTeamBattlePastBossGroupForBossRestoresRoomShape(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"0": 700400010, "1": 0, "2": 0, "3": 1, "4": "测试妖精",
		"13": []any{
			map[string]any{"0": 40001004, "9": 10002014},
			map[string]any{"0": 40001005, "9": 10002015},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	groupID, value, found := teamBattlePastBossGroupForBoss(
		[]json.RawMessage{raw}, 40001005,
	)
	if !found || groupID != 700400010 {
		t.Fatalf("past boss lookup = (%d, %t), want (700400010, true)", groupID, found)
	}
	group, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("past boss room group type = %T", value)
	}
	if got := group["7"]; got != 10002015 {
		t.Fatalf("past boss room pictid = %v, want 10002015", got)
	}
	if bosses, ok := group["10"].([]any); !ok || len(bosses) != 2 {
		t.Fatalf("past boss room bosses = %T %+v", group["10"], group["10"])
	}
	if !teamBattlePastBossHasBoss([]json.RawMessage{raw}, 40001004) ||
		teamBattlePastBossHasBoss([]json.RawMessage{raw}, 40001006) {
		t.Fatal("past boss membership differs from archive DTO")
	}
}
