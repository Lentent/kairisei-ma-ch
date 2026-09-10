package multiplayer

// 49e3e: any nonzero selector may match any of the eight profile tags.
// A card contributes once, even if several selectors/tags match.
func combatCardHasProfileTag(card CombatCardDefinition, wanted ...int) bool {
	for _, tag := range card.ProfileTags {
		if tag == 0 {
			continue
		}
		for _, selector := range wanted {
			if selector != 0 && selector == tag {
				return true
			}
		}
	}
	return false
}
