package httpapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestPastBossDisplayMatchesClientSelectionOrder(t *testing.T) {
	type entry struct {
		ID         int    `json:"0"`
		OnlyMyDeck int    `json:"1"`
		Difficulty string `json:"4"`
		BP         int    `json:"5"`
		Picture    int    `json:"9"`
		State      int    `json:"10"`
	}
	low := entry{ID: 40002002, Difficulty: "中级", BP: 8, Picture: 10001045}
	high := entry{ID: 40002003, Difficulty: "上级", BP: 9, Picture: 10001046}
	variant := entry{ID: 140002003, OnlyMyDeck: 1, Difficulty: "上级", BP: 9, Picture: 10001046}
	progress := json.RawMessage(`{"10":[{"10":[{"0":40002002,"10":2},{"0":40002003,"10":1}]}],"11":[{"10":[{"0":140002003,"10":2}]}],"12":[]}`)
	for _, test := range []struct {
		name    string
		entries []entry
	}{
		{"ascending", []entry{low, high}},
		{"descending", []entry{high, low}},
		{"own deck", []entry{low, high, variant}},
		{"single", []entry{high}},
		{"empty", []entry{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"0": 700400020, "4": "犬魔", "13": test.entries})
			if err != nil {
				t.Fatal(err)
			}
			before := string(raw)
			got, err := pastBossProgress([]json.RawMessage{raw}, progress)
			if err != nil {
				t.Fatal(err)
			}
			var group struct {
				ID      int     `json:"0"`
				Name    string  `json:"4"`
				Entries []entry `json:"13"`
			}
			if err := json.Unmarshal(got[0], &group); err != nil {
				t.Fatal(err)
			}
			// Model TeamSlStMItemList.Open's Sort + Reverse and GetID(index).
			clickIDs := make([]int, len(test.entries))
			byID := make(map[int]entry)
			for index, boss := range test.entries {
				clickIDs[index] = boss.ID
				boss.State = map[int]int{low.ID: 2, high.ID: 1, variant.ID: 2}[boss.ID]
				byID[boss.ID] = boss
			}
			sort.Sort(sort.Reverse(sort.IntSlice(clickIDs)))
			if len(group.Entries) != len(clickIDs) {
				t.Fatalf("entry count = %d, want %d", len(group.Entries), len(clickIDs))
			}
			for index, boss := range group.Entries {
				if !reflect.DeepEqual(boss, byID[clickIDs[index]]) {
					t.Fatalf("row %d = %+v, selected encounter = %+v", index, boss, byID[clickIDs[index]])
				}
			}
			if group.ID != 700400020 || group.Name != "犬魔" || string(raw) != before {
				t.Fatal("archive identity or shared source changed")
			}
		})
	}
}

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
