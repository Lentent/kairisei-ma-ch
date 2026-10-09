package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// Weekdays use ISO numbering: Monday=1, Sunday=7, in China Standard Time.
// A missing/empty weekday list means every day, preserving older policies.
type BattleGroupSchedule struct {
	GroupID   int   `json:"group_id"`
	StartUnix int64 `json:"start_unix,omitempty"`
	EndUnix   int64 `json:"end_unix,omitempty"`
	Weekdays  []int `json:"weekdays,omitempty"`
}

var battleScheduleZone = time.FixedZone("Asia/Shanghai", 8*60*60)

func normalizeBattleSchedules(schedules []BattleGroupSchedule) error {
	seen := make(map[int]bool, len(schedules))
	for i := range schedules {
		s := &schedules[i]
		if s.GroupID <= 0 || seen[s.GroupID] {
			return errors.New("BOSS独立排期包含无效或重复的组ID")
		}
		seen[s.GroupID] = true
		if s.StartUnix < 0 || s.EndUnix < 0 || (s.EndUnix != 0 && s.EndUnix <= s.StartUnix) {
			return fmt.Errorf("BOSS组 %d：结束时间须晚于开始时间", s.GroupID)
		}
		for _, day := range s.Weekdays {
			if day < 1 || day > 7 {
				return fmt.Errorf("BOSS组 %d：星期须为1至7（周一至周日）", s.GroupID)
			}
		}
		sort.Ints(s.Weekdays)
		s.Weekdays = slices.Compact(s.Weekdays)
	}
	sort.Slice(schedules, func(i, j int) bool { return schedules[i].GroupID < schedules[j].GroupID })
	return nil
}

func (s BattleGroupSchedule) active(now time.Time) bool {
	unix := now.Unix()
	day := (int(now.In(battleScheduleZone).Weekday())+6)%7 + 1
	return unix >= s.StartUnix && (s.EndUnix == 0 || unix < s.EndUnix) &&
		(len(s.Weekdays) == 0 || slices.Contains(s.Weekdays, day))
}

func battlePublicationAllowlist(p TeamBattlePublication, allGroups []int, now time.Time) (map[int]struct{}, error) {
	if now.Unix() < p.StartUnix || (p.EndUnix != 0 && now.Unix() >= p.EndUnix) {
		return map[int]struct{}{}, nil
	}
	ids := p.GroupIDs
	switch p.Mode {
	case "all":
		// Preserve the unrestricted default until a group actually needs hiding.
		closed := false
		for _, s := range p.GroupSchedules {
			if !s.active(now) {
				closed = true
				break
			}
		}
		if !closed {
			return nil, nil
		}
		ids = allGroups
	case "allowlist":
	default:
		return nil, errors.New("CN team battle publication mode is invalid")
	}
	allowed := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("CN team battle publication contains an invalid group ID")
		}
		allowed[id] = struct{}{}
	}
	for _, s := range p.GroupSchedules {
		if !s.active(now) {
			delete(allowed, s.GroupID)
		}
	}
	return allowed, nil
}

// Cache only immutable catalog identities; active schedules are evaluated per request.
func battlePublicationGroupIDs(catalog gamestate.State) (map[string][]int, error) {
	type identity struct {
		ID int `json:"0"`
	}
	var categories struct {
		Source   []identity `json:"9"`
		Activity []identity `json:"10"`
		Keys     []identity `json:"11"`
		Bosses   []identity `json:"12"`
	}
	if len(catalog.TeamBattleSolo) > 0 {
		if err := json.Unmarshal(catalog.TeamBattleSolo, &categories); err != nil {
			return nil, fmt.Errorf("decode publication group identities: %w", err)
		}
	}
	groups := map[string][]int{}
	// Generated activities can live in source category 9 and move to 10/11/12
	// during the per-account projection. Retain these identities as well;
	// presentation filtering never applies this allowlist to normal quests.
	for _, category := range [][]identity{categories.Source, categories.Activity, categories.Keys, categories.Bosses} {
		for _, group := range category {
			groups[teamBattlePublicationKey] = append(groups[teamBattlePublicationKey], group.ID)
		}
	}
	for _, raw := range catalog.TeamBattlePastBossGroups {
		var group identity
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, fmt.Errorf("decode past publication group identity: %w", err)
		}
		groups[PastBattlePublicationKey] = append(groups[PastBattlePublicationKey], group.ID)
	}
	return groups, nil
}
