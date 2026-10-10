package admin

import (
	"errors"
	"strings"
)

// CardCsvData.comments starts at column 67. GetSkillInvokeComment selects
// comments[12] for attack/debuff/special and comments[13] for heal/support/defense.
// Voice IDs live separately at columns 89/90 and remain inherited.
const customCardAttackDialogueColumn = 79
const customCardSupportDialogueColumn = 80

type customCardDialogue struct {
	Attack  string `json:"attack"`
	Support string `json:"support"`
}

func customCardTemplateDialogues(s customCardSources, cards []customCard) map[int]customCardDialogue {
	dialogues := map[int]customCardDialogue{}
	for _, c := range cards {
		row := s.Cards[c.TemplateID]
		if len(row) > customCardSupportDialogueColumn {
			dialogues[c.TemplateID] = customCardDialogue{row[customCardAttackDialogueColumn], row[customCardSupportDialogueColumn]}
		}
	}
	return dialogues
}

func validateCustomCardDialogue(c customCard, row []string) error {
	for i, text := range []*string{c.AttackDialogue, c.SupportDialogue} {
		if text == nil {
			continue // Old drafts inherit; a present empty string clears the line.
		}
		column := customCardAttackDialogueColumn + i
		if len(row) <= column {
			return errors.New("模板卡牌表缺少出牌台词字段")
		}
		if *text != "" && *text != row[column] && (!collectionText(*text, 100) || strings.ContainsAny(*text, ",\"")) {
			return errors.New("出牌台词最多100字，可留空清除，不能包含英文逗号、双引号或换行")
		}
	}
	return nil
}

func applyCustomCardDialogue(c customCard, row []string) error {
	if err := validateCustomCardDialogue(c, row); err != nil {
		return err
	}
	if c.AttackDialogue != nil {
		row[customCardAttackDialogueColumn] = *c.AttackDialogue
	}
	if c.SupportDialogue != nil {
		row[customCardSupportDialogueColumn] = *c.SupportDialogue
	}
	return nil
}
