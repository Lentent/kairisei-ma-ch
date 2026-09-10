package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"slices"

	"kairisei.local/server/internal/release"
)

// Archive card portraits are a view of the effective drop table, not a second
// reward catalog. Older generated masters guessed cards/evo_cards from boss
// names and artwork, independently of the actual reward assignment. Rebuild
// that view whenever the complete runtime catalog is assembled, including for
// existing accounts. Never change rewards to match an inferred portrait.
func projectCNPastBossDropCatalog(groups []json.RawMessage, profiles []release.TeamBattleRewardProfile,
	transitions []release.EvolutionTransition,
) ([]json.RawMessage, error) {
	dropsByBoss := make(map[int][]int, len(profiles))
	for _, profile := range profiles {
		if profile.StageQuestAreaID != 0 || profile.TowerID != 0 {
			continue
		}
		cards := make([]int, 0)
		add := func(reward release.Reward) {
			if reward.Type == 6 && reward.Num > 0 && reward.RewardTypeID > 0 {
				cards = append(cards, reward.RewardTypeID)
			}
		}
		if profile.EnemyDrops == nil {
			for _, reward := range profile.ResultRewards {
				add(reward)
			}
		} else {
			for _, drop := range profile.EnemyDrops {
				if drop.ChancePerMillion == nil || *drop.ChancePerMillion > 0 {
					add(drop.Reward)
				}
			}
		}
		slices.Sort(cards)
		dropsByBoss[profile.BossID] = slices.Compact(cards)
	}
	// Only ordinary evolution belongs here; same-name variants and reversible
	// GuaiLi transformations must not select a different collectible card.
	next := make(map[int][]int)
	for _, transition := range transitions {
		if transition.Type == 0 && transition.FromCardID != transition.ToCardID {
			next[transition.FromCardID] = append(next[transition.FromCardID], transition.ToCardID)
		}
	}
	projected := make([]json.RawMessage, 0, len(groups))
	for _, raw := range groups {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, fmt.Errorf("decode past-boss drop view: %w", err)
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group["13"], &bosses); err != nil {
			return nil, fmt.Errorf("decode past-boss difficulties: %w", err)
		}
		cards := make([]int, 0)
		for _, boss := range bosses {
			var bossID int
			if err := json.Unmarshal(boss["0"], &bossID); err != nil {
				return nil, fmt.Errorf("decode past-boss drop identity: %w", err)
			}
			ids, exists := dropsByBoss[bossID]
			if !exists || len(ids) == 0 {
				return nil, fmt.Errorf("past-boss %d has no obtainable reward card", bossID)
			}
			view := make([]map[string]int, 0, len(ids))
			for _, id := range ids {
				view = append(view, map[string]int{"0": id, "1": 0})
				if !slices.Contains(cards, id) {
					cards = append(cards, id)
				}
			}
			boss["12"], _ = json.Marshal(view)
		}
		if len(cards) == 0 {
			return nil, fmt.Errorf("past-boss group %s has no difficulties", group["0"])
		}
		evolved := make([]int, 0, len(cards))
		for _, id := range cards {
			seen := map[int]bool{id: true}
			queue := []int{id}
			leaves := []int{}
			for i := 0; i < len(queue); i++ {
				current := queue[i]
				if len(next[current]) == 0 {
					leaves = append(leaves, current)
				}
				for _, target := range next[current] {
					if !seen[target] {
						seen[target] = true
						queue = append(queue, target)
					}
				}
			}
			if len(leaves) == 0 {
				return nil, fmt.Errorf("ordinary evolution cycle for past-boss reward %d", id)
			}
			evolved = append(evolved, leaves...)
		}
		slices.Sort(evolved)
		evolved = slices.Compact(evolved)
		group["7"], _ = json.Marshal(cards)
		group["8"], _ = json.Marshal(evolved)
		group["13"], _ = json.Marshal(bosses)
		encoded, err := json.Marshal(group)
		if err != nil {
			return nil, err
		}
		projected = append(projected, encoded)
	}
	return projected, nil
}
