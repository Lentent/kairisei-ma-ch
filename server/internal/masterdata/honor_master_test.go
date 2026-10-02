package masterdata

import (
	"encoding/json"
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func starterHonorTestMaster() honorRuntimeMaster {
	master := honorRuntimeMaster{
		SchemaVersion: 1,
		ClientProfile: "cn602-bootstrap",
		Source:        json.RawMessage(`{"test":true}`),
	}
	for _, id := range []int{10000000, 10100001, 10100002, 10100003, 10100004, 10100005, 10100006, 10100007, 10100008, 10500001, 26093002} {
		master.Honors = append(master.Honors, honorDefinition{HonorID: id, Name: "Title", SlotMask: 15, DefaultOwned: true})
	}
	return master
}

func TestHonorMasterLimitsNewAccountDefaults(t *testing.T) {
	master := starterHonorTestMaster()
	if err := validateHonorRuntimeMaster(master); err != nil {
		t.Fatal(err)
	}
	state := gamestate.State{Honors: gamestate.HonorCollectionState{DeckHonorIDs: []int{0, 0, 0, 0}}}
	changed, err := ApplyHonorRuntimeMaster(&state, master)
	if err != nil || !changed {
		t.Fatalf("initialize honors: changed=%v err=%v", changed, err)
	}
	want := []int{10000000, 10100001, 10100002, 10100003, 10100004, 10100005, 10100006, 10100007, 10100008}
	if !reflect.DeepEqual(state.Honors.HonorIDs, want) {
		t.Fatalf("starter honors = %v, want %v", state.Honors.HonorIDs, want)
	}
	if len(state.CollectionRewards) != len(master.Honors) {
		t.Fatal("non-default titles disappeared from the reward catalog")
	}
	changed, err = ApplyHonorRuntimeMaster(&state, master)
	if err != nil || changed {
		t.Fatalf("reapply honors: changed=%v err=%v", changed, err)
	}
}

func TestHonorMasterPreservesEarnedTitles(t *testing.T) {
	state := gamestate.State{Honors: gamestate.HonorCollectionState{
		HonorIDs:     []int{10100001, 26093002},
		DeckHonorIDs: []int{0, 0, 0, 26093002},
	}}
	want := state.Honors
	changed, err := ApplyHonorRuntimeMaster(&state, starterHonorTestMaster())
	if err != nil || changed || !reflect.DeepEqual(state.Honors, want) {
		t.Fatalf("earned titles changed: %v changed=%v err=%v", state.Honors, changed, err)
	}
}

func TestHonorMasterRequiresAStarterDefault(t *testing.T) {
	master := starterHonorTestMaster()
	for i := range master.Honors {
		master.Honors[i].DefaultOwned = master.Honors[i].HonorID == 26093002
	}
	if err := validateHonorRuntimeMaster(master); err == nil {
		t.Fatal("master without a starter default was accepted")
	}
}
