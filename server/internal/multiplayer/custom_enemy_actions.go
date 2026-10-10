package multiplayer

import (
	"errors"
	"kairisei.local/server/internal/gamestate"
	"path/filepath"
)

// Load only the existing enemy skill definitions needed by the editor.
func LoadOperationsEnemySkills(root string) (*CombatCatalog, error) {
	c := &CombatCatalog{EnemySkills: map[int][]CombatSkillDefinition{}, EnemySkillRoles: map[int][]CombatSkillRole{}}
	if err := readCombatCSV(filepath.Join(root, "skill_enemy.csv"), func(row []string) error {
		s, err := parseCombatSkill(row)
		if err == nil {
			c.EnemySkills[s.ID] = append(c.EnemySkills[s.ID], s)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := readCombatCSV(filepath.Join(root, "skill_role_enemy.csv"), func(row []string) error {
		r, err := parseCombatSkillRole(row)
		if err == nil {
			r.RoleIndex = len(c.EnemySkillRoles[r.SkillID])
			c.EnemySkillRoles[r.SkillID] = append(c.EnemySkillRoles[r.SkillID], r)
		}
		return err
	}); err != nil {
		return nil, err
	}
	return c, nil
}

// Selecting a concrete function freezes that variant instead of inheriting
// the donor's HP/part/AI branch conditions, which may not exist on this boss.
func (c *CombatCatalog) CustomEnemySkill(a gamestate.TeamBattleEnemyAction) (CombatSkillDefinition, []CombatSkillRole, error) {
	if c != nil {
		for _, s := range c.EnemySkills[a.SkillID] {
			if s.FunctionID != a.FunctionID {
				continue
			}
			roles := c.EnemySkillRoles[s.FunctionID]
			if _, ok := combatSkillTargetCode(s.Target); !ok {
				break
			}
			if len(roles) == 0 || len(roles) > 5 || roles[0].Function == "NONE" {
				break
			}
			attack := false
			for _, r := range roles {
				if !enemyCombatFunctionRegistered(r.Function) {
					return s, nil, errors.New("招式包含未支持的技能效果")
				}
				attack = attack || r.Function == "ATTACK_AA"
			}
			if !attack && (a.Power != nil || a.PowerRate != nil || a.Hits != nil) {
				return s, nil, errors.New("只有攻击招式可以修改伤害和攻击次数")
			}
			if a.AnimationFunctionID > 0 && !CompatibleCustomEnemyAnimation(roles, c.EnemySkillRoles[a.AnimationFunctionID]) {
				return s, nil, errors.New("演出须使用效果数量和类型兼容的招式")
			}
			edited, err := applyCustomEnemyBuffs(s, roles, a.Buffs)
			return s, edited, err
		}
	}
	return CombatSkillDefinition{}, nil, errors.New("招式不存在或缺少完整的技能效果")
}

func (engine *BattleEngine) customEnemyActionPlan(enemy *battleEnemy) []enemyActionCandidate {
	var plan []enemyActionCandidate
	for i := range enemy.CustomActions {
		a := &enemy.CustomActions[i]
		if !a.MatchesTurn(engine.turn) {
			continue
		}
		plan = append(plan, enemyActionCandidate{action: CombatEnemyAction{SkillID: a.SkillID, Priority: i, Target: a.Target}, custom: a})
	}
	return plan
}

func customEnemyTarget(a gamestate.TeamBattleEnemyAction, s CombatSkillDefinition) string {
	if a.Target != "AUTO" {
		return a.Target
	}
	switch s.Target {
	case "SELF", "USER_ALL", "ENEMY_ALL", "DEAD_ENEMY_ALL":
		return s.Target
	case "ENEMY_ONE":
		return "ENEMY_ONE"
	case "DEAD_ENEMY_ONE":
		return "DEAD_ENEMY_RANDOM"
	default:
		return "RANDOM"
	}
}
