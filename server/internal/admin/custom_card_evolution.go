package admin

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

const customEvolutionCSVPath = "_local/control/server/cn602-card-master/card_evolution.csv"

type customCardEvolution struct {
	TargetID     int `json:"target_card_id"`
	RecipeFromID int `json:"recipe_from_card_id"`
	RecipeToID   int `json:"recipe_to_card_id"`
}

type customEvolutionRecipe struct {
	Type         int                           `json:"evolution_type"`
	FromID       int                           `json:"from_card_id"`
	ToID         int                           `json:"to_card_id"`
	Name         string                        `json:"name"`
	FromRank     int                           `json:"from_rarity"`
	ToRank       int                           `json:"to_rarity"`
	Gold         int                           `json:"gold"`
	Materials    []gamestate.EvolutionMaterial `json:"materials"`
	MaterialText string                        `json:"material_text"`
}

func customEffectiveRarity(c customCard, s customCardSources) int {
	if c.RarityRank != nil {
		return *c.RarityRank
	}
	return customCardTemplateRarities(s, []customCard{c})[c.TemplateID]
}

// Only borrow complete native NORMAL/GOD recipes. Keep the raw material slots,
// including repeated IDs, so the client and server consume the same recipe.
func customEvolutionRecipeRows(s customCardSources, from, to, kind int) ([]string, gamestate.EvolutionTransition, bool) {
	if kind < 0 || kind > 1 {
		return nil, gamestate.EvolutionTransition{}, false
	}
	if from >= customCardFirstID && from <= customCardLastID {
		return nil, gamestate.EvolutionTransition{}, false
	}
	if to >= customCardFirstID && to <= customCardLastID {
		return nil, gamestate.EvolutionTransition{}, false
	}
	for _, tr := range s.Master.EvolutionTransitions {
		if tr.FromCardID != from || tr.ToCardID != to || tr.Type != kind {
			continue
		}
		for _, row := range s.EvolutionRows {
			if len(row) >= 5 && customRowInt(row, 0) == from && customRowInt(row, 4) == to && row[1] == []string{"NORMAL", "GOD"}[kind] {
				return append([]string(nil), row...), tr, true
			}
		}
	}
	return nil, gamestate.EvolutionTransition{}, false
}

func customEvolutionRecipes(s customCardSources) []customEvolutionRecipe {
	result := []customEvolutionRecipe{}
	for _, tr := range s.Master.EvolutionTransitions {
		if _, _, ok := customEvolutionRecipeRows(s, tr.FromCardID, tr.ToCardID, tr.Type); !ok {
			continue
		}
		from := customEffectiveRarity(customCard{TemplateID: tr.FromCardID}, s)
		to := customEffectiveRarity(customCard{TemplateID: tr.ToCardID}, s)
		if from < 1 || to < 1 || (tr.Type == 0 && to <= from) {
			continue
		}
		row := s.Cards[tr.FromCardID]
		if len(row) < 6 {
			continue
		}
		text := ""
		for _, m := range tr.Materials {
			name := strconv.Itoa(m.CardID)
			if r := s.Cards[m.CardID]; len(r) > 5 {
				name = r[5]
			}
			if text != "" {
				text += "、"
			}
			if tr.Type == 1 {
				text += fmt.Sprintf("%s × %d（每张名声 %d）", name, m.Num, m.Fame)
			} else {
				text += fmt.Sprintf("%s × %d", name, m.Num)
			}
		}
		result = append(result, customEvolutionRecipe{tr.Type, tr.FromCardID, tr.ToCardID, row[5], from, to, tr.Gold, tr.Materials, text})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].FromID != result[j].FromID {
			return result[i].FromID < result[j].FromID
		}
		return result[i].ToID < result[j].ToID
	})
	return result
}

