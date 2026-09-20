package masterdata

import (
	"encoding/json"
	"testing"
)

func TestCollectBattlePersistedStatesMigratesExactDuplicateFamily(t *testing.T) {
	persistedGroups := []json.RawMessage{
		json.RawMessage(`{"0":700300101,"10":[{"0":30010102,"10":1},{"0":30010103,"10":2}]}`),
		json.RawMessage(`{"0":700300102,"10":[{"0":30010202,"10":2},{"0":30010203,"10":1}]}`),
	}
	canonicalByDuplicate := map[int]int{
		30010202: 30010102,
		30010203: 30010103,
	}

	states, err := collectBattlePersistedStates(
		persistedGroups, canonicalByDuplicate,
	)
	if err != nil {
		t.Fatalf("collect persisted TeamBattle states: %v", err)
	}
	if got := states[30010102]; got != 2 {
		t.Fatalf("canonical middle difficulty state = %d, want 2", got)
	}
	if got := states[30010103]; got != 2 {
		t.Fatalf("canonical upper difficulty state = %d, want 2", got)
	}
	if _, exists := states[30010202]; exists {
		t.Fatal("duplicate family Boss state was not migrated to its canonical Boss")
	}
}

func TestRetiredSeedBattleGroupsAreExplicit(t *testing.T) {
	for _, groupID := range []int{200000, 200001} {
		if !isBattleRetiredSeedGroupID(groupID) {
			t.Fatalf("legacy battle group %d is not retired", groupID)
		}
	}
	for _, groupID := range []int{100001, 700300201} {
		if isBattleRetiredSeedGroupID(groupID) {
			t.Fatalf("active battle group %d was retired", groupID)
		}
	}
}
