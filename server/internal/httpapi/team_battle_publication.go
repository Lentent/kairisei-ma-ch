package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	cnNormalQuestAreaMin = 100001
	cnNormalQuestAreaMax = 100018
	cnGeneratedGroupMin  = 700000000
	cnGeneratedGroupMax  = 800000000
)

type teamBattlePublicationGroup struct {
	GroupID          int `json:"0"`
	StageType        int `json:"1"`
	StageQuestAreaID int `json:"9"`
	Bosses           []struct {
		BossID     int    `json:"0"`
		Difficulty string `json:"4"`
		State      int    `json:"10"`
		IsModel    int    `json:"14"`
		IsLock     int    `json:"26"`
	} `json:"10"`
}

type stageQuestPublicationProgress struct {
	StageQuest struct {
		AreaID int `json:"areaid"`
		Stages []struct {
			StageID     int `json:"stageid"`
			IsClearDone int `json:"is_clear_done"`
		} `json:"stage_object"`
	} `json:"stage_quest"`
}

// projectCNTeamBattlePublication keeps the original client's four list
// contracts distinct. Normal StageQuest routes are unlocked in account order;
// local activity archives are all published and split by their official 2D/3D
// render contract. Normal quests use their official area order. Activity
// archives use the earliest official boss identity in each group: the CN
// master allocates those identities by content batch, while pict_id is only a
// reusable visual identity and cannot represent first publication order.
func projectCNTeamBattlePublication(
	configuration json.RawMessage,
	stageQuests map[int]json.RawMessage,
	limitedGroupIDs []int,
	tutorialNormalQuest bool,
	tutorialActivity bool,
) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, fmt.Errorf("decode team battle publication: %w", err)
	}
	var source []json.RawMessage
	if err := json.Unmarshal(top["9"], &source); err != nil {
		return nil, fmt.Errorf("decode team battle normal groups: %w", err)
	}
	unlocked, hasNormalQuest, err := unlockedCNNormalQuestAreas(stageQuests)
	if err != nil {
		return nil, err
	}

	normal := make([]json.RawMessage, 0, len(source))
	activity2D := make([]json.RawMessage, 0, len(source))
	activity3D := make([]json.RawMessage, 0, len(source))
	limited := make([]json.RawMessage, 0, len(limitedGroupIDs))
	limitedIDs := make(map[int]struct{}, len(limitedGroupIDs))
	for _, groupID := range limitedGroupIDs {
		if groupID <= 0 {
			return nil, errors.New("team battle publication contains an invalid limited group")
		}
		limitedIDs[groupID] = struct{}{}
	}
	seen := make(map[int]struct{}, len(source))
	for _, raw := range source {
		var group teamBattlePublicationGroup
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, fmt.Errorf("decode team battle group: %w", err)
		}
		if group.GroupID <= 0 {
			return nil, errors.New("team battle publication contains an invalid group")
		}
		seen[group.GroupID] = struct{}{}
		switch {
		case group.StageQuestAreaID >= cnNormalQuestAreaMin &&
			group.StageQuestAreaID <= cnNormalQuestAreaMax:
			if _, available := unlocked[group.StageQuestAreaID]; available {
				projected, projectErr := projectCNNormalQuestGroup(
					raw, stageQuests[group.StageQuestAreaID],
					tutorialNormalQuest && group.StageQuestAreaID == cnNormalQuestAreaMin,
				)
				if projectErr != nil {
					return nil, projectErr
				}
				normal = append(normal, projected)
			}
		case group.GroupID > cnGeneratedGroupMin && group.GroupID < cnGeneratedGroupMax:
			is3D, modeErr := teamBattleGroupIs3D(group)
			if modeErr != nil {
				return nil, modeErr
			}
			// The stock client gives 3D activities their own list even when the
			// historical event was time-limited. The local archive keeps every
			// activity open, so only 2D strengthening-material families use the
			// separate limited/key list.
			if is3D {
				activity3D = append(activity3D, append(json.RawMessage(nil), raw...))
			} else if _, isLimited := limitedIDs[group.GroupID]; isLimited {
				limited = append(limited, append(json.RawMessage(nil), raw...))
			} else {
				activity2D = append(activity2D, append(json.RawMessage(nil), raw...))
			}
		case !hasNormalQuest:
			// Retain the small bootstrap group only when the complete normal
			// quest catalog has not been installed.
			normal = append(normal, append(json.RawMessage(nil), raw...))
		}
	}

	// Preserve any future explicitly categorized official groups without
	// duplicating the generated archive groups projected above.
	for key, destination := range map[string]*[]json.RawMessage{
		"10": &activity3D,
		"11": &limited,
		"12": &activity2D,
	} {
		var groups []json.RawMessage
		if raw, exists := top[key]; exists {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return nil, fmt.Errorf("decode team battle category %s: %w", key, err)
			}
		}
		for _, raw := range groups {
			var identity struct {
				GroupID int `json:"0"`
			}
			if err := json.Unmarshal(raw, &identity); err != nil || identity.GroupID <= 0 {
				return nil, fmt.Errorf("decode team battle category %s identity", key)
			}
			if _, duplicate := seen[identity.GroupID]; duplicate {
				continue
			}
			seen[identity.GroupID] = struct{}{}
			*destination = append(*destination, append(json.RawMessage(nil), raw...))
		}
	}
	if err := sortCNTeamBattleGroupsByFirstPublication(normal, true); err != nil {
		return nil, err
	}
	if err := sortCNTeamBattleGroupsByFirstPublication(activity3D, false); err != nil {
		return nil, err
	}
	if err := sortCNTeamBattleGroupsByFirstPublication(activity2D, false); err != nil {
		return nil, err
	}
	if err := sortCNTeamBattleGroupsByFirstPublication(limited, false); err != nil {
		return nil, err
	}
	if tutorialActivity {
		if len(activity3D) == 0 || len(activity2D) == 0 {
			return nil, errors.New("CN activity tutorial categories are unavailable")
		}
		activity3D[0], err = projectCNTutorialActivityGroup(activity3D[0])
		if err != nil {
			return nil, err
		}
		activity2D[0], err = projectCNTutorialActivityGroup(activity2D[0])
		if err != nil {
			return nil, err
		}
	}

	encoded, err := json.Marshal(normal)
	if err != nil {
		return nil, err
	}
	top["9"] = encoded
	encoded, err = json.Marshal(activity3D)
	if err != nil {
		return nil, err
	}
	top["10"] = encoded
	encoded, err = json.Marshal(limited)
	if err != nil {
		return nil, err
	}
	top["11"] = encoded
	encoded, err = json.Marshal(activity2D)
	if err != nil {
		return nil, err
	}
	top["12"] = encoded
	return json.Marshal(top)
}

