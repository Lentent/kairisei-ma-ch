package cnbootstrap

import (
	"encoding/json"
)

// The original ProtoGen reward receivers use named fields, including nested
// CardInfo and CardStackInfo. CardShow2 and GachaPlay2 use numeric keys instead;
// keep this conversion at the named endpoint boundaries.
func remapCNNamedRewardArrays(method map[string]json.RawMessage) error {
	for field, fields := range map[string]map[string]string{
		"deck_cards":      cnCardInfoNamedFieldMap,
		"new_cards":       cnCardInfoNamedFieldMap,
		"new_stack_cards": cnCardStackInfoNamedFieldMap,
		"new_sphrs":       cnSphereInfoNamedFieldMap,
		"new_buddys":      cnBuddyInfoNamedFieldMap,
	} {
		if _, exists := method[field]; !exists {
			continue
		}
		entries, err := remapJSONArray(method[field], fields)
		if err != nil {
			return err
		}
		method[field], err = json.Marshal(entries)
		if err != nil {
			return err
		}
	}
	return nil
}

func adaptCNNamedRewardMethod(method map[string]json.RawMessage) (any, error) {
	if err := remapCNNamedRewardArrays(method); err != nil {
		return nil, err
	}
	return method, nil
}
