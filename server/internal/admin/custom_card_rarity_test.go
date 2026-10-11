package admin

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCustomCardRarityIndependentOfFiveStarTemplate(t *testing.T) {
	s := customUnitSources()
	s.Cards[101][7] = "ULTRARARE"
	s.Master.CardTemplates[0].RarityRank = 5
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
		if card.LevelMax != base.LevelMax || card.FameMax != base.FameMax || card.PremiumRarity != base.PremiumRarity || card.ExperienceTableID != base.ExperienceTableID {
			t.Fatal("rarity edit changed inherited growth rules")
		}
	}
	for _, raw := range []string{`{}`, `{"rarity_rank":null}`} {
		var old customCard
		if err := json.Unmarshal([]byte(raw), &old); err != nil {
			t.Fatal(err)
		}
		c.RarityRank = old.RarityRank
		row, _, _, card, err := materializeCustomCard(c, s)
		if err != nil || row[7] != "ULTRARARE" || card.RarityRank != 5 {
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