// projectCNNormalQuestGroup publishes a permanent account-progression area
// through the client's regular TeamSlSt difficulty-list contract. The
// StageQuest DTO remains server-internal state for sequential clear tracking;
// publishing stage type 11/15 would route the stock client into its
// conquest/occupation UI instead.
func projectCNNormalQuestGroup(
	raw json.RawMessage,
	areaRaw json.RawMessage,
	tutorial bool,
) (json.RawMessage, error) {
	var group map[string]json.RawMessage
	if err := json.Unmarshal(raw, &group); err != nil {
		return nil, fmt.Errorf("decode normal quest group: %w", err)
	}
	var identity teamBattlePublicationGroup
	if err := json.Unmarshal(raw, &identity); err != nil || identity.GroupID <= 0 {
		return nil, errors.New("decode normal quest group identity")
	}
	var progress stageQuestPublicationProgress
	if err := json.Unmarshal(areaRaw, &progress); err != nil ||
		progress.StageQuest.AreaID != identity.StageQuestAreaID ||
		len(progress.StageQuest.Stages) != len(identity.Bosses) {
		return nil, fmt.Errorf("decode normal quest area %d progress", identity.StageQuestAreaID)
	}
	var bosses []map[string]json.RawMessage
	if err := json.Unmarshal(group["10"], &bosses); err != nil || len(bosses) != len(identity.Bosses) {
		return nil, fmt.Errorf("decode normal quest area %d bosses", identity.StageQuestAreaID)
	}

	firstUncleared := len(bosses)
	for index, stage := range progress.StageQuest.Stages {
		if stage.StageID != identity.Bosses[index].BossID || stage.IsClearDone < 0 || stage.IsClearDone > 1 {
			return nil, fmt.Errorf("normal quest area %d progression differs from its group", identity.StageQuestAreaID)
		}
		if stage.IsClearDone == 0 && firstUncleared == len(bosses) {
			firstUncleared = index
		}
	}
	for index := range bosses {
		clear := progress.StageQuest.Stages[index].IsClearDone != 0
		state := 0
		if clear {
			state = 2
		}
		isLock := 0
		if index > firstUncleared {
			isLock = 1
		}
		bosses[index]["10"], _ = json.Marshal(state)
		bosses[index]["26"], _ = json.Marshal(isLock)
	}
	// The client's first onboarding guide targets row zero of the difficulty
	// list and cannot scroll. Its fixed "first step" quest therefore receives
	// only the first stage. As soon as that battle advances onboarding, the
	// complete area topology is published again for ordinary progression.
	if tutorial {
		if identity.StageQuestAreaID != cnNormalQuestAreaMin || firstUncleared != 0 {
			return nil, errors.New("normal quest tutorial does not target the fresh first area")
		}
		bosses = bosses[:1]
	}
	encoded, err := json.Marshal(bosses)
	if err != nil {
		return nil, err
	}
	group["1"] = json.RawMessage("0")
	group["10"] = encoded
	return json.Marshal(group)
}

