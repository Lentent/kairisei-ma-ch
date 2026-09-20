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

type OperationsCardInfo struct {
	Attribute   string `json:"attribute"`
	Cost        int    `json:"cost"`
	NormalSkill string `json:"normal_skill"`
	ArthurSkill string `json:"arthur_skill"`
}

func LoadOperationsCardCatalog(cardPath, battleRoot string) (map[int]OperationsCardInfo, error) {
	skills := make(map[int]CombatSkillDefinition)
	if err := readCombatCSV(filepath.Join(battleRoot, "skill_player.csv"), func(row []string) error {
		skill, err := parseCombatSkill(row)
		if _, exists := skills[skill.ID]; err == nil && !exists {
			skills[skill.ID] = skill
		}
		return err
	}); err != nil {
		return nil, err
	}
	result := make(map[int]OperationsCardInfo)
	err := readCombatCSV(cardPath, func(row []string) error {
		card, err := parseCombatCard(row)
		if err != nil {
			return err
		}
		normal, arthur := skills[card.NormalSkillID], skills[card.ArthurSkillID]
		result[card.ID] = OperationsCardInfo{Attribute: normal.Attribute, Cost: normal.Cost, NormalSkill: normal.Name, ArthurSkill: arthur.Name}
		return nil
	})
	return result, err
}
