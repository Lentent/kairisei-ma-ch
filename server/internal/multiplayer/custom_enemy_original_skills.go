package multiplayer

import (
	"errors"
	"kairisei.local/server/internal/gamestate"
)

// Keep native scheduling, target selection and branch conditions intact. Only
// the attack calculation of a selected slot/branch may be changed.
func (c *CombatCatalog) ValidateOriginalEnemySkills(level CombatEnemyLevel, rows []gamestate.TeamBattleEnemyOriginalSkill) error {
	if err := gamestate.ValidateTeamBattleEnemyOriginalSkills(rows); err != nil {
		return err
	}
	for _, edit := range rows {
		found := false
		for _, a := range level.Actions {
			if a.Slot == edit.Slot && a.SkillID == edit.SkillID && (a.Category == "normal" || a.Category == "skill" || a.Category == "special") {
				found = true
				break
			}
		}
		if !found {
			return errors.New("只能修改当前部位原有的普通／蓄力招式")
		}
		_, roles, err := c.CustomEnemySkill(edit.AttackOverride())
		if err != nil {
			return err
		}
		attack := false
		for _, role := range roles {
			attack = attack || role.Function == "ATTACK_AA"
		}
		if !attack {
			return errors.New("该原有招式没有可编辑的攻击伤害")
		}
	}
	return nil
}
