package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"kairisei.local/server/internal/release"
)

var errNoActiveBurstStory = errors.New("no active sword-release story")

// LOCAL_POLICY: finishing all nine training steps opens all four swords.
// Learning stories remain optional. The feature bits also serve as the
// one-time receipt for each initial Buddy, including after selling it.
func (s *store) completeTrainingBurstLocked() error {
	if s.onboarding.ConfigVersion != cnOnboardingConfigVersion || s.onboarding.Step != cnOnboardingStepCount {
		return nil
	}
	pending := []release.BurstQuest{}
	rewards := []release.Reward{}
	for _, quest := range release.BurstQuests() {
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
			rewards = append(rewards, release.Reward{Type: 19, Num: 1, RewardTypeID: quest.BuddyID, CardSkillLevels: []int16{}})
		}
	}
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
	}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &presentReceiveResult{}, "圣剑解放奖励"); err != nil {
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

func (s *store) arthurBurstUnlocked(arthur int8) int8 {
	if arthur < 1 || arthur > 4 {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, unlocked := s.unlockedFeatureIDs[uint(32+arthur)]
	return int8(boolInt(unlocked))
}

func (s *store) clearBurstStory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storyTeamBattleSession = release.StoryTeamBattleSession{}
}

func (s *store) burstStoryResponse() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append(json.RawMessage(nil), s.storyTeamBattleSession.Response...)
}

func (s *store) burstQuestAvailableLocked(quest release.BurstQuest) bool {
	_, profession := s.unlockedFeatureIDs[uint(quest.ArthurType-1)]
	_, buddy := s.buddyDefinitions[quest.BuddyID]
	_, script := s.storyBattleIDsByStory[fmt.Sprintf("sub:%d", quest.StoryIDs[2])]
	return profession && buddy && script && s.onboarding.Step >= cnOnboardingStepCount
}

// The original solo picker supports story-only groups. Publish only the
// selected profession and keep these entries out of multiplayer room creation.
func (s *store) withBurstQuest(configuration json.RawMessage, arthur int8) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	quest := release.BurstQuests()[arthur-1]
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

func (s *store) beginBurstStory(storyID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, step, exists := release.FindBurstStory(storyID)
	if !exists || !s.burstQuestAvailableLocked(quest) || step > int(s.burstProgress[quest.ArthurType-1]) {
		return errors.New("unknown or locked sword-release story")
	}
	s.storyTeamBattleSession = release.StoryTeamBattleSession{StoryID: storyID}
	s.activeMainStoryID, s.activeSubStoryID = 0, 0
	s.activeMainStoryCN = false
	return nil
}

type burstStoryResult struct {
	Clear  presentReceiveResult
	Unlock presentReceiveResult
	Arthur int8
	Decks  []deckInfo
}

func (s *store) endBurstStory() (burstStoryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, step, exists := release.FindBurstStory(s.storyTeamBattleSession.StoryID)
	if !exists || !s.burstQuestAvailableLocked(quest) || step > int(s.burstProgress[quest.ArthurType-1]) {
		return burstStoryResult{}, errNoActiveBurstStory
	}
	result := burstStoryResult{}
	if step < int(s.burstProgress[quest.ArthurType-1]) {
		return result, nil
	}
	// The original StoryMgr uses IsEnableWeb(STORY=2), which is false, so
	// its fixed battle runs locally without protocol 89. Protocol 120 follows
	// the script's StoryEnd; only a win continues the final script to it.
	// Validate the active chapter and prerequisites, not an absent handshake.
	rewards := []release.Reward{s.storyRewardPolicy.SubFirstClear}
	_, alreadyUnlocked := s.unlockedFeatureIDs[uint(32+quest.ArthurType)]
	if step == 2 && !alreadyUnlocked {
		rewards = append(rewards, release.Reward{Type: 19, Num: 1, RewardTypeID: quest.BuddyID, CardSkillLevels: []int16{}})
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

func (a *API) storyTeamBattleStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StoryID int `json:"story_teambattleid"`
	}
	if err := decodeExact(request, []string{"story_teambattleid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.beginBurstStory(payload.StoryID); err != nil {
		a.writeProtocolResult(writer, map[string]any{}, -6800, "请先完成训练及前一段圣剑解放剧情。")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"seed": 0, "hp": 68000, "hp_max": 68000,
		"cost_initial": 3, "burst_gauge_initial": 0, "hold_max": 5,
	})
}

func (a *API) storyTeamBattleEnd(writer http.ResponseWriter, _ *http.Request) {
	if cached := a.store.burstStoryResponse(); len(cached) > 0 {
		a.writeProtocol(writer, cached)
		return
	}
	result, err := a.store.endBurstStory()
	if err != nil {
		a.store.mu.RLock()
		storyID := a.store.storyTeamBattleSession.StoryID
		a.store.mu.RUnlock()
		a.logger.Warn("reject sword-release story settlement", "story_id", storyID, "error", err)
		message := "学习剧情奖励结算失败，请稍后重试。"
		if errors.Is(err, errNoActiveBurstStory) {
			message = "学习剧情记录已失效，请重新进入本段剧情。"
		}
		a.writeProtocolResult(writer, map[string]any{}, -6800, message)
		return
	}
	payload := a.storyEndPayload(result.Clear)
	payload["burst_unlock"] = []any{}
	if result.Arthur != 0 {
		payload["burst_unlock"] = []any{map[string]any{
			"arthur_type": result.Arthur,
			"rewards":     battleResultRewardsWire(result.Unlock.Rewards),
			"new_buddys":  toWireBuddies(result.Unlock.Buddies),
			"decks":       toWireDecks(result.Decks),
		}}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.store.mu.Lock()
	a.store.storyTeamBattleSession.Response = encoded
	a.store.mu.Unlock()
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, payload)
}
