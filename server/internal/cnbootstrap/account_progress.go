package cnbootstrap

import (
	"encoding/json"
	"fmt"

	"kairisei.local/server/internal/release"
)

// Only identities and player progress cross the persistence boundary. Names,
// artwork, costs, enemy definitions and reward lineups come from the catalog.
type cnCatalogProgress struct {
	GachaPlays    map[int]int             `json:"gacha_plays,omitempty"`
	ShopPurchases map[int]int             `json:"shop_purchases,omitempty"`
	BossStates    map[int]int             `json:"boss_states,omitempty"`
	Areas         []cnNormalQuestProgress `json:"areas,omitempty"`
	MainStory     map[int]int             `json:"main_story,omitempty"`
	CNMainStory   map[int]int             `json:"cn_main_story,omitempty"`
	SubStory      map[int]int             `json:"sub_story,omitempty"`
	EventStory    map[int]int             `json:"event_story,omitempty"`
}

var cnBattleProgressCategories = [...]string{"9", "10", "11", "12"}

func collectCNCatalogProgress(state release.State) (cnCatalogProgress, error) {
	p := cnCatalogProgress{
		GachaPlays: map[int]int{}, ShopPurchases: map[int]int{}, BossStates: map[int]int{},
		MainStory: map[int]int{}, CNMainStory: map[int]int{}, SubStory: map[int]int{}, EventStory: map[int]int{},
	}
	for _, gacha := range state.Gachas {
		if gacha.PlayCount != 0 {
			p.GachaPlays[gacha.GachaID] = gacha.PlayCount
		}
	}
	for _, tab := range state.ItemShopTabs {
		for _, item := range tab.Lineup {
			if used := item.StockNum - item.StockRemain; used > 0 {
				p.ShopPurchases[item.LineupID] = used
			}
		}
	}
	var solo map[string]json.RawMessage
	if len(state.TeamBattleSolo) > 0 {
		if err := json.Unmarshal(state.TeamBattleSolo, &solo); err != nil {
			return p, err
		}
	}
	for _, category := range cnBattleProgressCategories {
		if len(solo[category]) == 0 {
			continue
		}
		var groups []cnBattleGroupIdentity
		if err := json.Unmarshal(solo[category], &groups); err != nil {
			return p, fmt.Errorf("decode battle progress category %s: %w", category, err)
		}
		for _, group := range groups {
			for _, boss := range group.Bosses {
				if boss.State > p.BossStates[boss.BossID] {
					p.BossStates[boss.BossID] = boss.State
				}
			}
		}
	}
	areas := state.StageQuestAreas
	if len(areas) == 0 && len(state.MainQuest) > 0 {
		areas = []json.RawMessage{state.MainQuest}
	}
	for _, raw := range areas {
		var progress cnNormalQuestProgress
		if err := json.Unmarshal(raw, &progress); err != nil {
			return p, err
		}
		p.Areas = append(p.Areas, progress)
	}
	collectMain := func(parts []release.StoryMainPart, flags map[int]int) {
		for _, part := range parts {
			for _, section := range part.Sections {
				for _, story := range section.Stories {
					if flag := story.StateFlag & 7; flag != 0 {
						flags[story.StoryMainID] = flag
					}
				}
			}
		}
	}
	collectMain(state.Story.MainParts, p.MainStory)
	collectMain(state.Story.CNMainParts, p.CNMainStory)
	for _, character := range state.Story.SubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				if flag := story.StateFlag & 7; flag != 0 {
					p.SubStory[story.StorySubID] = flag
				}
			}
		}
	}
	for _, event := range state.Story.Events {
		for _, story := range event.Stories {
			if flag := story.Sub.StateFlag & 7; flag != 0 {
				p.EventStory[story.Sub.StorySubID] = flag
			}
		}
	}
	return p, nil
}

