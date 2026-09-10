package multiplayer

import "path/filepath"

// LoadOperationsEnemyCatalog reuses the combat parsers without loading skills
// or building an engine. Editors must offer real occupied slots, not guess that
// every enemy party has four targets.
func LoadOperationsEnemyCatalog(root string) (map[int]CombatEnemyParty, map[int]CombatEnemyDefinition, error) {
	parties := make(map[int]CombatEnemyParty)
	enemies := make(map[int]CombatEnemyDefinition)
	err := readCombatCSV(filepath.Join(root, "enemy_party.csv"), func(row []string) error {
		v, err := parseCombatEnemyParty(row)
		if err == nil {
			parties[v.ID] = v
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	err = readCombatCSV(filepath.Join(root, "enemy.csv"), func(row []string) error {
		v, err := parseCombatEnemy(row)
		if err == nil {
			enemies[v.ID] = v
		}
		return err
	})
	return parties, enemies, err
}
