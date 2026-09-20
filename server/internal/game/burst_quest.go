package game

import (
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

var ErrNoActiveBurstStory = errors.New("no active sword-release story")

// LOCAL_POLICY: finishing all nine training steps opens all four swords.
// Learning stories remain optional. The feature bits also serve as the
// one-time receipt for each initial Buddy, including after selling it.
func (s *Account) completeTrainingBurstLocked() error {
	if s.onboarding.ConfigVersion != cnOnboardingConfigVersion || s.onboarding.Step != cnOnboardingStepCount {
		return nil
	}
	pending := []gamestate.BurstQuest{}
	rewards := []gamestate.Reward{}
	for _, quest := range gamestate.BurstQuests() {
		if _, done := s.unlockedFeatureIDs[uint(32+quest.ArthurType)]; done {
			continue
		}
		pending = append(pending, quest)
		owned := false
		for _, buddy := range s.buddies {
			if buddy.BuddyID == quest.BuddyID {
				owned = true
				break
			}
		}
		// Non-CN/minimal catalogs need not contain the four CN learning Buddies.
		if _, present := s.buddyDefinitions[quest.BuddyID]; present && !owned {
			rewards = append(rewards, gamestate.Reward{Type: 19, Num: 1, RewardTypeID: quest.BuddyID, CardSkillLevels: []int16{}})
		}
	}
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
	}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &PresentReceiveResult{}, "圣剑解放奖励"); err != nil {
			return err
		}
	}
	for _, quest := range pending {
		s.unlockedFeatureIDs[uint(32+quest.ArthurType)] = struct{}{}
		var uniqueID int64
		for _, buddy := range s.buddies {
			if buddy.BuddyID == quest.BuddyID {
				uniqueID = buddy.UniqueID
				break
			}
		}
		if uniqueID == 0 {
			continue
		}
		for i := range s.decks {
			deck := &s.decks[i]
			if deck.ArthurType == quest.ArthurType && len(deck.BuddyUniqueIDs) > 0 && deck.BuddyUniqueIDs[0] == 0 {
				deck.BuddyUniqueIDs[0] = uniqueID
			}
		}
	}
	return nil
}

func (s *Account) ArthurBurstUnlocked(arthur int8) int8 {
	if arthur < 1 || arthur > 4 {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, unlocked := s.unlockedFeatureIDs[uint(32+arthur)]
	return int8(BoolInt(unlocked))
}

func (s *Account) ClearBurstStory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
}

func (s *Account) BurstStoryResponse() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append(json.RawMessage(nil), s.storyTeamBattleSession.Response...)
}

func (s *Account) burstQuestAvailableLocked(quest gamestate.BurstQuest) bool {
	_, profession := s.unlockedFeatureIDs[uint(quest.ArthurType-1)]
	_, buddy := s.buddyDefinitions[quest.BuddyID]
	_, script := s.storyBattleIDsByStory[fmt.Sprintf("sub:%d", quest.StoryIDs[2])]
	return profession && buddy && script && s.onboarding.Step >= cnOnboardingStepCount
}

