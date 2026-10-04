package accountstore

import (
	"encoding/json"
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestGachaBoxSnapshotProgressSurvivesCatalogRebuild(t *testing.T) {
	boxes := map[int]gamestate.GachaBoxProgress{70000001: {Round: 1000, Remaining: []gamestate.GachaBoxReward{{Stock: 17, Reward: gamestate.Reward{Type: 4, Num: 123, CardSkillLevels: []int16{}}}}}}
	state := gamestate.State{GachaBoxes: boxes}
	state.User.UserID = PrimaryUserID
	snapshot, err := accountSnapshotFromState(state)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeAccountSnapshot(content)
	if err != nil {
		t.Fatal(err)
	}
	// The publication can be absent from the rebuilt catalog until configuration applies.
	catalog := gamestate.State{}
	if err := restored.Progress.apply(&catalog); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(boxes, catalog.GachaBoxes) {
		t.Fatal("rebuilding catalog lost round or remaining reward inventory")
	}
	copy := catalog.GachaBoxes[70000001]
	copy.Remaining[0].Stock = 1
	if snapshot.Progress.GachaBoxes[70000001].Remaining[0].Stock != 17 {
		t.Fatal("snapshot progress aliases account inventory")
	}
	// The previous snapshot format without box progress remains readable.
	snapshot.Progress.GachaBoxes = nil
	content, _ = json.Marshal(snapshot)
	if _, err = DecodeAccountSnapshot(content); err != nil {
		t.Fatal("old snapshot is incompatible", err)
	}
	invalid := gamestate.CloneGachaBoxes(boxes)
	p := invalid[70000001]
	p.Round = 0
	invalid[70000001] = p
	if _, err := collectCatalogProgress(gamestate.State{GachaBoxes: invalid}); err == nil {
		t.Fatal("invalid box persisted")
	}
}

func TestGachaBoxPurgeRetainsOtherBoxInventory(t *testing.T) {
	state := gamestate.State{GachaBoxes: map[int]gamestate.GachaBoxProgress{70000001: {Round: 2, Remaining: []gamestate.GachaBoxReward{{Stock: 12, Reward: gamestate.Reward{Type: 4, Num: 10, CardSkillLevels: []int16{}}}}}, 70000002: {Round: 11, Remaining: []gamestate.GachaBoxReward{{Stock: 9, Reward: gamestate.Reward{Type: 4, Num: 20, CardSkillLevels: []int16{}}}}}}}
	state.User.UserID = PrimaryUserID
	snapshot, err := accountSnapshotFromState(state)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	raw, counts, err := pruneOperationalMetadata(raw, OperationalReferences{}, map[int]bool{70000001: true})
	if err != nil || counts["gacha_boxes"] != 1 {
		t.Fatal("box purge failed", err, counts)
	}
	decoded, err := DecodeAccountSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Progress.GachaBoxes) != 1 || decoded.Progress.GachaBoxes[70000002].Remaining[0].Stock != 9 {
		t.Fatal("purge changed another pool's inventory")
	}
}