func (p cnCatalogProgress) apply(state *release.State) error {
	state.Gachas = cloneGachas(state.Gachas)
	for i := range state.Gachas {
		state.Gachas[i].PlayCount = p.GachaPlays[state.Gachas[i].GachaID]
	}
	state.ItemShopTabs = cloneItemShopTabs(state.ItemShopTabs)
	for _, tab := range state.ItemShopTabs {
		for i := range tab.Lineup {
			item := &tab.Lineup[i]
			item.StockRemain = max(0, item.StockNum-p.ShopPurchases[item.LineupID])
		}
	}
	if len(state.TeamBattleSolo) > 0 {
		var solo map[string]json.RawMessage
		if err := json.Unmarshal(state.TeamBattleSolo, &solo); err != nil {
			return err
		}
		for _, category := range cnBattleProgressCategories {
			if len(solo[category]) == 0 {
				continue
			}
			var groups []map[string]json.RawMessage
			if err := json.Unmarshal(solo[category], &groups); err != nil {
				return err
			}
			for _, group := range groups {
				var bosses []map[string]json.RawMessage
				if err := json.Unmarshal(group["10"], &bosses); err != nil {
					return err
				}
				for _, boss := range bosses {
					var id int
					if err := json.Unmarshal(boss["0"], &id); err != nil {
						return err
					}
					boss["10"] = json.RawMessage(fmt.Sprint(p.BossStates[id]))
				}
				encoded, err := json.Marshal(bosses)
				if err != nil {
					return err
				}
				group["10"] = encoded
			}
			encoded, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			solo[category] = encoded
		}
		encodedSolo, err := json.Marshal(solo)
		if err != nil {
			return err
		}
		state.TeamBattleSolo = encodedSolo
	}
	byArea := make(map[int]json.RawMessage, len(p.Areas))
	for _, area := range p.Areas {
		encoded, err := json.Marshal(area)
		if err != nil {
			return err
		}
		byArea[area.StageQuest.AreaID] = encoded
	}
	applyArea := func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return raw, nil
		}
		var identity cnNormalQuestProgress
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, err
		}
		if progress, exists := byArea[identity.StageQuest.AreaID]; exists {
			return mergeCNNormalQuestAreaProgress(raw, progress)
		}
		return raw, nil
	}
	var err error
	state.MainQuest, err = applyArea(state.MainQuest)
	if err != nil {
		return err
	}
	state.StageQuestAreas = cloneRawMessages(state.StageQuestAreas)
	for i, area := range state.StageQuestAreas {
		state.StageQuestAreas[i], err = applyArea(area)
		if err != nil {
			return err
		}
	}
	// Clone the mutable story projection; the catalog remains shared/read-only.
	encoded, err := json.Marshal(state.Story)
	if err != nil {
		return err
	}
	var story release.StoryCatalogState
	if err := json.Unmarshal(encoded, &story); err != nil {
		return err
	}
	story.StoryBattleIDs = state.Story.StoryBattleIDs
	story.StoryBattleReferences = state.Story.StoryBattleReferences
	applyMain := func(parts []release.StoryMainPart, flags map[int]int) {
		for _, part := range parts {
			for _, section := range part.Sections {
				for i := range section.Stories {
					item := &section.Stories[i]
					item.StateFlag = item.StateFlag&8 | flags[item.StoryMainID]
				}
			}
		}
		normalizeCNStoryProgress(parts)
	}
	applyMain(story.MainParts, p.MainStory)
	applyMain(story.CNMainParts, p.CNMainStory)
	for _, character := range story.SubCharacters {
		for _, section := range character.Sections {
			for i := range section.Stories {
				item := &section.Stories[i]
				flag := p.SubStory[item.StorySubID]
				item.StateFlag = item.StateFlag&8 | flag
				if flag&3 != 0 {
					item.UnlockText = ""
				}
			}
		}
	}
	for _, event := range story.Events {
		for i := range event.Stories {
			item := &event.Stories[i].Sub
			flag := p.EventStory[item.StorySubID]
			item.StateFlag = item.StateFlag&(8|16) | flag
			if flag&3 != 0 {
				item.UnlockText = ""
			}
		}
	}
	state.Story = story
	return nil
}