// projectCNTutorialActivityGroup adapts the inferred local archive topology to
// the stock quest guide. The retained service data does not include historical
// activity grouping, so the archive groups several official difficulty
// identities under one visual family. The client sorts those identities by
// boss ID descending and its fixed guide accepts only row zero. During quest
// 1047, publish the earliest/easiest retained identity for the first family;
// the complete family returns immediately after the tutorial is cleared.
func projectCNTutorialActivityGroup(raw json.RawMessage) (json.RawMessage, error) {
	var group map[string]json.RawMessage
	if err := json.Unmarshal(raw, &group); err != nil {
		return nil, fmt.Errorf("decode activity tutorial group: %w", err)
	}
	var bosses []json.RawMessage
	if err := json.Unmarshal(group["10"], &bosses); err != nil || len(bosses) == 0 {
		return nil, errors.New("decode activity tutorial bosses")
	}
	selected := bosses[0]
	selectedID := 0
	for _, boss := range bosses {
		var identity struct {
			BossID int `json:"0"`
		}
		if err := json.Unmarshal(boss, &identity); err != nil || identity.BossID <= 0 {
			return nil, errors.New("decode activity tutorial boss identity")
		}
		if selectedID == 0 || identity.BossID < selectedID {
			selected = boss
			selectedID = identity.BossID
		}
	}
	encoded, err := json.Marshal([]json.RawMessage{selected})
	if err != nil {
		return nil, err
	}
	group["10"] = encoded
	return json.Marshal(group)
}

