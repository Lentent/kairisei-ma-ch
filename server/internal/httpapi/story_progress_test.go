package httpapi

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestCollectedCardUnlocksCharacterStoryWithoutReload(t *testing.T) {
	for _, listFirst := range []bool{false, true} {
		s := &store{cardCollectionIDs: map[int]struct{}{}, storySubCharacters: []release.StorySubCharacter{
			{Sections: []release.StorySubSection{{StorySubSectionID: 101, Stories: []release.StorySub{
				{StorySubID: 1, StateFlag: 4 | 8, UnlockText: "获得卡牌后解锁"},
				{StorySubID: 2, StateFlag: 4},
			}}}},
		}}
		if s.beginSubStory(1) {
			t.Fatal("uncollected character was unlocked")
		}
		// Card acquisition records this durable discovery even if the player
		// sells or fuses the card before opening the story screen.
		s.cardCollectionIDs[101] = struct{}{}
		if listFirst {
			story := s.storySubState()[0].Sections[0].Stories[0]
			if story.StateFlag != 1|8 || story.UnlockText != "" {
				t.Fatalf("newly collected character remains locked in list: %+v", story)
			}
		}
		if !s.beginSubStory(1) || s.beginSubStory(2) {
			t.Fatal("discovery did not unlock exactly the first unfinished story")
		}
		content, err := json.Marshal(s.storySubState())
		if err != nil {
			t.Fatal(err)
		}
		reloaded := &store{}
		if err := json.Unmarshal(content, &reloaded.storySubCharacters); err != nil || !reloaded.beginSubStory(1) {
			t.Fatalf("saved story unlock was lost after card disposal: %v", err)
		}
	}
}

func TestStoryReplayPreservesLaterClearAndFirstClearReward(t *testing.T) {
	for _, kind := range []string{"main", "cn main", "character", "event"} {
		t.Run(kind, func(t *testing.T) {
			reward := release.Reward{Type: 4, Num: 5}
			s := &store{storyRewardPolicy: release.StoryRewardPolicy{
				MainFirstClear: reward, SubFirstClear: reward, EventFirstClear: reward,
			}}
			var begin func(int) bool
			var end func(bool) (presentReceiveResult, error)
			var secondFlag func() int
			switch kind {
			case "main", "cn main":
				parts := []release.StoryMainPart{{Sections: []release.StoryMainSection{
					{Stories: []release.StoryMain{{StoryMainID: 1, StateFlag: 1}}},
					{Stories: []release.StoryMain{{StoryMainID: 2, StateFlag: 4 | 8}}},
				}}}
				if kind == "cn main" {
					s.cnStoryMainParts = parts
				} else {
					s.storyMainParts = parts
				}
				begin, end = s.beginMainStory, s.endMainStory
				secondFlag = func() int { return parts[0].Sections[1].Stories[0].StateFlag }
			case "character":
				s.storySubCharacters = []release.StorySubCharacter{{Sections: []release.StorySubSection{{Stories: []release.StorySub{
					{StorySubID: 1, StateFlag: 1}, {StorySubID: 2, StateFlag: 4 | 8},
				}}}}}
				begin, end = s.beginSubStory, s.endSubStory
				secondFlag = func() int { return s.storySubCharacters[0].Sections[0].Stories[1].StateFlag }
			case "event":
				s.storyEvents = []release.StoryEvent{{Stories: []release.StorySubEvent{
					{Sub: release.StorySub{StorySubID: 1, StateFlag: 1}},
					{Sub: release.StorySub{StorySubID: 2, StateFlag: 4 | 8}},
				}}}
				begin, end = s.beginSubStory, s.endSubStory
				secondFlag = func() int { return s.storyEvents[0].Stories[1].Sub.StateFlag }
			}
			for _, id := range []int{1, 2, 1, 2} {
				if !begin(id) {
					t.Fatalf("story %d is unexpectedly locked", id)
				}
				if _, err := end(true); err != nil {
					t.Fatal(err)
				}
			}
			if s.gold != 10 || secondFlag() != 2|8 {
				t.Fatalf("replay changed clear progress/repeated reward: gold=%d second flag=%d", s.gold, secondFlag())
			}
		})
	}
}
