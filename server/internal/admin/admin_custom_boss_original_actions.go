package admin

import (
	"fmt"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"strings"
)

type customBossOriginalAction struct {
	BattleIndex int      `json:"battle_index"`
	EnemyIndex  int      `json:"enemy_index"`
	Slot        int      `json:"slot"`
	SkillID     int      `json:"skill_id"`
	FunctionID  int      `json:"function_id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Rule        string   `json:"rule"`
	Branch      string   `json:"branch"`
	Effects     []string `json:"effects"`
	Editable    bool     `json:"editable"`
	Reason      string   `json:"reason,omitempty"`
}

func (c *contentStore) customBossOriginalActions(source DropBoss) []customBossOriginalAction {
	rows := []customBossOriginalAction{}
	if c.customSkillCatalog == nil {
		return rows
	}
	for _, t := range source.Targets {
		level := c.customSkillCatalog.EnemyLevels[t.Stats.EnemyID]
		actions := append([]multiplayer.CombatEnemyAction(nil), level.Actions...)
		if level.PassiveSkillID != 0 {
			actions = append(actions, multiplayer.CombatEnemyAction{Slot: -1, Category: "passive", SkillID: level.PassiveSkillID})
		}
		for _, a := range actions {
			if a.SkillID == 0 {
				continue
			}
			base := customBossOriginalAction{BattleIndex: t.BattleIndex, EnemyIndex: t.EnemyIndex, Slot: a.Slot, SkillID: a.SkillID, Category: a.Category,
				Rule: fmt.Sprintf("原行动概率 %d%% · 优先级 %d · 行动消耗 %d · 最多使用 %d 次 · 原目标 %s", a.Rate, a.Priority, a.ActionCost, a.MaxUses, a.Target)}
			if a.AIConditionID != 0 {
				base.Rule += fmt.Sprintf(" · 按原 AI 条件 #%d 判断", a.AIConditionID)
			}
			if a.Category == "passive" {
				base.Rule = "按模板开场被动执行"
			}
			if a.Category == "death" {
				base.Rule = "按模板部位破坏触发条件执行"
			}
			variants := c.customSkillCatalog.EnemySkills[a.SkillID]
			if len(variants) == 0 {
				base.Reason = "缺少技能资料，保留模板行为"
				rows = append(rows, base)
				continue
			}
			seen := map[int]bool{}
			for _, skill := range variants {
				if seen[skill.FunctionID] {
					continue
				}
				seen[skill.FunctionID] = true
				row := base
				row.Name, row.FunctionID = skill.Name, skill.FunctionID
				var conditions []string
				for _, cond := range []struct {
					name   string
					params [5]string
				}{{skill.BranchCondition, skill.BranchParameters}, {skill.BranchCondition2, skill.BranchParameters2}} {
					if cond.name != "" && cond.name != "NULL" && cond.name != "NONE" {
						conditions = append(conditions, cond.name+" ("+strings.Join(cond.params[:], ", ")+")")
					}
				}
				row.Branch = "沿用原分支判断"
				if len(conditions) > 0 {
					row.Branch += "：" + strings.Join(conditions, "；")
				}
				roles := c.customSkillCatalog.EnemySkillRoles[skill.FunctionID]
				row.Effects, _ = multiplayer.DescribeCustomEnemySkill(skill, roles)
				edit := gamestate.TeamBattleEnemyOriginalSkill{Slot: a.Slot, SkillID: a.SkillID, FunctionID: skill.FunctionID}
				if err := c.customSkillCatalog.ValidateOriginalEnemySkills(level, []gamestate.TeamBattleEnemyOriginalSkill{edit}); err != nil {
					row.Reason = err.Error()
				} else {
					row.Editable = true
				}
				rows = append(rows, row)
			}
		}
	}
	return rows
}
