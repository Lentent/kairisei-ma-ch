package admin

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func customDivergenceFixture(t *testing.T) (customCardSources, customCardDraft) {
	t.Helper()
	s, d := customEvolutionFixture(t)
	s.Cards[103] = append([]string(nil), s.Cards[102]...)
	s.Cards[103][0] = "103"
	base := s.Master.CardTemplates[1]
	base.CardID = 103
	s.Master.CardTemplates = append(s.Master.CardTemplates, base)
	for _, pair := range [][2]int{{102, 103}, {103, 102}, {101, 103}} {
		s.Master.EvolutionTransitions = append(s.Master.EvolutionTransitions, gamestate.EvolutionTransition{
			FromCardID: pair[0], ToCardID: pair[1], Type: 1, Gold: 20000, KeepLevel: true,
			Materials: []gamestate.EvolutionMaterial{{CardID: 201, Num: 2, Fame: 3}},
		})
		s.EvolutionRows = append(s.EvolutionRows, []string{strconv.Itoa(pair[0]), "GOD", "1", "20000", strconv.Itoa(pair[1]), "201", "3", "1", "201", "3", "1", ""})
	}
	target := d.Cards[1]
	target.ID, target.TemplateID, target.ArthurType = customCardFirstID+2, 103, 2
	d.Cards = append(d.Cards, target)
	d.Cards[1].Divergence = &customCardEvolution{target.ID, 102, 103}
	d.Cards[2].Divergence = &customCardEvolution{d.Cards[1].ID, 103, 102}
	return s, d
}

func TestCustomCardDivergenceValidationAndReplacement(t *testing.T) {
	s, d := customDivergenceFixture(t)
	if err := validateCustomCardEvolutions(d, s); err != nil {
		t.Fatal("same-rarity cross-job bidirectional routes rejected", err)
	}
	recipes := customEvolutionRecipes(s)
	if len(recipes) != 4 {
		t.Fatal("normal/divergent recipe list incomplete", recipes)
	}
	for _, r := range recipes {
		if r.Type == 1 && !strings.Contains(r.MaterialText, "每张名声 3") {
			t.Fatal("missing per-card fame cost", r)
		}
	}
	raw, _ := json.Marshal(d)
	var saved customCardDraft
	if err := json.Unmarshal(raw, &saved); err != nil || !reflect.DeepEqual(saved, d) {
		t.Fatal("divergent draft did not round trip", err)
	}
	for _, kind := range []string{"missing", "self", "rarity", "wrong-type", "same-target", "csv-type"} {
		t.Run(kind, func(t *testing.T) {
			var bad customCardDraft
			_ = json.Unmarshal(raw, &bad)
			ss := s
			switch kind {
			case "missing":
				bad.Cards[1].Divergence.TargetID += 99
			case "self":
				bad.Cards[1].Divergence.TargetID = bad.Cards[1].ID
			case "rarity":
				rank := 5
				bad.Cards[2].RarityRank = &rank
			case "wrong-type":
				bad.Cards[1].Divergence.RecipeFromID, bad.Cards[1].Divergence.RecipeToID = 101, 102
			case "same-target":
				bad.Cards[0].Divergence = &customCardEvolution{bad.Cards[1].ID, 101, 103}
			case "csv-type":
				ss.EvolutionRows = append([][]string(nil), s.EvolutionRows...)
				ss.EvolutionRows[1] = append([]string(nil), ss.EvolutionRows[1]...)
				ss.EvolutionRows[1][1] = "NORMAL"
			}
			if err := validateCustomCardEvolutions(bad, ss); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
	// One source can offer both paths. Preserve native GOD slots and level flag.
	d.Cards[0].Divergence = &customCardEvolution{d.Cards[2].ID, 101, 103}
	master := s.Master
	master.CardTemplates = append([]gamestate.Card(nil), master.CardTemplates...)
	cards := map[string][][]string{}
	for _, c := range d.Cards {
		cards[strconv.Itoa(c.ID)] = [][]string{{strconv.Itoa(c.ID), "", "", ""}}
		master.CardTemplates = append(master.CardTemplates, gamestate.Card{CardID: c.ID})
	}
	for i := 0; i < 2; i++ {
		updates, err := materializeCustomEvolutions(d, s, &master, cards)
		if err != nil {
			t.Fatal(err)
		}
		if len(master.EvolutionTransitions) != 8 || len(updates[strconv.Itoa(d.Cards[0].ID)]) != 2 {
			t.Fatal("normal/GOD branches missing or duplicated", master.EvolutionTransitions, updates)
		}
		for _, c := range d.Cards {
			if cards[strconv.Itoa(c.ID)][0][2] != strconv.Itoa(d.Cards[0].ID) {
				t.Fatal("connected forms do not share family", cards)
			}
			if c.Divergence != nil {
				want, tr, _ := customEvolutionRecipeRows(s, c.Divergence.RecipeFromID, c.Divergence.RecipeToID, 1)
				want[0], want[4] = strconv.Itoa(c.ID), strconv.Itoa(c.Divergence.TargetID)
				rows := updates[strconv.Itoa(c.ID)]
				if !reflect.DeepEqual(rows[len(rows)-1], want) {
					t.Fatal("client fame slots or KeepLevel flag changed", rows, want)
				}
				found := false
				for _, actual := range master.EvolutionTransitions {
					if actual.FromCardID == c.ID && actual.ToCardID == c.Divergence.TargetID {
						tr.FromCardID, tr.ToCardID = c.ID, c.Divergence.TargetID
						found = reflect.DeepEqual(actual, tr)
					}
				}
				if !found {
					t.Fatal("server GOD recipe changed")
				}
			}
		}
		assertCustomEvolutionStars(t, append(updates[strconv.Itoa(d.Cards[0].ID)], updates[strconv.Itoa(d.Cards[1].ID)]...), map[int]int{d.Cards[0].ID: 5, d.Cards[1].ID: 6}, d.Cards[0].ID, 6)
	}
	// Remove only the forward direction: the reverse still works and keeps the family.
	d.Cards[0].Divergence, d.Cards[1].Divergence = nil, nil
	updates, err := materializeCustomEvolutions(d, s, &master, cards)
	if err != nil || len(master.EvolutionTransitions) != 6 || updates[strconv.Itoa(d.Cards[1].ID)][0][1] != "" || updates[strconv.Itoa(d.Cards[2].ID)][0][1] != "GOD" {
		t.Fatal("directional removal incorrect", err, updates)
	}
	d.Cards[2].Divergence = nil
	if _, err := materializeCustomEvolutions(d, s, &master, cards); err != nil || len(master.EvolutionTransitions) != 5 || cards[strconv.Itoa(d.Cards[2].ID)][0][2] != strconv.Itoa(d.Cards[2].ID) {
		t.Fatal("removed GOD route/family remains", err)
	}
}