// The original solo picker supports story-only groups. Publish only the
// selected profession and keep these entries out of multiplayer room creation.
func (s *Account) WithBurstQuest(configuration json.RawMessage, arthur int8) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	quest := gamestate.BurstQuests()[arthur-1]
	if !s.burstQuestAvailableLocked(quest) {
		return configuration, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, err
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(top["12"], &groups); err != nil {
		return nil, err
	}
	stories := make([]any, 0, 3)
	titles := [3]string{"新的王者之剑", "与暗影的对话", "圣剑解放"}
	for i, id := range quest.StoryIDs {
		// Later chapters are published after their prerequisite finishes. This
		// avoids misusing the original cross-quest prerequisite dialog fields.
		if i > int(s.burstProgress[arthur-1]) {
			break
		}
		state := 1
		if i < int(s.burstProgress[arthur-1]) {
			state = 2
		}
		stories = append(stories, map[string]any{
			"0": id, "1": id, "2": state, "3": titles[i], "4": 0,
			// TeamBattleSoloShowReceive reads unlock fields unconditionally,
			// including for unlocked stories (is_lock=0).
			"5": map[string]any{"0": "", "1": "", "2": "", "3": 0, "4": "", "5": "", "6": ""},
		})
	}
	group, err := json.Marshal(map[string]any{
		"0": quest.GroupID, "1": 0, "2": 0, "3": 0, "4": quest.Name,
		"5": 0, "6": 0, "7": quest.PictID, "8": arthur, "9": 0,
		"10": []any{}, "11": stories, "12": []string{}, "13": []string{},
		"14": 0, "15": 0,
	})
	if err != nil {
		return nil, err
	}
	top["12"], err = json.Marshal(append([]json.RawMessage{group}, groups...))
	if err != nil {
		return nil, err
	}
	return json.Marshal(top)
}

func (s *Account) BeginBurstStory(storyID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, step, exists := gamestate.FindBurstStory(storyID)
	if !exists || !s.burstQuestAvailableLocked(quest) || step > int(s.burstProgress[quest.ArthurType-1]) {
		return errors.New("unknown or locked sword-release story")
	}
	s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{StoryID: storyID}
	s.activeMainStoryID, s.activeSubStoryID = 0, 0
	s.activeMainStoryCN = false
	return nil
}

type burstStoryResult struct {
	Clear  PresentReceiveResult
	Unlock PresentReceiveResult
	Arthur int8
	Decks  []DeckInfo
}

func (s *Account) EndBurstStory() (burstStoryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, step, exists := gamestate.FindBurstStory(s.storyTeamBattleSession.StoryID)
	if !exists || !s.burstQuestAvailableLocked(quest) || step > int(s.burstProgress[quest.ArthurType-1]) {
		return burstStoryResult{}, ErrNoActiveBurstStory
	}
	result := burstStoryResult{}
	if step < int(s.burstProgress[quest.ArthurType-1]) {
		return result, nil
	}
	// The original StoryMgr uses IsEnableWeb(STORY=2), which is false, so
	// its fixed battle runs locally without protocol 89. Protocol 120 follows
	// the script's StoryEnd; only a win continues the final script to it.
	// Validate the active chapter and prerequisites, not an absent handshake.
	rewards := []gamestate.Reward{s.storyRewardPolicy.SubFirstClear}
	_, alreadyUnlocked := s.unlockedFeatureIDs[uint(32+quest.ArthurType)]
	if step == 2 && !alreadyUnlocked {
		rewards = append(rewards, gamestate.Reward{Type: 19, Num: 1, RewardTypeID: quest.BuddyID, CardSkillLevels: []int16{}})
	}
	// Validate all inventory/currency changes before granting anything. The
	// unlock card must be in inventory so the original unlock scene can use it.
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return result, err
		}
	}
	if err := s.validateRewardBatchCapacityLocked(rewards); err != nil {
		return result, err
	}
	if err := s.applyRewardLocked(rewards[0], &result.Clear); err != nil {
		return result, err
	}
	if len(rewards) == 2 {
		if err := s.applyRewardLocked(rewards[1], &result.Unlock); err != nil {
			return result, err
		}
		uniqueID := result.Unlock.Buddies[0].UniqueID
		for i := range s.decks {
			deck := &s.decks[i]
			if deck.ArthurType == quest.ArthurType && len(deck.BuddyUniqueIDs) > 0 && deck.BuddyUniqueIDs[0] == 0 {
				deck.BuddyUniqueIDs[0] = uniqueID
				result.Decks = append(result.Decks, cloneDeck(*deck))
			}
		}
		s.unlockedFeatureIDs[uint(32+quest.ArthurType)] = struct{}{}
		result.Arthur = quest.ArthurType
	}
	s.burstProgress[quest.ArthurType-1] = uint8(step + 1)
	return result, nil
}
