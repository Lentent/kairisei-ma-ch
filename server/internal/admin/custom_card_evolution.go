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

// Only borrow complete native NORMAL recipes. Keep the raw material slots,
// including repeated IDs, so the client and server consume the same recipe.
func customEvolutionRecipeRows(s customCardSources, from, to int) ([]string, gamestate.EvolutionTransition, bool) {
	if from >= customCardFirstID && from <= customCardLastID {
		return nil, gamestate.EvolutionTransition{}, false
	}
	if to >= customCardFirstID && to <= customCardLastID {
		return nil, gamestate.EvolutionTransition{}, false
	}
	for _, tr := range s.Master.EvolutionTransitions {
		if tr.FromCardID != from || tr.ToCardID != to || tr.Type != 0 {
			continue
		}
		for _, row := range s.EvolutionRows {
			if len(row) >= 5 && customRowInt(row, 0) == from && customRowInt(row, 4) == to && row[1] == "NORMAL" {
				return append([]string(nil), row...), tr, true
			}
		}
	}
	return nil, gamestate.EvolutionTransition{}, false
}

func customEvolutionRecipes(s customCardSources) []customEvolutionRecipe {
	result := []customEvolutionRecipe{}
	for _, tr := range s.Master.EvolutionTransitions {
		if _, _, ok := customEvolutionRecipeRows(s, tr.FromCardID, tr.ToCardID); !ok {
			continue
		}
		from := customEffectiveRarity(customCard{TemplateID: tr.FromCardID}, s)
		to := customEffectiveRarity(customCard{TemplateID: tr.ToCardID}, s)
		if from < 1 || to <= from {
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
			text += fmt.Sprintf("%s × %d", name, m.Num)
		}
		result = append(result, customEvolutionRecipe{tr.FromCardID, tr.ToCardID, row[5], from, to, tr.Gold, tr.Materials, text})
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
		if c.Evolution == nil {
			continue
		}
		ev := c.Evolution
		target, ok := cards[ev.TargetID]
		if !ok || target.ID == c.ID {
			return errors.New("进化目标须为草稿中的另一张自制卡，请先制作进化后的形态")
		}
		if incoming[target.ID] {
			return errors.New("同一自制卡不能作为多条普通进化链的目标")
		}
		incoming[target.ID] = true
		if c.ArthurType != target.ArthurType {
			return errors.New("普通进化前后须为同一职业")
		}
		_, tr, ok := customEvolutionRecipeRows(s, ev.RecipeFromID, ev.RecipeToID)
		if !ok {
			return errors.New("请选择现有普通进化路线作为材料配方")
		}
		from, to := customEffectiveRarity(c, s), customEffectiveRarity(target, s)
		if from < 1 || to <= from || from != customEffectiveRarity(customCard{TemplateID: tr.FromCardID}, s) || to != customEffectiveRarity(customCard{TemplateID: tr.ToCardID}, s) {
			return errors.New("进化目标星级须提高，并与所选材料配方的进化前后星级一致")
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
		if c.Evolution == nil {
			continue
		}
		row, tr, _ := customEvolutionRecipeRows(s, c.Evolution.RecipeFromID, c.Evolution.RecipeToID)
		row[0], row[4] = strconv.Itoa(c.ID), strconv.Itoa(c.Evolution.TargetID)
		updates[row[0]] = [][]string{row}
		tr.FromCardID, tr.ToCardID = c.ID, c.Evolution.TargetID
		tr.Materials = append([]gamestate.EvolutionMaterial(nil), tr.Materials...)
		transitions = append(transitions, tr)
	}
	master.EvolutionTransitions = transitions
	for _, c := range d.Cards {
		root := c.ID
		for parents[root] != 0 {
			root = parents[root]
		}
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
