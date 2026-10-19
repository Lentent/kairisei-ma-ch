package admin

import (
	"errors"
	"strconv"

	"kairisei.local/server/internal/gamestate"
)

// CardCsvData.RARITY is zero based in the client enum, while the runtime
// master exposes the eight native rarities as ranks 1..8.
var customCardRarityCodes = [...]string{"", "NORMAL", "HIGHNORMAL", "RARE", "SUPERRARE", "ULTRARARE", "MILLIONRARE", "EXRARE", "LEGEND"}

// Native ordinary-card progression. LEGEND is the client's special one-level
// rarity, not another ordinary leveling tier. Premium status is independent.
var customRarityLevels = [...]int{0, 15, 20, 30, 40, 50, 60, 80, 1}
var customRarityFame = [...]int{0, 10, 30, 50, 70, 90, 100, 100, 100}

func applyCustomRarityProgression(c customCard, row []string, card *gamestate.Card) {
	if c.RarityRank == nil {
		return
	}
	rank := *c.RarityRank
	level := customRarityLevels[rank]
	// Retain an existing six-star limit-break stage only at the same rarity.
	if rank == 6 && card.RarityRank == 6 && customRowInt(row, 22) >= 1 && customRowInt(row, 22) <= 2 {
		level += 5 * customRowInt(row, 22)
	} else {
		row[22] = ""
	}
	experience := 100 + rank
	if rank == 8 {
		experience = 101
	}
	card.RarityRank, card.LevelMax, card.FameMax = rank, level, customRarityFame[rank]
	card.LoveMax, card.ExperienceTableID = 10000, experience
	row[23], row[24], row[25], row[41] = strconv.Itoa(level), "10000", strconv.Itoa(card.FameMax), strconv.Itoa(experience)
}

func validateCustomCardRarity(c customCard) error {
	if c.RarityRank != nil && (*c.RarityRank < 1 || *c.RarityRank >= len(customCardRarityCodes)) {
		return errors.New("稀有度须为1–8星，或选择沿用模板")
	}
	return nil
}

func customCardTemplateRarities(s customCardSources, cards []customCard) map[int]int {
	ranks := map[int]int{}
	for _, c := range cards {
		row := s.Cards[c.TemplateID]
		if len(row) <= 7 {
			continue
		}
		for rank := 1; rank < len(customCardRarityCodes); rank++ {
			if row[7] == customCardRarityCodes[rank] {
				ranks[c.TemplateID] = rank
				break
			}
		}
	}
	return ranks
}
