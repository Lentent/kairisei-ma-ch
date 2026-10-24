package admin

// Difficulty has its own battle identity; matching this ID never credits a
// lower difficulty or a different boss with the same display name.
type missionBossTarget struct {
	BossID     int    `json:"boss_id"`
	Name       string `json:"name"`
	Difficulty string `json:"difficulty"`
}

func (a *API) missionBossTargets() []missionBossTarget {
	groups := a.operations.namedBossGroups(a.groups)
	groups = append(groups, a.operations.namedBossGroups(a.pastGroups)...)
	groups = append(groups, a.operations.customBossGroups()...)
	rows := []missionBossTarget{}
	seen := map[int]bool{}
	for _, group := range groups {
		for i, id := range group.BossIDs {
			if id <= 0 || seen[id] {
				continue
			}
			difficulty := ""
			if i < len(group.Difficulties) {
				difficulty = group.Difficulties[i]
			}
			rows = append(rows, missionBossTarget{BossID: id, Name: group.Name, Difficulty: difficulty})
			seen[id] = true
		}
	}
	return rows
}
