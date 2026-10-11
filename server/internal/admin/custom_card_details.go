package admin

import (
	"fmt"
	"strings"
)

// These are display strings in CardCsvData: flavor/credits at 44–46 and
// visible profile at 62–66. Numeric profile data at 47–61 stays inherited.
// Missing/null fields inherit; a present empty string clears the display.
type customCardDetails struct {
	Description  *string `json:"description,omitempty"`
	Illustrator  *string `json:"illustrator,omitempty"`
	VoiceActor   *string `json:"voice_actor,omitempty"`
	Gender       *string `json:"gender,omitempty"`
	Measurements *string `json:"measurements,omitempty"`
	Height       *string `json:"height,omitempty"`
	Weight       *string `json:"weight,omitempty"`
	Hand         *string `json:"hand,omitempty"`
}

type customCardDetailField struct {
	text        *string
	column, max int
	label       string
}

func (d customCardDetails) fields() []customCardDetailField {
	return []customCardDetailField{
		{d.Description, 44, 2000, "角色介绍"},
		{d.Illustrator, 45, 100, "画师"},
		{d.VoiceActor, 46, 100, "声优"},
		{d.Gender, 62, 100, "性别"},
		{d.Measurements, 63, 100, "三围"},
		{d.Height, 64, 100, "身高"},
		{d.Weight, 65, 100, "体重"},
		{d.Hand, 66, 100, "惯用手"},
	}
}

func customCardTemplateDetails(s customCardSources, cards []customCard) map[int]customCardDetails {
	details := map[int]customCardDetails{}
	for _, c := range cards {
		row := s.Cards[c.TemplateID]
		read := func(column int) *string {
			if len(row) <= column {
				return nil
			}
			text := row[column]
			return &text
		}
		details[c.TemplateID] = customCardDetails{read(44), read(45), read(46), read(62), read(63), read(64), read(65), read(66)}
	}
	return details
}

func validateCustomCardDetails(c customCard, row []string) error {
	if c.Details == nil {
		return nil
	}
	for _, field := range c.Details.fields() {
		if field.text == nil {
			continue
		}
		if len(row) <= field.column {
			return fmt.Errorf("模板卡牌表缺少%s字段", field.label)
		}
		text := *field.text
		// Existing native quoted text can be inherited unchanged.
		if text != "" && text != row[field.column] && (!collectionText(text, field.max) || strings.ContainsAny(text, ",\"")) {
			return fmt.Errorf("%s最多%d字，可留空清除，不能包含英文逗号、双引号或换行", field.label, field.max)
		}
	}
	return nil
}

func applyCustomCardDetails(c customCard, row []string) error {
	if err := validateCustomCardDetails(c, row); err != nil {
		return err
	}
	if c.Details != nil {
		for _, field := range c.Details.fields() {
			if field.text != nil {
				row[field.column] = *field.text
			}
		}
	}
	return nil
}
