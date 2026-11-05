package admin

import (
	"fmt"
	"sort"
	"strconv"
)

// Compile per-role prerequisites into native skill branches. Both local battles
// and server battles then select the same role set using DECK_COMBO_COUNT.
func compileCustomCardChains(cardID int, skills, roles [][]string) ([][]string, [][]string, error) {
	byFunction := map[string][][]string{}
	next := customSkillFirstID + (cardID-customCardFirstID)*64
	limit := next + 64
	for _, row := range roles {
		byFunction[row[0]] = append(byFunction[row[0]], row)
		if n := customRowInt(row, 0); n >= next {
			next = n + 1
		}
	}
	for _, row := range skills {
		for _, col := range []int{0, 49} {
			if n := customRowInt(row, col); n >= next {
				next = n + 1
			}
		}
	}
	unconditional := map[string]bool{}
	var result [][]string
	counts := map[string]int{}
	for _, skill := range skills {
		group := byFunction[skill[49]]
		hasBoost := false
		thresholds := map[int]bool{0: true}
		for _, role := range group {
			hasBoost = hasBoost || role[8] == customPowerBoost
			if n := customRowInt(role, 33); n > 0 {
				thresholds[n] = true
			}
		}
		bounds := make([]int, 0, len(thresholds))
		for n := range thresholds {
			bounds = append(bounds, n)
		}
		sort.Ints(bounds)
		if len(bounds) == 1 {
			if hasBoost {
				unconditional[skill[49]] = true
			}
			result = append(result, skill)
			counts[skill[0]]++
			continue
		}
		slot := 36
		if skill[slot] != "" {
			slot = 42
		}
		if skill[slot] != "" {
			return nil, nil, fmt.Errorf("卡牌%d的模板已占用两个原生条件，无法再添加连携前提，请更换模板", cardID)
		}
		for index, lower := range bounds {
			if next >= limit {
				return nil, nil, fmt.Errorf("卡牌%d的连携分支编号超出容量", cardID)
			}
			upper := 4
			if index+1 < len(bounds) {
				upper = bounds[index+1] - 1
			}
			branch := append([]string(nil), skill...)
			branch[slot] = "DECK_COMBO_COUNT"
			for i := 1; i <= 5; i++ {
				branch[slot+i] = ""
			}
			branch[slot+1], branch[slot+2] = strconv.Itoa(lower), strconv.Itoa(upper)
			branch[49] = strconv.Itoa(next)
			next++
			percent := customBoostPercent(group, lower)
			activeDisplay := false
			for i, role := range group {
				copyRole := append([]string(nil), role...)
				copyRole[0] = branch[49]
				if customRowInt(role, 33) > lower {
					// Keep role positions stable for native description placeholders.
					copyRole[8] = "ATK_OP_PIERCING"
					for col := 20; col < 30; col++ {
						copyRole[col] = "0"
					}
					if len(copyRole) > 33 {
						copyRole = copyRole[:33]
					}
				} else if i+1 == customRowInt(skill, 5) && !isCustomSpecialRole(role) {
					activeDisplay = true
				}
				if err := applyCustomPower(copyRole, percent); err != nil {
					return nil, nil, err
				}
				roles = append(roles, copyRole)
			}
			if !activeDisplay {
				branch[4], branch[5], branch[6] = "0", "0", "0"
			}
			result = append(result, branch)
			counts[skill[0]]++
		}
	}
	for id, count := range counts {
		if count > customClientSkillGroupLimit {
			return nil, nil, fmt.Errorf("卡牌%d的技能%s展开连携条件后有%d个分支，客户端最多支持5个，请减少条件或更换模板", cardID, id, count)
		}
	}
	// Apply unconditional boosts once per original group, even if several skills share it.
	for fn := range unconditional {
		group := byFunction[fn]
		percent := customBoostPercent(group, 0)
		for _, role := range group {
			if err := applyCustomPower(role, percent); err != nil {
				return nil, nil, err
			}
		}
	}
	// Original groups may still be included in exports; never leak custom enums.
	for _, role := range roles {
		if role[8] == customPowerBoost {
			if err := applyCustomPower(role, 0); err != nil {
				return nil, nil, err
			}
		}
	}
	return result, roles, nil
}
