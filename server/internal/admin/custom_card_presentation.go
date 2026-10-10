package admin

import (
	"errors"
	"fmt"
)

func customActionSourceCardIDs(c customCard) []int {
	ids := make([]int, 0, len(c.ActionSources))
	for _, source := range c.ActionSources {
		ids = append(ids, source.CardID)
	}
	return ids
}

func customFirstDirectionRows(c customCard) map[int][]string {
	rows := map[int][]string{}
	for _, row := range c.Roles {
		id := customRowInt(row, 0)
		if _, exists := rows[id]; !exists {
			rows[id] = row
		}
	}
	return rows
}

func customFunctionSkill(c customCard, fn int) []string {
	for _, row := range c.Skills {
		id := customRowInt(row, 49)
		if id == 0 {
			id = customRowInt(row, 0)
		}
		if id == fn {
			return row
		}
	}
	return nil
}

// Player cut-in style is card.csv[35], not skill_role_player.csv[2] (the
// latter controls enemy cut-ins). Actions use the complete role direction
// header, including the 2D script, 3D playlist, hit, charge and movie fields.
// Resolve only existing ordinary-card references; old drafts default to their
// template and retain their own artwork, voice, damage and skill identities.
func customCardPresentation(c customCard, s customCardSources, ready func(int) bool) (string, map[int][]string, error) {
	base, err := s.template(c.TemplateID)
	if err != nil {
		return "", nil, err
	}
	cutin := s.Cards[c.TemplateID][35]
	directions := customFirstDirectionRows(base)
	load := func(id int) (customCard, error) {
		if ready != nil && !ready(id) {
			return customCard{}, errors.New("演出来源卡牌资源不完整")
		}
		return s.template(id)
	}
	if c.CutinTemplateID != 0 {
		if _, err := load(c.CutinTemplateID); err != nil {
			return "", nil, fmt.Errorf("出牌特写来源无效：%w", err)
		}
		cutin = s.Cards[c.CutinTemplateID][35]
	}
	if len(c.ActionSources) > len(directions) {
		return "", nil, errors.New("动作来源数量超过模板效果组数量")
	}
	seen := map[int]bool{}
	for _, source := range c.ActionSources {
		if _, exists := directions[source.FunctionID]; !exists || seen[source.FunctionID] {
			return "", nil, errors.New("动作来源须对应模板已有效果组且不能重复")
		}
		seen[source.FunctionID] = true
		from, err := load(source.CardID)
		if err != nil {
			return "", nil, fmt.Errorf("攻击／技能动作来源无效：%w", err)
		}
		row, exists := customFirstDirectionRows(from)[source.SourceFunctionID]
		if !exists || row[1] == "" || row[3] == "" {
			return "", nil, errors.New("来源效果组须包含完整的2D脚本和3D动作")
		}
		toSkill, fromSkill := customFunctionSkill(base, source.FunctionID), customFunctionSkill(from, source.SourceFunctionID)
		if toSkill == nil || fromSkill == nil || toSkill[10] != fromSkill[10] || toSkill[19] != fromSkill[19] {
			return "", nil, errors.New("动作来源的技能类型和目标范围须与当前效果组相同")
		}
		directions[source.FunctionID] = row
	}
	return cutin, directions, nil
}
