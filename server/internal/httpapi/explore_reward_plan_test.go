package httpapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestExploreDropPlanMatchesNativeDisplayAndSettlement(t *testing.T) {
	raw := json.RawMessage(`{"dialogue":"keep","symbols":[{"pos":17,"reward":[{"type":4,"num":100,"card_skill_lv":[]},{"type":10,"num":2,"card_skill_lv":[]},{"type":12,"num":3,"card_skill_lv":[]}],"local_reward_chances":[1000000,0,500000]}],"treasureboxes":[]}`)
	source := []json.RawMessage{raw}
	hits, misses := 0, 0
	for i := range 32 {
		seed := []byte{byte(i)}
		events, rewards, err := planExploreRewards(source, seed)
		if err != nil || bytes.Contains(events[0], []byte("local_reward_chances")) {
			t.Fatal("local configuration leaked into native events", err)
		}
		if len(rewards) < 1 || rewards[0].Type != 4 || rewards[0].Num != 100 {
			t.Fatal("guaranteed reward missing")
		}
		for _, reward := range rewards {
			if reward.Type == 10 {
				t.Fatal("zero chance reward selected")
			}
		}
		if len(rewards) == 2 {
			hits++
		} else {
			misses++
		}
		displayed, err := decodeExploreRewards(events)
		if err != nil || !reflect.DeepEqual(displayed, rewards) {
			t.Fatal("display and accepted settlement disagree")
		}
		again, same, err := planExploreRewards(source, seed)
		if err != nil || !reflect.DeepEqual(again, events) || !reflect.DeepEqual(same, rewards) {
			t.Fatal("same plan seed rerolled")
		}
		var event map[string]json.RawMessage
		_ = json.Unmarshal(events[0], &event)
		if string(event["dialogue"]) != `"keep"` || !bytes.Contains(event["symbols"], []byte(`"pos":17`)) {
			t.Fatal("native event changed")
		}
	}
	if hits == 0 || misses == 0 || !bytes.Equal(source[0], raw) {
		t.Fatal("chance ignored or source configuration mutated")
	}
}
