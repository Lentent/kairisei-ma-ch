package admin

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCustomRarityLimitBreakStage(t *testing.T) {
	s := customUnitSources()
	for _, sourceRank := range []int{5, 6} {
		for _, stage := range []string{"1", "2"} {
			row := append([]string(nil), s.Cards[101]...)
			row[22] = stage
			card := s.Master.CardTemplates[0]
			card.RarityRank = sourceRank
			rank := 6
			applyCustomRarityProgression(customCard{RarityRank: &rank}, row, &card)
			want := 60
			if sourceRank == 6 {
				want += 5 * customRowInt(row, 22)
				if row[22] != stage {
					t.Fatal("lost existing six-star limit break")
				}
			} else if row[22] != "" {
				t.Fatal("inherited another rarity's limit break")
			}
			if card.LevelMax != want || card.FameMax != 100 {
				t.Fatal("invalid limit-break progression")
			}
		}
	}
}

func TestCustomCardRarityIndependentOfFiveStarTemplate(t *testing.T) {
	s := customUnitSources()
	s.Cards[101][7] = "ULTRARARE"
	s.Master.CardTemplates[0].RarityRank = 5
	s.Master.CardTemplates[0].FameMax = 90
	s.Master.CardTemplates[0].ExperienceTableID = 105
	s.Cards[101][23], s.Cards[101][24], s.Cards[101][25], s.Cards[101][41] = "50", "10000", "90", "105"
	original := append([]string(nil), s.Cards[101]...)
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	if rank := customCardTemplateRarities(s, []customCard{c})[101]; rank != 5 {
		t.Fatalf("template rank = %d", rank)
	}
	for rank := 1; rank <= 8; rank++ {
		c.RarityRank = &rank
		draft := customCardDraft{Cards: []customCard{c}}
		if err := a.validateCustomCards(&draft, s); err != nil {
			t.Fatalf("rank %d: %v", rank, err)
		}
		raw, err := json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		var saved customCardDraft
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		row, _, _, card, err := materializeCustomCard(saved.Cards[0], s)
		if err != nil {
			t.Fatal(err)
		}
		if row[7] != customCardRarityCodes[rank] || card.RarityRank != rank {
			t.Fatalf("rank %d did not reach client/server: %s, %d", rank, row[7], card.RarityRank)
		}
		base := s.Master.CardTemplates[0]
		levels := []int{0, 15, 20, 30, 40, 50, 60, 80, 1}
		fame := []int{0, 10, 30, 50, 70, 90, 100, 100, 100}
		experience := 100 + rank
		if rank == 8 {
			experience = 101
		}
		if card.LevelMax != levels[rank] || card.FameMax != fame[rank] || card.LoveMax != 10000 || card.PremiumRarity != base.PremiumRarity || card.ExperienceTableID != experience {
			t.Fatal("rarity progression did not follow selected rank")
		}
		if customRowInt(row, 23) != card.LevelMax || customRowInt(row, 24) != card.LoveMax || customRowInt(row, 25) != card.FameMax || customRowInt(row, 41) != card.ExperienceTableID {
			t.Fatal("client/server progression mismatch")
		}
	}
	for _, raw := range []string{`{}`, `{"rarity_rank":null}`} {
		var old customCard
		if err := json.Unmarshal([]byte(raw), &old); err != nil {
			t.Fatal(err)
		}
		c.RarityRank = old.RarityRank
		row, _, _, card, err := materializeCustomCard(c, s)
		if err != nil || row[7] != "ULTRARARE" || card.RarityRank != 5 || card.LevelMax != 50 || card.FameMax != 90 || card.ExperienceTableID != 105 {
			t.Fatalf("old/reset draft lost template rarity: %v", err)
		}
	}
	if !reflect.DeepEqual(original, s.Cards[101]) || s.Master.CardTemplates[0].RarityRank != 5 {
		t.Fatal("export mutated original card")
	}
	for _, rank := range []int{-1, 0, 9, 100} {
		c.RarityRank = &rank
		if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err == nil {
			t.Fatalf("accepted invalid rank %d", rank)
		}
		if _, _, _, _, err := materializeCustomCard(c, s); err == nil {
			t.Fatalf("export accepted invalid rank %d", rank)
		}
	}
}