func validateCustomCardEvolutions(d customCardDraft, s customCardSources) error {
	cards := map[int]customCard{}
	for _, c := range d.Cards {
		cards[c.ID] = c
	}
	incoming := map[int]bool{}
	for _, c := range d.Cards {
		if c.Evolution != nil && c.Divergence != nil && c.Evolution.TargetID == c.Divergence.TargetID {
			return errors.New("普通进化与乖离进化不能指向同一目标")
		}
		for kind, ev := range []*customCardEvolution{c.Evolution, c.Divergence} {
			if ev == nil {
				continue
			}
			target, ok := cards[ev.TargetID]
			if !ok || target.ID == c.ID {
				return errors.New("进化目标须为草稿中的另一张自制卡，请先制作进化后的形态")
			}
			if kind == 0 && incoming[target.ID] {
				return errors.New("同一自制卡不能作为多条普通进化链的目标")
			}
			if kind == 0 {
				incoming[target.ID] = true
			}
			if kind == 0 && c.ArthurType != target.ArthurType {
				return errors.New("普通进化前后须为同一职业")
			}
			_, tr, ok := customEvolutionRecipeRows(s, ev.RecipeFromID, ev.RecipeToID, kind)
			if !ok {
				return fmt.Errorf("请选择现有%s路线作为材料配方", []string{"普通进化", "乖离进化"}[kind])
			}
			from, to := customEffectiveRarity(c, s), customEffectiveRarity(target, s)
			if from < 1 || to < 1 || (kind == 0 && to <= from) || from != customEffectiveRarity(customCard{TemplateID: tr.FromCardID}, s) || to != customEffectiveRarity(customCard{TemplateID: tr.ToCardID}, s) {
				return errors.New("普通进化目标星级须提高；普通及乖离进化的前后星级须与各自材料配方一致")
			}
		}
	}
	return nil
}

// Replace every drafted card's outgoing route, so clearing or changing a
// previously published route cannot leave stale client/server transitions.
func materializeCustomEvolutions(d customCardDraft, s customCardSources, master *masterdata.CardRuntimeMaster, cards map[string][][]string) (map[string][][]string, error) {
	if err := validateCustomCardEvolutions(d, s); err != nil {
		return nil, err
	}
	updates := map[string][][]string{}
	parents := map[int]int{}
	for _, c := range d.Cards {
		id := strconv.Itoa(c.ID)
		// Native terminal forms have an "ID," row, not an absent row. The
		// client's IsEvolutionState inspects the target's table entry before
		// getEvolutionMaxRarity follows a NORMAL route. Without that entry it
		// treats the source as terminal and hides its unfilled evolution stars.
		updates[id] = [][]string{{id, ""}}
		if c.Evolution != nil {
			parents[c.Evolution.TargetID] = c.ID
		}
	}
	transitions := []gamestate.EvolutionTransition{}
	for _, tr := range master.EvolutionTransitions {
		if _, edited := updates[strconv.Itoa(tr.FromCardID)]; !edited {
			transitions = append(transitions, tr)
		}
	}
	for _, c := range d.Cards {
		for kind, ev := range []*customCardEvolution{c.Evolution, c.Divergence} {
			if ev == nil {
				continue
			}
			row, tr, _ := customEvolutionRecipeRows(s, ev.RecipeFromID, ev.RecipeToID, kind)
			row[0], row[4] = strconv.Itoa(c.ID), strconv.Itoa(ev.TargetID)
			if updates[row[0]][0][1] == "" {
				updates[row[0]] = nil
			}
			updates[row[0]] = append(updates[row[0]], row)
			tr.FromCardID, tr.ToCardID = c.ID, ev.TargetID
			tr.Materials = append([]gamestate.EvolutionMaterial(nil), tr.Materials...)
			transitions = append(transitions, tr)
		}
	}
	master.EvolutionTransitions = transitions
	// Normal ancestry is acyclic because rarity strictly increases. Join its
	// roots for divergent routes separately: GOD conversions may be bidirectional.
	families := map[int]int{}
	var family func(int) int
	family = func(id int) int {
		if families[id] != id {
			families[id] = family(families[id])
		}
		return families[id]
	}
	for _, c := range d.Cards {
		root := c.ID
		for parents[root] != 0 {
			root = parents[root]
		}
		families[c.ID] = root
	}
	for _, c := range d.Cards {
		if c.Divergence != nil {
			a, b := family(c.ID), family(c.Divergence.TargetID)
			if a > b {
				a, b = b, a
			}
			families[b] = a
		}
	}
	for _, c := range d.Cards {
		root := family(c.ID)
		for _, row := range cards[strconv.Itoa(c.ID)] {
			row[2], row[3] = strconv.Itoa(root), strconv.Itoa(root)
		}
		for i := range master.CardTemplates {
			if master.CardTemplates[i].CardID == c.ID {
				master.CardTemplates[i].SameCardID, master.CardTemplates[i].SameSupportCardID = root, root
			}
		}
	}
	return updates, nil
}
