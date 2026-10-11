package admin

import "fmt"

// CN development_type is an enum: 0=none, 1=decompose, 2=fame training.
// Fame training is separate from same-family fusion and fame_max.
func customCardTemplateFameTraining(s customCardSources, cards []customCard) map[int]bool {
	result := map[int]bool{}
	for _, c := range cards {
		result[c.TemplateID] = customRowInt(s.Cards[c.TemplateID], 95) == 2
	}
	return result
}

func validateCustomCardFame(c customCard, row []string) error {
	if c.AllowFameTraining != nil && len(row) <= 97 {
		return fmt.Errorf("模板卡牌表缺少名声培养字段")
	}
	return nil
}

func applyCustomCardFame(c customCard, row []string) error {
	if err := validateCustomCardFame(c, row); err != nil {
		return err
	}
	if c.AllowFameTraining == nil {
		return nil
	}
	if *c.AllowFameTraining {
		row[95] = "2"
		if customRowInt(row, 97) <= 0 {
			// All trainable cards in the original CN master use 20 dust/fame.
			row[97] = "20"
		}
	} else if customRowInt(row, 95) == 2 {
		row[95] = "0"
	}
	return nil
}
