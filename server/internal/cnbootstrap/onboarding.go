package cnbootstrap

import (
	"errors"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

var cnLegacyLocalDeckNameByArthur = map[int8]string{
	1: "佣兵本地卡组",
	2: "富豪本地卡组",
	3: "盗贼本地卡组",
	4: "歌姬本地卡组",
}

// migrateCNDefaultDeckNames removes an early local-server marker only from
// untouched generated deck names. Player-renamed decks are never rewritten.
func migrateCNDefaultDeckNames(state *gamestate.State) bool {
	if state == nil {
		return false
	}
	changed := false
	for index := range state.Decks {
		deck := &state.Decks[index]
		if deck.Name != cnLegacyLocalDeckNameByArthur[deck.ArthurType] {
			continue
		}
		deck.Name = accountstore.DefaultDeckNameByArthur[deck.ArthurType]
		changed = true
	}
	return changed
}

// migrateCNOnboardingState restores the two client-native steps that the
// first local profile skipped after deck editing.  Completed legacy accounts
// stay complete; an account inside the old Explore/Story tail returns to the
// first missing original objective so it cannot silently skip the sequence.
func migrateCNOnboardingState(state *gamestate.State) (bool, error) {
	if state == nil || state.Onboarding.ConfigVersion == 0 ||
		state.Onboarding.ConfigVersion == masterdata.OnboardingConfigVersion {
		return false, nil
	}
	if state.Onboarding.ConfigVersion != masterdata.LegacyOnboardingConfigVersion ||
		state.Onboarding.Step < 0 || state.Onboarding.Step > 7 {
		return false, errors.New("CN legacy onboarding state is invalid")
	}
	switch {
	case state.Onboarding.Step == 7:
		state.Onboarding.Step = masterdata.OnboardingStepCount
		state.Onboarding.CurrentAnnounced = false
		state.Onboarding.PendingClearQuestID = 0
	case state.Onboarding.Step >= 4:
		state.Onboarding.Step = 4
		state.Onboarding.CurrentAnnounced = false
		if state.Onboarding.PendingClearQuestID < 1041 ||
			state.Onboarding.PendingClearQuestID > 1044 {
			state.Onboarding.PendingClearQuestID = 0
		}
	}
	state.Onboarding.ConfigVersion = masterdata.OnboardingConfigVersion
	if err := accountstore.InstallOnboardingGacha(state); err != nil {
		return false, err
	}
	return true, nil
}

func migrateCNOnboardingGachaID(state *gamestate.State) (bool, error) {
	if state == nil {
		return false, nil
	}
	foundSingle := false
	foundMulti := false
	for _, gacha := range state.Gachas {
		switch gacha.GachaID {
		case masterdata.LegacyOnboardingGachaID:
			if err := accountstore.InstallOnboardingGacha(state); err != nil {
				return false, err
			}
			return true, nil
		case masterdata.OnboardingGachaID:
			foundSingle = true
		case masterdata.OnboardingMultiGachaID:
			foundMulti = true
		}
	}
	if state.Onboarding.ConfigVersion != masterdata.OnboardingConfigVersion {
		return false, nil
	}
	if !foundSingle || !foundMulti {
		if err := accountstore.InstallOnboardingGacha(state); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