// sortCNTeamBattleGroupsByFirstPublication is an INFERRED chronology contract
// derived from official CN master identities. It deliberately does not invent
// wall-clock release dates that are absent from the retained service data.
func sortCNTeamBattleGroupsByFirstPublication(groups []json.RawMessage, normal bool) error {
	type sortableGroup struct {
		raw       json.RawMessage
		groupID   int
		primaryID int
	}
	values := make([]sortableGroup, len(groups))
	for index, raw := range groups {
		var group teamBattlePublicationGroup
		if err := json.Unmarshal(raw, &group); err != nil || group.GroupID <= 0 {
			return errors.New("decode team battle publication order")
		}
		primaryID := group.StageQuestAreaID
		if !normal {
			primaryID = 0
			for _, boss := range group.Bosses {
				if boss.BossID > 0 && (primaryID == 0 || boss.BossID < primaryID) {
					primaryID = boss.BossID
				}
			}
		}
		if primaryID <= 0 {
			return fmt.Errorf("team battle group %d has no publication identity", group.GroupID)
		}
		values[index] = sortableGroup{
			raw: append(json.RawMessage(nil), raw...), groupID: group.GroupID, primaryID: primaryID,
		}
	}
	sort.SliceStable(values, func(left, right int) bool {
		if values[left].primaryID != values[right].primaryID {
			return values[left].primaryID < values[right].primaryID
		}
		return values[left].groupID < values[right].groupID
	})
	for index := range values {
		groups[index] = values[index].raw
	}
	return nil
}

func teamBattleGroupIs3D(group teamBattlePublicationGroup) (bool, error) {
	if len(group.Bosses) == 0 {
		return false, fmt.Errorf("team battle group %d has no bosses", group.GroupID)
	}
	is3D := group.Bosses[0].IsModel == 1
	for _, boss := range group.Bosses {
		if boss.IsModel != 0 && boss.IsModel != 1 {
			return false, fmt.Errorf("team battle group %d has an invalid render mode", group.GroupID)
		}
		if (boss.IsModel == 1) != is3D {
			return false, fmt.Errorf("team battle group %d mixes 2D and 3D bosses", group.GroupID)
		}
	}
	return is3D, nil
}

func unlockedCNNormalQuestAreas(
	stageQuests map[int]json.RawMessage,
) (map[int]struct{}, bool, error) {
	areaIDs := make([]int, 0, len(stageQuests))
	progress := make(map[int]stageQuestPublicationProgress, len(stageQuests))
	for areaID, raw := range stageQuests {
		if areaID < cnNormalQuestAreaMin || areaID > cnNormalQuestAreaMax {
			continue
		}
		var area stageQuestPublicationProgress
		if err := json.Unmarshal(raw, &area); err != nil || area.StageQuest.AreaID != areaID ||
			len(area.StageQuest.Stages) == 0 {
			return nil, false, fmt.Errorf("decode normal quest area %d progress", areaID)
		}
		areaIDs = append(areaIDs, areaID)
		progress[areaID] = area
	}
	if len(areaIDs) == 0 {
		return map[int]struct{}{}, false, nil
	}
	sort.Ints(areaIDs)
	unlocked := make(map[int]struct{}, len(areaIDs))
	previousComplete := true
	for _, areaID := range areaIDs {
		if !previousComplete {
			break
		}
		unlocked[areaID] = struct{}{}
		previousComplete = true
		for _, stage := range progress[areaID].StageQuest.Stages {
			if stage.IsClearDone == 0 {
				previousComplete = false
				break
			}
		}
	}
	return unlocked, true, nil
}

func projectCNStageQuestPublication(configuration json.RawMessage) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, err
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return nil, err
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil || len(stages) == 0 {
		return nil, errors.New("normal quest stage catalog is empty")
	}
	previousClear := true
	for index := range stages {
		released := previousClear
		var isClear int
		if err := json.Unmarshal(stages[index]["is_clear_done"], &isClear); err != nil {
			return nil, errors.New("decode normal quest clear state")
		}
		if released {
			stages[index]["is_release_now"] = json.RawMessage("1")
		} else {
			stages[index]["is_release_now"] = json.RawMessage("0")
		}
		previousClear = released && isClear != 0
	}
	encoded, err := json.Marshal(stages)
	if err != nil {
		return nil, err
	}
	stageQuest["stage_object"] = encoded
	encoded, err = json.Marshal(stageQuest)
	if err != nil {
		return nil, err
	}
	top["stage_quest"] = encoded
	return json.Marshal(top)
}
