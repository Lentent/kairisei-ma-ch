package gamestate

// UnlockCollectedCharacterStories uses the section's card identity and durable
// discovery history. Clearing earlier episodes still controls later episodes.
func UnlockCollectedCharacterStories(characters []StorySubCharacter, collected map[int]struct{}) {
	for _, character := range characters {
		for _, section := range character.Sections {
			if _, found := collected[section.StorySubSectionID]; !found {
				continue
			}
			for index := range section.Stories {
				story := &section.Stories[index]
				if story.StateFlag&2 != 0 {
					continue
				}
				story.StateFlag = story.StateFlag&8 | 1
				story.UnlockText = ""
				break
			}
		}
	}
}
