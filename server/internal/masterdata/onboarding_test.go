package masterdata

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestOnboardingDoesNotReceiveLocalQAInitialItems(t *testing.T) {
	state := gamestate.State{
		Onboarding: gamestate.OnboardingState{ConfigVersion: OnboardingConfigVersion},
		Items:      []gamestate.Item{{ItemID: 2001}},
	}
	master := ItemRuntimeMaster{
		LocalAccountConfigVersion: 3,
		LocalAccountInitialItems:  []gamestate.Item{{ItemID: 4000, Num: 10000}},
		Items: []gamestate.ItemDefinition{
			{ItemID: 2001}, {ItemID: 4000},
		},
	}
	changed, err := ApplyItemRuntimeMaster(&state, master)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || state.LocalAccountConfigVersion != 3 {
		t.Fatalf("local item config migration = (%t, %d)", changed, state.LocalAccountConfigVersion)
	}
	if len(state.Items) != 1 || state.Items[0].ItemID != 2001 {
		t.Fatalf("onboarding items = %+v", state.Items)
	}
}
